import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'

import { graphql } from '../../api'
import { FinanceNetWorthSection } from './financeNetWorth'

vi.hoisted(() => {
  // Read when the modules load, before any test runs.
  window.matchMedia = ((query: string) => ({
    matches: true,
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
  })) as unknown as typeof window.matchMedia
})
vi.mock('../../api', () => ({ graphql: vi.fn() }))
vi.mock('../../i18n/i18n', () => ({
  useTranslation: () => ({
    t: (key: string, values?: Record<string, unknown>) => (values ? `${key} ${JSON.stringify(values)}` : key),
    plural: (count: number, forms: { one: string; other: string }) =>
      `${count === 1 ? forms.one : forms.other} ${count}`,
    language: 'en',
  }),
}))
vi.mock('../../components/toast', () => ({ useToast: () => ({ done: vi.fn(), failed: vi.fn(), failure: vi.fn() }) }))
const execute = vi.mocked(graphql)

// An invented holding of an invented fund, and sixty trades of it.
const holding = {
  id: 'asset-fund',
  assetName: 'Invented Index Fund',
  assetKind: 'investment',
  isLiability: false,
  currencyCode: 'USD',
  financeAccountId: 'account-brokerage',
  financeSecurityId: 'security-fund',
  valuationSource: 'finance_sync',
  isEstimateAllowed: false,
}

function trade(index: number) {
  return {
    id: `trade-${index}`,
    financeAccountId: 'account-brokerage',
    financeSecurityId: 'security-fund',
    providerTradeId: `provider-trade-${index}`,
    tradedOn: '2026-09-02',
    tradeKind: 'buy',
    tradedQuantity: '1',
    unitPrice: '100',
    tradeAmount: '-100',
    feeAmount: '0',
    currencyCode: 'USD',
    description: `Invented trade ${index}`,
  }
}

function Address() {
  return <output data-testid="address">{useLocation().search}</output>
}
const address = () => new URLSearchParams(screen.getByTestId('address').textContent ?? '')

const tradeReads = () =>
  execute.mock.calls
    .filter(([document]) => document.includes('FinanceTrades('))
    .map(([, variables]) => [variables?.offset, variables?.limit, variables?.financeSecurityId])

beforeEach(() => {
  execute.mockImplementation(async (document: string, variables?: Record<string, unknown>) => {
    if (document.includes('FinanceTrades(')) {
      const offset = Number(variables?.offset ?? 0)
      const count = Math.max(0, Math.min(Number(variables?.limit), 60 - offset))
      return {
        FinanceTrades: {
          financeTrades: Array.from({ length: count }, (_, index) => trade(offset + index)),
          totalCount: 60,
        },
      }
    }
    if (document.includes('AssetHistory(')) return { AssetHistory: { asset: holding, assetValuations: [] } }
    if (document.includes('Assets {')) return { Assets: [holding] }
    if (document.includes('FinanceAccounts')) return { FinanceAccounts: [] }
    return {}
  })
})
afterEach(() => {
  cleanup()
  execute.mockReset()
})

// A holding's trades page on the server with the page in the address,
// beside the asset it belongs to.
it('pages a holding’s trades with the page in the address', async () => {
  render(
    <MemoryRouter initialEntries={['/?asset=asset-fund']}>
      <FinanceNetWorthSection />
      <Address />
    </MemoryRouter>,
  )
  expect(await screen.findByText('Invented trade 0')).toBeTruthy()
  expect(screen.getByText('table.range {"first":"1","last":"50","total":"60"}')).toBeTruthy()

  fireEvent.click(screen.getByRole('button', { name: 'table.next' }))
  expect(await screen.findByText('Invented trade 50')).toBeTruthy()
  expect(screen.queryByText('Invented trade 0')).toBeNull()
  expect(address().get('page')).toBe('2')
  expect(tradeReads()).toEqual([
    [0, 50, 'security-fund'],
    [50, 50, 'security-fund'],
  ])
  expect(address().get('asset')).toBe('asset-fund')
})
