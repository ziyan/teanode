import Foundation
import XCTest

@testable import TeaNodeKit

final class DeviceSetupTests: XCTestCase {
    private let capabilityJSON =
        #"{"canSetUpMail":true,"canSetUpCalendar":true,"canSetUpReminders":false,"canSetUpContacts":true,"mailboxes":[{"id":"mailbox-a","name":"Personal","addresses":[]}]}"#
    private func capabilities() throws -> DeviceSetupCapabilities {
        try JSONDecoder().decode(DeviceSetupCapabilities.self, from: Data(capabilityJSON.utf8))
    }
    private func connection(answer: String) throws -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, answer) }
        let account = Account(
            server: try ServerAddress(parsing: "node.example.com"), clientId: "test-client", accessToken: "test-token", refreshToken: "",
            expiresAt: .distantFuture, username: "test-owner")
        return (Connection(account: account, transport: fake), fake)
    }
    func testGenericSetupNeedsNoMailboxAndExplicitShortcutSelectsOnlyOwnedMailbox() throws {
        let available = try capabilities()
        let generic = DeviceSetupSelection(deviceName: "Phone", capabilities: available)
        XCTAssertFalse(generic.hasMailAccess)
        XCTAssertNil(generic.mailboxId)
        XCTAssertTrue(generic.hasCalendarAccess)
        XCTAssertFalse(generic.hasRemindersAccess)
        XCTAssertTrue(generic.canCreateProfile(capabilities: available))
        let owned = DeviceSetupSelection(deviceName: "Phone", capabilities: available, preselectedMailboxId: "mailbox-a")
        XCTAssertTrue(owned.hasMailAccess)
        let foreign = DeviceSetupSelection(deviceName: "Phone", capabilities: available, preselectedMailboxId: "foreign-mailbox")
        XCTAssertFalse(foreign.hasMailAccess)
        XCTAssertNil(foreign.mailboxId)
    }
    func testUnavailableScopeAndForeignMailboxRefuseBeforeAnyMutation() async throws {
        let available = try capabilities()
        let (connection, fake) = try connection(answer: #"{"data":{}}"#)
        var selection = DeviceSetupSelection(deviceName: "Phone", capabilities: available)
        selection.hasRemindersAccess = true
        do {
            _ = try await connection.createDeviceSetupProfile(selection, capabilities: available)
            XCTFail("unavailable scope accepted")
        } catch {}
        selection.hasRemindersAccess = false
        selection.hasMailAccess = true
        selection.mailboxId = "foreign-mailbox"
        do {
            _ = try await connection.createDeviceSetupProfile(selection, capabilities: available)
            XCTFail("foreign mailbox accepted")
        } catch {}
        XCTAssertTrue(fake.sent.isEmpty)
    }
    func testMailboxFreeRequestClearsStaleMailboxAndKeepsExactSelectedScopes() async throws {
        let available = try capabilities()
        let answer =
            #"{"data":{"CreateDeviceSetupProfile":{"downloadPath":"/api/v1/mailbox/profile/test.mobileconfig","downloadLink":"https://node.example.com/api/v1/mailbox/profile/test.mobileconfig","expiresAt":"2099-01-01T00:00:00Z","mailboxAppPassword":null,"syncCredential":{"id":"sync-a","credentialName":"Phone","hasCalendarAccess":true,"hasRemindersAccess":false,"hasContactsAccess":true}}}}"#
        let (connection, fake) = try connection(answer: answer)
        var selection = DeviceSetupSelection(deviceName: "Phone", capabilities: available, preselectedMailboxId: "mailbox-a")
        selection.hasMailAccess = false
        let profile = try await connection.createDeviceSetupProfile(selection, capabilities: available)
        XCTAssertNil(profile.mailboxAppPassword)
        XCTAssertEqual(profile.syncCredential?.id, "sync-a")
        let requestBody = try XCTUnwrap(fake.sent.first?.httpBody)
        let request = try XCTUnwrap(JSONSerialization.jsonObject(with: requestBody) as? [String: Any])
        let variables = try XCTUnwrap(request["variables"] as? [String: Any])
        XCTAssertTrue(variables["mailboxId"] is NSNull)
        XCTAssertEqual(variables["hasMailAccess"] as? Bool, false)
        XCTAssertEqual(variables["hasRemindersAccess"] as? Bool, false)
    }
    func testProfileDoesNotSubstituteForeignDownloadOrBroaderCredential() async throws {
        let available = try capabilities()
        let selection = DeviceSetupSelection(deviceName: "Phone", capabilities: available)
        for substitution in ["foreign-host", "broader-scope"] {
            let host = substitution == "foreign-host" ? "foreign.example.com" : "node.example.com"
            let hasBroaderScope = substitution == "broader-scope" ? "true" : "false"
            let answer = """
                {"data":{"CreateDeviceSetupProfile":{"downloadPath":"/api/v1/mailbox/profile/test.mobileconfig","downloadLink":"https://\(host)/api/v1/mailbox/profile/test.mobileconfig","expiresAt":"2099-01-01T00:00:00Z","mailboxAppPassword":null,"syncCredential":{"id":"sync-a","credentialName":"Phone","hasCalendarAccess":true,"hasRemindersAccess":\(hasBroaderScope),"hasContactsAccess":true}}}}
                """
            let (connection, _) = try connection(answer: answer)
            do {
                _ = try await connection.createDeviceSetupProfile(selection, capabilities: available)
                XCTFail("substituted receipt accepted")
            } catch {}
        }
    }
    func testCapabilityRefusalDoesNotInventServiceAvailability() async throws {
        let (connection, _) = try connection(answer: #"{"errors":[{"message":"not permitted"}]}"#)
        do {
            _ = try await connection.deviceSetupCapabilities()
            XCTFail("refusal became availability")
        } catch {}
    }
    func testRefreshPrunesUnavailableChoicesWithoutSelectingNewServices() throws {
        let available = try capabilities()
        var selection = DeviceSetupSelection(deviceName: "Phone", capabilities: available, preselectedMailboxId: "mailbox-a")
        selection.hasContactsAccess = false
        let refreshed = try JSONDecoder().decode(
            DeviceSetupCapabilities.self,
            from: Data(
                #"{"canSetUpMail":false,"canSetUpCalendar":false,"canSetUpReminders":true,"canSetUpContacts":true,"mailboxes":[]}"#.utf8))
        let retained = selection.retainingAvailableServices(capabilities: refreshed)
        XCTAssertFalse(retained.hasMailAccess)
        XCTAssertNil(retained.mailboxId)
        XCTAssertFalse(retained.hasCalendarAccess)
        XCTAssertFalse(retained.hasContactsAccess)
        XCTAssertFalse(retained.hasRemindersAccess)
        selection.hasContactsAccess = true
        XCTAssertTrue(selection.retainingAvailableServices(capabilities: refreshed).canCreateProfile(capabilities: refreshed))
    }

    func testExpiredProfileRetainsRevocationReceipt() throws {
        let profile = try JSONDecoder().decode(
            CreatedDeviceSetupProfile.self,
            from: Data(
                #"{"downloadPath":"/api/v1/mailbox/profile/test.mobileconfig","downloadLink":"https://node.example.com/api/v1/mailbox/profile/test.mobileconfig","expiresAt":"2000-01-01T00:00:00Z","mailboxAppPassword":{"id":"password-a","mailboxId":"mailbox-a","name":"Phone","createdAt":"2000-01-01T00:00:00Z"},"syncCredential":{"id":"sync-a","credentialName":"Phone","hasCalendarAccess":true,"hasRemindersAccess":false,"hasContactsAccess":true}}"#
                    .utf8))
        let earlierDate = try XCTUnwrap(ServerTime.date("1999-01-01T00:00:00Z"))
        let laterDate = try XCTUnwrap(ServerTime.date("2001-01-01T00:00:00Z"))
        XCTAssertFalse(profile.hasExpired(at: earlierDate))
        XCTAssertTrue(profile.hasExpired(at: laterDate))
        XCTAssertTrue(profile.hasExpired(at: laterDate))
        XCTAssertEqual(profile.mailboxAppPassword?.mailboxId, "mailbox-a")
        XCTAssertEqual(profile.syncCredential?.id, "sync-a")
    }

    func testExpiredIssuedConnectionResponseRetainsValidatedCredentialReceipt() async throws {
        let answer =
            #"{"data":{"CreateDeviceSetupProfile":{"downloadPath":"/api/v1/mailbox/profile/test.mobileconfig","downloadLink":"https://node.example.com/api/v1/mailbox/profile/test.mobileconfig","expiresAt":"2000-01-01T00:00:00Z","mailboxAppPassword":null,"syncCredential":{"id":"sync-expired","credentialName":"Phone","hasCalendarAccess":true,"hasRemindersAccess":false,"hasContactsAccess":true}}}}"#
        let (connection, fake) = try connection(answer: answer)
        let available = try capabilities()
        let selection = DeviceSetupSelection(deviceName: "Phone", capabilities: available)
        let profile = try await connection.createDeviceSetupProfile(selection, capabilities: available)
        XCTAssertEqual(fake.sent.count, 1)
        XCTAssertTrue(profile.hasExpired())
        XCTAssertEqual(profile.syncCredential?.id, "sync-expired")
        XCTAssertEqual(profile.syncCredential?.hasCalendarAccess, true)
        XCTAssertEqual(profile.syncCredential?.hasRemindersAccess, false)
        XCTAssertNil(profile.mailboxAppPassword)
        XCTAssertThrowsError(try profile.downloadURL(server: ServerAddress(parsing: "node.example.com")))
    }

}
