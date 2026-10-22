package netcup

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"sama/backend/internal/connection"
	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

// All wire examples are synthetic, based on the official Legacy JSON request
// envelope and DomainWebservice types inspected 2026-10-20; no account capture.
type transport func(*http.Request) (*http.Response, error)

func (t transport) RoundTrip(r *http.Request) (*http.Response, error) { return t(r) }
func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}
func binding(t *testing.T, family string, version int64) provider.Binding {
	t.Helper()
	w, _ := uuid.NewV7()
	c, _ := uuid.NewV7()
	b, err := provider.NewBinding(w, c, family, version)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func legacyFixture(t *testing.T, handle func(string, map[string]string) string) (*Legacy, provider.Binding) {
	t.Helper()
	b := binding(t, LegacyFamily, 1)
	c, err := NewLegacy(b, connection.Credential{Type: "netcup_legacy_v1", CustomerNumber: "123456", APIKey: "synthetic-api-key", APIPassword: "synthetic-api-password"}, outbound.NewBudget())
	if err != nil {
		t.Fatal(err)
	}
	c.http = &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "POST" || r.URL.String() != legacyURL || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Authorization") != "" {
			t.Error("wrong endpoint or credential family")
			return response(`{}`), nil
		}
		var input struct {
			Action string            `json:"action"`
			Param  map[string]string `json:"param"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			t.Error("request envelope")
		}
		if input.Param["customernumber"] != "123456" || input.Param["apikey"] != "synthetic-api-key" {
			t.Error("credential binding")
		}
		if input.Action == "login" {
			if input.Param["apipassword"] != "synthetic-api-password" || input.Param["apisessionid"] != "" {
				t.Error("login contract")
			}
		} else if input.Param["apisessionid"] != "synthetic-session" || input.Param["apipassword"] != "" || input.Param["domainname"] != "example.test" {
			t.Error("read session contract")
		}
		return response(handle(input.Action, input.Param)), nil
	})}
	return c, b
}
func success(action, data string) string {
	return `{"action":"` + action + `","status":"success","statuscode":"2000","responsedata":` + data + `}`
}
func login() string { return success("login", `{"apisessionid":"synthetic-session"}`) }

func TestLegacyLoginIsSerializedAndVersionOwned(t *testing.T) {
	var count atomic.Int32
	c, _ := legacyFixture(t, func(action string, _ map[string]string) string {
		if action != "login" {
			t.Error("validation read unrelated product")
		}
		count.Add(1)
		return login()
	})
	parallel := func() {
		t.Helper()
		var wg sync.WaitGroup
		for range 12 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				account, rights, err := c.Validate(context.Background(), c.credential)
				if err != provider.ErrIdentityUnverified || account != "" || len(rights) != 0 {
					t.Error("login projected unverified account identity or rights", err)
				}
			}()
		}
		wg.Wait()
	}
	parallel()
	if count.Load() != 1 {
		t.Fatal("session per caller")
	}
	c.now = func() time.Time { return time.Now().Add(time.Hour) }
	parallel()
	if count.Load() != 2 {
		t.Fatal("expiry was not serialized")
	}
	other, _ := legacyFixture(t, func(string, map[string]string) string { count.Add(1); return login() })
	if _, _, err := other.Validate(context.Background(), other.credential); err != provider.ErrIdentityUnverified || count.Load() != 3 {
		t.Fatal("session shared across clients")
	}
	changed := c.credential
	changed.APIKey = "rotated-synthetic-key"
	if _, _, err := c.Validate(context.Background(), changed); err != provider.ErrBinding {
		t.Fatal("credential version crossed")
	}
}

func TestLegacyReadNormalizesWithoutGrantingWrites(t *testing.T) {
	var calls []string
	c, b := legacyFixture(t, func(action string, _ map[string]string) string {
		calls = append(calls, action)
		switch action {
		case "login":
			return login()
		case "infoDnsZone":
			return success(action, `{"name":"example.test","ttl":"3600","serial":"42"}`)
		case "infoDnsRecords":
			return success(action, `{"dnsrecords":[{"id":"1","hostname":"WWW","type":"a","destination":"192.0.2.4","priority":""},{"id":"2","hostname":"www","type":"A","destination":"192.0.2.3","priority":""},{"id":"3","hostname":"@","type":"TYPE65400","destination":"opaque","priority":""}]}`)
		}
		t.Fatal("write or reseller action")
		return `{}`
	})
	zone, err := c.Zone(b, "EXAMPLE.TEST.")
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.ReadZone(context.Background(), b, zone)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, ",") != "login,infoDnsZone,infoDnsRecords,infoDnsZone" || s.Version != "42" || len(s.RecordSets) != 2 || len(s.Fingerprint) != 64 {
		t.Fatal("bounded complete read")
	}
	if s.RecordSets[0].KnownType || s.RecordSets[0].Immutable != nil || s.RecordSets[0].Name != "example.test" || s.RecordSets[1].Values[0].Data != "192.0.2.3" || *s.RecordSets[1].TTL != 3600 || s.RecordSets[1].TTLSource != "zone" {
		t.Fatal("normalization lost native semantics")
	}
	if CheckWrite(b, s, s) != provider.ErrActionUnsupported {
		t.Fatal("local version enabled unsafe write")
	}
	stale := s
	stale.Fingerprint = "changed"
	if CheckWrite(b, stale, s) != provider.ErrPrecondition {
		t.Fatal("stale review")
	}
	other := binding(t, LegacyFamily, 1)
	if _, err = c.ReadZone(context.Background(), other, zone); err != provider.ErrBinding {
		t.Fatal("cross-workspace read")
	}
	rotated, _ := provider.NewBinding(b.Workspace(), b.Connection(), LegacyFamily, 2)
	if _, err = c.ReadZone(context.Background(), rotated, zone); err != provider.ErrBinding {
		t.Fatal("cross-version read")
	}
	if len(calls) != 4 {
		t.Fatal("provider called after binding rejection")
	}
}

func TestLegacyReadPortProjectsOnlyScopedDNSFacts(t *testing.T) {
	calls := 0
	c, b := legacyFixture(t, func(action string, _ map[string]string) string {
		calls++
		switch action {
		case "login":
			return login()
		case "infoDnsZone":
			return success(action, `{"name":"example.test","ttl":3600,"serial":42}`)
		case "infoDnsRecords":
			return success(action, `{"dnsrecords":[{"id":1,"hostname":"@","type":"TLSA","destination":"3 1 1 abcdef","priority":""}]}`)
		}
		t.Fatal("unapproved action")
		return `{}`
	})
	var reader provider.DNSReader = c
	row, err := reader.ReadDNS(context.Background(), b, "EXAMPLE.TEST.")
	if err != nil {
		t.Fatal(err)
	}
	if row.NativeID != "example.test" || row.Name != "example.test" || row.Version != "42" || row.ObservedAt.IsZero() || len(row.RecordSets) != 1 || !row.RecordSets[0].KnownType || row.RecordSets[0].Immutable != nil {
		t.Fatal("DNS read projection lost or invented provider facts")
	}
	data, err := json.Marshal(row)
	if err != nil || c.credential.ContainsSecret(string(data)) || strings.Contains(string(data), "synthetic-session") || strings.Contains(string(data), "customer_number") || strings.Contains(string(data), "available_actions") {
		t.Fatal("read projection leaked credentials or implied authority")
	}
	rotated, _ := provider.NewBinding(b.Workspace(), b.Connection(), b.Family(), b.Version()+1)
	if _, err := reader.ReadDNS(context.Background(), rotated, "example.test"); err != provider.ErrBinding || calls != 4 {
		t.Fatal("read port crossed credential version or performed I/O on rejected binding")
	}
	if err := c.call(context.Background(), "updateDnsRecords", "example.test", new(any)); err != provider.ErrActionUnsupported || calls != 4 {
		t.Fatal("read candidate dispatched a write")
	}
}

func TestLegacyRejectionIsSafeAndNotRetried(t *testing.T) {
	var calls int
	c, b := legacyFixture(t, func(action string, _ map[string]string) string {
		calls++
		if action == "login" {
			return login()
		}
		return `{"action":"infoDnsZone","status":"error","statuscode":4000,"longmessage":"synthetic-api-password","responsedata":null}`
	})
	zone, _ := c.Zone(b, "example.test")
	_, err := c.ReadZone(context.Background(), b, zone)
	if !outbound.IsFailure(err, "provider_rejected") || calls != 2 || strings.Contains(err.Error(), "synthetic-api-password") || c.session != "" {
		t.Fatal("denial replayed or exposed provider body", err)
	}
}

func TestLegacyHTTPDenialAndTimeoutAreNotReplayed(t *testing.T) {
	for _, code := range []int{0, 401, 403} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			c, b := legacyFixture(t, func(string, map[string]string) string { return login() })
			if _, _, err := c.Validate(context.Background(), c.credential); err != provider.ErrIdentityUnverified {
				t.Fatal(err)
			}
			calls := 0
			c.http = &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
				calls++
				if code == 0 {
					return nil, context.DeadlineExceeded
				}
				r := response(`{"private":"synthetic-api-password"}`)
				r.StatusCode = code
				return r, nil
			})}
			zone, _ := c.Zone(b, "example.test")
			_, err := c.ReadZone(context.Background(), b, zone)
			if err == nil || calls != 1 || strings.Contains(err.Error(), "synthetic-api-password") {
				t.Fatal("denial/timeout replay or disclosure")
			}
			if code != 0 && (!outbound.IsFailure(err, "provider_permission_denied") || c.session != "") {
				t.Fatal("HTTP denial misclassified or session retained")
			}
		})
	}
}

func TestLegacyRejectsTornUnknownAndSecretBearingResponses(t *testing.T) {
	for _, kind := range []string{"changed_serial", "missing_records", "duplicate_id", "missing_serial", "out_of_zone", "secret", "missing_ttl", "too_many"} {
		t.Run(kind, func(t *testing.T) {
			zones := 0
			c, b := legacyFixture(t, func(action string, _ map[string]string) string {
				if action == "login" {
					return login()
				}
				if action == "infoDnsZone" {
					zones++
					serial := "42"
					ttl := "3600"
					if kind == "changed_serial" && zones == 2 {
						serial = "43"
					}
					if kind == "missing_serial" {
						serial = ""
					}
					if kind == "missing_ttl" {
						ttl = ""
					}
					return success(action, `{"name":"example.test","ttl":"`+ttl+`","serial":"`+serial+`"}`)
				}
				switch kind {
				case "missing_records":
					return success(action, `{}`)
				case "duplicate_id":
					return success(action, `{"dnsrecords":[{"id":"1","hostname":"@","type":"A","destination":"192.0.2.1"},{"id":"1","hostname":"@","type":"A","destination":"192.0.2.2"}]}`)
				case "out_of_zone":
					return success(action, `{"dnsrecords":[{"id":"1","hostname":"other.test.","type":"A","destination":"192.0.2.1"}]}`)
				case "secret":
					return success(action, `{"dnsrecords":[{"id":"1","hostname":"@","type":"TXT","destination":"synthetic-api-password"}]}`)
				case "too_many":
					return success(action, `{"dnsrecords":[`+strings.Repeat(`{},`, maxRecords)+`{}]}`)
				}
				return success(action, `{"dnsrecords":[]}`)
			})
			zone, _ := c.Zone(b, "example.test")
			if _, err := c.ReadZone(context.Background(), b, zone); err == nil {
				t.Fatal("untrustworthy snapshot accepted")
			}
		})
	}
}

func TestLegacyCancellationAndNoFallback(t *testing.T) {
	c, b := legacyFixture(t, func(string, map[string]string) string { t.Error("request after canceled wait"); return `{}` })
	c.gate <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := c.Validate(ctx, c.credential); err == nil {
		t.Fatal("canceled waiter")
	}
	<-c.gate
	if _, err := NewLegacy(b, connection.Credential{Type: "bearer_v1", Token: "synthetic-token"}, outbound.NewBudget()); err != provider.ErrBinding {
		t.Fatal("other family accepted")
	}
	for _, raw := range []string{"example.test/path", "https://example.test", "other.test..", "éxample.test"} {
		if _, err := c.Zone(b, raw); err == nil {
			t.Fatal("invalid zone")
		}
	}
}
