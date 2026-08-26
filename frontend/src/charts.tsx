import { Fragment, useState } from 'react'

import type { Bucket } from './api'
const heatShades = ['level-1', 'level-2', 'level-3', 'level-4', 'level-5'] as const
const maxTicks = 5

function formatCount(value: number): string {
  return value >= 10000 ? `${Math.round(value / 1000)}K` : value.toLocaleString('uk-UA')
}

export function hourLabel(iso: string): string {
  return new Date(iso).toLocaleTimeString('uk-UA', { hour: '2-digit', minute: '2-digit', timeZone: 'UTC' })
}

export function dayLabel(iso: string): string {
  return new Date(iso).toLocaleDateString('uk-UA', { day: '2-digit', month: '2-digit', timeZone: 'UTC' })
}

function axisTicks(max: number): number[] {
  if (max <= 0) {
    return [0, 1]
  }

  const candidates: number[] = []
  for (let power = Math.floor(Math.log10(max)) - 2; power <= Math.ceil(Math.log10(max)); power++) {
    for (const unit of [1, 2, 2.5, 5, 10]) {
      candidates.push(unit * 10 ** power)
    }
  }

  const step = candidates.sort((a, b) => a - b).find((s) => s > 0 && Math.ceil(max / s) <= maxTicks) ?? max
  const top = Math.ceil(max / step) * step

  const ticks: number[] = []
  for (let value = 0; value <= top + step / 2; value += step) {
    ticks.push(Math.round(value * 100) / 100)
  }
  return ticks
}

export type Column = {
  key: string
  count: number
  label: string
  tick?: string
}

type Hovered = { index: number; x: number } | null

export function ActivityColumns({ columns }: { columns: Column[] }) {
  const [hovered, setHovered] = useState<Hovered>(null)

  if (columns.length === 0) {
    return <p className="muted">Подій за цей період немає.</p>
  }

  const ordered = [...columns].sort((a, b) => a.key.localeCompare(b.key))
  const ticks = axisTicks(Math.max(...ordered.map((c) => c.count)))
  const top = ticks[ticks.length - 1] || 1
  const active = hovered ? ordered[hovered.index] : null

  return (
    <figure className="chart">
      <div className="plot">
        <div className="y-axis" aria-hidden="true">
          {[...ticks].reverse().map((tick) => (
            <span key={tick}>{formatCount(tick)}</span>
          ))}
        </div>

        <div className="plot-body">
          <div className="columns" onMouseLeave={() => setHovered(null)}>
            {[...ticks].reverse().map((tick) => (
              <span className="gridline" key={tick} style={{ bottom: `${(tick / top) * 100}%` }} aria-hidden="true" />
            ))}

            {ordered.map((column, index) => (
              <button
                type="button"
                className={`column${hovered?.index === index ? ' is-active' : ''}`}
                key={column.key}
                onMouseEnter={(e) => setHovered({ index, x: anchor(e.currentTarget) })}
                onFocus={(e) => setHovered({ index, x: anchor(e.currentTarget) })}
                onBlur={() => setHovered(null)}
                aria-label={`${column.label}: ${column.count}`}
              >
                <span className="column-fill" style={{ height: `${(column.count / top) * 100}%` }} />
              </button>
            ))}

            {active && (
              <div className="tooltip" style={{ left: `${hovered?.x ?? 50}%` }} role="status">
                <strong>{formatCount(active.count)}</strong>
                <span>{active.label}</span>
              </div>
            )}
          </div>

          <div className="x-axis" aria-hidden="true">
            {ordered.map((column) => (
              <span className="x-tick" key={column.key}>
                {column.tick ?? ''}
              </span>
            ))}
          </div>
        </div>
      </div>

      <TableView
        caption="Події по вікнах"
        head={['Вікно', 'Подій']}
        rows={ordered.map((c) => [c.label, formatCount(c.count)])}
      />
    </figure>
  )
}


function anchor(element: HTMLElement): number {
  const track = element.parentElement
  if (!track) {
    return 50
  }

  const center = element.offsetLeft + element.offsetWidth / 2
  return Math.min(Math.max((center / track.offsetWidth) * 100, 12), 88)
}

function thresholds(values: number[]): number[] {
  const sorted = values.filter((v) => v > 0).sort((a, b) => a - b)
  if (sorted.length === 0) {
    return []
  }

  return [0.2, 0.4, 0.6, 0.8].map((q) => sorted[Math.floor(q * (sorted.length - 1))])
}

