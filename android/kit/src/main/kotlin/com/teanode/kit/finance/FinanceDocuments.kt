package com.teanode.kit.finance

/**
 * The GraphQL of this part, the same text as the iPhone app's (ios/TeaNodeKit). The server's tests check the iPhone
 * app's documents against its schema, so a document copied from there is checked too.
 */
object FinanceDocuments {
    const val FINANCE_ACCOUNT_FIELDS = $$"""id sourceId providerKind institutionName accountName accountMask accountKind currencyCode
        currentBalance availableBalance balanceAt isSignInRequired reportingCurrencyCode convertedCurrentBalance"""

    const val FINANCE_TRANSACTION_FIELDS = $$"""id financeAccountId providerTransactionId postedOn transactedAt amount currencyCode
        description merchantName providerCategoryPrimary providerCategoryDetailed isPending pendingProviderTransactionId
        spendingCategoryId categorizedBy categorizationConfidence
        duplicateOfTransactionId duplicateDecidedBy annotation annotatedBy receiptCount createdAt modifiedAt"""

    const val FINANCE_RECEIPT_FIELDS = $$"""id receiptSourceKind agentAttachmentId mailboxItemId merchantName merchantReceiptNumber purchasedOn
        currencyCode subtotalAmount totalAmount receiptCheckState checkDifferenceAmount
        receiptLines { id lineNumber receiptLineKind description quantity quantityUnit unitPriceAmount lineAmount }
        receiptMatches { receiptId financeTransactionId matchedAmount receiptMatchSource }"""

    const val FINANCE_ASSET_FIELDS = $$"""id assetName assetKind isLiability currencyCode financeAccountId
        financeSecurity { tickerSymbol securityName closePrice closePriceOn }
        valuationSource estimateDescription isEstimateAllowed closedOn"""

    const val FINANCE_VALUATION_FIELDS = $$"""id valuedOn value currencyCode valuationSource estimateLow estimateHigh valuationNote heldQuantity unitPrice costBasis"""

    const val FINANCE_PRESENCE = $$"""query { FinanceProviders { providerKind isBrowserRequired } FinanceSources { id } }"""

    const val FINANCE_ZONE = $$"""query { ReadAgent { timezone } }"""

    const val FINANCE_SOURCES = $$"""query {
          FinanceSources { id name providerKind institutionName isEnabled lastRunAt nextRunAt lastError isSignInRequired
            financeAccounts { $${FINANCE_ACCOUNT_FIELDS} } }
        }"""

    const val FINANCE_ACCOUNTS = $$"""query { FinanceAccounts { $${FINANCE_ACCOUNT_FIELDS} } }"""

    const val CREDIT_USAGE = $$"""query {
          CreditUsage { reportingCurrencyCode unconvertedCurrencyCodes totalOwedAmount totalCreditLimitAmount usageShare
            leftOutCardCount leftOutOwedAmount
            creditCards { financeAccountId accountName accountMask institutionName currencyCode owedAmount creditLimitAmount
              creditLimitSource usageShare } }
        }"""

    const val FINANCE_TRANSACTIONS = $$"""query ($from: String, $to: String, $financeAccountId: String, $text: String,
          $spendingCategoryId: String, $isUncategorized: Boolean, $duplicateOfTransactionId: String,
          $isDuplicateIncluded: Boolean, $financeTransactionIds: [String!], $limit: Int, $offset: Int) {
          FinanceTransactions(from: $from, to: $to, financeAccountId: $financeAccountId, text: $text,
            spendingCategoryId: $spendingCategoryId, isUncategorized: $isUncategorized,
            duplicateOfTransactionId: $duplicateOfTransactionId, isDuplicateIncluded: $isDuplicateIncluded,
            financeTransactionIds: $financeTransactionIds, limit: $limit, offset: $offset) {
            financeTransactions { $${FINANCE_TRANSACTION_FIELDS} }
            totalCount
            leftOutDuplicateCount
          }
        }"""

    const val CATEGORIZE_TRANSACTION = $$"""mutation ($financeTransactionId: String!, $spendingCategoryId: String, $shouldCreateSpendingRule: Boolean) {
          CategorizeTransaction(financeTransactionId: $financeTransactionId, spendingCategoryId: $spendingCategoryId,
            shouldCreateSpendingRule: $shouldCreateSpendingRule) {
            financeTransaction { $${FINANCE_TRANSACTION_FIELDS} }
            spendingRule { id matchText }
          }
        }"""

    const val CATEGORIZE_TRANSACTIONS = $$"""mutation ($financeTransactionIds: [String!]!, $spendingCategoryId: String) {
          CategorizeTransactions(financeTransactionIds: $financeTransactionIds, spendingCategoryId: $spendingCategoryId) {
            financeTransactions { id }
          }
        }"""

