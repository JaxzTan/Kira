import { expect, test } from '@playwright/test'
import fs from 'node:fs'
import path from 'node:path'
import { cleanup, E2E_PORT, NORMAL_PORT, REPO, startServer, stopServer } from '../server'

test('normal (non-e2e) binary has no /auth/test-login', async () => {
  const srv = await startServer({ bin: 'kira', port: NORMAL_PORT })
  try {
    const r = await fetch(`${srv.url}/auth/test-login`, { method: 'POST' })
    expect(r.status).toBe(404)
    const g = await fetch(`${srv.url}/auth/test-login`)
    expect(g.status).toBe(404)
    // and nothing could have logged us in
    expect((await fetch(`${srv.url}/api/me`)).status).toBe(401)
  } finally {
    await stopServer(srv)
    cleanup(srv)
  }
})

test('sanity: the e2e binary does expose it (so the 404 above is meaningful)', async ({ request }) => {
  expect(E2E_PORT).not.toBe(NORMAL_PORT)
  expect((await request.post('/auth/test-login')).status()).toBe(204)
  expect(fs.statSync(path.join(REPO, 'bin', 'kira')).size).not.toBe(fs.statSync(path.join(REPO, 'bin', 'kira-e2e')).size)
})
