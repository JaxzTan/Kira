import { describe, expect, it } from 'vitest'
import { isApiError } from './errors'
import { createMockApi } from './mock'
import type { EntryInput } from './types'

const input = (over: Partial<EntryInput> = {}): EntryInput => ({
  occurred_on: new Date().toISOString().slice(0, 10), amount_sen: 1000, category_id: 1, payee: 'X',
  method: 'card', note: null, receipt_sha256: null, ...over,
})

describe('mock api semantics', () => {
  it('amend keeps history rows; void needs a reason and leaves totals', async () => {
    const api = createMockApi({ seed: false })
    const a = await api.createEntry(input())
    await api.amendEntry(a.entry.id, input({ amount_sen: 2000 }))
    const h = await api.history(a.entry.id)
    expect(h.rows.map((r) => r.kind)).toEqual(['create', 'amend'])
    expect(h.rows[0].amount_sen).toBe(1000)
    expect(h.entry).toMatchObject({ amount_sen: 2000, status: 'amended', amend_count: 1 })

    await expect(api.voidEntry(a.entry.id, { reason: '  ' })).rejects.toMatchObject({ code: 'validation' })
    await api.voidEntry(a.entry.id, { reason: 'dup' })
    const list = await api.listEntries()
    expect(list.entries).toHaveLength(1)
    expect(list.entries[0].status).toBe('voided')
    expect(list).toMatchObject({ total_sen: 0, count: 0, voided_count: 1 })
    await expect(api.amendEntry(a.entry.id, input())).rejects.toSatisfy((e) => isApiError(e) && e.status === 409)
    expect((await api.verify()).entries).toBe(3)
  })

  it('seeded summary excludes voided and has 6 trend months', async () => {
    const api = createMockApi()
    const s = await api.summary()
    expect(s.trend).toHaveLength(6)
    expect(s.voided_count).toBe(1)
    expect(s.by_category.reduce((n, c) => n + c.total_sen, 0)).toBe(s.total_sen)
  })

  it('mixed directions: totals, net, filters and category/direction mismatch', async () => {
    const api = createMockApi({ seed: false })
    const { categories } = await api.listCategories()
    const exp = categories.find((c) => c.direction === 'expense')!
    const inc = categories.find((c) => c.name === 'Client revenue')!
    expect(categories.filter((c) => c.direction === 'income').map((c) => c.name)).toEqual(['Client revenue', 'Product sales', 'Refunds & rebates', 'Other income'])
    await api.createEntry(input({ category_id: exp.id, amount_sen: 30000 }))
    await api.createEntry(input({ category_id: inc.id, amount_sen: 100000, direction: 'income' }))
    const voided = await api.createEntry(input({ category_id: inc.id, amount_sen: 999, direction: 'income' }))
    await api.voidEntry(voided.entry.id, { reason: 'x' })

    await expect(api.createEntry(input({ category_id: inc.id }))).rejects.toMatchObject({ code: 'validation', fields: { category_id: 'category is for income' } })
    await expect(api.createEntry(input({ category_id: exp.id, direction: 'income' }))).rejects.toMatchObject({ fields: { category_id: 'category is for expenses' } })

    const all = await api.listEntries()
    expect(all).toMatchObject({ total_sen: 30000, income_sen: 100000, net_sen: 70000, count: 2, voided_count: 1 })
    expect(await api.listEntries({ direction: 'income' })).toMatchObject({ total_sen: 0, income_sen: 100000, net_sen: 100000 })
    expect(await api.listEntries({ direction: 'expense' })).toMatchObject({ total_sen: 30000, income_sen: 0, net_sen: -30000 })

    const s = await api.summary()
    expect(s).toMatchObject({ total_sen: 30000, income_sen: 100000, net_sen: 70000, missing_receipts: 1 })
    expect(s.by_category).toHaveLength(1)
    expect(s.trend[5]).toMatchObject({ total_sen: 30000, income_sen: 100000 })
    expect(s.recent).toHaveLength(3)
  })

  it('amend can switch direction together with a matching category; history records it', async () => {
    const api = createMockApi({ seed: false })
    const { categories } = await api.listCategories()
    const a = await api.createEntry(input({ category_id: 1 }))
    const inc = categories.find((c) => c.direction === 'income')!
    await expect(api.amendEntry(a.entry.id, input({ category_id: 1, direction: 'income' }))).rejects.toMatchObject({ code: 'validation' })
    await api.amendEntry(a.entry.id, input({ category_id: inc.id, direction: 'income' }))
    const h = await api.history(a.entry.id)
    expect(h.rows.map((r) => r.direction)).toEqual(['expense', 'income'])
    expect(h.entry.direction).toBe('income')
  })

  it('seeded data has income, net can be negative, and by_category is expenses only', async () => {
    const api = createMockApi()
    const s = await api.summary()
    expect(s.income_sen).toBeGreaterThan(0)
    expect(s.net_sen).toBe(s.income_sen - s.total_sen)
    expect(s.trend.some((m) => m.income_sen > 0)).toBe(true)
    expect(s.by_category.reduce((n, c) => n + c.total_sen, 0)).toBe(s.total_sen)
  })
})
