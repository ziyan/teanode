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

    func testAPictureOfAStatementIsQueuedToBeRead() async throws {
        let (connection, fake) = try connection(answering: #"{"data":{"ReadStatement":{"agentAttachmentId":"attachment-1","agentJobId":"job-1"}}}"#)
        let reading = try await connection.readStatement(attachmentId: "attachment-1")
        XCTAssertEqual(reading?.agentJobId, "job-1")
        let sent = try JSONSerialization.jsonObject(with: try XCTUnwrap(fake.sent.last?.httpBody)) as? [String: Any]
        XCTAssertTrue((sent?["query"] as? String)?.contains("ReadStatement") == true)
        XCTAssertEqual((sent?["variables"] as? [String: Any])?["agentAttachmentId"] as? String, "attachment-1")
    }

    func testTheFilesReadIntoFinanceArePagedNewestFirst() async throws {
        let body = #"{"data":{"FinanceImports":{"financeImports":[{"agentJobId":"job-2","financeImportKind":"statement","financeImportSource":"upload","agentJobStatus":"done","fileName":"picture-1.jpg","queuedAt":"2026-10-14T12:00:00Z","finishedAt":"2026-10-14T12:02:30.123456789Z","outcomeText":"Added 12 transactions to Example Bank Checking.","runConversationId":"conversation-1","agentAttachmentId":"attachment-1","attachmentContentType":"image/jpeg","mailboxItemId":""},{"agentJobId":"job-1","financeImportKind":"receipt","financeImportSource":"photo","agentJobStatus":"queued","fileName":"","queuedAt":"2026-10-13T08:00:00Z","finishedAt":"","outcomeText":"","runConversationId":"","agentAttachmentId":"","attachmentContentType":"","mailboxItemId":"mail-item-1"}],"hasMore":true,"totalCount":7}}}"#
        let (connection, fake) = try connection(answering: body)
        let page = try await connection.financeImports(limit: 2, offset: 4)
        XCTAssertEqual(page.shownImports.map(\.id), ["job-2", "job-1"])
        XCTAssertEqual(page.hasMore, true)
        XCTAssertEqual(page.totalCount, 7)
        let first = try XCTUnwrap(page.shownImports.first)
        XCTAssertEqual(first.financeImportKind, "statement")
        XCTAssertEqual(first.agentJobStatus, "done")
        XCTAssertEqual(first.shownAt, ServerTime.date("2026-10-14T12:02:30Z"))
        XCTAssertEqual(first.agentAttachmentId, "attachment-1")
        XCTAssertEqual(first.attachmentContentType, "image/jpeg")
        XCTAssertEqual(first.mailboxItemId, "")
        XCTAssertTrue(first.isPicture)
        XCTAssertEqual(page.shownImports.last?.mailboxItemId, "mail-item-1")
        XCTAssertFalse(try XCTUnwrap(page.shownImports.last).isPicture)
        XCTAssertEqual(page.shownImports.last?.shownAt, ServerTime.date("2026-10-13T08:00:00Z"))
        let sent = try JSONSerialization.jsonObject(with: try XCTUnwrap(fake.sent.last?.httpBody)) as? [String: Any]
        XCTAssertEqual((sent?["variables"] as? [String: Any])?["offset"] as? Int, 4)
        XCTAssertTrue((sent?["query"] as? String)?.contains("agentAttachmentId attachmentContentType mailboxItemId") == true)
    }
}
