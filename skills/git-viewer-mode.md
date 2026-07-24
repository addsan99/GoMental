---
name: git-viewer-mode
description: Understand and operate a GoMental wiki running in read-only git-viewer mode (server `serve` or desktop `viewer`), where the notes are authored in a git repository (PRs/review) and GoMental only tracks a ref and serves it. Covers what read-only means for an agent, how content pulls in (webhook POST /api/git/sync, --git-poll, or the desktop pull chip), and how GET /api/git/status reports the current commit.
when_to_use: When the wiki you're connected to is backed by git — writes fail with workspace.read_only, /api/info reports readOnly:true with a git block, or the operator says "content is managed in git". Read this to know why you can't write and how content gets updated.
---

# git-viewer mode

Some GoMental servers run as a **read-only view over a git repository**. The git
repo is the **source of truth**: notes are authored and curated *outside*
GoMental (LLM drafting + a human PR/review process). GoMental clones the repo,
tracks a ref, and serves the content — search, graph, backlinks, inference — but
**never writes back**. This skill is for agents and operators working against
such a server. The read tools from `README.md` / `wiki-answer.md` all work
unchanged; only *writing* differs.

## How do I know I'm in git-viewer mode?

- `GET /api/info` reports `readOnly: true` and a non-null `git` block:
  `{remote, ref, commit, lastSyncAt, lastError, syncing}`.
- The browser UI shows a read-only banner ("Read-only — content is managed in
  git") and a small git status chip (ref + short commit).
- Any write — `create_note`, `edit_note`, `upload_asset`, or the REST
  `POST/PUT/DELETE /api/notes`, `POST /api/import`, `POST /api/assets` — returns
  **`403 {code:"workspace.read_only"}`**.

## What read-only means for you (agent)

- **Do not try to author into the wiki.** There is no write path through
  GoMental in this mode; retrying a write won't help. To change content, the edit
  must go through the **git repository** — open a PR / branch in the upstream repo
  and let review merge it. Once merged and the tracked ref advances, the server
  picks it up (below) and the note appears here.
- **Reading is fully supported.** Use `search_wiki`, `read_note`, `list_notes`,
  `backlinks`, `neighborhood`, `expand_context`, `explain_link` exactly as usual.
- **Local view state is still writable.** Graph layout and UI state are stored in
  the server's local metadata, not the git tree, so those still persist. Content
  does not.
- If you were asked to "capture" knowledge into a git-viewer wiki, surface it as a
  proposed change to the upstream git repo (a note file / PR), not a GoMental
  write.

## How content gets updated: the server pulls

Advancing the working copy = `git fetch` → `git reset --hard origin/<ref>` →
`git clean` (preserving the server's own metadata). The server's existing file
watcher then reconciles the changed notes incrementally and emits the usual
`note:updated` / `graph:updated` SSE events, so the graph and search stay current
without a full rebuild. A sync happens two ways:

1. **Webhook (push-driven).** A git host (GitHub/GitLab) calls
   `POST /api/git/sync` on push. It authenticates with a shared **webhook secret**
   — header `X-GoMental-Token: <secret>` or `?token=<secret>` — so no API key is
   needed. An operator can trigger the same endpoint with an **admin API key**:

   ```bash
   # operator-triggered sync (admin API key)
   curl -X POST https://wiki.example.com/api/git/sync \
     -H "Authorization: Bearer gm_…"
   ```

   Response: `{ok, oldCommit, newCommit, changed, deleted}`.

2. **Polling.** If the server was started with `--git-poll <interval>` (e.g.
   `2m`), it fetches on that interval as a backstop. `--git-poll 0` (the default)
   means webhook-only.

On success the server emits a `git:synced` SSE event
(`{commit, oldCommit, changed, deleted}`); on failure, `git:sync-error`
(`{error}`). If you keep an event stream open, `git:synced` is the "content just
changed upstream" signal — re-read notes you care about after it.

## Checking the current commit

`GET /api/git/status` reports what the server is serving right now:

```bash
curl -sS https://wiki.example.com/api/git/status
# → {"remote":"https://github.com/org/wiki.git","ref":"main",
#    "commit":"a1b2c3d","lastSyncAt":"2026-07-20T12:00:00Z",
#    "lastError":"","syncing":false}
```

- `commit` — the short SHA currently checked out (`""` if not yet cloned).
- `lastSyncAt` / `lastError` — freshness and the last failure, if any.
- `syncing` — true while a sync is in flight.

Both `/api/git/sync` and `/api/git/status` return **404** on a server that is
*not* in git mode — that 404 is a reliable way to tell "this server isn't
git-backed".

## Operator quickstart

```sh
gomental serve \
  --workspace /srv/wiki \
  --git-remote https://github.com/org/wiki.git \
  --git-ref main \
  --git-poll 2m
```

The workspace dir need not pre-exist (GoMental clones the remote). `--read-only`
defaults **on** when `--git-remote` is set. Remote credentials come from the
ambient git setup (credential helper / tokenized https / SSH agent) — GoMental
does not manage them. Full operator guide:
[`../docs/GIT_SYNC.md`](../docs/GIT_SYNC.md); systemd example:
[`../deploy/gomental-git.service`](../deploy/gomental-git.service).

## Desktop viewer (offline / solo)

The same git-viewer behavior is available in the **desktop GUI** via the
`viewer` subcommand — for one person browsing a git-managed wiki without running
the `serve` daemon:

```sh
gomental viewer \
  --workspace ~/wiki-copy \
  --git-remote https://github.com/org/wiki.git \
  --git-ref main \
  --git-poll 5m
```

It clones on first open, is read-only by default, and shows the same read-only
banner + git status chip as the browser UI. Differences from `serve`:

- **No HTTP endpoints.** There is no `/api/git/sync` / `/api/git/status` and no
  webhook — it's a local GUI, not a server. `--git-webhook-secret` and the
  network flags (`--addr`, `--tls-*`, `--cors-origin`, rate limits) do not apply.
- **Pull is manual or polled.** Click the git status chip in the header to pull
  the latest commit, or set `--git-poll <interval>` for an automatic backstop.
- **Content updates the same way.** The pull advances the working copy and the
  in-process file watcher reconciles notes + emits `git:synced` (a toast), exactly
  as on the server.

For an agent, nothing changes: writes are still blocked, reads still work, and
content still originates from the upstream git repo. Use `serve` when many
viewers must agree on one version; use `viewer` for the offline/solo case.

## See also

- **`README.md`** — the MCP tool surface and how to connect.
- **`connect-central-server.md`** — connecting an agent to a central server over
  HTTP (a git-viewer server is a central server that also happens to be read-only).
- **`wiki-answer.md`** — reading the wiki with cited, graph-aware context (works
  identically in git-viewer mode).
