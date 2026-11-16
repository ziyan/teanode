package com.teanode.kit.finance

import java.math.BigDecimal

/** One row of "Where it went". */
data class SpendingLine(val id: String, val label: String, val amount: BigDecimal, val count: Int)

/** The rows of a spending summary as the dashboard counts them. */
object SpendingLines {
    /**
     * The rows as the dashboard counts them: income left out; Other and uncategorized by money out alone, the rest by
     * money out less money in; nothing left at zero, and merchants and accounts only above it.
     *
     * @param groupBy "spendingCategory", "merchant" or "financeAccount".
     * @param categoryName how a category's id is shown.
     */
    fun of(
        summary: SpendingSummary?,
        groupBy: String,
        categories: List<SpendingCategory>,
        categoryName: (String?) -> String,
        unknownLabel: String,
    ): List<SpendingLine> {
        if (summary == null) return emptyList()
        class Raw(val key: String, val label: String, val moneyOut: BigDecimal, val moneyIn: BigDecimal, val count: Int)
        val rows = summary.spendingSummaryRows.orEmpty().filterNotNull()
        val isConverted = !summary.reportingCurrencyCode.isNullOrEmpty()
        val raws = if (isConverted) {
            summary.convertedSpendingSummaryRows.orEmpty().filterNotNull().map { row ->
                val count = rows.filter { each -> each.groupKey == row.groupKey }.sumOf { each -> each.financeTransactionCount ?: 0 }
                Raw(row.groupKey, row.groupLabel ?: "", FinanceMoney.decimal(row.moneyOut) ?: BigDecimal.ZERO, FinanceMoney.decimal(row.moneyIn) ?: BigDecimal.ZERO, count)
            }
        } else {
            rows.map { row ->
                Raw(row.groupKey, row.groupLabel ?: "", FinanceMoney.decimal(row.moneyOut) ?: BigDecimal.ZERO, FinanceMoney.decimal(row.moneyIn) ?: BigDecimal.ZERO, row.financeTransactionCount ?: 0)
            }
        }
        return raws.mapNotNull { raw ->
            val category = if (groupBy == "spendingCategory") categories.firstOrNull { each -> each.id == raw.key } else null
            if (category?.isIncome == true) return@mapNotNull null
            val isOutOnly = groupBy == "spendingCategory" && (raw.key.isEmpty() || category?.isOther == true)
            val amount = if (isOutOnly) raw.moneyOut else raw.moneyOut - raw.moneyIn
            if (amount.signum() == 0 || (groupBy != "spendingCategory" && amount.signum() <= 0)) return@mapNotNull null
            val label = if (groupBy == "spendingCategory") categoryName(raw.key) else raw.label.ifEmpty { unknownLabel }
            SpendingLine(raw.key, label, amount, raw.count)
        }.sortedByDescending { line -> line.amount }
    }
}
