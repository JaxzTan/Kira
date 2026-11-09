import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, isApiError } from '../api'
import type { Category, Entry, EntryHistory, EntryInput, Method } from '../api/types'
import { LIMITS, METHODS } from '../api/types'
import { HistoryTimeline } from '../components/HistoryTimeline'
import { IconFile } from '../components/icons'
import { useLedger } from '../components/ledger-context'
import { VerifiedBadge } from '../components/VerifiedBadge'
import { Button, Card, ErrorNote, Field, Spinner, inputClass } from '../components/ui'
import { longDate, shortHash } from '../lib/dates'
import { formatRM, parseRMToSen, senToInput } from '../lib/money'

const methodLabel = (m: string) => METHODS.find((x) => x.value === m)?.label ?? m

function statusText(e: Entry): string {
  if (e.status === 'voided') return 'Voided'
  if (e.status === 'amended') return e.amend_count === 1 ? 'Amended once' : `Amended ${e.amend_count} times`
  return 'Active'
}

function Meta({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <dt className="text-xs font-semibold text-text-muted">{label}</dt>
      <dd className="m-0 mt-0.5 text-[15px] font-semibold">{children}</dd>
    </div>
  )
}

function ReceiptPreview({ sha }: { sha: string | null }) {
  const [imgOk, setImgOk] = useState(true)
  useEffect(() => setImgOk(true), [sha])
  const box = 'mb-5 grid min-h-[180px] place-items-center rounded-control border border-dashed border-border-strong bg-bg p-4 text-center'
  if (!sha) {
    return <div className={box}><p className="text-sm text-danger">No receipt attached</p></div>
  }
  const url = api.receiptUrl(sha)
  return (
    <div className={box}>
      {imgOk ? (
        <a href={url} target="_blank" rel="noreferrer" aria-label="Open receipt in a new tab">
          <img src={url} alt="Receipt" onError={() => setImgOk(false)} className="max-h-[320px] max-w-full rounded-[6px]" />
        </a>
      ) : (
        <div className="flex flex-col items-center gap-1 text-text-muted">
          <IconFile />
          <a href={url} target="_blank" rel="noreferrer" className="text-sm font-semibold text-accent">Open receipt (PDF)</a>
        </div>
      )}
      <p className="mt-2 font-mono text-[11px] text-text-muted">sha256 {shortHash(sha)}</p>
    </div>
  )
}

