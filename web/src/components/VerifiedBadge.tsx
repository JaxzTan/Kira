import type { VerifyResult } from '../api/types'
import { IconShield, IconShieldX } from './icons'

export function VerifiedBadge({ verify }: { verify: VerifyResult | null }) {
  if (!verify) return null
  if (verify.ok) {
    return (
      <div className="flex items-center gap-2 rounded-control bg-accent-soft px-3 py-2.5 text-sm text-accent">
        <IconShield /> Chain verified · originals are never overwritten
      </div>
    )
  }
  return (
    <div role="alert" className="flex items-center gap-2 rounded-control bg-red-50 px-3 py-2.5 text-sm text-danger">
      <IconShieldX /> Chain broken at entry #{verify.broken_at} · history can’t be trusted
    </div>
  )
}
