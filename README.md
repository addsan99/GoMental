# GoMental

GoMental is a local-first desktop note-taking and knowledge graph app written in Go with Wails, React, and TypeScript. Notes are stored as Google's Open Knowledge Format (OKF) Markdown concept documents.

A workspace can optionally carry an [ingest profile](docs/INGEST_PROFILES.md) that lets GoMental read a Markdown corpus authored elsewhere — such as an [AI-Overload](profiles/ai-overload/) repo overload — without changing how anything else works.

## Development

Run commands from the project root.

### Windows

```powershell
Set-Location frontend
npm install
Set-Location ..

wails dev

.\build.cmd

go test ./...
```

### macOS

Install Xcode Command Line Tools, Go 1.25 or newer, Node.js/npm, and Wails CLI
v2.13.0. Then run the checked macOS release build:

```sh
xcode-select --install # only when the tools are not installed yet
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
sh ./build-macos.sh
```

The script installs the exact frontend lockfile, runs TypeScript and Go tests,
runs `wails doctor`, and builds `build/bin/GoMental.app` for Apple Silicon and
Intel by default. To build only for the current Apple Silicon target, use:

```sh
GOMENTAL_MAC_PLATFORM=darwin/arm64 sh ./build-macos.sh
```

The resulting app is unsigned. Code signing and notarization require the
Apple Developer identity and credentials on the Mac and are intentionally not
performed by this repository script.

### Linux

```sh
cd frontend
npm ci
cd ..

wails dev

sh ./build.sh

go test ./...
```

## Build Notes

`wails.json` calls Vite directly with `node node_modules/vite/bin/vite.js build`, and dev mode calls Vite directly with `node node_modules/vite/bin/vite.js`. Keep TypeScript typechecking as a separate command because Phase 0 found npm and batch wrappers can fail with `Access is denied` when Wails captures frontend build output on this Windows setup.

The Windows-only console attachment and pre-render splash live behind `//go:build windows`; macOS and Linux compile the matching `*_other.go` no-op implementations. The repo keeps both `build.cmd` and `build.sh` so local builds do not depend on one platform's shell.

