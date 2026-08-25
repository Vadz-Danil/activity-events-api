import { useEffect, useState } from 'react'

import { exchangeGoogleCode, googleSignInURL, signOut, type Session } from './api'
import { clearSession, loadSession, saveSession } from './session'

const callbackPath = '/auth/callback'

let exchangeStarted = false

export default function App() {
  const [session, setSession] = useState<Session | null>(() => loadSession())
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(window.location.pathname === callbackPath)

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
      setError(failure ?? 'Sign-in was interrupted')
      setBusy(false)
      window.history.replaceState({}, '', '/')
      return
    }

    exchangeGoogleCode(code)
      .then((next) => {
        saveSession(next)
        setSession(next)
      })
      .catch((err: Error) => setError(err.message))
      .finally(() => {
        setBusy(false)
        window.history.replaceState({}, '', back)
      })
  }, [])

  const handleSignOut = () => {
    if (session) {
      void signOut(session.refresh_token)
    }
    clearSession()
    setSession(null)
  }

  return (
    <main className="page">
      <h1>Activity Events</h1>

      {busy && <p className="muted">Finishing sign-in…</p>}

      {error && <p className="error">Sign-in failed: {error}</p>}

      {session ? (
        <section className="card">
          <p className="muted">
            Signed in as <strong>{session.user.email}</strong> ({session.user.role})
          </p>
          <button type="button" onClick={handleSignOut}>
            Sign out
          </button>
        </section>
      ) : (
        !busy && (
          <section className="card">
            <p className="muted">Sign in to send and browse activity events.</p>
            <a className="button" href={googleSignInURL('/')}>
              Continue with Google
            </a>
          </section>
        )
      )}
    </main>
  )
}
