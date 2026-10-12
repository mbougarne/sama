package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"unicode/utf8"

	"sama/backend/internal/platform/outbound"
)

var ErrIdentityUnverified = errors.New("provider identity unverified")

var numericNativeID = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)

func NumericID(id json.Number) (string, error) {
	if !numericNativeID.MatchString(string(id)) {
		return "", InvalidResponse()
	}
	return string(id), nil
}

func InvalidResponse() error { return &outbound.Failure{Code: "provider_response_invalid"} }

// StrictQuery rejects provider links with hidden filters or duplicated keys.
func StrictQuery(raw string, allowed ...string) (url.Values, error) {
	q, err := url.ParseQuery(raw)
	if err != nil {
		return nil, InvalidResponse()
	}
	for key, values := range q {
		found := false
		for _, candidate := range allowed {
			if key == candidate {
				found = true
			}
		}
		if !found || len(values) != 1 || values[0] == "" {
			return nil, InvalidResponse()
		}
	}
	return q, nil
}

// GetJSON is transport-only. Provider paging, identity and normalization stay
// with their adapter. The client comes from the shared process budget.
func GetJSON(ctx context.Context, b, expected Binding, profile outbound.Profile, client *http.Client, raw, token string, out any) error {
	if b != expected || client == nil || token == "" {
		return ErrBinding
	}
	u, err := profile.URL(raw)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return ErrBinding
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	return outbound.RequestJSON(ctx, client, request, out)
}

func CheckedObservation(o Observation) (Observation, error) {
	if len(o.NativeID) == 0 || len(o.NativeID) > 512 || len(o.NativeStatus) > 128 || utf8.RuneCountInString(o.Name) > 200 || !utf8.ValidString(o.Name) || len(o.Details.Region) > 128 || len(o.Details.MachineType) > 128 {
		return Observation{}, InvalidResponse()
	}
	return o, nil
}
