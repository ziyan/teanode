import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// SharingTests cover the Sharing screen: what the server's list decodes to,
/// and what stopping and declining a share send.
final class SharingTests: XCTestCase {
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

    private let listAnswer = #"""
        {"data":{"ListSharing":{
          "sharesMade":[
            {"shareKind":"calendar","shareId":"share-1","subjectId":"calendar-1","subjectName":"Calendar","subjectMask":"","shareLevel":"titles","shareScopes":[],"passwordItemCount":0,
             "granteeUserId":"user-2","granteeUsername":"sam","granteeName":"Sam Lee","granteeCircleId":null,"granteeCircleName":"","createdAt":"2030-01-02T00:00:00Z"},
            {"shareKind":"passwords","shareId":"","subjectId":"","subjectName":"","subjectMask":"","shareLevel":"","shareScopes":[],"passwordItemCount":3,
             "granteeUserId":null,"granteeUsername":"","granteeName":"","granteeCircleId":"circle-1","granteeCircleName":"Home","createdAt":"2030-01-03T00:00:00.123456Z"},
            {"shareKind":"finance","shareId":"share-4","subjectId":"account-1","subjectName":"Everyday Checking","subjectMask":"0001","shareLevel":"spending","shareScopes":[],"passwordItemCount":0,
             "granteeUserId":"user-2","granteeUsername":"sam","granteeName":"Sam Lee","granteeCircleId":null,"granteeCircleName":"","createdAt":"2030-01-06T00:00:00Z"}],
          "sharesReceived":[
            {"shareKind":"memory_page","shareId":"share-2","subjectId":"people/kim","subjectName":"Kim","subjectMask":"","shareLevel":"","shareScopes":["quotes"],"passwordItemCount":0,
             "ownerUserId":"user-2","ownerUsername":"sam","ownerName":"","granteeCircleId":null,"granteeCircleName":"","isDirect":true,"canDecline":true,"createdAt":"2030-01-04T00:00:00Z"},
            {"shareKind":"location","shareId":"share-3","subjectId":"","subjectName":"","subjectMask":"","shareLevel":"current","shareScopes":[],"passwordItemCount":0,
             "ownerUserId":"user-3","ownerUsername":"alex","ownerName":"Alex","granteeCircleId":"circle-1","granteeCircleName":"Home","isDirect":false,"canDecline":false,"createdAt":"2030-01-05T00:00:00Z"},
            {"shareKind":"recording","shareId":"share-5","subjectId":"recording-1","subjectName":"Standup","subjectMask":"","shareLevel":"transcript","shareScopes":[],"passwordItemCount":0,
             "ownerUserId":"user-2","ownerUsername":"sam","ownerName":"Sam Lee","granteeCircleId":null,"granteeCircleName":"","isDirect":true,"canDecline":true,"createdAt":"2030-01-07T00:00:00Z"}]}}}
        """#

    func testReadsWhatIsSharedByAndWithThePerson() async throws {
        let (connection, fake) = connection(answering: listAnswer)
        let list = try await connection.sharing()
        XCTAssertEqual(list.sharesMade.count, 3)
        XCTAssertEqual(list.sharesReceived.count, 3)
        XCTAssertFalse(list.isEmpty)
        XCTAssertEqual(fake.sent.count, 1)

        let calendar = try XCTUnwrap(list.sharesMade.first)
        XCTAssertEqual(calendar.knownKind, .calendar)
        XCTAssertEqual(calendar.shownGrantee, "Sam Lee")
        XCTAssertFalse(calendar.isCircle)
        XCTAssertTrue(calendar.canStop)

        // A finance account is named with the last digits of its number, and a recording with its title.
        let account = try XCTUnwrap(list.sharesMade.last)
        XCTAssertEqual(account.knownKind, .finance)
        XCTAssertEqual(account.subjectMask, "0001")
        XCTAssertEqual(account.shareLevel, "spending")
        XCTAssertTrue(account.canStop)
        let recording = try XCTUnwrap(list.sharesReceived.last)
        XCTAssertEqual(recording.knownKind, .recording)
        XCTAssertEqual(recording.subjectName, "Standup")
        XCTAssertTrue(recording.canDecline)

        // A share of passwords is counted, goes to a circle, and cannot be stopped from the list.
        let passwords = try XCTUnwrap(list.sharesMade.first { $0.knownKind == .passwords })
        XCTAssertEqual(passwords.knownKind, .passwords)
        XCTAssertEqual(passwords.passwordItemCount, 3)
        XCTAssertEqual(passwords.shownGrantee, "Home")
        XCTAssertTrue(passwords.isCircle)
        XCTAssertFalse(passwords.canStop)
        XCTAssertNotEqual(calendar.id, passwords.id)

        // The owner is shown by username when they have no name, and a share through a circle cannot be declined.
        let page = try XCTUnwrap(list.sharesReceived.first)
        XCTAssertEqual(page.knownKind, .memoryPage)
        XCTAssertEqual(page.shownOwner, "sam")
        XCTAssertEqual(page.shareScopes, ["quotes"])
        XCTAssertTrue(page.canDecline)
        let location = try XCTUnwrap(list.sharesReceived.first { $0.knownKind == .location })
        XCTAssertFalse(location.isDirect)
        XCTAssertFalse(location.canDecline)
        XCTAssertEqual(location.granteeCircleName, "Home")
    }

    func testReadsTheMomentWithOrWithoutFractionsOfASecond() {
        let plain = SharingMade(shareKind: "calendar", shareId: "a", createdAt: "2030-01-02T00:00:00Z")
        let fractional = SharingMade(shareKind: "calendar", shareId: "b", createdAt: "2030-01-02T00:00:00.5Z")
        XCTAssertNotNil(plain.since)
        XCTAssertNotNil(fractional.since)
        XCTAssertNil(SharingMade(shareKind: "calendar", shareId: "c", createdAt: "yesterday").since)
        // Go writes up to nine digits of fractions of a second.
        let nanoseconds = SharingMade(shareKind: "calendar", shareId: "d", createdAt: "2030-01-02T00:00:00.123456789Z")
        XCTAssertEqual(nanoseconds.since, plain.since)
    }

    func testAServerThatDoesNotSendTheAccountMaskStillDecodes() async throws {
        let answer = #"""
            {"data":{"ListSharing":{"sharesMade":[
              {"shareKind":"calendar","shareId":"share-9","subjectId":"calendar-1","subjectName":"Calendar","shareLevel":"titles","shareScopes":[],"passwordItemCount":0,
               "granteeUserId":"user-2","granteeUsername":"sam","granteeName":"","granteeCircleId":null,"granteeCircleName":"","createdAt":"2030-01-02T00:00:00Z"}],
             "sharesReceived":[
              {"shareKind":"calendar","shareId":"share-10","subjectId":"calendar-2","subjectName":"Calendar","shareLevel":"titles","shareScopes":[],"passwordItemCount":0,
               "ownerUserId":"user-3","ownerUsername":"kim","ownerName":"","granteeCircleId":null,"granteeCircleName":"","isDirect":true,"canDecline":true,"createdAt":"2030-01-02T00:00:00Z"}]}}}
            """#
        let (connection, _) = connection(answering: answer)
        let list = try await connection.sharing()
        XCTAssertEqual(list.sharesMade[0].shownSubjectMask, "")
        XCTAssertEqual(list.sharesReceived[0].shownSubjectMask, "")
    }

    func testAPageThatIsGoneIsKnownFromTheServersFlag() async throws {
        let answer = #"""
            {"data":{"ListSharing":{"sharesMade":[
              {"shareKind":"memory_page","shareId":"share-7","subjectId":"people/lee","subjectName":"people/lee","subjectMask":"","isSubjectDeleted":true,"shareLevel":"","shareScopes":[],"passwordItemCount":0,
               "granteeUserId":"user-2","granteeUsername":"sam","granteeName":"","granteeCircleId":null,"granteeCircleName":"","createdAt":"2030-01-02T00:00:00Z"},
              {"shareKind":"memory_page","shareId":"share-8","subjectId":"people/kim","subjectName":"Kim","subjectMask":"","isSubjectDeleted":false,"shareLevel":"","shareScopes":[],"passwordItemCount":0,
               "granteeUserId":"user-2","granteeUsername":"sam","granteeName":"","granteeCircleId":null,"granteeCircleName":"","createdAt":"2030-01-02T00:00:00Z"}],
             "sharesReceived":[]}}}
            """#
        let (connection, _) = connection(answering: answer)
        let list = try await connection.sharing()
        XCTAssertTrue(list.sharesMade[0].isPageGone)
        XCTAssertTrue(list.sharesMade[0].canStop)
        XCTAssertFalse(list.sharesMade[1].isPageGone)
        // A server that does not send the flag is an older one: the page is there.
        let (olderConnection, _) = self.connection(answering: listAnswer)
        let older = try await olderConnection.sharing()
        XCTAssertFalse(older.sharesMade[0].isPageGone)
    }

    func testAKindTheAppDoesNotKnowIsKeptButCannotBeStopped() async throws {
        let unknown = SharingMade(shareKind: "recipes", shareId: "share-9")
        XCTAssertNil(unknown.knownKind)
        XCTAssertFalse(unknown.canStop)
        let (connection, fake) = connection(answering: #"{"data":{}}"#)
        do {
            try await connection.stopSharing(unknown)
            XCTFail("an unknown kind was stopped")
        } catch let error as SharingError {
            XCTAssertEqual(error, .unknownKind("recipes"))
        }
        XCTAssertTrue(fake.sent.isEmpty)
    }

    func testStoppingASharePassesItToTheKindsOwnOperation() async throws {
        let cases: [(String, String)] = [
            ("calendar", "UnshareCalendar"), ("location", "UnshareLocation"), ("memory_page", "UnshareAgentPage"),
            ("photo_album", "RemovePhotoAlbumShare"), ("skill", "UnshareSkill"), ("recording", "UnshareRecording"),
            ("finance", "UnshareFinanceAccount"), ("shopping_list", "UnshareShoppingList"),
        ]
        for (kind, operation) in cases {
            let (connection, fake) = connection(answering: #"{"data":{"\#(operation)":true}}"#)
            try await connection.stopSharing(SharingMade(shareKind: kind, shareId: "share-1", granteeUserId: "user-2"))
            XCTAssertEqual(fake.sent.count, 1, kind)
            let body = String(decoding: fake.sent[0].httpBody ?? Data(), as: UTF8.self)
            XCTAssertTrue(body.contains(operation), "\(kind) sent \(body)")
        }
    }

    func testStoppingHealthSharesNoCategoriesWithTheGranteeItWasSharedWith() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"SetHealthShare":null}}"#)
        try await connection.stopSharing(SharingMade(shareKind: "health", shareId: "share-1", granteeUserId: "user-2"))
        let variables = try sentVariables(fake.sent[0])
        XCTAssertEqual(variables["granteeUserId"], .string("user-2"))
        XCTAssertEqual(variables["granteeCircleId"], .null)
        XCTAssertEqual(variables["healthCategories"], .array([]))
    }

    func testPasswordsAreNotStoppedFromTheList() async throws {
        let (connection, fake) = connection(answering: #"{"data":{}}"#)
        do {
            try await connection.stopSharing(SharingMade(shareKind: "passwords", shareId: "", passwordItemCount: 2))
            XCTFail("passwords were stopped from the list")
        } catch let error as SharingError {
            XCTAssertEqual(error, .passwordsAreStoppedOnTheirOwnScreen)
        }
        XCTAssertTrue(fake.sent.isEmpty)
    }

    func testDecliningARecordingOrFinanceShareSendsItsKind() async throws {
        for kind in ["recording", "finance"] {
            let (connection, fake) = connection(answering: #"{"data":{"DeclineShare":true}}"#)
            let share = SharingReceived(shareKind: kind, shareId: "share-6", ownerUserId: "user-2", ownerUsername: "sam", isDirect: true, canDecline: true)
            try await connection.declineShare(share)
            XCTAssertEqual(try sentVariables(fake.sent[0])["shareKind"], .string(kind))
        }
    }

    func testDecliningSendsTheKindAndTheShare() async throws {
        let (connection, fake) = connection(answering: #"{"data":{"DeclineShare":true}}"#)
        let share = SharingReceived(shareKind: "memory_page", shareId: "share-2", ownerUserId: "user-2", ownerUsername: "sam", isDirect: true, canDecline: true)
        try await connection.declineShare(share)
        let variables = try sentVariables(fake.sent[0])
        XCTAssertEqual(variables["shareKind"], .string("memory_page"))
        XCTAssertEqual(variables["shareId"], .string("share-2"))
    }
}
