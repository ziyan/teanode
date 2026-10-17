import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif
import XCTest

@testable import TeaNodeKit

private func photoFields(_ id: String) -> String {
    #""id":"\#(id)","createdAt":"2026-03-01T00:00:00Z","photoKind":"picture","takenAt":null,"takenAtOffsetMinutes":null,"widthPixels":1,"heightPixels":1,"durationMS":0,"latitude":null,"longitude":null,"placeName":"","cameraMake":"","cameraModel":"","originalFilename":"","isFavorite":false,"isScreenshot":false,"photoProcessingState":"ready","trashedAt":null,"smallPreviewUrl":"","largePreviewUrl":"","videoPreviewUrl":"""#
}

private let describedPhotoFields =
    #""photoDescription":"A red kite over a field.","descriptionVisibleText":"KITE FESTIVAL","describedAt":"2026-03-02T10:00:00Z","isDescriptionRequested":false"#

private let groupJSON =
    #"{"id":"g1","createdAt":"2026-03-01T00:00:00Z","updatedAt":"2026-03-02T00:00:00Z","contactId":"c1","contactAddressBookId":"b1","contactName":"Sample Person","faceCount":5,"photoCount":4,"facePictureUrl":"/api/v1/photos/faces/f1/picture","coverPhotoFaceId":"f1","isHidden":false}"#

final class PhotoPeopleTests: XCTestCase {
    private func connection(answer: @escaping @Sendable (String) -> String) throws -> (Connection, FakeServer) {
        let fake = FakeServer { request in (200, answer(String(decoding: request.httpBody ?? Data(), as: UTF8.self))) }
        let account = Account(
            server: try ServerAddress(parsing: "assistant.example.com"), clientId: "test-client", accessToken: "tnt_test", refreshToken: "",
            expiresAt: Date().addingTimeInterval(3600), username: "test-owner")
        return (Connection(account: account, transport: fake), fake)
    }

    private func variables(of request: URLRequest) throws -> [String: Any] {
        let sent = try XCTUnwrap(JSONSerialization.jsonObject(with: try XCTUnwrap(request.httpBody)) as? [String: Any])
        return try XCTUnwrap(sent["variables"] as? [String: Any])
    }

    // MARK: Descriptions

    func testADetailCarriesTheDescriptionOfItsPhotoAndTheFacesInIt() throws {
        let json =
            #"{"photo":{\#(photoFields("p1")),\#(describedPhotoFields)},"photoFiles":[],"photoPreviews":[],"albums":[],"faces":[{"id":"f1","photoId":"p1","boxLeftFraction":0.1,"boxTopFraction":0.2,"boxWidthFraction":0.3,"boxHeightFraction":0.4,"photoFaceGroupId":"g1","contactId":"c1","contactName":"Sample Person","facePictureUrl":"/api/v1/photos/faces/f1/picture"}]}"#
        let detail = try JSONDecoder().decode(PhotoDetail.self, from: Data(json.utf8))
        XCTAssertEqual(detail.photoDescription.photoDescription, "A red kite over a field.")
        XCTAssertEqual(detail.photoDescription.descriptionVisibleText, "KITE FESTIVAL")
        XCTAssertTrue(detail.photoDescription.isDescribed)
        XCTAssertFalse(detail.photoDescription.isDescriptionRequested)
        XCTAssertEqual(detail.faces.map(\.id), ["f1"])
        XCTAssertEqual(detail.faces[0].boxWidthFraction, 0.3, accuracy: 0.0001)
        XCTAssertEqual(detail.faces[0].contactName, "Sample Person")
        XCTAssertTrue(detail.faces[0].isGrouped)
        XCTAssertEqual(detail.photo.id, "p1")
    }

    func testAnOlderServerSaysNothingOfDescriptionsOrFaces() throws {
        let json = #"{"photo":{\#(photoFields("p1"))},"photoFiles":[],"photoPreviews":[]}"#
        let detail = try JSONDecoder().decode(PhotoDetail.self, from: Data(json.utf8))
        XCTAssertEqual(detail.photoDescription, PhotoDescription())
        XCTAssertFalse(detail.photoDescription.isDescribed)
        XCTAssertEqual(detail.faces, [])
    }

    func testADescriptionOnItsWayIsNotYetDescribed() throws {
        let json = #"{\#(photoFields("p1")),"photoDescription":"","descriptionVisibleText":"","describedAt":null,"isDescriptionRequested":true}"#
        let description = try JSONDecoder().decode(PhotoDescription.self, from: Data(json.utf8))
        XCTAssertTrue(description.isDescriptionRequested)
        XCTAssertFalse(description.isDescribed)
        XCTAssertNil(description.describedOnText())
    }

