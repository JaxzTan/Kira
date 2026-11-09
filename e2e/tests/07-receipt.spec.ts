import { expect, test } from './fixtures'
import { row, uniq } from './helpers'
import { makePng } from './helpers'

test('attach receipt in quick add; list shows Attached; detail previews with sha256', async ({ page, request }) => {
  const payee = uniq('Rcpt')
  await page.goto('/entries')
  await page.getByTestId('receipt-input').setInputFiles({ name: 'receipt.png', mimeType: 'image/png', buffer: makePng(16, 16) })
  await expect(page.getByRole('button', { name: /Receipt attached/ })).toBeVisible()
  await page.getByLabel('Amount (RM)').fill('8.80')
  await page.locator('#quick-payee').fill(payee)
  await page.getByRole('button', { name: 'Save', exact: true }).click()

  const r = row(page, payee)
  await expect(r).toContainText('Attached')
  // cleared after save
  await expect(page.getByRole('button', { name: 'Attach receipt' })).toBeVisible()

  await r.getByRole('link', { name: payee }).click()
  const img = page.getByRole('img', { name: 'Receipt' })
  await expect(img).toBeVisible()
  await expect.poll(() => img.evaluate((i: HTMLImageElement) => i.naturalWidth)).toBeGreaterThan(0)
  const sha = /sha256 (\w{4}…\w{4})/
  await expect(page.getByText(sha)).toBeVisible()

  const id = Number(page.url().split('/').pop())
  const e = await (await request.get(`/api/entries/${id}`)).json()
  expect(e.receipt_sha256).toMatch(/^[0-9a-f]{64}$/)
  const short = `${e.receipt_sha256.slice(0, 4)}…${e.receipt_sha256.slice(-4)}`
  await expect(page.getByText(`sha256 ${short}`)).toBeVisible()
  const file = await request.get(`/api/receipts/${e.receipt_sha256}`)
  expect(file.status()).toBe(200)
  expect(file.headers()['content-type']).toContain('image/png')
  expect(file.headers()['x-content-type-options']).toBe('nosniff')
})
