import { useState } from 'react'
import { isApiError } from '../api/errors'
import { relativeTime, shortHash } from '../lib/dates'
import { IconShield, IconShieldX } from './icons'
import { useLedger } from './ledger-context'

/** Sidebar footer card + top-of-page red banner when the chain is broken. */
export function LedgerStatus() {
  const { verify, failed } = useLedger()
  if (failed && !verify) {
    return <div className="rounded-control border border-white/15 p-3 text-xs text-text-on-dark-muted">Ledger status unavailable</div>
  }
  if (!verify) {
    return <div className="rounded-control border border-white/15 p-3 text-xs text-text-on-dark-muted">Checking ledger…</div>
  }
  return (
    <div className="rounded-control border border-white/15 p-3 text-xs leading-relaxed text-text-on-dark" aria-live="polite">
      <div className={`mb-1 flex items-center gap-1.5 font-semibold ${verify.ok ? 'text-accent-on-dark' : 'text-red-300'}`}>
        {verify.ok ? <IconShield width={14} height={14} /> : <IconShieldX width={14} height={14} />}
        {verify.ok ? 'Ledger verified' : verify.acknowledged ? 'Ledger broken (acknowledged)' : 'Ledger broken'}
      </div>
      <div>{verify.entries} entries · head <span className="font-mono">{shortHash(verify.head_hash)}</span></div>
      <div className="mt-1 text-text-on-dark-muted">Last backup {relativeTime(verify.last_backup_at)}</div>
    </div>
  )
}

export function LedgerBanner() {
  const { verify, acknowledge } = useLedger()
  const [busy, setBusy] = useState(false)
  const [ackError, setAckError] = useState<string | null>(null)
  async function onAck() {
    if (busy) return
    setBusy(true); setAckError(null)
    try { await acknowledge() } catch (e) { setAckError(isApiError(e) ? e.message : 'Could not acknowledge. Try again.') } finally { setBusy(false) }
  }
  if (!verify || verify.ok || verify.acknowledged) return null
  return (
    <div role="alert" className="flex w-full flex-wrap items-center justify-between gap-3 bg-danger px-6 py-3 text-white">
      <p className="text-sm font-semibold">
        Ledger verification failed at entry #{verify.broken_at}{verify.reason ? ` (${verify.reason})` : ''}. Writes are disabled until you acknowledge.
      </p>
      {ackError && <p className="w-full text-sm font-semibold">{ackError}</p>}
      <button
        type="button"
        disabled={busy}
        onClick={() => void onAck()}
        className="h-control cursor-pointer rounded-control border border-white bg-transparent px-5 text-sm font-semibold text-white hover:bg-white/15 disabled:cursor-not-allowed disabled:opacity-60"
      >
        {busy ? 'Acknowledging…' : 'Acknowledge'}
      </button>
    </div>
  )
}
