import Foundation
import XCTest

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

@testable import TeaNodeKit

/// The pieces of the iPhone's photo screens that live in the Kit: searching,
/// retrying a failed photo, the files a photo is made of, the limits, typed
/// errors, who is offered Photos, and forgetting an account's previews.
final class PhotoClientTests: XCTestCase {
    private func photoJSON(extraFields: String = "") -> String {
        #"{"id":"p1","createdAt":"2026-03-01T00:00:00Z","photoKind":"livePhoto","takenAt":null,"takenAtOffsetMinutes":null,"widthPixels":10,"heightPixels":10,"durationMS":0,"latitude":null,"longitude":null,"placeName":"","cameraMake":"","cameraModel":"","originalFilename":"IMG_0001.HEIC","isFavorite":false,"isScreenshot":false,"photoProcessingState":"failed","trashedAt":null,"smallPreviewUrl":"","largePreviewUrl":"","videoPreviewUrl":""\#(extraFields)}"#
    }

    private func connection(_ answer: @escaping @Sendable (URLRequest) -> (Int, String)) throws -> (Connection, FakeServer) {
        let fake = FakeServer(answer: answer)
        let account = Account(
            server: try ServerAddress(parsing: "assistant.example.com"), clientId: "test-client", accessToken: "tnt_test", refreshToken: "",
            expiresAt: Date().addingTimeInterval(3600), username: "test-owner")
        return (Connection(account: account, transport: fake), fake)
    }

    private func sentVariables(of request: URLRequest) throws -> [String: Any] {
        let object = try XCTUnwrap(JSONSerialization.jsonObject(with: try XCTUnwrap(request.httpBody)) as? [String: Any])
        return try XCTUnwrap(object["variables"] as? [String: Any])
    }

    // MARK: Photos as the list says them

