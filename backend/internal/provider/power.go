package provider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"

	"sama/backend/internal/platform/outbound"
)

var ErrActionUnsupported = errors.New("provider action unsupported")
var ErrPrecondition = errors.New("provider precondition changed")

// PowerPreconditions hashes stable review facts, excluding observation time.
// Adapters must first check native restrictions and supported action semantics.
func PowerPreconditions(b Binding, action string, o Observation) (Preconditions, error) {
	allowed := false
	switch action {
	case "compute.server.power_on":
		allowed = o.Status == "stopped"
	case "compute.server.shutdown", "compute.server.power_off", "compute.server.reboot":
		allowed = o.Status == "running"
	default:
		return Preconditions{}, ErrActionUnsupported
	}
	data, err := json.Marshal(struct {
		Workspace, Connection, Family                string
		Version                                      int64
		Action, NativeID, NativeStatus, Status, Name string
		Details                                      ServerDetails
	}{b.Workspace().String(), b.Connection().String(), b.Family(), b.Version(), action, o.NativeID, o.NativeStatus, o.Status, o.Name, o.Details})
	if err != nil {
		return Preconditions{}, InvalidResponse()
	}
	return Preconditions{Fingerprint: sha256.Sum256(data), TargetName: o.Name, Allowed: allowed, HighImpact: action == "compute.server.power_off"}, nil
}

// PostMutation uses the existing response cap, deadline and connection budget.
// POST is attempted once. A timeout, 408, 429, 5xx or malformed receipt remains
// ambiguous; callers never convert a tracing header into a native action ID.
func PostMutation(ctx context.Context, b, expected Binding, profile outbound.Profile, client *http.Client, raw, token string, body []byte, headers http.Header, out any) (Receipt, error) {
	if b != expected || client == nil || token == "" {
		return Receipt{}, ErrBinding
	}
	u, err := profile.URL(raw)
	if err != nil {
		return Receipt{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return Receipt{}, ErrBinding
	}
	for key, values := range headers {
		req.Header[key] = append([]string(nil), values...)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	status := 0
	copy := *client
	copy.Transport = mutationStatus{client.Transport, &status}
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return outbound.ErrEndpoint }
	err = outbound.RequestJSON(ctx, &copy, req, out)
	if err != nil {
		switch status {
		case 400, 401, 403, 404, 409, 422:
			return Receipt{Outcome: "rejected"}, nil
		}
		return Receipt{Outcome: "ambiguous"}, err
	}
	return Receipt{Outcome: "ambiguous"}, nil
}

type mutationStatus struct {
	base http.RoundTripper
	code *int
}

func (t mutationStatus) RoundTrip(r *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	response, err := base.RoundTrip(r)
	if err == nil && response != nil {
		*t.code = response.StatusCode
	}
	return response, err
}
