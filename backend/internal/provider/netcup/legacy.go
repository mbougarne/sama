package netcup

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"sama/backend/internal/connection"
	"sama/backend/internal/platform/outbound"
	"sama/backend/internal/provider"
)

const legacyURL = "https://ccp.netcup.net/run/webservice/servers/endpoint.php?JSON"

// Legacy owns a session for exactly one immutable connection/credential version.
// Factories must retain this client for that version; no global session exists.
type Legacy struct {
	binding    provider.Binding
	credential connection.Credential
	http       *http.Client
	gate       chan struct{}
	session    string
	expires    time.Time
	now        func() time.Time
}

var _ provider.DNSReader = (*Legacy)(nil)
var _ connection.Validator = (*Legacy)(nil)

func NewLegacy(b provider.Binding, credential connection.Credential, budget *outbound.Budget) (*Legacy, error) {
	if b.Family() != LegacyFamily || credential.Type != "netcup_legacy_v1" || credential.Validate() != nil || budget == nil {
		return nil, provider.ErrBinding
	}
	scope := b.Workspace().String() + "/" + b.Connection().String()
	return &Legacy{binding: b, credential: credential, http: budget.Client(outbound.NetcupLegacyDNS, b.Connection(), scope), gate: make(chan struct{}, 1), now: time.Now}, nil
}

func (c *Legacy) Zone(b provider.Binding, name string) (Zone, error) {
	if b != c.binding {
		return Zone{}, provider.ErrBinding
	}
	name, err := zoneName(name)
	if err != nil {
		return Zone{}, err
	}
	return Zone{b, name, name}, nil
}

func (c *Legacy) call(ctx context.Context, action, domain string, out any) error {
	// This is an allowlist of reads, not an arbitrary Legacy API dispatcher.
	if action != "login" && action != "infoDnsZone" && action != "infoDnsRecords" {
		return provider.ErrActionUnsupported
	}
	param := map[string]string{"customernumber": c.credential.CustomerNumber, "apikey": c.credential.APIKey}
	if action == "login" {
		param["apipassword"] = c.credential.APIPassword
	} else {
		param["apisessionid"] = c.session
		param["domainname"] = domain
	}
	body, err := json.Marshal(struct {
		Action string            `json:"action"`
		Param  map[string]string `json:"param"`
	}{action, param})
	if err != nil {
		return provider.InvalidResponse()
	}
	defer clear(body)
	u, err := outbound.NetcupLegacyDNS.URL(legacyURL)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return provider.ErrBinding
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	var response struct {
		Action string          `json:"action"`
		Status string          `json:"status"`
		Code   json.Number     `json:"statuscode"`
		Data   json.RawMessage `json:"responsedata"`
	}
	// JSON reads use POST; shared transport intentionally makes one attempt only.
	if err = outbound.RequestJSON(ctx, c.http, req, &response); err != nil {
		if outbound.IsFailure(err, "provider_permission_denied") {
			c.session = ""
			c.expires = time.Time{}
		}
		return err
	}
	if response.Action != action {
		return provider.InvalidResponse()
	}
	code := string(response.Code)
	number, parseErr := strconv.Atoi(code)
	if len(code) != 4 || parseErr != nil {
		return provider.InvalidResponse()
	}
	if number >= 4000 && number < 5000 {
		c.session = ""
		c.expires = time.Time{}
		// The documented class includes all client errors; it does not prove
		// a permission denial. Do not invent finer native error semantics.
		return &outbound.Failure{Code: "provider_rejected"}
	}
	if number >= 5000 && number < 6000 {
		return &outbound.Failure{Code: "provider_unavailable"}
	}
	if response.Status != "success" || number < 2000 || number >= 3000 || len(response.Data) == 0 || string(response.Data) == "null" {
		return provider.InvalidResponse()
	}
	if action != "login" && (c.credential.ContainsSecret(string(response.Data)) || c.session != "" && strings.Contains(string(response.Data), c.session)) {
		return provider.InvalidResponse()
	}
	if json.Unmarshal(response.Data, out) != nil {
		return provider.InvalidResponse()
	}
	return nil
}

