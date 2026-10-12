import Foundation

// Money is a decimal string, negative for money out, as the server keeps it:
// never a floating-point number, so nothing is rounded on the way.

public struct FinanceAccount: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let sourceId: String?
    public let providerKind: String?
    public let institutionName: String?
    public let accountName: String?
    public let accountMask: String?
    public let accountKind: String?
    public let currencyCode: String?
    public let currentBalance: String?
    public let availableBalance: String?
    public let balanceAt: String?
    public let isSignInRequired: Bool?
    public let reportingCurrencyCode: String?
    public let convertedCurrentBalance: String?

    /// "Checking ··1234".
    public var shownName: String {
        let name = accountName.flatMap { $0.isEmpty ? nil : $0 } ?? institutionName ?? ""
        guard let mask = accountMask, !mask.isEmpty else { return name }
        return "\(name) ··\(mask)"
    }

    public var isFromStatements: Bool { providerKind == "statement" }
}

public struct FinanceSource: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let name: String?
    public let providerKind: String?
    public let institutionName: String?
    public let isEnabled: Bool?
    public let lastRunAt: String?
    public let nextRunAt: String?
    public let lastError: String?
    public let isSignInRequired: Bool?
    public let financeAccounts: [FinanceAccount?]?
}

public struct FinanceProvider: Codable, Hashable, Sendable {
    public let providerKind: String
    public let isBrowserRequired: Bool?
}

public struct FinanceTransaction: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let financeAccountId: String?
    public let postedOn: String?
    public let transactedAt: String?
    public let amount: String?
    public let currencyCode: String?
    public let description: String?
    public let merchantName: String?
    public let providerCategoryPrimary: String?
    public let providerCategoryDetailed: String?
    public let isPending: Bool?
    public let providerTransactionId: String?
    public let pendingProviderTransactionId: String?
    public let spendingCategoryId: String?
    public let categorizedBy: String?
    public let categorizationConfidence: String?
    public let duplicateOfTransactionId: String?
    public let duplicateDecidedBy: String?
    public let annotation: String?
    public let annotatedBy: String?
    public let receiptCount: Int?
    public let createdAt: String?
    public let modifiedAt: String?

    /// The merchant, else the bank's description.
    public var shownName: String {
        if let merchantName, !merchantName.isEmpty { return merchantName }
        return description ?? ""
    }

    public var isDuplicate: Bool { !(duplicateOfTransactionId ?? "").isEmpty }
}

public struct FinanceTransactionPage: Codable, Hashable, Sendable {
    public let financeTransactions: [FinanceTransaction?]
    public let totalCount: Int?
    public let leftOutDuplicateCount: Int?
}

/// FinanceTransactionFilter is what the transaction list is narrowed to.
public struct FinanceTransactionFilter: Hashable, Sendable {
    public var from = ""
    public var to = ""
    public var financeAccountId = ""
    public var spendingCategoryId = ""
    public var text = ""
    public var isUncategorized = false
    public var isDuplicateIncluded = false

    public init() {}

    var variables: [String: JSONValue] {
        var variables: [String: JSONValue] = [:]
        for (key, value) in [("from", from), ("to", to), ("financeAccountId", financeAccountId), ("spendingCategoryId", spendingCategoryId), ("text", text)]
        where !value.trimmingCharacters(in: .whitespaces).isEmpty {
            variables[key] = .string(value.trimmingCharacters(in: .whitespaces))
        }
        if isUncategorized { variables["isUncategorized"] = .bool(true) }
        if isDuplicateIncluded { variables["isDuplicateIncluded"] = .bool(true) }
        return variables
    }

    public var isNarrowing: Bool { variables.count > 0 }
}

public struct SpendingCategory: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let spendingCategoryName: String
    public let parentSpendingCategoryId: String?
    public let isIncome: Bool?
    public let isHidden: Bool?
    public let isTransfer: Bool?
    public let isOther: Bool?
}

/// FinanceCategories names categories and orders them for pickers, as the
/// dashboard does: hidden ones left out, "Parent › Child", Other after the
/// rest and Transfer last.
public enum FinanceCategories {
    /// A category as shown, under its parent; `named` is how one name is
    /// shown, which turns a built-in name into the reader's words.
    public static func label(
        _ category: SpendingCategory, in categories: [SpendingCategory], named: (SpendingCategory) -> String = { $0.spendingCategoryName }
    ) -> String {
        guard let parentId = category.parentSpendingCategoryId, !parentId.isEmpty,
            let parent = categories.first(where: { $0.id == parentId })
        else { return named(category) }
        return "\(named(parent)) › \(named(category))"
    }

    public static func pickable(
        _ categories: [SpendingCategory], keeping chosen: String? = nil, named: (SpendingCategory) -> String = { $0.spendingCategoryName }
    ) -> [SpendingCategory] {
        let shown = categories.filter { $0.isHidden != true || $0.id == chosen }
        func rank(_ category: SpendingCategory) -> Int {
            if category.isTransfer == true { return 2 }
            if category.isOther == true { return 1 }
            return 0
        }
        return shown.sorted { left, right in
            let leftRank = rank(left)
            let rightRank = rank(right)
            if leftRank != rightRank { return leftRank < rightRank }
            return label(left, in: categories, named: named).localizedStandardCompare(label(right, in: categories, named: named)) == .orderedAscending
        }
    }
}

public struct CreditUsage: Codable, Hashable, Sendable {
    public struct Card: Codable, Hashable, Sendable {
        public let financeAccountId: String
        public let accountName: String?
        public let accountMask: String?
        public let institutionName: String?
        public let currencyCode: String?
        public let owedAmount: String?
        public let creditLimitAmount: String?
        public let creditLimitSource: String?
        public let usageShare: Double?
    }

    public let reportingCurrencyCode: String?
    public let unconvertedCurrencyCodes: [String]?
    public let totalOwedAmount: String?
    public let totalCreditLimitAmount: String?
    public let usageShare: Double?
    public let leftOutCardCount: Int?
    public let leftOutOwedAmount: String?
    public let creditCards: [Card?]?
}

public struct FinanceReceipt: Codable, Hashable, Sendable, Identifiable {
    public struct Line: Codable, Hashable, Sendable {
        public let id: String
        public let lineNumber: Int?
        public let receiptLineKind: String?
        public let description: String?
        public let quantity: String?
        public let quantityUnit: String?
        public let unitPriceAmount: String?
        public let lineAmount: String?
    }

    public struct Match: Codable, Hashable, Sendable {
        public let receiptId: String
        public let financeTransactionId: String
        public let matchedAmount: String?
        public let receiptMatchSource: String?
    }

