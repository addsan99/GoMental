#!/usr/bin/env sh
set -eu

cd "$(dirname "$0")"

if ! command -v go >/dev/null 2>&1; then
  echo "Build FAILED: Go is not on PATH. Install the Go version from go.mod." >&2
  exit 1
fi
if [ -z "${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}" ]; then
  echo "Build FAILED: set ANDROID_HOME or ANDROID_SDK_ROOT to an Android SDK." >&2
  exit 1
fi

target="${GOMENTAL_ANDROID_TARGET:-android/arm64,android/amd64}"
api="${GOMENTAL_ANDROID_API:-26}"
output="build/android/gomental-mobile.aar"
tool_dir="build/android/.tools"

mkdir -p "$(dirname "$output")" "$tool_dir"
go build -o "$tool_dir/gobind" golang.org/x/mobile/cmd/gobind
PATH="$(pwd)/$tool_dir:$PATH" go tool gomobile bind \
  -target="$target" \
  -androidapi="$api" \
  -javapkg=com.gomental \
  -o "$output" \
  ./mobile

echo "Build succeeded: $output"
