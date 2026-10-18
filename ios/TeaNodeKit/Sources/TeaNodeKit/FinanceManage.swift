import Foundation

// What the dashboard and the command line do with finance that the app
// did not: an account's trades, renaming and deleting an account of
// imported statements, the spending rules to suggest for a category,
// bringing in a provider connection made elsewhere, and importing
// transaction rows after a preview of what they would do.

/// FinanceTrade is one buy, sell, cancel or transfer of a security in an
/// account. Its amount is the cash the trade moved: negative when cash
/// left the account.
public struct FinanceTrade: Codable, Hashable, Sendable, Identifiable {
    public struct Security: Codable, Hashable, Sendable {
        public let tickerSymbol: String?
        public let securityName: String?
    }

    public let id: String
    public let financeAccountId: String?
    public let financeSecurityId: String?
    public let financeSecurity: Security?
    public let providerTradeId: String?
    public let tradedOn: String?
    public let tradeKind: String?
    public let tradeSubkind: String?
    public let tradedQuantity: String?
    public let unitPrice: String?
    public let tradeAmount: String?
    public let feeAmount: String?
    public let currencyCode: String?
    public let description: String?

    /// The ticker, else the security's name, else the institution's words.
    public var shownName: String {
        for text in [financeSecurity?.tickerSymbol, financeSecurity?.securityName, description] {
            if let text, !text.isEmpty { return text }
        }
        return ""
    }
}

/// FinanceTradePage is one page of trades, newest first, and how many
/// there are in all.
public struct FinanceTradePage: Codable, Hashable, Sendable {
    public let financeTrades: [FinanceTrade?]
    public let totalCount: Int?
}

/// StatementAccountDeleted is what deleting an account of imported
/// statements removed.
public struct StatementAccountDeleted: Codable, Hashable, Sendable {
    public let financeAccountId: String?
    public let deletedTransactionCount: Int?
    public let deletedAssetCount: Int?
}

/// SpendingRuleProposals is the spending rules to offer for a set of
/// transactions filed under one category, and how many words were left out
/// and why.
public struct SpendingRuleProposals: Codable, Hashable, Sendable {
    public struct Proposal: Codable, Hashable, Sendable, Identifiable {
        public struct AheadOf: Codable, Hashable, Sendable {
            public let id: String?
            public let matchText: String?
            public let spendingCategoryId: String?
        }

        public let matchText: String
        public let spendingCategoryId: String?
        public let financeTransactionCount: Int?
        public let changedTransactionCount: Int?
        public let aheadOfSpendingRule: AheadOf?

        public var id: String { matchText }
    }

    public let spendingRuleProposals: [Proposal?]
    public let tooGenericMatchTextCount: Int?
    public let changingNumberMatchTextCount: Int?
    public let overLimitMatchTextCount: Int?

    /// The proposals, with none missing.
    public var proposals: [Proposal] { spendingRuleProposals.compactMap { $0 } }
}

/// TransactionRowsPreview is what importing transaction rows would do,
/// written nowhere: the account, the rows it would add, the rows it holds
/// already, and what was checked.
public struct TransactionRowsPreview: Codable, Hashable, Sendable {
    public struct Row: Codable, Hashable, Sendable, Identifiable {
        public let rowNumber: Int?
        public let postedOn: String?
        public let description: String?
        public let amount: String?
        public let hasNearbyStoredTransaction: Bool?

        public var id: Int { rowNumber ?? 0 }
    }

    public let financeAccountId: String?
    public let accountName: String?
    public let isNewAccount: Bool?
    public let accountMatch: String?
    public let currencyCode: String?
    public let newTransactionRows: [Row?]?
    public let presentTransactionRows: [Row?]?
    public let firstPostedOn: String?
    public let lastPostedOn: String?
    public let moneyInAmount: String?
    public let moneyOutAmount: String?
    public let verificationSummary: String?

    public var newRows: [Row] { (newTransactionRows ?? []).compactMap { $0 } }
    public var presentRows: [Row] { (presentTransactionRows ?? []).compactMap { $0 } }
}

/// TransactionRowsFile reads the file the command line's
/// import-transactions reads: one JSON object of the ImportTransactions
/// arguments, with transactionRows in it.
public enum TransactionRowsFile {
    /// The amounts a file may write as numbers, which the API takes as text.
    private static let amountKeys: Set<String> = ["amount", "runningBalanceAmount", "totalAmount", "ledgerBalanceAmount"]

    /// The arguments in the file, or nil when it is not an object holding
    /// transactionRows. Amounts written as numbers become decimal text, and
    /// null fields are left out.
    public static func variables(from data: Data) -> [String: JSONValue]? {
        guard let decoded = try? JSONDecoder().decode(JSONValue.self, from: data),
            case .object(let fields) = decoded,
            case .array(_)? = fields["transactionRows"]
        else { return nil }
        guard case .object(let variables) = normalized(.object(fields), key: "") else { return nil }
        return variables
    }

