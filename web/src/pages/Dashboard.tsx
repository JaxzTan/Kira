import { useEffect, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api, isApiError } from '../api'
import type { Summary } from '../api/types'
import { goAddExpense } from '../components/AppShell'
import { CategoryBars } from '../components/CategoryBars'
import { EntriesTable } from '../components/EntriesTable'
import { IconChevronLeft, IconChevronRight, IconPlus } from '../components/icons'
import { KpiCard } from '../components/KpiCard'
import { TrendBars } from '../components/TrendBars'
import { Card, ErrorNote, Spinner } from '../components/ui'
import { currentMonth, monthLabel, shiftMonth } from '../lib/dates'
import { formatRM } from '../lib/money'

function pctText(p: number | null): string {
  if (p === null) return '—'
  const s = Math.abs(p) >= 10 ? String(Math.round(p)) : p.toFixed(1)
  return `${p > 0 ? '+' : ''}${s}%`
}

export default function Dashboard() {
  const [params, setParams] = useSearchParams()
  const month = params.get('month') ?? currentMonth()
  const [data, setData] = useState<Summary | null>(null)
  const [error, setError] = useState<string | null>(null)
  const navigate = useNavigate()

  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let live = true
    setError(null)
    setData(null)
    api.summary(month).then(
      (s) => live && setData(s),
      (e) => live && setError(isApiError(e) ? e.message : 'Could not load the dashboard'),
    )
    return () => { live = false }
  }, [month, attempt])

  const go = (delta: number) => setParams({ month: shiftMonth(month, delta) })
  const navBtn = 'grid h-10 w-10 cursor-pointer place-items-center rounded-control border border-border-strong bg-surface text-text hover:bg-bg'
  const top = data?.by_category[0]
  const prevName = monthLabel(shiftMonth(month, -1)).split(' ')[0]

  return (
    <>
      <div className="mb-6 flex flex-wrap items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <button type="button" aria-label="Previous month" onClick={() => go(-1)} className={navBtn}><IconChevronLeft /></button>
          <h1 className="min-w-[200px] text-center font-display text-[28px] font-semibold leading-tight" aria-live="polite">{monthLabel(month)}</h1>
          <button type="button" aria-label="Next month" onClick={() => go(1)} className={navBtn}><IconChevronRight /></button>
        </div>
        <button
          type="button"
          onClick={() => goAddExpense(navigate)}
          className="inline-flex h-control cursor-pointer items-center gap-2 rounded-control border border-accent bg-accent px-5 text-[15px] font-semibold text-white hover:bg-accent-hover"
        >
          <IconPlus /> Add expense
          <kbd aria-label="shortcut N" className="rounded-[6px] border border-white/50 bg-white/10 px-1.5 text-xs font-semibold">N</kbd>
        </button>
      </div>
      <ErrorNote message={error} onRetry={() => setAttempt((n) => n + 1)} />
      {!data && !error && <Spinner />}
      {data && (
        <>
          <div className="mb-5 grid grid-cols-[repeat(auto-fit,minmax(200px,1fr))] gap-4">
            <KpiCard label="Spent this month" value={formatRM(data.total_sen)}
              sub={`${data.count} ${data.count === 1 ? 'entry' : 'entries'} · ${data.voided_count} voided`} />
            <KpiCard label={`vs ${prevName}`} value={pctText(data.change_pct)}
              sub={data.prev_month_total_sen ? `${formatRM(data.prev_month_total_sen)} last month` : 'No spending last month'} />
            <KpiCard label="Top category" value={top ? top.name.split(/\s*&\s*/)[0] : '—'}
              sub={top ? `${formatRM(top.total_sen)} · ${Math.round(top.pct)}%` : 'No spending yet'} />
            <KpiCard label="Missing receipts" value={String(data.missing_receipts)}
              sub={data.missing_receipts ? 'Attach before month end' : 'All receipts attached'} />
          </div>
          <div className="mb-5 grid grid-cols-[repeat(auto-fit,minmax(340px,1fr))] gap-5">
            <Card>
              <h2 className="mb-4 font-display text-base font-semibold">By category</h2>
              <CategoryBars items={data.by_category} />
            </Card>
            <Card>
              <h2 className="mb-4 font-display text-base font-semibold">Last 6 months</h2>
              <TrendBars items={data.trend} current={data.month} />
            </Card>
          </div>
          <Card>
            <div className="mb-2 flex items-center justify-between">
              <h2 className="font-display text-base font-semibold">Recent entries</h2>
              <Link to="/entries" className="text-sm font-semibold text-accent no-underline hover:underline">View all</Link>
            </div>
            <EntriesTable entries={data.recent} compact emptyText="No entries yet. Press N to add your first expense." />
          </Card>
        </>
      )}
    </>
  )
}
