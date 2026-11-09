import { request } from '@playwright/test'
import fs from 'node:fs'
import { cleanup, E2E_PORT, INFO_FILE, startServer, STATE_DIR, STATE_FILE, stopServer } from './server'

export default async function globalSetup() {
  fs.rmSync(STATE_DIR, { recursive: true, force: true })
  fs.mkdirSync(STATE_DIR, { recursive: true })
  const srv = await startServer({ bin: 'kira-e2e', port: E2E_PORT })
  fs.writeFileSync(INFO_FILE, JSON.stringify({ workDir: srv.workDir, dataDir: srv.dataDir }))

  const ctx = await request.newContext({ baseURL: srv.url })
  const res = await ctx.post('/auth/test-login')
  if (res.status() !== 204) throw new Error(`test-login returned ${res.status()}`)
  await ctx.storageState({ path: STATE_FILE })
  await ctx.dispose()

  return async () => {
    await stopServer(srv)
    cleanup(srv)
    fs.rmSync(STATE_DIR, { recursive: true, force: true })
  }
}
