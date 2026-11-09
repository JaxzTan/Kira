const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
const MONTHS_LONG = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December']

const pad = (n: number) => String(n).padStart(2, '0')

export function todayISO(now = new Date()): string {
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`
}
export function currentMonth(now = new Date()): string {
  return todayISO(now).slice(0, 7)
}
export function shiftMonth(month: string, delta: number): string {
  const [y, m] = month.split('-').map(Number)
  const idx = y * 12 + (m - 1) + delta
  return `${Math.floor(idx / 12)}-${pad((idx % 12) + 1)}`
}
export function monthLabel(month: string): string {
  const [y, m] = month.split('-').map(Number)
  return `${MONTHS_LONG[m - 1]} ${y}`
}
export function monthShort(month: string): string {
  return MONTHS[Number(month.slice(5, 7)) - 1]
}
export function monthRange(month: string): { from: string; to: string } {
  const [y, m] = month.split('-').map(Number)
  const last = new Date(y, m, 0).getDate()
  return { from: `${month}-01`, to: `${month}-${pad(last)}` }
}
/** "2026-11-09" -> "9 Nov" */
export function shortDate(iso: string): string {
  return `${Number(iso.slice(8, 10))} ${MONTHS[Number(iso.slice(5, 7)) - 1]}`
}
/** "2026-11-08" -> "8 Nov 2026" */
export function longDate(iso: string): string {
  return `${shortDate(iso)} ${iso.slice(0, 4)}`
}
/** RFC3339 -> "8 Nov, 21:14" (local time) */
export function shortDateTime(ts: string): string {
  const d = new Date(ts)
  return `${d.getDate()} ${MONTHS[d.getMonth()]}, ${pad(d.getHours())}:${pad(d.getMinutes())}`
}
export function relativeTime(ts: string | null, now = Date.now()): string {
  if (!ts) return 'never'
  const s = Math.max(0, Math.round((now - new Date(ts).getTime()) / 1000))
  if (s < 60) return `${s} s ago`
  if (s < 3600) return `${Math.floor(s / 60)} min ago`
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`
  return `${Math.floor(s / 86400)} d ago`
}
export function shortHash(h: string | null | undefined): string {
  if (!h) return '—'
  if (h.includes('…')) return h
  return h.length > 10 ? `${h.slice(0, 4)}…${h.slice(-4)}` : h
}
