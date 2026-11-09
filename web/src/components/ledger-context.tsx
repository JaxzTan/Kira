import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { api, subscribe } from '../api'
import type { VerifyResult } from '../api/types'

interface LedgerCtx {
  verify: VerifyResult | null
  failed: boolean
  refresh: () => Promise<void>
  acknowledge: () => Promise<void>
}
const Ctx = createContext<LedgerCtx>({ verify: null, failed: false, refresh: async () => {}, acknowledge: async () => {} })

export function LedgerProvider({ children }: { children: ReactNode }) {
  const [verify, setVerify] = useState<VerifyResult | null>(null)
  const [failed, setFailed] = useState(false)

  const refresh = useCallback(async () => {
    try {
      setVerify(await api.verify())
      setFailed(false)
    } catch {
      setFailed(true)
    }
  }, [])
  const acknowledge = useCallback(async () => {
    setVerify(await api.acknowledge())
  }, [])

  // Verify recomputes the whole chain server-side: run on mount, after writes (callers use refresh())
  // and when the window regains focus, not on route changes.
  useEffect(() => {
    void refresh()
  }, [refresh])
  useEffect(() => {
    const onFocus = () => { void refresh() }
    window.addEventListener('focus', onFocus)
    return () => window.removeEventListener('focus', onFocus)
  }, [refresh])

  // Any write that returns 423 ledger_broken: re-verify so the red banner appears.
  useEffect(() => subscribe('ledgerBroken', () => { void refresh() }), [refresh])

  const value = useMemo(() => ({ verify, failed, refresh, acknowledge }), [verify, failed, refresh, acknowledge])
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

export const useLedger = () => useContext(Ctx)
