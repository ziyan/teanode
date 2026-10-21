import Foundation
import XCTest

@testable import TeaNodeKit

final class PasswordsCacheTests: XCTestCase {
    private func view(_ id: String, revision: Int64, fields: String = "sealed", isSharedWithMe: Bool = false) -> PasswordItemView {
        PasswordItemView(
            id: id, ownerUserId: "user-ada", ownerName: "Ada", ownerPublicKey: "owner-key", itemKind: "login", encryptedFields: fields, itemKey: "sealed-key",
            isSharedWithMe: isSharedWithMe, revision: revision, isUsableByAgent: false, createdAt: "2026-10-20T10:00:00Z",
            updatedAt: "2026-10-20T10:00:00Z")
    }

    private func contents(items: [PasswordItemView], revision: Int64) -> PasswordsCacheContents {
        PasswordsCacheContents(
            userId: "user-ada", publicKey: "key", privateKeyByAccountKey: "sealed-private-key", revision: revision, lockAfterMinutes: 15,
            items: items)
    }

    func testMergeReplacesChangedItemsAddsNewOnesAndDropsWhatTheServerNoLongerLists() {
        var cache = contents(items: [view("one", revision: 1), view("two", revision: 2), view("three", revision: 3)], revision: 3)
        let result = cache.merge(
            PasswordItemListView(
                items: [view("two", revision: 5, fields: "changed"), view("four", revision: 6)], itemIds: ["one", "two", "four"], revision: 6))
        XCTAssertEqual(result.changedItemIds, ["two", "four"])
        XCTAssertEqual(result.removedItemIds, ["three"])
        XCTAssertEqual(cache.items.map(\.id), ["one", "two", "four"])
        XCTAssertEqual(cache.item(id: "two")?.encryptedFields, "changed")
        XCTAssertEqual(cache.item(id: "one")?.revision, 1)
        XCTAssertEqual(cache.revision, 6)
    }

    func testAnEmptyPageChangesNothingAndTheRevisionFollowsTheServer() {
        var cache = contents(items: [view("one", revision: 4)], revision: 4)
        let result = cache.merge(PasswordItemListView(items: [], itemIds: ["one"], revision: 4))
        XCTAssertEqual(result, PasswordsMergeResult())
        XCTAssertEqual(cache.items.count, 1)
        // The item holding the highest number was unshared: the revision comes down with it.
        _ = cache.merge(PasswordItemListView(items: [], itemIds: [], revision: 2))
        XCTAssertEqual(cache.items.count, 0)
        XCTAssertEqual(cache.revision, 2)
    }

    func testReplaceKeepsTheRevisionOfTheCopy() {
        var cache = contents(items: [view("one", revision: 1)], revision: 1)
        cache.replace(view("one", revision: 9, fields: "saved"))
        cache.replace(view("two", revision: 10))
        XCTAssertEqual(cache.revision, 1)
        XCTAssertEqual(cache.items.map(\.id), ["one", "two"])
        XCTAssertEqual(cache.item(id: "one")?.encryptedFields, "saved")
    }

    private func temporaryFile() -> URL {
        FileManager.default.temporaryDirectory.appendingPathComponent("passwords-cache-\(UUID().uuidString)").appendingPathComponent("passwords.json")
    }

    func testTheFileRoundTripsAndNeverHoldsAClearValue() throws {
        let url = temporaryFile()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = PasswordsCache(fileURL: url)
        XCTAssertNil(try cache.load())
        let stored = contents(items: [view("one", revision: 1), view("two", revision: 2, isSharedWithMe: true)], revision: 2)
        try cache.save(stored)
        XCTAssertEqual(try cache.load(), stored)
        let text = String(decoding: try Data(contentsOf: url), as: UTF8.self)
        XCTAssertTrue(text.contains("sealed-private-key"))
        XCTAssertFalse(text.contains("passphrase"))
        try cache.erase()
        XCTAssertNil(try cache.load())
        XCTAssertNoThrow(try cache.erase())
    }

    func testKnownPublicKeysRoundTripAndAnOlderFileWithoutThemReadsAsNoneKnown() throws {
        let url = temporaryFile()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = PasswordsCache(fileURL: url)
        var stored = contents(items: [view("one", revision: 1, isSharedWithMe: true)], revision: 1)
        stored.knownPublicKeys = ["user-grace": "grace-key"]
        try cache.save(stored)
        XCTAssertEqual(try cache.load()?.knownPublicKeys, ["user-grace": "grace-key"])

        // The same file as an older build wrote it: no knownPublicKeys, and no ownerPublicKey on the item.
        var object = try XCTUnwrap(try JSONSerialization.jsonObject(with: Data(contentsOf: url)) as? [String: Any])
        object["knownPublicKeys"] = nil
        var items = try XCTUnwrap(object["items"] as? [[String: Any]])
        items[0]["ownerPublicKey"] = nil
        object["items"] = items
        try JSONSerialization.data(withJSONObject: object).write(to: url)
        let older = try XCTUnwrap(try cache.load())
        XCTAssertEqual(older.knownPublicKeys, [:])
        XCTAssertEqual(older.items.first?.ownerPublicKey, "")
    }

    func testADamagedFileAndANewerFormatAreTyped() throws {
        let url = temporaryFile()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        try Data("not json".utf8).write(to: url)
        XCTAssertThrowsError(try PasswordsCache(fileURL: url).load()) { error in
            XCTAssertEqual(error as? PasswordsCacheError, .unreadable)
        }
        var newer = contents(items: [], revision: 0)
        newer.formatVersion = 99
        try JSONEncoder().encode(newer).write(to: url)
        XCTAssertThrowsError(try PasswordsCache(fileURL: url).load()) { error in
            XCTAssertEqual(error as? PasswordsCacheError, .unsupportedVersion)
        }
    }
}
