package com.teanode.kit.finance

/**
 * The GraphQL of this part, the same text as the iPhone app's (ios/TeaNodeKit). The server's tests check the iPhone
 * app's documents against its schema, so a document copied from there is checked too.
 */
object FinanceTaxDocuments {
    const val FINANCE_PAYSTUB_FIELDS = $$"""id agentAttachmentId employerName employerTaxId employeeName employeeTaxIdMask payDate periodStartOn periodEndOn
        currencyCode grossAmount yearToDateGrossAmount totalTaxAmount totalDeductionAmount netAmount paystubCheckState
        checkDifferenceAmount createdAt modifiedAt
        paystubLines { id lineNumber paystubLineKind description hours rateAmount currentAmount yearToDateAmount paystubTaxKind
          taxJurisdictionCode currentWagesAmount yearToDateWagesAmount paystubDeductionKind employerCurrentAmount
          employerYearToDateAmount }"""

    const val TAX_FORM_VIEW_FIELDS = $$"""financeTaxForm { id agentAttachmentId taxFormKind taxYear isCorrected payerName payerTaxId recipientName
          recipientTaxIdMask createdAt modifiedAt
          taxFormEntries { id entryNumber boxCode boxQualifier amount textValue isChecked } }
        taxFormFindings { taxFormFindingCode isAgreeing expectedAmount printedAmount findingText }"""

    const val TAX_YEAR_FIGURES_FIELDS = $$"""wagesAmount federalIncomeTaxAmount socialSecurityWagesAmount socialSecurityTaxAmount medicareWagesAmount
        medicareTaxAmount dependentCareAmount retirementTraditionalAmount retirementRothAmount hsaAmount healthCoverageAmount
        localIncomeTaxAmount taxYearStateFigures { stateCode stateWagesAmount stateIncomeTaxAmount }"""

    const val FINANCE_PAYSTUBS = $$"""query ($taxYear: Int, $from: String, $to: String, $limit: Int, $offset: Int) {
          FinancePaystubs(taxYear: $taxYear, from: $from, to: $to, limit: $limit, offset: $offset) {
            financePaystubs { $${FINANCE_PAYSTUB_FIELDS} } hasMore totalCount }
        }"""

    const val FINANCE_PAYSTUB = $$"""query ($paystubId: String!) { FinancePaystub(paystubId: $paystubId) { $${FINANCE_PAYSTUB_FIELDS} } }"""

    const val FINANCE_TAX_FORMS = $$"""query ($taxYear: Int, $limit: Int, $offset: Int) {
          FinanceTaxForms(taxYear: $taxYear, limit: $limit, offset: $offset) { taxForms { $${TAX_FORM_VIEW_FIELDS} } hasMore totalCount }
        }"""

    const val FINANCE_TAX_FORM = $$"""query ($taxFormId: String!) { FinanceTaxForm(taxFormId: $taxFormId) { $${TAX_FORM_VIEW_FIELDS} } }"""

