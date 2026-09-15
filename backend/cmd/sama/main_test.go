package main

import (
	"context"
	"log/slog"
	"testing"

	"sama/backend/internal/platform"
)

func TestRunRejectsInvalidConfigurationBeforeStarting(t *testing.T) {
	for _, tc := range []struct {
		name  string
		env   string
		value string
		code  string
	}{
		{"origin", "SAMA_PUBLIC_ORIGIN", "not-an-origin", "invalid_public_origin"},
		{"origin query delimiter", "SAMA_PUBLIC_ORIGIN", "https://console.example.test?", "invalid_public_origin"},
		{"budget", "SAMA_WORKER_COUNT", "0", "invalid_budget"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SAMA_PUBLIC_ORIGIN", "http://127.0.0.1:8080")
			t.Setenv(tc.env, tc.value)
			err := run(context.Background(), slog.Default())
			code, ok := platform.ConfigurationCode(err)
			if !ok || code != tc.code {
				t.Fatalf("configuration code = %q, ok = %t, err = %v", code, ok, err)
			}
		})
	}
}
