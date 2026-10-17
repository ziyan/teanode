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

    func testPhotosAreOffUnlessTheServerSaysOtherwise() async throws {
        let older = #"{"data":{"GetServiceCapabilities":{"isMailRoutingEnabled":true,"isMailboxEnabled":true,"isCalendarEnabled":true,"isRemindersEnabled":true,"isContactsEnabled":true,"isAssistantEnabled":true,"hasPendingServiceRestart":false,"hasCompletedSetup":true}}}"#
        let (withoutPhotos, _) = try connection(answering: older)
        let withoutCapabilities = try await withoutPhotos.serviceCapabilities()
        XCTAssertFalse(withoutCapabilities.isPhotosEnabled)
        XCTAssertTrue(withoutCapabilities.isContactsEnabled)
        let newer = older.replacingOccurrences(of: #""isContactsEnabled":true,"#, with: #""isContactsEnabled":true,"isPhotosEnabled":true,"#)
        let (withPhotos, _) = try connection(answering: newer)
        let capabilities = try await withPhotos.serviceCapabilities()
        XCTAssertTrue(capabilities.isPhotosEnabled)
        XCTAssertFalse(ServiceCapabilities.legacy.isPhotosEnabled)
        XCTAssertFalse(ServiceCapabilities.unavailable.isPhotosEnabled)
    }

    func testAServerThatPredatesPhotosIsAskedWithoutTheField() async throws {
        let fake = FakeServer { request in
            let query = String(decoding: request.httpBody ?? Data(), as: UTF8.self)
            if query.contains("isPhotosEnabled") {
                return (200, #"{"errors":[{"message":"Cannot query field \"isPhotosEnabled\" on type \"ServiceCapabilities\"."}]}"#)
            }
            return (200, #"{"data":{"GetServiceCapabilities":{"isMailRoutingEnabled":true,"isMailboxEnabled":false,"isCalendarEnabled":true,"isRemindersEnabled":true,"isContactsEnabled":true,"isAssistantEnabled":true,"hasPendingServiceRestart":false,"hasCompletedSetup":true}}}"#)
        }
        let account = Account(server: try ServerAddress(parsing: "assistant.example.com"), clientId: "test-client", accessToken: "tnt_test", refreshToken: "", expiresAt: Date().addingTimeInterval(3600), username: "test-owner")
        let capabilities = try await Connection(account: account, transport: fake).serviceCapabilities()
        XCTAssertFalse(capabilities.isPhotosEnabled)
        XCTAssertFalse(capabilities.isMailboxEnabled)
        XCTAssertEqual(fake.sent.count, 2)
    }
}
