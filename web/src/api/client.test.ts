import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { realApi, FORBIDDEN_MESSAGE, NETWORK_MESSAGE, RATE_LIMIT_MESSAGE } from './client'
import { ApiError, isNetworkError } from './errors'
import { sessionHooks, subscribe } from './events'

function respond(status: number, body?: unknown) {
  const text = body === undefined ? '' : JSON.stringify(body)
  return vi.fn().mockImplementation(async () => new Response(status === 204 ? null : text, { status }))
}

describe('real client', () => {
  const unauth = vi.fn()
  const orig = sessionHooks.onUnauthorized
  beforeEach(() => { sessionHooks.onUnauthorized = unauth })
  afterEach(() => { sessionHooks.onUnauthorized = orig; unauth.mockReset(); vi.unstubAllGlobals() })

  it('sends JSON with credentials on POST', async () => {
    const f = respond(201, { entry: {}, verify: {} })
    vi.stubGlobal('fetch', f)
    await realApi.createEntry({ amount_sen: 100 } as never)
    const [path, init] = f.mock.calls[0]
    expect(path).toBe('/api/entries')
    expect(init.credentials).toBe('same-origin')
    expect(init.headers['Content-Type']).toBe('application/json')
    expect(init.body).toBe('{"amount_sen":100}')
  })

  it('sends bodyless POSTs for logout and acknowledge without Content-Type', async () => {
    const f = respond(200, {})
    vi.stubGlobal('fetch', f)
    await realApi.logout()
    await realApi.acknowledge()
    for (const [, init] of f.mock.calls) {
      expect(init.body).toBeUndefined()
      expect(init.headers['Content-Type']).toBeUndefined()
    }
    expect(f.mock.calls[0][0]).toBe('/auth/logout')
    expect(f.mock.calls[1][0]).toBe('/api/ledger/acknowledge')
  })

  it('uploads receipts as multipart without a manual Content-Type', async () => {
    const f = respond(201, { sha256: 'a' })
    vi.stubGlobal('fetch', f)
    await realApi.uploadReceipt(new File(['x'], 'r.png', { type: 'image/png' }))
    const [, init] = f.mock.calls[0]
    expect(init.body).toBeInstanceOf(FormData)
    expect(init.headers['Content-Type']).toBeUndefined()
  })

  it('parses validation errors into ApiError with fields', async () => {
    vi.stubGlobal('fetch', respond(422, { error: 'invalid', code: 'validation', fields: { amount_sen: 'must be positive' } }))
    const e = await realApi.createEntry({} as never).catch((x) => x)
    expect(e).toBeInstanceOf(ApiError)
    expect(e.status).toBe(422)
    expect(e.code).toBe('validation')
    expect(e.fields).toEqual({ amount_sen: 'must be positive' })
  })

  it('401 triggers the login redirect hook', async () => {
    vi.stubGlobal('fetch', respond(401, { error: 'no session', code: 'unauthorized', fields: null }))
    const e = await realApi.me().catch((x) => x)
    expect(e.code).toBe('unauthorized')
    expect(unauth).toHaveBeenCalledOnce()
  })

  it('default 401 hook does not redirect when already on /login', () => {
    const assign = vi.fn()
    vi.stubGlobal('location', { pathname: '/login', assign })
    orig()
    expect(assign).not.toHaveBeenCalled()
    vi.stubGlobal('location', { pathname: '/entries', assign })
    orig()
    expect(assign).toHaveBeenCalledWith('/login')
  })

  it('403 emits forbidden with a clear message', async () => {
    const fn = vi.fn()
    const off = subscribe('forbidden', fn)
    vi.stubGlobal('fetch', respond(403, { error: 'x', code: 'forbidden', fields: null }))
    const e = await realApi.me().catch((x) => x)
    off()
    expect(e.message).toBe(FORBIDDEN_MESSAGE)
    expect(fn).toHaveBeenCalledOnce()
  })

  it.each([
    ['createEntry', () => realApi.createEntry({} as never)],
    ['createCategory', () => realApi.createCategory({ name: 'x' })],
    ['patchCategory', () => realApi.patchCategory(1, { name: 'y' })],
    ['uploadReceipt', () => realApi.uploadReceipt(new File(['x'], 'r.png'))],
  ])('423 ledger_broken from %s emits ledgerBroken', async (_n, call) => {
    const fn = vi.fn()
    const off = subscribe('ledgerBroken', fn)
    vi.stubGlobal('fetch', respond(423, { error: 'read-only', code: 'ledger_broken', fields: null }))
    const e = await call().catch((x: unknown) => x)
    off()
    expect(e).toBeInstanceOf(ApiError)
    expect((e as ApiError).code).toBe('ledger_broken')
    expect(fn).toHaveBeenCalledOnce()
  })

  it('429 gives a friendly message', async () => {
    vi.stubGlobal('fetch', respond(429, { error: 'rate limited', code: 'rate_limited', fields: null }))
    const e = await realApi.me().catch((x) => x)
    expect(e.message).toBe(RATE_LIMIT_MESSAGE)
    expect(e.code).toBe('rate_limited')
  })

  it('network failure becomes a retry-able ApiError(0)', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')))
    const e = await realApi.me().catch((x) => x)
    expect(isNetworkError(e)).toBe(true)
    expect(e.message).toBe(NETWORK_MESSAGE)
  })

  it('normalizes null arrays from the server', async () => {
    vi.stubGlobal('fetch', respond(200, { month: '2026-11', by_category: null, trend: null, recent: null }))
    const s = await realApi.summary('2026-11')
    expect(s.by_category).toEqual([])
    expect(s.trend).toEqual([])
    expect(s.recent).toEqual([])
  })
})
