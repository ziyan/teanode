package com.teanode.kit.finance

import kotlinx.serialization.Serializable

/** ReceiptEntryCandidate is a charge a typed-in receipt could explain. */
@Serializable
data class ReceiptEntryCandidate(
    val financeTransactionId: String,
    val financeTransaction: FinanceTransaction? = null,
    val matchedAmount: String? = null,
    val isExactAmount: Boolean? = null,
    val isSameAccount: Boolean? = null,
    val isMerchantNameShared: Boolean? = null,
    val dayDistanceCount: Int? = null,
    val isAutomatic: Boolean? = null,
    val matchConfidence: String? = null,
)

/** ReceiptEntryPreview is what recording a receipt would do, which writes nothing. */
@Serializable
data class ReceiptEntryPreview(
    val receiptCheckState: String? = null,
    val checkDifferenceAmount: String? = null,
    val receiptCheckSummary: String? = null,
    val receiptMatchCandidates: List<ReceiptEntryCandidate?>? = null,
    val isReplacing: Boolean? = null,
)

/** RecordedReceipt is what recording a receipt did. */
@Serializable
data class RecordedReceipt(
    val financeReceipt: FinanceReceipt? = null,
    val receiptCheckSummary: String? = null,
    val receiptMatchCandidates: List<ReceiptEntryCandidate?>? = null,
    val isReplaced: Boolean? = null,
    val droppedReceiptMatchReasons: List<String>? = null,
)

/** CurrencyRate is what one unit of a currency bought in another, on a day. */
@Serializable
data class CurrencyRate(
    val fromCurrencyCode: String,
    val toCurrencyCode: String,
    val rate: String,
    val rateOn: String? = null,
    val rateSource: String? = null,
)
