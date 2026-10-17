import Foundation
import XCTest

@testable import TeaNodeKit

final class PhotosTests: XCTestCase {
    private func photoJSON(
        id: String, kind: String = "picture", takenAt: String? = nil, offsetMinutes: Int? = nil, createdAt: String = "2026-03-01T00:00:00Z",
        durationMS: Int = 0, smallPreviewUrl: String = "", largePreviewUrl: String = "", placeName: String = ""
    ) -> String {
        let takenAt = takenAt.map { #""\#($0)""# } ?? "null"
        let offset = offsetMinutes.map { String($0) } ?? "null"
        return #"{"id":"\#(id)","createdAt":"\#(createdAt)","photoKind":"\#(kind)","takenAt":\#(takenAt),"takenAtOffsetMinutes":\#(offset),"widthPixels":3024,"heightPixels":4032,"durationMS":\#(durationMS),"latitude":null,"longitude":null,"placeName":"\#(placeName)","cameraMake":"","cameraModel":"","originalFilename":"IMG_0001.HEIC","isFavorite":false,"isScreenshot":false,"photoProcessingState":"ready","trashedAt":null,"smallPreviewUrl":"\#(smallPreviewUrl)","largePreviewUrl":"\#(largePreviewUrl)","videoPreviewUrl":""}"#
    }

    private func photo(
        _ id: String, kind: String = "picture", takenAt: String? = nil, offsetMinutes: Int? = nil, createdAt: String = "2026-03-01T00:00:00Z",
        placeName: String = ""
    ) throws -> Photo {
        try JSONDecoder().decode(
            Photo.self,
            from: Data(photoJSON(id: id, kind: kind, takenAt: takenAt, offsetMinutes: offsetMinutes, createdAt: createdAt, placeName: placeName).utf8))
    }

    private let losAngeles = TimeZone(identifier: "America/Los_Angeles")!
    private let tokyo = TimeZone(identifier: "Asia/Tokyo")!

    private func connection(answering body: String) throws -> (Connection, FakeServer) {
        let fake = FakeServer { _ in (200, body) }
        let account = Account(server: try ServerAddress(parsing: "assistant.example.com"), clientId: "test-client", accessToken: "tnt_test", refreshToken: "", expiresAt: Date().addingTimeInterval(3600), username: "test-owner")
        return (Connection(account: account, transport: fake), fake)
    }

    func testAListAnswerIsDecoded() async throws {
        let answer =
            #"{"data":{"ListPhotos":["# + photoJSON(id: "p1", kind: "video", takenAt: "2026-03-10T08:00:00.123456789Z", offsetMinutes: 540, durationMS: 83_000, smallPreviewUrl: "/api/v1/photos/p1/previews/small?v=1") + "]}}"
        let (connection, fake) = try connection(answering: answer)
        let photos = try await connection.photos(in: .favorites, beforePhotoId: "p0")
        XCTAssertEqual(photos.map(\.id), ["p1"])
        XCTAssertTrue(photos[0].isVideo)
        XCTAssertEqual(photos[0].durationMS, 83_000)
        XCTAssertEqual(photos[0].takenAtOffsetMinutes, 540)
        XCTAssertNil(photos[0].latitude)
        XCTAssertEqual(photos[0].gridPreviewPath, "/api/v1/photos/p1/previews/small?v=1")
        // Only the small preview is made so far, so the viewer starts from it.
        XCTAssertEqual(photos[0].viewerPreviewPath, "/api/v1/photos/p1/previews/small?v=1")
        let body = try XCTUnwrap(fake.sent.first?.httpBody)
        let sent = try XCTUnwrap(JSONSerialization.jsonObject(with: body) as? [String: Any])
        let variables = try XCTUnwrap(sent["variables"] as? [String: Any])
        XCTAssertEqual(variables["beforePhotoId"] as? String, "p0")
        XCTAssertEqual(variables["isFavorite"] as? Bool, true)
        XCTAssertEqual(variables["pageSize"] as? Int, PhotoPaging.pageSize)
        XCTAssertNil(variables["isTrashed"])
    }

    func testTheTrashAsksForTrashedPhotosOnly() async throws {
        let (connection, fake) = try connection(answering: #"{"data":{"ListPhotos":[]}}"#)
        _ = try await connection.photos(in: .trash)
        _ = try await connection.photos(in: .library)
        let variables = try fake.sent.map { request -> [String: Any] in
            let sent = try XCTUnwrap(JSONSerialization.jsonObject(with: try XCTUnwrap(request.httpBody)) as? [String: Any])
            return try XCTUnwrap(sent["variables"] as? [String: Any])
        }
        XCTAssertEqual(variables[0]["isTrashed"] as? Bool, true)
        XCTAssertNil(variables[0]["isFavorite"])
        XCTAssertNil(variables[1]["isTrashed"])
        XCTAssertNil(variables[1]["isFavorite"])
    }

    func testADetailAnswerIsDecoded() throws {
        let json =
            #"{"photo":"# + photoJSON(id: "p1")
            + #","photoFiles":[{"photoFileRole":"original","contentType":"image/heic","fileSizeBytes":2345678,"originalFilename":"IMG_0001.HEIC","fileUrl":"/api/v1/photos/p1/files/original"}],"photoPreviews":[{"photoPreviewSize":"large","contentType":"image/jpeg","previewSizeBytes":300000,"previewUrl":"/api/v1/photos/p1/previews/large?v=1"}]}"#
        let detail = try JSONDecoder().decode(PhotoDetail.self, from: Data(json.utf8))
        XCTAssertEqual(detail.originalFile?.fileSizeBytes, 2_345_678)
        XCTAssertEqual(detail.originalFile?.fileUrl, "/api/v1/photos/p1/files/original")
        XCTAssertEqual(detail.photoPreviews.first?.photoPreviewSize, "large")
    }

    func testAPhotoTakenAbroadIsPlacedByTheWallClockThere() throws {
        // 23:30 UTC on March 10th. In Tokyo (UTC+9) that is 08:30 on the 11th;
        // in Los Angeles (UTC-8 at the time of year the photo says) 15:30 on the 10th.
        let inTokyo = try photo("tokyo", takenAt: "2026-03-10T23:30:00Z", offsetMinutes: 540)
        let inLosAngeles = try photo("la", takenAt: "2026-03-10T23:30:00Z", offsetMinutes: -480)
        let moment = try XCTUnwrap(PhotoTimeline.moment(of: inTokyo, phoneTimeZone: losAngeles))
        XCTAssertTrue(moment.isTimeZoneOfPhoto)
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = moment.timeZone
        XCTAssertEqual(calendar.dateComponents([.month, .day, .hour, .minute], from: moment.date), DateComponents(month: 3, day: 11, hour: 8, minute: 30))
        // The phone's own zone does not matter to either: both stay on the
        // day of the place they were taken, whichever zone the phone is in.
        for phoneTimeZone in [losAngeles, tokyo] {
            let days = PhotoTimeline.days([inTokyo, inLosAngeles], phoneTimeZone: phoneTimeZone)
            XCTAssertEqual(days.map(\.id), ["2026-03-11", "2026-03-10"])
            XCTAssertEqual(days.map { $0.photos.map(\.id) }, [["tokyo"], ["la"]])
        }
    }

    func testTheChipOfAPhotoNamesItByPlaceAndDayThenByFileName() throws {
        // The day is the one on the wall where it was taken: 08:30 on the 11th
        // in Tokyo, whichever zone the phone is in.
        let inTokyo = try photo("tokyo", takenAt: "2026-03-10T23:30:00Z", offsetMinutes: 540)
        XCTAssertEqual(PhotoTimeline.referenceTitle(of: inTokyo, phoneTimeZone: losAngeles), "2026-03-11")
        let atTheHarbor = try photo("harbor", takenAt: "2026-03-10T23:30:00Z", offsetMinutes: 540, placeName: "Harbor Town")
        XCTAssertEqual(PhotoTimeline.referenceTitle(of: atTheHarbor, phoneTimeZone: losAngeles), "Harbor Town \u{00B7} 2026-03-11")
        let placeOnly = try photo("place", placeName: "Harbor Town")
        XCTAssertEqual(PhotoTimeline.referenceTitle(of: placeOnly), "Harbor Town")
        // Nothing but a date the upload arrived on is no date taken: the file name.
        let undated = try photo("undated")
        XCTAssertEqual(PhotoTimeline.referenceTitle(of: undated), "IMG_0001.HEIC")
    }

    func testAPhotoReferenceIsSentByItsIdAndNamedByItsTitle() throws {
        let reference = AgentReference(photoId: "photo-1", photoTitle: "Harbor Town \u{00B7} 2026-03-11")
        XCTAssertEqual(reference.label, "Harbor Town \u{00B7} 2026-03-11")
        XCTAssertFalse(reference.isReply)
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: JSONEncoder().encode(reference)) as? [String: Any])
        XCTAssertEqual(object["photoId"] as? String, "photo-1")
        XCTAssertEqual(object["photoTitle"] as? String, "Harbor Town \u{00B7} 2026-03-11")
        XCTAssertNil(object["recordingId"])
        XCTAssertEqual(AgentReference(photoId: "photo-1", photoTitle: "").label, "")
    }

