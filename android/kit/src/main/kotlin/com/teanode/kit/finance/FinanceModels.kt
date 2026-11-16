package com.teanode.kit.finance

import kotlinx.serialization.Serializable

@Serializable
data class FinanceAccount(
    val id: String,
    val sourceId: String? = null,
    val providerKind: String? = null,
    val institutionName: String? = null,
    val accountName: String? = null,
    val accountMask: String? = null,
    val accountKind: String? = null,
    val currencyCode: String? = null,
    val currentBalance: String? = null,
    val availableBalance: String? = null,
    val balanceAt: String? = null,
    val isSignInRequired: Boolean? = null,
    val reportingCurrencyCode: String? = null,
    val convertedCurrentBalance: String? = null,
)

@Serializable
data class FinanceSource(
    val id: String,
    val name: String? = null,
    val providerKind: String? = null,
    val institutionName: String? = null,
    val isEnabled: Boolean? = null,
    val lastRunAt: String? = null,
    val nextRunAt: String? = null,
    val lastError: String? = null,
    val isSignInRequired: Boolean? = null,
    val financeAccounts: List<FinanceAccount?>? = null,
)

@Serializable
data class FinanceProvider(
    val providerKind: String,
    val isBrowserRequired: Boolean? = null,
)

@Serializable
data class FinanceTransaction(
    val id: String,
    val financeAccountId: String? = null,
    val postedOn: String? = null,
    val transactedAt: String? = null,
    val amount: String? = null,
    val currencyCode: String? = null,
    val description: String? = null,
    val merchantName: String? = null,
    val providerCategoryPrimary: String? = null,
    val providerCategoryDetailed: String? = null,
    val isPending: Boolean? = null,
    val providerTransactionId: String? = null,
    val pendingProviderTransactionId: String? = null,
    val spendingCategoryId: String? = null,
    val categorizedBy: String? = null,
    val categorizationConfidence: String? = null,
    val duplicateOfTransactionId: String? = null,
    val duplicateDecidedBy: String? = null,
    val annotation: String? = null,
    val annotatedBy: String? = null,
    val receiptCount: Int? = null,
    val createdAt: String? = null,
    val modifiedAt: String? = null,
)

@Serializable
data class FinanceTransactionPage(
    val financeTransactions: List<FinanceTransaction?>,
    val totalCount: Int? = null,
    val leftOutDuplicateCount: Int? = null,
)

@Serializable
data class SpendingCategory(
    val id: String,
    val spendingCategoryName: String,
    val parentSpendingCategoryId: String? = null,
    val isIncome: Boolean? = null,
    val isHidden: Boolean? = null,
    val isTransfer: Boolean? = null,
    val isOther: Boolean? = null,
)

@Serializable
data class CreditUsage(
    val reportingCurrencyCode: String? = null,
    val unconvertedCurrencyCodes: List<String>? = null,
    val totalOwedAmount: String? = null,
    val totalCreditLimitAmount: String? = null,
    val usageShare: Double? = null,
    val leftOutCardCount: Int? = null,
    val leftOutOwedAmount: String? = null,
    val creditCards: List<Card?>? = null,
) {
    @Serializable
    data class Card(
        val financeAccountId: String,
        val accountName: String? = null,
        val accountMask: String? = null,
        val institutionName: String? = null,
        val currencyCode: String? = null,
        val owedAmount: String? = null,
        val creditLimitAmount: String? = null,
        val creditLimitSource: String? = null,
        val usageShare: Double? = null,
    )
}

