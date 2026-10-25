import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// LocationNotificationMuteTests cover muting the arrived and left notices of
/// one person sharing: what the server's mute decodes to, when it is in
/// effect, and what each call sends.
final class LocationNotificationMuteTests: XCTestCase {
    let server = try! ServerAddress(parsing: "mail.example.com")

    private func connection(answering body: String) -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(
            server: server, clientId: "a-client", accessToken: "tnt_a", refreshToken: "", expiresAt: Date().addingTimeInterval(30 * 86400),
            username: "robin")
        return (Connection(account: account, transport: fake), fake)
    }

    private func sentVariables(_ request: URLRequest) throws -> [String: JSONValue] {
        let body = try JSONDecoder().decode([String: JSONValue].self, from: request.httpBody ?? Data())
        guard case .object(let variables) = body["variables"] else {
            XCTFail("no variables")
            return [:]
        }
        return variables
    }

    private func date(_ text: String) -> Date { ServerTime.date(text)! }

    private func decodePerson(notificationMute: String?) throws -> PersonLocation {
        let muteField = notificationMute.map { #","notificationMute":\#($0)"# } ?? ""
        let text = #"{"userId":"user-2","username":"sam","name":"Sam","avatarUrl":null,"avatarColorIndex":3,"shareLevel":"current","latestPoint":null,"openVisit":null\#(muteField)}"#
        return try JSONDecoder().decode(PersonLocation.self, from: Data(text.utf8))
    }

    // MARK: The mute

    func testAPersonWithNoMuteDecodesWithNone() throws {
        XCTAssertNil(try decodePerson(notificationMute: nil).notificationMute)
        let person = try decodePerson(notificationMute: "null")
        XCTAssertNil(person.notificationMute)
        XCTAssertFalse(person.isNotificationMuted(at: date("2024-01-02T14:00:00Z")))
    }

    func testAPersonWithAMuteUntilATimeDecodesIt() throws {
        let person = try decodePerson(notificationMute: #"{"mutedUntil":"2024-01-02T15:00:00Z"}"#)
        let mute = try XCTUnwrap(person.notificationMute)
        XCTAssertEqual(mute.mutedUntilDate, date("2024-01-02T15:00:00Z"))
        XCTAssertFalse(mute.isOpenEnded)
        XCTAssertTrue(person.isNotificationMuted(at: date("2024-01-02T14:59:59Z")))
        XCTAssertFalse(person.isNotificationMuted(at: date("2024-01-02T15:00:00Z")))
    }

    func testAMuteWithNoEndGoesOnUntilUnmuted() throws {
        let person = try decodePerson(notificationMute: #"{"mutedUntil":null}"#)
        let mute = try XCTUnwrap(person.notificationMute)
        XCTAssertNil(mute.mutedUntilDate)
        XCTAssertTrue(mute.isOpenEnded)
        XCTAssertTrue(person.isNotificationMuted(at: date("2030-01-01T00:00:00Z")))
    }

    // MARK: The calls

    func testMutesUntilTheEndGiven() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"MuteLocationNotifications":{"mutedUntil":"2024-01-02T15:00:00Z"}}}"#)
        let made = try await connection.muteLocationNotifications(userId: "user-2", until: date("2024-01-02T15:00:00Z"))
        XCTAssertEqual(made?.mutedUntilDate, date("2024-01-02T15:00:00Z"))
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["userId": .string("user-2"), "mutedUntil": .string("2024-01-02T15:00:00Z")])
    }

    func testMutesUntilUnmutedSendsNoEnd() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"MuteLocationNotifications":{"mutedUntil":null}}}"#)
        let made = try await connection.muteLocationNotifications(userId: "user-2")
        XCTAssertEqual(made?.isOpenEnded, true)
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["userId": .string("user-2")])
    }

    func testUnmutes() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"UnmuteLocationNotifications":true}}"#)
        let wasMuted = try await connection.unmuteLocationNotifications(userId: "user-2")
        XCTAssertTrue(wasMuted)
        XCTAssertEqual(try sentVariables(fake.sent[0]), ["userId": .string("user-2")])
        let (otherConnection, _) = self.connection(answering: #"{"data":{"UnmuteLocationNotifications":false}}"#)
        let wasMutedAgain = try await otherConnection.unmuteLocationNotifications(userId: "user-2")
        XCTAssertFalse(wasMutedAgain)
    }

    func testTheLocationDocumentsAskForTheMute() {
        for name in ["muteLocationNotifications", "unmuteLocationNotifications"] {
            XCTAssertNotNil(Documents.locationDocuments[name], name)
        }
        XCTAssertTrue(Documents.sharedLocations.contains("notificationMute { mutedUntil }"))
        XCTAssertTrue(Documents.location.contains("notificationMute { mutedUntil }"))
        XCTAssertTrue(Documents.muteLocationNotifications.contains("$userId: String!"))
        XCTAssertTrue(Documents.muteLocationNotifications.contains("$mutedUntil: DateTime"))
    }
}
