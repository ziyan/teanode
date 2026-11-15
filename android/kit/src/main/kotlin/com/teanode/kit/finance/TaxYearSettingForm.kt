package com.teanode.kit.finance

/**
 * TaxYearSettingForm is the text fields of a year's settings, as they are typed. Saving sends only what changed: an
 * amount emptied is sent as nothing, and both gains emptied clear the estimate.
 */
data class TaxYearSettingForm(
    val childCountText: String = "",
    val otherDependentCountText: String = "",
    val dependentCareExpenseText: String = "",
    val otherIncomeText: String = "",
    val propertyTaxText: String = "",
    val stateIncomeTaxPaidText: String = "",
    val charitableContributionText: String = "",
    val shortTermGainText: String = "",
    val longTermGainText: String = "",
    val gainNoteText: String = "",
) {
    /** The change to send: only the fields that differ from [setting]. */
    fun change(setting: TaxYearSetting?): TaxYearSettingChange {
        fun changedAmount(text: String, before: String?): String? {
            val trimmed = text.trim()
            return if (trimmed == plain(before)) null else trimmed.ifEmpty { "0" }
        }
        val childCountBefore = (setting?.childCount ?: 0).toString()
        val otherDependentCountBefore = (setting?.otherDependentCount ?: 0).toString()
        val shortTermBefore = plain(setting?.estimatedShortTermGainAmount)
        val longTermBefore = plain(setting?.estimatedLongTermGainAmount)
        val isGainCleared = shortTermGainText.isBlank() && longTermGainText.isBlank() && (shortTermBefore.isNotEmpty() || longTermBefore.isNotEmpty())
        return TaxYearSettingChange(
            childCount = if (childCountText.trim() != childCountBefore) childCountText.trim().toIntOrNull() ?: 0 else null,
            otherDependentCount = if (otherDependentCountText.trim() != otherDependentCountBefore) otherDependentCountText.trim().toIntOrNull() ?: 0 else null,
            dependentCareExpenseAmount = changedAmount(dependentCareExpenseText, setting?.dependentCareExpenseAmount),
            otherIncomeAmount = changedAmount(otherIncomeText, setting?.otherIncomeAmount),
            propertyTaxAmount = changedAmount(propertyTaxText, setting?.propertyTaxAmount),
            stateIncomeTaxPaidAmount = changedAmount(stateIncomeTaxPaidText, setting?.stateIncomeTaxPaidAmount),
            charitableContributionAmount = changedAmount(charitableContributionText, setting?.charitableContributionAmount),
            estimatedShortTermGainAmount = if (!isGainCleared && shortTermGainText.trim() != shortTermBefore) shortTermGainText.trim() else null,
            estimatedLongTermGainAmount = if (!isGainCleared && longTermGainText.trim() != longTermBefore) longTermGainText.trim() else null,
            estimatedGainNote = if (gainNoteText.trim() != setting?.estimatedGainNote.orEmpty()) gainNoteText.trim() else null,
            isEstimatedGainCleared = isGainCleared,
        )
    }

    companion object {
        /** The fields filled from the year's setting. */
        fun of(setting: TaxYearSetting?): TaxYearSettingForm = TaxYearSettingForm(
            childCountText = (setting?.childCount ?: 0).toString(),
            otherDependentCountText = (setting?.otherDependentCount ?: 0).toString(),
            dependentCareExpenseText = plain(setting?.dependentCareExpenseAmount),
            otherIncomeText = plain(setting?.otherIncomeAmount),
            propertyTaxText = plain(setting?.propertyTaxAmount),
            stateIncomeTaxPaidText = plain(setting?.stateIncomeTaxPaidAmount),
            charitableContributionText = plain(setting?.charitableContributionAmount),
            shortTermGainText = plain(setting?.estimatedShortTermGainAmount),
            longTermGainText = plain(setting?.estimatedLongTermGainAmount),
            gainNoteText = setting?.estimatedGainNote.orEmpty(),
        )

        /** An amount as the person types it: no trailing zeros, empty for none. */
        fun plain(amount: String?): String {
            val decimal = FinanceMoney.decimal(amount) ?: return ""
            return if (decimal.signum() == 0) "" else decimal.stripTrailingZeros().toPlainString()
        }
    }
}

/** TaxWords are small rules of the tax screens' wording. */
object TaxWords {
    /** A person with the last four digits of their taxpayer number, when known. */
    fun personText(name: String?, taxIdMask: String?): String {
        val shownName = name.orEmpty()
        return if (taxIdMask.isNullOrEmpty()) shownName else "$shownName ··$taxIdMask"
    }

    /** Social Security withheld beyond the maximum comes back on the return: worth knowing, not a problem. */
    fun isProblem(finding: TaxYearSummary.Finding): Boolean = finding.taxYearFindingCode != "excess_social_security"

    /** Who a finding is about: the employee and the employer, those that are named. */
    fun whoseText(finding: TaxYearSummary.Finding): String = listOfNotNull(finding.employeeName, finding.employerName).filter { name -> name.isNotEmpty() }.joinToString(", ")
}

/** What the toast says after renaming an account of imported statements: each thing that changed, so a number that did not take is not hidden behind a rename that did. */
enum class StatementAccountRenameWords { NameAndMaskCleared, NameAndMask, NameOnly, MaskCleared, MaskOnly }

/**
 * StatementAccountRename is the name and last digits of an imported account as they are typed. Only what changed is sent:
 * an empty last-digits field, once changed, takes the statements' own number back.
 */
data class StatementAccountRename(val account: FinanceAccount, val nameText: String, val maskText: String) {
    private val trimmedName get() = nameText.trim()
    private val trimmedMask get() = maskText.trim()
    val isNameChanged: Boolean get() = trimmedName.isNotEmpty() && trimmedName != account.accountName.orEmpty()
    val isMaskChanged: Boolean get() = trimmedMask != account.accountMask.orEmpty()

    /** Whether Save is allowed: a name is kept, and something changed. */
    val canSave: Boolean get() = trimmedName.isNotEmpty() && (isNameChanged || isMaskChanged)

    val sentName: String? get() = if (isNameChanged) trimmedName else null
    val sentMask: String? get() = if (isMaskChanged) trimmedMask else null

    val words: StatementAccountRenameWords
        get() = when {
            isNameChanged && isMaskChanged -> if (trimmedMask.isEmpty()) StatementAccountRenameWords.NameAndMaskCleared else StatementAccountRenameWords.NameAndMask
            isNameChanged -> StatementAccountRenameWords.NameOnly
            else -> if (trimmedMask.isEmpty()) StatementAccountRenameWords.MaskCleared else StatementAccountRenameWords.MaskOnly
        }

    companion object {
        /** The name is at most 200 characters and the last digits at most 8, as typed. */
        fun limited(nameText: String, maskText: String): Pair<String, String> = nameText.take(200) to maskText.take(8)
    }
}
