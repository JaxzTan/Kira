import { useEffect } from 'react'
import { Outlet, useNavigate } from 'react-router-dom'
import { LedgerBanner } from './LedgerStatus'
import { LedgerProvider } from './ledger-context'
import { SidebarNav } from './SidebarNav'

import { AMOUNT_ID } from '../lib/ids'

function isTypingTarget(t: EventTarget | null): boolean {
  if (!(t instanceof HTMLElement)) return false
  return t.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(t.tagName)
}

/** `N` anywhere (outside inputs) -> /entries and focus Amount. */
export function useNewEntryShortcut() {
  const navigate = useNavigate()
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() !== 'n' || e.ctrlKey || e.metaKey || e.altKey || e.defaultPrevented) return
      if (isTypingTarget(e.target) || document.querySelector('[role="dialog"]')) return
      e.preventDefault()
      goAddExpense(navigate)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [navigate])
}

export function goAddExpense(navigate: ReturnType<typeof useNavigate>) {
  const el = document.getElementById(AMOUNT_ID)
  if (el) {
    el.focus()
    ;(el as HTMLInputElement).select?.()
  } else {
    navigate('/entries', { state: { focusAmount: Date.now() } })
  }
}

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