    public let id: String
    public let receiptSourceKind: String?
    public let agentAttachmentId: String?
    public let mailboxItemId: String?
    public let merchantName: String?
    public let merchantReceiptNumber: String?
    public let purchasedOn: String?
    public let currencyCode: String?
    public let subtotalAmount: String?
    public let totalAmount: String?
    public let receiptCheckState: String?
    public let checkDifferenceAmount: String?
    public let receiptLines: [Line?]?
    public let receiptMatches: [Match?]?
}

public struct FinanceReceiptPage: Codable, Hashable, Sendable {
    public let financeReceipts: [FinanceReceipt?]
    public let totalCount: Int?
}

public struct ReceiptMatchProposal: Codable, Hashable, Sendable {
    public let financeTransactionId: String
    public let matchedAmount: String?
    public let isExactAmount: Bool?
    public let isSameAccount: Bool?
    public let isMerchantNameShared: Bool?
    public let dayDistanceCount: Int?
    public let financeTransaction: FinanceTransaction?
}

public struct SpendingSummary: Codable, Hashable, Sendable {
    public struct Row: Codable, Hashable, Sendable {
        public let groupKey: String
        public let groupLabel: String?
        public let currencyCode: String?
        public let moneyOut: String?
        public let moneyIn: String?
        public let financeTransactionCount: Int?
    }

    public struct ConvertedRow: Codable, Hashable, Sendable {
        public let groupKey: String
        public let groupLabel: String?
        public let moneyOut: String?
        public let moneyIn: String?
    }

    public let groupBy: String?
    public let spendingSummaryRows: [Row?]?
    public let reportingCurrencyCode: String?
    public let convertedMoneyOut: String?
    public let convertedMoneyIn: String?
    public let unconvertedCurrencyCodes: [String]?
    public let convertedSpendingSummaryRows: [ConvertedRow?]?
}

public struct BudgetStatus: Codable, Hashable, Sendable {
    public struct Spending: Codable, Hashable, Sendable {
        public let spendingCategoryId: String
        public let spendingCategoryName: String?
        public let budgetAmount: String?
        public let currencyCode: String?
        public let budgetToDateAmount: String?
        public let spendingAmount: String?
        public let spendingBySameDayLastMonthAmount: String?
        public let fixedChargesDueAmount: String?
        public let projectedAmount: String?
        public let budgetPace: String?
    }

    public struct Income: Codable, Hashable, Sendable {
        public let spendingCategoryId: String
        public let spendingCategoryName: String?
        public let budgetAmount: String?
        public let currencyCode: String?
        public let incomeAmount: String?
        public let expectedByTodayAmount: String?
        public let projectedAmount: String?
        public let incomePace: String?
    }

    public let month: String?
    public let year: String?
    public let asOf: String?
    public let spendingCategories: [Spending?]?
    public let incomeCategories: [Income?]?
}

public struct SavingSummary: Codable, Hashable, Sendable {
    public let reportingCurrencyCode: String?
    public let expectedIncomeAmount: String?
    public let expectedSpendingAmount: String?
    public let expectedSavingAmount: String?
    public let incomeAmount: String?
    public let spendingAmount: String?
    public let savingAmount: String?
    public let projectedIncomeAmount: String?
    public let projectedSpendingAmount: String?
    public let projectedSavingAmount: String?
    public let savingPace: String?
    public let incomeBudgetCount: Int?
    public let spendingBudgetCount: Int?
    public let unconvertedCurrencyCodes: [String]?
}

public struct CashFlow: Codable, Hashable, Sendable {
    public struct Month: Codable, Hashable, Sendable {
        public let cashFlowMonth: String
        public let incomeAmount: String?
        public let spendingAmount: String?
        public let netAmount: String?
    }

    public let reportingCurrencyCode: String?
    public let unconvertedCurrencyCodes: [String]?
    public let cashFlowMonths: [Month?]?
}

public struct SpendingRule: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let matchText: String
    public let financeAccountId: String?
    public let minimumAmount: String?
    public let maximumAmount: String?
    public let spendingCategoryId: String?
    public let rulePriority: Int?
}

public struct Budget: Codable, Hashable, Sendable, Identifiable {
    public let id: String
    public let spendingCategoryId: String
    public let monthlyAmount: String
    public let currencyCode: String?
    public let effectiveFrom: String
}

public struct NetWorth: Codable, Hashable, Sendable {
    public struct Point: Codable, Hashable, Sendable {
        public let netWorthOn: String
        public let netWorthAmount: String?
    }

    public let reportingCurrencyCode: String?
    public let unconvertedCurrencyCodes: [String]?
    public let convertedNetWorthPoints: [Point?]?
}

public struct FinanceAsset: Codable, Hashable, Sendable, Identifiable {
    public struct Security: Codable, Hashable, Sendable {
        public let tickerSymbol: String?
        public let securityName: String?
        public let closePrice: String?
        public let closePriceOn: String?
    }

    public struct Valuation: Codable, Hashable, Sendable, Identifiable {
        public let id: String
        public let valuedOn: String?
        public let value: String?
        public let currencyCode: String?
        public let valuationSource: String?
        public let estimateLow: String?
        public let estimateHigh: String?
        public let valuationNote: String?
        public let heldQuantity: String?
        public let unitPrice: String?
        public let costBasis: String?
    }

    public let id: String
    public let assetName: String
    public let assetKind: String
    public let isLiability: Bool?
    public let currencyCode: String?
    public let financeAccountId: String?
    public let financeSecurity: Security?
    public let valuationSource: String?
    public let estimateDescription: String?
    public let isEstimateAllowed: Bool?
    public let closedOn: String?
    public let latestValuation: Valuation?

    public var isClosed: Bool { !(closedOn ?? "").isEmpty }

    /// The kinds of what is owned and what is owed, in balance-sheet order.
    public static let ownedKinds = ["cash", "investment", "retirement", "property", "vehicle", "other_asset"]
    public static let owedKinds = ["credit_card", "loan", "mortgage", "other_liability"]
}

public struct SavingsTarget: Codable, Hashable, Sendable, Identifiable {
    public struct Target: Codable, Hashable, Sendable {
        public let id: String
        public let savingsTargetName: String
        public let targetAmount: String
        public let currencyCode: String?
        public let targetOn: String?
        public let targetMeasure: String?
        public let startingAmount: String?
        public let startedOn: String?
        public let closedOn: String?
        public let assetIds: [String]?
        public let financeAccountIds: [String]?
    }

