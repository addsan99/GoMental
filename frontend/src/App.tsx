import {lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState} from 'react';
import type {CSSProperties, PointerEvent as ReactPointerEvent, ReactNode} from 'react';
import './App.css';
import type {MdxNoteEditorHandle} from './MdxNoteEditor';
import type {CodeMirrorEditorHandle} from './CodeMirrorEditor';
import CommandPalette from './ui/CommandPalette';
import LinkPicker from './ui/LinkPicker';
import SidebarNoteTree from './ui/SidebarNoteTree';
import Toast from './ui/Toast';
import {MarkdownArticle, parseArticle, slugify} from './ui/MarkdownArticle';
import type {OutlineEntry} from './ui/MarkdownArticle';
import FindBar from './ui/FindBar';
import {basename, errorMessage} from './util';
import {FacetFilters, facetMatchesNote, anyFacetActive, folderOf} from './ui/graph/filters';
import type {FacetFilter, FacetOption} from './ui/graph/filters';
import {
  AppMark,
  Wordmark,
  ChevronIcon,
  ClockIcon,
  CloseIcon,
  CodeIcon,
  TrashIcon,
  EditIcon,
  FolderIcon,
  GearIcon,
  GraphTabIcon,
  ImportIcon,
  LinkIcon,
  MoonIcon,
  NoteTabIcon,
  PlusIcon,
  RefreshIcon,
  SaveIcon,
  SearchIcon,
  StarIcon,
  SunIcon,
} from './ui/icons';
import {
  Backlinks,
  DeleteNote,
  DeleteNoteType,
  GitMergePullRequest,
  GitOpenPullRequest,
  GitSync,
  Info,
  ImportURL,
  ImportNoteTypeCollection,
  ListNotes,
  ListNoteTypes,
  LoadNoteAssetDataURL,
  LoadSettings,
  LoadUIState,
  MoveNote,
  OpenWorkspace,
  ReadNote,
  Rebuild,
  RecentWorkspaces,
  SaveNote,
  SaveNoteType,
  SaveNoteAsset,
  SaveSettings,
  SaveUIState,
  Search,
  SuggestLinks,
  SetNoteFavorite,
  SelectWorkspaceDirectory,
  onEvent,
} from './transport';
import type {application} from '../wailsjs/go/models';
import type {AppInfoWithMode, GoMentalSettings, GoMentalWorkspaceSettings, LinkSuggestion, NoteDTOWithVersion, NoteType} from './transport/types';
import {CSS_VARIABLE_NAMES, cssVariablesForTheme, loadVSCodeTheme} from './themes/vscode';
import {themeOption, vscodeThemeOptions} from './themes/catalog';

// Heavy views are code-split so they stay out of the initial bundle: the graph
// canvas (sigma + graphology) and the two editors (@mdxeditor / CodeMirror) are
// only fetched when the graph tab or an edit mode is first entered. The first
// screen (reading a note) needs none of them — it renders via MarkdownArticle.
const GraphView3D = lazy(() => import('./ui/GraphView3D'));
const MdxNoteEditor = lazy(() => import('./MdxNoteEditor'));
const CodeMirrorEditor = lazy(() => import('./CodeMirrorEditor'));

// prefetchEditors warms the editor chunks during idle time after first paint so
// entering edit mode never stalls on a network/disk fetch. Exported-style helper
// kept module-local; safe to call repeatedly (import() is cached).
function prefetchEditors() {
  void import('./MdxNoteEditor');
  void import('./CodeMirrorEditor');
}

type TreeGroup = {
  name: string;
  depth: number;
  notes: application.NoteSummaryDTO[];
};

type RebuildProgress = {
  Stage?: string;
  stage?: string;
  Completed?: number;
  completed?: number;
  Total?: number;
  total?: number;
};

type SaveState = 'idle' | 'dirty' | 'saving' | 'saved' | 'conflict';
type SearchStatus = 'idle' | 'searching' | 'ready' | 'error';
type WorkspaceTab = 'note' | 'graph';
type ThemeMode = string;
type SettingsSection = 'appearance' | 'noteView' | 'graphView' | 'workspaceSettings' | 'types';

// Above this many rendered graph nodes, 3D is auto-disabled: thousands of lit
// spheres + text sprites orbiting is far heavier than the flat top-down view, and
// depth cues stop helping once the scene is that dense. The flat view (with LOD +
// the render cap in GraphView3D) stays usable well beyond this.
const LARGE_GRAPH_3D_MAX = 1200;

// How many recently-visited notes the back/forward history retains.
const HISTORY_MAX = 15;

const emptyInfo: AppInfoWithMode = {
  name: 'GoMental',
  description: 'Local-first OKF notes and knowledge graph',
  phase: '',
};

const DEFAULT_SETTINGS: GoMentalSettings = {
  version: 3,
  appearance: {
    theme: 'dark',
  },
  noteView: {
    defaultEditMode: 'rich',
    showFindBar: true,
  },
  graphView: {
    defaultMode: '2d',
    defaultDepth: 2,
  },
  workspaces: {},
};

