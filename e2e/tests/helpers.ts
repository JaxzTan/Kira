import { APIRequestContext, expect, Page } from '@playwright/test'
import zlib from 'node:zlib'

export interface Entry {
  id: number; amount_sen: number; payee: string; status: string; note: string | null
  category_id: number; category_name: string; receipt_sha256: string | null; method: string
}

export const today = () => {
  const d = new Date()
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}
export const monthOf = (iso: string) => iso.slice(0, 7)
export const uniq = (p: string) => `${p}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 5)}`

export async function categories(api: APIRequestContext): Promise<{ id: number; name: string }[]> {
  const r = await api.get('/api/categories')
  expect(r.ok()).toBeTruthy()
  return (await r.json()).categories
}

export async function seed(api: APIRequestContext, e: {
  payee: string; amount_sen: number; category_id: number; method?: string; note?: string | null
  occurred_on?: string; receipt_sha256?: string | null
}): Promise<Entry> {
  const r = await api.post('/api/entries', {
    data: {
      occurred_on: e.occurred_on ?? today(), amount_sen: e.amount_sen, category_id: e.category_id,
      payee: e.payee, method: e.method ?? 'card', note: e.note ?? null, receipt_sha256: e.receipt_sha256 ?? null,
    },
  })
  expect(r.status(), await r.text()).toBe(201)
  return (await r.json()).entry
}

/** Row in the entries table by payee text. */
export const row = (page: Page, payee: string) => page.getByRole('row').filter({ hasText: payee })

/** Minimal valid RGB PNG (w x h, solid colour). */
export function makePng(w = 8, h = 8): Buffer {
  const crcTable = Array.from({ length: 256 }, (_, n) => {
    let c = n
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1
    return c >>> 0
  })
  const crc = (b: Buffer) => {
    let c = 0xffffffff
    for (const x of b) c = crcTable[(c ^ x) & 0xff] ^ (c >>> 8)
    return (c ^ 0xffffffff) >>> 0
  }
  const chunk = (type: string, data: Buffer) => {
    const len = Buffer.alloc(4); len.writeUInt32BE(data.length)
    const td = Buffer.concat([Buffer.from(type), data])
    const c = Buffer.alloc(4); c.writeUInt32BE(crc(td))
    return Buffer.concat([len, td, c])
  }
  const ihdr = Buffer.alloc(13)
  ihdr.writeUInt32BE(w, 0); ihdr.writeUInt32BE(h, 4); ihdr[8] = 8; ihdr[9] = 2
  const raw = Buffer.concat(Array.from({ length: h }, () => Buffer.concat([Buffer.from([0]), Buffer.from(Array(w).fill([30, 140, 120]).flat())])))
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk('IHDR', ihdr), chunk('IDAT', zlib.deflateSync(raw)), chunk('IEND', Buffer.alloc(0)),
  ])
}

/** Parse "RM 1,234.56" -> sen. */
export function rmToSen(s: string): number {
  const m = s.match(/RM\s*([\d,]+)\.(\d{2})/)
  if (!m) throw new Error(`no RM amount in "${s}"`)
  return Number(m[1].replace(/,/g, '')) * 100 + Number(m[2])
}
