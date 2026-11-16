package com.teanode.kit.finance

import kotlinx.serialization.Serializable

/**
 * FinanceTrade is one buy, sell, cancel or transfer of a security in an
 * account. Its amount is the cash the trade moved: negative when cash
 * left the account.
 */
@Serializable
data class FinanceTrade(
    val id: String,
    val financeAccountId: String? = null,
    val financeSecurityId: String? = null,
    val financeSecurity: Security? = null,
    val providerTradeId: String? = null,
    val tradedOn: String? = null,
    val tradeKind: String? = null,
    val tradeSubkind: String? = null,
    val tradedQuantity: String? = null,
    val unitPrice: String? = null,
    val tradeAmount: String? = null,
    val feeAmount: String? = null,
    val currencyCode: String? = null,
    val description: String? = null,
) {
    @Serializable
    data class Security(
        val tickerSymbol: String? = null,
        val securityName: String? = null,
    )
}

/**
 * FinanceTradePage is one page of trades, newest first, and how many
 * there are in all.
 */
@Serializable
data class FinanceTradePage(
    val financeTrades: List<FinanceTrade?>,
    val totalCount: Int? = null,
)

/**
 * StatementAccountDeleted is what deleting an account of imported
 * statements removed.
 */
@Serializable
data class StatementAccountDeleted(
    val financeAccountId: String? = null,
    val deletedTransactionCount: Int? = null,
    val deletedAssetCount: Int? = null,
)

/**
 * SpendingRuleProposals is the spending rules to offer for a set of
 * transactions filed under one category, and how many words were left out
 * and why.
 */
@Serializable
data class SpendingRuleProposals(
    val spendingRuleProposals: List<Proposal?>,
    val tooGenericMatchTextCount: Int? = null,
    val changingNumberMatchTextCount: Int? = null,
    val overLimitMatchTextCount: Int? = null,
) {
    @Serializable
    data class Proposal(
        val matchText: String,
        val spendingCategoryId: String? = null,
        val financeTransactionCount: Int? = null,
        val changedTransactionCount: Int? = null,
        val aheadOfSpendingRule: AheadOf? = null,
    ) {
        @Serializable
        data class AheadOf(
            val id: String? = null,
            val matchText: String? = null,
            val spendingCategoryId: String? = null,
        )
    }
}

/**
 * TransactionRowsPreview is what importing transaction rows would do,
 * written nowhere: the account, the rows it would add, the rows it holds
 * already, and what was checked.
 */
@Serializable
data class TransactionRowsPreview(
    val financeAccountId: String? = null,
    val accountName: String? = null,
    val isNewAccount: Boolean? = null,
    val accountMatch: String? = null,
    val currencyCode: String? = null,
    val newTransactionRows: List<Row?>? = null,
    val presentTransactionRows: List<Row?>? = null,
    val firstPostedOn: String? = null,
    val lastPostedOn: String? = null,
    val moneyInAmount: String? = null,
    val moneyOutAmount: String? = null,
    val verificationSummary: String? = null,
) {
    @Serializable
    data class Row(
        val rowNumber: Int? = null,
        val postedOn: String? = null,
        val description: String? = null,
        val amount: String? = null,
        val hasNearbyStoredTransaction: Boolean? = null,
    )
}
