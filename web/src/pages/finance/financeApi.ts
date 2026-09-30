// The Finance tab's questions to the server, and the shapes of the answers.
// Every one is an operation of FinanceQuery or FinanceMutation, the same
// ones the command line's teanode finance and the agent's finance tool
// call, so the three show the same numbers. Amounts arrive as decimal
// strings, exact as the database keeps them; they become numbers only to
// be drawn or formatted.

export type ProviderKind = 'plaid' | 'simplefin'

// A provider people may link through. One that needs a browser (Plaid) is
// linked on /finance-link; the other (SimpleFIN) by pasting a setup token.
export type FinanceProvider = { providerKind: ProviderKind; isBrowserRequired: boolean }

export type FinanceAccount = {
  id: string
  sourceId: string
  providerKind: ProviderKind
  institutionName?: string | null
  accountName: string
  accountMask?: string | null
  accountKind: string
  currencyCode: string
  currentBalance?: string | null
  availableBalance?: string | null
  balanceAt?: string | null
  isSignInRequired: boolean
  reportingCurrencyCode?: string | null
  convertedCurrentBalance?: string | null
}

export type FinanceSource = {
  id: string
  name: string
  providerKind: ProviderKind
  institutionName?: string | null
  isEnabled: boolean
  lastRunAt?: string | null
  nextRunAt?: string | null
  lastError?: string | null
  isSignInRequired: boolean
  financeAccounts: FinanceAccount[]
}

export type FinanceTransaction = {
  id: string
  financeAccountId: string
  postedOn: string
  amount: string
  currencyCode: string
  description: string
  merchantName?: string | null
  providerCategoryPrimary?: string | null
  isPending: boolean
  spendingCategoryId?: string | null
  categorizedBy?: string | null
  isTransfer: boolean
}

export type FinanceTransactionPage = {
  financeTransactions: FinanceTransaction[]
  nextCursor?: string | null
}

export type SpendingCategory = {
  id: string
  spendingCategoryName: string
  parentSpendingCategoryId?: string | null
  isIncome: boolean
  isHidden: boolean
}

export type SpendingRule = {
  id: string
  matchText: string
  financeAccountId?: string | null
  minimumAmount?: string | null
  maximumAmount?: string | null
  spendingCategoryId?: string | null
  isTransfer: boolean
  rulePriority: number
}

export type Budget = {
  id: string
  spendingCategoryId: string
  monthlyAmount: string
  currencyCode: string
  effectiveFrom: string
}

export type BudgetPace = 'under' | 'on_track' | 'at_risk' | 'over'

export type CurrencyAmount = { currencyCode: string; amount: string }

export type SpendingCategoryBudgetStatus = {
  spendingCategoryId: string
  spendingCategoryName: string
  budgetAmount: string
  currencyCode: string
  spendingAmount: string
  spendingBySameDayLastMonthAmount: string
  fixedChargesDueAmount: string
  projectedAmount: string
  budgetPace: BudgetPace
  unconvertedSpending: CurrencyAmount[]
}

export type BudgetStatus = {
  month: string
  asOf: string
  dayOfMonth: number
  daysInMonth: number
  spendingCategories: SpendingCategoryBudgetStatus[]
}

// A spending summary's figures are money out and money in, both positive.
export type SpendingSummaryRow = {
  groupKey: string
  groupLabel: string
  currencyCode: string
  moneyOut: string
  moneyIn: string
  financeTransactionCount: number
}

export type CurrencyTotal = {
  currencyCode: string
  moneyOut: string
  moneyIn: string
  financeTransactionCount: number
}

export type ConvertedSummaryRow = { groupKey: string; groupLabel: string; moneyOut: string; moneyIn: string }

export type SpendingSummary = {
  groupBy: string
  spendingSummaryRows: SpendingSummaryRow[]
  currencyTotals: CurrencyTotal[]
  reportingCurrencyCode?: string | null
  convertedSpendingSummaryRows: ConvertedSummaryRow[]
  convertedMoneyOut?: string | null
  convertedMoneyIn?: string | null
  unconvertedCurrencyCodes: string[]
}

