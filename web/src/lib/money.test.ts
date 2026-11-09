import { describe, expect, it } from 'vitest'
import { formatAmount, formatRM, formatRMWhole, parseRMToSen, senToInput } from './money'

describe('formatRM', () => {
  it.each([
    [0, 'RM 0.00'],
    [5, 'RM 0.05'],
    [4590, 'RM 45.90'],
    [100, 'RM 1.00'],
    [123456, 'RM 1,234.56'],
    [128450, 'RM 1,284.50'],
    [100000000, 'RM 1,000,000.00'],
    [999999999, 'RM 9,999,999.99'],
    [-4590, 'RM -45.90'],
  ])('%i sen -> %s', (sen, out) => {
    expect(formatRM(sen)).toBe(out)
  })
  it('formatAmount omits prefix', () => expect(formatAmount(123456)).toBe('1,234.56'))
  it('formatRMWhole rounds to whole ringgit', () => {
    expect(formatRMWhole(128450)).toBe('1,285')
    expect(formatRMWhole(64000)).toBe('640')
    expect(formatRMWhole(0)).toBe('0')
  })
  it('senToInput gives editable text', () => {
    expect(senToInput(4480)).toBe('44.80')
    expect(senToInput(5)).toBe('0.05')
  })
})

describe('parseRMToSen', () => {
  it.each([
    ['45.90', 4590],
    ['45.9', 4590],
    ['45', 4500],
    ['0.05', 5],
    ['.5', 50],
    ['1,234.56', 123456],
    ['RM 12.00', 1200],
    ['  7.10 ', 710],
    ['0.29', 29], // classic float trap: 0.29 * 100 = 28.999...
    ['1.15', 115],
  ])('%s -> %i', (input, sen) => {
    expect(parseRMToSen(input)).toBe(sen)
  })
  it.each(['', ' ', 'abc', '12.345', '-5', '-0.50', '1.2.3', '1e3', 'NaN', '12.', 'RM', '99999999999999999999'])(
    'rejects %j',
    (input) => {
      expect(parseRMToSen(input)).toBeNull()
    },
  )
  it('round-trips', () => {
    for (const sen of [1, 29, 115, 4590, 123456, 99999999]) expect(parseRMToSen(senToInput(sen))).toBe(sen)
  })
})
