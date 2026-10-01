import { describe, expect, it } from 'vitest'

import { Asset, FinanceAccount } from './financeApi'
import { hasChoice, isCountedThroughAccount, savingsTargetChoices } from './savingsTargetChoices'

function financeAccount(id: string, accountName: string, institutionName?: string): FinanceAccount {
  return {
    id,
    sourceId: 'source-1',
    providerKind: 'plaid',
    institutionName,
    accountName,
    accountKind: 'investment',
    currencyCode: 'USD',
    isSignInRequired: false,
  }
}

function asset(id: string, assetName: string, fields: Partial<Asset> = {}): Asset {
  return {
    id,
    assetName,
    assetKind: 'investment',
    isLiability: false,
    currencyCode: 'USD',
    valuationSource: 'manual',
    isEstimateAllowed: false,
    ...fields,
  }
}

const brokerage = financeAccount('account-brokerage', 'Brokerage', 'Invented Investments')
const checking = financeAccount('account-checking', 'Checking', 'Example Bank')
const savings = financeAccount('account-savings', 'Savings', 'Example Bank')
const unnamed = financeAccount('account-unnamed', 'Wallet')

describe('savingsTargetChoices', () => {
  it('groups finance accounts by institution, by name, the unnamed one last', () => {
    const { financeAccountGroups } = savingsTargetChoices([unnamed, savings, brokerage, checking], [])
    expect(financeAccountGroups.map((group) => group.institutionName)).toEqual([
      'Example Bank',
      'Invented Investments',
      '',
    ])
    expect(financeAccountGroups[0].financeAccounts.map((account) => account.id)).toEqual([
      'account-checking',
      'account-savings',
    ])
  })

  // A brokerage's positions stay under it, out of the short list.
  it('keeps holdings and account cash under their account, apart from assets that stand alone', () => {
    const { standaloneAssets, accountAssets } = savingsTargetChoices(
      [brokerage, checking],
      [
        asset('asset-fund', 'EXIF (Brokerage)', {
          financeAccountId: 'account-brokerage',
          financeSecurityId: 'security-fund',
        }),
        asset('asset-cash', 'Brokerage', { financeAccountId: 'account-brokerage' }),
        asset('asset-house', 'the house', { assetKind: 'property' }),
        asset('asset-boat', 'a boat', { assetKind: 'vehicle' }),
      ],
    )
    expect(standaloneAssets.map((found) => found.id)).toEqual(['asset-boat', 'asset-house'])
    expect(accountAssets).toHaveLength(1)
    expect(accountAssets[0].financeAccount.id).toBe('account-brokerage')
    expect(accountAssets[0].assets.map((found) => found.id)).toEqual(['asset-cash', 'asset-fund'])
  })

  it('leaves out what is owed and what is closed, unless it is already chosen', () => {
    const assets = [
      asset('asset-loan', 'car loan', { assetKind: 'loan', isLiability: true }),
      asset('asset-sold', 'sold boat', { closedOn: '2026-08-01' }),
      asset('asset-house', 'the house'),
    ]
    expect(savingsTargetChoices([], assets).standaloneAssets.map((found) => found.id)).toEqual(['asset-house'])
    expect(savingsTargetChoices([], assets, ['asset-sold']).standaloneAssets.map((found) => found.id)).toEqual([
      'asset-sold',
      'asset-house',
    ])
  })

  // An asset whose account is gone is still offered, on its own.
  it('lists an asset of an unknown finance account on its own', () => {
    const { standaloneAssets, accountAssets } = savingsTargetChoices(
      [checking],
      [asset('asset-detached', 'old brokerage', { financeAccountId: 'account-gone' })],
    )
    expect(standaloneAssets.map((found) => found.id)).toEqual(['asset-detached'])
    expect(accountAssets).toEqual([])
  })
})

describe('isCountedThroughAccount', () => {
  it('is true only for an asset of a chosen finance account', () => {
    const fund = asset('asset-fund', 'EXIF', { financeAccountId: 'account-brokerage' })
    expect(isCountedThroughAccount(fund, ['account-brokerage'])).toBe(true)
    expect(isCountedThroughAccount(fund, ['account-checking'])).toBe(false)
    expect(isCountedThroughAccount(asset('asset-house', 'the house'), ['account-brokerage'])).toBe(false)
  })
})

describe('hasChoice', () => {
  it('needs an asset or a finance account', () => {
    expect(hasChoice([], [])).toBe(false)
    expect(hasChoice(['asset-house'], [])).toBe(true)
    expect(hasChoice([], ['account-brokerage'])).toBe(true)
  })
})
