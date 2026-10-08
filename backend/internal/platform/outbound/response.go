package outbound

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"time"
)

// Failure contains only a stable category, never a URL, header or provider body.
type Failure struct {
	Code       string
	RetryAfter time.Duration
}

func (f *Failure) Error() string { return f.Code }

const maxMetadataBytes = 8 << 20

// RequestJSON owns the entire retry deadline. It does not retry mutations;
// adapters and job layers must not wrap this operation in another retry loop.
func RequestJSON(ctx context.Context, client *http.Client, request *http.Request, output any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	attempts := 1
	if request.Method == http.MethodGet || request.Method == http.MethodHead {
		attempts = 5
	}
	for attempt := 0; attempt < attempts; attempt++ {
		req := request.Clone(ctx)
		response, err := client.Do(req)
		var failure *Failure
		if err != nil {
			failure = &Failure{Code: "provider_unavailable"}
		} else {
			// Always close before waiting, including malformed and oversized bodies.
			data, readErr := io.ReadAll(io.LimitReader(response.Body, maxMetadataBytes+1))
			response.Body.Close()
			// A received denial is terminal even if its body cannot be read.
			if response.StatusCode == 401 || response.StatusCode == 403 {
				return &Failure{Code: "provider_permission_denied"}
			}
			if readErr != nil {
				failure = &Failure{Code: "provider_unavailable"}
			} else if len(data) > maxMetadataBytes {
				return &Failure{Code: "provider_response_invalid"}
			} else {
				switch {
				case response.StatusCode >= 200 && response.StatusCode < 300:
					if output != nil && json.Unmarshal(data, output) != nil {
						return &Failure{Code: "provider_response_invalid"}
					}
					return nil
				case response.StatusCode == 429:
					failure = &Failure{Code: "provider_rate_limited", RetryAfter: retryAfter(response.Header, time.Now())}
				case response.StatusCode >= 500:
					failure = &Failure{Code: "provider_unavailable"}
				default:
					return &Failure{Code: "provider_rejected"}
				}
			}
		}
		if attempt == attempts-1 || ctx.Err() != nil {
			return failure
		}
		delay := time.Duration(rand.Int64N(int64(100*time.Millisecond<<attempt) + 1))
		if failure.RetryAfter > delay {
			delay = failure.RetryAfter
		}
		if wait(ctx, delay) != nil {
			return &Failure{Code: "provider_unavailable"}
		}
	}
	return &Failure{Code: "provider_unavailable"}
}

func wait(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func IsFailure(err error, code string) bool {
	var failure *Failure
	return errors.As(err, &failure) && failure.Code == code
}
