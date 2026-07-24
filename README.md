# GoMental

GoMental is a local-first desktop note-taking and knowledge graph app written in Go with Wails, React, and TypeScript. Notes are stored as Google's Open Knowledge Format (OKF) Markdown concept documents.

## Development

Run commands from the project root.

```powershell
# Install frontend dependencies
Set-Location frontend
npm install
Set-Location ..

# Start the Wails development app
wails dev

# Typecheck the frontend
Set-Location frontend
npm run typecheck
Set-Location ..

# Build a production desktop executable
wails build

# Backend tests
go test ./...
```

## Build Notes

`wails.json` calls Vite directly with `node node_modules/vite/bin/vite.js build`, and dev mode calls Vite directly with `node node_modules/vite/bin/vite.js`. Keep TypeScript typechecking as a separate command because Phase 0 found npm and batch wrappers can fail with `Access is denied` when Wails captures frontend build output on this Windows setup.

