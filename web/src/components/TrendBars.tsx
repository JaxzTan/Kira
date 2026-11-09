import type { MonthTotal } from '../api/types'
import { monthShort } from '../lib/dates'
import { formatRM, formatRMWhole } from '../lib/money'

const HEIGHT = 160

export function TrendBars({ items, current }: { items: MonthTotal[]; current: string }) {
  const max = Math.max(1, ...items.map((i) => i.total_sen))
  return (
    <ul className="flex items-end justify-between gap-3 px-2" style={{ height: HEIGHT + 44 }}>
      {items.map((m) => {
        const isCur = m.month === current
        const h = Math.max(m.total_sen > 0 ? 4 : 0, Math.round((m.total_sen * HEIGHT) / max))
        return (
          <li key={m.month} className="flex flex-1 flex-col items-center justify-end gap-1.5" title={`${monthShort(m.month)}: ${formatRM(m.total_sen)}`}>
            <span className="text-[11px] text-text-muted">{formatRMWhole(m.total_sen)}</span>
            <div className={`w-full max-w-11 rounded-t-[6px] ${isCur ? 'bg-accent' : 'bg-[#B8DBD4]'}`} style={{ height: h }} />
            <span className={`text-xs ${isCur ? 'font-semibold text-text' : 'text-text-muted'}`}>{monthShort(m.month)}</span>
            <span className="sr-only-text">{formatRM(m.total_sen)}</span>
          </li>
        )
      })}
    </ul>
  )
}
