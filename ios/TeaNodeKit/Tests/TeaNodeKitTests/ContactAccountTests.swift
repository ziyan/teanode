import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif
import XCTest

@testable import TeaNodeKit

final class ContactAccountTests: XCTestCase {
    private func connection(answering body: String) throws -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(
            server: try ServerAddress(parsing: "assistant.example.com"), clientId: "test-client", accessToken: "tnt_test", refreshToken: "",
            expiresAt: Date().addingTimeInterval(3600), username: "test-owner")
        return (Connection(account: account, transport: fake), fake)
    }

    private func sentVariables(_ request: URLRequest) throws -> [String: Any] {
        let sent = try XCTUnwrap(JSONSerialization.jsonObject(with: try XCTUnwrap(request.httpBody)) as? [String: Any])
        return try XCTUnwrap(sent["variables"] as? [String: Any])
    }

    private let personJSON =
        #"{"userId":"user-2","username":"person-b","name":"Person B","avatarUrl":"/api/v1/avatar/user/user-2?v=1","avatarColorIndex":3}"#

    func testALinkedContactCarriesItsAccountAndAnOlderServerSaysNone() throws {
        let linked = #"{"id":"c1","name":"Person B","accountUserId":"user-2","account":\#(personJSON)}"#
        let contact = try JSONDecoder().decode(ContactDetail.self, from: Data(linked.utf8))
        XCTAssertTrue(contact.isLinkedToAccount)
        XCTAssertEqual(contact.account?.username, "person-b")
        XCTAssertEqual(contact.account?.displayName, "Person B")

        let plain = try JSONDecoder().decode(ContactDetail.self, from: Data(#"{"id":"c2","name":"Person C"}"#.utf8))
        XCTAssertFalse(plain.isLinkedToAccount)
        XCTAssertNil(plain.account)

        let cleared = try JSONDecoder().decode(ContactDetail.self, from: Data(#"{"id":"c3","accountUserId":null,"account":null}"#.utf8))
        XCTAssertFalse(cleared.isLinkedToAccount)
    }

    func testLinkingSendsTheBookTheContactAndTheAccountAndClearingSendsNull() async throws {
        let (connection, fake) = try connection(
            answering: #"{"data":{"SetContactAccount":{"id":"c1","accountUserId":"user-2","account":\#(personJSON)}}}"#)
        let link = try await connection.setContactAccount(addressBookId: "book-1", contactId: "c1", accountUserId: "user-2")
        XCTAssertEqual(link.accountUserId, "user-2")
        XCTAssertEqual(link.account?.userId, "user-2")
        let variables = try sentVariables(try XCTUnwrap(fake.sent.first))
        XCTAssertEqual(variables["addressBookId"] as? String, "book-1")
        XCTAssertEqual(variables["contactId"] as? String, "c1")
        XCTAssertEqual(variables["accountUserId"] as? String, "user-2")

        let (clearing, clearingFake) = try self.connection(answering: #"{"data":{"SetContactAccount":{"id":"c1","accountUserId":null,"account":null}}}"#)
        let cleared = try await clearing.setContactAccount(addressBookId: "book-1", contactId: "c1", accountUserId: nil)
        XCTAssertNil(cleared.account)
        let clearedVariables = try sentVariables(try XCTUnwrap(clearingFake.sent.first))
        XCTAssertTrue(clearedVariables["accountUserId"] is NSNull)
    }

    func testSuggestionsPairAContactWithAnAccountAndAreIdentifiedByTheContact() async throws {
        let (connection, fake) = try connection(
            answering:
                #"{"data":{"ListContactAccountSuggestions":[{"addressBookId":"book-2","contact":{"id":"c1","name":"Person B","organization":null,"emails":["b@example.com"],"phones":[],"hasPhoto":false},"account":\#(personJSON)}]}}"#
        )
        let suggestions = try await connection.contactAccountSuggestions()
        XCTAssertEqual(suggestions.map(\.id), ["c1"])
        XCTAssertEqual(suggestions[0].account.userId, "user-2")
        XCTAssertEqual(suggestions[0].addressBookId, "book-2")
        XCTAssertEqual(suggestions[0].contact.shownName, "Person B")
        let sent = String(decoding: try XCTUnwrap(fake.sent.first?.httpBody), as: UTF8.self)
        XCTAssertTrue(sent.contains("ListContactAccountSuggestions"))
    }

    func testAFaceGroupCarriesItsAccountAndAnOlderServerSaysNone() throws {
        let base =
            #""id":"g1","createdAt":"2026-03-01T00:00:00Z","updatedAt":"2026-03-02T00:00:00Z","contactId":"c1","contactAddressBookId":"b1","contactName":"Person B","faceCount":5,"photoCount":4,"facePictureUrl":"/f","coverPhotoFaceId":"f1","isHidden":false"#
        let linked = try JSONDecoder().decode(
            PhotoFaceGroup.self, from: Data(#"{\#(base),"accountUserId":"user-2","account":\#(personJSON)}"#.utf8))
        XCTAssertTrue(linked.isInCircles)
        XCTAssertEqual(linked.account?.avatarColorIndex, 3)

        let plain = try JSONDecoder().decode(PhotoFaceGroup.self, from: Data(#"{\#(base),"accountUserId":null,"account":null}"#.utf8))
        XCTAssertFalse(plain.isInCircles)
        XCTAssertEqual(plain.accountUserId, "")

        let older = try JSONDecoder().decode(PhotoFaceGroup.self, from: Data("{\(base)}".utf8))
        XCTAssertFalse(older.isInCircles)
    }

    func testACircleMemberSaysWhichContactOfTheCallerIsThemAndNothingWhenNone() throws {
        let member = #"{"userId":"user-2","username":"person-b","name":null,"avatarUrl":null,"avatarColorIndex":3,"memberRole":"member","joinedAt":"2030-01-02T00:00:00Z""#
        let linked = try JSONDecoder().decode(CircleMember.self, from: Data(#"\#(member),"linkedContactId":"c1"}"#.utf8))
        XCTAssertTrue(linked.isLinkedToContact)
        XCTAssertEqual(linked.linkedContactId, "c1")

        let empty = try JSONDecoder().decode(CircleMember.self, from: Data(#"\#(member),"linkedContactId":""}"#.utf8))
        XCTAssertFalse(empty.isLinkedToContact)
        let older = try JSONDecoder().decode(CircleMember.self, from: Data("\(member)}".utf8))
        XCTAssertFalse(older.isLinkedToContact)
    }

    func testPhotosOfAPersonSendTheAccountAndTheDocumentsAskForTheLink() async throws {
        let (connection, fake) = try connection(answering: #"{"data":{"ListPhotos":[]}}"#)
        let photos = try await connection.photos(in: .library, filter: PhotoFilter(accountUserId: "user-2", accountName: "Sam Placeholder"))
        XCTAssertEqual(photos.count, 0)
        let request = try XCTUnwrap(fake.sent.first)
        XCTAssertEqual(try sentVariables(request)["accountUserId"] as? String, "user-2")
        XCTAssertTrue(Documents.listPhotos.contains("accountUserId: $accountUserId"))
        XCTAssertTrue(Documents.contact.contains("accountUserId account {"))
        XCTAssertNotNil(Documents.calendarAndContacts["setContactAccount"])
        XCTAssertNotNil(Documents.calendarAndContacts["contactAccountSuggestions"])
    }

    func testAPersonFilterIsActiveCombinesWithDaysAndSendsOnlyTheID() {
        XCTAssertFalse(PhotoFilter(accountName: "Sam Placeholder").isActive)
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(secondsFromGMT: 0)!
        let day = calendar.date(from: DateComponents(year: 2026, month: 3, day: 10, hour: 15))!
        var filter = PhotoFilter(firstDay: day, lastDay: day, accountUserId: "user-2", accountName: "Sam Placeholder")
        XCTAssertTrue(filter.isActive)
        let sent = filter.variables(calendar: calendar)
        XCTAssertEqual(sent["accountUserId"], .string("user-2"))
        XCTAssertEqual(sent["takenAfter"], .string("2026-03-10T00:00:00Z"))
        XCTAssertEqual(sent.count, 3)
        // Clearing the person keeps the days.
        filter.accountUserId = nil
        XCTAssertNil(filter.variables(calendar: calendar)["accountUserId"])
        XCTAssertNotNil(filter.variables(calendar: calendar)["takenAfter"])
    }
}