    public struct Progress: Codable, Hashable, Sendable {
        public let savedAmount: String?
        public let remainingAmount: String?
        public let monthsLeftCount: Int?
        public let requiredMonthlyAmount: String?
        public let isBehind: Bool?
        public let unconvertedCurrencyCodes: [String]?
    }

    public let savingsTarget: Target?
    public let savingsTargetProgress: Progress?

    public var id: String { savingsTarget?.id ?? "" }
}

/// FinanceMoney writes and reads the server's decimal strings.
public enum FinanceMoney {
    /// Whether typed money is a decimal the server takes: up to four places.
    public static func isDecimal(_ text: String) -> Bool {
        text.range(of: #"^-?\d+(\.\d{1,4})?$"#, options: .regularExpression) != nil
    }

    public static func decimal(_ text: String?) -> Decimal? {
        guard let text, !text.isEmpty else { return nil }
        return Decimal(string: text, locale: Locale(identifier: "en_US_POSIX"))
    }

    /// An amount in its currency, or "—" when there is none.
    public static func shown(_ text: String?, currencyCode: String?) -> String {
        guard let amount = decimal(text) else { return "—" }
        return amount.formatted(.currency(code: (currencyCode ?? "").isEmpty ? "USD" : currencyCode ?? "USD"))
    }

    /// An amount on a chart's axis: short, in its currency, as "$12K" or
    /// "¥1.2万", since an axis has room for a few characters only.
    public static func axisText(_ amount: Double, currencyCode: String?) -> String {
        let code = (currencyCode ?? "").isEmpty ? "USD" : currencyCode ?? "USD"
        return amount.formatted(.currency(code: code).notation(.compactName))
    }

    /// A share as a whole percentage; "<1%" for one that rounds to nothing.
    public static func percent(_ share: Double) -> String {
        if share > 0, share < 0.005 { return "<1%" }
        return share.formatted(.percent.precision(.fractionLength(0)))
    }
}

extension Documents {
    static let financeAccountFields = """
        id sourceId providerKind institutionName accountName accountMask accountKind currencyCode
        currentBalance availableBalance balanceAt isSignInRequired reportingCurrencyCode convertedCurrentBalance
        """
    static let financeTransactionFields = """
        id financeAccountId providerTransactionId postedOn transactedAt amount currencyCode
        description merchantName providerCategoryPrimary providerCategoryDetailed isPending pendingProviderTransactionId
        spendingCategoryId categorizedBy categorizationConfidence
        duplicateOfTransactionId duplicateDecidedBy annotation annotatedBy receiptCount createdAt modifiedAt
        """
    static let financeReceiptFields = """
        id receiptSourceKind agentAttachmentId mailboxItemId merchantName merchantReceiptNumber purchasedOn
        currencyCode subtotalAmount totalAmount receiptCheckState checkDifferenceAmount
        receiptLines { id lineNumber receiptLineKind description quantity quantityUnit unitPriceAmount lineAmount }
        receiptMatches { receiptId financeTransactionId matchedAmount receiptMatchSource }
        """
    static let financeAssetFields = """
        id assetName assetKind isLiability currencyCode financeAccountId
        financeSecurity { tickerSymbol securityName closePrice closePriceOn }
        valuationSource estimateDescription isEstimateAllowed closedOn
        """
    static let financeValuationFields = """
        id valuedOn value currencyCode valuationSource estimateLow estimateHigh valuationNote heldQuantity unitPrice costBasis
        """
    public static let financePresence = """
        query { FinanceProviders { providerKind isBrowserRequired } FinanceSources { id } }
        """
    public static let financeZone = """
        query { ReadAgent { timezone } }
        """
    public static let financeSources = """
        query {
          FinanceSources { id name providerKind institutionName isEnabled lastRunAt nextRunAt lastError isSignInRequired
            financeAccounts { \(financeAccountFields) } }
        }
        """
    public static let financeAccounts = """
        query { FinanceAccounts { \(financeAccountFields) } }
        """
    public static let creditUsage = """
        query {
          CreditUsage { reportingCurrencyCode unconvertedCurrencyCodes totalOwedAmount totalCreditLimitAmount usageShare
            leftOutCardCount leftOutOwedAmount
            creditCards { financeAccountId accountName accountMask institutionName currencyCode owedAmount creditLimitAmount
              creditLimitSource usageShare } }
        }
        """
    public static let financeTransactions = """
        query ($from: String, $to: String, $financeAccountId: String, $text: String,
          $spendingCategoryId: String, $isUncategorized: Boolean, $duplicateOfTransactionId: String,
          $isDuplicateIncluded: Boolean, $financeTransactionIds: [String!], $limit: Int, $offset: Int) {
          FinanceTransactions(from: $from, to: $to, financeAccountId: $financeAccountId, text: $text,
            spendingCategoryId: $spendingCategoryId, isUncategorized: $isUncategorized,
            duplicateOfTransactionId: $duplicateOfTransactionId, isDuplicateIncluded: $isDuplicateIncluded,
            financeTransactionIds: $financeTransactionIds, limit: $limit, offset: $offset) {
            financeTransactions { \(financeTransactionFields) }
            totalCount
            leftOutDuplicateCount
          }
        }
        """
    public static let categorizeTransaction = """
        mutation ($financeTransactionId: String!, $spendingCategoryId: String, $shouldCreateSpendingRule: Boolean) {
          CategorizeTransaction(financeTransactionId: $financeTransactionId, spendingCategoryId: $spendingCategoryId,
            shouldCreateSpendingRule: $shouldCreateSpendingRule) {
            financeTransaction { \(financeTransactionFields) }
            spendingRule { id matchText }
          }
        }
        """
    public static let categorizeTransactions = """
        mutation ($financeTransactionIds: [String!]!, $spendingCategoryId: String) {
          CategorizeTransactions(financeTransactionIds: $financeTransactionIds, spendingCategoryId: $spendingCategoryId) {
            financeTransactions { id }
          }
        }
        """
    public static let countTransaction = """
        mutation ($financeTransactionId: String!) { CountTransaction(financeTransactionId: $financeTransactionId) { id } }
        """
    public static let undoCountTransaction = """
        mutation ($financeTransactionId: String!) { UndoCountTransaction(financeTransactionId: $financeTransactionId) { id } }
        """
    public static let annotateTransaction = """
        mutation ($financeTransactionId: String!, $annotation: String) {
          AnnotateTransaction(financeTransactionId: $financeTransactionId, annotation: $annotation) { id }
        }
        """
    public static let financeReceipts = """
        query ($financeTransactionId: String, $from: String, $to: String,
          $isUndated: Boolean, $isUnmatched: Boolean, $text: String, $limit: Int, $offset: Int) {
          FinanceReceipts(financeTransactionId: $financeTransactionId, from: $from, to: $to, isUndated: $isUndated,
            isUnmatched: $isUnmatched, text: $text, limit: $limit, offset: $offset) {
            financeReceipts { \(financeReceiptFields) }
            totalCount
          }
        }
        """
    public static let proposeReceiptMatches = """
        query ($receiptId: String!) {
          ProposeReceiptMatches(receiptId: $receiptId) {
            financeTransactionId matchedAmount isExactAmount isSameAccount isMerchantNameShared dayDistanceCount
            financeTransaction { \(financeTransactionFields) }
          }
        }
        """
    public static let matchReceipt = """
        mutation ($receiptId: String!, $financeTransactionId: String!, $matchedAmount: String) {
          MatchReceipt(receiptId: $receiptId, financeTransactionId: $financeTransactionId, matchedAmount: $matchedAmount) { id }
        }
        """
    public static let unmatchReceipt = """
        mutation ($receiptId: String!, $financeTransactionId: String!) {
          UnmatchReceipt(receiptId: $receiptId, financeTransactionId: $financeTransactionId) { id }
        }
        """
    public static let deleteReceipt = """
        mutation ($receiptId: String!) { DeleteReceipt(receiptId: $receiptId) }
        """
    public static let readReceipt = """
        mutation ($agentAttachmentId: String, $mailboxItemId: String, $financeTransactionId: String) {
          ReadReceipt(agentAttachmentId: $agentAttachmentId, mailboxItemId: $mailboxItemId,
            financeTransactionId: $financeTransactionId) { agentJobId }
        }
        """
    public static let spendingSummary = """
        query ($from: String, $to: String, $groupBy: String, $financeAccountId: String) {
          FinanceSpendingSummary(from: $from, to: $to, groupBy: $groupBy, financeAccountId: $financeAccountId) {
            groupBy
            spendingSummaryRows { groupKey groupLabel currencyCode moneyOut moneyIn financeTransactionCount }
            reportingCurrencyCode convertedMoneyOut convertedMoneyIn unconvertedCurrencyCodes
            convertedSpendingSummaryRows { groupKey groupLabel moneyOut moneyIn }
          }
        }
        """
    public static let budgetStatus = """
        query ($month: String, $year: String) {
          BudgetStatus(month: $month, year: $year) { month year asOf
            spendingCategories { spendingCategoryId spendingCategoryName budgetAmount currencyCode budgetToDateAmount
              spendingAmount spendingBySameDayLastMonthAmount fixedChargesDueAmount projectedAmount budgetPace }
            incomeCategories { spendingCategoryId spendingCategoryName budgetAmount currencyCode
              incomeAmount expectedByTodayAmount projectedAmount incomePace } }
        }
        """
    public static let savingSummary = """
        query ($month: String, $year: String) {
          SavingSummary(month: $month, year: $year) { reportingCurrencyCode incomeBudgetCount spendingBudgetCount
            expectedIncomeAmount expectedSpendingAmount expectedSavingAmount incomeAmount spendingAmount savingAmount
            projectedIncomeAmount projectedSpendingAmount projectedSavingAmount savingPace unconvertedCurrencyCodes }
        }
        """
    public static let cashFlow = """
        query ($fromMonth: String, $toMonth: String) {
          CashFlow(fromMonth: $fromMonth, toMonth: $toMonth) { reportingCurrencyCode unconvertedCurrencyCodes
            cashFlowMonths { cashFlowMonth incomeAmount spendingAmount netAmount } }
        }
        """
    public static let spendingCategories = """
        query { SpendingCategories { id spendingCategoryName parentSpendingCategoryId isIncome isHidden isTransfer isOther } }
        """
    public static let createSpendingCategory = """
        mutation ($spendingCategoryName: String!, $parentSpendingCategoryId: String, $isIncome: Boolean, $isHidden: Boolean) {
          CreateSpendingCategory(spendingCategoryName: $spendingCategoryName, parentSpendingCategoryId: $parentSpendingCategoryId,
            isIncome: $isIncome, isHidden: $isHidden) { id }
        }
        """
    public static let updateSpendingCategory = """
        mutation ($spendingCategoryId: String!, $spendingCategoryName: String, $parentSpendingCategoryId: String,
          $isIncome: Boolean, $isHidden: Boolean) {
          UpdateSpendingCategory(spendingCategoryId: $spendingCategoryId, spendingCategoryName: $spendingCategoryName,
            parentSpendingCategoryId: $parentSpendingCategoryId, isIncome: $isIncome, isHidden: $isHidden) { id }
        }
        """
    public static let deleteSpendingCategory = """
        mutation ($spendingCategoryId: String!) { DeleteSpendingCategory(spendingCategoryId: $spendingCategoryId) }
        """
    public static let spendingRules = """
        query { SpendingRules { id matchText financeAccountId minimumAmount maximumAmount spendingCategoryId rulePriority } }
        """
    public static let createSpendingRule = """
        mutation ($matchText: String!, $financeAccountId: String, $minimumAmount: String,
          $maximumAmount: String, $spendingCategoryId: String!, $rulePriority: Int) {
          CreateSpendingRule(matchText: $matchText, financeAccountId: $financeAccountId, minimumAmount: $minimumAmount,
            maximumAmount: $maximumAmount, spendingCategoryId: $spendingCategoryId, rulePriority: $rulePriority) { id }
        }
        """
    public static let updateSpendingRule = """
        mutation ($spendingRuleId: String!, $matchText: String, $financeAccountId: String,
          $minimumAmount: String, $maximumAmount: String, $spendingCategoryId: String, $rulePriority: Int) {
          UpdateSpendingRule(spendingRuleId: $spendingRuleId, matchText: $matchText, financeAccountId: $financeAccountId,
            minimumAmount: $minimumAmount, maximumAmount: $maximumAmount, spendingCategoryId: $spendingCategoryId,
            rulePriority: $rulePriority) { id }
        }
        """
    public static let deleteSpendingRule = """
        mutation ($spendingRuleId: String!) { DeleteSpendingRule(spendingRuleId: $spendingRuleId) }
        """
    public static let budgets = """
        query { Budgets { id spendingCategoryId monthlyAmount currencyCode effectiveFrom } }
        """
    public static let setBudget = """
        mutation ($spendingCategoryId: String!, $monthlyAmount: String!, $currencyCode: String, $effectiveFrom: String) {
          SetBudget(spendingCategoryId: $spendingCategoryId, monthlyAmount: $monthlyAmount, currencyCode: $currencyCode,
            effectiveFrom: $effectiveFrom) { id }
        }
        """
    public static let netWorth = """
        query ($from: String, $to: String) {
          NetWorth(from: $from, to: $to) { reportingCurrencyCode unconvertedCurrencyCodes
            convertedNetWorthPoints { netWorthOn netWorthAmount } }
        }
        """
    public static let assets = """
        query { Assets { \(financeAssetFields) latestValuation { \(financeValuationFields) } } }
        """
    public static let assetHistory = """
        query ($assetId: String!) {
          AssetHistory(assetId: $assetId) { assetValuations { \(financeValuationFields) } }
        }
        """
    public static let createAsset = """
        mutation ($assetName: String!, $assetKind: String!, $currencyCode: String!,
          $valuationSource: String, $estimateDescription: String, $isEstimateAllowed: Boolean, $value: String, $valuedOn: String) {
          CreateAsset(assetName: $assetName, assetKind: $assetKind, currencyCode: $currencyCode,
            valuationSource: $valuationSource, estimateDescription: $estimateDescription,
            isEstimateAllowed: $isEstimateAllowed, value: $value, valuedOn: $valuedOn) { id }
        }
        """
    public static let updateAsset = """
        mutation ($assetId: String!, $assetName: String, $assetKind: String, $currencyCode: String,
          $valuationSource: String, $estimateDescription: String, $isEstimateAllowed: Boolean) {
          UpdateAsset(assetId: $assetId, assetName: $assetName, assetKind: $assetKind, currencyCode: $currencyCode,
            valuationSource: $valuationSource, estimateDescription: $estimateDescription,
            isEstimateAllowed: $isEstimateAllowed) { id }
        }
        """
    public static let closeAsset = """
        mutation ($assetId: String!, $closedOn: String, $shouldReopen: Boolean) {
          CloseAsset(assetId: $assetId, closedOn: $closedOn, shouldReopen: $shouldReopen) { id }
        }
        """
    public static let deleteAsset = """
        mutation ($assetId: String!) { DeleteAsset(assetId: $assetId) }
        """
    public static let recordValuation = """
        mutation ($assetId: String!, $value: String!, $valuedOn: String, $valuationNote: String) {
          RecordValuation(assetId: $assetId, value: $value, valuedOn: $valuedOn, valuationNote: $valuationNote) { id }
        }
        """
    public static let deleteValuation = """
        mutation ($valuationId: String!) { DeleteValuation(valuationId: $valuationId) }
        """
    public static let savingsTargets = """
        query {
          SavingsTargets {
            savingsTarget { id savingsTargetName targetAmount currencyCode targetOn targetMeasure startingAmount startedOn
              closedOn assetIds financeAccountIds }
            savingsTargetProgress { savedAmount remainingAmount monthsLeftCount requiredMonthlyAmount isBehind
              unconvertedCurrencyCodes }
          }
        }
        """
    public static let createSavingsTarget = """
        mutation ($savingsTargetName: String!, $targetAmount: String!, $currencyCode: String,
          $targetOn: String!, $targetMeasure: String, $startingAmount: String, $startedOn: String, $assetIds: [String!],
          $financeAccountIds: [String!]) {
          CreateSavingsTarget(savingsTargetName: $savingsTargetName, targetAmount: $targetAmount, currencyCode: $currencyCode,
            targetOn: $targetOn, targetMeasure: $targetMeasure, startingAmount: $startingAmount, startedOn: $startedOn,
            assetIds: $assetIds, financeAccountIds: $financeAccountIds) { savingsTarget { id } }
        }
        """
    public static let updateSavingsTarget = """
        mutation ($savingsTargetId: String!, $savingsTargetName: String,
          $targetAmount: String, $targetOn: String, $targetMeasure: String, $startingAmount: String, $assetIds: [String!],
          $financeAccountIds: [String!]) {
          UpdateSavingsTarget(savingsTargetId: $savingsTargetId, savingsTargetName: $savingsTargetName,
            targetAmount: $targetAmount, targetOn: $targetOn, targetMeasure: $targetMeasure, startingAmount: $startingAmount,
            assetIds: $assetIds, financeAccountIds: $financeAccountIds) { savingsTarget { id } }
        }
        """
    public static let closeSavingsTarget = """
        mutation ($savingsTargetId: String!, $shouldReopen: Boolean) {
          CloseSavingsTarget(savingsTargetId: $savingsTargetId, shouldReopen: $shouldReopen) { savingsTarget { id } }
        }
        """
    public static let reportingCurrency = """
        query { ReportingCurrency { reportingCurrencyCode isChosen } }
        """
    public static let setReportingCurrency = """
        mutation ($currencyCode: String!) { SetReportingCurrency(currencyCode: $currencyCode) }
        """
    public static let convertCurrency = """
        query ($amount: String!, $fromCurrencyCode: String!, $toCurrencyCode: String!, $rateOn: String) {
          ConvertCurrency(amount: $amount, fromCurrencyCode: $fromCurrencyCode, toCurrencyCode: $toCurrencyCode, rateOn: $rateOn) {
            amount fromCurrencyCode convertedAmount toCurrencyCode rate rateOn rateSource }
        }
        """
    public static let syncFinanceSource = """
        mutation ($sourceId: String!) { SyncAgentKnowledgeSource(sourceId: $sourceId) }
        """
    public static let deleteFinanceSource = """
        mutation ($sourceId: String!) { DeleteAgentKnowledgeSource(sourceId: $sourceId) }
        """
    public static let switchFinanceSource = """
        mutation ($sourceId: String, $enabled: Boolean) { SaveAgentKnowledgeSource(sourceId: $sourceId, enabled: $enabled) { id } }
        """
    public static let linkSimpleFIN = """
        mutation ($setupToken: String!) { LinkSimpleFIN(setupToken: $setupToken) { id } }
        """
}

/// FinanceConversion is an amount in another currency, and the rate used.
public struct FinanceConversion: Codable, Hashable, Sendable {
    public let convertedAmount: String?
    public let rate: String?
    public let rateOn: String?
    public let rateSource: String?
}

extension Connection {
    /// Whether finance is offered: a provider to link, or a source linked.
    public func isFinanceShown() async throws -> Bool {
        struct Answer: Decodable, Sendable {
            struct Source: Decodable, Sendable { let id: String }
            let FinanceProviders: [FinanceProvider?]?
            let FinanceSources: [Source?]?
        }
        let answer = try await execute(Documents.financePresence, as: Answer.self)
        return !(answer.FinanceProviders ?? []).compactMap { $0 }.isEmpty || !(answer.FinanceSources ?? []).compactMap { $0 }.isEmpty
    }

