// The operations behind the dialogs that record by hand what the agent can
// also read: a receipt, a paystub, a tax form, and the rows of an account's
// statement. Each is the operation teanode finance and the agent's finance
// tool call, with the same arguments and the same names, so the three
// record the same thing. Amounts are decimal strings, sent as typed.

import { ReceiptMatchCandidate } from './financeApi'

// --- exchange rate ---------------------------------------------------------

export type CurrencyPairRate = {
  fromCurrencyCode: string
  toCurrencyCode: string
  rate: string
  rateOn: string
  rateSource: string
}

export const EXCHANGE_RATE = `query ($fromCurrencyCode: String!, $toCurrencyCode: String!, $rateOn: String) {
  ExchangeRate(fromCurrencyCode: $fromCurrencyCode, toCurrencyCode: $toCurrencyCode, rateOn: $rateOn) {
    fromCurrencyCode toCurrencyCode rate rateOn rateSource
  }
}`

// --- receipt ---------------------------------------------------------------

export type ReceiptLineKind = 'item' | 'discount' | 'tax' | 'fee' | 'tip'

export const RECEIPT_LINE_KINDS: ReceiptLineKind[] = ['item', 'discount', 'tax', 'fee', 'tip']

export type ReceiptLineInput = {
  lineNumber?: number
  receiptLineKind: ReceiptLineKind
  description?: string
  quantity?: string
  quantityUnit?: string
  unitPriceAmount?: string
  lineAmount: string
  taxClassCode?: string
  discountedLineNumber?: number
}

const RECEIPT_VARIABLES = `($agentAttachmentId: String, $merchantName: String!, $merchantReceiptNumber: String, $purchasedOn: String, $purchasedAt: String, $currencyCode: String!, $subtotalAmount: String, $totalAmount: String!, $paymentAccountMask: String, $receiptLines: [ReceiptLineInput!]!, $financeTransactionId: String, $isUnbalancedAccepted: Boolean)`

const RECEIPT_ARGUMENTS = `(agentAttachmentId: $agentAttachmentId, merchantName: $merchantName, merchantReceiptNumber: $merchantReceiptNumber, purchasedOn: $purchasedOn, purchasedAt: $purchasedAt, currencyCode: $currencyCode, subtotalAmount: $subtotalAmount, totalAmount: $totalAmount, paymentAccountMask: $paymentAccountMask, receiptLines: $receiptLines, financeTransactionId: $financeTransactionId, isUnbalancedAccepted: $isUnbalancedAccepted)`

const CANDIDATE_FIELDS = `financeTransactionId matchedAmount isExactAmount isSameAccount isMerchantNameShared dayDistanceCount
    isAutomatic matchConfidence financeTransaction { id postedOn amount currencyCode description merchantName }`

export type ReceiptEntryPreview = {
  receiptCheckState: 'balanced' | 'unbalanced'
  checkDifferenceAmount: string
  receiptCheckSummary: string
  receiptMatchCandidates: ReceiptMatchCandidate[]
  isReplacing: boolean
}

export type RecordedReceipt = {
  financeReceipt: { id: string; merchantName: string }
  receiptCheckSummary: string
  receiptMatchCandidates: ReceiptMatchCandidate[]
  isReplaced: boolean
  droppedReceiptMatchReasons: string[]
}

export const PREVIEW_RECORD_RECEIPT = `query ${RECEIPT_VARIABLES} {
  PreviewRecordReceipt${RECEIPT_ARGUMENTS} {
    receiptCheckState checkDifferenceAmount receiptCheckSummary isReplacing
    receiptMatchCandidates { ${CANDIDATE_FIELDS} }
  }
}`

export const RECORD_RECEIPT = `mutation ${RECEIPT_VARIABLES} {
  RecordReceipt${RECEIPT_ARGUMENTS} {
    financeReceipt { id merchantName } receiptCheckSummary isReplaced droppedReceiptMatchReasons
    receiptMatchCandidates { ${CANDIDATE_FIELDS} }
  }
}`

// --- paystub ---------------------------------------------------------------

export type PaystubLineKind = 'earning' | 'tax' | 'deduction' | 'reimbursement'

export const PAYSTUB_LINE_KINDS: PaystubLineKind[] = ['earning', 'tax', 'deduction', 'reimbursement']

export const PAYSTUB_TAX_KINDS = [
  'federal_income',
  'social_security',
  'medicare',
  'medicare_additional',
  'state_income',
  'local_income',
  'state_disability',
  'other',
]

export const PAYSTUB_DEDUCTION_KINDS = [
  'retirement_traditional',
  'retirement_roth',
  'hsa',
  'health_premium',
  'fsa_health',
  'fsa_dependent_care',
  'commuter',
  'insurance',
  'other',
]

export type PaystubLineInput = {
  paystubLineKind: PaystubLineKind
  description?: string
  hours?: string
  rateAmount?: string
  currentAmount?: string
  yearToDateAmount?: string
  paystubTaxKind?: string
  taxJurisdictionCode?: string
  currentWagesAmount?: string
  yearToDateWagesAmount?: string
  paystubDeductionKind?: string
  employerCurrentAmount?: string
  employerYearToDateAmount?: string
}

