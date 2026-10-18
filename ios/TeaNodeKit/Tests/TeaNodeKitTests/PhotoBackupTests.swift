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

/// ManualClock is a clock the test moves by hand.
private final class ManualClock: @unchecked Sendable {
    private let lock = NSLock()
    private var current = Date(timeIntervalSince1970: 1_000_000)

    var now: Date { lock.withLock { current } }

    func advance(seconds: TimeInterval) { lock.withLock { current = current.addingTimeInterval(seconds) } }
}

private func makeItem(
    _ localIdentifier: String, cloudIdentifier: String? = nil, secondsSince2026 seconds: Double? = 0, modifiedSecondsSince2026: Double? = nil,
    kind: PhotoBackupMediaKind = .picture, location: PhotoBackupLocation? = nil, roles: [PhotoBackupFileRole] = [.original],
    sizeBytes: Int64? = nil, isLocallyAvailable: Bool = true
) -> PhotoBackupItem {
    let start = Date(timeIntervalSince1970: 1_767_225_600)  // 2026-01-01T00:00:00Z
    return PhotoBackupItem(
        localIdentifier: localIdentifier, cloudIdentifier: cloudIdentifier, creationDate: seconds.map { start.addingTimeInterval($0) },
        modificationDate: (modifiedSecondsSince2026 ?? seconds).map { start.addingTimeInterval($0) }, location: location,
        isFavorite: false, isScreenshot: false, mediaKind: kind,
        resources: roles.map {
            PhotoBackupResource(
                role: $0, originalFilename: "\(localIdentifier)-\($0.rawValue).heic", contentType: $0 == .liveVideo ? "video/quicktime" : "image/heic",
                isLocallyAvailable: isLocallyAvailable, fileSizeBytes: sizeBytes)
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
            modificationDate: nil, location: PhotoBackupLocation(latitude: 12.3456789, longitude: -98.7654321),
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
                "X-Photo-Taken-At": "2026-01-01T18:42:00Z",
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

    func testRFC3339IsUTCWithAZAndNoOffset() {
        XCTAssertEqual(PhotoBackupPlanner.rfc3339(Date(timeIntervalSince1970: 1_767_225_600)), "2026-01-01T00:00:00Z")
        XCTAssertEqual(PhotoBackupPlanner.rfc3339(Date(timeIntervalSince1970: 1_752_308_100)), "2025-07-12T08:15:00Z")
        XCTAssertEqual(PhotoBackupPlanner.rfc3339(Date(timeIntervalSince1970: 1_767_225_599)), "2025-12-31T23:59:59Z")
    }

    func testTheTakenAtHeaderDoesNotDependOnThePhonesZone() throws {
        let item = makeItem("L1", secondsSince2026: 3600 * 18 + 42 * 60)
        let original = NSTimeZone.default
        defer { NSTimeZone.default = original }
        for identifier in ["Asia/Tokyo", "America/New_York"] {
            NSTimeZone.default = TimeZone(identifier: identifier)!
            let upload = try XCTUnwrap(PhotoBackupPlanner.plan(items: [item], records: [:]).first)
            XCTAssertEqual(upload.headers["X-Photo-Taken-At"], "2026-01-01T18:42:00Z")
        }
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

    func testTheSizeTheLibraryReportsIsCarriedToTheUpload() throws {
        let upload = try XCTUnwrap(PhotoBackupPlanner.plan(items: [makeItem("a", sizeBytes: 4_000_000_000)], records: [:]).first)
        XCTAssertEqual(upload.fileSizeBytes, 4_000_000_000)
        let unknown = try XCTUnwrap(PhotoBackupPlanner.plan(items: [makeItem("b")], records: [:]).first)
        XCTAssertNil(unknown.fileSizeBytes)
    }

    func testAnImpossibleCoordinateIsNotSent() throws {
        let item = makeItem("L1", location: PhotoBackupLocation(latitude: 95, longitude: 10))
        let upload = try XCTUnwrap(PhotoBackupPlanner.plan(items: [item], records: [:]).first)
        XCTAssertNil(upload.headers["X-Photo-Latitude"])
        XCTAssertNil(upload.headers["X-Photo-Longitude"])
    }

    func testUploadsComeNewestFirstAndAPhotosOwnFilesOriginalFirst() {
        let items = [
            makeItem("newest", secondsSince2026: 300),
            makeItem("oldest", secondsSince2026: 100, kind: .livePhoto, roles: [.edited, .liveVideo, .original]),
            makeItem("middle", secondsSince2026: 200),
            makeItem("undated", secondsSince2026: nil),
        ]
        XCTAssertEqual(
            PhotoBackupPlanner.plan(items: items, records: [:]).map(\.key),
            ["newest|original", "middle|original", "oldest|original", "oldest|liveVideo", "oldest|edited", "undated|original"])
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
        record.roles["original"] = PhotoBackupRoleRecord(refusalCount: PhotoBackupPlanner.maximumRefusalCount - 1, refusalFailure: .fileRefused(serverReason: "too large"))
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
        await index.markUploaded(localIdentifier: "a", clientPhotoId: "cloud-a", role: .original, serverPhotoId: "p1", assetModificationDate: Date(timeIntervalSince1970: 90))
        await index.markUploaded(
            localIdentifier: "a", clientPhotoId: "cloud-a", role: .edited, serverPhotoId: "p1", assetModificationDate: Date(timeIntervalSince1970: 100))
        await index.markUploaded(localIdentifier: "a", clientPhotoId: "cloud-a", role: .liveVideo, serverPhotoId: "p1", assetModificationDate: nil)
        await index.setChangeToken(Data([1, 2, 3]))
        await index.setLibraryItemCount(40)
        await index.setError(failure: .systemDescription("The disk is full."))
        await index.finishPass(at: Date(timeIntervalSince1970: 500))
        await index.flush()

        let reopened = try PhotoBackupIndex(directory: directory)
        let wasStartedOver = await reopened.wasStartedOver
        XCTAssertFalse(wasStartedOver)
        let record = await reopened.record(forLocalIdentifier: "a")
        XCTAssertEqual(record?.clientPhotoId, "cloud-a")
        XCTAssertEqual(record?.roles["edited"]?.serverPhotoId, "p1")
        XCTAssertEqual(record?.roles["edited"]?.assetModificationDate, Date(timeIntervalSince1970: 100))
        XCTAssertEqual(record?.roles["liveVideo"]?.isOnServer, true)
        let accountKey = await reopened.accountKey
        let token = await reopened.changeToken
        XCTAssertEqual(accountKey, "photos.example.com|robin")
        XCTAssertEqual(token, Data([1, 2, 3]))
        let summary = await reopened.summary()
        XCTAssertEqual(summary.backedUpItemCount, 1)
        XCTAssertEqual(summary.libraryItemCount, 40)
        XCTAssertEqual(summary.lastFailure, .systemDescription("The disk is full."))
        XCTAssertEqual(summary.lastPassFinishedAt, Date(timeIntervalSince1970: 500))
    }

    func testAnUnreadableFileIsStartedOverKeepingNothing() async throws {
        let directory = try temporaryDirectory(self)
        try Data("{ this is not an index".utf8).write(to: directory.appendingPathComponent(PhotoBackupIndex.indexFilename))
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
        try Data(#"{"records": [1, 2, 3]}"#.utf8).write(to: directory.appendingPathComponent(PhotoBackupIndex.indexFilename))
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

    func testChangesAreWrittenByTimeAndFlushWritesTheRest() async throws {
        let directory = try temporaryDirectory(self)
        let clock = ManualClock()
        let index = try PhotoBackupIndex(directory: directory, now: { clock.now })
        await index.markUploaded(localIdentifier: "a", clientPhotoId: "local:a", role: .original, serverPhotoId: "p1", assetModificationDate: nil)
        let file = directory.appendingPathComponent(PhotoBackupIndex.indexFilename)
        XCTAssertFalse(FileManager.default.fileExists(atPath: file.path), "a change is not written at once")
        for number in 0..<50 {
            await index.markUploaded(localIdentifier: "x\(number)", clientPhotoId: "local:x\(number)", role: .original, serverPhotoId: "p", assetModificationDate: nil)
        }
        XCTAssertFalse(FileManager.default.fileExists(atPath: file.path), "the number of changes does not decide when the file is written")
        clock.advance(seconds: PhotoBackupIndex.secondsBetweenWrites - 1)
        await index.markUploaded(localIdentifier: "early", clientPhotoId: "local:early", role: .original, serverPhotoId: "p", assetModificationDate: nil)
        XCTAssertFalse(FileManager.default.fileExists(atPath: file.path))
        clock.advance(seconds: 1)
        await index.markUploaded(localIdentifier: "due", clientPhotoId: "local:due", role: .original, serverPhotoId: "p", assetModificationDate: nil)
        XCTAssertTrue(FileManager.default.fileExists(atPath: file.path), "thirty seconds after the last write the next change is written")
        await index.markUploaded(localIdentifier: "last", clientPhotoId: "local:last", role: .original, serverPhotoId: "p", assetModificationDate: nil)
        let beforeFlush = try PhotoBackupIndex(directory: directory)
        let knownBeforeFlush = await beforeFlush.knownLocalIdentifiers()
        XCTAssertFalse(knownBeforeFlush.contains("last"))
        await index.flush()
        let afterFlush = try PhotoBackupIndex(directory: directory)
        let knownAfterFlush = await afterFlush.knownLocalIdentifiers()
        XCTAssertTrue(knownAfterFlush.contains("last"))
        XCTAssertEqual(knownAfterFlush.count, 54)
    }

    func testACrashBetweenWritesLosesOnlyTheChangesNotYetWritten() async throws {
        let directory = try temporaryDirectory(self)
        let clock = ManualClock()
        let index = try PhotoBackupIndex(directory: directory, now: { clock.now })
        await index.adopt(accountKey: "photos.example.com|robin")
        for name in ["a", "b", "c"] {
            await index.markUploaded(localIdentifier: name, clientPhotoId: "local:" + name, role: .original, serverPhotoId: "p-" + name, assetModificationDate: nil)
        }
        // Thirty seconds on, the next change writes the file with a, b, c and d.
        clock.advance(seconds: PhotoBackupIndex.secondsBetweenWrites)
        await index.markUploaded(localIdentifier: "d", clientPhotoId: "local:d", role: .original, serverPhotoId: "p-d", assetModificationDate: nil)
        // These two are held in memory only when the app is killed.
        await index.markUploaded(localIdentifier: "e", clientPhotoId: "local:e", role: .original, serverPhotoId: "p-e", assetModificationDate: nil)
        await index.markUploaded(localIdentifier: "f", clientPhotoId: "local:f", role: .original, serverPhotoId: "p-f", assetModificationDate: nil)

        let afterCrash = try PhotoBackupIndex(directory: directory)
        let wasStartedOver = await afterCrash.wasStartedOver
        let known = await afterCrash.knownLocalIdentifiers()
        let accountKey = await afterCrash.accountKey
        XCTAssertFalse(wasStartedOver)
        XCTAssertEqual(known, ["a", "b", "c", "d"], "everything written before the crash is there, the two unwritten changes are not")
        XCTAssertEqual(accountKey, "photos.example.com|robin")
        let kept = await afterCrash.record(forLocalIdentifier: "a")
        XCTAssertEqual(kept?.roles["original"]?.serverPhotoId, "p-a")
    }

    func testTheFileIsCompact() async throws {
        let directory = try temporaryDirectory(self)
        let index = try PhotoBackupIndex(directory: directory)
        await index.adopt(accountKey: "photos.example.com|robin")
        await index.markUploaded(
            localIdentifier: "8F1E2C3A-0000-4000-8000-000000000001/L0/001", clientPhotoId: "local:8F1E2C3A-0000-4000-8000-000000000001/L0/001",
            role: .original, serverPhotoId: "p1", assetModificationDate: Date(timeIntervalSince1970: 100))
        await index.flush()
        let text = try String(contentsOf: directory.appendingPathComponent(PhotoBackupIndex.indexFilename), encoding: .utf8)
        XCTAssertFalse(text.contains("\n"))
        XCTAssertFalse(text.contains(" "), "no spaces between the parts")
        XCTAssertFalse(text.contains("\\/"), "slashes in identifiers are not escaped")
        XCTAssertFalse(text.contains("T00:01:40Z"), "an original's modification date is not kept: only an edit is compared with it")
    }

    func testForgettingAnAccountRemovesItsFileAndLeavesAnotherAccountAlone() async throws {
        let directory = try temporaryDirectory(self)
        let index = try PhotoBackupIndex(directory: directory)
        await index.adopt(accountKey: "one|robin")
        await index.markUploaded(localIdentifier: "a", clientPhotoId: "local:a", role: .original, serverPhotoId: "p1", assetModificationDate: nil)
        await index.flush()
        let file = directory.appendingPathComponent(PhotoBackupIndex.indexFilename)
        await index.forget(accountKey: "two|robin")
        XCTAssertTrue(FileManager.default.fileExists(atPath: file.path))
        let kept = await index.record(forLocalIdentifier: "a")
        XCTAssertNotNil(kept)
        await index.forget(accountKey: "one|robin")
        XCTAssertFalse(FileManager.default.fileExists(atPath: file.path))
        let gone = await index.record(forLocalIdentifier: "a")
        let accountKey = await index.accountKey
        XCTAssertNil(gone)
        XCTAssertNil(accountKey)
        let reopened = try PhotoBackupIndex(directory: directory)
        let known = await reopened.knownLocalIdentifiers()
        XCTAssertTrue(known.isEmpty)
    }

    func testPhotosTheServerHasAreMarkedDoneForTheirOriginalWhenItSaysNothingMore() async throws {
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

    func testEveryRoleTheServerHoldsIsMarkedDoneSoAReinstallSendsNoEditOrLiveVideoAgain() async throws {
        let index = try PhotoBackupIndex(directory: try temporaryDirectory(self))
        let items = [
            makeItem("a", cloudIdentifier: "cloud-a", roles: [.original, .edited, .liveVideo]),
            makeItem("b", cloudIdentifier: "cloud-b", roles: [.original, .edited, .liveVideo]),
        ]
        await index.markServerHas(
            [
                PhotoBackupServerPhoto(id: "p1", clientPhotoId: "cloud-a", photoKind: "livePhoto", photoFileRoles: ["original", "edited", "liveVideo", "somethingNew"]),
                PhotoBackupServerPhoto(id: "p2", clientPhotoId: "cloud-b", photoKind: "livePhoto", photoFileRoles: ["original", "liveVideo"]),
            ], for: items)
        let plan = await index.plan(items: items)
        // Only the edit the server does not hold goes up, and a role this app
        // does not know is ignored.
        XCTAssertEqual(plan.map(\.key), ["b|edited"])
        let record = await index.record(forLocalIdentifier: "a")
        XCTAssertEqual(record?.roles.keys.sorted(), ["edited", "liveVideo", "original"])
        XCTAssertTrue(record?.roles.values.allSatisfy { $0.isOnServer && $0.serverPhotoId == "p1" } == true)
    }

    func testRefusalsCountAndAreClearedByBackUpNow() async throws {
        let index = try PhotoBackupIndex(directory: try temporaryDirectory(self))
        let items = [makeItem("a")]
        for _ in 0..<PhotoBackupPlanner.maximumRefusalCount {
            await index.markRefused(localIdentifier: "a", clientPhotoId: "local:a", role: .original, failure: .fileRefused(serverReason: "not a picture"))
        }
        var plan = await index.plan(items: items)
        var summary = await index.summary()
        XCTAssertTrue(plan.isEmpty)
        XCTAssertEqual(summary.refusedFileCount, 1)
        var pending = await index.pendingLocalIdentifiers()
        XCTAssertEqual(pending, [])
        await index.clearRefusals()
        plan = await index.plan(items: items)
        summary = await index.summary()
        pending = await index.pendingLocalIdentifiers()
        XCTAssertEqual(plan.map(\.key), ["a|original"])
        XCTAssertEqual(pending, ["a"], "a cleared file is still to send, so a pass reads the item again")
        XCTAssertEqual(summary.refusedFileCount, 0)
        let record = await index.record(forLocalIdentifier: "a")
        XCTAssertEqual(record?.roles["original"], PhotoBackupRoleRecord())
    }

    func testRefusalsOfAFileOnTheServerAreResetToo() async throws {
        let index = try PhotoBackupIndex(directory: try temporaryDirectory(self))
        await index.markUploaded(localIdentifier: "a", clientPhotoId: "local:a", role: .original, serverPhotoId: "p1", assetModificationDate: nil)
        for _ in 0..<PhotoBackupPlanner.maximumRefusalCount {
            await index.markRefused(localIdentifier: "a", clientPhotoId: "local:a", role: .edited, failure: .fileRefused(serverReason: nil))
        }
        await index.clearRefusals()
        let record = await index.record(forLocalIdentifier: "a")
        XCTAssertEqual(record?.roles["original"]?.isOnServer, true)
        XCTAssertEqual(record?.roles["edited"]?.refusalCount, 0)
        let plan = await index.plan(items: [makeItem("a", roles: [.original, .edited])])
        XCTAssertEqual(plan.map(\.key), ["a|edited"])
    }

    func testTheSummaryCountsFollowEveryWriteAndMatchAFreshCount() async throws {
        let directory = try temporaryDirectory(self)
        let index = try PhotoBackupIndex(directory: directory)
        await index.adopt(accountKey: "photos.example.com|robin")
        for number in 0..<20 {
            await index.markUploaded(localIdentifier: "up\(number)", clientPhotoId: "local:up\(number)", role: .original, serverPhotoId: "p", assetModificationDate: nil)
        }
        // An edit going up does not count a second item; an item known only by its edit does not count at all.
        await index.markUploaded(localIdentifier: "up0", clientPhotoId: "local:up0", role: .edited, serverPhotoId: "p", assetModificationDate: nil)
        await index.markUploaded(localIdentifier: "editOnly", clientPhotoId: "local:editOnly", role: .edited, serverPhotoId: "p", assetModificationDate: nil)
        for _ in 0..<PhotoBackupPlanner.maximumRefusalCount {
            await index.markRefused(localIdentifier: "refused", clientPhotoId: "local:refused", role: .original, failure: .fileRefused(serverReason: nil))
            await index.markRefused(localIdentifier: "up1", clientPhotoId: "local:up1", role: .liveVideo, failure: .fileRefused(serverReason: nil))
        }
        await index.markPending([
            PhotoBackupUpload(
                localIdentifier: "up2", role: .edited, clientPhotoId: "local:up2", filename: "x", contentType: "image/heic", isLocallyAvailable: true,
                fileSizeBytes: nil, assetModificationDate: nil, headers: [:])
        ])
        var summary = await index.summary()
        XCTAssertEqual(summary.backedUpItemCount, 20)
        XCTAssertEqual(summary.refusedFileCount, 2)
        await index.forgetPending(of: ["refused", "up2"])
        summary = await index.summary()
        XCTAssertEqual(summary.backedUpItemCount, 20)
        XCTAssertEqual(summary.refusedFileCount, 1, "a refused file of an item that left the library is forgotten with it")
        await index.clearRefusals()
        summary = await index.summary()
        XCTAssertEqual(summary.refusedFileCount, 0)
        await index.markServerHas([PhotoBackupServerPhoto(id: "s", clientPhotoId: "local:found")], for: [makeItem("found")])
        summary = await index.summary()
        XCTAssertEqual(summary.backedUpItemCount, 21)

        await index.flush()
        let reopened = try PhotoBackupIndex(directory: directory)
        let counted = await reopened.summary()
        XCTAssertEqual(counted.backedUpItemCount, summary.backedUpItemCount, "the running count is what a count of every record says")
        XCTAssertEqual(counted.refusedFileCount, summary.refusedFileCount)

        await index.forget(accountKey: "photos.example.com|robin")
        summary = await index.summary()
        XCTAssertEqual(summary.backedUpItemCount, 0)
        XCTAssertEqual(summary.refusedFileCount, 0)
    }

    func testASignInRefusalIsRememberedUntilCleared() async throws {
        let index = try PhotoBackupIndex(directory: try temporaryDirectory(self))
        await index.setError(failure: .signInRefused, isSignInNeeded: true)
        var summary = await index.summary()
        XCTAssertTrue(summary.isSignInNeeded)
        await index.clearError()
        summary = await index.summary()
        XCTAssertFalse(summary.isSignInNeeded)
        XCTAssertNil(summary.lastFailure)
    }

    func testAnotherProblemRecordedWhileSignInIsNeededLeavesItSet() async throws {
        let index = try PhotoBackupIndex(directory: try temporaryDirectory(self))
        await index.setError(failure: .signInRefused, isSignInNeeded: true)
        await index.setError(failure: .systemDescription("cancelled"))
        var summary = await index.summary()
        XCTAssertTrue(summary.isSignInNeeded, "only an explicit value changes the flag")
        XCTAssertEqual(summary.lastFailure, .systemDescription("cancelled"))
        await index.setError(failure: nil)
        summary = await index.summary()
        XCTAssertTrue(summary.isSignInNeeded)
        await index.setError(failure: nil, isSignInNeeded: false)
        summary = await index.summary()
        XCTAssertFalse(summary.isSignInNeeded)
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
        XCTAssertEqual(
            PhotoBackupOutcome.interpret(statusCode: 401, photoIdHeader: nil, bodyText: "not signed in", errorDescription: nil, isConnectivityFailure: false),
            .signInNeeded)
        XCTAssertEqual(
            PhotoBackupOutcome.interpret(
                statusCode: 403, photoIdHeader: nil, bodyText: "this device password does not grant photos\n", errorDescription: nil, isConnectivityFailure: false),
            .signInNeeded)
    }

    func testAnOtherForbiddenAnswerIsTheServersReasonAndIsNotAWrongPassword() {
        for reason in ["photos are not available on this server", "this account has no photo library", "photos are uploaded with a device password over HTTPS only"] {
            XCTAssertEqual(
                PhotoBackupOutcome.interpret(statusCode: 403, photoIdHeader: nil, bodyText: reason + "\n", errorDescription: nil, isConnectivityFailure: false),
                .retryLater(failure: .serverReason(reason), isConnectivityFailure: false))
        }
        XCTAssertEqual(
            PhotoBackupOutcome.interpret(statusCode: 403, photoIdHeader: nil, bodyText: "", errorDescription: nil, isConnectivityFailure: false),
            .retryLater(failure: nil, isConnectivityFailure: false))
    }

    func testAFileTheServerRefusesIsRefusedAndAServerTroubleIsRetried() {
        XCTAssertEqual(
            PhotoBackupOutcome.interpret(statusCode: 413, photoIdHeader: nil, bodyText: "the library is full\n", errorDescription: nil, isConnectivityFailure: false),
            .refused(failure: .fileRefused(serverReason: "the library is full")))
        XCTAssertEqual(
            PhotoBackupOutcome.interpret(statusCode: 415, photoIdHeader: nil, bodyText: "", errorDescription: nil, isConnectivityFailure: false),
            .refused(failure: .fileRefused(serverReason: nil)))
        for status in [400, 413, 415, 422] {
            guard case .refused = PhotoBackupOutcome.interpret(
                statusCode: status, photoIdHeader: nil, bodyText: "no", errorDescription: nil, isConnectivityFailure: false)
            else { return XCTFail("\(status) is the server refusing the file") }
        }
        for status in [404, 408, 409, 410, 425, 429, 500, 502, 503] {
            guard case .retryLater(_, let isConnectivityFailure, _) = PhotoBackupOutcome.interpret(
                statusCode: status, photoIdHeader: nil, bodyText: nil, errorDescription: nil, isConnectivityFailure: false)
            else { return XCTFail("\(status) is the server's trouble, not the file's") }
            XCTAssertFalse(isConnectivityFailure)
        }
    }

    func testAnUploadRefusedForTheAccountsOtherUploadsIsRetriedAfterTheTimeTheServerSays() {
        // The server answers 503 with Retry-After, not 413, when only the
        // account's other unfinished uploads are in the way.
        XCTAssertEqual(
            PhotoBackupOutcome.interpret(
                statusCode: 503, photoIdHeader: nil, bodyText: "other uploads of this account are not finished\n", errorDescription: nil,
                isConnectivityFailure: false, retryAfterHeader: "60"),
            .retryLater(failure: .serverReason("other uploads of this account are not finished"), isConnectivityFailure: false, retryAfterSeconds: 60))
        XCTAssertEqual(
            PhotoBackupOutcome.interpret(statusCode: 503, photoIdHeader: nil, bodyText: nil, errorDescription: nil, isConnectivityFailure: false),
            .retryLater(failure: nil, isConnectivityFailure: false, retryAfterSeconds: nil))
        XCTAssertEqual(
            PhotoBackupOutcome.interpret(statusCode: 413, photoIdHeader: nil, bodyText: "the library is full", errorDescription: nil, isConnectivityFailure: false, retryAfterHeader: "60"),
            .refused(failure: .fileRefused(serverReason: "the library is full")), "a 413 is still the server refusing the file")
    }

    func testARetryAfterIsReadAsSecondsOrAsADateAndIsNeverLongerThanAnHour() {
        let now = Date(timeIntervalSince1970: 1_800_000_000)
        XCTAssertEqual(PhotoBackupOutcome.retryAfterSeconds(fromHeader: "120", now: now), 120)
        XCTAssertEqual(PhotoBackupOutcome.retryAfterSeconds(fromHeader: " 30 ", now: now), 30)
        XCTAssertEqual(PhotoBackupOutcome.retryAfterSeconds(fromHeader: "999999", now: now), PhotoBackupOutcome.longestRetryAfterSeconds)
        XCTAssertNil(PhotoBackupOutcome.retryAfterSeconds(fromHeader: "0", now: now))
        XCTAssertNil(PhotoBackupOutcome.retryAfterSeconds(fromHeader: "-5", now: now))
        XCTAssertNil(PhotoBackupOutcome.retryAfterSeconds(fromHeader: "soon", now: now))
        XCTAssertNil(PhotoBackupOutcome.retryAfterSeconds(fromHeader: nil, now: now))
        // 1_800_000_000 is Fri, 15 Jan 2027 08:00:00 GMT.
        XCTAssertEqual(PhotoBackupOutcome.retryAfterSeconds(fromHeader: "Fri, 15 Jan 2027 08:01:30 GMT", now: now), 90)
        XCTAssertNil(PhotoBackupOutcome.retryAfterSeconds(fromHeader: "Fri, 15 Jan 2027 07:59:00 GMT", now: now), "a date that has passed asks for no wait")
    }

    func testALostOrConflictingUploadIsStartedAgainNotRefused() {
        for status in [404, 409, 410] {
            XCTAssertEqual(
                PhotoBackupOutcome.interpret(statusCode: status, photoIdHeader: nil, bodyText: "gone\n", errorDescription: nil, isConnectivityFailure: false),
                .retryLater(failure: .serverReason("gone"), isConnectivityFailure: false), "\(status)")
        }
    }

    func testACancelledTaskIsNeitherAnErrorNorACount() {
        XCTAssertEqual(
            PhotoBackupOutcome.interpret(
                statusCode: nil, photoIdHeader: nil, bodyText: nil, errorDescription: "cancelled", isConnectivityFailure: false, isCancelled: true),
            .cancelled)
    }

    func testATransportErrorIsRetriedAndSaysWhetherTheNetworkWasTheCause() {
        XCTAssertEqual(
            PhotoBackupOutcome.interpret(statusCode: nil, photoIdHeader: nil, bodyText: nil, errorDescription: "offline", isConnectivityFailure: true),
            .retryLater(failure: .systemDescription("offline"), isConnectivityFailure: true))
        XCTAssertEqual(
            PhotoBackupOutcome.interpret(statusCode: 401, photoIdHeader: nil, bodyText: nil, errorDescription: "cancelled", isConnectivityFailure: false),
            .retryLater(failure: .systemDescription("cancelled"), isConnectivityFailure: false))
    }

    func testTheCreationRequestIsAPostWithNoBodyThatDescribesTheFileAndSaysTheBytesFollow() throws {
        let server = try ServerAddress(parsing: "photos.example.com")
        let credential = PhotoBackupCredential(credentialId: "c1", credentialName: "Robin's iPhone photo backup", username: "robin", password: "sync-secret")
        let upload = try XCTUnwrap(PhotoBackupPlanner.plan(items: [makeItem("a", cloudIdentifier: "cloud-a")], records: [:]).first)
        let request = PhotoBackupRequest.makeCreation(server: server, credential: credential, upload: upload, sizeBytes: 4096, usesMobileData: false)
        XCTAssertEqual(request.httpMethod, "POST")
        XCTAssertEqual(request.url?.absoluteString, "https://photos.example.com/api/v1/photos/uploads")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Basic " + Data("robin:sync-secret".utf8).base64EncodedString())
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-Photo-Client-ID"), "cloud-a")
        XCTAssertEqual(request.value(forHTTPHeaderField: "X-Photo-Source"), "iphone")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Content-Type"), "image/heic")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Upload-Complete"), "?0", "the bytes follow in a request of their own")
        XCTAssertEqual(request.value(forHTTPHeaderField: "Upload-Length"), "4096")
        XCTAssertNil(request.httpBody)
        XCTAssertFalse(request.allowsCellularAccess)
        XCTAssertEqual(request.timeoutInterval, PhotoBackupRequest.smallRequestTimeoutSeconds)
        XCTAssertTrue(PhotoBackupRequest.makeCreation(server: server, credential: credential, upload: upload, sizeBytes: 1, usesMobileData: true).allowsCellularAccess)
    }

    func testTheStatusRequestIsAHeadAndTheContinuationAPatchThatSaysWhereItGoesAndThatItIsTheEnd() throws {
        let server = try ServerAddress(parsing: "photos.example.com")
        let credential = PhotoBackupCredential(credentialId: "c1", credentialName: "backup", username: "robin", password: "sync-secret")
        let status = PhotoBackupRequest.makeStatus(server: server, credential: credential, uploadPath: "/api/v1/photos/uploads/u1", usesMobileData: false)
        XCTAssertEqual(status.httpMethod, "HEAD")
        XCTAssertEqual(status.url?.absoluteString, "https://photos.example.com/api/v1/photos/uploads/u1")
        XCTAssertEqual(status.value(forHTTPHeaderField: "Authorization"), credential.authorizationHeader)
        XCTAssertEqual(status.timeoutInterval, PhotoBackupRequest.smallRequestTimeoutSeconds)
        let continuation = PhotoBackupRequest.makeContinuation(
            server: server, credential: credential, uploadPath: "/api/v1/photos/uploads/u1", offsetBytes: 8_388_608, usesMobileData: true)
        XCTAssertEqual(continuation.httpMethod, "PATCH")
        XCTAssertEqual(continuation.url?.absoluteString, "https://photos.example.com/api/v1/photos/uploads/u1")
        XCTAssertEqual(continuation.value(forHTTPHeaderField: "Upload-Offset"), "8388608")
        XCTAssertEqual(continuation.value(forHTTPHeaderField: "Upload-Complete"), "?1")
        XCTAssertEqual(continuation.value(forHTTPHeaderField: "Content-Type"), "application/partial-upload", "the server refuses a PATCH of any other type")
        XCTAssertEqual(continuation.value(forHTTPHeaderField: "Authorization"), credential.authorizationHeader)
        XCTAssertNil(continuation.value(forHTTPHeaderField: "X-Photo-Client-ID"), "the upload already knows what the file is")
        XCTAssertTrue(continuation.allowsCellularAccess)
    }

    func testOnlyTheServersUploadPathsAreFollowed() {
        XCTAssertTrue(PhotoBackupRequest.isUploadPath("/api/v1/photos/uploads/abc123"))
        for other in ["/api/v1/photos/uploads", "/api/v1/photos/uploads/", "/api/v1/photos/uploads/a/b", "/api/v1/photos/uploads/a?b", "/graphql", ""] {
            XCTAssertFalse(PhotoBackupRequest.isUploadPath(other), other)
        }
    }

    func testTheCreationRequestDeclaresTheFilesDigestInLowercaseOnlyWhenItIsSixtyFourHexadecimalDigits() throws {
        let server = try ServerAddress(parsing: "photos.example.com")
        let credential = PhotoBackupCredential(credentialId: "c1", credentialName: "backup", username: "robin", password: "sync-secret")
        let upload = try XCTUnwrap(PhotoBackupPlanner.plan(items: [makeItem("a", cloudIdentifier: "cloud-a")], records: [:]).first)
        let digest = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
        let declared = PhotoBackupRequest.makeCreation(
            server: server, credential: credential, upload: upload, sizeBytes: 3, usesMobileData: false, sha256: digest.uppercased())
        XCTAssertEqual(declared.value(forHTTPHeaderField: "X-Photo-Declared-SHA256"), digest)
        for notADigest in [nil, "", "abc", String(digest.dropLast()), digest + "0", String(repeating: "g", count: 64)] {
            let request = PhotoBackupRequest.makeCreation(
                server: server, credential: credential, upload: upload, sizeBytes: 3, usesMobileData: false, sha256: notADigest)
            XCTAssertNil(request.value(forHTTPHeaderField: "X-Photo-Declared-SHA256"), "\(notADigest ?? "nil") is not sent")
        }
    }

    func testTheDigestOfTheExportedFileIsSentOnTheCreationRequest() async throws {
        let rig = try makeRig(self, items: [makeItem("a"), makeItem("b", secondsSince2026: 50)])
        let digest = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
        rig.library.set(sha256: digest, for: PhotoBackupUpload.key(localIdentifier: "a", role: .original))
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p-" + job.localIdentifier)) }
        }
        _ = await coordinator.runPass(settings: settings)
        let requests = rig.uploads.creationRequests
        XCTAssertEqual(requests.count, 2)
        let sent = requests.map { $0.value(forHTTPHeaderField: "X-Photo-Declared-SHA256") }
        XCTAssertTrue(sent.contains(digest), "the file with a digest declares it")
        XCTAssertTrue(sent.contains(nil), "a file whose digest could not be worked out is sent without one")
    }

    func testTheBudgetKeepsTwoHundredUploadsAndTenGigabytesButAlwaysTakesTheFirst() {
        let budget = PhotoBackupBudget.standard
        let gigabyte: Int64 = 1024 * 1024 * 1024
        XCTAssertEqual(budget.maximumUploadCount, 200)
        XCTAssertEqual(budget.maximumOutstandingBytes, 10 * gigabyte)
        XCTAssertTrue(budget.allowsAnother(outstandingCount: 0, outstandingBytes: 0))
        XCTAssertTrue(budget.allowsAnother(outstandingCount: 199, outstandingBytes: gigabyte))
        XCTAssertFalse(budget.allowsAnother(outstandingCount: 200, outstandingBytes: 10))
        XCTAssertFalse(budget.allowsAnother(outstandingCount: 1, outstandingBytes: 10 * gigabyte))
        XCTAssertTrue(budget.allowsFile(sizeBytes: 15 * gigabyte, outstandingCount: 0, outstandingBytes: 0))
        XCTAssertFalse(budget.allowsFile(sizeBytes: gigabyte + 1, outstandingCount: 1, outstandingBytes: 9 * gigabyte))
    }

    func testTheBytesQueuedAreHeldToAQuarterOfTheFreeSpaceWhenThatIsLess() {
        let gigabyte: Int64 = 1024 * 1024 * 1024
        let budget = PhotoBackupBudget.standard
        XCTAssertEqual(budget.limited(toFreeSpaceBytes: 8 * gigabyte).maximumOutstandingBytes, 2 * gigabyte, "a quarter of 8 GB")
        XCTAssertEqual(budget.limited(toFreeSpaceBytes: 200 * gigabyte).maximumOutstandingBytes, 10 * gigabyte, "the budget is the lesser")
        XCTAssertEqual(budget.limited(toFreeSpaceBytes: nil), budget, "free space that cannot be read changes nothing")
        XCTAssertEqual(budget.limited(toFreeSpaceBytes: -5).maximumOutstandingBytes, 0)
        XCTAssertEqual(budget.limited(toFreeSpaceBytes: 8 * gigabyte).maximumUploadCount, 200, "the count is not touched")
        let limited = budget.limited(toFreeSpaceBytes: 4 * gigabyte)
        XCTAssertTrue(limited.allowsFile(sizeBytes: 5 * gigabyte, outstandingCount: 0, outstandingBytes: 0), "the first file always may")
        XCTAssertFalse(limited.allowsFile(sizeBytes: 1, outstandingCount: 1, outstandingBytes: gigabyte), "a quarter of 4 GB is full")
    }

    func testFilesOnThePhoneComeBeforeFilesThatNeedICloudEachGroupKeepingItsOrder() {
        func upload(_ name: String, isLocal: Bool) -> PhotoBackupUpload {
            PhotoBackupUpload(
                localIdentifier: name, role: .original, clientPhotoId: "local:" + name, filename: name, contentType: "image/heic",
                isLocallyAvailable: isLocal, fileSizeBytes: nil, assetModificationDate: nil, headers: [:])
        }
        let ordered = PhotoBackupPlanner.preferringLocalFiles([
            upload("a", isLocal: false), upload("b", isLocal: true), upload("c", isLocal: false), upload("d", isLocal: true),
        ])
        XCTAssertEqual(ordered.map(\.localIdentifier), ["b", "d", "a", "c"])
    }

    func testTheOldFourFileBudgetStillWorks() {
        let budget = legacyBudget
        let gigabyte: Int64 = 1024 * 1024 * 1024
        XCTAssertTrue(budget.allowsAnother(outstandingCount: 0, outstandingBytes: 0))
        XCTAssertTrue(budget.allowsAnother(outstandingCount: 3, outstandingBytes: gigabyte))
        XCTAssertFalse(budget.allowsAnother(outstandingCount: 4, outstandingBytes: 10))
        XCTAssertFalse(budget.allowsAnother(outstandingCount: 1, outstandingBytes: 2 * gigabyte))
        XCTAssertTrue(budget.allowsFile(sizeBytes: 5 * gigabyte, outstandingCount: 0, outstandingBytes: 0))
        XCTAssertTrue(budget.allowsFile(sizeBytes: gigabyte, outstandingCount: 1, outstandingBytes: gigabyte))
        XCTAssertFalse(budget.allowsFile(sizeBytes: gigabyte + 1, outstandingCount: 1, outstandingBytes: gigabyte))
    }

    func testALostNetworkThatComesBackStartsAPassOnlyWhenFilesWaitAndNothingRuns() {
        let offline = PhotoBackupNetworkState(isSatisfied: false, isExpensive: false)
        let wifi = PhotoBackupNetworkState(isSatisfied: true, isExpensive: false)
        let mobile = PhotoBackupNetworkState(isSatisfied: true, isExpensive: true)
        func shouldResume(
            from previous: PhotoBackupNetworkState?, to current: PhotoBackupNetworkState, usesMobileData: Bool = false, leftForLater: Int = 3, isPassRunning: Bool = false
        ) -> Bool {
            PhotoBackupResumePolicy.shouldResume(
                from: previous, to: current, usesMobileData: usesMobileData, leftForLaterFileCount: leftForLater, isPassRunning: isPassRunning)
        }
        XCTAssertTrue(shouldResume(from: offline, to: wifi))
        XCTAssertTrue(shouldResume(from: nil, to: wifi), "the first report counts as a network that came")
        XCTAssertFalse(shouldResume(from: offline, to: wifi, leftForLater: 0), "nothing waits")
        XCTAssertFalse(shouldResume(from: offline, to: wifi, isPassRunning: true), "a pass is running already")
        XCTAssertFalse(shouldResume(from: wifi, to: offline), "the network went")
        XCTAssertFalse(shouldResume(from: wifi, to: wifi), "nothing changed")
        XCTAssertFalse(shouldResume(from: offline, to: mobile), "mobile data is not allowed")
        XCTAssertTrue(shouldResume(from: offline, to: mobile, usesMobileData: true))
        XCTAssertTrue(shouldResume(from: mobile, to: wifi), "Wi-Fi replaced mobile data")
        XCTAssertFalse(shouldResume(from: wifi, to: mobile, usesMobileData: true), "mobile data replacing Wi-Fi is not a network that came")
    }

    func testTheBackgroundTaskIsAskedForSoonWhenFilesWaitOrTheListingIsNotOver() {
        XCTAssertEqual(PhotoBackupSchedule.earliestBeginDelaySeconds(leftForLaterFileCount: 4, isListingUnfinished: false), 15 * 60)
        XCTAssertEqual(PhotoBackupSchedule.earliestBeginDelaySeconds(leftForLaterFileCount: 0, isListingUnfinished: false), 60 * 60)
        XCTAssertEqual(
            PhotoBackupSchedule.earliestBeginDelaySeconds(leftForLaterFileCount: 0, isListingUnfinished: true), 15 * 60,
            "photos the listing has not reached are work left, though no file waits")
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
        // Removed by the suite's name, so the teardown does not capture the
        // defaults object, which is not Sendable.
        addTeardownBlock { UserDefaults().removePersistentDomain(forName: suiteName) }
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

    func testThePausedFlagIsOffByDefaultAndSurvivesAReloadOfTheSavedSettings() throws {
        let suiteName = "photo-backup-paused-tests-" + UUID().uuidString
        let defaults = try XCTUnwrap(UserDefaults(suiteName: suiteName))
        addTeardownBlock { UserDefaults().removePersistentDomain(forName: suiteName) }
        XCTAssertFalse(PhotoBackupPreferences(defaults: defaults).isPaused)
        PhotoBackupPreferences(defaults: defaults).isPaused = true
        XCTAssertTrue(PhotoBackupPreferences(defaults: defaults).isPaused, "paused until the person resumes, whatever the launch")
        // Pausing is not turning off: the account stays.
        PhotoBackupPreferences(defaults: defaults).enabledAccountKey = "one|robin"
        XCTAssertEqual(PhotoBackupPreferences(defaults: defaults).enabledAccountKey, "one|robin")
        PhotoBackupPreferences(defaults: defaults).isPaused = false
        XCTAssertFalse(PhotoBackupPreferences(defaults: defaults).isPaused)
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
        XCTAssertTrue(Documents.findPhotos.contains("photoFileRoles"))
        let withRoles = try JSONDecoder().decode(
            [PhotoBackupServerPhoto].self,
            from: Data(#"[{"id":"p1","clientPhotoId":"cloud-a","photoKind":"livePhoto","photoFileRoles":["original","liveVideo"]}]"#.utf8))
        XCTAssertEqual(withRoles.first?.photoFileRoles, ["original", "liveVideo"])
    }
}

// MARK: A pass with a library and a transfer that are not real

private final class FakeLibrary: PhotoBackupLibrary, @unchecked Sendable {
    private let lock = NSLock()
    private var allItems: [PhotoBackupItem] { didSet { orderedItems = nil; undatedItems = nil; itemsByIdentifier = nil } }
    /// The items with a creation date newest first, made once, as a real library
    /// keeps its own order.
    private var orderedItems: [PhotoBackupItem]?
    /// The items with no creation date, which the listing ends with.
    private var undatedItems: [PhotoBackupItem]?
    private var itemsByIdentifier: [String: PhotoBackupItem]?
    private var exportedBytes: [String: Int64] = [:]
    private var exportedDigests: [String: String] = [:]
    private var exportedContents: [String: Data] = [:]
    private var unavailableKeys: Set<String> = []
    private var unavailableFailures: [String: PhotoBackupFailure] = [:]
    private var failedKeys: Set<String> = []
    private var accessLevel = PhotoBackupAccess.full
    private var tokenIsExpired = false
    private var changedItems: [PhotoBackupItem] = []
    private var listingCount = 0
    private var changesCount = 0
    private var builtIdentifiers: [String] = []
    private var largestRequest = 0
    private var listedResumePoints: [PhotoBackupResumePoint?] = []
    private var batchHook: (@Sendable (Int) async -> Void)?
    private var itemsHook: (@Sendable (Int) async -> Void)?
    private var itemRequestCount = 0
    private var exportedIncludesHidden: [Bool] = []
    private var hungKeys: Set<String> = []
    private var secondsPerExport: TimeInterval = 0
    private var exportedKeys: [String] = []
    private var exportedExpensiveFlags: [Bool] = []
    var clock: ManualClock?

    init(items: [PhotoBackupItem]) { allItems = items }

    /// An export of this file that never returns, as a stuck download from iCloud.
    func set(hung key: String) { lock.withLock { _ = hungKeys.insert(key) } }
    /// Each export moves the test's clock on by this much.
    func set(secondsPerExport seconds: TimeInterval) { lock.withLock { secondsPerExport = seconds } }
    /// The keys exported, in order.
    var exportedUploadKeys: [String] { lock.withLock { exportedKeys } }
    /// Whether each export was allowed to use a network that costs.
    var exportsAllowedExpensiveNetwork: [Bool] { lock.withLock { exportedExpensiveFlags } }

    /// The Hidden album setting each export was asked with.
    var exportsIncludedHidden: [Bool] { lock.withLock { exportedIncludesHidden } }
    /// How many files were exported.
    var exportCount: Int { lock.withLock { exportedIncludesHidden.count } }

    func set(access: PhotoBackupAccess) { lock.withLock { accessLevel = access } }
    func set(bytes: Int64, for key: String) { lock.withLock { exportedBytes[key] = bytes } }
    func set(sha256: String, for key: String) { lock.withLock { exportedDigests[key] = sha256 } }
    /// The exact bytes the file is exported with, whole, and their size. For a
    /// test that checks what is read from the exported file.
    func set(content: Data, for key: String) { lock.withLock { exportedContents[key] = content } }
    func set(unavailable key: String, _ isUnavailable: Bool, failure: PhotoBackupFailure = .systemDescription("in iCloud")) {
        lock.withLock {
            if isUnavailable {
                unavailableKeys.insert(key)
                unavailableFailures[key] = failure
            } else {
                unavailableKeys.remove(key)
            }
        }
    }
    func set(failed key: String) { lock.withLock { _ = failedKeys.insert(key) } }
    func set(tokenIsExpired: Bool) { lock.withLock { self.tokenIsExpired = tokenIsExpired } }
    func set(changes: [PhotoBackupItem]) { lock.withLock { changedItems = changes } }
    /// How many times the whole library was listed from the start.
    var listings: Int { lock.withLock { listingCount } }
    var changeReads: Int { lock.withLock { changesCount } }
    /// The identifiers items were built for, in the order asked.
    var identifiersItemsWereBuiltFor: [String] { lock.withLock { builtIdentifiers } }
    /// The most items asked for in one call.
    var largestItemRequestCount: Int { lock.withLock { largestRequest } }
    /// The resume point each batch of a listing was asked after.
    var batchResumePoints: [PhotoBackupResumePoint?] { lock.withLock { listedResumePoints } }
    /// Called after each batch is listed with how many batches there have been.
    func set(afterBatch hook: (@Sendable (Int) async -> Void)?) { lock.withLock { batchHook = hook } }
    /// Called after each request for items is answered with how many requests there have been.
    func set(afterItems hook: (@Sendable (Int) async -> Void)?) { lock.withLock { itemsHook = hook } }

    func access() -> PhotoBackupAccess { lock.withLock { accessLevel } }

    func beginListing(includesHidden: Bool) async throws -> PhotoBackupListingStart {
        lock.withLock {
            listingCount += 1
            return PhotoBackupListingStart(totalCount: allItems.count, changeToken: Data("token-\(listingCount)".utf8))
        }
    }

    /// Newest first, the same date by identifier.
    private static func comesBefore(_ first: PhotoBackupItem, _ second: PhotoBackupItem) -> Bool {
        if let firstDate = first.creationDate, let secondDate = second.creationDate, firstDate != secondDate { return firstDate > secondDate }
        return first.localIdentifier < second.localIdentifier
    }

    /// A batch as the real library makes it: the dated items after the resume
    /// point, where `PhotoBackupListingWindow` says the batch ends, and the
    /// undated items once, with the batch that reaches the end of the dated ones.
    func listBatch(after resumePoint: PhotoBackupResumePoint?, limit: Int, includesHidden: Bool) async throws -> PhotoBackupListingBatch {
        let (batch, hook, batchCount) = lock.withLock { () -> (PhotoBackupListingBatch, (@Sendable (Int) async -> Void)?, Int) in
            listedResumePoints.append(resumePoint)
            let ordered = orderedItems ?? allItems.filter { $0.creationDate != nil }.sorted(by: Self.comesBefore)
            orderedItems = ordered
            let undated = undatedItems ?? allItems.filter { $0.creationDate == nil }.sorted { $0.localIdentifier < $1.localIdentifier }
            undatedItems = undated
            // The first item the query for the batch holds, found by halving: the
            // dates only go down.
            var low = 0
            if let resumePoint {
                let newestDate = PhotoBackupListingWindow.newestCreationDate(after: resumePoint)
                var high = ordered.count
                while low < high {
                    let middle = (low + high) / 2
                    if let date = ordered[middle].creationDate, date <= newestDate { high = middle } else { low = middle + 1 }
                }
            }
            let candidateCount = ordered.count - low
            let takeCount = PhotoBackupListingWindow.takeCount(candidateCount: candidateCount, limit: limit) { ordered[low + $0].creationDate ?? .distantPast }
            var items = Array(ordered[low..<(low + takeCount)])
            let isEndOfDatedItems = takeCount >= candidateCount
            if isEndOfDatedItems { items.append(contentsOf: undated) }
            let entries = items.map { PhotoBackupLibraryEntry(localIdentifier: $0.localIdentifier, creationDate: $0.creationDate) }
            return (PhotoBackupListingBatch(entries: entries, isLast: isEndOfDatedItems), batchHook, listedResumePoints.count)
        }
        await hook?(batchCount)
        return batch
    }

    func removeFromLibrary(_ localIdentifier: String) { lock.withLock { allItems.removeAll { $0.localIdentifier == localIdentifier } } }

    func items(withLocalIdentifiers localIdentifiers: [String], includesHidden: Bool) async throws -> [PhotoBackupItem] {
        let (found, hook, requestCount) = lock.withLock { () -> ([PhotoBackupItem], (@Sendable (Int) async -> Void)?, Int) in
            itemRequestCount += 1
            builtIdentifiers.append(contentsOf: localIdentifiers)
            largestRequest = max(largestRequest, localIdentifiers.count)
            var byIdentifier = itemsByIdentifier ?? Dictionary(allItems.map { ($0.localIdentifier, $0) }, uniquingKeysWith: { first, _ in first })
            itemsByIdentifier = byIdentifier
            // What changed is read as it is now, in place of what it was.
            for item in changedItems { byIdentifier[item.localIdentifier] = item }
            return (localIdentifiers.compactMap { byIdentifier[$0] }, itemsHook, itemRequestCount)
        }
        await hook?(requestCount)
        return found
    }

    func changes(since token: Data, includesHidden: Bool) async throws -> PhotoBackupChanges {
        try lock.withLock {
            changesCount += 1
            if tokenIsExpired { throw PhotoBackupLibraryError.changeTokenExpired }
            return PhotoBackupChanges(
                localIdentifiers: changedItems.map(\.localIdentifier), totalCount: allItems.count + changedItems.count,
                changeToken: Data("changes-\(changesCount)".utf8))
        }
    }

    func export(_ upload: PhotoBackupUpload, to directory: URL, mayUseExpensiveNetwork: Bool, includesHidden: Bool) async throws -> PhotoBackupExportedFile {
        let (bytes, digest, content, isUnavailable, isFailed, isHung, seconds) = lock.withLock {
            exportedIncludesHidden.append(includesHidden)
            exportedKeys.append(upload.key)
            exportedExpensiveFlags.append(mayUseExpensiveNetwork)
            return (
                exportedContents[upload.key]?.count.asInt64 ?? exportedBytes[upload.key] ?? 10, exportedDigests[upload.key],
                exportedContents[upload.key], unavailableKeys.contains(upload.key), failedKeys.contains(upload.key), hungKeys.contains(upload.key),
                secondsPerExport
            )
        }
        if isHung { try await Task.sleep(nanoseconds: 3_600_000_000_000) }
        clock?.advance(seconds: seconds)
        if isUnavailable {
            throw PhotoBackupLibraryError.unavailable(lock.withLock { unavailableFailures[upload.key] } ?? .systemDescription("in iCloud"))
        }
        if isFailed { throw PhotoBackupLibraryError.failed(.systemDescription("cannot be read")) }
        let outgoingFilename = UUID().uuidString
        try (content ?? Data(count: Int(min(bytes, 1000)))).write(to: directory.appendingPathComponent(outgoingFilename))
        return PhotoBackupExportedFile(outgoingFilename: outgoingFilename, sizeBytes: bytes, sha256: digest)
    }
}

private final class FakeTransfer: PhotoBackupTransfer, @unchecked Sendable {
    private let lock = NSLock()
    private var jobs: [PhotoBackupJob] = []
    private var requests: [URLRequest] = []
    private var bodies: [Data] = []
    private var heldJobs: [PhotoBackupJob] = []
    private var wasCancelled = false
    /// Called with each job as it is queued; the test finishes it or leaves it.
    var onStart: (@Sendable (PhotoBackupJob) -> Void)?

    var startedJobs: [PhotoBackupJob] { lock.withLock { jobs } }
    var startedRequests: [URLRequest] { lock.withLock { requests } }
    /// What each queued file held when it was queued, which is what the session
    /// would have sent.
    var startedBodies: [Data] { lock.withLock { bodies } }
    var isCancelled: Bool { lock.withLock { wasCancelled } }

    func start(request: URLRequest, job: PhotoBackupJob, fileURL: URL) async throws {
        XCTAssertTrue(FileManager.default.fileExists(atPath: fileURL.path), "the file is written before it is queued")
        let body = (try? Data(contentsOf: fileURL)) ?? Data()
        lock.withLock {
            jobs.append(job)
            requests.append(request)
            bodies.append(body)
            heldJobs.append(job)
        }
        onStart?(job)
    }

    func queuedJobs() async -> [PhotoBackupJob] { lock.withLock { heldJobs } }

    func release(_ job: PhotoBackupJob) { lock.withLock { heldJobs.removeAll { $0.key == job.key } } }

    func cancelAll() async { lock.withLock { wasCancelled = true; heldJobs.removeAll() } }
}

private extension Int {
    var asInt64: Int64 { Int64(self) }
}

/// The server's side of the small requests: the uploads it made, how many bytes
/// of each it has, and the answers a test makes it give.
private final class FakeUploads: PhotoBackupUploads, @unchecked Sendable {
    private let lock = NSLock()
    private var offsets: [String: Int64] = [:]
    private var photoIds: [String: String] = [:]
    private var createdCount = 0
    private var creations: [URLRequest] = []
    private var statuses: [URLRequest] = []
    private var creationOutcomes: [PhotoBackupOutcome] = []
    private var statusOutcomes: [PhotoBackupOutcome] = []

    /// The creation requests, in the order they were made.
    var creationRequests: [URLRequest] { lock.withLock { creations } }
    /// The requests that asked where an upload is.
    var statusRequests: [URLRequest] { lock.withLock { statuses } }
    /// The paths of the uploads created, in order.
    var createdPaths: [String] { lock.withLock { (0..<createdCount).map { Self.path(number: $0 + 1) } } }

    static func path(number: Int) -> String { "/api/v1/photos/uploads/fake-\(number)" }

    /// The server now has this many bytes of the upload.
    func set(offsetBytes: Int64, ofUploadAt path: String) { lock.withLock { offsets[path] = offsetBytes } }
    /// The server completed the upload as this photo.
    func set(completePhotoId photoId: String, ofUploadAt path: String) { lock.withLock { photoIds[path] = photoId } }
    /// The server has forgotten the upload (a week idle, cancelled, cleaned up).
    func forget(uploadAt path: String) { lock.withLock { offsets[path] = nil; photoIds[path] = nil } }
    /// The next creation does not work, with this outcome.
    func failNextCreation(with outcome: PhotoBackupOutcome) { lock.withLock { creationOutcomes.append(outcome) } }
    /// The next question about an upload is not answered, with this outcome.
    func failNextStatus(with outcome: PhotoBackupOutcome) { lock.withLock { statusOutcomes.append(outcome) } }

    func createUpload(_ request: URLRequest) async -> PhotoBackupServiceAnswer<String> {
        lock.withLock {
            creations.append(request)
            if !creationOutcomes.isEmpty { return .unsuccessful(creationOutcomes.removeFirst()) }
            createdCount += 1
            let path = Self.path(number: createdCount)
            offsets[path] = 0
            return .answered(path)
        }
    }

    func uploadState(_ request: URLRequest) async -> PhotoBackupServiceAnswer<PhotoBackupServerUpload> {
        lock.withLock {
            statuses.append(request)
            if !statusOutcomes.isEmpty { return .unsuccessful(statusOutcomes.removeFirst()) }
            let path = request.url?.path ?? ""
            if let photoId = photoIds[path] { return .answered(.complete(serverPhotoId: photoId)) }
            guard let offset = offsets[path] else { return .answered(.gone) }
            return .answered(.receiving(offsetBytes: offset))
        }
    }
}

private final class NetworkQuestions: @unchecked Sendable {
    private let lock = NSLock()
    private var count = 0
    func add() { lock.withLock { count += 1 } }
    var total: Int { lock.withLock { count } }
}

private struct FakeNetwork: PhotoBackupNetwork {
    var isExpensiveNetwork = false
    var isReachable = true
    let questions = NetworkQuestions()
    func isExpensive() async -> Bool {
        questions.add()
        return isExpensiveNetwork
    }
    func state() async -> PhotoBackupNetworkState {
        questions.add()
        return PhotoBackupNetworkState(isSatisfied: isReachable, isExpensive: isExpensiveNetwork)
    }
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

/// Four files and two gigabytes at a time, which most tests were written for;
/// the app's own budget is `PhotoBackupBudget.standard`.
private let legacyBudget = PhotoBackupBudget(maximumUploadCount: 4, maximumOutstandingBytes: 2 * 1024 * 1024 * 1024)

private struct Rig {
    let directory: URL
    let outgoing: URL
    let index: PhotoBackupIndex
    let library: FakeLibrary
    let transfer: FakeTransfer
    let uploads: FakeUploads
    let coordinator: PhotoBackupCoordinator
}

private func makeRig(
    _ test: XCTestCase, items: [PhotoBackupItem], lookup: FakeLookup? = nil, network: FakeNetwork = FakeNetwork(), budget: PhotoBackupBudget = legacyBudget,
    index existingIndex: PhotoBackupIndex? = nil, directory existingDirectory: URL? = nil, idleTimeoutSeconds: TimeInterval = 300,
    exportTimeoutSeconds: TimeInterval = 30 * 60, freeSpaceBytes: @escaping @Sendable () -> Int64? = { nil }, clock: ManualClock? = nil,
    batchSize: Int = PhotoBackupCoordinator.standardBatchSize, isPaused: Bool = false, uploads existingUploads: FakeUploads? = nil
) throws -> Rig {
    let directory = try existingDirectory ?? temporaryDirectory(test)
    let outgoing = directory.appendingPathComponent("outgoing")
    try FileManager.default.createDirectory(at: outgoing, withIntermediateDirectories: true)
    let index = try existingIndex ?? PhotoBackupIndex(directory: directory)
    let library = FakeLibrary(items: items)
    library.clock = clock
    let transfer = FakeTransfer()
    let uploads = existingUploads ?? FakeUploads()
    let coordinator = PhotoBackupCoordinator(
        index: index, library: library, transfer: transfer, uploads: uploads, lookup: lookup, network: network, outgoingDirectory: outgoing,
        server: try ServerAddress(parsing: "photos.example.com"), accountKey: "photos.example.com|robin",
        credential: PhotoBackupCredential(credentialId: "c1", credentialName: "backup", username: "robin", password: "sync-secret"),
        budget: budget, idleTimeoutSeconds: idleTimeoutSeconds, exportTimeoutSeconds: exportTimeoutSeconds, freeSpaceBytes: freeSpaceBytes,
        now: { clock?.now ?? Date(timeIntervalSince1970: 1_800_000_000) }, batchSize: batchSize, isPaused: isPaused)
    return Rig(directory: directory, outgoing: outgoing, index: index, library: library, transfer: transfer, uploads: uploads, coordinator: coordinator)
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
    func testEveryFileGoesUpNewestFirstAndThePassEndsWhenTheServerHasThemAll() async throws {
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
        XCTAssertEqual(rig.transfer.startedJobs.map(\.localIdentifier), ["item0", "item1", "item2", "item3", "item4", "item5"])
        let summary = await rig.index.summary()
        XCTAssertEqual(summary.backedUpItemCount, 6)
        XCTAssertEqual(summary.libraryItemCount, 6)
        XCTAssertEqual(summary.lastPassFinishedAt, Date(timeIntervalSince1970: 1_800_000_000))
        let token = await rig.index.changeToken
        XCTAssertEqual(token, Data("token-1".utf8))
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: rig.outgoing.path), [], "temporary files are deleted")
        let creation = try XCTUnwrap(rig.uploads.creationRequests.first)
        XCTAssertEqual(creation.value(forHTTPHeaderField: "X-Photo-Client-ID"), "local:item0")
        let sending = try XCTUnwrap(rig.transfer.startedRequests.first)
        XCTAssertEqual(sending.httpMethod, "PATCH")
        XCTAssertEqual(sending.url?.path, "/api/v1/photos/uploads/fake-1")
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
        XCTAssertEqual(lookup.askedBatches, [["local:b", "cloud-a"]], "the newest photo first")
        let summary = await rig.index.summary()
        XCTAssertEqual(summary.backedUpItemCount, 2)
    }

    func testTheServerIsAskedAboutAtMostFiveHundredNamesAtATime() async throws {
        let items = (0..<1200).map { makeItem("item\($0)", secondsSince2026: Double($0), roles: []) }
        let lookup = FakeLookup(known: [])
        let rig = try makeRig(self, items: items, lookup: lookup, batchSize: 1200)
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
        let builtBefore = rig.library.identifiersItemsWereBuiltFor.count
        rig.library.set(tokenIsExpired: true)
        let second = await coordinator.runPass(settings: settings)
        XCTAssertEqual(rig.library.listings, 2)
        XCTAssertEqual(Array(rig.library.identifiersItemsWereBuiltFor.dropFirst(builtBefore)), [], "no item is built for a photo the index has a record of")
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
            localIdentifier: "cloud", creationDate: Date(timeIntervalSince1970: 1), mediaKind: .picture,
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
            let outcome: PhotoBackupOutcome = job.localIdentifier == "a" ? .refused(failure: .fileRefused(serverReason: "not a kind of file the library keeps")) : .uploaded(serverPhotoId: "p")
            Task { await coordinator.finished(job, outcome: outcome) }
        }
        let result = await coordinator.runPass(settings: settings)
        XCTAssertEqual(result.refusedFileCount, 1)
        XCTAssertEqual(result.uploadedFileCount, 1)
        let record = await rig.index.record(forLocalIdentifier: "a")
        XCTAssertEqual(record?.roles["original"]?.refusalCount, 1)
        let summary = await rig.index.summary()
        XCTAssertEqual(summary.lastFailure, .fileRefused(serverReason: "not a kind of file the library keeps"))
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
            Task { await coordinator.finished(job, outcome: .retryLater(failure: .systemDescription("The network connection was lost."), isConnectivityFailure: true)) }
        }
        let first = await coordinator.runPass(settings: settings)
        XCTAssertEqual(first.uploadedFileCount, 0)
        let summary = await rig.index.summary()
        XCTAssertEqual(summary.backedUpItemCount, 0)
        XCTAssertNil(summary.lastFailure, "a lost connection is not shown as an error")
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p")) }
        }
        let second = await coordinator.runPass(settings: settings)
        XCTAssertEqual(second.uploadedFileCount, 1)
    }

    func testAPassThatLeavesAFileForLaterEndsWaitingForTheNetworkNotIdle() async throws {
        let rig = try makeRig(self, items: [makeItem("a", secondsSince2026: 1), makeItem("b", secondsSince2026: 2)])
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            let outcome: PhotoBackupOutcome =
                job.localIdentifier == "a"
                ? .retryLater(failure: .systemDescription("The network connection was lost."), isConnectivityFailure: true) : .uploaded(serverPhotoId: "p")
            Task { await coordinator.finished(job, outcome: outcome) }
        }
        let first = await coordinator.runPass(settings: settings)
        XCTAssertEqual(first.uploadedFileCount, 1)
        XCTAssertEqual(first.leftForLaterFileCount, 1)
        var status = await coordinator.status()
        XCTAssertEqual(status.phase, .waitingForNetwork, "not idle, which the screen reads as everything being backed up")
        XCTAssertEqual(status.leftForLaterFileCount, 1)
        XCTAssertEqual(status.backedUpItemCount, 1)

        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p")) }
        }
        let second = await coordinator.runPass(settings: settings)
        XCTAssertEqual(second.leftForLaterFileCount, 0)
        status = await coordinator.status()
        XCTAssertEqual(status.phase, .idle)
        XCTAssertEqual(status.leftForLaterFileCount, 0)
        XCTAssertEqual(status.backedUpItemCount, 2)
    }

    func testAServerTroubleLeavesTheFileForLaterAndShowsTheServersReason() async throws {
        let rig = try makeRig(self, items: [makeItem("a")])
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .retryLater(failure: .serverReason("photo storage is unavailable"), isConnectivityFailure: false)) }
        }
        let result = await coordinator.runPass(settings: settings)
        XCTAssertEqual(result.leftForLaterFileCount, 1)
        let status = await coordinator.status()
        XCTAssertEqual(status.phase, .waitingForNetwork)
        XCTAssertEqual(status.lastFailure, .serverReason("photo storage is unavailable"))
    }

    func testAFailureThatArrivesAfterThePassEndedPutsTheStatusOnWaiting() async throws {
        let rig = try makeRig(self, items: [makeItem("a")])
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p")) }
        }
        _ = await coordinator.runPass(settings: settings)
        let late = PhotoBackupJob(
            accountKey: "photos.example.com|robin", localIdentifier: "b", role: .original, clientPhotoId: "local:b", filename: "IMG_2.HEIC",
            assetModificationDate: nil, outgoingFilename: "late", sizeBytes: 10)
        await coordinator.finished(late, outcome: .retryLater(failure: nil, isConnectivityFailure: true))
        let status = await coordinator.status()
        XCTAssertEqual(status.phase, .waitingForNetwork)
        XCTAssertEqual(status.leftForLaterFileCount, 1)
    }

    func testFilesNoQueuedUploadRefersToAreDeletedWhenAPassStarts() async throws {
        let rig = try makeRig(self, items: [])
        try Data(count: 10).write(to: rig.outgoing.appendingPathComponent("left-by-a-killed-pass"))
        try Data(count: 10).write(to: rig.outgoing.appendingPathComponent("in-the-session"))
        let queued = PhotoBackupJob(
            accountKey: "photos.example.com|robin", localIdentifier: "q", role: .original, clientPhotoId: "local:q", filename: "IMG_1.HEIC",
            assetModificationDate: nil, outgoingFilename: "in-the-session", sizeBytes: 10)
        try await rig.transfer.start(
            request: URLRequest(url: URL(string: "https://photos.example.com")!), job: queued, fileURL: rig.outgoing.appendingPathComponent("in-the-session"))
        let coordinator = rig.coordinator
        let pass = Task { await coordinator.runPass(settings: settings) }
        try await waitUntil("the leftover is deleted") {
            !FileManager.default.fileExists(atPath: rig.outgoing.appendingPathComponent("left-by-a-killed-pass").path)
        }
        XCTAssertTrue(FileManager.default.fileExists(atPath: rig.outgoing.appendingPathComponent("in-the-session").path), "a file a queued upload reads from stays")
        rig.transfer.release(queued)
        await coordinator.finished(queued, outcome: .uploaded(serverPhotoId: "p"))
        _ = await pass.value
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: rig.outgoing.path), [])
    }

    func testAnAnswerWithARetryAfterHoldsBackTheNextFilesUntilThatTime() async throws {
        let rig = try makeRig(self, items: [makeItem("a", secondsSince2026: 100), makeItem("b", secondsSince2026: 90), makeItem("c", secondsSince2026: 80)],
            budget: PhotoBackupBudget(maximumUploadCount: 1, maximumOutstandingBytes: 1 << 30))
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .retryLater(failure: .serverReason("busy"), isConnectivityFailure: false, retryAfterSeconds: 60)) }
        }
        let result = await coordinator.runPass(settings: settings)
        XCTAssertEqual(rig.transfer.startedJobs.count, 1, "nothing more is queued once the server asked to wait")
        XCTAssertEqual(result.leftForLaterFileCount, 3)
        let retryTime = await coordinator.pendingRetryTime
        XCTAssertEqual(retryTime, Date(timeIntervalSince1970: 1_800_000_060))
    }

    func testAnUploadThatFinishesDuringAPassIsNotWrittenAtOnceButWhenThePassEnds() async throws {
        let rig = try makeRig(self, items: [makeItem("a", secondsSince2026: 100), makeItem("b", secondsSince2026: 90)])
        let coordinator = rig.coordinator
        // Both files stay with the session, so the pass is still running, waiting for them.
        let pass = Task { await coordinator.runPass(settings: settings) }
        try await waitUntil("both files queued") { rig.transfer.startedJobs.count == 2 }
        let first = try XCTUnwrap(rig.transfer.startedJobs.first { $0.localIdentifier == "a" })
        rig.transfer.release(first)
        await coordinator.finished(first, outcome: .uploaded(serverPhotoId: "p-a"))
        let record = await rig.index.record(forLocalIdentifier: "a")
        XCTAssertEqual(record?.serverPhotoId, "p-a")
        let isRunning = await coordinator.isPassRunning
        XCTAssertTrue(isRunning, "the pass is still running")
        // The clock does not move, so the thirty seconds between writes have not passed: the file is not rewritten for this upload.
        let indexFile = rig.directory.appendingPathComponent(PhotoBackupIndex.indexFilename)
        let textDuringPass = (try? String(contentsOf: indexFile, encoding: .utf8)) ?? ""
        XCTAssertFalse(textDuringPass.contains("p-a"), "a pass does not rewrite the index for every upload")
        await coordinator.stop()
        for job in rig.transfer.startedJobs { rig.transfer.release(job) }
        _ = await pass.value
        let textAfterPass = try String(contentsOf: indexFile, encoding: .utf8)
        XCTAssertTrue(textAfterPass.contains("p-a"), "the end of the pass writes what is left")
    }

    func testTheHiddenAlbumSettingIsUsedWhenAFileIsExported() async throws {
        for includesHidden in [false, true] {
            let rig = try makeRig(self, items: [makeItem("a")])
            let coordinator = rig.coordinator
            rig.transfer.onStart = { job in
                rig.transfer.release(job)
                Task { await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p")) }
            }
            _ = await coordinator.runPass(settings: PhotoBackupCoordinator.Settings(usesMobileData: false, includesHidden: includesHidden))
            XCTAssertEqual(rig.library.exportsIncludedHidden, [includesHidden])
        }
    }

    func testUploadsAlreadyInTheSessionAreNotQueuedAgain() async throws {
        let rig = try makeRig(self, items: [makeItem("a", secondsSince2026: 1), makeItem("b", secondsSince2026: 2)])
        let existing = PhotoBackupJob(
            accountKey: "photos.example.com|robin", localIdentifier: "a", role: .original, clientPhotoId: "local:a", filename: "a.heic",
            assetModificationDate: nil, outgoingFilename: "left-over", sizeBytes: 10)
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
            assetModificationDate: nil, outgoingFilename: "temporary", sizeBytes: 3)
        await rig.coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p1"))
        let record = await rig.index.record(forLocalIdentifier: "a")
        XCTAssertEqual(record?.serverPhotoId, "p1")
        XCTAssertFalse(FileManager.default.fileExists(atPath: file.path))
    }

    func testTheJobSurvivesBeingWrittenIntoATaskDescription() throws {
        let job = PhotoBackupJob(
            accountKey: "photos.example.com|robin", localIdentifier: "A1B2/L0/001", role: .liveVideo, clientPhotoId: "local:A1B2/L0/001",
            filename: "IMG_0001.MOV", assetModificationDate: Date(timeIntervalSince1970: 1_767_225_600), outgoingFilename: "5E2F", sizeBytes: 1234)
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
        XCTAssertTrue(statuses.contains { $0.plannedFileCount == 2 })
        XCTAssertTrue(statuses.contains { $0.listedItemCount == 2 }, "the listing says how far it has got")
        XCTAssertEqual(statuses.last?.phase, .idle)
        XCTAssertEqual(statuses.last?.backedUpItemCount, 2)
        XCTAssertEqual(statuses.last?.finishedFileCount, 2)
        XCTAssertEqual(statuses.last?.fractionBackedUp, 1)
    }

    func testAFinishedUploadIsWrittenAtOnceWhenNoPassRuns() async throws {
        let directory = try temporaryDirectory(self)
        let clock = ManualClock()
        let index = try PhotoBackupIndex(directory: directory, now: { clock.now })
        let rig = try makeRig(self, items: [], index: index, directory: directory)
        let job = PhotoBackupJob(
            accountKey: "photos.example.com|robin", localIdentifier: "a", role: .original, clientPhotoId: "cloud-a", filename: "a.heic",
            assetModificationDate: nil, outgoingFilename: "temporary", sizeBytes: 3)
        await rig.coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p1"))
        // The app may be suspended now, before the timer of the index would write.
        let reopened = try PhotoBackupIndex(directory: directory)
        let record = await reopened.record(forLocalIdentifier: "a")
        XCTAssertEqual(record?.serverPhotoId, "p1")
    }

    func testACancelledUploadChangesNothingAndLeavesTheSignInFlagAlone() async throws {
        let rig = try makeRig(self, items: [makeItem("a"), makeItem("b", secondsSince2026: 1)], budget: PhotoBackupBudget(maximumUploadCount: 1, maximumOutstandingBytes: 1_000))
        let coordinator = rig.coordinator
        let pass = Task { await coordinator.runPass(settings: settings) }
        try await waitUntil("the first upload queued") { rig.transfer.startedJobs.count == 1 }
        let queued = try XCTUnwrap(rig.transfer.startedJobs.first)
        await rig.index.setError(failure: .signInRefused, isSignInNeeded: true)
        // The same file queued again by a later pass is not undone by the late report of the first.
        let earlier = PhotoBackupJob(
            accountKey: queued.accountKey, localIdentifier: queued.localIdentifier, role: queued.role, clientPhotoId: queued.clientPhotoId,
            filename: queued.filename, assetModificationDate: nil, outgoingFilename: "an-earlier-export", sizeBytes: 10)
        await coordinator.finished(earlier, outcome: .cancelled)
        var status = await coordinator.status()
        XCTAssertEqual(status.queuedFileCount, 1)
        XCTAssertEqual(status.leftForLaterFileCount, 0, "a cancelled upload is not counted")
        let summary = await rig.index.summary()
        XCTAssertTrue(summary.isSignInNeeded)
        XCTAssertEqual(summary.refusedFileCount, 0)
        await coordinator.stop()
        _ = await pass.value
        rig.transfer.release(queued)
        await coordinator.finished(queued, outcome: .cancelled)
        status = await coordinator.status()
        XCTAssertEqual(status.queuedFileCount, 0)
        XCTAssertEqual(status.leftForLaterFileCount, 0)
        let after = await rig.index.summary()
        XCTAssertNil(after.lastFailure.flatMap { $0 == .systemDescription("cancelled") ? $0 : nil })
        let pending = await rig.index.pendingLocalIdentifiers()
        XCTAssertEqual(pending.count, 2, "what was cancelled is still to send")
    }

    func testAPassThatHearsNothingForAWhileEndsAndLeavesTheUploadsToTheSession() async throws {
        let rig = try makeRig(self, items: [makeItem("a", secondsSince2026: 1), makeItem("b", secondsSince2026: 2)], idleTimeoutSeconds: 0.15)
        let result = await rig.coordinator.runPass(settings: settings)
        XCTAssertFalse(result.wasStopped)
        XCTAssertEqual(rig.transfer.startedJobs.count, 2, "everything was queued")
        let status = await rig.coordinator.status()
        XCTAssertEqual(status.queuedFileCount, 2, "the uploads stay with the session")
        XCTAssertEqual(status.phase, .uploading)
        let isRunning = await rig.coordinator.isPassRunning
        XCTAssertFalse(isRunning)
        let summary = await rig.index.summary()
        XCTAssertNil(summary.lastPassFinishedAt, "a pass that left uploads running did not finish")
        // They report whenever the session finishes them.
        for job in rig.transfer.startedJobs { await rig.coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p")) }
        let done = await rig.index.summary()
        XCTAssertEqual(done.backedUpItemCount, 2)
    }

    func testAnIdlePassSaysWaitingForTheNetworkWhenTheNetworkDoesNotLetUploadsRun() async throws {
        for network in [FakeNetwork(isExpensiveNetwork: false, isReachable: false), FakeNetwork(isExpensiveNetwork: true, isReachable: true)] {
            let rig = try makeRig(self, items: [makeItem("a")], network: network, idleTimeoutSeconds: 0.1)
            let result = await rig.coordinator.runPass(settings: settings)
            XCTAssertEqual(result.leftForLaterFileCount, 1, "the queued upload that cannot run is left for later")
            let status = await rig.coordinator.status()
            XCTAssertEqual(status.phase, .waitingForNetwork)
            XCTAssertEqual(status.leftForLaterFileCount, 1)
            XCTAssertEqual(status.queuedFileCount, 1)
        }
        // Mobile data allowed: the uploads are not waiting for the network.
        let rig = try makeRig(self, items: [makeItem("a")], network: FakeNetwork(isExpensiveNetwork: true), idleTimeoutSeconds: 0.1)
        _ = await rig.coordinator.runPass(settings: PhotoBackupCoordinator.Settings(usesMobileData: true, includesHidden: false))
        let status = await rig.coordinator.status()
        XCTAssertEqual(status.phase, .uploading)
    }

    func testAPassWaitingForRoomEndsWhenNothingFinishesAndLeavesTheRestPending() async throws {
        let rig = try makeRig(
            self, items: (0..<6).map { makeItem("item\($0)", secondsSince2026: Double($0)) },
            budget: PhotoBackupBudget(maximumUploadCount: 2, maximumOutstandingBytes: 1_000), idleTimeoutSeconds: 0.15)
        let result = await rig.coordinator.runPass(settings: settings)
        XCTAssertEqual(rig.transfer.startedJobs.count, 2)
        XCTAssertEqual(result.deferredFileCount, 4)
        XCTAssertEqual(result.leftForLaterFileCount, 4)
        let pending = await rig.index.pendingLocalIdentifiers()
        XCTAssertEqual(pending.count, 6)
        let status = await rig.coordinator.status()
        XCTAssertEqual(status.phase, .uploading, "the network lets the queued uploads run; the rest follows as they finish, not Wi-Fi")
        XCTAssertEqual(status.queuedFileCount, 2)
        XCTAssertEqual(status.leftForLaterFileCount, 4)
    }

    func testAfterAFewServerFailuresInARowNothingMoreIsQueued() async throws {
        let rig = try makeRig(
            self, items: (0..<10).map { makeItem("item\($0)", secondsSince2026: Double($0)) },
            budget: PhotoBackupBudget(maximumUploadCount: 1, maximumOutstandingBytes: 1_000))
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .retryLater(failure: .serverReason("photo storage is unavailable"), isConnectivityFailure: false)) }
        }
        let result = await coordinator.runPass(settings: settings)
        XCTAssertEqual(rig.transfer.startedJobs.count, PhotoBackupCoordinator.serverFailureLimit)
        XCTAssertEqual(result.leftForLaterFileCount, 10)
        XCTAssertEqual(result.uploadedFileCount, 0)
        let status = await coordinator.status()
        XCTAssertEqual(status.phase, .waitingForNetwork)
        XCTAssertEqual(status.lastFailure, .serverReason("photo storage is unavailable"))
        let pending = await rig.index.pendingLocalIdentifiers()
        XCTAssertEqual(pending.count, 10)
    }

    func testAnUploadThatWorksStartsTheServerFailureCountAgain() async throws {
        let rig = try makeRig(
            self, items: (0..<8).map { makeItem("item\($0)", secondsSince2026: Double($0)) },
            budget: PhotoBackupBudget(maximumUploadCount: 1, maximumOutstandingBytes: 1_000))
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            // Two failures, then one that works, over and over: never three in a row.
            let number = Int(job.localIdentifier.dropFirst(4)) ?? 0
            let outcome: PhotoBackupOutcome =
                number % 3 == 2 ? .uploaded(serverPhotoId: "p") : .retryLater(failure: .serverReason("busy"), isConnectivityFailure: false)
            Task { await coordinator.finished(job, outcome: outcome) }
        }
        _ = await coordinator.runPass(settings: settings)
        XCTAssertEqual(rig.transfer.startedJobs.count, 8)
    }

    func testAConnectionFailureDoesNotCountAsTheServerFailing() async throws {
        let rig = try makeRig(
            self, items: (0..<6).map { makeItem("item\($0)", secondsSince2026: Double($0)) },
            budget: PhotoBackupBudget(maximumUploadCount: 1, maximumOutstandingBytes: 1_000))
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .retryLater(failure: .systemDescription("offline"), isConnectivityFailure: true)) }
        }
        _ = await coordinator.runPass(settings: settings)
        XCTAssertEqual(rig.transfer.startedJobs.count, 6)
    }

    func testTheNetworkIsAskedOnceInAPassHoweverManyFilesWaitOnIt() async throws {
        let cloudOnly = (0..<5).map { number in
            PhotoBackupItem(
                localIdentifier: "cloud\(number)", creationDate: Date(timeIntervalSince1970: Double(number)), mediaKind: .picture,
                resources: [PhotoBackupResource(role: .original, originalFilename: "IMG.HEIC", contentType: "image/heic", isLocallyAvailable: false)])
        }
        let network = FakeNetwork(isExpensiveNetwork: true)
        let rig = try makeRig(self, items: cloudOnly, network: network)
        let result = await rig.coordinator.runPass(settings: settings)
        XCTAssertEqual(result.deferredFileCount, 5)
        XCTAssertEqual(network.questions.total, 1)
    }

    func testAFileKnownToBeTooLargeForTheRoomLeftIsNotExportedToFindOut() async throws {
        let rig = try makeRig(
            self, items: [makeItem("a", secondsSince2026: 1, sizeBytes: 80), makeItem("b", secondsSince2026: 2, sizeBytes: 80)],
            budget: PhotoBackupBudget(maximumUploadCount: 4, maximumOutstandingBytes: 100))
        rig.library.set(bytes: 80, for: "a|original")
        rig.library.set(bytes: 80, for: "b|original")
        let coordinator = rig.coordinator
        let pass = Task { await coordinator.runPass(settings: settings) }
        try await waitUntil("the first upload queued") { rig.transfer.startedJobs.count == 1 }
        try await Task.sleep(nanoseconds: 150_000_000)
        XCTAssertEqual(rig.library.exportCount, 1, "the second file is not exported while there is no room for it")
        let first = try XCTUnwrap(rig.transfer.startedJobs.first)
        rig.transfer.release(first)
        await coordinator.finished(first, outcome: .uploaded(serverPhotoId: "p"))
        try await waitUntil("the second upload queued") { rig.transfer.startedJobs.count == 2 }
        let second = try XCTUnwrap(rig.transfer.startedJobs.last)
        rig.transfer.release(second)
        await coordinator.finished(second, outcome: .uploaded(serverPhotoId: "p"))
        let result = await pass.value
        XCTAssertEqual(result.uploadedFileCount, 2)
        XCTAssertEqual(rig.library.exportCount, 2)
    }

    func testAFileOfUnknownSizeHeldBackIsExportedOnceNotAtEveryFinish() async throws {
        let rig = try makeRig(
            self, items: [makeItem("a", secondsSince2026: 3), makeItem("c", secondsSince2026: 2), makeItem("b", secondsSince2026: 1)],
            budget: PhotoBackupBudget(maximumUploadCount: 4, maximumOutstandingBytes: 100))
        rig.library.set(bytes: 60, for: "a|original")
        rig.library.set(bytes: 30, for: "c|original")
        rig.library.set(bytes: 60, for: "b|original")
        let coordinator = rig.coordinator
        let pass = Task { await coordinator.runPass(settings: settings) }
        try await waitUntil("two uploads queued and the third exported") { rig.transfer.startedJobs.count == 2 && rig.library.exportCount == 3 }
        let jobs = rig.transfer.startedJobs
        let small = try XCTUnwrap(jobs.first { $0.localIdentifier == "c" })
        rig.transfer.release(small)
        await coordinator.finished(small, outcome: .uploaded(serverPhotoId: "p"))
        try await Task.sleep(nanoseconds: 150_000_000)
        XCTAssertEqual(rig.library.exportCount, 3, "there is still no room for it, and it is not exported again to find out")
        let large = try XCTUnwrap(jobs.first { $0.localIdentifier == "a" })
        rig.transfer.release(large)
        await coordinator.finished(large, outcome: .uploaded(serverPhotoId: "p"))
        try await waitUntil("the held back file queued") { rig.transfer.startedJobs.count == 3 }
        XCTAssertEqual(rig.library.exportCount, 4)
        let last = try XCTUnwrap(rig.transfer.startedJobs.last)
        rig.transfer.release(last)
        await coordinator.finished(last, outcome: .uploaded(serverPhotoId: "p"))
        let result = await pass.value
        XCTAssertEqual(result.uploadedFileCount, 3)
    }

    func testAnExportThatFailsForWantOfRoomOrALockedPhoneIsShown() async throws {
        let rig = try makeRig(self, items: [makeItem("a")])
        rig.library.set(unavailable: "a|original", true)
        _ = await rig.coordinator.runPass(settings: settings)
        let summary = await rig.index.summary()
        XCTAssertEqual(summary.lastFailure, .systemDescription("in iCloud"))
    }

    func testAFileWaitingForICloudOnMobileDataIsNotShownAsAProblem() async throws {
        let rig = try makeRig(self, items: [makeItem("a")])
        rig.library.set(unavailable: "a|original", true, failure: .fileIsInICloudOnMobileData)
        _ = await rig.coordinator.runPass(settings: settings)
        let summary = await rig.index.summary()
        XCTAssertNil(summary.lastFailure)
        let status = await rig.coordinator.status()
        XCTAssertEqual(status.phase, .waitingForNetwork)
    }
}

