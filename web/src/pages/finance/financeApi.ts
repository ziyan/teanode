// The questions the Finance page and the agent page's Finance tab ask the
// server, and the shapes of the answers.
// Every one is an operation of FinanceQuery or FinanceMutation, the same
// ones the command line's teanode finance and the agent's finance tool
// call, so the three show the same numbers. Amounts arrive as decimal
// strings, exact as the database keeps them; they become numbers only to
// be drawn or formatted.

// 'statement' is the finance source of imported statements: OFX files
// mailed in or uploaded, for the accounts no provider reaches.
export type ProviderKind = 'plaid' | 'simplefin' | 'statement'

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
  providerTransactionId: string
  postedOn: string
  transactedAt?: string | null
  amount: string
  currencyCode: string
  description: string
  merchantName?: string | null
  providerCategoryPrimary?: string | null
  providerCategoryDetailed?: string | null
  isPending: boolean
  pendingProviderTransactionId?: string | null
  // The provider's whole object for the transaction, as it arrived: text
  // from outside, to be shown and never interpreted.
  providerMetadata?: unknown
  // A transaction is a transfer between the person's own accounts when its
  // spending category is the transfer category (isTransfer on it), and
  // categorizedBy says what put it there.
  spendingCategoryId?: string | null
  categorizedBy?: string | null
  categorizationConfidence?: string | null
  // A mirrored copy: the same charge reported again on another account of
  // the same finance source. It names the copy that is counted and is
  // itself left out of every total. duplicateDecidedBy says what decided,
  // mirror_detection or person (the person counted it).
  duplicateOfTransactionId?: string | null
  duplicateDecidedBy?: string | null
  createdAt: string
  modifiedAt: string
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
  // The built-in transfer category: neither spending nor income, and it
  // cannot be deleted.
  isTransfer: boolean
}

export type SpendingRule = {
  id: string
  matchText: string
  financeAccountId?: string | null
  minimumAmount?: string | null
  maximumAmount?: string | null
  spendingCategoryId: string
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
  // The part of the budget for the days so far, and how many months it was
  // in force: one for a month, and for a year the months that had it,
  // which budgetAmount adds up.
  budgetToDateAmount: string
  budgetedMonthCount: number
  spendingAmount: string
  spendingBySameDayLastMonthAmount: string
  fixedChargesDueAmount: string
  expectedRepeatCharges: ExpectedRepeatCharge[]
  projectedAmount: string
  budgetPace: BudgetPace
  unconvertedSpending: CurrencyAmount[]
}

// A merchant that charged a spending category in each of the last three
// full months and has not yet this month, at the amount it is expected to
// charge: what fixedChargesDueAmount adds up.
export type ExpectedRepeatCharge = { merchantName: string; expectedAmount: string; currencyCode: string }

// How an income budget is going: falling short is the bad direction, so it
// has words of its own rather than a BudgetPace.
export type IncomePace = 'behind' | 'on_track' | 'ahead'

export type IncomeCategoryBudgetStatus = {
  spendingCategoryId: string
  spendingCategoryName: string
  budgetAmount: string
  currencyCode: string
  budgetedMonthCount: number
  incomeAmount: string
  incomeBySameDayLastMonthAmount: string
  expectedByTodayAmount: string
  projectedAmount: string
  incomePace: IncomePace
  unconvertedIncome: CurrencyAmount[]
}

// A month's budgets, or a year's: then month is empty, year is set, and
// each row adds up the months of the year that had its budget.
export type BudgetStatus = {
  month: string
  asOf: string
  dayOfMonth: number
  daysInMonth: number
  year: string
  monthsElapsedCount: number
  dayOfYear: number
  daysInYear: number
  spendingCategories: SpendingCategoryBudgetStatus[]
  incomeCategories: IncomeCategoryBudgetStatus[]
}

export type SavingPace = 'behind' | 'on_track' | 'ahead'

