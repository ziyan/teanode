package com.teanode.kit.finance

import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import java.math.BigDecimal
import java.math.MathContext
import java.math.RoundingMode

// What the models of Finance.swift, FinanceTax.swift, FinanceSharing.swift, FinanceManage.swift and
// FinanceStatement.swift compute from their own fields.

/** "Checking ··1234". */
val FinanceAccount.shownName: String
    get() {
        val name = accountName?.takeIf { text -> text.isNotEmpty() } ?: institutionName ?: ""
        val mask = accountMask
        if (mask.isNullOrEmpty()) return name
        return "$name ··$mask"
    }

val FinanceAccount.isFromStatements: Boolean get() = providerKind == "statement"

/** The merchant, else the bank's description. */
val FinanceTransaction.shownName: String
    get() {
        if (!merchantName.isNullOrEmpty()) return merchantName
        return description ?: ""
    }

val FinanceTransaction.isDuplicate: Boolean get() = !duplicateOfTransactionId.isNullOrEmpty()

/** FinanceTransactionFilter is what the transaction list is narrowed to. */
data class FinanceTransactionFilter(
    val from: String = "",
    val to: String = "",
    val financeAccountId: String = "",
    val spendingCategoryId: String = "",
    val text: String = "",
    val isUncategorized: Boolean = false,
    val isDuplicateIncluded: Boolean = false,
) {
    val variables: Map<String, JsonElement>
        get() {
            val variables = mutableMapOf<String, JsonElement>()
            for ((key, value) in listOf(
                "from" to from, "to" to to, "financeAccountId" to financeAccountId,
                "spendingCategoryId" to spendingCategoryId, "text" to text,
            )) {
                if (value.trim().isNotEmpty()) variables[key] = JsonPrimitive(value.trim())
            }
            if (isUncategorized) variables["isUncategorized"] = JsonPrimitive(true)
            if (isDuplicateIncluded) variables["isDuplicateIncluded"] = JsonPrimitive(true)
            return variables
        }

    val isNarrowing: Boolean get() = variables.isNotEmpty()
}

val FinanceAsset.isClosed: Boolean get() = !closedOn.isNullOrEmpty()

/** The kinds of what is owned and what is owed, in balance-sheet order. */
object FinanceAssetKinds {
    val owned = listOf("cash", "investment", "retirement", "property", "vehicle", "other_asset")
    val owed = listOf("credit_card", "loan", "mortgage", "other_liability")
}

val SavingsTarget.id: String get() = savingsTarget?.id ?: ""

val FinanceTrade.shownName: String
    get() {
        for (text in listOf(financeSecurity?.tickerSymbol, financeSecurity?.securityName, description)) {
            if (!text.isNullOrEmpty()) return text
        }
        return ""
    }

val SpendingRuleProposals.proposals: List<SpendingRuleProposals.Proposal> get() = spendingRuleProposals.filterNotNull()

val SpendingRuleProposals.Proposal.id: String get() = matchText

val TransactionRowsPreview.newRows: List<TransactionRowsPreview.Row> get() = newTransactionRows.orEmpty().filterNotNull()

val TransactionRowsPreview.presentRows: List<TransactionRowsPreview.Row> get() = presentTransactionRows.orEmpty().filterNotNull()

/** Whether anything went into an account. */
val StatementImport.LastImport.isImported: Boolean get() = !financeAccountNames.isNullOrEmpty()

// MARK: Tax

/** Whether its lines add up to its printed totals. */
val FinancePaystub.isBalanced: Boolean get() = paystubCheckState == "balanced"

/** Its lines of one kind ("earning", "tax", "deduction", "reimbursement"), in printed order. */
fun FinancePaystub.lines(lineKind: String): List<FinancePaystub.Line> =
    paystubLines.orEmpty().filterNotNull().filter { line -> line.paystubLineKind == lineKind }.sortedBy { line -> line.lineNumber ?: 0 }

private fun boxNameOf(boxCode: String?, boxQualifier: String?): String {
    if (boxQualifier.isNullOrEmpty()) return boxCode ?: ""
    return "${boxCode ?: ""} $boxQualifier"
}

/** "12 D", or "1" for a box that is one. */
val FinanceTaxForm.Entry.boxName: String get() = boxNameOf(boxCode, boxQualifier)

val TaxYearSummary.Comparison.boxName: String get() = boxNameOf(boxCode, boxQualifier)

val FinanceTaxForm.entries: List<FinanceTaxForm.Entry>
    get() = taxFormEntries.orEmpty().filterNotNull().sortedBy { entry -> entry.entryNumber ?: 0 }