function App() {
  const [info, setInfo] = useState<AppInfoWithMode>(emptyInfo);
  const [workspace, setWorkspace] = useState<application.WorkspaceDTO | null>(null);
  const [recent, setRecent] = useState<application.RecentWorkspaceDTO[]>([]);
  const [notes, setNotes] = useState<application.NoteSummaryDTO[]>([]);
  const [selectedID, setSelectedID] = useState<string>('');
  const [selectedNote, setSelectedNote] = useState<NoteDTOWithVersion | null>(null);
  const [draft, setDraft] = useState('');
  const [savedContent, setSavedContent] = useState('');
  const [saveState, setSaveState] = useState<SaveState>('idle');
  const [isEditing, setIsEditing] = useState(false);
  const [backlinks, setBacklinks] = useState<application.NoteLinkDTO[]>([]);
  const [linkSuggestions, setLinkSuggestions] = useState<LinkSuggestion[]>([]);
  const [suggestionsStatus, setSuggestionsStatus] = useState<'idle' | 'loading' | 'ready' | 'error'>('idle');
  const [saveSuggestionReview, setSaveSuggestionReview] = useState<{content: string; items: LinkSuggestion[]; exitEditMode: boolean} | null>(null);
  const [noteVersion, setNoteVersion] = useState<string>('');
  const [conflictOpen, setConflictOpen] = useState(false);
  const [deletedNotice, setDeletedNotice] = useState('');
  const [busy, setBusy] = useState<string>('');
  const [error, setError] = useState<string>('');
  const [progress, setProgress] = useState<string>('');
  const [projectionActive, setProjectionActive] = useState(false);
  const [newNoteOpen, setNewNoteOpen] = useState(false);
  const [newNoteTemplate, setNewNoteTemplate] = useState('term');

  const [noteTypes, setNoteTypes] = useState<NoteType[]>([]);
  const [newNoteTitle, setNewNoteTitle] = useState('');
  const [newNoteID, setNewNoteID] = useState('');
  const [importOpen, setImportOpen] = useState(false);
  const [importURL, setImportURL] = useState('');
  const [searchText, setSearchText] = useState('');
  const [searchResults, setSearchResults] = useState<application.SearchResultDTO[]>([]);
  const [searchStatus, setSearchStatus] = useState<SearchStatus>('idle');
  const [searchError, setSearchError] = useState('');
  const [graphRevision, setGraphRevision] = useState(0);
  const [activeTab, setActiveTab] = useState<WorkspaceTab>('note');
  // Once the graph tab has been opened, keep the 2D (flat) graph mounted (hidden
  // when not active) so its settings, camera and layout survive tab swaps. Gated
  // so the heavy graph chunk still isn't loaded until the tab is first opened.
  const [graphMounted, setGraphMounted] = useState(false);
  // Both modes render via GraphView3D (Three.js): 2D is a flat, top-down lens
  // that persists its {x,y} layout; 3D is the free-orbit lens. 2D is the default;
  // the orbit instance stays unmounted until 3D is first selected, then is kept
  // alive (hidden) so its camera survives mode swaps.
  const [graphMode, setGraphMode] = useState<'2d' | '3d'>(() => readStoredGraphMode());
  const [graph3dMounted, setGraph3dMounted] = useState(false);
  // Depth (hops from the selected note) is shared by both graph instances so it
  // carries over when switching between 2D and 3D.
  const [graphDepth, setGraphDepth] = useState(2);
  const [theme, setTheme] = useState<ThemeMode>(() => readStoredTheme());
  useEffect(() => {
    const shell = document.querySelector<HTMLElement>('.gm-shell');
    if (!shell) return;
    let cancelled = false;
    CSS_VARIABLE_NAMES.forEach((key) => shell.style.removeProperty(key));
    if (!themeOption(theme)) return;
    const importedTheme = loadVSCodeTheme(theme);
    if (!cancelled && importedTheme) {
      const variables = cssVariablesForTheme(importedTheme);
      CSS_VARIABLE_NAMES.forEach((key) => shell.style.setProperty(key, variables[key]));
    }
    return () => { cancelled = true; };
  }, [theme]);
  // Facet selection (Types / Tags / Folders), owned here and shared by the right-rail
  // filter panel, the note-list tree (hides non-matches), and both graph instances.
  const [facets, setFacets] = useState<FacetFilter>({types: [], tags: [], folders: [], favorites: false});
  // Browser-style visit history of note IDs. Every selection path funnels through
  // setSelectedID, so a single effect records history; back/forward/dropdown jumps
  // set suppressHistoryRef to avoid re-recording the entry they navigate to. Stack
  // and index live in one object so the recording updater stays pure.
  const [nav, setNav] = useState<{stack: string[]; index: number}>({stack: [], index: -1});
  const suppressHistoryRef = useRef(false);
  const [historyMenuOpen, setHistoryMenuOpen] = useState(false);
  const [openWorkspaceMenuOpen, setOpenWorkspaceMenuOpen] = useState(false);
  const [settings, setSettings] = useState<GoMentalSettings>(DEFAULT_SETTINGS);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [settingsSection, setSettingsSection] = useState<SettingsSection>('appearance');
  const [settingsSaveState, setSettingsSaveState] = useState<'idle' | 'saving' | 'saved' | 'error'>('idle');

  // New UI-only state for the redesigned shell.
  const [rawMode, setRawMode] = useState(false);
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [linkPickerOpen, setLinkPickerOpen] = useState(false);
  const [linkLabelDefault, setLinkLabelDefault] = useState('');
  const [savedFlash, setSavedFlash] = useState(false);
  const [toastMsg, setToastMsg] = useState('');
  const [activeAnchor, setActiveAnchor] = useState('');
  const [graphStats, setGraphStats] = useState<{notes: number; links: number}>({notes: 0, links: 0});

  // Resizable left pane (persisted) + collapsible right rail (persisted).
  const [sidebarWidth, setSidebarWidth] = useState<number>(() => readStoredSidebarWidth());
  const [railCollapsed, setRailCollapsed] = useState<boolean>(() => readStoredRailCollapsed());

  const mdxEditorRef = useRef<MdxNoteEditorHandle | null>(null);
  const codeMirrorRef = useRef<CodeMirrorEditorHandle | null>(null);
  const historyNavRef = useRef<HTMLDivElement | null>(null);
  const openWorkspaceMenuRef = useRef<HTMLDivElement | null>(null);
  const searchRequestRef = useRef(0);
  const noteRequestRef = useRef(0);
  const backlinksRequestRef = useRef(0);
  const workspaceEpochRef = useRef(0);
  const selectedIDRef = useRef('');
  const initialLoadRef = useRef(false);
  const graphReloadTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const toastTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const savedFlashTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const articleScrollRef = useRef<HTMLDivElement | null>(null);
  const pendingEditNoteRef = useRef('');
  const suggestionRequestRef = useRef(0);
  const reviewedSuggestionDraftRef = useRef('');

  const showToast = useCallback((message: string) => {
    setToastMsg(message);
    if (toastTimerRef.current !== null) {
      clearTimeout(toastTimerRef.current);
    }
    toastTimerRef.current = setTimeout(() => setToastMsg(''), 1900);
  }, []);

  const applySettingsToUI = useCallback((next: GoMentalSettings) => {
    const normalized = normalizeSettings(next);
    setSettings(normalized);
    setTheme(normalized.appearance.theme);
    setGraphMode(normalized.graphView.defaultMode);
    setGraphDepth(normalized.graphView.defaultDepth);
  }, []);

  const persistSettings = useCallback((next: GoMentalSettings) => {
    const normalized = normalizeSettings(next);
    applySettingsToUI(normalized);
    setSettingsSaveState('saving');
    void SaveSettings(normalized)
      .then(() => {
        setSettingsSaveState('saved');
        window.setTimeout(() => setSettingsSaveState('idle'), 1400);
      })
      .catch((err) => {
        setSettingsSaveState('error');
        setError(errorMessage(err));
      });
  }, [applySettingsToUI]);

  const loadRecent = useCallback(async () => {
    setRecent(await RecentWorkspaces());
  }, []);

  const fetchCurrentBacklinks = useCallback(async (id: string) => {
    const requestID = backlinksRequestRef.current + 1;
    backlinksRequestRef.current = requestID;
    const workspaceEpoch = workspaceEpochRef.current;
    try {
      const links = await Backlinks(id);
      if (
        backlinksRequestRef.current !== requestID ||
        workspaceEpochRef.current !== workspaceEpoch ||
        selectedIDRef.current !== id
      ) {
        return null;
      }
      return links;
    } catch (err) {
      // A request invalidated by a note/workspace switch is expected to fail if
      // the old graph store closes while it is in flight. Do not surface that
      // failure in the newly opened workspace.
      if (backlinksRequestRef.current !== requestID || workspaceEpochRef.current !== workspaceEpoch) {
        return null;
      }
      throw err;
    }
  }, []);

  // Re-fetch /api/info so the git status chip (ref/commit/lastSyncAt/error)
  // reflects the latest sync. Content refresh rides on note:updated/graph:updated
  // (emitted separately by the backend), so this only refreshes git-level state.
  const refreshInfo = useCallback(async () => {
    try {
      setInfo((await Info()) as AppInfoWithMode);
    } catch {
      // Non-fatal: leave the last-known info in place.
    }
  }, []);

  // Manual "pull latest" from the git status chip. A successful sync emits
  // git:synced (→ toast + info refresh + content reconcile via the watcher), so
  // this only needs to reflect an immediate error. Reflect syncing state up
  // front so the chip's dot pulses while the fetch is in flight.
  const pullGit = useCallback(async () => {
    setInfo((prev) => (prev.git ? {...prev, git: {...prev.git, syncing: true}} : prev));
    try {
      await GitSync();
    } catch (err) {
      showToast(errorMessage(err));
    } finally {
      await refreshInfo();
    }
  }, [refreshInfo, showToast]);

  const openGitPr = useCallback(async () => {
    setInfo((prev) => (prev.git ? {...prev, git: {...prev.git, syncing: true}} : prev));
    try {
      const result = await GitOpenPullRequest();
      showToast(result.url ? `Pull request #${result.number} ready` : 'Pull request ready');
      if (result.url) {
        window.open(result.url, '_blank', 'noopener,noreferrer');
      }
    } catch (err) {
      showToast(errorMessage(err));
    } finally {
      await refreshInfo();
    }
  }, [refreshInfo, showToast]);

  const mergeGitPr = useCallback(async () => {
    setInfo((prev) => (prev.git ? {...prev, git: {...prev.git, syncing: true}} : prev));
    try {
      const result = await GitMergePullRequest();
      showToast(result.merged ? `Merged pull request #${result.number}` : `Pull request #${result.number} ready`);
    } catch (err) {
      showToast(errorMessage(err));
    } finally {
      await refreshInfo();
    }
  }, [refreshInfo, showToast]);

  const loadNotes = useCallback(async (preferredID = '') => {
    const items = await ListNotes();
    setNotes(items);
    setWorkspace((current) => current ? {...current, noteCount: items.length} : current);
    const nextID = preferredID || items[0]?.id || '';
    selectedIDRef.current = nextID;
    setSelectedID(nextID);
    return {items, nextID};
  }, []);

  const openWorkspace = useCallback(async (path: string, preferredNote = '') => {
    if (!path || busy || projectionActive) {
      return;
    }
    setBusy('Opening workspace');
    setError('');
    // Invalidate every note-scoped request before the backend swaps graph
    // stores. Otherwise a backlink response from the previous workspace can
    // arrive late and repopulate the rail after it has been cleared.
    workspaceEpochRef.current += 1;
    noteRequestRef.current += 1;
    backlinksRequestRef.current += 1;
    suggestionRequestRef.current += 1;
    selectedIDRef.current = '';
    setSelectedID('');
    setSelectedNote(null);
    setDraft('');
    setSavedContent('');
    setBacklinks([]);
    setNotes([]);
    setSearchResults([]);
    setSearchStatus('idle');
    try {
      const opened = await OpenWorkspace(path);
      setWorkspace(opened);
      const loadedTypes = await ListNoteTypes();
      setNoteTypes(loadedTypes);
      void refreshInfo();
      // Show the note list as soon as it's ready — this is the critical path.
      const {nextID} = await loadNotes(preferredNote);
      // Recent-list refresh and UI-state persistence are not needed for the
      // list to be usable; run them without blocking readiness.
      void loadRecent().catch(() => {});
      void SaveUIState({lastWorkspace: opened.root, lastNote: nextID, theme}).catch(() => {});
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy('');
    }
  }, [busy, loadNotes, loadRecent, projectionActive, refreshInfo, theme]);

  const chooseWorkspace = useCallback(async () => {
    setError('');
    try {
      const path = await SelectWorkspaceDirectory();
      await openWorkspace(path);
    } catch (err) {
      setError(errorMessage(err));
    }
  }, [openWorkspace]);

  const rebuildWorkspace = useCallback(async () => {
    if (projectionActive) {
      return;
    }
    setProjectionActive(true);
    setBusy('Rebuilding projections');
    setError('');
    setProgress('Rebuilding 0% complete');
    try {
      await Rebuild();
      const {items} = await loadNotes(selectedID);
      setProgress('Rebuilding 100% complete');
      setProjectionActive(false);
      showToast(`Index rebuilt · ${items.length} notes`);
    } catch (err) {
      setProjectionActive(false);
      setError(errorMessage(err));
    } finally {
      setBusy('');
    }
  }, [loadNotes, projectionActive, selectedID, showToast]);

  const createNote = useCallback(async () => {
    if (!workspace || busy || info.readOnly || workspaceIsReadOnly(settings, workspace.root)) {
      return;
    }
    const title = newNoteTitle.trim();
    const id = normalizeNewNoteID(newNoteID || title);
    if (!id) {
      setError('Enter a note title or note ID.');
      return;
    }
    if (notes.some((note) => note.id.toLocaleLowerCase() === id.toLocaleLowerCase())) {
      setError(`A note already exists at ${id}.`);
      return;
    }
    setBusy('Creating note');
    setError('');
    try {
      const noteType = noteTypes.find((item) => item.id === newNoteTemplate) || noteTypes[0];
      if (!noteType) {
        setError('This workspace has no installed note types. Add one in Settings > Types.');
        return;
      }
      const content = renderNoteTypeStarterContent(noteType, title || basename(id), id);
      const saved = await SaveNote({id, content});
      pendingEditNoteRef.current = saved.id;
      setNewNoteOpen(false);
      setNewNoteTemplate(noteType.id);
      setNewNoteTitle('');
      setNewNoteID('');
      setSelectedNote(saved);
      setDraft(saved.content);
      setSavedContent(saved.content);
      setSaveState('saved');
      setNoteVersion(saved.version ?? '');
      setIsEditing(true);
      setRawMode(false);
      setActiveTab('note');
      await loadNotes(saved.id);
      setSelectedID(saved.id);
      await SaveUIState({lastWorkspace: workspace.root, lastNote: saved.id, theme});
      showToast('New note created');
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy('');
    }
  }, [busy, info.readOnly, loadNotes, newNoteID, newNoteTemplate, newNoteTitle, noteTypes, notes, settings, showToast, theme, workspace]);

  const importFromURL = useCallback(async () => {
    if (!workspace || busy || info.readOnly || workspaceIsReadOnly(settings, workspace.root)) {
      return;
    }
    const url = importURL.trim();
    if (!url) {
      setError('Enter a URL to import.');
      return;
    }
    setBusy('Importing URL');
    setError('');
    try {
      const saved = await ImportURL({url});
      setImportOpen(false);
      setImportURL('');
      setSelectedNote(saved);
      setDraft(saved.content);
      setSavedContent(saved.content);
      setSaveState('saved');
      setNoteVersion((saved as NoteDTOWithVersion).version ?? '');
      setIsEditing(false);
      setRawMode(false);
      await loadNotes(saved.id);
      setSelectedID(saved.id);
      setActiveTab('note');
      await SaveUIState({lastWorkspace: workspace.root, lastNote: saved.id, theme});
      showToast('Note imported');
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy('');
    }
  }, [busy, importURL, info.readOnly, loadNotes, settings, showToast, theme, workspace]);

  useEffect(() => {
    const offProgress = onEvent('index:progress', (payload: RebuildProgress) => {
      const stage = payload?.stage || payload?.Stage || 'indexing';
      const normalizedStage = stage.toLocaleLowerCase();
      const completed = payload?.completed ?? payload?.Completed ?? 0;
      const total = payload?.total ?? payload?.Total ?? 0;
      const percent = rebuildProgressPercent(normalizedStage, completed, total);
      if (normalizedStage === 'complete') {
        setProjectionActive(false);
        setProgress('Rebuilding 100% complete');
        return;
      }
      setProgress(`Rebuilding ${percent}% complete`);
    });
    const offRepairing = onEvent('projection:repairing', () => {
      setProjectionActive(true);
      setProgress('Rebuilding 0% complete');
    });
    const offRepaired = onEvent('projection:repaired', () => {
      setProjectionActive(false);
      setProgress('Rebuilding 100% complete');
    });
    const bumpGraphRevision = () => {
      if (graphReloadTimerRef.current !== null) {
        clearTimeout(graphReloadTimerRef.current);
      }
      graphReloadTimerRef.current = setTimeout(() => {
        graphReloadTimerRef.current = null;
        setGraphRevision((value) => value + 1);
      }, 280);
    };
    const isDirty = draft !== savedContent;
    const offUpdated = onEvent('note:updated', (payload: NoteDTOWithVersion) => {
      void loadNotes(selectedID);
      bumpGraphRevision();
      if (payload?.id !== selectedID) {
        return;
      }
      const incomingVersion = payload.version ?? '';
      if (incomingVersion && incomingVersion === noteVersion) {
        return;
      }
      if (payload.content === draft) {
        // This is our own save echoing back through the watcher/event stream.
        setSelectedNote(payload);
        setSavedContent(payload.content);
        setNoteVersion(incomingVersion);
        setSaveState('saved');
        setConflictOpen(false);
        void fetchCurrentBacklinks(payload.id)
          .then((links) => { if (links !== null) setBacklinks(links); })
          .catch((err) => setError(errorMessage(err)));
        return;
      }
      if (!isDirty && !isEditing) {
        // Safe live refresh: no unsaved local edits to clobber.
        setSelectedNote(payload);
        setDraft(payload.content);
        setSavedContent(payload.content);
        setNoteVersion(incomingVersion);
        setSaveState('saved');
        setConflictOpen(false);
        void fetchCurrentBacklinks(payload.id)
          .then((links) => { if (links !== null) setBacklinks(links); })
          .catch((err) => setError(errorMessage(err)));
      } else {
        // Someone else changed the open note while we have unsaved edits.
        setSaveState('conflict');
        setConflictOpen(true);
      }
    });
    const offDeleted = onEvent('note:deleted', (payload: {id?: string}) => {
      bumpGraphRevision();
      if (payload?.id && payload.id === selectedID) {
        setDeletedNotice('This note was removed on the server.');
      }
      void loadNotes('');
    });
    const offGraph = onEvent('graph:updated', () => bumpGraphRevision());
    // git:synced is the human-facing "just pulled" signal. Content refresh is
    // handled by note:updated/note:deleted/graph:updated (above); here we only
    // toast and refresh the git status chip.
    const offGitSynced = onEvent('git:synced', () => {
      showToast('Pulled latest from git');
      void refreshInfo();
    });
    const offGitStatus = onEvent('git:status', (payload: AppInfoWithMode['git']) => {
      if (!payload) {
        return;
      }
      setInfo((prev) => ({...prev, git: {...(prev.git ?? payload), ...payload}}));
    });
    const offGitPushed = onEvent('git:pushed', () => {
      showToast('Pushed branch to git');
      void refreshInfo();
    });
    const offGitPr = onEvent('git:pr', (payload: {url?: string; number?: number}) => {
      showToast(payload?.number ? `Pull request #${payload.number} ready` : 'Pull request ready');
      void refreshInfo();
    });
    const offGitMerged = onEvent('git:merged', (payload: {number?: number}) => {
      showToast(payload?.number ? `Merged pull request #${payload.number}` : 'Pull request merged');
      void refreshInfo();
    });
    const offGitError = onEvent('git:error', (payload: {error?: string}) => {
      showToast(payload?.error || 'Git operation failed');
      void refreshInfo();
    });
    return () => {
      offProgress();
      offRepairing();
      offRepaired();
      offUpdated();
      offDeleted();
      offGraph();
      offGitSynced();
      offGitStatus();
      offGitPushed();
      offGitPr();
      offGitMerged();
      offGitError();
    };
  }, [draft, fetchCurrentBacklinks, isEditing, loadNotes, noteVersion, refreshInfo, savedContent, selectedID, showToast]);

  useEffect(() => {
    if (initialLoadRef.current) {
      return;
    }
    initialLoadRef.current = true;
    void (async () => {
      try {
        const appInfo = (await Info()) as AppInfoWithMode;
        setInfo(appInfo);
        const [state, loadedSettings] = await Promise.all([LoadUIState(), LoadSettings()]);
        const appSettings = normalizeSettings(loadedSettings);
        applySettingsToUI(appSettings);
        const lastNote = typeof state.lastNote === 'string' ? state.lastNote : '';
        // In server mode the SPA must open the server's *configured* workspace,
        // not a path remembered from a different session/machine. Opening a
        // mismatched root returns 403 and would strand the user on the empty
        // picker even though the server already has a workspace open.
        // Server and viewer modes both pin a configured workspace the SPA must
        // open (rather than a path remembered from another session/machine).
        const serverWorkspace =
          appInfo.mode === 'server' || appInfo.mode === 'viewer'
            ? appInfo.workspace ?? ''
            : '';
        const lastWorkspace =
          serverWorkspace ||
          (typeof state.lastWorkspace === 'string' ? state.lastWorkspace : '');
        if (lastWorkspace) {
          // openWorkspace refreshes the recent list itself — no separate call.
          await openWorkspace(lastWorkspace, lastNote);
        } else {
          // No workspace to reopen: load recents for the empty-state picker.
          await loadRecent();
        }
      } catch (err) {
        setError(errorMessage(err));
      }
    })();
  }, [applySettingsToUI, loadRecent, openWorkspace]);

  // Warm the editor chunks during idle time once the shell is interactive, so
  // the first switch into edit mode never stalls on a lazy fetch. The graph
  // chunk is deliberately left on-demand (heaviest, least-used first).
  useEffect(() => {
    if (!workspace) {
      return;
    }
    const ric = (window as unknown as {requestIdleCallback?: (cb: () => void) => number}).requestIdleCallback;
    if (typeof ric === 'function') {
      const handle = ric(() => prefetchEditors());
      const cic = (window as unknown as {cancelIdleCallback?: (h: number) => void}).cancelIdleCallback;
      return () => { if (typeof cic === 'function') cic(handle); };
    }
    const timer = window.setTimeout(() => prefetchEditors(), 1200);
    return () => window.clearTimeout(timer);
  }, [workspace]);

  useEffect(() => {
    const requestID = noteRequestRef.current + 1;
    noteRequestRef.current = requestID;
    backlinksRequestRef.current += 1;
    selectedIDRef.current = selectedID;
    setSelectedNote(null);
    setDraft('');
    setSavedContent('');
    setSaveState('idle');
    const shouldOpenEdit = pendingEditNoteRef.current === selectedID;
    setIsEditing(shouldOpenEdit);
    setRawMode(false);
    setBacklinks([]);
    setNoteVersion('');
    setConflictOpen(false);
    setDeletedNotice('');
    setActiveAnchor('');
    if (articleScrollRef.current) {
      articleScrollRef.current.scrollTop = 0;
    }

    if (!selectedID) {
      return;
    }

    void (async () => {
      setError('');
      try {
        const noteID = selectedID;
        const [note, links] = await Promise.all([ReadNote(noteID), fetchCurrentBacklinks(noteID)]);
        if (noteRequestRef.current !== requestID || note.id !== noteID) {
          return;
        }
        setSelectedNote(note);
        setDraft(note.content);
        setSavedContent(note.content);
        setSaveState('saved');
        setNoteVersion(note.version ?? '');
        if (links !== null) {
          setBacklinks(links);
        }
        if (pendingEditNoteRef.current === noteID) {
          pendingEditNoteRef.current = '';
          setIsEditing(true);
          setRawMode(false);
        }
        if (workspace?.root) {
          await SaveUIState({lastWorkspace: workspace.root, lastNote: noteID, theme});
        }
      } catch (err) {
        if (noteRequestRef.current === requestID) {
          setError(errorMessage(err));
        }
      }
    })();
  }, [fetchCurrentBacklinks, selectedID, workspace?.root, theme]);

  const saveImageAsset = useCallback(async (file: File): Promise<string> => {
    if (!selectedID) {
      throw new Error('Select a note before adding images.');
    }
    const dataBase64 = await fileToBase64(file);
    const saved = await SaveNoteAsset({noteId: selectedID, fileName: file.name || 'image.png', mimeType: file.type || 'image/png', dataBase64});
    return saved.markdown;
  }, [selectedID]);

  const saveEditorImage = useCallback(async (file: File): Promise<{path: string; dataURL: string}> => {
    if (!selectedID) {
      throw new Error('Select a note before adding images.');
    }
    const dataBase64 = await fileToBase64(file);
    const saved = await SaveNoteAsset({noteId: selectedID, fileName: file.name || 'image.png', mimeType: file.type || 'image/png', dataBase64});
    const dataURL = await LoadNoteAssetDataURL({noteId: selectedID, path: saved.path});
    return {path: saved.path, dataURL};
  }, [selectedID]);

  const flashSaved = useCallback(() => {
    setSavedFlash(true);
    if (savedFlashTimerRef.current !== null) {
      clearTimeout(savedFlashTimerRef.current);
    }
    savedFlashTimerRef.current = setTimeout(() => setSavedFlash(false), 1500);
  }, []);

  const saveCurrentNote = useCallback(async (exitEditMode = false, force = false, contentOverride?: string) => {
    if (!selectedID || !selectedNote || saveState === 'saving' || info.readOnly || workspaceIsReadOnly(settings, workspace?.root || '')) {
      return;
    }
    let contentToSave = contentOverride ?? (force ? draft : (isEditing ? draft : (mdxEditorRef.current?.currentContent() ?? draft)));
    if (contentToSave !== draft) {
      setDraft(contentToSave);
    }
    if (!force && contentToSave === savedContent) {
      if (exitEditMode) {
        setIsEditing(false);
        setRawMode(false);
      }
      return;
    }
    const suggestionSettings = workspaceSettingsFor(settings, workspace?.root || '').suggestedLinks;
    if (!force && suggestionSettings.mode !== 'off' && suggestionSettings.trigger === 'onSave' && reviewedSuggestionDraftRef.current !== contentToSave) {
      try {
        const response = await SuggestLinks({
          id: selectedID,
          content: contentToSave,
          limit: suggestionSettings.maxSuggestions,
          minScore: suggestionSettings.minScore,
        });
        if (suggestionSettings.mode === 'prompt' && response.items.length > 0) {
          setSaveSuggestionReview({content: contentToSave, items: response.items, exitEditMode});
          return;
        }
        if (suggestionSettings.mode === 'automatic') {
          const accepted = response.items.filter(isAutomaticSuggestion).slice(0, 3);
          if (accepted.length > 0) {
            contentToSave = addRelatedNoteLinks(contentToSave, accepted);
            setDraft(contentToSave);
            showToast(`Adding ${accepted.length} high-confidence link${accepted.length === 1 ? '' : 's'}`);
          }
        }
        reviewedSuggestionDraftRef.current = contentToSave;
      } catch {
        // Suggestion generation is advisory and must never block an explicit save.
      }
    }
    setSaveState('saving');
    setError('');
    try {
      const saved = await SaveNote({id: selectedID, content: contentToSave, baseVersion: noteVersion, force});
      setSelectedNote(saved);
      setDraft(saved.content);
      setSavedContent(saved.content);
      setNoteVersion(saved.version ?? '');
      setSaveState('saved');
      setConflictOpen(false);
      if (exitEditMode) {
        setIsEditing(false);
        setRawMode(false);
      }
      const [, links] = await Promise.all([loadNotes(saved.id), fetchCurrentBacklinks(saved.id)]);
      if (links !== null) {
        setBacklinks(links);
      }
      flashSaved();
      showToast('Saved to disk');
    } catch (err) {
      if (isConflictError(err)) {
        setSaveState('conflict');
        setConflictOpen(true);
        return;
      }
      setSaveState('dirty');
      setError(errorMessage(err));
    }
  }, [draft, fetchCurrentBacklinks, flashSaved, info.readOnly, isEditing, loadNotes, noteVersion, savedContent, saveState, selectedID, selectedNote, settings, showToast, workspace?.root]);

  const deleteCurrentNote = useCallback(async () => {
    if (!selectedID || !selectedNote || info.readOnly || workspaceIsReadOnly(settings, workspace?.root || '') || busy || projectionActive) {
      return;
    }
    const title = notes.find((note) => note.id === selectedID)?.title || basename(selectedID);
    if (!window.confirm(`Delete "${title}"?\n\nThis removes the note from disk and updates search and graph projections.`)) {
      return;
    }
    setBusy('Deleting note');
    setError('');
    try {
      const deletedID = selectedID;
      await DeleteNote(deletedID);
      const remaining = notes.filter((note) => note.id !== deletedID);
      const nextID = remaining[0]?.id || '';
      setSelectedID(nextID);
      setSelectedNote(null);
      setDraft('');
      setSavedContent('');
      setSaveState('idle');
      setIsEditing(false);
      setRawMode(false);
      setBacklinks([]);
      setNoteVersion('');
      setActiveTab('note');
      setDeletedNotice('');
      await loadNotes(nextID);
      setGraphRevision((value) => value + 1);
      showToast('Note deleted');
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy('');
    }
  }, [busy, info.readOnly, loadNotes, notes, projectionActive, selectedID, selectedNote, settings, showToast, workspace?.root]);

  const toggleNoteFavorite = useCallback(async (id: string, favorite: boolean) => {
    if (!id || info.readOnly || workspaceIsReadOnly(settings, workspace?.root || '') || busy || projectionActive) {
      return;
    }
    if (id === selectedID && isEditing && draft !== savedContent) {
      setError("Save or discard your edits before changing this note's favorite state.");
      return;
    }
    setError('');
    setNotes((current) => current.map((note) => note.id === id ? {...note, favorite} : note));
    setSearchResults((current) => current.map((result) => result.id === id ? {...result, favorite} : result));
    try {
      const saved = await SetNoteFavorite({id, favorite});
      if (selectedID === id) {
        setSelectedNote(saved);
        setDraft(saved.content);
        setSavedContent(saved.content);
        setNoteVersion(saved.version ?? '');
      }
      await loadNotes(selectedID || id);
      setGraphRevision((value) => value + 1);
      showToast(favorite ? 'Added to favorites' : 'Removed from favorites');
    } catch (err) {
      await loadNotes(selectedID || id).catch(() => {});
      setError(errorMessage(err));
    }
  }, [busy, draft, info.readOnly, isEditing, loadNotes, projectionActive, savedContent, selectedID, settings, showToast, workspace?.root]);

  const moveNoteToFolder = useCallback(async (id: string, folder: string) => {
    if (!workspace || info.readOnly || workspaceIsReadOnly(settings, workspace.root) || busy || projectionActive) {
      return;
    }
    const cleanFolder = folder.replace(/^\/+|\/+$/g, '');
    const nextID = cleanFolder ? `${cleanFolder}/${basename(id)}` : basename(id);
    if (nextID === id) {
      return;
    }
    if (notes.some((note) => note.id.toLocaleLowerCase() === nextID.toLocaleLowerCase())) {
      setError(`A note already exists at ${nextID}.`);
      return;
    }
    setBusy('Moving note');
    setError('');
    try {
      const moved = await MoveNote({id, newId: nextID});
      if (selectedID === id) {
        pendingEditNoteRef.current = isEditing ? moved.id : '';
        setSelectedID(moved.id);
      }
      setDeletedNotice('');
      await loadNotes(moved.id);
      setGraphRevision((value) => value + 1);
      showToast('Note moved');
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy('');
    }
  }, [busy, info.readOnly, isEditing, loadNotes, notes, projectionActive, selectedID, settings, showToast, workspace]);

  const reloadFromServer = useCallback(async () => {
    if (!selectedID) {
      return;
    }
    setError('');
    try {
      const [note, links] = await Promise.all([ReadNote(selectedID), fetchCurrentBacklinks(selectedID)]);
      if (note.id !== selectedID) {
        return;
      }
      setSelectedNote(note);
      setDraft(note.content);
      setSavedContent(note.content);
      setNoteVersion(note.version ?? '');
      setSaveState('saved');
      if (links !== null) {
        setBacklinks(links);
      }
      setConflictOpen(false);
    } catch (err) {
      setError(errorMessage(err));
    }
  }, [fetchCurrentBacklinks, selectedID]);

  const overwriteConflict = useCallback(() => {
    void saveCurrentNote(false, true);
  }, [saveCurrentNote]);

  const dismissConflict = useCallback(() => {
    setConflictOpen(false);
    setSaveState(draft === savedContent ? 'saved' : 'dirty');
  }, [draft, savedContent]);

  useEffect(() => {
    const hasQuery = Boolean(searchText.trim());
    const requestID = searchRequestRef.current + 1;
    searchRequestRef.current = requestID;

    if (!workspace || !hasQuery) {
      setSearchResults([]);
      setSearchStatus('idle');
      setSearchError('');
      return;
    }

    setSearchStatus('searching');
    setSearchError('');
    const timer = window.setTimeout(() => {
      void (async () => {
        try {
          const results = await Search({
            text: searchText.trim(),
            tags: [],
            pathPrefix: '',
            favoritesOnly: facets.favorites,
            limit: 50,
          });
          if (searchRequestRef.current !== requestID) {
            return;
          }
          setSearchResults(results);
          setSearchStatus('ready');
        } catch (err) {
          if (searchRequestRef.current !== requestID) {
            return;
          }
          setSearchResults([]);
          setSearchStatus('error');
          setSearchError(errorMessage(err));
        }
      })();
    }, 220);

    return () => window.clearTimeout(timer);
  }, [facets.favorites, searchText, workspace]);

  const handleDraftChange = useCallback((next: string) => {
    setDraft(next);
    setSaveState(next === savedContent ? 'saved' : 'dirty');
  }, [savedContent]);

  useEffect(() => {
    const suggestionSettings = workspaceSettingsFor(settings, workspace?.root || '').suggestedLinks;
    const requestID = suggestionRequestRef.current + 1;
    suggestionRequestRef.current = requestID;
    if (!isEditing || !selectedID || suggestionSettings.mode === 'off' || suggestionSettings.trigger !== 'whileEditing' || draft.replace(/^---[\s\S]*?---/, '').trim().length < 80) {
      setLinkSuggestions([]);
      setSuggestionsStatus('idle');
      return;
    }
    setSuggestionsStatus('loading');
    const timer = window.setTimeout(() => {
      void SuggestLinks({id: selectedID, content: draft, limit: suggestionSettings.maxSuggestions, minScore: suggestionSettings.minScore})
        .then((response) => {
          if (suggestionRequestRef.current !== requestID) return;
          setLinkSuggestions(response.items);
          setSuggestionsStatus('ready');
        })
        .catch(() => {
          if (suggestionRequestRef.current !== requestID) return;
          setLinkSuggestions([]);
          setSuggestionsStatus('error');
        });
    }, 1000);
    return () => window.clearTimeout(timer);
  }, [draft, isEditing, selectedID, settings, workspace?.root]);

  const addSuggestionsToDraft = useCallback((items: LinkSuggestion[]) => {
    if (items.length === 0) return;
    const next = addRelatedNoteLinks(draft, items);
    setDraft(next);
    setSaveState('dirty');
    setLinkSuggestions((current) => current.filter((item) => !items.some((accepted) => accepted.targetId === item.targetId)));
    showToast(`Added ${items.length} related link${items.length === 1 ? '' : 's'} to draft`);
  }, [draft, showToast]);

  const navigateToNote = useCallback((id: string) => {
    const resolved = resolveLinkedNoteID(id, selectedID, notes);
    if (resolved) {
      setSelectedID(resolved);
      setActiveTab('note');
      setRawMode(false);
      return;
    }
    setError(`No note found for link: ${id}`);
  }, [notes, selectedID]);

  const handleNewTitleChange = useCallback((title: string) => {
    const previousGeneratedID = slugifyNoteID(newNoteTitle);
    const nextGeneratedID = slugifyNoteID(title);
    setNewNoteTitle(title);
    setNewNoteID((current) => current && current !== previousGeneratedID ? current : nextGeneratedID);
  }, [newNoteTitle]);

  const openSearchResult = useCallback((id: string) => {
    setSelectedID(id);
    setActiveTab('note');
    setRawMode(false);
  }, []);

  const selectNote = useCallback((id: string) => {
    setSelectedID(id);
    setActiveTab('note');
    setRawMode(false);
  }, []);

  // Select a node from the graph without leaving the Graph tab (single click).
  const selectGraphNode = useCallback((id: string) => {
    setSelectedID(id);
    setRawMode(false);
  }, []);

  // Record every note selection into the visit history. Jumps triggered by the
  // history controls set suppressHistoryRef so they don't re-append the entry
  // they land on. A new selection while somewhere in the past truncates the
  // forward entries, exactly like a browser.
  useEffect(() => {
    if (!selectedID) {
      return;
    }
    if (suppressHistoryRef.current) {
      suppressHistoryRef.current = false;
      return;
    }
    setNav((prev) => {
      if (prev.stack[prev.index] === selectedID) {
        return prev;
      }
      const truncated = prev.stack.slice(0, prev.index + 1);
      truncated.push(selectedID);
      const trimmed = truncated.slice(-HISTORY_MAX);
      return {stack: trimmed, index: trimmed.length - 1};
    });
  }, [selectedID]);

  const jumpToHistory = useCallback((index: number) => {
    if (index < 0 || index >= nav.stack.length || index === nav.index) {
      return;
    }
    suppressHistoryRef.current = true;
    setNav((prev) => ({...prev, index}));
    setSelectedID(nav.stack[index]);
    setActiveTab('note');
    setRawMode(false);
    setHistoryMenuOpen(false);
  }, [nav]);

  const goBack = useCallback(() => jumpToHistory(nav.index - 1), [jumpToHistory, nav.index]);
  const goForward = useCallback(() => jumpToHistory(nav.index + 1), [jumpToHistory, nav.index]);
  const canGoBack = nav.index > 0;
  const canGoForward = nav.index >= 0 && nav.index < nav.stack.length - 1;

  // Close the recent-notes dropdown on outside click or Escape.
  useEffect(() => {
    if (!historyMenuOpen) {
      return;
    }
    const onPointerDown = (event: PointerEvent) => {
      if (historyNavRef.current && !historyNavRef.current.contains(event.target as Node)) {
        setHistoryMenuOpen(false);
      }
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setHistoryMenuOpen(false);
      }
    };
    window.addEventListener('pointerdown', onPointerDown);
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('pointerdown', onPointerDown);
      window.removeEventListener('keydown', onKey);
    };
  }, [historyMenuOpen]);

  // Close the workspace picker dropdown on outside click or Escape.
  useEffect(() => {
    if (!openWorkspaceMenuOpen) {
      return;
    }
    const onPointerDown = (event: PointerEvent) => {
      if (openWorkspaceMenuRef.current && !openWorkspaceMenuRef.current.contains(event.target as Node)) {
        setOpenWorkspaceMenuOpen(false);
      }
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setOpenWorkspaceMenuOpen(false);
      }
    };
    window.addEventListener('pointerdown', onPointerDown);
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('pointerdown', onPointerDown);
      window.removeEventListener('keydown', onKey);
    };
  }, [openWorkspaceMenuOpen]);

  const cancelEdit = useCallback(() => {
    setDraft(savedContent);
    setSaveState('saved');
    setIsEditing(false);
    setRawMode(false);
    setConflictOpen(false);
  }, [savedContent]);

  // Enter the editor using the saved Note View preference. The source toggle
  // below still switches representations without dropping the draft.
  const startRichEdit = useCallback(() => {
    setActiveTab('note');
    setRawMode(settings.noteView.defaultEditMode === 'source');
    setIsEditing(true);
  }, [settings.noteView.defaultEditMode]);

  // Toggle between rich (WYSIWYG) and raw markdown (CodeMirror) while editing.
  // The draft is shared between both editors (handleDraftChange), so flipping
  // preserves in-progress content. isEditing stays true throughout.
  const toggleSourceMode = useCallback(() => {
    setActiveTab('note');
    setIsEditing(true);
    setRawMode((current) => !current);
  }, []);

  const openPalette = useCallback(() => setPaletteOpen(true), []);

  const selectFromPalette = useCallback((id: string) => {
    setPaletteOpen(false);
    selectNote(id);
  }, [selectNote]);

  // Insert a link to another note via the note picker. Only meaningful while an
  // editor is mounted (source or WYSIWYG). Capture the active editor's current
  // selection *before* opening the modal so it can become the link label — the
  // modal would otherwise steal focus and collapse the selection.
  const openLinkPicker = useCallback(() => {
    if ((!isEditing && !rawMode) || info.readOnly === true) {
      return;
    }
    const handle = rawMode ? codeMirrorRef.current : mdxEditorRef.current;
    setLinkLabelDefault((handle?.getSelectionText() ?? '').trim());
    setLinkPickerOpen(true);
  }, [isEditing, rawMode, info.readOnly]);

  const insertLink = useCallback((note: application.NoteSummaryDTO) => {
    const label = (linkLabelDefault || note.title || basename(note.id)).trim();
    if (rawMode) {
      codeMirrorRef.current?.insertNoteLink(note.id, label);
    } else {
      mdxEditorRef.current?.insertNoteLink(note.id, label);
    }
    setLinkPickerOpen(false);
    setLinkLabelDefault('');
  }, [linkLabelDefault, rawMode]);

  const scrollToAnchor = useCallback((anchor: string) => {
    const container = articleScrollRef.current;
    if (!container) {
      return;
    }
    const el = container.querySelector<HTMLElement>(`[data-anchor="${anchor}"]`);
    if (el) {
      container.scrollTo({top: el.offsetTop - 16, behavior: 'smooth'});
      setActiveAnchor(anchor);
    }
  }, []);

  // Facet options (Types / Tags / Folders) with the number of notes carrying each,
  // ranked by count so the busiest surface first. Derived from all notes so every
  // facet stays selectable even while a filter is active.
  const availableFacets = useMemo(() => {
    const bump = (counts: Map<string, number>, key: string) => counts.set(key, (counts.get(key) ?? 0) + 1);
    const typeCounts = new Map<string, number>();
    const tagCounts = new Map<string, number>();
    const folderCounts = new Map<string, number>();
    for (const note of notes) {
      if (note.type) {
        bump(typeCounts, note.type);
      }
      for (const tag of note.tags || []) {
        bump(tagCounts, tag);
      }
      bump(folderCounts, folderOf(note.path));
    }
    const ranked = (counts: Map<string, number>): FacetOption[] =>
      Array.from(counts, ([value, count]) => ({value, count})).sort(
        (a, b) => b.count - a.count || a.value.localeCompare(b.value),
      );
    return {types: ranked(typeCounts), tags: ranked(tagCounts), folders: ranked(folderCounts)};
  }, [notes]);

  // When any facet is active the note list hides non-matches (user choice); the
  // graph is filtered separately via the same facets prop.
  const facetsActive = anyFacetActive(facets);
  const visibleNotes = useMemo(
    () => (facetsActive ? notes.filter((note) => facetMatchesNote(note, facets)) : notes),
    [notes, facets, facetsActive],
  );
  const matchCount = facetsActive ? visibleNotes.length : notes.length;

  const tree = useMemo(() => groupNotes(visibleNotes), [visibleNotes]);
  const selectedNoteReady = Boolean(selectedNote && selectedNote.id === selectedID);
  const noteSummaryForSelected = notes.find((note) => note.id === selectedID);
  const selectedTags = noteSummaryForSelected?.tags || [];
  const hasSearchQuery = Boolean(searchText.trim());
  // Note IDs of the current search hits, passed to the graph to spotlight them.
  const searchMatchIds = useMemo(() => searchResults.map((result) => result.id), [searchResults]);

  // Mount the graph the first time its tab is opened, then keep it mounted.
  useEffect(() => {
    if (activeTab === 'graph') {
      setGraphMounted(true);
    }
  }, [activeTab]);
  // Mount the 3D view the first time 3D mode is selected on the graph tab, then
  // keep it mounted so its camera survives mode swaps.
  useEffect(() => {
    if (activeTab === 'graph' && graphMode === '3d') {
      setGraph3dMounted(true);
    }
  }, [activeTab, graphMode]);
  // Large graphs auto-fall back to the flat 2D view (3D is disabled): see
  // LARGE_GRAPH_3D_MAX. graphStats.notes reflects the currently rendered node
  // count reported by whichever instance is active.
  const graph3dAllowed = graphStats.notes <= LARGE_GRAPH_3D_MAX;
  useEffect(() => {
    if (!graph3dAllowed && graphMode === '3d') {
      setGraphMode('2d');
    }
  }, [graph3dAllowed, graphMode]);
  const interactionBusy = Boolean(busy) || projectionActive;

  // Parse the current note markdown into the reading-article model.
  const renderContent = saveState === 'dirty' || saveState === 'conflict' ? draft : savedContent;
  const article = useMemo(
    () => parseArticle(renderContent, noteSummaryForSelected?.title || basename(selectedID)),
    [renderContent, noteSummaryForSelected?.title, selectedID],
  );

  // Outgoing wiki-links extracted from the note content (resolved to loaded notes).
  const linkedNotes = useMemo(
    () => extractLinkedNotes(renderContent, selectedID, notes),
    [renderContent, selectedID, notes],
  );

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    document.documentElement.dataset.accent = 'iris';
    try {
      localStorage.setItem('gm-theme', theme);
    } catch {
      // The app-level settings file remains authoritative if browser storage is
      // unavailable; local storage only prevents a default-theme flash.
    }
  }, [theme]);

  // Persist the pane layout preferences.
  useEffect(() => {
    try {
      localStorage.setItem('gm-sidebar-width', String(sidebarWidth));
    } catch {
      // Ignore storage failures (private mode, etc.).
    }
  }, [sidebarWidth]);
  useEffect(() => {
    try {
      localStorage.setItem('gm-rail-collapsed', railCollapsed ? '1' : '0');
    } catch {
      // Ignore storage failures.
    }
  }, [railCollapsed]);
  useEffect(() => {
    try {
      localStorage.setItem('gm-graph-mode', graphMode);
    } catch {
      // Ignore storage failures.
    }
  }, [graphMode]);

  // Drag the seam between the sidebar and main to resize the left pane, clamped
  // to 50%–200% of its base width. Double-click the handle resets it.
  const startSidebarResize = useCallback((event: ReactPointerEvent<HTMLDivElement>) => {
    event.preventDefault();
    const startX = event.clientX;
    const startWidth = sidebarWidth;
    document.body.classList.add('gm-resizing');
    const onMove = (moveEvent: PointerEvent) => {
      setSidebarWidth(clamp(startWidth + (moveEvent.clientX - startX), SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH));
    };
    const onUp = () => {
      document.body.classList.remove('gm-resizing');
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onUp);
    };
    window.addEventListener('pointermove', onMove);
    window.addEventListener('pointerup', onUp);
  }, [sidebarWidth]);

  const resetSidebarWidth = useCallback(() => setSidebarWidth(SIDEBAR_BASE_WIDTH), []);
  const toggleRail = useCallback(() => setRailCollapsed((current) => !current), []);

  useEffect(() => () => {
    if (graphReloadTimerRef.current !== null) {
      clearTimeout(graphReloadTimerRef.current);
    }
    if (toastTimerRef.current !== null) {
      clearTimeout(toastTimerRef.current);
    }
    if (savedFlashTimerRef.current !== null) {
      clearTimeout(savedFlashTimerRef.current);
    }
  }, []);

  // Global ⌘K / Ctrl-K palette toggle, ⌘L / Ctrl-L link picker (while editing) + Esc close.
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault();
        setPaletteOpen((current) => !current);
      } else if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'l') {
        if (isEditing || rawMode) {
          event.preventDefault();
          openLinkPicker();
        }
      } else if (event.key === 'Escape' && (paletteOpen || linkPickerOpen)) {
        setPaletteOpen(false);
        setLinkPickerOpen(false);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [paletteOpen, linkPickerOpen, isEditing, rawMode, openLinkPicker]);

  const toggleTheme = useCallback(() => {
    const nextTheme = themeAppearance(theme) === 'dark' ? 'light' : 'dark';
    persistSettings({...settings, appearance: {...settings.appearance, theme: nextTheme}});
    if (workspace?.root) {
      void SaveUIState({lastWorkspace: workspace.root, lastNote: selectedID, theme: nextTheme}).catch((err) => setError(errorMessage(err)));
    }
  }, [persistSettings, selectedID, settings, theme, workspace?.root]);

  const toggleFolder = useCallback((name: string) => {
    setExpanded((current) => ({...current, [name]: current[name] === false ? true : false}));
  }, []);

  const noteCount = notes.length;
  const wordCount = article.wordCount;
  const readTime = Math.max(1, Math.round(wordCount / 200));
  const modified = noteSummaryForSelected ? relativeTime(noteSummaryForSelected.modifiedAt) : '';
  const breadcrumbFolder = selectedID.includes('/') ? selectedID.slice(0, selectedID.lastIndexOf('/')) : '';
  const fileNameShort = basename(selectedID);
  const dirty = saveState === 'dirty' || saveState === 'conflict';
  const currentWorkspaceSettings = workspace?.root ? workspaceSettingsFor(settings, workspace.root) : defaultWorkspaceSettings();
  const workspaceReadOnly = Boolean(workspace && currentWorkspaceSettings.accessMode !== 'editable' && currentWorkspaceSettings.accessMode !== 'writableGit');
  const readOnly = info.readOnly === true || workspaceReadOnly;
  const showSaveBar = Boolean(selectedNote) && !readOnly;
  const git = info.git ?? null;
  const writableGit = currentWorkspaceSettings.accessMode === 'writableGit' || info.mode === 'writable-git';
  const readOnlyBannerText = info.readOnly === true
    ? 'Read-only — content is managed in git.'
    : currentWorkspaceSettings.accessMode === 'readOnlyGit'
      ? 'Read-only — workspace is configured as git connected.'
      : 'Read-only — workspace is configured local read-only.';
  const authoringDisabledTitle = readOnly ? readOnlyBannerText : undefined;
  const workspaceNoteTemplateOptions = noteTypes.filter((template) => currentWorkspaceSettings.enabledTypes.includes(template.id));
  const enabledNoteTemplateOptions = workspaceNoteTemplateOptions.length > 0 ? workspaceNoteTemplateOptions : noteTypes;
  // Server and viewer modes pin the workspace to a configured root, so the
  // header "Open" affordance (which would pick a different folder) does not
  // apply — the picker is a no-op in server mode and reopens the same root in
  // viewer mode. Hide it to avoid the dead/wandering control.
  const workspacePinned = info.mode === 'server' || info.mode === 'viewer';

  useEffect(() => {
    if (!writableGit || currentWorkspaceSettings.gitExitAction !== 'prompt') {
      return;
    }
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = '';
    };
    window.addEventListener('beforeunload', onBeforeUnload);
    return () => window.removeEventListener('beforeunload', onBeforeUnload);
  }, [currentWorkspaceSettings.gitExitAction, writableGit]);

  useEffect(() => {
    if (!workspace) {
      return;
    }
    if (!enabledNoteTemplateOptions.some((template) => template.id === newNoteTemplate)) {
      setNewNoteTemplate(currentWorkspaceSettings.defaultType || enabledNoteTemplateOptions[0].id);
    }
  }, [currentWorkspaceSettings.defaultType, enabledNoteTemplateOptions, newNoteTemplate, workspace]);

  return (
    <div className="gm-shell" data-theme={themeAppearance(theme)}>
      {/* ============================ HEADER ============================ */}
      <header className="gm-header">
        <div className="gm-brand">
          <AppMark size={30} />
          <div className="gm-brand-text">
            <Wordmark className="gm-brand-name" />
            <span className="gm-brand-path">{workspace ? workspace.root : info.description}</span>
          </div>
        </div>

        <button type="button" className="gm-palette-trigger" onClick={openPalette}>
          <SearchIcon size={15} />
          <span className="gm-palette-trigger-label">Jump to anything…</span>
          <kbd className="gm-kbd">⌘K</kbd>
        </button>

        <div className="gm-header-spacer" />

        {git && !writableGit && (
          <button
            type="button"
            className={git.lastError ? 'gm-git-chip gm-git-chip-error' : 'gm-git-chip'}
            title={`${gitChipTitle(git)}\nClick to pull latest`}
            onClick={pullGit}
            disabled={git.syncing}
          >
            <span className={git.syncing ? 'gm-git-dot gm-git-dot-syncing' : 'gm-git-dot'} />
            {git.syncing && git.operation ? (
              <span className="gm-git-operation">{git.operation}</span>
            ) : (
              <>
                <span className="gm-git-ref">{git.ref}</span>
                <span className="gm-git-commit">{shortCommit(git.commit)}</span>
              </>
            )}
          </button>
        )}
        {git && writableGit && (
          <div className="gm-git-actions" title={gitChipTitle(git)}>
            <button
              type="button"
              className={git.lastError ? 'gm-git-chip gm-git-chip-error' : 'gm-git-chip'}
              onClick={refreshInfo}
              disabled={git.syncing}
            >
              <span className={git.syncing ? 'gm-git-dot gm-git-dot-syncing' : 'gm-git-dot'} />
              {git.syncing && git.operation ? (
                <span className="gm-git-operation">{git.operation}</span>
              ) : (
                <>
                  <span className="gm-git-ref">{git.branch || git.ref}</span>
                  <span className="gm-git-commit">{shortCommit(git.commit)}</span>
                  {typeof git.ahead === 'number' && git.ahead > 0 && <span className="gm-git-ahead">+{git.ahead}</span>}
                </>
              )}
            </button>
            <button type="button" className="gm-btn gm-btn-ghost gm-btn-sm" onClick={openGitPr} disabled={git.syncing}>
              PR
            </button>
            <button type="button" className="gm-btn gm-btn-ghost gm-btn-sm" onClick={mergeGitPr} disabled={git.syncing}>
              Merge
            </button>
          </div>
        )}

        {!workspacePinned && (
          <div className="gm-open-menu-wrap" ref={openWorkspaceMenuRef}>
            <button
              type="button"
              className="gm-btn gm-btn-ghost gm-open-menu-trigger"
              onClick={() => {
                setOpenWorkspaceMenuOpen((open) => !open);
                void loadRecent().catch(() => {});
              }}
              disabled={interactionBusy}
              aria-haspopup="menu"
              aria-expanded={openWorkspaceMenuOpen}
            >
              <FolderIcon size={15} />
              Open
              <ChevronIcon size={13} className={openWorkspaceMenuOpen ? 'gm-open-menu-chevron open' : 'gm-open-menu-chevron'} />
            </button>
            {openWorkspaceMenuOpen && (
              <div className="gm-open-menu" role="menu" aria-label="Open workspace">
                {recent.slice(0, 6).length > 0 ? (
                  <>
                    <div className="gm-open-menu-label">Recent workspaces</div>
                    {recent.slice(0, 6).map((item) => (
                      <button
                        type="button"
                        className="gm-open-menu-item"
                        role="menuitem"
                        key={item.path}
                        title={item.path}
                        onClick={() => {
                          setOpenWorkspaceMenuOpen(false);
                          void openWorkspace(item.path);
                        }}
                      >
                        <span className="gm-open-menu-name">{basename(item.path)}</span>
                        <span className="gm-open-menu-path">{item.path}</span>
                      </button>
                    ))}
                    <div className="gm-open-menu-separator" />
                  </>
                ) : (
                  <div className="gm-open-menu-empty">No recent workspaces</div>
                )}
                <button
                  type="button"
                  className="gm-open-menu-item gm-open-menu-browse"
                  role="menuitem"
                  onClick={() => {
                    setOpenWorkspaceMenuOpen(false);
                    void chooseWorkspace();
                  }}
                >
                  <FolderIcon size={15} />
                  <span>Browse…</span>
                </button>
              </div>
            )}
          </div>
        )}
        <button type="button" className="gm-btn gm-btn-ghost" onClick={rebuildWorkspace} disabled={!workspace || interactionBusy}>
          <span className={projectionActive ? 'gm-spin' : ''}><RefreshIcon size={15} /></span>
          {projectionActive ? 'Rebuilding…' : 'Rebuild index'}
        </button>
        <button type="button" className="gm-btn gm-btn-icon" onClick={toggleTheme} title="Toggle theme">
          {themeAppearance(theme) === 'dark' ? <SunIcon size={17} /> : <MoonIcon size={17} />}
        </button>
        <button type="button" className="gm-btn gm-btn-icon" onClick={() => setSettingsOpen(true)} title="Settings" aria-label="Settings">
          <GearIcon size={17} />
        </button>
      </header>

      {error && (
        <div className="gm-error-strip" role="alert">
          <span>{error}</span>
          <div className="gm-error-actions">
            {workspace && <button type="button" onClick={rebuildWorkspace} disabled={interactionBusy}>Rebuild</button>}
            <button type="button" onClick={() => setError('')}>Dismiss</button>
          </div>
        </div>
      )}

      {readOnly && (
        <div className="gm-readonly-banner" role="status">
          <span>{readOnlyBannerText}</span>
        </div>
      )}

      <div
        className={railCollapsed ? 'gm-body gm-rail-collapsed' : 'gm-body'}
        style={{'--gm-sidebar-w': `${sidebarWidth}px`} as CSSProperties}
      >
        {/* ============================ SIDEBAR ============================ */}
        <aside className="gm-sidebar">
          <div className="gm-sidebar-head">
            <div className="gm-sidebar-head-row">
              <div className="gm-notes-label">
                <span className="gm-section-title">Notes</span>
                <span className="gm-notes-count">{noteCount}</span>
              </div>
              <div className="gm-sidebar-actions">
                <button
                  type="button"
                  className="gm-btn gm-btn-primary gm-btn-sm"
                  onClick={() => {
                    setImportOpen(false);
                    setNewNoteTemplate(currentWorkspaceSettings.defaultType || enabledNoteTemplateOptions[0].id);
                    setNewNoteOpen((open) => !open);
                  }}
                  disabled={!workspace || interactionBusy || readOnly}
                  title={authoringDisabledTitle}
                >
                  <PlusIcon size={14} />New
                </button>
                <button
                  type="button"
                  className="gm-btn gm-btn-icon gm-btn-sm"
                  title={authoringDisabledTitle ?? 'Import from URL'}
                  onClick={() => { setNewNoteOpen(false); setImportOpen((open) => !open); }}
                  disabled={!workspace || interactionBusy || readOnly}
                >
                  <ImportIcon size={15} />
                </button>
              </div>
            </div>

            <div className="gm-search-field">
              <SearchIcon size={16} className="gm-search-field-icon" />
              <input
                className="gm-search-input"
                value={searchText}
                onChange={(event) => setSearchText(event.target.value)}
                placeholder="Search notes"
              />
              {hasSearchQuery && (
                <button type="button" className="gm-search-clear" onClick={() => setSearchText('')} aria-label="Clear search">
                  <CloseIcon size={14} />
                </button>
              )}
            </div>

            {workspace && newNoteOpen && (
              <form className="gm-inline-form" onSubmit={(event) => { event.preventDefault(); void createNote(); }}>
                <label>
                  <span>Note Type</span>
                  <select value={newNoteTemplate} onChange={(event) => setNewNoteTemplate(event.target.value)} autoFocus>
                    {enabledNoteTemplateOptions.map((template) => (
                      <option key={template.id} value={template.id}>{template.label}</option>
                    ))}
                  </select>
                </label>
                <label>
                  <span>Title</span>
                  <input value={newNoteTitle} onChange={(event) => handleNewTitleChange(event.target.value)} />
                </label>
                <label>
                  <span>Note ID</span>
                  <input value={newNoteID} onChange={(event) => setNewNoteID(event.target.value)} placeholder="folder/note-name" />
                </label>
                <div className="gm-inline-form-actions">
                  <button type="button" className="gm-btn gm-btn-ghost gm-btn-sm" onClick={() => setNewNoteOpen(false)}>Cancel</button>
                  <button type="submit" className="gm-btn gm-btn-primary gm-btn-sm" disabled={interactionBusy}>Create</button>
                </div>
              </form>
            )}
            {workspace && importOpen && (
              <form className="gm-inline-form" onSubmit={(event) => { event.preventDefault(); void importFromURL(); }}>
                <label>
                  <span>URL</span>
                  <input value={importURL} onChange={(event) => setImportURL(event.target.value)} placeholder="https://example.com/recipe" autoFocus />
                </label>
                <div className="gm-inline-form-actions">
                  <button type="button" className="gm-btn gm-btn-ghost gm-btn-sm" onClick={() => setImportOpen(false)}>Cancel</button>
                  <button type="submit" className="gm-btn gm-btn-primary gm-btn-sm" disabled={interactionBusy}>Import</button>
                </div>
              </form>
            )}
          </div>

          <div className="gm-sidebar-body scroll">
            {!workspace ? (
              <RecentWorkspaceList recent={recent} disabled={interactionBusy} onOpen={(path) => void openWorkspace(path)} />
            ) : hasSearchQuery ? (
              <SearchResultsList
                results={searchResults}
                status={searchStatus}
                query={searchText}
                error={searchError}
                onOpen={openSearchResult}
                onToggleFavorite={toggleNoteFavorite}
              />
            ) : (
              <SidebarNoteTree
                tree={tree}
                expanded={expanded}
                selectedID={selectedID}
                activeTab={activeTab}
                onSelectNote={selectNote}
                onToggleFolder={toggleFolder}
                onToggleFavorite={toggleNoteFavorite}
                onMoveNote={moveNoteToFolder}
                moveDisabled={readOnly || interactionBusy}
              />
            )}
          </div>

          <div className="gm-sidebar-foot">
            <span className="gm-status-dot" />
            <span className="gm-status-text">{projectionActive ? 'Rebuilding index…' : 'Search index ready'}</span>
          </div>

          <div
            className="gm-resizer"
            onPointerDown={startSidebarResize}
            onDoubleClick={resetSidebarWidth}
            role="separator"
            aria-orientation="vertical"
            aria-label="Resize sidebar (double-click to reset)"
            title="Drag to resize · double-click to reset"
          />
        </aside>

        {/* ============================ MAIN ============================ */}
        <main className="gm-main">
          <div className="gm-subheader">
            <div className="gm-subheader-top">
              <div className="gm-subheader-left">
              {workspace && (
                <div className="gm-nav-history" ref={historyNavRef}>
                  <button
                    type="button"
                    className="gm-nav-btn"
                    onClick={goBack}
                    disabled={!canGoBack}
                    title="Back"
                    aria-label="Back"
                  >
                    <ChevronIcon size={15} style={{transform: 'rotate(180deg)'}} />
                  </button>
                  <button
                    type="button"
                    className="gm-nav-btn"
                    onClick={goForward}
                    disabled={!canGoForward}
                    title="Forward"
                    aria-label="Forward"
                  >
                    <ChevronIcon size={15} />
                  </button>
                  <button
                    type="button"
                    className="gm-nav-btn gm-nav-menu-btn"
                    onClick={() => setHistoryMenuOpen((open) => !open)}
                    disabled={nav.stack.length === 0}
                    title="Recent notes"
                    aria-label="Recent notes"
                    aria-expanded={historyMenuOpen}
                  >
                    <ChevronIcon size={13} style={{transform: 'rotate(90deg)'}} />
                  </button>
                  {historyMenuOpen && nav.stack.length > 0 && (
                    <div className="gm-history-menu" role="menu">
                      {nav.stack
                        .map((id, index) => ({id, index}))
                        .reverse()
                        .map(({id, index}) => (
                          <button
                            type="button"
                            key={`${id}-${index}`}
                            role="menuitem"
                            className={index === nav.index ? 'gm-history-item active' : 'gm-history-item'}
                            onClick={() => jumpToHistory(index)}
                          >
                            {titleForNoteID(id, notes)}
                          </button>
                        ))}
                    </div>
                  )}
                </div>
              )}
              <div className="gm-breadcrumb">
                <span>GoMental</span>
                {breadcrumbFolder && (
                  <>
                    <span className="gm-breadcrumb-sep">/</span>
                    <span>{breadcrumbFolder}</span>
                  </>
                )}
                <span className="gm-breadcrumb-sep">/</span>
                <span className="gm-breadcrumb-file">{fileNameShort || 'No note selected'}</span>
              </div>
              </div>
              {showSaveBar && (
                <div className="gm-subheader-actions">
                  {isEditing ? (
                    <>
                      <button
                        type="button"
                        className={rawMode ? 'gm-btn gm-btn-toggle active' : 'gm-btn gm-btn-toggle'}
                        onClick={toggleSourceMode}
                        aria-pressed={rawMode}
                        title={rawMode ? 'Back to rich text' : 'Edit markdown source'}
                      >
                        <CodeIcon size={15} />Edit source
                      </button>
                      <button
                        type="button"
                        className="gm-btn gm-btn-ghost"
                        onClick={openLinkPicker}
                        title="Link to another note (⌘L)"
                      >
                        <LinkIcon size={15} />Link
                      </button>
                      <button type="button" className="gm-btn gm-btn-ghost" onClick={cancelEdit}>
                        Cancel
                      </button>
                      <button
                        type="button"
                        className="gm-btn gm-btn-danger"
                        onClick={() => void deleteCurrentNote()}
                        disabled={saveState === 'saving' || interactionBusy}
                        title="Delete this note"
                      >
                        <TrashIcon size={15} />Delete
                      </button>
                      <button
                        type="button"
                        className="gm-btn gm-btn-primary"
                        onClick={() => void saveCurrentNote(true)}
                        disabled={saveState === 'saving'}
                      >
                        <SaveIcon size={15} />{savedFlash ? 'Saved' : saveState === 'saving' ? 'Saving…' : 'Save'}
                      </button>
                    </>
                  ) : (
                    <button
                      type="button"
                      className="gm-btn gm-btn-toggle"
                      onClick={startRichEdit}
                      title="Edit this note"
                    >
                      <EditIcon size={15} />Edit
                    </button>
                  )}
                </div>
              )}
            </div>
            <div className="gm-subheader-bottom">
              <div className="gm-tabs">
                <button
                  type="button"
                  className={activeTab === 'note' ? 'gm-tab active' : 'gm-tab'}
                  onClick={() => { setActiveTab('note'); }}
                >
                  <NoteTabIcon size={15} />Note
                </button>
                <button
                  type="button"
                  className={activeTab === 'graph' ? 'gm-tab active' : 'gm-tab'}
                  onClick={() => setActiveTab('graph')}
                >
                  <GraphTabIcon size={15} />Graph
                </button>
              </div>
              {activeTab === 'graph' && (
                <div className="gm-graph-mode" role="group" aria-label="Graph dimension">
                  <button
                    type="button"
                    className={graphMode === '2d' ? 'gm-graph-mode-btn active' : 'gm-graph-mode-btn'}
                    onClick={() => setGraphMode('2d')}
                    aria-pressed={graphMode === '2d'}
                  >
                    2D
                  </button>
                  <button
                    type="button"
                    className={graphMode === '3d' ? 'gm-graph-mode-btn active' : 'gm-graph-mode-btn'}
                    onClick={() => setGraphMode('3d')}
                    aria-pressed={graphMode === '3d'}
                    disabled={!graph3dAllowed}
                    title={graph3dAllowed ? '3D orbit view' : `3D is disabled above ${LARGE_GRAPH_3D_MAX} nodes — showing the flat 2D view`}
                  >
                    3D
                  </button>
                </div>
              )}
              {selectedNote && activeTab === 'note' && (
                <div className="gm-meta">
                  <span className="gm-meta-item"><ClockIcon size={13} />Edited {modified || 'recently'}</span>
                  <span>{wordCount} words · {readTime} min read</span>
                </div>
              )}
            </div>
          </div>

          {deletedNotice && (
            <div className="gm-notice" role="status">
              <span>{deletedNotice}</span>
              <button type="button" onClick={() => setDeletedNotice('')}>Dismiss</button>
            </div>
          )}
          {conflictOpen && (
            <div className="gm-conflict" role="alert">
              <span>This note was changed elsewhere. Reload the server copy, overwrite it with your version, or keep editing.</span>
              <div className="gm-conflict-actions">
                <button type="button" onClick={() => void reloadFromServer()}>Reload</button>
                <button type="button" onClick={overwriteConflict}>Overwrite</button>
                <button type="button" onClick={dismissConflict}>Dismiss</button>
              </div>
            </div>
          )}

          {graphMounted && (
            <div className="gm-graph-wrap" style={activeTab === 'graph' && graphMode === '2d' ? undefined : {display: 'none'}}>
              <Suspense fallback={<div className="gm-empty"><h2>Loading graph…</h2></div>}>
                <GraphView3D
                  flat
                  workspaceOpen={Boolean(workspace)}
                  notes={notes}
                  facets={facets}
                  onFacetsChange={setFacets}
                  selectedID={selectedID}
                  refreshKey={graphRevision}
                  theme={themeAppearance(theme)}
                  active={activeTab === 'graph' && graphMode === '2d'}
                  depth={graphDepth}
                  onDepthChange={setGraphDepth}
                  searchMatchIds={searchMatchIds}
                  searchActive={hasSearchQuery}
                  onSelectNote={selectGraphNode}
                  onOpenNote={selectNote}
                  onError={setError}
                  onStats={setGraphStats}
                />
              </Suspense>
            </div>
          )}
          {graph3dMounted && (
            <div className="gm-graph-wrap" style={activeTab === 'graph' && graphMode === '3d' ? undefined : {display: 'none'}}>
              <Suspense fallback={<div className="gm-empty"><h2>Loading 3D graph…</h2></div>}>
                <GraphView3D
                  workspaceOpen={Boolean(workspace)}
                  notes={notes}
                  facets={facets}
                  onFacetsChange={setFacets}
                  selectedID={selectedID}
                  refreshKey={graphRevision}
                  theme={themeAppearance(theme)}
                  active={activeTab === 'graph' && graphMode === '3d'}
                  depth={graphDepth}
                  onDepthChange={setGraphDepth}
                  searchMatchIds={searchMatchIds}
                  searchActive={hasSearchQuery}
                  onSelectNote={selectGraphNode}
                  onOpenNote={selectNote}
                  onError={setError}
                  onStats={setGraphStats}
                />
              </Suspense>
            </div>
          )}
          {activeTab === 'graph' ? null : rawMode && selectedNoteReady && selectedNote ? (
            <div className="gm-source-scroll scroll">
              <div className="gm-source-wrap">
                <div className="gm-source-card">
                  <div className="gm-source-titlebar">
                    <span className="gm-source-filename">{fileNameShort}.md</span>
                  </div>
                  <div className="gm-source-editor">
                    <Suspense fallback={<div className="gm-empty"><h2>Loading editor…</h2></div>}>
                      <CodeMirrorEditor
                        ref={codeMirrorRef}
                        value={draft}
                        notes={notes}
                        theme={themeAppearance(theme)}
                        onChange={handleDraftChange}
                        onSave={() => saveCurrentNote(true)}
                        onNavigate={navigateToNote}
                        onRequestLink={openLinkPicker}
                        onSaveImage={saveImageAsset}
                      />
                    </Suspense>
                  </div>
                </div>
              </div>
            </div>
          ) : isEditing && selectedNoteReady && selectedNote ? (
            <div className="gm-article-scroll scroll" ref={articleScrollRef}>
              <div className="gm-article-editor-wrap">
                <Suspense fallback={<div className="gm-empty"><h2>Loading editor…</h2></div>}>
                  <MdxNoteEditor
                    ref={mdxEditorRef}
                    noteID={selectedID}
                    content={renderContent}
                    theme={themeAppearance(theme)}
                    onNavigate={navigateToNote}
                    onChange={handleDraftChange}
                    onSaveImage={saveEditorImage}
                    onRequestLink={openLinkPicker}
                  />
                </Suspense>
              </div>
            </div>
          ) : selectedNoteReady && selectedNote ? (
            <div className="gm-article-scroll scroll" ref={articleScrollRef}>
              {settings.noteView.showFindBar && <FindBar containerRef={articleScrollRef} contentKey={selectedID} />}
              <MarkdownArticle model={article} tags={selectedTags} noteID={selectedID} onNavigate={navigateToNote} theme={themeAppearance(theme)} />
            </div>
          ) : workspace && selectedID ? (
            <div className="gm-empty">
              <h2>Loading note</h2>
              <p className="gm-mono">{selectedID}</p>
            </div>
          ) : (
            <div className="gm-empty">
              <h2>Open a workspace</h2>
              <p>Select a local folder containing OKF Markdown concept documents.</p>
              <button type="button" className="gm-btn gm-btn-primary" onClick={chooseWorkspace} disabled={interactionBusy}>Open Workspace</button>
              <RecentWorkspaceList recent={recent} disabled={interactionBusy} onOpen={(path) => void openWorkspace(path)} variant="main" />
            </div>
          )}
        </main>

        {/* ============================ RIGHT RAIL ============================ */}
        <aside className="gm-rail scroll">
          {workspace && (
            <div className="gm-rail-block">
              <div className="gm-section-title gm-rail-title">Filters</div>
              <FacetFilters
                available={availableFacets}
                facets={facets}
                matchCount={matchCount}
                totalCount={noteCount}
                onChange={setFacets}
              />
            </div>
          )}
          {activeTab === 'graph' ? (
            <>
              <div className="gm-section-title gm-rail-title">Graph</div>
              <div className="gm-stat-cards">
                <div className="gm-stat-card">
                  <div className="gm-stat-value">{graphStats.notes || noteCount}</div>
                  <div className="gm-stat-label">Notes</div>
                </div>
                <div className="gm-stat-card">
                  <div className="gm-stat-value">{graphStats.links}</div>
                  <div className="gm-stat-label">Links</div>
                </div>
              </div>
            </>
          ) : (
            <>
              <div className="gm-rail-block">
                <div className="gm-section-title gm-rail-title">On this page</div>
                {article.outline.length > 0 ? (
                  <div className="gm-outline">
                    {article.outline.map((entry: OutlineEntry) => (
                      <button
                        type="button"
                        key={entry.anchor}
                        className={activeAnchor === entry.anchor ? 'gm-outline-link active' : 'gm-outline-link'}
                        onClick={() => scrollToAnchor(entry.anchor)}
                      >
                        {entry.text}
                      </button>
                    ))}
                  </div>
                ) : (
                  <div className="gm-rail-empty">No sections yet.</div>
                )}
              </div>

              <div className="gm-rail-block">
                <div className="gm-section-title gm-rail-title">Details</div>
                <div className="gm-details">
                  <DetailRow label="Path" value={selectedNote?.path || selectedID || '—'} />
                  <DetailRow label="Format" value="Markdown" />
                  <DetailRow label="Modified" value={modified || '—'} />
                  <DetailRow label="Words" value={String(wordCount)} />
                  <DetailRow label="Links" value={String(linkedNotes.length)} />
                  <DetailRow label="Backlinks" value={String(backlinks.length)} />
                </div>
              </div>

              <div className="gm-rail-block">
                <div className="gm-rail-heading">
                  <span className="gm-section-title">Linked notes</span>
                  <span className="gm-pill">{linkedNotes.length}</span>
                </div>
                {linkedNotes.length > 0 ? (
                  <div className="gm-linked">
                    {linkedNotes.map((link) => (
                      <button type="button" key={link.id} className="gm-linked-row" onClick={() => selectNote(link.id)}>
                        <LinkIcon size={14} stroke="var(--accent-text)" />
                        <span className="gm-linked-title">{link.title}</span>
                      </button>
                    ))}
                  </div>
                ) : (
                  <div className="gm-rail-empty">No outgoing links.</div>
                )}
              </div>

              {isEditing && suggestionsStatus !== 'idle' && (
                <div className="gm-rail-block">
                  <div className="gm-rail-heading">
                    <span className="gm-section-title">Suggested links</span>
                    <span className="gm-pill">{linkSuggestions.length}</span>
                  </div>
                  {suggestionsStatus === 'loading' ? (
                    <div className="gm-rail-empty">Finding related notes…</div>
                  ) : suggestionsStatus === 'error' ? (
                    <div className="gm-rail-empty">Suggestions unavailable.</div>
                  ) : linkSuggestions.length > 0 ? (
                    <div className="gm-suggestions">
                      {linkSuggestions.map((suggestion) => (
                        <div className="gm-suggestion-card" key={suggestion.targetId}>
                          <div className="gm-suggestion-preview">
                            <strong>{suggestion.targetTitle || basename(suggestion.targetId)}</strong>
                            <span>{suggestionConfidenceLabel(suggestion)}</span>
                          </div>
                          <small>{suggestionEvidenceLabel(suggestion)}</small>
                          <div className="gm-suggestion-actions">
                            <button type="button" className="gm-btn gm-btn-sm" onClick={() => setLinkSuggestions((current) => current.filter((item) => item.targetId !== suggestion.targetId))}>Reject</button>
                            <button type="button" className="gm-btn gm-btn-sm gm-btn-primary" onClick={() => addSuggestionsToDraft([suggestion])}>Add</button>
                          </div>
                        </div>
                      ))}
                      <button type="button" className="gm-btn gm-btn-primary gm-suggestion-add-all" onClick={() => addSuggestionsToDraft(linkSuggestions)}>Add all</button>
                    </div>
                  ) : (
                    <div className="gm-rail-empty">No related notes found.</div>
                  )}
                </div>
              )}

              <div className="gm-rail-block">
                <div className="gm-rail-heading">
                  <span className="gm-section-title">Backlinks</span>
                  <span className="gm-pill">{backlinks.length}</span>
                </div>
                {backlinks.length > 0 ? (
                  <div className="gm-backlinks">
                    {backlinks.map((link) => (
                      <button
                        type="button"
                        key={`${link.source}-${link.target}`}
                        className="gm-backlink-card"
                        onClick={() => selectNote(link.source)}
                      >
                        <div className="gm-backlink-title">{titleForNoteID(link.source, notes)}</div>
                        {link.displayText && <div className="gm-backlink-context">{link.displayText}</div>}
                      </button>
                    ))}
                  </div>
                ) : (
                  <div className="gm-backlink-empty">
                    <LinkIcon size={22} stroke="var(--text-3)" />
                    <span>No other note links here yet.</span>
                  </div>
                )}
              </div>
            </>
          )}
        </aside>

        <button
          type="button"
          className="gm-rail-toggle"
          onClick={toggleRail}
          title={railCollapsed ? 'Show details panel' : 'Hide details panel'}
          aria-label={railCollapsed ? 'Show details panel' : 'Hide details panel'}
          aria-expanded={!railCollapsed}
        >
          <ChevronIcon size={14} />
        </button>
      </div>

      <CommandPalette open={paletteOpen} notes={notes} onClose={() => setPaletteOpen(false)} onSelect={selectFromPalette} />
      <LinkPicker open={linkPickerOpen} notes={notes} onClose={() => setLinkPickerOpen(false)} onPick={insertLink} />
      {saveSuggestionReview && (
        <SuggestedLinksReview
          review={saveSuggestionReview}
          onCancel={() => setSaveSuggestionReview(null)}
          onSaveWithout={() => {
            reviewedSuggestionDraftRef.current = saveSuggestionReview.content;
            const pending = saveSuggestionReview;
            setSaveSuggestionReview(null);
            void saveCurrentNote(pending.exitEditMode, false, pending.content);
          }}
          onApply={(items) => {
            const pending = saveSuggestionReview;
            const next = addRelatedNoteLinks(pending.content, items);
            reviewedSuggestionDraftRef.current = next;
            setDraft(next);
            setSaveSuggestionReview(null);
            void saveCurrentNote(pending.exitEditMode, false, next);
          }}
        />
      )}
      <SettingsModal
        open={settingsOpen}
        settings={settings}
        recent={recent}
        currentWorkspace={workspace?.root || ''}
        noteTypes={noteTypes}
        activeSection={settingsSection}
        saveState={settingsSaveState}
        onClose={() => setSettingsOpen(false)}
        onSectionChange={setSettingsSection}
        onBrowseWorkspace={async () => {
          const path = await SelectWorkspaceDirectory();
          if (path) {
            void loadRecent().catch(() => {});
          }
          return path;
        }}
        onChange={persistSettings}
        onSaveNoteType={async (definition) => {
          let saved: NoteType;
          try {
            saved = await SaveNoteType(definition);
          } catch (err) {
            setError(errorMessage(err));
            throw err;
          }
          setNoteTypes((current) => [...current.filter((item) => item.id !== saved.id), saved].sort((a, b) => a.label.localeCompare(b.label)));
          if (workspace?.root) {
            const workspaceSettings = workspaceSettingsFor(settings, workspace.root);
            persistSettings({...settings, workspaces: {...settings.workspaces, [workspace.root]: {...workspaceSettings, enabledTypes: ensureEnabledType(workspaceSettings.enabledTypes, saved.id)}}});
          }
          showToast(`Saved ${saved.label} Note Type`);
        }}
        onDeleteNoteType={async (id) => {
          try {
            await DeleteNoteType(id);
          } catch (err) {
            setError(errorMessage(err));
            throw err;
          }
          setNoteTypes((current) => current.filter((item) => item.id !== id));
          setNotes((current) => current.map((note) => note.type === id ? {...note, type: 'general'} : note));
          if (workspace?.root) {
            const workspaceSettings = workspaceSettingsFor(settings, workspace.root);
            const enabledTypes = workspaceSettings.enabledTypes.filter((type) => type !== id);
            persistSettings({...settings, workspaces: {...settings.workspaces, [workspace.root]: {...workspaceSettings, enabledTypes, defaultType: workspaceSettings.defaultType === id ? (enabledTypes[0] || '') : workspaceSettings.defaultType}}});
          }
          showToast('Note Type removed');
        }}
        onImportCollection={async (content) => {
          const imported = await ImportNoteTypeCollection(content);
          setNoteTypes((current) => [...current.filter((item) => !imported.some((saved) => saved.id === item.id)), ...imported].sort((a, b) => a.label.localeCompare(b.label)));
          if (workspace?.root) {
            const workspaceSettings = workspaceSettingsFor(settings, workspace.root);
            persistSettings({...settings, workspaces: {...settings.workspaces, [workspace.root]: {...workspaceSettings, enabledTypes: Array.from(new Set([...workspaceSettings.enabledTypes, ...imported.map((type) => type.id)]))}}});
          }
          showToast(`Imported ${imported.length} Note Type${imported.length === 1 ? '' : 's'}`);
        }}
      />
      <Toast message={toastMsg} />
    </div>
  );
}

