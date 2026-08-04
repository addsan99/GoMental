# GoMental

GoMental is a local-first desktop note-taking and knowledge graph app written in Go with Wails, React, and TypeScript. Notes are stored as Google's Open Knowledge Format (OKF) Markdown concept documents.

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

### macOS / Linux

```sh
cd frontend
npm install
cd ..

wails dev

sh ./build.sh

go test ./...
```

## Build Notes

`wails.json` calls Vite directly with `node node_modules/vite/bin/vite.js build`, and dev mode calls Vite directly with `node node_modules/vite/bin/vite.js`. Keep TypeScript typechecking as a separate command because Phase 0 found npm and batch wrappers can fail with `Access is denied` when Wails captures frontend build output on this Windows setup.

The Windows-only console attachment and pre-render splash live behind `//go:build windows`; macOS and Linux compile the matching `*_other.go` no-op implementations. The repo keeps both `build.cmd` and `build.sh` so local builds do not depend on one platform's shell.