    /// The time zone the person's days are counted in.
    public func financeZone() async throws -> TimeZone {
        struct Answer: Decodable, Sendable {
            struct Agent: Decodable, Sendable { let timezone: String? }
            let ReadAgent: Agent?
        }
        let name = try await execute(Documents.financeZone, as: Answer.self).ReadAgent?.timezone ?? ""
        return TimeZone(identifier: name) ?? .current
    }

    public func financeSources() async throws -> [FinanceSource] {
        struct Answer: Decodable, Sendable { let FinanceSources: [FinanceSource?] }
        return try await execute(Documents.financeSources, as: Answer.self).FinanceSources.compactMap { $0 }
    }

    public func financeAccounts() async throws -> [FinanceAccount] {
        struct Answer: Decodable, Sendable { let FinanceAccounts: [FinanceAccount?] }
        return try await execute(Documents.financeAccounts, as: Answer.self).FinanceAccounts.compactMap { $0 }
    }

    public func creditUsage() async throws -> CreditUsage? {
        struct Answer: Decodable, Sendable { let CreditUsage: CreditUsage? }
        return try await execute(Documents.creditUsage, as: Answer.self).CreditUsage
    }

    public func financeTransactions(
        _ filter: FinanceTransactionFilter, ids: [String]? = nil, duplicatesOf: String? = nil, limit: Int = 50, offset: Int = 0
    ) async throws -> FinanceTransactionPage {
        struct Answer: Decodable, Sendable { let FinanceTransactions: FinanceTransactionPage }
        var variables = filter.variables
        variables["limit"] = .number(Double(limit))
        variables["offset"] = .number(Double(offset))
        if let ids { variables["financeTransactionIds"] = .array(ids.map { .string($0) }) }
        if let duplicatesOf { variables["duplicateOfTransactionId"] = .string(duplicatesOf) }
        return try await execute(Documents.financeTransactions, variables: variables, as: Answer.self).FinanceTransactions
    }

