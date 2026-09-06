// Run with: node scripts/run-tests.mjs
import assert from 'node:assert/strict'
import {filtersHidingNote, facetMatchesNote, anyFacetActive, folderOf} from './match'
import type {FacetFilter} from './types'

const noFacets: FacetFilter = {types: [], tags: [], folders: [], favorites: false}
const note = (over: Record<string, unknown> = {}) =>
  ({id: 'new-note', title: 'New note', path: 'new-note.md', type: 'general', tags: [], favorite: false, ...over}) as any

export const tests: Record<string, () => void> = {
  'folderOf reads the top segment'() {
    assert.equal(folderOf(''), 'root')
    assert.equal(folderOf('note.md'), 'root')
    assert.equal(folderOf('projects/note.md'), 'projects')
    assert.equal(folderOf('projects\\note.md'), 'projects')
  },

  'no filters hide nothing'() {
    assert.deepEqual(filtersHidingNote('', noFacets, note()), {search: false, facets: false})
    assert.deepEqual(filtersHidingNote('   ', noFacets, note()), {search: false, facets: false})
  },

  'an active search always hides a brand new note'() {
    // The search index is asynchronous, so even a note whose text would match is
    // absent from the hits currently on screen.
    const hiding = filtersHidingNote('kubernetes', noFacets, note({title: 'Kubernetes'}))
    assert.equal(hiding.search, true)
  },

  'a tag facet hides an untagged new note'() {
    const facets: FacetFilter = {...noFacets, tags: ['kubernetes']}
    assert.deepEqual(filtersHidingNote('', facets, note()), {search: false, facets: true})
  },

  'a tag facet the note satisfies is left alone'() {
    const facets: FacetFilter = {...noFacets, tags: ['kubernetes']}
    assert.deepEqual(filtersHidingNote('', facets, note({tags: ['kubernetes']})), {search: false, facets: false})
  },

  'a type facet the note misses hides it'() {
    const facets: FacetFilter = {...noFacets, types: ['recipe']}
    assert.equal(filtersHidingNote('', facets, note({type: 'general'})).facets, true)
    assert.equal(filtersHidingNote('', facets, note({type: 'recipe'})).facets, false)
  },

  'a folder facet is judged on the new path'() {
    const facets: FacetFilter = {...noFacets, folders: ['projects']}
    assert.equal(filtersHidingNote('', facets, note({path: 'inbox/new-note.md'})).facets, true)
    assert.equal(filtersHidingNote('', facets, note({path: 'projects/new-note.md'})).facets, false)
  },

  'a favorites filter hides a new note, which is never a favorite'() {
    const facets: FacetFilter = {...noFacets, favorites: true}
    assert.equal(filtersHidingNote('', facets, note()).facets, true)
  },

  'a missing summary counts as hidden while facets are on'() {
    // loadNotes races the projection, so the new note can be absent from the
    // list it returns. Clearing is the safe answer: the alternative is an empty
    // sidebar with no explanation.
    const facets: FacetFilter = {...noFacets, tags: ['kubernetes']}
    assert.equal(filtersHidingNote('', facets, undefined).facets, true)
    assert.equal(filtersHidingNote('', noFacets, undefined).facets, false)
  },

  'facet axes are ANDed'() {
    const facets: FacetFilter = {...noFacets, types: ['general'], tags: ['kubernetes']}
    assert.equal(facetMatchesNote(note({type: 'general', tags: []}), facets), false)
    assert.equal(facetMatchesNote(note({type: 'general', tags: ['kubernetes']}), facets), true)
    assert.equal(anyFacetActive(facets), true)
    assert.equal(anyFacetActive(noFacets), false)
  },
}
