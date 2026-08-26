import { useEffect, useState } from 'react'

import { applyTheme, loadTheme, themes, type Theme } from './theme'

export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(loadTheme)

  useEffect(() => {
    applyTheme(theme)
  }, [theme])

  return (
    <div className="theme-toggle" role="group" aria-label="Тема">
      {themes.map((option) => (
        <button
          type="button"
          key={option.id}
          className={theme === option.id ? 'is-active' : ''}
          aria-pressed={theme === option.id}
          onClick={() => setTheme(option.id)}
        >
          {option.label}
        </button>
      ))}
    </div>
  )
}