    func testAPhotoWithoutAnOffsetFollowsThePhonesZone() throws {
        // The same instant, 23:30 UTC, falls on two different days for a phone
        // in Los Angeles (16:30 on the 10th) and one in Tokyo (08:30 on the 11th).
        let unplaced = try photo("a", takenAt: "2026-03-10T23:30:00Z")
        XCTAssertEqual(PhotoTimeline.days([unplaced], phoneTimeZone: losAngeles).map(\.id), ["2026-03-10"])
        XCTAssertEqual(PhotoTimeline.days([unplaced], phoneTimeZone: tokyo).map(\.id), ["2026-03-11"])
        XCTAssertEqual(try XCTUnwrap(PhotoTimeline.moment(of: unplaced, phoneTimeZone: tokyo)).isTimeZoneOfPhoto, false)
    }

    func testAPhotoWithoutADateIsPlacedByWhenItArrived() throws {
        let arrived = try photo("arrived", createdAt: "2026-03-10T23:30:00Z")
        XCTAssertEqual(PhotoTimeline.days([arrived], phoneTimeZone: tokyo).map(\.id), ["2026-03-11"])
        XCTAssertEqual(PhotoTimeline.days([arrived], phoneTimeZone: losAngeles).map(\.id), ["2026-03-10"])
        // An offset on a photo that has no takenAt means nothing.
        let withOffsetOnly = try photo("odd", offsetMinutes: -660, createdAt: "2026-03-10T23:30:00Z")
        XCTAssertEqual(PhotoTimeline.days([withOffsetOnly], phoneTimeZone: tokyo).map(\.id), ["2026-03-11"])
    }

