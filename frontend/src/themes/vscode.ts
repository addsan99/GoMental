import {generatedVSCodeThemes} from './generated-vscode-themes'

export type AppTheme = {
  id: string
  label: string
  category: 'light' | 'dark' | 'high-contrast'
  variables: Record<string, string>
}


const defaults: Record<string, string> = {
  '--bg': '#0e0e12', '--surface': '#16161c', '--surface-2': '#1e1e26', '--surface-3': '#26262f',
  '--text': '#eceaf2', '--text-2': '#cfc9dd', '--text-3': '#a3a0ad', '--border': '#30303a',
  '--border-strong': '#4a4a56', '--accent': '#8b83ff', '--accent-text': '#b9b4ff',
  '--accent-soft': 'rgba(139, 131, 255, .14)', '--accent-soft-line': 'rgba(139, 131, 255, .4)',
}
export const CSS_VARIABLE_NAMES = Object.keys(defaults)

// VS Code workbench tokens are intentionally mapped to semantic app tokens so
// a theme can be incomplete without leaving any GoMental surface unreadable.
export function cssVariablesForTheme(theme: AppTheme): Record<string, string> { return theme.variables }
export function loadVSCodeTheme(id: string): AppTheme | undefined { return generatedVSCodeThemes.find((theme) => theme.id === id) as AppTheme | undefined }
