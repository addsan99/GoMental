// Intent lenses: one-click presets that reshape the graph view for a purpose.
// Grounded in the real note `type` values: intro, playbook, concept, decision, policy.
import type {Lens} from './types'

export const LENSES: Lens[] = [
  {
    id: 'onboarding',
    label: 'Onboarding',
    description: 'Where a newcomer starts',
    apply: () => ({
      seed: undefined,
      facets: {types: ['intro'], tags: [], folders: []},
      layout: 'force',
      groupBy: undefined,
      includeSoftLinks: false,
    }),
  },
  {
    id: 'playbooks',
    label: 'Playbooks',
    description: 'Task-oriented guides',
    apply: () => ({
      seed: undefined,
      facets: {types: ['playbook'], tags: [], folders: []},
      layout: 'zoned',
      groupBy: 'folder',
    }),
  },
  {
    id: 'concept-map',
    label: 'Concept map',
    description: 'The knowledge landscape',
    apply: () => ({
      seed: undefined,
      facets: {types: ['concept'], tags: [], folders: []},
      layout: 'zoned',
      groupBy: 'folder',
    }),
  },
  {
    id: 'decisions',
    label: 'Decisions',
    description: 'ADRs & policies',
    apply: () => ({
      seed: undefined,
      facets: {types: ['decision', 'policy'], tags: [], folders: []},
      layout: 'force',
      groupBy: undefined,
    }),
  },
  {
    id: 'focus',
    label: "This note's world",
    description: 'Neighborhood of the selected note',
    apply: (ctx) => ({
      seed: ctx.selectedId,
      depth: 2,
      facets: {types: [], tags: [], folders: []},
      layout: 'radial',
      groupBy: undefined,
    }),
  },
  {
    id: 'everything',
    label: 'Everything',
    description: 'Full graph',
    apply: () => ({
      seed: undefined,
      facets: {types: [], tags: [], folders: []},
      layout: 'force',
      groupBy: undefined,
    }),
  },
]
