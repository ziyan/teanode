package com.teanode.kit.finance

import com.teanode.kit.TeaNodeJson
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Test
import java.math.BigDecimal

class SpendingLinesTest {
    private fun summary(json: String): SpendingSummary = TeaNodeJson.decodeFromString(SpendingSummary.serializer(), json)

    private val categories = listOf(
        SpendingCategory("groceries", "groceries"), SpendingCategory("pay", "income", isIncome = true), SpendingCategory("other", "other", isOther = true),
    )

    @Test
    fun incomeIsLeftOutAndOtherCountsWhatWentOutAlone() {
        val read = summary(
            """{"groupBy":"spendingCategory","spendingSummaryRows":[
                {"groupKey":"groceries","groupLabel":"Groceries","currencyCode":"USD","moneyOut":"120.00","moneyIn":"20.00","financeTransactionCount":5},
                {"groupKey":"pay","groupLabel":"Pay","currencyCode":"USD","moneyOut":"0","moneyIn":"3000","financeTransactionCount":1},
                {"groupKey":"other","groupLabel":"Other","currencyCode":"USD","moneyOut":"30.00","moneyIn":"10.00","financeTransactionCount":2},
                {"groupKey":"","groupLabel":"","currencyCode":"USD","moneyOut":"7.00","moneyIn":"0","financeTransactionCount":1}]}""",
        )
        val lines = SpendingLines.of(read, "spendingCategory", categories, { id -> id.orEmpty() }, "Unknown")
        assertEquals(listOf("groceries", "other", ""), lines.map { line -> line.id })
        assertEquals(0, BigDecimal("100.00").compareTo(lines[0].amount))
        assertEquals(0, BigDecimal("30.00").compareTo(lines[1].amount))
        assertEquals(5, lines[0].count)
    }

    @Test
    fun merchantsAreOnlyThoseWithMoneyLeftOut() {
        val read = summary(
            """{"groupBy":"merchant","spendingSummaryRows":[
                {"groupKey":"a","groupLabel":"Corner Market","moneyOut":"10","moneyIn":"0","financeTransactionCount":2},
                {"groupKey":"b","groupLabel":"Refunds","moneyOut":"1","moneyIn":"5","financeTransactionCount":1},
                {"groupKey":"c","groupLabel":"","moneyOut":"4","moneyIn":"0","financeTransactionCount":1}]}""",
        )
        val lines = SpendingLines.of(read, "merchant", emptyList(), { id -> id.orEmpty() }, "Unknown")
        assertEquals(listOf("Corner Market", "Unknown"), lines.map { line -> line.label })
    }

    @Test
    fun aConvertedSummaryAddsTheTransactionCountsOfItsCurrencies() {
        val read = summary(
            """{"groupBy":"spendingCategory","reportingCurrencyCode":"USD",
                "spendingSummaryRows":[{"groupKey":"groceries","moneyOut":"10","moneyIn":"0","currencyCode":"USD","financeTransactionCount":2},{"groupKey":"groceries","moneyOut":"9","moneyIn":"0","currencyCode":"EUR","financeTransactionCount":3}],
                "convertedSpendingSummaryRows":[{"groupKey":"groceries","groupLabel":"Groceries","moneyOut":"20.00","moneyIn":"0"}]}""",
        )
        val lines = SpendingLines.of(read, "spendingCategory", categories, { id -> id.orEmpty() }, "Unknown")
        assertEquals(5, lines.single().count)
        assertNull(SpendingLines.of(null, "merchant", emptyList(), { id -> id.orEmpty() }, "Unknown").firstOrNull())
    }
}
