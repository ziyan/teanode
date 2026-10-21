import Foundation
import XCTest

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

@testable import TeaNodeKit

/// PasswordsReplayTests: a device that has never seen an item cannot tell an old signed record from
/// the newest one, so it does nothing by itself for that item; a record with the version it
/// remembers and another mac is a replay too; and what the device remembers comes only from what it
/// verified and did itself.
final class PasswordsReplayTests: XCTestCase {
    private let iterationCount = PasswordsCrypto.minimumPassphraseIterationCount
    private let passphrase = "ada invented passphrase"
    private var directories: [URL] = []

    override func tearDown() {
        for directory in directories { try? FileManager.default.removeItem(at: directory) }
        directories = []
        super.tearDown()
    }

    private func makeCache() -> PasswordsCache {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("replay-\(UUID().uuidString)")
        directories.append(directory)
        return PasswordsCache(fileURL: directory.appendingPathComponent("passwords.json"), pinStore: PasswordsMemoryPinStore())
    }

    private func makeSession(_ server: FakePasswordsServer, userId: String, cache: PasswordsCache? = nil) -> PasswordsSession {
        PasswordsSession(api: FakePasswordsAPI(server: server, userId: userId), cache: cache ?? makeCache(), userId: userId)
    }

    private func loginFields() -> PasswordsItemFields {
        .login(PasswordsLoginFields(name: "Example", userName: "ada@example.com", password: "correct horse", sites: ["https://example.com"]))
    }

    /// Ada's first device turned the agent on (record version 2) and then off (version 3). The server
    /// state while the agent was on is returned.
    private func makeItemWhoseAgentWasTurnedOff() async throws -> (
        server: FakePasswordsServer, one: PasswordsSession, oneCache: PasswordsCache, itemId: String, whileOn: FakePasswordsServer.StoredItem
    ) {
        let server = FakePasswordsServer()
        await server.addPerson(userId: "user-ada", name: "Ada")
        let oneCache = makeCache()
        let one = makeSession(server, userId: "user-ada", cache: oneCache)
        _ = try await one.setUp(passphrase: passphrase, iterationCount: iterationCount)
        let item = try await one.save(fields: loginFields())
        let on = try await one.setUsableByAgent(itemId: item.id, isUsable: true)
        XCTAssertEqual(on.fields.access?.recordVersion, 2)
        let snapshotOptional = await server.item(item.id)
        let whileOn = try XCTUnwrap(snapshotOptional)
        XCTAssertNotNil(whileOn.itemKeyByAgentKey)
        let off = try await one.setUsableByAgent(itemId: item.id, isUsable: false)
        XCTAssertEqual(off.fields.access?.recordVersion, 3)
        XCTAssertFalse(off.isUsableByAgent)
        return (server, one, oneCache, item.id, whileOn)
    }