/// What keeps the backup going after the person leaves the app, and what stops
/// it from stalling while the app is open.
final class PhotoBackupContinuityTests: XCTestCase {
    /// A pass over `count` photos, a budget of `budgetCount` files, and a short idle
    /// wait, so that it ends with `budgetCount` uploads left to the session.
    private func idleEndedPass(count: Int, budgetCount: Int) async throws -> (Rig, PhotoBackupPassResult) {
        let rig = try makeRig(
            self, items: (0..<count).map { makeItem("item\($0)", secondsSince2026: Double($0)) },
            budget: PhotoBackupBudget(maximumUploadCount: budgetCount, maximumOutstandingBytes: 1 << 30), idleTimeoutSeconds: 0.1)
        let result = await rig.coordinator.runPass(settings: settings)
        return (rig, result)
    }

    func testACompletionAfterAPassEndedIdleSaysToStartTheNextPass() async throws {
        let (rig, result) = try await idleEndedPass(count: 6, budgetCount: 2)
        XCTAssertEqual(result.uploadedFileCount, 0, "nothing finished while the pass waited")
        XCTAssertEqual(rig.transfer.startedJobs.count, 2)
        let isRunning = await rig.coordinator.isPassRunning
        XCTAssertFalse(isRunning, "the pass ended and left the uploads to the session")
        // The session reports the first upload long after the pass ended.
        let first = try XCTUnwrap(rig.transfer.startedJobs.first)
        rig.transfer.release(first)
        let shouldStart = await rig.coordinator.finished(first, outcome: .uploaded(serverPhotoId: "p"))
        XCTAssertTrue(shouldStart, "files are still to send, so the app starts the next pass")
        // That pass queues the rest as the uploads finish.
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p")) }
        }
        let second = try XCTUnwrap(rig.transfer.startedJobs.last)
        rig.transfer.release(second)
        await coordinator.finished(second, outcome: .uploaded(serverPhotoId: "p"))
        let next = await coordinator.runPass(settings: settings)
        XCTAssertEqual(next.uploadedFileCount, 4)
        let summary = await rig.index.summary()
        XCTAssertEqual(summary.backedUpItemCount, 6)
    }

    func testTheLastUploadOfTheLibraryDoesNotAskForAnotherPass() async throws {
        let (rig, _) = try await idleEndedPass(count: 2, budgetCount: 2)
        let jobs = rig.transfer.startedJobs
        XCTAssertEqual(jobs.count, 2)
        rig.transfer.release(jobs[0])
        rig.transfer.release(jobs[1])
        let afterFirst = await rig.coordinator.finished(jobs[0], outcome: .uploaded(serverPhotoId: "p"))
        XCTAssertFalse(afterFirst, "the other file is already with the session, nothing is left to queue")
        let afterLast = await rig.coordinator.finished(jobs[1], outcome: .uploaded(serverPhotoId: "p"))
        XCTAssertFalse(afterLast, "everything is up")
    }

    func testARefusedFileAndAFileToRetryDoNotStartAPass() async throws {
        let (rig, _) = try await idleEndedPass(count: 6, budgetCount: 3)
        let jobs = rig.transfer.startedJobs
        for job in jobs { rig.transfer.release(job) }
        let refused = await rig.coordinator.finished(jobs[0], outcome: .refused(failure: .fileRefused(serverReason: "not a picture")))
        XCTAssertFalse(refused, "a refused file must not start passes as fast as the server refuses")
        let retry = await rig.coordinator.finished(
            jobs[1], outcome: .retryLater(failure: .serverReason("busy"), isConnectivityFailure: false, retryAfterSeconds: 60))
        XCTAssertFalse(retry)
        let cancelled = await rig.coordinator.finished(jobs[2], outcome: .cancelled)
        XCTAssertFalse(cancelled)
    }

    func testAnUploadThatWorksEndsAServerWaitAndAsksForAPassButTheWaitItselfDoesNot() async throws {
        let clock = ManualClock()
        let rig = try makeRig(
            self, items: (0..<6).map { makeItem("item\($0)", secondsSince2026: Double($0)) },
            budget: PhotoBackupBudget(maximumUploadCount: 2, maximumOutstandingBytes: 1 << 30), idleTimeoutSeconds: 0.1, clock: clock)
        _ = await rig.coordinator.runPass(settings: settings)
        let jobs = rig.transfer.startedJobs
        rig.transfer.release(jobs[0])
        rig.transfer.release(jobs[1])
        let afterRetry = await rig.coordinator.finished(
            jobs[0], outcome: .retryLater(failure: .serverReason("busy"), isConnectivityFailure: false, retryAfterSeconds: 120))
        XCTAssertFalse(afterRetry)
        var retryTime = await rig.coordinator.pendingRetryTime
        XCTAssertEqual(retryTime, clock.now.addingTimeInterval(120), "the app starts a pass at this time")
        let afterUpload = await rig.coordinator.finished(jobs[1], outcome: .uploaded(serverPhotoId: "p"))
        retryTime = await rig.coordinator.pendingRetryTime
        XCTAssertNil(retryTime, "the server is answering again, so the wait is over")
        XCTAssertTrue(afterUpload)
    }

    func testACompletionDuringARunningPassLeavesTheQueueingToThePass() async throws {
        let rig = try makeRig(self, items: (0..<6).map { makeItem("item\($0)", secondsSince2026: Double($0)) }, budget: PhotoBackupBudget(maximumUploadCount: 2, maximumOutstandingBytes: 1 << 30))
        let coordinator = rig.coordinator
        let pass = Task { await coordinator.runPass(settings: settings) }
        try await waitUntil("two uploads queued") { rig.transfer.startedJobs.count == 2 }
        let job = try XCTUnwrap(rig.transfer.startedJobs.first)
        rig.transfer.release(job)
        let shouldStart = await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p"))
        XCTAssertFalse(shouldStart, "the running pass queues the next file itself")
        await coordinator.stop()
        _ = await pass.value
    }

    func testServerFailuresThatStopAPassSetATimeToTryAgainThatGrowsAndIsClearedByAnUpload() async throws {
        let clock = ManualClock()
        let rig = try makeRig(
            self, items: (0..<40).map { makeItem("item\($0)", secondsSince2026: Double($0)) },
            budget: PhotoBackupBudget(maximumUploadCount: 1, maximumOutstandingBytes: 1 << 30), clock: clock)
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .retryLater(failure: .serverReason("busy"), isConnectivityFailure: false)) }
        }
        let start = clock.now
        _ = await coordinator.runPass(settings: settings)
        var retryTime = await coordinator.pendingRetryTime
        XCTAssertEqual(retryTime, start.addingTimeInterval(60), "the first stop waits a minute")
        // The app runs a pass when that time comes; the server is still failing.
        clock.advance(seconds: 61)
        _ = await coordinator.runPass(settings: settings)
        retryTime = await coordinator.pendingRetryTime
        XCTAssertEqual(retryTime, clock.now.addingTimeInterval(120), "then twice as long")
        for _ in 0..<8 {
            clock.advance(seconds: 1_000)
            _ = await coordinator.runPass(settings: settings)
        }
        retryTime = await coordinator.pendingRetryTime
        XCTAssertEqual(retryTime, clock.now.addingTimeInterval(15 * 60), "and never more than a quarter of an hour")
        // Nothing is sent while the wait lasts.
        let sentBefore = rig.transfer.startedJobs.count
        _ = await coordinator.runPass(settings: settings)
        XCTAssertEqual(rig.transfer.startedJobs.count, sentBefore)
    }

    func testAFileThatNeverComesOutOfThePhotoLibraryDoesNotHoldUpTheRest() async throws {
        let rig = try makeRig(
            self, items: (0..<4).map { makeItem("item\($0)", secondsSince2026: Double($0)) }, exportTimeoutSeconds: 0.15)
        rig.library.set(hung: "item3|original")
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p")) }
        }
        let result = await coordinator.runPass(settings: settings)
        XCTAssertEqual(rig.transfer.startedJobs.map(\.localIdentifier), ["item2", "item1", "item0"], "the pass went on past the stuck file")
        XCTAssertEqual(result.deferredFileCount, 1)
        XCTAssertEqual(result.leftForLaterFileCount, 1)
        let summary = await rig.index.summary()
        XCTAssertEqual(summary.lastFailure, .fileTookTooLongToRead)
        let pending = await rig.index.pendingLocalIdentifiers()
        XCTAssertEqual(pending, ["item3"], "the file is tried again by a later pass")
    }

    func testStoppingEndsAPassThatIsWaitingForAFileFromThePhotoLibrary() async throws {
        let rig = try makeRig(self, items: [makeItem("a")], exportTimeoutSeconds: 3_600)
        rig.library.set(hung: "a|original")
        let coordinator = rig.coordinator
        let pass = Task { await coordinator.runPass(settings: settings) }
        try await waitUntil("the export began") { rig.library.exportCount == 1 }
        await coordinator.stop()
        let result = await pass.value
        XCTAssertTrue(result.wasStopped)
        let isRunning = await coordinator.isPassRunning
        XCTAssertFalse(isRunning)
    }

    func testFilesOnThePhoneAreQueuedBeforeFilesInICloud() async throws {
        let items = [
            makeItem("cloudOld", secondsSince2026: 1, isLocallyAvailable: false),
            makeItem("phoneA", secondsSince2026: 2),
            makeItem("cloudNew", secondsSince2026: 3, isLocallyAvailable: false),
            makeItem("phoneB", secondsSince2026: 4),
        ]
        let rig = try makeRig(self, items: items, idleTimeoutSeconds: 0.1)
        _ = await rig.coordinator.runPass(settings: PhotoBackupCoordinator.Settings(usesMobileData: true, includesHidden: false))
        XCTAssertEqual(rig.transfer.startedJobs.map(\.localIdentifier), ["phoneB", "phoneA", "cloudNew", "cloudOld"])
    }

    func testTheQueueStopsGrowingWhenTheFreeSpaceIsLowAndFreeSpaceIsReadBeforeEachFile() async throws {
        let questions = NetworkQuestions()
        let rig = try makeRig(
            self, items: (0..<6).map { makeItem("item\($0)", secondsSince2026: Double($0)) }, budget: .standard, idleTimeoutSeconds: 0.1,
            freeSpaceBytes: {
                questions.add()
                return 800
            })
        for number in 0..<6 { rig.library.set(bytes: 100, for: "item\(number)|original") }
        _ = await rig.coordinator.runPass(settings: settings)
        // A quarter of 800 bytes is 200: two files of 100 fit, a third does not.
        XCTAssertEqual(rig.transfer.startedJobs.count, 2)
        XCTAssertGreaterThanOrEqual(questions.total, 3, "asked again before each file")
        let roomy = try makeRig(self, items: (0..<6).map { makeItem("item\($0)", secondsSince2026: Double($0)) }, budget: .standard, idleTimeoutSeconds: 0.1)
        _ = await roomy.coordinator.runPass(settings: settings)
        XCTAssertEqual(roomy.transfer.startedJobs.count, 6, "with room the whole library is queued at once")
    }

    // MARK: Waking in the background

    private func pendingRig(
        sizes: [String: Int64], clock: ManualClock? = nil, budget: PhotoBackupBudget = .standard, notLocal: Set<String> = []
    ) async throws -> Rig {
        let items = sizes.keys.sorted().enumerated().map {
            makeItem($0.element, secondsSince2026: Double($0.offset), sizeBytes: sizes[$0.element], isLocallyAvailable: !notLocal.contains($0.element))
        }
        let rig = try makeRig(self, items: items, budget: budget, clock: clock)
        for (name, size) in sizes { rig.library.set(bytes: size, for: "\(name)|original") }
        // What an earlier pass leaves: every file noted as still to send.
        await rig.index.adopt(accountKey: "photos.example.com|robin")
        await rig.index.markPending(await rig.index.plan(items: items))
        return rig
    }

    func testAWakeQueuesTheSmallestFilesOnThePhoneFirstAndNeverTouchesICloud() async throws {
        let rig = try await pendingRig(
            sizes: ["big": 900, "small": 10, "middle": 100, "cloud": 5, "tiny": 1],
            budget: PhotoBackupBudget(maximumUploadCount: 3, maximumOutstandingBytes: 1 << 30), notLocal: ["cloud"])
        let queuedCount = await rig.coordinator.topUp(settings: PhotoBackupCoordinator.Settings(usesMobileData: true, includesHidden: false), until: Date(timeIntervalSince1970: 1_800_000_060))
        XCTAssertEqual(queuedCount, 3)
        XCTAssertEqual(rig.transfer.startedJobs.map(\.localIdentifier), ["tiny", "small", "middle"], "smallest first, up to the budget")
        XCTAssertFalse(rig.library.exportedUploadKeys.contains("cloud|original"), "a file in iCloud is not exported in a short wake")
        XCTAssertEqual(rig.library.exportsAllowedExpensiveNetwork, [false, false, false], "no network that costs is used for a wake")
        let isRunning = await rig.coordinator.isPassRunning
        XCTAssertFalse(isRunning)
        let files = try FileManager.default.contentsOfDirectory(atPath: rig.outgoing.path)
        XCTAssertEqual(files.count, 3)
    }

    func testAWakeStopsAtItsDeadline() async throws {
        let clock = ManualClock()
        let rig = try await pendingRig(sizes: ["a": 1, "b": 2, "c": 3, "d": 4, "e": 5, "f": 6], clock: clock)
        rig.library.set(secondsPerExport: 10)
        let queuedCount = await rig.coordinator.topUp(settings: settings, until: clock.now.addingTimeInterval(25))
        XCTAssertEqual(queuedCount, 3, "files are exported at 0, 10 and 20 seconds, and the deadline is at 25")
        XCTAssertEqual(rig.transfer.startedJobs.count, 3)
        // Nothing past the deadline: the next wake carries on.
        let again = await rig.coordinator.topUp(settings: settings, until: clock.now.addingTimeInterval(25))
        XCTAssertEqual(again, 3)
        XCTAssertEqual(rig.transfer.startedJobs.map(\.localIdentifier), ["a", "b", "c", "d", "e", "f"])
    }

    func testAWakeGivesUpOnAFileThatOutlastsItsDeadlineAndGoesOnToTheNext() async throws {
        let clock = ManualClock()
        let rig = try await pendingRig(sizes: ["a": 1, "b": 2], clock: clock)
        rig.library.set(hung: "a|original")
        let started = Date()
        let queuedCount = await rig.coordinator.topUp(settings: settings, until: clock.now.addingTimeInterval(0.2))
        XCTAssertLessThan(Date().timeIntervalSince(started), 3, "a stuck file does not hold the wake past its deadline")
        XCTAssertEqual(queuedCount, 1)
        XCTAssertEqual(rig.transfer.startedJobs.map(\.localIdentifier), ["b"])
    }

    func testAWakeDoesNothingWhileAPassRunsOrWhenSigningInIsNeededOrAWaitIsOn() async throws {
        let rig = try await pendingRig(sizes: ["a": 1, "b": 2])
        let coordinator = rig.coordinator
        let pass = Task { await coordinator.runPass(settings: settings) }
        try await waitUntil("the pass queued the files") { rig.transfer.startedJobs.count == 2 }
        let duringPass = await coordinator.topUp(settings: settings, until: Date(timeIntervalSince1970: 1_800_000_060))
        XCTAssertEqual(duringPass, 0, "a running pass queues the files itself")
        await coordinator.stop()
        _ = await pass.value

        let signedOut = try await pendingRig(sizes: ["a": 1])
        await signedOut.index.setError(failure: .signInRefused, isSignInNeeded: true)
        let afterRefusal = await signedOut.coordinator.topUp(settings: settings, until: Date(timeIntervalSince1970: 1_800_000_060))
        XCTAssertEqual(afterRefusal, 0)
        XCTAssertEqual(signedOut.transfer.startedJobs.count, 0)

        let waiting = try await pendingRig(sizes: ["a": 1, "b": 2, "c": 3], budget: PhotoBackupBudget(maximumUploadCount: 1, maximumOutstandingBytes: 1 << 30))
        let queued = await waiting.coordinator.topUp(settings: settings, until: Date(timeIntervalSince1970: 1_800_000_060))
        XCTAssertEqual(queued, 1)
        let again = await waiting.coordinator.topUp(settings: settings, until: Date(timeIntervalSince1970: 1_800_000_060))
        XCTAssertEqual(again, 0, "the queue is full, and what the session holds is not queued twice")
        XCTAssertEqual(waiting.transfer.startedJobs.count, 1)
    }

    // MARK: Saying what is happening

    func testTheStatusSaysUploadsContinueInTheBackgroundAndWhatWaitsForTheApp() async throws {
        let (rig, _) = try await idleEndedPass(count: 6, budgetCount: 2)
        var status = await rig.coordinator.refreshedStatus()
        XCTAssertFalse(status.isPassRunning)
        XCTAssertEqual(status.queuedFileCount, 2, "two uploads continue with the system")
        XCTAssertEqual(status.waitingFileCount, 4, "four wait for the app to be opened or for the night")
        // The system finishes one with the app closed; a fresh start asks the session.
        let finishedJob = try XCTUnwrap(rig.transfer.startedJobs.first)
        rig.transfer.release(finishedJob)
        status = await rig.coordinator.refreshedStatus()
        XCTAssertEqual(status.queuedFileCount, 1)
        XCTAssertEqual(status.waitingFileCount, 5)
    }

    func testTheScreenSaysBackgroundUploadsAndWaitingFilesOnlyWhenNoPassWorksAndNothingElseNeedsSaying() {
        let background = PhotoBackupStatus(phase: .idle, libraryItemCount: 10, queuedFileCount: 3, waitingFileCount: 5)
        XCTAssertTrue(background.isUploadingInBackground)
        XCTAssertTrue(background.hasFilesWaitingForTheApp)
        var working = background
        working.isPassRunning = true
        XCTAssertFalse(working.isUploadingInBackground, "a pass working is shown as the pass")
        XCTAssertFalse(working.hasFilesWaitingForTheApp)
        for phase in [PhotoBackupStatus.Phase.waitingForNetwork, .needsSignIn, .needsAccess(.limited), .off, .listing] {
            var other = background
            other.phase = phase
            XCTAssertFalse(other.isUploadingInBackground, "\(phase) has words of its own")
            XCTAssertFalse(other.hasFilesWaitingForTheApp)
        }
        XCTAssertFalse(PhotoBackupStatus(phase: .idle, queuedFileCount: 0, waitingFileCount: 0).isUploadingInBackground)
        XCTAssertTrue(PhotoBackupStatus(phase: .uploading, queuedFileCount: 1).isUploadingInBackground)
    }

    func testTheNewFailureIsKeptInTheIndexLikeTheOthers() throws {
        let encoded = try JSONEncoder().encode(PhotoBackupFailure.fileTookTooLongToRead)
        XCTAssertEqual(try JSONDecoder().decode(PhotoBackupFailure.self, from: encoded), .fileTookTooLongToRead)
    }

    func testNothingWaitsForTheAppWhileAPassWorks() async throws {
        let rig = try makeRig(self, items: (0..<4).map { makeItem("item\($0)", secondsSince2026: Double($0)) }, budget: PhotoBackupBudget(maximumUploadCount: 1, maximumOutstandingBytes: 1 << 30))
        let coordinator = rig.coordinator
        let pass = Task { await coordinator.runPass(settings: settings) }
        try await waitUntil("one upload queued") { rig.transfer.startedJobs.count == 1 }
        let status = await coordinator.status()
        XCTAssertTrue(status.isPassRunning)
        XCTAssertEqual(status.waitingFileCount, 0)
        await coordinator.stop()
        _ = await pass.value
    }
}

