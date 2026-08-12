#!/usr/bin/env sh
set -eu

# Validate the macOS toolchain and produce a release .app. A universal build is
# the safest handoff artifact; set GOMENTAL_MAC_PLATFORM to darwin/arm64 or
# darwin/amd64 when only one architecture is required.

cd "$(dirname "$0")"

if [ "$(uname -s)" != "Darwin" ]; then
  echo "Build FAILED: build-macos.sh must run on macOS." >&2
  exit 1
fi

for tool in go node npm wails xcode-select; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "Build FAILED: ${tool} is not on PATH." >&2
    exit 1
  fi
done

if ! xcode-select -p >/dev/null 2>&1; then
  echo "Build FAILED: Xcode Command Line Tools are unavailable. Run: xcode-select --install" >&2
  exit 1
fi

platform="${GOMENTAL_MAC_PLATFORM:-darwin/universal}"
case "$platform" in
  darwin/universal|darwin/arm64|darwin/amd64) ;;
  *)
    echo "Build FAILED: GOMENTAL_MAC_PLATFORM must be darwin/universal, darwin/arm64, or darwin/amd64." >&2
    exit 1
    ;;
esac

echo "=== GoMental macOS build =================================================="
go version
node --version
npm --version
wails version
echo "Xcode tools: $(xcode-select -p)"
echo "Target: ${platform}"
echo "==========================================================================="

(cd frontend && npm ci && npm run typecheck)
go test ./...
wails doctor -nocolour
wails build -clean -platform "$platform" "$@"

echo
echo "Build succeeded: build/bin/GoMental.app"
echo "The app is unsigned. Sign and notarize it before distributing outside local testing."
