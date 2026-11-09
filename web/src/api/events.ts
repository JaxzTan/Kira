/** Tiny pub/sub so the fetch client can tell the UI about session-level failures. */
type Listener = () => void
const listeners: Record<'forbidden' | 'ledgerBroken', Set<Listener>> = {
  forbidden: new Set(),
  ledgerBroken: new Set(),
}

export function subscribe(kind: 'forbidden' | 'ledgerBroken', fn: Listener): () => void {
  listeners[kind].add(fn)
  return () => { listeners[kind].delete(fn) }
}

export function emit(kind: 'forbidden' | 'ledgerBroken') {
  listeners[kind].forEach((fn) => fn())
}

/** 401 handler: hard-navigate to /login unless already there (no redirect loop). Overridable for tests. */
export const sessionHooks = {
  onUnauthorized(): void {
    if (typeof window !== 'undefined' && window.location.pathname !== '/login') {
      window.location.assign('/login')
    }
  },
}