// ---- Small presentational helpers ---------------------------------------

function DetailRow({label, value}: {label: string; value: string}) {
  return (
    <div className="gm-detail-row">
      <span className="gm-detail-label">{label}</span>
      <span className="gm-detail-value">{value}</span>
    </div>
  );
}

function SuggestedLinksReview({
  review,
  onCancel,
  onSaveWithout,
  onApply,
}: {
  review: {content: string; items: LinkSuggestion[]; exitEditMode: boolean};
  onCancel: () => void;
  onSaveWithout: () => void;
  onApply: (items: LinkSuggestion[]) => void;
}) {
  const [selected, setSelected] = useState(() => new Set(review.items.map((item) => item.targetId)));
  const accepted = review.items.filter((item) => selected.has(item.targetId));
  return (
    <div className="gm-settings-scrim" role="presentation" onClick={onCancel}>
      <div className="gm-suggestion-review" role="dialog" aria-modal="true" aria-label="Review suggested links" onClick={(event) => event.stopPropagation()}>
        <div className="gm-settings-header">
          <div><h2>Suggested links</h2><p>Choose which related notes to add before saving.</p></div>
          <button type="button" className="gm-icon-btn" onClick={onCancel} aria-label="Cancel"><CloseIcon size={18} /></button>
        </div>
        <div className="gm-suggestion-review-list">
          {review.items.map((suggestion) => (
            <label className="gm-suggestion-review-row" key={suggestion.targetId}>
              <input
                type="checkbox"
                checked={selected.has(suggestion.targetId)}
                onChange={(event) => setSelected((current) => {
                  const next = new Set(current);
                  if (event.target.checked) next.add(suggestion.targetId); else next.delete(suggestion.targetId);
                  return next;
                })}
              />
              <span><strong>{suggestion.targetTitle || basename(suggestion.targetId)}</strong><small>{suggestionEvidenceLabel(suggestion)}</small></span>
              <em>{suggestionConfidenceLabel(suggestion)}</em>
            </label>
          ))}
        </div>
        <div className="gm-suggestion-review-actions">
          <button type="button" className="gm-btn" onClick={onCancel}>Cancel</button>
          <button type="button" className="gm-btn" onClick={onSaveWithout}>Save without links</button>
          <button type="button" className="gm-btn gm-btn-primary" disabled={accepted.length === 0} onClick={() => onApply(accepted)}>Add selected &amp; save ({accepted.length})</button>
        </div>
      </div>
    </div>
  );
}

