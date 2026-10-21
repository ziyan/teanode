import Foundation
import XCTest

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

@testable import TeaNodeKit

/// Finishing a share leaves the copy current, the server's two warnings about sharing are read,
/// and what a device remembers about an item does not depend on the user id the server names.
final class PasswordsSharingFollowUpTests: XCTestCase {
    private let iterationCount = PasswordsCrypto.minimumPassphraseIterationCount
    private var directories: [URL] = []

    override func tearDown() {
        for directory in directories { try? FileManager.default.removeItem(at: directory) }
        directories = []
        super.tearDown()
    }

    private func makeCache(pinStore: (any PasswordsPinStore)? = nil) -> PasswordsCache {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("passwords-follow-up-\(UUID().uuidString)")
        directories.append(directory)
        return PasswordsCache(fileURL: directory.appendingPathComponent("passwords.json"), pinStore: pinStore)
    }

    private func makeSession(_ server: FakePasswordsServer, userId: String, flags: HookedServerFlags? = nil) -> PasswordsSession {
        let api = FakePasswordsAPI(server: server, userId: userId)
        if let flags {
            return PasswordsSession(api: HookedAPI(inner: api, flags: flags), cache: makeCache(), userId: userId)
        }
        return PasswordsSession(api: api, cache: makeCache(), userId: userId)
    }

    private func makeServer(_ people: [(String, String)]) async -> FakePasswordsServer {
        let server = FakePasswordsServer()
        for (userId, name) in people { await server.addPerson(userId: userId, name: name) }
        return server
    }

    private func loginFields() -> PasswordsItemFields {
        .login(PasswordsLoginFields(name: "Example", userName: "ada@example.com", password: "correct horse", sites: ["https://example.com"]))
    }

    private func setUp(_ session: PasswordsSession, passphrase: String) async throws {
        _ = try await session.setUp(passphrase: passphrase, iterationCount: iterationCount)
    }

    /// Ada shares an item with Grace before Grace has set up Passwords, so Grace is waiting.
    private func shareWithWaitingGrace(
        _ server: FakePasswordsServer, ada: PasswordsSession
    ) async throws -> PasswordItem {
        let item = try await ada.save(fields: loginFields())
        let recipients = try await ada.recipients(granteeUserId: "user-grace")
        return try await ada.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-grace")], recipients: recipients)
    }

    // MARK: The copy after finishing

    func testFinishingSharingReadsTheItemAgainSoTheCopyShowsTheKeyGiven() async throws {
        let server = await makeServer([("user-ada", "Ada"), ("user-grace", "Grace")])
        let ada = makeSession(server, userId: "user-ada")
        try await setUp(ada, passphrase: "ada invented passphrase")
        let item = try await shareWithWaitingGrace(server, ada: ada)
        try await setUp(makeSession(server, userId: "user-grace"), passphrase: "grace invented passphrase")
        try await ada.sync()
        let before = try await ada.item(id: item.id)
        XCTAssertEqual(before.shares.flatMap(\.recipients).map(\.hasKey), [false])

        try await ada.finishSharing(itemId: item.id)
        let after = try await ada.item(id: item.id)
        XCTAssertEqual(after.shares.flatMap(\.recipients).map(\.hasKey), [true], "the copy was read again after the key was added")
    }

    func testAnAutomaticCompletionReadsTheChangedItemsAgainToo() async throws {
        let server = await makeServer([("user-ada", "Ada"), ("user-grace", "Grace")])
        let flags = HookedServerFlags()
        let ada = makeSession(server, userId: "user-ada", flags: flags)
        try await setUp(ada, passphrase: "ada invented passphrase")
        let item = try await shareWithWaitingGrace(server, ada: ada)
        try await setUp(makeSession(server, userId: "user-grace"), passphrase: "grace invented passphrase")
        // This session chose Grace itself, so it may complete the share by itself.
        let completed = try await ada.completeWaitingShares()
        XCTAssertEqual(completed.addedKeyCount, 1)
        let after = try await ada.item(id: item.id)
        XCTAssertEqual(after.shares.flatMap(\.recipients).map(\.hasKey), [true])
        // Sharing again seals for nobody: the copy knows Grace has a key.
        let recipients = try await ada.recipients(granteeUserId: "user-grace")
        _ = try await ada.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-grace")], recipients: recipients)
        XCTAssertEqual(flags.lastShareKeys.count, 0)
    }

