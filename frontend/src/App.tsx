import { useCallback, useEffect, useRef, useState } from 'react'

import { exchangeGoogleCode, refreshSession, signOut, type Session } from './api'
import { Dashboard } from './Dashboard'
import { SignIn } from './SignIn'
import { clearSession, loadSession, saveSession } from './session'

const callbackPath = '/auth/callback'

let exchangeStarted = false

export default function App() {
  const [session, setSession] = useState<Session | null>(() => loadSession())
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(window.location.pathname === callbackPath)

  const refreshing = useRef<Promise<void> | null>(null)

  const accept = useCallback((next: Session) => {
    saveSession(next)
    setSession(next)
  }, [])

  const drop = useCallback(() => {
    clearSession()
    setSession(null)
  }, [])

  useEffect(() => {
    if (window.location.pathname !== callbackPath || exchangeStarted) {
      return
    }
    exchangeStarted = true

    const params = new URLSearchParams(window.location.search)
    const failure = params.get('error')
    const code = params.get('code')
    const back = params.get('redirect_to') ?? '/'

    if (failure || !code) {
      setError(failure ?? 'Вхід через Google перервано')
      setBusy(false)
      window.history.replaceState({}, '', '/')
      return
    }

    exchangeGoogleCode(code)
      .then(accept)
      .catch((err: Error) => setError(err.message))
      .finally(() => {
        setBusy(false)
        window.history.replaceState({}, '', back)
      })
  }, [accept])

  const renew = useCallback(() => {
    if (!session) {
      return
    }

    if (!refreshing.current) {
      refreshing.current = refreshSession(session.refresh_token)
        .then(accept)
        .catch(drop)
        .finally(() => {
          refreshing.current = null
        })
    }
  }, [session, accept, drop])

  const leave = useCallback(() => {
    if (session) {
      void signOut(session.refresh_token)
    }
    drop()
  }, [session, drop])

  if (busy) {
    return (
      <main className="page">
        <h1>Activity Events</h1>
        <p className="muted">Завершуємо вхід через Google…</p>
      </main>
    )
  }

  if (!session) {
    return <SignIn onSignedIn={accept} notice={error} />
  }

  return <Dashboard session={session} onSignOut={leave} onUnauthorized={renew} />
}
