// Lightweight Markdown -> block renderer implementing the design spec's reading
// typography: serif H1/lead, H2 (with scroll-spy anchors), tables, numbered
// steps, bulleted lists, blockquote callouts, and inline **bold** / `code` /
// [[wikilink]] handling. Wikilink clicks route through onNavigate(slug).
import {Fragment, useEffect, useState} from 'react';
import type {ReactNode} from 'react';
import {BulbIcon} from './icons';
import {MermaidDiagram} from './MermaidDiagram';
import {LoadNoteAssetDataURL} from '../transport';

export type OutlineEntry = {anchor: string; text: string};

type ArticleModel = {
  frontmatter: Record<string, string>;
  title: string;
  lead: string;
  blocks: Block[];
  outline: OutlineEntry[];
  wordCount: number;
};

type Block =
  | {t: 'h2'; text: string; anchor: string}
  | {t: 'h3'; text: string}
  | {t: 'p'; text: string}
  | {t: 'table'; head: string[]; rows: string[][]}
  | {t: 'steps'; items: {text: string; subs: string[]}[]}
  | {t: 'list'; items: string[]}
  | {t: 'callout'; title: string; text: string}
  | {t: 'code'; text: string; lang: string}
  | {t: 'image'; alt: string; src: string};

export function slugify(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
}

// ---- Parsing -------------------------------------------------------------