    const val COUNT_TRANSACTION = $$"""mutation ($financeTransactionId: String!) { CountTransaction(financeTransactionId: $financeTransactionId) { id } }"""

    const val UNDO_COUNT_TRANSACTION = $$"""mutation ($financeTransactionId: String!) { UndoCountTransaction(financeTransactionId: $financeTransactionId) { id } }"""

    const val ANNOTATE_TRANSACTION = $$"""mutation ($financeTransactionId: String!, $annotation: String) {
          AnnotateTransaction(financeTransactionId: $financeTransactionId, annotation: $annotation) { id }
        }"""

    const val FINANCE_RECEIPTS = $$"""query ($financeTransactionId: String, $from: String, $to: String,
          $isUndated: Boolean, $isUnmatched: Boolean, $text: String, $limit: Int, $offset: Int) {
          FinanceReceipts(financeTransactionId: $financeTransactionId, from: $from, to: $to, isUndated: $isUndated,
            isUnmatched: $isUnmatched, text: $text, limit: $limit, offset: $offset) {
            financeReceipts { $${FINANCE_RECEIPT_FIELDS} }
            totalCount
          }
        }"""

    const val PROPOSE_RECEIPT_MATCHES = $$"""query ($receiptId: String!) {
          ProposeReceiptMatches(receiptId: $receiptId) {
            financeTransactionId matchedAmount isExactAmount isSameAccount isMerchantNameShared dayDistanceCount
            financeTransaction { $${FINANCE_TRANSACTION_FIELDS} }
          }
        }"""

    const val MATCH_RECEIPT = $$"""mutation ($receiptId: String!, $financeTransactionId: String!, $matchedAmount: String) {
          MatchReceipt(receiptId: $receiptId, financeTransactionId: $financeTransactionId, matchedAmount: $matchedAmount) { id }
        }"""

    const val UNMATCH_RECEIPT = $$"""mutation ($receiptId: String!, $financeTransactionId: String!) {
          UnmatchReceipt(receiptId: $receiptId, financeTransactionId: $financeTransactionId) { id }
        }"""

    const val DELETE_RECEIPT = $$"""mutation ($receiptId: String!) { DeleteReceipt(receiptId: $receiptId) }"""

    const val READ_RECEIPT = $$"""mutation ($agentAttachmentId: String, $mailboxItemId: String, $financeTransactionId: String) {
          ReadReceipt(agentAttachmentId: $agentAttachmentId, mailboxItemId: $mailboxItemId,
            financeTransactionId: $financeTransactionId) { agentJobId }
        }"""

    const val SPENDING_SUMMARY = $$"""query ($from: String, $to: String, $groupBy: String, $financeAccountId: String) {
          FinanceSpendingSummary(from: $from, to: $to, groupBy: $groupBy, financeAccountId: $financeAccountId) {
            groupBy
            spendingSummaryRows { groupKey groupLabel currencyCode moneyOut moneyIn financeTransactionCount }
            reportingCurrencyCode convertedMoneyOut convertedMoneyIn unconvertedCurrencyCodes
            convertedSpendingSummaryRows { groupKey groupLabel moneyOut moneyIn }
          }
        }"""

    const val BUDGET_STATUS = $$"""query ($month: String, $year: String) {
          BudgetStatus(month: $month, year: $year) { month year asOf
            spendingCategories { spendingCategoryId spendingCategoryName budgetAmount currencyCode budgetToDateAmount
              spendingAmount spendingBySameDayLastMonthAmount fixedChargesDueAmount projectedAmount budgetPace }
            incomeCategories { spendingCategoryId spendingCategoryName budgetAmount currencyCode
              incomeAmount expectedByTodayAmount projectedAmount incomePace } }
        }"""

    const val SAVING_SUMMARY = $$"""query ($month: String, $year: String) {
          SavingSummary(month: $month, year: $year) { reportingCurrencyCode incomeBudgetCount spendingBudgetCount
            expectedIncomeAmount expectedSpendingAmount expectedSavingAmount incomeAmount spendingAmount savingAmount
            projectedIncomeAmount projectedSpendingAmount projectedSavingAmount savingPace unconvertedCurrencyCodes }
        }"""

    const val CASH_FLOW = $$"""query ($fromMonth: String, $toMonth: String) {
          CashFlow(fromMonth: $fromMonth, toMonth: $toMonth) { reportingCurrencyCode unconvertedCurrencyCodes
            cashFlowMonths { cashFlowMonth incomeAmount spendingAmount netAmount } }
        }"""

    const val SPENDING_BY_DAY = $$"""query ($month: String, $compareMonth: String) {
          SpendingByDay(month: $month, compareMonth: $compareMonth) {
            month compareMonth reportingCurrencyCode unconvertedCurrencyCodes
            monthDays { spentOn spendingAmount cumulativeSpendingAmount }
            compareMonthDays { spentOn spendingAmount cumulativeSpendingAmount }
          }
        }"""