func (c *Legacy) withSession(ctx context.Context, work func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	select {
	case c.gate <- struct{}{}:
	case <-ctx.Done():
		return &outbound.Failure{Code: "provider_unavailable"}
	}
	defer func() { <-c.gate }()
	if ctx.Err() != nil {
		return &outbound.Failure{Code: "provider_unavailable"}
	}
	if c.session == "" || !c.now().Before(c.expires) {
		c.session = ""
		var data struct {
			ID string `json:"apisessionid"`
		}
		if err := c.call(ctx, "login", "", &data); err != nil {
			return err
		}
		if len(data.ID) == 0 || len(data.ID) > 4096 || strings.ContainsAny(data.ID, "\r\n\x00") {
			return provider.InvalidResponse()
		}
		c.session = data.ID
		// Conservative absolute lifetime; never extend from an unsuccessful read.
		c.expires = c.now().Add(14 * time.Minute)
	}
	return work(ctx)
}

func (c *Legacy) Validate(ctx context.Context, credential connection.Credential) (string, []string, error) {
	if credential != c.credential {
		return "", nil, provider.ErrBinding
	}
	err := c.withSession(ctx, func(context.Context) error { return nil })
	if err != nil {
		return "", nil, err
	}
	// The session response does not return native customer identity. The shared
	// save/rotation contract requires provider-derived identity, so the supplied
	// customer number must not be projected as verified account metadata.
	return "", nil, provider.ErrIdentityUnverified
}

// ReadDNS projects one explicitly selected zone through the optional shared DNS
// port. It neither registers this candidate nor qualifies the connection.
func (c *Legacy) ReadDNS(ctx context.Context, b provider.Binding, nativeZone string) (provider.DNSObservation, error) {
	zone, err := c.Zone(b, nativeZone)
	if err != nil {
		return provider.DNSObservation{}, err
	}
	result, err := c.ReadZone(ctx, b, zone)
	if err != nil {
		return provider.DNSObservation{}, err
	}
	return provider.DNSObservation{NativeID: result.Zone.nativeID, Name: result.Zone.name, Version: result.Version,
		Fingerprint: result.Fingerprint, ObservedAt: c.now().UTC(), RecordSets: result.RecordSets}, nil
}

type legacyZone struct {
	Name   string      `json:"name"`
	TTL    json.Number `json:"ttl"`
	Serial json.Number `json:"serial"`
}

func (c *Legacy) ReadZone(ctx context.Context, b provider.Binding, zone Zone) (Snapshot, error) {
	if b != c.binding || zone.binding != b || zone.nativeID != zone.name {
		return Snapshot{}, provider.ErrBinding
	}
	var result Snapshot
	err := c.withSession(ctx, func(ctx context.Context) error {
		var first, last legacyZone
		if err := c.call(ctx, "infoDnsZone", zone.name, &first); err != nil {
			return err
		}
		var records struct {
			Data *[]struct {
				ID       json.Number `json:"id"`
				Host     string      `json:"hostname"`
				Type     string      `json:"type"`
				Priority string      `json:"priority"`
				Value    string      `json:"destination"`
			} `json:"dnsrecords"`
		}
		if err := c.call(ctx, "infoDnsRecords", zone.name, &records); err != nil {
			return err
		}
		if err := c.call(ctx, "infoDnsZone", zone.name, &last); err != nil {
			return err
		}
		name, err := zoneName(first.Name)
		if err != nil || name != zone.name || first != last || records.Data == nil || len(*records.Data) > maxRecords {
			return provider.InvalidResponse()
		}
		ttl, err := strconv.ParseInt(string(first.TTL), 10, 64)
		if err != nil || ttl < 1 {
			return provider.InvalidResponse()
		}
		serial, err := provider.NumericID(first.Serial)
		if err != nil {
			return err
		}
		sets := []RecordSet{}
		groups := map[string]int{}
		ids := map[string]bool{}
		for _, record := range *records.Data {
			id, err := provider.NumericID(record.ID)
			if err != nil || ids[id] {
				return provider.InvalidResponse()
			}
			ids[id] = true
			name, err := recordName(record.Host, zone.name)
			if err != nil {
				return err
			}
			kind := strings.ToUpper(record.Type)
			key := name + "/" + kind
			index, ok := groups[key]
			if !ok {
				index = len(sets)
				groups[key] = index
				sets = append(sets, RecordSet{Name: name, Type: kind, TTL: &ttl, TTLSource: "zone", KnownType: knownType(kind)})
			}
			sets[index].Values = append(sets[index].Values, DNSValue{Data: record.Value, Priority: record.Priority})
		}
		result, err = snapshot(zone, serial, sets)
		return err
	})
	return result, err
}
