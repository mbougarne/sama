package outbound

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

type observedBody struct {
	io.Reader
	closed bool
}

func (b *observedBody) Close() error { b.closed = true; return nil }

func TestPermissionDenialStopsDespiteInvalidBody(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		for _, test := range []struct {
			name string
			body func() io.Reader
		}{
			{"unreadable", func() io.Reader { return iotest.ErrReader(errors.New("secret-sentinel")) }},
			{"truncated", func() io.Reader {
				return io.MultiReader(strings.NewReader(`{"token":"secret-sentinel`), iotest.ErrReader(io.ErrUnexpectedEOF))
			}},
			{"oversized", func() io.Reader { return strings.NewReader(strings.Repeat("x", maxMetadataBytes+1)) }},
		} {
			t.Run(http.StatusText(status)+"/"+test.name, func(t *testing.T) {
				calls := 0
				var bodies []*observedBody
				client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					calls++
					body := &observedBody{Reader: test.body()}
					bodies = append(bodies, body)
					return &http.Response{StatusCode: status, Header: http.Header{}, Body: body}, nil
				})}
				request, _ := http.NewRequest(http.MethodGet, "https://provider.example/v1", nil)
				err := RequestJSON(context.Background(), client, request, nil)
				if calls != 1 || !IsFailure(err, "provider_permission_denied") {
					t.Errorf("denial must stop after one attempt: calls=%d error=%v", calls, err)
				}
				if err != nil && strings.Contains(err.Error(), "sentinel") {
					t.Error("secret error")
				}
				for _, body := range bodies {
					if !body.closed {
						t.Error("body leaked")
					}
				}
			})
		}
	}
}

func TestResponseBoundsRetriesAndSecretSafeErrors(t *testing.T) {
	for _, test := range []struct {
		method string
		status int
		body   string
		calls  int
		code   string
	}{
		{"GET", 503, `{"token":"secret-sentinel"}`, 5, "provider_unavailable"},
		{"POST", 503, "secret-sentinel", 1, "provider_unavailable"},
		{"GET", 401, "secret-sentinel", 1, "provider_permission_denied"},
		{"GET", 403, "secret-sentinel", 1, "provider_permission_denied"},
		{"GET", 200, "invalid-secret-sentinel", 1, "provider_response_invalid"},
		{"GET", 200, strings.Repeat("x", maxMetadataBytes+1), 1, "provider_response_invalid"},
		{"GET", 200, `{"ok":true}`, 1, ""},
	} {
		calls := 0
		var bodies []*observedBody
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			body := &observedBody{Reader: strings.NewReader(test.body)}
			bodies = append(bodies, body)
			return &http.Response{StatusCode: test.status, Header: http.Header{}, Body: body}, nil
		})}
		request, _ := http.NewRequest(test.method, "https://provider.example/v1?token=secret-sentinel", nil)
		var result struct {
			OK bool `json:"ok"`
		}
		err := RequestJSON(context.Background(), client, request, &result)
		if calls != test.calls || test.code != "" && !IsFailure(err, test.code) || test.code == "" && (err != nil || !result.OK) {
			t.Fatal(test.method, test.status, calls, err)
		}
		if err != nil && strings.Contains(err.Error(), "sentinel") {
			t.Fatal("secret error")
		}
		for _, body := range bodies {
			if !body.closed {
				t.Fatal("body leaked")
			}
		}
	}
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { calls++; return nil, errors.New("secret-sentinel") })}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	request, _ := http.NewRequest("GET", "https://provider.example/v1", nil)
	err := RequestJSON(ctx, client, request, nil)
	if calls > 2 || !IsFailure(err, "provider_unavailable") {
		t.Fatal("deadline", calls, err)
	}
}
