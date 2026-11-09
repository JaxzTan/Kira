import type { AnchorHTMLAttributes, ButtonHTMLAttributes, ReactNode } from 'react'

type Variant = 'primary' | 'outline' | 'danger-outline'

const variants: Record<Variant, string> = {
  primary: 'bg-accent text-white hover:bg-accent-hover border border-accent',
  outline: 'bg-surface text-accent border border-accent hover:bg-accent-soft',
  'danger-outline': 'bg-surface text-danger border border-danger hover:bg-red-50',
}
const baseBtn =
  'inline-flex h-control min-w-control items-center justify-center gap-2 rounded-control px-5 text-[15px] font-semibold ' +
  'cursor-pointer select-none no-underline disabled:cursor-not-allowed disabled:opacity-60 aria-disabled:cursor-not-allowed aria-disabled:opacity-60'

type BtnProps = { variant?: Variant } & ButtonHTMLAttributes<HTMLButtonElement>
export function Button({ variant = 'primary', className = '', type = 'button', ...rest }: BtnProps) {
  return <button type={type} className={`${baseBtn} ${variants[variant]} ${className}`} {...rest} />
}

type LinkBtnProps = { variant?: Variant } & AnchorHTMLAttributes<HTMLAnchorElement>
/** Anchor styled as a button (downloads, external navigation). */
export function ButtonLink({ variant = 'primary', className = '', ...rest }: LinkBtnProps) {
  return <a className={`${baseBtn} ${variants[variant]} ${className}`} {...rest} />
}

export const inputClass =
  'h-control w-full rounded-control border border-border-strong bg-surface px-3 text-[15px] text-text ' +
  'placeholder:text-text-muted aria-[invalid=true]:border-danger'

export function Field({
  label, htmlFor, error, className = '', children, hint,
}: { label: string; htmlFor: string; error?: string | null; className?: string; children: ReactNode; hint?: string }) {
  return (
    <div className={`flex min-w-0 flex-col gap-1 ${className}`}>
      <label htmlFor={htmlFor} className="text-xs font-semibold text-text-muted">{label}</label>
      {children}
      {hint && !error && <p className="text-xs text-text-muted">{hint}</p>}
      {error && <p id={`${htmlFor}-error`} role="alert" className="text-xs text-danger">{error}</p>}
    </div>
  )
}

export function Card({ className = '', children, ...rest }: { className?: string; children: ReactNode } & React.HTMLAttributes<HTMLDivElement>) {
  return <div className={`rounded-card border border-border bg-surface p-6 ${className}`} {...rest}>{children}</div>
}

export function PageTitle({ children, right }: { children: ReactNode; right?: ReactNode }) {
  return (
    <div className="mb-6 flex flex-wrap items-center justify-between gap-4">
      <h1 className="font-display text-[28px] font-semibold leading-tight">{children}</h1>
      {right}
    </div>
  )
}

export function ErrorNote({ message, onRetry }: { message: string | null; onRetry?: () => void }) {
  if (!message) return null
  return (
    <div role="alert" className="mb-3 flex flex-wrap items-center justify-between gap-3 rounded-control border border-danger/30 bg-red-50 px-3 py-2 text-sm text-danger">
      <span>{message}</span>
      {onRetry && (
        <button type="button" onClick={onRetry} className="cursor-pointer rounded-control border border-danger bg-white px-3 py-1 text-sm font-semibold text-danger hover:bg-red-100">
          Retry
        </button>
      )}
    </div>
  )
}

export function Spinner({ label = 'Loading…' }: { label?: string }) {
  return <p role="status" className="py-10 text-center text-sm text-text-muted">{label}</p>
}