// A month's saving in the reporting currency: income budgets less spending
// budgets, against income less spending so far and where it is heading.
export type SavingSummary = {
  month: string
  asOf: string
  dayOfMonth: number
  daysInMonth: number
  year: string
  monthsElapsedCount: number
  dayOfYear: number
  daysInYear: number
  // The months with a budget in force, which a year's saving counts alone.
  budgetedMonths: string[]
  budgetedMonthCount: number
  budgetedMonthsElapsedCount: number
  reportingCurrencyCode: string
  incomeBudgetCount: number
  spendingBudgetCount: number
  expectedIncomeAmount: string
  expectedSpendingAmount: string
  expectedSavingAmount: string
  incomeAmount: string
  spendingAmount: string
  savingAmount: string
  projectedIncomeAmount: string
  projectedSpendingAmount: string
  projectedSavingAmount: string
  savingDifferenceAmount: string
  savingPace: SavingPace
  unconvertedCurrencyCodes: string[]
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
  // A holding's quantity, the price of one and what the whole cost, as the
  // provider reported them on the day; none on anything else.
  heldQuantity?: string | null
  unitPrice?: string | null
  costBasis?: string | null
}

export type SecurityKind =
  'cash' | 'cryptocurrency' | 'derivative' | 'equity' | 'etf' | 'fixed_income' | 'loan' | 'mutual_fund' | 'other'

// FinanceSecurity is something an investment account can hold, as the
// provider that reported it describes it.
export type FinanceSecurity = {
  id: string
  tickerSymbol?: string | null
  securityName: string
  securityKind: SecurityKind | string
  currencyCode?: string | null
  closePrice?: string | null
  closePriceOn?: string | null
}

export type Asset = {
  id: string
  assetName: string
  assetKind: string
  isLiability: boolean
  currencyCode: string
  financeAccountId?: string | null
  // Set on a holding: an asset a finance account values for one security.
  financeSecurityId?: string | null
  financeSecurity?: FinanceSecurity | null
  valuationSource: string
  estimateDescription?: string | null
  isEstimateAllowed: boolean
  closedOn?: string | null
  latestValuation?: AssetValuation | null
}

export type AssetHistory = { asset: Asset; assetValuations: AssetValuation[] }

export type TradeKind = 'buy' | 'sell' | 'cancel' | 'transfer'

// FinanceTrade is one trade in an investment account. Its amount is the
// cash the trade moved in the account: negative when cash left it, as it
// does for a buy.
export type FinanceTrade = {
  id: string
  financeAccountId: string
  financeSecurityId?: string | null
  financeSecurity?: FinanceSecurity | null
  providerTradeId: string
  tradedOn: string
  tradeKind: TradeKind | string
  tradeSubkind?: string | null
  tradedQuantity?: string | null
  unitPrice?: string | null
  tradeAmount: string
  feeAmount?: string | null
  currencyCode: string
  description: string
}

export type FinanceTradePage = {
  financeTrades: FinanceTrade[]
  nextCursor?: string | null
}

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
  targetMeasure: 'cash_flow' | 'asset_value' | 'net_worth'
  startingAmount?: string | null
  startedOn: string
  closedOn?: string | null
  assetIds: string[]
  // Finance accounts counted whole: every asset each values, holdings
  // bought after the target started included.
  financeAccountIds: string[]
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
export const LINK_SIMPLEFIN = `mutation ($setupToken: String!) { LinkSimpleFIN(setupToken: $setupToken) { id } }`

// Bring in a provider connection made elsewhere: a Plaid access token of a
// link made with this server's Plaid keys, or a SimpleFIN access URL
// already claimed.
export const IMPORT_FINANCE_CREDENTIAL = `mutation ($providerKind: String!, $credential: String!, $institutionName: String) {
  ImportFinanceCredential(providerKind: $providerKind, credential: $credential, institutionName: $institutionName) { id }
}`

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

// --- imported statements -------------------------------------------------

// What one import of statement files did.
export type FinanceStatementImport = {
  importedAt: string
  statementImportOrigin: 'mail' | 'upload' | 'message'
  statementFileNames: string[]
  addedTransactionCount: number
  updatedTransactionCount: number
  unchangedTransactionCount: number
  skippedTransactionCount: number
  transactionWithoutFitIdCount: number
  financeAccountIds: string[]
  financeAccountNames: string[]
  importErrorMessage?: string | null
}