    func testPhotosAcrossMidnightAreSplitIntoDaysInOrder() throws {
        // Newest first, as the server lists them, in one zone (UTC+9).
        let photos = try [
            photo("late", takenAt: "2026-03-10T15:10:00Z", offsetMinutes: 540),  // 00:10 on the 11th
            photo("evening", takenAt: "2026-03-10T14:50:00Z", offsetMinutes: 540),  // 23:50 on the 10th
            photo("noon", takenAt: "2026-03-10T03:00:00Z", offsetMinutes: 540),  // 12:00 on the 10th
            photo("morning", takenAt: "2026-03-09T21:30:00Z", offsetMinutes: 540),  // 06:30 on the 10th
        ]
        let days = PhotoTimeline.days(photos, phoneTimeZone: losAngeles)
        XCTAssertEqual(days.map(\.id), ["2026-03-11", "2026-03-10"])
        XCTAssertEqual(days[1].photos.map(\.id), ["evening", "noon", "morning"])
        XCTAssertEqual(days[0].day, 11)
        let noon = try XCTUnwrap(days[0].noonUTC)
        XCTAssertEqual(noon, ServerTime.date("2026-03-11T12:00:00Z"))
        XCTAssertTrue(PhotoTimeline.days([]).isEmpty)
    }

    func testAPhotoWithNoDateAtAllHasADayOfItsOwn() throws {
        let nothing = try photo("none", createdAt: "not a time")
        let days = PhotoTimeline.days([nothing])
        XCTAssertEqual(days.map(\.id), ["unknown"])
        XCTAssertNil(days[0].noonUTC)
        XCTAssertNil(PhotoTimeline.takenText(of: nothing))
    }

