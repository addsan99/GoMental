#!/usr/bin/env sh
set -eu

# Full Wails build for GoMental on macOS/Linux.
#
# Prerequisites:
#   - Go matching go.mod
#   - Node.js + npm
#   - Wails CLI on PATH
#
# Output:
#   macOS: build/bin/GoMental.app
#   Linux: build/bin/GoMental

cd "$(dirname "$0")"

echo "=== GoMental full Wails build ============================================="
go version
command -v wails
echo "==========================================================================="

if ! go list unsafe >/dev/null 2>&1; then
  echo
  echo "Build FAILED: Go standard library is not usable. Reinstall or repair Go, then retry."
  exit 1
fi

if ! wails build "$@"; then
  rc=$?
  echo
  echo "Build FAILED with exit code ${rc}."
  exit "${rc}"
fi

case "$(uname -s)" in
  Darwin*) out="build/bin/GoMental.app" ;;
  *) out="build/bin/GoMental" ;;
esac

echo
echo "Build succeeded: ${out}"
