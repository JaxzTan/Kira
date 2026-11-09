import { useEffect } from 'react'
import { Outlet, useNavigate } from 'react-router-dom'
import { LedgerBanner } from './LedgerStatus'
import { LedgerProvider } from './ledger-context'
import { SidebarNav } from './SidebarNav'

import type { Direction } from '../api/types'
import { AMOUNT_ID, NEW_ENTRY_EVENT } from '../lib/ids'

function isTypingTarget(t: EventTarget | null): boolean {
  if (!(t instanceof HTMLElement)) return false
  return t.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(t.tagName)
}

/** `N` = new expense, `I` = new income (anywhere outside inputs) -> /entries, set type, focus Amount. */
export function useNewEntryShortcut() {
  const navigate = useNavigate()
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const k = e.key.toLowerCase()
      if ((k !== 'n' && k !== 'i') || e.ctrlKey || e.metaKey || e.altKey || e.defaultPrevented) return
      if (isTypingTarget(e.target) || document.querySelector('[role="dialog"]')) return
      e.preventDefault()
      goAddEntry(navigate, k === 'i' ? 'income' : 'expense')
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [navigate])
}

export function goAddEntry(navigate: ReturnType<typeof useNavigate>, direction: Direction) {
  if (document.getElementById(AMOUNT_ID)) {
    window.dispatchEvent(new CustomEvent(NEW_ENTRY_EVENT, { detail: direction }))
  } else {
    navigate('/entries', { state: { focusAmount: Date.now(), direction } })
  }
}

export const goAddExpense = (navigate: ReturnType<typeof useNavigate>) => goAddEntry(navigate, 'expense')

export function AppShell() {
  useNewEntryShortcut()
  return (
    <LedgerProvider>
      <div className="flex min-h-full flex-col">
        <LedgerBanner />
        <div className="flex flex-1 flex-wrap items-start">
          <SidebarNav />
          <main className="min-w-0 flex-[1_1_560px] px-10 py-8 max-[800px]:px-5">
            <div className="mx-auto max-w-[1240px]">
              <Outlet />
            </div>
          </main>
        </div>
      </div>
    </LedgerProvider>
  )
}
