import { expect, request as pwRequest, test, SAME_ORIGIN } from './fixtures'
import fs from 'node:fs'
import path from 'node:path'
import { cleanup, E2E_PORT, startServer, stopServer, Srv } from '../server'
import { makePng, uniq } from './helpers'

const PORT = E2E_PORT + 3

test('tampered receipt: detail warns, ledger shows broken, writes 423 until Acknowledge, ack survives restart', async ({ browser }) => {
  let srv: Srv = await startServer({ bin: 'kira-e2e', port: PORT })
  const ctx = await browser.newContext({ baseURL: srv.url, storageState: { cookies: [], origins: [] } })
  try {
    const api = ctx.request
    expect((await api.post('/auth/test-login')).status()).toBe(204)
    const hdr = SAME_ORIGIN
    const cats = (await (await api.get('/api/categories')).json()).categories as { id: number }[]
    const up = await api.post('/api/receipts', { headers: hdr, multipart: { file: { name: 'r.png', mimeType: 'image/png', buffer: makePng(10, 10) } } })
    expect(up.status(), await up.text()).toBe(201)
    const sha = (await up.json()).sha256 as string
    const payee = uniq('Tamper')
    const created = await api.post('/api/entries', { headers: hdr, data: {
      occurred_on: new Date().toISOString().slice(0, 10), amount_sen: 500, category_id: cats[0].id, payee, method: 'card', note: null, receipt_sha256: sha } })
    expect(created.status(), await created.text()).toBe(201)
    const id = (await created.json()).entry.id
    expect((await (await api.get('/api/ledger/verify')).json()).ok).toBe(true)

    // overwrite the stored receipt file
    const dir = path.join(srv.dataDir, 'receipts')
    const f = fs.readdirSync(dir).find((n) => n.startsWith(sha))
    expect(f).toBeTruthy()
    fs.writeFileSync(path.join(dir, f!), makePng(11, 11))

    expect((await api.get(`/api/receipts/${sha}`)).status()).toBe(409)
    const page = await ctx.newPage()
    await page.goto(`/entries/${id}`)
    await expect(page.getByText("doesn't match its fingerprint")).toBeVisible()

    const v = await (await api.get('/api/ledger/verify')).json()
    expect(v.ok).toBe(false)
    expect(v.reason).toBe('receipt mismatch')

    await page.goto('/settings')
    const card = page.getByRole('heading', { name: 'Ledger status' }).locator('..')
    await expect(card.getByText(/Broken at entry/)).toBeVisible()
    await expect(page.getByText('Writes are disabled until you acknowledge')).toBeVisible()

    const body = { occurred_on: new Date().toISOString().slice(0, 10), amount_sen: 1, category_id: cats[0].id, payee: uniq('blocked'), method: 'card', note: null, receipt_sha256: null }
    expect((await api.post('/api/entries', { headers: hdr, data: body })).status()).toBe(423)

    await page.getByRole('button', { name: 'Acknowledge' }).click()
    await expect(page.getByText('Writes are disabled until you acknowledge')).toHaveCount(0)
    expect((await api.post('/api/entries', { headers: hdr, data: body })).status()).toBe(201)
    await page.close()

    // restart on the same DATA_DIR: acknowledgement persists
    await stopServer(srv)
    srv = await startServer({ bin: 'kira-e2e', port: PORT, workDir: srv.workDir, env: srv.env })
    const ctx2 = await pwRequest.newContext({ baseURL: srv.url, extraHTTPHeaders: SAME_ORIGIN })
    expect((await ctx2.post('/auth/test-login')).status()).toBe(204)
    const v2 = await (await ctx2.get('/api/ledger/verify')).json()
    expect(v2.ok).toBe(false)
    expect(v2.acknowledged).toBe(true)
    const w = await ctx2.post('/api/entries', { data: { ...body, payee: uniq('after-restart') } })
    expect(w.status(), await w.text()).toBe(201)
    await ctx2.dispose()
  } finally {
    await ctx.close()
    await stopServer(srv)
    cleanup(srv)
  }
})
