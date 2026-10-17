import Foundation
import XCTest

@testable import TeaNodeKit

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// A directory that is gone when the test ends.
private func temporaryDirectory(_ test: XCTestCase) throws -> URL {
    let directory = FileManager.default.temporaryDirectory.appendingPathComponent("photo-backup-" + UUID().uuidString)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    test.addTeardownBlock { try? FileManager.default.removeItem(at: directory) }
    return directory
}

private let eastern = TimeZone(secondsFromGMT: -4 * 3600)!

private func makeItem(
    _ localIdentifier: String, cloudIdentifier: String? = nil, secondsSince2026 seconds: Double? = 0, modifiedSecondsSince2026: Double? = nil,
    kind: PhotoBackupMediaKind = .picture, location: PhotoBackupLocation? = nil, roles: [PhotoBackupFileRole] = [.original]
) -> PhotoBackupItem {
    let start = Date(timeIntervalSince1970: 1_767_225_600)  // 2026-01-01T00:00:00Z
    return PhotoBackupItem(
        localIdentifier: localIdentifier, cloudIdentifier: cloudIdentifier, creationDate: seconds.map { start.addingTimeInterval($0) },
        modificationDate: (modifiedSecondsSince2026 ?? seconds).map { start.addingTimeInterval($0) }, timeZone: eastern, location: location,
        isFavorite: false, isScreenshot: false, mediaKind: kind,
        resources: roles.map {
            PhotoBackupResource(
                role: $0, originalFilename: "\(localIdentifier)-\($0.rawValue).heic", contentType: $0 == .liveVideo ? "video/quicktime" : "image/heic",
                isLocallyAvailable: true)
        })
}

final class PhotoBackupPlannerTests: XCTestCase {
    func testTheClientPhotoIdIsTheCloudIdentifierElseTheLocalOneWithAPrefix() {
        XCTAssertEqual(PhotoBackupPlanner.clientPhotoId(for: makeItem("A/1", cloudIdentifier: "cloud-1")), "cloud-1")
        XCTAssertEqual(PhotoBackupPlanner.clientPhotoId(for: makeItem("A/1")), "local:A/1")
        XCTAssertEqual(PhotoBackupPlanner.clientPhotoId(for: makeItem("A/1", cloudIdentifier: "")), "local:A/1")
    }

    func testTheHeadersDescribeTheFile() throws {
        let item = PhotoBackupItem(
            localIdentifier: "L1", cloudIdentifier: "cloud-1", creationDate: Date(timeIntervalSince1970: 1_767_225_600 + 3600 * 18 + 42 * 60),
            modificationDate: nil, timeZone: eastern, location: PhotoBackupLocation(latitude: 12.3456789, longitude: -98.7654321),
            isFavorite: true, isScreenshot: false, mediaKind: .livePhoto,
            resources: [PhotoBackupResource(role: .liveVideo, originalFilename: "IMG_0001.MOV", contentType: "video/quicktime", isLocallyAvailable: false)])
        let upload = try XCTUnwrap(PhotoBackupPlanner.plan(items: [item], records: [:]).first)
        XCTAssertEqual(
            upload.headers,
            [
                "X-Photo-Client-ID": "cloud-1",
                "X-Photo-File-Role": "liveVideo",
                "X-Photo-Kind": "livePhoto",
                "X-Photo-Filename": "IMG_0001.MOV",
                "Content-Type": "video/quicktime",
                "X-Photo-Taken-At": "2026-01-01T14:42:00-04:00",
                "X-Photo-Latitude": "12.345679",
                "X-Photo-Longitude": "-98.765432",
                "X-Photo-Is-Favorite": "true",
                "X-Photo-Is-Screenshot": "false",
                "X-Photo-Source": "iphone",
            ])
        XCTAssertEqual(upload.key, "L1|liveVideo")
        XCTAssertFalse(upload.isLocallyAvailable)
    }

    func testAnItemWithoutADateOrPlaceSendsNeitherHeader() throws {
        let upload = try XCTUnwrap(PhotoBackupPlanner.plan(items: [makeItem("L1", secondsSince2026: nil)], records: [:]).first)
        XCTAssertNil(upload.headers["X-Photo-Taken-At"])
        XCTAssertNil(upload.headers["X-Photo-Latitude"])
        XCTAssertNil(upload.headers["X-Photo-Longitude"])
        XCTAssertEqual(upload.headers["X-Photo-Client-ID"], "local:L1")
    }

    func testRFC3339CarriesTheOffsetOfTheZone() {
        let date = Date(timeIntervalSince1970: 1_767_225_600)
        XCTAssertEqual(PhotoBackupPlanner.rfc3339(date, in: TimeZone(secondsFromGMT: 0)!), "2026-01-01T00:00:00+00:00")
        XCTAssertEqual(PhotoBackupPlanner.rfc3339(date, in: TimeZone(secondsFromGMT: 9 * 3600)!), "2026-01-01T09:00:00+09:00")
        XCTAssertEqual(PhotoBackupPlanner.rfc3339(date, in: TimeZone(secondsFromGMT: -(5 * 3600 + 30 * 60))!), "2025-12-31T18:30:00-05:30")
        XCTAssertEqual(PhotoBackupPlanner.rfc3339(date, in: TimeZone(secondsFromGMT: 5 * 3600 + 45 * 60)!), "2026-01-01T05:45:00+05:45")
    }

    func testACoordinateHasSixDecimalsAndAPointWhateverTheLocale() {
        XCTAssertEqual(PhotoBackupPlanner.coordinateText(0), "0.000000")
        XCTAssertEqual(PhotoBackupPlanner.coordinateText(-33.8688), "-33.868800")
        XCTAssertEqual(PhotoBackupPlanner.coordinateText(151.2093), "151.209300")
    }

    func testAFilenameThatAHeaderCannotCarryIsCleaned() {
        XCTAssertEqual(PhotoBackupPlanner.headerSafeFilename("IMG_0001.HEIC"), "IMG_0001.HEIC")
        XCTAssertEqual(PhotoBackupPlanner.headerSafeFilename("holiday 写真.jpg"), "holiday __.jpg")
        XCTAssertEqual(PhotoBackupPlanner.headerSafeFilename("a/b\\c\r\nd"), "a_b_c__d")
        XCTAssertEqual(PhotoBackupPlanner.headerSafeFilename("   "), "")
    }

    func testAnImpossibleCoordinateIsNotSent() throws {
        let item = makeItem("L1", location: PhotoBackupLocation(latitude: 95, longitude: 10))
        let upload = try XCTUnwrap(PhotoBackupPlanner.plan(items: [item], records: [:]).first)
        XCTAssertNil(upload.headers["X-Photo-Latitude"])
        XCTAssertNil(upload.headers["X-Photo-Longitude"])
    }