    /// Files a transaction under a category; with a rule, every later one
    /// from the merchant too. Returns the rule's words when one was made.
    public func categorize(_ transactionId: String, as categoryId: String, makingRule: Bool) async throws -> String? {
        struct Answer: Decodable, Sendable {
            struct Categorized: Decodable, Sendable {
                struct Rule: Decodable, Sendable { let matchText: String }
                let spendingRule: Rule?
            }
            let CategorizeTransaction: Categorized?
        }
        var variables: [String: JSONValue] = ["financeTransactionId": .string(transactionId), "spendingCategoryId": .string(categoryId)]
        if makingRule { variables["shouldCreateSpendingRule"] = .bool(true) }
        return try await execute(Documents.categorizeTransaction, variables: variables, as: Answer.self).CategorizeTransaction?.spendingRule?.matchText
    }

    /// Files several transactions under one category, 500 at a time.
    public func categorize(_ transactionIds: [String], as categoryId: String) async throws {
        struct Answer: Decodable, Sendable {
            struct Categorized: Decodable, Sendable {}
            let CategorizeTransactions: Categorized?
        }
        for start in stride(from: 0, to: transactionIds.count, by: 500) {
            let chunk = Array(transactionIds[start..<min(start + 500, transactionIds.count)])
            _ = try await execute(Documents.categorizeTransactions, variables: [
                "financeTransactionIds": .array(chunk.map { .string($0) }), "spendingCategoryId": .string(categoryId),
            ], as: Answer.self)
        }
    }