    func testTheDayADescriptionWasWrittenIsSaidInTheReadersZone() {
        let description = PhotoDescription(photoDescription: "x", describedAt: "2026-03-02T23:30:00Z")
        let utc = TimeZone(secondsFromGMT: 0)!
        let tokyo = TimeZone(identifier: "Asia/Tokyo")!
        let english = Locale(identifier: "en_US")
        XCTAssertEqual(description.describedOnText(timeZone: utc, locale: english), "March 2, 2026")
        XCTAssertEqual(description.describedOnText(timeZone: tokyo, locale: english), "March 3, 2026")
    }

    func testADescriptionIsWaitedForForTwoMinutes() {
        XCTAssertEqual(PhotoDescriptionPolling.intervalSeconds, 3)
        XCTAssertTrue(PhotoDescriptionPolling.shouldAskAgain(afterWaiting: 0))
        XCTAssertTrue(PhotoDescriptionPolling.shouldAskAgain(afterWaiting: 117))
        XCTAssertFalse(PhotoDescriptionPolling.shouldAskAgain(afterWaiting: 120))
        XCTAssertFalse(PhotoDescriptionPolling.shouldAskAgain(afterWaiting: 300))
    }

    func testDescribingAsksForANewDescriptionOnlyWhenToldTo() async throws {
        let (connection, fake) = try connection { _ in
            #"{"data":{"DescribePhoto":{"photoDescription":"","descriptionVisibleText":"","describedAt":null,"isDescriptionRequested":true}}}"#
        }
        let first = try await connection.describePhoto("p1")
        _ = try await connection.describePhoto("p1", isRedescribed: true)
        XCTAssertTrue(first.isDescriptionRequested)
        let sent = try fake.sent.map { try variables(of: $0) }
        XCTAssertEqual(sent[0]["photoId"] as? String, "p1")
        XCTAssertEqual(sent[0]["isRedescribed"] as? Bool, false)
        XCTAssertEqual(sent[1]["isRedescribed"] as? Bool, true)
    }

