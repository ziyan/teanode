import Foundation
import XCTest

@testable import TeaNodeKit

final class DevicePasswordsTests: XCTestCase {
    private func decode(_ json: String) throws -> [SyncCredential] {
        struct Answer: Decodable { let ListSyncCredentials: [SyncCredential] }
        return try JSONDecoder().decode(Answer.self, from: Data(#"{"ListSyncCredentials":\#(json)}"#.utf8)).ListSyncCredentials
    }

    func testSyncCredentialDecodesLastUsedAtNullAndPresent() throws {
        let credentials = try decode(
            #"""
            [
              {"id":"a","credentialName":"iPhone","hasCalendarAccess":true,"hasRemindersAccess":false,"hasContactsAccess":false,"createdAt":"2030-01-01T00:00:00Z","lastUsedAt":null},
              {"id":"b","credentialName":"iPhone","hasCalendarAccess":true,"hasRemindersAccess":false,"hasContactsAccess":false,"createdAt":"2030-02-01T00:00:00.123456789Z","lastUsedAt":"2030-02-02T10:00:00Z"},
              {"id":"c","credentialName":"iPhone","hasCalendarAccess":true,"hasRemindersAccess":false,"hasContactsAccess":false}
            ]
            """#)
        XCTAssertNil(credentials[0].lastUsedAt)
        XCTAssertEqual(credentials[0].createdAt, "2030-01-01T00:00:00Z")
        XCTAssertEqual(credentials[1].lastUsedAt, "2030-02-02T10:00:00Z")
        XCTAssertNotNil(ServerTime.date(credentials[1].createdAt))
        XCTAssertNil(credentials[2].createdAt)
    }

    func testTheSyncListAsksForCreatedAndLastUsed() {
        XCTAssertTrue(Documents.syncCredentials.contains("createdAt lastUsedAt"))
    }

    func testNewestFirstOrdersByCreationTimeAndPutsUnreadableLast() throws {
        let credentials = try decode(
            #"""
            [
              {"id":"old","credentialName":"iPhone","hasCalendarAccess":true,"hasRemindersAccess":false,"hasContactsAccess":false,"createdAt":"2030-01-01T00:00:00Z"},
              {"id":"unknown","credentialName":"iPhone","hasCalendarAccess":true,"hasRemindersAccess":false,"hasContactsAccess":false},
              {"id":"new","credentialName":"iPhone","hasCalendarAccess":true,"hasRemindersAccess":false,"hasContactsAccess":false,"createdAt":"2030-03-01T00:00:00Z"}
            ]
            """#)
        XCTAssertEqual(credentials.newestFirst().map(\.id), ["new", "old", "unknown"])
    }

    func testAPasswordNeverUsedAfterAnHourOrIdleForNinetyDaysMayBeSafeToDelete() throws {
        let now = ServerTime.date("2030-06-01T12:00:00Z")!
        func password(createdAt: String, lastUsedAt: String?) -> MailAppPassword {
            MailAppPassword(mailboxId: "mailbox-a", id: "p", name: "Phone", createdAt: createdAt, lastUsedAt: lastUsedAt)
        }
        XCTAssertFalse(password(createdAt: "2030-06-01T11:30:00Z", lastUsedAt: nil).isMaybeSafeToDelete(now: now))
        XCTAssertTrue(password(createdAt: "2030-06-01T10:30:00Z", lastUsedAt: nil).isMaybeSafeToDelete(now: now))
        XCTAssertFalse(password(createdAt: "2029-01-01T00:00:00Z", lastUsedAt: "2030-05-01T00:00:00Z").isMaybeSafeToDelete(now: now))
        XCTAssertTrue(password(createdAt: "2029-01-01T00:00:00Z", lastUsedAt: "2030-01-01T00:00:00Z").isMaybeSafeToDelete(now: now))
    }
}