function CorrectForm({ entry, categories, onDone, onCancel }: { entry: Entry; categories: Category[]; onDone: () => void; onCancel: () => void }) {
  const [date, setDate] = useState(entry.occurred_on)
  const [amount, setAmount] = useState(senToInput(entry.amount_sen))
  const [categoryId, setCategoryId] = useState(entry.category_id)
  const [payee, setPayee] = useState(entry.payee)
  const [method, setMethod] = useState<Method>(entry.method)
  const [note, setNote] = useState(entry.note ?? '')
  const [receipt, setReceipt] = useState<string | null>(entry.receipt_sha256)
  const [fields, setFields] = useState<Record<string, string>>({})
  const [error, setError] = useState<string | null>(null)
  const [needsConfirm, setNeedsConfirm] = useState(false)
  const [busy, setBusy] = useState(false)

  const selectable = categories.filter((c) => !c.archived_at || c.id === entry.category_id)

  async function submit(e?: FormEvent, confirm = false) {
    e?.preventDefault()
    const sen = parseRMToSen(amount)
    if (sen === null || sen <= 0) { setFields({ amount_sen: 'Enter an amount like 45.90' }); return }
    const input: EntryInput = {
      occurred_on: date, amount_sen: sen, category_id: categoryId, payee: payee.trim(), method,
      note: note.trim() ? note.trim() : null, receipt_sha256: receipt, ...(confirm ? { confirm_date: true } : {}),
    }
    setBusy(true); setFields({}); setError(null)
    try {
      await api.amendEntry(entry.id, input)
      onDone()
    } catch (err) {
      if (isApiError(err) && err.code === 'date_confirm_required') setNeedsConfirm(true)
      else if (isApiError(err) && err.code === 'validation' && err.fields) setFields(err.fields)
      else setError(isApiError(err) ? err.message : 'Could not save the correction')
    } finally { setBusy(false) }
  }
  async function upload(file?: File) {
    if (!file) return
    try { setReceipt((await api.uploadReceipt(file)).sha256) } catch (err) { setError(isApiError(err) ? err.message : 'Receipt upload failed') }
  }

  return (
    <form onSubmit={(e) => void submit(e)} noValidate aria-label="Correct entry" className="mb-2 flex flex-col gap-3">
      <p className="text-sm text-text-muted">Saving creates a new amendment row. The original stays in the history.</p>
      <div className="grid grid-cols-[repeat(auto-fit,minmax(180px,1fr))] gap-3">
        <Field label="Date" htmlFor="c-date" error={fields.occurred_on}>
          <input id="c-date" type="date" value={date} onChange={(e) => { setDate(e.target.value); setNeedsConfirm(false) }} className={inputClass} />
        </Field>
        <Field label="Amount (RM)" htmlFor="c-amount" error={fields.amount_sen}>
          <input id="c-amount" inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} className={`${inputClass} text-right font-semibold`} />
        </Field>
        <Field label="Category" htmlFor="c-cat" error={fields.category_id}>
          <select id="c-cat" value={categoryId} onChange={(e) => setCategoryId(Number(e.target.value))} className={inputClass}>
            {selectable.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
          </select>
        </Field>
        <Field label="Payee" htmlFor="c-payee" error={fields.payee}>
          <input id="c-payee" value={payee} maxLength={LIMITS.maxPayeeLen} onChange={(e) => setPayee(e.target.value)} className={inputClass} />
        </Field>
        <Field label="Paid with" htmlFor="c-method" error={fields.method}>
          <select id="c-method" value={method} onChange={(e) => setMethod(e.target.value as Method)} className={inputClass}>
            {METHODS.map((m) => <option key={m.value} value={m.value}>{m.label}</option>)}
          </select>
        </Field>
        <Field label="Receipt" htmlFor="c-receipt" error={fields.receipt_sha256}>
          <div className="flex items-center gap-2">
            <input id="c-receipt" type="file" accept="image/jpeg,image/png,application/pdf" onChange={(e) => void upload(e.target.files?.[0])} className="min-w-0 flex-1 text-sm" />
            {receipt && <button type="button" onClick={() => setReceipt(null)} className="h-control cursor-pointer text-sm text-danger underline">Remove</button>}
          </div>
        </Field>
      </div>
      <Field label="Note" htmlFor="c-note" error={fields.note}>
        <input id="c-note" value={note} maxLength={LIMITS.maxNoteLen} onChange={(e) => setNote(e.target.value)} className={inputClass} />
      </Field>
      {needsConfirm && (
        <div role="alert" className="flex flex-wrap items-center gap-3 rounded-control border border-border-strong bg-bg px-3 py-2 text-sm">
          <span>{date} is more than a year from today. Is that date correct?</span>
          <Button variant="outline" className="h-9" onClick={() => void submit(undefined, true)}>Yes, use this date</Button>
        </div>
      )}
      <ErrorNote message={error} />
      <div className="flex gap-3">
        <Button type="submit" disabled={busy}>{busy ? 'Saving…' : 'Save correction'}</Button>
        <Button variant="outline" onClick={onCancel}>Cancel</Button>
      </div>
    </form>
  )
}

function VoidDialog({ entry, onDone, onCancel }: { entry: Entry; onDone: () => void; onCancel: () => void }) {
  const [reason, setReason] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const ref = useRef<HTMLTextAreaElement>(null)
  useEffect(() => ref.current?.focus(), [])

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (!reason.trim()) { setError('A reason is required'); ref.current?.focus(); return }
    setBusy(true); setError(null)
    try {
      await api.voidEntry(entry.id, { reason: reason.trim() })
      onDone()
    } catch (err) {
      setError(isApiError(err) ? (err.fields?.reason ?? err.message) : 'Could not void the entry')
    } finally { setBusy(false) }
  }

  return (
    <form
      role="dialog" aria-labelledby="void-title" onSubmit={(e) => void submit(e)} noValidate
      onKeyDown={(e) => { if (e.key === 'Escape') onCancel() }}
      className="mt-4 rounded-control border border-danger/40 bg-red-50/50 p-4"
    >
      <h2 id="void-title" className="mb-1 font-display text-base font-semibold">Void this entry?</h2>
      <p className="mb-3 text-sm text-text-muted">It stays in the ledger, struck through, and no longer counts towards totals. This can’t be undone.</p>
      <Field label="Reason (required)" htmlFor="void-reason" error={error}>
        <textarea
          id="void-reason" ref={ref} rows={2} maxLength={LIMITS.maxVoidReasonLen} value={reason}
          onChange={(e) => setReason(e.target.value)}
          aria-invalid={error ? true : undefined}
          className="w-full rounded-control border border-border-strong bg-surface p-3 text-[15px] aria-[invalid=true]:border-danger"
        />
      </Field>
      <div className="mt-3 flex gap-3">
        <Button type="submit" variant="danger-outline" disabled={busy}>{busy ? 'Voiding…' : 'Void entry'}</Button>
        <Button variant="outline" onClick={onCancel}>Cancel</Button>
      </div>
    </form>
  )
}