function SettingsModal({
  open,
  settings,
  recent,
  currentWorkspace,
  noteTypes,
  activeSection,
  saveState,
  onClose,
  onSectionChange,
  onBrowseWorkspace,
  onChange,
  onSaveNoteType,
  onDeleteNoteType,
  onImportCollection,
}: {
  open: boolean;
  settings: GoMentalSettings;
  recent: application.RecentWorkspaceDTO[];
  currentWorkspace: string;
  noteTypes: NoteType[];
  activeSection: SettingsSection;
  saveState: 'idle' | 'saving' | 'saved' | 'error';
  onClose: () => void;
  onSectionChange: (section: SettingsSection) => void;
  onBrowseWorkspace: () => Promise<string>;
  onChange: (settings: GoMentalSettings) => void;
  onSaveNoteType: (definition: NoteType) => Promise<void>;
  onDeleteNoteType: (id: string) => Promise<void>;
  onImportCollection: (content: string) => Promise<void>;
}) {
  const workspacePaths = useMemo(
    () => knownWorkspacePaths(settings, recent, currentWorkspace),
    [currentWorkspace, recent, settings],
  );
  const [selectedWorkspacePath, setSelectedWorkspacePath] = useState('');
  const [typeDraft, setTypeDraft] = useState<NoteType | null>(null);
  const [typeSaving, setTypeSaving] = useState(false);

  useEffect(() => {
    if (!open) {
      return;
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        onClose();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose, open]);

  useEffect(() => {
    if (!open) {
      return;
    }
    setSelectedWorkspacePath((current) => current || currentWorkspace || workspacePaths[0] || '');
  }, [currentWorkspace, open, workspacePaths]);

  if (!open) {
    return null;
  }

  const sections: {id: SettingsSection; label: string}[] = [
    {id: 'appearance', label: 'Appearance'},
    {id: 'noteView', label: 'Note View'},
    {id: 'graphView', label: 'Graph View'},
    {id: 'workspaceSettings', label: 'Workspace Settings'},
    {id: 'types', label: 'Note Types'},
  ];
  const saveLabel = saveState === 'saving' ? 'Saving...' : saveState === 'saved' ? 'Saved' : saveState === 'error' ? 'Could not save' : 'Auto-saved';
  const selectedWorkspaceSettings = selectedWorkspacePath
    ? workspaceSettingsFor(settings, selectedWorkspacePath)
    : defaultWorkspaceSettings();
  const updateSelectedWorkspaceSettings = (nextWorkspaceSettings: GoMentalWorkspaceSettings) => {
    if (!selectedWorkspacePath) {
      return;
    }
    onChange({
      ...settings,
      workspaces: {
        ...settings.workspaces,
        [selectedWorkspacePath]: normalizeWorkspaceSettings(nextWorkspaceSettings),
      },
    });
  };
  const addWorkspace = async () => {
    const path = await onBrowseWorkspace();
    const trimmed = path.trim();
    if (!trimmed) {
      return;
    }
    setSelectedWorkspacePath(trimmed);
    if (!settings.workspaces[trimmed]) {
      onChange({
        ...settings,
        workspaces: {
          ...settings.workspaces,
          [trimmed]: defaultWorkspaceSettings(),
        },
      });
    }
  };

  return (
    <div className="gm-settings-scrim" onClick={onClose} role="presentation">
      <div className="gm-settings-modal" onClick={(event) => event.stopPropagation()} role="dialog" aria-modal="true" aria-label="Settings">
        <header className="gm-settings-header">
          <div>
            <h2>Settings</h2>
            <p>Stored as app-level JSON in GoMental.Settings.json.</p>
          </div>
          <div className="gm-settings-header-actions">
            <span className={saveState === 'error' ? 'gm-settings-save error' : 'gm-settings-save'}>{saveLabel}</span>
            <button type="button" className="gm-btn gm-btn-icon" onClick={onClose} title="Close settings" aria-label="Close settings">
              <CloseIcon size={15} />
            </button>
          </div>
        </header>
        <div className="gm-settings-body">
          <nav className="gm-settings-nav" aria-label="Settings sections">
            {sections.map((section) => (
              <button
                type="button"
                key={section.id}
                className={activeSection === section.id ? 'gm-settings-nav-item active' : 'gm-settings-nav-item'}
                onClick={() => onSectionChange(section.id)}
              >
                {section.label}
              </button>
            ))}
          </nav>
          <section className="gm-settings-pane">
            {activeSection === 'appearance' && (
              <SettingsGroup title="Appearance">
                <label className="gm-setting-row">
                  <span>
                    <strong>Theme</strong>
                    <small>Changes the whole shell immediately.</small>
                  </span>
                  <select
                    value={settings.appearance.theme}
                    onChange={(event) => onChange({
                      ...settings,
                      appearance: {...settings.appearance, theme: event.target.value as ThemeMode},
                    })}
                  >
                    <option value="dark">Dark</option>
                    <option value="light">Light</option>
                    {vscodeThemeOptions.map((theme) => <option key={theme.id} value={theme.id}>{theme.label} ({theme.category})</option>)}
                  </select>
                </label>
              </SettingsGroup>
            )}
            {activeSection === 'noteView' && (
              <SettingsGroup title="Note View">
                <label className="gm-setting-row">
                  <span>
                    <strong>Default editor</strong>
                    <small>Used when opening a note for editing.</small>
                  </span>
                  <select
                    value={settings.noteView.defaultEditMode}
                    onChange={(event) => onChange({
                      ...settings,
                      noteView: {...settings.noteView, defaultEditMode: event.target.value as 'rich' | 'source'},
                    })}
                  >
                    <option value="rich">Rich text</option>
                    <option value="source">Markdown source</option>
                  </select>
                </label>
                <label className="gm-setting-row gm-setting-row-checkbox">
                  <span>
                    <strong>Find bar</strong>
                    <small>Show in the read-only note view.</small>
                  </span>
                  <input
                    type="checkbox"
                    checked={settings.noteView.showFindBar}
                    onChange={(event) => onChange({
                      ...settings,
                      noteView: {...settings.noteView, showFindBar: event.target.checked},
                    })}
                  />
                </label>
              </SettingsGroup>
            )}
            {activeSection === 'graphView' && (
              <SettingsGroup title="Graph View">
                <label className="gm-setting-row">
                  <span>
                    <strong>Default mode</strong>
                    <small>Choose the graph lens opened by default.</small>
                  </span>
                  <select
                    value={settings.graphView.defaultMode}
                    onChange={(event) => onChange({
                      ...settings,
                      graphView: {...settings.graphView, defaultMode: event.target.value as '2d' | '3d'},
                    })}
                  >
                    <option value="2d">2D</option>
                    <option value="3d">3D</option>
                  </select>
                </label>
                <label className="gm-setting-row">
                  <span>
                    <strong>Default depth</strong>
                    <small>How many hops from the selected note.</small>
                  </span>
                  <input
                    type="range"
                    min={1}
                    max={4}
                    value={settings.graphView.defaultDepth}
                    onChange={(event) => onChange({
                      ...settings,
                      graphView: {...settings.graphView, defaultDepth: Number(event.target.value)},
                    })}
                  />
                  <b className="gm-setting-value">{settings.graphView.defaultDepth}</b>
                </label>
              </SettingsGroup>
            )}
            {activeSection === 'types' && (
              <SettingsGroup title="Workspace Note Types">
                {currentWorkspace ? (
                  <div className="gm-type-manager">
                    <div className="gm-type-manager-toolbar">
                      <button type="button" className="gm-btn gm-btn-sm" onClick={() => setTypeDraft({id: '', label: '', description: '', template: '---\ntype: {{type}}\ntitle: {{titleYaml}}\n---\n\n# {{title}}\n\n', source: 'workspace'})}>New Note Type</button>
                      <button type="button" className="gm-btn gm-btn-sm gm-btn-ghost" onClick={() => { const content = window.prompt('Paste a Note Type collection YAML.'); if (content?.trim()) { void onImportCollection(content).catch(() => {}); } }}>Import</button>
                      <label className="gm-type-manager-select"><span>Note Type</span><select value={typeDraft?.id || ''} onChange={(event) => setTypeDraft(noteTypes.find((type) => type.id === event.target.value) || null)}><option value="">Choose a Note Type</option>{noteTypes.map((type) => <option key={type.id} value={type.id}>{type.label}</option>)}</select></label>
                    </div>
                    <div className="gm-type-manager-editor">
                      {typeDraft ? <>
                        <label>Note Type ID<input value={typeDraft.id} disabled={typeDraft.source === 'builtin' || noteTypes.some((type) => type.id === typeDraft.id)} onChange={(event) => setTypeDraft({...typeDraft, id: event.target.value.toLowerCase()})} placeholder="project-brief" /></label>
                        <label>Note Type Name<input value={typeDraft.label} disabled={typeDraft.source === 'builtin'} onChange={(event) => setTypeDraft({...typeDraft, label: event.target.value})} placeholder="Project brief" /></label>
                        <label>Description<input value={typeDraft.description} disabled={typeDraft.source === 'builtin'} onChange={(event) => setTypeDraft({...typeDraft, description: event.target.value})} placeholder="What this Note Type is for" /></label>
                        <label>Starter Content<textarea value={typeDraft.template} disabled={typeDraft.source === 'builtin'} onChange={(event) => setTypeDraft({...typeDraft, template: event.target.value})} spellCheck={false} /></label>
                        <div className="gm-type-manager-actions">
                          {typeDraft.source !== 'builtin' && <button type="button" className="gm-btn" disabled={typeSaving} onClick={() => { setTypeSaving(true); void onSaveNoteType(typeDraft).then(() => setTypeSaving(false)).catch(() => setTypeSaving(false)); }}>{typeSaving ? 'Saving...' : 'Save Note Type'}</button>}
                          {typeDraft.source !== 'builtin' && noteTypes.some((type) => type.id === typeDraft.id) && <button type="button" className="gm-btn gm-btn-ghost" disabled={typeSaving} onClick={() => { if (window.confirm(`Remove ${typeDraft.label}? Existing notes retain their original metadata and behave as General until this Note Type is restored.`)) { setTypeSaving(true); void onDeleteNoteType(typeDraft.id).then(() => { setTypeDraft(null); setTypeSaving(false); }).catch(() => setTypeSaving(false)); } }}>Remove Note Type</button>}
                        </div>
                      </> : <div className="gm-workspace-empty gm-workspace-empty-large">Choose a type to edit its workspace file.</div>}
                    </div>
                  </div>
                ) : <div className="gm-workspace-empty gm-workspace-empty-large">Open a workspace to manage its installed types.</div>}
              </SettingsGroup>
            )}
            {activeSection === 'workspaceSettings' && (
              <SettingsGroup title="Workspace Settings">
                <div className="gm-workspace-settings">
                  <div className="gm-workspace-list">
                    <div className="gm-workspace-list-head">
                      <span>Known workspaces</span>
                      <button type="button" className="gm-btn gm-btn-sm gm-btn-ghost" onClick={() => void addWorkspace()}>
                        <FolderIcon size={14} />Browse
                      </button>
                    </div>
                    {workspacePaths.length > 0 ? (
                      workspacePaths.map((path) => (
                        <button
                          type="button"
                          key={path}
                          className={selectedWorkspacePath === path ? 'gm-workspace-item active' : 'gm-workspace-item'}
                          onClick={() => setSelectedWorkspacePath(path)}
                        >
                          <span className="gm-workspace-name">{basename(path)}</span>
                          <span className="gm-workspace-path">{path}</span>
                        </button>
                      ))
                    ) : (
                      <div className="gm-workspace-empty">No recent workspaces yet.</div>
                    )}
                  </div>
                  <div className="gm-workspace-editor">
                    {selectedWorkspacePath ? (
                      <>
                        <div className="gm-workspace-editor-title">
                          <span>{basename(selectedWorkspacePath)}</span>
                          <code>{selectedWorkspacePath}</code>
                        </div>
                        <label className="gm-setting-row">
                          <span>
                            <strong>Default Note Type</strong>
                            <small>Used as the starting Note Type for new notes in this workspace.</small>
                          </span>
                          <select
                            value={selectedWorkspaceSettings.defaultType}
                            onChange={(event) => updateSelectedWorkspaceSettings({
                              ...selectedWorkspaceSettings,
                              defaultType: event.target.value,
                              enabledTypes: ensureEnabledType(selectedWorkspaceSettings.enabledTypes, event.target.value),
                            })}
                          >
                            {noteTypes.map((template) => (
                              <option key={template.id} value={template.id}>{template.label}</option>
                            ))}
                          </select>
                        </label>
                        <div className="gm-setting-block">
                          <div>
                            <strong>Enabled Note Types</strong>
                            <small>Controls which Note Types are available in this workspace.</small>
                          </div>
                          <div className="gm-type-checks">
                            {noteTypes.map((template) => {
                              const checked = selectedWorkspaceSettings.enabledTypes.includes(template.id);
                              return (
                                <label className="gm-type-check" key={template.id}>
                                  <input
                                    type="checkbox"
                                    checked={checked}
                                    onChange={(event) => {
                                      const enabledTypes = event.target.checked
                                        ? ensureEnabledType(selectedWorkspaceSettings.enabledTypes, template.id)
                                        : selectedWorkspaceSettings.enabledTypes.filter((type) => type !== template.id);
                                      updateSelectedWorkspaceSettings({
                                        ...selectedWorkspaceSettings,
                                        defaultType: enabledTypes.includes(selectedWorkspaceSettings.defaultType) ? selectedWorkspaceSettings.defaultType : (enabledTypes[0] || template.id),
                                        enabledTypes: enabledTypes.length > 0 ? enabledTypes : [template.id],
                                      });
                                    }}
                                  />
                                  <span>{template.label}</span>
                                </label>
                              );
                            })}
                          </div>
                        </div>
                        <label className="gm-setting-row">
                          <span>
                            <strong>Access mode</strong>
                            <small>Controls how GoMental should treat local edits for this workspace.</small>
                          </span>
                          <select
                            value={selectedWorkspaceSettings.accessMode}
                            onChange={(event) => updateSelectedWorkspaceSettings({
                              ...selectedWorkspaceSettings,
                              accessMode: event.target.value as GoMentalWorkspaceSettings['accessMode'],
                            })}
                          >
                            <option value="editable">Editable</option>
                            <option value="readOnlyLocal">Read-only local</option>
                            <option value="readOnlyGit">Read-only git connected</option>
                            <option value="writableGit">Writable git branch</option>
                          </select>
                        </label>
                        {(selectedWorkspaceSettings.accessMode === 'readOnlyGit' || selectedWorkspaceSettings.accessMode === 'writableGit') && (
                          <>
                            <label className="gm-setting-row">
                              <span>
                                <strong>Git URL</strong>
                                <small>Remote repository for this workspace.</small>
                              </span>
                              <input
                                type="url"
                                value={selectedWorkspaceSettings.gitUrl}
                                placeholder="https://github.com/org/wiki.git"
                                onChange={(event) => updateSelectedWorkspaceSettings({
                                  ...selectedWorkspaceSettings,
                                  gitUrl: event.target.value,
                                })}
                              />
                            </label>
                            <label className="gm-setting-row">
                              <span>
                                <strong>Base branch</strong>
                                <small>The branch GoMental branches from and opens PRs into.</small>
                              </span>
                              <input
                                value={selectedWorkspaceSettings.gitBaseRef}
                                placeholder="main"
                                onChange={(event) => updateSelectedWorkspaceSettings({
                                  ...selectedWorkspaceSettings,
                                  gitBaseRef: event.target.value,
                                })}
                              />
                            </label>
                          </>
                        )}
                        {selectedWorkspaceSettings.accessMode === 'writableGit' && (
                          <>
                            <label className="gm-setting-row">
                              <span>
                                <strong>Instance branch</strong>
                                <small>Leave blank to use a machine-specific GoMental branch.</small>
                              </span>
                              <input
                                value={selectedWorkspaceSettings.gitBranch}
                                placeholder="gomental/this-machine/wiki"
                                onChange={(event) => updateSelectedWorkspaceSettings({
                                  ...selectedWorkspaceSettings,
                                  gitBranch: event.target.value,
                                })}
                              />
                            </label>
                            <label className="gm-setting-row">
                              <span>
                                <strong>GitHub username</strong>
                                <small>Optional. Used only with the app-managed token below.</small>
                              </span>
                              <input
                                value={selectedWorkspaceSettings.gitUsername}
                                placeholder="x-access-token"
                                onChange={(event) => updateSelectedWorkspaceSettings({
                                  ...selectedWorkspaceSettings,
                                  gitUsername: event.target.value,
                                })}
                              />
                            </label>
                            <label className="gm-setting-row">
                              <span>
                                <strong>GitHub token</strong>
                                <small>Optional. Allows push, PR, and merge without Git credential manager or SSH.</small>
                              </span>
                              <input
                                type="password"
                                value={selectedWorkspaceSettings.gitToken}
                                placeholder="github_pat_..."
                                onChange={(event) => updateSelectedWorkspaceSettings({
                                  ...selectedWorkspaceSettings,
                                  gitToken: event.target.value,
                                })}
                              />
                            </label>
                            <label className="gm-setting-row">
                              <span>
                                <strong>On exit</strong>
                                <small>What GoMental should do with the branch when the app closes.</small>
                              </span>
                              <select
                                value={selectedWorkspaceSettings.gitExitAction}
                                onChange={(event) => updateSelectedWorkspaceSettings({
                                  ...selectedWorkspaceSettings,
                                  gitExitAction: event.target.value as GoMentalWorkspaceSettings['gitExitAction'],
                                })}
                              >
                                <option value="none">Do nothing</option>
                                <option value="prompt">Prompt me</option>
                                <option value="autoPr">Open PR</option>
                                <option value="autoMerge">Merge PR</option>
                              </select>
                            </label>
                          </>
                        )}
                        <div className="gm-setting-block">
                          <div>
                            <strong>Suggested links</strong>
                            <small>Find related notes locally and add accepted results as Markdown links.</small>
                          </div>
                          <label className="gm-setting-row">
                            <span><strong>Behavior</strong></span>
                            <select
                              value={selectedWorkspaceSettings.suggestedLinks.mode}
                              onChange={(event) => updateSelectedWorkspaceSettings({
                                ...selectedWorkspaceSettings,
                                suggestedLinks: {...selectedWorkspaceSettings.suggestedLinks, mode: event.target.value as GoMentalWorkspaceSettings['suggestedLinks']['mode']},
                              })}
                            >
                              <option value="off">Off</option>
                              <option value="prompt">Ask before adding</option>
                              <option value="automatic">Add high-confidence links</option>
                            </select>
                          </label>
                          {selectedWorkspaceSettings.suggestedLinks.mode !== 'off' && (
                            <>
                              <label className="gm-setting-row">
                                <span><strong>Check for links</strong></span>
                                <select
                                  value={selectedWorkspaceSettings.suggestedLinks.trigger}
                                  onChange={(event) => updateSelectedWorkspaceSettings({
                                    ...selectedWorkspaceSettings,
                                    suggestedLinks: {...selectedWorkspaceSettings.suggestedLinks, trigger: event.target.value as GoMentalWorkspaceSettings['suggestedLinks']['trigger']},
                                  })}
                                >
                                  <option value="whileEditing">While editing</option>
                                  <option value="onSave">When saving</option>
                                </select>
                              </label>
                              <label className="gm-setting-row">
                                <span><strong>Maximum suggestions</strong></span>
                                <input
                                  type="number"
                                  min={1}
                                  max={10}
                                  value={selectedWorkspaceSettings.suggestedLinks.maxSuggestions}
                                  onChange={(event) => updateSelectedWorkspaceSettings({
                                    ...selectedWorkspaceSettings,
                                    suggestedLinks: {...selectedWorkspaceSettings.suggestedLinks, maxSuggestions: Number(event.target.value)},
                                  })}
                                />
                              </label>
                            </>
                          )}
                        </div>
                      </>
                    ) : (
                      <div className="gm-workspace-empty gm-workspace-empty-large">Choose or browse for a workspace to configure it.</div>
                    )}
                  </div>
                </div>
              </SettingsGroup>
            )}
          </section>
        </div>
      </div>
    </div>
  );
}

function SettingsGroup({title, children}: {title: string; children: ReactNode}) {
  return (
    <div className="gm-settings-group">
      <h3>{title}</h3>
      <div className="gm-settings-fields">{children}</div>
    </div>
  );
}

function SearchResultsList({
  results,
  status,
  query,
  error,
  onOpen,
  onToggleFavorite,
}: {
  results: application.SearchResultDTO[];
  status: SearchStatus;
  query: string;
  error: string;
  onOpen: (id: string) => void;
  onToggleFavorite: (id: string, favorite: boolean) => void;
}) {
  if (status === 'searching') {
    return <div className="gm-result-label">Searching…</div>;
  }
  if (status === 'error') {
    return <div className="gm-result-error">{error || 'Search failed.'}</div>;
  }
  return (
    <div className="gm-results">
      <div className="gm-result-label">{results.length} result{results.length === 1 ? '' : 's'}</div>
      {results.map((result) => (
        <button type="button" className="gm-result" key={result.id} onClick={() => onOpen(result.id)}>
          <div className="gm-result-head">
            <span className="gm-result-title">{result.title || basename(result.id) || result.id}</span>
            <span className="gm-result-path">{result.path || result.id}</span>
            <span
              role="button"
              tabIndex={0}
              className={result.favorite ? 'gm-star gm-star-active' : 'gm-star'}
              title={result.favorite ? 'Remove from favorites' : 'Add to favorites'}
              aria-label={result.favorite ? 'Remove from favorites' : 'Add to favorites'}
              aria-pressed={Boolean(result.favorite)}
              onClick={(event) => {
                event.preventDefault();
                event.stopPropagation();
                onToggleFavorite(result.id, !result.favorite);
              }}
              onKeyDown={(event) => {
                if (event.key !== 'Enter' && event.key !== ' ') {
                  return;
                }
                event.preventDefault();
                event.stopPropagation();
                onToggleFavorite(result.id, !result.favorite);
              }}
            >
              <StarIcon size={14} filled={Boolean(result.favorite)} />
            </span>
          </div>
          {searchSnippet(result) && <SearchSnippet fragment={searchSnippet(result)} />}
        </button>
      ))}
      {results.length === 0 && <div className="gm-result-empty">No notes match “{query}”.</div>}
    </div>
  );
}

function SearchSnippet({fragment}: {fragment: string}) {
  const parts = splitSearchFragment(fragment);
  const trimmed = trimOuterSearchFragmentParts(parts);
  return (
    <div className="gm-result-snippet">
      {trimmed.map((part, index) => part.marked ? <mark key={index}>{part.text}</mark> : <span key={index}>{part.text}</span>)}
    </div>
  );
}

function RecentWorkspaceList({
  recent,
  disabled,
  onOpen,
  variant = 'sidebar',
}: {
  recent: application.RecentWorkspaceDTO[];
  disabled: boolean;
  onOpen: (path: string) => void;
  variant?: 'sidebar' | 'main';
}) {
  const visibleRecent = recent.slice(0, 10);
  if (visibleRecent.length === 0) {
    return <p className="gm-rail-empty">No recent workspaces.</p>;
  }
  return (
    <div className={variant === 'main' ? 'gm-recent gm-recent-main' : 'gm-recent'}>
      <div className="gm-section-title gm-recent-title">Recent workspaces</div>
      {visibleRecent.map((item) => (
        <button type="button" className="gm-recent-row" key={item.path} onClick={() => onOpen(item.path)} disabled={disabled}>
          <span className="gm-recent-name">{basename(item.path)}</span>
          <span className="gm-recent-path">{item.path}</span>
        </button>
      ))}
    </div>
  );
}

// ---- Pure helpers --------------------------------------------------------

function extractLinkedNotes(content: string, sourceID: string, notes: application.NoteSummaryDTO[]): {id: string; title: string}[] {
  const re = /\[\[([^\]|#]+)(?:[|#][^\]]*)?\]\]/g;
  const seen = new Set<string>();
  const out: {id: string; title: string}[] = [];
  let match: RegExpExecArray | null;
  while ((match = re.exec(content)) !== null) {
    const resolved = resolveLinkedNoteID(match[1], sourceID, notes);
    if (resolved && resolved !== sourceID && !seen.has(resolved)) {
      seen.add(resolved);
      out.push({id: resolved, title: titleForNoteID(resolved, notes)});
    }
  }
  return out;
}

function titleForNoteID(id: string, notes: application.NoteSummaryDTO[]): string {
  const note = notes.find((item) => item.id === id);
  return note?.title || basename(id) || id;
}

function relativeTime(iso: string): string {
  if (!iso) {
    return '';
  }
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) {
    return '';
  }
  const seconds = Math.max(0, Math.round((Date.now() - then) / 1000));
  if (seconds < 60) {
    return 'just now';
  }
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) {
    return `${minutes} minute${minutes === 1 ? '' : 's'} ago`;
  }
  const hours = Math.round(minutes / 60);
  if (hours < 24) {
    return `${hours} hour${hours === 1 ? '' : 's'} ago`;
  }
  const days = Math.round(hours / 24);
  if (days < 30) {
    return `${days} day${days === 1 ? '' : 's'} ago`;
  }
  const months = Math.round(days / 30);
  if (months < 12) {
    return `${months} month${months === 1 ? '' : 's'} ago`;
  }
  const years = Math.round(months / 12);
  return `${years} year${years === 1 ? '' : 's'} ago`;
}

function normalizeNoteID(raw: string): string {
  return normalizeNotePath(raw.replace(/^\//, '').replace(/\.md$/i, '').replace(/\\/g, '/'));
}

function resolveLinkedNoteID(raw: string, sourceID: string, notes: application.NoteSummaryDTO[]): string {
  const target = normalizeNoteID(raw.split('#')[0].trim());
  if (!target) {
    return '';
  }

  const byID = new Map(notes.map((note) => [note.id.toLocaleLowerCase(), note.id]));
  const candidates = new Set<string>();
  candidates.add(target);

  const sourceFolder = sourceID.includes('/') ? sourceID.slice(0, sourceID.lastIndexOf('/')) : '';
  if (raw.startsWith('./') || raw.startsWith('../')) {
    candidates.add(normalizeNotePath(sourceFolder ? `${sourceFolder}/${target}` : target));
  } else if (!target.includes('/') && sourceFolder) {
    candidates.add(normalizeNotePath(`${sourceFolder}/${target}`));
  }

  for (const candidate of candidates) {
    const match = byID.get(candidate.toLocaleLowerCase());
    if (match) {
      return match;
    }
  }

  const titleTarget = target.split('/').pop()?.toLocaleLowerCase() || target.toLocaleLowerCase();
  const titleMatch = notes.find((note) =>
    note.title.toLocaleLowerCase() === raw.trim().toLocaleLowerCase() ||
    note.title.toLocaleLowerCase() === titleTarget ||
    basename(note.id).toLocaleLowerCase() === titleTarget ||
    slugify(note.title).toLocaleLowerCase() === slugify(raw).toLocaleLowerCase()
  );
  return titleMatch?.id || '';
}

function normalizeNotePath(path: string): string {
  const parts: string[] = [];
  for (const part of path.replace(/\\/g, '/').split('/')) {
    if (!part || part === '.') {
      continue;
    }
    if (part === '..') {
      parts.pop();
      continue;
    }
    parts.push(part);
  }
  return parts.join('/');
}

function groupNotes(notes: application.NoteSummaryDTO[]): TreeGroup[] {
  const groups = new Map<string, application.NoteSummaryDTO[]>();
  for (const note of notes) {
    const index = note.id.lastIndexOf('/');
    const group = index >= 0 ? note.id.slice(0, index) : 'Root';
    const items = groups.get(group) ?? [];
    items.push(note);
    groups.set(group, items);
  }
  return Array.from(groups.entries())
    .sort(([left], [right]) => {
      if (left === 'Root') {
        return -1;
      }
      if (right === 'Root') {
        return 1;
      }
      return left.localeCompare(right);
    })
    .map(([name, items]) => ({
      name,
      depth: name === 'Root' ? 0 : name.split('/').length,
      notes: items.sort((a, b) => a.id.localeCompare(b.id)),
    }));
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

function rebuildProgressPercent(stage: string, completed: number, total: number): number {
  if (stage === 'complete') {
    return 100;
  }
  const fraction = total > 0 ? clamp(completed / total, 0, 1) : 0;
  switch (stage) {
    case 'scanning':
      return Math.round(fraction * 10);
    case 'parsing':
      return Math.round(10 + fraction * 45);
    case 'indexing':
      return Math.round(55 + fraction * 25);
    case 'graph':
      return Math.round(80 + fraction * 20);
    default:
      return Math.round(fraction * 100);
  }
}

function splitSearchFragment(fragment: string): {text: string; marked: boolean}[] {
  const parts: {text: string; marked: boolean}[] = [];
  const pattern = /<mark>(.*?)<\/mark>/gi;
  let cursor = 0;
  let match: RegExpExecArray | null;
  while ((match = pattern.exec(fragment)) !== null) {
    if (match.index > cursor) {
      parts.push({text: cleanSearchFragmentText(fragment.slice(cursor, match.index)), marked: false});
    }
    parts.push({text: cleanSearchFragmentText(match[1]), marked: true});
    cursor = match.index + match[0].length;
  }
  if (cursor < fragment.length) {
    parts.push({text: cleanSearchFragmentText(fragment.slice(cursor)), marked: false});
  }
  return parts.length > 0 ? parts : [{text: cleanSearchFragmentText(fragment), marked: false}];
}

function searchSnippet(result: application.SearchResultDTO): string {
  const title = cleanSearchFragmentText(result.title || basename(result.id) || result.id).trim().toLocaleLowerCase();
  return (result.fragments || []).find((fragment) => {
    const text = cleanSearchFragmentText(fragment).trim().toLocaleLowerCase();
    return text && text !== title;
  }) || '';
}

function cleanSearchFragmentText(value: string): string {
  return value
    .replace(/<[^>]*>/g, '')
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&amp;/g, '&')
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/\s+/g, ' ');
}

function trimOuterSearchFragmentParts(parts: {text: string; marked: boolean}[]): {text: string; marked: boolean}[] {
  const next = parts.filter((part) => part.text.length > 0);
  if (next.length === 0) {
    return [];
  }
  next[0] = {...next[0], text: next[0].text.trimStart()};
  const last = next.length - 1;
  next[last] = {...next[last], text: next[last].text.trimEnd()};
  return next.filter((part) => part.text.length > 0);
}

function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(reader.error || new Error('Could not read image file.'));
    reader.onload = () => {
      const result = String(reader.result || '');
      resolve(result.includes(',') ? result.slice(result.indexOf(',') + 1) : result);
    };
    reader.readAsDataURL(file);
  });
}