    const val SPENDING_CATEGORIES = $$"""query { SpendingCategories { id spendingCategoryName parentSpendingCategoryId isIncome isHidden isTransfer isOther } }"""

    const val CREATE_SPENDING_CATEGORY = $$"""mutation ($spendingCategoryName: String!, $parentSpendingCategoryId: String, $isIncome: Boolean, $isHidden: Boolean) {
          CreateSpendingCategory(spendingCategoryName: $spendingCategoryName, parentSpendingCategoryId: $parentSpendingCategoryId,
            isIncome: $isIncome, isHidden: $isHidden) { id }
        }"""

    const val UPDATE_SPENDING_CATEGORY = $$"""mutation ($spendingCategoryId: String!, $spendingCategoryName: String, $parentSpendingCategoryId: String,
          $isIncome: Boolean, $isHidden: Boolean) {
          UpdateSpendingCategory(spendingCategoryId: $spendingCategoryId, spendingCategoryName: $spendingCategoryName,
            parentSpendingCategoryId: $parentSpendingCategoryId, isIncome: $isIncome, isHidden: $isHidden) { id }
        }"""

    const val DELETE_SPENDING_CATEGORY = $$"""mutation ($spendingCategoryId: String!) { DeleteSpendingCategory(spendingCategoryId: $spendingCategoryId) }"""

    const val SPENDING_RULES = $$"""query { SpendingRules { id matchText financeAccountId minimumAmount maximumAmount spendingCategoryId rulePriority } }"""

    const val CREATE_SPENDING_RULE = $$"""mutation ($matchText: String!, $financeAccountId: String, $minimumAmount: String,
          $maximumAmount: String, $spendingCategoryId: String!, $rulePriority: Int) {
          CreateSpendingRule(matchText: $matchText, financeAccountId: $financeAccountId, minimumAmount: $minimumAmount,
            maximumAmount: $maximumAmount, spendingCategoryId: $spendingCategoryId, rulePriority: $rulePriority) { id }
        }"""

    const val UPDATE_SPENDING_RULE = $$"""mutation ($spendingRuleId: String!, $matchText: String, $financeAccountId: String,
          $minimumAmount: String, $maximumAmount: String, $spendingCategoryId: String, $rulePriority: Int) {
          UpdateSpendingRule(spendingRuleId: $spendingRuleId, matchText: $matchText, financeAccountId: $financeAccountId,
            minimumAmount: $minimumAmount, maximumAmount: $maximumAmount, spendingCategoryId: $spendingCategoryId,
            rulePriority: $rulePriority) { id }
        }"""

    const val DELETE_SPENDING_RULE = $$"""mutation ($spendingRuleId: String!) { DeleteSpendingRule(spendingRuleId: $spendingRuleId) }"""

    const val BUDGETS = $$"""query { Budgets { id spendingCategoryId monthlyAmount currencyCode effectiveFrom } }"""

    const val SET_BUDGET = $$"""mutation ($spendingCategoryId: String!, $monthlyAmount: String!, $currencyCode: String, $effectiveFrom: String) {
          SetBudget(spendingCategoryId: $spendingCategoryId, monthlyAmount: $monthlyAmount, currencyCode: $currencyCode,
            effectiveFrom: $effectiveFrom) { id }
        }"""

    const val NET_WORTH = $$"""query ($from: String, $to: String) {
          NetWorth(from: $from, to: $to) { reportingCurrencyCode unconvertedCurrencyCodes
            convertedNetWorthPoints { netWorthOn netWorthAmount } }
        }"""

    const val ASSETS = $$"""query { Assets { $${FINANCE_ASSET_FIELDS} latestValuation { $${FINANCE_VALUATION_FIELDS} } } }"""

    const val ASSET_HISTORY = $$"""query ($assetId: String!) {
          AssetHistory(assetId: $assetId) { assetValuations { $${FINANCE_VALUATION_FIELDS} } }
        }"""

    const val CREATE_ASSET = $$"""mutation ($assetName: String!, $assetKind: String!, $currencyCode: String!,
          $valuationSource: String, $estimateDescription: String, $isEstimateAllowed: Boolean, $value: String, $valuedOn: String) {
          CreateAsset(assetName: $assetName, assetKind: $assetKind, currencyCode: $currencyCode,
            valuationSource: $valuationSource, estimateDescription: $estimateDescription,
            isEstimateAllowed: $isEstimateAllowed, value: $value, valuedOn: $valuedOn) { id }
        }"""

