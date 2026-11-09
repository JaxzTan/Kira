import { expect, test } from '@playwright/test'
import { categories, rmToSen, row, seed, uniq } from './helpers'

test('void: reason required, row struck through, excluded from totals', async ({ page, request }) => {
  const cats = await categories(request)
  const payee = uniq('VoidMe')
  const e = await seed(request, { payee, amount_sen: 77_700, category_id: cats[0].id })

  const kpi = () => page.getByText('Spent this month').locator('xpath=following-sibling::p[1]')
  const footer = () => page.getByText(/ entr(y|ies) · RM /)

  await page.goto('/')
  const dashBefore = rmToSen(await kpi().innerText())
  await page.goto('/entries')
  await expect(row(page, payee)).toBeVisible()
  const entriesBefore = rmToSen(await footer().innerText())

  await row(page, payee).getByRole('link', { name: payee }).click()
  await page.getByRole('button', { name: 'Void…' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()

  // empty reason blocked
  await dialog.getByRole('button', { name: 'Void entry' }).click()
  await expect(dialog.getByText('A reason is required')).toBeVisible()
  const still = await (await request.get(`/api/entries/${e.id}`)).json()
  expect(still.status).toBe('active')
  // whitespace only is also blocked
  await dialog.getByLabel('Reason (required)').fill('   ')
  await dialog.getByRole('button', { name: 'Void entry' }).click()
  await expect(dialog.getByText('A reason is required')).toBeVisible()

  await dialog.getByLabel('Reason (required)').fill('Duplicate of another claim')
  await dialog.getByRole('button', { name: 'Void entry' }).click()
  await expect(page.getByText('Voided', { exact: true }).first()).toBeVisible()
  await expect(page.getByText('Duplicate of another claim').first()).toBeVisible()
  await expect(page.getByRole('button', { name: 'Correct entry' })).toHaveCount(0)
  await expect(page.getByRole('heading', { name: payee })).toHaveClass(/line-through/)
  // history has Voided row with reason
  await expect(page.getByText('Reason: Duplicate of another claim')).toBeVisible()

  // list: struck through + "— voided"
  await page.goto('/entries')
  const r = row(page, payee)
  await expect(r).toContainText(`${payee} — voided`)
  await expect(r).toHaveClass(/line-through/)
  await expect(footer()).toContainText('voided (not counted)')
  const entriesAfter = rmToSen(await footer().innerText())
  expect(entriesAfter).toBe(entriesBefore - 77_700)

  // dashboard excludes it
  await page.goto('/')
  await expect(kpi()).toBeVisible()
  expect(rmToSen(await kpi().innerText())).toBe(dashBefore - 77_700)
  await expect(page.getByText(/\d+ voided/).first()).toBeVisible()

  // second void -> 409
  const again = await request.post(`/api/entries/${e.id}/void`, { data: { reason: 'x' } })
  expect(again.status()).toBe(409)
})
