import type { CategoryTotal } from '../api/types'
import { formatRM } from '../lib/money'

export function CategoryBars({ items }: { items: CategoryTotal[] }) {
  if (items.length === 0) return <p className="py-6 text-sm text-text-muted">No spending this month.</p>
  const max = items[0].total_sen
  return (
    <ul className="flex flex-col gap-3">
      {items.map((c, i) => (
        <li key={c.category_id}>
          <div className="mb-1 flex justify-between gap-3 text-[15px]">
            <span>{c.name}</span>
            <span className="font-semibold">{formatRM(c.total_sen)}</span>
          </div>
          <div className="h-2 overflow-hidden rounded-full bg-track" aria-hidden="true">
            <div
              className="h-full rounded-full"
              style={{ width: `${Math.round((c.total_sen * 10000) / max) / 100}%`, background: c.color ?? `var(--color-cat-${(i % 5) + 1})` }}
            />
          </div>
        </li>
      ))}
    </ul>
  )
}