    func testUploadsComeOldestFirstAndAPhotosOwnFilesOriginalFirst() {
        let items = [
            makeItem("newest", secondsSince2026: 300),
            makeItem("oldest", secondsSince2026: 100, kind: .livePhoto, roles: [.edited, .liveVideo, .original]),
            makeItem("middle", secondsSince2026: 200),
            makeItem("undated", secondsSince2026: nil),
        ]
        XCTAssertEqual(
            PhotoBackupPlanner.plan(items: items, records: [:]).map(\.key),
            ["oldest|original", "oldest|liveVideo", "oldest|edited", "middle|original", "newest|original", "undated|original"])
    }

    func testWhatIsOnTheServerIsNotPlannedAgain() {
        var record = PhotoBackupRecord(clientPhotoId: "local:a")
        record.roles["original"] = PhotoBackupRoleRecord(isOnServer: true, serverPhotoId: "p1")
        let items = [makeItem("a", roles: [.original, .liveVideo]), makeItem("b")]
        XCTAssertEqual(PhotoBackupPlanner.plan(items: items, records: ["a": record]).map(\.key), ["a|liveVideo", "b|original"])
    }

    func testUploadsAlreadyQueuedAreNotPlannedAgain() {
        let items = [makeItem("a"), makeItem("b")]
        XCTAssertEqual(PhotoBackupPlanner.plan(items: items, records: [:], inFlight: ["a|original"]).map(\.key), ["b|original"])
    }

    func testAPhotoKeepsTheNameItFirstWentUpUnder() throws {
        let record = PhotoBackupRecord(clientPhotoId: "local:a")
        let upload = try XCTUnwrap(PhotoBackupPlanner.plan(items: [makeItem("a", cloudIdentifier: "cloud-a", roles: [.original])], records: ["a": record]).first)
        XCTAssertEqual(upload.clientPhotoId, "local:a")
        XCTAssertEqual(upload.headers["X-Photo-Client-ID"], "local:a")
    }

    func testAnEditAfterTheEditWentUpIsSentAgainButAnUnchangedOneIsNot() {
        let sent = Date(timeIntervalSince1970: 1_767_225_700)
        var record = PhotoBackupRecord(clientPhotoId: "local:a")
        record.roles["original"] = PhotoBackupRoleRecord(isOnServer: true, serverPhotoId: "p1")
        record.roles["edited"] = PhotoBackupRoleRecord(isOnServer: true, serverPhotoId: "p1", assetModificationDate: sent)
        let unchanged = makeItem("a", secondsSince2026: 0, modifiedSecondsSince2026: 100, roles: [.original, .edited])
        let editedAgain = makeItem("a", secondsSince2026: 0, modifiedSecondsSince2026: 5000, roles: [.original, .edited])
        XCTAssertEqual(PhotoBackupPlanner.plan(items: [unchanged], records: ["a": record]).map(\.key), [])
        XCTAssertEqual(PhotoBackupPlanner.plan(items: [editedAgain], records: ["a": record]).map(\.key), ["a|edited"])
    }

    func testAnEditNeverSentIsPlannedAfterTheOriginalWentUp() {
        var record = PhotoBackupRecord(clientPhotoId: "local:a")
        record.roles["original"] = PhotoBackupRoleRecord(isOnServer: true, serverPhotoId: "p1")
        XCTAssertEqual(PhotoBackupPlanner.plan(items: [makeItem("a", roles: [.original, .edited])], records: ["a": record]).map(\.key), ["a|edited"])
    }

    func testAFileRefusedOftenEnoughIsLeftAlone() {
        var record = PhotoBackupRecord(clientPhotoId: "local:a")
        record.roles["original"] = PhotoBackupRoleRecord(refusalCount: PhotoBackupPlanner.maximumRefusalCount - 1, refusalMessage: "too large")
        XCTAssertEqual(PhotoBackupPlanner.plan(items: [makeItem("a")], records: ["a": record]).map(\.key), ["a|original"])
        record.roles["original"]?.refusalCount = PhotoBackupPlanner.maximumRefusalCount
        XCTAssertEqual(PhotoBackupPlanner.plan(items: [makeItem("a")], records: ["a": record]).map(\.key), [])
    }
}

final class PhotoBackupIndexTests: XCTestCase {
    func testWhatWentUpIsRememberedAcrossOpeningTheFile() async throws {
        let directory = try temporaryDirectory(self)
        let index = try PhotoBackupIndex(directory: directory)
        await index.adopt(accountKey: "photos.example.com|robin")
        await index.markUploaded(
            localIdentifier: "a", clientPhotoId: "cloud-a", role: .original, serverPhotoId: "p1", assetModificationDate: Date(timeIntervalSince1970: 100))
        await index.markUploaded(localIdentifier: "a", clientPhotoId: "cloud-a", role: .liveVideo, serverPhotoId: "p1", assetModificationDate: nil)
        await index.setChangeToken(Data([1, 2, 3]))
        await index.setLibraryItemCount(40)
        await index.setError(message: "The disk is full.")
        await index.finishPass(at: Date(timeIntervalSince1970: 500))
        await index.flush()

        let reopened = try PhotoBackupIndex(directory: directory)
        let wasStartedOver = await reopened.wasStartedOver
        XCTAssertFalse(wasStartedOver)
        let record = await reopened.record(forLocalIdentifier: "a")
        XCTAssertEqual(record?.clientPhotoId, "cloud-a")
        XCTAssertEqual(record?.roles["original"]?.serverPhotoId, "p1")
        XCTAssertEqual(record?.roles["original"]?.assetModificationDate, Date(timeIntervalSince1970: 100))
        XCTAssertEqual(record?.roles["liveVideo"]?.isOnServer, true)
        let accountKey = await reopened.accountKey
        let token = await reopened.changeToken
        XCTAssertEqual(accountKey, "photos.example.com|robin")
        XCTAssertEqual(token, Data([1, 2, 3]))
        let summary = await reopened.summary()
        XCTAssertEqual(summary.backedUpItemCount, 1)
        XCTAssertEqual(summary.libraryItemCount, 40)
        XCTAssertEqual(summary.lastErrorMessage, "The disk is full.")
        XCTAssertEqual(summary.lastPassFinishedAt, Date(timeIntervalSince1970: 500))
    }

    func testAnUnreadableFileIsStartedOverKeepingNothing() async throws {
        let directory = try temporaryDirectory(self)
        try Data("{ this is not an index".utf8).write(to: directory.appendingPathComponent(PhotoBackupIndex.fileName))
        let index = try PhotoBackupIndex(directory: directory)
        let wasStartedOver = await index.wasStartedOver
        let known = await index.knownLocalIdentifiers()
        let token = await index.changeToken
        XCTAssertTrue(wasStartedOver)
        XCTAssertTrue(known.isEmpty)
        XCTAssertNil(token)
        // It works afterwards, and the bad file is gone.
        await index.markUploaded(localIdentifier: "a", clientPhotoId: "local:a", role: .original, serverPhotoId: "p1", assetModificationDate: nil)
        await index.flush()
        let reopened = try PhotoBackupIndex(directory: directory)
        let record = await reopened.record(forLocalIdentifier: "a")
        XCTAssertNotNil(record)
    }

