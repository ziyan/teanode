import Foundation
import XCTest

@testable import TeaNodeKit

final class PhotoPreviewCacheTests: XCTestCase {
    private var directory: URL!

    override func setUpWithError() throws {
        directory = FileManager.default.temporaryDirectory.appendingPathComponent("photo-preview-cache-\(UUID().uuidString)", isDirectory: true)
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(at: directory)
    }

    /// A server that answers every address with that many bytes, so the
    /// test knows how large each preview is.
    private func connection(previewBytes: Int) throws -> (Connection, FakeServer) {
        let answer = String(repeating: "x", count: previewBytes)
        let fake = FakeServer { _ in (200, answer) }
        let account = Account(server: try ServerAddress(parsing: "assistant.example.com"), clientId: "test-client", accessToken: "tnt_test", refreshToken: "", expiresAt: Date().addingTimeInterval(3600), username: "test-owner")
        return (Connection(account: account, transport: fake), fake)
    }

    private func files() throws -> [String] {
        try FileManager.default.contentsOfDirectory(atPath: directory.path).sorted()
    }

    private func setAge(_ seconds: TimeInterval, ofFileFor path: String, accountKey: String) throws {
        let name = PhotoPreviewCache.cacheFilename(accountKey: accountKey, path: path)
        try FileManager.default.setAttributes([.modificationDate: Date().addingTimeInterval(-seconds)], ofItemAtPath: directory.appendingPathComponent(name).path)
    }

    func testAPreviewIsFetchedOnceAndThenKeptOnDisk() async throws {
        let (connection, fake) = try connection(previewBytes: 40)
        let cache = PhotoPreviewCache(directory: directory, limitBytes: 1_000)
        let path = "/api/v1/photos/p1/previews/small?v=1"
        let first = try await cache.data(for: path, using: connection)
        let second = try await cache.data(for: path, using: connection)
        XCTAssertEqual(first.count, 40)
        XCTAssertEqual(second, first)
        XCTAssertEqual(fake.sent.count, 1)
        XCTAssertEqual(fake.sent.first?.value(forHTTPHeaderField: "Authorization"), "Bearer tnt_test")
        // A cache made afterwards, as after the app is launched again, finds the file.
        let later = PhotoPreviewCache(directory: directory, limitBytes: 1_000)
        let third = try await later.data(for: path, using: connection)
        XCTAssertEqual(third, first)
        XCTAssertEqual(fake.sent.count, 1)
        let size = await later.sizeBytes()
        XCTAssertEqual(size, 40)
    }

    func testAPreviewThatCannotBeReadIsNotKept() async throws {
        let fake = FakeServer { _ in (404, "no such photo") }
        let account = Account(server: try ServerAddress(parsing: "assistant.example.com"), clientId: "test-client", accessToken: "tnt_test", refreshToken: "", expiresAt: Date().addingTimeInterval(3600), username: "test-owner")
        let connection = Connection(account: account, transport: fake)
        let cache = PhotoPreviewCache(directory: directory, limitBytes: 1_000)
        do {
            _ = try await cache.data(for: "/api/v1/photos/gone/previews/small", using: connection)
            XCTFail("a refused preview was returned")
        } catch {}
        let kept = FileManager.default.fileExists(atPath: directory.path) ? try files() : []
        XCTAssertTrue(kept.isEmpty)
    }

    func testTheOldestFilesGoWhenTheCacheIsOverItsLimit() async throws {
        let (connection, _) = try connection(previewBytes: 60)
        // Room for two previews of 60 bytes, not three; going over takes the
        // files down to 80% of the limit (136 bytes), which is two.
        let cache = PhotoPreviewCache(directory: directory, limitBytes: 170)
        let paths = (1...3).map { "/api/v1/photos/p\($0)/previews/small?v=1" }
        _ = try await cache.data(for: paths[0], using: connection)
        _ = try await cache.data(for: paths[1], using: connection)
        try setAge(1_000, ofFileFor: paths[0], accountKey: connection.accountKey)
        try setAge(500, ofFileFor: paths[1], accountKey: connection.accountKey)
        _ = try await cache.data(for: paths[2], using: connection)
        let names = Set(paths.map { PhotoPreviewCache.cacheFilename(accountKey: connection.accountKey, path: $0) })
        let kept = Set(try files())
        XCTAssertEqual(kept, [PhotoPreviewCache.cacheFilename(accountKey: connection.accountKey, path: paths[1]), PhotoPreviewCache.cacheFilename(accountKey: connection.accountKey, path: paths[2])])
        XCTAssertTrue(kept.isSubset(of: names))
        let size = await cache.sizeBytes()
        XCTAssertEqual(size, 120)
    }

