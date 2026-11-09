// Money is integer sen. Formatting and parsing use integer/string math only.

/** 123456 -> "RM 1,234.56". Negative values get a leading minus. */
export function formatRM(sen: number): string {
  return `RM ${formatAmount(sen)}`
}

/** 123456 -> "1,234.56" (no currency prefix). */
export function formatAmount(sen: number): string {
  const neg = sen < 0
  const abs = Math.abs(Math.trunc(sen))
  const whole = Math.floor(abs / 100)
  const frac = abs - whole * 100
  const grouped = String(whole).replace(/\B(?=(\d{3})+(?!\d))/g, ',')
  return `${neg ? '-' : ''}${grouped}.${String(frac).padStart(2, '0')}`
}

/** Whole-ringgit display for chart labels: 128450 -> "1,285" (rounded with integers). */
export function formatRMWhole(sen: number): string {
  const whole = Math.floor((Math.abs(sen) + 50) / 100)
  return String(whole).replace(/\B(?=(\d{3})+(?!\d))/g, ',')
}

/**
 * Parse user text ("45.90", "1,200", "12.5") into sen. Returns null for
 * junk, negatives, more than 2 decimals, or empty input. Never uses floats.
 */
export function parseRMToSen(input: string): number | null {
  const s = input.trim().replace(/^RM\s*/i, '').replace(/,/g, '')
  if (!/^\d+(\.\d{1,2})?$/.test(s) && !/^\d*\.\d{1,2}$/.test(s)) return null
  const [w = '', f = ''] = s.split('.')
  const sen = Number(w || '0') * 100 + Number(f.padEnd(2, '0') || '0')
  if (!Number.isSafeInteger(sen)) return null
  return sen
}

/** sen -> editable text "45.90". */
export function senToInput(sen: number): string {
  const whole = Math.floor(sen / 100)
  return `${whole}.${String(sen - whole * 100).padStart(2, '0')}`
}
