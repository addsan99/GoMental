import {generatedVSCodeThemeOptions} from './generated-vscode-themes'

export type ThemeCategory = 'light' | 'dark' | 'high-contrast'
export type ThemeOption = { id: string; label: string; category: ThemeCategory }
export const vscodeThemeOptions: ThemeOption[] = generatedVSCodeThemeOptions as ThemeOption[]

export function themeOption(id: string): ThemeOption | undefined { return vscodeThemeOptions.find((theme) => theme.id === id) }
