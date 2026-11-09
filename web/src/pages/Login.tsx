import { IconGoogle, IconLock } from '../components/icons'

const MOCK = import.meta.env.VITE_MOCK_API === '1'

export default function Login() {
  const denied = new URLSearchParams(window.location.search).get('error') === 'forbidden'
  return (
    <main className="on-dark grid min-h-full place-items-center bg-sidebar p-6 text-text-on-dark">
      <div className="w-full max-w-[420px]">
        <span aria-hidden="true" className="mb-6 grid h-[52px] w-[52px] place-items-center rounded-[14px] bg-accent font-display text-2xl font-bold text-white">K</span>
        <h1 className="mb-4 font-display text-[40px] font-bold leading-none">Kira</h1>
        <p className="mb-8 text-[17px] leading-relaxed text-text-on-dark-muted">
          Business expenses, recorded the moment they happen. Append-only. Yours only.
        </p>
        {denied && <p role="alert" className="mb-4 rounded-control border border-white/30 p-3 text-sm">This account is not allowed.</p>}
        <a
          href={MOCK ? '/' : '/auth/google/login'}
          className="flex h-[52px] items-center justify-center gap-3 rounded-control bg-white text-[15px] font-semibold text-text no-underline hover:bg-gray-100"
        >
          <IconGoogle /> Continue with Google
        </a>
        <p className="mt-7 flex items-start gap-3 rounded-control border border-white/15 p-4 text-[13px] leading-relaxed text-text-on-dark-muted">
          <IconLock className="mt-0.5 shrink-0" />
          <span>Private ledger. Only one Google account is allowed in; every other sign-in is refused and logged.</span>
        </p>
      </div>
    </main>
  )
}
