import Foundation
import XCTest

@testable import TeaNodeKit

/// TestClock is a clock a test moves by hand.
final class TestClock: @unchecked Sendable {
    var date = Date(timeIntervalSince1970: 1_800_000_000)

    func advance(minutes: Double) {
        date = date.addingTimeInterval(minutes * 60)
    }
}

/// PasswordsRecordTests check that the access record sealed in an item is the person's own
/// (signed with the account key, not older than one already seen), that a new key given without
/// the person's say never reaches further than the old one did, and that the share screen's
/// split between what the record lists and what only the server lists is the same everywhere.
final class PasswordsRecordTests: XCTestCase {
    private let iterationCount = PasswordsCrypto.minimumPassphraseIterationCount
    private var directories: [URL] = []

    override func tearDown() {
        for directory in directories { try? FileManager.default.removeItem(at: directory) }
        directories = []
        super.tearDown()
    }

    // MARK: Helpers

    private func makeCache() -> PasswordsCache {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("passwords-record-\(UUID().uuidString)")
        directories.append(directory)
        return PasswordsCache(fileURL: directory.appendingPathComponent("passwords.json"), pinStore: PasswordsMemoryPinStore())
    }

    private func makeSession(
        _ server: FakePasswordsServer, userId: String, flags: HookedServerFlags? = nil, cache: PasswordsCache? = nil,
        clock: TestClock? = nil
    ) -> PasswordsSession {
        let api = FakePasswordsAPI(server: server, userId: userId)
        let cacheToUse = cache ?? makeCache()
        let now: @Sendable () -> Date = { clock?.date ?? Date() }
        if let flags {
            return PasswordsSession(api: HookedAPI(inner: api, flags: flags), cache: cacheToUse, userId: userId, now: now)
        }
        return PasswordsSession(api: api, cache: cacheToUse, userId: userId, now: now)
    }

    private func makeServer(_ people: [(String, String)]) async -> FakePasswordsServer {
        let server = FakePasswordsServer()
        for (userId, name) in people { await server.addPerson(userId: userId, name: name) }
        return server
    }

    private func loginFields(name: String = "Example", password: String = "correct horse") -> PasswordsItemFields {
        .login(PasswordsLoginFields(name: name, userName: "ada@example.com", password: password, sites: ["https://example.com"]))
    }

    private func setUp(_ session: PasswordsSession, passphrase: String) async throws {
        _ = try await session.setUp(passphrase: passphrase, iterationCount: iterationCount)
    }

    private let adaPassphrase = "ada invented passphrase"

    /// What a holder of the item key can do: open the item's key (with the account key, which the
    /// test has, standing in for the key a grantee was given) and seal other fields with it.
    private func replaceFields(
        of itemId: String, server: FakePasswordsServer, accountKey: Data, access: PasswordsItemAccess?, needsNewKey: Bool = false
    ) async throws {
        let storedOptional = await server.item(itemId)
        let stored = try XCTUnwrap(storedOptional)
        let itemKey = try PasswordsCrypto.open(
            try PasswordsSealedValue(jsonText: stored.itemKeyByAccountKey), key: accountKey,
            associatedData: PasswordsCrypto.itemKeyAssociatedData(itemId: itemId))
        let kind = try XCTUnwrap(PasswordsItemKind(rawValue: stored.itemKind))
        let opened = try PasswordsCrypto.openItemFields(
            try PasswordsSealedValue(jsonText: stored.encryptedFields), itemKey: itemKey, itemId: itemId, itemKind: kind)
        let forged = try PasswordsCrypto.sealItemFields(opened.withAccess(access), itemKey: itemKey, itemId: itemId)
        await server.replaceStoredFields(
            itemId: itemId, encryptedFields: forged.jsonText, itemKeyByAccountKey: stored.itemKeyByAccountKey, needsNewKey: needsNewKey)
    }

    /// A signature made with another account key, as a holder of the item key who is not the owner would have to.
    private func foreignMac(itemId: String, access: PasswordsItemAccess) throws -> String {
        try PasswordsCrypto.macOfAccess(accountKey: PasswordsCrypto.makeKey(), itemId: itemId, itemKind: .login, access: access)
    }

