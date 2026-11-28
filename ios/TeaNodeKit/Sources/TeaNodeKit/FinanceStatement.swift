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

extension Documents {
    public static let importStatement = """
        mutation ($agentAttachmentId: String) {
          ImportStatement(agentAttachmentId: $agentAttachmentId) {
            importedAt statementImportOrigin addedTransactionCount updatedTransactionCount unchangedTransactionCount financeAccountNames importErrorMessage
          }
        }
        """
}

/// StatementReading is what queuing the reading of one picture or PDF of a
/// statement answers: the upload and the background job that reads it.
public struct StatementReading: Codable, Hashable, Sendable {
    public let agentAttachmentId: String?
    public let agentJobId: String?
}

/// FinanceImport is one file read into finance in the background: a tax
/// document, a statement or a receipt, from an upload, a backed-up photo or
/// mail. The outcome text is what the run says came of it, empty while it
/// waits.
public struct FinanceImport: Codable, Hashable, Sendable, Identifiable {
    public let agentJobId: String
    /// `tax_document`, `statement` or `receipt`.
    public let financeImportKind: String
    /// `upload`, `photo` or `mail`.
    public let financeImportSource: String
    /// `queued`, `running`, `done`, `dead` (failed) or `cancelled`.
    public let agentJobStatus: String
    public let fileName: String?
    public let queuedAt: String?
    public let finishedAt: String?
    public let outcomeText: String?
    public let runConversationId: String?

    public var id: String { agentJobId }

    /// When it was last known to change: when it finished, else when it was queued.
    public var shownAt: Date? { ServerTime.date(finishedAt) ?? ServerTime.date(queuedAt) }
}

/// FinanceImportPage is a page of the files read into finance, newest first.
public struct FinanceImportPage: Codable, Hashable, Sendable {
    public let financeImports: [FinanceImport?]
    public let hasMore: Bool?
    public let totalCount: Int?

    /// The imports of the page, none missing.
    public var shownImports: [FinanceImport] { financeImports.compactMap { $0 } }
}

extension Documents {
    public static let readStatement = """
        mutation ($agentAttachmentId: String!) {
          ReadStatement(agentAttachmentId: $agentAttachmentId) { agentAttachmentId agentJobId }
        }
        """
    public static let financeImports = """
        query ($limit: Int, $offset: Int) {
          FinanceImports(limit: $limit, offset: $offset) {
            financeImports { agentJobId financeImportKind financeImportSource agentJobStatus fileName queuedAt finishedAt outcomeText runConversationId }
            hasMore totalCount
          }
        }
        """
}

extension Connection {
    /// Has the agent read the transactions of one account out of an uploaded
    /// picture or PDF (a screenshot of a bank app, a photo of a statement, a
    /// PDF statement) in the background. What came of it is posted to the
    /// feed and listed by `financeImports`.
    public func readStatement(attachmentId: String) async throws -> StatementReading? {
        struct Answer: Decodable, Sendable { let ReadStatement: StatementReading? }
        return try await execute(Documents.readStatement, variables: ["agentAttachmentId": .string(attachmentId)], as: Answer.self).ReadStatement
    }

    /// The files read into finance in the background, newest first.
    public func financeImports(limit: Int = 50, offset: Int = 0) async throws -> FinanceImportPage {
        struct Answer: Decodable, Sendable { let FinanceImports: FinanceImportPage }
        return try await execute(
            Documents.financeImports, variables: ["limit": .number(Double(limit)), "offset": .number(Double(offset))], as: Answer.self
        ).FinanceImports
    }

    /// Imports an OFX statement (.ofx, .qfx or .qbo) already uploaded to
    /// the agent's attachments, and answers what the import did.
    public func importStatement(attachmentId: String) async throws -> StatementImport.LastImport? {
        struct Answer: Decodable, Sendable { let ImportStatement: StatementImport.LastImport? }
        return try await execute(Documents.importStatement, variables: ["agentAttachmentId": .string(attachmentId)], as: Answer.self).ImportStatement
    }

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