@Serializable
data class FinanceReceipt(
    val id: String,
    val receiptSourceKind: String? = null,
    val agentAttachmentId: String? = null,
    val mailboxItemId: String? = null,
    val merchantName: String? = null,
    val merchantReceiptNumber: String? = null,
    val purchasedOn: String? = null,
    val currencyCode: String? = null,
    val subtotalAmount: String? = null,
    val totalAmount: String? = null,
    val receiptCheckState: String? = null,
    val checkDifferenceAmount: String? = null,
    val receiptLines: List<Line?>? = null,
    val receiptMatches: List<Match?>? = null,
) {
    @Serializable
    data class Line(
        val id: String,
        val lineNumber: Int? = null,
        val receiptLineKind: String? = null,
        val description: String? = null,
        val quantity: String? = null,
        val quantityUnit: String? = null,
        val unitPriceAmount: String? = null,
        val lineAmount: String? = null,
    )
    @Serializable
    data class Match(
        val receiptId: String,
        val financeTransactionId: String,
        val matchedAmount: String? = null,
        val receiptMatchSource: String? = null,
    )
}

@Serializable
data class FinanceReceiptPage(
    val financeReceipts: List<FinanceReceipt?>,
    val totalCount: Int? = null,
)

@Serializable
data class ReceiptMatchProposal(
    val financeTransactionId: String,
    val matchedAmount: String? = null,
    val isExactAmount: Boolean? = null,
    val isSameAccount: Boolean? = null,
    val isMerchantNameShared: Boolean? = null,
    val dayDistanceCount: Int? = null,
    val financeTransaction: FinanceTransaction? = null,
)

@Serializable
data class SpendingSummary(
    val groupBy: String? = null,
    val spendingSummaryRows: List<Row?>? = null,
    val reportingCurrencyCode: String? = null,
    val convertedMoneyOut: String? = null,
    val convertedMoneyIn: String? = null,
    val unconvertedCurrencyCodes: List<String>? = null,
    val convertedSpendingSummaryRows: List<ConvertedRow?>? = null,
) {
    @Serializable
    data class Row(
        val groupKey: String,
        val groupLabel: String? = null,
        val currencyCode: String? = null,
        val moneyOut: String? = null,
        val moneyIn: String? = null,
        val financeTransactionCount: Int? = null,
    )
    @Serializable
    data class ConvertedRow(
        val groupKey: String,
        val groupLabel: String? = null,
        val moneyOut: String? = null,
        val moneyIn: String? = null,
    )
}

@Serializable
data class BudgetStatus(
    val month: String? = null,
    val year: String? = null,
    val asOf: String? = null,
    val spendingCategories: List<Spending?>? = null,
    val incomeCategories: List<Income?>? = null,
) {
    @Serializable
    data class Spending(
        val spendingCategoryId: String,
        val spendingCategoryName: String? = null,
        val budgetAmount: String? = null,
        val currencyCode: String? = null,
        val budgetToDateAmount: String? = null,
        val spendingAmount: String? = null,
        val spendingBySameDayLastMonthAmount: String? = null,
        val fixedChargesDueAmount: String? = null,
        val projectedAmount: String? = null,
        val budgetPace: String? = null,
    )
    @Serializable
    data class Income(
        val spendingCategoryId: String,
        val spendingCategoryName: String? = null,
        val budgetAmount: String? = null,
        val currencyCode: String? = null,
        val incomeAmount: String? = null,
        val expectedByTodayAmount: String? = null,
        val projectedAmount: String? = null,
        val incomePace: String? = null,
    )
}

@Serializable
data class SavingSummary(
    val reportingCurrencyCode: String? = null,
    val expectedIncomeAmount: String? = null,
    val expectedSpendingAmount: String? = null,
    val expectedSavingAmount: String? = null,
    val incomeAmount: String? = null,
    val spendingAmount: String? = null,
    val savingAmount: String? = null,
    val projectedIncomeAmount: String? = null,
    val projectedSpendingAmount: String? = null,
    val projectedSavingAmount: String? = null,
    val savingPace: String? = null,
    val incomeBudgetCount: Int? = null,
    val spendingBudgetCount: Int? = null,
    val unconvertedCurrencyCodes: List<String>? = null,
)

