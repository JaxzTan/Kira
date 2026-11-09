import type { MonthTotal } from '../api/types'
import { monthShort } from '../lib/dates'
import { formatRM, formatRMWhole } from '../lib/money'

const HEIGHT = 160

/** Paired bars per month: expense (solid / pale teal) beside income (accent outline on accent-soft). */
export function TrendBars({ items, current }: { items: MonthTotal[]; current: string }) {
  const max = Math.max(1, ...items.map((i) => Math.max(i.total_sen, i.income_sen)))
  const barH = (sen: number) => Math.max(sen > 0 ? 4 : 0, Math.round((sen * HEIGHT) / max))
  return (
    <>
      <ul className="m-0 flex list-none items-end justify-between gap-3 px-2" style={{ height: HEIGHT + 44 }}>
        {items.map((m) => {
          const isCur = m.month === current
          return (
            <li
              key={m.month} className="flex flex-1 flex-col items-center justify-end gap-1.5"
              title={`${monthShort(m.month)}: spent ${formatRM(m.total_sen)}, income ${formatRM(m.income_sen)}`}
            >
              <div className="flex w-full items-end justify-center gap-1">
                <div className="flex flex-1 flex-col items-center gap-1" style={{ maxWidth: 28 }}>
                  <span className="text-[10px] leading-none text-text-muted">{m.total_sen > 0 ? formatRMWhole(m.total_sen) : ''}</span>
                  <div className={`w-full rounded-t-[6px] ${isCur ? 'bg-accent' : 'bg-[#B8DBD4]'}`} style={{ height: barH(m.total_sen) }} />
                </div>
                <div className="flex flex-1 flex-col items-center gap-1" style={{ maxWidth: 28 }}>
                  <span className={`text-[10px] leading-none ${isCur ? 'font-semibold text-accent' : 'text-text-muted'}`}>{m.income_sen > 0 ? formatRMWhole(m.income_sen) : ''}</span>
                  <div
                    className={`w-full rounded-t-[6px] border-2 border-b-0 border-accent ${isCur ? 'bg-accent-soft' : 'bg-accent-soft/60'}`}
                    style={{ height: barH(m.income_sen) }}
                  />
                </div>
              </div>
              <span className={`text-xs ${isCur ? 'font-semibold text-text' : 'text-text-muted'}`}>{monthShort(m.month)}</span>
              <span className="sr-only-text">Spent {formatRM(m.total_sen)}, income {formatRM(m.income_sen)}</span>
            </li>
          )
        })}
      </ul>
      <p className="mt-3 flex justify-center gap-5 text-xs text-text-muted" aria-hidden="true">
        <span className="inline-flex items-center gap-1.5"><span className="h-2.5 w-2.5 rounded-[3px] bg-accent" /> Spent</span>
        <span className="inline-flex items-center gap-1.5"><span className="h-2.5 w-2.5 rounded-[3px] border-2 border-accent bg-accent-soft" /> Income</span>
      </p>
    </>
  )
}