    const val TAX_YEAR = $$"""query ($taxYear: Int!, $taxFilingStatus: String) {
          TaxYear(taxYear: $taxYear, taxFilingStatus: $taxFilingStatus) {
            taxYear socialSecurityWageBaseAmount retirementDeferralLimitAmount isYearOver paystubCount taxFormCount
            figures { $${TAX_YEAR_FIGURES_FIELDS} } projectedFigures { $${TAX_YEAR_FIGURES_FIELDS} }
            taxYearEmployments { employeeName employerName employerTaxId taxYearFigureSource taxFormId latestPayDate paystubCount
              payCadenceDayCount isPaystubYearComplete remainingPayCount isRetirementDeferralCapped
              figures { $${TAX_YEAR_FIGURES_FIELDS} } paystubFigures { $${TAX_YEAR_FIGURES_FIELDS} } projectedFigures { $${TAX_YEAR_FIGURES_FIELDS} }
              taxYearComparisons { boxCode boxQualifier boxLabel w2Amount paystubAmount differenceAmount isMatching } }
            taxYearPeople { employeeName excessSocialSecurityTaxAmount figures { $${TAX_YEAR_FIGURES_FIELDS} }
              projectedFigures { $${TAX_YEAR_FIGURES_FIELDS} } }
            taxYearFindings { taxYearFindingCode employeeName employerName findingText paystubIds }
            taxYearInvestmentPreview { interestAmount dividendsAmount projectedInterestAmount projectedDividendsAmount
              isInterestCounted isDividendsCounted taxAdvantagedAmount
              taxYearAccountIncomes { financeAccountId accountName incomeKind amount isTaxAdvantaged } }
            taxYearSetting { taxYear childCount otherDependentCount dependentCareExpenseAmount otherIncomeAmount
              charitableContributionAmount propertyTaxAmount stateIncomeTaxPaidAmount estimatedShortTermGainAmount
              estimatedLongTermGainAmount estimatedGainNote }
            taxYearInvestment { interestAmount taxExemptInterestAmount ordinaryDividendsAmount qualifiedDividendsAmount
              capitalGainDistributionsAmount shortTermGainAmount longTermGainAmount federalIncomeTaxWithheldAmount
              foreignTaxPaidAmount isCostBasisMissing taxFormIds }
            taxYearEstimate { taxFilingStatus isTaxFilingStatusAssumed isProjected payAmount investmentIncomeAmount incomeAmount
              adjustedGrossIncomeAmount preferentialIncomeAmount preferentialIncomeTaxAmount capitalLossDeductionAmount
              foreignTaxCreditAmount netInvestmentIncomeTaxAmount isInvestmentIncomePreviewed otherIncomeAmount deductionAmount
              isItemized itemizedDeductionAmount mortgageInterestAmount stateAndLocalTaxDeductionAmount
              charitableContributionAmount childTaxCreditAmount dependentCareCreditAmount standardDeductionAmount taxableIncomeAmount
              federalIncomeTaxAmount socialSecurityTaxAmount medicareTaxAmount stateIncomeTaxAmount localIncomeTaxAmount
              totalTaxAmount paidTaxAmount upcomingWithheldTaxAmount owingTaxAmount refundAmount remainingIncomeAmount }
          }
        }"""

    const val SET_TAX_YEAR_SETTING = $$"""mutation ($taxYear: Int!, $childCount: Int, $otherDependentCount: Int, $dependentCareExpenseAmount: String,
          $otherIncomeAmount: String, $charitableContributionAmount: String, $propertyTaxAmount: String,
          $stateIncomeTaxPaidAmount: String, $estimatedShortTermGainAmount: String, $estimatedLongTermGainAmount: String,
          $estimatedGainNote: String, $isEstimatedGainCleared: Boolean) {
          SetTaxYearSetting(taxYear: $taxYear, childCount: $childCount, otherDependentCount: $otherDependentCount,
            dependentCareExpenseAmount: $dependentCareExpenseAmount, otherIncomeAmount: $otherIncomeAmount,
            charitableContributionAmount: $charitableContributionAmount, propertyTaxAmount: $propertyTaxAmount,
            stateIncomeTaxPaidAmount: $stateIncomeTaxPaidAmount, estimatedShortTermGainAmount: $estimatedShortTermGainAmount,
            estimatedLongTermGainAmount: $estimatedLongTermGainAmount, estimatedGainNote: $estimatedGainNote,
            isEstimatedGainCleared: $isEstimatedGainCleared) {
            taxYear childCount otherDependentCount dependentCareExpenseAmount otherIncomeAmount charitableContributionAmount
            propertyTaxAmount stateIncomeTaxPaidAmount estimatedShortTermGainAmount estimatedLongTermGainAmount estimatedGainNote
          }
        }"""

    const val TAX_DOCUMENT_ADDRESS = $$"""query { TaxDocumentAddress { taxDocumentAddress canReadPdfs } }"""

    const val REGENERATE_TAX_DOCUMENT_ADDRESS = $$"""mutation { RegenerateTaxDocumentAddress { taxDocumentAddress canReadPdfs } }"""

    const val SET_TAX_FILING_STATUS = $$"""mutation ($taxFilingStatus: String!) { SetTaxFilingStatus(taxFilingStatus: $taxFilingStatus) }"""

    const val READ_TAX_DOCUMENT = $$"""mutation ($agentAttachmentId: String, $mailboxItemId: String) {
          ReadTaxDocument(agentAttachmentId: $agentAttachmentId, mailboxItemId: $mailboxItemId) { agentAttachmentIds agentJobIds }
        }"""

    const val DELETE_PAYSTUB = $$"""mutation ($paystubId: String!) { DeletePaystub(paystubId: $paystubId) }"""

    const val DELETE_TAX_FORM = $$"""mutation ($taxFormId: String!) { DeleteTaxForm(taxFormId: $taxFormId) }"""

}
