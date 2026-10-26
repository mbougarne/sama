package vultr

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"sama/backend/internal/provider"
)

const snapshotJSON = `{"id":"snap-7","date_created":"2026-10-26 10:00:00","size":42949672960,"status":"complete","description":"synthetic-secret","instance_id":"unqualified-source","contents":"synthetic-secret","compressed_size":1078864689}`

func TestSnapshotMetadataPreservesUnavailableSourceAndTimezone(t *testing.T) {
	c, b := fixture(t, func(r *http.Request) *http.Response {
		if r.URL.Path == "/v2/snapshots/snap-7" {
			return reply(200, `{"snapshot":`+snapshotJSON+`}`)
		}
		if r.URL.Path != "/v2/snapshots" || r.URL.Query().Get("per_page") != "50" {
			t.Fatal("wrong snapshot scope")
		}
		if r.URL.Query().Get("cursor") == "next-cursor" {
			return reply(200, `{"snapshots":[],"meta":{"links":{"next":""}}}`)
		}
		return reply(200, `{"snapshots":[`+strings.Replace(snapshotJSON, `"complete"`, `"future-state"`, 1)+`],"meta":{"links":{"next":"next-cursor"}}}`)
	})
	ctx := context.Background()
	p, err := c.SnapshotPage(ctx, b, "")
	if err != nil || len(p.Data) != 1 || p.Next != "2:next-cursor" || p.Data[0].Status != "unknown" || p.Data[0].NativeStatus != "future-state" {
		t.Fatal(p, err)
	}
	encoded, _ := json.Marshal(p)
	if strings.Contains(string(encoded), "synthetic-secret") || strings.Contains(string(encoded), "unqualified-source") {
		t.Fatal("unqualified metadata leaked")
	}
	o, err := c.ReadSnapshot(ctx, b, "snap-7")
	if err != nil || o.Status != "available" || o.SizeBytes == nil || *o.SizeBytes != 42949672960 || o.SizeGB != nil || o.SourceNativeID != "" || o.CreatedAt != nil || o.NativeCreatedAt != "2026-10-26 10:00:00" || o.CreationTimeReason != "timezone_not_reported" {
		t.Fatal(o, err)
	}
	empty, err := c.SnapshotPage(ctx, b, p.Next)
	if err != nil || len(empty.Data) != 0 || empty.Next != "" {
		t.Fatal(empty, err)
	}
	wrong, _ := provider.NewBinding(b.Workspace(), b.Connection(), Family, b.Version()+1)
	if _, err = c.SnapshotPage(ctx, wrong, ""); err != provider.ErrBinding {
		t.Fatal("binding escaped")
	}
	if _, err = c.ReadSnapshot(ctx, b, "../instances/42"); err == nil {
		t.Fatal("unsafe native identifier")
	}
}

func TestSnapshotExplicitInstantAndStates(t *testing.T) {
	for native, want := range map[string]string{"pending": "pending", "complete": "available", "deleted": "deleted", "future": "unknown"} {
		o, err := mapSnapshot(snapshot{ID: "snap-7", Status: native, Created: "2026-10-26T10:00:00+02:00"})
		if err != nil || o.Status != want || o.CreatedAt == nil || o.CreatedAt.Hour() != 8 || o.CreatedAt.Location() != time.UTC {
			t.Fatal(o, err)
		}
	}
	o, err := mapSnapshot(snapshot{ID: "snap-7"})
	if err != nil || o.SizeBytes != nil || o.CreatedAt != nil || o.CreationTimeReason != "not_reported" {
		t.Fatal("invented metadata", o, err)
	}
}

func TestSnapshotMalformedPartialAndUnsafePages(t *testing.T) {
	for _, body := range []string{
		`{}`, `{"snapshots":null,"meta":{"links":{}}}`, `{"snapshots":[],"meta":{}}`,
		`{"snapshots":[` + snapshotJSON + `,{}],"meta":{"links":{}}}`,
		`{"snapshots":[` + strings.Replace(snapshotJSON, `42949672960`, `-1`, 1) + `],"meta":{"links":{}}}`,
		`{"snapshots":[` + strings.Replace(snapshotJSON, `2026-10-26 10:00:00`, `not-a-date`, 1) + `],"meta":{"links":{}}}`,
		`{"snapshots":[],"meta":{"links":{"next":"https://evil.example/page"}}}`,
		`broken`, strings.Repeat("x", (8<<20)+1),
	} {
		c, b := fixture(t, func(*http.Request) *http.Response { return reply(200, body) })
		p, err := c.SnapshotPage(context.Background(), b, "")
		if err == nil || len(p.Data) != 0 || p.Next != "" {
			t.Fatal("unsafe partial scan", p, err)
		}
	}
	c, b := fixture(t, func(*http.Request) *http.Response {
		return reply(200, `{"snapshots":[],"meta":{"links":{"next":"same-cursor"}}}`)
	})
	if _, err := c.SnapshotPage(context.Background(), b, "2:same-cursor"); err == nil {
		t.Fatal("cursor loop accepted")
	}
	for _, cursor := range []string{"https://evil.example/page", "1:next-cursor", "101:next-cursor", "02:next-cursor", "2:bad/url"} {
		if _, err := c.SnapshotPage(context.Background(), b, cursor); err == nil {
			t.Fatal("unsafe cursor accepted", cursor)
		}
	}
	if _, err := c.SnapshotPage(context.Background(), b, "100:last-page"); err == nil {
		t.Fatal("unbounded page walk")
	}
	for _, code := range []int{401, 403, 429, 500} {
		calls := 0
		c, b := fixture(t, func(*http.Request) *http.Response { calls++; return reply(code, `{"secret":"synthetic-secret"}`) })
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		_, err := c.SnapshotPage(ctx, b, "")
		cancel()
		if err == nil || strings.Contains(err.Error(), "synthetic-secret") || calls > 5 || code < 429 && calls != 1 {
			t.Fatal("unsafe read error", err, calls)
		}
	}
}
