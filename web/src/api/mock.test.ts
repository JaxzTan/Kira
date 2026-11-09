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
})