export type SpendingDay = { spentOn: string; spendingAmount: string; cumulativeSpendingAmount: string }

export type SpendingByDay = {
  month: string
  compareMonth: string
  reportingCurrencyCode?: string | null
  monthDays: SpendingDay[]
  compareMonthDays: SpendingDay[]
  unconvertedCurrencyCodes: string[]
}

export type CashFlowMonth = {
  cashFlowMonth: string
  incomeAmount: string
  spendingAmount: string
  netAmount: string
}

export type CashFlow = {
  fromMonth: string
  toMonth: string
  reportingCurrencyCode?: string | null
  cashFlowMonths: CashFlowMonth[]
  unconvertedCurrencyCodes: string[]
}

export type AssetValuation = {
  id: string
  assetId: string
  valuedOn: string
  value: string
  currencyCode: string
  valuationSource: string
  estimateLow?: string | null
  estimateHigh?: string | null
  valuationNote?: string | null
  evidenceUrls: string[]
}

export type Asset = {
  id: string
  assetName: string
  assetKind: string
  isLiability: boolean
  currencyCode: string
  financeAccountId?: string | null
  valuationSource: string
  estimateDescription?: string | null
  isEstimateAllowed: boolean
  closedOn?: string | null
  latestValuation?: AssetValuation | null
}

export type AssetHistory = { asset: Asset; assetValuations: AssetValuation[] }

export type NetWorthPoint = { netWorthOn: string; netWorthAmount: string }

export type NetWorth = {
  from: string
  to: string
  reportingCurrencyCode?: string | null
  convertedNetWorthPoints: NetWorthPoint[]
  unconvertedCurrencyCodes: string[]
}

export type SavingsTarget = {
  id: string
  savingsTargetName: string
  targetAmount: string
  currencyCode: string
  targetOn: string
  targetMeasure: 'cash_flow' | 'asset_value'
  startingAmount?: string | null
  startedOn: string
  closedOn?: string | null
  assetIds: string[]
}

export type SavingsTargetProgress = {
  savedAmount: string
  remainingAmount: string
  monthsLeftCount: number
  requiredMonthlyAmount: string
  isBehind: boolean
  unconvertedCurrencyCodes: string[]
}

export type SavingsTargetView = { savingsTarget: SavingsTarget; savingsTargetProgress?: SavingsTargetProgress | null }

export type CurrencyConversion = {
  amount: string
  fromCurrencyCode: string
  convertedAmount: string
  toCurrencyCode: string
  rate: string
  rateOn: string
  rateSource: string
}

// --- linking and the finance sources ---------------------------------------

export const FINANCE_PROVIDERS = `query { FinanceProviders { providerKind isBrowserRequired } }`

const ACCOUNT_FIELDS = `id sourceId providerKind institutionName accountName accountMask accountKind currencyCode
  currentBalance availableBalance balanceAt isSignInRequired reportingCurrencyCode convertedCurrentBalance`

export const FINANCE_SOURCES = `query {
  FinanceSources { id name providerKind institutionName isEnabled lastRunAt nextRunAt lastError isSignInRequired
    financeAccounts { ${ACCOUNT_FIELDS} } }
}`

// Whether the tab is shown: something to link through, or something
// linked already (a provider the operator stopped offering still leaves
// the person's finance sources to look at and delete).
export const FINANCE_PRESENCE = `query { FinanceProviders { providerKind } FinanceSources { id } }`

export const LINK_SIMPLEFIN = `mutation ($setupToken: String!) { LinkSimpleFIN(setupToken: $setupToken) { id } }`

export const CREATE_FINANCE_LINK_TOKEN = `mutation ($sourceId: String) {
  CreateFinanceLinkToken(sourceId: $sourceId) { linkToken sourceId }
}`

