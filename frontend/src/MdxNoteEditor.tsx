import {forwardRef, useCallback, useEffect, useImperativeHandle, useMemo, useRef, useState} from 'react';
import type {ChangeEvent, MouseEvent} from 'react';
import {
  BlockTypeSelect,
  BoldItalicUnderlineToggles,
  ButtonWithTooltip,
  codeBlockPlugin,
  codeMirrorPlugin,
  headingsPlugin,
  imagePlugin,
  InsertTable,
  linkPlugin,
  listsPlugin,
  markdownShortcutPlugin,
  MDXEditor,
  quotePlugin,
  Separator,
  tablePlugin,
  thematicBreakPlugin,
  toolbarPlugin,
  type MDXEditorMethods,
} from '@mdxeditor/editor';
import '@mdxeditor/editor/style.css';
import {LoadNoteAssetDataURL} from './transport';
import {ImageIcon, LinkIcon} from './ui/icons';

type SavedEditorImage = {
  path: string;
  dataURL: string;
};

type MdxNoteEditorProps = {
  noteID: string;
  content: string;
  // Drives MDXEditor's `.dark-theme` palette so the WYSIWYG surface matches the
  // shell in dark mode (the editor ships light defaults otherwise).
  theme: 'light' | 'dark';
  onChange: (content: string) => void;
  onNavigate: (id: string) => void;
  onSaveImage: (file: File) => Promise<SavedEditorImage>;
  // Opens the app-level note picker (⌘L). The toolbar Link button routes through
  // this so rich-text linking reuses the same picker as source mode.
  onRequestLink?: () => void;
};

export type MdxNoteEditorHandle = {
  currentContent: () => string;
  // Insert a markdown link to the given note at the cursor. Uses a leading "/"
  // so the backend resolves it workspace-absolute (not relative to this note's
  // folder), and renders as a real clickable link — unlike `[[wiki]]`, which
  // would show as literal text in the WYSIWYG surface.
  insertNoteLink: (id: string, label: string) => void;
  // The currently selected text (used as the link label by the picker).
  getSelectionText: () => string;
};

