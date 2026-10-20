import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif
import XCTest

@testable import TeaNodeKit

/// PersonCardTests cover what the card of a person is read from: what the
/// server's answer decodes to for a linked person, one without a contact and a
/// contact without an account, what each call sends, and the small facts the
/// card's buttons are made from.
final class PersonCardTests: XCTestCase {
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

    private func decode(_ json: String) throws -> PersonCard {
        try JSONDecoder().decode(PersonCard.self, from: Data(json.utf8))
    }

    private let personJSON =
        #"{"userId":"user-2","username":"sam","name":"Sam Fixture","avatarUrl":"/api/v1/avatar/user/user-2?v=1","avatarColorIndex":2,"memberRole":"member","joinedAt":"2030-01-02T00:00:00Z","relationshipKind":"parent","relationshipTermGender":"feminine","relationshipWord":"mother","linkedContactId":"contact-2"}"#

    /// The same person for somebody who has no contact for them: the member says none is linked.
    private var unlinkedPersonJSON: String {
        personJSON.replacingOccurrences(of: "\"linkedContactId\":\"contact-2\"", with: "\"linkedContactId\":\"\"")
    }

    private let contactJSON =
        #"{"id":"contact-2","name":"Sam Fixture","organization":null,"emails":["sam@example.org","sam.work@example.org"],"phones":["+1 (555) 0142"],"note":"Likes tea","hasPhoto":false,"addresses":[{"street":"1 Example Road","locality":"Sampleton","region":"","postalCode":"00000","country":"Exampleland"}],"accountUserId":"user-2","account":{"userId":"user-2","username":"sam","name":"Sam Fixture","avatarUrl":null,"avatarColorIndex":2}}"#

    private let placeJSON =
        #"{"id":"place-1","userId":"user-2","placeName":"Fixture Park","isNamedByPerson":false,"placeLabel":"","latitudeDegrees":10.5,"longitudeDegrees":20.5,"radiusMeters":75,"osmType":"way","osmId":1,"osmName":"Fixture Park","osmCategory":"leisure=park","addressText":"","isAddressTyped":false,"visitCount":3,"lastVisitAt":"2030-01-02T10:00:00Z","namingErrorMessage":"","createdAt":"2030-01-01T00:00:00Z","updatedAt":"2030-01-02T10:00:00Z"}"#

