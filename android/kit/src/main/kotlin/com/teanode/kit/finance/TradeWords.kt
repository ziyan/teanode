package com.teanode.kit.finance

/** A trade's kind, for the words it is shown in. */
enum class TradeKindWords { Buy, Sell, Canceled, Transfer }

/** TradeWords is what the trades list says of a trade, in the parts the screen puts words to. */
object TradeWords {
    /** The kind in words; a kind the app does not know is shown as sent. */
    fun kind(tradeKind: String?): TradeKindWords? = when (tradeKind) {
        "buy" -> TradeKindWords.Buy
        "sell" -> TradeKindWords.Sell
        "cancel" -> TradeKindWords.Canceled
        "transfer" -> TradeKindWords.Transfer
        else -> null
    }

    /** "2 at $100.00" is the quantity and the unit price; the quantity alone when the price is not known; nothing without a quantity. */
    data class Quantity(val quantityText: String, val unitPriceAmount: String?)

    fun quantity(trade: FinanceTrade): Quantity? {
        val quantityText = trade.tradedQuantity?.takeIf { text -> text.isNotEmpty() } ?: return null
        return Quantity(quantityText, trade.unitPrice?.takeIf { text -> text.isNotEmpty() })
    }

    /** Whether a fee is shown: only one that is not zero. */
    fun hasFee(trade: FinanceTrade): Boolean = (FinanceMoney.decimal(trade.feeAmount)?.signum() ?: 0) != 0

    /** Whether cash came into the account, which the amount is shown in green for. */
    fun isCashIn(trade: FinanceTrade): Boolean = (FinanceMoney.decimal(trade.tradeAmount)?.signum() ?: 0) > 0

    /** The trades still to read after those held, from one page's total: nothing once all are held. */
    fun hasMore(heldCount: Int, totalCount: Int): Boolean = heldCount < totalCount
}
