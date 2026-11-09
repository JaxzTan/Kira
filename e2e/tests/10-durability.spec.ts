import { expect, request as pwRequest, test } from '@playwright/test'
import { cleanup, DURABLE_PORT, startServer, stopServer } from '../server'
import { categories, seed, uniq } from './helpers'

test('SIGKILL then restart on same DATA_DIR keeps entries and verify is ok', async () => {
  let srv = await startServer({ bin: 'kira-e2e', port: DURABLE_PORT })
  try {
    let api = await pwRequest.newContext({ baseURL: srv.url })
    expect((await api.post('/auth/test-login')).status()).toBe(204)
    const cats = await categories(api)
    const payees = [uniq('dur1'), uniq('dur2'), uniq('dur3')]
    const ids: number[] = []
    for (const [i, p] of payees.entries()) ids.push((await seed(api, { payee: p, amount_sen: 1000 + i, category_id: cats[0].id })).id)
    const before = await (await api.get('/api/ledger/verify')).json()
    expect(before.ok).toBe(true)
    await api.dispose()

    await stopServer(srv, 'SIGKILL')
    expect(srv.proc.signalCode).toBe('SIGKILL')

    // restart with the same env / DATA_DIR / keys
    srv = await startServer({ bin: 'kira-e2e', port: DURABLE_PORT, workDir: srv.workDir, env: srv.env })
    api = await pwRequest.newContext({ baseURL: srv.url })
    expect((await api.post('/auth/test-login')).status()).toBe(204)
    const list = await (await api.get('/api/entries')).json()
    expect(list.entries.map((e: { id: number }) => e.id).sort()).toEqual([...ids].sort())
    const after = await (await api.get('/api/ledger/verify')).json()
    expect(after.ok).toBe(true)
    expect(after.entries).toBe(before.entries)
    expect(after.head_hash).toBe(before.head_hash)
    // can still write
    await seed(api, { payee: uniq('dur4'), amount_sen: 5, category_id: cats[0].id })
    expect((await (await api.get('/api/ledger/verify')).json()).ok).toBe(true)
    await api.dispose()
  } finally {
    await stopServer(srv)
    cleanup(srv)
  }
})