const MdxNoteEditor = forwardRef<MdxNoteEditorHandle, MdxNoteEditorProps>(function MdxNoteEditor({noteID, content, theme, onChange, onNavigate, onSaveImage, onRequestLink}, ref) {
  const editorRef = useRef<MDXEditorMethods | null>(null);
  const loadRequestRef = useRef(0);
  const lastEmittedContentRef = useRef('');
  const imagePathByDataURLRef = useRef<Map<string, string>>(new Map());
  const imageDataURLByPathRef = useRef<Map<string, string>>(new Map());
  const imageInputRef = useRef<HTMLInputElement | null>(null);
  const [frontmatter, setFrontmatter] = useState('');
  const [editorMarkdown, setEditorMarkdown] = useState('');

  // Kept in a ref so the memoized toolbar can call the latest handler without
  // re-creating the plugin list (and thus re-mounting the editor) each render.
  const onRequestLinkRef = useRef(onRequestLink);
  onRequestLinkRef.current = onRequestLink;

  // Upload an image file and insert it at the cursor. Mirrors the drag/drop
  // imageUploadHandler below: register the dataURL↔path mapping so the markdown
  // serializes back to the stored asset path (see restoreMarkdownImages), while
  // the editor shows the inline data URL.
  const insertImageFile = useCallback(async (file: File) => {
    try {
      const saved = await onSaveImage(file);
      imagePathByDataURLRef.current.set(saved.dataURL, saved.path);
      imageDataURLByPathRef.current.set(saved.path, saved.dataURL);
      const editor = editorRef.current;
      if (editor) {
        editor.focus();
        editor.insertMarkdown(`![${file.name.replace(/\.[^./\\]+$/, '')}](${saved.dataURL})`);
      }
    } catch {
      // Leave the document unchanged if the upload fails.
    }
  }, [onSaveImage]);

  const contentFromMarkdown = useCallback((markdown: string) => {
    const restored = restoreMarkdownImages(markdown, imagePathByDataURLRef.current);
    return joinOKFDocument({frontmatter, body: restored}, restored);
  }, [frontmatter]);

  const contentFromParts = useCallback((nextFrontmatter: string, markdown: string) => {
    const restored = restoreMarkdownImages(markdown, imagePathByDataURLRef.current);
    return joinOKFDocument({frontmatter: nextFrontmatter, body: restored}, restored);
  }, []);

  useImperativeHandle(ref, () => ({
    currentContent: () => contentFromMarkdown(editorRef.current?.getMarkdown() ?? editorMarkdown),
    insertNoteLink: (id: string, label: string) => {
      const editor = editorRef.current;
      if (!editor) {
        return;
      }
      editor.focus();
      editor.insertMarkdown(`[${label}](/${id})`);
    },
    getSelectionText: () => editorRef.current?.getSelectionMarkdown() ?? '',
  }), [contentFromMarkdown, editorMarkdown]);

  useEffect(() => {
    if (content === lastEmittedContentRef.current) {
      return;
    }

    const requestID = loadRequestRef.current + 1;
    loadRequestRef.current = requestID;
    const document = splitOKFDocument(content);
    setFrontmatter(document.frontmatter);

    void hydrateMarkdownImages(noteID, document.body, imageDataURLByPathRef.current).then(({markdown, imagePathByDataURL, imageDataURLByPath}) => {
      if (loadRequestRef.current !== requestID) {
        return;
      }
      imagePathByDataURLRef.current = imagePathByDataURL;
      imageDataURLByPathRef.current = imageDataURLByPath;
      setEditorMarkdown(markdown);
      editorRef.current?.setMarkdown(markdown);
    });
  }, [content, noteID]);

  const plugins = useMemo(() => [
    toolbarPlugin({
      toolbarClassName: 'mdx-note-toolbar',
      toolbarContents: () => (
        <>
          <BlockTypeSelect />
          <BoldItalicUnderlineToggles options={['Bold', 'Italic', 'Underline']} />
          <Separator />
          <ButtonWithTooltip title="Link to a note (⌘L)" onClick={() => onRequestLinkRef.current?.()}>
            <LinkIcon size={18} />
          </ButtonWithTooltip>
          <ButtonWithTooltip title="Insert image" onClick={() => imageInputRef.current?.click()}>
            <ImageIcon size={18} />
          </ButtonWithTooltip>
          <InsertTable />
        </>
      ),
    }),
    headingsPlugin(),
    listsPlugin(),
    quotePlugin(),
    thematicBreakPlugin(),
    linkPlugin(),
    tablePlugin(),
    codeBlockPlugin({defaultCodeBlockLanguage: 'txt'}),
    codeMirrorPlugin({codeBlockLanguages: {go: 'Go', js: 'JavaScript', json: 'JSON', md: 'Markdown', sql: 'SQL', ts: 'TypeScript', txt: 'Plain text', yaml: 'YAML'}}),
    imagePlugin({
      imageUploadHandler: async (file: File) => {
        const saved = await onSaveImage(file);
        imagePathByDataURLRef.current.set(saved.dataURL, saved.path);
        imageDataURLByPathRef.current.set(saved.path, saved.dataURL);
        return saved.dataURL;
      },
    }),
    markdownShortcutPlugin(),
  ], [onSaveImage]);

  const handleChange = useCallback((nextMarkdown: string) => {
    const nextContent = contentFromMarkdown(nextMarkdown);
    lastEmittedContentRef.current = nextContent;
    onChange(nextContent);
  }, [contentFromMarkdown, onChange]);

  const handleFrontmatterChange = useCallback((nextFrontmatter: string) => {
    setFrontmatter(nextFrontmatter);
    const nextContent = contentFromParts(nextFrontmatter, editorRef.current?.getMarkdown() ?? editorMarkdown);
    lastEmittedContentRef.current = nextContent;
    onChange(nextContent);
  }, [contentFromParts, editorMarkdown, onChange]);

  const handleClickCapture = useCallback((event: MouseEvent<HTMLElement>) => {
    const anchor = (event.target as HTMLElement).closest('a');
    if (!anchor) {
      return;
    }
    const linkedNoteID = markdownHrefToNoteID(anchor.getAttribute('href') || '');
    if (!linkedNoteID) {
      return;
    }
    event.preventDefault();
    onNavigate(linkedNoteID);
  }, [onNavigate]);

  return (
    <article className="markdown-viewer mdx-note-editor" aria-label="Note editor" onClickCapture={handleClickCapture}>
      <input
        ref={imageInputRef}
        type="file"
        accept="image/*"
        style={{display: 'none'}}
        onChange={(event) => {
          const file = event.target.files?.[0];
          event.target.value = '';
          if (file) {
            void insertImageFile(file);
          }
        }}
      />
      {frontmatter && (
        <details className="okf-metadata" open>
          <summary>OKF metadata</summary>
          <MetadataFields frontmatter={frontmatter} onChange={handleFrontmatterChange} />
        </details>
      )}
      <MDXEditor
        ref={editorRef}
        markdown={editorMarkdown}
        onChange={handleChange}
        plugins={plugins}
        className={theme === 'dark' ? 'dark-theme' : undefined}
        contentEditableClassName="mdx-note-content"
      />
    </article>
  );
});

