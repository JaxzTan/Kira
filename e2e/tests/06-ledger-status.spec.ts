import { expect, test } from '@playwright/test'
import { categories, seed, uniq } from './helpers'

test('Settings shows Verified with entry count; verify API ok', async ({ page, request }) => {
  const cats = await categories(request)
  await seed(request, { payee: uniq('ledger'), amount_sen: 123, category_id: cats[0].id })

  const v = await (await request.get('/api/ledger/verify')).json()
  expect(v.ok).toBe(true)
  expect(v.entries).toBeGreaterThan(0)
  expect(v.head_hash).toMatch(/^[0-9a-f]{64}$/)

  await page.goto('/settings')
  const card = page.getByRole('heading', { name: 'Ledger status' }).locator('..')
  await expect(card.getByText('Verified')).toBeVisible()
  await expect(card.getByText('Entries').locator('xpath=following-sibling::dd[1]')).toHaveText(String(v.entries))
  await expect(card.getByText('Head').locator('xpath=following-sibling::dd[1]')).toContainText(`#${v.head_id}`)

  // sidebar
  await expect(page.getByText('Ledger verified')).toBeVisible()
  await expect(page.getByText(`${v.entries} entries`)).toBeVisible()

  // Verify now keeps it green
  await card.getByRole('button', { name: 'Verify now' }).click()
  await expect(card.getByText('Verified')).toBeVisible()
  await expect(page.getByText(/Chain broken|Ledger broken/)).toHaveCount(0)
})
