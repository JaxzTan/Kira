import { expect, test } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { categories, makePng, seed, today, uniq } from './helpers'

const HEADER = 'date,category,payee,method,amount_myr,note,status,entry_id,receipt_file'

test('export CSV and ZIP via the Export page', async ({ page, request }) => {
  const cats = await categories(request)
  const tag = uniq('exp')
  const ok = await seed(request, { payee: `${tag}-ok`, amount_sen: 4590, category_id: cats[1].id, method: 'tng_ewallet', note: 'a, "quoted" note' })
  const gone = await seed(request, { payee: `${tag}-gone`, amount_sen: 100_005, category_id: cats[2].id })
  const inj = await seed(request, { payee: `=${tag}-inj`, amount_sen: 5, category_id: cats[0].id })
  expect((await request.post(`/api/entries/${gone.id}/void`, { data: { reason: 'oops' } })).status()).toBe(201)

  // receipt entry for the ZIP
  const up = await request.post('/api/receipts', { multipart: { file: { name: 'r.png', mimeType: 'image/png', buffer: makePng() } } })
  expect(up.status()).toBe(201)
  const rc = await up.json()
  const withR = await seed(request, { payee: `${tag}-rcpt`, amount_sen: 999, category_id: cats[0].id, receipt_sha256: rc.sha256 })

  await page.goto('/export')
  const year = today().slice(0, 4)
  await page.locator('#ex-from').fill(`${year}-01-01`)
  await page.locator('#ex-to').fill(today())

  // CSV
  const [dl] = await Promise.all([page.waitForEvent('download'), page.getByRole('link', { name: 'Download CSV' }).click()])
  expect(dl.suggestedFilename()).toBe(`kira-${year}-01-01_${today()}.csv`)
  const csv = fs.readFileSync((await dl.path())!, 'utf8')
  const lines = csv.split('\r\n')
  expect(lines[0]).toBe(HEADER)
  const find = (id: number) => lines.find((l) => new RegExp(`,${id},[^,]*$`).test(l))!
  expect(find(ok.id)).toBe(`${today()},${cats[1].name.includes(',') ? `"${cats[1].name}"` : cats[1].name},${tag}-ok,TNG eWallet,45.90,"a, ""quoted"" note",active,${ok.id},`)
  const g = find(gone.id)
  expect(g).toContain(',1000.05,')
  expect(g).toContain(',voided,')
  expect(find(inj.id)).toContain(`'=${tag}-inj`) // CSV injection guard
  expect(find(withR.id)).toContain(`${rc.sha256}.png`)
  // dates ascending
  const dates = lines.slice(1).filter(Boolean).map((l) => l.slice(0, 10))
  expect([...dates].sort()).toEqual(dates)

  // ZIP
  const [dz] = await Promise.all([page.waitForEvent('download'), page.getByRole('link', { name: 'Download ZIP with receipts' }).click()])
  expect(dz.suggestedFilename()).toBe(`kira-${year}-01-01_${today()}.zip`)
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'kira-zip-'))
  try {
    const zipPath = path.join(dir, 'x.zip')
    fs.copyFileSync((await dz.path())!, zipPath)
    const outDir = path.join(dir, 'out')
    execFileSync('unzip', ['-q', zipPath, '-d', outDir])
    const inner = fs.readFileSync(path.join(outDir, 'expenses.csv'), 'utf8')
    expect(inner.split('\r\n')[0]).toBe(HEADER)
    expect(inner).toContain(`${tag}-ok`)
    expect(fs.existsSync(path.join(outDir, 'receipts', `${rc.sha256}.png`))).toBe(true)
  } finally {
    fs.rmSync(dir, { recursive: true, force: true })
  }
})

test('export page blocks an inverted range', async ({ page }) => {
  await page.goto('/export')
  await page.locator('#ex-from').fill('2030-01-02')
  await page.locator('#ex-to').fill('2030-01-01')
  await expect(page.getByText('Choose a valid range')).toBeVisible()
  await expect(page.getByText('Download CSV')).toHaveAttribute('aria-disabled', 'true')
})
