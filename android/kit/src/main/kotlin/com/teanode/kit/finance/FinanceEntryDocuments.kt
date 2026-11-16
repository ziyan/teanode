package com.teanode.kit.finance

/**
 * The GraphQL of this part, the same text as the iPhone app's (ios/TeaNodeKit). The server's tests check the iPhone
 * app's documents against its schema, so a document copied from there is checked too.
 */
object FinanceEntryDocuments {
    const val RECEIPT_VARIABLE_TYPES = $$"""$agentAttachmentId: String, $merchantName: String!, $merchantReceiptNumber: String, $purchasedOn: String,
        $currencyCode: String!, $subtotalAmount: String, $totalAmount: String!, $paymentAccountMask: String,
        $receiptLines: [ReceiptLineInput!]!, $isUnbalancedAccepted: Boolean"""

    const val RECEIPT_ARGUMENTS = $$"""agentAttachmentId: $agentAttachmentId, merchantName: $merchantName, merchantReceiptNumber: $merchantReceiptNumber,
        purchasedOn: $purchasedOn, currencyCode: $currencyCode, subtotalAmount: $subtotalAmount, totalAmount: $totalAmount,
        paymentAccountMask: $paymentAccountMask, receiptLines: $receiptLines, isUnbalancedAccepted: $isUnbalancedAccepted"""

    const val PREVIEW_RECORD_RECEIPT = $$"""query ($${RECEIPT_VARIABLE_TYPES}) {
          PreviewRecordReceipt($${RECEIPT_ARGUMENTS}) {
            receiptCheckState checkDifferenceAmount receiptCheckSummary
            receiptMatchCandidates { financeTransactionId financeTransaction { $${FinanceDocuments.FINANCE_TRANSACTION_FIELDS} } matchedAmount isExactAmount
              isSameAccount isMerchantNameShared dayDistanceCount isAutomatic matchConfidence } isReplacing }
        }"""

    const val RECORD_RECEIPT = $$"""mutation ($${RECEIPT_VARIABLE_TYPES}) {
          RecordReceipt($${RECEIPT_ARGUMENTS}) {
            financeReceipt { $${FinanceDocuments.FINANCE_RECEIPT_FIELDS} } receiptCheckSummary
            receiptMatchCandidates { financeTransactionId financeTransaction { $${FinanceDocuments.FINANCE_TRANSACTION_FIELDS} } matchedAmount isExactAmount
              isSameAccount isMerchantNameShared dayDistanceCount isAutomatic matchConfidence } isReplaced droppedReceiptMatchReasons }
        }"""

    const val RECORD_PAYSTUB = $$"""mutation ($agentAttachmentId: String!, $employerName: String!, $employerTaxId: String, $employeeName: String!,
          $employeeTaxIdMask: String, $payDate: String!, $periodStartOn: String, $periodEndOn: String, $currencyCode: String,
          $grossAmount: String!, $yearToDateGrossAmount: String, $totalTaxAmount: String, $totalDeductionAmount: String,
          $netAmount: String!, $paystubLines: [PaystubLineInput!]!, $isUnbalancedAccepted: Boolean) {
          RecordPaystub(agentAttachmentId: $agentAttachmentId, employerName: $employerName, employerTaxId: $employerTaxId,
            employeeName: $employeeName, employeeTaxIdMask: $employeeTaxIdMask, payDate: $payDate, periodStartOn: $periodStartOn,
            periodEndOn: $periodEndOn, currencyCode: $currencyCode, grossAmount: $grossAmount,
            yearToDateGrossAmount: $yearToDateGrossAmount, totalTaxAmount: $totalTaxAmount,
            totalDeductionAmount: $totalDeductionAmount, netAmount: $netAmount, paystubLines: $paystubLines,
            isUnbalancedAccepted: $isUnbalancedAccepted) { $${FinanceTaxDocuments.FINANCE_PAYSTUB_FIELDS} }
        }"""

    const val RECORD_TAX_FORM = $$"""mutation ($agentAttachmentId: String!, $taxFormKind: String, $taxYear: Int!, $isCorrected: Boolean,
          $payerName: String!, $payerTaxId: String, $recipientName: String!, $recipientTaxIdMask: String,
          $taxFormEntries: [TaxFormEntryInput!]!) {
          RecordTaxForm(agentAttachmentId: $agentAttachmentId, taxFormKind: $taxFormKind, taxYear: $taxYear,
            isCorrected: $isCorrected, payerName: $payerName, payerTaxId: $payerTaxId, recipientName: $recipientName,
            recipientTaxIdMask: $recipientTaxIdMask, taxFormEntries: $taxFormEntries) { $${FinanceTaxDocuments.TAX_FORM_VIEW_FIELDS} }
        }"""

    const val EXCHANGE_RATE = $$"""query ($fromCurrencyCode: String!, $toCurrencyCode: String!) {
          ExchangeRate(fromCurrencyCode: $fromCurrencyCode, toCurrencyCode: $toCurrencyCode) {
            fromCurrencyCode toCurrencyCode rate rateOn rateSource }
        }"""

}
