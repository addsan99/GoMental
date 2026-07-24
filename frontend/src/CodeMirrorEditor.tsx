import {forwardRef, useEffect, useImperativeHandle, useRef} from 'react';
import {CompletionContext, autocompletion} from '@codemirror/autocomplete';
import {defaultKeymap, history, historyKeymap} from '@codemirror/commands';
import {markdown} from '@codemirror/lang-markdown';
import {HighlightStyle, defaultHighlightStyle, syntaxHighlighting} from '@codemirror/language';
import {tags as t} from '@lezer/highlight';
import {EditorState} from '@codemirror/state';
import {
  EditorView,
  crosshairCursor,
  drawSelection,
  dropCursor,
  highlightActiveLine,
  highlightActiveLineGutter,
  highlightSpecialChars,
  keymap,
  lineNumbers,
  rectangularSelection,
} from '@codemirror/view';
import {openSearchPanel, searchKeymap} from '@codemirror/search';
import type {application} from '../wailsjs/go/models';

type CodeMirrorEditorProps = {
  value: string;
  notes: application.NoteSummaryDTO[];
  disabled?: boolean;
  theme?: 'light' | 'dark';
  onChange: (value: string) => void;
  onSave: () => void | Promise<void>;
  onNavigate: (id: string) => void;
  onRequestLink?: () => void;
  onSaveImage?: (file: File) => Promise<string>;
};

export type CodeMirrorEditorHandle = {
  // Replace the current selection with a wiki link to the given note. Wiki links
  // resolve workspace-absolute, so a full note id always points at the right note
  // regardless of where the editing note lives.
  insertNoteLink: (id: string, label: string) => void;
  // The currently selected text (used as the link label by the picker).
  getSelectionText: () => string;
};

// Syntax palette for dark mode. The default CodeMirror highlight style is tuned
// for light backgrounds (dark, low-contrast glyphs) — on our dark surface it
// renders as dark-on-dark, so we supply a light palette keyed to the app tokens.
const DARK_HIGHLIGHT = HighlightStyle.define([
  {tag: [t.heading, t.heading1, t.heading2, t.heading3, t.heading4], color: '#a9a2ff', fontWeight: '600'},
  {tag: t.strong, color: '#eceaf2', fontWeight: '600'},
  {tag: t.emphasis, color: '#cfc9dd', fontStyle: 'italic'},
  {tag: [t.link, t.url], color: '#7fb0ff', textDecoration: 'underline'},
  {tag: t.monospace, color: '#e5c07b'},
  {tag: t.quote, color: '#a3a0ad'},
  {tag: [t.list, t.processingInstruction, t.contentSeparator], color: '#6d6a78'},
  {tag: t.keyword, color: '#c678dd'},
  {tag: [t.string, t.regexp], color: '#98c379'},
  {tag: [t.number, t.bool, t.atom], color: '#d19a66'},
  {tag: [t.comment, t.meta], color: '#6d6a78', fontStyle: 'italic'},
]);

// editorTheme builds a CodeMirror theme that follows the app's light/dark mode.
// The editor surface itself is transparent (App.css lets the themed page show
// through); we only need to colour text, gutters, active line, caret, selection,
// search bar and the autocomplete tooltip so nothing renders light-on-dark.
function editorTheme(theme: 'light' | 'dark') {
  const dark = theme === 'dark';
  return EditorView.theme(
    {
      '&': {
        height: '100%',
        width: '100%',
        backgroundColor: 'transparent',
        color: dark ? '#eceaf2' : '#272a2f',
      },
      '.cm-scroller': {
        fontFamily: '"Cascadia Code", Consolas, "Liberation Mono", monospace',
        fontSize: '14px',
        lineHeight: '1.65',
      },
      '.cm-gutters': {
        backgroundColor: dark ? '#1a1a21' : '#f0ede5',
        borderRight: `1px solid ${dark ? '#26262e' : '#d8d3c6'}`,
        color: dark ? '#6d6a78' : '#7a7468',
      },
      '.cm-activeLine': {
        backgroundColor: dark ? 'rgba(139,131,255,0.08)' : '#edf6f1',
      },
      '.cm-activeLineGutter': {
        backgroundColor: dark ? 'rgba(139,131,255,0.12)' : '#e6efe9',
        color: dark ? '#a9a2ff' : '#5a746a',
      },
      '.cm-content': {
        padding: '14px 0',
        caretColor: dark ? '#a9a2ff' : '#272a2f',
      },
      '.cm-cursor, .cm-dropCursor': {
        borderLeftColor: dark ? '#a9a2ff' : '#272a2f',
      },
      '.cm-selectionBackground, .cm-content ::selection': {
        backgroundColor: dark ? 'rgba(139,131,255,0.25)' : '#cfe6da',
      },
      '&.cm-focused .cm-selectionBackground, &.cm-focused .cm-content ::selection': {
        backgroundColor: dark ? 'rgba(139,131,255,0.32)' : '#bfe0d0',
      },
      '.cm-line': {
        padding: '0 16px',
      },
      '.cm-search': {
        backgroundColor: dark ? '#1e1e26' : '#fffdf8',
        borderBottom: `1px solid ${dark ? '#26262e' : '#d8d3c6'}`,
        color: dark ? '#eceaf2' : 'inherit',
      },
      '.cm-tooltip': {
        backgroundColor: dark ? '#1e1e26' : '#fffdf8',
        border: `1px solid ${dark ? '#34343f' : '#d8d3c6'}`,
        color: dark ? '#eceaf2' : '#272a2f',
      },
      '.cm-tooltip-autocomplete ul li[aria-selected]': {
        backgroundColor: dark ? 'rgba(139,131,255,0.22)' : '#e6efe9',
        color: dark ? '#ffffff' : 'inherit',
      },
    },
    {dark},
  );
}

