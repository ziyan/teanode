package com.teanode.kit.finance

import java.math.BigDecimal

// The combined spending of somebody who shares all of their finance: the server answers with one row per month,
// spending category and currency, and the screen shows a month or a year of it the way the person's own Spending
// does. The sums are made here so they can be tested.

/** One spending category over a month or a year: what went out of it, less what came back in. */
data class SharedSpendingLine(
    /** Empty for money with no category yet. */
    val spendingCategoryName: String,
    val spentAmount: BigDecimal,
    val financeTransactionCount: Int,
) {
    val id: String get() = spendingCategoryName
}

/** A month's money out and money in, added up over every spending category. */
data class SharedSpendingMonth(
    /** "2026-09". */
    val spendingMonth: String,
    val moneyOut: BigDecimal,
    val moneyIn: BigDecimal,
) {
    val id: String get() = spendingMonth
}

/** Adds up the rows of a person's combined spending. */
object SharedSpendingSummary {
    /** The currencies the rows are in, the one with the most money out first. */
    fun currencyCodes(rows: List<SharedFinanceSpendingRow>): List<String> {
        val outByCurrency = mutableMapOf<String, BigDecimal>()
        for (row in rows) {
            outByCurrency[row.currencyCode] = (outByCurrency[row.currencyCode] ?: BigDecimal.ZERO) + (FinanceMoney.decimal(row.moneyOut) ?: BigDecimal.ZERO)
        }
        return outByCurrency.keys.sortedWith { left, right ->
            val leftOut = outByCurrency[left] ?: BigDecimal.ZERO
            val rightOut = outByCurrency[right] ?: BigDecimal.ZERO
            if (leftOut.compareTo(rightOut) == 0) left.compareTo(right) else rightOut.compareTo(leftOut)
        }
    }

    /** Each month that has rows in the currency, earliest first. */
    fun months(rows: List<SharedFinanceSpendingRow>, currencyCode: String): List<SharedSpendingMonth> {
        val outByMonth = mutableMapOf<String, BigDecimal>()
        val inByMonth = mutableMapOf<String, BigDecimal>()
        for (row in rows.filter { each -> each.currencyCode == currencyCode }) {
            outByMonth[row.spendingMonth] = (outByMonth[row.spendingMonth] ?: BigDecimal.ZERO) + (FinanceMoney.decimal(row.moneyOut) ?: BigDecimal.ZERO)
            inByMonth[row.spendingMonth] = (inByMonth[row.spendingMonth] ?: BigDecimal.ZERO) + (FinanceMoney.decimal(row.moneyIn) ?: BigDecimal.ZERO)
        }
        return (outByMonth.keys + inByMonth.keys).toSortedSet().map { monthText ->
            SharedSpendingMonth(monthText, outByMonth[monthText] ?: BigDecimal.ZERO, inByMonth[monthText] ?: BigDecimal.ZERO)
        }
    }

    /** The months of a period added up: "2026-09" for a month, "2026" for a year. */
    fun total(months: List<SharedSpendingMonth>, period: String): SharedSpendingMonth {
        val inPeriod = months.filter { month -> month.spendingMonth.startsWith(period) }
        return SharedSpendingMonth(
            period,
            inPeriod.fold(BigDecimal.ZERO) { sum, month -> sum + month.moneyOut },
            inPeriod.fold(BigDecimal.ZERO) { sum, month -> sum + month.moneyIn },
        )
    }

    /**
     * Where the money of a period went, largest first. A category counts what went out of it less what came back in,
     * as the person's own Spending counts it; money with no category yet counts what went out alone. A category that
     * took in more than it spent is left out, which leaves out income.
     */
    fun lines(rows: List<SharedFinanceSpendingRow>, currencyCode: String, period: String): List<SharedSpendingLine> {
        val outByCategory = mutableMapOf<String, BigDecimal>()
        val inByCategory = mutableMapOf<String, BigDecimal>()
        val countByCategory = mutableMapOf<String, Int>()
        for (row in rows.filter { each -> each.currencyCode == currencyCode && each.spendingMonth.startsWith(period) }) {
            val name = row.spendingCategoryName
            outByCategory[name] = (outByCategory[name] ?: BigDecimal.ZERO) + (FinanceMoney.decimal(row.moneyOut) ?: BigDecimal.ZERO)
            inByCategory[name] = (inByCategory[name] ?: BigDecimal.ZERO) + (FinanceMoney.decimal(row.moneyIn) ?: BigDecimal.ZERO)
            countByCategory[name] = (countByCategory[name] ?: 0) + row.financeTransactionCount
        }
        val lines = mutableListOf<SharedSpendingLine>()
        for ((categoryName, moneyOut) in outByCategory) {
            val moneyIn = inByCategory[categoryName] ?: BigDecimal.ZERO
            val spentAmount = if (categoryName.isEmpty()) moneyOut else moneyOut - moneyIn
            if (spentAmount.signum() > 0) lines.add(SharedSpendingLine(categoryName, spentAmount, countByCategory[categoryName] ?: 0))
        }
        return lines.sortedWith { left, right ->
            if (left.spentAmount.compareTo(right.spentAmount) == 0) left.spendingCategoryName.compareTo(right.spendingCategoryName)
            else right.spentAmount.compareTo(left.spentAmount)
        }
    }

    /** The latest month that has rows, "2026-09". */
    fun latestMonth(rows: List<SharedFinanceSpendingRow>): String? = rows.maxOfOrNull { row -> row.spendingMonth }

    /** The first year that has rows. */
    fun firstYear(rows: List<SharedFinanceSpendingRow>): Int? = rows.mapNotNull { row -> row.spendingMonth.take(4).toIntOrNull() }.minOrNull()
}
