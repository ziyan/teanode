import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// AccountProfileTests cover the profile, password, phones and passkeys
/// settings: what the server's answers decode to, and what the forms send.
final class AccountProfileTests: XCTestCase {
    let server = try! ServerAddress(parsing: "mail.example.com")

    private func connection(answering page: String) -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, page) }
        let account = Account(
            server: server, clientId: "a-client", accessToken: "tnt_a", refreshToken: "", expiresAt: Date().addingTimeInterval(30 * 86400),
            username: "robin")
        return (Connection(account: account, transport: fake), fake)
    }

    /// The variables of the one request the fake server was sent.
    private func sentVariables(_ fake: FakeServer) throws -> [String: JSONValue] {
        let body = try JSONDecoder().decode([String: JSONValue].self, from: fake.sent.first?.httpBody ?? Data())
        guard case .object(let variables) = body["variables"] else {
            XCTFail("no variables")
            return [:]
        }
        return variables
    }

    private let profile = AccountProfile(id: "user-1", username: "robin", name: "Robin", email: "robin@example.com", timezone: "Europe/Berlin", timezoneMode: "fixed")

    // MARK: Profile

    func testDecodesTheServersProfile() async throws {
        let (connection, _) = connection(answering: #"""
            {"data":{"GetCurrentUser":{"id":"user-1","username":"robin","name":"Robin","email":null,"timezone":"Europe/Berlin","timezoneMode":""}}}
            """#)
        let read = try await connection.accountProfile()
        XCTAssertEqual(read?.username, "robin")
        XCTAssertNil(read?.email)
        XCTAssertEqual(read?.chosenTimezoneMode, .auto)
    }

    func testAServerWithNoAccountsHasNoProfile() async throws {
        let (connection, _) = connection(answering: #"{"data":{"GetCurrentUser":null}}"#)
        let read = try await connection.accountProfile()
        XCTAssertNil(read)
    }

    func testAnUntouchedProfileHasNothingToSave() {
        let draft = ProfileDraft(profile)
        XCTAssertFalse(draft.hasChanges)
        XCTAssertFalse(draft.isRenaming)
        XCTAssertFalse(draft.canSave)
    }

    func testSpacesAroundTheWordsAreNoChange() {
        var draft = ProfileDraft(profile)
        draft.name = "  Robin "
        draft.username = "robin "
        XCTAssertFalse(draft.hasChanges)
    }

    func testANewUsernameIsARename() {
        var draft = ProfileDraft(profile)
        draft.username = "robin2"
        XCTAssertTrue(draft.isRenaming)
        XCTAssertTrue(draft.canSave)
        draft.username = "  "
        XCTAssertFalse(draft.canSave)
    }

    func testFollowingTheBrowserSendsNoZone() {
        var draft = ProfileDraft(profile)
        draft.choose(.auto, deviceTimezone: "Asia/Tokyo")
        draft.email = " someone@example.com "
        XCTAssertTrue(draft.hasChanges)
        XCTAssertEqual(draft.variables["timezone"], .string(""))
        XCTAssertEqual(draft.variables["timezoneMode"], .string("auto"))
        XCTAssertEqual(draft.variables["email"], .string("someone@example.com"))
        XCTAssertEqual(draft.variables["userId"], .string("user-1"))
    }

    func testAFixedZoneStartsFromTheAccountsOrThePhones() {
        var withZone = ProfileDraft(AccountProfile(id: "user-1", username: "robin", timezone: "Europe/Berlin", timezoneMode: "auto"))
        withZone.timezone = ""
        withZone.choose(.fixed, deviceTimezone: "Asia/Tokyo")
        XCTAssertEqual(withZone.timezone, "Europe/Berlin")

        var withoutZone = ProfileDraft(AccountProfile(id: "user-1", username: "robin"))
        withoutZone.choose(.fixed, deviceTimezone: "Asia/Tokyo")
        XCTAssertEqual(withoutZone.timezone, "Asia/Tokyo")
        XCTAssertEqual(withoutZone.variables["timezone"], .string("Asia/Tokyo"))

        var typed = ProfileDraft(AccountProfile(id: "user-1", username: "robin"))
        typed.timezone = "America/Denver"
        typed.choose(.fixed, deviceTimezone: "Asia/Tokyo")
        XCTAssertEqual(typed.timezone, "America/Denver")
    }

    func testSavingSendsTheDraftAndReadsTheAnswer() async throws {
        let (connection, fake) = connection(answering: #"""
            {"data":{"UpdateUser":{"id":"user-1","username":"robin2","name":"Robin","email":"robin@example.com","timezone":"Europe/Berlin","timezoneMode":"fixed"}}}
            """#)
        var draft = ProfileDraft(profile)
        draft.username = " robin2 "
        let saved = try await connection.updateAccountProfile(draft)
        XCTAssertEqual(saved.username, "robin2")
        let variables = try sentVariables(fake)
        XCTAssertEqual(variables["username"], .string("robin2"))
        XCTAssertEqual(variables["timezone"], .string("Europe/Berlin"))
        XCTAssertEqual(variables["timezoneMode"], .string("fixed"))
    }

    // MARK: Password

    func testAPasswordChangeNeedsBothTypingsToMatch() {
        let typed = ["correct", "horse", String(Int.random(in: 1000...9999))].joined(separator: "-")
        var draft = PasswordChangeDraft()
        XCTAssertFalse(draft.canChange)
        draft.currentPassword = typed + "-old"
        draft.newPassword = typed
        XCTAssertFalse(draft.doesConfirmationDiffer)
        XCTAssertFalse(draft.canChange)
        draft.newPasswordAgain = typed + "x"
        XCTAssertTrue(draft.doesConfirmationDiffer)
        XCTAssertFalse(draft.canChange)
        draft.newPasswordAgain = typed
        XCTAssertFalse(draft.doesConfirmationDiffer)
        XCTAssertTrue(draft.canChange)
    }

    func testChangingThePasswordSendsBoth() async throws {
        let current = ["made", "up", String(Int.random(in: 1000...9999))].joined()
        let replacement = ["also", "made", "up", String(Int.random(in: 1000...9999))].joined()
        let (connection, fake) = connection(answering: #"{"data":{"ChangePassword":{"authenticated":true,"username":"robin"}}}"#)
        try await connection.changePassword(currentPassword: current, newPassword: replacement)
        let variables = try sentVariables(fake)
        XCTAssertEqual(variables["currentPassword"], .string(current))
        XCTAssertEqual(variables["newPassword"], .string(replacement))
    }

    func testAWrongPasswordIsTheServersReason() async throws {
        let (connection, _) = connection(answering: #"{"data":{"ChangePassword":null},"errors":[{"message":"invalid credentials"}]}"#)
        do {
            try await connection.changePassword(currentPassword: "a", newPassword: "b")
            XCTFail("changed")
        } catch {
            XCTAssertEqual(error as? TeaNodeError, .refused("invalid credentials"))
        }
    }

    // MARK: Phones

    private let phones = #"""
        {"data":{"ListAgentPhones":{"phones":[
          {"id":"phone-1","phoneName":"Work phone","phoneModel":"iPhone","notificationKinds":["approval","message"],"canBeNotified":true,"lastSeenAt":"2026-01-02T03:04:05.123456789Z","lastDeliveredAt":null,"lastDeliveryError":""},
          {"id":"phone-2","phoneName":"","phoneModel":"iPad","notificationKinds":["message","approval","question","answer"],"canBeNotified":false,"lastSeenAt":null,"lastDeliveredAt":null,"lastDeliveryError":""},
          {"id":"phone-3","phoneName":"Old phone","phoneModel":"iPhone","notificationKinds":["message"],"canBeNotified":true,"lastSeenAt":null,"lastDeliveredAt":null,"lastDeliveryError":"the token is no longer valid"}
        ],"isPushEnabled":true,"notificationKinds":["message","approval","question","answer"]}}}
        """#

    func testDecodesTheServersPhones() async throws {
        let (connection, _) = connection(answering: phones)
        let list = try await connection.pairedPhones()
        XCTAssertTrue(list.isPushEnabled)
        XCTAssertEqual(list.notificationKinds, ["message", "approval", "question", "answer"])
        XCTAssertEqual(list.phones.map(\.id), ["phone-1", "phone-2", "phone-3"])
        XCTAssertEqual(list.phones.map(\.notificationState), [.notified, .notRegistered, .lastFailed])
    }

    func testAPhoneDraftSendsOnlyWhatChanged() async throws {
        let (connection, _) = connection(answering: phones)
        let phone = try await connection.pairedPhones().phones[0]
        var draft = PairedPhoneDraft(phone)
        XCTAssertFalse(draft.canSave)
        draft.notificationKinds = ["message", "approval"]
        XCTAssertFalse(draft.hasNotificationKindsChanged)
        draft.notificationKinds.insert("answer")
        XCTAssertTrue(draft.hasNotificationKindsChanged)
        XCTAssertFalse(draft.hasNameChanged)
        XCTAssertTrue(draft.canSave)
        XCTAssertEqual(draft.orderedNotificationKinds(among: ["message", "approval", "question", "answer"]), ["message", "approval", "answer"])
        draft.phoneName = " "
        XCTAssertFalse(draft.canSave)
        draft.phoneName = "Desk phone"
        XCTAssertTrue(draft.hasNameChanged)
    }

    func testPhoneActionsNameThePhone() async throws {
        let (renaming, renamed) = connection(answering: #"{"data":{"UpdateAgentPhone":{"id":"phone-1"}}}"#)
        try await renaming.renamePhone("phone-1", phoneName: "Desk phone")
        XCTAssertEqual(try sentVariables(renamed), ["phoneId": .string("phone-1"), "phoneName": .string("Desk phone")])

        let (testing, tested) = connection(answering: #"{"data":{"TestAgentPhone":{"id":"phone-1"}}}"#)
        try await testing.testPhone("phone-1")
        XCTAssertEqual(try sentVariables(tested), ["phoneId": .string("phone-1")])

        let (removing, removed) = connection(answering: #"{"data":{"RemoveAgentPhone":true}}"#)
        try await removing.removePhone("phone-1")
        XCTAssertEqual(try sentVariables(removed), ["phoneId": .string("phone-1")])
    }

    // MARK: Passkeys

    func testDecodesTheServersPasskeys() async throws {
        let (connection, _) = connection(answering: #"""
            {"data":{"GetPasskeyPolicy":{"enabled":true,"maximumPerUser":5},"ListPasskeys":[
              {"id":"passkey-1","name":"Laptop","createdAt":"2026-01-02T03:04:05Z","usedAt":"2026-02-03T04:05:06.5Z","ip":"192.0.2.10","backupEligible":true,"backupState":true,"transports":["internal","hybrid"]},
              null,
              {"id":"passkey-2","name":"Security key","createdAt":"2026-01-02T03:04:05Z","usedAt":"0001-01-01T00:00:00Z","ip":"","backupEligible":false,"backupState":false,"transports":["usb"]}
            ]}}
            """#)
        let read = try await connection.passkeys()
        XCTAssertEqual(read.policy, PasskeyPolicy(enabled: true, maximumPerUser: 5))
        XCTAssertEqual(read.passkeys.map(\.name), ["Laptop", "Security key"])
        XCTAssertTrue(read.passkeys[0].isSynced)
        XCTAssertFalse(read.passkeys[1].isSynced)
        XCTAssertNotNil(read.passkeys[0].createdDate)
        XCTAssertNotNil(read.passkeys[0].lastUsedDate)
        // The zero time is a passkey never used, not one used in the first
        // year.
        XCTAssertNil(read.passkeys[1].lastUsedDate)
    }

    func testDeletingAPasskeyNamesIt() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"DeletePasskey":true}}"#)
        try await connection.deletePasskey("passkey-1")
        XCTAssertEqual(try sentVariables(fake), ["passkeyId": .string("passkey-1")])
    }
}
