// In-memory KiraApi for VITE_MOCK_API=1 and tests. Enforces the same semantics as docs/api-contract.md:
// append-only rows (amend/void add rows, never mutate), list includes voided but totals exclude them.
import { currentMonth, monthRange, shiftMonth, todayISO } from '../lib/dates'
import { ApiError } from './errors'
import type {
  AppendResult, Category, CategoryCreate, CategoryPatch, Entry, EntryFilter, EntryInput, KiraApi,
  Direction, LedgerRow, Method, Receipt, Status, Summary, VerifyResult,
} from './types'
import { LIMITS, METHODS } from './types'

const ZERO_HASH = '0'.repeat(64)
const CAT_SEED: Array<[string, string]> = [
  ['Cloud & hosting', '#0F766E'], ['Software & subscriptions', '#2F6FB5'], ['Hardware', '#C2701E'],
  ['Internet & phone', '#7A5BB0'], ['Travel & transport', '#9AA09B'], ['Meals & entertainment', '#0F766E'],
  ['Professional fees', '#2F6FB5'], ['Marketing', '#C2701E'], ['Office', '#7A5BB0'], ['Other', '#9AA09B'],
]
const INCOME_CAT_SEED: Array<[string, string]> = [
  ['Client revenue', '#0F766E'], ['Product sales', '#2F6FB5'], ['Refunds & rebates', '#C2701E'], ['Other income', '#9AA09B'],
]
const EXPENSE_COUNT = CAT_SEED.length

/** Non-cryptographic stand-in hash, 64 hex chars. Real hashing is server-side (HMAC). */
function fakeHash(s: string): string {
  let out = ''
  let h = 2166136261
  for (let r = 0; r < 8; r++) {
    for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619) >>> 0
    h = Math.imul(h ^ (r + 1), 2654435761) >>> 0
    out += h.toString(16).padStart(8, '0')
  }
  return out
}

