package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"syscall"
)

// Log only allowlisted classifications. Error strings and configuration can
// contain credentials or attacker-controlled text, including wrapped errors.
func logServerFailure(logger *slog.Logger, err error) {
	code := "server_failure"
	var addressError *net.AddrError
	switch {
	case errors.Is(err, syscall.EADDRINUSE):
		code = "address_in_use"
	case errors.Is(err, syscall.EACCES):
		code = "permission_denied"
	case errors.Is(err, context.DeadlineExceeded):
		code = "shutdown_timeout"
	case errors.As(err, &addressError):
		code = "invalid_listen_address"
	}
	logger.Error("server stopped", "code", code)
}

// net/http's diagnostic stream may include addresses, panic values and stacks.
// Preserve the occurrence, without forwarding its unstructured payload.
type serverLogWriter struct {
	logger *slog.Logger
}

func (w serverLogWriter) Write(payload []byte) (int, error) {
	w.logger.Error("HTTP server reported an error", "code", "http_server_error")
	return len(payload), nil
}
