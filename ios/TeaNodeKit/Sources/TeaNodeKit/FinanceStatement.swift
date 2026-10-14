import Foundation

/// StatementImport is the person's statement import: the address to mail
/// OFX statements to (empty without a mailbox address), whether importing
/// is on, and what the last import did. The dashboard's Accounts tab shows
/// the same.
public struct StatementImport: Codable, Hashable, Sendable {
    /// LastImport is what one import of statement files did.
    public struct LastImport: Codable, Hashable, Sendable {
        public let importedAt: String?
        public let statementImportOrigin: String?
        public let addedTransactionCount: Int?
        public let updatedTransactionCount: Int?
        public let unchangedTransactionCount: Int?
        public let financeAccountNames: [String]?
        public let importErrorMessage: String?

        /// Whether anything went into an account.
        public var isImported: Bool { !(financeAccountNames ?? []).isEmpty }
    }

    public let sourceId: String?
    public let importAddress: String?
    public let isEnabled: Bool?
    public let lastStatementImport: LastImport?
}

extension Documents {
    public static let statementImport = """
        query {
          StatementImport {
            sourceId importAddress isEnabled
            lastStatementImport { importedAt statementImportOrigin addedTransactionCount updatedTransactionCount unchangedTransactionCount financeAccountNames importErrorMessage }
          }
        }
        """
    public static let regenerateStatementImportAddress = """
        mutation {
          RegenerateStatementImportAddress {
            sourceId importAddress isEnabled
            lastStatementImport { importedAt statementImportOrigin addedTransactionCount updatedTransactionCount unchangedTransactionCount financeAccountNames importErrorMessage }
          }
        }
        """
}

extension Connection {
    /// The statement import, its address made the first time it is asked.
    public func statementImport() async throws -> StatementImport? {
        struct Answer: Decodable, Sendable { let StatementImport: StatementImport? }
        return try await execute(Documents.statementImport, as: Answer.self).StatementImport
    }

    /// A new statement import address; mail to the old one is refused from
    /// now on.
    public func regenerateStatementImportAddress() async throws -> StatementImport? {
        struct Answer: Decodable, Sendable { let RegenerateStatementImportAddress: StatementImport? }
        return try await execute(Documents.regenerateStatementImportAddress, as: Answer.self).RegenerateStatementImportAddress
    }
}
