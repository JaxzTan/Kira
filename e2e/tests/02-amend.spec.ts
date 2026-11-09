import { expect, test } from './fixtures'
import { categories, seed, uniq } from './helpers'

test('amend: payee shows (edited), history lists Created + Amended with ids and hashes', async ({ page, request }) => {
  const payee = uniq('AmendMe')
  const cats = await categories(request)
  const e = await seed(request, { payee, amount_sen: 1000, category_id: cats[0].id })

  await page.goto(`/entries/${e.id}`)
  await expect(page.getByRole('heading', { name: payee })).toBeVisible()
  await page.getByRole('button', { name: 'Correct entry' }).click()
  const form = page.getByRole('form', { name: 'Correct entry' })
  await form.getByLabel('Amount (RM)').fill('12.34')
  await form.getByRole('button', { name: 'Save correction' }).click()

  await expect(page.getByText('Amended once')).toBeVisible()
  await expect(page.getByText('RM 12.34').first()).toBeVisible()

  const history = page.getByRole('heading', { name: 'History' }).locator('..')
  await expect(history.getByText('Created')).toBeVisible()
  await expect(history.getByText('Amended', { exact: true })).toBeVisible()
  await expect(history.getByText('Amount RM 10.00 → RM 12.34')).toBeVisible()
  // "#id · abcd…wxyz" for both rows
  await expect(history.getByText(/^#\d+ · \w{4}…\w{4}$/)).toHaveCount(2)
  await expect(history.getByText(new RegExp(`^#${e.id} · `))).toBeVisible()

  // list shows (edited)
  await page.goto('/entries')
  await expect(page.getByRole('row').filter({ hasText: payee })).toContainText(`${payee} (edited)`)
  await expect(page.getByRole('row').filter({ hasText: payee })).toContainText('RM 12.34')

  // API agrees: two rows ascending
  const h = await (await request.get(`/api/entries/${e.id}/history`)).json()
  expect(h.rows.map((r: { kind: string }) => r.kind)).toEqual(['create', 'amend'])
  expect(h.rows[1].ref_id).toBe(e.id)
  expect(h.rows[1].prev_hash).toBe(h.rows[0].hash)
})
