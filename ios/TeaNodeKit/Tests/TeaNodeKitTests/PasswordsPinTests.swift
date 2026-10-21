import Foundation
import XCTest

@testable import TeaNodeKit

/// Forwards to the fake server, and lets the "server" lie in two ways: it adds an item nobody shared, and it answers the setup read with another public key.
final class HostileServerFlags: @unchecked Sendable {
    var plantedItem: PasswordItemView?
    var setupPublicKeyOverride: String?
}

struct HostileServerAPI: PasswordsAPI {
    let inner: FakePasswordsAPI
    let flags: HostileServerFlags
    func readPasswordsSetup() async throws -> PasswordsSetupView {
        let real = try await inner.readPasswordsSetup()
        guard let override = flags.setupPublicKeyOverride else { return real }
        return PasswordsSetupView(
            isSetUp: real.isSetUp, passphraseSalt: real.passphraseSalt, passphraseIterationCount: real.passphraseIterationCount,
            publicKey: override, lockAfterMinutes: real.lockAfterMinutes, itemCount: real.itemCount)
    }
    func setUpPasswords(_ input: PasswordsSetUpInput) async throws -> PasswordsSetupView { try await inner.setUpPasswords(input) }
    func unlockPasswords(proof: String, isRecoveryKey: Bool) async throws -> PasswordsUnlockView {
        try await inner.unlockPasswords(proof: proof, isRecoveryKey: isRecoveryKey)
    }
    func changePasswordsPassphrase(_ input: PasswordsChangePassphraseInput) async throws -> PasswordsSetupView {
        try await inner.changePasswordsPassphrase(input)
    }
    func replacePasswordsRecoveryKey(_ input: PasswordsReplaceRecoveryKeyInput) async throws -> PasswordsSetupView {
        try await inner.replacePasswordsRecoveryKey(input)
    }
    func updatePasswordsSettings(lockAfterMinutes: Int) async throws -> PasswordsSetupView {
        try await inner.updatePasswordsSettings(lockAfterMinutes: lockAfterMinutes)
    }
    func erasePasswords() async throws -> Bool { try await inner.erasePasswords() }
    func listPasswordItems(sinceRevision: Int64?) async throws -> PasswordItemListView {
        let real = try await inner.listPasswordItems(sinceRevision: sinceRevision)
        guard let planted = flags.plantedItem else { return real }
        return PasswordItemListView(items: real.items + [planted], itemIds: real.itemIds + [planted.id], revision: max(real.revision, planted.revision))
    }
    func listPasswordRecipients(granteeUserId: String?, granteeCircleId: String?) async throws -> [PasswordRecipientView] {
        try await inner.listPasswordRecipients(granteeUserId: granteeUserId, granteeCircleId: granteeCircleId)
    }
    func listWaitingPasswordShares() async throws -> [PasswordWaitingShareView] { try await inner.listWaitingPasswordShares() }
    func readPasswordsAgentKey() async throws -> String { try await inner.readPasswordsAgentKey() }
    func savePasswordItem(_ input: PasswordItemSaveInput) async throws -> PasswordItemView { try await inner.savePasswordItem(input) }
    func deletePasswordItem(id: String) async throws -> PasswordItemView { try await inner.deletePasswordItem(id: id) }
    func restorePasswordItem(id: String) async throws -> PasswordItemView { try await inner.restorePasswordItem(id: id) }
    func setPasswordItemShares(itemId: String, grantees: [PasswordShareGranteeInput], keys: [PasswordShareKeyInput]) async throws
        -> PasswordItemView
    {
        try await inner.setPasswordItemShares(itemId: itemId, grantees: grantees, keys: keys)
    }
    func addPasswordShareKeys(_ keys: [PasswordItemShareKeyInput]) async throws -> Int { try await inner.addPasswordShareKeys(keys) }
    func rekeyPasswordItem(_ input: PasswordItemRekeyInput) async throws -> PasswordItemView { try await inner.rekeyPasswordItem(input) }
    func setPasswordItemUsableByAgent(itemId: String, isUsableByAgent: Bool, itemKeyByAgentKey: String?) async throws -> PasswordItemView {
        try await inner.setPasswordItemUsableByAgent(itemId: itemId, isUsableByAgent: isUsableByAgent, itemKeyByAgentKey: itemKeyByAgentKey)
    }
}

/// PasswordsPinTests are the checks that a server choosing keys cannot get an item key out of the phone,
/// and cannot make the phone forget whose key it had seen.
final class PasswordsPinTests: XCTestCase {
    private let iterationCount = PasswordsCrypto.minimumPassphraseIterationCount
    private var directories: [URL] = []

