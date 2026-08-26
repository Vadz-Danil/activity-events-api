import { useCallback, useEffect, useState } from 'react'

import { ApiError, getStats, type Session, type Stats } from './api'
import { AdminPanel } from './AdminPanel'
import { ActivityColumns, dayLabel, Heatmap, hourLabel, TypeBars, type Column } from './charts'
import { LiveFeed } from './LiveFeed'
import { ThemeToggle } from './ThemeToggle'

const ranges = [
  { id: '24h', label: '24 години', hours: 24, granularity: 'bucket' },
  { id: '7d', label: '7 днів', hours: 24 * 7, granularity: 'bucket' },
  { id: '30d', label: '30 днів', hours: 24 * 30, granularity: 'day' },
] as const

type RangeId = (typeof ranges)[number]['id']

export function Dashboard({
  session,
  onSignOut,
  onUnauthorized,
}: {
  session: Session
  onSignOut: () => void
  onUnauthorized: () => void
}) {
  const [range, setRange] = useState<RangeId>('7d')
  const [stats, setStats] = useState<Stats | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const token = session.access_token

  const selected = ranges.find((r) => r.id === range)!

  const load = useCallback(async () => {
    const option = ranges.find((r) => r.id === range)!
    const from = new Date(Date.now() - option.hours * 3600_000).toISOString()

    setLoading(true)
    try {
      setStats(await getStats(token, { from, granularity: option.granularity }))
      setError(null)
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        onUnauthorized()
        return
      }
      setError(err instanceof ApiError ? err.message : 'Не вдалося завантажити статистику')
    } finally {
      setLoading(false)
    }
  }, [token, range, onUnauthorized])

  useEffect(() => {
    void load()
  }, [load])

  const buckets = stats?.buckets ?? []
  const days = stats?.days ?? []
  const daily = selected.granularity === 'day'
  const columns: Column[] = daily
    ? days.map((d, i) => ({
        key: d.day,
        count: d.event_count,
        label: dayLabel(`${d.day}T00:00:00Z`),
        tick: i % 3 === 0 ? dayLabel(`${d.day}T00:00:00Z`) : undefined,
      }))
    : buckets
        .slice()
        .sort((a, b) => a.bucket_start.localeCompare(b.bucket_start))
        .map((b) => ({
          key: b.bucket_start,
          count: b.event_count,
          label: `${dayLabel(b.bucket_start)} ${hourLabel(b.bucket_start)}–${hourLabel(b.bucket_end)}`,
          tick: new Date(b.bucket_start).getUTCHours() === 0 ? dayLabel(b.bucket_start) : undefined,
        }))

  const total = columns.reduce((sum, c) => sum + c.count, 0)
  const types = mergeTypes(daily ? days.map((d) => d.type_counts) : buckets.map((b) => b.type_counts))
  const busiest = columns.reduce<Column | null>(
    (best, c) => (best === null || c.count > best.count ? c : best),
    null,
  )

  return (
    <main className="page">
      <header className="masthead">
        <div>
          <h1>Activity Events</h1>
          <p className="muted">
            {session.user.email}
            {session.user.role === 'admin' && <span className="badge">admin</span>}
          </p>
        </div>
        <div className="masthead-tools">
          <ThemeToggle />
          <button type="button" onClick={onSignOut}>
            Вийти
          </button>
        </div>
      </header>

      <div className="filters" role="group" aria-label="Період">
        {ranges.map((option) => (
          <button
            type="button"
            key={option.id}
            className={range === option.id ? 'is-active' : ''}
            aria-pressed={range === option.id}
            onClick={() => setRange(option.id)}
          >
            {option.label}
          </button>
        ))}
      </div>

      {error && <p className="error">{error}</p>}

      <div className={loading && stats ? 'content is-loading' : 'content'}>
        <section className="tiles">
          <div className="tile tile-hero">
            <span className="tile-label">Подій за період</span>
            <span className="tile-value hero">{total.toLocaleString('uk-UA')}</span>
          </div>
          <div className="tile">
            <span className="tile-label">Типів подій</span>
            <span className="tile-value">{Object.keys(types).length}</span>
          </div>
          <div className="tile">
            <span className="tile-label">{daily ? 'Найактивніша доба' : 'Найактивніше вікно'}</span>
            <span className="tile-value">{busiest ? busiest.count.toLocaleString('uk-UA') : '—'}</span>
            <span className="tile-note muted">{busiest ? busiest.label : 'даних немає'}</span>
          </div>
        </section>

        <section className="panel">
          <header className="panel-head">
            <h2>Активність по вікнах</h2>
            <span className="muted">крок {daily ? 'доба' : humanBucket(stats?.bucket)}</span>
          </header>
          <ActivityColumns columns={columns} />
        </section>

        <div className="split">
          <section className="panel">
            <header className="panel-head">
              <h2>Добовий ритм</h2>
            </header>
            {daily ? (
              <p className="muted">
                Ритм доби показуємо для періодів до тижня — на місяці чотиригодинні вікна вже згорнуті в добові.
              </p>
            ) : (
              <Heatmap buckets={buckets} />
            )}
          </section>

          <section className="panel">
            <header className="panel-head">
              <h2>Типи подій</h2>
            </header>
            <TypeBars counts={types} />
          </section>
        </div>
      </div>

      <LiveFeed token={token} onUnauthorized={onUnauthorized} onCreated={load} />

      {session.user.role === 'admin' && (
        <AdminPanel token={token} onUnauthorized={onUnauthorized} onRecomputed={load} />
      )}

      <footer className="foot muted">
        Статистику рахує фоновий воркер що чотири години; свіжі події з’являються у стрічці одразу, а у вікнах — після
        наступного перерахунку.
      </footer>
    </main>
  )
}

function humanBucket(raw: string | undefined): string {
  const hours = raw?.match(/^(\d+)h/)?.[1]
  return hours ? `${hours} год` : (raw ?? '4 год')
}

function mergeTypes(counts: Record<string, number>[]): Record<string, number> {
  const merged: Record<string, number> = {}

  for (const bucket of counts) {
    for (const [type, count] of Object.entries(bucket)) {
      merged[type] = (merged[type] ?? 0) + count
    }
  }

  return merged
}