const CodeMirrorEditor = forwardRef<CodeMirrorEditorHandle, CodeMirrorEditorProps>(function CodeMirrorEditor(
  {value, notes, disabled = false, theme = 'light', onChange, onSave, onNavigate, onRequestLink, onSaveImage},
  ref,
) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const viewRef = useRef<EditorView | null>(null);
  const valueRef = useRef(value);
  const notesRef = useRef(notes);
  const onChangeRef = useRef(onChange);
  const onSaveRef = useRef(onSave);
  const onNavigateRef = useRef(onNavigate);
  const onRequestLinkRef = useRef(onRequestLink);
  const onSaveImageRef = useRef(onSaveImage);

  useImperativeHandle(ref, () => ({
    insertNoteLink: (id: string, label: string) => {
      const view = viewRef.current;
      if (!view) {
        return;
      }
      const {from, to} = view.state.selection.main;
      const text = label ? `[[${id}|${label}]]` : `[[${id}]]`;
      view.dispatch({
        changes: {from, to, insert: text},
        selection: {anchor: from + text.length},
        scrollIntoView: true,
      });
      view.focus();
    },
    getSelectionText: () => {
      const view = viewRef.current;
      if (!view) {
        return '';
      }
      const {from, to} = view.state.selection.main;
      return view.state.sliceDoc(from, to);
    },
  }), []);

  useEffect(() => {
    valueRef.current = value;
    const view = viewRef.current;
    if (view && view.state.doc.toString() !== value) {
      view.dispatch({changes: {from: 0, to: view.state.doc.length, insert: value}});
    }
  }, [value]);

  useEffect(() => {
    notesRef.current = notes;
  }, [notes]);

  useEffect(() => {
    onChangeRef.current = onChange;
    onSaveRef.current = onSave;
    onNavigateRef.current = onNavigate;
    onRequestLinkRef.current = onRequestLink;
    onSaveImageRef.current = onSaveImage;
  }, [onChange, onSave, onNavigate, onRequestLink, onSaveImage]);

  useEffect(() => {
    if (!hostRef.current) {
      return;
    }

    const insertImageFiles = async (view: EditorView, files: File[], position?: number) => {
      const saveImage = onSaveImageRef.current;
      if (!saveImage || files.length === 0) {
        return;
      }
      const snippets: string[] = [];
      for (const file of files) {
        snippets.push(await saveImage(file));
      }
      const insert = snippets.filter(Boolean).join('\n');
      if (!insert) {
        return;
      }
      const selection = view.state.selection.main;
      const from = position ?? selection.from;
      const to = position ?? selection.to;
      const before = from > 0 && !/\s/.test(view.state.doc.sliceString(from - 1, from)) ? '\n' : '';
      const after = to < view.state.doc.length && !/\s/.test(view.state.doc.sliceString(to, to + 1)) ? '\n' : '';
      const text = `${before}${insert}${after}`;
      view.dispatch({
        changes: {from, to, insert: text},
        selection: {anchor: from + text.length},
        scrollIntoView: true,
      });
      view.focus();
    };

    const editor = new EditorView({
      parent: hostRef.current,
      state: EditorState.create({
        doc: valueRef.current,
        extensions: [
          lineNumbers(),
          highlightActiveLineGutter(),
          highlightSpecialChars(),
          history(),
          drawSelection(),
          dropCursor(),
          rectangularSelection(),
          crosshairCursor(),
          highlightActiveLine(),
          markdown(),
          syntaxHighlighting(theme === 'dark' ? DARK_HIGHLIGHT : defaultHighlightStyle, {fallback: true}),
          autocompletion({override: [okfCompletionSource(notesRef)]}),
          EditorState.readOnly.of(disabled),
          EditorView.editable.of(!disabled),
          EditorView.lineWrapping,
          EditorView.updateListener.of((update) => {
            if (update.docChanged) {
              const next = update.state.doc.toString();
              valueRef.current = next;
              onChangeRef.current(next);
            }
          }),
          EditorView.domEventHandlers({
            paste(event, view) {
              const files = imageFilesFromClipboard(event.clipboardData);
              if (files.length === 0) {
                return false;
              }
              event.preventDefault();
              void insertImageFiles(view, files);
              return true;
            },
            drop(event, view) {
              const files = imageFilesFromFileList(event.dataTransfer?.files);
              if (files.length === 0) {
                return false;
              }
              event.preventDefault();
              const pos = view.posAtCoords({x: event.clientX, y: event.clientY}) ?? view.state.selection.main.head;
              void insertImageFiles(view, files, pos);
              return true;
            },
            mousedown(event, view) {
              if ((!event.ctrlKey && !event.metaKey) || event.button !== 0) {
                return false;
              }
              const pos = view.posAtCoords({x: event.clientX, y: event.clientY});
              if (pos == null) {
                return false;
              }
              const id = wikiLinkAt(view, pos);
              if (!id) {
                return false;
              }
              event.preventDefault();
              onNavigateRef.current(id);
              return true;
            },
          }),
          keymap.of([
            {key: 'Mod-s', run: () => { void onSaveRef.current(); return true; }},
            {key: 'Mod-l', run: () => { onRequestLinkRef.current?.(); return true; }},
            {key: 'Mod-f', run: openSearchPanel},
            ...searchKeymap,
            ...historyKeymap,
            ...defaultKeymap,
          ]),
          editorTheme(theme),
        ],
      }),
    });

    viewRef.current = editor;
    return () => {
      editor.destroy();
      viewRef.current = null;
    };
  }, [disabled, theme]);

  return <div className="editor-surface" ref={hostRef} />;
});

