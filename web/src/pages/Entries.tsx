import { useCallback, useEffect, useRef, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { api, isApiError } from '../api'
import type { Category, Direction, EntryList, Method } from '../api/types'
import { METHODS } from '../api/types'
import { useLedger } from '../components/ledger-context'
import { EntriesTable } from '../components/EntriesTable'
import { QuickAddBar } from '../components/QuickAddBar'
import { Card, ErrorNote, PageTitle, Spinner, inputClass } from '../components/ui'
import { currentMonth, monthLabel, monthRange, shiftMonth } from '../lib/dates'
import { NEW_ENTRY_EVENT } from '../lib/ids'
import { formatNet, formatRM } from '../lib/money'

export default function Entries() {
  const [categories, setCategories] = useState<Category[]>([])
  const [list, setList] = useState<EntryList | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [q, setQ] = useState('')
  const [dq, setDq] = useState('')
  const [month, setMonth] = useState(currentMonth())
  const [category, setCategory] = useState('')
  const [method, setMethod] = useState('')
  const [type, setType] = useState<'all' | Direction>('all')
  const { refresh } = useLedger()
  const location = useLocation()
  const reqId = useRef(0)

  useEffect(() => { api.listCategories().then((r) => setCategories(r.categories), () => {}) }, [])
  useEffect(() => {
    const t = setTimeout(() => setDq(q), 200)
    return () => clearTimeout(t)
  }, [q])

  const load = useCallback(async () => {
    const id = ++reqId.current
    try {
      const range = month ? monthRange(month) : {}
      const r = await api.listEntries({
        ...range, q: dq || undefined, category: category ? Number(category) : undefined,
        method: (method || undefined) as Method | undefined,
        direction: type === 'all' ? undefined : type,
      })
      if (id === reqId.current) { setList(r); setError(null) }
    } catch (e) {
      if (id === reqId.current) setError(isApiError(e) ? e.message : 'Could not load entries')
    }
  }, [month, dq, category, method, type])
  useEffect(() => { void load() }, [load])

  // Arrived via the N / I shortcut from another page: QuickAddBar is mounted, so ask it to switch type and focus Amount.
  useEffect(() => {
    const st = location.state as { focusAmount?: number; direction?: Direction } | null
    if (st?.focusAmount) window.dispatchEvent(new CustomEvent(NEW_ENTRY_EVENT, { detail: st.direction ?? 'expense' }))
  }, [location.state])

  const months = Array.from({ length: 13 }, (_, i) => shiftMonth(currentMonth(), -i))
  if (month && !months.includes(month)) months.push(month)
  const filtered = Boolean(dq || category || method || type !== 'all')

  return (
    <>
      <PageTitle>Entries</PageTitle>
      <QuickAddBar api={api} categories={categories} onSaved={() => { void load(); void refresh() }} onUncertain={() => { void load() }} />
      <div className="mb-4 flex flex-wrap gap-3">
        <div className="flex-[1_1_260px]">
          <label htmlFor="f-q" className="sr-only-text">Search payee or note</label>
          <input id="f-q" type="search" placeholder="Search payee or note" value={q} onChange={(e) => setQ(e.target.value)} className={inputClass} />
        </div>
        <div>
          <label htmlFor="f-month" className="sr-only-text">Month</label>
          <select id="f-month" value={month} onChange={(e) => setMonth(e.target.value)} className={`${inputClass} w-auto`}>
            <option value="">All months</option>
            {months.map((m) => <option key={m} value={m}>{monthLabel(m)}</option>)}
          </select>
        </div>
        <div>
          <label htmlFor="f-type" className="sr-only-text">Type</label>
          <select id="f-type" value={type} onChange={(e) => { setType(e.target.value as 'all' | Direction); setCategory('') }} className={`${inputClass} w-auto`}>
            <option value="all">All types</option>
            <option value="expense">Expenses</option>
            <option value="income">Income</option>
          </select>
        </div>
        <div>
          <label htmlFor="f-cat" className="sr-only-text">Category</label>
          <select id="f-cat" value={category} onChange={(e) => setCategory(e.target.value)} className={`${inputClass} w-auto`}>
            <option value="">All categories</option>
            {categories.filter((c) => type === 'all' || c.direction === type).map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
          </select>
        </div>
        <div>
          <label htmlFor="f-method" className="sr-only-text">Paid with</label>
          <select id="f-method" value={method} onChange={(e) => setMethod(e.target.value)} className={`${inputClass} w-auto`}>
            <option value="">All methods</option>
            {METHODS.map((m) => <option key={m.value} value={m.value}>{m.label}</option>)}
          </select>
        </div>
      </div>
      <ErrorNote message={error} onRetry={() => void load()} />
      {!list && !error && <Spinner />}
      {list && (
        <Card className="px-5 py-3">
          <EntriesTable entries={list.entries} emptyText={filtered ? 'No entries match these filters.' : 'No entries yet. Press N to add one.'} />
          <p className="flex flex-wrap gap-x-4 gap-y-1 border-t border-border pt-3 text-xs text-text-muted" aria-live="polite">
            <span>{list.count} {list.count === 1 ? 'entry' : 'entries'}</span>
            <span>Spent {formatRM(list.total_sen)}</span>
            <span>Income <span className="font-semibold text-accent">{formatRM(list.income_sen)}</span></span>
            <span>Net <span className={`font-semibold ${list.net_sen < 0 ? 'text-danger' : 'text-accent'}`}>{formatNet(list.net_sen)}</span></span>
            {list.voided_count > 0 && <span>{list.voided_count} voided (not counted)</span>}
          </p>
        </Card>
      )}
    </>
  )
}