    func testAFailedPhotoSaysWhyAndWhichFilesItHas() throws {
        let photo = try JSONDecoder().decode(
            Photo.self,
            from: Data(photoJSON(extraFields: ##","photoFileRoles":["original","edited","liveVideo"],"processingErrorMessage":"The picture could not be read.""##).utf8))
        XCTAssertTrue(photo.isFailed)
        XCTAssertEqual(photo.processingErrorMessage, "The picture could not be read.")
        XCTAssertTrue(photo.hasEditedFile)
        XCTAssertTrue(photo.hasLiveVideoFile)
    }

    func testAServerThatSaysNothingOfFilesOrReasonsReadsAsNone() throws {
        let photo = try JSONDecoder().decode(Photo.self, from: Data(photoJSON().utf8))
        XCTAssertEqual(photo.processingErrorMessage, "")
        XCTAssertEqual(photo.photoFileRoles, [])
        XCTAssertFalse(photo.hasEditedFile)
        XCTAssertFalse(photo.hasLiveVideoFile)
    }

    func testTheListAsksForTheReasonAndTheFilesOfEachPhoto() {
        XCTAssertTrue(Documents.photoFields.contains("processingErrorMessage"))
        XCTAssertTrue(Documents.photoFields.contains("photoFileRoles"))
    }

    // MARK: Search

    func testASearchAsksForTheTypedWordsAndAtMostFiveHundredPhotos() async throws {
        let (connection, fake) = try connection { _ in (200, #"{"data":{"ListPhotos":[]}}"#) }
        _ = try await connection.photos(matching: "  harbor market ")
        let variables = try sentVariables(of: try XCTUnwrap(fake.sent.first))
        XCTAssertEqual(variables["searchText"] as? String, "harbor market")
        XCTAssertEqual(variables["pageSize"] as? Int, 500)
        XCTAssertNil(variables["beforePhotoId"], "a search is not paged")
        XCTAssertTrue(Documents.listPhotos.contains("searchText: $searchText"))
    }

    func testTypedSpacesAreNotASearch() {
        XCTAssertNil(PhotoSearch.words(from: "   \n"))
        XCTAssertEqual(PhotoSearch.words(from: " cat "), "cat")
    }

    func testSearchResultsKeepTheirRankInOneGroup() throws {
        let first = try JSONDecoder().decode(Photo.self, from: Data(photoJSON().utf8))
        XCTAssertEqual(PhotoTimeline.rankedDay([first, first]).count, 1)
        XCTAssertEqual(PhotoTimeline.rankedDay([first, first])[0].photos.count, 2)
        XCTAssertTrue(PhotoTimeline.rankedDay([]).isEmpty)
    }

    // MARK: Location

    func testSearchingPlacesSendsTheWordsAndWhereToBeNearAndReadsTheDistance() async throws {
        let (connection, fake) = try connection { _ in
            (200, #"{"data":{"SearchPhotoPlaces":[{"placeName":"Lakeside, Exampleland","latitude":10.5,"longitude":-20.25,"distanceKilometers":12.5},{"placeName":"Lakeview, Exampleland","latitude":11,"longitude":-21,"distanceKilometers":null}]}}"#)
        }
        let places = try await connection.searchPhotoPlaces(words: "lake", nearLatitude: 10.0, nearLongitude: -20.0)
        XCTAssertEqual(places.map(\.placeName), ["Lakeside, Exampleland", "Lakeview, Exampleland"])
        XCTAssertEqual(places[0].distanceKilometers, 12.5)
        XCTAssertNil(places[1].distanceKilometers)
        XCTAssertEqual(places[0].longitude, -20.25)
        let variables = try sentVariables(of: try XCTUnwrap(fake.sent.first))
        XCTAssertEqual(variables["query"] as? String, "lake")
        XCTAssertEqual(variables["nearLatitude"] as? Double, 10.0)
        XCTAssertEqual(variables["nearLongitude"] as? Double, -20.0)
        XCTAssertTrue(Documents.searchPhotoPlaces.contains("SearchPhotoPlaces(query: $query"))
    }

    func testSearchingPlacesWithNoPointToBeNearLeavesItOut() async throws {
        let (connection, fake) = try connection { _ in (200, #"{"data":{"SearchPhotoPlaces":[]}}"#) }
        let places = try await connection.searchPhotoPlaces(words: "lake", nearLatitude: 10.0, nearLongitude: nil)
        XCTAssertTrue(places.isEmpty)
        let variables = try sentVariables(of: try XCTUnwrap(fake.sent.first))
        XCTAssertNil(variables["nearLatitude"])
        XCTAssertNil(variables["nearLongitude"])
    }

    func testSettingALocationSendsThePointAndSaysHowManyChanged() async throws {
        let (connection, fake) = try connection { _ in (200, #"{"data":{"SetPhotoLocation":2}}"#) }
        let changedCount = try await connection.setPhotoLocation(photoIds: ["p1", "p2"], latitude: 10.5, longitude: -20.25)
        XCTAssertEqual(changedCount, 2)
        let variables = try sentVariables(of: try XCTUnwrap(fake.sent.first))
        XCTAssertEqual(variables["photoIds"] as? [String], ["p1", "p2"])
        XCTAssertEqual(variables["latitude"] as? Double, 10.5)
        XCTAssertEqual(variables["longitude"] as? Double, -20.25)
        XCTAssertTrue(Documents.setPhotoLocation.contains("SetPhotoLocation(photoIds: $photoIds"))
    }

    func testRemovingLocationsNamesThePhotosAndSaysHowManyChanged() async throws {
        let (connection, fake) = try connection { _ in (200, #"{"data":{"RemovePhotoLocation":1}}"#) }
        let changedCount = try await connection.removePhotoLocation(photoIds: ["p1"])
        XCTAssertEqual(changedCount, 1)
        let variables = try sentVariables(of: try XCTUnwrap(fake.sent.first))
        XCTAssertEqual(variables["photoIds"] as? [String], ["p1"])
        XCTAssertTrue(Documents.removePhotoLocation.contains("RemovePhotoLocation(photoIds: $photoIds)"))
    }

    func testLocationsOfMoreThanFiveHundredPhotosAreChangedInBatches() async throws {
        let (connection, fake) = try connection { request in
            let object = (try? JSONSerialization.jsonObject(with: request.httpBody ?? Data())) as? [String: Any]
            let count = ((object?["variables"] as? [String: Any])?["photoIds"] as? [Any])?.count ?? 0
            return (200, #"{"data":{"SetPhotoLocation":\#(count)}}"#)
        }
        let photoIds = (0..<1_200).map { "p\($0)" }
        let changedCount = try await connection.setPhotoLocation(photoIds: photoIds, latitude: 1, longitude: 2)
        XCTAssertEqual(changedCount, 1_200)
        XCTAssertEqual(fake.sent.count, 3)
    }

    // MARK: Retry

    func testRetryingFailedPhotosNamesThemAndCountsWhatMoved() async throws {
        let (connection, fake) = try connection { _ in (200, #"{"data":{"RetryPhotoProcessing":2}}"#) }
        let retriedCount = try await connection.retryPhotoProcessing(["p1", "p2"])
        XCTAssertEqual(retriedCount, 2)
        XCTAssertEqual(try sentVariables(of: try XCTUnwrap(fake.sent.first))["photoIds"] as? [String], ["p1", "p2"])
        XCTAssertTrue(Documents.retryPhotoProcessing.contains("RetryPhotoProcessing(photoIds: $photoIds)"))
    }

    // MARK: Limits

    func testTheLimitsOfTheLibraryAreDecodedAndAreNoLimitFromAnOlderServer() throws {
        let withLimits = try JSONDecoder().decode(
            PhotoLibrary.self,
            from: Data(
                #"{"photoCount":1,"videoCount":0,"favoriteCount":0,"trashedCount":0,"usedBytes":5000,"maximumFileBytes":2000,"maximumAccountBytes":90000,"trashRetentionDays":30}"#.utf8))
        XCTAssertEqual(withLimits.maximumFileBytes, 2000)
        XCTAssertEqual(withLimits.maximumAccountBytes, 90000)
        let without = try JSONDecoder().decode(
            PhotoLibrary.self, from: Data(#"{"photoCount":1,"videoCount":0,"favoriteCount":0,"trashedCount":0,"usedBytes":5000,"trashRetentionDays":30}"#.utf8))
        XCTAssertEqual(without.maximumFileBytes, 0)
        XCTAssertEqual(without.maximumAccountBytes, 0)
        XCTAssertTrue(Documents.readPhotoLibrary.contains("maximumFileBytes maximumAccountBytes"))
    }

    // MARK: The edited file and the Live Photo's video

    func testTheEditedFileAndTheLiveVideoAreDownloadedFromTheirOwnAddresses() async throws {
        let detail =
            #"{"data":{"ReadPhoto":{"photo":"# + photoJSON()
            + #","photoFiles":[{"photoFileRole":"original","contentType":"image/heic","fileSizeBytes":9,"originalFilename":"IMG_0001.HEIC","fileUrl":"/o"},{"photoFileRole":"liveVideo","contentType":"video/quicktime","fileSizeBytes":9,"originalFilename":"IMG_0001.MOV","fileUrl":"/v"}],"photoPreviews":[]}}}"#
        let (connection, fake) = try connection { request in
            let path = request.url?.path ?? ""
            guard path.hasSuffix("/graphql") else { return (200, "video bytes") }
            let body = String(decoding: request.httpBody ?? Data(), as: UTF8.self)
            if body.contains("PhotoPlaybackAddress") { return (200, #"{"data":{"PhotoPlaybackAddress":"/api/v1/photos/playback/p1/files/liveVideo?signature=s"}}"#) }
            return (200, detail)
        }
        let photo = try JSONDecoder().decode(Photo.self, from: Data(photoJSON().utf8))
        let download = try await connection.downloadPhotoFile(photo, role: "liveVideo")
        defer { try? FileManager.default.removeItem(at: download.fileURL.deletingLastPathComponent()) }
        XCTAssertFalse(download.isOriginal)
        XCTAssertEqual(download.filename, "IMG_0001.MOV")
        XCTAssertEqual(try String(contentsOf: download.fileURL, encoding: .utf8), "video bytes")
        let playbackRequest = try XCTUnwrap(fake.sent.first { String(decoding: $0.httpBody ?? Data(), as: UTF8.self).contains("PhotoPlaybackAddress") })
        XCTAssertEqual(try sentVariables(of: playbackRequest)["photoFileRole"] as? String, "liveVideo")

        do {
            _ = try await connection.downloadPhotoFile(photo, role: "edited")
            XCTFail("a photo with no edited file has nothing to hand on")
        } catch {
            XCTAssertEqual(error as? PhotoError, .nothingToSaveYet)
        }
    }

    // MARK: Typed errors

    func testAFileTheServerDoesNotServeIsATypedErrorWithNoStatusCode() async throws {
        let (gone, _) = try connection { _ in (404, "gone") }
        do {
            _ = try await gone.downloadFile(from: URL(string: "https://assistant.example.com/api/v1/photos/playback/p1/files/original")!, filename: "a.jpg")
            XCTFail("a refused file was returned")
        } catch {
            XCTAssertEqual(error as? PhotoError, .fileNotAvailable)
            XCTAssertFalse(String(describing: error).contains("404"))
        }
        // A server that failed is not "no such file": nothing stands in for it.
        let (failing, _) = try connection { _ in (500, "oops") }
        do {
            _ = try await failing.downloadFile(from: URL(string: "https://assistant.example.com/api/v1/photos/playback/p1/files/original")!, filename: "a.jpg")
            XCTFail("a failed read was returned")
        } catch {
            XCTAssertEqual(error as? PhotoError, .fileCouldNotBeRead)
        }
    }

    func testAPhotoWithNothingToSaveIsATypedError() async throws {
        let detail = #"{"data":{"ReadPhoto":{"photo":"# + photoJSON() + #","photoFiles":[],"photoPreviews":[]}}}"#
        let (connection, _) = try connection { _ in (200, detail) }
        let photo = try JSONDecoder().decode(Photo.self, from: Data(photoJSON().utf8))
        do {
            _ = try await connection.downloadPhotoFile(photo)
            XCTFail("nothing could have been saved")
        } catch {
            XCTAssertEqual(error as? PhotoError, .nothingToSaveYet)
        }
    }

    func testEveryBackupFailureSurvivesBeingKeptInTheIndexFile() throws {
        let failures: [PhotoBackupFailure] = [
            .signInRefused, .serverDidNotSayWhichPhoto, .fileRefused(serverReason: "too large"), .fileRefused(serverReason: nil),
            .serverReason("photo storage is unavailable"), .fileIsEmpty, .fileIsNotInLibrary, .fileIsInICloudOnMobileData, .systemDescription("offline"),
        ]
        for failure in failures {
            let data = try JSONEncoder().encode(failure)
            XCTAssertEqual(try JSONDecoder().decode(PhotoBackupFailure.self, from: data), failure)
        }
    }

    func testAnIndexFileFromBeforeTypedFailuresIsStillRead() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("photo-index-old-" + UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let old =
            #"{"version":1,"accountKey":"photos.example.com|robin","records":{"a":{"clientPhotoId":"local:a","roles":{"original":{"isOnServer":true,"serverPhotoId":"p1","assetModificationDate":"1970-01-01T00:01:30Z","refusalCount":0}}}},"libraryItemCount":4,"lastErrorMessage":"The disk is full.","isSignInNeeded":false}"#
        try Data(old.utf8).write(to: directory.appendingPathComponent(PhotoBackupIndex.indexFilename))
        let index = try PhotoBackupIndex(directory: directory)
        let wasStartedOver = await index.wasStartedOver
        XCTAssertFalse(wasStartedOver, "the file is read, not started over")
        let summary = await index.summary()
        XCTAssertEqual(summary.backedUpItemCount, 1)
        XCTAssertNil(summary.lastFailure, "the old text is not shown: it was English")
    }

    func testNoThrownKitErrorOfPhotosIsEnglish() {
        XCTAssertFalse(PhotoError.nothingToSaveYet is LocalizedError, "the app writes the words, in the person's language")
    }

    // MARK: Who is offered Photos

    private func session(everywhere: [String]?) throws -> AccessSession {
        let permissions = everywhere.map { #","permissions":{"everywhere":[\#($0.map { "\"\($0)\"" }.joined(separator: ","))]}"# } ?? ""
        return try JSONDecoder().decode(AccessSession.self, from: Data(#"{"userId":"robin"\#(permissions)}"#.utf8))
    }

    func testPhotosAreOfferedOnlyToSomeoneWhoMayUseThemOnAServerThatHasThem() throws {
        let allowed = try session(everywhere: ["agent:use", "photos:use"])
        let notAllowed = try session(everywhere: ["agent:use"])
        XCTAssertTrue(PhotosAvailability.isOffered(isServiceEnabled: true, session: allowed))
        XCTAssertFalse(PhotosAvailability.isOffered(isServiceEnabled: true, session: notAllowed))
        XCTAssertFalse(PhotosAvailability.isOffered(isServiceEnabled: false, session: allowed))
    }

    func testPermissionsThatCouldNotBeReadDoNotHidePhotos() throws {
        XCTAssertTrue(PhotosAvailability.isOffered(isServiceEnabled: true, session: nil))
        XCTAssertTrue(PhotosAvailability.isOffered(isServiceEnabled: true, session: try session(everywhere: nil)))
        XCTAssertFalse(PhotosAvailability.isOffered(isServiceEnabled: false, session: nil))
    }
}

final class PhotoPreviewCacheAccountTests: XCTestCase {
    private var directory: URL!

    override func setUpWithError() throws {
        directory = FileManager.default.temporaryDirectory.appendingPathComponent("photo-preview-accounts-\(UUID().uuidString)", isDirectory: true)
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(at: directory)
    }

    private func connection(host: String, username: String) throws -> Connection {
        let fake = FakeServer { _ in (200, String(repeating: "x", count: 25)) }
        let account = Account(
            server: try ServerAddress(parsing: host), clientId: "test-client", accessToken: "tnt_test", refreshToken: "", expiresAt: Date().addingTimeInterval(3600),
            username: username)
        return Connection(account: account, transport: fake)
    }

    func testSigningOutRemovesThatAccountsPreviewsAndLeavesTheOthers() async throws {
        let leaving = try connection(host: "one.example.com", username: "robin")
        let staying = try connection(host: "two.example.com", username: "robin")
        let cache = PhotoPreviewCache(directory: directory, limitBytes: 10_000)
        for number in 1...3 {
            _ = try await cache.data(for: "/api/v1/photos/p\(number)/previews/small?v=1", using: leaving)
        }
        _ = try await cache.data(for: "/api/v1/photos/p1/previews/small?v=1", using: staying)
        let sizeBefore = await cache.sizeBytes()
        XCTAssertEqual(sizeBefore, 100)

        await cache.removeAccount(accountKey: leaving.accountKey)
        let sizeAfter = await cache.sizeBytes()
        XCTAssertEqual(sizeAfter, 25, "only the other account's preview is left on disk")
        let files = try FileManager.default.contentsOfDirectory(atPath: directory.path)
        XCTAssertEqual(files, [PhotoPreviewCache.cacheFilename(accountKey: staying.accountKey, path: "/api/v1/photos/p1/previews/small?v=1")])
        XCTAssertTrue(files.allSatisfy { !$0.hasPrefix(PhotoPreviewCache.accountPrefix(accountKey: leaving.accountKey)) })
    }

    func testThePreviewsOfARemovedAccountAreNotHeldInMemoryEither() async throws {
        let leaving = try connection(host: "one.example.com", username: "robin")
        let fake = FakeServer { _ in (200, "pixels") }
        let account = Account(
            server: try ServerAddress(parsing: "one.example.com"), clientId: "test-client", accessToken: "tnt_test", refreshToken: "", expiresAt: Date().addingTimeInterval(3600),
            username: "robin")
        let counting = Connection(account: account, transport: fake)
        XCTAssertEqual(counting.accountKey, leaving.accountKey)
        let cache = PhotoPreviewCache(directory: directory, limitBytes: 10_000)
        _ = try await cache.data(for: "/api/v1/photos/p1/previews/small?v=1", using: counting)
        _ = try await cache.data(for: "/api/v1/photos/p1/previews/small?v=1", using: counting)
        XCTAssertEqual(fake.sent.count, 1)
        await cache.removeAccount(accountKey: counting.accountKey)
        _ = try await cache.data(for: "/api/v1/photos/p1/previews/small?v=1", using: counting)
        XCTAssertEqual(fake.sent.count, 2, "it is fetched again, not served from memory")
    }
}
