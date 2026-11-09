import { ApiError } from './errors'
import type {
  AppendResult, Category, CategoryCreate, CategoryList, CategoryPatch, Entry, EntryFilter,
  EntryHistory, EntryInput, EntryList, ExportFormat, KiraApi, Me, Receipt, Summary,
  VerifyResult, VoidInput,
} from './types'

function redirectToLogin() {
  if (typeof window !== 'undefined' && window.location.pathname !== '/login') {
    window.location.assign('/login')
  }
}

async function request<T>(method: string, path: string, body?: unknown, form?: FormData): Promise<T> {
  const headers: Record<string, string> = {}
  let payload: BodyInit | undefined
  if (form) {
    payload = form
  } else if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
    payload = JSON.stringify(body)
  } else if (method !== 'GET') {
    headers['Content-Type'] = 'application/json'
  }
  let res: Response
  try {
    res = await fetch(path, { method, headers, body: payload, credentials: 'same-origin' })
  } catch {
    throw new ApiError(0, { error: 'Network error: could not reach the server', code: 'internal' })
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
    const err = new ApiError(
      res.status,
      data && typeof data === 'object' && 'error' in data
        ? (data as { error: string })
        : { error: res.statusText || 'Request failed', code: res.status === 401 ? 'unauthorized' : 'internal' },
    )
    if (res.status === 401) redirectToLogin()
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
  listEntries: (f: EntryFilter = {}) =>
    request<EntryList>('GET', `/api/entries${qs({ from: f.from, to: f.to, category: f.category, method: f.method, q: f.q, status: f.status })}`),
  getEntry: (id) => request<Entry>('GET', `/api/entries/${id}`),
  createEntry: (input: EntryInput) => request<AppendResult>('POST', '/api/entries', input),
  history: (id) => request<EntryHistory>('GET', `/api/entries/${id}/history`),
  amendEntry: (id, input) => request<AppendResult>('POST', `/api/entries/${id}/amend`, input),
  voidEntry: (id, input: VoidInput) => request<AppendResult>('POST', `/api/entries/${id}/void`, input),
  uploadReceipt: (file) => {
    const form = new FormData()
    form.append('file', file)
    return request<Receipt>('POST', '/api/receipts', undefined, form)
  },
  receiptUrl: (sha256) => `/api/receipts/${sha256}`,
  listCategories: (includeArchived = false) =>
    request<CategoryList>('GET', `/api/categories${includeArchived ? '?include_archived=1' : ''}`),
  createCategory: (input: CategoryCreate) => request<Category>('POST', '/api/categories', input),
  patchCategory: (id, input: CategoryPatch) => request<Category>('PATCH', `/api/categories/${id}`, input),
  summary: (month) => request<Summary>('GET', `/api/summary${qs({ month })}`),
  exportUrl: (from, to, format: ExportFormat) => `/api/export${qs({ from, to, format })}`,
  verify: () => request<VerifyResult>('GET', '/api/ledger/verify'),
  acknowledge: () => request<VerifyResult>('POST', '/api/ledger/acknowledge'),
}
