package contabo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"sama/backend/internal/provider"
)

const snapshotItem = `{"snapshotId":"snap42","instanceId":42,"name":"safe snapshot","tenantId":"DE","customerId":"54321","createdDate":"2026-10-01T01:02:03Z","description":"synthetic-secret","userData":"synthetic-secret"}`

func snapshotList(item, next string, pages int) string {
	return fmt.Sprintf(`{"data":[%s],"_pagination":{"page":1,"size":50,"totalPages":%d,"totalElements":1},"_links":{"next":%q}}`, item, pages, next)
}
func snapshotFixture(t *testing.T, api func(*http.Request) *http.Response) (*Client, provider.Binding) {
	t.Helper()
	return fixture(t, func(r *http.Request) *http.Response {
		if r.URL.Path == "/v1/compute/instances" {
			return reply(200, strings.ReplaceAll(strings.ReplaceAll(inventory, `"totalPages":2`, `"totalPages":1`), `"next":"/v1/compute/instances?page=2"`, `"next":""`))
		}
		return api(r)
	}, func(*http.Request) *http.Response { return tokenReply() })
}
func TestSnapshotMetadataAndScopedPaging(t *testing.T) {
	ctx := context.Background()
	c, b := snapshotFixture(t, func(r *http.Request) *http.Response {
		if r.URL.Path == "/v1/compute/instances/42/snapshots/snap42" {
			return reply(200, `{"data":[`+snapshotItem+`]}`)
		}
		if r.URL.Query().Get("size") != "50" {
			t.Fatal("unbounded snapshot page")
		}
		if r.URL.Query().Get("page") == "2" {
			return reply(200, `{"data":[],"_pagination":{"page":2,"size":50,"totalPages":2,"totalElements":1},"_links":{}}`)
		}
		return reply(200, snapshotList(snapshotItem, "/v1/compute/instances/42/snapshots?page=2", 2))
	})
	page, err := c.SnapshotPage(ctx, b, "")
	if err != nil || len(page.Data) != 1 || page.Next == "" {
		t.Fatal(page, err)
	}
	o := page.Data[0]
	if o.NativeID != "42/snap42" || o.SourceNativeID != "42" || o.Status != "unknown" || o.StatusReason != "not_reported" || o.NativeStatus != "" || o.SizeBytes != nil || o.SizeGB != nil || o.CreatedAt == nil {
		t.Fatal(o)
	}
	body, _ := json.Marshal(o)
	if strings.Contains(string(body), "synthetic-secret") {
		t.Fatal("metadata leak")
	}
	end, err := c.SnapshotPage(ctx, b, page.Next)
	if err != nil || end.Next != "" || len(end.Data) != 0 {
		t.Fatal(end, err)
	}
	detail, err := c.ReadSnapshot(ctx, b, "42/snap42")
	if err != nil || detail.NativeID != o.NativeID {
		t.Fatal(detail, err)
	}
}
func TestSnapshotInvalidResponsesAndCursors(t *testing.T) {
	valid := snapshotList(snapshotItem, "", 1)
	for _, body := range []string{
		`{}`, `broken`, strings.Repeat("x", (8<<20)+1),
		strings.ReplaceAll(valid, `"instanceId":42`, `"instanceId":43`),
		strings.ReplaceAll(valid, `"snapshotId":"snap42"`, `"snapshotId":"../secret"`),
		strings.ReplaceAll(valid, `2026-10-01T01:02:03Z`, `invalid`),
		strings.ReplaceAll(valid, `"totalPages":1`, `"totalPages":101`),
		strings.ReplaceAll(valid, `"totalPages":1`, `"totalPages":2`),
		snapshotList(snapshotItem, "https://evil.example/page", 2),
		snapshotList(snapshotItem, "/v1/compute/instances/43/snapshots?page=2", 2),
		snapshotList(snapshotItem, "/v1/compute/instances/42/snapshots?page=2&token=secret", 2),
		snapshotList(snapshotItem, "/v1/compute/instances/42/snapshots?page=1", 2),
		snapshotList(snapshotItem+","+snapshotItem, "", 1),
	} {
		c, b := snapshotFixture(t, func(*http.Request) *http.Response { return reply(200, body) })
		if _, err := c.SnapshotPage(context.Background(), b, ""); err == nil {
			t.Fatal("invalid snapshot response accepted")
		}
	}
	c, b := snapshotFixture(t, func(*http.Request) *http.Response { return reply(200, valid) })
	for _, cursor := range []string{"https://evil.example", "1:0:1:101:42", "1:50:1:2:42", "01:0:1:2:42", "1:0:0:2:42", "1:0:1:2:43", "1:0:1:2:../x"} {
		if _, err := c.SnapshotPage(context.Background(), b, cursor); err == nil {
			t.Fatal("cursor accepted", cursor)
		}
	}
	for _, id := range []string{"snap42", "42/../secret", "42/snap42/extra", "0/snap42", "42/%2f"} {
		if _, err := c.ReadSnapshot(context.Background(), b, id); err == nil {
			t.Fatal("unsafe id", id)
		}
	}
	wrong, _ := provider.NewBinding(b.Workspace(), b.Connection(), Family, 2)
	if _, err := c.SnapshotPage(context.Background(), wrong, ""); err != provider.ErrBinding {
		t.Fatal(err)
	}
}
func TestSnapshotWalkAcrossSourcesAndFailure(t *testing.T) {
	ctx := context.Background()
	calls := 0
	c, b := snapshotFixture(t, func(r *http.Request) *http.Response {
		calls++
		if calls == 2 {
			return reply(403, `{}`)
		}
		return reply(200, snapshotList(snapshotItem, "/v1/compute/instances/42/snapshots?page=2", 2))
	})
	page, err := c.SnapshotPage(ctx, b, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.SnapshotPage(ctx, b, page.Next); err == nil {
		t.Fatal("partial scan completed")
	}
	c, b = fixture(t, func(r *http.Request) *http.Response {
		if r.URL.Path == "/v1/compute/instances" {
			return reply(200, emptyInventory)
		}
		t.Fatal("empty inventory fetched snapshots")
		return nil
	}, func(*http.Request) *http.Response { return tokenReply() })
	page, err = c.SnapshotPage(ctx, b, "")
	if err != nil || page.Next != "" || len(page.Data) != 0 {
		t.Fatal(page, err)
	}
	c, b = snapshotFixture(t, func(*http.Request) *http.Response {
		return reply(200, `{"data":[`+strings.ReplaceAll(snapshotItem, `"instanceId":42`, `"instanceId":43`)+`]}`)
	})
	if _, err = c.ReadSnapshot(ctx, b, "42/snap42"); err == nil {
		t.Fatal("foreign source detail")
	}
	c, b = snapshotFixture(t, func(*http.Request) *http.Response {
		return reply(200, snapshotList(snapshotItem, "/v1/compute/instances/42/snapshots?page=2", 2))
	})
	if _, err = c.SnapshotPage(ctx, b, "1:0:1:100:42"); err == nil {
		t.Fatal("walk cap ignored")
	}
}

func TestSnapshotWalkAdvancesInstancePagesAndDetectsReordering(t *testing.T) {
	ctx := context.Background()
	reordered := false
	c, b := fixture(t, func(r *http.Request) *http.Response {
		switch r.URL.Path {
		case "/v1/compute/instances":
			if r.URL.Query().Get("page") == "2" {
				return reply(200, strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(inventory, `"instanceId":42`, `"instanceId":44`), `"page":1`, `"page":2`), `"next":"/v1/compute/instances?page=2"`, `"next":""`))
			}
			first := inventory
			if reordered {
				first = strings.ReplaceAll(first, `"instanceId":42`, `"instanceId":45`)
			}
			return reply(200, first)
		case "/v1/compute/instances/42/snapshots":
			return reply(200, snapshotList(snapshotItem, "", 1))
		case "/v1/compute/instances/44/snapshots":
			return reply(200, snapshotList(strings.ReplaceAll(snapshotItem, `"instanceId":42`, `"instanceId":44`), "", 1))
		}
		t.Fatal("unexpected path", r.URL)
		return nil
	}, func(*http.Request) *http.Response { return tokenReply() })
	first, err := c.SnapshotPage(ctx, b, "")
	if err != nil || first.Next != "2:0:1:2:" {
		t.Fatal(first, err)
	}
	second, err := c.SnapshotPage(ctx, b, first.Next)
	if err != nil || second.Next != "" || second.Data[0].NativeID != "44/snap42" {
		t.Fatal(second, err)
	}
	reordered = true
	if _, err = c.SnapshotPage(ctx, b, "1:0:2:2:42"); err == nil {
		t.Fatal("reordered source accepted")
	}
}