/// Notes taken by closures the library and the transfer call while a pass runs.
private final class PassNotes: @unchecked Sendable {
    private let lock = NSLock()
    private var firstUploadBatchCountValue: Int?
    private var isTokenKeptEarlyValue = false
    private var isListingProgressMissingValue = false
    func noteFirstUpload(batchCount: Int) { lock.withLock { if firstUploadBatchCountValue == nil { firstUploadBatchCountValue = batchCount } } }
    func noteTokenKeptEarly() { lock.withLock { isTokenKeptEarlyValue = true } }
    func noteListingProgressMissing() { lock.withLock { isListingProgressMissingValue = true } }
    /// How many batches had been listed when the first upload was queued.
    var firstUploadBatchCount: Int? { lock.withLock { firstUploadBatchCountValue } }
    var isTokenKeptEarly: Bool { lock.withLock { isTokenKeptEarlyValue } }
    var isListingProgressMissing: Bool { lock.withLock { isListingProgressMissingValue } }
}

/// Items made ten seconds apart, the highest number the newest. With
/// `itemsPerMoment` above one, that many in a row share one creation date, as a
/// burst does; `undatedCount` more are named `undated0`, `undated1`, and have no
/// creation date at all.
private func makeLibraryItems(count: Int, itemsPerMoment: Int = 1, undatedCount: Int = 0) -> [PhotoBackupItem] {
    let dated = (0..<count).map { makeItem("item\($0)", secondsSince2026: Double($0 / itemsPerMoment) * 10) }
    let undated = (0..<undatedCount).map { makeItem("undated\($0)", secondsSince2026: nil) }
    return dated + undated
}