@Serializable
data class CashFlow(
    val reportingCurrencyCode: String? = null,
    val unconvertedCurrencyCodes: List<String>? = null,
    val cashFlowMonths: List<Month?>? = null,
) {
    @Serializable
    data class Month(
        val cashFlowMonth: String,
        val incomeAmount: String? = null,
        val spendingAmount: String? = null,
        val netAmount: String? = null,
    )
}

/**
 * SpendingByDay is a month's spending day by day, each day with the
 * running total to it, beside the month it is compared with, in the
 * reporting currency.
 */
@Serializable
data class SpendingByDay(
    val month: String? = null,
    val compareMonth: String? = null,
    val reportingCurrencyCode: String? = null,
    val unconvertedCurrencyCodes: List<String>? = null,
    val monthDays: List<Day?>? = null,
    val compareMonthDays: List<Day?>? = null,
) {
    @Serializable
    data class Day(
        val spentOn: String,
        val spendingAmount: String? = null,
        val cumulativeSpendingAmount: String? = null,
    )
}

@Serializable
data class SpendingRule(
    val id: String,
    val matchText: String,
    val financeAccountId: String? = null,
    val minimumAmount: String? = null,
    val maximumAmount: String? = null,
    val spendingCategoryId: String? = null,
    val rulePriority: Int? = null,
)

@Serializable
data class Budget(
    val id: String,
    val spendingCategoryId: String,
    val monthlyAmount: String,
    val currencyCode: String? = null,
    val effectiveFrom: String,
)

@Serializable
data class NetWorth(
    val reportingCurrencyCode: String? = null,
    val unconvertedCurrencyCodes: List<String>? = null,
    val convertedNetWorthPoints: List<Point?>? = null,
) {
    @Serializable
    data class Point(
        val netWorthOn: String,
        val netWorthAmount: String? = null,
    )
}

@Serializable
data class FinanceAsset(
    val id: String,
    val assetName: String,
    val assetKind: String,
    val isLiability: Boolean? = null,
    val currencyCode: String? = null,
    val financeAccountId: String? = null,
    val financeSecurity: Security? = null,
    val valuationSource: String? = null,
    val estimateDescription: String? = null,
    val isEstimateAllowed: Boolean? = null,
    val closedOn: String? = null,
    val latestValuation: Valuation? = null,
) {
    @Serializable
    data class Security(
        val tickerSymbol: String? = null,
        val securityName: String? = null,
        val closePrice: String? = null,
        val closePriceOn: String? = null,
    )
    @Serializable
    data class Valuation(
        val id: String,
        val valuedOn: String? = null,
        val value: String? = null,
        val currencyCode: String? = null,
        val valuationSource: String? = null,
        val estimateLow: String? = null,
        val estimateHigh: String? = null,
        val valuationNote: String? = null,
        val heldQuantity: String? = null,
        val unitPrice: String? = null,
        val costBasis: String? = null,
    )
}

@Serializable
data class SavingsTarget(
    val savingsTarget: Target? = null,
    val savingsTargetProgress: Progress? = null,
) {
    @Serializable
    data class Target(
        val id: String,
        val savingsTargetName: String,
        val targetAmount: String,
        val currencyCode: String? = null,
        val targetOn: String? = null,
        val targetMeasure: String? = null,
        val startingAmount: String? = null,
        val startedOn: String? = null,
        val closedOn: String? = null,
        val assetIds: List<String>? = null,
        val financeAccountIds: List<String>? = null,
    )
    @Serializable
    data class Progress(
        val savedAmount: String? = null,
        val remainingAmount: String? = null,
        val monthsLeftCount: Int? = null,
        val requiredMonthlyAmount: String? = null,
        val isBehind: Boolean? = null,
        val unconvertedCurrencyCodes: List<String>? = null,
    )
}

/** FinanceConversion is an amount in another currency, and the rate used. */
@Serializable
data class FinanceConversion(
    val convertedAmount: String? = null,
    val rate: String? = null,
    val rateOn: String? = null,
    val rateSource: String? = null,
)
