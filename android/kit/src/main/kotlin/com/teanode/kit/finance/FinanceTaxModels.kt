package com.teanode.kit.finance

import kotlinx.serialization.Serializable

@Serializable
data class FinancePaystub(
    val id: String,
    val agentAttachmentId: String? = null,
    val employerName: String? = null,
    val employerTaxId: String? = null,
    val employeeName: String? = null,
    val employeeTaxIdMask: String? = null,
    val payDate: String? = null,
    val periodStartOn: String? = null,
    val periodEndOn: String? = null,
    val currencyCode: String? = null,
    val grossAmount: String? = null,
    val yearToDateGrossAmount: String? = null,
    val totalTaxAmount: String? = null,
    val totalDeductionAmount: String? = null,
    val netAmount: String? = null,
    val paystubCheckState: String? = null,
    val checkDifferenceAmount: String? = null,
    val paystubLines: List<Line?>? = null,
) {
    @Serializable
    data class Line(
        val id: String,
        val lineNumber: Int? = null,
        val paystubLineKind: String? = null,
        val description: String? = null,
        val hours: String? = null,
        val rateAmount: String? = null,
        val currentAmount: String? = null,
        val yearToDateAmount: String? = null,
        val paystubTaxKind: String? = null,
        val taxJurisdictionCode: String? = null,
        val currentWagesAmount: String? = null,
        val yearToDateWagesAmount: String? = null,
        val paystubDeductionKind: String? = null,
        val employerCurrentAmount: String? = null,
        val employerYearToDateAmount: String? = null,
    )
}

@Serializable
data class FinancePaystubPage(
    val financePaystubs: List<FinancePaystub?>,
    val hasMore: Boolean? = null,
    val totalCount: Int? = null,
)

@Serializable
data class FinanceTaxForm(
    val id: String,
    val agentAttachmentId: String? = null,
    val taxFormKind: String? = null,
    val taxYear: Int? = null,
    val isCorrected: Boolean? = null,
    val payerName: String? = null,
    val payerTaxId: String? = null,
    val recipientName: String? = null,
    val recipientTaxIdMask: String? = null,
    val taxFormEntries: List<Entry?>? = null,
) {
    @Serializable
    data class Entry(
        val id: String,
        val entryNumber: Int? = null,
        val boxCode: String? = null,
        val boxQualifier: String? = null,
        val amount: String? = null,
        val textValue: String? = null,
        val isChecked: Boolean? = null,
    )
}

/** FinanceTaxFormView is a tax form with what it says about itself. */
@Serializable
data class FinanceTaxFormView(
    val financeTaxForm: FinanceTaxForm? = null,
    val taxFormFindings: List<Finding?>? = null,
) {
    @Serializable
    data class Finding(
        val taxFormFindingCode: String? = null,
        val isAgreeing: Boolean? = null,
        val expectedAmount: String? = null,
        val printedAmount: String? = null,
        val findingText: String? = null,
    )
}

@Serializable
data class FinanceTaxFormPage(
    val taxForms: List<FinanceTaxFormView?>,
    val hasMore: Boolean? = null,
    val totalCount: Int? = null,
)

/** TaxYearFigures are a year's wages and withholding as a W-2 names them. */
@Serializable
data class TaxYearFigures(
    val wagesAmount: String? = null,
    val federalIncomeTaxAmount: String? = null,
    val socialSecurityWagesAmount: String? = null,
    val socialSecurityTaxAmount: String? = null,
    val medicareWagesAmount: String? = null,
    val medicareTaxAmount: String? = null,
    val dependentCareAmount: String? = null,
    val retirementTraditionalAmount: String? = null,
    val retirementRothAmount: String? = null,
    val hsaAmount: String? = null,
    val healthCoverageAmount: String? = null,
    val localIncomeTaxAmount: String? = null,
    val taxYearStateFigures: List<State?>? = null,
) {
    @Serializable
    data class State(
        val stateCode: String,
        val stateWagesAmount: String? = null,
        val stateIncomeTaxAmount: String? = null,
    )
}