export function parseArticle(rawContent: string, fallbackTitle: string): ArticleModel {
  const {frontmatter, body} = splitFrontmatter(rawContent || '');
  const lines = body.replace(/\r\n/g, '\n').split('\n');

  let title = '';
  let lead = '';
  const blocks: Block[] = [];
  const outline: OutlineEntry[] = [];

  let i = 0;
  // First H1 becomes the title; the first non-empty paragraph after it is the lead.
  while (i < lines.length) {
    const line = lines[i];
    const trimmed = line.trim();

    if (!trimmed) {
      i += 1;
      continue;
    }

    // Fenced code block
    if (trimmed.startsWith('```')) {
      const fence = trimmed.slice(0, 3);
      const lang = trimmed.slice(3).trim().split(/\s+/)[0].toLowerCase();
      const buf: string[] = [];
      i += 1;
      while (i < lines.length && !lines[i].trim().startsWith(fence)) {
        buf.push(lines[i]);
        i += 1;
      }
      i += 1;
      blocks.push({t: 'code', text: buf.join('\n'), lang});
      continue;
    }

    // Headings
    const heading = /^(#{1,6})\s+(.*)$/.exec(trimmed);
    if (heading) {
      const level = heading[1].length;
      const text = heading[2].trim();
      const headingImage = parseStandaloneImage(text);
      if (headingImage) {
        blocks.push({t: 'image', alt: headingImage.alt, src: headingImage.src});
        i += 1;
        continue;
      }
      if (level === 1 && !title) {
        title = text;
      } else if (level === 2) {
        const outlineText = plainInlineText(text);
        const anchor = slugify(outlineText);
        blocks.push({t: 'h2', text, anchor});
        outline.push({anchor, text: outlineText});
      } else if (level >= 3) {
        blocks.push({t: 'h3', text});
      } else {
        // Additional H1s render as H2-style sections.
        const outlineText = plainInlineText(text);
        const anchor = slugify(outlineText);
        blocks.push({t: 'h2', text, anchor});
        outline.push({anchor, text: outlineText});
      }
      i += 1;
      continue;
    }

    // Tables (GitHub-flavoured): header row, delimiter row, then body rows.
    if (trimmed.startsWith('|') && i + 1 < lines.length && /^\s*\|?[\s:|-]+\|?\s*$/.test(lines[i + 1]) && lines[i + 1].includes('-')) {
      const head = splitTableRow(trimmed);
      i += 2;
      const rows: string[][] = [];
      while (i < lines.length && lines[i].trim().startsWith('|')) {
        rows.push(splitTableRow(lines[i].trim()));
        i += 1;
      }
      blocks.push({t: 'table', head, rows});
      continue;
    }

    // Blockquote -> callout (first bold segment becomes the title).
    if (trimmed.startsWith('>')) {
      const buf: string[] = [];
      while (i < lines.length && lines[i].trim().startsWith('>')) {
        buf.push(lines[i].trim().replace(/^>\s?/, ''));
        i += 1;
      }
      const joined = buf.join(' ').trim();
      const boldTitle = /^\*\*(.+?)\*\*\s*(.*)$/.exec(joined);
      if (boldTitle) {
        blocks.push({t: 'callout', title: boldTitle[1], text: boldTitle[2]});
      } else {
        blocks.push({t: 'callout', title: 'Note', text: joined});
      }
      continue;
    }

    // Ordered list -> steps (with nested bullet sub-items).
    if (/^\d+[.)]\s+/.test(trimmed)) {
      const items: {text: string; subs: string[]}[] = [];
      while (i < lines.length && /^\d+[.)]\s+/.test(lines[i].trim())) {
        items.push({text: lines[i].trim().replace(/^\d+[.)]\s+/, ''), subs: []});
        i += 1;
        // Indented bullet sub-items belong to the previous step.
        while (i < lines.length && /^\s+[-*+]\s+/.test(lines[i]) && lines[i].trim()) {
          items[items.length - 1].subs.push(lines[i].trim().replace(/^[-*+]\s+/, ''));
          i += 1;
        }
      }
      blocks.push({t: 'steps', items});
      continue;
    }

    // Unordered list.
    if (/^[-*+]\s+/.test(trimmed)) {
      const items: string[] = [];
      while (i < lines.length && /^[-*+]\s+/.test(lines[i].trim())) {
        items.push(lines[i].trim().replace(/^[-*+]\s+/, ''));
        i += 1;
      }
      blocks.push({t: 'list', items});
      continue;
    }

    // Standalone image -> image block (asset resolved by the renderer).
    const image = parseStandaloneImage(trimmed);
    if (image) {
      blocks.push({t: 'image', alt: image.alt, src: image.src});
      i += 1;
      continue;
    }

    // Paragraph (gather consecutive non-empty, non-structural lines).
    const buf: string[] = [];
    while (i < lines.length && lines[i].trim() && !isStructural(lines[i].trim())) {
      buf.push(lines[i].trim());
      i += 1;
    }
    const paragraph = buf.join(' ').trim();
    if (paragraph) {
      if (!lead && title && blocks.length === 0) {
        lead = paragraph;
      } else {
        blocks.push({t: 'p', text: paragraph});
      }
    }
  }

  const resolvedTitle = title || frontmatter.title || fallbackTitle;
  return {
    frontmatter,
    title: resolvedTitle,
    lead,
    blocks,
    outline,
    wordCount: countWords(lead, blocks),
  };
}

function isStructural(line: string): boolean {
  return (
    /^#{1,6}\s+/.test(line) ||
    line.startsWith('|') ||
    line.startsWith('>') ||
    line.startsWith('```') ||
    /^\d+[.)]\s+/.test(line) ||
    /^[-*+]\s+/.test(line) ||
    Boolean(parseStandaloneImage(line))
  );
}

function parseStandaloneImage(line: string): {alt: string; src: string} | null {
  const image = /^\\?!\[([^\]]*)\]\(([^)]+)\)\s*$/.exec(line.trim());
  return image ? {alt: image[1].trim(), src: image[2].trim()} : null;
}

function splitTableRow(row: string): string[] {
  return row
    .replace(/^\|/, '')
    .replace(/\|$/, '')
    .split('|')
    .map((cell) => cell.trim());
}

