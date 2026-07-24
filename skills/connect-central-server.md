---
name: connect-central-server
description: Connect a coding agent to a shared, centrally-hosted GoMental wiki over HTTP (the /mcp endpoint) instead of a local stdio server, so the agent reads/writes the same workspace the team's browser UI uses. Covers MCP client config, API-key auth, roles, and the stdio-bridge fallback.
when_to_use: When the wiki is not a local folder but a GoMental server someone runs centrally (a URL like https://wiki.example.com), and you need your agent to use it. Read this before wiki-answer/wiki-capture if you don't yet have the MCP tools connected.
---

# connect-central-server

Use this when the wiki lives on a **central `gomental serve` instance** reached over
HTTP — not as a local workspace folder. All the wiki skills (`wiki-answer`,
`wiki-capture`, `type-aware-authoring`) work unchanged once the tools are connected;
this skill only covers *getting connected* to a remote server.

## Which mode am I in?

| You have… | Use |
|---|---|
| A local wiki folder on this machine | Local **stdio** MCP: `gomental mcp --workspace <path>` (see `README.md`). |
| A server URL (e.g. `https://wiki.example.com`) + an API key | Remote **HTTP** MCP: the `/mcp` endpoint, below. |

The remote path is the right one for a team: everyone — browsers and agents — goes
through the **one** server process that owns the workspace, so your edits and the
team's edits stay consistent and are safe under concurrent writes (optimistic
concurrency + per-note locking live in that one server).

## Connect over HTTP (`/mcp`)

The server exposes the identical MCP tool surface at:

```
POST https://<host>/mcp        # MCP Streamable-HTTP transport (JSON-RPC 2.0)
```

Authenticate with a **Bearer API key** (ask a server admin to mint one via
`POST /api/keys`, or the GoMental UI). Configure your MCP client with the URL and key.

**Native HTTP MCP client** (Claude Code, and other clients that support remote MCP):

```json
{
  "mcpServers": {
    "gomental": {
      "url": "https://wiki.example.com/mcp",
      "headers": { "Authorization": "Bearer <API_KEY>" }
    }
  }
}
```

**stdio-only client (fallback).** If your client can only spawn a local stdio MCP
server, bridge it to the remote endpoint with the standard `mcp-remote` shim:

```json
{
  "mcpServers": {
    "gomental": {
      "command": "npx",
      "args": ["-y", "mcp-remote", "https://wiki.example.com/mcp",
               "--header", "Authorization: Bearer <API_KEY>"]
    }
  }
}
```

## Roles: what your key can do

The key's role gates the tools, matching the server's REST authorization:

- **viewer** — read tools: `search_wiki`, `read_note`, `list_notes`, `backlinks`,
  `neighborhood`, `expand_context`, `explain_link`.
- **editor** — the above **plus** the write tools `create_note`, `edit_note`, and `upload_asset`.

If you call a write tool with a viewer key, the server returns a JSON-RPC error
(`insufficient role: … requires the 'editor' role`). Ask for an editor key if you
need to capture notes.

## Smoke-test the connection

```bash
curl -sS https://wiki.example.com/mcp \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

A JSON response listing the tools means you're connected. `initialize` and
`tools/list` need only a viewer key.

## Operational notes (differ from local stdio)

- **No workspace flag, no local files.** You never point at a path — the server
  chose the workspace. You also cannot delete notes over MCP (no delete tool) *and*
  you have no filesystem access to remove the `.md` yourself; ask a server admin.
- **Concurrency is real.** Other people and agents may edit the same notes. Always
  pass `base_version` (from `read_note`) to `edit_note`; on a conflict error,
  re-read and retry rather than force-overwriting. This is the main reason to prefer
  the central server over each agent holding its own copy.
- **Rate limits & audit.** Requests are rate-limited per key and write tool calls
  are recorded in the server audit log against your key — so keep keys per-agent,
  not shared.
- **HTTPS.** Use `https://` for anything beyond a trusted LAN; the API key is a
  bearer credential.
