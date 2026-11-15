package com.teanode.kit.finance

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class TradeWordsTest {
    @Test
    fun theKindsAreNamedAndAnUnknownOneIsLeftAsSent() {
        assertEquals(TradeKindWords.Buy, TradeWords.kind("buy"))
        assertEquals(TradeKindWords.Sell, TradeWords.kind("sell"))
        assertEquals(TradeKindWords.Canceled, TradeWords.kind("cancel"))
        assertEquals(TradeKindWords.Transfer, TradeWords.kind("transfer"))
        assertNull(TradeWords.kind("split"))
        assertNull(TradeWords.kind(null))
    }

    @Test
    fun theQuantityLineNeedsAQuantityAndTakesAPriceWhenThereIsOne() {
        assertEquals(TradeWords.Quantity("2", "100.00"), TradeWords.quantity(FinanceTrade(id = "t1", tradedQuantity = "2", unitPrice = "100.00")))
        assertEquals(TradeWords.Quantity("2", null), TradeWords.quantity(FinanceTrade(id = "t1", tradedQuantity = "2", unitPrice = "")))
        assertNull(TradeWords.quantity(FinanceTrade(id = "t1", unitPrice = "100.00")))
        assertNull(TradeWords.quantity(FinanceTrade(id = "t1", tradedQuantity = "")))
    }

    @Test
    fun aFeeAtZeroIsNotShownAndCashInIsPositive() {
        assertFalse(TradeWords.hasFee(FinanceTrade(id = "t1", feeAmount = "0.00")))
        assertFalse(TradeWords.hasFee(FinanceTrade(id = "t1")))
        assertTrue(TradeWords.hasFee(FinanceTrade(id = "t1", feeAmount = "1.50")))
        assertTrue(TradeWords.isCashIn(FinanceTrade(id = "t1", tradeAmount = "250.00")))
        assertFalse(TradeWords.isCashIn(FinanceTrade(id = "t1", tradeAmount = "-250.00")))
    }

    @Test
    fun moreAreReadUntilAllAreHeld() {
        assertTrue(TradeWords.hasMore(50, 120))
        assertFalse(TradeWords.hasMore(120, 120))
    }
}
