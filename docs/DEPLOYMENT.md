# GoMental Server Deployment & Operations

The single `gomental` binary contains both the **desktop** app (default, no
subcommand — Wails) and the **headless server** (`gomental serve`). The server
embeds the browser SPA and exposes the REST/SSE API, the agent API, and API-key
management. There is also a stdio MCP server (`gomental mcp`).

## Modes

| Command | Purpose |
| --- | --- |
| `gomental` | Desktop app (Wails). Unchanged; single-user, offline. |
| `gomental serve` | Headless HTTP server for a team + agents. |
| `gomental mcp --workspace <path>` | stdio MCP server for a local coding agent. |

## Quick start

```sh
gomental serve --workspace /path/to/workspace --addr :8080
# browse http://localhost:8080
```

## Configuration

Precedence: **flag > environment variable > JSON config file > default**.

| Flag | Env | Default | Meaning |
| --- | --- | --- | --- |
| `--config` | — | — | JSON config file (lowest precedence) |
| `--workspace` | `GOMENTAL_WORKSPACE` | — (required) | OKF workspace root |
| `--addr` | `GOMENTAL_ADDR` | `:8080` | listen address |
| `--auth` | `GOMENTAL_AUTH` | `trustall` | identity posture |
| `--tls-cert` | `GOMENTAL_TLS_CERT` | — | TLS certificate (with key → HTTPS) |
| `--tls-key` | `GOMENTAL_TLS_KEY` | — | TLS private key |
| `--cors-origin` | `GOMENTAL_CORS_ORIGINS` | — | comma-separated CORS allow-list |
| `--request-rate` | `GOMENTAL_REQUEST_RATE` | `50` | per-actor requests/sec |
| `--write-rate` | `GOMENTAL_WRITE_RATE` | `10` | per-actor writes/sec |

A sample config file is in [deploy/gomental.example.json](../deploy/gomental.example.json).

## Security posture (IMPORTANT)

The default auth mode is **`trustall`**: every request is treated as a local
admin and nothing is rejected. This is intended for a **trusted LAN** or behind
an **authenticating reverse proxy**. Do **not** expose a `trustall` server
directly to an untrusted network.

Even in trust-all mode the server still:

- **Audits every write** to `.workspace/audit/audit.log` (actor, action, note id,
  version, result; rotates at 10 MiB, keeps one previous generation).
- Attributes requests that present a valid **API key** (`Authorization: Bearer …`
  or `X-API-Key`) to that key's actor/role; invalid/revoked keys are rejected.
- Enforces per-actor **rate limits** and a stricter **write budget** (HTTP 429).
- Caps request bodies (40 MiB), guards `ImportURL` against **SSRF** (private/loopback
  ranges denied), and sets baseline security headers (plus HSTS when TLS is on).

To move beyond trust-all, provide a real authenticator (the role gates and audit
trail are already wired) or require API keys.

### TLS

Provide `--tls-cert`/`--tls-key` to terminate TLS in-process (HSTS is then sent),
or terminate TLS at a reverse proxy (nginx/Caddy/Traefik) and forward to the
plain HTTP listener.

### CORS

The browser SPA is served same-origin by default (no CORS needed). If you host
the SPA on a different origin, add it with `--cors-origin https://spa.example`.

## API keys (agents)

Mint keys (admin; open under trust-all):

```sh
curl -s -X POST localhost:8080/api/keys -d '{"name":"agent","role":"editor"}'
# → {"id":"…","key":"gm_…"}   # the token is shown ONCE
```

Use the key:

```sh
curl -s localhost:8080/api/search -H "Authorization: Bearer gm_…" \
  -d '{"text":"design","limit":5}'
```

Revoke: `DELETE /api/keys/{id}`. API docs: `GET /api/openapi.json`.

## MCP for coding agents

Point an MCP client (Claude Code, Cursor, MCP Inspector) at:

```sh
gomental mcp --workspace /path/to/workspace
```

Tools: `search_wiki`, `read_note`, `list_notes`, `create_note`, `edit_note`
(optimistic — pass `base_version`), `backlinks`, `neighborhood`.

## Observability

- `GET /api/healthz` — liveness.
- `GET /api/readyz` — readiness (200 once a workspace is open, else 503).
- `GET /api/metrics` — Prometheus-style request/response/rate-limit counters.
- Structured logs to stderr.

## Container

```sh
docker build -t gomental .
docker run --rm -p 8080:8080 -v /srv/wiki:/data gomental
```

The image runs `gomental serve` with `GOMENTAL_WORKSPACE=/data`.

## systemd

See [deploy/gomental.service](../deploy/gomental.service). Copy the binary to
`/usr/local/bin`, the unit to `/etc/systemd/system`, then
`systemctl enable --now gomental`.

## Backup

The **workspace directory is the backup unit** — it contains the OKF notes plus
all derived/operational state under `.workspace/` (search index, graph DB and its
`-wal`/`-shm` sidecars, audit log, API keys, layout, UI state). Back up the whole
directory; a consistent copy is safest with the server stopped. OKF note files
remain the source of truth and are readable by the desktop app and a plain
checkout (Guardrail G4).

## Scaling note

Exactly **one** server process owns a workspace (Bleve holds an exclusive lock;
the SQLite graph is one file). Horizontal scaling of the write path over a single
workspace is out of scope — run one instance per workspace.
