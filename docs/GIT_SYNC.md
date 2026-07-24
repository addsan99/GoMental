# GoMental git-sync (git-viewer mode)

Point a `gomental serve` instance at a **git repository that is the source of
truth** for the notes, and it becomes a read replica of that repo with a graph,
search index, and link inference on top. Content is authored *outside* GoMental
(LLM drafting + human PR review); GoMental fetches the tracked ref, advances its
working copy, and serves humans and agents. It never writes back to the repo.

This is Track C's companion doc to [`../GIT_SYNC_PLAN.md`](../GIT_SYNC_PLAN.md)
(the design source of truth) and [`DEPLOYMENT.md`](DEPLOYMENT.md) (general
server ops). Read those for the full rationale; this page is the operator's guide.

## Scenario & topology: one server, not local-copy-per-client

The recommended topology is **server mode against a single git working copy** —
"a read replica of git with a graph on top" — rather than every client cloning
and indexing locally. Why (strongest first):

1. **Index cost is paid once.** The SQLite graph, Bleve index, and soft-link /
   corpus inference are built once on the server and shared by all viewers.
   Local-per-client rebuilds the whole index on every machine (N× CPU/RAM/disk).
2. **One version everyone agrees on.** The wiki's value is the *shared* link graph
   (backlinks, inferred links, neighborhoods). A single working copy at a single
   commit gives every viewer identical content *and* identical derived
   relationships; independent clones drift to different commits and disagree.
3. **Sync, credentials, and remote load are centralized.** One fetcher, one
   reconcile, one webhook endpoint, one place holding repo credentials — not N
   clients polling the remote.
4. **Thin clients.** Browser viewers need only a URL; the SPA is served from the
   embedded bundle.
5. **Read-mostly makes server mode easy here.** Server mode is normally hard
   because of multi-writer concurrency. In a viewer scenario nobody writes through
   GoMental, so that cost disappears.

**When local-per-client is appropriate:** the offline/solo power user browsing a
personal clone. The `viewer` subcommand runs the desktop GUI in exactly this mode
— it clones the remote on first open, pulls on demand (or on a `--git-poll`
interval), and is read-only by default:

```
GoMental viewer --workspace C:\wiki-copy --git-remote https://github.com/org/wiki.git --git-poll 5m
```

The git status chip in the header shows the tracked ref + short commit; click it
to pull the latest. There is no webhook (it is a local GUI, not a daemon), so
pulls are manual or interval-driven. Same binary as the desktop app and `serve`.

> Decision rule: *many viewers who must agree on a version → server (`serve`);
> one offline viewer → local GUI (`viewer`).*

## How it works

The server **already runs a polling workspace watcher** (snapshot +
`(mtime,size)` diff). Its changesets flow into **incremental** projection updates
and emit the existing SSE events `note:updated` / `note:deleted` /
`graph:updated`, and mark the inference worker dirty.

git-sync leans entirely on that. It only needs to *advance the working copy* on
disk; the watcher then reconciles content incrementally and notifies clients for
free. So git-sync does **not** compute diffs or touch the reconcile path.

A **sync** is: `git fetch` → `git reset --hard origin/<ref>` → `git clean -fd -e
<metadataDir>`. Because `reset --hard` only rewrites *tracked* files and `clean`
is run with `-e <metadataDir>` (and never `-x`), the untracked `.workspace/`
metadata dir — search index, graph DB, layout, UI state, API keys, audit log —
**survives every sync**. The external repo does not need to gitignore it.

A force-push / non-fast-forward on the tracked ref is handled naturally: `reset
--hard origin/<ref>` always matches the remote, and the watcher reconciles
whatever changed.

## Quickstart

```sh
gomental serve \
  --workspace /srv/wiki \
  --git-remote https://github.com/org/wiki.git \
  --git-ref main \
  --git-poll 2m
```

The workspace dir need **not** pre-exist — when `--git-remote` is set, GoMental
clones it before opening the workspace. `--read-only` defaults **ON** here
(because `--git-remote` is set), so authoring routes are disabled.

## Configuration

Precedence: **flag > environment variable > JSON config file > default**.

| Flag | Env | Default | Meaning |
| --- | --- | --- | --- |
| `--git-remote` | `GOMENTAL_GIT_REMOTE` | `""` (git mode off) | Remote URL to track, e.g. `https://github.com/org/wiki.git`. Setting it enables git mode. |
| `--git-ref` | `GOMENTAL_GIT_REF` | `main` | Branch or tag to track. |
| `--git-poll` | `GOMENTAL_GIT_POLL` | `0` (off; webhook-driven) | Poll interval as a Go duration (`30s`, `5m`). `0` disables polling — sync only via the webhook endpoint. |
| `--git-webhook-secret` | `GOMENTAL_GIT_WEBHOOK_SECRET` | `""` | Shared secret that authorizes `POST /api/git/sync` without an API key (for git-host webhooks). |
| `--read-only` | `GOMENTAL_READ_ONLY` | **`true` when `--git-remote` is set**, else `false` | Reject content-mutating routes with `403`. |

