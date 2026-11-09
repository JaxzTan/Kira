import { useRef, type KeyboardEvent } from 'react'
import type { Direction } from '../api/types'

const OPTIONS: ReadonlyArray<{ value: Direction; label: string; on: string }> = [
  { value: 'expense', label: 'Expense', on: 'bg-text text-white' },
  { value: 'income', label: 'Income', on: 'bg-accent text-white' },
]

/**
 * Expense / Income segmented switch. A radio group: one Tab stop (the selected option),
 * arrow keys flip the selection.
 */
export function TypeSwitch({ value, onChange, id, label = 'Type' }: { value: Direction; onChange: (d: Direction) => void; id: string; label?: string }) {
  const refs = useRef<Record<string, HTMLButtonElement | null>>({})
  function onKey(e: KeyboardEvent) {
    const keys = ['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown']
    if (!keys.includes(e.key)) return
    e.preventDefault()
    const next: Direction = value === 'expense' ? 'income' : 'expense'
    onChange(next)
    refs.current[next]?.focus()
  }
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <span id={`${id}-label`} className="text-xs font-semibold text-text-muted">{label}</span>
      <div id={id} role="radiogroup" aria-labelledby={`${id}-label`} onKeyDown={onKey} className="inline-flex h-control rounded-control border border-border-strong bg-surface p-[3px]">
        {OPTIONS.map((o) => {
          const checked = o.value === value
          return (
            <button
              key={o.value} ref={(el) => { refs.current[o.value] = el }} type="button" role="radio" aria-checked={checked}
              tabIndex={checked ? 0 : -1} onClick={() => onChange(o.value)}
              className={`cursor-pointer rounded-[7px] px-3.5 text-sm font-semibold ${checked ? o.on : 'text-text hover:bg-bg'}`}
            >
              {o.label}
            </button>
          )
        })}
      </div>
    </div>
  )
}
