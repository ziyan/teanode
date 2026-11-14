package com.teanode.kit.finance

import com.teanode.kit.TeaNodeJson
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Test
import java.math.BigDecimal

class SharedSpendingSummaryTest {
    private fun row(month: String, category: String, out: String, moneyIn: String = "0", currency: String = "USD", count: Int = 1): SharedFinanceSpendingRow =
        TeaNodeJson.decodeFromString(
            SharedFinanceSpendingRow.serializer(),
            """{"spendingMonth":"$month","spendingCategoryName":"$category","currencyCode":"$currency","moneyOut":"$out","moneyIn":"$moneyIn","financeTransactionCount":$count}""",
        )

    private val rows = listOf(
        row("2026-09", "Groceries", "412.30", count = 14),
        row("2026-09", "Dining", "128.50", count = 6),
        row("2026-09", "Pay", "0", "3200", count = 2),
        row("2026-09", "", "20", count = 1),
        row("2026-08", "Groceries", "388.90", count = 12),
        row("2026-08", "Dining", "50", "10", count = 2),
        row("2025-12", "Groceries", "100", count = 3),
        row("2026-09", "Groceries", "90", currency = "EUR", count = 2),
    )

    private fun amount(text: String) = BigDecimal(text)

    @Test
    fun theCurrencyWithTheMostMoneyOutComesFirst() {
        assertEquals(listOf("USD", "EUR"), SharedSpendingSummary.currencyCodes(rows))
        assertEquals(emptyList<String>(), SharedSpendingSummary.currencyCodes(emptyList()))
    }

    @Test
    fun monthsAreAddedUpPerCurrencyEarliestFirst() {
        val months = SharedSpendingSummary.months(rows, "USD")
        assertEquals(listOf("2025-12", "2026-08", "2026-09"), months.map { month -> month.spendingMonth })
        assertEquals(0, amount("560.8").compareTo(months[2].moneyOut))
        assertEquals(0, amount("3200").compareTo(months[2].moneyIn))
        assertEquals(1, SharedSpendingSummary.months(rows, "EUR").size)
    }

    @Test
    fun aPeriodIsAMonthOrAYear() {
        val months = SharedSpendingSummary.months(rows, "USD")
        assertEquals(0, amount("560.8").compareTo(SharedSpendingSummary.total(months, "2026-09").moneyOut))
        val year = SharedSpendingSummary.total(months, "2026")
        assertEquals(0, (amount("560.8") + amount("438.9")).compareTo(year.moneyOut))
        assertEquals(0, amount("3210").compareTo(year.moneyIn))
        assertEquals(0, BigDecimal.ZERO.compareTo(SharedSpendingSummary.total(months, "2024").moneyOut))
    }

    @Test
    fun whereItWentCountsNetSpendingAndLeavesOutIncome() {
        val lines = SharedSpendingSummary.lines(rows, "USD", "2026-09")
        assertEquals(listOf("Groceries", "Dining", ""), lines.map { line -> line.spendingCategoryName })
        assertEquals(0, amount("412.3").compareTo(lines[0].spentAmount))
        assertEquals(14, lines[0].financeTransactionCount)
        assertFalse(lines.any { line -> line.spendingCategoryName == "Pay" })
        val august = SharedSpendingSummary.lines(rows, "USD", "2026-08")
        assertEquals(0, amount("40").compareTo(august.first { line -> line.spendingCategoryName == "Dining" }.spentAmount))
    }

    @Test
    fun aYearAddsItsMonthsOfACategoryTogether() {
        val lines = SharedSpendingSummary.lines(rows, "USD", "2026")
        assertEquals("Groceries", lines.first().spendingCategoryName)
        assertEquals(0, amount("801.2").compareTo(lines.first().spentAmount))
        assertEquals(26, lines.first().financeTransactionCount)
    }

    @Test
    fun moneyWithNoCategoryCountsWhatWentOutAlone() {
        val uncategorized = listOf(row("2026-09", "", "30", "5"))
        assertEquals(0, amount("30").compareTo(SharedSpendingSummary.lines(uncategorized, "USD", "2026-09").first().spentAmount))
    }

    @Test
    fun theLatestMonthAndFirstYear() {
        assertEquals("2026-09", SharedSpendingSummary.latestMonth(rows))
        assertEquals(2025, SharedSpendingSummary.firstYear(rows))
        assertNull(SharedSpendingSummary.latestMonth(emptyList()))
        assertNull(SharedSpendingSummary.firstYear(emptyList()))
    }
}
