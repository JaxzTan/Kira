import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { useLocation } from 'react-router-dom'
import { api } from '../api'
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
  const { pathname } = useLocation()

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

  useEffect(() => {
    void refresh()
  }, [refresh, pathname])

  const value = useMemo(() => ({ verify, failed, refresh, acknowledge }), [verify, failed, refresh, acknowledge])
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

export const useLedger = () => useContext(Ctx)
