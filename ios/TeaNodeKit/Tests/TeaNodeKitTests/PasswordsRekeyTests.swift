import Foundation
import XCTest

@testable import TeaNodeKit

/// PasswordsRekeyTests check that an item gets a new item key when access to it narrows, so the
/// old key (kept by the agent's copy, or by somebody who was removed) opens nothing saved later.
final class PasswordsRekeyTests: XCTestCase {
    private let iterationCount = PasswordsCrypto.minimumPassphraseIterationCount
    private var directories: [URL] = []

    override func tearDown() {
        for directory in directories { try? FileManager.default.removeItem(at: directory) }
        directories = []
        super.tearDown()
    }

    private func makeDevice(_ server: FakePasswordsServer, userId: String) -> PasswordsSession {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("passwords-rekey-test-\(UUID().uuidString)")
        directories.append(directory)
        let cache = PasswordsCache(fileURL: directory.appendingPathComponent("passwords.json"))
        return PasswordsSession(api: FakePasswordsAPI(server: server, userId: userId), cache: cache, userId: userId)
    }

    private func loginFields(password: String = "correct horse") -> PasswordsItemFields {
        .login(PasswordsLoginFields(name: "Example", userName: "ada@example.com", password: password, sites: ["https://example.com"]))
    }

    private func makeServer(_ people: [(String, String)]) async -> FakePasswordsServer {
        let server = FakePasswordsServer()
        for (userId, name) in people { await server.addPerson(userId: userId, name: name) }
        return server
    }

    /// The item key an item's stored account copy holds.
    private func accountItemKey(of stored: FakePasswordsServer.StoredItem, accountKey: Data) throws -> Data {
        try PasswordsCrypto.open(
            try PasswordsSealedValue(jsonText: stored.itemKeyByAccountKey), key: accountKey,
            associatedData: PasswordsCrypto.itemKeyAssociatedData(itemId: stored.id))
    }

    private func fields(of stored: FakePasswordsServer.StoredItem, itemKey: Data) throws -> PasswordsItemFields {
        try PasswordsCrypto.openItemFields(
            try PasswordsSealedValue(jsonText: stored.encryptedFields), itemKey: itemKey, itemId: stored.id, itemKind: .login)
    }

    func testTurningTheAgentOffGivesTheItemANewKeyThatTheOldAgentCopyCannotOpen() async throws {
        let server = await makeServer([("user-ada", "Ada")])
        let device = makeDevice(server, userId: "user-ada")
        _ = try await device.setUp(passphrase: "ada invented passphrase", iterationCount: iterationCount)
        let accountKey = try await makeDevice(server, userId: "user-ada").unlock(passphrase: "ada invented passphrase").accountKey
        let item = try await device.save(fields: loginFields())
        _ = try await device.setUsableByAgent(itemId: item.id, isUsable: true)

        // The agent's copy of the item key, as a copy of the database would hold it.
        let usableOptional = await server.item(item.id)
        let usable = try XCTUnwrap(usableOptional)
        let agentCopy = try PasswordsKeyPairSealedValue(jsonText: try XCTUnwrap(usable.itemKeyByAgentKey))
        let ownerPublicKeyText = await server.publicKey(of: "user-ada")
        let ownerPublicKey = try XCTUnwrap(Data(base64Encoded: ownerPublicKeyText))
        let agentPrivateKey = await server.agentKeyPair.privateKey
        let oldItemKey = try PasswordsCrypto.openForKeyPair(
            agentCopy, recipientPrivateKey: agentPrivateKey, ownerPublicKey: ownerPublicKey, ownerId: "user-ada",
            recipientId: PasswordsCrypto.agentRecipientId, itemId: item.id)
        XCTAssertEqual(try fields(of: usable, itemKey: oldItemKey), loginFields())

        let stopped = try await device.setUsableByAgent(itemId: item.id, isUsable: false)
        XCTAssertFalse(stopped.isUsableByAgent)
        XCTAssertEqual(stopped.fields, loginFields())
        let stoppedStoredOptional = await server.item(item.id)
        let stoppedStored = try XCTUnwrap(stoppedStoredOptional)
        XCTAssertNil(stoppedStored.itemKeyByAgentKey)
        let newItemKey = try accountItemKey(of: stoppedStored, accountKey: accountKey)
        XCTAssertNotEqual(newItemKey, oldItemKey)

        // A password saved afterwards is sealed by the new key; the old key opens nothing of it.
        _ = try await device.save(fields: loginFields(password: "changed afterwards"), id: item.id)
        let afterOptional = await server.item(item.id)
        let after = try XCTUnwrap(afterOptional)
        XCTAssertEqual(try fields(of: after, itemKey: newItemKey), loginFields(password: "changed afterwards"))
        XCTAssertThrowsError(try fields(of: after, itemKey: oldItemKey))
        // The person's other devices read it as before.
        let other = makeDevice(server, userId: "user-ada")
        _ = try await other.unlock(passphrase: "ada invented passphrase")
        try await other.sync()
        let opened = try await other.item(id: item.id)
        XCTAssertEqual(opened.fields, loginFields(password: "changed afterwards"))
    }