    // MARK: Too much shared, and more waiting

    func testAWaitingPersonWithTooMuchSharedIsSkippedAndNamed() async throws {
        let server = await makeServer([("user-ada", "Ada"), ("user-grace", "Grace")])
        let flags = HookedServerFlags()
        let ada = makeSession(server, userId: "user-ada", flags: flags)
        try await setUp(ada, passphrase: "ada invented passphrase")
        let item = try await shareWithWaitingGrace(server, ada: ada)
        try await setUp(makeSession(server, userId: "user-grace"), passphrase: "grace invented passphrase")
        await server.setHasTooMuchShared("user-grace", true)

        let recipients = try await ada.recipients(granteeUserId: "user-grace")
        XCTAssertEqual(recipients.map(\.hasTooMuchShared), [true])
        let waiting = try await ada.waitingShares()
        XCTAssertEqual(waiting.map(\.recipient.hasTooMuchShared), [true])

        let completed = try await ada.completeWaitingShares()
        XCTAssertEqual(completed.addedKeyCount, 0)
        XCTAssertEqual(completed.tooMuchSharedNames, ["Grace"])
        try await ada.finishSharing(itemId: item.id)
        XCTAssertEqual(flags.addedShareKeyCalls.count, 0, "no key is sealed for somebody the server stores none for")
        let needingYou = await ada.itemIdsNeedingYou
        XCTAssertEqual(needingYou, [])

        // Sharing again does not seal for her either.
        _ = try await ada.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-grace")], recipients: recipients)
        XCTAssertEqual(flags.lastShareKeys.count, 0)

        // Once she has room again, the next completion seals for her.
        await server.setHasTooMuchShared("user-grace", false)
        try await ada.finishSharing(itemId: item.id)
        XCTAssertEqual(flags.addedShareKeyCalls.count, 1)
    }

    func testTheCompletionSaysWhenMoreAreWaitingThanTheServerListed() async throws {
        let server = await makeServer([("user-ada", "Ada"), ("user-grace", "Grace")])
        let ada = makeSession(server, userId: "user-ada")
        try await setUp(ada, passphrase: "ada invented passphrase")
        _ = try await shareWithWaitingGrace(server, ada: ada)
        try await setUp(makeSession(server, userId: "user-grace"), passphrase: "grace invented passphrase")
        let plain = try await ada.completeWaitingShares()
        XCTAssertFalse(plain.hasMoreWaiting)

        let second = await makeServer([("user-ada", "Ada"), ("user-grace", "Grace")])
        let adaAgain = makeSession(second, userId: "user-ada")
        try await setUp(adaAgain, passphrase: "ada invented passphrase")
        _ = try await shareWithWaitingGrace(second, ada: adaAgain)
        try await setUp(makeSession(second, userId: "user-grace"), passphrase: "grace invented passphrase")
        await second.setHasMoreWaiting(true)
        let waiting = try await adaAgain.waitingShares()
        XCTAssertEqual(waiting.map(\.hasMoreWaiting), [true])
        let completed = try await adaAgain.completeWaitingShares()
        XCTAssertTrue(completed.hasMoreWaiting)
    }

