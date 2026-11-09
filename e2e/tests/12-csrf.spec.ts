import { expect, test } from './fixtures'
import { categories, row, uniq } from './helpers'

test('cross-site POSTs are rejected (403) while the UI keeps working', async ({ page, request, baseURL, playwright, storageState }) => {
  // bare client: sends exactly the headers given (no default Sec-Fetch-Site)
  const bare = await playwright.request.newContext({ baseURL, storageState: storageState as string })
  const cats = await categories(request)
  const body = { occurred_on: new Date().toISOString().slice(0, 10), amount_sen: 100, category_id: cats[0].id, payee: uniq('evil'), method: 'card', note: null, receipt_sha256: null }
  const before = (await (await request.get('/api/entries')).json()).entries.length

  // Origin only (no Sec-Fetch-Site), foreign origin
  const evil = await bare.post('/api/entries', { data: body, headers: { Origin: 'http://evil.example' } })
  expect(evil.status()).toBe(403)
  // browser-declared cross-site
  const cross = await request.post('/api/entries', { data: body, headers: { 'Sec-Fetch-Site': 'cross-site' } })
  expect(cross.status()).toBe(403)
  // no hints at all
  const none = await bare.post('/api/entries', { data: body })
  expect(none.status()).toBe(403)
  // matching Origin alone is accepted
  const ok = await bare.post('/api/entries', { data: { ...body, payee: uniq('origin-ok') }, headers: { Origin: baseURL! } })
  expect(ok.status()).toBe(201)
  expect((await (await request.get('/api/entries')).json()).entries.length).toBe(before + 1)

  await bare.dispose()

  // the real UI still works
  const payee = uniq('UiOk')
  await page.goto('/entries')
  await page.getByLabel('Amount (RM)').fill('3.20')
  await page.locator('#quick-payee').fill(payee)
  await page.keyboard.press('Enter')
  await expect(row(page, payee)).toBeVisible()
})