    func testTurningTheAgentOffOnAnItemTheAgentDoesNotUseChangesNothing() async throws {
        let server = await makeServer([("user-ada", "Ada")])
        let device = makeDevice(server, userId: "user-ada")
        _ = try await device.setUp(passphrase: "ada invented passphrase", iterationCount: iterationCount)
        let item = try await device.save(fields: loginFields())
        _ = try await device.setUsableByAgent(itemId: item.id, isUsable: false)
        let rekeyCallCount = await server.rekeyCallCount
        XCTAssertEqual(rekeyCallCount, 0)
    }

    /// Ada has two devices. The first shares an item with Grace; the second shares it with Linus and Heidi and confirms
    /// their keys, which the first never did.
    private func makeSharedWorld() async throws -> (
        server: FakePasswordsServer, ada: PasswordsSession, people: [String: PasswordsSession], itemId: String
    ) {
        let server = await makeServer([("user-ada", "Ada"), ("user-grace", "Grace"), ("user-linus", "Linus"), ("user-heidi", "Heidi")])
        let ada = makeDevice(server, userId: "user-ada")
        _ = try await ada.setUp(passphrase: "ada invented passphrase", iterationCount: iterationCount)
        var people: [String: PasswordsSession] = [:]
        for userId in ["user-grace", "user-linus", "user-heidi"] {
            let session = makeDevice(server, userId: userId)
            _ = try await session.setUp(passphrase: "\(userId) invented passphrase", iterationCount: iterationCount)
            people[userId] = session
        }
        let item = try await ada.save(fields: loginFields())
        let graceRecipients = try await ada.recipients(granteeUserId: "user-grace")
        _ = try await ada.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-grace")], recipients: graceRecipients,
            confirmedUserIds: ["user-grace"])