function shortCommit(commit: string): string {
  return commit ? commit.slice(0, 7) : '—';
}

// Build the git status chip's hover title from lastSyncAt / lastError, so the
// unobtrusive chip carries the full sync state on hover.
function gitChipTitle(git: {remote: string; ref: string; commit: string; lastSyncAt?: string | null; lastError?: string; operation?: string}): string {
  const lines = [`${git.remote}`, `${git.ref} @ ${git.commit || '(not yet cloned)'}`];
  if (git.operation) {
    lines.push(git.operation);
  }
  if (git.lastSyncAt) {
    lines.push(`Last synced ${relativeTime(git.lastSyncAt)}`);
  }
  if (git.lastError) {
    lines.push(`Error: ${git.lastError}`);
  }
  return lines.join('\n');
}

function normalizeNewNoteID(value: string): string {
  return slugifyNoteID(value)
    .replace(/^\/+/, '')
    .replace(/\.md$/i, '')
    .split('/')
    .filter((part) => part && part !== '.' && part !== '..')
    .join('/');
}

function slugifyNoteID(value: string): string {
  return value
    .trim()
    .replace(/\\/g, '/')
    .replace(/\.md$/i, '')
    .split('/')
    .map((part) => part
      .trim()
      .toLocaleLowerCase()
      .replace(/[^a-z0-9 _-]+/g, '')
      .replace(/\s+/g, '-')
      .replace(/-+/g, '-')
      .replace(/^-|-$/g, ''))
    .filter(Boolean)
    .join('/');
}