    /// Counts a transaction marked a duplicate, or lets detection decide again.
    public func count(_ transactionId: String, isUndone: Bool) async throws {
        struct Answer: Decodable, Sendable {
            struct Counted: Decodable, Sendable { let id: String }
            let CountTransaction: Counted?
            let UndoCountTransaction: Counted?
        }
        _ = try await execute(isUndone ? Documents.undoCountTransaction : Documents.countTransaction,
            variables: ["financeTransactionId": .string(transactionId)], as: Answer.self)
    }

    public func annotate(_ transactionId: String, with annotation: String) async throws {
        struct Answer: Decodable, Sendable {
            struct Annotated: Decodable, Sendable { let id: String }
            let AnnotateTransaction: Annotated?
        }
        _ = try await execute(Documents.annotateTransaction, variables: [
            "financeTransactionId": .string(transactionId), "annotation": .string(annotation),
        ], as: Answer.self)
    }

    public func financeReceipts(
        transactionId: String? = nil, from: String = "", to: String = "", isUnmatched: Bool = false, isUndated: Bool = false,
        text: String = "", limit: Int = 50, offset: Int = 0
    ) async throws -> FinanceReceiptPage {
        struct Answer: Decodable, Sendable { let FinanceReceipts: FinanceReceiptPage }
        var variables: [String: JSONValue] = ["limit": .number(Double(limit)), "offset": .number(Double(offset))]
        if let transactionId { variables["financeTransactionId"] = .string(transactionId) }
        if !from.isEmpty { variables["from"] = .string(from) }
        if !to.isEmpty { variables["to"] = .string(to) }
        if isUnmatched { variables["isUnmatched"] = .bool(true) }
        if isUndated { variables["isUndated"] = .bool(true) }
        if !text.isEmpty { variables["text"] = .string(text) }
        return try await execute(Documents.financeReceipts, variables: variables, as: Answer.self).FinanceReceipts
    }

    public func proposeReceiptMatches(_ receiptId: String) async throws -> [ReceiptMatchProposal] {
        struct Answer: Decodable, Sendable { let ProposeReceiptMatches: [ReceiptMatchProposal?] }
        return try await execute(Documents.proposeReceiptMatches, variables: ["receiptId": .string(receiptId)], as: Answer.self)
            .ProposeReceiptMatches.compactMap { $0 }
    }

    public func matchReceipt(_ receiptId: String, to transactionId: String, amount: String?, isUndone: Bool = false) async throws {
        struct Answer: Decodable, Sendable {
            struct Matched: Decodable, Sendable { let id: String }
            let MatchReceipt: Matched?
            let UnmatchReceipt: Matched?
        }
        var variables: [String: JSONValue] = ["receiptId": .string(receiptId), "financeTransactionId": .string(transactionId)]
        if !isUndone, let amount { variables["matchedAmount"] = .string(amount) }
        _ = try await execute(isUndone ? Documents.unmatchReceipt : Documents.matchReceipt, variables: variables, as: Answer.self)
    }

    public func deleteReceipt(_ receiptId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteReceipt: JSONValue? }
        _ = try await execute(Documents.deleteReceipt, variables: ["receiptId": .string(receiptId)], as: Answer.self)
    }