    func testTheNewFieldsAreReadFromTheServersAnswerAndAnOlderAnswerMeansFalse() throws {
        let withFields = Data(
            """
            {"itemId":"item-one","hasMoreWaiting":true,"recipient":{"userId":"user-grace","name":"Grace","username":"grace","publicKey":null,"hasKey":false,"hasTooMuchShared":true}}
            """.utf8)
        let decoded = try JSONDecoder().decode(PasswordWaitingShareView.self, from: withFields)
        XCTAssertTrue(decoded.hasMoreWaiting)
        XCTAssertTrue(decoded.recipient.hasTooMuchShared)
        let without = Data(
            """
            {"itemId":"item-one","recipient":{"userId":"user-grace","name":"Grace","username":"grace","publicKey":null,"hasKey":false}}
            """.utf8)
        let older = try JSONDecoder().decode(PasswordWaitingShareView.self, from: without)
        XCTAssertFalse(older.hasMoreWaiting)
        XCTAssertFalse(older.recipient.hasTooMuchShared)
        XCTAssertTrue(Documents.waitingPasswordShares.contains("hasMoreWaiting"))
        XCTAssertTrue(Documents.waitingPasswordShares.contains("hasTooMuchShared"))
        XCTAssertTrue(Documents.passwordRecipients.contains("hasTooMuchShared"))
        XCTAssertTrue(Documents.passwordItems.contains("hasTooMuchShared"))
    }

    // MARK: What a device remembers about an item

    private func memory(version: Int) -> PasswordsItemMemory {
        PasswordsItemMemory(highestRecordVersion: version, highestRecordMac: "mac-\(version)")
    }

    private func assertMemoriesFollowTheItemAndNotTheUserId(_ pinStore: any PasswordsPinStore, file: StaticString = #filePath, line: UInt = #line) throws {
        try pinStore.recordSeenRecord(userId: "user-ada", itemId: "item-one", version: 4, mac: "mac-4")
        try pinStore.recordKnownIfAbsent(userId: "user-ada", otherUserId: "user-grace", publicKey: "grace-key")
        let named = try pinStore.loadPins(userId: "user-ada")
        XCTAssertEqual(named.itemMemories["item-one"]?.highestRecordVersion, 4, file: file, line: line)

        // The server names another user id for the same sign-in: what was remembered about the
        // item is still read, and the keys known for other people are not carried over.
        let renamed = try pinStore.loadPins(userId: "user-other")
        XCTAssertEqual(renamed.itemMemories["item-one"]?.highestRecordVersion, 4, file: file, line: line)
        XCTAssertEqual(renamed.itemMemories["item-one"]?.highestRecordMac, "mac-4", file: file, line: line)
        XCTAssertEqual(renamed.knownPublicKeys, [:], file: file, line: line)
        XCTAssertEqual(named.knownPublicKeys["user-grace"], "grace-key", file: file, line: line)
    }

    func testTheMemoryPinStoreKeepsItemMemoriesByItemId() throws {
        try assertMemoriesFollowTheItemAndNotTheUserId(PasswordsMemoryPinStore())
    }

    func testTheFilePinStoreKeepsItemMemoriesByItemId() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("passwords-pins-\(UUID().uuidString)")
        directories.append(directory)
        try assertMemoriesFollowTheItemAndNotTheUserId(PasswordsFilePinStore(fileURL: directory.appendingPathComponent("pins.json")))
    }

    func testTwoCopiesOfAnItemMemoryMergeToTheStricterOne() {
        var older = memory(version: 2)
        older.sealedForUserIds = ["user-ben"]
        older.didMakeAgentCopy = true
        var newer = memory(version: 5)
        newer.sealedForUserIds = ["user-grace"]
        newer.chosenUserIds = ["user-grace"]
        let merged = older.merging(newer)
        XCTAssertEqual(merged.highestRecordVersion, 5)
        XCTAssertEqual(merged.highestRecordMac, "mac-5")
        XCTAssertEqual(merged.sealedForUserIds, ["user-ben", "user-grace"])
        XCTAssertEqual(merged.chosenUserIds, ["user-grace"])
        XCTAssertFalse(merged.didMakeAgentCopy, "the agent copy counts only when both made it")
        XCTAssertEqual(newer.merging(older), merged)
    }
}
