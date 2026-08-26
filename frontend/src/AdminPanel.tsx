import { useCallback, useEffect, useState } from 'react'

import { ApiError, listRuns, triggerRun, type AggregationRun } from './api'
import { dayLabel, hourLabel } from './charts'

const statusLabels: Record<AggregationRun['status'], string> = {
  succeeded: 'пораховано',
  skipped: 'пропущено',
  failed: 'помилка',
}

const statusClasses: Record<AggregationRun['status'], string> = {
  succeeded: 'run-succeeded',
  skipped: 'run-skipped',
  failed: 'run-failed',
}

const triggerLabels: Record<AggregationRun['trigger'], string> = {
  schedule: 'планувальник',
  manual: 'вручну',
}

export function AdminPanel({
  token,
  onUnauthorized,
  onRecomputed,
}: {
  token: string
  onUnauthorized: () => void
  onRecomputed: () => void
}) {
  const [runs, setRuns] = useState<AggregationRun[]>([])
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    try {
      const page = await listRuns(token)
      setRuns(page.items)
      setError(null)
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        onUnauthorized()
        return
      }
      setError(err instanceof ApiError ? err.message : 'Не вдалося прочитати історію запусків')
    }
  }, [token, onUnauthorized])

  useEffect(() => {
    void load()
  }, [load])

  async function recompute() {
    setBusy(true)
    setError(null)

    try {
      const run = await triggerRun(token)
      await load()

      if (run.status === 'succeeded') {
        onRecomputed()
      }
      if (run.status === 'failed') {
        setError(run.error ?? 'Перерахунок не вдався')
      }
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Не вдалося запустити перерахунок')
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="panel">
      <header className="panel-head">
        <h2>Агрегація · запуски</h2>
        <button type="button" onClick={recompute} disabled={busy}>
          {busy ? 'Рахую…' : 'Перерахувати останнє закрите вікно'}
        </button>
      </header>

      <p className="muted hint">
        Планувальник іде тільки вперед від останнього успішного вікна. Подія, що прийшла заднім числом у вже пораховане
        вікно, сама не підхопиться — цей перерахунок і є штатний спосіб її врахувати.
      </p>

      {error && <p className="error">{error}</p>}

      {runs.length === 0 ? (
        <p className="muted">Запусків ще не було.</p>
      ) : (
        <div className="table-scroll">
          <table className="runs">
            <thead>
              <tr>
                <th>Вікно</th>
                <th>Результат</th>
                <th>Тригер</th>
                <th>Користувачів</th>
                <th>Тривалість</th>
              </tr>
            </thead>
            <tbody>
              {runs.map((run) => (
                <tr key={run.id}>
                  <td>
                    {dayLabel(run.bucket_start)} {hourLabel(run.bucket_start)}–{hourLabel(run.bucket_end)}
                  </td>
                  <td>
                    <span className={`run-status ${statusClasses[run.status]}`}>{statusLabels[run.status]}</span>
                  </td>
                  <td>{triggerLabels[run.trigger]}</td>
                  <td>{run.users_touched}</td>
                  <td>{duration(run)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

function duration(run: AggregationRun): string {
  if (!run.finished_at) {
    return '—'
  }

  const ms = new Date(run.finished_at).getTime() - new Date(run.started_at).getTime()
  return ms < 1000 ? `${ms} мс` : `${(ms / 1000).toFixed(1)} с`
}