    private var fullCardJSON: String {
        #"""
        {"person":\#(personJSON),"contact":\#(contactJSON),
         "sharedCircles":[{"circleId":"circle-1","circleName":"Home","circleKind":"family"}],"canMessage":true,
         "location":{"sectionState":"shown","hasMoreTodayVisits":true,
           "location":{"userId":"user-2","username":"sam","name":"Sam Fixture","avatarUrl":null,"avatarColorIndex":2,"shareLevel":"history",
             "latestPoint":{"latitudeDegrees":10.5,"longitudeDegrees":20.5,"accuracyMeters":12,"altitudeMeters":null,"speedMetersPerSecond":null,"courseDegrees":null,"measuredAt":"2030-01-02T10:30:00Z","locationSource":"phone"},
             "openVisit":{"id":"visit-1","arrivedAt":"2030-01-02T09:00:00Z","departedAt":null,"durationSeconds":null,"latitudeDegrees":10.5,"longitudeDegrees":20.5,"accuracyMeters":30,"visitSource":"visit","place":\#(placeJSON)}},
           "todayVisits":[{"id":"visit-1","arrivedAt":"2030-01-02T09:00:00Z","departedAt":null,"durationSeconds":null,"latitudeDegrees":10.5,"longitudeDegrees":20.5,"accuracyMeters":30,"visitSource":"visit","place":\#(placeJSON)}]},
         "health":{"sectionState":"shown","hasMore":false,"healthTypes":[
           {"healthType":"stepCount","healthCategory":"activity","typeName":"Steps","valueKind":"quantity","quantityUnit":"count","summaryKind":"sum","firstDay":"2029-12-01","lastDay":"2030-01-02","sampleCount":0,
            "latestDaySummary":{"summaryDay":"2030-01-02","quantityUnit":"count","sumValue":8214,"averageValue":null,"minimumValue":null,"maximumValue":null,"sampleCount":40,"durationSeconds":null,"categorySeconds":null}}]},
         "photos":{"photosState":"shown","hasMorePhotos":true,"albumsState":"shown","hasMoreAlbums":false,
           "photos":[{"id":"photo-1","createdAt":"2030-01-01T00:00:00Z","photoKind":"picture","takenAt":"2030-01-01T10:00:00Z","takenAtOffsetMinutes":0,"widthPixels":400,"heightPixels":300,"durationMS":0,"latitude":null,"longitude":null,"placeName":"","cameraMake":"","cameraModel":"","originalFilename":"a.jpg","isFavorite":false,"isScreenshot":false,"photoProcessingState":"ready","trashedAt":null,"smallPreviewUrl":"/p/1/small","largePreviewUrl":"/p/1/large","videoPreviewUrl":"","photoFileRoles":["original"],"processingErrorMessage":"","isOwnedByCaller":true,"ownerName":""}],
           "albums":[{"id":"album-1","albumTitle":"Picnic","createdAt":"2030-01-01T00:00:00Z","updatedAt":"2030-01-02T00:00:00Z","coverPhotoId":null,"photoCount":12,"firstTakenAt":null,"lastTakenAt":null,"coverPreviewUrl":"/p/1/small","ownerUserId":"user-2","circleId":null,"ownerUsername":"sam","ownerName":"Sam Fixture","ownerAvatarUrl":null,"ownerAvatarColorIndex":2,"circleName":"","isOwnedByCaller":false,"shareLevel":"view","canCallerContribute":false,"canCallerManage":false,"isLocationIncluded":true}]},
         "mail":{"sectionState":"shown","hasMore":true,"mails":[
           {"mailboxItemId":"item-1","mailboxId":"box-1","folderPath":"Inbox","subject":"Lunch on Friday","fromAddress":"sam@example.org","fromName":"Sam Fixture","isFromPerson":true,"isUnread":true,"receivedAt":"2030-01-02T08:00:00.123456789Z","snippet":"Sam suggests the noodle place."},
           {"mailboxItemId":"item-2","mailboxId":"box-1","folderPath":"Sent","subject":"Re: Lunch on Friday","fromAddress":"me@example.com","fromName":"","isFromPerson":false,"isUnread":false,"receivedAt":"2030-01-02T08:30:00Z","snippet":""}]},
         "recordings":{"sectionState":"shown","hasMore":false,"recordings":[
           {"recordingId":"recording-1","title":"Planning the picnic","recordedAt":"2030-01-01T12:00:00Z","durationMS":600000,"spokenPassageCount":12}]},
         "memory":{"sectionState":"shown","hasMore":false,"pages":[
           {"nodePath":"people/sam","nodeName":"Sam","nodeKind":"person","summary":"Likes tea.","updatedAt":"2030-01-02T00:00:00Z"}]}}
        """#
    }

    func testALinkedPersonsCardDecodesEverySection() throws {
        let card = try decode(fullCardJSON)
        XCTAssertEqual(card.person?.userId, "user-2")
        XCTAssertEqual(card.person?.relationshipWord, "mother")
        XCTAssertTrue(card.hasContact)
        XCTAssertEqual(card.contact?.id, "contact-2")
        XCTAssertTrue(card.contact?.isLinkedToAccount == true)
        XCTAssertEqual(card.sharedCircles.map(\.circleName), ["Home"])
        XCTAssertEqual(card.sharedCircles.first?.knownCircleKind, .family)
        XCTAssertTrue(card.canMessage)

        let location = try XCTUnwrap(card.location)
        XCTAssertEqual(location.sectionState, .shown)
        XCTAssertEqual(location.location?.level, .history)
        XCTAssertEqual(location.location?.openVisit?.place.placeName, "Fixture Park")
        XCTAssertEqual(location.todayVisits.map(\.id), ["visit-1"])
        XCTAssertTrue(location.hasMoreTodayVisits)

        let health = try XCTUnwrap(card.health)
        XCTAssertEqual(health.healthTypes.map(\.healthType), ["stepCount"])
        XCTAssertEqual(health.healthTypes.first?.latestDaySummary?.sumValue, 8214)

        let photos = try XCTUnwrap(card.photos)
        XCTAssertEqual(photos.photosState, .shown)
        XCTAssertEqual(photos.photos.map(\.id), ["photo-1"])
        XCTAssertTrue(photos.hasMorePhotos)
        XCTAssertEqual(photos.albums.map(\.albumTitle), ["Picnic"])
        XCTAssertTrue(photos.albums.first?.isSharedWithCaller == true)

        let mail = try XCTUnwrap(card.mail)
        XCTAssertEqual(mail.mails.map(\.mailboxItemId), ["item-1", "item-2"])
        XCTAssertEqual(mail.mails[0].senderText, "Sam Fixture")
        XCTAssertEqual(mail.mails[1].senderText, "me@example.com")
        XCTAssertEqual(mail.mails[0].receivedDate, ServerTime.date("2030-01-02T08:00:00Z"))
        XCTAssertTrue(mail.hasMore)

        let recordings = try XCTUnwrap(card.recordings)
        XCTAssertEqual(recordings.recordings.first?.spokenPassageCount, 12)
        XCTAssertNotNil(recordings.recordings.first?.recordedDate)

        let memory = try XCTUnwrap(card.memory)
        XCTAssertEqual(memory.pages.map(\.nodePath), ["people/sam"])
    }

    func testAPersonWithNoContactAsksForOneAndKeepsTheAccountsSections() throws {
        let card = try decode(
            #"""
            {"person":\#(unlinkedPersonJSON),"contact":null,
             "sharedCircles":[],"canMessage":true,
             "location":{"sectionState":"shown","location":null,"todayVisits":null},
             "health":{"sectionState":"notShared","healthTypes":null,"hasMore":false},
             "photos":{"photosState":"needsContact","photos":null,"hasMorePhotos":false,"albumsState":"shown","albums":null,"hasMoreAlbums":false},
             "mail":{"sectionState":"needsContact","mails":null,"hasMore":false},
             "recordings":{"sectionState":"needsContact","recordings":null,"hasMore":false},
             "memory":{"sectionState":"needsContact","pages":null,"hasMore":false}}
            """#)
        XCTAssertFalse(card.hasContact)
        XCTAssertTrue(card.needsContact)
        XCTAssertNil(card.contact)
        XCTAssertFalse(card.person?.isLinkedToContact ?? true)
        XCTAssertTrue(card.sharedCircles.isEmpty)
        XCTAssertEqual(card.health?.sectionState, .notShared)
        XCTAssertTrue(card.health?.healthTypes.isEmpty == true)
        XCTAssertEqual(card.photos?.photosState, .needsContact)
        XCTAssertEqual(card.photos?.albumsState, .shown)
        XCTAssertEqual(card.mail?.sectionState, .needsContact)
        XCTAssertTrue(card.location?.todayVisits.isEmpty == true)
        XCTAssertNil(card.mailAddress)
        XCTAssertNil(card.callURL)
        XCTAssertNil(card.directionsURL)
        XCTAssertEqual(card.displayName, "Sam Fixture")
    }

    func testAContactWithoutAnAccountHasNoPersonAndLeavesTheAccountsSectionsOut() throws {
        let card = try decode(
            #"""
            {"person":null,"contact":{"id":"contact-9","name":null,"organization":"Fixture Books","emails":["orders@example.org"],"phones":[],"hasPhoto":false,"addresses":[],"accountUserId":null,"account":null},
             "sharedCircles":null,"canMessage":false,
             "location":{"sectionState":"unavailable","location":null,"todayVisits":[]},
             "health":{"sectionState":"unavailable","healthTypes":[],"hasMore":false},
             "photos":{"photosState":"shown","photos":[],"hasMorePhotos":false,"albumsState":"unavailable","albums":[],"hasMoreAlbums":false},
             "mail":{"sectionState":"shown","mails":[],"hasMore":false},
             "recordings":{"sectionState":"shown","recordings":[],"hasMore":false},
             "memory":{"sectionState":"shown","pages":[],"hasMore":false}}
            """#)
        XCTAssertNil(card.person)
        XCTAssertFalse(card.needsContact)
        XCTAssertEqual(card.displayName, "orders@example.org")
        XCTAssertEqual(card.mailAddress, "orders@example.org")
        XCTAssertFalse(card.canMessage)
        XCTAssertEqual(card.location?.sectionState, .unavailable)
        XCTAssertEqual(card.photos?.albumsState, .unavailable)
        XCTAssertTrue(card.sharedCircles.isEmpty)
    }

    func testASectionStateThisAppDoesNotKnowIsUnavailableAndAMissingSectionIsNil() throws {
        let card = try decode(#"{"person":null,"contact":null,"canMessage":false,"health":{"sectionState":"snoozed","healthTypes":[],"hasMore":false}}"#)
        XCTAssertEqual(card.health?.sectionState, .unavailable)
        XCTAssertNil(card.location)
        XCTAssertNil(card.photos)
        XCTAssertNil(card.mail)
        XCTAssertNil(card.recordings)
        XCTAssertNil(card.memory)
        XCTAssertEqual(card.displayName, "")
    }

    func testASectionIsListedUnlessItIsLeftOutOrWaitsForAContact() {
        XCTAssertTrue(PersonCardSectionState.shown.isListed)
        XCTAssertTrue(PersonCardSectionState.notShared.isListed)
        XCTAssertTrue(PersonCardSectionState.failed.isListed)
        XCTAssertFalse(PersonCardSectionState.unavailable.isListed)
        XCTAssertFalse(PersonCardSectionState.needsContact.isListed)
    }

    func testTheButtonsAreMadeFromTheContactAndTheLatestPoint() throws {
        let card = try decode(fullCardJSON)
        XCTAssertEqual(card.mailAddress, "sam@example.org")
        XCTAssertEqual(card.callURL?.absoluteString, "tel:+15550142")
        XCTAssertEqual(card.directionsURL?.absoluteString, "https://maps.apple.com/?daddr=10.5,20.5")
    }

    func testAKindIsTodayOnlyWhenItsLatestDayIsTheDayGiven() throws {
        let card = try decode(fullCardJSON)
        let steps = try XCTUnwrap(card.health?.healthTypes.first)
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(secondsFromGMT: 0)!
        let sameDay = calendar.date(from: DateComponents(year: 2030, month: 1, day: 2, hour: 23))!
        let nextDay = calendar.date(from: DateComponents(year: 2030, month: 1, day: 3, hour: 1))!
        XCTAssertTrue(PersonCardHealthSection.isToday(steps, today: sameDay, calendar: calendar))
        XCTAssertFalse(PersonCardHealthSection.isToday(steps, today: nextDay, calendar: calendar))
    }

    func testAnAccountAndAContactAreNamedAsTheServerTakesThem() {
        XCTAssertEqual(PersonCardTarget.account(userId: "user-2").variables, ["userId": .string("user-2")])
        XCTAssertEqual(
            PersonCardTarget.contact(addressBookId: "book-1", contactId: "contact-9").variables,
            ["addressBookId": .string("book-1"), "contactId": .string("contact-9")])
        XCTAssertNotEqual(PersonCardTarget.account(userId: "user-2").id, PersonCardTarget.contact(addressBookId: "user-2", contactId: "user-2").id)
    }

    func testReadingACardSendsTheAccountAndTheLimitAndNothingElse() async throws {
        let (connection, fake) = try connection(answering: #"{"data":{"ReadPersonCard":\#(fullCardJSON)}}"#)
        let card = try await connection.readPersonCard(.account(userId: "user-2"))
        XCTAssertEqual(card.person?.username, "sam")
        let variables = try sentVariables(try XCTUnwrap(fake.sent.first))
        XCTAssertEqual(variables["userId"] as? String, "user-2")
        XCTAssertEqual(variables["itemLimit"] as? Int, PersonCard.itemLimit)
        XCTAssertNil(variables["addressBookId"])
        XCTAssertNil(variables["contactId"])
        let sent = String(decoding: try XCTUnwrap(fake.sent.first?.httpBody), as: UTF8.self)
        XCTAssertTrue(sent.contains("ReadPersonCard"))
    }

    func testReadingAContactsCardSendsTheBookAndTheContactAndNoAccount() async throws {
        let (connection, fake) = try connection(
            answering: #"{"data":{"ReadPersonCard":{"person":null,"contact":null,"sharedCircles":[],"canMessage":false}}}"#)
        let card = try await connection.readPersonCard(.contact(addressBookId: "book-1", contactId: "contact-9"), itemLimit: 6)
        XCTAssertNil(card.person)
        let variables = try sentVariables(try XCTUnwrap(fake.sent.first))
        XCTAssertEqual(variables["addressBookId"] as? String, "book-1")
        XCTAssertEqual(variables["contactId"] as? String, "contact-9")
        XCTAssertEqual(variables["itemLimit"] as? Int, 6)
        XCTAssertNil(variables["userId"])
    }

    func testARefusedCardIsAnErrorNotAnEmptyCard() async throws {
        let (connection, _) = try connection(answering: #"{"data":null,"errors":[{"message":"not found"}]}"#)
        do {
            _ = try await connection.readPersonCard(.account(userId: "user-9"))
            XCTFail("expected a refusal")
        } catch {
            XCTAssertEqual(error as? TeaNodeError, .refused("not found"))
        }
    }

    func testTheDocumentAsksForEverySectionTheCardShows() {
        let document = Documents.personCard
        for name in ["person {", "contact {", "sharedCircles", "canMessage", "location {", "health {", "photos {", "mail {", "recordings {", "memory {"] {
            XCTAssertTrue(document.contains(name), name)
        }
        XCTAssertTrue(document.contains("ReadPersonCard(userId: $userId, addressBookId: $addressBookId, contactId: $contactId, itemLimit: $itemLimit)"))
    }
}