    /// Has the agent read a receipt: an uploaded picture, or a mail; for a
    /// charge, it is matched to it.
    public func readReceipt(attachmentId: String? = nil, mailboxItemId: String? = nil, transactionId: String? = nil) async throws {
        struct Answer: Decodable, Sendable {
            struct Reading: Decodable, Sendable { let agentJobId: String? }
            let ReadReceipt: Reading?
        }
        var variables: [String: JSONValue] = [:]
        if let attachmentId { variables["agentAttachmentId"] = .string(attachmentId) }
        if let mailboxItemId { variables["mailboxItemId"] = .string(mailboxItemId) }
        if let transactionId { variables["financeTransactionId"] = .string(transactionId) }
        _ = try await execute(Documents.readReceipt, variables: variables, as: Answer.self)
    }

    public func spendingSummary(from: String, to: String, groupBy: String) async throws -> SpendingSummary? {
        struct Answer: Decodable, Sendable { let FinanceSpendingSummary: SpendingSummary? }
        return try await execute(Documents.spendingSummary, variables: [
            "from": .string(from), "to": .string(to), "groupBy": .string(groupBy),
        ], as: Answer.self).FinanceSpendingSummary
    }

    public func budgetStatus(month: String) async throws -> BudgetStatus? {
        struct Answer: Decodable, Sendable { let BudgetStatus: BudgetStatus? }
        return try await execute(Documents.budgetStatus, variables: ["month": .string(month)], as: Answer.self).BudgetStatus
    }

    public func savingSummary(month: String) async throws -> SavingSummary? {
        struct Answer: Decodable, Sendable { let SavingSummary: SavingSummary? }
        return try await execute(Documents.savingSummary, variables: ["month": .string(month)], as: Answer.self).SavingSummary
    }

    public func cashFlow(fromMonth: String, toMonth: String) async throws -> CashFlow? {
        struct Answer: Decodable, Sendable { let CashFlow: CashFlow? }
        return try await execute(Documents.cashFlow, variables: ["fromMonth": .string(fromMonth), "toMonth": .string(toMonth)], as: Answer.self)
            .CashFlow
    }

    public func spendingCategories() async throws -> [SpendingCategory] {
        struct Answer: Decodable, Sendable { let SpendingCategories: [SpendingCategory?] }
        return try await execute(Documents.spendingCategories, as: Answer.self).SpendingCategories.compactMap { $0 }
    }

    /// Adds a category, or changes one by its id.
    public func saveSpendingCategory(_ categoryId: String?, name: String, parentId: String, isIncome: Bool, isHidden: Bool) async throws {
        struct Answer: Decodable, Sendable {
            struct Saved: Decodable, Sendable { let id: String }
            let CreateSpendingCategory: Saved?
            let UpdateSpendingCategory: Saved?
        }
        var variables: [String: JSONValue] = [
            "spendingCategoryName": .string(name), "parentSpendingCategoryId": .string(parentId), "isIncome": .bool(isIncome), "isHidden": .bool(isHidden),
        ]
        if let categoryId { variables["spendingCategoryId"] = .string(categoryId) }
        _ = try await execute(categoryId == nil ? Documents.createSpendingCategory : Documents.updateSpendingCategory, variables: variables, as: Answer.self)
    }