    func testTheTimeShownInInfoIsReadWhereThePhotoWasTaken() throws {
        let inTokyo = try photo("tokyo", takenAt: "2026-03-10T23:30:00Z", offsetMinutes: 540)
        let text = try XCTUnwrap(PhotoTimeline.takenText(of: inTokyo, phoneTimeZone: losAngeles, locale: Locale(identifier: "en_US")))
        XCTAssertTrue(text.contains("March 11, 2026"), text)
        XCTAssertTrue(text.contains("8:30"), text)
    }

    func testDurationsAreMinutesAndSeconds() {
        XCTAssertEqual(PhotoTimeline.durationText(milliseconds: 0), "0:00")
        XCTAssertEqual(PhotoTimeline.durationText(milliseconds: 7_400), "0:07")
        XCTAssertEqual(PhotoTimeline.durationText(milliseconds: 83_000), "1:23")
        XCTAssertEqual(PhotoTimeline.durationText(milliseconds: 59_600), "1:00")
        XCTAssertEqual(PhotoTimeline.durationText(milliseconds: 3_600_000), "1:00:00")
        XCTAssertEqual(PhotoTimeline.durationText(milliseconds: 3_725_000), "1:02:05")
        XCTAssertEqual(PhotoTimeline.durationText(milliseconds: -5), "0:00")
    }

    func testTheNextPageStartsNearTheEndOfWhatIsLoaded() throws {
        let photos = try (0..<100).map { try photo("p\($0)") }
        XCTAssertFalse(PhotoPaging.shouldLoadMore(whenShowing: "p10", in: photos, loadAhead: 30))
        XCTAssertTrue(PhotoPaging.shouldLoadMore(whenShowing: "p70", in: photos, loadAhead: 30))
        XCTAssertTrue(PhotoPaging.shouldLoadMore(whenShowing: "p99", in: photos, loadAhead: 30))
        XCTAssertFalse(PhotoPaging.shouldLoadMore(whenShowing: "missing", in: photos, loadAhead: 30))
        XCTAssertTrue(PhotoPaging.hasMore(afterPageOf: 200))
        XCTAssertFalse(PhotoPaging.hasMore(afterPageOf: 199))
        let page = try [photo("p99"), photo("p100")]
        XCTAssertEqual(PhotoPaging.appending(page, to: photos).count, 101)
    }
}

final class PhotoDownloadTests: XCTestCase {
    private let photoJSON =
        #"{"id":"p1","createdAt":"2026-03-01T00:00:00Z","photoKind":"picture","takenAt":null,"takenAtOffsetMinutes":null,"widthPixels":10,"heightPixels":10,"durationMS":0,"latitude":null,"longitude":null,"placeName":"","cameraMake":"","cameraModel":"","originalFilename":"IMG_0001.HEIC","isFavorite":false,"isScreenshot":false,"photoProcessingState":"ready","trashedAt":null,"smallPreviewUrl":"/api/v1/photos/p1/previews/small?v=1","largePreviewUrl":"/api/v1/photos/p1/previews/large?v=1","videoPreviewUrl":""}"#

    private func detailAnswer() -> String {
        #"{"data":{"ReadPhoto":{"photo":"# + photoJSON
            + #","photoFiles":[{"photoFileRole":"original","contentType":"image/heic","fileSizeBytes":9,"originalFilename":"IMG_0001.HEIC","fileUrl":"/api/v1/photos/p1/files/original"}],"photoPreviews":[{"photoPreviewSize":"large","contentType":"image/jpeg","previewSizeBytes":5,"previewUrl":"/api/v1/photos/p1/previews/large?v=1"}]}}}"#
    }