    func testAPreviewSeenAgainIsTheLastToGo() async throws {
        let (connection, fake) = try connection(previewBytes: 60)
        let first = PhotoPreviewCache(directory: directory, limitBytes: 170)
        let paths = (1...3).map { "/api/v1/photos/p\($0)/previews/small?v=1" }
        _ = try await first.data(for: paths[0], using: connection)
        _ = try await first.data(for: paths[1], using: connection)
        try setAge(1_000, ofFileFor: paths[0], accountKey: connection.accountKey)
        try setAge(500, ofFileFor: paths[1], accountKey: connection.accountKey)
        // The person scrolls back to the first one, in a later launch, so it
        // is read from its file and its date moves up to now.
        let later = PhotoPreviewCache(directory: directory, limitBytes: 170)
        _ = try await later.data(for: paths[0], using: connection)
        XCTAssertEqual(fake.sent.count, 2)
        _ = try await later.data(for: paths[2], using: connection)
        let kept = Set(try files())
        XCTAssertEqual(kept, [PhotoPreviewCache.cacheFilename(accountKey: connection.accountKey, path: paths[0]), PhotoPreviewCache.cacheFilename(accountKey: connection.accountKey, path: paths[2])])
    }

    func testGoingOverTheLimitMakesRoomForTheNextWritesWithoutReadingTheDirectoryAgain() async throws {
        let (connection, _) = try connection(previewBytes: 100)
        let cache = PhotoPreviewCache(directory: directory, limitBytes: 1_000)
        for number in 1...10 {
            _ = try await cache.data(for: "/api/v1/photos/p\(number)/previews/small?v=1", using: connection)
        }
        var names = try files()
        XCTAssertEqual(names.count, 10, "at the limit, not over it")
        _ = try await cache.data(for: "/api/v1/photos/p11/previews/small?v=1", using: connection)
        names = try files()
        XCTAssertEqual(names.count, 8, "over the limit, the files are taken down to 80% of it")
        var size = await cache.sizeBytes()
        XCTAssertEqual(size, 800)
        for number in 12...13 {
            _ = try await cache.data(for: "/api/v1/photos/p\(number)/previews/small?v=1", using: connection)
        }
        names = try files()
        XCTAssertEqual(names.count, 10, "the next writes fit in the room that was made, and nothing is removed")
        size = await cache.sizeBytes()
        XCTAssertEqual(size, 1_000)
    }

    func testAPreviewLargerThanTheWholeLimitIsStillReturned() async throws {
        let (connection, _) = try connection(previewBytes: 200)
        let cache = PhotoPreviewCache(directory: directory, limitBytes: 100)
        let data = try await cache.data(for: "/api/v1/photos/big/previews/large", using: connection)
        XCTAssertEqual(data.count, 200)
        XCTAssertTrue(try files().isEmpty)
    }

    func testRemovingEverythingEmptiesTheCache() async throws {
        let (connection, fake) = try connection(previewBytes: 10)
        let cache = PhotoPreviewCache(directory: directory, limitBytes: 1_000)
        _ = try await cache.data(for: "/api/v1/photos/p1/previews/small", using: connection)
        await cache.removeAll()
        let size = await cache.sizeBytes()
        XCTAssertEqual(size, 0)
        _ = try await cache.data(for: "/api/v1/photos/p1/previews/small", using: connection)
        XCTAssertEqual(fake.sent.count, 2)
    }

    func testFileNamesAreStableAndTellAddressesApart() {
        let name = PhotoPreviewCache.cacheFilename(accountKey: "a", path: "/p?v=1")
        XCTAssertEqual(name, PhotoPreviewCache.cacheFilename(accountKey: "a", path: "/p?v=1"))
        XCTAssertEqual(name.count, 32)
        XCTAssertNotEqual(name, PhotoPreviewCache.cacheFilename(accountKey: "a", path: "/p?v=2"))
        XCTAssertNotEqual(name, PhotoPreviewCache.cacheFilename(accountKey: "b", path: "/p?v=1"))
    }
}