    public func deleteSpendingCategory(_ categoryId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteSpendingCategory: JSONValue? }
        _ = try await execute(Documents.deleteSpendingCategory, variables: ["spendingCategoryId": .string(categoryId)], as: Answer.self)
    }

    public func spendingRules() async throws -> [SpendingRule] {
        struct Answer: Decodable, Sendable { let SpendingRules: [SpendingRule?] }
        return try await execute(Documents.spendingRules, as: Answer.self).SpendingRules.compactMap { $0 }
    }

    /// Adds a rule, or changes one by its id; blank fields are sent empty.
    public func saveSpendingRule(
        _ ruleId: String?, matchText: String, categoryId: String, accountId: String, minimum: String, maximum: String, priority: Int?
    ) async throws {
        struct Answer: Decodable, Sendable {
            struct Saved: Decodable, Sendable { let id: String }
            let CreateSpendingRule: Saved?
            let UpdateSpendingRule: Saved?
        }
        var variables: [String: JSONValue] = [
            "matchText": .string(matchText), "spendingCategoryId": .string(categoryId), "financeAccountId": .string(accountId),
            "minimumAmount": .string(minimum), "maximumAmount": .string(maximum),
        ]
        if let priority { variables["rulePriority"] = .number(Double(priority)) }
        if let ruleId { variables["spendingRuleId"] = .string(ruleId) }
        _ = try await execute(ruleId == nil ? Documents.createSpendingRule : Documents.updateSpendingRule, variables: variables, as: Answer.self)
    }

    public func deleteSpendingRule(_ ruleId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteSpendingRule: JSONValue? }
        _ = try await execute(Documents.deleteSpendingRule, variables: ["spendingRuleId": .string(ruleId)], as: Answer.self)
    }

    public func budgets() async throws -> [Budget] {
        struct Answer: Decodable, Sendable { let Budgets: [Budget?] }
        return try await execute(Documents.budgets, as: Answer.self).Budgets.compactMap { $0 }
    }

    /// Sets a category's monthly budget from a month on; zero ends it.
    public func setBudget(categoryId: String, monthlyAmount: String, currencyCode: String?, effectiveFrom: String) async throws {
        struct Answer: Decodable, Sendable {
            struct Set: Decodable, Sendable { let id: String }
            let SetBudget: Set?
        }
        var variables: [String: JSONValue] = [
            "spendingCategoryId": .string(categoryId), "monthlyAmount": .string(monthlyAmount), "effectiveFrom": .string(effectiveFrom),
        ]
        if let currencyCode, !currencyCode.isEmpty { variables["currencyCode"] = .string(currencyCode) }
        _ = try await execute(Documents.setBudget, variables: variables, as: Answer.self)
    }

    public func netWorth(from: String, to: String) async throws -> NetWorth? {
        struct Answer: Decodable, Sendable { let NetWorth: NetWorth? }
        return try await execute(Documents.netWorth, variables: ["from": .string(from), "to": .string(to)], as: Answer.self).NetWorth
    }

    public func assets() async throws -> [FinanceAsset] {
        struct Answer: Decodable, Sendable { let Assets: [FinanceAsset?] }
        return try await execute(Documents.assets, as: Answer.self).Assets.compactMap { $0 }
    }

    public func assetHistory(_ assetId: String) async throws -> [FinanceAsset.Valuation] {
        struct Answer: Decodable, Sendable {
            struct History: Decodable, Sendable { let assetValuations: [FinanceAsset.Valuation?] }
            let AssetHistory: History?
        }
        return try await execute(Documents.assetHistory, variables: ["assetId": .string(assetId)], as: Answer.self)
            .AssetHistory?.assetValuations.compactMap { $0 } ?? []
    }

    /// Adds an asset with its first value, or changes one by its id.
    public func saveAsset(_ assetId: String?, fields: [String: JSONValue]) async throws {
        struct Answer: Decodable, Sendable {
            struct Saved: Decodable, Sendable { let id: String }
            let CreateAsset: Saved?
            let UpdateAsset: Saved?
        }
        var variables = fields
        if let assetId { variables["assetId"] = .string(assetId) }
        _ = try await execute(assetId == nil ? Documents.createAsset : Documents.updateAsset, variables: variables, as: Answer.self)
    }

    public func closeAsset(_ assetId: String, closedOn: String?, isReopened: Bool) async throws {
        struct Answer: Decodable, Sendable {
            struct Closed: Decodable, Sendable { let id: String }
            let CloseAsset: Closed?
        }
        var variables: [String: JSONValue] = ["assetId": .string(assetId)]
        if isReopened { variables["shouldReopen"] = .bool(true) } else if let closedOn { variables["closedOn"] = .string(closedOn) }
        _ = try await execute(Documents.closeAsset, variables: variables, as: Answer.self)
    }

    public func deleteAsset(_ assetId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteAsset: JSONValue? }
        _ = try await execute(Documents.deleteAsset, variables: ["assetId": .string(assetId)], as: Answer.self)
    }

    public func recordValuation(assetId: String, value: String, valuedOn: String, note: String) async throws {
        struct Answer: Decodable, Sendable {
            struct Recorded: Decodable, Sendable { let id: String }
            let RecordValuation: Recorded?
        }
        _ = try await execute(Documents.recordValuation, variables: [
            "assetId": .string(assetId), "value": .string(value), "valuedOn": .string(valuedOn), "valuationNote": .string(note),
        ], as: Answer.self)
    }

    public func deleteValuation(_ valuationId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteValuation: JSONValue? }
        _ = try await execute(Documents.deleteValuation, variables: ["valuationId": .string(valuationId)], as: Answer.self)
    }

    public func savingsTargets() async throws -> [SavingsTarget] {
        struct Answer: Decodable, Sendable { let SavingsTargets: [SavingsTarget?] }
        return try await execute(Documents.savingsTargets, as: Answer.self).SavingsTargets.compactMap { $0 }
    }

    /// Adds a savings target, or changes one by its id.
    public func saveSavingsTarget(_ targetId: String?, fields: [String: JSONValue]) async throws {
        struct Answer: Decodable, Sendable {
            struct Saved: Decodable, Sendable {}
            let CreateSavingsTarget: Saved?
            let UpdateSavingsTarget: Saved?
        }
        var variables = fields
        if let targetId { variables["savingsTargetId"] = .string(targetId) }
        _ = try await execute(targetId == nil ? Documents.createSavingsTarget : Documents.updateSavingsTarget, variables: variables, as: Answer.self)
    }

    public func closeSavingsTarget(_ targetId: String, isReopened: Bool) async throws {
        struct Answer: Decodable, Sendable {
            struct Closed: Decodable, Sendable {}
            let CloseSavingsTarget: Closed?
        }
        var variables: [String: JSONValue] = ["savingsTargetId": .string(targetId)]
        if isReopened { variables["shouldReopen"] = .bool(true) }
        _ = try await execute(Documents.closeSavingsTarget, variables: variables, as: Answer.self)
    }

    /// The currency totals are converted to, and whether the person chose it.
    public func reportingCurrency() async throws -> (code: String, isChosen: Bool) {
        struct Answer: Decodable, Sendable {
            struct Currency: Decodable, Sendable {
                let reportingCurrencyCode: String?
                let isChosen: Bool?
            }
            let ReportingCurrency: Currency?
        }
        let currency = try await execute(Documents.reportingCurrency, as: Answer.self).ReportingCurrency
        return (currency?.reportingCurrencyCode ?? "", currency?.isChosen ?? false)
    }

    /// Chooses the reporting currency; empty clears the choice.
    public func setReportingCurrency(_ code: String) async throws {
        struct Answer: Decodable, Sendable { let SetReportingCurrency: JSONValue? }
        _ = try await execute(Documents.setReportingCurrency, variables: ["currencyCode": .string(code)], as: Answer.self)
    }

    public func convert(_ amount: String, from: String, to: String, on day: String? = nil) async throws -> FinanceConversion? {
        struct Answer: Decodable, Sendable { let ConvertCurrency: FinanceConversion? }
        var variables: [String: JSONValue] = ["amount": .string(amount), "fromCurrencyCode": .string(from), "toCurrencyCode": .string(to)]
        if let day { variables["rateOn"] = .string(day) }
        return try await execute(Documents.convertCurrency, variables: variables, as: Answer.self).ConvertCurrency
    }

    public func syncFinanceSource(_ sourceId: String) async throws {
        struct Answer: Decodable, Sendable { let SyncAgentKnowledgeSource: JSONValue? }
        _ = try await execute(Documents.syncFinanceSource, variables: ["sourceId": .string(sourceId)], as: Answer.self)
    }

    public func deleteFinanceSource(_ sourceId: String) async throws {
        struct Answer: Decodable, Sendable { let DeleteAgentKnowledgeSource: JSONValue? }
        _ = try await execute(Documents.deleteFinanceSource, variables: ["sourceId": .string(sourceId)], as: Answer.self)
    }

    public func switchFinanceSource(_ sourceId: String, isEnabled: Bool) async throws {
        struct Answer: Decodable, Sendable {
            struct Saved: Decodable, Sendable { let id: String }
            let SaveAgentKnowledgeSource: Saved?
        }
        _ = try await execute(Documents.switchFinanceSource, variables: ["sourceId": .string(sourceId), "enabled": .bool(isEnabled)], as: Answer.self)
    }

    /// Links a bank through SimpleFIN. The setup token works once, so this
    /// is never retried on its own.
    public func linkSimpleFIN(setupToken: String) async throws {
        struct Answer: Decodable, Sendable {
            struct Linked: Decodable, Sendable { let id: String }
            let LinkSimpleFIN: Linked?
        }
        _ = try await execute(Documents.linkSimpleFIN, variables: ["setupToken": .string(setupToken)], as: Answer.self)
    }
}
