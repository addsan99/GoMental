// Unified transport surface. Selects the Wails desktop bindings or the HTTP
// (server) implementation at runtime, with VITE_TRANSPORT as an override.
import * as http from './http'
import * as wails from './wails'

const forced = import.meta.env.VITE_TRANSPORT as string | undefined
const useHttp =
  forced === 'http' ||
  (forced !== 'wails' && !(typeof window !== 'undefined' && (window as any)?.go?.main?.App))

const impl = useHttp ? http : wails

export const Backlinks = impl.Backlinks
export const DeleteNote = impl.DeleteNote
export const FullGraph = impl.FullGraph
export const GitSync = impl.GitSync
export const GraphQuery = impl.GraphQuery
export const ImportURL = impl.ImportURL
export const Info = impl.Info
export const ListNotes = impl.ListNotes
export const ListNotesPage = impl.ListNotesPage
export const LoadGraphLayout = impl.LoadGraphLayout
export const LoadNoteAssetDataURL = impl.LoadNoteAssetDataURL
export const LoadUIState = impl.LoadUIState
export const LoadSettings = impl.LoadSettings
export const MoveNote = impl.MoveNote
export const Neighborhood = impl.Neighborhood
export const OpenWorkspace = impl.OpenWorkspace
export const ReadNote = impl.ReadNote
export const Rebuild = impl.Rebuild
export const RecentWorkspaces = impl.RecentWorkspaces
export const SaveGraphLayout = impl.SaveGraphLayout
export const SaveNote = impl.SaveNote
export const SaveNoteAsset = impl.SaveNoteAsset
export const SaveUIState = impl.SaveUIState
export const SaveSettings = impl.SaveSettings
export const Search = impl.Search
export const SetNoteFavorite = impl.SetNoteFavorite
export const SelectWorkspaceDirectory = impl.SelectWorkspaceDirectory
export const onEvent = impl.onEvent
