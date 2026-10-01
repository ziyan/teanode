import { Asset, FinanceAccount } from './financeApi'

// What a savings target measured by assets and accounts can choose, laid
// out for its dialog. A linked brokerage values an asset per position, so
// the dialog offers finance accounts first, one checkbox each and grouped
// by institution, and keeps the assets those accounts value apart, under
// their account, out of the short list of assets that stand on their own.

export type TargetMeasure = 'cash_flow' | 'asset_value' | 'net_worth'

// The order the dialog offers the measures in.
export const TARGET_MEASURES: TargetMeasure[] = ['cash_flow', 'net_worth', 'asset_value']

export type FinanceAccountGroup = {
  // Empty when the finance accounts do not say their institution.
  institutionName: string
  financeAccounts: FinanceAccount[]
}

export type AccountAssets = {
  financeAccount: FinanceAccount
  assets: Asset[]
}

export type SavingsTargetChoices = {
  financeAccountGroups: FinanceAccountGroup[]
  // Assets no finance account values: a house, a car, an account the
  // agent reads from a connected server.
  standaloneAssets: Asset[]
  // The assets each finance account values (its cash and its holdings),
  // for choosing one alone; only accounts that value any are listed.
  accountAssets: AccountAssets[]
}

const byName = (left: string, right: string) => left.localeCompare(right, undefined, { sensitivity: 'base' })

// isChoosable says an asset can be chosen: owned and still open. One
// already chosen stays, so a target that counts it can be saved unchanged.
function isChoosable(asset: Asset, chosenAssetIds: Set<string>): boolean {
  return chosenAssetIds.has(asset.id) || (!asset.isLiability && !asset.closedOn)
}

export function savingsTargetChoices(
  financeAccounts: FinanceAccount[],
  assets: Asset[],
  chosenAssetIds: string[] = [],
): SavingsTargetChoices {
  const chosen = new Set(chosenAssetIds)
  const groupByInstitution = new Map<string, FinanceAccount[]>()
  for (const financeAccount of financeAccounts) {
    const institutionName = financeAccount.institutionName?.trim() ?? ''
    groupByInstitution.set(institutionName, [...(groupByInstitution.get(institutionName) ?? []), financeAccount])
  }
  const financeAccountGroups = [...groupByInstitution.entries()]
    .map(([institutionName, grouped]) => ({
      institutionName,
      financeAccounts: [...grouped].sort((left, right) => byName(left.accountName, right.accountName)),
    }))
    // A group with no institution's name goes last.
    .sort((left, right) =>
      !left.institutionName || !right.institutionName
        ? Number(!left.institutionName) - Number(!right.institutionName)
        : byName(left.institutionName, right.institutionName),
    )

  const isKnownAccount = new Set(financeAccounts.map((financeAccount) => financeAccount.id))
  const standaloneAssets: Asset[] = []
  const assetsByAccount = new Map<string, Asset[]>()
  for (const asset of assets) {
    if (!isChoosable(asset, chosen)) continue
    const financeAccountId = asset.financeAccountId ?? ''
    if (financeAccountId && isKnownAccount.has(financeAccountId)) {
      assetsByAccount.set(financeAccountId, [...(assetsByAccount.get(financeAccountId) ?? []), asset])
    } else {
      standaloneAssets.push(asset)
    }
  }
  standaloneAssets.sort((left, right) => byName(left.assetName, right.assetName))
  const accountAssets = financeAccountGroups
    .flatMap((group) => group.financeAccounts)
    .filter((financeAccount) => assetsByAccount.has(financeAccount.id))
    .map((financeAccount) => ({
      financeAccount,
      assets: [...(assetsByAccount.get(financeAccount.id) ?? [])].sort((left, right) =>
        byName(left.assetName, right.assetName),
      ),
    }))
  return { financeAccountGroups, standaloneAssets, accountAssets }
}

// isCountedThroughAccount says a chosen finance account already counts the
// asset, so choosing it alone as well would change nothing.
export function isCountedThroughAccount(asset: Asset, financeAccountIds: string[]): boolean {
  return !!asset.financeAccountId && financeAccountIds.includes(asset.financeAccountId)
}

// hasChoice says an asset value target counts something.
export function hasChoice(assetIds: string[], financeAccountIds: string[]): boolean {
  return assetIds.length > 0 || financeAccountIds.length > 0
}