type MetadataLine =
  | {kind: 'field'; key: string; value: string}
  | {kind: 'raw'; text: string};

function MetadataFields({frontmatter, onChange}: {frontmatter: string; onChange: (frontmatter: string) => void}) {
  const lines = useMemo(() => parseMetadataLines(frontmatter), [frontmatter]);

  const updateField = (index: number, value: string) => {
    const next = lines.map((line, lineIndex) => {
      if (lineIndex !== index) {
        return serializeMetadataLine(line);
      }
      if (line.kind === 'field') {
        return serializeMetadataLine({...line, value});
      }
      return value;
    }).join('\n');
    onChange(next);
  };

  return (
    <div className="okf-metadata-fields">
      {lines.map((line, index) => line.kind === 'field' ? (
        <label className="okf-metadata-row" key={`${line.key}-${index}`}>
          <span className="okf-metadata-key">{line.key}</span>
          <input
            value={line.value}
            onChange={(event: ChangeEvent<HTMLInputElement>) => updateField(index, event.target.value)}
            spellCheck={false}
          />
        </label>
      ) : (
        <label className="okf-metadata-row okf-metadata-row-raw" key={`raw-${index}`}>
          <span className="okf-metadata-key">raw</span>
          <input
            value={line.text}
            onChange={(event: ChangeEvent<HTMLInputElement>) => updateField(index, event.target.value)}
            spellCheck={false}
          />
        </label>
      ))}
    </div>
  );
}

function parseMetadataLines(frontmatter: string): MetadataLine[] {
  const physicalLines = frontmatter.split('\n');
  const lines: MetadataLine[] = [];
  for (let index = 0; index < physicalLines.length; index += 1) {
    const line = physicalLines[index];
    const match = /^([A-Za-z0-9_-]+):\s*(.*)$/.exec(line);
    if (!match) {
      lines.push({kind: 'raw', text: line});
      continue;
    }

    const key = match[1];
    if (key.toLocaleLowerCase() !== 'tags') {
      lines.push({kind: 'field', key, value: match[2]});
      continue;
    }

    const tags = parseInlineTags(match[2]);
    while (index + 1 < physicalLines.length) {
      const item = /^\s+-\s+(.*?)\s*$/.exec(physicalLines[index + 1]);
      if (!item) {
        break;
      }
      tags.push(unquoteYAMLScalar(item[1]));
      index += 1;
    }
    lines.push({kind: 'field', key, value: tags.filter(Boolean).join(', ')});
  }
  return lines;
}

function serializeMetadataLine(line: MetadataLine): string {
  if (line.kind !== 'field') {
    return line.text;
  }
  // The backend accepts a comma-delimited scalar as well as a YAML sequence.
  // Quoting keeps the YAML valid while the user is midway through typing a
  // comma, colon, hash, or other YAML-significant character.
  if (line.key.toLocaleLowerCase() === 'tags') {
    return `${line.key}: ${JSON.stringify(line.value)}`;
  }
  return `${line.key}: ${line.value}`;
}

function parseInlineTags(value: string): string[] {
  const trimmed = value.trim();
  if (!trimmed) {
    return [];
  }
  if (trimmed.startsWith('[') && trimmed.endsWith(']')) {
    try {
      const parsed = JSON.parse(trimmed);
      if (Array.isArray(parsed)) {
        return parsed.map((item) => String(item).trim()).filter(Boolean);
      }
    } catch {
      return trimmed.slice(1, -1).split(',').map(unquoteYAMLScalar).filter(Boolean);
    }
  }
  return [unquoteYAMLScalar(trimmed)];
}