final class PhotoBackupLargeLibraryTests: XCTestCase {
    /// The most time listing 35,000 fake items through the coordinator and the
    /// index may take in this (unoptimized) test build. It takes about a second
    /// here; the bound is fifteen times that, so a slow machine passes, and a
    /// listing whose cost grows with the library at every batch (writing the
    /// whole index after each of 117 batches, or building every item at every
    /// batch) fails it.
    static let listingTimeBoundSeconds: Double = 15

    func testAThirtyFiveThousandItemLibraryUploadsAfterOneBatchAndTheTokenIsKeptAfterTheLastOne() async throws {
        let items = makeLibraryItems(count: 35_000)
        let rig = try makeRig(self, items: items, idleTimeoutSeconds: 0.05)
        let notes = PassNotes()
        let library = rig.library
        let index = rig.index
        rig.transfer.onStart = { _ in notes.noteFirstUpload(batchCount: library.batchResumePoints.count) }
        library.set(afterBatch: { batchCount in
            // Not the last batch: the library's token is not kept yet, and the
            // listing under way is.
            guard batchCount < 117 else { return }
            if await index.changeToken != nil { notes.noteTokenKeptEarly() }
            if await index.listingProgress == nil { notes.noteListingProgressMissing() }
        })
        let start = ContinuousClock.now
        let result = await rig.coordinator.runPass(settings: settings)
        let duration = start.duration(to: .now)
        let seconds = Double(duration.components.seconds) + Double(duration.components.attoseconds) / 1e18
        print("listing 35,000 items: \(seconds) seconds")

        XCTAssertEqual(notes.firstUploadBatchCount, 1, "the first upload is queued after one batch, not after the whole listing")
        XCTAssertFalse(notes.isTokenKeptEarly, "the change token is kept only after the last batch")
        XCTAssertFalse(notes.isListingProgressMissing)
        XCTAssertEqual(library.batchResumePoints.count, 117)
        XCTAssertEqual(library.listings, 1)
        let token = await rig.index.changeToken
        XCTAssertEqual(token, Data("token-1".utf8))
        let listing = await rig.index.listingProgress
        XCTAssertNil(listing, "the listing is over")
        let known = await rig.index.knownLocalIdentifiers()
        XCTAssertEqual(known.count, 35_000)
        let pendingCount = await rig.index.pendingFileCount()
        XCTAssertEqual(pendingCount, 35_000)
        XCTAssertFalse(result.wasStopped)
        XCTAssertEqual(rig.transfer.startedJobs.count, 4, "as many as the budget allows")
        XCTAssertEqual(rig.transfer.startedJobs.map(\.localIdentifier), ["item34999", "item34998", "item34997", "item34996"], "newest first")
        XCTAssertLessThan(seconds, Self.listingTimeBoundSeconds)
        XCTAssertLessThanOrEqual(library.largestItemRequestCount, 300, "no more than a batch of items is built at once")
        XCTAssertLessThanOrEqual(library.identifiersItemsWereBuiltFor.count, 35_000 + 300, "every item is built once for the listing, and one batch more to send")
    }

