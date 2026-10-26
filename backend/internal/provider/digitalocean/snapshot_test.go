package digitalocean

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"sama/backend/internal/provider"
)

const snapshotJSON = `{"id":"7","name":"safe","resource_id":"42","resource_type":"droplet","created_at":"2026-10-26T10:00:00Z","regions":["ams3"],"size_gigabytes":1.5,"min_disk_size":20,"status":"available","tags":["synthetic-secret"],"contents":"synthetic-secret"}`

func TestSnapshotMetadataWithoutInventedCompletion(t *testing.T) {
	c, b := fixture(t, func(r *http.Request) *http.Response {
		if r.URL.Path == "/v2/snapshots/7" {
			return reply(200, `{"snapshot":`+snapshotJSON+`}`)
		}
		if r.URL.Path != "/v2/snapshots" || r.URL.Query().Get("per_page") != "50" {
			t.Fatal("wrong snapshot route")
		}
		if r.URL.Query().Get("page") == "2" {
			return reply(200, `{"snapshots":[],"links":{}}`)
		}
		return reply(200, `{"snapshots":[`+snapshotJSON+`],"links":{"pages":{"next":"https://api.digitalocean.com/v2/snapshots?page=2&per_page=50"}}}`)
	})
	ctx := context.Background()
	p, err := c.SnapshotPage(ctx, b, "")
	if err != nil || len(p.Data) != 1 || p.Next != "2" || p.Data[0].Status != "unknown" || p.Data[0].NativeStatus != "" || p.Data[0].StatusReason != "not_reported" {
		t.Fatal(p, err)
	}
	data, _ := json.Marshal(p)
	if strings.Contains(string(data), "synthetic-secret") {
		t.Fatal("unsafe metadata")
	}
	o, err := c.ReadSnapshot(ctx, b, "7")
	if err != nil || o.SourceNativeID != "42" || o.SourceType != "compute.server" || *o.SizeGB != 1.5 || o.Regions[0] != "ams3" {
		t.Fatal(o, err)
	}
	empty, err := c.SnapshotPage(ctx, b, p.Next)
	if err != nil || len(empty.Data) != 0 || empty.Next != "" {
		t.Fatal(empty, err)
	}
	wrong, _ := provider.NewBinding(b.Workspace(), b.Connection(), Family, b.Version()+1)
	if _, err = c.ReadSnapshot(ctx, wrong, "7"); err != provider.ErrBinding {
		t.Fatal("binding escaped")
	}
	if _, err = c.ReadSnapshot(ctx, b, "../droplets/42"); err == nil {
		t.Fatal("unsafe identifier")
	}
	volume, err := mapSnapshot(snapshot{ID: "synthetic-volume", ResourceID: "synthetic-source", ResourceType: "volume"})
	if err != nil || volume.SourceType != "storage.volume" || volume.SizeGB != nil || volume.CreatedAt != nil {
		t.Fatal("missing metadata invented", volume, err)
	}
}

func TestSnapshotIncompleteUnsafeAndDeniedReads(t *testing.T) {
	for _, body := range []string{
		`{}`, `{"snapshots":null,"links":{}}`, `{"snapshots":[],"links":null}`,
		`{"snapshots":[` + snapshotJSON + `,{}],"links":{}}`,
		`{"snapshots":[` + strings.Replace(snapshotJSON, `1.5`, `-1`, 1) + `],"links":{}}`,
		`{"snapshots":[` + strings.Replace(snapshotJSON, `"droplet"`, `"unknown-kind"`, 1) + `],"links":{}}`,
		`broken`, strings.Repeat("x", (8<<20)+1),
	} {
		c, b := fixture(t, func(*http.Request) *http.Response { return reply(200, body) })
		p, err := c.SnapshotPage(context.Background(), b, "")
		if err == nil || len(p.Data) != 0 || p.Next != "" {
			t.Fatal("partial scan accepted", err)
		}
	}
	for _, next := range []string{
		"https://evil.example/v2/snapshots?page=2&per_page=50",
		"https://api.digitalocean.com/v2/droplets?page=2&per_page=50",
		"https://api.digitalocean.com/v2/snapshots?page=2&page=2&per_page=50",
		"https://api.digitalocean.com/v2/snapshots?page=1&per_page=50",
		"https://api.digitalocean.com/v2/snapshots?page=2&per_page=50&resource_type=volume",
	} {
		c, b := fixture(t, func(*http.Request) *http.Response {
			return reply(200, fmt.Sprintf(`{"snapshots":[],"links":{"pages":{"next":%q}}}`, next))
		})
		if _, err := c.SnapshotPage(context.Background(), b, ""); err == nil {
			t.Fatal("unsafe pagination")
		}
	}
	for _, code := range []int{401, 403, 429, 500} {
		calls := 0
		c, b := fixture(t, func(*http.Request) *http.Response { calls++; return reply(code, `{"secret":"synthetic-secret"}`) })
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		_, err := c.SnapshotPage(ctx, b, "")
		cancel()
		if err == nil || strings.Contains(err.Error(), "synthetic-secret") || calls < 1 || calls > 5 || code < 429 && calls != 1 {
			t.Fatal("unsafe failure", err, calls)
		}
	}
	c, b := fixture(t, func(*http.Request) *http.Response {
		return reply(200, `{"snapshot":`+strings.Replace(snapshotJSON, `"id":"7"`, `"id":"8"`, 1)+`}`)
	})
	if _, err := c.ReadSnapshot(context.Background(), b, "7"); err == nil {
		t.Fatal("wrong detail identity")
	}
	if _, err := c.SnapshotPage(context.Background(), b, "101"); err == nil {
		t.Fatal("page bound escaped")
	}
}