    func testTheLibrarySaysWhyDescribingOrFacesAreUnavailable() throws {
        let json =
            #"{"photoCount":1,"videoCount":0,"favoriteCount":0,"trashedCount":0,"usedBytes":10,"trashRetentionDays":30,"isDescriptionAvailable":false,"descriptionUnavailableMessage":"The assistant is off.","isFaceRecognitionAvailable":true,"faceRecognitionUnavailableMessage":"","isPhotoFaceRecognitionEnabled":false}"#
        let library = try JSONDecoder().decode(PhotoLibrary.self, from: Data(json.utf8))
        XCTAssertFalse(library.isDescriptionAvailable)
        XCTAssertEqual(library.descriptionUnavailableMessage, "The assistant is off.")
        XCTAssertTrue(library.isFaceRecognitionAvailable)
        XCTAssertFalse(library.isPhotoFaceRecognitionEnabled)
        let older = try JSONDecoder().decode(
            PhotoLibrary.self, from: Data(#"{"photoCount":1,"videoCount":0,"favoriteCount":0,"trashedCount":0,"usedBytes":10,"trashRetentionDays":30}"#.utf8))
        XCTAssertFalse(older.isDescriptionAvailable)
        XCTAssertFalse(older.isFaceRecognitionAvailable)
    }

    // MARK: People

    func testAGroupIsNamedByItsContact() throws {
        let group = try JSONDecoder().decode(PhotoFaceGroup.self, from: Data(groupJSON.utf8))
        XCTAssertTrue(group.isNamed)
        XCTAssertEqual(PhotoNaming.shownName(of: group), "Sample Person")
        XCTAssertEqual(group.photoCount, 4)
        XCTAssertEqual(group.facePictureUrl, "/api/v1/photos/faces/f1/picture")
        let unnamed = PhotoFaceGroup(id: "g2", contactName: "  ")
        XCTAssertFalse(unnamed.isNamed)
        XCTAssertNil(PhotoNaming.shownName(of: unnamed))
    }

    func testThePeopleAreListedWithTheHiddenOnesOnlyWhenAskedFor() async throws {
        let (connection, fake) = try connection { _ in #"{"data":{"ListPhotoFaceGroups":[\#(groupJSON)]}}"# }
        let groups = try await connection.photoFaceGroups()
        _ = try await connection.photoFaceGroups(isHiddenIncluded: true)
        XCTAssertEqual(groups.map(\.id), ["g1"])
        let sent = try fake.sent.map { try variables(of: $0) }
        XCTAssertEqual(sent[0]["isHiddenIncluded"] as? Bool, false)
        XCTAssertEqual(sent[1]["isHiddenIncluded"] as? Bool, true)
    }

    func testThePhotosOfAPersonAreAskedForByTheirGroup() async throws {
        let (connection, fake) = try connection { _ in #"{"data":{"ListPhotos":[]}}"# }
        _ = try await connection.photos(inFaceGroup: "g1", beforePhotoId: "p9")
        let sent = try variables(of: try XCTUnwrap(fake.sent.first))
        XCTAssertEqual(sent["faceGroupId"] as? String, "g1")
        XCTAssertEqual(sent["beforePhotoId"] as? String, "p9")
        XCTAssertNil(sent["albumId"])
    }

    func testNamingMergingHidingAndRemovingAFaceSendTheServersNames() async throws {
        let (connection, fake) = try connection { body in
            for name in ["NamePhotoFaceGroup", "UnnamePhotoFaceGroup", "MergePhotoFaceGroups", "RemovePhotoFaceFromGroup", "HidePhotoFaceGroup"]
            where body.contains(name) {
                return #"{"data":{"\#(name)":\#(groupJSON)}}"#
            }
            return #"{"data":{"SetPhotoFaceRecognition":false}}"#
        }
        _ = try await connection.namePhotoFaceGroup("g1", contactId: "c1")
        _ = try await connection.unnamePhotoFaceGroup("g1")
        _ = try await connection.mergePhotoFaceGroups(["g1", "g2"])
        _ = try await connection.removePhotoFaceFromGroup("f1")
        _ = try await connection.hidePhotoFaceGroup("g1", isHidden: true)
        let isEnabled = try await connection.setPhotoFaceRecognition(isEnabled: false)
        XCTAssertFalse(isEnabled)
        let sent = try fake.sent.map { try variables(of: $0) }
        XCTAssertEqual(sent[0]["photoFaceGroupId"] as? String, "g1")
        XCTAssertEqual(sent[0]["contactId"] as? String, "c1")
        XCTAssertEqual(sent[1]["photoFaceGroupId"] as? String, "g1")
        XCTAssertEqual(sent[2]["photoFaceGroupIds"] as? [String], ["g1", "g2"])
        XCTAssertEqual(sent[3]["photoFaceId"] as? String, "f1")
        XCTAssertEqual(sent[4]["isHidden"] as? Bool, true)
        XCTAssertEqual(sent[5]["isEnabled"] as? Bool, false)
    }

    func testANewContactIsOfferedForANameNoContactHas() {
        func contact(_ id: String, _ name: String) -> ContactSummary {
            ContactSummary(id: id, name: name, organization: nil, emails: nil, phones: nil, hasPhoto: nil, addresses: nil)
        }
        let contacts = [contact("c1", "Sample Person")]
        XCTAssertFalse(PhotoNaming.offersNewContact(typed: "", contacts: contacts))
        XCTAssertFalse(PhotoNaming.offersNewContact(typed: "   ", contacts: contacts))
        XCTAssertFalse(PhotoNaming.offersNewContact(typed: " sample person ", contacts: contacts))
        XCTAssertTrue(PhotoNaming.offersNewContact(typed: "Someone Else", contacts: contacts))
        XCTAssertEqual(PhotoNaming.trimmedName("  Someone Else \n"), "Someone Else")
    }

    func testANewContactIsMadeInTheFirstAddressBookWithOnlyAName() async throws {
        let (connection, fake) = try connection { body in
            if body.contains("ListAddressBooks") { return #"{"data":{"ListAddressBooks":[{"id":"b1","name":"Contacts","contacts":2}]}}"# }
            return #"{"data":{"SaveContact":{"id":"c9","name":"Someone Else"}}}"#
        }
        let made = try await connection.createNamedContact("Someone Else")
        XCTAssertEqual(made.id, "c9")
        XCTAssertEqual(made.shownName, "Someone Else")
        let sent = try variables(of: try XCTUnwrap(fake.sent.last))
        XCTAssertEqual(sent["addressBookId"] as? String, "b1")
        XCTAssertEqual(sent["name"] as? String, "Someone Else")
    }
}