    /// Ada owns an item shared with Ben; both have set up Passwords.
    private struct Setup {
        let server: FakePasswordsServer
        let flags: HookedServerFlags
        let ada: PasswordsSession
        let adaCache: PasswordsCache
        let accountKey: Data
        let itemId: String
    }

    private func makeSharedItem(
        extraPeople: [(String, String)] = [], sharedWith: [String] = ["user-ben"], clock: TestClock? = nil
    ) async throws -> Setup {
        let server = await makeServer([("user-ada", "Ada"), ("user-ben", "Ben"), ("user-mallory", "Mallory")] + extraPeople)
        let flags = HookedServerFlags()
        let cache = makeCache()
        let ada = makeSession(server, userId: "user-ada", flags: flags, cache: cache, clock: clock)
        try await setUp(ada, passphrase: adaPassphrase)
        for (userId, name) in [("user-ben", "Ben"), ("user-mallory", "Mallory")] + extraPeople {
            try await setUp(makeSession(server, userId: userId), passphrase: "\(name) invented passphrase")
        }
        let accountKey = try await makeSession(server, userId: "user-ada").unlock(passphrase: adaPassphrase).accountKey
        let item = try await ada.save(fields: loginFields())
        var recipients: [PasswordRecipient] = []
        for userId in sharedWith { recipients += try await ada.recipients(granteeUserId: userId) }
        _ = try await ada.setShares(
            itemId: item.id, grantees: sharedWith.map { PasswordShareGranteeInput(granteeUserId: $0) }, recipients: recipients)
        return Setup(server: server, flags: flags, ada: ada, adaCache: cache, accountKey: accountKey, itemId: item.id)
    }

    // MARK: A record the person did not sign

    func testARecordForgedByAnItemKeyHolderReachesNobodyAndLetsTheAgentNowhere() async throws {
        let setup = try await makeSharedItem()
        let forgedGrantees: [PasswordsGrantee] = [.person(userId: "user-ben"), .person(userId: "user-mallory")]
        let unsigned = PasswordsItemAccess(isUsableByAgent: true, grantees: forgedGrantees)
        let missingMac = PasswordsItemAccess(isUsableByAgent: true, grantees: forgedGrantees, recordVersion: 99)
        var wrongMac = PasswordsItemAccess(isUsableByAgent: true, grantees: forgedGrantees, recordVersion: 99)
        wrongMac.recordMac = try foreignMac(itemId: setup.itemId, access: wrongMac)
        // A signature that was right for another version of the same record.
        let signed = try PasswordsCrypto.signAccess(
            accountKey: setup.accountKey, itemId: setup.itemId, itemKind: .login,
            access: PasswordsItemAccess(isUsableByAgent: false, grantees: []), highestVersionSeen: 40)
        var tampered = PasswordsItemAccess(isUsableByAgent: true, grantees: forgedGrantees, recordVersion: signed.recordVersion)
        tampered.recordMac = signed.recordMac

        for forged in [unsigned, missingMac, wrongMac, tampered] {
            try await replaceFields(
                of: setup.itemId, server: setup.server, accountKey: setup.accountKey, access: forged, needsNewKey: true)
            // The server adds its own word to the forged consent: Mallory is a grantee, and the agent flag is on.
            _ = try await setup.server.setShares(
                userId: "user-ada", itemId: setup.itemId,
                grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben"), PasswordShareGranteeInput(granteeUserId: "user-mallory")],
                keys: [])
            _ = try await setup.server.setUsableByAgent(
                userId: "user-ada", itemId: setup.itemId, isUsableByAgent: true, itemKeyByAgentKey: "forged agent copy")
            try await setup.ada.sync(isFull: true)

            let item = try await setup.ada.item(id: setup.itemId)
            XCTAssertEqual(item.sharingCheck, .unverified)
            XCTAssertNil(item.verifiedAccess)
            XCTAssertFalse(item.isUsableByAgent)
            XCTAssertTrue(item.recordedShares.isEmpty)
            XCTAssertEqual(Set(item.unrecordedShares.compactMap(\.granteeUserId)), ["user-ben", "user-mallory"])

            // Nothing is sealed for anybody from it, and nothing re-keys.
            setup.flags.calls = []
            let completed = try await setup.ada.completeWaitingShares()
            XCTAssertEqual(completed.addedKeyCount, 0)
            XCTAssertEqual(setup.flags.calls.filter { $0 == "addPasswordShareKeys" }, [])
            let given = await setup.ada.giveNewKeysToMarkedItems()
            XCTAssertEqual(given, [])
            XCTAssertFalse(setup.flags.calls.contains("rekeyPasswordItem"))
            do {
                _ = try await setup.ada.setUsableByAgent(itemId: setup.itemId, isUsable: true)
                XCTFail("the agent must not be let in on a record that was not checked")
            } catch PasswordsError.sharingCouldNotBeChecked {
            }
            do {
                _ = try await setup.ada.giveNewKey(itemId: setup.itemId)
                XCTFail("a new key must not be built on a record that was not checked")
            } catch PasswordsError.sharingCouldNotBeChecked {
            }
            XCTAssertFalse(setup.flags.calls.contains("setPasswordItemUsableByAgent"))
        }
    }

