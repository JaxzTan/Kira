import type { SVGProps } from 'react'

const base = { width: 16, height: 16, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 2, strokeLinecap: 'round', strokeLinejoin: 'round', 'aria-hidden': true } as const
type P = SVGProps<SVGSVGElement>

export const IconPlus = (p: P) => <svg {...base} {...p}><path d="M12 5v14M5 12h14" /></svg>
export const IconChevronLeft = (p: P) => <svg {...base} {...p}><path d="m15 18-6-6 6-6" /></svg>
export const IconChevronRight = (p: P) => <svg {...base} {...p}><path d="m9 18 6-6-6-6" /></svg>
export const IconShield = (p: P) => <svg {...base} {...p}><path d="M12 3 4 6v6c0 4.5 3.3 8 8 9 4.7-1 8-4.5 8-9V6l-8-3Z" /><path d="m9 12 2 2 4-4" /></svg>
export const IconShieldX = (p: P) => <svg {...base} {...p}><path d="M12 3 4 6v6c0 4.5 3.3 8 8 9 4.7-1 8-4.5 8-9V6l-8-3Z" /><path d="m10 10 4 4M14 10l-4 4" /></svg>
export const IconPaperclip = (p: P) => <svg {...base} {...p}><path d="m21 11-9.2 9.2a5 5 0 0 1-7-7l9.2-9.2a3.5 3.5 0 0 1 5 5l-9.2 9.2a2 2 0 0 1-3-3l8.5-8.5" /></svg>
export const IconFile = (p: P) => <svg {...base} width={28} height={28} strokeWidth={1.6} {...p}><path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8l-5-5Z" /><path d="M14 3v5h5" /></svg>
export const IconLock = (p: P) => <svg {...base} width={14} height={14} {...p}><rect x="5" y="11" width="14" height="9" rx="2" /><path d="M8 11V8a4 4 0 0 1 8 0v3" /></svg>
export const IconCheck = (p: P) => <svg {...base} {...p}><path d="m5 12 5 5 9-10" /></svg>
export const IconGoogle = (p: P) => (
  <svg width="20" height="20" viewBox="0 0 48 48" aria-hidden="true" {...p}>
    <path fill="#EA4335" d="M24 9.5c3.5 0 6.6 1.2 9.1 3.6l6.8-6.8C35.8 2.4 30.3 0 24 0 14.6 0 6.5 5.4 2.6 13.2l7.9 6.1C12.4 13.6 17.7 9.5 24 9.5Z" />
    <path fill="#4285F4" d="M46.5 24.5c0-1.6-.1-3.1-.4-4.5H24v9h12.7c-.6 3-2.3 5.5-4.8 7.2l7.5 5.8c4.4-4.1 7.1-10.1 7.1-17.5Z" />
    <path fill="#FBBC05" d="M10.5 28.7a14.5 14.5 0 0 1 0-9.4l-7.9-6.1a24 24 0 0 0 0 21.6l7.9-6.1Z" />
    <path fill="#34A853" d="M24 48c6.5 0 11.9-2.1 15.9-5.8l-7.5-5.8c-2.1 1.4-4.9 2.3-8.4 2.3-6.3 0-11.6-4.1-13.5-9.8l-7.9 6.1C6.5 42.6 14.6 48 24 48Z" />
  </svg>
)