/** The form's kind as it is titled: W-2, 1099-INT, 1099-DIV, 1099-B. */
val FinanceTaxForm.kindName: String
    get() = when (taxFormKind) {
        "1099_int" -> "1099-INT"
        "1099_div" -> "1099-DIV"
        "1099_b" -> "1099-B"
        "1098" -> "1098"
        else -> "W-2"
    }

/** Whether it is a W-2, from an employer, rather than a 1099. */
val FinanceTaxForm.isW2: Boolean get() = taxFormKind == null || taxFormKind == "w2"

val FinanceTaxFormView.id: String get() = financeTaxForm?.id ?: ""

val FinanceTaxFormView.findings: List<FinanceTaxFormView.Finding> get() = taxFormFindings.orEmpty().filterNotNull()

/** How many of its checks disagree; none when it agrees with itself. */
val FinanceTaxFormView.disagreeingCount: Int get() = findings.count { finding -> finding.isAgreeing == false }

val TaxYearSummary.employments: List<TaxYearSummary.Employment> get() = taxYearEmployments.orEmpty().filterNotNull()

val TaxYearSummary.findings: List<TaxYearSummary.Finding> get() = taxYearFindings.orEmpty().filterNotNull()

/** Whether its figures are the W-2's rather than the paystubs'. */
val TaxYearSummary.Employment.isFromW2: Boolean get() = taxYearFigureSource == "w2"

val TaxYearSummary.Employment.comparisons: List<TaxYearSummary.Comparison> get() = taxYearComparisons.orEmpty().filterNotNull()

val TaxYearSummary.InvestmentPreview.accountIncomes: List<TaxYearSummary.InvestmentPreview.AccountIncome>
    get() = taxYearAccountIncomes.orEmpty().filterNotNull()

/** How a return is filed, which the estimate's federal tax depends on. */
enum class TaxFilingStatus(val rawValue: String) {
    Single("single"),
    MarriedFilingJointly("married_filing_jointly"),
    MarriedFilingSeparately("married_filing_separately"),
    HeadOfHousehold("head_of_household");

    companion object {
        fun fromRawValue(rawValue: String?): TaxFilingStatus? = entries.firstOrNull { status -> status.rawValue == rawValue }
    }
}

/** Which part of a year's pay a slice of the tax ring is. */
enum class TaxEstimateSliceKind { Paid, UpcomingWithheld, Owing, RemainingIncome }

/** One slice of the tax ring. */
data class TaxEstimateSlice(val sliceKind: TaxEstimateSliceKind, val amount: String?) {
    val amountValue: Double get() = amount?.toDoubleOrNull() ?: 0.0
}

/** The ring's slices in the order they are drawn, the ones of nothing left out. */
val TaxYearSummary.Estimate.slices: List<TaxEstimateSlice>
    get() = listOf(
        TaxEstimateSlice(TaxEstimateSliceKind.Paid, paidTaxAmount),
        TaxEstimateSlice(TaxEstimateSliceKind.UpcomingWithheld, upcomingWithheldTaxAmount),
        TaxEstimateSlice(TaxEstimateSliceKind.Owing, owingTaxAmount),
        TaxEstimateSlice(TaxEstimateSliceKind.RemainingIncome, remainingIncomeAmount),
    ).filter { slice -> slice.amountValue > 0 }

val TaxYearSummary.Estimate.filingStatus: TaxFilingStatus? get() = TaxFilingStatus.fromRawValue(taxFilingStatus)

/**
 * An amount's share of income, 0.191 for 19.1%; null without income. Income is pay and investment income, or pay from
 * a server that does not say.
 */
fun TaxYearSummary.Estimate.shareOfPay(amount: String?): Double? {
    val income = (incomeAmount ?: payAmount)?.toDoubleOrNull() ?: return null
    if (income <= 0) return null
    return (amount?.toDoubleOrNull() ?: 0.0) / income
}

/** Which figure of a year a row shows. */
sealed class TaxYearFigureKind {
    data object Wages : TaxYearFigureKind()
    data object FederalIncomeTax : TaxYearFigureKind()
    data object SocialSecurityWages : TaxYearFigureKind()
    data object SocialSecurityTax : TaxYearFigureKind()
    data object MedicareWages : TaxYearFigureKind()
    data object MedicareTax : TaxYearFigureKind()
    data object RetirementTraditional : TaxYearFigureKind()
    data object RetirementRoth : TaxYearFigureKind()
    data object Hsa : TaxYearFigureKind()
    data object DependentCare : TaxYearFigureKind()
    data class StateWages(val stateCode: String) : TaxYearFigureKind()
    data class StateIncomeTax(val stateCode: String) : TaxYearFigureKind()
    data object LocalIncomeTax : TaxYearFigureKind()
}

