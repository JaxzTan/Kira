import { ApiError } from './errors'
import { emit, sessionHooks } from './events'
import type {
  AppendResult, Category, CategoryCreate, CategoryList, CategoryPatch, Entry, EntryFilter,
  EntryHistory, EntryInput, ErrorResponse, EntryList, ExportFormat, KiraApi, Me, Receipt, Summary,
  VerifyResult, VoidInput,
} from './types'

export const NETWORK_MESSAGE = 'Could not reach the server. Check your connection and try again.'
export const RATE_LIMIT_MESSAGE = 'Too many requests. Wait a moment and try again.'
export const FORBIDDEN_MESSAGE = 'This account is not allowed.'

async function request<T>(method: string, path: string, body?: unknown, form?: FormData): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  let payload: BodyInit | undefined
  if (form) {
    payload = form // browser sets multipart boundary
  } else if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
    payload = JSON.stringify(body)
  }
  let res: Response
  try {
    res = await fetch(path, { method, headers, body: payload, credentials: 'same-origin' })
  } catch {
    throw new ApiError(0, { error: NETWORK_MESSAGE, code: 'internal' })
  }
  if (res.status === 204) return undefined as T
  const text = await res.text()
  let data: unknown = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = null
    }
  }
  if (!res.ok) {
    const body = data && typeof data === 'object' && 'error' in data ? (data as Partial<ErrorResponse> & { error: string }) : null
    let err: ApiError
    if (res.status === 401) err = new ApiError(401, { code: 'unauthorized', ...body, error: body?.error ?? 'Please sign in.' })
    else if (res.status === 403) err = new ApiError(403, { code: 'forbidden', ...body, error: FORBIDDEN_MESSAGE })
    else if (res.status === 429) err = new ApiError(429, { code: 'rate_limited', ...body, error: RATE_LIMIT_MESSAGE })
    else err = new ApiError(res.status, body ?? { error: res.statusText || 'Request failed', code: 'internal' })
    if (res.status === 401) sessionHooks.onUnauthorized()
    else if (res.status === 403) emit('forbidden')
    else if (res.status === 423 || err.code === 'ledger_broken') emit('ledgerBroken')
    throw err
  }
  return data as T
}

function qs(params: Record<string, string | number | undefined>): string {
  const sp = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== '') sp.set(k, String(v))
  }
  const s = sp.toString()
  return s ? `?${s}` : ''
}

export const realApi: KiraApi = {
  me: () => request<Me>('GET', '/api/me'),
  logout: () => request<void>('POST', '/auth/logout'),
  listEntries: async (f: EntryFilter = {}) => {
    const r = await request<EntryList>('GET', `/api/entries${qs({ from: f.from, to: f.to, category: f.category, method: f.method, q: f.q, status: f.status })}`)
    return { ...r, entries: r.entries ?? [] }
  },
  getEntry: (id) => request<Entry>('GET', `/api/entries/${id}`),
  createEntry: (input: EntryInput) => request<AppendResult>('POST', '/api/entries', input),
  history: async (id) => {
    const r = await request<EntryHistory>('GET', `/api/entries/${id}/history`)
    return { ...r, rows: r.rows ?? [] }
  },
  amendEntry: (id, input) => request<AppendResult>('POST', `/api/entries/${id}/amend`, input),
  voidEntry: (id, input: VoidInput) => request<AppendResult>('POST', `/api/entries/${id}/void`, input),
  uploadReceipt: (file) => {
    const form = new FormData()
    form.append('file', file)
    return request<Receipt>('POST', '/api/receipts', undefined, form)
  },
  receiptUrl: (sha256) => `/api/receipts/${sha256}`,
  listCategories: async (includeArchived = false) => {
    const r = await request<CategoryList>('GET', `/api/categories${includeArchived ? '?include_archived=1' : ''}`)
    return { ...r, categories: r.categories ?? [] }
  },
  createCategory: (input: CategoryCreate) => request<Category>('POST', '/api/categories', input),
  patchCategory: (id, input: CategoryPatch) => request<Category>('PATCH', `/api/categories/${id}`, input),
  summary: async (month) => {
    const r = await request<Summary>('GET', `/api/summary${qs({ month })}`)
    return { ...r, by_category: r.by_category ?? [], trend: r.trend ?? [], recent: r.recent ?? [] }
  },
  exportUrl: (from, to, format: ExportFormat) => `/api/export${qs({ from, to, format })}`,
  verify: () => request<VerifyResult>('GET', '/api/ledger/verify'),
  acknowledge: () => request<VerifyResult>('POST', '/api/ledger/acknowledge'),
}