// The address to mail statements to, whether importing is on, and the
// last import. Asking makes the address the first time.
export type StatementImport = {
  sourceId: string
  importAddress?: string | null
  isEnabled: boolean
  maximumStatementBytes: number
  lastStatementImport?: FinanceStatementImport | null
}

const STATEMENT_IMPORT_RESULT_FIELDS = `importedAt statementImportOrigin statementFileNames addedTransactionCount
  updatedTransactionCount unchangedTransactionCount skippedTransactionCount transactionWithoutFitIdCount
  financeAccountIds financeAccountNames importErrorMessage`

const STATEMENT_IMPORT_FIELDS = `sourceId importAddress isEnabled maximumStatementBytes
  lastStatementImport { ${STATEMENT_IMPORT_RESULT_FIELDS} }`

export const STATEMENT_IMPORT = `query { StatementImport { ${STATEMENT_IMPORT_FIELDS} } }`

// A file reaches the import the way a file reaches the agent: uploaded to
// its attachments, then named by id.
export const IMPORT_STATEMENT = `mutation ($agentAttachmentId: String) {
  ImportStatement(agentAttachmentId: $agentAttachmentId) { ${STATEMENT_IMPORT_RESULT_FIELDS} }
}`

export const REGENERATE_STATEMENT_IMPORT_ADDRESS = `mutation { RegenerateStatementImportAddress { ${STATEMENT_IMPORT_FIELDS} } }`

export const AGENT_ATTACHMENTS_PATH = '/api/v1/agent/attachments'

// --- accounts and transactions ---------------------------------------------

export const FINANCE_ACCOUNTS = `query { FinanceAccounts { ${ACCOUNT_FIELDS} } }`

// Where the limit a card's usage is measured against comes from: the
// provider, what is owed plus the credit available, or nowhere.
export type CreditLimitSource = 'provider' | 'derived' | 'unknown'

// One credit card's owed amount against its credit limit, in its own
// currency and in the reporting currency. A usage share is a fraction:
// 0.25 is a quarter of the limit used.
export type CreditCardUsage = {
  financeAccountId: string
  accountName: string
  accountMask?: string | null
  institutionName?: string | null
  currencyCode: string
  owedAmount?: string | null
  creditLimitAmount?: string | null
  creditLimitSource: CreditLimitSource
  usageShare?: number | null
  convertedOwedAmount?: string | null
  convertedCreditLimitAmount?: string | null
}

// What the credit cards owe against their limits. The totals count only
// the cards whose limit and balance are known, in the reporting currency;
// the rest are counted in leftOutCardCount.
export type CreditUsage = {
  reportingCurrencyCode?: string | null
  unconvertedCurrencyCodes: string[]
  totalOwedAmount: string
  totalCreditLimitAmount: string
  usageShare?: number | null
  leftOutCardCount: number
  leftOutOwedAmount: string
  creditCards: CreditCardUsage[]
}

export const CREDIT_USAGE = `query {
  CreditUsage { reportingCurrencyCode unconvertedCurrencyCodes totalOwedAmount totalCreditLimitAmount usageShare
    leftOutCardCount leftOutOwedAmount
    creditCards { financeAccountId accountName accountMask institutionName currencyCode owedAmount creditLimitAmount
      creditLimitSource usageShare convertedOwedAmount convertedCreditLimitAmount } }
}`

// Everything about a finance transaction, the provider's own object too:
// the details dialog shows it all, and there is no query for one
// transaction to fetch it with when the dialog opens.
const TRANSACTION_FIELDS = `id financeAccountId providerTransactionId postedOn transactedAt amount currencyCode
  description merchantName providerCategoryPrimary providerCategoryDetailed isPending pendingProviderTransactionId
  providerMetadata spendingCategoryId categorizedBy categorizationConfidence
  duplicateOfTransactionId duplicateDecidedBy createdAt modifiedAt`

