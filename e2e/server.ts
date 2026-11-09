// Helpers to run a throwaway kira server for E2E. Never reads the repo .env:
// the process cwd is the temp dir and every required env var is set explicitly.
import { spawn, ChildProcess } from 'node:child_process'
import crypto from 'node:crypto'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'

export const REPO = path.resolve(__dirname, '..')
export const E2E_PORT = Number(process.env.E2E_PORT ?? 18090)
export const NORMAL_PORT = E2E_PORT + 1
export const DURABLE_PORT = E2E_PORT + 2
export const STATE_DIR = path.join(os.tmpdir(), 'kira-e2e-state')
export const STATE_FILE = path.join(STATE_DIR, 'storage.json')
export const INFO_FILE = path.join(STATE_DIR, 'info.json')
export const EMAIL = 'e2e@example.com'

export interface Srv {
  proc: ChildProcess
  port: number
  url: string
  dataDir: string
  workDir: string
  env: NodeJS.ProcessEnv
  log: () => string
}

export function freshEnv(port: number, dataDir: string): NodeJS.ProcessEnv {
  return {
    PATH: process.env.PATH,
    HOME: dataDir,
    PORT: String(port),
    PUBLIC_URL: `http://localhost:${port}`,
    DATA_DIR: dataDir,
    GOOGLE_CLIENT_ID: 'dummy.apps.googleusercontent.com',
    GOOGLE_CLIENT_SECRET: 'dummy',
    ALLOWED_EMAIL: EMAIL,
    SESSION_SECRET: crypto.randomBytes(32).toString('hex'),
    LEDGER_KEY: crypto.randomBytes(32).toString('hex'),
  }
}

export async function waitReady(url: string, proc: ChildProcess, ms = 15_000): Promise<void> {
  const end = Date.now() + ms
  while (Date.now() < end) {
    if (proc.exitCode !== null) throw new Error(`server exited early with code ${proc.exitCode}`)
    try {
      const r = await fetch(`${url}/api/me`)
      if (r.status === 401) return
    } catch { /* not up yet */ }
    await new Promise((r) => setTimeout(r, 100))
  }
  throw new Error(`server at ${url} not ready after ${ms}ms`)
}

export async function startServer(opts: {
  bin: string; port: number; workDir?: string; env?: NodeJS.ProcessEnv
}): Promise<Srv> {
  const workDir = opts.workDir ?? fs.mkdtempSync(path.join(os.tmpdir(), 'kira-e2e-'))
  const dataDir = opts.env?.DATA_DIR ?? path.join(workDir, 'data')
  const env = opts.env ?? freshEnv(opts.port, dataDir)
  const bin = path.join(REPO, 'bin', opts.bin)
  if (!fs.existsSync(bin)) throw new Error(`${bin} missing: run make build / make build-e2e first`)
  let out = ''
  const proc = spawn(bin, ['serve'], { cwd: workDir, env: { ...env, KIRA_E2E: '1' }, stdio: ['ignore', 'pipe', 'pipe'] })
  proc.stdout?.on('data', (d) => (out += d))
  proc.stderr?.on('data', (d) => (out += d))
  const url = `http://localhost:${opts.port}`
  try {
    await waitReady(url, proc)
  } catch (e) {
    proc.kill('SIGKILL')
    throw new Error(`${(e as Error).message}\n--- server log ---\n${out}`)
  }
  return { proc, port: opts.port, url, dataDir, workDir, env, log: () => out }
}

export async function stopServer(s: Srv, signal: NodeJS.Signals = 'SIGTERM'): Promise<void> {
  if (s.proc.exitCode === null) {
    const done = new Promise<void>((r) => s.proc.once('exit', () => r()))
    s.proc.kill(signal)
    await Promise.race([done, new Promise((r) => setTimeout(r, 5000))])
    if (s.proc.exitCode === null) s.proc.kill('SIGKILL')
  }
}

export function cleanup(s: Srv): void {
  fs.rmSync(s.workDir, { recursive: true, force: true })
}
