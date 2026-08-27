import { useEffect, useState, type SyntheticEvent } from 'react'

import { ApiError, getAuthMethods, googleSignInURL, signIn, signUp, type AuthMethods, type Session } from './api'
import { ThemeToggle } from './ThemeToggle'

type Mode = 'sign-in' | 'sign-up'

const facts = [
  { key: '4 год', text: 'вікно агрегації; доба — це сума шести таких вікон, а не окремий підрахунок' },
  { key: 'SSE', text: 'живий потік подій без опитування сервера' },
  { key: 'ключ', text: 'повторний запис із тим самим ключем ідемпотентності не створює дубля' },
]

export function SignIn({
  onSignedIn,
  notice,
}: {
  onSignedIn: (session: Session) => void
  notice?: string | null
}) {
  const [mode, setMode] = useState<Mode>('sign-in')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [methods, setMethods] = useState<AuthMethods | null>(null)

  // Сервер вирішує, які способи входу існують: кнопка Google на деплої без
  // облікових даних Google веде лише в глухий кут.
  useEffect(() => {
    getAuthMethods()
      .then(setMethods)
      .catch(() => setMethods({ password: true, google: false }))
  }, [])

  async function submit(e: SyntheticEvent) {
    e.preventDefault()
    setBusy(true)
    setError(null)

    try {
      const session = mode === 'sign-in' ? await signIn(email, password) : await signUp(email, password)
      onSignedIn(session)
    } catch (err) {
      setError(explain(err, mode))
    } finally {
      setBusy(false)
    }
  }

  return (
    <main className="gate">
      <div className="gate-corner">
        <ThemeToggle />
      </div>

      <section className="gate-pitch">
        <h1>Події активності, згорнуті у вікна</h1>
        <p className="lede">
          Сервіс приймає потік подій, віддає його назад у реальному часі й раз на чотири години перераховує статистику
          фоновим воркером.
        </p>

        <ul className="facts">
          {facts.map((fact) => (
            <li key={fact.key}>
              <b>{fact.key}</b>
              <span>{fact.text}</span>
            </li>
          ))}
        </ul>
      </section>

      <form className="gate-form" onSubmit={submit}>
        <h2>Акаунт</h2>

        <div className="tabs" role="group" aria-label="Режим">
          <button
            type="button"
            aria-pressed={mode === 'sign-in'}
            className={mode === 'sign-in' ? 'is-active' : ''}
            onClick={() => setMode('sign-in')}
          >
            Вхід
          </button>
          <button
            type="button"
            aria-pressed={mode === 'sign-up'}
            className={mode === 'sign-up' ? 'is-active' : ''}
            onClick={() => setMode('sign-up')}
          >
            Реєстрація
          </button>
        </div>

        <label className="field">
          <span>Пошта</span>
          <input type="email" value={email} autoComplete="email" required onChange={(e) => setEmail(e.target.value)} />
        </label>

        <label className="field">
          <span>Пароль</span>
          <input
            type="password"
            value={password}
            autoComplete={mode === 'sign-in' ? 'current-password' : 'new-password'}
            required
            onChange={(e) => setPassword(e.target.value)}
          />
        </label>

        <button type="submit" className="primary" disabled={busy}>
          {busy ? 'Хвилинку…' : mode === 'sign-in' ? 'Увійти' : 'Створити акаунт'}
        </button>

        {(error ?? notice) && <p className="error">{error ?? notice}</p>}

        {methods?.google && (
          <>
            <p className="divider" aria-hidden="true">
              <span>або</span>
            </p>

            <a className="google" href={googleSignInURL('/')}>
              <GoogleMark />
              Продовжити з Google
            </a>
          </>
        )}
      </form>
    </main>
  )
}

function GoogleMark() {
  return (
    <svg viewBox="0 0 18 18" width="17" height="17" aria-hidden="true" focusable="false">
      <path
        fill="#4285F4"
        d="M17.64 9.2c0-.64-.06-1.25-.16-1.84H9v3.48h4.84a4.14 4.14 0 0 1-1.8 2.72v2.26h2.92c1.7-1.57 2.68-3.88 2.68-6.62z"
      />
      <path
        fill="#34A853"
        d="M9 18c2.43 0 4.47-.8 5.96-2.18l-2.92-2.26c-.8.54-1.84.86-3.04.86-2.34 0-4.32-1.58-5.03-3.7H.96v2.33A9 9 0 0 0 9 18z"
      />
      <path fill="#FBBC05" d="M3.97 10.71a5.4 5.4 0 0 1 0-3.42V4.96H.96a9 9 0 0 0 0 8.08l3-2.33z" />
      <path
        fill="#EA4335"
        d="M9 3.58c1.32 0 2.5.45 3.44 1.35l2.58-2.58C13.46.9 11.43 0 9 0A9 9 0 0 0 .96 4.96l3 2.33C4.68 5.16 6.66 3.58 9 3.58z"
      />
    </svg>
  )
}

function explain(err: unknown, mode: Mode): string {
  if (!(err instanceof ApiError)) {
    return 'Не вдалося зв’язатися з сервером. Перевір, чи він запущений.'
  }

  switch (err.code) {
    case 'invalid_credentials':
      return 'Неправильна пошта або пароль.'
    case 'email_taken':
      return 'Такий акаунт уже існує — перемкнись на вхід.'
    case 'password_login_unavailable':
      return 'Цей акаунт створено через Google, пароля в нього немає.'
    case 'validation_failed':
      return mode === 'sign-up' ? 'Пароль закороткий або пошта неправильна.' : err.message
    default:
      return err.message
  }
}
