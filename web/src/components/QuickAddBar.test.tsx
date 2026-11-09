import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { ApiError } from '../api/errors'
import { createMockApi } from '../api/mock'
import { todayISO } from '../lib/dates'
import { QuickAddBar } from './QuickAddBar'

async function setup() {
  const api = createMockApi()
  const { categories } = await api.listCategories()
  const onSaved = vi.fn()
  render(<QuickAddBar api={api} categories={categories} onSaved={onSaved} />)
  return { api, categories, onSaved, user: userEvent.setup() }
}

describe('QuickAddBar', () => {
  it('tab order is Date, Amount, Category, Payee, Paid with, Note, Receipt, Save', async () => {
    const { user } = await setup()
    const date = screen.getByLabelText('Date')
    date.focus()
    const order = [
      'Amount (RM)', 'Category', 'Payee', 'Paid with', 'Note',
    ]
    for (const label of order) {
      await user.tab()
      expect(screen.getByLabelText(label)).toHaveFocus()
    }
    await user.tab()
    expect(screen.getByRole('button', { name: /attach receipt/i })).toHaveFocus()
    await user.tab()
    expect(screen.getByRole('button', { name: 'Save' })).toHaveFocus()
  })

  it('defaults the date to today', async () => {
    await setup()
    expect(screen.getByLabelText('Date')).toHaveValue(todayISO())
  })

  it('Enter saves, clears amount/payee/note, keeps date/category/method, refocuses Amount', async () => {
    const { api, user, onSaved } = await setup()
    await user.clear(screen.getByLabelText('Date'))
    await user.type(screen.getByLabelText('Date'), '2026-10-31')
    await user.selectOptions(screen.getByLabelText('Category'), 'Hardware')
    await user.selectOptions(screen.getByLabelText('Paid with'), 'cash')
    const date = (screen.getByLabelText('Date') as HTMLInputElement).value

    await user.type(screen.getByLabelText('Amount (RM)'), '45.90')
    await user.type(screen.getByLabelText('Payee'), 'Cloudflare')
    await user.type(screen.getByLabelText('Note'), 'R2{Enter}')

    await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(1))
    expect(screen.getByLabelText('Amount (RM)')).toHaveValue('')
    expect(screen.getByLabelText('Payee')).toHaveValue('')
    expect(screen.getByLabelText('Note')).toHaveValue('')
    expect(screen.getByLabelText('Date')).toHaveValue(date)
    expect(screen.getByLabelText('Category')).toHaveDisplayValue('Hardware')
    expect(screen.getByLabelText('Paid with')).toHaveDisplayValue('Cash')
    expect(screen.getByLabelText('Amount (RM)')).toHaveFocus()

    const saved = onSaved.mock.calls[0][0]
    expect(saved).toMatchObject({ amount_sen: 4590, payee: 'Cloudflare', method: 'cash', note: 'R2' })
    // new row is first in the ledger view for that date
    const list = await api.listEntries({ q: 'Cloudflare' })
    expect(list.entries[0].amount_sen).toBe(4590)
  })

  it('Enter in the Amount field also saves', async () => {
    const { user, onSaved } = await setup()
    await user.type(screen.getByLabelText('Payee'), 'Grab')
    await user.type(screen.getByLabelText('Amount (RM)'), '12{Enter}')
    await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(1))
    expect(onSaved.mock.calls[0][0].amount_sen).toBe(1200)
  })

  it('shows inline errors and does not save invalid input', async () => {
    const { user, onSaved } = await setup()
    await user.type(screen.getByLabelText('Amount (RM)'), '12.345{Enter}')
    expect(await screen.findByText(/enter an amount/i)).toBeInTheDocument()
    expect(screen.getByText(/payee is required/i)).toBeInTheDocument()
    expect(onSaved).not.toHaveBeenCalled()
    expect(screen.getByLabelText('Amount (RM)')).toHaveFocus()
  })

  it('asks to confirm a date more than a year away, then resends with confirm_date', async () => {
    const { api, user, onSaved } = await setup()
    const spy = vi.spyOn(api, 'createEntry')
    const year = new Date().getFullYear() + 3
    const date = screen.getByLabelText('Date')
    await user.clear(date)
    await user.type(date, `${year}-01-15`)
    await user.type(screen.getByLabelText('Amount (RM)'), '10')
    await user.type(screen.getByLabelText('Payee'), 'Future Co{Enter}')
    expect(await screen.findByText(/more than a year/i)).toBeInTheDocument()
    expect(onSaved).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: /yes, save with this date/i }))
    await waitFor(() => expect(onSaved).toHaveBeenCalledTimes(1))
    expect(spy).toHaveBeenLastCalledWith(expect.objectContaining({ confirm_date: true }))
  })
})

