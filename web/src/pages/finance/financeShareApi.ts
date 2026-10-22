// What the Finance page asks the server about sharing a finance account, and
// the shapes it gets back. The operations are the ones teanode finance share
// and the iPhone app use too (internal/client/finance_share.go); the agent's
// finance tool has none of them. docs/subsystems/finance.md ("Sharing") says
// what each level shows.

// The levels, from least to most: each shows what the one before it shows.
export const FINANCE_SHARE_LEVELS = ['balance', 'spending', 'transactions'] as const
export type FinanceShareLevel = (typeof FINANCE_SHARE_LEVELS)[number]

// isAtLeast says a level shows what another does: a person who reads
// transactions reads spending too.
export function isAtLeast(level: string, other: FinanceShareLevel): boolean {
  const rank = (candidate: string) => FINANCE_SHARE_LEVELS.indexOf(candidate as FinanceShareLevel)
  return rank(level) >= 0 && rank(level) >= rank(other)
}

// One share of one of the person's accounts: with a person or with a circle,
// never both.
export type FinanceShare = {
  id: string
  financeAccountId: string
  accountName: string
  accountMask: string
  granteeUserId: string | null
  granteeUsername: string
  granteeName: string
  granteeCircleId: string | null
  granteeCircleName: string
  financeShareLevel: FinanceShareLevel
}

export type SharedCreditUsage = {
  owedAmount?: string | null
  creditLimitAmount?: string | null
  creditLimitSource: string
  usageShare?: number | null
}

// Somebody else's account as the level it is shared at shows it.
export type SharedFinanceAccount = {
  financeAccountId: string
  ownerUserId: string
  ownerUsername: string
  ownerName: string
  financeShareLevel: FinanceShareLevel
  accountName: string
  accountMask?: string | null
  accountKind: string
  currencyCode: string
  currentBalance?: string | null
  balanceAt?: string | null
  creditUsage: SharedCreditUsage | null
}

export type SharedFinanceSpendingRow = {
  spendingMonth: string
  spendingCategoryName: string
  currencyCode: string
  moneyOut: string
  moneyIn: string
  financeTransactionCount: number
}

export type SharedFinanceTransaction = {
  id: string
  postedOn: string
  amount: string
  currencyCode: string
  merchantName?: string | null
  spendingCategoryName?: string | null
}

export type SharedFinanceTransactionPage = {
  financeTransactions: SharedFinanceTransaction[]
  nextCursor?: string | null
  totalCount: number
}

export const LIST_FINANCE_ACCOUNT_SHARES = `query {
  ListFinanceAccountShares {
    id financeAccountId accountName accountMask granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName financeShareLevel
  }
}`

export const SHARE_FINANCE_ACCOUNT = `mutation ($financeAccountId: String!, $granteeUserId: String, $granteeCircleId: String, $financeShareLevel: String!) {
  ShareFinanceAccount(financeAccountId: $financeAccountId, granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, financeShareLevel: $financeShareLevel) { id }
}`

export const UNSHARE_FINANCE_ACCOUNT = `mutation ($shareId: String!) { UnshareFinanceAccount(shareId: $shareId) }`

export const LIST_SHARED_FINANCE_ACCOUNTS = `query ($ownerUserId: String) {
  ListSharedFinanceAccounts(ownerUserId: $ownerUserId) {
    financeAccountId ownerUserId ownerUsername ownerName financeShareLevel accountName accountMask accountKind currencyCode currentBalance balanceAt
    creditUsage { owedAmount creditLimitAmount creditLimitSource usageShare }
  }
}`

export const LIST_SHARED_FINANCE_SPENDING = `query ($financeAccountId: String!, $from: String, $to: String) {
  ListSharedFinanceSpending(financeAccountId: $financeAccountId, from: $from, to: $to) {
    spendingMonth spendingCategoryName currencyCode moneyOut moneyIn financeTransactionCount
  }
}`

export const LIST_SHARED_FINANCE_TRANSACTIONS = `query ($financeAccountId: String!, $limit: Int, $offset: Int) {
  ListSharedFinanceTransactions(financeAccountId: $financeAccountId, limit: $limit, offset: $offset) {
    nextCursor totalCount financeTransactions { id postedOn amount currencyCode merchantName spendingCategoryName }
  }
}`
