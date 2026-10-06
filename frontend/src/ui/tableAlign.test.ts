import {alignOf, parseTableAlignment} from './tableAlign';

function eq(actual: unknown, expected: unknown, label: string) {
  const a = JSON.stringify(actual);
  const b = JSON.stringify(expected);
  if (a !== b) {
    throw new Error(`${label}: expected ${b}, got ${a}`);
  }
}

export const tests: Record<string, () => void> = {
  'bare dashes default to left': () => {
    eq(parseTableAlignment('|---|---|---|'), ['left', 'left', 'left'], 'bare');
  },

  'trailing colon is right': () => {
    eq(parseTableAlignment('|---:|'), ['right'], 'right');
  },

  'leading colon is left': () => {
    eq(parseTableAlignment('|:---|'), ['left'], 'left');
  },

  'colons on both sides are centre': () => {
    eq(parseTableAlignment('|:---:|'), ['center'], 'center');
  },

  'mixed row maps each column independently': () => {
    eq(
      parseTableAlignment('| :--- | ---: | :---: | --- |'),
      ['left', 'right', 'center', 'left'],
      'mixed',
    );
  },

  'tolerates missing outer pipes': () => {
    eq(parseTableAlignment('--- | ---:'), ['left', 'right'], 'no outer pipes');
  },

  'tolerates padding whitespace': () => {
    eq(parseTableAlignment('|   ---:   |   :---   |'), ['right', 'left'], 'padded');
  },

  'single-dash columns still parse': () => {
    eq(parseTableAlignment('|-|-:|'), ['left', 'right'], 'short');
  },

  // A single ':' is degenerate but startsWith and endsWith are both true, so it
  // reads as centre rather than throwing.
  'lone colon reads as centre': () => {
    eq(parseTableAlignment('|:|'), ['center'], 'lone colon');
  },

  'alignOf falls back to left past the end': () => {
    const align = parseTableAlignment('|---:|');
    eq(alignOf(align, 0), 'right', 'in range');
    eq(alignOf(align, 1), 'left', 'past end');
    eq(alignOf(align, 99), 'left', 'far past end');
  },

  'alignOf handles an empty alignment list': () => {
    eq(alignOf([], 0), 'left', 'empty');
  },
};
