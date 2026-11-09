import { Link, useNavigate } from 'react-router-dom'
import type { Entry } from '../api/types'
import { METHODS } from '../api/types'
import { shortDate } from '../lib/dates'
import { formatRM } from '../lib/money'

const methodLabel = (m: string) => METHODS.find((x) => x.value === m)?.label ?? m

/** Dashboard variant omits the # and Receipt columns (`compact`). */
export function EntriesTable({ entries, compact = false, emptyText = 'No entries yet.' }: { entries: Entry[]; compact?: boolean; emptyText?: string }) {
  const navigate = useNavigate()
  const th = 'py-2.5 pr-4 text-left text-xs font-semibold text-text-muted'
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[640px] border-collapse text-[15px]">
        <thead>
          <tr className="border-b border-border">
            {!compact && <th scope="col" className={th}>#</th>}
            <th scope="col" className={th}>Date</th>
            <th scope="col" className={th}>Payee</th>
            <th scope="col" className={th}>Category</th>
            <th scope="col" className={th}>Paid with</th>
            {!compact && <th scope="col" className={th}>Receipt</th>}
            <th scope="col" className={`${th} pr-0 text-right`}>Amount</th>
          </tr>
        </thead>
        <tbody>
          {entries.length === 0 && (
            <tr><td colSpan={compact ? 5 : 7} className="py-8 text-center text-sm text-text-muted">{emptyText}</td></tr>
          )}
          {entries.map((e) => {
            const voided = e.status === 'voided'
            const muted = voided ? 'text-text-muted line-through' : ''
            const payee = voided ? `${e.payee} — voided` : e.status === 'amended' ? `${e.payee} (edited)` : e.payee
            return (
              <tr
                key={e.id}
                onClick={() => navigate(`/entries/${e.id}`)}
                className={`cursor-pointer border-b border-border last:border-b-0 hover:bg-bg ${muted}`}
              >
                {!compact && <td className="py-3 pr-4 font-mono text-xs text-text-muted">#{e.id}</td>}
                <td className="whitespace-nowrap py-3 pr-4">{shortDate(e.occurred_on)}</td>
                <td className="py-3 pr-4 font-semibold">
                  <Link to={`/entries/${e.id}`} onClick={(ev) => ev.stopPropagation()} className="text-inherit no-underline hover:underline">
                    {payee}
                  </Link>
                </td>
                <td className="py-3 pr-4">{e.category_name}</td>
                <td className="whitespace-nowrap py-3 pr-4">{methodLabel(e.method)}</td>
                {!compact && (
                  <td className={`py-3 pr-4 text-sm ${voided ? 'text-text-muted' : e.receipt_sha256 ? 'text-accent' : 'text-danger'}`}>
                    {e.receipt_sha256 ? 'Attached' : 'Missing'}
                  </td>
                )}
                <td className="whitespace-nowrap py-3 text-right font-semibold">{formatRM(e.amount_sen)}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