export const FINANCE_TRANSACTIONS = `query ($from: String, $to: String, $financeAccountId: String, $text: String,
  $spendingCategoryId: String, $isUncategorized: Boolean, $duplicateOfTransactionId: String,
  $financeTransactionIds: [String!], $limit: Int, $after: String) {
  FinanceTransactions(from: $from, to: $to, financeAccountId: $financeAccountId, text: $text,
    spendingCategoryId: $spendingCategoryId, isUncategorized: $isUncategorized,
    duplicateOfTransactionId: $duplicateOfTransactionId, financeTransactionIds: $financeTransactionIds,
    limit: $limit, after: $after) {
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

// CATEGORIZE_TRANSACTIONS gives several transactions one spending category
// as the person's choice, all or none, at most MAXIMUM_CATEGORIZED_TRANSACTION_COUNT
// at a time, and saves exactly the spending rules given: the ones
// PROPOSE_SPENDING_RULES proposed and the person confirmed.
export const CATEGORIZE_TRANSACTIONS = `mutation ($financeTransactionIds: [String!]!, $spendingCategoryId: String,
  $spendingRules: [ConfirmedSpendingRuleInput!]) {
  CategorizeTransactions(financeTransactionIds: $financeTransactionIds, spendingCategoryId: $spendingCategoryId,
    spendingRules: $spendingRules) {
    financeTransactions { ${TRANSACTION_FIELDS} }
    spendingRules { id matchText }
  }
}`

// PROPOSE_SPENDING_RULES is the spending rules to offer for a whole
// selection at once, at most MAXIMUM_PROPOSED_TRANSACTION_COUNT
// transactions, and what it left out.
export const PROPOSE_SPENDING_RULES = `query ($financeTransactionIds: [String!]!, $spendingCategoryId: String!) {
  ProposeSpendingRules(financeTransactionIds: $financeTransactionIds, spendingCategoryId: $spendingCategoryId) {
    spendingRuleProposals {
      matchText spendingCategoryId financeTransactionCount changedTransactionCount
      aheadOfSpendingRule { id matchText spendingCategoryId }
    }
    tooGenericMatchTextCount changingNumberMatchTextCount overLimitMatchTextCount
  }
}`

// The server's limit on one CATEGORIZE_TRANSACTIONS.
export const MAXIMUM_CATEGORIZED_TRANSACTION_COUNT = 500

// The server's limit on one PROPOSE_SPENDING_RULES, which saves nothing and
// so takes a whole selection.
export const MAXIMUM_PROPOSED_TRANSACTION_COUNT = 5000

// The most spending rules one PROPOSE_SPENDING_RULES offers.
export const MAXIMUM_SPENDING_RULE_PROPOSAL_COUNT = 50

export type SpendingRuleProposal = {
  matchText: string
  spendingCategoryId: string
  financeTransactionCount: number
  changedTransactionCount: number
  aheadOfSpendingRule?: { id: string; matchText: string; spendingCategoryId: string } | null
}

export type SpendingRuleProposals = {
  spendingRuleProposals: SpendingRuleProposal[]
  tooGenericMatchTextCount: number
  changingNumberMatchTextCount: number
  overLimitMatchTextCount: number
}

// A proposed spending rule as CATEGORIZE_TRANSACTIONS takes it back.
export type ConfirmedSpendingRule = { matchText: string; spendingCategoryId: string; aheadOfSpendingRuleId?: string }

export const COUNT_TRANSACTION = `mutation ($financeTransactionId: String!) {
  CountTransaction(financeTransactionId: $financeTransactionId) { ${TRANSACTION_FIELDS} }
}`

export const UNDO_COUNT_TRANSACTION = `mutation ($financeTransactionId: String!) {
  UndoCountTransaction(financeTransactionId: $financeTransactionId) { ${TRANSACTION_FIELDS} }
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

export const BUDGET_STATUS = `query ($month: String, $year: String) {
  BudgetStatus(month: $month, year: $year) { month asOf dayOfMonth daysInMonth year monthsElapsedCount dayOfYear daysInYear
    spendingCategories { spendingCategoryId spendingCategoryName budgetAmount currencyCode budgetToDateAmount
      budgetedMonthCount spendingAmount
      spendingBySameDayLastMonthAmount fixedChargesDueAmount projectedAmount budgetPace
      expectedRepeatCharges { merchantName expectedAmount currencyCode }
      unconvertedSpending { currencyCode amount } }
    incomeCategories { spendingCategoryId spendingCategoryName budgetAmount currencyCode budgetedMonthCount incomeAmount
      incomeBySameDayLastMonthAmount expectedByTodayAmount projectedAmount incomePace
      unconvertedIncome { currencyCode amount } } }
}`

export const SAVING_SUMMARY = `query ($month: String, $year: String) {
  SavingSummary(month: $month, year: $year) { month asOf dayOfMonth daysInMonth year monthsElapsedCount dayOfYear
    daysInYear budgetedMonths budgetedMonthCount budgetedMonthsElapsedCount reportingCurrencyCode incomeBudgetCount
    spendingBudgetCount expectedIncomeAmount expectedSpendingAmount expectedSavingAmount incomeAmount spendingAmount
    savingAmount projectedIncomeAmount projectedSpendingAmount projectedSavingAmount savingDifferenceAmount savingPace
    unconvertedCurrencyCodes }
}`

export const CASH_FLOW = `query ($fromMonth: String, $toMonth: String) {
  CashFlow(fromMonth: $fromMonth, toMonth: $toMonth) { fromMonth toMonth reportingCurrencyCode unconvertedCurrencyCodes
    cashFlowMonths { cashFlowMonth incomeAmount spendingAmount netAmount } }
}`

// --- budgets, spending categories and spending rules ------------------------

export const SPENDING_CATEGORIES = `query {
  SpendingCategories { id spendingCategoryName parentSpendingCategoryId isIncome isHidden isTransfer }
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
  SpendingRules { id matchText financeAccountId minimumAmount maximumAmount spendingCategoryId rulePriority }
}`

export const CREATE_SPENDING_RULE = `mutation ($matchText: String!, $financeAccountId: String, $minimumAmount: String,
  $maximumAmount: String, $spendingCategoryId: String!, $rulePriority: Int) {
  CreateSpendingRule(matchText: $matchText, financeAccountId: $financeAccountId, minimumAmount: $minimumAmount,
    maximumAmount: $maximumAmount, spendingCategoryId: $spendingCategoryId, rulePriority: $rulePriority) { id }
}`

export const UPDATE_SPENDING_RULE = `mutation ($spendingRuleId: String!, $matchText: String, $financeAccountId: String,
  $minimumAmount: String, $maximumAmount: String, $spendingCategoryId: String, $rulePriority: Int) {
  UpdateSpendingRule(spendingRuleId: $spendingRuleId, matchText: $matchText, financeAccountId: $financeAccountId,
    minimumAmount: $minimumAmount, maximumAmount: $maximumAmount, spendingCategoryId: $spendingCategoryId,
    rulePriority: $rulePriority) { id }
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
  evidenceUrls heldQuantity unitPrice costBasis`

const SECURITY_FIELDS = `id tickerSymbol securityName securityKind currencyCode closePrice closePriceOn`

const ASSET_FIELDS = `id assetName assetKind isLiability currencyCode financeAccountId financeSecurityId
  financeSecurity { ${SECURITY_FIELDS} } valuationSource estimateDescription isEstimateAllowed closedOn`

export const NET_WORTH = `query ($from: String, $to: String) {
  NetWorth(from: $from, to: $to) { from to reportingCurrencyCode unconvertedCurrencyCodes
    convertedNetWorthPoints { netWorthOn netWorthAmount } }
}`

export const ASSETS = `query { Assets { ${ASSET_FIELDS} latestValuation { ${VALUATION_FIELDS} } } }`

export const ASSET_HISTORY = `query ($assetId: String!) {
  AssetHistory(assetId: $assetId) { asset { ${ASSET_FIELDS} } assetValuations { ${VALUATION_FIELDS} } }
}`

// An asset is made with its first value, when one is given, in the same
// operation: the value is recorded with the asset's valuation source.
export const CREATE_ASSET = `mutation ($assetName: String!, $assetKind: String!, $currencyCode: String!,
  $valuationSource: String, $estimateDescription: String, $isEstimateAllowed: Boolean, $value: String,
  $valuedOn: String) {
  CreateAsset(assetName: $assetName, assetKind: $assetKind, currencyCode: $currencyCode,
    valuationSource: $valuationSource, estimateDescription: $estimateDescription,
    isEstimateAllowed: $isEstimateAllowed, value: $value, valuedOn: $valuedOn) { id }
}`

export const UPDATE_ASSET = `mutation ($assetId: String!, $assetName: String, $assetKind: String, $currencyCode: String,
  $valuationSource: String, $estimateDescription: String, $isEstimateAllowed: Boolean) {
  UpdateAsset(assetId: $assetId, assetName: $assetName, assetKind: $assetKind, currencyCode: $currencyCode,
    valuationSource: $valuationSource, estimateDescription: $estimateDescription,
    isEstimateAllowed: $isEstimateAllowed) { id }
}`

export const CLOSE_ASSET = `mutation ($assetId: String!, $closedOn: String, $shouldReopen: Boolean) {
  CloseAsset(assetId: $assetId, closedOn: $closedOn, shouldReopen: $shouldReopen) { id }
}`

export const DELETE_ASSET = `mutation ($assetId: String!) { DeleteAsset(assetId: $assetId) }`

export const RECORD_VALUATION = `mutation ($assetId: String!, $value: String!, $valuedOn: String, $valuationNote: String) {
  RecordValuation(assetId: $assetId, value: $value, valuedOn: $valuedOn, valuationNote: $valuationNote) { id }
}`

export const DELETE_VALUATION = `mutation ($valuationId: String!) { DeleteValuation(valuationId: $valuationId) }`

// --- trades ----------------------------------------------------------------

const TRADE_FIELDS = `id financeAccountId financeSecurityId financeSecurity { ${SECURITY_FIELDS} } providerTradeId tradedOn
  tradeKind tradeSubkind tradedQuantity unitPrice tradeAmount feeAmount currencyCode description`

export const FINANCE_TRADES = `query ($from: String, $to: String, $financeAccountId: String, $financeSecurityId: String,
  $limit: Int, $after: String) {
  FinanceTrades(from: $from, to: $to, financeAccountId: $financeAccountId, financeSecurityId: $financeSecurityId,
    limit: $limit, after: $after) {
    financeTrades { ${TRADE_FIELDS} }
    nextCursor
  }
}`

// --- savings targets -------------------------------------------------------

export const SAVINGS_TARGETS = `query {
  SavingsTargets {
    savingsTarget { id savingsTargetName targetAmount currencyCode targetOn targetMeasure startingAmount startedOn
      closedOn assetIds financeAccountIds }
    savingsTargetProgress { savedAmount remainingAmount monthsLeftCount requiredMonthlyAmount isBehind
      unconvertedCurrencyCodes }
  }
}`

export const CREATE_SAVINGS_TARGET = `mutation ($savingsTargetName: String!, $targetAmount: String!, $currencyCode: String,
  $targetOn: String!, $targetMeasure: String, $startingAmount: String, $startedOn: String, $assetIds: [String!],
  $financeAccountIds: [String!]) {
  CreateSavingsTarget(savingsTargetName: $savingsTargetName, targetAmount: $targetAmount, currencyCode: $currencyCode,
    targetOn: $targetOn, targetMeasure: $targetMeasure, startingAmount: $startingAmount, startedOn: $startedOn,
    assetIds: $assetIds, financeAccountIds: $financeAccountIds) { savingsTarget { id } }
}`

export const UPDATE_SAVINGS_TARGET = `mutation ($savingsTargetId: String!, $savingsTargetName: String,
  $targetAmount: String, $targetOn: String, $targetMeasure: String, $startingAmount: String, $assetIds: [String!],
  $financeAccountIds: [String!]) {
  UpdateSavingsTarget(savingsTargetId: $savingsTargetId, savingsTargetName: $savingsTargetName,
    targetAmount: $targetAmount, targetOn: $targetOn, targetMeasure: $targetMeasure, startingAmount: $startingAmount,
    assetIds: $assetIds, financeAccountIds: $financeAccountIds) {
    savingsTarget { id } }
}`

export const CLOSE_SAVINGS_TARGET = `mutation ($savingsTargetId: String!, $shouldReopen: Boolean) {
  CloseSavingsTarget(savingsTargetId: $savingsTargetId, shouldReopen: $shouldReopen) { savingsTarget { id } }
}`

// --- settings --------------------------------------------------------------

// The currency totals are shown in: the person's choice, or when they made
// none the currency of their first finance account, else of their first
// asset. Empty only when there is nothing to take one from.
export const REPORTING_CURRENCY = `query { ReportingCurrency { reportingCurrencyCode isChosen } }`

export type ReportingCurrencyAnswer = { ReportingCurrency: { reportingCurrencyCode: string; isChosen: boolean } }

// The person's own time zone, from their agent: a month on the Finance page
// is the person's month, whatever zone the browser is in.
export const PERSON_ZONE = `query { ReadAgent { timezone } }`

export type PersonZoneAnswer = { ReadAgent: { timezone?: string | null } | null }

// An empty currency code clears the choice.
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

// isHolding says an asset is a holding: one a finance account values for
// one security, with a quantity and a price on each day.
export function isHolding(asset: Pick<Asset, 'financeSecurityId'>): boolean {
  return Boolean(asset.financeSecurityId)
}

// formatQuantity is a number of shares or units the way a person reads
// one: grouped, and to four places, since the eight the server keeps are
// noise on a share count. Under one unit, as a fraction of a coin often
// is, it keeps all eight so the quantity does not read as zero. Nothing,
// or something that is not a number, is a dash.
export function formatQuantity(decimal?: string | null, locale?: string): string {
  if (!hasAmount(decimal)) return '—'
  const quantity = Number(decimal)
  return new Intl.NumberFormat(locale, {
    minimumFractionDigits: 0,
    maximumFractionDigits: quantity !== 0 && Math.abs(quantity) < 1 ? 8 : 4,
  }).format(quantity)
}

// isDecimal says what somebody typed is an amount the server will take.
export function isDecimal(typed: string): boolean {
  return /^-?\d+(\.\d{1,4})?$/.test(typed.trim())
}

// The zone the Finance page's days and months are in: the person's own,
// which their agent keeps, set by the page once it has read it. Empty until
// then, which is the browser's zone. The server answers "this month" in the
// person's zone too, so a default range drawn in the browser's would be a
// different month for somebody travelling.
let personZone = ''

export function setPersonZone(zone: string) {
  personZone = zone
}

// isoDay is a calendar date as the server writes it, from its parts as
// they stand; for calendar arithmetic, not for "now".
export function isoDay(day: Date): string {
  const month = String(day.getMonth() + 1).padStart(2, '0')
  const date = String(day.getDate()).padStart(2, '0')
  return `${day.getFullYear()}-${month}-${date}`
}

export function isoMonth(day: Date): string {
  return isoDay(day).slice(0, 7)
}

// personToday is today in the person's zone, 2006-01-02.
export function personToday(): string {
  if (personZone) {
    try {
      // The Canadian English form is the one written year first.
      return new Intl.DateTimeFormat('en-CA', {
        timeZone: personZone,
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
      }).format(new Date())
    } catch {
      // A zone the browser does not know: the browser's own.
    }
  }
  return isoDay(new Date())
}

// personMonth is this month in the person's zone, 2006-01.
export function personMonth(): string {
  return personToday().slice(0, 7)
}

// daysBefore is the day a number of days before another, both 2006-01-02.
export function daysBefore(day: string, count: number): string {
  const [year, month, date] = day.split('-').map(Number)
  return isoDay(new Date(year, month - 1, date - count))
}

// monthBefore is the month a number of months before another, 2006-01.
export function monthBefore(month: string, count: number): string {
  const [year, number] = month.split('-').map(Number)
  return isoMonth(new Date(year, number - 1 - count, 1))
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