/** One figure of a year, and the same figure projected to December 31 when there is a projection. */
data class TaxYearFigureRow(val figureKind: TaxYearFigureKind, val amount: String?, val projectedAmount: String?)

/**
 * The figures in the order a W-2 prints them, each with its projection when one is given. Rows at zero are left out,
 * except wages and federal income tax, which always show.
 */
fun TaxYearFigures.rows(projectedTo: TaxYearFigures? = null): List<TaxYearFigureRow> {
    fun row(figureKind: TaxYearFigureKind, amount: String?, projectedAmount: String?, isAlwaysShown: Boolean = false): TaxYearFigureRow? {
        val isZero = (FinanceMoney.decimal(amount) ?: BigDecimal.ZERO).signum() == 0 &&
            (FinanceMoney.decimal(projectedAmount) ?: BigDecimal.ZERO).signum() == 0
        if (isZero && !isAlwaysShown) return null
        return TaxYearFigureRow(figureKind, amount, if (projectedTo == null) null else projectedAmount)
    }
    val projected = projectedTo
    val rows = mutableListOf(
        row(TaxYearFigureKind.Wages, wagesAmount, projected?.wagesAmount, isAlwaysShown = true),
        row(TaxYearFigureKind.FederalIncomeTax, federalIncomeTaxAmount, projected?.federalIncomeTaxAmount, isAlwaysShown = true),
        row(TaxYearFigureKind.SocialSecurityWages, socialSecurityWagesAmount, projected?.socialSecurityWagesAmount),
        row(TaxYearFigureKind.SocialSecurityTax, socialSecurityTaxAmount, projected?.socialSecurityTaxAmount),
        row(TaxYearFigureKind.MedicareWages, medicareWagesAmount, projected?.medicareWagesAmount),
        row(TaxYearFigureKind.MedicareTax, medicareTaxAmount, projected?.medicareTaxAmount),
        row(TaxYearFigureKind.RetirementTraditional, retirementTraditionalAmount, projected?.retirementTraditionalAmount),
        row(TaxYearFigureKind.RetirementRoth, retirementRothAmount, projected?.retirementRothAmount),
        row(TaxYearFigureKind.Hsa, hsaAmount, projected?.hsaAmount),
        row(TaxYearFigureKind.DependentCare, dependentCareAmount, projected?.dependentCareAmount),
    )
    val states = taxYearStateFigures.orEmpty().filterNotNull()
    val projectedStates = projected?.taxYearStateFigures.orEmpty().filterNotNull()
    val stateCodes = states.map { state -> state.stateCode }.toMutableList()
    for (state in projectedStates) if (state.stateCode !in stateCodes) stateCodes.add(state.stateCode)
    for (stateCode in stateCodes) {
        val state = states.firstOrNull { each -> each.stateCode == stateCode }
        val projectedState = projectedStates.firstOrNull { each -> each.stateCode == stateCode }
        rows.add(row(TaxYearFigureKind.StateWages(stateCode), state?.stateWagesAmount, projectedState?.stateWagesAmount))
        rows.add(row(TaxYearFigureKind.StateIncomeTax(stateCode), state?.stateIncomeTaxAmount, projectedState?.stateIncomeTaxAmount))
    }
    rows.add(row(TaxYearFigureKind.LocalIncomeTax, localIncomeTaxAmount, projected?.localIncomeTaxAmount))
    return rows.filterNotNull()
}