function imageFilesFromClipboard(clipboard: DataTransfer | null): File[] {
  if (!clipboard) {
    return [];
  }
  const files: File[] = [];
  for (const item of Array.from(clipboard.items || [])) {
    if (item.kind === 'file' && item.type.startsWith('image/')) {
      const file = item.getAsFile();
      if (file) {
        files.push(file);
      }
    }
  }
  return files.length > 0 ? files : imageFilesFromFileList(clipboard.files);
}

function imageFilesFromFileList(list: FileList | null | undefined): File[] {
  return Array.from(list || []).filter((file) => file.type.startsWith('image/'));
}

function okfCompletionSource(notesRef: React.MutableRefObject<application.NoteSummaryDTO[]>) {
  return (context: CompletionContext) => {
    const wiki = context.matchBefore(/\[\[[^\]\s|#]*/);
    if (wiki && (wiki.from < context.pos || context.explicit)) {
      const from = wiki.from + 2;
      return {
        from,
        options: notesRef.current.map((note) => ({
          label: note.title || note.id,
          detail: note.id,
          type: 'reference',
          apply: `${note.id}]]`,
        })),
      };
    }

    const tag = context.matchBefore(/#[\w/-]*/);
    if (tag && (tag.from < context.pos || context.explicit)) {
      const tags = Array.from(new Set(notesRef.current.flatMap((note) => note.tags || []))).sort((a, b) => a.localeCompare(b));
      return {
        from: tag.from + 1,
        options: tags.map((item) => ({label: item, type: 'keyword', apply: item})),
      };
    }

    return null;
  };
}

function wikiLinkAt(view: EditorView, pos: number): string {
  const line = view.state.doc.lineAt(pos);
  const offset = pos - line.from;
  const before = line.text.lastIndexOf('[[', offset);
  const after = line.text.indexOf(']]', offset);
  if (before < 0 || after < 0 || before > offset || after < offset) {
    return '';
  }
  const raw = line.text.slice(before + 2, after).split('|')[0].split('#')[0].trim();
  return normalizeNoteID(raw);
}

function normalizeNoteID(raw: string): string {
  return raw.replace(/^\//, '').replace(/\.md$/i, '').replace(/\\/g, '/');
}

export default CodeMirrorEditor;