// PAYSTUB_UNBALANCED_REFUSAL is the part of the server's refusal that says a
// paystub's lines do not add up to its printed totals (ErrPaystubUnbalanced).
export const PAYSTUB_UNBALANCED_REFUSAL = 'its lines do not add up to its printed totals'

export const RECORD_PAYSTUB = `mutation ($agentAttachmentId: String!, $employerName: String!, $employerTaxId: String, $employeeName: String!, $employeeTaxIdMask: String, $payDate: String!, $periodStartOn: String, $periodEndOn: String, $currencyCode: String, $grossAmount: String!, $yearToDateGrossAmount: String, $totalTaxAmount: String, $totalDeductionAmount: String, $netAmount: String!, $paystubLines: [PaystubLineInput!]!, $isUnbalancedAccepted: Boolean) {
  RecordPaystub(agentAttachmentId: $agentAttachmentId, employerName: $employerName, employerTaxId: $employerTaxId, employeeName: $employeeName, employeeTaxIdMask: $employeeTaxIdMask, payDate: $payDate, periodStartOn: $periodStartOn, periodEndOn: $periodEndOn, currencyCode: $currencyCode, grossAmount: $grossAmount, yearToDateGrossAmount: $yearToDateGrossAmount, totalTaxAmount: $totalTaxAmount, totalDeductionAmount: $totalDeductionAmount, netAmount: $netAmount, paystubLines: $paystubLines, isUnbalancedAccepted: $isUnbalancedAccepted) {
    id employerName payDate paystubCheckState checkDifferenceAmount
  }
}`

// --- tax form --------------------------------------------------------------

export const TAX_FORM_KINDS = ['w2', '1099_int', '1099_div', '1099_b', '1098']

export type TaxFormEntryInput = {
  boxCode: string
  boxQualifier?: string
  amount?: string
  textValue?: string
  isChecked?: boolean
}

export const RECORD_TAX_FORM = `mutation ($agentAttachmentId: String!, $taxFormKind: String, $taxYear: Int!, $isCorrected: Boolean, $payerName: String!, $payerTaxId: String, $recipientName: String!, $recipientTaxIdMask: String, $taxFormEntries: [TaxFormEntryInput!]!) {
  RecordTaxForm(agentAttachmentId: $agentAttachmentId, taxFormKind: $taxFormKind, taxYear: $taxYear, isCorrected: $isCorrected, payerName: $payerName, payerTaxId: $payerTaxId, recipientName: $recipientName, recipientTaxIdMask: $recipientTaxIdMask, taxFormEntries: $taxFormEntries) {
    financeTaxForm { id taxFormKind taxYear payerName }
    taxFormFindings { taxFormFindingCode isAgreeing expectedAmount printedAmount findingText }
  }
}`

// --- transaction rows ------------------------------------------------------

export type TransactionRowInput = {
  postedOn: string
  description: string
  amount: string
}

export type TransactionRowPreview = {
  rowNumber: number
  postedOn: string
  description: string
  amount: string
  hasNearbyStoredTransaction: boolean
}

export type TransactionRowsPreview = {
  financeAccountId?: string | null
  accountName: string
  isNewAccount: boolean
  accountMatch: string
  currencyCode: string
  newTransactionRows: TransactionRowPreview[]
  presentTransactionRows: TransactionRowPreview[]
  firstPostedOn?: string | null
  lastPostedOn?: string | null
  moneyInAmount: string
  moneyOutAmount: string
  verificationSummary: string
}

const TRANSACTION_ROWS_VARIABLES = `($financeAccountId: String, $isNewAccount: Boolean, $institutionName: String!, $accountName: String, $accountNumber: String, $isAccountNumberPartial: Boolean, $statementAccountKind: String!, $currencyCode: String!, $transactionRows: [TransactionRowInput!]!)`

const TRANSACTION_ROWS_ARGUMENTS = `(financeAccountId: $financeAccountId, isNewAccount: $isNewAccount, institutionName: $institutionName, accountName: $accountName, accountNumber: $accountNumber, isAccountNumberPartial: $isAccountNumberPartial, statementAccountKind: $statementAccountKind, currencyCode: $currencyCode, transactionRows: $transactionRows)`

const TRANSACTION_ROW_PREVIEW_FIELDS = `rowNumber postedOn description amount hasNearbyStoredTransaction`

export const PREVIEW_IMPORT_TRANSACTIONS = `query ${TRANSACTION_ROWS_VARIABLES} {
  PreviewImportTransactions${TRANSACTION_ROWS_ARGUMENTS} {
    financeAccountId accountName isNewAccount accountMatch currencyCode
    newTransactionRows { ${TRANSACTION_ROW_PREVIEW_FIELDS} }
    presentTransactionRows { ${TRANSACTION_ROW_PREVIEW_FIELDS} }
    firstPostedOn lastPostedOn moneyInAmount moneyOutAmount verificationSummary
  }
}`

export const IMPORT_TRANSACTIONS = `mutation ${TRANSACTION_ROWS_VARIABLES} {
  ImportTransactions${TRANSACTION_ROWS_ARGUMENTS} {
    importedAt statementImportOrigin statementFileNames addedTransactionCount updatedTransactionCount
    unchangedTransactionCount skippedTransactionCount transactionWithoutFitIdCount financeAccountIds
    financeAccountNames importErrorMessage
  }
}`