export const COMPLETE_FINANCE_LINK = `mutation ($publicToken: String!, $institutionId: String, $institutionName: String) {
  CompleteFinanceLink(publicToken: $publicToken, institutionId: $institutionId, institutionName: $institutionName) { id }
}`

export const COMPLETE_FINANCE_REPAIR = `mutation ($sourceId: String!) { CompleteFinanceRepair(sourceId: $sourceId) { id } }`

// The operations every agent source has, which a finance source is.
export const SYNC_SOURCE = `mutation ($sourceId: String!) { SyncAgentKnowledgeSource(sourceId: $sourceId) }`
export const DELETE_SOURCE = `mutation ($sourceId: String!) { DeleteAgentKnowledgeSource(sourceId: $sourceId) }`
export const SWITCH_SOURCE = `mutation ($sourceId: String, $enabled: Boolean) {
  SaveAgentKnowledgeSource(sourceId: $sourceId, enabled: $enabled) { id }
}`

// --- accounts and transactions ---------------------------------------------

export const FINANCE_ACCOUNTS = `query { FinanceAccounts { ${ACCOUNT_FIELDS} } }`

const TRANSACTION_FIELDS = `id financeAccountId postedOn amount currencyCode description merchantName
  providerCategoryPrimary isPending spendingCategoryId categorizedBy isTransfer`

export const FINANCE_TRANSACTIONS = `query ($from: String, $to: String, $financeAccountId: String, $text: String,
  $isUncategorized: Boolean, $limit: Int, $after: String) {
  FinanceTransactions(from: $from, to: $to, financeAccountId: $financeAccountId, text: $text,
    isUncategorized: $isUncategorized, limit: $limit, after: $after) {
    financeTransactions { ${TRANSACTION_FIELDS} }
    nextCursor
  }
}`

export const CATEGORIZE_TRANSACTION = `mutation ($financeTransactionId: String!, $spendingCategoryId: String,
  $shouldCreateSpendingRule: Boolean) {
  CategorizeTransaction(financeTransactionId: $financeTransactionId, spendingCategoryId: $spendingCategoryId,
    shouldCreateSpendingRule: $shouldCreateSpendingRule) {
    financeTransaction { ${TRANSACTION_FIELDS} }
    spendingRule { id matchText }
  }
}`

export const MARK_TRANSFER = `mutation ($financeTransactionId: String!, $isTransfer: Boolean!) {
  MarkTransfer(financeTransactionId: $financeTransactionId, isTransfer: $isTransfer) { ${TRANSACTION_FIELDS} }
}`

// --- spending --------------------------------------------------------------

export const SPENDING_SUMMARY = `query ($from: String, $to: String, $groupBy: String, $financeAccountId: String) {
  FinanceSpendingSummary(from: $from, to: $to, groupBy: $groupBy, financeAccountId: $financeAccountId) {
    groupBy
    spendingSummaryRows { groupKey groupLabel currencyCode moneyOut moneyIn financeTransactionCount }
    currencyTotals { currencyCode moneyOut moneyIn financeTransactionCount }
    reportingCurrencyCode convertedMoneyOut convertedMoneyIn unconvertedCurrencyCodes
    convertedSpendingSummaryRows { groupKey groupLabel moneyOut moneyIn }
  }
}`

export const SPENDING_BY_DAY = `query ($month: String, $compareMonth: String) {
  SpendingByDay(month: $month, compareMonth: $compareMonth) {
    month compareMonth reportingCurrencyCode unconvertedCurrencyCodes
    monthDays { spentOn spendingAmount cumulativeSpendingAmount }
    compareMonthDays { spentOn spendingAmount cumulativeSpendingAmount }
  }
}`

