import {registerPlugin} from '@capacitor/core'

export type NativeStatus = {
  ready: boolean
  repositoryPath: string
  noteCount: number
  remote?: string
  ref?: string
  commit?: string
  configuredRemote?: string
  configuredRef?: string
  hasCredential?: boolean
}

export type NativeNote = {
  id: string
  title: string
  path: string
  type?: string
  tags: string[]
  favorite: boolean
  modifiedAt?: string
}

export type NativeNoteDetail = NativeNote & {
  raw: string
  body: string
  description?: string
  resource?: string
  headings: Array<{level: number; text: string; slug: string}>
  links: Array<{rawTarget: string; resolvedId?: string; displayText?: string; heading?: string; kind: string}>
  metadata: Array<{key: string; value: string}>
  incomingLinks: Array<{id: string; title: string; path: string; displayText?: string}>
}

export type NativeSearchResult = {
  id: string
  path: string
  title: string
  score: number
  fragments: string[]
  favorite: boolean
}

type GoMentalNativePlugin = {
  configure(options: {remote: string; ref: string}): Promise<{remote: string; ref: string}>
  editCredential(): Promise<{hasCredential: boolean}>
  status(): Promise<NativeStatus>
  sync(): Promise<{cloned: boolean; fetched: boolean; changed: boolean; newCommit: string; noteCount: number}>
  listNotes(options: {query: Record<string, unknown>}): Promise<{notes: NativeNote[]}>
  search(options: {query: Record<string, unknown>}): Promise<{results: NativeSearchResult[]}>
  readNote(options: {id: string}): Promise<NativeNoteDetail>
  loadAsset(options: {noteId: string; path: string}): Promise<{dataUrl: string}>
  cancel(): Promise<void>
  close(): Promise<void>
}

export const GoMentalNative = registerPlugin<GoMentalNativePlugin>('GoMentalNative')

export async function loadAssetDataURL(request: {noteId: string; path: string}): Promise<string> {
  return (await GoMentalNative.loadAsset(request)).dataUrl
}
