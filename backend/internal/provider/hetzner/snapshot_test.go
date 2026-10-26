package hetzner

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

const snapshotJSON = `{"id":7,"type":"snapshot","name":null,"status":"available","created":"2026-10-26T10:00:00Z","created_from":{"id":42},"image_size":1.5,"disk_size":20,"description":"synthetic-secret","labels":{"secret":"synthetic-secret"}}`

func TestSnapshotMetadataAndBoundPages(t *testing.T) {
	c, b := fixture(t, func(r *http.Request) *http.Response {
		if r.URL.Path == "/v1/images/7" {
			return reply(200, `{"image":`+snapshotJSON+`}`)
		}
		if r.URL.Path != "/v1/images" || r.URL.Query().Get("type") != "snapshot" || r.URL.Query().Get("per_page") != "50" {
			t.Fatal("wrong snapshot scope")
		}
		page := r.URL.Query().Get("page")
		if page == "2" {
			return reply(200, `{"images":[],"meta":{"pagination":{"page":2,"next_page":null}}}`)
		}
		return reply(200, `{"images":[`+strings.Replace(snapshotJSON, `"available"`, `"future-state"`, 1)+`],"meta":{"pagination":{"page":1,"next_page":2}}}`)
	})
	ctx := context.Background()
	p, err := c.SnapshotPage(ctx, b, "")
	if err != nil || p.Next != "2" || len(p.Data) != 1 || p.Data[0].Status != "unknown" || p.Data[0].NativeStatus != "future-state" {
		t.Fatal(p, err)
	}
	data, _ := json.Marshal(p)
	if strings.Contains(string(data), "synthetic-secret") {
		t.Fatal("unsafe metadata")
	}
	o, err := c.ReadSnapshot(ctx, b, "7")
	if err != nil || o.SourceNativeID != "42" || o.SourceType != "compute.server" || o.Status != "available" || o.SizeGB == nil || *o.SizeGB != 1.5 || o.CreatedAt == nil {
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
	if _, err = c.ReadSnapshot(ctx, b, "../servers/42"); err == nil {
		t.Fatal("unsafe identifier")
	}
}

func TestSnapshotMalformedAndPartialPagesFailClosed(t *testing.T) {
	for _, body := range []string{
		`{}`, `{"images":null,"meta":{"pagination":{"page":1}}}`, `{"images":[],"meta":{"pagination":{"page":2}}}`,
		`{"images":[],"meta":{"pagination":{"page":1,"next_page":1}}}`,
		`{"images":[` + snapshotJSON + `,{}],"meta":{"pagination":{"page":1}}}`,
		`{"images":[` + strings.Replace(snapshotJSON, `"snapshot"`, `"backup"`, 1) + `],"meta":{"pagination":{"page":1}}}`,
		`{"images":[` + strings.Replace(snapshotJSON, `1.5`, `-1`, 1) + `],"meta":{"pagination":{"page":1}}}`,
		`{"images":[` + strings.Replace(snapshotJSON, `"id":42`, `"id":0`, 1) + `],"meta":{"pagination":{"page":1}}}`,
		`not json`, strings.Repeat("x", (8<<20)+1),
	} {
		c, b := fixture(t, func(*http.Request) *http.Response { return reply(200, body) })
		p, err := c.SnapshotPage(context.Background(), b, "")
		if err == nil || len(p.Data) != 0 || p.Next != "" {
			t.Fatal("partial or malformed scan accepted", err)
		}
	}
	for _, code := range []int{401, 403, 429, 500} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			calls := 0
			c, b := fixture(t, func(*http.Request) *http.Response { calls++; return reply(code, `{"error":"synthetic-secret"}`) })
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()
			if _, err := c.SnapshotPage(ctx, b, ""); err == nil || strings.Contains(err.Error(), "synthetic-secret") || calls < 1 || calls > 5 || code < 429 && calls != 1 {
				t.Fatal("unsafe failed read", err, calls)
			}
		})
	}
	c, b := fixture(t, func(*http.Request) *http.Response {
		return reply(200, `{"image":`+strings.Replace(snapshotJSON, `"id":7`, `"id":8`, 1)+`}`)
	})
	if _, err := c.ReadSnapshot(context.Background(), b, "7"); err == nil {
		t.Fatal("wrong snapshot accepted")
	}
	if _, err := c.SnapshotPage(context.Background(), b, "101"); err == nil {
		t.Fatal("page bound escaped")
	}
}