    override func tearDown() {
        for directory in directories { try? FileManager.default.removeItem(at: directory) }
        directories = []
        super.tearDown()
    }

    private func makeCache() -> PasswordsCache {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("passwords-pin-test-\(UUID().uuidString)")
        directories.append(directory)
        return PasswordsCache(fileURL: directory.appendingPathComponent("passwords.json"))
    }

    private func loginFields() -> PasswordsItemFields {
        .login(PasswordsLoginFields(name: "Example", userName: "ada@example.com", password: "correct horse", sites: ["https://example.com"]))
    }

    /// A key learned only from an incoming shared item (nobody compared its code) must not let the phone
    /// seal an outgoing item key without asking.
    func testAKeyPlantedByAnIncomingItemIsSeenButNeverLetsTheServerHaveAnItemKey() async throws {
        let server = FakePasswordsServer()
        await server.addPerson(userId: "user-ada", name: "Ada")
        await server.addPerson(userId: "user-mallory", name: "Mallory")
        await server.setCircle("circle-one", members: ["user-ada", "user-mallory"])
        let flags = HostileServerFlags()
        let adaCache = makeCache()
        let ada = PasswordsSession(
            api: HostileServerAPI(inner: FakePasswordsAPI(server: server, userId: "user-ada"), flags: flags), cache: adaCache, userId: "user-ada")
        _ = try await ada.setUp(passphrase: "ada invented passphrase", iterationCount: iterationCount)
        let item = try await ada.save(fields: loginFields())
        // Ada shares with her circle. Mallory has not set up Passwords, so nothing is sealed for anybody.
        let recipients = try await ada.recipients(granteeCircleId: "circle-one")
        _ = try await ada.setShares(itemId: item.id, grantees: [PasswordShareGranteeInput(granteeCircleId: "circle-one")], recipients: recipients)
        // The server operator sets up Passwords for the account Mallory in the circle, with a key pair it holds.
        let operatorDevice = PasswordsSession(
            api: FakePasswordsAPI(server: server, userId: "user-mallory"), cache: makeCache(), userId: "user-mallory")
        _ = try await operatorDevice.setUp(passphrase: "operator passphrase", iterationCount: iterationCount)
        let operatorKey = await server.publicKey(of: "user-mallory")

        try await ada.sync()
        let control = try await ada.completeWaitingShares()
        XCTAssertEqual(control.addedKeyCount, 0)
        XCTAssertEqual(control.waitingForConfirmation.count, 1)

        // The operator adds one made-up shared item to Ada's list, "from" Mallory, carrying the operator's key.
        flags.plantedItem = PasswordItemView(
            id: "planted-1", ownerUserId: "user-mallory", ownerName: "Mallory", ownerPublicKey: operatorKey, itemKind: "login",
            encryptedFields: "{}", itemKey: "{}", isSharedWithMe: true, revision: 99999, isUsableByAgent: false,
            createdAt: "2026-10-20T10:00:00Z", updatedAt: "2026-10-20T10:00:00Z")
        try await ada.sync()
        let pinned = try XCTUnwrap(try adaCache.load())
        // The planted item opens with nothing, so the key it carries is not even recorded as seen.
        XCTAssertNil(pinned.seenPublicKeys["user-mallory"])
        XCTAssertEqual(pinned.confirmedPublicKeys, [:])

        // What the phone does after every sync: the planted key still has to be confirmed.
        let result = try await ada.completeWaitingShares()
        XCTAssertEqual(result.addedKeyCount, 0)
        XCTAssertEqual(result.waitingForConfirmation.count, 1)
        XCTAssertEqual(result.waitingForConfirmation.first?.recipient.keyTrust, .new)

        // Sharing again does not seal for it either.
        let again = try await ada.recipients(granteeCircleId: "circle-one")
        XCTAssertEqual(again.first?.keyTrust, .new)
        _ = try await ada.setShares(itemId: item.id, grantees: [PasswordShareGranteeInput(granteeCircleId: "circle-one")], recipients: again)
        let stored = await server.item(item.id)
        XCTAssertNil(stored?.keys["user-mallory"])

        // The operator's device cannot open Ada's item.
        _ = try await operatorDevice.unlock(passphrase: "operator passphrase")
        try await operatorDevice.sync()
        do {
            _ = try await operatorDevice.item(id: item.id)
            XCTFail("the operator was never given the item key")
        } catch {
            XCTAssertEqual(error as? PasswordsError, .itemNotFound)
        }
    }

