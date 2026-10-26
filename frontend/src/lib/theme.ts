export type ThemePreference = 'system' | 'light' | 'dark'
export const THEME_KEY = 'taxpl.theme'

export function readThemePreference(): ThemePreference {
  try {
    const v = localStorage.getItem(THEME_KEY)
    if (v === 'light' || v === 'dark' || v === 'system') return v
  } catch {
    /* storage unavailable */
  }
  return 'system'
}

export function resolveTheme(pref: ThemePreference): 'light' | 'dark' {
  if (pref === 'system') {
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  }
  return pref
}

export function applyTheme(pref: ThemePreference): void {
  document.documentElement.dataset.theme = resolveTheme(pref)
  try {
    localStorage.setItem(THEME_KEY, pref)
  } catch {
    /* ignore */
  }
}
