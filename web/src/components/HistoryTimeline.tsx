import type { Category, LedgerRow } from '../api/types'
import { METHODS } from '../api/types'
import { longDate, shortDateTime, shortHash } from '../lib/dates'
import { formatRM } from '../lib/money'

const methodLabel = (m: string | null) => METHODS.find((x) => x.value === m)?.label ?? m ?? '—'

export function diffSummary(prev: LedgerRow | undefined, row: LedgerRow, catName: (id: number | null) => string): string {
  if (row.kind === 'void') return `Reason: ${row.void_reason ?? ''}`
  if (row.kind === 'create' || !prev) {
    return `${row.direction === 'income' ? 'Income · ' : ''}${formatRM(row.amount_sen ?? 0)} · ${catName(row.category_id)} · ${methodLabel(row.method)}`
  }
  const parts: string[] = []
  if (prev.direction !== row.direction) parts.push(`Type ${prev.direction} → ${row.direction}`)
  if (prev.amount_sen !== row.amount_sen) parts.push(`Amount ${formatRM(prev.amount_sen ?? 0)} → ${formatRM(row.amount_sen ?? 0)}`)
  if (prev.occurred_on !== row.occurred_on) parts.push(`Date ${longDate(prev.occurred_on ?? '')} → ${longDate(row.occurred_on ?? '')}`)
  if (prev.category_id !== row.category_id) parts.push(`Category ${catName(prev.category_id)} → ${catName(row.category_id)}`)
  if (prev.payee !== row.payee) parts.push(`Payee ${prev.payee} → ${row.payee}`)
  if (prev.method !== row.method) parts.push(`Paid with ${methodLabel(prev.method)} → ${methodLabel(row.method)}`)
  if ((prev.note ?? '') !== (row.note ?? '')) parts.push(`Note “${prev.note ?? ''}” → “${row.note ?? ''}”`)
  if (prev.receipt_sha256 !== row.receipt_sha256) parts.push(row.receipt_sha256 ? 'Receipt attached or replaced' : 'Receipt removed')
  return parts.length ? parts.join('; ') : 'No field changes'
}

const KIND_LABEL = { create: 'Created', amend: 'Amended', void: 'Voided' } as const

/** rows are the chain rows for one entry, ascending; shown newest first. */
export function HistoryTimeline({ rows, categories }: { rows: LedgerRow[]; categories: Category[] }) {
  const catName = (id: number | null) => categories.find((c) => c.id === id)?.name ?? (id === null ? '—' : `#${id}`)
  const ordered = [...rows].sort((a, b) => b.id - a.id)
  return (
    <ol className="m-0 list-none p-0">
      {ordered.map((row, i) => {
        const prev = rows.filter((r) => r.id < row.id && r.kind !== 'void').pop()
        const dot = row.kind === 'void' ? 'bg-danger' : i === 0 ? 'bg-accent' : 'bg-cat-5'
        return (
          <li key={row.id} className="relative flex gap-3 pb-5 last:pb-0">
            <div className="flex flex-col items-center pt-1.5">
              <span className={`h-2.5 w-2.5 rounded-full ${dot}`} aria-hidden="true" />
              {i < ordered.length - 1 && <span className="mt-1 w-px flex-1 bg-border-strong" aria-hidden="true" />}
            </div>
            <div className="min-w-0 flex-1">
              <div className="flex justify-between gap-3">
                <span className="text-sm font-semibold">{KIND_LABEL[row.kind]}</span>
                <time dateTime={row.created_at} className="text-[13px] text-text-muted">{shortDateTime(row.created_at)}</time>
              </div>
              <p className="my-0.5 break-words text-sm text-text">{diffSummary(prev, row, catName)}</p>
              <p className="font-mono text-[11px] text-text-muted">#{row.id} · {shortHash(row.hash)}</p>
            </div>
          </li>
        )
      })}
    </ol>
  )
}