    /// The audit's proof: the server shows a second device that has nothing of the item the record
    /// from while the agent was on. The device believes it (it cannot know better) but gives the item
    /// no new key by itself, so the agent gets no copy of a new key; the person finishes it.
    func testAServerThatShowsAnOldRecordToANewDeviceCannotMakeItGiveTheAgentANewKey() async throws {
        let world = try await makeItemWhoseAgentWasTurnedOff()
        let twoCache = makeCache()
        let two = makeSession(world.server, userId: "user-ada", cache: twoCache)
        _ = try await two.unlock(passphrase: passphrase)
        await world.server.replaceStoredFields(
            itemId: world.itemId, encryptedFields: world.whileOn.encryptedFields, itemKeyByAccountKey: world.whileOn.itemKeyByAccountKey,
            needsNewKey: false)
        _ = try await world.server.setUsableByAgent(
            userId: "user-ada", itemId: world.itemId, isUsableByAgent: true, itemKeyByAgentKey: world.whileOn.itemKeyByAgentKey)
        try await two.sync(isFull: true)
        let seenOnTwo = try await two.item(id: world.itemId)
        XCTAssertEqual(seenOnTwo.sharingCheck, .verified, "no memory of version 3 on this device, so the old record is believed")

        await world.server.markNeedsNewKey(itemId: world.itemId)
        try await two.sync()
        let rekeysBefore = await world.server.rekeyCallCount
        let given = await two.giveNewKeysToMarkedItems()
        XCTAssertEqual(given, [])
        let rekeysAfter = await world.server.rekeyCallCount
        XCTAssertEqual(rekeysAfter, rekeysBefore, "nothing was sent to the server")
        let needingYou = await two.itemIdsNeedingYou
        XCTAssertEqual(needingYou, [world.itemId])

        // The same device in a later session (the same pin store) remembers the item, but not that it
        // ever sealed an agent copy.
        let laterSession = makeSession(world.server, userId: "user-ada", cache: twoCache)
        _ = try await laterSession.unlock(passphrase: passphrase)
        try await laterSession.sync()
        let givenLater = await laterSession.giveNewKeysToMarkedItems()
        XCTAssertEqual(givenLater, [world.itemId], "an item remembered from an earlier session is given a key")
        let afterLater = await world.server.item(world.itemId)
        XCTAssertNil(afterLater?.itemKeyByAgentKey, "this device never made an agent copy, so the new key has none")
    }

