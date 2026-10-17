import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif
import XCTest

@testable import TeaNodeKit

private let albumJSON =
    #"{"id":"a1","albumTitle":"Picnic","createdAt":"2026-03-01T00:00:00Z","updatedAt":"2026-03-02T00:00:00Z","coverPhotoId":null,"photoCount":3,"firstTakenAt":"2026-03-10T09:00:00Z","lastTakenAt":"2026-03-12T18:00:00Z","coverPreviewUrl":"/api/v1/photos/p1/previews/small?v=1"}"#

private func photoJSON(_ id: String) -> String {
    #"{"id":"\#(id)","createdAt":"2026-03-01T00:00:00Z","photoKind":"picture","takenAt":null,"takenAtOffsetMinutes":null,"widthPixels":1,"heightPixels":1,"durationMS":0,"latitude":null,"longitude":null,"placeName":"","cameraMake":"","cameraModel":"","originalFilename":"","isFavorite":false,"isScreenshot":false,"photoProcessingState":"ready","trashedAt":null,"smallPreviewUrl":"","largePreviewUrl":"","videoPreviewUrl":""}"#
}

final class PhotoAlbumTests: XCTestCase {
    private let utc = TimeZone(secondsFromGMT: 0)!
    private let english = Locale(identifier: "en_US")

    private func connection(answer: @escaping @Sendable (String) -> String) throws -> (Connection, FakeServer) {
        let fake = FakeServer { request in (200, answer(String(decoding: request.httpBody ?? Data(), as: UTF8.self))) }
        let account = Account(server: try ServerAddress(parsing: "assistant.example.com"), clientId: "test-client", accessToken: "tnt_test", refreshToken: "", expiresAt: Date().addingTimeInterval(3600), username: "test-owner")
        return (Connection(account: account, transport: fake), fake)
    }

    private func variables(of request: URLRequest) throws -> [String: Any] {
        let sent = try XCTUnwrap(JSONSerialization.jsonObject(with: try XCTUnwrap(request.httpBody)) as? [String: Any])
        return try XCTUnwrap(sent["variables"] as? [String: Any])
    }

    func testAnAlbumIsDecodedWithItsDatesAndCover() throws {
        let album = try JSONDecoder().decode(PhotoAlbum.self, from: Data(albumJSON.utf8))
        XCTAssertEqual(album.albumTitle, "Picnic")
        XCTAssertEqual(album.photoCount, 3)
        XCTAssertNil(album.coverPhotoId)
        XCTAssertEqual(album.coverPreviewUrl, "/api/v1/photos/p1/previews/small?v=1")
        // Newer ICU and CLDR put thin spaces round the dash; they read the same.
        let text = try XCTUnwrap(album.dateRangeText(timeZone: utc, locale: english))
        let spaced = text.replacingOccurrences(of: "\u{2009}", with: " ").replacingOccurrences(of: "\u{202F}", with: " ").replacingOccurrences(of: "\u{00A0}", with: " ")
        XCTAssertEqual(spaced, "Mar 10 – 12, 2026")
    }

    func testAnAlbumWithNoPhotosHasNoDates() throws {
        let album = PhotoAlbum(id: "a2", albumTitle: "Empty")
        XCTAssertNil(album.dateRangeText(timeZone: utc, locale: english))
    }

    func testOneDayIsWrittenAsOneDate() {
        let text = PhotoAlbumText.dateRange(from: "2026-03-10T08:00:00Z", to: "2026-03-10T20:00:00Z", timeZone: utc, locale: english)
        XCTAssertEqual(text, "Mar 10, 2026")
    }

    func testASuggestionIsDecodedAndKeepsTheServersMoments() throws {
        let json =
            #"{"takenAfter":"2026-03-10T08:00:00Z","takenBefore":"2026-03-12T20:30:01Z","placeNames":["Fixtureville","Exampleton"],"albumTitle":"Fixtureville and Exampleton","photoCount":16,"samplePhotoIds":["p1","p2"]}"#
        let suggestion = try JSONDecoder().decode(PhotoAlbumSuggestion.self, from: Data(json.utf8))
        XCTAssertEqual(suggestion.placeNames, ["Fixtureville", "Exampleton"])
        XCTAssertEqual(suggestion.photoCount, 16)
        XCTAssertEqual(suggestion.samplePhotoIds, ["p1", "p2"])
        XCTAssertEqual(suggestion.takenBefore, "2026-03-12T20:30:01Z")
        XCTAssertEqual(suggestion.id, "2026-03-10T08:00:00Z/2026-03-12T20:30:01Z")
    }

