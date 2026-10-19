import Foundation
import XCTest

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

@testable import TeaNodeKit

private func photoJSON(_ id: String = "p1", extraFields: String = "") -> String {
    #"{"id":"\#(id)","createdAt":"2026-03-01T00:00:00Z","photoKind":"picture","takenAt":"2026-03-10T14:42:00Z","takenAtOffsetMinutes":-240,"widthPixels":10,"heightPixels":10,"durationMS":0,"latitude":null,"longitude":null,"placeName":"","cameraMake":"","cameraModel":"","originalFilename":"IMG_0001.HEIC","isFavorite":false,"isScreenshot":false,"photoProcessingState":"ready","trashedAt":null,"smallPreviewUrl":"/api/v1/photos/p1/previews/small?v=1","largePreviewUrl":"/api/v1/photos/p1/previews/large?v=1","videoPreviewUrl":""\#(extraFields)}"#
}

private func albumJSON(_ id: String, extraFields: String = "") -> String {
    #"{"id":"\#(id)","albumTitle":"Picnic","createdAt":"2026-03-01T00:00:00Z","updatedAt":"2026-03-02T00:00:00Z","coverPhotoId":null,"photoCount":3,"firstTakenAt":null,"lastTakenAt":null,"coverPreviewUrl":""\#(extraFields)}"#
}

private let utc = TimeZone(secondsFromGMT: 0)!

final class PhotoSharingTests: XCTestCase {
    private func connection(_ answer: @escaping @Sendable (URLRequest) -> (Int, String)) throws -> (Connection, FakeServer) {
        let fake = FakeServer(answer: answer)
        let account = Account(
            server: try ServerAddress(parsing: "assistant.example.com"), clientId: "test-client", accessToken: "tnt_test", refreshToken: "",
            expiresAt: Date().addingTimeInterval(3600), username: "test-owner")
        return (Connection(account: account, transport: fake), fake)
    }