function yamlQuote(value: string): string {
  return JSON.stringify(value);
}

function todayISO(): string {
  const now = new Date();
  const local = new Date(now.getTime() - now.getTimezoneOffset() * 60_000);
  return local.toISOString().slice(0, 10);
}

function normalizeSettings(value: GoMentalSettings): GoMentalSettings {
	const needsGeneralTypeMigration = (value?.version || 0) < 2;
  const theme = value?.appearance?.theme === 'light' || value?.appearance?.theme === 'dark' || themeOption(value?.appearance?.theme || '')
    ? value.appearance.theme
    : DEFAULT_SETTINGS.appearance.theme;
  const defaultEditMode = value?.noteView?.defaultEditMode === 'source' || value?.noteView?.defaultEditMode === 'rich'
    ? value.noteView.defaultEditMode
    : DEFAULT_SETTINGS.noteView.defaultEditMode;
  const defaultMode = value?.graphView?.defaultMode === '3d' || value?.graphView?.defaultMode === '2d'
    ? value.graphView.defaultMode
    : DEFAULT_SETTINGS.graphView.defaultMode;
  const defaultDepth = clamp(Number(value?.graphView?.defaultDepth) || DEFAULT_SETTINGS.graphView.defaultDepth, 1, 4);
  const workspaces: Record<string, GoMentalWorkspaceSettings> = {};
  for (const [path, workspaceSettings] of Object.entries(value?.workspaces || {})) {
    const trimmedPath = path.trim();
    if (!trimmedPath) {
      continue;
    }
    const normalizedWorkspaceSettings = normalizeWorkspaceSettings(workspaceSettings);
    if (needsGeneralTypeMigration && !normalizedWorkspaceSettings.enabledTypes.includes('general')) {
      normalizedWorkspaceSettings.enabledTypes = ensureEnabledType(normalizedWorkspaceSettings.enabledTypes, 'general');
    }
    workspaces[trimmedPath] = normalizedWorkspaceSettings;
  }
  return {
    version: 3,
    appearance: {theme},
    noteView: {
      defaultEditMode,
      showFindBar: typeof value?.noteView?.showFindBar === 'boolean' ? value.noteView.showFindBar : DEFAULT_SETTINGS.noteView.showFindBar,
    },
    graphView: {
      defaultMode,
      defaultDepth,
    },
    workspaces,
  };
}