    func testAFileOfTheWrongShapeIsStartedOverToo() async throws {
        let directory = try temporaryDirectory(self)
        try Data(#"{"records": [1, 2, 3]}"#.utf8).write(to: directory.appendingPathComponent(PhotoBackupIndex.fileName))
        let index = try PhotoBackupIndex(directory: directory)
        let wasStartedOver = await index.wasStartedOver
        XCTAssertTrue(wasStartedOver)
    }

    func testAnotherAccountStartsFromNothing() async throws {
        let index = try PhotoBackupIndex(directory: try temporaryDirectory(self))
        await index.adopt(accountKey: "one|robin")
        await index.markUploaded(localIdentifier: "a", clientPhotoId: "local:a", role: .original, serverPhotoId: "p1", assetModificationDate: nil)
        await index.setChangeToken(Data([9]))
        await index.adopt(accountKey: "one|robin")
        let kept = await index.record(forLocalIdentifier: "a")
        XCTAssertNotNil(kept)
        await index.adopt(accountKey: "two|robin")
        let dropped = await index.record(forLocalIdentifier: "a")
        let token = await index.changeToken
        XCTAssertNil(dropped)
        XCTAssertNil(token)
    }

    func testChangesAreWrittenInGroupsAndFlushWritesTheRest() async throws {
        let directory = try temporaryDirectory(self)
        let index = try PhotoBackupIndex(directory: directory)
        await index.markUploaded(localIdentifier: "a", clientPhotoId: "local:a", role: .original, serverPhotoId: "p1", assetModificationDate: nil)
        let file = directory.appendingPathComponent(PhotoBackupIndex.fileName)
        XCTAssertFalse(FileManager.default.fileExists(atPath: file.path))
        await index.flush()
        XCTAssertTrue(FileManager.default.fileExists(atPath: file.path))
        for number in 0..<25 {
            await index.markUploaded(localIdentifier: "x\(number)", clientPhotoId: "local:x\(number)", role: .original, serverPhotoId: "p", assetModificationDate: nil)
        }
        let partway = try PhotoBackupIndex(directory: directory)
        let known = await partway.knownLocalIdentifiers()
        XCTAssertGreaterThanOrEqual(known.count, 20)
        XCTAssertLessThan(known.count, 26)
    }

    func testPhotosTheServerHasAreMarkedDoneForTheirOriginalOnly() async throws {
        let index = try PhotoBackupIndex(directory: try temporaryDirectory(self))
        let items = [makeItem("a", cloudIdentifier: "cloud-a", roles: [.original, .liveVideo]), makeItem("b"), makeItem("c")]
        let asked = await index.clientPhotoIdsToAsk(about: items)
        XCTAssertEqual(asked, ["cloud-a", "local:b", "local:c"])
        await index.markServerHas([PhotoBackupServerPhoto(id: "p1", clientPhotoId: "cloud-a"), PhotoBackupServerPhoto(id: "p9", clientPhotoId: "unrelated")], for: items)
        let plan = await index.plan(items: items)
        XCTAssertEqual(plan.map(\.key), ["a|liveVideo", "b|original", "c|original"])
        let record = await index.record(forLocalIdentifier: "a")
        XCTAssertEqual(record?.serverPhotoId, "p1")
        let askedAgain = await index.clientPhotoIdsToAsk(about: items)
        XCTAssertEqual(askedAgain, ["local:b", "local:c"])
    }

    func testRefusalsCountAndAreClearedByBackUpNow() async throws {
        let index = try PhotoBackupIndex(directory: try temporaryDirectory(self))
        let items = [makeItem("a")]
        for _ in 0..<PhotoBackupPlanner.maximumRefusalCount {
            await index.markRefused(localIdentifier: "a", clientPhotoId: "local:a", role: .original, message: "not a picture")
        }
        var plan = await index.plan(items: items)
        var summary = await index.summary()
        XCTAssertTrue(plan.isEmpty)
        XCTAssertEqual(summary.refusedFileCount, 1)
        await index.clearRefusals()
        plan = await index.plan(items: items)
        summary = await index.summary()
        XCTAssertEqual(plan.map(\.key), ["a|original"])
        XCTAssertEqual(summary.refusedFileCount, 0)
    }

    func testASignInRefusalIsRememberedUntilCleared() async throws {
        let index = try PhotoBackupIndex(directory: try temporaryDirectory(self))
        await index.setError(message: "refused", isSignInNeeded: true)
        var summary = await index.summary()
        XCTAssertTrue(summary.isSignInNeeded)
        await index.clearError()
        summary = await index.summary()
        XCTAssertFalse(summary.isSignInNeeded)
        XCTAssertNil(summary.lastErrorMessage)
    }
}

final class PhotoBackupServerTests: XCTestCase {
    func testAnUploadThatCameBackWithAPhotoIsDone() {
        XCTAssertEqual(
            PhotoBackupOutcome.interpret(statusCode: 201, photoIdHeader: "p1", bodyText: nil, errorDescription: nil, isConnectivityFailure: false),
            .uploaded(serverPhotoId: "p1"))
        XCTAssertEqual(
            PhotoBackupOutcome.interpret(statusCode: 200, photoIdHeader: " p2 ", bodyText: nil, errorDescription: nil, isConnectivityFailure: false),
            .uploaded(serverPhotoId: "p2"))
    }

    func testASuccessWithoutAPhotoIsTriedAgain() {
        guard case .retryLater = PhotoBackupOutcome.interpret(statusCode: 201, photoIdHeader: nil, bodyText: nil, errorDescription: nil, isConnectivityFailure: false) else {
            return XCTFail("a success that names no photo is not a finished upload")
        }
    }

    func testRefusedSignInStopsTheBackup() {
        for status in [401, 403] {
            XCTAssertEqual(
                PhotoBackupOutcome.interpret(statusCode: status, photoIdHeader: nil, bodyText: "not signed in", errorDescription: nil, isConnectivityFailure: false),
                .signInNeeded)
        }
    }

    func testAFileTheServerRefusesIsRefusedAndAServerTroubleIsRetried() {
        XCTAssertEqual(
            PhotoBackupOutcome.interpret(statusCode: 413, photoIdHeader: nil, bodyText: "the library is full\n", errorDescription: nil, isConnectivityFailure: false),
            .refused(message: "the library is full"))
        XCTAssertEqual(
            PhotoBackupOutcome.interpret(statusCode: 415, photoIdHeader: nil, bodyText: "", errorDescription: nil, isConnectivityFailure: false),
            .refused(message: "The server refused this file (415)."))
        for status in [408, 429, 500, 502, 503] {
            guard case .retryLater(_, let isConnectivityFailure) = PhotoBackupOutcome.interpret(
                statusCode: status, photoIdHeader: nil, bodyText: nil, errorDescription: nil, isConnectivityFailure: false)
            else { return XCTFail("\(status) is the server's trouble, not the file's") }
            XCTAssertFalse(isConnectivityFailure)
        }
    }