function shadeOf(count: number, bins: number[]): string {
  let index = 0
  for (const bin of bins) {
    if (count > bin) index++
  }

  return heatShades[Math.min(index, heatShades.length - 1)]
}

export function Heatmap({ buckets }: { buckets: Bucket[] }) {
  const [hovered, setHovered] = useState<string | null>(null)

  if (buckets.length === 0) {
    return <p className="muted">Подій за цей період немає.</p>
  }

  const byDay = new Map<string, Map<number, Bucket>>()
  for (const bucket of buckets) {
    const start = new Date(bucket.bucket_start)
    const day = start.toISOString().slice(0, 10)
    const slot = Math.floor(start.getUTCHours() / 4)

    if (!byDay.has(day)) {
      byDay.set(day, new Map())
    }
    byDay.get(day)!.set(slot, bucket)
  }

  const days = [...byDay.keys()].sort()
  const bins = thresholds(buckets.map((b) => b.event_count))
  const slots = [0, 1, 2, 3, 4, 5]
  const active = buckets.find((b) => b.bucket_start === hovered)

  return (
    <figure className="chart">
      <div className="heatmap" onMouseLeave={() => setHovered(null)}>
        <span />
        {slots.map((slot) => (
          <span className="heat-head" key={slot}>
            {String(slot * 4).padStart(2, '0')}
          </span>
        ))}

        {days.map((day) => (
          <Fragment key={day}>
            <span className="heat-day">{dayLabel(`${day}T00:00:00Z`)}</span>
            {slots.map((slot) => {
              const bucket = byDay.get(day)?.get(slot)
              const shade = bucket ? shadeOf(bucket.event_count, bins) : 'is-empty'
              const when = `${dayLabel(`${day}T00:00:00Z`)}, ${String(slot * 4).padStart(2, '0')} год`

              return (
                <button
                  type="button"
                  key={slot}
                  className={`heat-cell ${shade}${hovered && bucket?.bucket_start === hovered ? ' is-active' : ''}`}
                  onMouseEnter={() => setHovered(bucket?.bucket_start ?? null)}
                  onFocus={() => setHovered(bucket?.bucket_start ?? null)}
                  onBlur={() => setHovered(null)}
                  aria-label={bucket ? `${when}: ${bucket.event_count}` : `${when}: даних немає`}
                />
              )
            })}
          </Fragment>
        ))}
      </div>

      <div className="scale">
        <span className="muted">менше</span>
        {heatShades.map((shade) => (
          <span className={`heat-cell ${shade}`} key={shade} aria-hidden="true" />
        ))}
        <span className="muted">більше</span>
        <span className="heat-cell is-empty scale-gap" aria-hidden="true" />
        <span className="muted">немає даних</span>
      </div>

      <p className="chart-note muted">
        {active
          ? `${dayLabel(active.bucket_start)} · ${hourLabel(active.bucket_start)}–${hourLabel(active.bucket_end)} — ${formatCount(active.event_count)}`
          : 'Наведи на клітинку, щоб побачити точне число. Години в UTC.'}
      </p>

      <TableView
        caption="Активність за добою і вікном"
        head={['День', 'Вікно', 'Подій']}
        rows={buckets
          .slice()
          .sort((a, b) => a.bucket_start.localeCompare(b.bucket_start))
          .map((b) => [dayLabel(b.bucket_start), hourLabel(b.bucket_start), formatCount(b.event_count)])}
      />
    </figure>
  )
}

export function TypeBars({ counts }: { counts: Record<string, number> }) {
  const entries = Object.entries(counts).sort((a, b) => b[1] - a[1])
  if (entries.length === 0) {
    return <p className="muted">Типів подій ще немає.</p>
  }

  const max = entries[0][1]

  return (
    <ul className="type-bars">
      {entries.map(([type, count]) => (
        <li key={type}>
          <span className="type-name">{type}</span>
          <span className="type-track">
            <span className="type-fill" style={{ width: `${(count / max) * 100}%` }} />
          </span>
          <span className="type-value">{formatCount(count)}</span>
        </li>
      ))}
    </ul>
  )
}

function TableView({ caption, head, rows }: { caption: string; head: string[]; rows: string[][] }) {
  return (
    <details className="table-view">
      <summary>Таблиця</summary>
      <div className="table-scroll">
        <table>
          <caption>{caption}</caption>
          <thead>
            <tr>
              {head.map((cell) => (
                <th key={cell}>{cell}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((row, i) => (
              <tr key={i}>
                {row.map((cell, j) => (
                  <td key={j}>{cell}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </details>
  )
}