/** TaxYearSettingChange is the fields of a year's settings to change; null keeps one as it is. */
data class TaxYearSettingChange(
    val childCount: Int? = null,
    val otherDependentCount: Int? = null,
    val dependentCareExpenseAmount: String? = null,
    val otherIncomeAmount: String? = null,
    val charitableContributionAmount: String? = null,
    val propertyTaxAmount: String? = null,
    val stateIncomeTaxPaidAmount: String? = null,
    val estimatedShortTermGainAmount: String? = null,
    val estimatedLongTermGainAmount: String? = null,
    val estimatedGainNote: String? = null,
    val isEstimatedGainCleared: Boolean = false,
) {
    val variables: Map<String, JsonElement>
        get() {
            val variables = mutableMapOf<String, JsonElement>()
            childCount?.let { number -> variables["childCount"] = JsonPrimitive(number) }
            otherDependentCount?.let { number -> variables["otherDependentCount"] = JsonPrimitive(number) }
            for ((key, text) in listOf(
                "dependentCareExpenseAmount" to dependentCareExpenseAmount, "otherIncomeAmount" to otherIncomeAmount,
                "charitableContributionAmount" to charitableContributionAmount, "propertyTaxAmount" to propertyTaxAmount,
                "stateIncomeTaxPaidAmount" to stateIncomeTaxPaidAmount,
                "estimatedShortTermGainAmount" to estimatedShortTermGainAmount,
                "estimatedLongTermGainAmount" to estimatedLongTermGainAmount, "estimatedGainNote" to estimatedGainNote,
            )) {
                if (text != null) variables[key] = JsonPrimitive(text)
            }
            if (isEstimatedGainCleared) variables["isEstimatedGainCleared"] = JsonPrimitive(true)
            return variables
        }
}

// MARK: Sharing

val FinanceShareLevel.rank: Int
    get() = when (this) {
        FinanceShareLevel.Balance -> 1
        FinanceShareLevel.Spending -> 2
        FinanceShareLevel.Transactions -> 3
    }

/** Orders the levels; a level reaches every one below it. */
fun FinanceShareLevel.isAtLeast(other: FinanceShareLevel): Boolean = rank >= other.rank

private fun levelOf(rawValue: String): FinanceShareLevel? = FinanceShareLevel.entries.firstOrNull { level -> level.rawValue == rawValue }

private fun shownOwnerName(ownerName: String?, ownerUsername: String): String =
    if (!ownerName.isNullOrEmpty()) ownerName else ownerUsername

private fun shownAccount(accountName: String, accountMask: String?): String =
    if (accountMask.isNullOrEmpty()) accountName else "$accountName ··$accountMask"

/** The level, when it is one the app knows. */
val FinanceShareView.level: FinanceShareLevel? get() = levelOf(financeShareLevel)

/** "Checking ··1234". */
val FinanceShareView.shownAccountName: String get() = shownAccount(accountName, accountMask)

/** Who the share is with, as a target a sheet can show. */
val FinanceShareView.target: ShareTarget
    get() = if (granteeCircleId != null) ShareTarget.Circle(granteeCircleId) else ShareTarget.Person(granteeUserId ?: "")

val AllFinanceShareView.level: FinanceShareLevel? get() = levelOf(financeShareLevel)

val AllFinanceShareView.target: ShareTarget
    get() = if (granteeCircleId != null) ShareTarget.Circle(granteeCircleId) else ShareTarget.Person(granteeUserId ?: "")

val SharedAllFinance.id: String get() = ownerUserId

val SharedAllFinance.level: FinanceShareLevel? get() = levelOf(financeShareLevel)

/** Whether the transactions are shared too, not only the spending. */
val SharedAllFinance.isSharingTransactions: Boolean get() = level?.isAtLeast(FinanceShareLevel.Transactions) ?: false

/** The owner's name, else their username. */
val SharedAllFinance.ownerShownName: String get() = shownOwnerName(ownerName, ownerUsername)

val SharedFinanceAccount.id: String get() = financeAccountId

val SharedFinanceAccount.level: FinanceShareLevel? get() = levelOf(financeShareLevel)

/** Whether the account is shared at this level or more. */
fun SharedFinanceAccount.isShared(atLeast: FinanceShareLevel): Boolean = level?.isAtLeast(atLeast) ?: false

val SharedFinanceAccount.shownName: String get() = shownAccount(accountName, accountMask)

val SharedFinanceAccount.ownerShownName: String get() = shownOwnerName(ownerName, ownerUsername)

val SharedFinanceSpendingRow.id: String get() = "$spendingMonth|$spendingCategoryName|$currencyCode"

/** Who a share is with: a person or a circle. */
sealed class ShareTarget {
    data class Person(val userId: String) : ShareTarget()
    data class Circle(val circleId: String) : ShareTarget()

    val granteeUserId: String? get() = (this as? Person)?.userId
    val granteeCircleId: String? get() = (this as? Circle)?.circleId
}

/** "1.0834": rounded to five significant digits, without trailing zeros. */
val CurrencyRate.shownRate: String?
    get() {
        val value = FinanceMoney.decimal(rate) ?: return null
        if (value.signum() <= 0) return null
        val rounded = value.round(MathContext(5, RoundingMode.HALF_UP))
        return rounded.stripTrailingZeros().toPlainString()
    }

val TransactionRowsPreview.Row.id: Int get() = rowNumber ?: 0
