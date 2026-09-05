package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"strings"
	"syscall"
	"testing"
)

func TestServerFailureLogsOnlySafeClassification(t *testing.T) {
	const privateText = "fixture-secret\nforged log entry"
	for _, tc := range []struct {
		name string
		err  error
		code string
	}{
		{"unknown", errors.New(privateText), "server_failure"},
		{"wrapped bind failure", fmt.Errorf("%s: %w", privateText, syscall.EADDRINUSE), "address_in_use"},
		{"permission", fmt.Errorf("%s: %w", privateText, syscall.EACCES), "permission_denied"},
		{"shutdown", fmt.Errorf("%s: %w", privateText, context.DeadlineExceeded), "shutdown_timeout"},
		{"address", &net.AddrError{Err: privateText, Addr: privateText}, "invalid_listen_address"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			logServerFailure(slog.New(slog.NewJSONHandler(&output, nil)), tc.err)
			assertSafeRecord(t, output.String(), tc.code)
		})
	}
}

func TestHTTPDiagnosticsDoNotForwardPayload(t *testing.T) {
	var output bytes.Buffer
	writer := serverLogWriter{logger: slog.New(slog.NewJSONHandler(&output, nil))}
	log.New(writer, "", 0).Print("panic: fixture-secret\nforged log entry")
	assertSafeRecord(t, output.String(), "http_server_error")
}

func assertSafeRecord(t *testing.T, output, code string) {
	t.Helper()
	if strings.Contains(output, "fixture-secret") || strings.Contains(output, "forged") || strings.Count(output, "\n") != 1 {
		t.Fatalf("unsafe diagnostic record: %q", output)
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(output), &record); err != nil {
		t.Fatal(err)
	}
	if record["code"] != code || record["level"] != "ERROR" || len(record) != 4 {
		t.Fatalf("unexpected diagnostic fields: %v", record)
	}
}
