import { test as base, expect } from '@playwright/test'

export * from '@playwright/test'
export { expect }

/** Non-browser API clients do not send Sec-Fetch-Site; the server's CSRF check needs it. */
export const SAME_ORIGIN = { 'Sec-Fetch-Site': 'same-origin' }

export const test = base.extend({
  request: async ({ playwright, baseURL, storageState }, use) => {
    const ctx = await playwright.request.newContext({ baseURL, storageState: storageState as string, extraHTTPHeaders: SAME_ORIGIN })
    await use(ctx)
    await ctx.dispose()
  },
})
