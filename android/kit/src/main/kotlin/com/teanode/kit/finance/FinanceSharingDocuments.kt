package com.teanode.kit.finance

/**
 * The GraphQL of this part, the same text as the iPhone app's (ios/TeaNodeKit). The server's tests check the iPhone
 * app's documents against its schema, so a document copied from there is checked too.
 */
object FinanceSharingDocuments {
    const val FINANCE_SHARES = $$"""query {
          ListFinanceAccountShares {
            id financeAccountId accountName accountMask granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName financeShareLevel
          }
        }"""

    const val SHARE_FINANCE_ACCOUNT = $$"""mutation ($financeAccountId: String!, $granteeUserId: String, $granteeCircleId: String, $financeShareLevel: String!) {
          ShareFinanceAccount(financeAccountId: $financeAccountId, granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, financeShareLevel: $financeShareLevel) {
            id financeAccountId accountName accountMask granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName financeShareLevel
          }
        }"""

    const val UNSHARE_FINANCE_ACCOUNT = $$"""mutation ($shareId: String!) { UnshareFinanceAccount(shareId: $shareId) }"""

    const val SHARED_FINANCE_ACCOUNTS = $$"""query ($ownerUserId: String) {
          ListSharedFinanceAccounts(ownerUserId: $ownerUserId) {
            financeAccountId ownerUserId ownerUsername ownerName financeShareLevel accountName accountMask accountKind currencyCode currentBalance balanceAt
            creditUsage { owedAmount creditLimitAmount creditLimitSource usageShare }
          }
        }"""

    const val SHARED_FINANCE_SPENDING = $$"""query ($financeAccountId: String!) {
          ListSharedFinanceSpending(financeAccountId: $financeAccountId) {
            spendingMonth spendingCategoryName currencyCode moneyOut moneyIn financeTransactionCount
          }
        }"""

    const val SHARED_FINANCE_TRANSACTIONS = $$"""query ($financeAccountId: String!, $limit: Int, $offset: Int) {
          ListSharedFinanceTransactions(financeAccountId: $financeAccountId, limit: $limit, offset: $offset) {
            nextCursor totalCount financeTransactions { id postedOn amount currencyCode merchantName spendingCategoryName }
          }
        }"""

    const val ALL_FINANCE_SHARES = $$"""query {
          ListFinanceShares {
            id granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName financeShareLevel createdAt
          }
        }"""

    const val SHARE_ALL_FINANCE = $$"""mutation ($granteeUserId: String, $granteeCircleId: String, $financeShareLevel: String!) {
          ShareFinance(granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, financeShareLevel: $financeShareLevel) {
            id granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName financeShareLevel createdAt
          }
        }"""

    const val UNSHARE_ALL_FINANCE = $$"""mutation ($financeShareId: String!) { UnshareFinance(financeShareId: $financeShareId) }"""

    const val SHARED_ALL_FINANCE = $$"""query ($ownerUserId: String) {
          ListSharedFinance(ownerUserId: $ownerUserId) { ownerUserId ownerUsername ownerName financeShareLevel }
        }"""

    const val SHARED_ALL_FINANCE_SPENDING = $$"""query ($ownerUserId: String!) {
          ListSharedFinanceSpending(ownerUserId: $ownerUserId) {
            spendingMonth spendingCategoryName currencyCode moneyOut moneyIn financeTransactionCount
          }
        }"""

    const val SHARED_ALL_FINANCE_TRANSACTIONS = $$"""query ($ownerUserId: String!, $limit: Int, $offset: Int) {
          ListSharedFinanceTransactions(ownerUserId: $ownerUserId, limit: $limit, offset: $offset) {
            nextCursor totalCount financeTransactions { id postedOn amount currencyCode merchantName spendingCategoryName }
          }
        }"""

}
