import {decideRefresh} from './noteRefresh';
import type {RefreshInput} from './noteRefresh';

function base(over: Partial<RefreshInput> = {}): RefreshInput {
  return {
    incomingID: 'alpha',
    selectedID: 'alpha',
    incomingVersion: 'v2',
    currentVersion: 'v1',
    incomingContent: 'disk',
    draft: 'loaded',
    savedContent: 'loaded',
    ...over,
  };
}

function eq(actual: string, expected: string, label: string) {
  if (actual !== expected) {
    throw new Error(`${label}: expected ${expected}, got ${actual}`);
  }
}

export const tests: Record<string, () => void> = {
  'an event for another note is ignored': () => {
    eq(decideRefresh(base({incomingID: 'beta'})), 'ignore', 'other note');
  },

  'an event with no id is ignored': () => {
    eq(decideRefresh(base({incomingID: ''})), 'ignore', 'empty id');
  },

  'an event while nothing is selected is ignored': () => {
    eq(decideRefresh(base({incomingID: '', selectedID: ''})), 'ignore', 'nothing selected');
  },

  'a matching version is ignored': () => {
    eq(decideRefresh(base({incomingVersion: 'v1', currentVersion: 'v1'})), 'ignore', 'same version');
  },

  'a missing version does not short-circuit': () => {
    // '' must not compare equal to a missing currentVersion and swallow the event.
    eq(decideRefresh(base({incomingVersion: '', currentVersion: ''})), 'refresh', 'no versions');
  },

  'content equal to the draft is our own save echoing back': () => {
    eq(decideRefresh(base({incomingContent: 'typed', draft: 'typed', savedContent: 'old'})), 'echo', 'echo');
  },

  'an external change with a clean buffer refreshes': () => {
    eq(decideRefresh(base({incomingContent: 'disk', draft: 'same', savedContent: 'same'})), 'refresh', 'clean');
  },

  'an external change with unsaved edits is a conflict': () => {
    eq(decideRefresh(base({incomingContent: 'disk', draft: 'typed', savedContent: 'loaded'})), 'conflict', 'dirty');
  },

  // Read mode is the case the user reported: viewing a note while something
  // else rewrites the file on disk.
  'read mode refreshes on an external change': () => {
    eq(
      decideRefresh(base({incomingContent: 'new body', draft: 'old body', savedContent: 'old body'})),
      'refresh',
      'read mode',
    );
  },

  // Having the editor open is not itself a conflict; only unsaved edits are.
  'an open editor with no edits still refreshes': () => {
    eq(
      decideRefresh(base({incomingContent: 'new body', draft: 'untouched', savedContent: 'untouched'})),
      'refresh',
      'clean editor',
    );
  },

  'echo wins over conflict when the draft already matches disk': () => {
    eq(
      decideRefresh(base({incomingContent: 'typed', draft: 'typed', savedContent: 'before'})),
      'echo',
      'echo precedence',
    );
  },

  'an empty incoming file is still applied when clean': () => {
    eq(decideRefresh(base({incomingContent: '', draft: 'x', savedContent: 'x'})), 'refresh', 'emptied file');
  },
};
