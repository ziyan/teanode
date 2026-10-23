import Foundation

// Finance accounts shared with other people: the caller's own accounts shared
// with a person or a circle at a level, and what other people share with the
// caller. The levels, from least to most: a balance (the account's name, kind
// and balance, and for a card what it owes against its limit), spending (the
// months by spending category) and transactions (each one's day, amount,
// merchant and category). The agent has no part in any of it.

/// FinanceShareLevel is how much of a finance account somebody sees. Each
/// level shows what the one before it shows.
public enum FinanceShareLevel: String, CaseIterable, Hashable, Sendable {
    case balance
    case spending
    case transactions

    /// Orders the levels; a level reaches every one below it.
    public var rank: Int {
        switch self {
        case .balance: 1
        case .spending: 2
        case .transactions: 3
        }
    }

    public func isAtLeast(_ other: FinanceShareLevel) -> Bool {
        rank >= other.rank
    }
}

/// FinanceShareView is one share of one of the caller's finance accounts:
/// with a person or with a circle, never both.
public struct FinanceShareView: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let financeAccountId: String
    public let accountName: String
    public let accountMask: String?
    public let granteeUserId: String?
    public let granteeUsername: String?
    public let granteeName: String?
    public let granteeCircleId: String?
    public let granteeCircleName: String?
    public let financeShareLevel: String

    /// The level, when it is one the app knows.
    public var level: FinanceShareLevel? { FinanceShareLevel(rawValue: financeShareLevel) }

    /// "Checking ··1234".
    public var shownAccountName: String {
        guard let accountMask, !accountMask.isEmpty else { return accountName }
        return "\(accountName) ··\(accountMask)"
    }

    /// Who the share is with, as a target a sheet can show.
    public var target: ShareTarget {
        if let granteeCircleId { return .circle(granteeCircleId) }
        return .person(granteeUserId ?? "")
    }
}

/// SharedCreditUsage is what a shared credit card owes against its limit, in
/// the card's own currency.
public struct SharedCreditUsage: Codable, Hashable, Sendable {
    public let owedAmount: String?
    public let creditLimitAmount: String?
    public let creditLimitSource: String?
    /// 0.25 is a quarter of the limit used.
    public let usageShare: Double?
}

/// SharedFinanceAccount is somebody else's finance account as the level it is
/// shared at shows it.
public struct SharedFinanceAccount: Codable, Identifiable, Hashable, Sendable {
    public let financeAccountId: String
    public let ownerUserId: String
    public let ownerUsername: String
    public let ownerName: String?
    public let financeShareLevel: String
    public let accountName: String
    public let accountMask: String?
    public let accountKind: String?
    public let currencyCode: String
    public let currentBalance: String?
    public let balanceAt: String?
    public let creditUsage: SharedCreditUsage?

    public var id: String { financeAccountId }

    /// The level, when it is one the app knows.
    public var level: FinanceShareLevel? { FinanceShareLevel(rawValue: financeShareLevel) }

    /// Whether the account is shared at this level or more.
    public func isShared(atLeast other: FinanceShareLevel) -> Bool {
        level?.isAtLeast(other) ?? false
    }

    /// "Checking ··1234".
    public var shownName: String {
        guard let accountMask, !accountMask.isEmpty else { return accountName }
        return "\(accountName) ··\(accountMask)"
    }

    /// The owner's name, else their username.
    public var ownerShownName: String {
        if let ownerName, !ownerName.isEmpty { return ownerName }
        return ownerUsername
    }
}

/// SharedFinanceSpendingRow is one month of one spending category of a shared
/// account, in one currency. The category is empty for transactions with none.
public struct SharedFinanceSpendingRow: Codable, Identifiable, Hashable, Sendable {
    /// "2026-09".
    public let spendingMonth: String
    public let spendingCategoryName: String
    public let currencyCode: String
    public let moneyOut: String
    public let moneyIn: String
    public let financeTransactionCount: Int

    public var id: String { "\(spendingMonth)|\(spendingCategoryName)|\(currencyCode)" }
}

/// SharedFinanceTransaction is a transaction of a shared account as a reader
/// at the transactions level sees it: no description, no notes, no receipts.
public struct SharedFinanceTransaction: Codable, Identifiable, Hashable, Sendable {
    public let id: String
    public let postedOn: String
    public let amount: String
    public let currencyCode: String
    public let merchantName: String?
    public let spendingCategoryName: String?
}

public struct SharedFinanceTransactionPage: Codable, Hashable, Sendable {
    public let financeTransactions: [SharedFinanceTransaction]
    public let nextCursor: String?
    public let totalCount: Int
}

// MARK: Documents