/** TaxYearSummary is a tax year put together from paystubs and W-2s. */
@Serializable
data class TaxYearSummary(
    val taxYear: Int? = null,
    val socialSecurityWageBaseAmount: String? = null,
    /**
     * The year's limit on a person's retirement plan contributions from
     * pay before catch-up, when TeaNode knows it.
     */
    val retirementDeferralLimitAmount: String? = null,
    val isYearOver: Boolean? = null,
    val paystubCount: Int? = null,
    val taxFormCount: Int? = null,
    val figures: TaxYearFigures? = null,
    val projectedFigures: TaxYearFigures? = null,
    val taxYearEmployments: List<Employment?>? = null,
    val taxYearPeople: List<Person?>? = null,
    val taxYearFindings: List<Finding?>? = null,
    val taxYearEstimate: Estimate? = null,
    val taxYearInvestment: Investment? = null,
    val taxYearInvestmentPreview: InvestmentPreview? = null,
    val taxYearSetting: TaxYearSetting? = null,
) {
    @Serializable
    data class Comparison(
        val boxCode: String? = null,
        val boxQualifier: String? = null,
        val boxLabel: String? = null,
        val w2Amount: String? = null,
        val paystubAmount: String? = null,
        val differenceAmount: String? = null,
        val isMatching: Boolean? = null,
    )
    @Serializable
    data class Employment(
        val employeeName: String? = null,
        val employerName: String? = null,
        val employerTaxId: String? = null,
        val taxYearFigureSource: String? = null,
        val taxFormId: String? = null,
        val latestPayDate: String? = null,
        val paystubCount: Int? = null,
        val payCadenceDayCount: Int? = null,
        val isPaystubYearComplete: Boolean? = null,
        val remainingPayCount: Int? = null,
        /**
         * Whether the projection stops retirement contributions at the
         * year's limit, the rest counted as wages.
         */
        val isRetirementDeferralCapped: Boolean? = null,
        val figures: TaxYearFigures? = null,
        val paystubFigures: TaxYearFigures? = null,
        val projectedFigures: TaxYearFigures? = null,
        val taxYearComparisons: List<Comparison?>? = null,
    )
    @Serializable
    data class Person(
        val employeeName: String? = null,
        val excessSocialSecurityTaxAmount: String? = null,
        val figures: TaxYearFigures? = null,
        val projectedFigures: TaxYearFigures? = null,
    )
    @Serializable
    data class Finding(
        val taxYearFindingCode: String? = null,
        val employeeName: String? = null,
        val employerName: String? = null,
        val findingText: String? = null,
        val paystubIds: List<String>? = null,
    )
    /**
     * Estimate is the year's tax on its wages beside what was withheld,
     * the household's, to December 31 when the year is projected. Paid,
     * upcoming withheld, owing and remaining add up to pay; a refund is
     * inside what was withheld.
     */
    @Serializable
    data class Estimate(
        val taxFilingStatus: String? = null,
        /** The person has not said how they file, and this one is assumed. */
        val isTaxFilingStatusAssumed: Boolean? = null,
        val isProjected: Boolean? = null,
        val payAmount: String? = null,
        /**
         * Interest, ordinary dividends and the capital result from 1099s,
         * and pay and it together: the ring's whole.
         */
        val investmentIncomeAmount: String? = null,
        val incomeAmount: String? = null,
        val adjustedGrossIncomeAmount: String? = null,
        /**
         * Qualified dividends and net long-term gain, and their tax at
         * 0%, 15% or 20%, part of the federal income tax.
         */
        val preferentialIncomeAmount: String? = null,
        val preferentialIncomeTaxAmount: String? = null,
        val capitalLossDeductionAmount: String? = null,
        val foreignTaxCreditAmount: String? = null,
        val netInvestmentIncomeTaxAmount: String? = null,
        /**
         * Investment income counted from the accounts or the gains given,
         * until the 1099s come.
         */
        val isInvestmentIncomePreviewed: Boolean? = null,
        val otherIncomeAmount: String? = null,
        /** The deduction taken: itemized when that is larger. */
        val deductionAmount: String? = null,
        val isItemized: Boolean? = null,
        val itemizedDeductionAmount: String? = null,
        val mortgageInterestAmount: String? = null,
        val stateAndLocalTaxDeductionAmount: String? = null,
        val charitableContributionAmount: String? = null,
        val childTaxCreditAmount: String? = null,
        val dependentCareCreditAmount: String? = null,
        val standardDeductionAmount: String? = null,
        val taxableIncomeAmount: String? = null,
        val federalIncomeTaxAmount: String? = null,
        val socialSecurityTaxAmount: String? = null,
        val medicareTaxAmount: String? = null,
        val stateIncomeTaxAmount: String? = null,
        val localIncomeTaxAmount: String? = null,
        val totalTaxAmount: String? = null,
        val paidTaxAmount: String? = null,
        val upcomingWithheldTaxAmount: String? = null,
        val owingTaxAmount: String? = null,
        val refundAmount: String? = null,
        val remainingIncomeAmount: String? = null,
    )
    /**
     * InvestmentPreview is the year's interest and dividends as the linked
     * accounts were paid them, projected to December 31.
     */
    @Serializable
    data class InvestmentPreview(
        val interestAmount: String? = null,
        val dividendsAmount: String? = null,
        val projectedInterestAmount: String? = null,
        val projectedDividendsAmount: String? = null,
        val isInterestCounted: Boolean? = null,
        val isDividendsCounted: Boolean? = null,
        val taxAdvantagedAmount: String? = null,
        val taxYearAccountIncomes: List<AccountIncome?>? = null,
    ) {
        @Serializable
        data class AccountIncome(
            val financeAccountId: String? = null,
            val accountName: String? = null,
            val incomeKind: String? = null,
            val amount: String? = null,
            val isTaxAdvantaged: Boolean? = null,
        )
    }
    /**
     * Investment is the year's investment income from its 1099s, the
     * household's.
     */
    @Serializable
    data class Investment(
        val interestAmount: String? = null,
        val taxExemptInterestAmount: String? = null,
        val ordinaryDividendsAmount: String? = null,
        val qualifiedDividendsAmount: String? = null,
        val capitalGainDistributionsAmount: String? = null,
        val shortTermGainAmount: String? = null,
        val longTermGainAmount: String? = null,
        val federalIncomeTaxWithheldAmount: String? = null,
        val foreignTaxPaidAmount: String? = null,
        val isCostBasisMissing: Boolean? = null,
        val taxFormIds: List<String>? = null,
    )
}

/**
 * TaxDocumentAddress is the address paystubs and W-2s are mailed to,
 * empty without a mailbox address, and whether the server reads PDFs.
 */
@Serializable
data class TaxDocumentAddress(
    val taxDocumentAddress: String? = null,
    val canReadPdfs: Boolean? = null,
)

/**
 * TaxYearSetting is what a tax year's estimate takes that no document
 * says.
 */
@Serializable
data class TaxYearSetting(
    val taxYear: Int? = null,
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
)

/**
 * TaxDocumentReading is the uploads queued to be read as paystubs or tax
 * forms, and the job reading each.
 */
@Serializable
data class TaxDocumentReading(
    val agentAttachmentIds: List<String>? = null,
    val agentJobIds: List<String>? = null,
)
