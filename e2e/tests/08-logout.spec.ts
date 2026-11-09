import { expect, test } from '@playwright/test'

test('sign out redirects to /login and the API returns 401', async ({ page, request, browser, baseURL }) => {
  // own session so the shared storageState stays valid for other specs
  const ctx = await browser.newContext({ baseURL })
  expect((await ctx.request.post('/auth/test-login')).status()).toBe(204)
  const p = await ctx.newPage()
  await p.goto('/settings')
  await expect(p.getByText('Signed in as')).toBeVisible()
  expect((await ctx.request.get('/api/me')).status()).toBe(200)

  await p.getByRole('button', { name: 'Sign out' }).click()
  await expect(p).toHaveURL(/\/login$/)
  expect((await ctx.request.get('/api/me')).status()).toBe(401)
  await p.goto('/')
  await expect(p).toHaveURL(/\/login$/)
  await expect(p.getByRole('link', { name: /Continue with Google/ })).toBeVisible()
  await ctx.close()
})

test('no session: / redirects to /login, /api/* is 401', async ({ browser, baseURL }) => {
  const ctx = await browser.newContext({ baseURL, storageState: { cookies: [], origins: [] } })
  const p = await ctx.newPage()
  await p.goto('/entries')
  await expect(p).toHaveURL(/\/login$/)
  for (const u of ['/api/me', '/api/entries', '/api/ledger/verify', '/api/summary']) {
    expect((await ctx.request.get(u)).status(), u).toBe(401)
  }
  await ctx.close()
})
