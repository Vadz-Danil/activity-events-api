export type Theme = 'system' | 'light' | 'dark'

const storageKey = 'activity-events.theme'

export const themes: { id: Theme; label: string }[] = [
  { id: 'light', label: 'світла' },
  { id: 'dark', label: 'темна' },
  { id: 'system', label: 'система' },
]

export function loadTheme(): Theme {
  try {
    const saved = localStorage.getItem(storageKey)
    if (saved === 'light' || saved === 'dark' || saved === 'system') {
      return saved
    }
  } catch {
  }

  return 'system'
}
export function applyTheme(theme: Theme): void {
  const root = document.documentElement

  if (theme === 'system') {
    root.removeAttribute('data-theme')
  } else {
    root.setAttribute('data-theme', theme)
  }

  try {
    localStorage.setItem(storageKey, theme)
  } catch {
  }
}
