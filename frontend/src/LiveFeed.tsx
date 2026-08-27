import { useEffect, useRef, useState, type SyntheticEvent } from 'react'

import { ApiError, createEvent, listEvents, streamEvents, type ActivityEvent } from './api'

const feedLimit = 50
const reconnectDelay = 3000

type Connection = 'connecting' | 'live' | 'offline' | 'history'

const connectionClasses: Record<Connection, string> = {
  connecting: 'status-connecting',
  live: 'status-live',
  offline: 'status-offline',
  history: 'status-history',
}

const connectionLabels: Record<Connection, string> = {
  connecting: 'під’єднання',
  live: 'на зв’язку',
  offline: 'нема зв’язку',
  history: 'лише історія',
}

export function LiveFeed({
  token,
  onUnauthorized,
  onCreated,
  userId,
}: {
  token: string
  onUnauthorized: () => void
  onCreated: () => void
  userId?: number
}) {
  const [events, setEvents] = useState<ActivityEvent[]>([])
  const [connection, setConnection] = useState<Connection>('connecting')
  const [type, setType] = useState('demo.click')
  const [error, setError] = useState<string | null>(null)
  const [sending, setSending] = useState(false)

  const unauthorized = useRef(onUnauthorized)

  useEffect(() => {
    unauthorized.current = onUnauthorized
  }, [onUnauthorized])

  useEffect(() => {
    let cancelled = false
    const controller = new AbortController()
    let retry: ReturnType<typeof setTimeout> | undefined

    function add(incoming: ActivityEvent) {
      setEvents((current) =>
        current.some((e) => e.id === incoming.id) ? current : [incoming, ...current].slice(0, feedLimit),
      )
    }

    async function connect() {
      setConnection(userId ? 'history' : 'connecting')

      try {
        const page = await listEvents(token, { limit: 20, userId })
        if (cancelled) {
          return
        }
        setEvents(page.items)

        if (userId) {
          setConnection('history')
          return
        }
        setConnection('live')

        await streamEvents(token, add, controller.signal)
        if (!cancelled) {
          setConnection('offline')
          retry = setTimeout(connect, reconnectDelay)
        }
      } catch (err) {
        if (cancelled || controller.signal.aborted) {
          return
        }
        if (err instanceof ApiError && err.status === 401) {
          unauthorized.current()
          return
        }

        setConnection('offline')
        retry = setTimeout(connect, reconnectDelay)
      }
    }

    void connect()

    return () => {
      cancelled = true
      controller.abort()
      clearTimeout(retry)
    }
  }, [token, userId])

  async function send(e: SyntheticEvent) {
    e.preventDefault()
    setSending(true)
    setError(null)

    try {
      await createEvent(token, { type, payload: { source: 'web' } })
      onCreated()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Не вдалося надіслати подію')
    } finally {
      setSending(false)
    }
  }

  return (
    <section className="panel">
      <header className="panel-head">
        <h2>Живий потік</h2>
        <span className={`status ${connectionClasses[connection]}`}>
          <span className="dot" aria-hidden="true" />
          {connectionLabels[connection]}
        </span>
      </header>

      {!userId && (
        <form className="composer" onSubmit={send}>
          <label className="field">
            <span className="sr-only">Тип події</span>
            <input value={type} onChange={(e) => setType(e.target.value)} required />
          </label>
          <button type="submit" className="primary" disabled={sending}>
            {sending ? 'Надсилаю…' : 'Надіслати подію'}
          </button>
        </form>
      )}

      {error && <p className="error">{error}</p>}

      <p className="muted hint">
        {userId
          ? 'Останні 20 подій цього користувача. Живий стрім доступний лише для власного акаунта — читати чужу активність у реальному часі це вже спостереження, а не звітність. Час у UTC.'
          : 'Надіслана подія повертається сюди через SSE, а не додається локально — тобто список показує те, що справді дійшло до сервера. Час у UTC.'}
      </p>

      <ol className="feed">
        {events.map((event) => (
          <li key={event.id}>
            <span className="feed-time">{stamp(event.occurred_at)}</span>
            <span className="feed-type">{event.type}</span>
          </li>
        ))}
      </ol>

      {events.length === 0 && (
        <p className="muted">{userId ? 'У цього користувача подій немає.' : 'Подій ще немає. Надішли першу.'}</p>
      )}
    </section>
  )
}

function stamp(iso: string): string {
  const at = new Date(iso)
  const date = at.toLocaleDateString('uk-UA', { day: '2-digit', month: '2-digit', timeZone: 'UTC' })
  const time = at.toLocaleTimeString('uk-UA', { hour12: false, timeZone: 'UTC' })

  return `${date} ${time}`
}