function unquoteYAMLScalar(value: string): string {
  const trimmed = value.trim();
  if (trimmed.startsWith('"') && trimmed.endsWith('"')) {
    try {
      return String(JSON.parse(trimmed));
    } catch {
      // Keep malformed/incomplete input editable rather than discarding it.
    }
  }
  if (trimmed.startsWith("'") && trimmed.endsWith("'")) {
    return trimmed.slice(1, -1).replaceAll("''", "'");
  }
  return trimmed;
}

function splitOKFDocument(content: string): {frontmatter: string; body: string} {
  const normalized = content.replace(/\r\n/g, '\n');
  if (!normalized.startsWith('---\n')) {
    return {frontmatter: '', body: normalized};
  }
  const end = normalized.indexOf('\n---', 4);
  if (end < 0) {
    return {frontmatter: '', body: normalized};
  }
  const afterFence = normalized.indexOf('\n', end + 4);
  return {
    frontmatter: normalized.slice(4, end).trim(),
    body: afterFence >= 0 ? normalized.slice(afterFence + 1) : '',
  };
}

function joinOKFDocument(document: {frontmatter: string; body: string}, body: string): string {
  return document.frontmatter ? `---\n${document.frontmatter}\n---\n\n${body}` : body;
}

async function hydrateMarkdownImages(noteID: string, markdown: string, cachedImageDataURLByPath: Map<string, string>): Promise<{markdown: string; imagePathByDataURL: Map<string, string>; imageDataURLByPath: Map<string, string>}> {
  const imagePathByDataURL = new Map<string, string>();
  const imageDataURLByPath = new Map(cachedImageDataURLByPath);
  const replacements = new Map<string, string>();
  const imagePattern = /!\[([^\]]*)\]\(([^)]+)\)/g;
  const sources = Array.from(markdown.matchAll(imagePattern)).map((match) => match[2].trim()).filter(isLocalImagePath);

  for (const source of Array.from(new Set(sources))) {
    const cached = imageDataURLByPath.get(source);
    if (cached) {
      replacements.set(source, cached);
      imagePathByDataURL.set(cached, source);
      continue;
    }
    try {
      const dataURL = await LoadNoteAssetDataURL({noteId: noteID, path: source});
      replacements.set(source, dataURL);
      imagePathByDataURL.set(dataURL, source);
      imageDataURLByPath.set(source, dataURL);
    } catch {
      // Leave broken local image references untouched so the markdown remains recoverable.
    }
  }

  if (replacements.size === 0) {
    return {markdown, imagePathByDataURL, imageDataURLByPath};
  }

  return {
    markdown: markdown.replace(imagePattern, (full, alt: string, source: string) => {
      const dataURL = replacements.get(source.trim());
      return dataURL ? `![${alt}](${dataURL})` : full;
    }),
    imagePathByDataURL,
    imageDataURLByPath,
  };
}

function restoreMarkdownImages(markdown: string, imagePathByDataURL: Map<string, string>): string {
  if (imagePathByDataURL.size === 0) {
    return markdown;
  }

  let restored = markdown.replace(/!\[([^\]]*)\]\((data:[^)]+)\)/g, (full, alt: string, dataURL: string) => {
    const path = imagePathByDataURL.get(dataURL.trim());
    return path ? `![${alt}](${path})` : full;
  });

  for (const [dataURL, path] of imagePathByDataURL.entries()) {
    restored = restored.replaceAll(`src="${dataURL}"`, `src="${path}"`);
  }
  return restored;
}

function isLocalImagePath(source: string): boolean {
  return Boolean(source) && !/^(?:[a-z][a-z0-9+.-]*:|\/\/|#)/i.test(source);
}

function markdownHrefToNoteID(href: string): string {
  const withoutHash = href.split('#')[0].trim();
  if (!withoutHash || /^(?:https?:|mailto:|data:|blob:)/i.test(withoutHash)) {
    return '';
  }
  return decodeURIComponent(withoutHash);
}

export default MdxNoteEditor;
