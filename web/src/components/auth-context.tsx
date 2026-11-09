import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { Navigate } from 'react-router-dom'
import { api, isApiError } from '../api'
import type { Me } from '../api/types'
import { Button } from './ui'

const MeCtx = createContext<Me | null>(null)
export const useMe = () => useContext(MeCtx)

/** Auth guard: api.me(); 401 -> /login. */
export function RequireAuth({ children }: { children: ReactNode }) {
  const [me, setMe] = useState<Me | null>(null)
  const [state, setState] = useState<'loading' | 'ok' | 'unauth' | 'error'>('loading')
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let live = true
    setState('loading')
    api.me().then(
      (m) => { if (live) { setMe(m); setState('ok') } },
      (e) => { if (live) setState(isApiError(e) && (e.status === 401 || e.status === 403) ? 'unauth' : 'error') },
    )
    return () => { live = false }
  }, [attempt])

  if (state === 'unauth') return <Navigate to="/login" replace />
  if (state === 'error') {
    return (
      <main className="grid min-h-full place-items-center p-8">
        <div className="text-center">
          <h1 className="font-display text-2xl font-semibold">Can’t reach Kira</h1>
          <p className="mb-4 mt-2 text-text-muted">Check that the server is running, then try again.</p>
          <Button onClick={() => setAttempt((n) => n + 1)}>Retry</Button>
        </div>
      </main>
    )
  }
  if (state === 'loading') return <p role="status" className="p-8 text-text-muted">Loading…</p>
  return <MeCtx.Provider value={me}>{children}</MeCtx.Provider>
}
