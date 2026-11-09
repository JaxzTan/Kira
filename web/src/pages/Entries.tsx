import { useCallback, useEffect, useRef, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { api, isApiError } from '../api'
import type { Category, EntryList, Method } from '../api/types'
import { METHODS } from '../api/types'
import { useLedger } from '../components/ledger-context'
import { EntriesTable } from '../components/EntriesTable'
import { QuickAddBar } from '../components/QuickAddBar'
import { Card, ErrorNote, PageTitle, Spinner, inputClass } from '../components/ui'
import { currentMonth, monthLabel, monthRange, shiftMonth } from '../lib/dates'
import { AMOUNT_ID } from '../lib/ids'
import { formatRM } from '../lib/money'

export default function Entries() {
  const [categories, setCategories] = useState<Category[]>([])
  const [list, setList] = useState<EntryList | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [q, setQ] = useState('')
  const [dq, setDq] = useState('')
  const [month, setMonth] = useState(currentMonth())
  const [category, setCategory] = useState('')
  const [method, setMethod] = useState('')
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
      })
      if (id === reqId.current) { setList(r); setError(null) }
    } catch (e) {
      if (id === reqId.current) setError(isApiError(e) ? e.message : 'Could not load entries')
    }
  }, [month, dq, category, method])
  useEffect(() => { void load() }, [load])

  // Arrived via the N shortcut from another page.
  useEffect(() => {
    if ((location.state as { focusAmount?: number } | null)?.focusAmount) {
      document.getElementById(AMOUNT_ID)?.focus()
    }
  }, [location.state, categories])

  const months = Array.from({ length: 13 }, (_, i) => shiftMonth(currentMonth(), -i))
  if (month && !months.includes(month)) months.push(month)
  const filtered = Boolean(dq || category || method)

  return (
    <>
      <PageTitle>Entries</PageTitle>
      <QuickAddBar api={api} categories={categories} onSaved={() => { void load(); void refresh() }} />
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
          <label htmlFor="f-cat" className="sr-only-text">Category</label>
          <select id="f-cat" value={category} onChange={(e) => setCategory(e.target.value)} className={`${inputClass} w-auto`}>
            <option value="">All categories</option>
            {categories.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
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
          <p className="border-t border-border pt-3 text-xs text-text-muted" aria-live="polite">
            {list.count} {list.count === 1 ? 'entry' : 'entries'} · {formatRM(list.total_sen)}
            {list.voided_count > 0 && ` · ${list.voided_count} voided (not counted)`}
          </p>
        </Card>
      )}
    </>
  )
}