    private static func normalized(_ value: JSONValue, key: String) -> JSONValue {
        switch value {
        case .number(let number) where amountKeys.contains(key):
            return .string(decimalText(number))
        case .array(let elements):
            return .array(elements.map { normalized($0, key: key) })
        case .object(let fields):
            var kept: [String: JSONValue] = [:]
            for (fieldKey, fieldValue) in fields where fieldValue != .null {
                kept[fieldKey] = normalized(fieldValue, key: fieldKey)
            }
            return .object(kept)
        default:
            return value
        }
    }

    /// A number as the shortest decimal text: "12.5", "-3", not "1.25e1".
    static func decimalText(_ number: Double) -> String {
        if number == number.rounded(), abs(number) < 1e15 { return String(Int64(number)) }
        var text = String(format: "%.8f", number)
        while text.hasSuffix("0") { text.removeLast() }
        if text.hasSuffix(".") { text.removeLast() }
        return text
    }
}

extension Documents {
    static let financeTradeFields = """
        id financeAccountId financeSecurityId financeSecurity { tickerSymbol securityName } providerTradeId tradedOn
        tradeKind tradeSubkind tradedQuantity unitPrice tradeAmount feeAmount currencyCode description
        """
    public static let financeTrades = """
        query ($financeAccountId: String, $financeSecurityId: String, $limit: Int, $offset: Int) {
          FinanceTrades(financeAccountId: $financeAccountId, financeSecurityId: $financeSecurityId, limit: $limit, offset: $offset) {
            financeTrades { \(financeTradeFields) }
            totalCount
          }
        }
        """
    public static let renameStatementAccount = """
        mutation ($financeAccountId: String!, $accountName: String, $accountMask: String) {
          RenameStatementAccount(financeAccountId: $financeAccountId, accountName: $accountName, accountMask: $accountMask) {
            id accountName accountMask
          }
        }
        """
    public static let deleteStatementAccount = """
        mutation ($financeAccountId: String!) {
          DeleteStatementAccount(financeAccountId: $financeAccountId) { financeAccountId deletedTransactionCount deletedAssetCount }
        }
        """
    public static let proposeSpendingRules = """
        query ($financeTransactionIds: [String!]!, $spendingCategoryId: String!) {
          ProposeSpendingRules(financeTransactionIds: $financeTransactionIds, spendingCategoryId: $spendingCategoryId) {
            spendingRuleProposals {
              matchText spendingCategoryId financeTransactionCount changedTransactionCount
              aheadOfSpendingRule { id matchText spendingCategoryId }
            }
            tooGenericMatchTextCount changingNumberMatchTextCount overLimitMatchTextCount
          }
        }
        """
    public static let financeProviderKinds = """
        query { FinanceProviders { providerKind isBrowserRequired } }
        """
    public static let importFinanceCredential = """
        mutation ($providerKind: String!, $credential: String!, $institutionName: String) {
          ImportFinanceCredential(providerKind: $providerKind, credential: $credential, institutionName: $institutionName) { id }
        }
        """
    static let transactionRowsVariables = """
        ($financeAccountId: String, $isNewAccount: Boolean, $institutionName: String!, $accountName: String,
          $accountNumber: String, $isAccountNumberPartial: Boolean, $statementAccountKind: String!, $currencyCode: String!,
          $bankCode: String, $transactionRows: [TransactionRowInput!]!, $ledgerBalanceAmount: String, $ledgerBalanceOn: String,
          $ledgerBalanceTimeZone: String, $monthlyTotals: [MonthlyTotalInput!])
        """
    static let transactionRowsArguments = """
        (financeAccountId: $financeAccountId, isNewAccount: $isNewAccount, institutionName: $institutionName,
          accountName: $accountName, accountNumber: $accountNumber, isAccountNumberPartial: $isAccountNumberPartial,
          statementAccountKind: $statementAccountKind, currencyCode: $currencyCode, bankCode: $bankCode,
          transactionRows: $transactionRows, ledgerBalanceAmount: $ledgerBalanceAmount, ledgerBalanceOn: $ledgerBalanceOn,
          ledgerBalanceTimeZone: $ledgerBalanceTimeZone, monthlyTotals: $monthlyTotals)
        """
    public static let previewImportTransactions = """
        query \(transactionRowsVariables) {
          PreviewImportTransactions\(transactionRowsArguments) {
            financeAccountId accountName isNewAccount accountMatch currencyCode
            newTransactionRows { rowNumber postedOn description amount hasNearbyStoredTransaction }
            presentTransactionRows { rowNumber postedOn description amount hasNearbyStoredTransaction }
            firstPostedOn lastPostedOn moneyInAmount moneyOutAmount verificationSummary
          }
        }
        """
    public static let importTransactions = """
        mutation \(transactionRowsVariables) {
          ImportTransactions\(transactionRowsArguments) {
            importedAt statementImportOrigin addedTransactionCount updatedTransactionCount unchangedTransactionCount financeAccountNames importErrorMessage
          }
        }
        """
}