    func testPlacesAreSaidOnceByASuggestionWhoseTitleIsMadeOfThem() {
        func suggestion(title: String, places: [String]) -> PhotoAlbumSuggestion {
            PhotoAlbumSuggestion(takenAfter: "a", takenBefore: "b", placeNames: places, albumTitle: title, photoCount: 5, samplePhotoIds: [])
        }
        XCTAssertTrue(suggestion(title: "Fixtureville, Exampleland", places: ["Fixtureville, Exampleland"]).isTitleMadeOfPlaces)
        XCTAssertTrue(
            suggestion(title: "Alpha and Beta, Exampleland", places: ["Alpha, Exampleland", "Beta, Exampleland"]).isTitleMadeOfPlaces)
        XCTAssertTrue(
            suggestion(title: "Alpha, Firstland and Beta, Secondland", places: ["Alpha, Firstland", "Beta, Secondland"]).isTitleMadeOfPlaces)
        // A title that stops at "and more" leaves a place unsaid, and a title of dates names none.
        XCTAssertFalse(
            suggestion(title: "Alpha, Beta and more, Exampleland", places: ["Alpha, Exampleland", "Beta, Exampleland", "Gamma, Exampleland"])
                .isTitleMadeOfPlaces)
        XCTAssertFalse(suggestion(title: "July 12 to 14, 2030", places: ["Fixtureville, Exampleland"]).isTitleMadeOfPlaces)
        XCTAssertFalse(suggestion(title: "July 12, 2030", places: []).isTitleMadeOfPlaces)
    }

