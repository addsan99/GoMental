// Wails desktop transport: re-exports generated bindings with identical signatures.
import {
  Backlinks,
  DeleteNote,
  FullGraph,
  GraphQuery,
  ImportURL,
  Info,
  ListNotes,
  ListNotesPage,
  LoadGraphLayout,
  LoadNoteAssetDataURL,
  LoadUIState,
  MoveNote as MoveNoteBinding,
  Neighborhood,
  OpenWorkspace,
  ReadNote as ReadNoteBinding,
  Rebuild,
  RecentWorkspaces,
  SaveGraphLayout,
  SaveNote as SaveNoteBinding,
  SaveNoteAsset,
  SaveUIState,
  Search,
  SelectWorkspaceDirectory,
} from '../../wailsjs/go/main/App'
import {EventsOn} from '../../wailsjs/runtime/runtime'
import type {GitSyncResult, GoMentalSettings, MoveNoteRequest, NoteDTOWithVersion, SaveNoteRequestWithVersion, SetNoteFavoriteRequest} from './types'

export {
  Backlinks,
  DeleteNote,
  FullGraph,
  GraphQuery,
  ImportURL,
  Info,
  ListNotes,
  ListNotesPage,
  LoadGraphLayout,
  LoadNoteAssetDataURL,
  LoadUIState,
  Neighborhood,
  OpenWorkspace,
  Rebuild,
  RecentWorkspaces,
  SaveGraphLayout,
  SaveNoteAsset,
  SaveUIState,
  Search,
  SelectWorkspaceDirectory,
}

// The generated binding types omit the optimistic-concurrency fields. Runtime
// marshalling forwards extra fields fine; these wrappers only widen the TS types
// so App.tsx can thread `version`/`baseVersion`/`force` through.
export function ReadNote(id: string): Promise<NoteDTOWithVersion> {
  return ReadNoteBinding(id)
}

export function SaveNote(req: SaveNoteRequestWithVersion): Promise<NoteDTOWithVersion> {
  return SaveNoteBinding(req)
}

export function SetNoteFavorite(req: SetNoteFavoriteRequest): Promise<NoteDTOWithVersion> {
  const app = (window as any)?.go?.main?.App
  if (!app || typeof app.SetNoteFavorite !== 'function') {
    return Promise.reject(new Error('favorites are not available in this build'))
  }
  return app.SetNoteFavorite(req.id, req.favorite)
}

export function MoveNote(req: MoveNoteRequest): Promise<NoteDTOWithVersion> {
  return MoveNoteBinding(req)
}

// GitSync is only bound in viewer git mode. It is called dynamically (not via the
// generated bindings) so the generated module does not need regenerating; the
// desktop default build simply never exposes window.go.main.App.GitSync.
export function GitSync(): Promise<GitSyncResult> {
  const app = (window as any)?.go?.main?.App
  if (!app || typeof app.GitSync !== 'function') {
    return Promise.reject(new Error('git sync is not available (not in viewer git mode)'))
  }
  return app.GitSync()
}

export function LoadSettings(): Promise<GoMentalSettings> {
  const app = (window as any)?.go?.main?.App
  if (!app || typeof app.LoadSettings !== 'function') {
    return Promise.reject(new Error('settings are not available in this build'))
  }
  return app.LoadSettings()
}

export function SaveSettings(settings: GoMentalSettings): Promise<void> {
  const app = (window as any)?.go?.main?.App
  if (!app || typeof app.SaveSettings !== 'function') {
    return Promise.reject(new Error('settings are not available in this build'))
  }
  return app.SaveSettings(settings)
}

export function onEvent(name: string, cb: (...data: any[]) => void): () => void {
  return EventsOn(name, cb)
}
