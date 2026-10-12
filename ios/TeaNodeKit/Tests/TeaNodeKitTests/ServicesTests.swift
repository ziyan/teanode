import Foundation
import XCTest

@testable import TeaNodeKit

final class ServicesTests: XCTestCase {
    private func connection(answering body: String) throws -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(server: try ServerAddress(parsing: "assistant.example.com"), clientId: "test-client", accessToken: "tnt_test", refreshToken: "", expiresAt: Date().addingTimeInterval(3600), username: "test-owner")
        return (Connection(account: account, transport: fake), fake)
    }

    func testOnlyAnOlderSchemaGetsLegacyCapabilities() async throws {
        let (older, _) = try connection(answering: #"{"errors":[{"message":"Cannot query field \"GetServiceCapabilities\" on type \"Query\"."}]}"#)
        let capabilities = try await older.serviceCapabilities()
        XCTAssertEqual(capabilities, .legacy)
        let (refused, _) = try connection(answering: #"{"errors":[{"message":"not logged in"}]}"#)
        do {
            _ = try await refused.serviceCapabilities()
            XCTFail("an authorization failure became legacy availability")
        } catch {}
    }

    func testDisabledCollectionsAreNotReadByTheWidget() async throws {
        let (connection, fake) = try connection(answering: #"{"data":{"GetServiceCapabilities":{"isMailRoutingEnabled":false,"isMailboxEnabled":false,"isCalendarEnabled":false,"isRemindersEnabled":false,"isContactsEnabled":false,"isAssistantEnabled":false,"hasPendingServiceRestart":false,"hasCompletedSetup":true}}}"#)
        _ = try await connection.glance()
        XCTAssertEqual(fake.sent.count, 1)
    }
}
