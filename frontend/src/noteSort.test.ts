import {
  groupByRecency,
  isNoteSort,
  isRecentNote,
  noteTime,
  recencyBucket,
  sortByRecency,
} from './noteSort';
import type {application} from '../wailsjs/go/models';

function note(id: string, modifiedAt: string): application.NoteSummaryDTO {
  return {id, modifiedAt} as application.NoteSummaryDTO;
}

function assert(condition: boolean, message: string): void {
  if (!condition) {
    throw new Error(message);
  }
}

function assertEqual(actual: unknown, expected: unknown, message: string): void {
  const a = JSON.stringify(actual);
  const b = JSON.stringify(expected);
  if (a !== b) {
    throw new Error(`${message}: got ${a}, want ${b}`);
  }
}

// A Wednesday, so the Monday-based week boundary is exercised mid-week.
const NOW = new Date('2026-03-11T14:30:00').getTime();

function at(local: string): string {
  return new Date(local).toISOString();
}

export const tests: Record<string, () => void> = {
  'recognises the sort modes': () => {
    assert(isNoteSort('name'), 'name is a sort');
    assert(isNoteSort('recent'), 'recent is a sort');
    assert(!isNoteSort('modified'), 'unknown values are rejected');
    assert(!isNoteSort(undefined), 'undefined is rejected');
  },

  'treats a missing timestamp as undated': () => {
    assert(Number.isNaN(noteTime('')), 'empty is undated');
    assert(Number.isNaN(noteTime(undefined)), 'absent is undated');
    assert(Number.isNaN(noteTime('not a date')), 'garbage is undated');
  },

  'buckets todays notes as today': () => {
    assertEqual(recencyBucket(at('2026-03-11T09:00:00'), NOW), 'Today', 'earlier today');
    assertEqual(recencyBucket(at('2026-03-11T00:00:00'), NOW), 'Today', 'midnight is today');
  },

  'buckets the previous calendar day as yesterday': () => {
    assertEqual(recencyBucket(at('2026-03-10T23:59:00'), NOW), 'Yesterday', 'late yesterday');
    assertEqual(recencyBucket(at('2026-03-10T00:00:00'), NOW), 'Yesterday', 'early yesterday');
  },

  'counts yesterday by the calendar, not by 24 hours': () => {
    // Two hours ago in clock terms, but on the previous calendar day.
    const lateNight = new Date('2026-03-11T01:00:00').getTime();
    assertEqual(recencyBucket(at('2026-03-10T23:00:00'), lateNight), 'Yesterday', 'two hours ago but yesterday');
  },

  'buckets earlier in the same week': () => {
    assertEqual(recencyBucket(at('2026-03-09T12:00:00'), NOW), 'This week', 'Monday of this week');
  },

  'starts the week on Monday': () => {
    // The Sunday before this Wednesday belongs to the previous week.
    assertEqual(recencyBucket(at('2026-03-08T12:00:00'), NOW), 'This month', 'Sunday is the previous week');
  },

  'keeps Sunday with the week that began on Monday': () => {
    const sunday = new Date('2026-03-15T12:00:00').getTime();
    assertEqual(recencyBucket(at('2026-03-09T12:00:00'), sunday), 'This week', 'Monday through Sunday is one week');
  },

  'buckets earlier in the same month': () => {
    assertEqual(recencyBucket(at('2026-03-02T12:00:00'), NOW), 'This month', 'start of the month');
  },

  'buckets anything older': () => {
    assertEqual(recencyBucket(at('2026-02-27T12:00:00'), NOW), 'Older', 'last month');
    assertEqual(recencyBucket(at('2019-01-01T12:00:00'), NOW), 'Older', 'years ago');
  },

  'buckets an undated note separately': () => {
    assertEqual(recencyBucket('', NOW), 'Undated', 'no stamp');
  },

  'reads a future timestamp as today': () => {
    assertEqual(recencyBucket(at('2026-03-11T23:00:00'), NOW), 'Today', 'later today');
    assertEqual(recencyBucket(at('2026-04-01T10:00:00'), NOW), 'Today', 'clock skew is not old');
  },

  'spots a note inside the recent window': () => {
    assert(isRecentNote(at('2026-03-11T14:00:00'), NOW), 'half an hour ago');
    assert(isRecentNote(at('2026-03-10T15:00:00'), NOW), 'just inside 24h');
  },

  'excludes a note outside the recent window': () => {
    assert(!isRecentNote(at('2026-03-10T13:00:00'), NOW), 'just outside 24h');
    assert(!isRecentNote('', NOW), 'undated is never recent');
  },

  'sorts newest first': () => {
    const sorted = sortByRecency([
      note('old', at('2026-01-01T00:00:00')),
      note('new', at('2026-03-11T00:00:00')),
      note('middle', at('2026-02-01T00:00:00')),
    ]);
    assertEqual(sorted.map((item) => item.id), ['new', 'middle', 'old'], 'descending by time');
  },

  'sorts undated notes last': () => {
    const sorted = sortByRecency([
      note('undated', ''),
      note('dated', at('2026-01-01T00:00:00')),
    ]);
    assertEqual(sorted.map((item) => item.id), ['dated', 'undated'], 'undated sinks');
  },

  'breaks ties by id so the order is stable': () => {
    const stamp = at('2026-03-11T10:00:00');
    const sorted = sortByRecency([note('beta', stamp), note('alpha', stamp)]);
    assertEqual(sorted.map((item) => item.id), ['alpha', 'beta'], 'same instant sorts by id');
  },

  'breaks undated ties by id too': () => {
    const sorted = sortByRecency([note('beta', ''), note('alpha', '')]);
    assertEqual(sorted.map((item) => item.id), ['alpha', 'beta'], 'undated sorts by id');
  },

  'does not disturb the caller list': () => {
    const input = [note('b', at('2026-01-01T00:00:00')), note('a', at('2026-03-01T00:00:00'))];
    sortByRecency(input);
    assertEqual(input.map((item) => item.id), ['b', 'a'], 'input untouched');
  },

  'groups into buckets newest first': () => {
    const groups = groupByRecency([
      note('ancient', at('2020-05-05T10:00:00')),
      note('today', at('2026-03-11T10:00:00')),
      note('week', at('2026-03-09T10:00:00')),
      note('yesterday', at('2026-03-10T10:00:00')),
    ], NOW);
    assertEqual(groups.map((group) => group.name), ['Today', 'Yesterday', 'This week', 'Older'], 'bucket order');
    assertEqual(groups[0].notes.map((item) => item.id), ['today'], 'today holds its note');
  },

  'orders buckets regardless of scan order': () => {
    // Oldest note seen first: insertion order would invert the buckets.
    const groups = groupByRecency([
      note('ancient', at('2001-01-01T10:00:00')),
      note('today', at('2026-03-11T10:00:00')),
    ], NOW);
    assertEqual(groups.map((group) => group.name), ['Today', 'Older'], 'canonical order wins');
  },

  'omits empty buckets': () => {
    const groups = groupByRecency([note('today', at('2026-03-11T10:00:00'))], NOW);
    assertEqual(groups.map((group) => group.name), ['Today'], 'only non-empty buckets');
  },

  'sorts within a bucket newest first': () => {
    const groups = groupByRecency([
      note('morning', at('2026-03-11T08:00:00')),
      note('noon', at('2026-03-11T12:00:00')),
    ], NOW);
    assertEqual(groups[0].notes.map((item) => item.id), ['noon', 'morning'], 'newest first inside a bucket');
  },

  'groups nothing into nothing': () => {
    assertEqual(groupByRecency([], NOW), [], 'no notes means no buckets');
  },

  'puts undated notes in a bucket of their own, last': () => {
    const groups = groupByRecency([
      note('undated', ''),
      note('today', at('2026-03-11T10:00:00')),
    ], NOW);
    assertEqual(groups.map((group) => group.name), ['Today', 'Undated'], 'undated comes last');
  },
};