export const BUDGET_STATUS = `query ($month: String) {
  BudgetStatus(month: $month) { month asOf dayOfMonth daysInMonth
    spendingCategories { spendingCategoryId spendingCategoryName budgetAmount currencyCode spendingAmount
      spendingBySameDayLastMonthAmount fixedChargesDueAmount projectedAmount budgetPace
      unconvertedSpending { currencyCode amount } } }
}`

export const CASH_FLOW = `query ($fromMonth: String, $toMonth: String) {
  CashFlow(fromMonth: $fromMonth, toMonth: $toMonth) { fromMonth toMonth reportingCurrencyCode unconvertedCurrencyCodes
    cashFlowMonths { cashFlowMonth incomeAmount spendingAmount netAmount } }
}`

// --- budgets, spending categories and spending rules ------------------------

export const SPENDING_CATEGORIES = `query {
  SpendingCategories { id spendingCategoryName parentSpendingCategoryId isIncome isHidden }
}`

export const CREATE_SPENDING_CATEGORY = `mutation ($spendingCategoryName: String!, $parentSpendingCategoryId: String,
  $isIncome: Boolean, $isHidden: Boolean) {
  CreateSpendingCategory(spendingCategoryName: $spendingCategoryName, parentSpendingCategoryId: $parentSpendingCategoryId,
    isIncome: $isIncome, isHidden: $isHidden) { id }
}`

export const UPDATE_SPENDING_CATEGORY = `mutation ($spendingCategoryId: String!, $spendingCategoryName: String,
  $parentSpendingCategoryId: String, $isIncome: Boolean, $isHidden: Boolean) {
  UpdateSpendingCategory(spendingCategoryId: $spendingCategoryId, spendingCategoryName: $spendingCategoryName,
    parentSpendingCategoryId: $parentSpendingCategoryId, isIncome: $isIncome, isHidden: $isHidden) { id }
}`

export const DELETE_SPENDING_CATEGORY = `mutation ($spendingCategoryId: String!) {
  DeleteSpendingCategory(spendingCategoryId: $spendingCategoryId)
}`

export const SPENDING_RULES = `query {
  SpendingRules { id matchText financeAccountId minimumAmount maximumAmount spendingCategoryId isTransfer rulePriority }
}`

export const CREATE_SPENDING_RULE = `mutation ($matchText: String!, $financeAccountId: String, $minimumAmount: String,
  $maximumAmount: String, $spendingCategoryId: String, $isTransfer: Boolean, $rulePriority: Int) {
  CreateSpendingRule(matchText: $matchText, financeAccountId: $financeAccountId, minimumAmount: $minimumAmount,
    maximumAmount: $maximumAmount, spendingCategoryId: $spendingCategoryId, isTransfer: $isTransfer,
    rulePriority: $rulePriority) { id }
}`

export const UPDATE_SPENDING_RULE = `mutation ($spendingRuleId: String!, $matchText: String, $financeAccountId: String,
  $minimumAmount: String, $maximumAmount: String, $spendingCategoryId: String, $isTransfer: Boolean, $rulePriority: Int) {
  UpdateSpendingRule(spendingRuleId: $spendingRuleId, matchText: $matchText, financeAccountId: $financeAccountId,
    minimumAmount: $minimumAmount, maximumAmount: $maximumAmount, spendingCategoryId: $spendingCategoryId,
    isTransfer: $isTransfer, rulePriority: $rulePriority) { id }
}`

export const DELETE_SPENDING_RULE = `mutation ($spendingRuleId: String!) { DeleteSpendingRule(spendingRuleId: $spendingRuleId) }`

export const BUDGETS = `query { Budgets { id spendingCategoryId monthlyAmount currencyCode effectiveFrom } }`

export const SET_BUDGET = `mutation ($spendingCategoryId: String!, $monthlyAmount: String!, $currencyCode: String,
  $effectiveFrom: String) {
  SetBudget(spendingCategoryId: $spendingCategoryId, monthlyAmount: $monthlyAmount, currencyCode: $currencyCode,
    effectiveFrom: $effectiveFrom) { id }
}`

