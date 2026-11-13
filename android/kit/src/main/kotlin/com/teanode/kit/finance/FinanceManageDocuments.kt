package com.teanode.kit.finance

/**
 * The GraphQL of this part, the same text as the iPhone app's (ios/TeaNodeKit). The server's tests check the iPhone
 * app's documents against its schema, so a document copied from there is checked too.
 */
object FinanceManageDocuments {
    const val FINANCE_TRADE_FIELDS = $$"""id financeAccountId financeSecurityId financeSecurity { tickerSymbol securityName } providerTradeId tradedOn
        tradeKind tradeSubkind tradedQuantity unitPrice tradeAmount feeAmount currencyCode description"""

    const val TRANSACTION_ROWS_VARIABLES = $$"""($financeAccountId: String, $isNewAccount: Boolean, $institutionName: String!, $accountName: String,
          $accountNumber: String, $isAccountNumberPartial: Boolean, $statementAccountKind: String!, $currencyCode: String!,
          $bankCode: String, $transactionRows: [TransactionRowInput!]!, $ledgerBalanceAmount: String, $ledgerBalanceOn: String,
          $ledgerBalanceTimeZone: String, $monthlyTotals: [MonthlyTotalInput!])"""

    const val TRANSACTION_ROWS_ARGUMENTS = $$"""(financeAccountId: $financeAccountId, isNewAccount: $isNewAccount, institutionName: $institutionName,
          accountName: $accountName, accountNumber: $accountNumber, isAccountNumberPartial: $isAccountNumberPartial,
          statementAccountKind: $statementAccountKind, currencyCode: $currencyCode, bankCode: $bankCode,
          transactionRows: $transactionRows, ledgerBalanceAmount: $ledgerBalanceAmount, ledgerBalanceOn: $ledgerBalanceOn,
          ledgerBalanceTimeZone: $ledgerBalanceTimeZone, monthlyTotals: $monthlyTotals)"""

    const val FINANCE_TRADES = $$"""query ($financeAccountId: String, $financeSecurityId: String, $limit: Int, $offset: Int) {
          FinanceTrades(financeAccountId: $financeAccountId, financeSecurityId: $financeSecurityId, limit: $limit, offset: $offset) {
            financeTrades { $${FINANCE_TRADE_FIELDS} }
            totalCount
          }
        }"""

    const val RENAME_STATEMENT_ACCOUNT = $$"""mutation ($financeAccountId: String!, $accountName: String, $accountMask: String) {
          RenameStatementAccount(financeAccountId: $financeAccountId, accountName: $accountName, accountMask: $accountMask) {
            id accountName accountMask
          }
        }"""

    const val DELETE_STATEMENT_ACCOUNT = $$"""mutation ($financeAccountId: String!) {
          DeleteStatementAccount(financeAccountId: $financeAccountId) { financeAccountId deletedTransactionCount deletedAssetCount }
        }"""

    const val PROPOSE_SPENDING_RULES = $$"""query ($financeTransactionIds: [String!]!, $spendingCategoryId: String!) {
          ProposeSpendingRules(financeTransactionIds: $financeTransactionIds, spendingCategoryId: $spendingCategoryId) {
            spendingRuleProposals {
              matchText spendingCategoryId financeTransactionCount changedTransactionCount
              aheadOfSpendingRule { id matchText spendingCategoryId }
            }
            tooGenericMatchTextCount changingNumberMatchTextCount overLimitMatchTextCount
          }
        }"""

    const val FINANCE_PROVIDER_KINDS = $$"""query { FinanceProviders { providerKind isBrowserRequired } }"""

    const val IMPORT_FINANCE_CREDENTIAL = $$"""mutation ($providerKind: String!, $credential: String!, $institutionName: String) {
          ImportFinanceCredential(providerKind: $providerKind, credential: $credential, institutionName: $institutionName) { id }
        }"""

    const val PREVIEW_IMPORT_TRANSACTIONS = $$"""query $${TRANSACTION_ROWS_VARIABLES} {
          PreviewImportTransactions$${TRANSACTION_ROWS_ARGUMENTS} {
            financeAccountId accountName isNewAccount accountMatch currencyCode
            newTransactionRows { rowNumber postedOn description amount hasNearbyStoredTransaction }
            presentTransactionRows { rowNumber postedOn description amount hasNearbyStoredTransaction }
            firstPostedOn lastPostedOn moneyInAmount moneyOutAmount verificationSummary
          }
        }"""

    const val IMPORT_TRANSACTIONS = $$"""mutation $${TRANSACTION_ROWS_VARIABLES} {
          ImportTransactions$${TRANSACTION_ROWS_ARGUMENTS} {
            importedAt statementImportOrigin addedTransactionCount updatedTransactionCount unchangedTransactionCount financeAccountNames importErrorMessage
          }
        }"""

}
