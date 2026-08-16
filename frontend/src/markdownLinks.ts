// Canonicalize external links that rich-text editors otherwise split around
// SharePoint-style parentheses or redundant URL titles.
export function normalizeMarkdownLinksForEditor(markdown: string): string {
  let output = '';
  let inlineCodeTicks = 0;
  let fencedCodeMarker = '';

  for (let index = 0; index < markdown.length; index += 1) {
    const atLineStart = index === 0 || markdown[index - 1] === '\n';
    const fence = atLineStart ? /^ {0,3}(`{3,}|~{3,})/.exec(markdown.slice(index)) : null;
    if (fence) {
      const marker = fence[1][0];
      if (!fencedCodeMarker) {
        fencedCodeMarker = marker;
      } else if (fencedCodeMarker === marker) {
        fencedCodeMarker = '';
      }
      output += fence[0];
      index += fence[0].length - 1;
      continue;
    }

    if (fencedCodeMarker) {
      output += markdown[index];
      continue;
    }

    if (markdown[index] === '`') {
      let end = index + 1;
      while (markdown[end] === '`') {
        end += 1;
      }
      const ticks = end - index;
      if (inlineCodeTicks === 0) {
        inlineCodeTicks = ticks;
      } else if (inlineCodeTicks === ticks) {
        inlineCodeTicks = 0;
      }
      output += markdown.slice(index, end);
      index = end - 1;
      continue;
    }

    if (inlineCodeTicks === 0 && markdown[index] === ']' && markdown[index + 1] === '(') {
      const link = normalizeLinkAt(markdown, index + 2);
      if (link) {
        output += `](${link.content})`;
        index = link.end;
        continue;
      }
    }

    output += markdown[index];
  }

  return output;
}

function normalizeLinkAt(markdown: string, start: number): {content: string; end: number} | null {
  let nestedParentheses = 0;
  let quote = '';
  let end = start;

  for (; end < markdown.length; end += 1) {
    const character = markdown[end];
    if (character === '\\') {
      end += 1;
      continue;
    }
    if (quote) {
      if (character === quote) {
        quote = '';
      }
      continue;
    }
    if ((character === '"' || character === "'") && (end === start || /\s/.test(markdown[end - 1]))) {
      quote = character;
      continue;
    }
    if (character === '(') {
      nestedParentheses += 1;
      continue;
    }
    if (character === ')') {
      if (nestedParentheses === 0) {
        break;
      }
      nestedParentheses -= 1;
    }
  }

  if (end === markdown.length) {
    return null;
  }

  const original = markdown.slice(start, end);
  const normalized = normalizeExternalLinkContent(original);
  return normalized === original ? null : {content: normalized, end};
}

function normalizeExternalLinkContent(content: string): string {
  const parts = /^(\s*)(\S+)([\s\S]*?)(\s*)$/.exec(content);
  if (!parts) {
    return content;
  }

  const [, leading, rawDestination, rawTitle, trailing] = parts;
  let destination = rawDestination;
  let title = rawTitle.trim();
  const duplicateWithoutDelimiter = /^(.+)"(.+)"$/.exec(destination);
  if (duplicateWithoutDelimiter && duplicateWithoutDelimiter[1] === duplicateWithoutDelimiter[2]) {
    destination = duplicateWithoutDelimiter[1];
    title = '';
  }

  if (!/^https?:\/\//i.test(destination)) {
    return content;
  }

  const titleMatch = /^(["'])([\s\S]*)\1$/.exec(title);
  if (titleMatch && titleMatch[2] === destination) {
    title = '';
  }

  const normalizedDestination = /[()]/.test(destination) ? `<${destination}>` : destination;
  const normalizedTitle = title ? ` ${title}` : '';
  return `${leading}${normalizedDestination}${normalizedTitle}${trailing}`;
}
