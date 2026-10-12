package contabo

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"sama/backend/internal/provider"
)

const inventory = `{"data":[{"instanceId":42,"displayName":"safe","status":"new-status","region":"EU","productId":"V5","tenantId":"DE","customerId":"54321","userData":"synthetic-secret","rootPassword":"synthetic-secret"}],"_pagination":{"page":1,"size":50,"totalPages":2,"totalElements":51},"_links":{"next":"/v1/compute/instances?page=2"}}`
const emptyInventory = `{"data":[],"_pagination":{"page":1,"size":50,"totalPages":0,"totalElements":0},"_links":{}}`

func TestInstancePagesDetailsAndIdentity(t *testing.T) {
	c, b := fixture(t, func(r *http.Request) *http.Response {
		switch r.URL.Path {
		case "/v1/me/accounts":
			return reply(200, `{"data":[{"tenantId":"DE","customerId":"54321","default":true,"email":"synthetic-secret"}],"_pagination":{"page":1,"totalElements":1,"totalPages":1}}`)
		case "/v1/compute/instances/42":
			return reply(200, `{"data":[{"instanceId":42,"name":"native","status":"stopped","tenantId":"DE","customerId":"54321"}]}`)
		default:
			return reply(200, inventory)
		}
	}, func(*http.Request) *http.Response { return tokenReply() })
	page, err := c.Page(context.Background(), b, "")
	if err != nil || page.Next != "2" || len(page.Data) != 1 || page.Data[0].Status != "unknown" {
		t.Fatal(page, err)
	}
	encoded, _ := json.Marshal(page)
	if strings.Contains(string(encoded), "synthetic-secret") {
		t.Fatal("secret metadata leaked")
	}
	detail, err := c.Read(context.Background(), b, "42")
	if err != nil || detail.Status != "stopped" || detail.Name != "native" {
		t.Fatal(detail, err)
	}
	account, err := c.DefaultAccountIdentity(context.Background(), b)
	if err != nil || account != "DE/54321" {
		t.Fatal(account, err)
	}
	if account, _, err := c.Validate(context.Background(), c.credential); err != provider.ErrIdentityUnverified || account != "" {
		t.Fatal("unqualified active account enabled")
	}
}

func TestPageBoundsEmptyAndPartialFailure(t *testing.T) {
	for _, body := range []string{
		`{}`, `invalid`, strings.Repeat("x", (8<<20)+1),
		strings.ReplaceAll(inventory, "/v1/compute/instances?page=2", "https://evil.example/page"),
		strings.ReplaceAll(inventory, "page=2", "page=1"),
		strings.ReplaceAll(inventory, "page=2", "page=2&token=secret"),
		strings.ReplaceAll(inventory, `"totalPages":2`, `"totalPages":101`),
		strings.ReplaceAll(inventory, `"instanceId":42`, `"instanceId":0`),
	} {
		c, b := fixture(t, func(*http.Request) *http.Response { return reply(200, body) }, func(*http.Request) *http.Response { return tokenReply() })
		if _, err := c.Page(context.Background(), b, ""); err == nil {
			t.Fatal("unsafe response accepted")
		}
	}
	c, b := fixture(t, func(r *http.Request) *http.Response {
		if r.URL.Query().Get("page") == "2" {
			return reply(403, `{}`)
		}
		return reply(200, emptyInventory)
	}, func(*http.Request) *http.Response { return tokenReply() })
	page, err := c.Page(context.Background(), b, "")
	if err != nil || len(page.Data) != 0 || page.Next != "" {
		t.Fatal(page, err)
	}
	if _, err = c.Page(context.Background(), b, "2"); err == nil {
		t.Fatal("denied later page ignored")
	}
	if _, err = c.Page(context.Background(), b, "https://evil.example"); err == nil {
		t.Fatal("URL cursor")
	}
	c, b = fixture(t, func(*http.Request) *http.Response { return reply(429, `{}`) }, func(*http.Request) *http.Response { return tokenReply() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err = c.Page(ctx, b, ""); err == nil {
		t.Fatal("429 ignored")
	}
}

func TestAmbiguousIdentityAndBindingFailClosed(t *testing.T) {
	c, b := fixture(t, func(*http.Request) *http.Response {
		return reply(200, `{"data":[],"_pagination":{"page":1,"totalElements":0,"totalPages":0}}`)
	}, func(*http.Request) *http.Response { return tokenReply() })
	if _, err := c.DefaultAccountIdentity(context.Background(), b); err != provider.ErrIdentityUnverified {
		t.Fatal("empty account invented")
	}
	wrong, _ := provider.NewBinding(b.Workspace(), b.Connection(), Family, 2)
	if _, err := c.Page(context.Background(), wrong, ""); err != provider.ErrBinding {
		t.Fatal("version fallback")
	}
	if _, err := c.Read(context.Background(), b, "../me/accounts"); err == nil {
		t.Fatal("native path injection")
	}
}