    func testADetailNamesTheAlbumsAndOlderServersSayNothingOfThem() throws {
        let photo =
            #"{"id":"p1","createdAt":"2026-03-01T00:00:00Z","photoKind":"picture","takenAt":null,"takenAtOffsetMinutes":null,"widthPixels":1,"heightPixels":1,"durationMS":0,"latitude":null,"longitude":null,"placeName":"","cameraMake":"","cameraModel":"","originalFilename":"","isFavorite":false,"isScreenshot":false,"photoProcessingState":"ready","trashedAt":null,"smallPreviewUrl":"","largePreviewUrl":"","videoPreviewUrl":""}"#
        let withAlbums = try JSONDecoder().decode(
            PhotoDetail.self, from: Data(#"{"photo":\#(photo),"photoFiles":[],"photoPreviews":[],"albums":[{"id":"a1","albumTitle":"Picnic"}]}"#.utf8))
        XCTAssertEqual(withAlbums.albums, [PhotoAlbumReference(id: "a1", albumTitle: "Picnic")])
        let without = try JSONDecoder().decode(PhotoDetail.self, from: Data(#"{"photo":\#(photo),"photoFiles":[],"photoPreviews":[]}"#.utf8))
        XCTAssertEqual(without.albums, [])
    }

    func testPhotoIdsAreSplitIntoTheBatchesTheServerTakes() {
        let photoIds = (0..<1201).map { "p\($0)" }
        let batches = PhotoBatches.batches(of: photoIds)
        XCTAssertEqual(batches.map(\.count), [500, 500, 201])
        XCTAssertEqual(batches.flatMap { $0 }, photoIds)
        XCTAssertEqual(PhotoBatches.batches(of: []), [])
    }

    func testTheAlbumsAreListedAsTheServerSaysThem() async throws {
        let (connection, _) = try connection { _ in #"{"data":{"ListPhotoAlbums":[\#(albumJSON)]}}"# }
        let albums = try await connection.photoAlbums()
        XCTAssertEqual(albums.map(\.id), ["a1"])
    }

    func testThePhotosOfAnAlbumAreAskedForByItsId() async throws {
        let (connection, fake) = try connection { _ in #"{"data":{"ListPhotos":[]}}"# }
        _ = try await connection.photos(inAlbum: "a1", beforePhotoId: "p9")
        let sent = try variables(of: try XCTUnwrap(fake.sent.first))
        XCTAssertEqual(sent["albumId"] as? String, "a1")
        XCTAssertEqual(sent["beforePhotoId"] as? String, "p9")
        XCTAssertNil(sent["isTrashed"])
    }

    func testAnAlbumIsMadeWithATrimmedTitleAndTheRestOfItsPhotosAreAddedAfterwards() async throws {
        let (connection, fake) = try connection { body in
            body.contains("CreatePhotoAlbum") ? #"{"data":{"CreatePhotoAlbum":\#(albumJSON)}}"# : #"{"data":{"AddPhotosToAlbum":1}}"#
        }
        let photoIds = (0..<501).map { "p\($0)" }
        let album = try await connection.createPhotoAlbum(title: "  Picnic \n", photoIds: photoIds)
        XCTAssertEqual(album.id, "a1")
        XCTAssertEqual(fake.sent.count, 2)
        let created = try variables(of: fake.sent[0])
        XCTAssertEqual(created["albumTitle"] as? String, "Picnic")
        XCTAssertEqual((created["photoIds"] as? [String])?.count, 500)
        let added = try variables(of: fake.sent[1])
        XCTAssertEqual(added["photoAlbumId"] as? String, "a1")
        XCTAssertEqual(added["photoIds"] as? [String], ["p500"])
    }

    func testAnEmptyAlbumIsMadeWithNoPhotosAndNoSecondCall() async throws {
        let (connection, fake) = try connection { _ in #"{"data":{"CreatePhotoAlbum":\#(albumJSON)}}"# }
        _ = try await connection.createPhotoAlbum(title: "Picnic")
        XCTAssertEqual(fake.sent.count, 1)
        XCTAssertEqual((try variables(of: fake.sent[0])["photoIds"] as? [Any])?.count, 0)
    }

    func testAddingAndRemovingSayHowManyPhotosMoved() async throws {
        let (connection, _) = try connection { body in
            body.contains("RemovePhotosFromAlbum") ? #"{"data":{"RemovePhotosFromAlbum":2}}"# : #"{"data":{"AddPhotosToAlbum":3}}"#
        }
        let addedCount = try await connection.addPhotos(["p1", "p2", "p3"], toAlbum: "a1")
        let removedCount = try await connection.removePhotos(["p1", "p2"], fromAlbum: "a1")
        XCTAssertEqual(addedCount, 3)
        XCTAssertEqual(removedCount, 2)
    }

    func testASuggestionsPhotosAreListedByItsDatesAcrossPages() async throws {
        let firstPage = (0..<500).map { photoJSON("p\($0)") }.joined(separator: ",")
        let lastPage = #"{"data":{"ListPhotos":[\#(photoJSON("p500"))]}}"#
        let wholePage = #"{"data":{"ListPhotos":[\#(firstPage)]}}"#
        let (connection, fake) = try connection { body in
            body.contains(#""beforePhotoId":""#) ? lastPage : wholePage
        }
        let suggestion = PhotoAlbumSuggestion(
            takenAfter: "2026-03-10T08:00:00Z", takenBefore: "2026-03-12T20:30:01Z", placeNames: [], albumTitle: "Trip", photoCount: 501, samplePhotoIds: [])
        let photoIds = try await connection.photoIds(of: suggestion)
        XCTAssertEqual(photoIds.count, 501)
        XCTAssertEqual(photoIds.last, "p500")
        XCTAssertEqual(fake.sent.count, 2)
        let first = try variables(of: fake.sent[0])
        XCTAssertEqual(first["takenAfter"] as? String, "2026-03-10T08:00:00Z")
        XCTAssertEqual(first["takenBefore"] as? String, "2026-03-12T20:30:01Z")
        XCTAssertEqual(try variables(of: fake.sent[1])["beforePhotoId"] as? String, "p499")
    }
}
