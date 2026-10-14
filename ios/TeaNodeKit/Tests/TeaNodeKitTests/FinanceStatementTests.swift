import Foundation
import XCTest

@testable import TeaNodeKit

final class FinanceStatementTests: XCTestCase {
    private func connection(answering body: String) throws -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(server: try ServerAddress(parsing: "assistant.example.com"), clientId: "test-client", accessToken: "tnt_test", refreshToken: "", expiresAt: Date().addingTimeInterval(3600), username: "test-owner")
        return (Connection(account: account, transport: fake), fake)
    }

    func testTheStatementImportIsReadAndItsAddressRenewed() async throws {
        let body = #"{"data":{"StatementImport":{"sourceId":"source-1","importAddress":"robin+statements-invented0token01@example.com","isEnabled":true,"lastStatementImport":{"importedAt":"2026-10-01T12:00:00Z","addedTransactionCount":3,"updatedTransactionCount":0,"unchangedTransactionCount":1,"financeAccountNames":["Invented Card ··1a11"],"importErrorMessage":""}},"RegenerateStatementImportAddress":{"sourceId":"source-1","importAddress":"robin+statements-invented0token02@example.com","isEnabled":true}}}"#
        let (connection, fake) = try connection(answering: body)
        let read = try await connection.statementImport()
        let statementImport = try XCTUnwrap(read)
        XCTAssertEqual(statementImport.importAddress, "robin+statements-invented0token01@example.com")
        XCTAssertEqual(statementImport.lastStatementImport?.addedTransactionCount, 3)
        XCTAssertEqual(statementImport.lastStatementImport?.isImported, true)
        let renewed = try await connection.regenerateStatementImportAddress()
        XCTAssertEqual(renewed?.importAddress, "robin+statements-invented0token02@example.com")
        XCTAssertNil(renewed?.lastStatementImport)
        let sent = try JSONSerialization.jsonObject(with: try XCTUnwrap(fake.sent.last?.httpBody)) as? [String: Any]
        XCTAssertTrue((sent?["query"] as? String)?.contains("RegenerateStatementImportAddress") == true)
    }

    func testAnUploadedStatementIsImported() async throws {
        let (connection, fake) = try connection(answering: #"{"data":{"ImportStatement":{"importedAt":"2026-10-14T12:00:00Z","addedTransactionCount":2,"updatedTransactionCount":0,"unchangedTransactionCount":0,"financeAccountNames":["Invented Card ··1a11"],"importErrorMessage":""}}}"#)
        let imported = try await connection.importStatement(attachmentId: "attachment-1")
        XCTAssertEqual(imported?.addedTransactionCount, 2)
        XCTAssertEqual(imported?.isImported, true)
        let sent = try JSONSerialization.jsonObject(with: try XCTUnwrap(fake.sent.last?.httpBody)) as? [String: Any]
        XCTAssertEqual((sent?["variables"] as? [String: Any])?["agentAttachmentId"] as? String, "attachment-1")
    }
}