    /// Device one remembers version 3. A device that has never seen the item signs a record with
    /// the same version 3 (it was shown version 2): the server then shows device one a signed record
    /// with the version it remembers and another mac, which is a replay.
    func testARecordWithTheRememberedVersionAndAnotherMacIsAReplayAndSigningGoesPastBoth() async throws {
        let world = try await makeItemWhoseAgentWasTurnedOff()
        let rememberedOptional = try await world.one.item(id: world.itemId).fields.access
        let remembered = try XCTUnwrap(rememberedOptional)
        XCTAssertEqual(remembered.recordVersion, 3)

        let two = makeSession(world.server, userId: "user-ada")
        _ = try await two.unlock(passphrase: passphrase)
        await world.server.replaceStoredFields(
            itemId: world.itemId, encryptedFields: world.whileOn.encryptedFields, itemKeyByAccountKey: world.whileOn.itemKeyByAccountKey,
            needsNewKey: false)
        try await two.sync(isFull: true)
        await world.server.addPerson(userId: "user-ben", name: "Ben")
        _ = try await makeSession(world.server, userId: "user-ben").setUp(passphrase: "ben invented passphrase", iterationCount: iterationCount)
        let benRecipients = try await two.recipients(granteeUserId: "user-ben")
        let signedByTwo = try await two.setShares(
            itemId: world.itemId, grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben")], recipients: benRecipients)
        XCTAssertEqual(signedByTwo.fields.access?.recordVersion, 3, "version 2 was all this device had seen")
        XCTAssertNotEqual(signedByTwo.fields.access?.recordMac, remembered.recordMac)

        try await world.one.sync(isFull: true)
        let onOne = try await world.one.item(id: world.itemId)
        XCTAssertEqual(onOne.sharingCheck, .replayed)
        XCTAssertFalse(onOne.isUsableByAgent)
        XCTAssertNil(onOne.verifiedAccess)
        let given = await world.one.giveNewKeysToMarkedItems()
        XCTAssertEqual(given, [])

        // Setting sharing again goes past the remembered version and past the incoming one.
        let repaired = try await world.one.setShares(itemId: world.itemId, grantees: [], recipients: [])
        XCTAssertEqual(repaired.sharingCheck, .verified)
        XCTAssertGreaterThan(repaired.fields.access?.recordVersion ?? 0, 3)
    }

    /// The remembered version and mac survive a new session on the same pin store, and a record that
    /// is the same one is not a replay.
    func testTheSameRecordSeenAgainIsNotAReplayAndTheMacIsRemembered() async throws {
        let world = try await makeItemWhoseAgentWasTurnedOff()
        let reopened = makeSession(world.server, userId: "user-ada", cache: world.oneCache)
        _ = try await reopened.unlock(passphrase: passphrase)
        try await reopened.sync(isFull: true)
        let again = try await reopened.item(id: world.itemId)
        XCTAssertEqual(again.sharingCheck, .verified)
        let pins = try world.oneCache.loadPins(userId: "user-ada")
        XCTAssertEqual(pins.itemMemories[world.itemId]?.highestRecordVersion, 3)
        XCTAssertEqual(pins.itemMemories[world.itemId]?.highestRecordMac, again.fields.access?.recordMac)
    }

    /// Nothing about who holds a key is taken from the server's lists: a memory made by looking at a
    /// shared item has no people sealed for and no agent copy.
    func testLookingAtAnItemRemembersNoOneAsSealedFor() async throws {
        let server = FakePasswordsServer()
        await server.addPerson(userId: "user-ada", name: "Ada")
        await server.addPerson(userId: "user-ben", name: "Ben")
        let ben = makeSession(server, userId: "user-ben")
        _ = try await ben.setUp(passphrase: "ben invented passphrase", iterationCount: iterationCount)
        let oneCache = makeCache()
        let one = makeSession(server, userId: "user-ada", cache: oneCache)
        _ = try await one.setUp(passphrase: passphrase, iterationCount: iterationCount)
        let item = try await one.save(fields: loginFields())
        let recipients = try await one.recipients(granteeUserId: "user-ben")
        _ = try await one.setShares(itemId: item.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben")], recipients: recipients)
        _ = try await one.setUsableByAgent(itemId: item.id, isUsable: true)
        let own = try oneCache.loadPins(userId: "user-ada").itemMemories[item.id]
        XCTAssertEqual(own?.sealedForUserIds, ["user-ben"])
        XCTAssertEqual(own?.didMakeAgentCopy, true)

        let twoCache = makeCache()
        let two = makeSession(server, userId: "user-ada", cache: twoCache)
        _ = try await two.unlock(passphrase: passphrase)
        try await two.sync(isFull: true)
        _ = try await two.list()
        let seen = try twoCache.loadPins(userId: "user-ada").itemMemories[item.id]
        XCTAssertEqual(seen?.highestRecordVersion, 3)
        XCTAssertEqual(seen?.sealedForUserIds, [])
        XCTAssertEqual(seen?.didMakeAgentCopy, false)
    }

    // MARK: What the phone remembers survives the server ending the sign-in

    func testOnlyAPersonRemovingTheAccountMakesThePhoneForgetKeysAndItemMemories() async throws {
        XCTAssertTrue(PasswordsSignInEnd.removedByPerson.isForgettingKeysAndItemMemories)
        XCTAssertFalse(PasswordsSignInEnd.endedByServer.isForgettingKeysAndItemMemories)

        let world = try await makeItemWhoseAgentWasTurnedOff()
        // The copy of the items goes when the sign-in ends; the item memory stays.
        try world.oneCache.eraseKeepingPins()
        let kept = try world.oneCache.loadPins(userId: "user-ada").itemMemories[world.itemId]
        XCTAssertEqual(kept?.highestRecordVersion, 3)
        XCTAssertNotNil(kept?.highestRecordMac)

        // Signing in again as the same person: the session finds the memory and still calls the old record a replay.
        let again = makeSession(world.server, userId: "user-ada", cache: world.oneCache)
        _ = try await again.unlock(passphrase: passphrase)
        await world.server.replaceStoredFields(
            itemId: world.itemId, encryptedFields: world.whileOn.encryptedFields, itemKeyByAccountKey: world.whileOn.itemKeyByAccountKey,
            needsNewKey: false)
        try await again.sync(isFull: true)
        let replayed = try await again.item(id: world.itemId)
        XCTAssertEqual(replayed.sharingCheck, .replayed)

        // Removing the account from the phone clears it.
        try world.oneCache.erase()
        XCTAssertTrue(try world.oneCache.loadPins(userId: "user-ada").itemMemories.isEmpty)
    }

    func testAFileOfPinsIsKeptWhenTheServerEndedTheSignInAndRemovedWhenThePersonRemovedTheAccount() throws {
        let container = FileManager.default.temporaryDirectory.appendingPathComponent("replay-\(UUID().uuidString)")
        directories.append(container)
        let accountKey = "demo.example.com|ada"
        let cache = PasswordsCache(fileURL: PasswordsLocation.fileURL(groupContainer: container, signInAccountKey: accountKey))
        try cache.recordSeenRecord(userId: "user-ada", itemId: "item-1", version: 2, mac: "mac-two")
        PasswordsLocation.removeCopy(groupContainer: container, signInAccountKey: accountKey, isKeepingPins: true)
        XCTAssertEqual(try cache.loadPins(userId: "user-ada").itemMemories["item-1"]?.highestRecordVersion, 2)
        PasswordsLocation.removeCopy(groupContainer: container, signInAccountKey: accountKey, isKeepingPins: false)
        XCTAssertTrue(try cache.loadPins(userId: "user-ada").itemMemories.isEmpty)
    }

    // MARK: People the person named on this device

    /// A person Ada named directly on this device is hers to complete by itself later; a person named
    /// only by a circle is not (who is in a circle is the server's word); and a device that never saw
    /// the item does not, whoever Ada named elsewhere.
    func testAWaitingShareCompletesByItselfOnlyForAPersonNamedDirectlyOnThisDevice() async throws {
        let server = FakePasswordsServer()
        for (userId, name) in [("user-ada", "Ada"), ("user-ben", "Ben"), ("user-grace", "Grace")] { await server.addPerson(userId: userId, name: name) }
        await server.setCircle("circle-one", members: ["user-ada", "user-grace"])
        let oneCache = makeCache()
        let one = makeSession(server, userId: "user-ada", cache: oneCache)
        _ = try await one.setUp(passphrase: passphrase, iterationCount: iterationCount)
        let item = try await one.save(fields: loginFields())
        // Ben and Grace have not set up Passwords: Ben is named, Grace only through the circle.
        _ = try await one.setShares(
            itemId: item.id,
            grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben"), PasswordShareGranteeInput(granteeCircleId: "circle-one")],
            recipients: [])
        XCTAssertEqual(try oneCache.loadPins(userId: "user-ada").itemMemories[item.id]?.chosenUserIds, ["user-ben"])
        for (userId, name) in [("user-ben", "ben"), ("user-grace", "grace")] {
            _ = try await makeSession(server, userId: userId).setUp(passphrase: "\(name) invented passphrase", iterationCount: iterationCount)
        }

        // A later session on the same device (the memory is from before it) seals for Ben only.
        let later = makeSession(server, userId: "user-ada", cache: oneCache)
        _ = try await later.unlock(passphrase: passphrase)
        try await later.sync()
        let completed = try await later.completeWaitingShares()
        XCTAssertEqual(completed.addedKeyCount, 1)
        let stored = await server.item(item.id)
        XCTAssertNotNil(stored?.keys["user-ben"])
        XCTAssertNil(stored?.keys["user-grace"])
        let needingYou = await later.itemIdsNeedingYou
        XCTAssertEqual(needingYou, [item.id], "Grace joined through the circle and waits for the person")

        // A device that never saw the item does nothing by itself, whoever was named elsewhere.
        let other = makeSession(server, userId: "user-ada")
        _ = try await other.unlock(passphrase: passphrase)
        try await other.sync()
        let byOther = try await other.completeWaitingShares()
        XCTAssertEqual(byOther.addedKeyCount, 0)

        // Taking Ben off the list forgets the choice.
        _ = try await later.setShares(itemId: item.id, grantees: [PasswordShareGranteeInput(granteeCircleId: "circle-one")], recipients: [])
        XCTAssertEqual(try oneCache.loadPins(userId: "user-ada").itemMemories[item.id]?.chosenUserIds, [])
    }
}
