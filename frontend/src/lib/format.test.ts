import { describe, expect, it } from 'vitest'
import { formatDate, formatINR, formatQty, formatRate, formatSignedINR, signOf } from './format'

describe('formatINR', () => {
  it('groups in lakhs and crores', () => {
    expect(formatINR('1234567.89')).toBe('₹12,34,567.89')
    expect(formatINR('123456789.5')).toBe('₹12,34,56,789.50')
    expect(formatINR('999')).toBe('₹999.00')
  })
  it('renders whole amounts without paise when asked', () => {
    expect(formatINR('125000', { whole: true })).toBe('₹1,25,000')
    expect(formatINR('10000000', { whole: true })).toBe('₹1,00,00,000')
  })
  it('uses a real minus sign for negatives and a dash for missing', () => {
    expect(formatINR('-12.5')).toBe('−₹12.50')
    expect(formatINR(null)).toBe('—')
    expect(formatINR('')).toBe('—')
  })
})

describe('formatSignedINR', () => {
  it('always carries a sign', () => {
    expect(formatSignedINR('18450.0000')).toBe('+₹18,450.00')
    expect(formatSignedINR('-2200')).toBe('−₹2,200.00')
  })
  it('shows zero without a sign', () => {
    expect(formatSignedINR('0')).toBe('₹0.00')
    expect(formatSignedINR('0.0000')).toBe('₹0.00')
    expect(formatSignedINR('-0.00')).toBe('₹0.00')
  })
})

describe('signOf', () => {
  it('reads the sign from the text, not a float', () => {
    expect(signOf('0.0001')).toBe('positive')
    expect(signOf('-0.0001')).toBe('negative')
    expect(signOf('-0')).toBe('zero')
    expect(signOf(undefined)).toBe('zero')
  })
})

describe('other formatters', () => {
  it('formats quantities, rates and dates', () => {
    expect(formatQty('50.000000')).toBe('50')
    expect(formatQty('12.5')).toBe('12.5')
    expect(formatRate('0.125')).toBe('12.5%')
    expect(formatRate('0.2')).toBe('20%')
    expect(formatDate('2025-08-12')).toBe('12 Aug 2025')
    expect(formatDate('not-a-date')).toBe('not-a-date')
  })
})
