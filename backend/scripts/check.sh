#!/bin/sh
set -eu
cd "$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
mode=${1:-check}
case "$mode" in
  format) go fmt ./...; exit 0 ;;
  check) ;;
  *) printf '%s\n' 'Usage: sh scripts/check.sh [check|format]' >&2; exit 2 ;;
esac
# Both source and tests must be formatted; this gate never rewrites files.
unformatted=$("$(go env GOROOT)/bin/gofmt" -l cmd internal)
if [ -n "$unformatted" ]; then
  printf 'Go formatting required:\n%s\nRun: sh backend/scripts/check.sh format\n' "$unformatted" >&2
  exit 1
fi
go vet ./...
go test -race ./...
# -buildvcs=false also supports the isolated staged snapshot used by the hook.
go build -buildvcs=false -o bin/sama ./cmd/sama