    func testAPassStoppedHalfwayIsFollowedByOneThatResumesWithoutListingTheEarlierItemsAgain() async throws {
        let items = makeLibraryItems(count: 3_000)
        let lookup = FakeLookup(known: [])
        let rig = try makeRig(self, items: items, lookup: lookup, idleTimeoutSeconds: 0.05)
        let coordinator = rig.coordinator
        rig.library.set(afterBatch: { batchCount in if batchCount == 4 { await coordinator.stop() } })
        let first = await coordinator.runPass(settings: settings)
        XCTAssertTrue(first.wasStopped)

        let storedProgress = await rig.index.listingProgress
        let progress = try XCTUnwrap(storedProgress)
        XCTAssertEqual(progress.listedItemCount, 1_200)
        XCTAssertEqual(progress.resumePoint, PhotoBackupResumePoint(creationDate: items[1_800].creationDate!), "the oldest item of the four batches, newest first")
        let tokenAfterFirst = await rig.index.changeToken
        XCTAssertNil(tokenAfterFirst, "the library has not been listed to the end")
        XCTAssertEqual(progress.changeToken, Data("token-1".utf8))
        let batchesInFirstPass = rig.library.batchResumePoints.count
        let askedInFirstPass = lookup.askedBatches.count

        // The phone's app is killed: a new index reads the same file, and a new
        // pass goes on from where the old one stopped.
        let reopened = try PhotoBackupIndex(directory: rig.directory)
        let reopenedProgress = await reopened.listingProgress
        XCTAssertEqual(reopenedProgress, progress)
        let second = try makeRig(self, items: items, lookup: lookup, index: reopened, directory: rig.directory, idleTimeoutSeconds: 0.05)
        second.library.set(afterBatch: nil)
        _ = await second.coordinator.runPass(settings: settings)

        XCTAssertEqual(second.library.listings, 0, "a listing under way is not begun again, so its token is the one from the start")
        let resumePoints = second.library.batchResumePoints
        XCTAssertEqual(resumePoints.count, 6, "the 1,800 items left, 300 at a time")
        XCTAssertEqual(resumePoints.first ?? nil, progress.resumePoint)
        XCTAssertTrue(resumePoints.allSatisfy { ($0?.creationDate ?? .distantFuture) <= (progress.resumePoint?.creationDate ?? .distantPast) })
        let askedInSecondPass = lookup.askedBatches.dropFirst(askedInFirstPass).flatMap { $0 }
        XCTAssertEqual(Set(askedInSecondPass), Set((0..<1_800).map { "local:item\($0)" }), "the server is asked about the items not listed yet, and no others")
        XCTAssertEqual(askedInSecondPass.count, 1_800)
        XCTAssertGreaterThan(batchesInFirstPass, 0)
        let token = await reopened.changeToken
        XCTAssertEqual(token, Data("token-1".utf8))
        let listing = await reopened.listingProgress
        XCTAssertNil(listing)
        let known = await reopened.knownLocalIdentifiers()
        XCTAssertEqual(known.count, 3_000)
    }