    func testATransportErrorIsRetriedAndSaysWhetherTheNetworkWasTheCause() {
        XCTAssertEqual(
            PhotoBackupOutcome.interpret(statusCode: nil, photoIdHeader: nil, bodyText: nil, errorDescription: "offline", isConnectivityFailure: true),
            .retryLater(message: "offline", isConnectivityFailure: true))
        XCTAssertEqual(
            PhotoBackupOutcome.interpret(statusCode: 401, photoIdHeader: nil, bodyText: nil, errorDescription: "cancelled", isConnectivityFailure: false),
            .retryLater(message: "cancelled", isConnectivityFailure: false))
    }

    func testTheRequestIsAPostWithTheDevicePasswordAndTheFilesHeaders() throws {
        let server = try ServerAddress(parsing: "photos.example.com")
        let credential = PhotoBackupCredential(credentialId: "c1", credentialName: "Robin's iPhone photo backup", username: "robin", password: "sync-secret")
        let upload = try XCTUnwrap(PhotoBackupPlanner.plan(items: [makeItem("a", cloudIdentifier: "cloud-a")], records: [:]).first)
        let request = PhotoBackupRequest.make(server: server, credential: credential, upload: upload, usesMobileData: false)
        XCTAssertEqual(request.httpMethod, "POST")
        XCTAssertEqual(request.url?.absoluteString, "https://photos.example.com/api/v1/photos/uploads")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Basic " + Data("robin:sync-secret".utf8).base64EncodedString())
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-Photo-Client-ID"), "cloud-a")
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-Photo-Source"), "iphone")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Content-Type"), "image/heic")
        XCTAssertFalse(request.allowsCellularAccess)
        // The resumable-upload headers are URLSession's to add.
        XCTAssertNil(request.value(forHTTPHeaderField: "Upload-Complete"))
        XCTAssertNil(request.value(forHTTPHeaderField: "Upload-Draft-Interop-Version"))
        XCTAssertTrue(PhotoBackupRequest.make(server: server, credential: credential, upload: upload, usesMobileData: true).allowsCellularAccess)
    }

    func testTheBudgetKeepsFourUploadsAndTwoGigabytesButAlwaysTakesTheFirst() {
        let budget = PhotoBackupBudget.standard
        let gigabyte: Int64 = 1024 * 1024 * 1024
        XCTAssertTrue(budget.allowsAnother(outstandingCount: 0, outstandingBytes: 0))
        XCTAssertTrue(budget.allowsAnother(outstandingCount: 3, outstandingBytes: gigabyte))
        XCTAssertFalse(budget.allowsAnother(outstandingCount: 4, outstandingBytes: 10))
        XCTAssertFalse(budget.allowsAnother(outstandingCount: 1, outstandingBytes: 2 * gigabyte))
        XCTAssertTrue(budget.allowsFile(sizeBytes: 5 * gigabyte, outstandingCount: 0, outstandingBytes: 0))
        XCTAssertTrue(budget.allowsFile(sizeBytes: gigabyte, outstandingCount: 1, outstandingBytes: gigabyte))
        XCTAssertFalse(budget.allowsFile(sizeBytes: gigabyte + 1, outstandingCount: 1, outstandingBytes: gigabyte))
    }

    func testTheDevicePasswordIsKeptPerAccount() throws {
        let store = PhotoBackupCredentialStore(secrets: MemorySecrets())
        let credential = PhotoBackupCredential(credentialId: "c1", credentialName: "name", username: "robin", password: "secret")
        XCTAssertNil(try store.load(accountKey: "one|robin"))
        try store.save(credential, accountKey: "one|robin")
        XCTAssertEqual(try store.load(accountKey: "one|robin"), credential)
        XCTAssertNil(try store.load(accountKey: "two|robin"))
        try store.delete(accountKey: "one|robin")
        XCTAssertNil(try store.load(accountKey: "one|robin"))
    }

    func testThePreferencesDefaultToOffAndAreRemembered() throws {
        let suiteName = "photo-backup-tests-" + UUID().uuidString
        let defaults = try XCTUnwrap(UserDefaults(suiteName: suiteName))
        addTeardownBlock { defaults.removePersistentDomain(forName: suiteName) }
        let preferences = PhotoBackupPreferences(defaults: defaults)
        XCTAssertNil(preferences.enabledAccountKey)
        XCTAssertFalse(preferences.usesMobileData)
        XCTAssertFalse(preferences.includesHidden)
        preferences.enabledAccountKey = "one|robin"
        preferences.usesMobileData = true
        XCTAssertEqual(PhotoBackupPreferences(defaults: defaults).enabledAccountKey, "one|robin")
        XCTAssertTrue(PhotoBackupPreferences(defaults: defaults).usesMobileData)
        preferences.enabledAccountKey = nil
        XCTAssertNil(PhotoBackupPreferences(defaults: defaults).enabledAccountKey)
    }

    func testTheDocumentsAskForWhatTheDecodersRead() throws {
        XCTAssertTrue(Documents.createPhotoBackupCredential.contains("hasPhotosAccess: true"))
        XCTAssertTrue(Documents.createPhotoBackupCredential.contains("hasCalendarAccess: false"))
        let answer = #"{"CreateSyncCredential":{"credential":{"id":"c1","credentialName":"Robin's iPhone photo backup"},"username":"robin","password":"sync-x"}}"#
        struct Answer: Decodable { let CreateSyncCredential: CreatedPhotoBackupCredential }
        let created = try JSONDecoder().decode(Answer.self, from: Data(answer.utf8)).CreateSyncCredential
        XCTAssertEqual(created.credential.id, "c1")
        XCTAssertEqual(created.username, "robin")
        let found = try JSONDecoder().decode(
            [PhotoBackupServerPhoto].self, from: Data(#"[{"id":"p1","clientPhotoId":"cloud-a","photoKind":"livePhoto"}]"#.utf8))
        XCTAssertEqual(found, [PhotoBackupServerPhoto(id: "p1", clientPhotoId: "cloud-a", photoKind: "livePhoto")])
    }
}

// MARK: A pass with a library and a transfer that are not real

private final class FakeLibrary: PhotoBackupLibrary, @unchecked Sendable {
    private let lock = NSLock()
    private var allItems: [PhotoBackupItem]
    private var exportedBytes: [String: Int64] = [:]
    private var unavailableKeys: Set<String> = []
    private var failedKeys: Set<String> = []
    private var accessLevel = PhotoBackupAccess.full
    private var tokenIsExpired = false
    private var changedItems: [PhotoBackupItem] = []
    private var listingCount = 0
    private var changesCount = 0
    private var skippedAtLastListing: Set<String> = []

    init(items: [PhotoBackupItem]) { allItems = items }

    func set(access: PhotoBackupAccess) { lock.withLock { accessLevel = access } }
    func set(bytes: Int64, for key: String) { lock.withLock { exportedBytes[key] = bytes } }
    func set(unavailable key: String, _ isUnavailable: Bool) { lock.withLock { if isUnavailable { unavailableKeys.insert(key) } else { unavailableKeys.remove(key) } } }
    func set(failed key: String) { lock.withLock { _ = failedKeys.insert(key) } }
    func set(tokenIsExpired: Bool) { lock.withLock { self.tokenIsExpired = tokenIsExpired } }
    func set(changes: [PhotoBackupItem]) { lock.withLock { changedItems = changes } }
    var listings: Int { lock.withLock { listingCount } }
    var changeReads: Int { lock.withLock { changesCount } }
    var skippedAtLastListingIdentifiers: Set<String> { lock.withLock { skippedAtLastListing } }

    func access() -> PhotoBackupAccess { lock.withLock { accessLevel } }

    func listItems(skipping known: Set<String>, includesHidden: Bool) async throws -> PhotoBackupListing {
        lock.withLock {
            listingCount += 1
            skippedAtLastListing = known
            return PhotoBackupListing(items: allItems.filter { !known.contains($0.localIdentifier) }, totalCount: allItems.count, changeToken: Data("token-\(listingCount)".utf8))
        }
    }

    func removeFromLibrary(_ localIdentifier: String) { lock.withLock { allItems.removeAll { $0.localIdentifier == localIdentifier } } }

    func items(withLocalIdentifiers localIdentifiers: [String], includesHidden: Bool) async throws -> [PhotoBackupItem] {
        lock.withLock { allItems.filter { localIdentifiers.contains($0.localIdentifier) } }
    }

    func changes(since token: Data, includesHidden: Bool) async throws -> PhotoBackupChanges {
        try lock.withLock {
            changesCount += 1
            if tokenIsExpired { throw PhotoBackupLibraryError.changeTokenExpired }
            return PhotoBackupChanges(items: changedItems, totalCount: allItems.count + changedItems.count, changeToken: Data("changes-\(changesCount)".utf8))
        }
    }

    func export(_ upload: PhotoBackupUpload, to directory: URL, mayUseExpensiveNetwork: Bool) async throws -> PhotoBackupExportedFile {
        let (bytes, isUnavailable, isFailed) = lock.withLock { (exportedBytes[upload.key] ?? 10, unavailableKeys.contains(upload.key), failedKeys.contains(upload.key)) }
        if isUnavailable { throw PhotoBackupLibraryError.unavailable("in iCloud") }
        if isFailed { throw PhotoBackupLibraryError.failed("cannot be read") }
        let fileName = UUID().uuidString
        try Data(count: Int(min(bytes, 1000))).write(to: directory.appendingPathComponent(fileName))
        return PhotoBackupExportedFile(fileName: fileName, sizeBytes: bytes)
    }
}

private final class FakeTransfer: PhotoBackupTransfer, @unchecked Sendable {
    private let lock = NSLock()
    private var jobs: [PhotoBackupJob] = []
    private var requests: [URLRequest] = []
    private var heldJobs: [PhotoBackupJob] = []
    private var wasCancelled = false
    /// Called with each job as it is queued; the test finishes it or leaves it.
    var onStart: (@Sendable (PhotoBackupJob) -> Void)?

    var startedJobs: [PhotoBackupJob] { lock.withLock { jobs } }
    var startedRequests: [URLRequest] { lock.withLock { requests } }
    var isCancelled: Bool { lock.withLock { wasCancelled } }

    func start(request: URLRequest, job: PhotoBackupJob, fileURL: URL) async throws {
        XCTAssertTrue(FileManager.default.fileExists(atPath: fileURL.path), "the file is written before it is queued")
        lock.withLock {
            jobs.append(job)
            requests.append(request)
            heldJobs.append(job)
        }
        onStart?(job)
    }

    func queuedJobs() async -> [PhotoBackupJob] { lock.withLock { heldJobs } }

    func release(_ job: PhotoBackupJob) { lock.withLock { heldJobs.removeAll { $0.key == job.key } } }

    func cancelAll() async { lock.withLock { wasCancelled = true; heldJobs.removeAll() } }
}

private struct FakeNetwork: PhotoBackupNetwork {
    var isExpensiveNetwork = false
    func isExpensive() async -> Bool { isExpensiveNetwork }
}

private final class FakeLookup: PhotoBackupServerLookup, @unchecked Sendable {
    private let lock = NSLock()
    private let known: [PhotoBackupServerPhoto]
    private var batches: [[String]] = []
    var isBroken = false

    init(known: [PhotoBackupServerPhoto]) { self.known = known }

    var askedBatches: [[String]] { lock.withLock { batches } }

    func findPhotos(clientPhotoIds: [String]) async throws -> [PhotoBackupServerPhoto] {
        if isBroken { throw TeaNodeError.signedOut }
        lock.withLock { batches.append(clientPhotoIds) }
        return known.filter { clientPhotoIds.contains($0.clientPhotoId) }
    }
}

private struct Rig {
    let directory: URL
    let outgoing: URL
    let index: PhotoBackupIndex
    let library: FakeLibrary
    let transfer: FakeTransfer
    let coordinator: PhotoBackupCoordinator
}

private func makeRig(
    _ test: XCTestCase, items: [PhotoBackupItem], lookup: FakeLookup? = nil, network: FakeNetwork = FakeNetwork(), budget: PhotoBackupBudget = .standard,
    index existingIndex: PhotoBackupIndex? = nil, directory existingDirectory: URL? = nil
) throws -> Rig {
    let directory = try existingDirectory ?? temporaryDirectory(test)
    let outgoing = directory.appendingPathComponent("outgoing")
    try FileManager.default.createDirectory(at: outgoing, withIntermediateDirectories: true)
    let index = try existingIndex ?? PhotoBackupIndex(directory: directory)
    let library = FakeLibrary(items: items)
    let transfer = FakeTransfer()
    let coordinator = PhotoBackupCoordinator(
        index: index, library: library, transfer: transfer, lookup: lookup, network: network, outgoingDirectory: outgoing,
        server: try ServerAddress(parsing: "photos.example.com"), accountKey: "photos.example.com|robin",
        credential: PhotoBackupCredential(credentialId: "c1", credentialName: "backup", username: "robin", password: "sync-secret"),
        budget: budget, now: { Date(timeIntervalSince1970: 1_800_000_000) })
    return Rig(directory: directory, outgoing: outgoing, index: index, library: library, transfer: transfer, coordinator: coordinator)
}

private func waitUntil(_ description: String, timeout: Double = 5, _ condition: @Sendable () async -> Bool) async throws {
    let deadline = Date().addingTimeInterval(timeout)
    while Date() < deadline {
        if await condition() { return }
        try await Task.sleep(nanoseconds: 5_000_000)
    }
    XCTFail("timed out waiting for \(description)")
}

private let settings = PhotoBackupCoordinator.Settings(usesMobileData: false, includesHidden: false)

final class PhotoBackupCoordinatorTests: XCTestCase {
    func testEveryFileGoesUpOldestFirstAndThePassEndsWhenTheServerHasThemAll() async throws {
        let items = (0..<6).map { makeItem("item\($0)", secondsSince2026: Double(100 - $0)) }
        let rig = try makeRig(self, items: items)
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p-" + job.localIdentifier)) }
        }
        let result = await coordinator.runPass(settings: settings)
        XCTAssertEqual(result.plannedFileCount, 6)
        XCTAssertEqual(result.uploadedFileCount, 6)
        XCTAssertFalse(result.wasStopped)
        XCTAssertEqual(rig.transfer.startedJobs.map(\.localIdentifier), ["item5", "item4", "item3", "item2", "item1", "item0"])
        let summary = await rig.index.summary()
        XCTAssertEqual(summary.backedUpItemCount, 6)
        XCTAssertEqual(summary.libraryItemCount, 6)
        XCTAssertEqual(summary.lastPassFinishedAt, Date(timeIntervalSince1970: 1_800_000_000))
        let token = await rig.index.changeToken
        XCTAssertEqual(token, Data("token-1".utf8))
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: rig.outgoing.path), [], "temporary files are deleted")
        let request = try XCTUnwrap(rig.transfer.startedRequests.first)
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-Photo-Client-ID"), "local:item5")
    }

    func testNoMoreThanFourUploadsAreOutstandingAndMoreFollowAsTheyFinish() async throws {
        let rig = try makeRig(self, items: (0..<10).map { makeItem("item\($0)", secondsSince2026: Double($0)) })
        let coordinator = rig.coordinator
        let pass = Task { await coordinator.runPass(settings: settings) }
        try await waitUntil("four uploads queued") { rig.transfer.startedJobs.count == 4 }
        try await Task.sleep(nanoseconds: 100_000_000)
        XCTAssertEqual(rig.transfer.startedJobs.count, 4, "a fifth is not queued while four are outstanding")
        var finished = 0
        while finished < 10 {
            let queued = await rig.transfer.queuedJobs()
            XCTAssertLessThanOrEqual(queued.count, 4)
            guard let job = queued.first else {
                try await Task.sleep(nanoseconds: 5_000_000)
                continue
            }
            rig.transfer.release(job)
            await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p"))
            finished += 1
        }
        let result = await pass.value
        XCTAssertEqual(result.uploadedFileCount, 10)
        XCTAssertEqual(rig.transfer.startedJobs.count, 10)
    }

    func testFilesAreHeldBackWhenTheTemporaryFilesWouldPassTheBudget() async throws {
        let gigabyte: Int64 = 1024 * 1024 * 1024
        let items = (0..<3).map { makeItem("item\($0)", secondsSince2026: Double($0)) }
        let rig = try makeRig(self, items: items)
        for key in ["item0|original", "item1|original", "item2|original"] { rig.library.set(bytes: gigabyte + 1, for: key) }
        let coordinator = rig.coordinator
        let pass = Task { await coordinator.runPass(settings: settings) }
        try await waitUntil("the first upload queued") { rig.transfer.startedJobs.count == 1 }
        try await Task.sleep(nanoseconds: 150_000_000)
        XCTAssertEqual(rig.transfer.startedJobs.count, 1, "a second file would take the temporary files past two gigabytes")
        var mostQueued = 0
        var finished = 0
        while finished < 3 {
            let queued = await rig.transfer.queuedJobs()
            mostQueued = max(mostQueued, queued.count)
            guard let job = queued.first else {
                try await Task.sleep(nanoseconds: 5_000_000)
                continue
            }
            rig.transfer.release(job)
            await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p"))
            finished += 1
        }
        let result = await pass.value
        XCTAssertEqual(result.uploadedFileCount, 3)
        XCTAssertEqual(mostQueued, 1)
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: rig.outgoing.path), [], "a file exported and then held back is deleted")
    }

    func testPhotosTheServerHasAreNotSent() async throws {
        let items = [makeItem("a", cloudIdentifier: "cloud-a"), makeItem("b", secondsSince2026: 1)]
        let lookup = FakeLookup(known: [PhotoBackupServerPhoto(id: "p1", clientPhotoId: "cloud-a")])
        let rig = try makeRig(self, items: items, lookup: lookup)
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p2")) }
        }
        let result = await coordinator.runPass(settings: settings)
        XCTAssertEqual(rig.transfer.startedJobs.map(\.localIdentifier), ["b"])
        XCTAssertEqual(result.plannedFileCount, 1)
        XCTAssertEqual(lookup.askedBatches, [["cloud-a", "local:b"]])
        let summary = await rig.index.summary()
        XCTAssertEqual(summary.backedUpItemCount, 2)
    }

    func testTheServerIsAskedAboutAtMostFiveHundredNamesAtATime() async throws {
        let items = (0..<1200).map { makeItem("item\($0)", secondsSince2026: Double($0), roles: []) }
        let lookup = FakeLookup(known: [])
        let rig = try makeRig(self, items: items, lookup: lookup)
        _ = await rig.coordinator.runPass(settings: settings)
        XCTAssertEqual(lookup.askedBatches.map(\.count), [500, 500, 200])
    }

    func testABrokenLookupDoesNotStopTheUploads() async throws {
        let lookup = FakeLookup(known: [])
        lookup.isBroken = true
        let rig = try makeRig(self, items: [makeItem("a")], lookup: lookup)
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p1")) }
        }
        let result = await coordinator.runPass(settings: settings)
        XCTAssertEqual(result.uploadedFileCount, 1)
    }

    func testAfterTheFirstPassOnlyTheChangesAreRead() async throws {
        let rig = try makeRig(self, items: [makeItem("a", secondsSince2026: 1)])
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p-" + job.localIdentifier)) }
        }
        _ = await coordinator.runPass(settings: settings)
        XCTAssertEqual(rig.library.listings, 1)
        rig.library.set(changes: [makeItem("new", secondsSince2026: 50)])
        let second = await coordinator.runPass(settings: settings)
        XCTAssertEqual(rig.library.listings, 1, "the library is not listed again")
        XCTAssertEqual(rig.library.changeReads, 1)
        XCTAssertEqual(second.uploadedFileCount, 1)
        XCTAssertEqual(rig.transfer.startedJobs.map(\.localIdentifier), ["a", "new"])
        let token = await rig.index.changeToken
        XCTAssertEqual(token, Data("changes-1".utf8))
    }

    func testAnExpiredTokenListsEverythingAgainSkippingWhatTheIndexHas() async throws {
        let rig = try makeRig(self, items: [makeItem("a", secondsSince2026: 1), makeItem("b", secondsSince2026: 2)])
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p")) }
        }
        _ = await coordinator.runPass(settings: settings)
        rig.library.set(tokenIsExpired: true)
        let second = await coordinator.runPass(settings: settings)
        XCTAssertEqual(rig.library.listings, 2)
        XCTAssertEqual(rig.library.skippedAtLastListingIdentifiers, ["a", "b"])
        XCTAssertEqual(second.plannedFileCount, 0)
        XCTAssertEqual(rig.transfer.startedJobs.count, 2, "nothing is sent twice")
    }

    func testAFileThatWaitsForICloudIsSentByALaterPassAfterTheTokenMovedOn() async throws {
        let rig = try makeRig(self, items: [makeItem("a", secondsSince2026: 1), makeItem("b", secondsSince2026: 2)])
        rig.library.set(unavailable: "a|original", true)
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p")) }
        }
        let first = await coordinator.runPass(settings: settings)
        XCTAssertEqual(first.deferredFileCount, 1)
        XCTAssertEqual(first.uploadedFileCount, 1)
        let pending = await rig.index.pendingLocalIdentifiers()
        XCTAssertEqual(pending, ["a"])
        var token = await rig.index.changeToken
        XCTAssertNotNil(token, "the file is in the index as still to send, so the token moves on")
        var status = await coordinator.status()
        XCTAssertEqual(status.phase, .waitingForNetwork)

        rig.library.set(unavailable: "a|original", false)
        let second = await coordinator.runPass(settings: settings)
        XCTAssertEqual(rig.library.changeReads, 1, "the second pass reads changes, which are none")
        XCTAssertEqual(second.uploadedFileCount, 1)
        XCTAssertEqual(second.deferredFileCount, 0)
        token = await rig.index.changeToken
        XCTAssertEqual(token, Data("changes-1".utf8))
        status = await coordinator.status()
        XCTAssertEqual(status.phase, .idle)
        XCTAssertEqual(status.backedUpItemCount, 2)
    }

    func testAPhotoDeletedOnThePhoneBeforeItWentUpIsForgottenAndOneAlreadyUpIsKept() async throws {
        let rig = try makeRig(self, items: [makeItem("gone", secondsSince2026: 1), makeItem("kept", secondsSince2026: 2, roles: [.original, .liveVideo])])
        rig.library.set(unavailable: "gone|original", true)
        rig.library.set(unavailable: "kept|liveVideo", true)
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p")) }
        }
        _ = await coordinator.runPass(settings: settings)
        rig.library.removeFromLibrary("gone")
        rig.library.removeFromLibrary("kept")
        let next = await coordinator.runPass(settings: settings)
        XCTAssertEqual(next.plannedFileCount, 0)
        let pending = await rig.index.pendingLocalIdentifiers()
        XCTAssertEqual(pending, [])
        let gone = await rig.index.record(forLocalIdentifier: "gone")
        let kept = await rig.index.record(forLocalIdentifier: "kept")
        XCTAssertNil(gone)
        XCTAssertEqual(kept?.isOriginalOnServer, true, "deleting on the phone leaves the server's copy alone")
    }

    func testAFileOutsideTheDeviceWaitsOnANetworkThatCostsUnlessAllowed() async throws {
        let cloudOnly = PhotoBackupItem(
            localIdentifier: "cloud", creationDate: Date(timeIntervalSince1970: 1), timeZone: eastern, mediaKind: .picture,
            resources: [PhotoBackupResource(role: .original, originalFilename: "IMG.HEIC", contentType: "image/heic", isLocallyAvailable: false)])
        let rig = try makeRig(self, items: [cloudOnly, makeItem("here", secondsSince2026: 5)], network: FakeNetwork(isExpensiveNetwork: true))
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p")) }
        }
        let held = await coordinator.runPass(settings: settings)
        XCTAssertEqual(held.deferredFileCount, 1)
        XCTAssertEqual(rig.transfer.startedJobs.map(\.localIdentifier), ["here"])
        let allowed = await coordinator.runPass(settings: PhotoBackupCoordinator.Settings(usesMobileData: true, includesHidden: false))
        XCTAssertEqual(allowed.deferredFileCount, 0)
        XCTAssertEqual(rig.transfer.startedJobs.map(\.localIdentifier), ["here", "cloud"])
    }

    func testARefusedFileIsRememberedAndTheOthersGoOn() async throws {
        let rig = try makeRig(self, items: [makeItem("a", secondsSince2026: 1), makeItem("b", secondsSince2026: 2)])
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            let outcome: PhotoBackupOutcome = job.localIdentifier == "a" ? .refused(message: "not a kind of file the library keeps") : .uploaded(serverPhotoId: "p")
            Task { await coordinator.finished(job, outcome: outcome) }
        }
        let result = await coordinator.runPass(settings: settings)
        XCTAssertEqual(result.refusedFileCount, 1)
        XCTAssertEqual(result.uploadedFileCount, 1)
        let record = await rig.index.record(forLocalIdentifier: "a")
        XCTAssertEqual(record?.roles["original"]?.refusalCount, 1)
        let summary = await rig.index.summary()
        XCTAssertEqual(summary.lastErrorMessage, "not a kind of file the library keeps")
    }

    func testAFileThatCannotBeReadIsCountedAsRefusedNotRetriedForEver() async throws {
        let rig = try makeRig(self, items: [makeItem("a")])
        rig.library.set(failed: "a|original")
        let result = await rig.coordinator.runPass(settings: settings)
        XCTAssertEqual(result.refusedFileCount, 1)
        XCTAssertTrue(rig.transfer.startedJobs.isEmpty)
        let record = await rig.index.record(forLocalIdentifier: "a")
        XCTAssertEqual(record?.roles["original"]?.refusalCount, 1)
    }

    func testARefusedSignInStopsEverythingAndIsShown() async throws {
        let rig = try makeRig(self, items: (0..<8).map { makeItem("item\($0)", secondsSince2026: Double($0)) })
        let coordinator = rig.coordinator
        let pass = Task { await coordinator.runPass(settings: settings) }
        try await waitUntil("four uploads queued") { rig.transfer.startedJobs.count == 4 }
        await coordinator.finished(rig.transfer.startedJobs[0], outcome: .signInNeeded)
        let result = await pass.value
        XCTAssertTrue(result.wasStopped)
        XCTAssertTrue(rig.transfer.isCancelled)
        XCTAssertEqual(rig.transfer.startedJobs.count, 4, "nothing more is queued")
        let status = await coordinator.status()
        XCTAssertEqual(status.phase, .needsSignIn)
        let summary = await rig.index.summary()
        XCTAssertTrue(summary.isSignInNeeded)
        let pending = await rig.index.pendingLocalIdentifiers()
        XCTAssertEqual(pending.count, 8, "everything planned stays to be sent once the person signs in again")

        // Until the person signs in again, a pass does not start sending.
        let next = await coordinator.runPass(settings: settings)
        XCTAssertEqual(next.plannedFileCount, 0)
        XCTAssertEqual(rig.transfer.startedJobs.count, 4)
        await rig.index.clearError()
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p")) }
        }
        let resumed = await coordinator.runPass(settings: settings)
        XCTAssertEqual(resumed.uploadedFileCount, 8)
    }

    func testAFailedUploadIsKeptForTheNextPass() async throws {
        let rig = try makeRig(self, items: [makeItem("a")])
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .retryLater(message: "The network connection was lost.", isConnectivityFailure: true)) }
        }
        let first = await coordinator.runPass(settings: settings)
        XCTAssertEqual(first.uploadedFileCount, 0)
        let summary = await rig.index.summary()
        XCTAssertEqual(summary.backedUpItemCount, 0)
        XCTAssertNil(summary.lastErrorMessage, "a lost connection is not shown as an error")
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p")) }
        }
        let second = await coordinator.runPass(settings: settings)
        XCTAssertEqual(second.uploadedFileCount, 1)
    }

    func testUploadsAlreadyInTheSessionAreNotQueuedAgain() async throws {
        let rig = try makeRig(self, items: [makeItem("a", secondsSince2026: 1), makeItem("b", secondsSince2026: 2)])
        let existing = PhotoBackupJob(
            accountKey: "photos.example.com|robin", localIdentifier: "a", role: .original, clientPhotoId: "local:a", filename: "a.heic",
            assetModificationDate: nil, fileName: "left-over", sizeBytes: 10)
        try await rig.transfer.start(request: URLRequest(url: URL(string: "https://photos.example.com")!), job: existing, fileURL: {
            let file = rig.outgoing.appendingPathComponent("left-over")
            try? Data(count: 1).write(to: file)
            return file
        }())
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p")) }
        }
        let pass = Task { await coordinator.runPass(settings: settings) }
        try await waitUntil("b queued") { rig.transfer.startedJobs.count == 2 }
        rig.transfer.release(existing)
        await coordinator.finished(existing, outcome: .uploaded(serverPhotoId: "p0"))
        let result = await pass.value
        XCTAssertEqual(rig.transfer.startedJobs.map(\.localIdentifier), ["a", "b"], "a was not queued a second time")
        XCTAssertEqual(result.plannedFileCount, 1)
    }

    func testWithoutFullAccessNothingIsRead() async throws {
        let rig = try makeRig(self, items: [makeItem("a")])
        rig.library.set(access: .limited)
        let result = await rig.coordinator.runPass(settings: settings)
        XCTAssertEqual(result.plannedFileCount, 0)
        XCTAssertEqual(rig.library.listings, 0)
        let status = await rig.coordinator.status()
        XCTAssertEqual(status.phase, .needsAccess(.limited))
    }

    func testStoppingEndsAPassWithoutQueueingMore() async throws {
        let rig = try makeRig(self, items: (0..<10).map { makeItem("item\($0)", secondsSince2026: Double($0)) })
        let coordinator = rig.coordinator
        let pass = Task { await coordinator.runPass(settings: settings) }
        try await waitUntil("four uploads queued") { rig.transfer.startedJobs.count == 4 }
        await coordinator.stop()
        let result = await pass.value
        XCTAssertTrue(result.wasStopped)
        XCTAssertEqual(rig.transfer.startedJobs.count, 4)
        let pending = await rig.index.pendingLocalIdentifiers()
        XCTAssertEqual(pending.count, 10, "what a stopped pass did not send is left for the next one")
        let isRunning = await coordinator.isPassRunning
        XCTAssertFalse(isRunning)
    }

    func testAnEditedPhotoIsSentAgainAfterTheEdit() async throws {
        let rig = try makeRig(self, items: [makeItem("a", secondsSince2026: 1, roles: [.original, .edited])])
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p")) }
        }
        let first = await coordinator.runPass(settings: settings)
        XCTAssertEqual(first.uploadedFileCount, 2)
        rig.library.set(changes: [makeItem("a", secondsSince2026: 1, modifiedSecondsSince2026: 90_000, roles: [.original, .edited])])
        let second = await coordinator.runPass(settings: settings)
        XCTAssertEqual(second.uploadedFileCount, 1)
        XCTAssertEqual(rig.transfer.startedJobs.map(\.key), ["a|original", "a|edited", "a|edited"])
    }

    func testAJobFinishedWhileNoPassRunsIsStillRecorded() async throws {
        let rig = try makeRig(self, items: [])
        let file = rig.outgoing.appendingPathComponent("temporary")
        try Data(count: 3).write(to: file)
        let job = PhotoBackupJob(
            accountKey: "photos.example.com|robin", localIdentifier: "a", role: .original, clientPhotoId: "cloud-a", filename: "a.heic",
            assetModificationDate: nil, fileName: "temporary", sizeBytes: 3)
        await rig.coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p1"))
        let record = await rig.index.record(forLocalIdentifier: "a")
        XCTAssertEqual(record?.serverPhotoId, "p1")
        XCTAssertFalse(FileManager.default.fileExists(atPath: file.path))
    }

    func testTheJobSurvivesBeingWrittenIntoATaskDescription() throws {
        let job = PhotoBackupJob(
            accountKey: "photos.example.com|robin", localIdentifier: "A1B2/L0/001", role: .liveVideo, clientPhotoId: "local:A1B2/L0/001",
            filename: "IMG_0001.MOV", assetModificationDate: Date(timeIntervalSince1970: 1_767_225_600), fileName: "5E2F", sizeBytes: 1234)
        let description = String(decoding: try JSONEncoder().encode(job), as: UTF8.self)
        XCTAssertEqual(try JSONDecoder().decode(PhotoBackupJob.self, from: Data(description.utf8)), job)
        XCTAssertEqual(job.key, "A1B2/L0/001|liveVideo")
    }

    func testTheStatusReportsProgressToItsHandler() async throws {
        let rig = try makeRig(self, items: [makeItem("a", secondsSince2026: 1), makeItem("b", secondsSince2026: 2)])
        let coordinator = rig.coordinator
        let seen = StatusLog()
        await coordinator.setStatusHandler { seen.add($0) }
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p")) }
        }
        _ = await coordinator.runPass(settings: settings)
        let statuses = seen.all
        XCTAssertTrue(statuses.contains { $0.phase == .listing })
        XCTAssertTrue(statuses.contains { $0.phase == .uploading && $0.plannedFileCount == 2 })
        XCTAssertEqual(statuses.last?.phase, .idle)
        XCTAssertEqual(statuses.last?.backedUpItemCount, 2)
        XCTAssertEqual(statuses.last?.finishedFileCount, 2)
        XCTAssertEqual(statuses.last?.fractionBackedUp, 1)
    }
}

private final class StatusLog: @unchecked Sendable {
    private let lock = NSLock()
    private var statuses: [PhotoBackupStatus] = []
    func add(_ status: PhotoBackupStatus) { lock.withLock { statuses.append(status) } }
    var all: [PhotoBackupStatus] { lock.withLock { statuses } }
}
