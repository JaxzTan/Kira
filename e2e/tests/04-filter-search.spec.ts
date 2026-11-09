import { expect, test } from '@playwright/test'
import { categories, monthOf, row, seed, today, uniq } from './helpers'

test('filter by category, method, month; search by payee and note', async ({ page, request }) => {
  const cats = await categories(request)
  const tag = uniq('flt')
  const travel = cats.find((c) => c.name.startsWith('Travel'))!
  const office = cats.find((c) => c.name === 'Office')!

  // previous month entry (15th of last month avoids month-edge issues)
  const now = new Date()
  const prev = new Date(now.getFullYear(), now.getMonth() - 1, 15)
  const pm = `${prev.getFullYear()}-${String(prev.getMonth() + 1).padStart(2, '0')}`
  const prevDate = `${pm}-15`

  const a = `${tag}-Grab`, b = `${tag}-Stationer`, c = `${tag}-OldMonth`
  await seed(request, { payee: a, amount_sen: 1500, category_id: travel.id, method: 'grabpay', note: `ride ${tag}-airport` })
  await seed(request, { payee: b, amount_sen: 2500, category_id: office.id, method: 'cash', note: 'paper and pens' })
  await seed(request, { payee: c, amount_sen: 3500, category_id: office.id, method: 'cash', occurred_on: prevDate })

  await page.goto('/entries')
  await expect(row(page, a)).toBeVisible()
  await expect(row(page, b)).toBeVisible()
  await expect(row(page, c)).toHaveCount(0) // default month = current

  // search by payee
  const search = page.locator('#f-q')
  await search.fill(`${tag}-gra`)
  await expect(row(page, a)).toBeVisible()
  await expect(row(page, b)).toHaveCount(0)
  // search by note text (case-insensitive)
  await search.fill(`${tag}-AIRPORT`.toLowerCase())
  await expect(row(page, a)).toBeVisible()
  await expect(row(page, b)).toHaveCount(0)
  await search.fill('PAPER and')
  await expect(row(page, b)).toBeVisible()
  await expect(row(page, a)).toHaveCount(0)
  await search.fill('no-such-thing-zzz')
  await expect(page.getByText('No entries match these filters.')).toBeVisible()
  await search.fill(tag)
  await expect(row(page, a)).toBeVisible()
  await expect(row(page, b)).toBeVisible()

  // category filter
  await page.locator('#f-cat').selectOption({ label: travel.name })
  await expect(row(page, a)).toBeVisible()
  await expect(row(page, b)).toHaveCount(0)
  await page.locator('#f-cat').selectOption({ label: 'All categories' })

  // method filter
  await page.locator('#f-method').selectOption({ label: 'Cash' })
  await expect(row(page, b)).toBeVisible()
  await expect(row(page, a)).toHaveCount(0)
  await page.locator('#f-method').selectOption({ label: 'All methods' })

  // month filter
  await page.locator('#f-month').selectOption(pm)
  await expect(row(page, c)).toBeVisible()
  await expect(row(page, a)).toHaveCount(0)
  await expect(page.getByText(/1 entry · RM 35\.00/)).toBeVisible()
  await page.locator('#f-month').selectOption('')
  await expect(row(page, c)).toBeVisible()
  await expect(row(page, a)).toBeVisible()

  // API-level filter agrees
  const r = await (await request.get(`/api/entries?q=${tag}&method=cash&from=${monthOf(today())}-01&to=${today()}`)).json()
  expect(r.entries.map((e: { payee: string }) => e.payee)).toEqual([b])
})