function knownWorkspacePaths(settings: GoMentalSettings, recent: application.RecentWorkspaceDTO[], currentWorkspace: string): string[] {
  const paths: string[] = [];
  const add = (path: string) => {
    const trimmed = path.trim();
    if (trimmed && !paths.some((item) => item.toLocaleLowerCase() === trimmed.toLocaleLowerCase())) {
      paths.push(trimmed);
    }
  };
  add(currentWorkspace);
  recent.forEach((item) => add(item.path || ''));
  Object.keys(settings.workspaces || {}).forEach(add);
  return paths;
}

function workspaceSettingsFor(settings: GoMentalSettings, path: string): GoMentalWorkspaceSettings {
  return normalizeWorkspaceSettings(settings.workspaces?.[path] || defaultWorkspaceSettings());
}

function workspaceIsReadOnly(settings: GoMentalSettings, path: string): boolean {
  const mode = workspaceSettingsFor(settings, path).accessMode;
  return Boolean(path && mode !== 'editable' && mode !== 'writableGit');
}

function renderNoteTypeStarterContent(type: NoteType, title: string, id: string): string {
  const safeTitle = title.trim() || basename(id);
  const replacements: Record<string, string> = {
    title: safeTitle,
    titleYaml: yamlQuote(safeTitle),
    id,
    type: type.id,
    date: todayISO(),
  };
  return type.template.replace(/{{\s*(title|titleYaml|id|type|date)\s*}}/g, (_full, key: string) => replacements[key]);
}