    func testTheTopUpAfterAWakeReadsOneBatchOfTheFilesStillToSendNotAllOfThem() async throws {
        let rig = try makeRig(self, items: makeLibraryItems(count: 2_000), idleTimeoutSeconds: 0.05)
        _ = await rig.coordinator.runPass(settings: settings)
        await rig.transfer.cancelAll()
        let builtBefore = rig.library.identifiersItemsWereBuiltFor.count
        let startedBefore = rig.transfer.startedJobs.count
        let queued = await rig.coordinator.topUp(settings: settings, until: Date(timeIntervalSince1970: 1_800_000_000).addingTimeInterval(60))
        XCTAssertEqual(queued, 4, "as many as the budget allows")
        XCTAssertEqual(rig.library.identifiersItemsWereBuiltFor.count - builtBefore, 300, "one batch of the 2,000 pending items was read")
        XCTAssertEqual(
            rig.transfer.startedJobs.dropFirst(startedBefore).map(\.localIdentifier), ["item1999", "item1998", "item1997", "item1996"], "the newest first")
    }

    func testTheStatusSaysHowFarTheListingHasGotAndTheProgressMovesWhileListing() async throws {
        let rig = try makeRig(self, items: makeLibraryItems(count: 3_000), idleTimeoutSeconds: 0.05)
        let seen = StatusLog()
        await rig.coordinator.setStatusHandler { seen.add($0) }
        _ = await rig.coordinator.runPass(settings: settings)
        let listing = seen.all.filter { $0.phase == .listing }
        let listed = listing.compactMap(\.listedItemCount)
        XCTAssertEqual(listed.first, 0)
        XCTAssertEqual(listed.last, 3_000)
        XCTAssertEqual(listed, listed.sorted(), "never goes back")
        XCTAssertGreaterThanOrEqual(Set(listed).count, 10, "once for each batch")
        XCTAssertTrue(listing.allSatisfy { $0.libraryItemCount == 3_000 || $0.libraryItemCount == 0 })
        let fractions = listing.map(\.progressFraction)
        XCTAssertEqual(fractions, fractions.sorted(), "the progress the system shows never goes back while listing")
        XCTAssertGreaterThan(fractions.last ?? 0, 0.25)
        XCTAssertGreaterThanOrEqual(Set(fractions).count, 10)
    }

    /// Finishes the first few uploads a test sees and leaves the rest held.
    private final class FirstUploadsFinisher: @unchecked Sendable {
        private let lock = NSLock()
        private var startedCount = 0
        let finishedLimit: Int
        init(finishedLimit: Int) { self.finishedLimit = finishedLimit }
        func shouldFinishNextUpload() -> Bool {
            lock.withLock {
                startedCount += 1
                return startedCount <= finishedLimit
            }
        }
    }

    func testABurstOfOneDateAcrossABatchBoundaryAndPhotosWithNoDateAreEachUploadedOnce() async throws {
        // Twelve photos in two bursts of six, and three with no date, in
        // batches of five: the newer burst is one batch of six, and the older
        // one comes with the photos without a date.
        let items = makeLibraryItems(count: 12, itemsPerMoment: 6, undatedCount: 3)
        let rig = try makeRig(self, items: items, batchSize: 5)
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p-" + job.localIdentifier)) }
        }
        let result = await coordinator.runPass(settings: settings)
        XCTAssertFalse(result.wasStopped)
        XCTAssertEqual(rig.transfer.startedJobs.map(\.localIdentifier).sorted(), items.map(\.localIdentifier).sorted(), "every item once, none twice")
        XCTAssertEqual(rig.library.batchResumePoints.count, 2, "one batch for each burst")
        XCTAssertLessThanOrEqual(rig.library.largestItemRequestCount, 5, "no more than a batch of items is built at once")
        let summary = await rig.index.summary()
        XCTAssertEqual(summary.backedUpItemCount, 15)
        let listing = await rig.index.listingProgress
        XCTAssertNil(listing, "the listing is over")
        let known = await rig.index.knownLocalIdentifiers()
        XCTAssertEqual(known.count, 15)
    }

    func testAMomentLargerThanTheBatchIsWorkedThroughABatchAtATimeAndItsResumePointIsSavedWhenWhole() async throws {
        // Eleven photos made at one instant, and two with no date, in batches of four.
        let items = makeLibraryItems(count: 11, itemsPerMoment: 11, undatedCount: 2)
        let rig = try makeRig(self, items: items, idleTimeoutSeconds: 0.05, batchSize: 4)
        let coordinator = rig.coordinator
        // The pass is stopped while the first four items are being read.
        rig.library.set(afterItems: { requestCount in if requestCount == 1 { await coordinator.stop() } })
        let first = await coordinator.runPass(settings: settings)
        XCTAssertTrue(first.wasStopped)
        let storedProgress = await rig.index.listingProgress
        let progress = try XCTUnwrap(storedProgress)
        XCTAssertNil(progress.resumePoint, "the moment is not whole, so no resume point is saved")
        XCTAssertEqual(progress.listedItemCount, 0)
        let knownAfterFirst = await rig.index.knownLocalIdentifiers()
        XCTAssertEqual(knownAfterFirst.count, 4, "the first four are in the index, and the rest of the moment is not read")
        XCTAssertEqual(rig.library.largestItemRequestCount, 4)
        let stoppedStatus = await coordinator.status()
        XCTAssertTrue(stoppedStatus.isListingUnfinished)

        // The next pass lists the moment again from the start, skipping the four.
        rig.library.set(afterItems: nil)
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p-" + job.localIdentifier)) }
        }
        let second = await coordinator.runPass(settings: settings)
        XCTAssertFalse(second.wasStopped)
        XCTAssertEqual(rig.library.batchResumePoints.count, 2)
        XCTAssertNil(rig.library.batchResumePoints[1], "asked from the start again")
        XCTAssertLessThanOrEqual(rig.library.largestItemRequestCount, 4, "a moment of eleven is built four at a time")
        XCTAssertEqual(rig.transfer.startedJobs.map(\.localIdentifier).sorted(), items.map(\.localIdentifier).sorted(), "every item once, none twice")
        let listing = await rig.index.listingProgress
        XCTAssertNil(listing)
        let endedStatus = await coordinator.status()
        XCTAssertFalse(endedStatus.isListingUnfinished)
    }

    func testAfterTheListingTheProgressIsWhatIsDoneOutOfWhatIsDoneAndWhatIsLeft() async throws {
        let rig = try makeRig(self, items: makeLibraryItems(count: 700), idleTimeoutSeconds: 0.05)
        let coordinator = rig.coordinator
        let index = rig.index
        let finisher = FirstUploadsFinisher(finishedLimit: 4)
        rig.transfer.onStart = { job in
            guard finisher.shouldFinishNextUpload() else { return }
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p-" + job.localIdentifier)) }
        }
        // The second batch is read after the first four uploads are on the server.
        rig.library.set(afterBatch: { batchCount in
            guard batchCount == 2 else { return }
            let deadline = Date().addingTimeInterval(5)
            while await index.summary().backedUpItemCount < finisher.finishedLimit, Date() < deadline {
                try? await Task.sleep(nanoseconds: 5_000_000)
            }
        })
        let seen = StatusLog()
        await coordinator.setStatusHandler { seen.add($0) }
        _ = await coordinator.runPass(settings: settings)
        let afterListing = try XCTUnwrap(seen.all.last { $0.phase == .uploading && $0.listedItemCount != nil })
        XCTAssertEqual(afterListing.finishedFileCount, 4)
        XCTAssertEqual(afterListing.plannedFileCount, 700, "four done and 696 left")
        let expected = PhotoBackupStatus.listingShareOfProgress + (1 - PhotoBackupStatus.listingShareOfProgress) * 4.0 / 700.0
        XCTAssertEqual(afterListing.progressFraction, expected, accuracy: 0.0001, "just over the share of the listing, not near the end")
        XCTAssertLessThan(afterListing.progressFraction, 0.5)
    }

    func testTheFilesShownAsDoneNeverExceedTheFilesPlanned() {
        XCTAssertEqual(PhotoBackupStatus(plannedFileCount: 5, finishedFileCount: 8).shownFinishedFileCount, 5)
        XCTAssertEqual(PhotoBackupStatus(plannedFileCount: 5, finishedFileCount: 3).shownFinishedFileCount, 3)
    }

    func testAResumePointSavedWithTheIdentifierOfItsItemIsStillRead() async throws {
        let directory = try temporaryDirectory(self)
        let index = try PhotoBackupIndex(directory: directory)
        await index.adopt(accountKey: "photos.example.com|robin")
        await index.beginListing(changeToken: Data([7]), libraryItemCount: 900)
        let point = PhotoBackupResumePoint(creationDate: Date(timeIntervalSince1970: 1_767_225_600.625))
        await index.recordListed(batchItemCount: 300, resumePoint: point)
        await index.flush()
        // The file as an earlier version wrote it, with the identifier beside the date.
        let indexFile = directory.appendingPathComponent(PhotoBackupIndex.indexFilename)
        var document = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(contentsOf: indexFile)) as? [String: Any])
        var listing = try XCTUnwrap(document["listing"] as? [String: Any])
        var savedPoint = try XCTUnwrap(listing["resumePoint"] as? [String: Any])
        savedPoint["localIdentifier"] = "x/L0/001"
        listing["resumePoint"] = savedPoint
        document["listing"] = listing
        try JSONSerialization.data(withJSONObject: document).write(to: indexFile)

        let reopened = try PhotoBackupIndex(directory: directory)
        let wasStartedOver = await reopened.wasStartedOver
        XCTAssertFalse(wasStartedOver, "the file is read, not thrown away")
        let progress = await reopened.listingProgress
        XCTAssertEqual(progress?.resumePoint, point)
        XCTAssertEqual(progress?.listedItemCount, 300)
    }

    func testAPassThatReadsOnlyChangesDoesNotCountAListing() {
        let status = PhotoBackupStatus(phase: .uploading, libraryItemCount: 35_000, plannedFileCount: 10, finishedFileCount: 5)
        XCTAssertNil(status.listedItemCount)
        XCTAssertEqual(status.progressFraction, 0.5, accuracy: 0.0001)
    }

    func testAListingIsThirtyPercentOfTheProgressAndTheFilesTheRest() {
        var status = PhotoBackupStatus(phase: .listing, libraryItemCount: 1_000, listedItemCount: 0)
        XCTAssertEqual(status.progressFraction, 0, accuracy: 0.0001)
        status.listedItemCount = 500
        XCTAssertEqual(status.progressFraction, 0.15, accuracy: 0.0001)
        status.listedItemCount = 1_000
        XCTAssertEqual(status.progressFraction, 1, accuracy: 0.0001, "listed with nothing to send is done")
        status.plannedFileCount = 100
        XCTAssertEqual(status.progressFraction, 0.3, accuracy: 0.0001)
        status.finishedFileCount = 50
        XCTAssertEqual(status.progressFraction, 0.65, accuracy: 0.0001)
    }

    func testTheIndexIsNotRewrittenForEachBatchOfAListing() async throws {
        let directory = try temporaryDirectory(self)
        let clock = ManualClock()
        let index = try PhotoBackupIndex(directory: directory, now: { clock.now })
        await index.adopt(accountKey: "photos.example.com|robin")
        await index.beginListing(changeToken: Data([1]), libraryItemCount: 35_000)
        let indexFile = directory.appendingPathComponent(PhotoBackupIndex.indexFilename)
        let sizeAtStart = try FileManager.default.attributesOfItem(atPath: indexFile.path)[.size] as? Int
        let items = Array(makeLibraryItems(count: 35_000).reversed())
        let start = ContinuousClock.now
        for batchStart in stride(from: 0, to: items.count, by: 300) {
            let batch = Array(items[batchStart..<min(batchStart + 300, items.count)])
            await index.markPending(PhotoBackupPlanner.plan(items: batch, records: [:]), shouldWriteAtOnce: false)
            await index.recordListed(batchItemCount: batch.count, resumePoint: batch.last?.creationDate.map { PhotoBackupResumePoint(creationDate: $0) })
        }
        let batchesDuration = start.duration(to: .now)
        let sizeAfterBatches = try FileManager.default.attributesOfItem(atPath: indexFile.path)[.size] as? Int
        XCTAssertEqual(sizeAfterBatches, sizeAtStart, "117 batches inside the thirty seconds between writes write nothing")
        let flushStart = ContinuousClock.now
        await index.finishListing()
        let flushDuration = flushStart.duration(to: .now)
        let sizeAtEnd = try XCTUnwrap(FileManager.default.attributesOfItem(atPath: indexFile.path)[.size] as? Int)
        print("35,000 records through the index in 117 batches: \(batchesDuration), the one write at the end: \(flushDuration), file \(sizeAtEnd) bytes")
        XCTAssertGreaterThan(sizeAtEnd, 1_000_000)
        let reopened = try PhotoBackupIndex(directory: directory)
        let known = await reopened.knownLocalIdentifiers()
        XCTAssertEqual(known.count, 35_000)
        let pending = await reopened.pendingLocalIdentifiers()
        XCTAssertEqual(pending.first, "item34999", "the line to send is newest first")
        XCTAssertEqual(pending.last, "item0")
        let token = await reopened.changeToken
        XCTAssertEqual(token, Data([1]))
    }

    func testAListingUnderWayIsKeptAcrossOpeningTheFileAndDroppedByAChangedToken() async throws {
        let directory = try temporaryDirectory(self)
        let index = try PhotoBackupIndex(directory: directory)
        await index.adopt(accountKey: "photos.example.com|robin")
        await index.beginListing(changeToken: Data([7]), libraryItemCount: 900)
        let point = PhotoBackupResumePoint(creationDate: Date(timeIntervalSince1970: 1_767_225_600.625))
        await index.recordListed(batchItemCount: 300, resumePoint: point)
        await index.flush()
        let reopened = try PhotoBackupIndex(directory: directory)
        let progress = await reopened.listingProgress
        XCTAssertEqual(progress?.resumePoint, point, "the date is kept to the fraction of a second")
        XCTAssertEqual(progress?.listedItemCount, 300)
        XCTAssertEqual(progress?.libraryItemCount, 900)
        let token = await reopened.changeToken
        XCTAssertNil(token, "the token waits for the end of the listing")
        await reopened.setChangeToken(nil)
        let dropped = await reopened.listingProgress
        XCTAssertNil(dropped, "the Hidden album setting changed: the next pass lists from the start")
    }

    func testPhotosThatCameInSinceGoBeforeTheRestOfTheLineAndAnEditIsStillToSend() async throws {
        let index = try PhotoBackupIndex(directory: try temporaryDirectory(self))
        await index.adopt(accountKey: "photos.example.com|robin")
        let older = [makeItem("b", secondsSince2026: 20), makeItem("a", secondsSince2026: 10)]
        await index.markPending(PhotoBackupPlanner.plan(items: older, records: [:]))
        let newer = [makeItem("d", secondsSince2026: 40), makeItem("c", secondsSince2026: 30)]
        await index.markPending(PhotoBackupPlanner.plan(items: newer, records: [:]), goesBeforeTheRest: true)
        var pending = await index.pendingLocalIdentifiers()
        XCTAssertEqual(pending, ["d", "c", "b", "a"])
        let pendingFileCount = await index.pendingFileCount()
        XCTAssertEqual(pendingFileCount, 4)

        await index.markUploaded(localIdentifier: "b", clientPhotoId: "local:b", role: .original, serverPhotoId: "p", assetModificationDate: nil)
        pending = await index.pendingLocalIdentifiers()
        XCTAssertEqual(pending, ["d", "c", "a"])
        let afterUpload = await index.pendingFileCount()
        XCTAssertEqual(afterUpload, 3)

        // The edit of a photo that is up goes up again.
        await index.markUploaded(localIdentifier: "b", clientPhotoId: "local:b", role: .edited, serverPhotoId: "p", assetModificationDate: Date(timeIntervalSince1970: 1))
        let edited = makeItem("b", secondsSince2026: 20, modifiedSecondsSince2026: 90_000, roles: [.original, .edited])
        let editUploads = await index.plan(items: [edited])
        XCTAssertEqual(editUploads.map(\.key), ["b|edited"])
        await index.markPending(editUploads, goesBeforeTheRest: true)
        pending = await index.pendingLocalIdentifiers()
        XCTAssertEqual(pending.first, "d", "b keeps its place, which is behind the photos that came in since")
        XCTAssertTrue(pending.contains("b"))
        let afterEdit = await index.pendingFileCount()
        XCTAssertEqual(afterEdit, 4)
    }

    func testTheScreenReadsTheLibraryWhenItAppearsAndNotMoreOftenThanEveryFifteenSeconds() {
        let now = Date(timeIntervalSince1970: 1_800_000_000)
        typealias Policy = PhotoBackupScreenReadPolicy
        XCTAssertTrue(Policy.shouldRead(isReading: false, lastReadAt: nil, now: now, mustRead: true))
        XCTAssertTrue(Policy.shouldRead(isReading: false, lastReadAt: now, now: now, mustRead: true), "appearing reads, whatever happened before")
        XCTAssertFalse(Policy.shouldRead(isReading: true, lastReadAt: nil, now: now, mustRead: true), "never twice at once")
        XCTAssertFalse(Policy.shouldRead(isReading: false, lastReadAt: now.addingTimeInterval(-14), now: now, mustRead: false))
        XCTAssertTrue(Policy.shouldRead(isReading: false, lastReadAt: now.addingTimeInterval(-15), now: now, mustRead: false))
        XCTAssertTrue(Policy.shouldRead(isReading: false, lastReadAt: nil, now: now, mustRead: false))
    }
}

/// The rule for where a batch of the listing ends, on bare dates.
final class PhotoBackupListingWindowTests: XCTestCase {
    private let newest = Date(timeIntervalSince1970: 1_767_225_600)

    func testTwoItemsOfOneDateStraddlingTheLimitAreTakenTogether() {
        let dates = [newest, newest.addingTimeInterval(-10), newest.addingTimeInterval(-10), newest.addingTimeInterval(-20)]
        XCTAssertEqual(PhotoBackupListingWindow.takeCount(candidateCount: dates.count, limit: 2) { dates[$0] }, 3, "the third was made at the moment of the second")
        XCTAssertEqual(PhotoBackupListingWindow.takeCount(candidateCount: dates.count, limit: 1) { dates[$0] }, 1, "the second was made earlier than the first")
        XCTAssertEqual(PhotoBackupListingWindow.takeCount(candidateCount: dates.count, limit: 3) { dates[$0] }, 3)
        XCTAssertEqual(PhotoBackupListingWindow.takeCount(candidateCount: dates.count, limit: 10) { dates[$0] }, 4, "a limit above what is left takes what is left")
        XCTAssertEqual(PhotoBackupListingWindow.takeCount(candidateCount: 0, limit: 2) { _ in newest }, 0)
        XCTAssertEqual(PhotoBackupListingWindow.takeCount(candidateCount: dates.count, limit: 0) { dates[$0] }, 1, "a batch takes one item at least")
    }

    func testAChainOfItemsLessThanAMillisecondApartCrossesTheLimit() {
        // Each is 0.9 milliseconds after the one before it, so none is a
        // millisecond from its neighbor although the chain spans nine.
        var dates = (0..<10).map { newest.addingTimeInterval(-0.0009 * Double($0)) }
        dates.append(newest.addingTimeInterval(-1))
        XCTAssertEqual(PhotoBackupListingWindow.takeCount(candidateCount: dates.count, limit: 3) { dates[$0] }, 10, "the whole chain, and not the item a second before it")
    }

    func testTheBoundaryIsExactlyAMillisecondBeforeTheLastItemTaken() {
        let boundary = newest.addingTimeInterval(-PhotoBackupListingWindow.sameMomentSeconds)
        let justInside = newest.addingTimeInterval(-0.0009)
        let justOutside = newest.addingTimeInterval(-0.0011)
        XCTAssertEqual(PhotoBackupListingWindow.takeCount(candidateCount: 2, limit: 1) { [newest, boundary][$0] }, 1, "a millisecond before is the next moment")
        XCTAssertEqual(PhotoBackupListingWindow.takeCount(candidateCount: 2, limit: 1) { [newest, justInside][$0] }, 2)
        XCTAssertEqual(PhotoBackupListingWindow.takeCount(candidateCount: 2, limit: 1) { [newest, justOutside][$0] }, 1)

        // The next batch starts at that boundary, included, so an item is in
        // this batch or in the next, never in both and never in neither.
        let resumePoint = PhotoBackupResumePoint(creationDate: newest)
        XCTAssertEqual(PhotoBackupListingWindow.newestCreationDate(after: resumePoint), boundary)
        for date in [boundary, justInside, justOutside, newest.addingTimeInterval(-0.002)] {
            let isInThisBatch = PhotoBackupListingWindow.takeCount(candidateCount: 2, limit: 1) { [newest, date][$0] } == 2
            let isInNextBatch = date <= PhotoBackupListingWindow.newestCreationDate(after: resumePoint)
            XCTAssertNotEqual(isInThisBatch, isInNextBatch, "\(newest.timeIntervalSince(date)) seconds before the last item taken")
        }
    }
}

