// Extended request/response shapes for optimistic concurrency.
//
// The generated wailsjs/go/models.ts (which we cannot edit) does not yet include
// the `version` token on NoteDTO nor the `baseVersion`/`force` fields on
// SaveNoteRequest. Mirror the UIState-gap pattern used elsewhere in this folder:
// define small local extensions here and have the transport functions
// accept/return them so the compiler stays green.
import type {application, main} from '../../wailsjs/go/models'

// Git-viewer status surfaced on `/api/info` (and `GET /api/git/status`) when the
// server tracks a git working copy. Null / absent when git mode is off.
export type GitStatusInfo = {
  remote: string
  ref: string
  baseRef?: string
  branch?: string
  commit: string
  ahead?: number
  dirty?: boolean
  pullRequest?: string
  lastSyncAt?: string | null
  lastPushedAt?: string | null
  lastError?: string
  syncing?: boolean
  operation?: string
}

// AppInfo plus the `mode`/`workspace` fields the HTTP `/api/info` handler adds
// (server-adapter-synthesized; not present on the desktop Wails Info()). Lets the
// SPA bootstrap detect server mode and open the server's configured workspace.
// `readOnly`/`git` are added by the git-viewer server mode (§5.5); both optional
// so the desktop Wails Info() (which omits them) still typechecks.
// Omit the generated `convertValues` helper method so plain object literals
// (emptyInfo, the git-status spread) satisfy the type — we only read the data
// fields, never call the Wails class method.
export type AppInfoWithMode = Omit<main.AppInfo, 'convertValues'> & {
  mode?: string
  workspace?: string
  readOnly?: boolean
  git?: GitStatusInfo | null
}

// Result of a manual git pull (POST /api/git/sync over HTTP, or the GitSync
// Wails binding in desktop viewer mode). Informational — the real content
// reconcile is driven by the workspace watcher + git:synced event.
export type GitSyncResult = {
  ok: boolean
  oldCommit?: string
  newCommit?: string
  changed?: number
  deleted?: number
}

export type GitPRResult = {
  url: string
  number: number
  merged: boolean
}

// NoteDTO plus the optimistic-concurrency version token returned by
// ReadNote/SaveNote.
export type NoteDTOWithVersion = application.NoteDTO & {
  version?: string
}

// SaveNoteRequest plus the optional base version / force flags the backend reads
// from the request body.
export type SaveNoteRequestWithVersion = application.SaveNoteRequest & {
  baseVersion?: string
  force?: boolean
}

export type SetNoteFavoriteRequest = {
  id: string
  favorite: boolean
}

export type MoveNoteRequest = {
  id: string
  newId: string
}

export type GoMentalSettings = {
  version: number
  appearance: {
    theme: string
  }
  noteView: {
    defaultEditMode: 'rich' | 'source'
    showFindBar: boolean
  }
  graphView: {
    defaultMode: '2d' | '3d'
    defaultDepth: number
  }
  workspaces: Record<string, GoMentalWorkspaceSettings>
}

export type GoMentalWorkspaceSettings = {
  defaultType: string
  enabledTypes: string[]
  accessMode: 'editable' | 'readOnlyLocal' | 'readOnlyGit' | 'writableGit'
  gitUrl: string
  gitBaseRef: string
  gitBranch: string
  gitUsername: string
  gitToken: string
  gitExitAction: 'none' | 'prompt' | 'autoPr' | 'autoMerge'
  suggestedLinks: {
    mode: 'off' | 'prompt' | 'automatic'
    trigger: 'whileEditing' | 'onSave'
    placement: 'relatedSection' | 'preferInline'
    minScore: number
    maxSuggestions: number
  }
}

export type SuggestLinksRequest = {
  id: string
  content: string
  limit: number
  minScore: number
}

export type LinkSuggestion = {
  targetId: string
  targetTitle: string
  score: number
  confidence: 'possible' | 'strong' | 'high'
  evidence: Array<{kind: string; detail: string; weight: number}>
  defaultPlacement: 'relatedSection'
}

export type SuggestLinksResponse = {
  draftHash: string
  algorithm: string
  items: LinkSuggestion[]
}

export type NoteType = {
  id: string
  label: string
  description: string
  template: string
  source: string
}