        let secondDevice = makeDevice(server, userId: "user-ada")
        _ = try await secondDevice.unlock(passphrase: "ada invented passphrase")
        try await secondDevice.sync()
        let linusRecipients = try await secondDevice.recipients(granteeUserId: "user-linus")
        let heidiRecipients = try await secondDevice.recipients(granteeUserId: "user-heidi")
        let linusAndHeidi = linusRecipients + heidiRecipients
        let everyone = ["user-grace", "user-linus", "user-heidi"].map { PasswordShareGranteeInput(granteeUserId: $0) }
        _ = try await secondDevice.setShares(
            itemId: item.id, grantees: everyone, recipients: linusAndHeidi, confirmedUserIds: ["user-linus", "user-heidi"])
        try await ada.sync()
        for session in people.values { try await session.sync() }
        return (server, ada, people, item.id)
    }

    func testRemovingSomebodyKeepsAConfirmedRecipientDropsTheRemovedOneAndLeavesAnUnconfirmedOneWaiting() async throws {
        let world = try await makeSharedWorld()
        for userId in ["user-grace", "user-linus", "user-heidi"] {
            let opened = try await XCTUnwrap(world.people[userId]).item(id: world.itemId)
            XCTAssertEqual(opened.fields, loginFields())
        }
        let beforeOptional = await world.server.item(world.itemId)
        let before = try XCTUnwrap(beforeOptional)
        let graceKeyBefore = try XCTUnwrap(before.keys["user-grace"]?.itemKeyByRecipientKey)

        // Heidi is removed. Grace's key was confirmed on this device; Linus's was confirmed only on the other one.
        let narrower = ["user-grace", "user-linus"].map { PasswordShareGranteeInput(granteeUserId: $0) }
        let result = try await world.ada.setShares(itemId: world.itemId, grantees: narrower, recipients: [])
        XCTAssertEqual(result.fields, loginFields())

        let afterOptional = await world.server.item(world.itemId)
        let after = try XCTUnwrap(afterOptional)
        XCTAssertNotNil(after.keys["user-grace"])
        XCTAssertNotEqual(after.keys["user-grace"]?.itemKeyByRecipientKey, graceKeyBefore)
        XCTAssertNil(after.keys["user-heidi"])
        XCTAssertNil(after.keys["user-linus"])
        let isRekeyed = await world.ada.needsNewKey(itemId: world.itemId)
        XCTAssertFalse(isRekeyed)

        // A password saved now is out of Heidi's reach: she sees the item go, and her old copy of the key opens nothing of it.
        _ = try await world.ada.save(fields: loginFields(password: "changed afterwards"), id: world.itemId)
        let grace = try XCTUnwrap(world.people["user-grace"])
        try await grace.sync()
        let graceOpened = try await grace.item(id: world.itemId)
        XCTAssertEqual(graceOpened.fields, loginFields(password: "changed afterwards"))
        let heidi = try XCTUnwrap(world.people["user-heidi"])
        let heidiResult = try await heidi.sync()
        XCTAssertEqual(heidiResult.removedItemIds, [world.itemId])

        // Linus is waiting, and the person is asked for the safety code as for any waiting share.
        let linus = try XCTUnwrap(world.people["user-linus"])
        let linusResult = try await linus.sync()
        XCTAssertEqual(linusResult.removedItemIds, [world.itemId])
        let waiting = try await world.ada.waitingShares()
        XCTAssertEqual(waiting.map(\.recipient.userId), ["user-linus"])
        XCTAssertEqual(waiting.first?.recipient.keyTrust, .new)
        let completed = try await world.ada.completeWaitingShares()
        XCTAssertEqual(completed.addedKeyCount, 0)
        XCTAssertEqual(completed.waitingForConfirmation.map(\.recipient.userId), ["user-linus"])
        try await world.ada.confirmWaitingShare(try XCTUnwrap(completed.waitingForConfirmation.first))
        try await linus.sync()
        let linusOpened = try await linus.item(id: world.itemId)
        XCTAssertEqual(linusOpened.fields, loginFields(password: "changed afterwards"))
    }

    func testARekeyThatFailsAfterTheRemovalLeavesTheRemovalAndCanBeRetried() async throws {
        let world = try await makeSharedWorld()
        await world.server.setRekeyRefusal("api: internal error")
        let narrower = ["user-grace", "user-linus"].map { PasswordShareGranteeInput(granteeUserId: $0) }
        do {
            _ = try await world.ada.setShares(itemId: world.itemId, grantees: narrower, recipients: [])
            XCTFail("the re-key was refused")
        } catch {
            XCTAssertEqual(error as? PasswordsError, .itemNeedsNewKey(itemId: world.itemId))
        }
        // The removal stands, and the item is marked.
        let reachedOptional = await world.server.item(world.itemId)
        XCTAssertEqual(try XCTUnwrap(reachedOptional).grantees.count, 2)
        let isMarked = await world.ada.needsNewKey(itemId: world.itemId)
        XCTAssertTrue(isMarked)
        let markedItemIds = await world.ada.itemIdsNeedingNewKey
        XCTAssertEqual(markedItemIds, [world.itemId])

        await world.server.setRekeyRefusal(nil)
        let retried = try await world.ada.giveNewKey(itemId: world.itemId)
        XCTAssertEqual(retried.fields, loginFields())
        let isStillMarked = await world.ada.needsNewKey(itemId: world.itemId)
        XCTAssertFalse(isStillMarked)
    }

    func testTheNextSaveOfAMarkedItemGivesItANewKeyAndSealsTheNewFieldsWithIt() async throws {
        let world = try await makeSharedWorld()
        await world.server.setRekeyRefusal("api: internal error")
        let narrower = ["user-grace", "user-linus"].map { PasswordShareGranteeInput(granteeUserId: $0) }
        _ = try? await world.ada.setShares(itemId: world.itemId, grantees: narrower, recipients: [])
        await world.server.setRekeyRefusal(nil)
        let keyBeforeOptional = await world.server.item(world.itemId)
        let keyBefore = try XCTUnwrap(keyBeforeOptional).itemKeyByAccountKey

        let saved = try await world.ada.save(fields: loginFields(password: "changed afterwards"), id: world.itemId)
        XCTAssertEqual(saved.fields, loginFields(password: "changed afterwards"))
        let keyAfterOptional = await world.server.item(world.itemId)
        XCTAssertNotEqual(try XCTUnwrap(keyAfterOptional).itemKeyByAccountKey, keyBefore)
        let isMarked = await world.ada.needsNewKey(itemId: world.itemId)
        XCTAssertFalse(isMarked)
    }

    func testAChangeOnAnotherDeviceIsReloadedAndTheRekeyTriedOnce() async throws {
        let server = await makeServer([("user-ada", "Ada")])
        let first = makeDevice(server, userId: "user-ada")
        _ = try await first.setUp(passphrase: "ada invented passphrase", iterationCount: iterationCount)
        let item = try await first.save(fields: loginFields())
        _ = try await first.setUsableByAgent(itemId: item.id, isUsable: true)
        let second = makeDevice(server, userId: "user-ada")
        _ = try await second.unlock(passphrase: "ada invented passphrase")
        try await second.sync()
        // The second device changes the password; the first has not heard.
        _ = try await second.save(fields: loginFields(password: "changed elsewhere"), id: item.id)

        let stopped = try await first.setUsableByAgent(itemId: item.id, isUsable: false)
        XCTAssertFalse(stopped.isUsableByAgent)
        XCTAssertEqual(stopped.fields, loginFields(password: "changed elsewhere"))
        let rekeyCallCount = await server.rekeyCallCount
        XCTAssertEqual(rekeyCallCount, 2)
    }
}
