import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import type { Gain } from '@/api/hooks'
import { GainsTable } from './gains-table'

const base: Gain = {
  id: 'g1',
  fy: '2025-26',
  category: 'LTCG_EQUITY',
  account_id: 'a1',
  account_name: 'Zerodha main',
  isin: 'INE009A01021',
  symbol: 'INFY',
  sale_date: '2025-08-12',
  acquisition_date: '2017-11-03',
  quantity: '50',
  sale_value: '76000.00',
  cost_of_acquisition: '57550.00',
  charges_deductible: '0',
  gain: '18450.0000',
  holding_days: 2839,
  rules_version: 'fy2025-26@1',
  grandfathered: true,
}

const rows: Gain[] = [
  base,
  { ...base, id: 'g2', symbol: 'TATAMOTORS', category: 'STCG_EQUITY', gain: '-2200.5', holding_days: 40, grandfathered: undefined },
  { ...base, id: 'g3', symbol: 'NIFTY', category: 'FNO', isin: '', contract_key: 'NIFTY|2025-09-25|24500|CE', gain: '0', holding_days: 0 },
]

describe('GainsTable', () => {
  it('renders every row with a signed, coloured gain', () => {
    render(<GainsTable rows={rows} onSelect={() => {}} virtualise={false} />)
    const trs = screen.getAllByTestId('gain-row')
    expect(trs).toHaveLength(3)
    const plus = screen.getByText('+₹18,450.00')
    const minus = screen.getByText('−₹2,200.50')
    expect(plus).toHaveAttribute('data-sign', 'positive')
    expect(minus).toHaveAttribute('data-sign', 'negative')
    expect(plus.className).toContain('text-gain')
    expect(minus.className).toContain('text-loss')
    expect(screen.getAllByText('₹0.00').some((el) => el.getAttribute('data-sign') === 'zero')).toBe(true)
    expect(screen.getAllByText('Grandfathered')).toHaveLength(2)
  })

  it('opens a row with the keyboard', () => {
    const onSelect = vi.fn()
    render(<GainsTable rows={rows} onSelect={onSelect} virtualise={false} />)
    const first = screen.getAllByTestId('gain-row')[0]!
    fireEvent.keyDown(first, { key: 'Enter' })
    expect(onSelect).toHaveBeenCalledWith(rows[0])
  })
})