function defaultWorkspaceSettings(): GoMentalWorkspaceSettings {
  return {
    defaultType: 'term',
    enabledTypes: [],
    accessMode: 'editable',
    gitUrl: '',
    gitBaseRef: 'main',
    gitBranch: '',
    gitUsername: '',
    gitToken: '',
    gitExitAction: 'none',
    suggestedLinks: {
      mode: 'off',
      trigger: 'onSave',
      placement: 'relatedSection',
      minScore: 0.45,
      maxSuggestions: 5,
    },
  };
}

function normalizeWorkspaceSettings(value: GoMentalWorkspaceSettings): GoMentalWorkspaceSettings {
  const defaults = defaultWorkspaceSettings();
  const enabledTypes = Array.from(new Set((value?.enabledTypes || []).map((type) => type.trim() === 'concept' ? 'term' : type.trim()).filter(Boolean)));
  const requestedDefaultType = value?.defaultType?.trim() === 'concept' ? 'term' : value?.defaultType?.trim();
  const defaultType = requestedDefaultType || defaults.defaultType;
  const accessMode = value?.accessMode === 'readOnlyLocal' || value?.accessMode === 'readOnlyGit' || value?.accessMode === 'writableGit' || value?.accessMode === 'editable'
    ? value.accessMode
    : defaults.accessMode;
  const gitExitAction = value?.gitExitAction === 'prompt' || value?.gitExitAction === 'autoPr' || value?.gitExitAction === 'autoMerge' || value?.gitExitAction === 'none'
    ? value.gitExitAction
    : defaults.gitExitAction;
  return {
    defaultType,
    enabledTypes,
    accessMode,
    gitUrl: accessMode === 'readOnlyGit' || accessMode === 'writableGit' ? (value?.gitUrl || '').trim() : '',
    gitBaseRef: accessMode === 'readOnlyGit' || accessMode === 'writableGit' ? (value?.gitBaseRef || 'main').trim() : '',
    gitBranch: accessMode === 'writableGit' ? (value?.gitBranch || '').trim() : '',
    gitUsername: accessMode === 'writableGit' ? (value?.gitUsername || '').trim() : '',
    gitToken: accessMode === 'writableGit' ? (value?.gitToken || '').trim() : '',
    gitExitAction: accessMode === 'writableGit' ? gitExitAction : 'none',
    suggestedLinks: {
      mode: value?.suggestedLinks?.mode === 'prompt' || value?.suggestedLinks?.mode === 'automatic' ? value.suggestedLinks.mode : 'off',
      trigger: value?.suggestedLinks?.trigger === 'whileEditing' ? 'whileEditing' : 'onSave',
      placement: value?.suggestedLinks?.placement === 'preferInline' ? 'preferInline' : 'relatedSection',
      minScore: clamp(Number(value?.suggestedLinks?.minScore) || 0.45, 0.30, 0.95),
      maxSuggestions: clamp(Math.round(Number(value?.suggestedLinks?.maxSuggestions) || 5), 1, 10),
    },
  };
}

function ensureEnabledType(enabledTypes: string[], type: string): string[] {
  return enabledTypes.includes(type) ? enabledTypes : [type, ...enabledTypes];
}

function isAutomaticSuggestion(suggestion: LinkSuggestion): boolean {
  if (suggestion.score < 0.85) return false;
  const families = new Set(suggestion.evidence.map((item) => item.kind));
  return families.has('title_mention') || families.size >= 2;
}

function suggestionConfidenceLabel(suggestion: LinkSuggestion): string {
  if (suggestion.confidence === 'high') return 'High confidence';
  if (suggestion.confidence === 'strong') return 'Strong';
  return 'Possible';
}

function suggestionEvidenceLabel(suggestion: LinkSuggestion): string {
  const labels: string[] = [];
  for (const evidence of suggestion.evidence) {
    if (evidence.kind === 'title_mention') labels.push('Mentioned in this note');
    else if (evidence.kind === 'lexical_similarity') labels.push('Similar content');
    else if (evidence.kind === 'shared_tag') labels.push(`Shares #${evidence.detail}`);
    else if (evidence.kind === 'shared_link') labels.push(`Both link to ${evidence.detail}`);
    else if (evidence.kind === 'shared_type') labels.push(`Same type: ${evidence.detail}`);
  }
  return Array.from(new Set(labels)).slice(0, 2).join(' · ') || 'Related note';
}

function addRelatedNoteLinks(content: string, suggestions: LinkSuggestion[]): string {
  const additions = suggestions.filter((suggestion) => {
    const target = suggestion.targetId.replace(/^\/+|\.md$/gi, '');
    const encoded = target.split('/').map(encodeURIComponent).join('/');
    return !content.includes(`(/${encoded}.md)`) && !content.includes(`[[${target}]]`) && !content.includes(`[[${target}|`);
  });
  if (additions.length === 0) return content;
  const lines = additions.map((suggestion) => {
    const target = suggestion.targetId.replace(/^\/+|\.md$/gi, '');
    const encoded = target.split('/').map(encodeURIComponent).join('/');
    const label = (suggestion.targetTitle || basename(target)).replace(/([\\[\]])/g, '\\$1');
    return `- [${label}](/${encoded}.md)`;
  });
  const trimmed = content.replace(/\s+$/, '');
  if (trimmed.includes('<!-- gomental:related-links -->')) {
    return `${trimmed}\n${lines.join('\n')}\n`;
  }
  return `${trimmed}\n\n<!-- gomental:related-links -->\n## Related notes\n\n${lines.join('\n')}\n`;
}

// Left-pane sizing: the grid's base sidebar column is 290px; the pane is
// resizable within 50%–200% of that and the preference is persisted.
const SIDEBAR_BASE_WIDTH = 290;
const SIDEBAR_MIN_WIDTH = Math.round(SIDEBAR_BASE_WIDTH * 0.5);
const SIDEBAR_MAX_WIDTH = SIDEBAR_BASE_WIDTH * 2;

function readStoredSidebarWidth(): number {
  try {
    const stored = Number(localStorage.getItem('gm-sidebar-width'));
    if (Number.isFinite(stored) && stored > 0) {
      return Math.min(SIDEBAR_MAX_WIDTH, Math.max(SIDEBAR_MIN_WIDTH, stored));
    }
  } catch {
    // Ignore storage failures.
  }
  return SIDEBAR_BASE_WIDTH;
}

function readStoredRailCollapsed(): boolean {
  try {
    return localStorage.getItem('gm-rail-collapsed') === '1';
  } catch {
    return false;
  }
}

function readStoredGraphMode(): '2d' | '3d' {
  try {
    const stored = localStorage.getItem('gm-graph-mode');
    if (stored === '2d' || stored === '3d') {
      return stored;
    }
  } catch {
    // Ignore storage failures.
  }
  return '2d';
}

function themeAppearance(theme: string): 'light' | 'dark' {
  if (theme === 'light') return 'light';
  return themeOption(theme)?.category === 'light' ? 'light' : 'dark';
}

function readStoredTheme(): ThemeMode {
  try {
    const stored = localStorage.getItem('gm-theme');
    if (stored && (stored === 'dark' || stored === 'light' || themeOption(stored))) {
      return stored;
    }
  } catch {
    // Ignore storage failures.
  }
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

function isConflictError(err: unknown): boolean {
  return (err as {code?: string})?.code === 'edit.external_conflict';
}


export default App;

