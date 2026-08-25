import type { Session } from './api'

const storageKey = 'activity-events.session'

export function loadSession(): Session | null {
  const raw = localStorage.getItem(storageKey)
  if (!raw) {
    return null
  }

  try {
    return JSON.parse(raw) as Session
  } catch {
    localStorage.removeItem(storageKey)
    return null
  }
}

export function saveSession(session: Session): void {
  localStorage.setItem(storageKey, JSON.stringify(session))
}

export function clearSession(): void {
  localStorage.removeItem(storageKey)
}
