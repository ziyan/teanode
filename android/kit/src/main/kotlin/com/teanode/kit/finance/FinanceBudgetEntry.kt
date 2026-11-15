package com.teanode.kit.finance

/** What a typed budget may be saved as, and when saving it rewrites months already past (iOS BudgetEditor). */
object FinanceBudgetEntry {
    /** A budget is saved with a category and an amount that is a decimal and not negative; a blank amount is not zero. */
    fun isSavable(categoryId: String, monthlyAmount: String): Boolean {
        val trimmed = monthlyAmount.trim()
        return categoryId.isNotEmpty() && FinanceMoney.isDecimal(trimmed) && !trimmed.startsWith("-")
    }

    /** Months compare as text ("2030-10"), as the server's months do. */
    fun changesMonthsAlreadyPast(effectiveFromMonth: String, thisMonth: String): Boolean = effectiveFromMonth.trim() < thisMonth

    /** The month a budget starts from when it is opened for change: this month, unless it starts later. The server keeps a budget's first day ("2030-10-01"); a month is the first seven characters. */
    fun startingMonth(budgetEffectiveFrom: String, thisMonth: String): String = maxOf(thisMonth, budgetEffectiveFrom.take(MONTH_LENGTH))

    /** Whether a budget stored from this day is in force in the month: it began that month or before. */
    fun isInForce(budgetEffectiveFrom: String, month: String): Boolean = budgetEffectiveFrom.take(MONTH_LENGTH) <= month

    private const val MONTH_LENGTH = 7
}

/** How an asset is valued, as the server names it. */
object FinanceValuationSources {
    const val MANUAL = "manual"
    const val AGENT_ESTIMATE = "agent_estimate"
    const val AGENT_READING = "agent_reading"
    val all = listOf(MANUAL, AGENT_ESTIMATE, AGENT_READING)

    /** An agent's estimate needs the words it estimates from. */
    fun isEstimateDescriptionNeeded(valuationSource: String): Boolean = valuationSource == AGENT_ESTIMATE

    fun isAssetSavable(assetName: String, valuationSource: String, estimateDescription: String, value: String): Boolean =
        assetName.isNotBlank() && (value.isBlank() || FinanceMoney.isDecimal(value.trim())) &&
            (!isEstimateDescriptionNeeded(valuationSource) || estimateDescription.isNotBlank())
}