/// The fake library lists as the real one does, which these check by paging through it.
final class PhotoBackupListingBatchTests: XCTestCase {
    private func listEverything(_ library: FakeLibrary, limit: Int) async throws -> [PhotoBackupListingBatch] {
        var batches: [PhotoBackupListingBatch] = []
        var resumePoint: PhotoBackupResumePoint?
        while batches.count < 1_000 {
            let batch = try await library.listBatch(after: resumePoint, limit: limit, includesHidden: false)
            batches.append(batch)
            if batch.isLast || batch.entries.isEmpty { return batches }
            resumePoint = batch.entries.compactMap { entry in entry.creationDate.map { PhotoBackupResumePoint(creationDate: $0) } }.last ?? resumePoint
        }
        XCTFail("the listing did not end")
        return batches
    }

    func testPagingThroughALibraryListsEveryItemOnceWithNoGapAndNoDuplicate() async throws {
        // Seven items in moments of three, then two with no date.
        let library = FakeLibrary(items: makeLibraryItems(count: 7, itemsPerMoment: 3, undatedCount: 2))
        let batches = try await listEverything(library, limit: 2)
        let listed = batches.flatMap(\.entries).map(\.localIdentifier)
        XCTAssertEqual(listed.sorted(), (0..<7).map { "item\($0)" }.sorted() + ["undated0", "undated1"], "every item once")
        XCTAssertEqual(batches.map(\.entries.count), [4, 5], "limit two, extended to the end of the moment of the second: 4, then three more and the two without a date")
        // No batch ends inside a moment: the date of its last dated item is not
        // that of the first item of the next.
        for (batch, next) in zip(batches, batches.dropFirst()) {
            let lastDate = batch.entries.compactMap(\.creationDate).last
            XCTAssertNotEqual(lastDate, next.entries.first?.creationDate)
        }
    }

    func testItemsWithNoDateAreListedOnceWithTheLastBatchOnly() async throws {
        let library = FakeLibrary(items: makeLibraryItems(count: 5, undatedCount: 3))
        let batches = try await listEverything(library, limit: 2)
        XCTAssertEqual(batches.map(\.isLast), [false, false, true])
        for batch in batches.dropLast() {
            XCTAssertTrue(batch.entries.allSatisfy { $0.creationDate != nil })
        }
        XCTAssertEqual(batches.last?.entries.map(\.localIdentifier), ["item0", "undated0", "undated1", "undated2"])
        // Asked again after the last dated item, the undated ones come again
        // only because the dated ones ended: a library of undated items alone
        // lists them in its one batch.
        let onlyUndated = FakeLibrary(items: makeLibraryItems(count: 0, undatedCount: 2))
        let undatedBatches = try await listEverything(onlyUndated, limit: 5)
        XCTAssertEqual(undatedBatches.count, 1)
        XCTAssertEqual(undatedBatches.first?.entries.map(\.localIdentifier), ["undated0", "undated1"])
    }

    func testAChainOfItemsLessThanAMillisecondApartIsOneBatchWhateverTheLimit() async throws {
        let chain = (0..<10).map { makeItem("chain\($0)", secondsSince2026: 100 - 0.0009 * Double($0)) }
        let earlier = makeItem("earlier", secondsSince2026: 50)
        let library = FakeLibrary(items: chain + [earlier])
        let batches = try await listEverything(library, limit: 4)
        XCTAssertEqual(batches.map(\.entries.count), [10, 1])
        XCTAssertEqual(batches.last?.entries.map(\.localIdentifier), ["earlier"])
    }
}

private final class StatusLog: @unchecked Sendable {
    private let lock = NSLock()
    private var statuses: [PhotoBackupStatus] = []
    func add(_ status: PhotoBackupStatus) { lock.withLock { statuses.append(status) } }
    var all: [PhotoBackupStatus] { lock.withLock { statuses } }
}

final class PhotoBackupPauseTests: XCTestCase {
    func testAPausedBackupStartsNoPassAndQueuesNothing() async throws {
        let items = (0..<4).map { makeItem("item\($0)", secondsSince2026: Double($0)) }
        let rig = try makeRig(self, items: items, isPaused: true)
        let result = await rig.coordinator.runPass(settings: settings)
        XCTAssertTrue(result.wasPaused)
        XCTAssertEqual(result.plannedFileCount, 0)
        XCTAssertEqual(rig.transfer.startedJobs.count, 0)
        XCTAssertEqual(rig.library.exportedUploadKeys.count, 0, "not even read from the library")
        let queuedByWake = await rig.coordinator.topUp(settings: settings, until: Date(timeIntervalSince1970: 1_800_000_060))
        XCTAssertEqual(queuedByWake, 0, "the wake in the background does nothing either")
        let status = await rig.coordinator.status()
        XCTAssertEqual(status.phase, .paused)
        XCTAssertTrue(status.isPaused)
    }

    func testResumingAllowsThePassAgain() async throws {
        let items = (0..<3).map { makeItem("item\($0)", secondsSince2026: Double($0)) }
        let rig = try makeRig(self, items: items, isPaused: true)
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p-" + job.localIdentifier)) }
        }
        let whilePaused = await coordinator.runPass(settings: settings)
        XCTAssertTrue(whilePaused.wasPaused)
        await coordinator.resume()
        let afterResume = await coordinator.runPass(settings: settings)
        XCTAssertFalse(afterResume.wasPaused)
        XCTAssertEqual(afterResume.uploadedFileCount, 3)
        let status = await coordinator.status()
        XCTAssertNotEqual(status.phase, .paused)
    }

    func testPausingCancelsTheUploadsTheSessionHoldsAndKeepsWhatWentUp() async throws {
        let rig = try makeRig(self, items: (0..<10).map { makeItem("item\($0)", secondsSince2026: Double($0)) })
        let coordinator = rig.coordinator
        let pass = Task { await coordinator.runPass(settings: settings) }
        try await waitUntil("four uploads queued") { rig.transfer.startedJobs.count == 4 }
        // One of them went up before the pause.
        let first = try XCTUnwrap(rig.transfer.startedJobs.first)
        rig.transfer.release(first)
        _ = await coordinator.finished(first, outcome: .uploaded(serverPhotoId: "p-" + first.localIdentifier))

        await coordinator.pause()
        _ = await pass.value
        // The call the app makes once the pass has ended.
        await coordinator.pause()

        XCTAssertTrue(rig.transfer.isCancelled, "the phone stops sending")
        let held = await rig.transfer.queuedJobs()
        XCTAssertEqual(held.count, 0)
        let status = await coordinator.status()
        XCTAssertEqual(status.phase, .paused)
        XCTAssertEqual(status.queuedFileCount, 0)
        XCTAssertFalse(status.isUploading)
        XCTAssertNil(status.estimatedSecondsLeft)
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: rig.outgoing.path), [], "the exported files are deleted")
        let summary = await rig.index.summary()
        XCTAssertEqual(summary.libraryItemCount, 10, "the index is kept")
        XCTAssertEqual(summary.backedUpItemCount, 1, "what went up stays recorded")
        let startedCount = rig.transfer.startedJobs.count
        let again = await coordinator.runPass(settings: settings)
        XCTAssertTrue(again.wasPaused)
        XCTAssertEqual(rig.transfer.startedJobs.count, startedCount, "nothing is queued while paused")
    }

    func testPausedShowsNeitherTheTimeLeftNorBackgroundUploading() {
        let status = PhotoBackupStatus(
            phase: .paused, backedUpItemCount: 10, libraryItemCount: 100, queuedFileCount: 3, waitingFileCount: 5, estimatedSecondsLeft: 600)
        XCTAssertTrue(status.isEnabled, "paused is not off")
        XCTAssertTrue(status.isPaused)
        XCTAssertFalse(status.isUploading)
        XCTAssertFalse(status.shouldShowTimeLeft)
        XCTAssertFalse(status.isUploadingInBackground)
        XCTAssertFalse(status.hasFilesWaitingForTheApp)
    }
}

// MARK: A file cut off part way goes on from where the server has it

/// A file of this many bytes, each different from its neighbors, so that a
/// test can tell which part of it was sent.
private func numberedBytes(_ count: Int) -> Data {
    Data((0..<count).map { UInt8($0 % 251) })
}

private let continuedFileDigest = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
private let otherFileDigest = "cdc76e5c9914fb9281a1c7e284d73e67f1809a48a497200e046d39ccc7112cd0"
private let continuedFileKey = PhotoBackupUpload.key(localIdentifier: "a", role: .original)

/// A rig whose library holds one photo, "a", exported as these bytes.
private func makeContinuationRig(
    _ test: XCTestCase, content: Data = numberedBytes(3000), digest: String? = continuedFileDigest, directory: URL? = nil,
    index: PhotoBackupIndex? = nil, uploads: FakeUploads? = nil, freeSpaceBytes: @escaping @Sendable () -> Int64? = { nil }
) throws -> Rig {
    let rig = try makeRig(test, items: [makeItem("a")], index: index, directory: directory, freeSpaceBytes: freeSpaceBytes, uploads: uploads)
    rig.library.set(content: content, for: continuedFileKey)
    if let digest { rig.library.set(sha256: digest, for: continuedFileKey) }
    return rig
}

/// The first try: the pass queues the file and the server takes `serverHasBytes`
/// of it before the pass is cut off by a pause. The task the session had is
/// reported cancelled, as the real one is, and backup is resumed. Returns the
/// job that was cut off.
@discardableResult
private func cutOffFirstTry(_ rig: Rig, serverHasBytes: Int64) async throws -> PhotoBackupJob {
    let coordinator = rig.coordinator
    let pass = Task { await coordinator.runPass(settings: settings) }
    try await waitUntil("the file is queued") { rig.transfer.startedJobs.count == 1 }
    let job = try XCTUnwrap(rig.transfer.startedJobs.first)
    rig.uploads.set(offsetBytes: serverHasBytes, ofUploadAt: try XCTUnwrap(rig.transfer.startedRequests.first?.url?.path))
    await coordinator.pause()
    _ = await pass.value
    await coordinator.pause()
    await coordinator.finished(job, outcome: .cancelled)
    await coordinator.resume()
    return job
}

/// A second pass in which every queued upload goes up.
private func runPassThatUploadsEverything(_ rig: Rig, photoId: String = "p1") async -> PhotoBackupPassResult {
    let coordinator = rig.coordinator
    rig.transfer.onStart = { job in
        rig.transfer.release(job)
        Task { await coordinator.finished(job, outcome: .uploaded(serverPhotoId: photoId)) }
    }
    return await coordinator.runPass(settings: settings)
}

final class PhotoBackupContinuationTests: XCTestCase {
    func testAFileIsMadeAnUploadFirstAndItsBytesGoInAPatchFromTheStart() async throws {
        let content = numberedBytes(3000)
        let rig = try makeContinuationRig(self, content: content)
        let result = await runPassThatUploadsEverything(rig)
        XCTAssertEqual(result.uploadedFileCount, 1)
        let creation = try XCTUnwrap(rig.uploads.creationRequests.first)
        XCTAssertEqual(creation.value(forHTTPHeaderField: "Upload-Length"), "3000")
        XCTAssertEqual(creation.value(forHTTPHeaderField: "X-Photo-Declared-SHA256"), continuedFileDigest)
        let sending = try XCTUnwrap(rig.transfer.startedRequests.first)
        XCTAssertEqual(sending.httpMethod, "PATCH")
        XCTAssertEqual(sending.url?.path, FakeUploads.path(number: 1))
        XCTAssertEqual(sending.value(forHTTPHeaderField: "Upload-Offset"), "0")
        XCTAssertEqual(rig.transfer.startedBodies.first, content, "the whole file, from the exported file itself")
        XCTAssertEqual(rig.uploads.statusRequests.count, 0, "nothing to ask about a file that was never started")
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: rig.outgoing.path), [])
    }

    func testTheAddressOfTheUploadIsKeptWithTheFileUntilItIsOnTheServer() async throws {
        let rig = try makeContinuationRig(self)
        try await cutOffFirstTry(rig, serverHasBytes: 1000)
        let kept = await rig.index.partialUpload(localIdentifier: "a", role: .original)
        XCTAssertEqual(kept, PhotoBackupPartialUpload(uploadPath: FakeUploads.path(number: 1), sizeBytes: 3000, sha256: continuedFileDigest))
        _ = await runPassThatUploadsEverything(rig)
        let afterwards = await rig.index.partialUpload(localIdentifier: "a", role: .original)
        XCTAssertNil(afterwards, "the upload is over")
    }

    func testAPausedUploadContinuesFromTheOffsetTheServerHasAndSendsOnlyTheRest() async throws {
        let content = numberedBytes(3000)
        let rig = try makeContinuationRig(self, content: content)
        let firstJob = try await cutOffFirstTry(rig, serverHasBytes: 1200)
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: rig.outgoing.path), [], "the pause deleted the exported file")

        let result = await runPassThatUploadsEverything(rig)

        XCTAssertEqual(result.uploadedFileCount, 1)
        XCTAssertEqual(rig.uploads.creationRequests.count, 1, "the upload made the first time is the one that goes on")
        let status = try XCTUnwrap(rig.uploads.statusRequests.first)
        XCTAssertEqual(status.httpMethod, "HEAD")
        XCTAssertEqual(status.url?.path, FakeUploads.path(number: 1))
        XCTAssertEqual(rig.transfer.startedJobs.count, 2)
        let continuation = try XCTUnwrap(rig.transfer.startedRequests.last)
        XCTAssertEqual(continuation.httpMethod, "PATCH")
        XCTAssertEqual(continuation.url?.path, FakeUploads.path(number: 1))
        XCTAssertEqual(continuation.value(forHTTPHeaderField: "Upload-Offset"), "1200")
        XCTAssertEqual(rig.transfer.startedBodies.last, content.suffix(from: 1200), "only the rest of the file is sent")
        let secondJob = try XCTUnwrap(rig.transfer.startedJobs.last)
        XCTAssertEqual(secondJob.sizeBytes, 1800, "the queue counts the rest, not the whole file")
        XCTAssertNotEqual(secondJob.outgoingFilename, firstJob.outgoingFilename)
        XCTAssertTrue(secondJob.outgoingFilename.hasSuffix(".tail"))
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: rig.outgoing.path), [], "the rest and the whole file are both deleted")
    }

    func testTheWholeFileIsDeletedAsSoonAsTheRestIsWrittenSoOnlyTheRestStaysWhileItIsSent() async throws {
        let rig = try makeContinuationRig(self)
        try await cutOffFirstTry(rig, serverHasBytes: 1000)
        let coordinator = rig.coordinator
        let pass = Task { await coordinator.runPass(settings: settings) }
        try await waitUntil("the rest is queued") { rig.transfer.startedJobs.count == 2 }
        let names = try FileManager.default.contentsOfDirectory(atPath: rig.outgoing.path)
        XCTAssertEqual(names.count, 1)
        XCTAssertTrue(try XCTUnwrap(names.first).hasSuffix(".tail"))
        let job = try XCTUnwrap(rig.transfer.startedJobs.last)
        rig.transfer.release(job)
        await coordinator.finished(job, outcome: .uploaded(serverPhotoId: "p1"))
        _ = await pass.value
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: rig.outgoing.path), [], "deleted when the upload ends")
    }

    func testTheRestIsDeletedWhenItsUploadFailsToo() async throws {
        let rig = try makeContinuationRig(self)
        try await cutOffFirstTry(rig, serverHasBytes: 1000)
        let coordinator = rig.coordinator
        rig.transfer.onStart = { job in
            rig.transfer.release(job)
            Task { await coordinator.finished(job, outcome: .retryLater(failure: nil, isConnectivityFailure: true)) }
        }
        _ = await coordinator.runPass(settings: settings)
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: rig.outgoing.path), [])
        let kept = await rig.index.partialUpload(localIdentifier: "a", role: .original)
        XCTAssertNotNil(kept, "the address stays, for the next try")
    }

    func testAnUploadCutOffManyTimesGoesOnFromWhereItGotEachTime() async throws {
        let content = numberedBytes(3000)
        let rig = try makeContinuationRig(self, content: content)
        try await cutOffFirstTry(rig, serverHasBytes: 500)
        let coordinator = rig.coordinator
        let pass = Task { await coordinator.runPass(settings: settings) }
        try await waitUntil("the rest is queued") { rig.transfer.startedJobs.count == 2 }
        let second = try XCTUnwrap(rig.transfer.startedJobs.last)
        rig.uploads.set(offsetBytes: 2000, ofUploadAt: FakeUploads.path(number: 1))
        await coordinator.pause()
        _ = await pass.value
        await coordinator.pause()
        await coordinator.finished(second, outcome: .cancelled)
        await coordinator.resume()

        _ = await runPassThatUploadsEverything(rig)

        XCTAssertEqual(rig.uploads.creationRequests.count, 1)
        XCTAssertEqual(rig.transfer.startedRequests.last?.value(forHTTPHeaderField: "Upload-Offset"), "2000")
        XCTAssertEqual(rig.transfer.startedBodies.last, content.suffix(from: 2000))
        XCTAssertEqual(rig.transfer.startedBodies[1], content.suffix(from: 500))
    }

    func testARelaunchWithAStoredAddressContinuesTheUpload() async throws {
        let content = numberedBytes(3000)
        let directory = try temporaryDirectory(self)
        let uploads = FakeUploads()
        let firstLaunch = try makeContinuationRig(self, content: content, directory: directory, uploads: uploads)
        try await cutOffFirstTry(firstLaunch, serverHasBytes: 1000)
        await firstLaunch.index.flush()

        // The app is started again: a new index read from the same file, a new
        // coordinator, the same server.
        let reopened = try PhotoBackupIndex(directory: directory)
        let secondLaunch = try makeContinuationRig(self, content: content, directory: directory, index: reopened, uploads: uploads)
        let result = await runPassThatUploadsEverything(secondLaunch)

        XCTAssertEqual(result.uploadedFileCount, 1)
        XCTAssertEqual(uploads.creationRequests.count, 1, "no second upload was made")
        XCTAssertEqual(secondLaunch.transfer.startedRequests.last?.value(forHTTPHeaderField: "Upload-Offset"), "1000")
        XCTAssertEqual(secondLaunch.transfer.startedBodies.last, content.suffix(from: 1000))
    }

    func testAnUploadTheServerNoLongerHasStartsOverFromTheFirstByteInANewUpload() async throws {
        let content = numberedBytes(3000)
        let rig = try makeContinuationRig(self, content: content)
        try await cutOffFirstTry(rig, serverHasBytes: 1000)
        rig.uploads.forget(uploadAt: FakeUploads.path(number: 1))

        let result = await runPassThatUploadsEverything(rig)

        XCTAssertEqual(result.uploadedFileCount, 1)
        XCTAssertEqual(rig.uploads.statusRequests.count, 1)
        XCTAssertEqual(rig.uploads.creationRequests.count, 2, "a new upload is made")
        let sending = try XCTUnwrap(rig.transfer.startedRequests.last)
        XCTAssertEqual(sending.url?.path, FakeUploads.path(number: 2))
        XCTAssertEqual(sending.value(forHTTPHeaderField: "Upload-Offset"), "0")
        XCTAssertEqual(rig.transfer.startedBodies.last, content, "the whole file")
        XCTAssertEqual(try XCTUnwrap(rig.transfer.startedJobs.last).sizeBytes, 3000)
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: rig.outgoing.path), [])
    }

    func testAnOffsetBeyondTheEndOfTheFileStartsOver() async throws {
        let rig = try makeContinuationRig(self)
        try await cutOffFirstTry(rig, serverHasBytes: 1000)
        rig.uploads.set(offsetBytes: 3001, ofUploadAt: FakeUploads.path(number: 1))

        _ = await runPassThatUploadsEverything(rig)

        XCTAssertEqual(rig.uploads.creationRequests.count, 2)
        XCTAssertEqual(rig.transfer.startedRequests.last?.value(forHTTPHeaderField: "Upload-Offset"), "0")
        XCTAssertEqual(rig.transfer.startedBodies.last?.count, 3000)
    }

    func testAFileThatHashesToSomethingElseIsNotContinuedEvenWhenTheServerHasBytes() async throws {
        let rig = try makeContinuationRig(self)
        try await cutOffFirstTry(rig, serverHasBytes: 1000)
        // The photo was edited in the meantime: the library exports other bytes.
        let edited = Data(numberedBytes(3000).reversed())
        rig.library.set(content: edited, for: continuedFileKey)
        rig.library.set(sha256: otherFileDigest, for: continuedFileKey)

        _ = await runPassThatUploadsEverything(rig)

        XCTAssertEqual(rig.uploads.statusRequests.count, 0, "the server is not asked about an upload that is not this file's")
        XCTAssertEqual(rig.uploads.creationRequests.count, 2)
        XCTAssertEqual(rig.uploads.creationRequests.last?.value(forHTTPHeaderField: "X-Photo-Declared-SHA256"), otherFileDigest)
        XCTAssertEqual(rig.transfer.startedRequests.last?.value(forHTTPHeaderField: "Upload-Offset"), "0")
        XCTAssertEqual(rig.transfer.startedBodies.last, edited)
    }

    func testAFileOfAnotherSizeIsNotContinuedEvenWithTheSameDigestText() async throws {
        let rig = try makeContinuationRig(self)
        try await cutOffFirstTry(rig, serverHasBytes: 1000)
        rig.library.set(content: numberedBytes(2500), for: continuedFileKey)

        _ = await runPassThatUploadsEverything(rig)

        XCTAssertEqual(rig.uploads.statusRequests.count, 0)
        XCTAssertEqual(rig.uploads.creationRequests.last?.value(forHTTPHeaderField: "Upload-Length"), "2500")
        XCTAssertEqual(rig.transfer.startedRequests.last?.value(forHTTPHeaderField: "Upload-Offset"), "0")
    }

    func testAnUploadTheServerCompletedIsRecordedOnceAndNothingIsSent() async throws {
        let rig = try makeContinuationRig(self)
        try await cutOffFirstTry(rig, serverHasBytes: 1000)
        // The last request got through and the answer was lost.
        rig.uploads.set(completePhotoId: "photo-9", ofUploadAt: FakeUploads.path(number: 1))
        let sentBefore = rig.transfer.startedJobs.count

        let result = await rig.coordinator.runPass(settings: settings)

        XCTAssertEqual(rig.transfer.startedJobs.count, sentBefore, "no bytes are sent")
        XCTAssertEqual(result.uploadedFileCount, 1)
        let record = await rig.index.record(forLocalIdentifier: "a")
        XCTAssertEqual(record?.roles["original"]?.serverPhotoId, "photo-9")
        XCTAssertEqual(record?.roles["original"]?.isOnServer, true)
        XCTAssertNil(record?.roles["original"]?.partialUpload)
        let summary = await rig.index.summary()
        XCTAssertEqual(summary.backedUpItemCount, 1)
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: rig.outgoing.path), [])
        let again = await rig.coordinator.runPass(settings: settings)
        XCTAssertEqual(again.plannedFileCount, 0, "it is not planned again")
        XCTAssertEqual(rig.transfer.startedJobs.count, sentBefore)
    }

    func testTheFinishedUploadIsRecordedExactlyOnceWhenTheRestGoesUp() async throws {
        let rig = try makeContinuationRig(self)
        try await cutOffFirstTry(rig, serverHasBytes: 1000)

        let result = await runPassThatUploadsEverything(rig, photoId: "photo-7")

        XCTAssertEqual(result.uploadedFileCount, 1)
        XCTAssertEqual(result.refusedFileCount, 0)
        let summary = await rig.index.summary()
        XCTAssertEqual(summary.backedUpItemCount, 1)
        let record = await rig.index.record(forLocalIdentifier: "a")
        XCTAssertEqual(record?.roles["original"]?.serverPhotoId, "photo-7")
        let pendingCount = await rig.index.pendingFileCount()
        XCTAssertEqual(pendingCount, 0)
        let again = await rig.coordinator.runPass(settings: settings)
        XCTAssertEqual(again.plannedFileCount, 0)
        XCTAssertEqual(rig.transfer.startedJobs.count, 2, "one upload, then its continuation, and no more")
    }

    func testAnUploadWithEveryByteButNotCompleteSendsAnEmptyRestThatCompletesIt() async throws {
        let rig = try makeContinuationRig(self)
        try await cutOffFirstTry(rig, serverHasBytes: 3000)

        let result = await runPassThatUploadsEverything(rig)

        XCTAssertEqual(result.uploadedFileCount, 1)
        XCTAssertEqual(rig.uploads.creationRequests.count, 1)
        let continuation = try XCTUnwrap(rig.transfer.startedRequests.last)
        XCTAssertEqual(continuation.value(forHTTPHeaderField: "Upload-Offset"), "3000")
        XCTAssertEqual(continuation.value(forHTTPHeaderField: "Upload-Complete"), "?1")
        XCTAssertEqual(rig.transfer.startedBodies.last, Data())
        XCTAssertEqual(try XCTUnwrap(rig.transfer.startedJobs.last).sizeBytes, 0)
    }

    func testTheRestIsNotWrittenWhenThePhoneHasNoRoomForItAndTheFileStartsOver() async throws {
        let content = numberedBytes(3000)
        let rig = try makeContinuationRig(self, content: content, freeSpaceBytes: { 1000 })
        try await cutOffFirstTry(rig, serverHasBytes: 1000)

        // 2000 bytes remain; half of the 1000 free is less.
        _ = await runPassThatUploadsEverything(rig)

        XCTAssertEqual(rig.uploads.creationRequests.count, 2)
        XCTAssertEqual(rig.transfer.startedRequests.last?.value(forHTTPHeaderField: "Upload-Offset"), "0")
        XCTAssertEqual(rig.transfer.startedBodies.last, content)
    }

    func testTheRestIsAllowedWhenItFitsInHalfOfTheFreeSpaceOrTheSpaceIsUnknown() {
        XCTAssertTrue(PhotoBackupBudget.allowsTail(sizeBytes: 500, freeSpaceBytes: 1000))
        XCTAssertFalse(PhotoBackupBudget.allowsTail(sizeBytes: 501, freeSpaceBytes: 1000))
        XCTAssertTrue(PhotoBackupBudget.allowsTail(sizeBytes: 1 << 40, freeSpaceBytes: nil))
        XCTAssertTrue(PhotoBackupBudget.allowsTail(sizeBytes: 0, freeSpaceBytes: -5))
        XCTAssertFalse(PhotoBackupBudget.allowsTail(sizeBytes: 1, freeSpaceBytes: -5))
    }

    func testAFileWithoutADigestIsNeverContinued() async throws {
        let rig = try makeContinuationRig(self, digest: nil)
        try await cutOffFirstTry(rig, serverHasBytes: 1000)
        let kept = await rig.index.partialUpload(localIdentifier: "a", role: .original)
        XCTAssertNil(kept, "nothing could tell the exported file from another one later")

        _ = await runPassThatUploadsEverything(rig)

        XCTAssertEqual(rig.uploads.statusRequests.count, 0)
        XCTAssertEqual(rig.uploads.creationRequests.count, 2)
        XCTAssertEqual(rig.transfer.startedRequests.last?.value(forHTTPHeaderField: "Upload-Offset"), "0")
    }

    func testAFileTheServerRefusedForgetsItsAddress() async throws {
        let rig = try makeContinuationRig(self)
        let job = try await cutOffFirstTry(rig, serverHasBytes: 1000)
        let kept = await rig.index.partialUpload(localIdentifier: "a", role: .original)
        XCTAssertNotNil(kept)
        await rig.coordinator.finished(job, outcome: .refused(failure: .fileRefused(serverReason: "not a picture")))
        let afterwards = await rig.index.partialUpload(localIdentifier: "a", role: .original)
        XCTAssertNil(afterwards)
    }

    func testAnAddressTheServerCannotBeAskedAboutIsKeptAndNothingIsSent() async throws {
        let rig = try makeContinuationRig(self)
        try await cutOffFirstTry(rig, serverHasBytes: 1000)
        rig.uploads.failNextStatus(with: .retryLater(failure: .systemDescription("offline"), isConnectivityFailure: true))
        let sentBefore = rig.transfer.startedJobs.count

        let result = await rig.coordinator.runPass(settings: settings)

        XCTAssertEqual(rig.transfer.startedJobs.count, sentBefore)
        XCTAssertEqual(rig.uploads.creationRequests.count, 1, "an upload is not made for a file whose upload could not be asked about")
        XCTAssertEqual(result.leftForLaterFileCount, 1)
        let kept = await rig.index.partialUpload(localIdentifier: "a", role: .original)
        XCTAssertNotNil(kept)
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: rig.outgoing.path), [])
        // And the next pass, with the network back, goes on from the offset.
        _ = await runPassThatUploadsEverything(rig)
        XCTAssertEqual(rig.transfer.startedRequests.last?.value(forHTTPHeaderField: "Upload-Offset"), "1000")
    }

    func testAServerThatRefusesTheQuestionAboutAnUploadGetsANewUploadInstead() async throws {
        let rig = try makeContinuationRig(self)
        try await cutOffFirstTry(rig, serverHasBytes: 1000)
        rig.uploads.failNextStatus(with: .refused(failure: .fileRefused(serverReason: "bad request")))

        let result = await runPassThatUploadsEverything(rig)

        XCTAssertEqual(result.uploadedFileCount, 1)
        XCTAssertEqual(result.refusedFileCount, 0, "the question was refused, not the file")
        XCTAssertEqual(rig.uploads.creationRequests.count, 2)
        XCTAssertEqual(rig.transfer.startedRequests.last?.value(forHTTPHeaderField: "Upload-Offset"), "0")
    }

    // MARK: When the upload cannot be made

    func testAFileTheServerRefusesToMakeAnUploadForIsRefusedAndNothingIsQueued() async throws {
        let rig = try makeContinuationRig(self)
        rig.uploads.failNextCreation(with: .refused(failure: .fileRefused(serverReason: "too large")))
        let result = await rig.coordinator.runPass(settings: settings)
        XCTAssertEqual(result.refusedFileCount, 1)
        XCTAssertEqual(rig.transfer.startedJobs.count, 0)
        let record = await rig.index.record(forLocalIdentifier: "a")
        XCTAssertEqual(record?.roles["original"]?.refusalCount, 1)
        XCTAssertEqual(record?.roles["original"]?.refusalFailure, .fileRefused(serverReason: "too large"))
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: rig.outgoing.path), [])
    }

    func testAPasswordTheServerRefusesWhenMakingAnUploadStopsTheBackup() async throws {
        let rig = try makeContinuationRig(self)
        rig.uploads.failNextCreation(with: .signInNeeded)
        _ = await rig.coordinator.runPass(settings: settings)
        let summary = await rig.index.summary()
        XCTAssertTrue(summary.isSignInNeeded)
        XCTAssertEqual(rig.transfer.startedJobs.count, 0)
        XCTAssertTrue(rig.transfer.isCancelled)
    }

    func testWithoutANetworkTheOtherFilesAreNotTriedAndTheyWaitForLater() async throws {
        let items = (0..<5).map { makeItem("item\($0)", secondsSince2026: Double($0)) }
        let rig = try makeRig(self, items: items)
        rig.uploads.failNextCreation(with: .retryLater(failure: .systemDescription("offline"), isConnectivityFailure: true))
        let result = await rig.coordinator.runPass(settings: settings)
        XCTAssertEqual(rig.uploads.creationRequests.count, 1, "the first request found there is no network; the others would wait out a timeout each")
        XCTAssertEqual(rig.transfer.startedJobs.count, 0)
        XCTAssertEqual(result.uploadedFileCount, 0)
        XCTAssertEqual(result.leftForLaterFileCount, 5)
        let pendingCount = await rig.index.pendingFileCount()
        XCTAssertEqual(pendingCount, 5, "every file is still to send")
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: rig.outgoing.path), [])
    }

    func testAServerThatKeepsFailingToMakeUploadsEndsThePassAfterThreeFiles() async throws {
        let items = (0..<8).map { makeItem("item\($0)", secondsSince2026: Double($0)) }
        let rig = try makeRig(self, items: items)
        for _ in 0..<8 { rig.uploads.failNextCreation(with: .retryLater(failure: .serverReason("storage is unavailable"), isConnectivityFailure: false)) }
        let result = await rig.coordinator.runPass(settings: settings)
        XCTAssertEqual(rig.uploads.creationRequests.count, 3)
        XCTAssertEqual(result.leftForLaterFileCount, 8)
        let retryTime = await rig.coordinator.pendingRetryTime
        XCTAssertNotNil(retryTime, "the app is told when to try again")
    }

    func testAWakeInTheBackgroundDoesNotTryMoreFilesOnceTheNetworkIsGone() async throws {
        let items = (0..<5).map { makeItem("item\($0)", secondsSince2026: Double($0)) }
        let rig = try makeRig(self, items: items)
        await rig.index.adopt(accountKey: "photos.example.com|robin")
        await rig.index.markPending(PhotoBackupPlanner.plan(items: items, records: [:]))
        rig.uploads.failNextCreation(with: .retryLater(failure: .systemDescription("offline"), isConnectivityFailure: true))
        let queued = await rig.coordinator.topUp(settings: settings, until: Date(timeIntervalSince1970: 1_800_000_060))
        XCTAssertEqual(queued, 0)
        XCTAssertEqual(rig.uploads.creationRequests.count, 1)
    }
}