    private func variables(of request: URLRequest) throws -> [String: Any] {
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: try XCTUnwrap(request.httpBody)) as? [String: Any])
        return try XCTUnwrap(object["variables"] as? [String: Any])
    }

    private func document(of request: URLRequest) throws -> String {
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: try XCTUnwrap(request.httpBody)) as? [String: Any])
        return try XCTUnwrap(object["query"] as? String)
    }

    // MARK: Albums and photos that are somebody else's

    func testAnAlbumFromAServerThatDoesNotShareIsThePersonsOwn() throws {
        let album = try JSONDecoder().decode(PhotoAlbum.self, from: Data(albumJSON("a1").utf8))
        XCTAssertTrue(album.isOwnedByCaller)
        XCTAssertTrue(album.canCallerManage)
        XCTAssertTrue(album.canCallerContribute)
        XCTAssertFalse(album.isCircleAlbum)
        XCTAssertFalse(album.isSharedWithCaller)
        XCTAssertTrue(album.canCallerShare)
    }

    func testAnAlbumSharedWithThePersonSaysWhoseItIsAndWhatTheyMayDo() throws {
        let extra =
            #","ownerUserId":"u2","circleId":null,"ownerUsername":"sam","ownerName":"","ownerAvatarUrl":null,"ownerAvatarColorIndex":3,"circleName":"","isOwnedByCaller":false,"shareLevel":"view","canCallerContribute":false,"canCallerManage":false,"isLocationIncluded":false"#
        let album = try JSONDecoder().decode(PhotoAlbum.self, from: Data(albumJSON("a1", extraFields: extra).utf8))
        XCTAssertFalse(album.isOwnedByCaller)
        XCTAssertTrue(album.isSharedWithCaller)
        XCTAssertFalse(album.canCallerContribute)
        XCTAssertFalse(album.canCallerShare)
        XCTAssertFalse(album.isLocationIncluded)
        // With no name, the username is what the screen calls the owner.
        XCTAssertEqual(album.ownerDisplayName, "sam")
        XCTAssertEqual(album.ownerAvatarColorIndex, 3)
    }

    func testACircleAlbumIsNotSharedFurtherAndEveryMemberAddsToIt() throws {
        let extra =
            #","ownerUserId":null,"circleId":"c1","ownerUsername":"","ownerName":"","ownerAvatarUrl":null,"ownerAvatarColorIndex":0,"circleName":"Neighbors","isOwnedByCaller":false,"shareLevel":"contribute","canCallerContribute":true,"canCallerManage":false,"isLocationIncluded":true"#
        let album = try JSONDecoder().decode(PhotoAlbum.self, from: Data(albumJSON("a2", extraFields: extra).utf8))
        XCTAssertTrue(album.isCircleAlbum)
        XCTAssertFalse(album.isSharedWithCaller)
        XCTAssertTrue(album.canCallerContribute)
        XCTAssertFalse(album.canCallerShare)
    }

    func testTheAlbumsAreLaidOutInTheirOwnThenSharedThenOneListPerCircle() {
        let own = PhotoAlbum(id: "own", albumTitle: "Album")
        let shared = PhotoAlbum(id: "shared", albumTitle: "Album", ownerName: "Sam Example", isOwnedByCaller: false, shareLevel: "view", canCallerContribute: false, canCallerManage: false)
        let zebra = PhotoAlbum(id: "z1", albumTitle: "Album", circleId: "cz", circleName: "Zebras", isOwnedByCaller: false, canCallerManage: false)
        let apples = PhotoAlbum(id: "a1", albumTitle: "Album", circleId: "ca", circleName: "Apples", isOwnedByCaller: false, canCallerManage: false)
        let applesAgain = PhotoAlbum(id: "a2", albumTitle: "Album", circleId: "ca", circleName: "Apples", isOwnedByCaller: true)
        let sections = PhotoAlbumSections([zebra, own, apples, shared, applesAgain])
        XCTAssertEqual(sections.own.map(\.id), ["own"])
        XCTAssertEqual(sections.sharedWithYou.map(\.id), ["shared"])
        XCTAssertEqual(sections.circles.map(\.circleName), ["Apples", "Zebras"])
        XCTAssertEqual(sections.circles[0].albums.map(\.id), ["a1", "a2"])
        XCTAssertTrue(sections.hasOthers)
        XCTAssertFalse(PhotoAlbumSections([own]).hasOthers)
    }

    func testAPhotoThatIsNotThePersonsSaysWhoseItIs() throws {
        let own = try JSONDecoder().decode(Photo.self, from: Data(photoJSON().utf8))
        XCTAssertTrue(own.isOwnedByCaller)
        XCTAssertEqual(own.ownerName, "")
        let foreign = try JSONDecoder().decode(Photo.self, from: Data(photoJSON(extraFields: #","isOwnedByCaller":false,"ownerName":"Sam Example""#).utf8))
        XCTAssertFalse(foreign.isOwnedByCaller)
        XCTAssertEqual(foreign.ownerName, "Sam Example")
        XCTAssertTrue(Documents.photoFields.contains("isOwnedByCaller"))
        XCTAssertTrue(Documents.photoAlbumFields.contains("canCallerContribute"))
    }

    func testAnAlbumForACircleIsCreatedWithTheCirclesId() async throws {
        let (connection, fake) = try connection { _ in (200, #"{"data":{"CreatePhotoAlbum":"# + albumJSON("a9") + "}}") }
        _ = try await connection.createPhotoAlbum(title: "  Block party ", photoIds: ["p1"], circleId: "c1")
        let sent = try variables(of: try XCTUnwrap(fake.sent.last))
        XCTAssertEqual(sent["albumTitle"] as? String, "Block party")
        XCTAssertEqual(sent["circleId"] as? String, "c1")
        _ = try await connection.createPhotoAlbum(title: "Mine")
        XCTAssertNil(try variables(of: try XCTUnwrap(fake.sent.last))["circleId"])
    }

    // MARK: Shares

    func testAShareIsReadForAPersonAndForACircle() throws {
        let person = try JSONDecoder().decode(
            PhotoAlbumShare.self,
            from: Data(
                #"{"id":"s1","photoAlbumId":"a1","granteeUserId":"u2","granteeUsername":"sam","granteeName":"Sam Example","granteeAvatarUrl":null,"granteeAvatarColorIndex":4,"granteeCircleId":null,"granteeCircleName":"","shareLevel":"contribute","isLocationIncluded":true,"createdAt":"2026-03-01T00:00:00Z"}"#
                    .utf8))
        XCTAssertFalse(person.isWithCircle)
        XCTAssertEqual(person.granteeDisplayName, "Sam Example")
        XCTAssertEqual(person.knownShareLevel, .contribute)
        XCTAssertTrue(person.isLocationIncluded)
        let circle = try JSONDecoder().decode(
            PhotoAlbumShare.self,
            from: Data(
                #"{"id":"s2","photoAlbumId":"a1","granteeUserId":null,"granteeUsername":"","granteeName":"","granteeAvatarUrl":null,"granteeAvatarColorIndex":0,"granteeCircleId":"c1","granteeCircleName":"Neighbors","shareLevel":"view","isLocationIncluded":false,"createdAt":"2026-03-01T00:00:00Z"}"#
                    .utf8))
        XCTAssertTrue(circle.isWithCircle)
        XCTAssertEqual(circle.granteeDisplayName, "Neighbors")
        XCTAssertEqual(circle.knownShareLevel, .view)
    }

    func testAShareNeedsSomebodyChosenAndSendsOneOfPersonOrCircle() async throws {
        var draft = PhotoShareDraft()
        XCTAssertFalse(draft.canSubmit)
        draft.target = .person(userId: "u2")
        draft.photoShareLevel = .contribute
        draft.isLocationIncluded = true
        XCTAssertTrue(draft.canSubmit)
        let share = #"{"id":"s1","photoAlbumId":"a1","granteeUserId":"u2","granteeUsername":"sam","granteeName":"","granteeAvatarUrl":null,"granteeAvatarColorIndex":0,"granteeCircleId":null,"granteeCircleName":"","shareLevel":"contribute","isLocationIncluded":true,"createdAt":""}"#
        let (connection, fake) = try connection { _ in (200, #"{"data":{"SharePhotoAlbum":"# + share + "}}") }
        _ = try await connection.sharePhotoAlbum("a1", draft: draft)
        let person = try variables(of: try XCTUnwrap(fake.sent.last))
        XCTAssertEqual(person["granteeUserId"] as? String, "u2")
        XCTAssertNil(person["granteeCircleId"])
        XCTAssertEqual(person["shareLevel"] as? String, "contribute")
        XCTAssertEqual(person["isLocationIncluded"] as? Bool, true)
        draft.target = .circle(circleId: "c1")
        _ = try await connection.sharePhotoAlbum("a1", draft: draft)
        let circle = try variables(of: try XCTUnwrap(fake.sent.last))
        XCTAssertEqual(circle["granteeCircleId"] as? String, "c1")
        XCTAssertNil(circle["granteeUserId"])
    }

    func testAShareIsChangedAndTakenBackByItsId() async throws {
        let share = #"{"id":"s1","photoAlbumId":"a1","granteeUserId":"u2","granteeUsername":"sam","granteeName":"","granteeAvatarUrl":null,"granteeAvatarColorIndex":0,"granteeCircleId":null,"granteeCircleName":"","shareLevel":"view","isLocationIncluded":false,"createdAt":""}"#
        let (connection, fake) = try connection { request in
            let text = String(decoding: request.httpBody ?? Data(), as: UTF8.self)
            if text.contains("RemovePhotoAlbumShare") { return (200, #"{"data":{"RemovePhotoAlbumShare":true}}"#) }
            if text.contains("UpdatePhotoAlbumShare") { return (200, #"{"data":{"UpdatePhotoAlbumShare":"# + share + "}}") }
            return (200, #"{"data":{"ListPhotoAlbumShares":["# + share + "]}}")
        }
        let shares = try await connection.photoAlbumShares("a1")
        XCTAssertEqual(shares.map(\.id), ["s1"])
        XCTAssertEqual(try variables(of: try XCTUnwrap(fake.sent.last))["photoAlbumId"] as? String, "a1")
        _ = try await connection.updatePhotoAlbumShare("s1", photoShareLevel: .view, isLocationIncluded: false)
        let updated = try variables(of: try XCTUnwrap(fake.sent.last))
        XCTAssertEqual(updated["photoAlbumShareId"] as? String, "s1")
        XCTAssertEqual(updated["shareLevel"] as? String, "view")
        try await connection.removePhotoAlbumShare("s1")
        XCTAssertEqual(try variables(of: try XCTUnwrap(fake.sent.last))["photoAlbumShareId"] as? String, "s1")
    }

    func testKeepingAPhotoAsTheirOwnNamesThePhotoOrTheConversationFile() async throws {
        let (connection, fake) = try connection { request in
            let text = String(decoding: request.httpBody ?? Data(), as: UTF8.self)
            if text.contains("SavePhotoFromAttachment") { return (200, #"{"data":{"SavePhotoFromAttachment":"# + photoJSON("p7") + "}}") }
            return (200, #"{"data":{"SavePhotoToLibrary":"# + photoJSON("p8") + "}}")
        }
        let kept = try await connection.savePhotoToLibrary("p3")
        XCTAssertEqual(kept.id, "p8")
        XCTAssertEqual(try variables(of: try XCTUnwrap(fake.sent.last))["photoId"] as? String, "p3")
        let saved = try await connection.savePhotoFromAttachment("att1")
        XCTAssertEqual(saved.id, "p7")
        XCTAssertEqual(try variables(of: try XCTUnwrap(fake.sent.last))["agentAttachmentId"] as? String, "att1")
    }

    func testTheAddedPhotosNotificationCanBeChosen() {
        XCTAssertTrue(AgentPhone.allNotificationKinds.contains("photo"))
    }

    // MARK: Filters

    func testAFilterWithNothingChosenKeepsEverything() {
        XCTAssertFalse(PhotoFilter().isActive)
        XCTAssertTrue(PhotoFilter().variables().isEmpty)
    }

    func testAFilterAsksForTheKindAndForWholeDaysOfThePhonesCalendar() {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = utc
        let first = calendar.date(from: DateComponents(year: 2026, month: 3, day: 10, hour: 15))!
        let last = calendar.date(from: DateComponents(year: 2026, month: 3, day: 12, hour: 8))!
        let filter = PhotoFilter(photoKindFilter: .video, firstDay: first, lastDay: last)
        XCTAssertTrue(filter.isActive)
        let sent = filter.variables(calendar: calendar)
        XCTAssertEqual(sent["photoKind"], .string("video"))
        // The range starts when the first day starts and ends where the day after the last begins.
        XCTAssertEqual(sent["takenAfter"], .string("2026-03-10T00:00:00Z"))
        XCTAssertEqual(sent["takenBefore"], .string("2026-03-13T00:00:00Z"))
        XCTAssertFalse(filter.isRangeBackwards)
        XCTAssertTrue(PhotoFilter(firstDay: last, lastDay: first).isRangeBackwards)
    }

    func testAFilterWithMomentsSendsThemInPlaceOfTheDays() {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = utc
        let day = calendar.date(from: DateComponents(year: 2026, month: 3, day: 10, hour: 15))!
        let arrival = calendar.date(from: DateComponents(year: 2026, month: 3, day: 11, hour: 9, minute: 50))!
        let departure = calendar.date(from: DateComponents(year: 2026, month: 3, day: 11, hour: 10, minute: 30))!
        let filter = PhotoFilter(firstDay: day, lastDay: day, takenAfter: arrival, takenBefore: departure)
        XCTAssertTrue(PhotoFilter(takenAfter: arrival).isActive)
        let sent = filter.variables(calendar: calendar)
        XCTAssertEqual(sent["takenAfter"], .string("2026-03-11T09:50:00Z"))
        XCTAssertEqual(sent["takenBefore"], .string("2026-03-11T10:30:00Z"))
        // One end given leaves the other to the days.
        let onlyAfter = PhotoFilter(firstDay: day, lastDay: day, takenAfter: arrival).variables(calendar: calendar)
        XCTAssertEqual(onlyAfter["takenAfter"], .string("2026-03-11T09:50:00Z"))
        XCTAssertEqual(onlyAfter["takenBefore"], .string("2026-03-11T00:00:00Z"))
    }

    func testTheLibraryIsListedWithTheFilter() async throws {
        let (connection, fake) = try connection { _ in (200, #"{"data":{"ListPhotos":[]}}"#) }
        _ = try await connection.photos(in: .library, filter: PhotoFilter(photoKindFilter: .livePhoto))
        let sent = try variables(of: try XCTUnwrap(fake.sent.last))
        XCTAssertEqual(sent["photoKind"] as? String, "livePhoto")
        XCTAssertNil(sent["takenAfter"])
        _ = try await connection.photos(in: .library)
        XCTAssertNil(try variables(of: try XCTUnwrap(fake.sent.last))["photoKind"])
        XCTAssertTrue(Documents.listPhotos.contains("$photoKind: String"))
    }

    func testAFilterNearAPlaceAsksForAllThreeOfItAndIsActive() throws {
        let located = #""latitude":48.5,"longitude":-12.25,"placeName":" Harbor ""#
        let json = photoJSON().replacingOccurrences(of: #""latitude":null,"longitude":null,"placeName":"""#, with: located)
        let photo = try JSONDecoder().decode(Photo.self, from: Data(json.utf8))
        let near = try XCTUnwrap(PhotoNearFilter(near: photo))
        XCTAssertEqual(near.placeName, "Harbor")
        XCTAssertEqual(near.distanceKilometers, 1)
        let filter = PhotoFilter(photoNearFilter: near)
        XCTAssertTrue(filter.isActive)
        XCTAssertEqual(filter.variables(), ["nearLatitude": .number(48.5), "nearLongitude": .number(-12.25), "nearDistanceKilometers": .number(1)])
        XCTAssertNil(PhotoFilter().variables()["nearLatitude"])
        XCTAssertTrue(Documents.listPhotos.contains("$nearLatitude: Float, $nearLongitude: Float, $nearDistanceKilometers: Float"))
    }

    func testAPhotoWithNoPlaceHasNothingToBeNear() throws {
        let photo = try JSONDecoder().decode(Photo.self, from: Data(photoJSON().utf8))
        XCTAssertNil(PhotoNearFilter(near: photo))
    }

    func testAPhotoCanBeTakenOutOfAnAlbumByItsOwnerOrByAManager() throws {
        let othersPhoto = try JSONDecoder().decode(Photo.self, from: Data(photoJSON("p2", extraFields: #","isOwnedByCaller":false,"ownerName":"Sam""#).utf8))
        let ownPhoto = try JSONDecoder().decode(Photo.self, from: Data(photoJSON("p1").utf8))
        let contributor = PhotoAlbum(id: "a1", albumTitle: "Picnic", isOwnedByCaller: false, shareLevel: "contribute", canCallerContribute: true, canCallerManage: false)
        XCTAssertTrue(contributor.canCallerRemove(ownPhoto))
        XCTAssertFalse(contributor.canCallerRemove(othersPhoto), "somebody else's photo is theirs to take out unless the person manages the album")
        let viewer = PhotoAlbum(id: "a1", albumTitle: "Picnic", isOwnedByCaller: false, shareLevel: "view", canCallerContribute: false, canCallerManage: false)
        XCTAssertFalse(viewer.canCallerRemove(ownPhoto))
        XCTAssertFalse(viewer.canCallerRemove(othersPhoto))
        let owner = PhotoAlbum(id: "a1", albumTitle: "Picnic")
        XCTAssertTrue(owner.canCallerRemove(ownPhoto))
        XCTAssertTrue(owner.canCallerRemove(othersPhoto))
        let circleAdmin = PhotoAlbum(id: "a1", albumTitle: "Picnic", circleId: "c1", isOwnedByCaller: false, canCallerContribute: true, canCallerManage: true)
        XCTAssertTrue(circleAdmin.canCallerRemove(othersPhoto))
    }

    func testTheLibraryCountsItsVideosAmongItsPhotos() {
        let library = PhotoLibrary(photoCount: 120, videoCount: 4, favoriteCount: 0, trashedCount: 0, usedBytes: 0, trashRetentionDays: 30)
        XCTAssertEqual(library.itemCount, 124)
    }

    // MARK: Placing a photo whose date changed

    private func photo(_ id: String, takenAt: String) throws -> Photo {
        let json = photoJSON(id).replacingOccurrences(of: #""takenAt":"2026-03-10T14:42:00Z""#, with: #""takenAt":"\#(takenAt)""#)
        return try JSONDecoder().decode(Photo.self, from: Data(json.utf8))
    }

    func testAPhotoIsListedNewestFirstAndALargerIdFirstAtTheSameMoment() throws {
        let newer = try photo("a", takenAt: "2026-03-11T00:00:00Z")
        let older = try photo("b", takenAt: "2026-03-10T00:00:00Z")
        let sameMomentLargerId = try photo("c", takenAt: "2026-03-10T00:00:00Z")
        XCTAssertEqual(PhotoTimeline.sorted([older, sameMomentLargerId, newer]).map(\.id), ["a", "c", "b"])
    }

    func testAPhotoWithANewDateGoesToItsPlaceAmongThoseHeld() throws {
        let held = try [photo("a", takenAt: "2026-03-14T00:00:00Z"), photo("b", takenAt: "2026-03-13T00:00:00Z"), photo("c", takenAt: "2026-03-12T00:00:00Z")]
        let moved = try photo("c", takenAt: "2026-03-15T00:00:00Z")
        let placement = PhotoPlacement.place(moved, in: held, pageCursorPhotoId: nil, hasMore: true)
        XCTAssertEqual(placement.photos.map(\.id), ["c", "a", "b"])
        XCTAssertEqual(placement.pageCursorPhotoId, "b", "the last photo held, which was c, is no longer the one the next page follows")
        XCTAssertFalse(placement.isBeyondHeldPhotos)
    }

    func testAPhotoMovedPastTheEndOfAListWithMorePagesWaitsForThePageItBelongsTo() throws {
        let held = try [photo("a", takenAt: "2026-03-14T00:00:00Z"), photo("b", takenAt: "2026-03-13T00:00:00Z"), photo("c", takenAt: "2026-03-12T00:00:00Z")]
        let moved = try photo("a", takenAt: "2020-01-01T00:00:00Z")
        let placement = PhotoPlacement.place(moved, in: held, pageCursorPhotoId: nil, hasMore: true)
        XCTAssertEqual(placement.photos.map(\.id), ["b", "c", "a"])
        XCTAssertEqual(placement.pageCursorPhotoId, "c", "the next page still follows the photo the server has last among those read")
        XCTAssertTrue(placement.isBeyondHeldPhotos)
        XCTAssertFalse(PhotoPlacement.place(moved, in: held, pageCursorPhotoId: nil, hasMore: false).isBeyondHeldPhotos)
    }

    func testAPhotoThatIsNotHeldLeavesTheListAsItWas() throws {
        let held = try [photo("a", takenAt: "2026-03-14T00:00:00Z")]
        let placement = PhotoPlacement.place(try photo("z", takenAt: "2026-03-15T00:00:00Z"), in: held, pageCursorPhotoId: nil, hasMore: true)
        XCTAssertEqual(placement.photos, held)
        XCTAssertEqual(placement.pageCursorPhotoId, "a")
    }

    // MARK: Changing a date

    func testADateIsWrittenWithTheOffsetThePhotoIsShownAt() throws {
        let photo = try JSONDecoder().decode(Photo.self, from: Data(photoJSON().utf8))
        let moment = ServerTime.date("2026-03-10T14:42:00Z")!
        // The photo's own offset is -04:00, so the same moment reads four hours earlier there.
        XCTAssertEqual(PhotoDateChange.takenAtText(for: moment, photo: photo, phoneTimeZone: utc), "2026-03-10T10:42:00-04:00")
        XCTAssertEqual(PhotoDateChange.takenAtText(for: moment, offsetSeconds: 0), "2026-03-10T14:42:00+00:00")
        XCTAssertEqual(PhotoDateChange.takenAtText(for: moment, offsetSeconds: 5 * 3600 + 30 * 60), "2026-03-10T20:12:00+05:30")
        XCTAssertEqual(PhotoDateChange.timeZone(of: photo, phoneTimeZone: utc).secondsFromGMT(), -4 * 3600)
    }

    func testAPhotoWithNoOffsetIsWrittenAtThePhonesOffset() throws {
        let photo = try JSONDecoder().decode(
            Photo.self, from: Data(photoJSON().replacingOccurrences(of: #""takenAtOffsetMinutes":-240"#, with: #""takenAtOffsetMinutes":null"#).utf8))
        let moment = ServerTime.date("2026-03-10T14:42:00Z")!
        let tokyo = TimeZone(identifier: "Asia/Tokyo")!
        XCTAssertEqual(PhotoDateChange.takenAtText(for: moment, photo: photo, phoneTimeZone: tokyo), "2026-03-10T23:42:00+09:00")
    }

    func testChangingADateSendsTheTextAndReturnsThePhoto() async throws {
        let (connection, fake) = try connection { _ in (200, #"{"data":{"UpdatePhoto":"# + photoJSON("p5") + "}}") }
        let changed = try await connection.changePhotoDate("p5", takenAtText: "2026-03-10T10:42:00-04:00")
        XCTAssertEqual(changed.id, "p5")
        let sent = try variables(of: try XCTUnwrap(fake.sent.last))
        XCTAssertEqual(sent["takenAt"] as? String, "2026-03-10T10:42:00-04:00")
        XCTAssertNil(sent["isFavorite"])
        XCTAssertTrue(try document(of: try XCTUnwrap(fake.sent.last)).contains("takenAt: $takenAt"))
    }

    // MARK: Batches of ids

    func testMoreThanFiveHundredPhotosAreTrashedRestoredAndDeletedInBatches() async throws {
        let (connection, fake) = try connection { request in
            let text = String(decoding: request.httpBody ?? Data(), as: UTF8.self)
            let count = ((try? JSONSerialization.jsonObject(with: request.httpBody ?? Data()) as? [String: Any])?["variables"] as? [String: Any])?["photoIds"] as? [Any]
            let name = ["TrashPhotos", "RestorePhotos", "DeletePhotos"].first { text.contains($0 + "(") } ?? "Nothing"
            return (200, #"{"data":{"\#(name)":\#(count?.count ?? 0)}}"#)
        }
        let photoIds = (0..<1_200).map { "p\($0)" }
        let trashed = try await connection.trashPhotos(photoIds)
        XCTAssertEqual(trashed, 1_200)
        XCTAssertEqual(fake.sent.count, 3)
        let restored = try await connection.restorePhotos(photoIds)
        XCTAssertEqual(restored, 1_200)
        let deleted = try await connection.deletePhotos(photoIds)
        XCTAssertEqual(deleted, 1_200)
        for request in fake.sent {
            let ids = try XCTUnwrap(variables(of: request)["photoIds"] as? [Any])
            XCTAssertLessThanOrEqual(ids.count, PhotoBatches.mostPhotoIdsPerCall)
        }
        XCTAssertEqual(fake.sent.count, 9)
    }

    // MARK: Saving to the phone

    private func downloadConnection(originalStatus: Int) throws -> (Connection, FakeServer) {
        let detail =
            #"{"data":{"ReadPhoto":{"photo":"# + photoJSON()
            + #","photoFiles":[{"photoFileRole":"original","contentType":"image/heic","fileSizeBytes":9,"originalFilename":"IMG_0001.HEIC","fileUrl":"/f"}],"photoPreviews":[{"photoPreviewSize":"large","contentType":"image/jpeg","previewSizeBytes":5,"previewUrl":"/p"}]}}}"#
        return try connection { request in
            let path = request.url?.path ?? ""
            if path.hasSuffix("/graphql") {
                let body = String(decoding: request.httpBody ?? Data(), as: UTF8.self)
                if body.contains("PhotoPlaybackAddress") {
                    let role = body.contains(#""photoFileRole":"original""#) ? "files/original" : "previews/large"
                    return (200, #"{"data":{"PhotoPlaybackAddress":"/api/v1/photos/playback/p1/\#(role)?signature=s"}}"#)
                }
                return (200, detail)
            }
            if path.hasSuffix("/files/original") { return (originalStatus, originalStatus == 200 ? "original" : "no") }
            return (200, "preview")
        }
    }

    func testAPreviewStandsInOnlyWhenTheServerHasNoFileForThePerson() async throws {
        for status in [403, 404, 410] {
            let (connection, _) = try downloadConnection(originalStatus: status)
            let photo = try JSONDecoder().decode(Photo.self, from: Data(photoJSON().utf8))
            let download = try await connection.downloadPhotoFile(photo)
            defer { try? FileManager.default.removeItem(at: download.fileURL.deletingLastPathComponent()) }
            XCTAssertFalse(download.isOriginal, "status \(status)")
        }
    }

    func testAServerThatFailedDoesNotQuietlySaveASmallerCopy() async throws {
        let (connection, _) = try downloadConnection(originalStatus: 500)
        let photo = try JSONDecoder().decode(Photo.self, from: Data(photoJSON().utf8))
        do {
            _ = try await connection.downloadPhotoFile(photo)
            XCTFail("a failed read saved a smaller copy")
        } catch {
            XCTAssertEqual(error as? PhotoError, .fileCouldNotBeRead)
        }
    }

    func testAnUnreadableDetailIsNotMistakenForAPhotoWithNoOriginal() async throws {
        let (connection, _) = try connection { _ in (500, "oops") }
        let photo = try JSONDecoder().decode(Photo.self, from: Data(photoJSON().utf8))
        do {
            _ = try await connection.downloadPhotoFile(photo)
            XCTFail("a failed read saved a smaller copy")
        } catch {
            XCTAssertNotEqual(error as? PhotoError, .nothingToSaveYet)
        }
    }

    // MARK: Files a download leaves behind

    func testOldDownloadFoldersAreSweptAndNothingElseIs() throws {
        let manager = FileManager.default
        let directory = manager.temporaryDirectory.appendingPathComponent("sweep-test-\(UUID().uuidString)", isDirectory: true)
        try manager.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? manager.removeItem(at: directory) }
        let old = directory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        let fresh = directory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        let other = directory.appendingPathComponent("keep-me.txt")
        for folder in [old, fresh] { try manager.createDirectory(at: folder, withIntermediateDirectories: true) }
        try Data("x".utf8).write(to: old.appendingPathComponent("IMG.jpg"))
        try Data("x".utf8).write(to: other)
        try manager.setAttributes([.modificationDate: Date().addingTimeInterval(-7_200)], ofItemAtPath: old.path)
        let removed = PhotoTemporaryFiles.sweep(olderThanSeconds: 3_600, in: directory)
        XCTAssertEqual(removed, 1)
        XCTAssertFalse(manager.fileExists(atPath: old.path))
        XCTAssertTrue(manager.fileExists(atPath: fresh.path))
        XCTAssertTrue(manager.fileExists(atPath: other.path))
    }

    func testAFolderIsRemovedAfterTheSharingHadItsTime() async throws {
        let manager = FileManager.default
        let folder = manager.temporaryDirectory.appendingPathComponent(UUID().uuidString, isDirectory: true)
        try manager.createDirectory(at: folder, withIntermediateDirectories: true)
        let file = folder.appendingPathComponent("IMG.jpg")
        try Data("x".utf8).write(to: file)
        PhotoTemporaryFiles.removeLater(PhotoDownload(fileURL: file, filename: "IMG.jpg", isOriginal: true), afterSeconds: 0)
        for _ in 0..<50 where manager.fileExists(atPath: folder.path) { try await Task.sleep(for: .milliseconds(20)) }
        XCTAssertFalse(manager.fileExists(atPath: folder.path))
    }
}
