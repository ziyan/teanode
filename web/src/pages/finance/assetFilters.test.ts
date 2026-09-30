import { expect, it } from 'vitest'

import {
  NO_ASSET_FILTERS,
  assetFiltersFromSearch,
  assetGroupRows,
  groupAssets,
  orderWithinGroup,
  reportingValue,
  writeAssetFilters,
} from './assetFilters'
import { Asset } from './financeApi'

function asset(id: string, value: number, overrides: Partial<Asset> = {}, currencyCode = 'USD'): Asset {
  return {
    id,
    assetName: `Asset ${id}`,
    assetKind: 'cash',
    isLiability: false,
    currencyCode,
    valuationSource: 'manual',
    isEstimateAllowed: false,
    latestValuation: {
      id: `valuation-${id}`,
      assetId: id,
      valuedOn: '2030-01-01',
      value: String(value),
      currencyCode,
      valuationSource: 'manual',
      evidenceUrls: [],
    },
    ...overrides,
  }
}

const assets = [
  asset('checking', 2000, { assetName: 'Everyday checking' }),
  asset('savings', 500, { assetName: 'Savings abroad' }, 'EUR'),
  asset('house', 300000, { assetName: 'The house', assetKind: 'property' }),
  asset('fund', 1500, {
    assetName: 'Example Index Fund',
    assetKind: 'investment',
    financeSecurityId: 'security-1',
    financeAccountId: 'account-b',
  }),
  asset('shares', 2500, {
    assetName: 'Example Shares',
    assetKind: 'investment',
    financeSecurityId: 'security-2',
    financeAccountId: 'account-a',
  }),
  asset('brokerage', 100, { assetName: 'Brokerage cash', assetKind: 'investment', financeAccountId: 'account-a' }),
  asset('card', 800, { assetName: 'Travel card', assetKind: 'credit_card', isLiability: true }),
  asset('old-car', 0, { assetName: 'Old car', assetKind: 'vehicle', closedOn: '2029-01-01' }),
  asset('yen', 1000, { assetName: 'Yen account' }, 'JPY'),
]
const ids = (list: Asset[]) => list.map((one) => one.id)
const rates = { EUR: 2 }

it('converts a value at its rate and says nothing without one', () => {
  expect(reportingValue(assets[1], 'USD', rates)).toBe(1000)
  expect(reportingValue(assets[0], 'USD', rates)).toBe(2000)
  expect(reportingValue(assets[8], 'USD', rates)).toBeNull()
})

// Owned and owed are totalled apart; a currency with no rate is named
// rather than added in; closed assets are left out unless asked for.
it('groups by kind in balance sheet order with totals', () => {
  const grouping = groupAssets(assets, NO_ASSET_FILTERS, 'USD', rates)
  expect(grouping.groups.map((group) => group.assetKind)).toEqual(['cash', 'investment', 'property', 'credit_card'])
  expect(grouping.groups.map((group) => group.totalAmount)).toEqual([3000, 4100, 300000, 800])
  expect(grouping.ownedAmount).toBe(307100)
  expect(grouping.owedAmount).toBe(800)
  expect(grouping.unconvertedCurrencyCodes).toEqual(['JPY'])
  expect(grouping.groups[3].isLiability).toBe(true)
  const withClosed = groupAssets(assets, { ...NO_ASSET_FILTERS, isClosedShown: true }, 'USD', rates)
  expect(withClosed.groups.map((group) => group.assetKind)).toContain('vehicle')
  expect(withClosed.ownedAmount).toBe(307100)
})

// Holdings sit under their account, after what is not a holding.
it('orders a kind with holdings a finance account at a time', () => {
  expect(ids(orderWithinGroup(assets.filter((one) => one.assetKind === 'investment')))).toEqual([
    'brokerage',
    'shares',
    'fund',
  ])
})

// Words keep the kinds with a match, their matches marked; totals stay the
// whole kind's.
it('keeps only the kinds the words match', () => {
  const grouping = groupAssets(assets, { ...NO_ASSET_FILTERS, text: 'example' }, 'USD', rates)
  expect(grouping.groups.map((group) => group.assetKind)).toEqual(['investment'])
  expect(ids(grouping.groups[0].matchingAssets)).toEqual(['shares', 'fund'])
  expect(grouping.groups[0].totalAmount).toBe(4100)
  expect(grouping.matchingCount).toBe(2)
})

// With closed assets shown an account's holdings are two runs, open then
// closed: two lines, two keys, each counting its own run.
it('names each run of an account\'s holdings once, with its own count and key', () => {
  const holding = (id: string, financeAccountId: string, closedOn?: string) =>
    asset(id, 10, {
      assetKind: 'investment',
      financeSecurityId: `security-${id}`,
      financeAccountId,
      closedOn: closedOn ?? null,
    })
  const grouping = groupAssets(
    [
      asset('brokerage', 5, { assetKind: 'investment', financeAccountId: 'account-a' }),
      holding('one', 'account-a'),
      holding('two', 'account-a'),
      holding('three', 'account-b'),
      holding('four', 'account-a', '2029-01-01'),
    ],
    { ...NO_ASSET_FILTERS, isClosedShown: true },
    'USD',
    {},
  )
  const rows = assetGroupRows('investment', grouping.groups[0].assets)
  const lines = rows.flatMap((row) => (row.rowKind === 'account' ? [[row.rowKey, row.holdingCount]] : []))
  expect(lines).toEqual([
    ['account-investment-account-a-open', 2],
    ['account-investment-account-b-open', 1],
    ['account-investment-account-a-closed', 1],
  ])
  expect(new Set(lines.map((line) => line[0])).size).toBe(lines.length)
  expect(rows.filter((row) => row.rowKind === 'asset')).toHaveLength(5)
})

it('reads and writes the filters in the address without touching the rest of it', () => {
  const filters = { text: 'fund', isClosedShown: true, expandedAssetKinds: ['investment', 'property'] }
  const written = writeAssetFilters(new URLSearchParams('asset=house'), filters)
  expect(written.get('asset')).toBe('house')
  expect(written.get('expandedAssetKinds')).toBe('investment,property')
  expect(assetFiltersFromSearch(written)).toEqual(filters)
  expect(writeAssetFilters(written, NO_ASSET_FILTERS).toString()).toBe('asset=house')
})
