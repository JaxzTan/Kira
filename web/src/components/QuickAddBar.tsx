import { useEffect, useRef, useState, type DragEvent, type KeyboardEvent } from 'react'
import { isApiError, isNetworkError } from '../api/errors'
import type { Category, Direction, Entry, EntryInput, KiraApi, Method, Receipt } from '../api/types'
import { LIMITS, METHODS } from '../api/types'
import { todayISO } from '../lib/dates'
import { formatIncome, formatRM, parseRMToSen } from '../lib/money'
import { AMOUNT_ID, NEW_ENTRY_EVENT } from '../lib/ids'
import { IconCheck, IconPaperclip } from './icons'
import { TypeSwitch } from './TypeSwitch'
import { Button, Field, inputClass } from './ui'

type Errors = Partial<Record<'date' | 'amount' | 'category' | 'payee' | 'method' | 'note' | 'receipt' | 'form', string>>

const FIELD_MAP: Record<string, keyof Errors> = {
  occurred_on: 'date', amount_sen: 'amount', category_id: 'category', payee: 'payee',
  method: 'method', note: 'note', receipt_sha256: 'receipt', direction: 'form',
}

interface Props {
  api: KiraApi
  categories: Category[]
  onSaved?: (entry: Entry) => void
  /** Create failed with no HTTP response: the server may or may not have saved it. Refetch the list. */
  onUncertain?: () => void
}

export const UPLOAD_WAIT_MESSAGE = 'Waiting for receipt upload…'
export const MAYBE_SAVED_MESSAGE = 'The server may have saved this — check the list before saving again.'

/**
 * Quick add (FR-3, FR-9). Tab order: Date, Type, Amount, Category, Payee, Paid with, Note, Receipt, Save.
 * Enter saves from any field. After save: amount/payee/note/receipt are cleared, date/category/method
 * kept, Amount refocused.
 */