    func testAnEditOfAnItemWhoseRecordWasNotCheckedKeepsItUnchecked() async throws {
        let setup = try await makeSharedItem()
        try await replaceFields(
            of: setup.itemId, server: setup.server, accountKey: setup.accountKey,
            access: PasswordsItemAccess(isUsableByAgent: true, grantees: [.person(userId: "user-mallory")]))
        try await setup.ada.sync(isFull: true)
        let edited = try await setup.ada.save(fields: loginFields(password: "edited"), id: setup.itemId)
        XCTAssertEqual(edited.sharingCheck, .unverified)
        XCTAssertFalse(edited.isUsableByAgent)
    }

    func testSettingSharingAgainRepairsAnItemWhoseRecordWasNotCheckedAndRekeysIt() async throws {
        let setup = try await makeSharedItem()
        // Mallory held the key and wrote a record that lets the agent in, and the server lists her.
        try await replaceFields(
            of: setup.itemId, server: setup.server, accountKey: setup.accountKey,
            access: PasswordsItemAccess(isUsableByAgent: true, grantees: [.person(userId: "user-mallory")]))
        _ = try await setup.server.setShares(
            userId: "user-ada", itemId: setup.itemId,
            grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben"), PasswordShareGranteeInput(granteeUserId: "user-mallory")],
            keys: [])
        _ = try await setup.server.setUsableByAgent(
            userId: "user-ada", itemId: setup.itemId, isUsableByAgent: true, itemKeyByAgentKey: "forged agent copy")
        try await setup.ada.sync(isFull: true)
        let keyBefore = await setup.server.item(setup.itemId)?.itemKeyByAccountKey

        // The share screen starts from what the record lists, which is nobody; the person chooses Ben again.
        let recipients = try await setup.ada.recipients(granteeUserId: "user-ben")
        setup.flags.calls = []
        let repaired = try await setup.ada.setShares(
            itemId: setup.itemId, grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben")], recipients: recipients)
        XCTAssertEqual(repaired.sharingCheck, .verified)
        XCTAssertEqual(repaired.fields.access?.grantees, [.person(userId: "user-ben")])
        XCTAssertEqual(repaired.fields.access?.isUsableByAgent, false)
        XCTAssertTrue(setup.flags.calls.contains("rekeyPasswordItem"))
        let stored = await setup.server.item(setup.itemId)
        XCTAssertNotEqual(stored?.itemKeyByAccountKey, keyBefore)
        XCTAssertNil(stored?.itemKeyByAgentKey)
        XCTAssertEqual(stored?.isUsableByAgent, false)
        XCTAssertEqual(stored?.grantees.compactMap(\.granteeUserId), ["user-ben"])
        XCTAssertEqual(stored?.needsNewKey, false)
        // Ben, whom the person chose, keeps a key for the new item key.
        XCTAssertNotNil(stored?.keys["user-ben"])
        XCTAssertNil(stored?.keys["user-mallory"])
    }

    // MARK: An old record sent back

    func testAnOlderRecordTheServerSendsBackIsNotActedOnAndIsRemembered() async throws {
        let setup = try await makeSharedItem()
        // Version 2 shares with Ben. Capture what the server holds, then take Ben away (version 3, new key).
        let atShare = await setup.server.item(setup.itemId)
        let sharedState = try XCTUnwrap(atShare)
        _ = try await setup.ada.setShares(itemId: setup.itemId, grantees: [], recipients: [])
        let unshared = try await setup.ada.item(id: setup.itemId)
        XCTAssertEqual(unshared.fields.access?.recordVersion, 3)

        // The server puts version 2 back, asks for a new key, and lists Ben as a share again.
        await setup.server.replaceStoredFields(
            itemId: setup.itemId, encryptedFields: sharedState.encryptedFields, itemKeyByAccountKey: sharedState.itemKeyByAccountKey,
            needsNewKey: true)
        _ = try await setup.server.setShares(
            userId: "user-ada", itemId: setup.itemId, grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben")], keys: [])
        setup.flags.calls = []
        try await setup.ada.sync(isFull: true)

        let replayed = try await setup.ada.item(id: setup.itemId)
        XCTAssertEqual(replayed.sharingCheck, .replayed)
        XCTAssertNil(replayed.verifiedAccess)
        XCTAssertFalse(replayed.isUsableByAgent)
        let given = await setup.ada.giveNewKeysToMarkedItems()
        XCTAssertEqual(given, [])
        let completed = try await setup.ada.completeWaitingShares()
        XCTAssertEqual(completed.addedKeyCount, 0)
        XCTAssertEqual(setup.flags.calls.filter { $0 == "rekeyPasswordItem" || $0 == "addPasswordShareKeys" }, [])

        // A device that is opened again later (the same pin store) still remembers version 3.
        let reopened = makeSession(setup.server, userId: "user-ada", cache: setup.adaCache)
        _ = try await reopened.unlock(passphrase: adaPassphrase)
        let again = try await reopened.item(id: setup.itemId)
        XCTAssertEqual(again.sharingCheck, .replayed)

        // The person sets sharing again: the new record is built on the highest version seen (3), not on
        // the replayed one's (2), so every device that saw version 3 takes it as newer.
        let repaired = try await setup.ada.setShares(itemId: setup.itemId, grantees: [], recipients: [])
        XCTAssertEqual(repaired.sharingCheck, .verified)
        XCTAssertGreaterThan(repaired.fields.access?.recordVersion ?? 0, 3)
    }

    func testEveryChangeOfTheRecordRaisesTheVersionByOne() async throws {
        let setup = try await makeSharedItem()
        let shared = try await setup.ada.item(id: setup.itemId)
        XCTAssertEqual(shared.fields.access?.recordVersion, 2)
        let agentOn = try await setup.ada.setUsableByAgent(itemId: setup.itemId, isUsable: true)
        XCTAssertEqual(agentOn.fields.access?.recordVersion, 3)
        let agentOff = try await setup.ada.setUsableByAgent(itemId: setup.itemId, isUsable: false)
        XCTAssertEqual(agentOff.fields.access?.recordVersion, 4)
        XCTAssertEqual(agentOff.sharingCheck, .verified)
        // A save that changes only the fields keeps the record as it was.
        let edited = try await setup.ada.save(fields: loginFields(password: "edited"), id: setup.itemId)
        XCTAssertEqual(edited.fields.access?.recordVersion, 4)
    }

    // MARK: A new key never reaches further than the old one did

    func testAnAutomaticNewKeyIsSealedOnlyForPeopleWhoHeldAKeyAndTheAgentCopyOnlyIfItHadOne() async throws {
        let setup = try await makeSharedItem(extraPeople: [("user-dan", "Dan")], sharedWith: ["user-ben"])
        let ben = "user-ben"
        // A second device of Ada's adds Dan and the agent, and then the server
        // asks for a new key before this device has looked at the item again.
        let second = makeSession(setup.server, userId: "user-ada")
        _ = try await second.unlock(passphrase: adaPassphrase)
        try await second.sync()
        let benRecipients = try await second.recipients(granteeUserId: "user-ben")
        let danRecipients = try await second.recipients(granteeUserId: "user-dan")
        let recipients = benRecipients + danRecipients
        _ = try await second.setShares(
            itemId: setup.itemId,
            grantees: [PasswordShareGranteeInput(granteeUserId: ben), PasswordShareGranteeInput(granteeUserId: "user-dan")],
            recipients: recipients)
        _ = try await second.setUsableByAgent(itemId: setup.itemId, isUsable: true)
        await setup.server.markNeedsNewKey(itemId: setup.itemId)
        // This device already knows Dan's key, but never saw Dan or the agent hold a key for the item.
        let danKey = await setup.server.publicKey(of: "user-dan")
        try setup.adaCache.recordKnownIfAbsent(userId: "user-ada", otherUserId: "user-dan", publicKey: danKey)

        try await setup.ada.sync()
        let marked = try await setup.ada.item(id: setup.itemId)
        XCTAssertTrue(marked.needsNewKey)
        XCTAssertEqual(marked.sharingCheck, .verified)
        XCTAssertEqual(Set(marked.fields.access?.grantees ?? []), [.person(userId: ben), .person(userId: "user-dan")])
        XCTAssertEqual(marked.fields.access?.isUsableByAgent, true)

        setup.flags.calls = []
        let given = await setup.ada.giveNewKeysToMarkedItems()
        XCTAssertEqual(given, [setup.itemId])
        let rekeyInput = try XCTUnwrap(setup.flags.lastRekeyInput)
        XCTAssertEqual(rekeyInput.shareKeys.map(\.recipientUserId), [ben])
        XCTAssertNil(rekeyInput.itemKeyByAgentKey)
        // Dan is in the record but this device never saw him hold a key, so the automatic key leaves him for the waiting shares.
        let waiting = try await setup.ada.waitingShares()
        XCTAssertEqual(waiting.map(\.recipient.userId), ["user-dan"])
        // The record stays what the person signed.
        let after = try await setup.ada.item(id: setup.itemId)
        XCTAssertEqual(after.fields.access, marked.fields.access)
    }

    func testAnAutomaticNewKeyKeepsTheAgentCopyThatThisDeviceSawAndTheKeysOfWhoeverItSawHoldOne() async throws {
        let setup = try await makeSharedItem()
        _ = try await setup.ada.setUsableByAgent(itemId: setup.itemId, isUsable: true)
        try await setup.ada.sync(isFull: true)
        _ = try await setup.ada.list()
        await setup.server.markNeedsNewKey(itemId: setup.itemId)
        try await setup.ada.sync()
        let given = await setup.ada.giveNewKeysToMarkedItems()
        XCTAssertEqual(given, [setup.itemId])
        let rekeyInput = try XCTUnwrap(setup.flags.lastRekeyInput)
        XCTAssertEqual(rekeyInput.shareKeys.map(\.recipientUserId), ["user-ben"])
        XCTAssertNotNil(rekeyInput.itemKeyByAgentKey)
        let item = try await setup.ada.item(id: setup.itemId)
        XCTAssertTrue(item.isUsableByAgent)
    }

    func testAnItemGetsAnAutomaticNewKeyOnceInTenMinutesAndASessionGivesAtMostTwenty() async throws {
        let clock = TestClock()
        let setup = try await makeSharedItem(clock: clock)
        await setup.server.markNeedsNewKey(itemId: setup.itemId)
        try await setup.ada.sync()
        let first = await setup.ada.giveNewKeysToMarkedItems()
        XCTAssertEqual(first, [setup.itemId])

        // Marked again straight away: held back, and said to need attention.
        await setup.server.markNeedsNewKey(itemId: setup.itemId)
        try await setup.ada.sync()
        clock.advance(minutes: 9)
        let second = await setup.ada.giveNewKeysToMarkedItems()
        XCTAssertEqual(second, [])
        let attention = await setup.ada.itemIdsNeedingAttention
        XCTAssertEqual(attention, [setup.itemId])
        let rekeyCountHeld = await setup.server.rekeyCallCount
        // Only the first, automatic one reached the server.
        XCTAssertEqual(rekeyCountHeld, 1)
        // The person can still ask for it themselves.
        _ = try await setup.ada.giveNewKey(itemId: setup.itemId)
        let rekeyCountAsked = await setup.server.rekeyCallCount
        XCTAssertEqual(rekeyCountAsked, 2)

        // After ten minutes it may again.
        await setup.server.markNeedsNewKey(itemId: setup.itemId)
        try await setup.ada.sync()
        clock.advance(minutes: 2)
        let third = await setup.ada.giveNewKeysToMarkedItems()
        XCTAssertEqual(third, [setup.itemId])
        let noAttention = await setup.ada.itemIdsNeedingAttention
        XCTAssertTrue(noAttention.isEmpty)

        // The limit per session.
        var itemIds: [String] = []
        for index in 0..<(PasswordsSession.mostAutomaticRekeysPerSession + 1) {
            let item = try await setup.ada.save(fields: loginFields(name: "Item \(index)"))
            itemIds.append(item.id)
            await setup.server.markNeedsNewKey(itemId: item.id)
        }
        try await setup.ada.sync()
        let batch = await setup.ada.giveNewKeysToMarkedItems()
        // Two keys were given already in this session (the first and the third above).
        XCTAssertEqual(batch.count, PasswordsSession.mostAutomaticRekeysPerSession - 2)
        let held = await setup.ada.itemIdsNeedingAttention
        XCTAssertEqual(held.count, itemIds.count - batch.count)
    }

    // MARK: Share keys carry the item key

    func testTheKeysOfAShareCarryTheItemKeyAsTheItemShowedIt() async throws {
        let setup = try await makeSharedItem()
        XCTAssertFalse(setup.flags.lastShareKeys.isEmpty)
        // The item key has not changed since (no rekey), so the text the keys carry is the stored one.
        let stored = await setup.server.item(setup.itemId)
        for key in setup.flags.lastShareKeys {
            XCTAssertEqual(key.itemKey, stored?.itemKeyByAccountKey)
        }
    }

    // MARK: One split of recorded and not recorded shares

    func testASharePeopleSeeIsOneTheRecordListsAndOtherSharesAreNamedAsUnrecordedUntilSharingIsSaved() async throws {
        let setup = try await makeSharedItem()
        // The server lists Mallory too; the person never shared the item with her.
        _ = try await setup.server.setShares(
            userId: "user-ada", itemId: setup.itemId,
            grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben"), PasswordShareGranteeInput(granteeUserId: "user-mallory")],
            keys: [])
        try await setup.ada.sync(isFull: true)
        let item = try await setup.ada.item(id: setup.itemId)
        XCTAssertEqual(item.sharingCheck, .verified)
        XCTAssertEqual(item.recordedShares.compactMap(\.granteeUserId), ["user-ben"])
        XCTAssertEqual(item.unrecordedShares.compactMap(\.granteeUserId), ["user-mallory"])

        // Saving sharing with what the record lists removes the server's extra share and gives the item a new key.
        let recipients = try await setup.ada.recipients(granteeUserId: "user-ben")
        let keyBefore = await setup.server.item(setup.itemId)?.itemKeyByAccountKey
        setup.flags.calls = []
        let saved = try await setup.ada.setShares(
            itemId: setup.itemId, grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben")], recipients: recipients)
        XCTAssertTrue(saved.unrecordedShares.isEmpty)
        XCTAssertEqual(saved.recordedShares.compactMap(\.granteeUserId), ["user-ben"])
        XCTAssertTrue(setup.flags.calls.contains("rekeyPasswordItem"))
        let keyAfter = await setup.server.item(setup.itemId)?.itemKeyByAccountKey
        XCTAssertNotEqual(keyBefore, keyAfter)
    }

    // MARK: Deleting for good

    func testPurgingADeletedItemRemovesItEverywhereAndRefusesAnItemThatWasNotDeleted() async throws {
        let setup = try await makeSharedItem()
        do {
            try await setup.ada.purge(id: setup.itemId)
            XCTFail("an item that was not deleted cannot be purged")
        } catch PasswordsError.itemNotFound {
        }
        _ = try await setup.ada.delete(id: setup.itemId)
        let listedBefore = try await setup.ada.list()
        XCTAssertEqual(listedBefore.items.filter(\.isDeleted).map(\.id), [setup.itemId])
        try await setup.ada.purge(id: setup.itemId)
        let stored = await setup.server.item(setup.itemId)
        XCTAssertNil(stored)
        let listedAfter = try await setup.ada.list()
        XCTAssertTrue(listedAfter.items.isEmpty)
        let cached = try setup.adaCache.load()
        XCTAssertEqual(cached?.items.isEmpty, true)
        do {
            _ = try await setup.ada.item(id: setup.itemId)
            XCTFail("the item is gone")
        } catch PasswordsError.itemNotFound {
        }
    }

    // MARK: What the device remembers

    func testWhatADeviceRemembersAboutAnItemOnlyGoesUpAndSurvivesANewPinStoreInstance() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("passwords-memory-\(UUID().uuidString)")
        directories.append(directory)
        let fileURL = directory.appendingPathComponent("pins.json")
        let first = PasswordsFilePinStore(fileURL: fileURL)
        try first.recordSeenRecord(userId: "user-ada", itemId: "item-1", version: 5, mac: "mac-five")
        try first.recordSeenRecord(userId: "user-ada", itemId: "item-1", version: 3, mac: "mac-three")
        try first.recordSeenRecord(userId: "user-ada", itemId: "item-1", version: 5, mac: "mac-other")
        try first.recordSealed(
            userId: "user-ada", itemId: "item-1",
            change: PasswordsSealedChange(userIds: ["user-ben", "user-ben", "user-carol"], isReplacing: false, didMakeAgentCopy: true))
        try first.recordSeenRecord(userId: "user-ada", itemId: "item-1", version: 4, mac: "mac-four")
        let second = PasswordsFilePinStore(fileURL: fileURL)
        let pins = try second.loadPins(userId: "user-ada")
        XCTAssertEqual(
            pins.itemMemories["item-1"],
            PasswordsItemMemory(
                highestRecordVersion: 5, highestRecordMac: "mac-five", sealedForUserIds: ["user-ben", "user-carol"], didMakeAgentCopy: true))
        // Another person's memory is not this person's.
        XCTAssertTrue(try second.loadPins(userId: "user-grace").itemMemories.isEmpty)

        let memory = PasswordsMemoryPinStore()
        try memory.recordSeenRecord(userId: "user-ada", itemId: "item-1", version: 2, mac: nil)
        try memory.recordSeenRecord(userId: "user-ada", itemId: "item-1", version: 1, mac: "mac-one")
        XCTAssertEqual(try memory.loadPins(userId: "user-ada").itemMemories["item-1"]?.highestRecordVersion, 2)
        // A version remembered without its mac takes the mac of the next record seen at that version.
        try memory.recordSeenRecord(userId: "user-ada", itemId: "item-1", version: 2, mac: "mac-two")
        XCTAssertEqual(try memory.loadPins(userId: "user-ada").itemMemories["item-1"]?.highestRecordMac, "mac-two")
        try memory.removeAll()
        XCTAssertTrue(try memory.loadPins(userId: "user-ada").itemMemories.isEmpty)
    }

    func testARecordWithTheRememberedVersionAndAnotherMacIsAReplayAndAMemoryOfAnEarlierBuildStillReads() throws {
        let memory = PasswordsItemMemory(highestRecordVersion: 3, highestRecordMac: "mac-three")
        XCTAssertFalse(memory.isReplay(version: 3, mac: "mac-three"))
        XCTAssertTrue(memory.isReplay(version: 3, mac: "mac-other"))
        XCTAssertTrue(memory.isReplay(version: 2, mac: "mac-three"))
        XCTAssertFalse(memory.isReplay(version: 4, mac: "mac-four"))
        // The first build kept only a version and the people it saw holding a key.
        let earlier = #"{"highestRecordVersion":4,"keyHolderUserIds":["user-ben"],"hadAgentCopy":true}"#
        let decoded = try JSONDecoder().decode(PasswordsItemMemory.self, from: Data(earlier.utf8))
        XCTAssertEqual(decoded, PasswordsItemMemory(highestRecordVersion: 4))
    }

    func testPinsWrittenBeforeItemsHadAMemoryStillRead() throws {
        let text = #"{"formatVersion":1,"userId":"user-ada","seenPublicKeys":{},"confirmedPublicKeys":{}}"#  // the first version's shape
        let pins = try JSONDecoder().decode(PasswordsPins.self, from: Data(text.utf8))
        XCTAssertTrue(pins.itemMemories.isEmpty)
    }
}
