package com.teanode.kit.finance

import java.math.BigDecimal
import java.text.NumberFormat
import java.util.Currency
import java.util.Locale

// Money is a decimal string, negative for money out, as the server keeps it:
// never a floating-point number, so nothing is rounded on the way.

/** FinanceMoney writes and reads the server's decimal strings. */
object FinanceMoney {
    private val decimalPattern = Regex("""^-?\d+(\.\d{1,4})?$""")

    /** Whether typed money is a decimal the server takes: up to four places. */
    fun isDecimal(text: String): Boolean = decimalPattern.matches(text)

    fun decimal(text: String?): BigDecimal? {
        if (text.isNullOrEmpty()) return null
        return text.toBigDecimalOrNull()
    }

    private fun currencyOf(currencyCode: String?): String = if (currencyCode.isNullOrEmpty()) "USD" else currencyCode

    /** An amount in its currency, or "—" when there is none. */
    fun shown(text: String?, currencyCode: String?, locale: Locale = Locale.getDefault()): String {
        val amount = decimal(text) ?: return "—"
        return currencyText(amount, currencyOf(currencyCode), locale)
    }

    fun shown(amount: BigDecimal, currencyCode: String?, locale: Locale = Locale.getDefault()): String =
        currencyText(amount, currencyOf(currencyCode), locale)

    private fun currencyText(amount: BigDecimal, code: String, locale: Locale): String {
        val format = NumberFormat.getCurrencyInstance(locale)
        try {
            format.currency = Currency.getInstance(code.uppercase())
        } catch (_: IllegalArgumentException) {
            return "$code ${NumberFormat.getNumberInstance(locale).format(amount)}"
        }
        return format.format(amount)
    }

    /**
     * An amount on a chart's axis: short, in its currency, as "$12K" or "¥1.2万", since an axis has room for a few
     * characters only.
     */
    fun axisText(amount: Double, currencyCode: String?, locale: Locale = Locale.getDefault()): String {
        val code = currencyOf(currencyCode)
        val symbol = try {
            Currency.getInstance(code.uppercase()).getSymbol(locale)
        } catch (_: IllegalArgumentException) {
            code
        }
        val compact = NumberFormat.getCompactNumberInstance(locale, NumberFormat.Style.SHORT).apply { maximumFractionDigits = 1 }
        val sign = if (amount < 0) "-" else ""
        return sign + symbol + compact.format(kotlin.math.abs(amount))
    }

    /** A share as a whole percentage; "<1%" for one that rounds to nothing. */
    fun percent(share: Double, locale: Locale = Locale.getDefault()): String {
        if (share > 0 && share < 0.005) return "<1%"
        val format = NumberFormat.getPercentInstance(locale)
        format.maximumFractionDigits = 0
        return format.format(share)
    }
}

/**
 * FinanceCategories names categories and orders them for pickers, as the dashboard does: hidden ones left out,
 * "Parent › Child", Other after the rest and Transfer last.
 */
object FinanceCategories {
    /**
     * A category as shown, under its parent; [named] is how one name is shown, which turns a built-in name into the
     * reader's words.
     */
    fun label(
        category: SpendingCategory,
        categories: List<SpendingCategory>,
        named: (SpendingCategory) -> String = { each -> each.spendingCategoryName },
    ): String {
        val parentId = category.parentSpendingCategoryId
        if (parentId.isNullOrEmpty()) return named(category)
        val parent = categories.firstOrNull { each -> each.id == parentId } ?: return named(category)
        return "${named(parent)} › ${named(category)}"
    }

    fun pickable(
        categories: List<SpendingCategory>,
        keeping: String? = null,
        named: (SpendingCategory) -> String = { each -> each.spendingCategoryName },
    ): List<SpendingCategory> {
        fun rank(category: SpendingCategory): Int = when {
            category.isTransfer == true -> 2
            category.isOther == true -> 1
            else -> 0
        }
        return categories.filter { each -> each.isHidden != true || each.id == keeping }
            .sortedWith { left, right ->
                val rankOrder = rank(left).compareTo(rank(right))
                if (rankOrder != 0) rankOrder
                else standardCompare(label(left, categories, named), label(right, categories, named))
            }
    }
}

/**
 * The comparison the system's own lists use: case and accents aside, and a number inside a name counted as a number,
 * so "Item 2" comes before "Item 10".
 */
fun standardCompare(left: String, right: String): Int {
    val collator = java.text.Collator.getInstance().apply { strength = java.text.Collator.SECONDARY }
    val pattern = Regex("""\d+|\D+""")
    val leftParts = pattern.findAll(left).map { match -> match.value }.toList()
    val rightParts = pattern.findAll(right).map { match -> match.value }.toList()
    for (index in 0 until minOf(leftParts.size, rightParts.size)) {
        val leftPart = leftParts[index]
        val rightPart = rightParts[index]
        val order = if (leftPart[0].isDigit() && rightPart[0].isDigit()) {
            leftPart.toBigInteger().compareTo(rightPart.toBigInteger())
        } else {
            collator.compare(leftPart, rightPart)
        }
        if (order != 0) return order
    }
    return leftParts.size.compareTo(rightParts.size)
}