export default function EntryDetail() {
  const id = Number(useParams().id)
  const [data, setData] = useState<EntryHistory | null>(null)
  const [categories, setCategories] = useState<Category[]>([])
  const [error, setError] = useState<string | null>(null)
  const [mode, setMode] = useState<'view' | 'edit' | 'void'>('view')
  const { verify, refresh } = useLedger()

  const load = useCallback(async () => {
    try {
      setData(await api.history(id))
      setError(null)
    } catch (e) {
      setError(isApiError(e) && e.code === 'not_found' ? 'Entry not found.' : isApiError(e) ? e.message : 'Could not load entry')
    }
  }, [id])
  useEffect(() => { void load() }, [load])
  useEffect(() => { api.listCategories(true).then((r) => setCategories(r.categories), () => {}) }, [])

  const done = () => { setMode('view'); void load(); void refresh() }

  if (error) {
    return (
      <>
        <Crumb id={id} />
        <ErrorNote message={error} />
      </>
    )
  }
  if (!data) return <Spinner />
  const e = data.entry
  const voided = e.status === 'voided'

  return (
    <>
      <Crumb id={e.id} />
      <div className="grid grid-cols-[repeat(auto-fit,minmax(340px,1fr))] items-start gap-5 min-[1100px]:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)]">
        <Card>
          <div className="mb-5 flex flex-wrap items-start justify-between gap-3">
            <div>
              <h1 className={`font-display text-2xl font-semibold leading-tight ${voided ? 'text-text-muted line-through' : ''}`}>{e.payee}</h1>
              <p className="text-[15px] text-text-muted">{e.category_name}</p>
            </div>
            <p className={`font-display text-[34px] font-bold leading-none ${voided ? 'text-text-muted line-through' : ''}`}>{formatRM(e.amount_sen)}</p>
          </div>
          {mode === 'edit' ? (
            <CorrectForm entry={e} categories={categories} onDone={done} onCancel={() => setMode('view')} />
          ) : (
            <>
              <dl className="mb-5 grid grid-cols-3 gap-4 max-[520px]:grid-cols-2">
                <Meta label="Date">{longDate(e.occurred_on)}</Meta>
                <Meta label="Paid with">{methodLabel(e.method)}</Meta>
                <Meta label="Status"><span className={voided ? 'text-danger' : ''}>{statusText(e)}</span></Meta>
                <div className="col-span-full"><Meta label="Note">{e.note || <span className="font-normal text-text-muted">—</span>}</Meta></div>
                {voided && <div className="col-span-full"><Meta label="Void reason"><span className="text-danger">{e.void_reason}</span></Meta></div>}
              </dl>
              <ReceiptPreview sha={e.receipt_sha256} />
              {!voided && mode === 'view' && (
                <div className="flex flex-wrap gap-3">
                  <Button variant="outline" onClick={() => setMode('edit')}>Correct entry</Button>
                  <Button variant="danger-outline" onClick={() => setMode('void')}>Void…</Button>
                </div>
              )}
              {mode === 'void' && <VoidDialog entry={e} onDone={done} onCancel={() => setMode('view')} />}
            </>
          )}
        </Card>
        <Card className="px-6 py-5">
          <h2 className="mb-4 font-display text-base font-semibold">History</h2>
          <div className="mb-5"><HistoryTimeline rows={data.rows} categories={categories} /></div>
          <VerifiedBadge verify={verify} />
        </Card>
      </div>
    </>
  )
}

function Crumb({ id }: { id: number }) {
  return (
    <nav aria-label="Breadcrumb" className="mb-3 text-[13px] text-text-muted">
      <Link to="/entries" className="font-semibold text-accent no-underline hover:underline">Entries</Link>
      <span className="mx-2" aria-hidden="true">/</span>
      <span className="font-mono">#{id}</span>
    </nav>
  )
}
