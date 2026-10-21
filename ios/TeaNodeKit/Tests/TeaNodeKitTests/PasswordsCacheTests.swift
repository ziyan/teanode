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

    func testAnEmptyPageChangesNothingAndTheRevisionNeverMovesBackwards() {
        var cache = contents(items: [view("one", revision: 4)], revision: 4)
        let result = cache.merge(PasswordItemListView(items: [], itemIds: ["one"], revision: 4))
        XCTAssertEqual(result, PasswordsMergeResult())
        XCTAssertEqual(cache.items.count, 1)
        // The item holding the highest number was unshared: it goes, and the revision stays,
        // since every later change has a higher number still.
        _ = cache.merge(PasswordItemListView(items: [], itemIds: [], revision: 2))
        XCTAssertEqual(cache.items.count, 0)
        XCTAssertEqual(cache.revision, 4)
        _ = cache.merge(PasswordItemListView(items: [], itemIds: [], revision: 9))
        XCTAssertEqual(cache.revision, 9)
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

    func testTheFilesRoundTripAndNeverHoldAClearValue() throws {
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
        XCTAssertFalse(text.contains("sealed-key"), "the header holds no item")
        try cache.erase()
        XCTAssertNil(try cache.load())
        XCTAssertNoThrow(try cache.erase())
    }

    func testPinnedKeysAreInThePinStoreNotInTheCacheFiles() throws {
        let url = temporaryFile()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let pinStore = PasswordsMemoryPinStore()
        let cache = PasswordsCache(fileURL: url, pinStore: pinStore)
        let stored = contents(items: [view("one", revision: 1, isSharedWithMe: true)], revision: 1)
        try cache.save(stored)
        try cache.recordKnownIfAbsent(userId: "user-ada", otherUserId: "user-grace", publicKey: "grace-key")
        try cache.recordCompared(userId: "user-ada", otherUserId: "user-linus", publicKey: "linus-key")

        var expected = stored
        expected.knownPublicKeys = ["user-grace": "grace-key", "user-linus": "linus-key"]
        expected.comparedPublicKeys = ["user-linus": "linus-key"]
        XCTAssertEqual(try cache.load(), expected)
        XCTAssertEqual(try cache.loadHeader()?.knownPublicKeys, expected.knownPublicKeys)
        // Neither the header nor an item file holds a key, and no pins file is written.
        for fileURL in [url] + (try FileManager.default.contentsOfDirectory(at: cache.itemsDirectoryURL, includingPropertiesForKeys: nil)) {
            let text = String(decoding: try Data(contentsOf: fileURL), as: UTF8.self)
            XCTAssertFalse(text.contains("grace-key"))
            XCTAssertFalse(text.contains("linus-key"))
        }
        XCTAssertFalse(FileManager.default.fileExists(atPath: cache.pinsFileURL.path))
        // The key seen first is not replaced by a later first sight; only an accepted change replaces it.
        try cache.recordKnownIfAbsent(userId: "user-ada", otherUserId: "user-grace", publicKey: "other-key")
        XCTAssertEqual(try cache.loadPins(userId: "user-ada").knownPublicKeys["user-grace"], "grace-key")
        try cache.recordChangedKey(userId: "user-ada", otherUserId: "user-grace", publicKey: "other-key")
        var pins = try cache.loadPins(userId: "user-ada")
        XCTAssertEqual(pins.knownPublicKeys["user-grace"], "other-key")
        XCTAssertEqual(pins.changedPublicKeys["user-grace"], "grace-key", "the key it replaced is kept for the notice")
        // Comparing codes marks the key and clears the notice; a dismissed notice keeps the new key.
        try cache.recordCompared(userId: "user-ada", otherUserId: "user-grace", publicKey: "other-key")
        pins = try cache.loadPins(userId: "user-ada")
        XCTAssertEqual(pins.comparedPublicKeys["user-grace"], "other-key")
        XCTAssertTrue(pins.isCompared(userId: "user-grace"))
        XCTAssertEqual(pins.changedPublicKeys, [:])
        try cache.recordChangedKey(userId: "user-ada", otherUserId: "user-grace", publicKey: "third-key")
        pins = try cache.loadPins(userId: "user-ada")
        XCTAssertFalse(pins.isCompared(userId: "user-grace"), "a new key is not the compared one")
        XCTAssertEqual(pins.changedPublicKeys["user-grace"], "other-key")
        try cache.dismissKeyChange(userId: "user-ada", otherUserId: "user-grace")
        pins = try cache.loadPins(userId: "user-ada")
        XCTAssertEqual(pins.changedPublicKeys, [:])
        XCTAssertEqual(pins.knownPublicKeys["user-grace"], "third-key")
    }

    func testPinsWrittenBeforeThereWasOneKnownKeyAreReadAsKnownAndCompared() throws {
        let url = temporaryFile()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = PasswordsCache(fileURL: url)
        try cache.save(contents(items: [], revision: 0))
        // The first version kept a seen key for Grace and a confirmed key for Linus (also recorded as seen).
        let text = #"""
            {"formatVersion":1,"userId":"user-ada","seenPublicKeys":{"user-grace":"grace-key","user-linus":"linus-key"},
             "confirmedPublicKeys":{"user-linus":"linus-key"}}
            """#
        try Data(text.utf8).write(to: cache.pinsFileURL)
        let pins = try cache.loadPins(userId: "user-ada")
        XCTAssertEqual(pins.knownPublicKeys, ["user-grace": "grace-key", "user-linus": "linus-key"])
        XCTAssertEqual(pins.comparedPublicKeys, ["user-linus": "linus-key"])
        XCTAssertFalse(pins.isCompared(userId: "user-grace"))
        XCTAssertTrue(pins.isCompared(userId: "user-linus"))
        // The next change is written in the new shape.
        try cache.recordKnownIfAbsent(userId: "user-ada", otherUserId: "user-heidi", publicKey: "heidi-key")
        let written = String(decoding: try Data(contentsOf: cache.pinsFileURL), as: UTF8.self)
        XCTAssertTrue(written.contains("knownPublicKeys"))
        XCTAssertFalse(written.contains("seenPublicKeys"))
        XCTAssertEqual(try cache.loadPins(userId: "user-ada").knownPublicKeys.count, 3)
    }

    func testAPlantedPinsFileIsNotReadWhenThePinsAreInAnotherStore() throws {
        let url = temporaryFile()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = PasswordsCache(fileURL: url, pinStore: PasswordsMemoryPinStore())
        try cache.save(contents(items: [], revision: 0))
        // What anything that can write to the app group's folder can do.
        let planted = PasswordsPins(userId: "user-ada", knownPublicKeys: ["user-mallory": "mallory-key"])
        try JSONEncoder().encode(planted).write(to: cache.pinsFileURL)
        XCTAssertEqual(try cache.loadPins(userId: "user-ada"), PasswordsPins(userId: "user-ada"))
        XCTAssertEqual(try XCTUnwrap(try cache.load()).knownPublicKeys, [:])
    }

    func testAPinsFileThatCannotBeReadIsNeverWrittenOverOrTakenForEmpty() throws {
        let url = temporaryFile()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = PasswordsCache(fileURL: url)
        try cache.save(contents(items: [], revision: 0))
        try cache.recordCompared(userId: "user-ada", otherUserId: "user-grace", publicKey: "grace-key")
        let stored = try Data(contentsOf: cache.pinsFileURL)
        let damaged = Data("{\"formatVersion\":1,\"userId\":\"user-ada\",\"seenPublicKeys\":5}".utf8)
        try damaged.write(to: cache.pinsFileURL)
        XCTAssertThrowsError(try cache.loadPins(userId: "user-ada")) { error in
            XCTAssertEqual(error as? PasswordsPinStoreError, .unreadable)
        }
        XCTAssertThrowsError(try cache.recordKnownIfAbsent(userId: "user-ada", otherUserId: "user-linus", publicKey: "linus-key"))
        XCTAssertThrowsError(try cache.recordCompared(userId: "user-ada", otherUserId: "user-linus", publicKey: "linus-key"))
        XCTAssertEqual(try Data(contentsOf: cache.pinsFileURL), damaged)
        try stored.write(to: cache.pinsFileURL)
        XCTAssertEqual(try cache.loadPins(userId: "user-ada").comparedPublicKeys, ["user-grace": "grace-key"])
    }

    func testErasingKeepingPinsLeavesThePinsAndErasingRemovesEverything() throws {
        let url = temporaryFile()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = PasswordsCache(fileURL: url)
        try cache.save(contents(items: [view("one", revision: 1)], revision: 1))
        try cache.saveSummaryData(Data("sealed".utf8))
        try cache.recordKnownIfAbsent(userId: "user-ada", otherUserId: "user-grace", publicKey: "grace-key")
        try cache.eraseKeepingPins()
        XCTAssertNil(try cache.load())
        XCTAssertNil(try cache.loadSummaryData())
        XCTAssertFalse(FileManager.default.fileExists(atPath: cache.itemsDirectoryURL.path))
        XCTAssertEqual(try cache.loadPins(userId: "user-ada").knownPublicKeys, ["user-grace": "grace-key"])
        // Another person's pins are never read as this person's.
        XCTAssertEqual(try cache.loadPins(userId: "user-other"), PasswordsPins(userId: "user-other"))
        try cache.erase()
        XCTAssertEqual(try cache.loadPins(userId: "user-ada"), PasswordsPins(userId: "user-ada"))
        XCTAssertFalse(FileManager.default.fileExists(atPath: cache.pinsFileURL.path))
        XCTAssertNoThrow(try cache.erase())
    }

    func testRemovingTheCopyOfASignInRemovesItsFiles() throws {
        let container = FileManager.default.temporaryDirectory.appendingPathComponent("passwords-location-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: container) }
        let cache = PasswordsCache(fileURL: PasswordsLocation.fileURL(groupContainer: container, signInAccountKey: "demo.example.com|ada"))
        try cache.save(contents(items: [view("one", revision: 1)], revision: 1))
        try cache.saveSummaryData(Data("sealed".utf8))
        try cache.recordCompared(userId: "user-ada", otherUserId: "user-grace", publicKey: "grace-key")
        PasswordsLocation.removeCopy(groupContainer: container, signInAccountKey: "demo.example.com|ada")
        XCTAssertFalse(FileManager.default.fileExists(atPath: cache.fileURL.path))
        XCTAssertFalse(FileManager.default.fileExists(atPath: cache.itemsDirectoryURL.path))
        XCTAssertFalse(FileManager.default.fileExists(atPath: cache.summaryFileURL.path))
        XCTAssertFalse(FileManager.default.fileExists(atPath: cache.pinsFileURL.path))
    }

    func testADamagedFileAnOlderFormatAndANewerFormatAreTyped() throws {
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
        // A copy that held its items in the one file is not used: its items have no files.
        var older = contents(items: [], revision: 0)
        older.formatVersion = 1
        try JSONEncoder().encode(older).write(to: url)
        XCTAssertThrowsError(try PasswordsCache(fileURL: url).load()) { error in
            XCTAssertEqual(error as? PasswordsCacheError, .unreadable)
        }
    }

    func testEachItemHasAFileOfItsOwnAndSavingWritesOnlyWhatChanged() throws {
        let url = temporaryFile()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = PasswordsCache(fileURL: url)
        try cache.save(contents(items: [view("one", revision: 1), view("two", revision: 2)], revision: 2))
        XCTAssertEqual(try cache.loadItem(id: "two")?.revision, 2)
        XCTAssertNil(try cache.loadItem(id: "three"))
        XCTAssertEqual(try cache.loadHeader()?.items, [])
        XCTAssertEqual(try cache.load()?.items.map(\.id), ["one", "two"])

        // An item's file is named by a hash, so no id can name another file or a folder.
        let hostileId = "../../outside/" + String(repeating: "x", count: 600)
        XCTAssertFalse(PasswordsCache.itemFileName(id: hostileId).contains("/"))
        XCTAssertEqual(PasswordsCache.itemFileName(id: hostileId).count, 64 + 5)
        try cache.save(contents(items: [view(hostileId, revision: 3)], revision: 3), changedItemIds: [hostileId])
        XCTAssertEqual(try cache.loadItem(id: hostileId)?.id, hostileId)

        // Only the changed item is written, and a removed one's file goes.
        let oneFile = cache.itemFileURL(id: "one")
        let before = try Data(contentsOf: oneFile)
        try FileManager.default.removeItem(at: oneFile)
        try cache.save(contents(items: [view("one", revision: 1), view("two", revision: 5, fields: "changed")], revision: 5), changedItemIds: ["two"], removedItemIds: [hostileId])
        XCTAssertFalse(FileManager.default.fileExists(atPath: oneFile.path), "an unchanged item is not written again")
        XCTAssertNil(try cache.loadItem(id: hostileId))
        XCTAssertEqual(try cache.loadItem(id: "two")?.encryptedFields, "changed")
        try before.write(to: oneFile)
        // Saving everything removes the files of items that are not in the contents.
        try cache.save(contents(items: [view("two", revision: 5, fields: "changed")], revision: 5))
        XCTAssertEqual(try cache.load()?.items.map(\.id), ["two"])
    }

    func testSavingMergesTheRevisionWithWhatIsOnDiskAndNeverMovesItBackwards() throws {
        let url = temporaryFile()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = PasswordsCache(fileURL: url)
        XCTAssertEqual(try cache.save(contents(items: [], revision: 9)), 9)
        // A writer that read the copy earlier saves with the revision it had.
        XCTAssertEqual(try cache.save(contents(items: [], revision: 4)), 9)
        XCTAssertEqual(try cache.loadHeader()?.revision, 9)
        XCTAssertEqual(try cache.save(contents(items: [], revision: 11)), 11)
        // A copy of another setup (another public key) is not merged with.
        var other = contents(items: [], revision: 2)
        other.publicKey = "another-key"
        XCTAssertEqual(try cache.save(other), 2)
    }

    func testTheSummaryFileIsUpdatedUnderTheWriteLock() throws {
        let url = temporaryFile()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = PasswordsCache(fileURL: url)
        XCTAssertNil(try cache.loadSummaryData())
        try cache.updateSummaryData { current in
            XCTAssertNil(current)
            return Data("first".utf8)
        }
        try cache.updateSummaryData { current in
            XCTAssertEqual(current, Data("first".utf8))
            return Data("second".utf8)
        }
        XCTAssertEqual(try cache.loadSummaryData(), Data("second".utf8))
        try cache.updateSummaryData { _ in nil }
        XCTAssertNil(try cache.loadSummaryData())
    }
}