final class PhotoBackupUploadClientTests: XCTestCase {
    func testAnUploadTheServerMadeIsKnownByThePathOfItsLocationWhateverTheSchemeAndHost() {
        for location in ["https://photos.example.com/api/v1/photos/uploads/u1", "http://10.0.0.5:8080/api/v1/photos/uploads/u1", "/api/v1/photos/uploads/u1"] {
            XCTAssertEqual(
                PhotoBackupUploadClient.interpretCreation(statusCode: 201, locationHeader: location, bodyText: nil, retryAfterHeader: nil),
                .answered("/api/v1/photos/uploads/u1"), location)
        }
    }

    func testALocationThatIsNotAnUploadIsNotFollowed() {
        for location in [nil, "", "https://photos.example.com/graphql", "https://photos.example.com/api/v1/photos/uploads/", "https://photos.example.com/other/u1"] {
            XCTAssertEqual(
                PhotoBackupUploadClient.interpretCreation(statusCode: 201, locationHeader: location, bodyText: nil, retryAfterHeader: nil),
                .unsuccessful(.retryLater(failure: nil, isConnectivityFailure: false)), location ?? "nil")
        }
    }

    func testAnythingButCreatedIsReadAsAnUploadsAnswerIs() {
        XCTAssertEqual(
            PhotoBackupUploadClient.interpretCreation(statusCode: 401, locationHeader: nil, bodyText: nil, retryAfterHeader: nil), .unsuccessful(.signInNeeded))
        XCTAssertEqual(
            PhotoBackupUploadClient.interpretCreation(statusCode: 413, locationHeader: nil, bodyText: "the file is too large", retryAfterHeader: nil),
            .unsuccessful(.refused(failure: .fileRefused(serverReason: "the file is too large"))))
        XCTAssertEqual(
            PhotoBackupUploadClient.interpretCreation(statusCode: 503, locationHeader: nil, bodyText: nil, retryAfterHeader: "120"),
            .unsuccessful(.retryLater(failure: nil, isConnectivityFailure: false, retryAfterSeconds: 120)))
        XCTAssertEqual(
            PhotoBackupUploadClient.interpretCreation(statusCode: 429, locationHeader: nil, bodyText: "too many uploads are unfinished", retryAfterHeader: nil),
            .unsuccessful(.retryLater(failure: .serverReason("too many uploads are unfinished"), isConnectivityFailure: false)))
    }

    func testTheStatusSaysHowManyBytesTheServerHasOrThatTheUploadIsComplete() {
        func state(_ statusCode: Int, offset: String? = nil, complete: String? = nil, photoId: String? = nil) -> PhotoBackupServiceAnswer<PhotoBackupServerUpload> {
            PhotoBackupUploadClient.interpretState(
                statusCode: statusCode, offsetHeader: offset, completeHeader: complete, photoIdHeader: photoId, bodyText: nil, retryAfterHeader: nil)
        }
        XCTAssertEqual(state(204, offset: "8388608", complete: "?0"), .answered(.receiving(offsetBytes: 8_388_608)))
        XCTAssertEqual(state(204, offset: "0", complete: "?0"), .answered(.receiving(offsetBytes: 0)))
        XCTAssertEqual(state(204, offset: "512", complete: "?1", photoId: "p1"), .answered(.complete(serverPhotoId: "p1")))
        XCTAssertEqual(state(404), .answered(.gone))
        XCTAssertEqual(state(410), .answered(.gone))
        XCTAssertEqual(state(204), .unsuccessful(.retryLater(failure: nil, isConnectivityFailure: false)), "no offset")
        XCTAssertEqual(state(204, offset: "-1"), .unsuccessful(.retryLater(failure: nil, isConnectivityFailure: false)))
        XCTAssertEqual(state(204, offset: "512", complete: "?1"), .unsuccessful(.retryLater(failure: .serverDidNotSayWhichPhoto, isConnectivityFailure: false)))
        XCTAssertEqual(state(401), .unsuccessful(.signInNeeded))
        XCTAssertEqual(state(500), .unsuccessful(.retryLater(failure: nil, isConnectivityFailure: false)))
    }

    func testTheClientSendsTheRequestThroughTheTransportAndReadsTheAnswer() async throws {
        let server = try ServerAddress(parsing: "photos.example.com")
        let credential = PhotoBackupCredential(credentialId: "c1", credentialName: "backup", username: "robin", password: "sync-secret")
        let transport = ScriptedTransport(statusCode: 204, headers: ["Upload-Offset": "4096", "Upload-Complete": "?0"])
        let client = PhotoBackupUploadClient(transport: transport)
        let request = PhotoBackupRequest.makeStatus(server: server, credential: credential, uploadPath: "/api/v1/photos/uploads/u1", usesMobileData: false)
        let answer = await client.uploadState(request)
        XCTAssertEqual(answer, .answered(.receiving(offsetBytes: 4096)))
        XCTAssertEqual(transport.sentRequests.map(\.httpMethod), ["HEAD"])
    }

    func testATransportThatFailsIsAReasonToTryLaterAndSaysWhetherTheNetworkWasTheCause() async throws {
        let request = URLRequest(url: try XCTUnwrap(URL(string: "https://photos.example.com/api/v1/photos/uploads")))
        let offline = PhotoBackupUploadClient(transport: ScriptedTransport(error: URLError(.notConnectedToInternet)))
        let creation = await offline.createUpload(request)
        guard case .unsuccessful(.retryLater(_, let isConnectivityFailure, _)) = creation else { return XCTFail("\(creation)") }
        XCTAssertTrue(isConnectivityFailure)
        let cancelled = PhotoBackupUploadClient(transport: ScriptedTransport(error: URLError(.cancelled)))
        let cancelledAnswer = await cancelled.uploadState(request)
        XCTAssertEqual(cancelledAnswer, .unsuccessful(.cancelled))
    }
}

private final class ScriptedTransport: HTTPTransport, @unchecked Sendable {
    private let lock = NSLock()
    private let statusCode: Int
    private let headers: [String: String]
    private let error: Error?
    private var requests: [URLRequest] = []

    init(statusCode: Int = 200, headers: [String: String] = [:], error: Error? = nil) {
        self.statusCode = statusCode
        self.headers = headers
        self.error = error
    }

    var sentRequests: [URLRequest] { lock.withLock { requests } }

    func send(_ request: URLRequest) async throws -> (Data, HTTPURLResponse) {
        lock.withLock { requests.append(request) }
        if let error { throw error }
        let response = try XCTUnwrap(HTTPURLResponse(url: try XCTUnwrap(request.url), statusCode: statusCode, httpVersion: "HTTP/1.1", headerFields: headers))
        return (Data(), response)
    }
}

final class PhotoBackupPartialUploadIndexTests: XCTestCase {
    func testTheAddressSurvivesOpeningTheFileAgain() async throws {
        let directory = try temporaryDirectory(self)
        let index = try PhotoBackupIndex(directory: directory)
        await index.adopt(accountKey: "photos.example.com|robin")
        let partial = PhotoBackupPartialUpload(uploadPath: "/api/v1/photos/uploads/u1", sizeBytes: 3000, sha256: continuedFileDigest)
        await index.markUploadStarted(localIdentifier: "a", clientPhotoId: "local:a", role: .original, partialUpload: partial)
        await index.flush()
        let reopened = try PhotoBackupIndex(directory: directory)
        let kept = await reopened.partialUpload(localIdentifier: "a", role: .original)
        XCTAssertEqual(kept, partial)
        let pendingCount = await reopened.pendingFileCount()
        XCTAssertEqual(pendingCount, 1, "a file with an upload is still a file to send")
    }

    func testAnIndexWrittenBeforeAddressesWereKeptIsStillRead() async throws {
        let directory = try temporaryDirectory(self)
        let written = #"{"version":1,"accountKey":"photos.example.com|robin","records":{"a":{"clientPhotoId":"local:a","roles":{"original":{"isOnServer":false,"refusalCount":0}}}},"libraryItemCount":1,"isSignInNeeded":false}"#
        try Data(written.utf8).write(to: directory.appendingPathComponent(PhotoBackupIndex.indexFilename))
        let index = try PhotoBackupIndex(directory: directory)
        let wasStartedOver = await index.wasStartedOver
        XCTAssertFalse(wasStartedOver)
        let record = await index.record(forLocalIdentifier: "a")
        XCTAssertEqual(record?.roles["original"]?.isOnServer, false)
        XCTAssertNil(record?.roles["original"]?.partialUpload)
    }

    func testAFileThatWentUpOrWasRefusedOrForgottenHasNoAddress() async throws {
        let index = try PhotoBackupIndex(directory: try temporaryDirectory(self))
        let partial = PhotoBackupPartialUpload(uploadPath: "/api/v1/photos/uploads/u1", sizeBytes: 10, sha256: continuedFileDigest)
        for role in [PhotoBackupFileRole.original, .edited, .liveVideo] {
            await index.markUploadStarted(localIdentifier: "a", clientPhotoId: "local:a", role: role, partialUpload: partial)
        }
        await index.markUploaded(localIdentifier: "a", clientPhotoId: "local:a", role: .original, serverPhotoId: "p1", assetModificationDate: nil)
        await index.markRefused(localIdentifier: "a", clientPhotoId: "local:a", role: .edited, failure: .fileRefused(serverReason: nil))
        await index.forgetPartialUpload(localIdentifier: "a", role: .liveVideo)
        for role in [PhotoBackupFileRole.original, .edited, .liveVideo] {
            let kept = await index.partialUpload(localIdentifier: "a", role: role)
            XCTAssertNil(kept, role.rawValue)
        }
    }

    func testEachFileOfAPhotoKeepsItsOwnAddress() async throws {
        let index = try PhotoBackupIndex(directory: try temporaryDirectory(self))
        let original = PhotoBackupPartialUpload(uploadPath: "/api/v1/photos/uploads/u1", sizeBytes: 10, sha256: continuedFileDigest)
        let video = PhotoBackupPartialUpload(uploadPath: "/api/v1/photos/uploads/u2", sizeBytes: 20, sha256: otherFileDigest)
        await index.markUploadStarted(localIdentifier: "a", clientPhotoId: "local:a", role: .original, partialUpload: original)
        await index.markUploadStarted(localIdentifier: "a", clientPhotoId: "local:a", role: .liveVideo, partialUpload: video)
        await index.forgetPartialUpload(localIdentifier: "a", role: .original)
        let keptOriginal = await index.partialUpload(localIdentifier: "a", role: .original)
        let keptVideo = await index.partialUpload(localIdentifier: "a", role: .liveVideo)
        XCTAssertNil(keptOriginal)
        XCTAssertEqual(keptVideo, video)
    }
}

#if canImport(CryptoKit)
    final class PhotoFileDigestTests: XCTestCase {
        func testTheDigestIsTheLowercaseSha256OfTheWholeFileReadInPieces() throws {
            let directory = try temporaryDirectory(self)
            let small = directory.appendingPathComponent("small")
            try Data("abc".utf8).write(to: small)
            XCTAssertEqual(PhotoFileDigest.sha256(ofFileAt: small), "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")
            // More than one piece long: a million 'a' is a published test vector.
            let large = directory.appendingPathComponent("large")
            try Data(repeating: UInt8(ascii: "a"), count: 1_000_000).write(to: large)
            XCTAssertEqual(PhotoFileDigest.sha256(ofFileAt: large), "cdc76e5c9914fb9281a1c7e284d73e67f1809a48a497200e046d39ccc7112cd0")
            XCTAssertNil(PhotoFileDigest.sha256(ofFileAt: directory.appendingPathComponent("missing")))
        }
    }
#endif
