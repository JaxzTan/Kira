import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes, useNavigate } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { createMockApi } from '../api/mock'

const mock = createMockApi()
vi.mock('../api', async (orig) => ({ ...(await orig<typeof import('../api')>()), api: mock }))
const { default: EntryDetail } = await import('./EntryDetail')

describe('EntryDetail', () => {
  it('does not carry entry A edit state onto entry B', async () => {
    const { entries } = await mock.listEntries({})
    const [a, b] = entries
    const nav = (id: number) => `/entries/${id}`
    function Harness() {
      const n = useNavigate()
      return <><button onClick={() => n(nav(b.id))}>goB</button><Routes><Route path="/entries/:id" element={<EntryDetail />} /></Routes></>
    }
    render(<MemoryRouter initialEntries={[nav(a.id)]}><Harness /></MemoryRouter>)
    const user = userEvent.setup()
    await screen.findByRole('heading', { name: a.payee })
    await user.click(screen.getByRole('button', { name: 'Correct entry' }))
    await user.clear(screen.getByLabelText('Payee'))
    await user.type(screen.getByLabelText('Payee'), 'EDITED')
    await user.click(screen.getByRole('button', { name: 'goB' }))
    await screen.findByRole('heading', { name: b.payee })
    expect(screen.queryByLabelText('Payee')).not.toBeInTheDocument()
    expect(screen.queryByDisplayValue('EDITED')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Correct entry' }))
    expect(screen.getByLabelText('Payee')).toHaveValue(b.payee)
  })

  it('blocks correction submit while a receipt uploads', async () => {
    const { entries } = await mock.listEntries({})
    const e = entries[0]
    vi.spyOn(mock, 'uploadReceipt').mockImplementation(() => new Promise(() => {}))
    const amend = vi.spyOn(mock, 'amendEntry')
    render(<MemoryRouter initialEntries={[`/entries/${e.id}`]}><Routes><Route path="/entries/:id" element={<EntryDetail />} /></Routes></MemoryRouter>)
    const user = userEvent.setup()
    await screen.findByRole('heading', { name: e.payee })
    await user.click(screen.getByRole('button', { name: 'Correct entry' }))
    await user.upload(screen.getByLabelText('Receipt'), new File(['x'], 'r.png', { type: 'image/png' }))
    expect(screen.getByRole('button', { name: 'Save correction' })).toBeDisabled()
    expect(amend).not.toHaveBeenCalled()
  })
})
