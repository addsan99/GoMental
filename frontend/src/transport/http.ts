// HTTP transport: fetch-based implementation of the binding surface for browser (server) mode.
import type {application, main} from '../../wailsjs/go/models'
import type {GitSyncResult, GoMentalSettings, MoveNoteRequest, NoteDTOWithVersion, SaveNoteRequestWithVersion} from './types'
import {subscribe} from './events'

// The generated App.d.ts references application.UIState, which is not defined in
// models.ts. Mirror the loose shape App.tsx reads/writes so the signatures match
// the binding surface without depending on the missing type.
type UIState = {
  lastWorkspace?: string
  lastNote?: string
  leftPanelWidth?: number
  theme?: string
}

class TransportError extends Error {
  code?: string
  detail?: string

  constructor(message: string, code?: string, detail?: string) {
    super(message)
    this.name = 'TransportError'
    this.code = code
    this.detail = detail
  }
}

function encodeID(id: string): string {
  return id.split('/').map(encodeURIComponent).join('/')
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: {
      ...(init?.body ? {'Content-Type': 'application/json'} : {}),
      ...(init?.headers || {}),
    },
  })
  if (!response.ok) {
    let code: string | undefined
    let message = `${response.status} ${response.statusText}`.trim()
    let detail: string | undefined
    try {
      const body = await response.json()
      if (body && typeof body === 'object') {
        code = typeof body.code === 'string' ? body.code : undefined
        detail = typeof body.detail === 'string' ? body.detail : undefined
        if (typeof body.message === 'string' && body.message) {
          message = body.message
        }
      }
    } catch {
      // Non-JSON error body; keep the status-derived message.
    }
    throw new TransportError(message, code, detail)
  }
  if (response.status === 204) {
    return undefined as T
  }
  const text = await response.text()
  if (!text) {
    return undefined as T
  }
  return JSON.parse(text) as T
}

function jsonBody(value: unknown): RequestInit {
  return {body: JSON.stringify(value)}
}

export function OpenWorkspace(root: string): Promise<application.WorkspaceDTO> {
  return request('/api/workspace/open', {method: 'POST', ...jsonBody({root})})
}

export function ListNotes(): Promise<Array<application.NoteSummaryDTO>> {
  return request('/api/notes')
}

export function ListNotesPage(req: application.ListNotesQueryDTO): Promise<application.NotesPageDTO> {
  const params = new URLSearchParams()
  params.set('limit', String(req.limit ?? 0))
  params.set('offset', String(req.offset ?? 0))
  if (req.sortBy) params.set('sort', req.sortBy)
  if (req.desc) params.set('desc', '1')
  if (req.tag) params.set('tag', req.tag)
  if (req.search) params.set('q', req.search)
  return request(`/api/notes?${params.toString()}`)
}

export function ReadNote(id: string): Promise<NoteDTOWithVersion> {
  return request(`/api/notes/${encodeID(id)}`)
}

export function SaveNote(req: SaveNoteRequestWithVersion): Promise<NoteDTOWithVersion> {
  return request(`/api/notes/${encodeID(req.id)}`, {
    method: 'PUT',
    ...jsonBody({content: req.content, baseVersion: req.baseVersion, force: req.force}),
  })
}

export function MoveNote(req: MoveNoteRequest): Promise<NoteDTOWithVersion> {
  return request('/api/notes/move', {
    method: 'POST',
    ...jsonBody({id: req.id, newId: req.newId}),
  })
}

export function ImportURL(req: application.ImportURLRequest): Promise<application.NoteDTO> {
  return request('/api/import', {method: 'POST', ...jsonBody({url: req.url})})
}

export function SaveNoteAsset(req: application.SaveNoteAssetRequest): Promise<application.SaveNoteAssetResponse> {
  return request(`/api/assets/${encodeID(req.noteId)}`, {
    method: 'POST',
    ...jsonBody({fileName: req.fileName, mimeType: req.mimeType, dataBase64: req.dataBase64}),
  })
}

export async function LoadNoteAssetDataURL(req: application.NoteAssetRequest): Promise<string> {
  const result = await request<{dataUrl: string}>(
    `/api/assets/${encodeID(req.noteId)}?path=${encodeURIComponent(req.path)}`,
  )
  return result.dataUrl
}

export function DeleteNote(id: string): Promise<void> {
  return request(`/api/notes/${encodeID(id)}`, {method: 'DELETE'})
}

export function Search(req: application.SearchQueryDTO): Promise<Array<application.SearchResultDTO>> {
  return request('/api/search', {method: 'POST', ...jsonBody(req)})
}

export function FullGraph(filter: application.GraphFilterDTO): Promise<application.GraphDTO> {
  return request('/api/graph', {method: 'POST', ...jsonBody(filter)})
}

export function GraphQuery(query: application.GraphQueryDTO): Promise<application.GraphDTO> {
  return request('/api/graph/query', {method: 'POST', ...jsonBody(query)})
}

export function Neighborhood(id: string, depth: number): Promise<application.GraphDTO> {
  return request(`/api/neighborhood/${encodeID(id)}?depth=${encodeURIComponent(String(depth))}`)
}

export function Backlinks(id: string): Promise<Array<application.NoteLinkDTO>> {
  return request(`/api/backlinks/${encodeID(id)}`)
}

export function LoadGraphLayout(): Promise<application.LayoutSnapshotDTO> {
  return request('/api/graph/layout')
}

export function SaveGraphLayout(snapshot: application.LayoutSnapshotDTO): Promise<void> {
  return request('/api/graph/layout', {method: 'PUT', ...jsonBody(snapshot)})
}

export function Rebuild(): Promise<application.RebuildResultDTO> {
  return request('/api/rebuild', {method: 'POST'})
}

export function RecentWorkspaces(): Promise<Array<application.RecentWorkspaceDTO>> {
  return request('/api/recent')
}

export function LoadUIState(): Promise<UIState> {
  return request('/api/ui-state')
}

export function SaveUIState(state: UIState): Promise<void> {
  return request('/api/ui-state', {method: 'PUT', ...jsonBody(state)})
}

export function LoadSettings(): Promise<GoMentalSettings> {
  return request('/api/settings')
}

export function SaveSettings(settings: GoMentalSettings): Promise<void> {
  return request('/api/settings', {method: 'PUT', ...jsonBody(settings)})
}

export function Info(): Promise<main.AppInfo> {
  return request('/api/info')
}

export function GitSync(): Promise<GitSyncResult> {
  return request('/api/git/sync', {method: 'POST'})
}

export function SelectWorkspaceDirectory(): Promise<string> {
  return Promise.reject(new Error('native directory picker is not available in server mode'))
}

export function onEvent(name: string, cb: (...data: any[]) => void): () => void {
  return subscribe(name, (payload) => cb(payload))
}