export function QuickAddBar({ api, categories, onSaved, onUncertain }: Props) {
  const [date, setDate] = useState(todayISO())
  const [amount, setAmount] = useState('')
  const [direction, setDirection] = useState<Direction>('expense')
  // Last-used category per direction; falls back to the first category of that direction.
  const [catByDir, setCatByDir] = useState<Record<Direction, number | null>>({ expense: null, income: null })
  const [payee, setPayee] = useState('')
  const [method, setMethod] = useState<Method>('card')
  const [note, setNote] = useState('')
  const [receipt, setReceipt] = useState<Receipt | null>(null)
  const [uploading, setUploading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [errors, setErrors] = useState<Errors>({})
  const [confirmDate, setConfirmDate] = useState(false)
  const [needsConfirm, setNeedsConfirm] = useState(false)
  const [flash, setFlash] = useState<string | null>(null)
  const [dragging, setDragging] = useState(false)
  const amountRef = useRef<HTMLInputElement>(null)
  const fileRef = useRef<HTMLInputElement>(null)
  const uploadSeq = useRef(0)
  const uploadingRef = useRef(false)

  const dirCategories = categories.filter((c) => c.direction === direction)
  const remembered = catByDir[direction]
  const effectiveCategory = dirCategories.find((c) => c.id === remembered)?.id ?? dirCategories[0]?.id ?? null
  const isIncome = direction === 'income'
  const payeeLabel = isIncome ? 'From' : 'Payee'

  // `N` / `I` shortcuts (AppShell or Entries page) set the type and focus Amount.
  useEffect(() => {
    const onNew = (e: Event) => {
      const d = (e as CustomEvent<Direction>).detail
      if (d === 'expense' || d === 'income') setDirection(d)
      amountRef.current?.focus()
      amountRef.current?.select()
    }
    window.addEventListener(NEW_ENTRY_EVENT, onNew)
    return () => window.removeEventListener(NEW_ENTRY_EVENT, onNew)
  }, [])

  // Editing the date invalidates a previous "confirm this far-off date".
  useEffect(() => {
    setConfirmDate(false)
    setNeedsConfirm(false)
  }, [date])

  // A late upload result must never land on a form that was reset or unmounted.
  useEffect(() => () => { uploadSeq.current++ }, [])

  async function save(confirm = confirmDate) {
    if (saving) return
    if (uploadingRef.current) {
      setErrors((p) => ({ ...p, form: UPLOAD_WAIT_MESSAGE }))
      return
    }
    const next: Errors = {}
    const sen = parseRMToSen(amount)
    if (sen === null || sen <= 0) next.amount = 'Enter an amount like 45.90'
    else if (sen >= LIMITS.maxAmountSen) next.amount = 'Amount is too large'
    if (!payee.trim()) next.payee = `${payeeLabel} is required`
    if (effectiveCategory === null) next.category = 'Choose a category'
    if (!date) next.date = 'Choose a date'
    if (Object.keys(next).length) {
      setErrors(next)
      if (next.amount) amountRef.current?.focus()
      else if (next.date) document.getElementById('quick-date')?.focus()
      else if (next.payee) document.getElementById('quick-payee')?.focus()
      return
    }
    const input: EntryInput = {
      direction, occurred_on: date, amount_sen: sen!, category_id: effectiveCategory!, payee: payee.trim(), method,
      note: note.trim() ? note.trim() : null, receipt_sha256: receipt?.sha256 ?? null,
      ...(confirm ? { confirm_date: true } : {}),
    }
    setSaving(true)
    setErrors({})
    try {
      const res = await api.createEntry(input)
      setAmount('')
      setPayee('')
      setNote('')
      uploadSeq.current++
      setReceipt(null)
      setNeedsConfirm(false)
      setConfirmDate(false)
      setFlash(`Saved ${res.entry.direction === 'income' ? formatIncome(res.entry.amount_sen) : formatRM(res.entry.amount_sen)} · ${res.entry.payee}`)
      onSaved?.(res.entry)
      amountRef.current?.focus()
    } catch (e) {
      if (isApiError(e) && e.code === 'date_confirm_required') {
        setNeedsConfirm(true)
        document.getElementById('quick-date')?.focus()
      } else if (isApiError(e) && e.code === 'validation' && e.fields) {
        const mapped: Errors = {}
        for (const [k, v] of Object.entries(e.fields)) mapped[FIELD_MAP[k] ?? 'form'] = v
        setErrors(mapped)
      } else if (isNetworkError(e)) {
        setErrors({ form: MAYBE_SAVED_MESSAGE })
        onUncertain?.()
      } else {
        setErrors({ form: isApiError(e) ? e.message : 'Could not save. Try again.' })
      }
    } finally {
      setSaving(false)
    }
  }

  async function upload(file: File | undefined) {
    if (!file) return
    const mine = ++uploadSeq.current
    uploadingRef.current = true
    setUploading(true)
    setErrors((p) => ({ ...p, receipt: undefined, form: p.form === UPLOAD_WAIT_MESSAGE ? undefined : p.form }))
    try {
      const r = await api.uploadReceipt(file)
      if (mine === uploadSeq.current) setReceipt(r)
    } catch (e) {
      if (mine === uploadSeq.current) setErrors((p) => ({ ...p, receipt: isApiError(e) ? e.message : 'Receipt upload failed' }))
    } finally {
      if (mine === uploadSeq.current) { uploadingRef.current = false; setUploading(false) }
    }
  }

  function onKeyDown(e: KeyboardEvent<HTMLFormElement>) {
    const t = e.target as HTMLElement
    if (e.key === 'Enter' && (t.tagName === 'INPUT' || t.tagName === 'SELECT') && (t as HTMLInputElement).type !== 'file') {
      e.preventDefault()
      void save()
    }
  }
  function onDrop(e: DragEvent) {
    e.preventDefault()
    setDragging(false)
    void upload(e.dataTransfer.files[0])
  }

  const err = (k: keyof Errors) => errors[k]
  const inv = (k: keyof Errors, id: string) => (err(k) ? { 'aria-invalid': true, 'aria-describedby': `${id}-error` } : {})

  return (
    <form
      aria-label="Quick add"
      noValidate
      onSubmit={(e) => { e.preventDefault(); void save() }}
      onKeyDown={onKeyDown}
      onDragOver={(e) => { e.preventDefault(); setDragging(true) }}
      onDragLeave={() => setDragging(false)}
      onDrop={onDrop}
      className={`mb-5 rounded-card border bg-surface px-4 py-3.5 ${dragging ? 'border-accent bg-accent-soft' : 'border-accent'}`}
    >
      <div className="mb-2 flex flex-wrap items-baseline justify-between gap-2">
        <h2 className="text-[15px] font-semibold">Quick add</h2>
        <p className="text-xs text-text-muted">Tab between fields · Enter to save · drop a receipt here</p>
      </div>
      <div className="flex flex-wrap items-start gap-3">
        <Field label="Date" htmlFor="quick-date" error={err('date')} className="w-[150px]">
          <input id="quick-date" type="date" value={date} onChange={(e) => setDate(e.target.value)} className={inputClass} {...inv('date', 'quick-date')} />
        </Field>
        <TypeSwitch id="quick-type" value={direction} onChange={setDirection} />
        <Field label="Amount (RM)" htmlFor={AMOUNT_ID} error={err('amount')} className="flex-[1_1_140px]">
          <input
            id={AMOUNT_ID} ref={amountRef} inputMode="decimal" autoComplete="off" placeholder="0.00"
            value={amount} onChange={(e) => setAmount(e.target.value)}
            className={`${inputClass} text-right font-semibold`} {...inv('amount', AMOUNT_ID)}
          />
        </Field>
        <Field label="Category" htmlFor="quick-category" error={err('category')} className="flex-[1.2_1_180px]">
          <select id="quick-category" value={effectiveCategory ?? ''} onChange={(e) => setCatByDir((p) => ({ ...p, [direction]: Number(e.target.value) }))} className={inputClass} {...inv('category', 'quick-category')}>
            {dirCategories.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
          </select>
        </Field>
        <Field label={payeeLabel} htmlFor="quick-payee" error={err('payee')} className="flex-[1.4_1_180px]">
          <input id="quick-payee" autoComplete="off" maxLength={LIMITS.maxPayeeLen} value={payee} onChange={(e) => setPayee(e.target.value)} className={inputClass} {...inv('payee', 'quick-payee')} />
        </Field>
        <Field label="Paid with" htmlFor="quick-method" error={err('method')} className="w-[140px]">
          <select id="quick-method" value={method} onChange={(e) => setMethod(e.target.value as Method)} className={inputClass} {...inv('method', 'quick-method')}>
            {METHODS.map((m) => <option key={m.value} value={m.value}>{m.label}</option>)}
          </select>
        </Field>
      </div>
      <div className="mt-3 flex flex-wrap items-end gap-3">
        <Field label="Note" htmlFor="quick-note" error={err('note')} className="flex-[1_1_300px]">
          <input id="quick-note" autoComplete="off" maxLength={LIMITS.maxNoteLen} value={note} onChange={(e) => setNote(e.target.value)} className={inputClass} {...inv('note', 'quick-note')} />
        </Field>
        <div className="flex flex-col gap-1">
          <input
            ref={fileRef} type="file" accept="image/jpeg,image/png,application/pdf" tabIndex={-1} aria-hidden="true"
            className="hidden" data-testid="receipt-input"
            onChange={(e) => { void upload(e.target.files?.[0]); e.target.value = '' }}
          />
          <button
            type="button"
            onClick={() => fileRef.current?.click()}
            aria-label={receipt ? `Receipt attached (${receipt.ext}). Replace receipt` : 'Attach receipt'}
            title={receipt ? 'Receipt attached — click to replace' : 'Attach receipt (JPEG, PNG or PDF)'}
            className={`grid h-control w-control cursor-pointer place-items-center rounded-control border bg-surface ${receipt ? 'border-accent text-accent' : 'border-dashed border-border-strong text-text'}`}
          >
            {uploading ? <span className="text-xs">…</span> : receipt ? <IconCheck /> : <IconPaperclip />}
          </button>
        </div>
        <Button type="submit" disabled={saving || uploading} className="min-w-[72px]">{saving ? 'Saving…' : 'Save'}</Button>
      </div>
      {needsConfirm && (
        <div role="alert" className="mt-3 flex flex-wrap items-center gap-3 rounded-control border border-border-strong bg-bg px-3 py-2 text-sm">
          <span>{date} is more than a year from today. Is that date correct?</span>
          <Button variant="outline" className="h-9" onClick={() => { setConfirmDate(true); void save(true) }}>Yes, save with this date</Button>
        </div>
      )}
      {err('receipt') && <p role="alert" className="mt-2 text-xs text-danger">{err('receipt')}</p>}
      {err('form') && <p role="alert" className="mt-2 text-sm text-danger">{err('form')}</p>}
      <p role="status" className={flash ? "mt-2 text-xs text-accent" : "sr-only-text"}>{flash}</p>
    </form>
  )
}