The untracked metadata dir protected during `git clean` is `.workspace`
(GoMental's standard workspace metadata dir); it is not separately configurable
here. Relocating it out of tree so the working copy is fully disposable is a
deferred, optional enhancement (see [Out-of-tree index](#out-of-tree-index-deferred)).

## HTTP endpoints & events

### `POST /api/git/sync` — trigger a sync

Runs one `fetch + reset + clean` cycle (mutex-serialized). Auth is **either**:

- an **admin API key** (`Authorization: Bearer …` / `X-API-Key`), or
- a valid **webhook secret** — header `X-GoMental-Token: <secret>` or query
  `?token=<secret>` — so a GitHub/GitLab webhook can call it with no API key.

Returns `{ok, oldCommit, newCommit, changed, deleted}` (the `changed`/`deleted`
counts are informational — the watcher does the real reconcile). Returns **404**
when git mode is off.

```sh
# from a git-host webhook (secret auth)
curl -X POST https://wiki.example.com/api/git/sync \
  -H "X-GoMental-Token: $GOMENTAL_GIT_WEBHOOK_SECRET"

# from an operator (admin API key)
curl -X POST https://wiki.example.com/api/git/sync \
  -H "Authorization: Bearer gm_…"
```

### `GET /api/git/status` — current git state

Returns the status snapshot as JSON. Returns **404** when git mode is off.

```json
{
  "remote": "https://github.com/org/wiki.git",
  "ref": "main",
  "commit": "a1b2c3d",
  "lastSyncAt": "2026-07-20T12:00:00Z",
  "lastError": "",
  "syncing": false
}
```

### SSE events on `/api/events`

The existing event stream additionally carries:

- **`git:synced`** — payload `{commit, oldCommit, changed, deleted}`. The
  human-facing "just pulled" signal; the actual content invalidation rides on the
  `note:updated` / `graph:updated` events the watcher already emits.
- **`git:sync-error`** — payload `{error}` when a sync fails.

### `/api/info` fields

`GET /api/info` gains two fields:

- `readOnly: bool`
- `git: {remote, ref, commit, lastSyncAt, lastError, syncing} | null` (`null` when
  git mode is off).

## Read-only enforcement

When read-only is on (the default under `--git-remote`), **content-mutating**
routes return `403 {code:"workspace.read_only"}`: `POST /api/notes`,
`PUT`/`DELETE /api/notes/{id…}`, `POST /api/import`, `POST /api/assets/{id…}`, and
the MCP write tools. **Still writable** (local view state under `.workspace`, not
the git tree): graph layout and UI state. Admin ops `POST /api/rebuild` and
`POST /api/git/sync` remain available.

## Operating notes

### Credentials

Repo credentials are **out of scope for GoMental to manage in-process**. Use the
ambient git tooling on the server, one of:

- a **git credential helper** configured for the service account,
- a **tokenized HTTPS remote** (`https://<token>@github.com/org/wiki.git`), or
- an **SSH remote** (`git@github.com:org/wiki.git`) with a key loaded in the
  server's SSH agent / `~/.ssh`.

Whatever a plain `git fetch` in the working copy can do, GoMental can do.

### Webhook wiring (push-driven sync)

Set `--git-webhook-secret` and register a webhook on the git host pointing at
`POST /api/git/sync`:

- **GitHub** — repo → Settings → Webhooks → Payload URL
  `https://wiki.example.com/api/git/sync?token=<secret>` (or add the
  `X-GoMental-Token` header), event: *Just the push event*.
- **GitLab** — project → Settings → Webhooks → URL + *Secret token* mapped to the
  `X-GoMental-Token` header (or `?token=`), trigger: *Push events*.

Filter the webhook to the tracked branch where the host allows it; a sync on an
unrelated ref is a harmless no-op (nothing to fast-forward). Combine with a small
`--git-poll` (e.g. `2m`) as a backstop if a webhook is missed.

### Out-of-tree index (deferred)

Relocating `.workspace` out of the working copy — so the checkout is fully
disposable and the derived index survives a re-clone — is **Phase E in the plan,
deferred and optional**. The `git clean -e <metadataDir>` guard already keeps the
index safe across syncs, so this is only worth doing if a deployment specifically
wants a throwaway working copy.

## Edge cases

- **Large diffs** (bulk import): the watcher's incremental path handles many
  files; `POST /api/rebuild` is the escape hatch for a full rebuild.
- **Renames**: git may report them; the watcher sees delete+add of note IDs and
  backlinks re-resolve. Rename info is informational only.
- **Concurrent syncs**: serialized by a mutex inside the git manager.

## See also

- [`../GIT_SYNC_PLAN.md`](../GIT_SYNC_PLAN.md) — design & contracts.
- [`DEPLOYMENT.md`](DEPLOYMENT.md) — general server ops, auth, TLS, backup.
- [`deploy/gomental-git.service`](../deploy/gomental-git.service) — systemd unit for git-viewer mode.
- [`../skills/git-viewer-mode.md`](../skills/git-viewer-mode.md) — agent/operator skill.
