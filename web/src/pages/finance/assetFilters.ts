import { Asset, amountOf, isHolding } from './financeApi'

// The Net worth section's assets, grouped by asset kind: a linked brokerage
// adds an asset a position, and a hundred holdings are read as one
// Investment line that opens to show them, beside the house and the car.
// The groups opened, the words searched for and whether closed assets are
// listed are kept in the address, as the Transactions section keeps its
// filters, so Back returns to the same view; the asset page (named by
// asset= in the same address) is untouched by them.

// The kinds of what is owned, then of what is owed, in the order a person
// reads a balance sheet.
export const OWNED_ASSET_KINDS = ['cash', 'investment', 'retirement', 'property', 'vehicle', 'other_asset']
export const OWED_ASSET_KINDS = ['credit_card', 'loan', 'mortgage', 'other_liability']

export type AssetFilters = {
  text: string
  isClosedShown: boolean
  expandedAssetKinds: string[]
}

export const NO_ASSET_FILTERS: AssetFilters = { text: '', isClosedShown: false, expandedAssetKinds: [] }

// assetFiltersFromSearch reads the filters out of the address.
export function assetFiltersFromSearch(search: URLSearchParams): AssetFilters {
  return {
    text: (search.get('text') ?? '').trim(),
    isClosedShown: search.get('isClosedShown') === 'true',
    expandedAssetKinds: (search.get('expandedAssetKinds') ?? '')
      .split(',')
      .map((kind) => kind.trim())
      .filter(Boolean),
  }
}

// writeAssetFilters puts the filters into an address, keeping everything
// else in it (an asset being shown), and leaving out the filters at their
// default so an unfiltered list has a bare address.
export function writeAssetFilters(search: URLSearchParams, filters: AssetFilters): URLSearchParams {
  const written = new URLSearchParams(search)
  const values: Record<string, string> = {
    text: filters.text.trim(),
    isClosedShown: filters.isClosedShown ? 'true' : '',
    expandedAssetKinds: [...new Set(filters.expandedAssetKinds)].join(','),
  }
  for (const [name, value] of Object.entries(values)) {
    if (value) {
      written.set(name, value)
    } else {
      written.delete(name)
    }
  }
  return written
}

// reportingValue is an asset's latest value in the reporting currency, a
// liability's as a positive amount owed: its own currency's value times
// the rate to the reporting one. Null when it has no value, or its currency
// has no rate, so it is left out of the totals rather than added in the
// wrong currency.
export function reportingValue(
  asset: Asset,
  reportingCurrencyCode: string,
  rates: Record<string, number>,
): number | null {
  const valuation = asset.latestValuation
  if (!valuation) return null
  const rate = valuation.currencyCode === reportingCurrencyCode ? 1 : rates[valuation.currencyCode]
  if (rate === undefined) return null
  return amountOf(valuation.value) * rate
}

export type AssetGroup = {
  assetKind: string
  isLiability: boolean
  // Every asset of the kind that is listed, and those the words match.
  assets: Asset[]
  matchingAssets: Asset[]
  // The open assets' latest values added up in the reporting currency,
  // positive for what is owed as for what is owned.
  totalAmount: number
}

export type AssetGrouping = {
  groups: AssetGroup[]
  ownedAmount: number
  owedAmount: number
  unconvertedCurrencyCodes: string[]
  matchingCount: number
}

// orderWithinGroup is a kind's assets as they are read: the ones that are
// not holdings first, largest first, then the holdings a finance account
// at a time, largest first within it.
export function orderWithinGroup(assets: Asset[]): Asset[] {
  const value = (asset: Asset) => amountOf(asset.latestValuation?.value)
  const plain = assets.filter((asset) => !isHolding(asset)).sort((left, right) => value(right) - value(left))
  const holdings = assets
    .filter(isHolding)
    .sort(
      (left, right) =>
        (left.financeAccountId ?? '').localeCompare(right.financeAccountId ?? '') || value(right) - value(left),
    )
  return [...plain, ...holdings]
}

// groupAssets is the assets a group a kind, in balance sheet order, with
// each kind's total and the totals of what is owned and what is owed. The
// totals count open assets only; closed ones are listed when asked for,
// under their kind, and closed last within it. A kind whose assets the
// words all miss is left out.
export function groupAssets(
  assets: Asset[],
  filters: AssetFilters,
  reportingCurrencyCode: string,
  rates: Record<string, number>,
): AssetGrouping {
  const words = filters.text.toLocaleLowerCase()
  const listed = assets.filter((asset) => filters.isClosedShown || !asset.closedOn)
  const knownKinds = [...OWNED_ASSET_KINDS, ...OWED_ASSET_KINDS]
  const otherKinds = [...new Set(listed.map((asset) => asset.assetKind))]
    .filter((kind) => !knownKinds.includes(kind))
    .sort()
  const unconverted = new Set<string>()
  let ownedAmount = 0
  let owedAmount = 0
  let matchingCount = 0
  const groups: AssetGroup[] = []
  for (const assetKind of [...knownKinds, ...otherKinds]) {
    const ofKind = listed.filter((asset) => asset.assetKind === assetKind)
    if (ofKind.length === 0) continue
    const matching = words ? ofKind.filter((asset) => asset.assetName.toLocaleLowerCase().includes(words)) : ofKind
    let totalAmount = 0
    for (const asset of ofKind) {
      if (asset.closedOn || !asset.latestValuation) continue
      const value = reportingValue(asset, reportingCurrencyCode, rates)
      if (value === null) {
        unconverted.add(asset.latestValuation.currencyCode)
        continue
      }
      totalAmount += value
    }
    const isLiability = ofKind.some((asset) => asset.isLiability)
    if (isLiability) {
      owedAmount += totalAmount
    } else {
      ownedAmount += totalAmount
    }
    if (matching.length === 0) continue
    matchingCount += matching.length
    const closedLast = (list: Asset[]) => [
      ...orderWithinGroup(list.filter((asset) => !asset.closedOn)),
      ...orderWithinGroup(list.filter((asset) => asset.closedOn)),
    ]
    groups.push({
      assetKind,
      isLiability,
      assets: closedLast(ofKind),
      matchingAssets: closedLast(matching),
      totalAmount,
    })
  }
  return { groups, ownedAmount, owedAmount, unconvertedCurrencyCodes: [...unconverted].sort(), matchingCount }
}