const PDF_URL = 'data:application/pdf;base64,JVBERi0xLjQK'
function svgReceipt(label: string): string {
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="320" height="420"><rect width="320" height="420" fill="#FAFAF8"/><text x="24" y="48" font-family="monospace" font-size="18">${label}</text><text x="24" y="80" font-family="monospace" font-size="12" fill="#5B6168">sample receipt (mock)</text></svg>`
  return `data:image/svg+xml;utf8,${encodeURIComponent(svg)}`
}

// [monthsAgo, day, sen, categoryIdx, payee, method, note, receipt: 'img' | 'pdf' | null]
type Seed = [number, number, number, number, string, Method, string | null, 'img' | 'pdf' | null]
const SEEDS: Seed[] = [
  [5, 3, 6400, 0, 'DigitalOcean', 'card', 'Droplet', 'img'],
  [5, 14, 39900, 2, 'Shopee — keyboard', 'bank_transfer', null, null],
  [5, 22, 7300, 1, 'Figma', 'card', null, 'img'],
  [4, 2, 21000, 0, 'AWS', 'card', 'Compute', 'pdf'],
  [4, 11, 11200, 3, 'Unifi', 'bank_transfer', 'Fibre', 'img'],
  [4, 19, 28000, 4, 'Grab', 'tng_ewallet', 'Client visit', null],
  [3, 5, 9800, 1, 'Notion', 'card', null, 'img'],
  [3, 17, 15000, 6, 'Accountant', 'bank_transfer', 'Quarterly filing', 'pdf'],
  [2, 4, 18000, 7, 'Meta Ads', 'card', 'Campaign', 'img'],
  [2, 20, 4200, 5, 'Kopitiam', 'cash', 'Lunch with client', null],
  [1, 6, 11200, 0, 'DigitalOcean', 'card', null, 'img'],
  [1, 15, 6710, 1, 'Figma', 'card', null, 'img'],
  [1, 24, 8900, 8, 'Popular — stationery', 'cash', null, null],
  [0, 2, 8900, 9, 'Shopee — USB-C hub', 'bank_transfer', null, null],
  [0, 4, 6710, 1, 'Figma', 'card', null, 'img'],
  [0, 6, 11200, 0, 'DigitalOcean', 'card', 'Droplet', 'img'],
  [0, 7, 3150, 5, 'Starbucks', 'card', 'Coffee', null],
  [0, 8, 4800, 1, 'GitHub Copilot', 'card', 'Monthly plan, Nov', 'pdf'],
  [0, 9, 2340, 4, 'Grab', 'tng_ewallet', 'Airport', null],
  [0, 9, 4590, 0, 'Cloudflare', 'card', 'R2 + domain renewal', 'img'],
  // Income (category index >= EXPENSE_COUNT): client invoices, product sales, a refund.
  [5, 10, 450000, EXPENSE_COUNT, 'Acme Sdn Bhd — invoice #1041', 'bank_transfer', 'Website build, milestone 1', null],
  [4, 8, 380000, EXPENSE_COUNT, 'Acme Sdn Bhd — invoice #1042', 'bank_transfer', 'Website build, milestone 2', null],
  [4, 21, 68000, EXPENSE_COUNT + 1, 'Gumroad payout', 'bank_transfer', null, null],
  [3, 12, 520000, EXPENSE_COUNT, 'Bintang Retail — invoice #1043', 'bank_transfer', 'Retainer, Q3', null],
  [2, 9, 15000, EXPENSE_COUNT + 2, 'Shopee — keyboard refund', 'bank_transfer', 'Returned, wrong layout', null],
  [2, 25, 410000, EXPENSE_COUNT, 'Acme Sdn Bhd — invoice #1044', 'bank_transfer', null, null],
  [1, 14, 62000, EXPENSE_COUNT + 1, 'Gumroad payout', 'bank_transfer', null, null],
  [1, 28, 600000, EXPENSE_COUNT, 'Bintang Retail — invoice #1045', 'bank_transfer', 'Retainer, Q4', null],
  [0, 3, 350000, EXPENSE_COUNT, 'Acme Sdn Bhd — invoice #1046', 'bank_transfer', 'Maintenance', null],
  [0, 5, 7500, EXPENSE_COUNT + 3, 'Maybank — interest', 'bank_transfer', null, null],
]

export interface MockControls {
  /** Simulate tamper detection so the broken-ledger banner can be seen. */
  breakLedger(atId?: number): void
  reset(): void
}

export function createMockApi(opts: { seed?: boolean } = {}): KiraApi & { controls: MockControls } {
  const seedData = opts.seed ?? true
  let rows: LedgerRow[] = []
  let categories: Category[] = []
  let receipts = new Map<string, { meta: Receipt; url: string }>()
  let broken: { at: number; reason: string } | null = null
  let acknowledgedHead: string | null = null
  let lastBackup: string | null = null

  const nowIso = () => new Date().toISOString().replace(/\.\d+Z$/, 'Z')

  const dirOf = (catId: number | null): Direction => categories.find((c) => c.id === catId)?.direction ?? 'expense'

  function append(partial: Omit<LedgerRow, 'id' | 'prev_hash' | 'hash' | 'created_at' | 'currency' | 'direction'> & { direction?: LedgerRow['direction'] }, createdAt?: string): LedgerRow {
    const prev = rows.length ? rows[rows.length - 1].hash : ZERO_HASH
    const id = rows.length + 1
    const row: LedgerRow = {
      direction: 'expense', currency: 'MYR', ...partial,
      id, created_at: createdAt ?? nowIso(), prev_hash: prev, hash: '',
    }
    row.hash = fakeHash(prev + JSON.stringify({ ...row, hash: undefined }))
    rows = [...rows, row]
    return row
  }

  function reset() {
    rows = []
    receipts = new Map()
    broken = null
    acknowledgedHead = null
    categories = [
      ...CAT_SEED.map(([name, color], i): Category => ({ id: i + 1, name, color, icon: null, archived_at: null, direction: 'expense' })),
      ...INCOME_CAT_SEED.map(([name, color], i): Category => ({ id: EXPENSE_COUNT + i + 1, name, color, icon: null, archived_at: null, direction: 'income' })),
    ]
    lastBackup = new Date(Date.now() - 12_000).toISOString()
    if (!seedData) return
    const now = new Date()
    const cur = currentMonth(now)
    const imgShas: string[] = []
    SEEDS.forEach(([mAgo, day, sen, cat, payee, method, note, rcpt], i) => {
      const month = shiftMonth(cur, -mAgo)
      const d = mAgo === 0 ? Math.min(day, now.getDate()) : day
      const occurred = `${month}-${String(d).padStart(2, '0')}`
      let sha: string | null = null
      if (rcpt) {
        sha = fakeHash(`receipt-${i}`)
        if (payee === 'GitHub Copilot') sha = '3e9a' + sha.slice(4, 60) + 'c41f'
        const ext = rcpt === 'pdf' ? 'pdf' : 'png'
        receipts.set(sha, {
          meta: { sha256: sha, ext, content_type: rcpt === 'pdf' ? 'application/pdf' : 'image/png', size: 48213 },
          url: rcpt === 'pdf' ? PDF_URL : svgReceipt(payee),
        })
        imgShas.push(sha)
      }
      const created = `${occurred}T0${2 + (i % 7)}:0${i % 6}:00Z`
      const row = append({
        kind: 'create', ref_id: null, direction: dirOf(cat + 1), occurred_on: occurred, amount_sen: sen, category_id: cat + 1,
        payee, method, note, void_reason: null, receipt_sha256: sha,
      }, created)
      if (payee === 'GitHub Copilot') {
        append({
          kind: 'amend', ref_id: row.id, direction: dirOf(cat + 1), occurred_on: occurred, amount_sen: 4480, category_id: cat + 1,
          payee, method, note, void_reason: null, receipt_sha256: sha,
        }, `${occurred}T13:14:00Z`)
      }
      if (payee === 'Starbucks') {
        append({
          kind: 'void', ref_id: row.id, direction: dirOf(cat + 1), occurred_on: null, amount_sen: null, category_id: null,
          payee: null, method: null, note: null, void_reason: 'Personal expense, not business', receipt_sha256: null,
        }, `${occurred}T18:30:00Z`)
      }
    })
  }
  reset()

  const catName = (id: number) => categories.find((c) => c.id === id)?.name ?? 'Unknown'

  function currentView(): Entry[] {
    const out: Entry[] = []
    for (const c of rows) {
      if (c.kind !== 'create') continue
      const mine = rows.filter((r) => r.ref_id === c.id)
      const amends = mine.filter((r) => r.kind === 'amend')
      const voidRow = mine.find((r) => r.kind === 'void')
      const latest = amends.length ? amends[amends.length - 1] : c
      const last = mine.length ? mine[mine.length - 1] : c
      const status: Status = voidRow ? 'voided' : amends.length ? 'amended' : 'active'
      out.push({
        id: c.id, version_id: latest.id, direction: latest.direction,
        occurred_on: latest.occurred_on!, amount_sen: latest.amount_sen!, currency: latest.currency,
        category_id: latest.category_id!, category_name: catName(latest.category_id!),
        payee: latest.payee!, method: latest.method!, note: latest.note,
        receipt_sha256: latest.receipt_sha256, status, amend_count: amends.length,
        void_reason: voidRow?.void_reason ?? null,
        created_at: c.created_at, updated_at: last.created_at, hash: last.hash,
      })
    }
    return out.sort((a, b) => (a.occurred_on === b.occurred_on ? b.id - a.id : a.occurred_on < b.occurred_on ? 1 : -1))
  }

  function totals(list: Entry[]) {
    const live = list.filter((e) => e.status !== 'voided')
    const total_sen = live.filter((e) => e.direction === 'expense').reduce((s, e) => s + e.amount_sen, 0)
    const income_sen = live.filter((e) => e.direction === 'income').reduce((s, e) => s + e.amount_sen, 0)
    return {
      total_sen,
      income_sen,
      net_sen: income_sen - total_sen,
      count: live.length,
      voided_count: list.length - live.length,
    }
  }

  function guardWrite() {
    if (broken && acknowledgedHead !== rows[rows.length - 1]?.hash) {
      throw new ApiError(423, { error: 'Ledger verification failed; acknowledge to continue', code: 'ledger_broken', fields: null })
    }
  }

  function validate(input: EntryInput, defaultDirection: Direction = 'expense') {
    const fields: Record<string, string> = {}
    if (!Number.isInteger(input.amount_sen) || input.amount_sen <= 0) fields.amount_sen = 'must be greater than 0'
    else if (input.amount_sen >= LIMITS.maxAmountSen) fields.amount_sen = 'must be less than RM 10,000,000'
    if (!/^\d{4}-\d{2}-\d{2}$/.test(input.occurred_on) || Number.isNaN(Date.parse(input.occurred_on))) fields.occurred_on = 'must be a valid date'
    const direction = input.direction ?? defaultDirection
    if (direction !== 'expense' && direction !== 'income') fields.direction = 'must be expense or income'
    const cat = categories.find((c) => c.id === input.category_id)
    if (!cat || cat.archived_at) fields.category_id = 'choose an existing, active category'
    else if (!fields.direction && cat.direction !== direction) fields.category_id = `category is for ${cat.direction === 'income' ? 'income' : 'expenses'}`
    if (!METHODS.some((m) => m.value === input.method)) fields.method = 'invalid payment method'
    const payee = (input.payee ?? '').trim()
    if (payee.length < 1 || payee.length > LIMITS.maxPayeeLen) fields.payee = `required, 1–${LIMITS.maxPayeeLen} characters`
    if (input.note && input.note.length > LIMITS.maxNoteLen) fields.note = `at most ${LIMITS.maxNoteLen} characters`
    if (input.receipt_sha256 && !receipts.has(input.receipt_sha256)) fields.receipt_sha256 = 'unknown receipt'
    if (Object.keys(fields).length) throw new ApiError(422, { error: 'Validation failed', code: 'validation', fields })
    if (!input.confirm_date) {
      const diff = Math.abs(Date.parse(input.occurred_on) - Date.parse(todayISO())) / 86_400_000
      if (diff > LIMITS.dateConfirmDays) {
        throw new ApiError(422, { error: 'Date is more than a year away; confirm to continue', code: 'date_confirm_required', fields: null })
      }
    }
    return { payee, note: input.note?.trim() ? input.note.trim() : null }
  }

  const find = (id: number) => {
    const e = currentView().find((x) => x.id === id)
    if (!e) throw new ApiError(404, { error: 'Entry not found', code: 'not_found', fields: null })
    return e
  }

  function resultFor(row: LedgerRow): AppendResult {
    const ref = row.ref_id ?? row.id
    return { ...row, entry: find(ref) }
  }

  const api: KiraApi & { controls: MockControls } = {
    controls: {
      reset,
      breakLedger(at = 3) { broken = { at, reason: 'hash mismatch' } },
    },
    async me() { return { email: 'owner@example.com', name: 'Jaxz', picture: null } },
    async logout() { /* session cleared in real server */ },
    async listEntries(f: EntryFilter = {}) {
      let list = currentView()
      if (f.from) list = list.filter((e) => e.occurred_on >= f.from!)
      if (f.to) list = list.filter((e) => e.occurred_on <= f.to!)
      if (f.category) list = list.filter((e) => e.category_id === f.category)
      if (f.method) list = list.filter((e) => e.method === f.method)
      if (f.q) {
        const q = f.q.toLowerCase()
        list = list.filter((e) => e.payee.toLowerCase().includes(q) || (e.note ?? '').toLowerCase().includes(q))
      }
      if (f.direction && f.direction !== 'all') list = list.filter((e) => e.direction === f.direction)
      if (f.status === 'active') list = list.filter((e) => e.status !== 'voided')
      if (f.status === 'voided') list = list.filter((e) => e.status === 'voided')
      return { entries: list, ...totals(list) }
    },
    async getEntry(id) { return find(id) },
    async createEntry(input) {
      guardWrite()
      const { payee, note } = validate(input)
      const row = append({
        kind: 'create', ref_id: null, direction: input.direction ?? 'expense', occurred_on: input.occurred_on,
        amount_sen: input.amount_sen, category_id: input.category_id, payee, method: input.method, note,
        void_reason: null, receipt_sha256: input.receipt_sha256,
      })
      return resultFor(row)
    },
    async history(id) {
      const entry = find(id)
      return { entry, rows: rows.filter((r) => r.id === id || r.ref_id === id) }
    },
    async amendEntry(id, input) {
      guardWrite()
      const cur = find(id)
      if (cur.status === 'voided') throw new ApiError(409, { error: 'Entry is voided', code: 'conflict', fields: null })
      const { payee, note } = validate(input, cur.direction)
      const row = append({
        kind: 'amend', ref_id: id, direction: input.direction ?? cur.direction, occurred_on: input.occurred_on,
        amount_sen: input.amount_sen, category_id: input.category_id, payee, method: input.method, note,
        void_reason: null, receipt_sha256: input.receipt_sha256,
      })
      return resultFor(row)
    },
    async voidEntry(id, input) {
      guardWrite()
      const cur = find(id)
      if (cur.status === 'voided') throw new ApiError(409, { error: 'Entry already voided', code: 'conflict', fields: null })
      const reason = (input.reason ?? '').trim()
      if (reason.length < 1 || reason.length > LIMITS.maxVoidReasonLen) {
        throw new ApiError(422, { error: 'Validation failed', code: 'validation', fields: { reason: `required, 1–${LIMITS.maxVoidReasonLen} characters` } })
      }
      const row = append({
        kind: 'void', ref_id: id, direction: cur.direction, occurred_on: null, amount_sen: null, category_id: null, payee: null,
        method: null, note: null, void_reason: reason, receipt_sha256: null,
      })
      return resultFor(row)
    },
    async uploadReceipt(file) {
      if (file.size > LIMITS.maxReceiptBytes) throw new ApiError(413, { error: 'Receipt too large (max 10 MB)', code: 'payload_too_large', fields: null })
      const ext: Receipt['ext'] | null = file.type === 'application/pdf' ? 'pdf' : file.type === 'image/png' ? 'png' : file.type === 'image/jpeg' ? 'jpg' : null
      if (!ext) throw new ApiError(415, { error: 'Receipt must be JPEG, PNG or PDF', code: 'unsupported_media_type', fields: null })
      const sha = fakeHash(`${file.name}|${file.size}|${file.lastModified}|${file.type}`)
      const meta: Receipt = { sha256: sha, ext, content_type: file.type, size: file.size }
      const url = typeof URL !== 'undefined' && URL.createObjectURL ? URL.createObjectURL(file) : PDF_URL
      receipts.set(sha, { meta, url })
      return meta
    },
    receiptUrl(sha) { return receipts.get(sha)?.url ?? PDF_URL },
    async listCategories(includeArchived = false) {
      return { categories: categories.filter((c) => includeArchived || !c.archived_at).map((c) => ({ ...c })) }
    },
    async createCategory(input: CategoryCreate) {
      const name = input.name.trim()
      if (name.length < 1 || name.length > LIMITS.maxCategoryLen) {
        throw new ApiError(422, { error: 'Validation failed', code: 'validation', fields: { name: `required, 1–${LIMITS.maxCategoryLen} characters` } })
      }
      if (categories.some((c) => c.name.toLowerCase() === name.toLowerCase())) {
        throw new ApiError(409, { error: 'A category with that name already exists', code: 'conflict', fields: null })
      }
      const direction = input.direction ?? 'expense'
      if (direction !== 'expense' && direction !== 'income') {
        throw new ApiError(422, { error: 'Validation failed', code: 'validation', fields: { direction: 'must be expense or income' } })
      }
      const cat: Category = { id: categories.length + 1, name, color: input.color ?? CAT_SEED[categories.length % 5][1], icon: null, archived_at: null, direction }
      categories = [...categories, cat]
      return { ...cat }
    },
    async patchCategory(id, input: CategoryPatch) {
      const cat = categories.find((c) => c.id === id)
      if (!cat) throw new ApiError(404, { error: 'Category not found', code: 'not_found', fields: null })
      const next = { ...cat }
      if (input.name !== undefined) {
        const name = input.name.trim()
        if (name.length < 1 || name.length > LIMITS.maxCategoryLen) {
          throw new ApiError(422, { error: 'Validation failed', code: 'validation', fields: { name: `required, 1–${LIMITS.maxCategoryLen} characters` } })
        }
        if (categories.some((c) => c.id !== id && c.name.toLowerCase() === name.toLowerCase())) {
          throw new ApiError(409, { error: 'A category with that name already exists', code: 'conflict', fields: null })
        }
        next.name = name
      }
      if (input.archived !== undefined) next.archived_at = input.archived ? nowIso() : null
      categories = categories.map((c) => (c.id === id ? next : c))
      return { ...next }
    },
    async summary(month): Promise<Summary> {
      const m = month ?? currentMonth()
      const all = currentView()
      const inMonth = (mm: string) => {
        const { from, to } = monthRange(mm)
        return all.filter((e) => e.occurred_on >= from && e.occurred_on <= to)
      }
      const list = inMonth(m)
      const t = totals(list)
      const prevT = totals(inMonth(shiftMonth(m, -1)))
      const prev = prevT.total_sen
      const live = list.filter((e) => e.status !== 'voided' && e.direction === 'expense')
      const byCat = new Map<number, number>()
      live.forEach((e) => byCat.set(e.category_id, (byCat.get(e.category_id) ?? 0) + e.amount_sen))
      const by_category = [...byCat.entries()]
        .map(([cid, total]) => {
          const c = categories.find((x) => x.id === cid)!
          return {
            category_id: cid, name: c.name, color: c.color, total_sen: total,
            pct: t.total_sen ? Math.round((total * 1000) / t.total_sen) / 10 : 0,
          }
        })
        .sort((a, b) => b.total_sen - a.total_sen)
      const trend = Array.from({ length: 6 }, (_, i) => {
        const mm = shiftMonth(m, i - 5)
        const tt = totals(inMonth(mm))
        return { month: mm, total_sen: tt.total_sen, income_sen: tt.income_sen }
      })
      return {
        month: m, ...t, prev_month_total_sen: prev, prev_month_income_sen: prevT.income_sen,
        change_pct: prev === 0 ? null : Math.round(((t.total_sen - prev) * 1000) / prev) / 10,
        missing_receipts: live.filter((e) => e.receipt_sha256 === null).length,
        by_category, trend, recent: all.slice(0, 5),
      }
    },
    exportUrl(from, to, format) { return `/api/export?from=${from}&to=${to}&format=${format}` },
    async verify(): Promise<VerifyResult> {
      const head = rows[rows.length - 1]
      return {
        ok: broken === null, entries: rows.length, head_id: head?.id ?? 0, head_hash: head?.hash ?? ZERO_HASH,
        broken_at: broken?.at ?? null, reason: broken?.reason ?? null, checked_at: nowIso(),
        acknowledged: broken !== null && acknowledgedHead === head?.hash, last_backup_at: lastBackup,
      }
    },
    async acknowledge() {
      acknowledgedHead = rows[rows.length - 1]?.hash ?? null
      return api.verify()
    },
  }
  return api
}