describe('QuickAddBar receipt upload race and network errors', () => {
  function deferredUpload(api: ReturnType<typeof createMockApi>) {
    let resolve!: (r: { sha256: string; ext: 'png' }) => void
    vi.spyOn(api, 'uploadReceipt').mockImplementation(() => new Promise((res) => { resolve = res as typeof resolve }))
    return (sha: string) => resolve({ sha256: sha, ext: 'png' })
  }
  const file = () => new File(['x'], 'r.png', { type: 'image/png' })

  it('refuses to save while a receipt uploads and a late result does not land on a reset form', async () => {
    const api = createMockApi()
    const { categories } = await api.listCategories()
    const finish = deferredUpload(api)
    const onSaved = vi.fn()
    render(<QuickAddBar api={api} categories={categories} onSaved={onSaved} />)
    const user = userEvent.setup()
    await user.type(screen.getByLabelText('Payee'), 'Grab')
    await user.type(screen.getByLabelText('Amount (RM)'), '12')
    await user.upload(screen.getByTestId('receipt-input'), file())
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
    await user.type(screen.getByLabelText('Note'), '{Enter}')
    expect(await screen.findByText('Waiting for receipt upload…')).toBeInTheDocument()
    expect(onSaved).not.toHaveBeenCalled()
    finish('a'.repeat(64))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled())
    expect(screen.getByRole('button', { name: /receipt attached/i })).toBeInTheDocument()
  })

  it('ignores a stale upload that resolves after the form was reset', async () => {
    const api = createMockApi()
    const { categories } = await api.listCategories()
    const calls: Array<(r: { sha256: string; ext: 'png' }) => void> = []
    vi.spyOn(api, 'uploadReceipt').mockImplementation(() => new Promise((res) => { calls.push(res as never) }))
    const { unmount } = render(<QuickAddBar api={api} categories={categories} />)
    const user = userEvent.setup()
    await user.upload(screen.getByTestId('receipt-input'), file())
    await user.upload(screen.getByTestId('receipt-input'), file())
    calls[0]({ sha256: 'b'.repeat(64), ext: 'png' }) // stale
    await Promise.resolve()
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
    expect(screen.queryByRole('button', { name: /receipt attached/i })).not.toBeInTheDocument()
    unmount()
    calls[1]({ sha256: 'c'.repeat(64), ext: 'png' }) // after unmount: must not throw
  })

  it('on a network error keeps values, warns, and asks the list to refetch', async () => {
    const api = createMockApi()
    const { categories } = await api.listCategories()
    vi.spyOn(api, 'createEntry').mockRejectedValue(new ApiError(0, { error: 'Could not reach the server.', code: 'internal' }))
    const onUncertain = vi.fn()
    const onSaved = vi.fn()
    render(<QuickAddBar api={api} categories={categories} onSaved={onSaved} onUncertain={onUncertain} />)
    const user = userEvent.setup()
    await user.type(screen.getByLabelText('Payee'), 'Grab')
    await user.type(screen.getByLabelText('Amount (RM)'), '12{Enter}')
    expect(await screen.findByText(/server may have saved this/i)).toBeInTheDocument()
    expect(onUncertain).toHaveBeenCalledTimes(1)
    expect(onSaved).not.toHaveBeenCalled()
    expect(screen.getByLabelText('Amount (RM)')).toHaveValue('12')
    expect(screen.getByLabelText('Payee')).toHaveValue('Grab')
  })
})
