import { expect, test } from './fixtures'
import { makePng, row, uniq } from './helpers'

test('Enter during a slow receipt upload does not save until the upload is done; receipt lands on the right entry', async ({ page, request }) => {
  const payee = uniq('Race')
  await page.route('**/api/receipts', async (route) => {
    if (route.request().method() !== 'POST') return route.continue()
    await new Promise((r) => setTimeout(r, 1500))
    await route.continue()
  })
  await page.goto('/entries')
  await page.getByLabel('Amount (RM)').fill('12.34')
  await page.locator('#quick-payee').fill(payee)
  await page.getByTestId('receipt-input').setInputFiles({ name: 'slow.png', mimeType: 'image/png', buffer: makePng(12, 9) })

  await page.locator('#quick-payee').press('Enter') // upload still in flight
  await expect(page.getByText('Waiting for receipt upload')).toBeVisible()
  let list = await (await request.get('/api/entries')).json()
  expect(list.entries.some((e: { payee: string }) => e.payee === payee)).toBe(false)
  await expect(page.getByRole('row').filter({ hasText: payee })).toHaveCount(0)

  await expect(page.getByRole('button', { name: /Receipt attached/ })).toBeVisible({ timeout: 10_000 })
  await page.locator('#quick-payee').press('Enter')
  await expect(row(page, payee)).toContainText('Attached')

  list = await (await request.get('/api/entries')).json()
  const mine = list.entries.filter((e: { payee: string }) => e.payee === payee)
  expect(mine).toHaveLength(1)
  expect(mine[0].receipt_sha256).toMatch(/^[0-9a-f]{64}$/)
  expect(mine[0].amount_sen).toBe(1234)
  const others = list.entries.filter((e: { payee: string; receipt_sha256: string | null }) => e.payee !== payee && e.receipt_sha256 === mine[0].receipt_sha256)
  expect(others).toHaveLength(0)
})
