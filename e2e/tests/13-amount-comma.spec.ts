import { expect, test } from './fixtures'
import { uniq } from './helpers'

test('"45,90" in quick-add Amount shows an error and saves nothing', async ({ page, request }) => {
  const payee = uniq('Comma')
  await page.goto('/entries')
  await page.getByLabel('Amount (RM)').fill('45,90')
  await page.locator('#quick-payee').fill(payee)
  await page.keyboard.press('Enter')
  await expect(page.getByText('Enter an amount like 45.90')).toBeVisible()
  await expect(page.getByLabel('Amount (RM)')).toHaveAttribute('aria-invalid', 'true')
  await expect(page.getByRole('row').filter({ hasText: payee })).toHaveCount(0)
  const list = await (await request.get('/api/entries')).json()
  expect(list.entries.some((e: { payee: string }) => e.payee === payee)).toBe(false)
})
