// Package migrations contains the immutable, embedded forward migration set.
package migrations

import "embed"

// Files is the source of truth consumed by the explicit migration command.
//
//go:embed *.sql
var Files embed.FS
