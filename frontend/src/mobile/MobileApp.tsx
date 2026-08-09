import {useDeferredValue, useEffect, useState} from 'react'
import {MarkdownArticle, parseArticle} from '../ui/MarkdownArticle'
import {GoMentalNative, loadAssetDataURL, type NativeNote, type NativeNoteDetail, type NativeStatus} from './native'

type Screen = 'library' | 'note' | 'settings' | 'setup'
type Theme = 'light' | 'dark'

function messageFrom(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

function normalizeTarget(sourceID: string, rawTarget: string): string {
  const target = rawTarget.trim().split('#', 1)[0].replace(/\.md$/i, '').replace(/\\/g, '/')
  if (target.startsWith('/')) return target.slice(1)
  if (!target.startsWith('.')) return target
  const parts = sourceID.split('/')
  parts.pop()
  for (const segment of target.split('/')) {
    if (!segment || segment === '.') continue
    if (segment === '..') parts.pop()
    else parts.push(segment)
  }
  return parts.join('/')
}

function resolveTarget(note: NativeNoteDetail, rawTarget: string): string {
  const clean = (value: string) => value.trim().split('#', 1)[0].replace(/\.md$/i, '').replace(/\\/g, '/')
  const target = clean(rawTarget)
  const resolved = note.links.find((link) => link.resolvedId && clean(link.rawTarget) === target)
  return resolved?.resolvedId ?? normalizeTarget(note.id, rawTarget)
}

export function MobileApp() {
  const [status, setStatus] = useState<NativeStatus | null>(null)
  const [notes, setNotes] = useState<NativeNote[]>([])
  const [selected, setSelected] = useState<NativeNoteDetail | null>(null)
  const [screen, setScreen] = useState<Screen>('library')
  const [query, setQuery] = useState('')
  const deferredQuery = useDeferredValue(query)
  const [remote, setRemote] = useState('')
  const [ref, setRef] = useState('main')
  const [hasCredential, setHasCredential] = useState(false)
  const [busy, setBusy] = useState(true)
  const [error, setError] = useState('')
  const [theme, setTheme] = useState<Theme>(() => {
    const saved = localStorage.getItem('gomental-mobile-theme')
    if (saved === 'light' || saved === 'dark') return saved
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  })

  useEffect(() => {
    document.documentElement.dataset.theme = theme
    localStorage.setItem('gomental-mobile-theme', theme)
    document.querySelector('meta[name="theme-color"]')?.setAttribute('content', theme === 'dark' ? '#171815' : '#eeece4')
  }, [theme])

  async function refreshLibrary(nextStatus?: NativeStatus) {
    const current = nextStatus ?? await GoMentalNative.status()
    setStatus(current)
    setRemote(current.configuredRemote ?? current.remote ?? '')
    setRef(current.configuredRef ?? current.ref ?? 'main')
    setHasCredential(Boolean(current.hasCredential))
    if (!current.ready) {
      setScreen('setup')
      setNotes([])
      return
    }
    setNotes((await GoMentalNative.listNotes({query: {}})).notes)
    setScreen((value) => value === 'setup' ? 'library' : value)
  }

  useEffect(() => {
    let active = true
    void (async () => {
      try {
        const initial = await GoMentalNative.status()
        if (active) await refreshLibrary(initial)
      } catch (cause) {
        if (active) setError(messageFrom(cause))
      } finally {
        if (active) setBusy(false)
      }
    })()
    return () => { active = false }
  }, [])

  useEffect(() => {
    if (!status?.ready || screen !== 'library') return
    let active = true
    const text = deferredQuery.trim()
    void (async () => {
      try {
        if (!text) {
          const result = await GoMentalNative.listNotes({query: {}})
          if (active) setNotes(result.notes)
        } else {
          const result = await GoMentalNative.search({query: {text, limit: 100}})
          if (active) setNotes(result.results.map((item) => ({...item, tags: [], type: ''})))
        }
      } catch (cause) {
        if (active) setError(messageFrom(cause))
      }
    })()
    return () => { active = false }
  }, [deferredQuery, screen, status?.ready])

  async function syncRepository() {
    setBusy(true)
    setError('')
    try {
      if (screen === 'setup' || remote !== status?.configuredRemote || ref !== status?.configuredRef) {
        await GoMentalNative.configure({remote, ref})
      }
      await GoMentalNative.sync()
      setSelected(null)
      setQuery('')
      await refreshLibrary()
    } catch (cause) {
      setError(messageFrom(cause))
    } finally {
      setBusy(false)
    }
  }

  async function editCredential() {
    setError('')
    try {
      await GoMentalNative.configure({remote, ref})
      const result = await GoMentalNative.editCredential()
      setHasCredential(result.hasCredential)
    } catch (cause) {
      setError(messageFrom(cause))
    }
  }

  async function openNote(id: string) {
    setBusy(true)
    setError('')
    try {
      const note = await GoMentalNative.readNote({id})
      setSelected(note)
      setScreen('note')
      window.scrollTo({top: 0})
    } catch (cause) {
      setError(messageFrom(cause))
    } finally {
      setBusy(false)
    }
  }

  if (busy && !status) {
    return <main className="mobile-loading"><div className="mobile-mark">GM</div><p>Opening your library</p></main>
  }

  if (screen === 'setup') {
    return (
      <main className="setup-screen">
        <section className="setup-card">
          <div className="mobile-mark">GM</div>
          <p className="eyebrow">Offline notes</p>
          <h1>Carry the repository.</h1>
          <p className="setup-copy">Clone a public HTTPS Git repository onto this device. After the first sync, reading and search work without a network.</p>
          <label>Repository URL<input value={remote} onChange={(event) => setRemote(event.target.value)} placeholder="https://github.com/you/notes.git" inputMode="url" autoCapitalize="none" /></label>
          <label>Branch<input value={ref} onChange={(event) => setRef(event.target.value)} placeholder="main" autoCapitalize="none" /></label>
          <button className="credential-action" disabled={!remote.trim()} onClick={() => void editCredential()}>{hasCredential ? 'Private credential saved' : 'Add private repository credential'}</button>
          {error && <p className="mobile-error" role="alert">{error}</p>}
          <button className="primary-action" disabled={busy || !remote.trim()} onClick={() => void syncRepository()}>{busy ? 'Cloning...' : 'Clone repository'}</button>
          <p className="security-note">Private tokens are entered in a native Android dialog, encrypted with Android Keystore, and never enter the WebView.</p>
        </section>
      </main>
    )
  }

  if (screen === 'note' && selected) {
    return (
      <main className="note-screen">
        <header className="note-toolbar">
          <button className="icon-button" onClick={() => { setScreen('library'); setSelected(null) }} aria-label="Back to library">&larr;</button>
          <span>{selected.path}</span>
        </header>
        {error && <p className="mobile-error floating-error" role="alert">{error}</p>}
        <MarkdownArticle
          model={parseArticle(selected.raw, selected.title)}
          tags={selected.tags ?? []}
          noteID={selected.id}
          onNavigate={(target) => void openNote(resolveTarget(selected, target))}
          loadAsset={loadAssetDataURL}
          theme={theme}
        />
        <section className="note-details">
          <div className="detail-section">
            <p className="detail-kicker">OKF metadata</p>
            <h2>Document fields</h2>
            {selected.metadata.length > 0 ? (
              <dl className="metadata-list">
                {selected.metadata.map((field) => (
                  <div key={field.key}><dt>{field.key}</dt><dd>{field.value}</dd></div>
                ))}
              </dl>
            ) : <p className="detail-empty">No metadata fields.</p>}
          </div>
          <div className="detail-section">
            <p className="detail-kicker">Incoming links</p>
            <h2>{selected.incomingLinks.length} {selected.incomingLinks.length === 1 ? 'reference' : 'references'}</h2>
            {selected.incomingLinks.length > 0 ? (
              <div className="incoming-list">
                {selected.incomingLinks.map((link) => (
                  <button key={link.id} onClick={() => void openNote(link.id)}>
                    <strong>{link.title || link.id}</strong>
                    <span>{link.path}</span>
                    {link.displayText && <small>{link.displayText}</small>}
                  </button>
                ))}
              </div>
            ) : <p className="detail-empty">No notes link to this document.</p>}
          </div>
        </section>
      </main>
    )
  }

  if (screen === 'settings') {
    return (
      <main className="settings-screen">
        <header className="note-toolbar">
          <button className="icon-button" onClick={() => setScreen('library')} aria-label="Back to library">&larr;</button>
          <span>Settings</span>
        </header>
        <div className="settings-content">
          <p className="eyebrow">Android</p>
          <h1>Settings</h1>
          <section className="settings-card">
            <p className="settings-label">Appearance</p>
            <div className="theme-toggle" role="group" aria-label="Theme">
              <button className={theme === 'light' ? 'active' : ''} onClick={() => setTheme('light')}>Light</button>
              <button className={theme === 'dark' ? 'active' : ''} onClick={() => setTheme('dark')}>Dark</button>
            </div>
          </section>
          <section className="settings-card">
            <p className="settings-label">GitHub repository</p>
            <div className="repository-summary"><strong>{remote}</strong><span>Branch: {ref}</span></div>
            <button className="credential-action" onClick={() => void editCredential()}>{hasCredential ? 'Update or clear credential' : 'Add private credential'}</button>
            <p className="security-note">The token is entered in Android UI and encrypted with Android Keystore. It is never available to this WebView.</p>
          </section>
          {error && <p className="mobile-error" role="alert">{error}</p>}
        </div>
      </main>
    )
  }

  return (
    <main className="library-screen">
      <header className="library-header">
        <div><p className="eyebrow">GoMental</p><h1>Library</h1></div>
        <div className="library-actions">
          <button className="settings-button" onClick={() => setScreen('settings')} aria-label="Settings">
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 8.5a3.5 3.5 0 1 0 0 7 3.5 3.5 0 0 0 0-7Z"/><path d="M19 13.5v-3l-2-.7-.5-1.2.9-1.9-2.1-2.1-1.9.9-1.2-.5-.7-2h-3l-.7 2-1.2.5-1.9-.9-2.1 2.1.9 1.9-.5 1.2-2 .7v3l2 .7.5 1.2-.9 1.9 2.1 2.1 1.9-.9 1.2.5.7 2h3l.7-2 1.2-.5 1.9.9 2.1-2.1-.9-1.9.5-1.2 2-.7Z"/></svg>
          </button>
          <button className="sync-button" disabled={busy} onClick={() => void syncRepository()}>{busy ? 'Syncing...' : 'Sync'}</button>
        </div>
      </header>
      <div className="search-wrap"><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="10.5" cy="10.5" r="6.5"/><path d="m15.5 15.5 5 5"/></svg><input type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search notes" aria-label="Search notes" /></div>
      {error && <p className="mobile-error" role="alert">{error}</p>}
      <p className="library-meta">{notes.length} {notes.length === 1 ? 'note' : 'notes'}{status?.commit ? ` / ${status.commit.slice(0, 8)}` : ''}</p>
      <section className="note-list" aria-label="Notes">
        {notes.map((note) => (
          <button className="note-row" key={note.id} onClick={() => void openNote(note.id)}>
            <span className="note-type">{note.type || 'note'}</span>
            <strong>{note.title || note.id}</strong>
            <span className="note-path">{note.path}</span>
            {note.tags?.length > 0 && <span className="note-tags">{note.tags.slice(0, 3).map((tag) => `#${tag}`).join(' ')}</span>}
          </button>
        ))}
        {!busy && notes.length === 0 && <div className="empty-library"><strong>No notes found</strong><span>Try another search or sync the repository.</span></div>}
      </section>
    </main>
  )
}