    /// The server makes the phone drop its copy by answering the setup read with another public key. The
    /// pins about other people stay, so a later change of an owner's key is still a warning.
    func testAServerAnswerThatDropsTheCopyLeavesThePinsAndALaterKeyChangeStillWarns() async throws {
        let server = FakePasswordsServer()
        await server.addPerson(userId: "user-ada", name: "Ada")
        await server.addPerson(userId: "user-grace", name: "Grace")
        await server.setCircle("circle-one", members: ["user-ada", "user-grace"])
        let ada = PasswordsSession(api: FakePasswordsAPI(server: server, userId: "user-ada"), cache: makeCache(), userId: "user-ada")
        let flags = HostileServerFlags()
        let graceCache = makeCache()
        let grace = PasswordsSession(
            api: HostileServerAPI(inner: FakePasswordsAPI(server: server, userId: "user-grace"), flags: flags), cache: graceCache,
            userId: "user-grace")
        _ = try await ada.setUp(passphrase: "ada invented passphrase", iterationCount: iterationCount)
        _ = try await grace.setUp(passphrase: "grace invented passphrase", iterationCount: iterationCount)
        let item = try await ada.save(fields: loginFields())
        let recipients = try await ada.recipients(granteeUserId: "user-grace")
        _ = try await ada.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-grace")], recipients: recipients,
            confirmedUserIds: ["user-grace"])
        try await grace.sync()
        let adaKey = await server.publicKey(of: "user-ada")
        XCTAssertEqual(try XCTUnwrap(try graceCache.load()).seenPublicKeys, ["user-ada": adaKey])

        // One lying answer to the setup read.
        flags.setupPublicKeyOverride = PasswordsCrypto.makeKeyPair().publicKey.base64EncodedString()
        do {
            try await grace.sync()
            XCTFail("expected needsPassphrase")
        } catch {
            XCTAssertEqual(error as? PasswordsError, .needsPassphrase)
        }
        XCTAssertNil(try graceCache.load())  // the copy is gone
        XCTAssertEqual(try graceCache.loadPins(userId: "user-grace").seenPublicKeys, ["user-ada": adaKey])  // the pins are not
        flags.setupPublicKeyOverride = nil
        _ = try await grace.unlock(passphrase: "grace invented passphrase")
        XCTAssertEqual(try XCTUnwrap(try graceCache.load()).seenPublicKeys, ["user-ada": adaKey])

        // Now Ada's key changes: with the pin kept this is a warning, not a silent first sight.
        let newOwnerKey = PasswordsCrypto.makeKeyPair().publicKey.base64EncodedString()
        await server.replacePublicKey(userId: "user-ada", publicKey: newOwnerKey)
        await server.setRevision(itemId: item.id, revision: 500)
        try await grace.sync(isFull: true)
        let changes = await grace.ownerKeyChanges()
        XCTAssertEqual(changes.map(\.userId), ["user-ada"])
        XCTAssertEqual(changes.first?.newPublicKey, newOwnerKey)
        XCTAssertEqual(try XCTUnwrap(try graceCache.load()).seenPublicKeys["user-ada"], adaKey)
    }

    /// Erasing Passwords on purpose (the person asked to start over) does remove the pins.
    func testErasingPasswordsOnPurposeRemovesThePinsToo() async throws {
        let server = FakePasswordsServer()
        await server.addPerson(userId: "user-ada", name: "Ada")
        await server.addPerson(userId: "user-grace", name: "Grace")
        let ada = PasswordsSession(api: FakePasswordsAPI(server: server, userId: "user-ada"), cache: makeCache(), userId: "user-ada")
        let graceCache = makeCache()
        let grace = PasswordsSession(api: FakePasswordsAPI(server: server, userId: "user-grace"), cache: graceCache, userId: "user-grace")
        _ = try await ada.setUp(passphrase: "ada invented passphrase", iterationCount: iterationCount)
        _ = try await grace.setUp(passphrase: "grace invented passphrase", iterationCount: iterationCount)
        let item = try await ada.save(fields: loginFields())
        let recipients = try await ada.recipients(granteeUserId: "user-grace")
        _ = try await ada.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-grace")], recipients: recipients,
            confirmedUserIds: ["user-grace"])
        try await grace.sync()
        XCTAssertFalse(try graceCache.loadPins(userId: "user-grace").seenPublicKeys.isEmpty)
        try await grace.erase()
        XCTAssertNil(try graceCache.load())
        XCTAssertEqual(try graceCache.loadPins(userId: "user-grace"), PasswordsPins(userId: "user-grace"))
        XCTAssertFalse(FileManager.default.fileExists(atPath: graceCache.pinsFileURL.path))
    }
}