extension Documents {
    public static let financeShares = """
        query {
          ListFinanceAccountShares {
            id financeAccountId accountName accountMask granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName financeShareLevel
          }
        }
        """
    public static let shareFinanceAccount = """
        mutation ($financeAccountId: String!, $granteeUserId: String, $granteeCircleId: String, $financeShareLevel: String!) {
          ShareFinanceAccount(financeAccountId: $financeAccountId, granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, financeShareLevel: $financeShareLevel) {
            id financeAccountId accountName accountMask granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName financeShareLevel
          }
        }
        """
    public static let unshareFinanceAccount = """
        mutation ($shareId: String!) { UnshareFinanceAccount(shareId: $shareId) }
        """
    public static let sharedFinanceAccounts = """
        query ($ownerUserId: String) {
          ListSharedFinanceAccounts(ownerUserId: $ownerUserId) {
            financeAccountId ownerUserId ownerUsername ownerName financeShareLevel accountName accountMask accountKind currencyCode currentBalance balanceAt
            creditUsage { owedAmount creditLimitAmount creditLimitSource usageShare }
          }
        }
        """
    public static let sharedFinanceSpending = """
        query ($financeAccountId: String!) {
          ListSharedFinanceSpending(financeAccountId: $financeAccountId) {
            spendingMonth spendingCategoryName currencyCode moneyOut moneyIn financeTransactionCount
          }
        }
        """
    public static let sharedFinanceTransactions = """
        query ($financeAccountId: String!, $limit: Int, $offset: Int) {
          ListSharedFinanceTransactions(financeAccountId: $financeAccountId, limit: $limit, offset: $offset) {
            nextCursor totalCount financeTransactions { id postedOn amount currencyCode merchantName spendingCategoryName }
          }
        }
        """

    /// The finance sharing documents, for the server's schema check.
    public static let financeSharingDocuments: [String: String] = [
        "financeShares": financeShares, "shareFinanceAccount": shareFinanceAccount, "unshareFinanceAccount": unshareFinanceAccount,
        "sharedFinanceAccounts": sharedFinanceAccounts, "sharedFinanceSpending": sharedFinanceSpending,
        "sharedFinanceTransactions": sharedFinanceTransactions,
    ]
}

extension Connection {
    /// The caller's own shares of their finance accounts.
    public func financeShares() async throws -> [FinanceShareView] {
        struct Answer: Decodable, Sendable { let ListFinanceAccountShares: [FinanceShareView] }
        return try await execute(Documents.financeShares, as: Answer.self).ListFinanceAccountShares
    }

    /// Shares an account with a person or a circle (one of the two) at a
    /// level, or changes the level of a share already made.
    @discardableResult
    public func shareFinanceAccount(financeAccountId: String, with target: ShareTarget, level: FinanceShareLevel) async throws -> FinanceShareView {
        struct Answer: Decodable, Sendable { let ShareFinanceAccount: FinanceShareView }
        return try await execute(
            Documents.shareFinanceAccount,
            variables: [
                "financeAccountId": .string(financeAccountId), "financeShareLevel": .string(level.rawValue),
                "granteeUserId": target.granteeUserId.map { .string($0) } ?? .null,
                "granteeCircleId": target.granteeCircleId.map { .string($0) } ?? .null,
            ], as: Answer.self
        ).ShareFinanceAccount
    }

    /// Takes a share back.
    public func unshareFinanceAccount(shareId: String) async throws {
        struct Answer: Decodable, Sendable { let UnshareFinanceAccount: Bool }
        _ = try await execute(Documents.unshareFinanceAccount, variables: ["shareId": .string(shareId)], as: Answer.self)
    }

    /// The finance accounts other people share with the caller, of one person
    /// when their identifier is given.
    public func sharedFinanceAccounts(ownerUserId: String? = nil) async throws -> [SharedFinanceAccount] {
        struct Answer: Decodable, Sendable { let ListSharedFinanceAccounts: [SharedFinanceAccount] }
        return try await execute(
            Documents.sharedFinanceAccounts, variables: ["ownerUserId": ownerUserId.map { .string($0) } ?? .null], as: Answer.self
        ).ListSharedFinanceAccounts
    }

    /// A shared account's money out and in by month and spending category.
    public func sharedFinanceSpending(financeAccountId: String) async throws -> [SharedFinanceSpendingRow] {
        struct Answer: Decodable, Sendable { let ListSharedFinanceSpending: [SharedFinanceSpendingRow] }
        return try await execute(
            Documents.sharedFinanceSpending, variables: ["financeAccountId": .string(financeAccountId)], as: Answer.self
        ).ListSharedFinanceSpending
    }

    /// A page of a shared account's transactions, newest first.
    public func sharedFinanceTransactions(financeAccountId: String, limit: Int = 50, offset: Int = 0) async throws -> SharedFinanceTransactionPage {
        struct Answer: Decodable, Sendable { let ListSharedFinanceTransactions: SharedFinanceTransactionPage }
        return try await execute(
            Documents.sharedFinanceTransactions,
            variables: [
                "financeAccountId": .string(financeAccountId), "limit": .number(Double(limit)), "offset": .number(Double(offset)),
            ], as: Answer.self
        ).ListSharedFinanceTransactions
    }
}
