#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
case "${1:-}" in
  '') race= ;;
  --race) race=-race ;;
  *) printf '%s\n' 'Usage: ./scripts/check.sh [--race]' >&2; exit 64 ;;
esac
[ "$(uname -s)" = Darwin ] || { printf '%s\n' 'Checks require macOS and Xcode Command Line Tools.' >&2; exit 1; }
command -v go >/dev/null
command -v node >/dev/null
# The normal suite must not read personal shell profiles or contact real servers.
unset FARSHMAC_PROBE_STARTUP FARSHMAC_PROBE_CUSTOM_PROMPT FARSHMAC_SFTP_CHECK
unformatted=$(gofmt -l cmd internal)
if [ -n "$unformatted" ]; then
  printf 'Run gofmt on:\n%s\n' "$unformatted" >&2
  exit 1
fi
go mod verify
go vet ./...
if [ -n "$race" ]; then
  go test -race ./...
else
  go test ./...
fi
# grid.cjs consumes JSON on stdin; harness.cjs is a helper, not a test program.
for test in input resize terminal commands preview switch throughput; do
  node "internal/native/tests/$test.cjs"
done

# Exercise macOS Option translation, including dead keys and punctuation.
native_test=$(mktemp "${TMPDIR:-/tmp}/farshmac-panel-keys.XXXXXX")
trap 'rm -f "$native_test"' EXIT
clang -framework Cocoa internal/native/tests/panel_keys.m -o "$native_test"
"$native_test"