// --- net worth -------------------------------------------------------------

const VALUATION_FIELDS = `id assetId valuedOn value currencyCode valuationSource estimateLow estimateHigh valuationNote
  evidenceUrls`

const ASSET_FIELDS = `id assetName assetKind isLiability currencyCode financeAccountId valuationSource
  estimateDescription isEstimateAllowed closedOn`

export const NET_WORTH = `query ($from: String, $to: String) {
  NetWorth(from: $from, to: $to) { from to reportingCurrencyCode unconvertedCurrencyCodes
    convertedNetWorthPoints { netWorthOn netWorthAmount } }
}`

export const ASSETS = `query { Assets { ${ASSET_FIELDS} latestValuation { ${VALUATION_FIELDS} } } }`

export const ASSET_HISTORY = `query ($assetId: String!) {
  AssetHistory(assetId: $assetId) { asset { ${ASSET_FIELDS} } assetValuations { ${VALUATION_FIELDS} } }
}`

export const CREATE_ASSET = `mutation ($assetName: String!, $assetKind: String!, $currencyCode: String!,
  $estimateDescription: String, $isEstimateAllowed: Boolean) {
  CreateAsset(assetName: $assetName, assetKind: $assetKind, currencyCode: $currencyCode,
    estimateDescription: $estimateDescription, isEstimateAllowed: $isEstimateAllowed) { id }
}`

export const UPDATE_ASSET = `mutation ($assetId: String!, $assetName: String, $assetKind: String,
  $estimateDescription: String, $isEstimateAllowed: Boolean) {
  UpdateAsset(assetId: $assetId, assetName: $assetName, assetKind: $assetKind,
    estimateDescription: $estimateDescription, isEstimateAllowed: $isEstimateAllowed) { id }
}`

export const CLOSE_ASSET = `mutation ($assetId: String!, $closedOn: String) { CloseAsset(assetId: $assetId, closedOn: $closedOn) { id } }`

export const DELETE_ASSET = `mutation ($assetId: String!) { DeleteAsset(assetId: $assetId) }`

export const RECORD_VALUATION = `mutation ($assetId: String!, $value: String!, $valuedOn: String, $valuationNote: String) {
  RecordValuation(assetId: $assetId, value: $value, valuedOn: $valuedOn, valuationNote: $valuationNote) { id }
}`

export const DELETE_VALUATION = `mutation ($valuationId: String!) { DeleteValuation(valuationId: $valuationId) }`

// --- savings targets -------------------------------------------------------

export const SAVINGS_TARGETS = `query {
  SavingsTargets {
    savingsTarget { id savingsTargetName targetAmount currencyCode targetOn targetMeasure startingAmount startedOn
      closedOn assetIds }
    savingsTargetProgress { savedAmount remainingAmount monthsLeftCount requiredMonthlyAmount isBehind
      unconvertedCurrencyCodes }
  }
}`

export const CREATE_SAVINGS_TARGET = `mutation ($savingsTargetName: String!, $targetAmount: String!, $currencyCode: String,
  $targetOn: String!, $targetMeasure: String, $startingAmount: String, $startedOn: String, $assetIds: [String!]) {
  CreateSavingsTarget(savingsTargetName: $savingsTargetName, targetAmount: $targetAmount, currencyCode: $currencyCode,
    targetOn: $targetOn, targetMeasure: $targetMeasure, startingAmount: $startingAmount, startedOn: $startedOn,
    assetIds: $assetIds) { savingsTarget { id } }
}`

export const UPDATE_SAVINGS_TARGET = `mutation ($savingsTargetId: String!, $savingsTargetName: String,
  $targetAmount: String, $targetOn: String, $startingAmount: String, $assetIds: [String!]) {
  UpdateSavingsTarget(savingsTargetId: $savingsTargetId, savingsTargetName: $savingsTargetName,
    targetAmount: $targetAmount, targetOn: $targetOn, startingAmount: $startingAmount, assetIds: $assetIds) {
    savingsTarget { id } }
}`

