import { expect, test } from '@playwright/test'
import { row, today, uniq } from './helpers'

test('keyboard-only add: N, Tab through fields, Enter saves, form resets, under 10 s', async ({ page }) => {
  const payee = uniq('KbdPayee')
  const note = uniq('kbd-note')
  await page.goto('/')
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible()
  await page.locator('body').click({ position: { x: 700, y: 5 } }) // not an input

  const t0 = Date.now()
  await page.keyboard.press('n')
  await expect(page).toHaveURL(/\/entries$/)
  const amount = page.getByLabel('Amount (RM)')
  await expect(amount).toBeFocused()

  await page.keyboard.type('45.90')
  await page.keyboard.press('Tab')
  await expect(page.locator('#quick-category')).toBeFocused()
  await page.keyboard.type('Hardware')
  await page.keyboard.press('Tab')
  await expect(page.locator('#quick-payee')).toBeFocused()
  await page.keyboard.type(payee)
  await page.keyboard.press('Tab')
  await expect(page.locator('#quick-method')).toBeFocused()
  await page.keyboard.type('Cash')
  await page.keyboard.press('Tab')
  await expect(page.locator('#quick-note')).toBeFocused()
  await page.keyboard.type(note)
  await page.keyboard.press('Enter')

  const saved = row(page, payee)
  await expect(saved).toBeVisible()
  const elapsed = Date.now() - t0
  expect(elapsed, `keyboard add took ${elapsed} ms`).toBeLessThan(10_000)

  // new row at the top of the table body
  await expect(page.locator('tbody tr').first()).toContainText(payee)
  await expect(saved).toContainText('RM 45.90')
  await expect(saved).toContainText('Hardware')
  await expect(saved).toContainText('Cash')
  await expect(saved).toContainText('Missing')

  // reset behaviour
  await expect(amount).toHaveValue('')
  await expect(page.locator('#quick-payee')).toHaveValue('')
  await expect(page.locator('#quick-note')).toHaveValue('')
  await expect(page.locator('#quick-date')).toHaveValue(today())
  await expect(page.locator('#quick-category')).toHaveValue(/\d+/)
  await expect(page.locator('#quick-category').locator('option:checked')).toHaveText('Hardware')
  await expect(page.locator('#quick-method').locator('option:checked')).toHaveText('Cash')
  await expect(amount).toBeFocused()
  console.log(`keyboard add flow: ${elapsed} ms`)
})

test('empty amount/payee is blocked with inline errors', async ({ page }) => {
  await page.goto('/entries')
  await page.getByLabel('Amount (RM)').focus()
  await page.keyboard.press('Enter')
  await expect(page.getByRole('alert').first()).toBeVisible()
  await expect(page.getByLabel('Amount (RM)')).toBeFocused()
})
