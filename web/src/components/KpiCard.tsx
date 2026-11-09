export function KpiCard({ label, value, sub, valueClass = '' }: { label: string; value: string; sub: string; valueClass?: string }) {
  return (
    <div className="rounded-card border border-border bg-surface px-5 py-4">
      <p className="text-[13px] text-text-muted">{label}</p>
      <p className={`my-1 font-display text-[28px] font-semibold leading-tight ${valueClass}`}>{value}</p>
      <p className="text-xs text-text-muted">{sub}</p>
    </div>
  )
}