function splitFrontmatter(content: string): {frontmatter: Record<string, string>; body: string} {
  const normalized = content.replace(/\r\n/g, '\n');
  if (!normalized.startsWith('---\n')) {
    return {frontmatter: {}, body: normalized};
  }
  const end = normalized.indexOf('\n---', 4);
  if (end < 0) {
    return {frontmatter: {}, body: normalized};
  }
  const block = normalized.slice(4, end);
  const afterFence = normalized.indexOf('\n', end + 4);
  const body = afterFence >= 0 ? normalized.slice(afterFence + 1) : '';
  const frontmatter: Record<string, string> = {};
  for (const line of block.split('\n')) {
    const match = /^([a-zA-Z0-9_-]+):\s*(.*)$/.exec(line);
    if (match) {
      frontmatter[match[1]] = match[2].replace(/^["']|["']$/g, '').trim();
    }
  }
  return {frontmatter, body};
}

function countWords(lead: string, blocks: Block[]): number {
  let n = 0;
  const add = (s?: string) => {
    if (!s) {
      return;
    }
    n += s.replace(/[*`[\]|>#-]/g, ' ').trim().split(/\s+/).filter(Boolean).length;
  };
  add(lead);
  for (const block of blocks) {
    if (block.t === 'h2' || block.t === 'h3' || block.t === 'p') {
      add(block.text);
    } else if (block.t === 'callout') {
      add(block.title);
      add(block.text);
    } else if (block.t === 'code') {
      add(block.text);
    } else if (block.t === 'table') {
      block.rows.forEach((row) => row.forEach(add));
    } else if (block.t === 'steps') {
      block.items.forEach((item) => {
        add(item.text);
        item.subs.forEach(add);
      });
    } else if (block.t === 'list') {
      block.items.forEach(add);
    }
  }
  return n;
}

function plainInlineText(text: string): string {
  return text
    .replace(/!\[([^\]]*)\]\(([^)]+)\)/g, '$1')
    .replace(/\[([^\]]+)\]\(([^)]+)\)/g, '$1')
    .replace(/\[\[([^\]|]+)(?:\|([^\]]+))?\]\]/g, (_full, target: string, label?: string) => label || target)
    .replace(/\*\*(.+?)\*\*/g, '$1')
    .replace(/\*([^*\n]+?)\*/g, '$1')
    .replace(/`([^`]+?)`/g, '$1')
    .trim();
}

// ---- Inline rendering ----------------------------------------------------

// Matches, in order: **bold**, `code`, [[wikilink]] (with optional |label),
// [label](href), *italic*. Module-scoped (compiled once); renderInline resets
// lastIndex before each scan since it carries the global flag.
const INLINE_PATTERN =
  /\*\*(.+?)\*\*|`([^`]+?)`|\[\[([^\]|]+?)(?:\|([^\]]+?))?\]\]|\[([^\]]+?)\]\(([^)]+?)\)|\*([^*\n]+?)\*/g;

function renderInline(text: string, onNavigate: (id: string) => void, keyPrefix: string): ReactNode[] {
  const out: ReactNode[] = [];
  const re = INLINE_PATTERN;
  re.lastIndex = 0;
  let last = 0;
  let key = 0;
  let match: RegExpExecArray | null;
  while ((match = re.exec(text)) !== null) {
    if (match.index > last) {
      out.push(<Fragment key={`${keyPrefix}-t${key++}`}>{text.slice(last, match.index)}</Fragment>);
    }
    if (match[1] != null) {
      out.push(<strong key={`${keyPrefix}-b${key++}`}>{match[1]}</strong>);
    } else if (match[2] != null) {
      out.push(<code key={`${keyPrefix}-c${key++}`} className="gm-inline-code">{match[2]}</code>);
    } else if (match[3] != null) {
      const target = match[3].trim();
      const label = (match[4] || match[3]).trim();
      out.push(
        <a
          key={`${keyPrefix}-w${key++}`}
          className="gm-wikilink"
          href="#"
          onClick={(event) => {
            event.preventDefault();
            onNavigate(target);
          }}
        >
          {label}
        </a>,
      );
    } else if (match[5] != null && match[6] != null) {
      const href = match[6].trim();
      const label = match[5].trim();
      const isExternal = /^[a-z][a-z0-9+.-]*:/i.test(href) || href.startsWith('#');
      if (isExternal) {
        out.push(
          <a key={`${keyPrefix}-l${key++}`} href={href} target="_blank" rel="noreferrer">
            {label}
          </a>,
        );
      } else {
        out.push(
          <a
            key={`${keyPrefix}-l${key++}`}
            className="gm-wikilink"
            href="#"
            onClick={(event) => {
              event.preventDefault();
              onNavigate(href.replace(/\.md$/i, ''));
            }}
          >
            {label}
          </a>,
        );
      }
    } else if (match[7] != null) {
      out.push(<em key={`${keyPrefix}-i${key++}`}>{match[7]}</em>);
    }
    last = re.lastIndex;
  }
  if (last < text.length) {
    out.push(<Fragment key={`${keyPrefix}-t${key++}`}>{text.slice(last)}</Fragment>);
  }
  return out;
}

// ---- Obsolescence --------------------------------------------------------

type ObsoleteInfo = {obsolete: boolean; supersededBy: string; reason: string};

// obsoleteInfo decides whether a note is marked obsolete and extracts an optional
// replacement id and reason. A note is obsolete when it carries a truthy
// `obsolete` frontmatter field (e.g. `obsolete: true`) or a reserved `obsolete`
// tag. `superseded_by` names the replacement note; a non-boolean `obsolete:` value
// (or `obsolete_reason`) is shown as the reason.
function obsoleteInfo(frontmatter: Record<string, string>, tags: string[]): ObsoleteInfo {
  const hasKey = Object.prototype.hasOwnProperty.call(frontmatter, 'obsolete');
  const rawVal = (frontmatter.obsolete ?? '').trim();
  const val = rawVal.toLowerCase();
  const falsey = val === 'false' || val === 'no' || val === '0';
  const tagged = tags.some((t) => t.toLowerCase() === 'obsolete');
  const obsolete = tagged || (hasKey && !falsey);

  const supersededBy = (frontmatter.superseded_by || frontmatter['superseded-by'] || '').trim();
  const explicitReason = (frontmatter.obsolete_reason || frontmatter['obsolete-reason'] || '').trim();
  // If the `obsolete` value isn't just a boolean word, treat it as the reason.
  const isBoolWord = val === '' || val === 'true' || val === 'yes' || val === '1' || falsey;
  const reason = explicitReason || (hasKey && !isBoolWord ? rawVal : '');
  return {obsolete, supersededBy, reason};
}

// ---- Images --------------------------------------------------------------

// AssetImage resolves a note-local asset path (e.g. assets/<id>/pic.svg) to a
// data URL via the same asset API the editor uses, then renders it. External or
// already-inlined (http/data) srcs render directly.
function AssetImage({noteID, src, alt}: {noteID: string; src: string; alt: string}) {
  const isExternal = /^(?:[a-z][a-z0-9+.-]*:|\/\/|data:)/i.test(src);
  const [resolved, setResolved] = useState<string>(isExternal ? src : '');
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    if (isExternal || !noteID) {
      return;
    }
    let cancelled = false;
    setResolved('');
    setFailed(false);
    LoadNoteAssetDataURL({noteId: noteID, path: src})
      .then((dataURL) => {
        if (!cancelled) {
          setResolved(dataURL);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setFailed(true);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [noteID, src, isExternal]);

  if (failed) {
    return <div className="gm-image-broken">Image not found: {src}</div>;
  }
  if (!resolved) {
    return <div className="gm-image-loading" aria-busy="true" />;
  }
  return (
    <figure className="gm-image">
      <img src={resolved} alt={alt} loading="lazy" />
    </figure>
  );
}

// ---- Component -----------------------------------------------------------

type MarkdownArticleProps = {
  model: ArticleModel;
  tags: string[];
  noteID: string;
  onNavigate: (id: string) => void;
  theme?: 'light' | 'dark';
};

export function MarkdownArticle({model, tags, noteID, onNavigate, theme = 'light'}: MarkdownArticleProps) {
  const obsolete = obsoleteInfo(model.frontmatter, tags);
  return (
    <article className="gm-article">
      {tags.length > 0 && (
        <div className="gm-article-tags">
          {tags.map((tag) => (
            <span className="gm-tag" key={tag}>#{tag}</span>
          ))}
        </div>
      )}
      {obsolete.obsolete && (
        <div className="gm-obsolete-banner" role="alert">
          <span className="gm-obsolete-badge">Obsolete</span>
          <span className="gm-obsolete-text">
            This note is marked obsolete and may be out of date.
            {obsolete.reason && <> {obsolete.reason}</>}
            {obsolete.supersededBy && (
              <>
                {' '}Superseded by{' '}
                <a
                  className="gm-wikilink"
                  href="#"
                  onClick={(event) => {
                    event.preventDefault();
                    onNavigate(obsolete.supersededBy.replace(/\.md$/i, ''));
                  }}
                >
                  {obsolete.supersededBy}
                </a>
                .
              </>
            )}
          </span>
        </div>
      )}
      <h1 className="gm-article-title">{renderInline(model.title, onNavigate, 'title')}</h1>
      {model.lead && <p className="gm-article-lead">{renderInline(model.lead, onNavigate, 'lead')}</p>}

      {model.blocks.map((block, index) => {
        const key = `block-${index}`;
        switch (block.t) {
          case 'h2':
            return (
              <h2 className="gm-h2" data-anchor={block.anchor} key={key}>
                {renderInline(block.text, onNavigate, key)}
              </h2>
            );
          case 'h3':
            return (
              <h3 className="gm-h3" key={key}>
                {renderInline(block.text, onNavigate, key)}
              </h3>
            );
          case 'p':
            return (
              <p className="gm-p" key={key}>
                {renderInline(block.text, onNavigate, key)}
              </p>
            );
          case 'code':
            if (block.lang === 'mermaid') {
              return <MermaidDiagram code={block.text} theme={theme} key={key} />;
            }
            return (
              <pre className="gm-code-block" key={key}>
                <code>{block.text}</code>
              </pre>
            );
          case 'image':
            return <AssetImage key={key} noteID={noteID} src={block.src} alt={block.alt} />;
          case 'table':
            return (
              <div className="gm-table-card" key={key}>
                <table className="gm-table">
                  <thead>
                    <tr>
                      {block.head.map((cell, ci) => (
                        <th className={ci === 0 ? 'gm-th' : 'gm-th gm-th-num'} key={ci}>
                          {cell}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {block.rows.map((row, ri) => (
                      <tr key={ri}>
                        {row.map((cell, ci) => (
                          <td className={ci === 0 ? 'gm-td' : 'gm-td gm-td-num'} key={ci}>
                            {renderInline(cell, onNavigate, `${key}-${ri}-${ci}`)}
                          </td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            );
          case 'steps':
            return (
              <div className="gm-steps" key={key}>
                {block.items.map((item, si) => (
                  <div className="gm-step" key={si}>
                    <span className="gm-step-num">{si + 1}</span>
                    <div className="gm-step-body">
                      <div className="gm-step-text">{renderInline(item.text, onNavigate, `${key}-${si}`)}</div>
                      {item.subs.length > 0 && (
                        <div className="gm-substeps">
                          {item.subs.map((sub, ssi) => (
                            <div className="gm-substep" key={ssi}>
                              <span className="gm-substep-dot" />
                              <span className="gm-substep-text">{renderInline(sub, onNavigate, `${key}-${si}-${ssi}`)}</span>
                            </div>
                          ))}
                        </div>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            );
          case 'list':
            return (
              <div className="gm-list" key={key}>
                {block.items.map((item, li) => (
                  <div className="gm-list-item" key={li}>
                    <span className="gm-list-dot" />
                    <span className="gm-list-text">{renderInline(item, onNavigate, `${key}-${li}`)}</span>
                  </div>
                ))}
              </div>
            );
          case 'callout':
            return (
              <div className="gm-callout" key={key}>
                <BulbIcon stroke="var(--accent-text)" />
                <div>
                  <div className="gm-callout-title">{block.title}</div>
                  <div className="gm-callout-body">{renderInline(block.text, onNavigate, key)}</div>
                </div>
              </div>
            );
          default:
            return null;
        }
      })}
    </article>
  );
}

export default MarkdownArticle;