export const CLOSE_SAVINGS_TARGET = `mutation ($savingsTargetId: String!) {
  CloseSavingsTarget(savingsTargetId: $savingsTargetId) { savingsTarget { id } }
}`

// --- settings --------------------------------------------------------------

// The reporting currency is the agent's own setting; empty means the
// currency of the first finance account.
export const REPORTING_CURRENCY = `query { ReadAgent { agent { reportingCurrencyCode } } }`

export type ReportingCurrencyAnswer = { ReadAgent: { agent: { reportingCurrencyCode?: string | null } | null } }

export const SET_REPORTING_CURRENCY = `mutation ($currencyCode: String!) { SetReportingCurrency(currencyCode: $currencyCode) }`

export const CONVERT_CURRENCY = `query ($amount: String!, $fromCurrencyCode: String!, $toCurrencyCode: String!, $rateOn: String) {
  ConvertCurrency(amount: $amount, fromCurrencyCode: $fromCurrencyCode, toCurrencyCode: $toCurrencyCode, rateOn: $rateOn) {
    amount fromCurrencyCode convertedAmount toCurrencyCode rate rateOn rateSource }
}`

// --- helpers ---------------------------------------------------------------

// amountOf turns a decimal string into a number for drawing and
// formatting. Nothing, or something that is not a number, is zero.
export function amountOf(decimal?: string | null): number {
  if (decimal === undefined || decimal === null || decimal === '') return 0
  const parsed = Number(decimal)
  return Number.isFinite(parsed) ? parsed : 0
}

// hasAmount says a decimal string holds a value at all: a balance the
// provider did not report is not a balance of zero.
export function hasAmount(decimal?: string | null): boolean {
  return decimal !== undefined && decimal !== null && decimal !== '' && Number.isFinite(Number(decimal))
}

// isDecimal says what somebody typed is an amount the server will take.
export function isDecimal(typed: string): boolean {
  return /^-?\d+(\.\d{1,4})?$/.test(typed.trim())
}

// The day and the month as the server writes them, in the reader's own
// calendar: a month is the reader's month, not the server's.
export function isoDay(day: Date): string {
  const month = String(day.getMonth() + 1).padStart(2, '0')
  const date = String(day.getDate()).padStart(2, '0')
  return `${day.getFullYear()}-${month}-${date}`
}

export function isoMonth(day: Date): string {
  return isoDay(day).slice(0, 7)
}

export function monthsBefore(day: Date, count: number): Date {
  return new Date(day.getFullYear(), day.getMonth() - count, 1)
}

export function monthLabel(month: string, style: 'short' | 'long' = 'short'): string {
  const day = new Date(`${month.slice(0, 7)}-01T00:00:00`)
  if (Number.isNaN(day.getTime())) return month
  return day.toLocaleDateString(undefined, style === 'short' ? { month: 'short' } : { month: 'long', year: 'numeric' })
}

export function formatDay(day?: string | null): string {
  if (!day) return '—'
  const parsed = new Date(`${day.slice(0, 10)}T00:00:00`)
  if (Number.isNaN(parsed.getTime())) return day
  return parsed.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
}

// The currencies offered where one is chosen: the ones the European
// Central Bank publishes a rate for, which are the ones that convert.
// Anything else can still be typed.
export const CURRENCY_CODES = [
  'USD',
  'EUR',
  'JPY',
  'GBP',
  'CNY',
  'CAD',
  'AUD',
  'CHF',
  'HKD',
  'SGD',
  'NZD',
  'SEK',
  'NOK',
  'DKK',
  'KRW',
  'INR',
  'BRL',
  'MXN',
  'ZAR',
  'PLN',
  'CZK',
  'HUF',
  'RON',
  'BGN',
  'ISK',
  'TRY',
  'ILS',
  'IDR',
  'MYR',
  'PHP',
  'THB',
]
