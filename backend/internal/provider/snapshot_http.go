package provider

import (
	"context"
	"net/http"
	"sama/backend/internal/platform/outbound"
)

// DeleteSnapshotRequest is transport-only. One DELETE is attempted. Native 204
// acknowledges the exact request; it is not an action ID or permission to retry.
func DeleteSnapshotRequest(ctx context.Context, b, expected Binding, profile outbound.Profile, client *http.Client, raw, token string) (Receipt, error) {
	if b != expected || client == nil || token == "" {
		return Receipt{}, ErrBinding
	}
	u, err := profile.URL(raw)
	if err != nil {
		return Receipt{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, u.String(), nil)
	if err != nil {
		return Receipt{}, ErrBinding
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	status := 0
	copy := *client
	copy.Transport = mutationStatus{client.Transport, &status}
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return outbound.ErrEndpoint }
	err = outbound.RequestJSON(ctx, &copy, request, nil)
	if err != nil {
		switch status {
		case 400, 401, 403, 404, 409, 422:
			return Receipt{Outcome: "rejected"}, nil
		}
		return Receipt{Outcome: "ambiguous"}, err
	}
	if status != 204 {
		return Receipt{Outcome: "ambiguous"}, InvalidResponse()
	}
	return Receipt{Outcome: "accepted"}, nil
}

// SnapshotJSONStatus exposes a bounded HTTP fact without forwarding raw error
// bodies. Adapters own the meaning of absence and must require a stored receipt.
func SnapshotJSONStatus(ctx context.Context, b, expected Binding, profile outbound.Profile, client *http.Client, raw, token string, out any) (int, error) {
	if client == nil {
		return 0, ErrBinding
	}
	status := 0
	copy := *client
	copy.Transport = mutationStatus{client.Transport, &status}
	err := GetJSON(ctx, b, expected, profile, &copy, raw, token, out)
	return status, err
}