    private func connection(isOriginalReadable: Bool) throws -> (Connection, FakeServer) {
        let detail = detailAnswer()
        let fake = FakeServer { request in
            let path = request.url?.path ?? ""
            if path.hasSuffix("/graphql") {
                let body = String(decoding: request.httpBody ?? Data(), as: UTF8.self)
                if body.contains("PhotoPlaybackAddress") {
                    if body.contains(#""photoFileRole":"original""#) {
                        return (200, #"{"data":{"PhotoPlaybackAddress":"/api/v1/photos/playback/p1/files/original?owner=o1&signature=s1"}}"#)
                    }
                    return (200, #"{"data":{"PhotoPlaybackAddress":"/api/v1/photos/playback/p1/previews/large?owner=o1&signature=s2"}}"#)
                }
                return (200, detail)
            }
            if path.hasSuffix("/files/original") { return isOriginalReadable ? (200, "original") : (404, "gone") }
            return (200, "preview")
        }
        let account = Account(server: try ServerAddress(parsing: "assistant.example.com"), clientId: "test-client", accessToken: "tnt_test", refreshToken: "", expiresAt: Date().addingTimeInterval(3600), username: "test-owner")
        return (Connection(account: account, transport: fake), fake)
    }

    private func decodedPhoto() throws -> Photo {
        try JSONDecoder().decode(PhotoDetail.self, from: Data(#"{"photo":"#.utf8) + Data(photoJSON.utf8) + Data(#","photoFiles":[],"photoPreviews":[]}"#.utf8)).photo
    }

    func testTheOriginalFileIsWhatIsSavedAndGoesStraightToAFile() async throws {
        let (connection, fake) = try connection(isOriginalReadable: true)
        let download = try await connection.downloadPhotoFile(decodedPhoto())
        defer { try? FileManager.default.removeItem(at: download.fileURL.deletingLastPathComponent()) }
        XCTAssertTrue(download.isOriginal)
        XCTAssertEqual(try String(contentsOf: download.fileURL, encoding: .utf8), "original")
        XCTAssertEqual(download.fileName, "IMG_0001.HEIC")
        XCTAssertEqual(download.fileURL.lastPathComponent, "IMG_0001.HEIC")
        // It was read from the signed address, which needs no sign-in, not through the token.
        let fileRequest = try XCTUnwrap(fake.sent.first { $0.url?.path.hasSuffix("/files/original") == true })
        XCTAssertEqual(fileRequest.url?.absoluteString, "https://assistant.example.com/api/v1/photos/playback/p1/files/original?owner=o1&signature=s1")
        XCTAssertNil(fileRequest.value(forHTTPHeaderField: "Authorization"))
    }

    func testTheLargePreviewStandsInWhenTheOriginalCannotBeRead() async throws {
        let (connection, _) = try connection(isOriginalReadable: false)
        let download = try await connection.downloadPhotoFile(decodedPhoto())
        defer { try? FileManager.default.removeItem(at: download.fileURL.deletingLastPathComponent()) }
        XCTAssertFalse(download.isOriginal)
        XCTAssertEqual(try String(contentsOf: download.fileURL, encoding: .utf8), "preview")
        XCTAssertEqual(download.fileName, "IMG_0001.jpg")
    }

    /// The variables of the last document sent, which is where an argument left out is seen to be left out.
    private func sentVariables(of fake: FakeServer) throws -> [String: String] {
        let body = try XCTUnwrap(fake.sent.last?.httpBody)
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: body) as? [String: Any])
        return try XCTUnwrap(object["variables"] as? [String: String])
    }

    func testAPlaybackAddressIsAFullAddressAskedForOneSource() async throws {
        let (connection, fake) = try connection(isOriginalReadable: true)
        let address = try await connection.photoPlaybackAddress("p1", source: .preview("large"))
        XCTAssertEqual(address.absoluteString, "https://assistant.example.com/api/v1/photos/playback/p1/previews/large?owner=o1&signature=s2")
        XCTAssertEqual(try sentVariables(of: fake), ["photoId": "p1", "photoPreviewSize": "large"])
        _ = try await connection.photoPlaybackAddress("p1", source: .file("original"))
        XCTAssertEqual(try sentVariables(of: fake), ["photoId": "p1", "photoFileRole": "original"])
        XCTAssertTrue(Documents.photoPlaybackAddress.contains("PhotoPlaybackAddress(photoId: $photoId, photoPreviewSize: $photoPreviewSize, photoFileRole: $photoFileRole)"))
    }

    func testFileExtensionsFollowTheContentType() {
        XCTAssertEqual(PhotoDownload.fileExtension(forContentType: "image/jpeg; charset=binary"), "jpg")
        XCTAssertEqual(PhotoDownload.fileExtension(forContentType: "VIDEO/MP4"), "mp4")
        XCTAssertNil(PhotoDownload.fileExtension(forContentType: "application/octet-stream"))
    }
}