    const val UPDATE_ASSET = $$"""mutation ($assetId: String!, $assetName: String, $assetKind: String, $currencyCode: String,
          $valuationSource: String, $estimateDescription: String, $isEstimateAllowed: Boolean) {
          UpdateAsset(assetId: $assetId, assetName: $assetName, assetKind: $assetKind, currencyCode: $currencyCode,
            valuationSource: $valuationSource, estimateDescription: $estimateDescription,
            isEstimateAllowed: $isEstimateAllowed) { id }
        }"""

    const val CLOSE_ASSET = $$"""mutation ($assetId: String!, $closedOn: String, $shouldReopen: Boolean) {
          CloseAsset(assetId: $assetId, closedOn: $closedOn, shouldReopen: $shouldReopen) { id }
        }"""

    const val DELETE_ASSET = $$"""mutation ($assetId: String!) { DeleteAsset(assetId: $assetId) }"""

    const val RECORD_VALUATION = $$"""mutation ($assetId: String!, $value: String!, $valuedOn: String, $valuationNote: String) {
          RecordValuation(assetId: $assetId, value: $value, valuedOn: $valuedOn, valuationNote: $valuationNote) { id }
        }"""

    const val DELETE_VALUATION = $$"""mutation ($valuationId: String!) { DeleteValuation(valuationId: $valuationId) }"""

    const val SAVINGS_TARGETS = $$"""query {
          SavingsTargets {
            savingsTarget { id savingsTargetName targetAmount currencyCode targetOn targetMeasure startingAmount startedOn
              closedOn assetIds financeAccountIds }
            savingsTargetProgress { savedAmount remainingAmount monthsLeftCount requiredMonthlyAmount isBehind
              unconvertedCurrencyCodes }
          }
        }"""

    const val CREATE_SAVINGS_TARGET = $$"""mutation ($savingsTargetName: String!, $targetAmount: String!, $currencyCode: String,
          $targetOn: String!, $targetMeasure: String, $startingAmount: String, $startedOn: String, $assetIds: [String!],
          $financeAccountIds: [String!]) {
          CreateSavingsTarget(savingsTargetName: $savingsTargetName, targetAmount: $targetAmount, currencyCode: $currencyCode,
            targetOn: $targetOn, targetMeasure: $targetMeasure, startingAmount: $startingAmount, startedOn: $startedOn,
            assetIds: $assetIds, financeAccountIds: $financeAccountIds) { savingsTarget { id } }
        }"""

    const val UPDATE_SAVINGS_TARGET = $$"""mutation ($savingsTargetId: String!, $savingsTargetName: String,
          $targetAmount: String, $targetOn: String, $targetMeasure: String, $startingAmount: String, $assetIds: [String!],
          $financeAccountIds: [String!]) {
          UpdateSavingsTarget(savingsTargetId: $savingsTargetId, savingsTargetName: $savingsTargetName,
            targetAmount: $targetAmount, targetOn: $targetOn, targetMeasure: $targetMeasure, startingAmount: $startingAmount,
            assetIds: $assetIds, financeAccountIds: $financeAccountIds) { savingsTarget { id } }
        }"""

    const val CLOSE_SAVINGS_TARGET = $$"""mutation ($savingsTargetId: String!, $shouldReopen: Boolean) {
          CloseSavingsTarget(savingsTargetId: $savingsTargetId, shouldReopen: $shouldReopen) { savingsTarget { id } }
        }"""

    const val REPORTING_CURRENCY = $$"""query { ReportingCurrency { reportingCurrencyCode isChosen } }"""

    const val SET_REPORTING_CURRENCY = $$"""mutation ($currencyCode: String!) { SetReportingCurrency(currencyCode: $currencyCode) }"""

    const val CONVERT_CURRENCY = $$"""query ($amount: String!, $fromCurrencyCode: String!, $toCurrencyCode: String!, $rateOn: String) {
          ConvertCurrency(amount: $amount, fromCurrencyCode: $fromCurrencyCode, toCurrencyCode: $toCurrencyCode, rateOn: $rateOn) {
            amount fromCurrencyCode convertedAmount toCurrencyCode rate rateOn rateSource }
        }"""

    const val SYNC_FINANCE_SOURCE = $$"""mutation ($sourceId: String!) { SyncAgentKnowledgeSource(sourceId: $sourceId) }"""

    const val DELETE_FINANCE_SOURCE = $$"""mutation ($sourceId: String!) { DeleteAgentKnowledgeSource(sourceId: $sourceId) }"""

    const val SWITCH_FINANCE_SOURCE = $$"""mutation ($sourceId: String, $enabled: Boolean) { SaveAgentKnowledgeSource(sourceId: $sourceId, enabled: $enabled) { id } }"""

    const val LINK_SIMPLE_FIN = $$"""mutation ($setupToken: String!) { LinkSimpleFIN(setupToken: $setupToken) { id } }"""

}
