import { useState } from 'react'
import { api } from '../api'
import { ButtonLink, Card, Field, PageTitle, inputClass } from '../components/ui'
import { todayISO } from '../lib/dates'

export default function Export() {
  const today = todayISO()
  const [from, setFrom] = useState(`${today.slice(0, 4)}-01-01`)
  const [to, setTo] = useState(today)
  const invalid = !from || !to || from > to

  const link = (format: 'csv' | 'zip') => ({
    href: invalid ? undefined : api.exportUrl(from, to, format),
    'aria-disabled': invalid || undefined,
    download: true,
  })

  return (
    <>
      <PageTitle>Export</PageTitle>
      <Card className="max-w-[720px]">
        <h2 className="mb-1 font-display text-base font-semibold">Date range</h2>
        <p className="mb-4 text-sm text-text-muted">
          Includes every entry in the range, voided ones too (marked in the status column). Receipts are included in the ZIP.
        </p>
        <div className="mb-5 flex flex-wrap gap-4">
          <Field label="From" htmlFor="ex-from" className="w-[180px]">
            <input id="ex-from" type="date" value={from} max={to || undefined} onChange={(e) => setFrom(e.target.value)} className={inputClass} />
          </Field>
          <Field label="To" htmlFor="ex-to" className="w-[180px]">
            <input id="ex-to" type="date" value={to} min={from || undefined} onChange={(e) => setTo(e.target.value)} className={inputClass} />
          </Field>
        </div>
        {invalid && <p role="alert" className="mb-4 text-sm text-danger">Choose a valid range where From is not after To.</p>}
        <div className="flex flex-wrap gap-3">
          <ButtonLink variant="primary" {...link('csv')}>Download CSV</ButtonLink>
          <ButtonLink variant="outline" {...link('zip')}>Download ZIP with receipts</ButtonLink>
        </div>
        <p className="mt-5 text-xs text-text-muted">
          CSV columns: date, category, payee, method, amount_myr, note, status, entry_id, receipt_file.
        </p>
      </Card>
    </>
  )
}
