import { expect, test } from './fixtures'
import { spawnSync } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { freshEnv, E2E_PORT, REPO } from '../server'

test('e2e binary refuses to start without KIRA_E2E=1', async () => {
  const work = fs.mkdtempSync(path.join(os.tmpdir(), 'kira-guard-'))
  try {
    const env = freshEnv(E2E_PORT + 5, path.join(work, 'data'))
    delete env.KIRA_E2E
    const r = spawnSync(path.join(REPO, 'bin', 'kira-e2e'), ['serve'], { cwd: work, env, timeout: 10_000, encoding: 'utf8' })
    expect(r.status, `${r.stdout}${r.stderr}`).not.toBe(0)
    expect(r.status).not.toBeNull() // exited on its own, did not hang as a server
    expect(`${r.stdout}${r.stderr}`).toContain('KIRA_E2E')
    expect(fs.existsSync(path.join(work, 'data', 'ledger.db'))).toBe(false)
  } finally {
    fs.rmSync(work, { recursive: true, force: true })
  }
})