extension Connection {
    /// An account's trades, newest first, a page at a time; narrowed to one
    /// security when it is given.
    public func financeTrades(financeAccountId: String, financeSecurityId: String? = nil, limit: Int = 50, offset: Int = 0) async throws -> FinanceTradePage {
        struct Answer: Decodable, Sendable { let FinanceTrades: FinanceTradePage }
        var variables: [String: JSONValue] = [
            "financeAccountId": .string(financeAccountId), "limit": .number(Double(limit)), "offset": .number(Double(offset)),
        ]
        if let financeSecurityId { variables["financeSecurityId"] = .string(financeSecurityId) }
        return try await execute(Documents.financeTrades, variables: variables, as: Answer.self).FinanceTrades
    }

    /// Gives an account of imported statements the person's own name and,
    /// when given, last digits; later imports keep them. A nil is left as it
    /// is, an empty mask takes the statements' own number back.
    public func renameStatementAccount(_ accountId: String, accountName: String?, accountMask: String?) async throws -> FinanceAccount? {
        struct Answer: Decodable, Sendable { let RenameStatementAccount: FinanceAccount? }
        var variables: [String: JSONValue] = ["financeAccountId": .string(accountId)]
        if let accountName { variables["accountName"] = .string(accountName) }
        if let accountMask { variables["accountMask"] = .string(accountMask) }
        return try await execute(Documents.renameStatementAccount, variables: variables, as: Answer.self).RenameStatementAccount
    }

    /// Deletes an account of imported statements with its transactions and
    /// its net worth history.
    public func deleteStatementAccount(_ accountId: String) async throws -> StatementAccountDeleted? {
        struct Answer: Decodable, Sendable { let DeleteStatementAccount: StatementAccountDeleted? }
        return try await execute(Documents.deleteStatementAccount, variables: ["financeAccountId": .string(accountId)], as: Answer.self).DeleteStatementAccount
    }

    /// The spending rules to offer for transactions filed under a category.
    /// Saves nothing.
    public func proposeSpendingRules(transactionIds: [String], categoryId: String) async throws -> SpendingRuleProposals? {
        struct Answer: Decodable, Sendable { let ProposeSpendingRules: SpendingRuleProposals? }
        let variables: [String: JSONValue] = [
            "financeTransactionIds": .array(transactionIds.map { .string($0) }), "spendingCategoryId": .string(categoryId),
        ]
        return try await execute(Documents.proposeSpendingRules, variables: variables, as: Answer.self).ProposeSpendingRules
    }

    /// The kinds of provider the server offers: "plaid", "simplefin".
    public func financeProviderKinds() async throws -> [String] {
        struct Answer: Decodable, Sendable { let FinanceProviders: [FinanceProvider?] }
        return try await execute(Documents.financeProviderKinds, as: Answer.self).FinanceProviders.compactMap { $0?.providerKind }
    }

    /// Brings in a provider connection made elsewhere: a Plaid access token
    /// or a SimpleFIN access URL already claimed. Never retried on its own.
    public func importFinanceCredential(providerKind: String, credential: String, institutionName: String?) async throws {
        struct Answer: Decodable, Sendable {
            struct Imported: Decodable, Sendable { let id: String }
            let ImportFinanceCredential: Imported?
        }
        var variables: [String: JSONValue] = ["providerKind": .string(providerKind), "credential": .string(credential)]
        if let institutionName { variables["institutionName"] = .string(institutionName) }
        _ = try await execute(Documents.importFinanceCredential, variables: variables, as: Answer.self)
    }

    /// What importing these rows would do. Nothing is written.
    public func previewImportTransactions(_ variables: [String: JSONValue]) async throws -> TransactionRowsPreview? {
        struct Answer: Decodable, Sendable { let PreviewImportTransactions: TransactionRowsPreview? }
        return try await execute(Documents.previewImportTransactions, variables: variables, as: Answer.self).PreviewImportTransactions
    }

    /// Imports the rows, and answers what the import did.
    public func importTransactions(_ variables: [String: JSONValue]) async throws -> StatementImport.LastImport? {
        struct Answer: Decodable, Sendable { let ImportTransactions: StatementImport.LastImport? }
        return try await execute(Documents.importTransactions, variables: variables, as: Answer.self).ImportTransactions
    }
}
