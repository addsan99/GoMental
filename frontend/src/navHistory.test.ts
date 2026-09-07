// Run with: npm test
import assert from 'node:assert/strict'
import {recordVisit, type NavHistory} from './navHistory'

const empty: NavHistory = {stack: [], index: -1}

// Replay a sequence of [id, mode] visits from empty.
const replay = (steps: Array<[string, 'push' | 'replace']>, max?: number) =>
  steps.reduce((history, [id, mode]) => recordVisit(history, id, mode, max), empty)

export const tests: Record<string, () => void> = {
  'a push appends and advances the cursor'() {
    assert.deepEqual(replay([['a', 'push']]), {stack: ['a'], index: 0})
    assert.deepEqual(replay([['a', 'push'], ['b', 'push']]), {stack: ['a', 'b'], index: 1})
  },

  'revisiting the current note is a no-op'() {
    const before = replay([['a', 'push'], ['b', 'push']])
    assert.equal(recordVisit(before, 'b', 'push'), before)
    assert.equal(recordVisit(before, 'b', 'replace'), before)
  },

  'an empty id is ignored'() {
    assert.equal(recordVisit(empty, '', 'push'), empty)
  },

  'a replace onto an empty history still pushes'() {
    // index -1 means there is nothing to overwrite.
    assert.deepEqual(replay([['a', 'replace']]), {stack: ['a'], index: 0})
  },

  'an arrow run collapses to one entry'() {
    // The run pushes once on its first step, then overwrites: Back returns to
    // where the run started, not to each note scrolled past.
    const history = replay([
      ['a', 'push'],
      ['b', 'push'],
      ['c', 'replace'],
      ['d', 'replace'],
      ['e', 'replace'],
    ])
    assert.deepEqual(history, {stack: ['a', 'e'], index: 1})
  },

  'a click after a run starts a new entry'() {
    const history = replay([
      ['a', 'push'],
      ['b', 'push'],
      ['c', 'replace'],
      ['d', 'push'],
    ])
    assert.deepEqual(history, {stack: ['a', 'c', 'd'], index: 2})
  },

  'a push from mid-history drops the forward entries'() {
    const back: NavHistory = {stack: ['a', 'b', 'c'], index: 0}
    assert.deepEqual(recordVisit(back, 'z', 'push'), {stack: ['a', 'z'], index: 1})
  },

  'a replace from mid-history also drops the forward entries'() {
    const back: NavHistory = {stack: ['a', 'b', 'c'], index: 1}
    assert.deepEqual(recordVisit(back, 'z', 'replace'), {stack: ['a', 'z'], index: 1})
  },

  'replace never grows the stack'() {
    const history = replay([['a', 'push'], ['b', 'push'], ...Array.from({length: 50}, (_, i) => ['n' + i, 'replace'] as [string, 'replace'])])
    assert.equal(history.stack.length, 2)
    assert.equal(history.stack[1], 'n49')
    assert.equal(history.index, 1)
  },

  'the stack is trimmed to the cap, keeping the newest'() {
    const history = replay(Array.from({length: 8}, (_, i) => ['n' + i, 'push'] as [string, 'push']), 3)
    assert.deepEqual(history, {stack: ['n5', 'n6', 'n7'], index: 2})
  },

  'trimming leaves the cursor on the entry just visited'() {
    const history = replay(Array.from({length: 20}, (_, i) => ['n' + i, 'push'] as [string, 'push']), 5)
    assert.equal(history.stack[history.index], 'n19')
  },
}
