import Foundation
import XCTest

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

@testable import TeaNodeKit

/// HookedServerFlags lets a test see the calls a session makes and lets the "server" lie.
final class HookedServerFlags: @unchecked Sendable {
    /// The names of the mutating calls, in order.
    var calls: [String] = []
    var lastRekeyInput: PasswordItemRekeyInput?
    /// The keys of the last `SetPasswordItemShares` call.
    var lastShareKeys: [PasswordShareKeyInput] = []
    var addedShareKeyCalls: [[PasswordItemShareKeyInput]] = []
    /// Item ids whose owner view claims the agent may use it and that it needs a new key.
    var falselyAgentAndMarkedItemIds: Set<String> = []
    /// Waiting rows the server invents.
    var forgedWaiting: [PasswordWaitingShareView] = []
    /// Shares the server invents on an owner's item, by item id.
    var forgedShares: [String: [PasswordShareView]] = [:]
    /// The answer to a change of shares is the item as it was before the change.
    var isAnsweringSharesUnchanged = false
    /// Replaces the sealed fields of a shared item the server lists, by item id.
    var damagedFieldsByItemId: [String: String] = [:]
}

/// HookedAPI forwards to the fake server and records or bends what the flags say.
struct HookedAPI: PasswordsAPI {
    let inner: FakePasswordsAPI
    let flags: HookedServerFlags

    private func bent(_ view: PasswordItemView) -> PasswordItemView {
        let isFalselyMarked = flags.falselyAgentAndMarkedItemIds.contains(view.id)
        let shares = view.shares + (flags.forgedShares[view.id] ?? [])
        let encryptedFields = flags.damagedFieldsByItemId[view.id] ?? view.encryptedFields
        return PasswordItemView(
            id: view.id, ownerUserId: view.ownerUserId, ownerName: view.ownerName, ownerPublicKey: view.ownerPublicKey,
            itemKind: view.itemKind, encryptedFields: encryptedFields, itemKey: view.itemKey, isSharedWithMe: view.isSharedWithMe,
            revision: view.revision, isUsableByAgent: view.isUsableByAgent || isFalselyMarked, lastUsedByAgentAt: view.lastUsedByAgentAt,
            deletedAt: view.deletedAt, createdAt: view.createdAt, updatedAt: view.updatedAt, shares: shares,
            needsNewKey: view.needsNewKey || isFalselyMarked)
    }

    func readPasswordsSetup() async throws -> PasswordsSetupView { try await inner.readPasswordsSetup() }
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
        return PasswordItemListView(items: real.items.map(bent), itemIds: real.itemIds, revision: real.revision)
    }
    func listPasswordRecipients(granteeUserId: String?, granteeCircleId: String?) async throws -> [PasswordRecipientView] {
        try await inner.listPasswordRecipients(granteeUserId: granteeUserId, granteeCircleId: granteeCircleId)
    }
    func listWaitingPasswordShares() async throws -> [PasswordWaitingShareView] {
        try await inner.listWaitingPasswordShares() + flags.forgedWaiting
    }
    func readPasswordsAgentKey() async throws -> String { try await inner.readPasswordsAgentKey() }
    func savePasswordItem(_ input: PasswordItemSaveInput) async throws -> PasswordItemView {
        flags.calls.append("savePasswordItem")
        return bent(try await inner.savePasswordItem(input))
    }
    func deletePasswordItem(id: String) async throws -> PasswordItemView {
        flags.calls.append("deletePasswordItem")
        return bent(try await inner.deletePasswordItem(id: id))
    }
    func restorePasswordItem(id: String) async throws -> PasswordItemView {
        flags.calls.append("restorePasswordItem")
        return bent(try await inner.restorePasswordItem(id: id))
    }
    func purgePasswordItem(id: String) async throws -> Bool {
        flags.calls.append("purgePasswordItem")
        return try await inner.purgePasswordItem(id: id)
    }
    func setPasswordItemShares(itemId: String, grantees: [PasswordShareGranteeInput], keys: [PasswordShareKeyInput]) async throws
        -> PasswordItemView
    {
        flags.calls.append("setPasswordItemShares")
        flags.lastShareKeys = keys
        var before: PasswordItemView?
        if flags.isAnsweringSharesUnchanged {
            before = try await inner.listPasswordItems(sinceRevision: nil).items.first { $0.id == itemId }
        }
        let real = try await inner.setPasswordItemShares(itemId: itemId, grantees: grantees, keys: keys)
        return bent(before ?? real)
    }
    func addPasswordShareKeys(_ keys: [PasswordItemShareKeyInput]) async throws -> Int {
        flags.calls.append("addPasswordShareKeys")
        flags.addedShareKeyCalls.append(keys)
        return try await inner.addPasswordShareKeys(keys)
    }
    func rekeyPasswordItem(_ input: PasswordItemRekeyInput) async throws -> PasswordItemView {
        flags.calls.append("rekeyPasswordItem")
        flags.lastRekeyInput = input
        return bent(try await inner.rekeyPasswordItem(input))
    }
    func setPasswordItemUsableByAgent(itemId: String, isUsableByAgent: Bool, itemKeyByAgentKey: String?) async throws -> PasswordItemView {
        flags.calls.append("setPasswordItemUsableByAgent")
        return bent(
            try await inner.setPasswordItemUsableByAgent(
                itemId: itemId, isUsableByAgent: isUsableByAgent, itemKeyByAgentKey: itemKeyByAgentKey))
    }
}

/// PasswordsAccessFlowTests check the rules of the access record: what is sealed inside the
/// item decides who gets a key and whether the agent may use it, in the order of calls that
/// keeps it from ever being wider than what the person chose, whatever the server says.
final class PasswordsAccessFlowTests: XCTestCase {
    private let iterationCount = PasswordsCrypto.minimumPassphraseIterationCount
    private var directories: [URL] = []

    override func tearDown() {
        for directory in directories { try? FileManager.default.removeItem(at: directory) }
        directories = []
        super.tearDown()
    }

    private func makeCache(pinStore: (any PasswordsPinStore)? = nil) -> PasswordsCache {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("passwords-flow-\(UUID().uuidString)")
        directories.append(directory)
        return PasswordsCache(fileURL: directory.appendingPathComponent("passwords.json"), pinStore: pinStore)
    }

    private func makeSession(
        _ server: FakePasswordsServer, userId: String, flags: HookedServerFlags? = nil, cache: PasswordsCache? = nil
    ) -> PasswordsSession {
        let api = FakePasswordsAPI(server: server, userId: userId)
        let cacheToUse = cache ?? makeCache()
        if let flags {
            return PasswordsSession(api: HookedAPI(inner: api, flags: flags), cache: cacheToUse, userId: userId)
        }
        return PasswordsSession(api: api, cache: cacheToUse, userId: userId)
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

    /// The access record the owner sealed in the item, read the way the server would have to: with the owner's account key.
    private func sealedAccess(of itemId: String, server: FakePasswordsServer, accountKey: Data) async throws -> PasswordsItemAccess? {
        let storedOptional = await server.item(itemId)
        let stored = try XCTUnwrap(storedOptional)
        let itemKey = try PasswordsCrypto.open(
            try PasswordsSealedValue(jsonText: stored.itemKeyByAccountKey), key: accountKey,
            associatedData: PasswordsCrypto.itemKeyAssociatedData(itemId: itemId))
        let kind = try XCTUnwrap(PasswordsItemKind(rawValue: stored.itemKind))
        return try PasswordsCrypto.openItemFields(
            try PasswordsSealedValue(jsonText: stored.encryptedFields), itemKey: itemKey, itemId: itemId, itemKind: kind
        ).access
    }

    // MARK: The order of calls

    func testSharingSavesTheWiderRecordBeforeTheServerIsToldAndUnsharingTellsTheServerBeforeTheRekey() async throws {
        let server = await makeServer([("user-ada", "Ada"), ("user-ben", "Ben")])
        let flags = HookedServerFlags()
        let ada = makeSession(server, userId: "user-ada", flags: flags)
        try await setUp(ada, passphrase: "ada invented passphrase")
        let accountKey = try await makeSession(server, userId: "user-ada").unlock(passphrase: "ada invented passphrase").accountKey
        let ben = makeSession(server, userId: "user-ben")
        try await setUp(ben, passphrase: "ben invented passphrase")
        let item = try await ada.save(fields: loginFields())
        flags.calls = []

        let recipients = try await ada.recipients(granteeUserId: "user-ben")
        let shared = try await ada.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben")], recipients: recipients)
        XCTAssertEqual(flags.calls, ["savePasswordItem", "setPasswordItemShares"])
        XCTAssertEqual(shared.sharingCheck, .verified)
        XCTAssertEqual(shared.fields.access?.grantees, [.person(userId: "user-ben")])
        XCTAssertEqual(shared.fields.access?.isUsableByAgent, false)
        // The item started at version 1; sharing signed version 2.
        XCTAssertEqual(shared.fields.access?.recordVersion, 2)
        let storedAccess = try await sealedAccess(of: item.id, server: server, accountKey: accountKey)
        XCTAssertEqual(storedAccess?.grantees, [.person(userId: "user-ben")])
        try await ben.sync()
        let benOpened = try await ben.item(id: item.id)
        XCTAssertEqual(benOpened.fields.withAccess(nil), loginFields())

        flags.calls = []
        _ = try await ada.setShares(itemId: item.id, grantees: [], recipients: [])
        XCTAssertEqual(flags.calls, ["setPasswordItemShares", "rekeyPasswordItem"])
        let rekeyed = try await sealedAccess(of: item.id, server: server, accountKey: accountKey)
        XCTAssertEqual(rekeyed?.grantees, [])
        XCTAssertEqual(rekeyed?.isUsableByAgent, false)
        XCTAssertEqual(rekeyed?.recordVersion, 3)
        XCTAssertTrue(PasswordsCrypto.isAccessVerified(accountKey: accountKey, itemId: item.id, itemKind: .login, access: rekeyed))
        let needsNewKey = await ada.needsNewKey(itemId: item.id)
        XCTAssertFalse(needsNewKey)
    }

    func testTurningTheAgentOnSavesTheRecordFirstAndTurningItOffTellsTheServerBeforeTheRekey() async throws {
        let server = await makeServer([("user-ada", "Ada")])
        let flags = HookedServerFlags()
        let ada = makeSession(server, userId: "user-ada", flags: flags)
        try await setUp(ada, passphrase: "ada invented passphrase")
        let item = try await ada.save(fields: loginFields())
        flags.calls = []

        let on = try await ada.setUsableByAgent(itemId: item.id, isUsable: true)
        XCTAssertTrue(on.isUsableByAgent)
        XCTAssertEqual(flags.calls, ["savePasswordItem", "setPasswordItemUsableByAgent"])
        XCTAssertEqual(on.fields.access?.isUsableByAgent, true)

        flags.calls = []
        let off = try await ada.setUsableByAgent(itemId: item.id, isUsable: false)
        XCTAssertFalse(off.isUsableByAgent)
        XCTAssertEqual(flags.calls, ["setPasswordItemUsableByAgent", "rekeyPasswordItem"])
        XCTAssertNil(flags.lastRekeyInput?.itemKeyByAgentKey)
        XCTAssertEqual(off.fields.access?.isUsableByAgent, false)
        let stored = await server.item(item.id)
        XCTAssertNil(stored?.itemKeyByAgentKey)
        XCTAssertEqual(stored?.needsNewKey, false)
    }

    func testAnEditNeverChangesTheAccessRecord() async throws {
        let server = await makeServer([("user-ada", "Ada"), ("user-ben", "Ben")])
        let ada = makeSession(server, userId: "user-ada")
        try await setUp(ada, passphrase: "ada invented passphrase")
        try await setUp(makeSession(server, userId: "user-ben"), passphrase: "ben invented passphrase")
        let item = try await ada.save(fields: loginFields())
        let recipients = try await ada.recipients(granteeUserId: "user-ben")
        _ = try await ada.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben")], recipients: recipients)
        _ = try await ada.setUsableByAgent(itemId: item.id, isUsable: true)
        let agentOnItem = try await ada.item(id: item.id)
        let access = try XCTUnwrap(agentOnItem.fields.access)
        XCTAssertEqual(agentOnItem.sharingCheck, .verified)
        XCTAssertEqual(access.isUsableByAgent, true)
        XCTAssertEqual(access.grantees, [.person(userId: "user-ben")])

        // An editor that built its fields without the record, or with a wider one, changes nothing.
        let withoutRecord = loginFields(password: "edited once")
        let widerRecord = loginFields(password: "edited twice").withAccess(
            PasswordsItemAccess(isUsableByAgent: true, grantees: [.person(userId: "user-ben"), .person(userId: "user-mallory")]))
        for fields in [withoutRecord, widerRecord] {
            let saved = try await ada.save(fields: fields, id: item.id)
            XCTAssertEqual(saved.fields.access, access)
        }
        // A new item starts with a signed record that grants nobody, whatever the fields carry.
        let fresh = try await ada.save(fields: loginFields(name: "Fresh").withAccess(access))
        XCTAssertEqual(fresh.sharingCheck, .verified)
        XCTAssertEqual(fresh.fields.access?.grantees, [])
        XCTAssertEqual(fresh.fields.access?.isUsableByAgent, false)
        XCTAssertEqual(fresh.fields.access?.recordVersion, 1)
    }

    // MARK: What the server says does not decide

    func testAServerThatAnswersWithTheSharesUnchangedAfterANarrowingStillGetsTheRekey() async throws {
        let server = await makeServer([("user-ada", "Ada"), ("user-ben", "Ben")])
        let flags = HookedServerFlags()
        let ada = makeSession(server, userId: "user-ada", flags: flags)
        try await setUp(ada, passphrase: "ada invented passphrase")
        let ben = makeSession(server, userId: "user-ben")
        try await setUp(ben, passphrase: "ben invented passphrase")
        let item = try await ada.save(fields: loginFields())
        let recipients = try await ada.recipients(granteeUserId: "user-ben")
        _ = try await ada.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben")], recipients: recipients)
        let keyBefore = await server.item(item.id)?.itemKeyByAccountKey

        flags.isAnsweringSharesUnchanged = true
        flags.calls = []
        _ = try await ada.setShares(itemId: item.id, grantees: [], recipients: [])
        // The answer was the old item, so the first re-key is based on a revision the server has left behind;
        // it is refused, the copy is read again, and the second one goes through.
        XCTAssertEqual(flags.calls.first, "setPasswordItemShares")
        XCTAssertEqual(flags.calls.last, "rekeyPasswordItem")
        XCTAssertGreaterThanOrEqual(flags.calls.filter { $0 == "rekeyPasswordItem" }.count, 1)
        let stored = await server.item(item.id)
        XCTAssertNotEqual(stored?.itemKeyByAccountKey, keyBefore)
        XCTAssertNil(stored?.keys["user-ben"])
        XCTAssertEqual(stored?.needsNewKey, false)
    }

    func testAForgedWaitingShareAndAForgedShareNeverGetAKey() async throws {
        let server = await makeServer([("user-ada", "Ada"), ("user-mallory", "Mallory")])
        let flags = HookedServerFlags()
        let ada = makeSession(server, userId: "user-ada", flags: flags)
        try await setUp(ada, passphrase: "ada invented passphrase")
        let mallory = makeSession(server, userId: "user-mallory")
        try await setUp(mallory, passphrase: "mallory invented passphrase")
        // Ada knows Mallory's key from sharing another item with her.
        let shared = try await ada.save(fields: loginFields(name: "Shared"))
        let recipients = try await ada.recipients(granteeUserId: "user-mallory")
        _ = try await ada.setShares(
            itemId: shared.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-mallory")], recipients: recipients)
        let secret = try await ada.save(fields: loginFields(name: "Secret"))
        let malloryView = try XCTUnwrap(recipients.first.map { recipient in
            PasswordRecipientView(
                userId: recipient.userId, name: recipient.name, username: recipient.username,
                publicKey: recipient.publicKey?.base64EncodedString(), hasKey: false)
        })

        // The server lists Mallory as waiting for the secret item and invents a share that reaches her.
        flags.forgedWaiting = [PasswordWaitingShareView(itemId: secret.id, recipient: malloryView)]
        flags.forgedShares[secret.id] = [
            PasswordShareView(id: "forged", granteeUserId: "user-mallory", granteeCircleId: nil, granteeName: "Mallory", recipients: [malloryView])
        ]
        try await ada.sync(isFull: true)
        flags.calls = []
        let completed = try await ada.completeWaitingShares()
        XCTAssertEqual(completed.addedKeyCount, 0)
        XCTAssertTrue(flags.addedShareKeyCalls.isEmpty)

        // A new key for the secret item is not sealed for her either.
        _ = try await ada.giveNewKey(itemId: secret.id)
        XCTAssertEqual(flags.lastRekeyInput?.shareKeys.count, 0)
        let stored = await server.item(secret.id)
        XCTAssertTrue(stored?.keys.isEmpty ?? false)
    }

    func testARekeySealsTheAgentCopyOnlyWhenTheOwnersRecordSaysTheAgentMayUseTheItem() async throws {
        let server = await makeServer([("user-ada", "Ada")])
        let flags = HookedServerFlags()
        let ada = makeSession(server, userId: "user-ada", flags: flags)
        try await setUp(ada, passphrase: "ada invented passphrase")
        let item = try await ada.save(fields: loginFields())
        // The server claims the agent may use the item and that it needs a new key.
        flags.falselyAgentAndMarkedItemIds = [item.id]
        try await ada.sync(isFull: true)
        let listed = try await ada.list()
        XCTAssertEqual(listed.items.count, 1)
        XCTAssertFalse(listed.items[0].isUsableByAgent, "the server's flag alone is not enough")
        XCTAssertTrue(listed.items[0].needsNewKey)

        let given = await ada.giveNewKeysToMarkedItems()
        XCTAssertEqual(given, [item.id])
        XCTAssertNil(flags.lastRekeyInput?.itemKeyByAgentKey)
        let stored = await server.item(item.id)
        XCTAssertNil(stored?.itemKeyByAgentKey)
    }

    func testARekeyKeepsTheAgentCopyWhenTheRecordSaysSo() async throws {
        let server = await makeServer([("user-ada", "Ada")])
        let flags = HookedServerFlags()
        let ada = makeSession(server, userId: "user-ada", flags: flags)
        try await setUp(ada, passphrase: "ada invented passphrase")
        let item = try await ada.save(fields: loginFields())
        _ = try await ada.setUsableByAgent(itemId: item.id, isUsable: true)
        _ = try await ada.giveNewKey(itemId: item.id)
        XCTAssertNotNil(flags.lastRekeyInput?.itemKeyByAgentKey)
        let opened = try await ada.item(id: item.id)
        XCTAssertTrue(opened.isUsableByAgent)
    }

    // MARK: The mark the server sets

    func testAMemberLeavingACircleMarksTheItemAndTheNextOpenGivesItANewKeyBeforeASave() async throws {
        let server = await makeServer([("user-ada", "Ada"), ("user-grace", "Grace"), ("user-linus", "Linus")])
        await server.setCircle("circle-one", members: ["user-ada", "user-grace", "user-linus"])
        let flags = HookedServerFlags()
        let ada = makeSession(server, userId: "user-ada", flags: flags)
        try await setUp(ada, passphrase: "ada invented passphrase")
        let grace = makeSession(server, userId: "user-grace")
        let linus = makeSession(server, userId: "user-linus")
        try await setUp(grace, passphrase: "grace invented passphrase")
        try await setUp(linus, passphrase: "linus invented passphrase")
        let item = try await ada.save(fields: loginFields())
        let recipients = try await ada.recipients(granteeCircleId: "circle-one")
        _ = try await ada.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeCircleId: "circle-one")], recipients: recipients)
        try await linus.sync()
        let linusOpened = try await linus.item(id: item.id)
        XCTAssertEqual(linusOpened.fields.withAccess(nil), loginFields())

        // Linus leaves the circle. The server marks the item and drops his key.
        await server.setCircle("circle-one", members: ["user-ada", "user-grace"])
        try await ada.sync()
        let marked = await ada.itemIdsNeedingNewKey
        XCTAssertEqual(marked, [item.id])
        flags.calls = []
        let saved = try await ada.save(fields: loginFields(password: "changed afterwards"), id: item.id)
        XCTAssertEqual(flags.calls, ["rekeyPasswordItem"], "a marked item is given a new key instead of being saved with the old one")
        XCTAssertEqual(saved.fields.withAccess(nil), loginFields(password: "changed afterwards"))
        XCTAssertFalse(saved.needsNewKey)
        let stillMarked = await ada.itemIdsNeedingNewKey
        XCTAssertEqual(stillMarked, [])

        // Grace keeps her access (she held a key when this device last saw the item); Linus is gone.
        try await grace.sync()
        let graceOpened = try await grace.item(id: item.id)
        XCTAssertEqual(graceOpened.fields.withAccess(nil), loginFields(password: "changed afterwards"))
        let linusResult = try await linus.sync()
        XCTAssertEqual(linusResult.removedItemIds, [item.id])
    }

    /// A process that stops between the removal and the re-key (killed, suspended for good) leaves nothing on the phone to
    /// remember it by; the server's mark on the item is what the next launch finds.
    func testTheServersMarkSurvivesAProcessThatStoppedBetweenTheRemovalAndTheRekey() async throws {
        let server = await makeServer([("user-ada", "Ada"), ("user-ben", "Ben")])
        let firstCache = makeCache()
        let first = makeSession(server, userId: "user-ada", cache: firstCache)
        try await setUp(first, passphrase: "ada invented passphrase")
        try await setUp(makeSession(server, userId: "user-ben"), passphrase: "ben invented passphrase")
        let item = try await first.save(fields: loginFields())
        let recipients = try await first.recipients(granteeUserId: "user-ben")
        _ = try await first.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben")], recipients: recipients)
        await server.setRekeyRefusal("api: internal error")
        do {
            _ = try await first.setShares(itemId: item.id, grantees: [], recipients: [])
            XCTFail("the re-key is refused")
        } catch {
            XCTAssertEqual(error as? PasswordsError, .itemNeedsNewKey(itemId: item.id))
        }
        await server.setRekeyRefusal(nil)

        // The phone starts again: the session is new, the pin store (the keychain) is the same, so
        // the memory of the item from the first session is still there.
        let relaunched = makeSession(server, userId: "user-ada", cache: firstCache)
        _ = try await relaunched.unlock(passphrase: "ada invented passphrase")
        try await relaunched.sync()
        let marked = await relaunched.itemIdsNeedingNewKey
        XCTAssertEqual(marked, [item.id])
        let given = await relaunched.giveNewKeysToMarkedItems()
        XCTAssertEqual(given, [item.id])
        let stillMarked = await relaunched.itemIdsNeedingNewKey
        XCTAssertEqual(stillMarked, [])
    }

    /// A device that has nothing of the item (another phone, or one reinstalled) cannot tell an old
    /// signed record from the newest one, so it leaves the marked item for the person, who finishes it.
    func testAMarkedItemADeviceHasNoMemoryOfWaitsForThePersonWhoFinishesIt() async throws {
        let server = await makeServer([("user-ada", "Ada"), ("user-ben", "Ben")])
        let first = makeSession(server, userId: "user-ada")
        try await setUp(first, passphrase: "ada invented passphrase")
        try await setUp(makeSession(server, userId: "user-ben"), passphrase: "ben invented passphrase")
        let item = try await first.save(fields: loginFields())
        let recipients = try await first.recipients(granteeUserId: "user-ben")
        _ = try await first.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben")], recipients: recipients)
        await server.markNeedsNewKey(itemId: item.id)

        let other = makeSession(server, userId: "user-ada")
        _ = try await other.unlock(passphrase: "ada invented passphrase")
        try await other.sync()
        _ = try await other.item(id: item.id)
        let given = await other.giveNewKeysToMarkedItems()
        XCTAssertEqual(given, [], "looking at the item in this session is not a memory of it")
        let needingYou = await other.itemIdsNeedingYou
        XCTAssertEqual(needingYou, [item.id])
        let stillMarked = await other.itemIdsNeedingNewKey
        XCTAssertEqual(stillMarked, [item.id])

        try await other.finishSharing(itemId: item.id)
        let marked = await other.itemIdsNeedingNewKey
        XCTAssertEqual(marked, [])
        let needingYouAfter = await other.itemIdsNeedingYou
        XCTAssertEqual(needingYouAfter, [])
        let stored = await server.item(item.id)
        XCTAssertEqual(Set(stored?.keys.keys.map { $0 } ?? []), ["user-ben"], "the person's own action seals for whom the record reaches")
    }

    func testADeviceGivesAMarkedItemANewKeyOnlyForPeopleItSealedForAndTheRestWaitForThePerson() async throws {
        let server = await makeServer([("user-ada", "Ada"), ("user-grace", "Grace"), ("user-linus", "Linus")])
        await server.setCircle("circle-one", members: ["user-ada", "user-grace", "user-linus"])
        let firstCache = makeCache()
        let first = makeSession(server, userId: "user-ada", cache: firstCache)
        try await setUp(first, passphrase: "ada invented passphrase")
        for userId in ["user-grace", "user-linus"] { try await setUp(makeSession(server, userId: userId), passphrase: "\(userId) invented passphrase") }
        let item = try await first.save(fields: loginFields())
        let recipients = try await first.recipients(granteeCircleId: "circle-one")
        _ = try await first.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeCircleId: "circle-one")], recipients: recipients)
        // Grace is removed from the circle and added again: the item is marked and she has no key.
        await server.setCircle("circle-one", members: ["user-ada", "user-linus"])
        await server.setCircle("circle-one", members: ["user-ada", "user-grace", "user-linus"])

        // The same phone in a later session (same pin store) remembers the item and sealing for both.
        let later = makeSession(server, userId: "user-ada", cache: firstCache)
        _ = try await later.unlock(passphrase: "ada invented passphrase")
        try await later.sync()
        let given = await later.giveNewKeysToMarkedItems()
        XCTAssertEqual(given, [item.id])
        let afterRekey = await server.item(item.id)
        XCTAssertEqual(Set(afterRekey?.keys.keys.map { $0 } ?? []), ["user-grace", "user-linus"])
    }

    func testWaitingSharesOfAnItemADeviceHasNoMemoryOfAreFinishedByThePersonWithANoticeForTheCircleMembers() async throws {
        let server = await makeServer([("user-ada", "Ada"), ("user-grace", "Grace")])
        await server.setCircle("circle-one", members: ["user-ada", "user-grace"])
        let first = makeSession(server, userId: "user-ada")
        try await setUp(first, passphrase: "ada invented passphrase")
        let item = try await first.save(fields: loginFields())
        let early = try await first.recipients(granteeCircleId: "circle-one")
        _ = try await first.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeCircleId: "circle-one")], recipients: early)
        // Grace sets up Passwords afterwards: a second phone of Ada's, which never saw the item, is not to seal for her.
        try await setUp(makeSession(server, userId: "user-grace"), passphrase: "grace invented passphrase")
        let second = makeSession(server, userId: "user-ada")
        _ = try await second.unlock(passphrase: "ada invented passphrase")
        try await second.sync()
        let completed = try await second.completeWaitingShares()
        XCTAssertEqual(completed.addedKeyCount, 0)
        let needingYou = await second.itemIdsNeedingYou
        XCTAssertEqual(needingYou, [item.id])
        let noticesBefore = await second.circleShareNotices()
        XCTAssertEqual(noticesBefore, [])

        // The person opens the item and presses the button.
        try await second.finishSharing(itemId: item.id)
        let stored = await server.item(item.id)
        XCTAssertNotNil(stored?.keys["user-grace"])
        let needingYouAfter = await second.itemIdsNeedingYou
        XCTAssertEqual(needingYouAfter, [])
        let notices = await second.circleShareNotices()
        XCTAssertEqual(
            notices,
            [
                PasswordsCircleShareNotice(
                    itemId: item.id, itemName: "Example", recipientUserId: "user-grace", recipientName: "Grace", circleName: "circle-one")
            ])
        try await second.dismissCircleShareNotice(itemId: item.id, recipientUserId: "user-grace")
        let noticesAfter = await second.circleShareNotices()
        XCTAssertEqual(noticesAfter, [])
    }

    /// Sealing for a circle's member the device sealed for before shows no notice; a person the
    /// record names itself never does.
    func testOnlyACircleMemberTheDeviceNeverSealedForGetsANotice() async throws {
        let server = await makeServer([("user-ada", "Ada"), ("user-ben", "Ben"), ("user-grace", "Grace")])
        await server.setCircle("circle-one", members: ["user-ada", "user-grace"])
        let ada = makeSession(server, userId: "user-ada")
        try await setUp(ada, passphrase: "ada invented passphrase")
        for userId in ["user-ben", "user-grace"] { try await setUp(makeSession(server, userId: userId), passphrase: "\(userId) invented passphrase") }
        let item = try await ada.save(fields: loginFields())
        let benRecipients = try await ada.recipients(granteeUserId: "user-ben")
        let circleRecipients = try await ada.recipients(granteeCircleId: "circle-one")
        _ = try await ada.setShares(
            itemId: item.id,
            grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben"), PasswordShareGranteeInput(granteeCircleId: "circle-one")],
            recipients: benRecipients + circleRecipients)
        let notices = await ada.circleShareNotices()
        XCTAssertEqual(notices.map(\.recipientUserId), ["user-grace"])
        XCTAssertEqual(notices.first?.circleName, "circle-one")
        // Sharing again seals for nobody new: no second notice.
        try await ada.dismissCircleShareNotice(itemId: item.id, recipientUserId: "user-grace")
        _ = try await ada.setShares(
            itemId: item.id,
            grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben"), PasswordShareGranteeInput(granteeCircleId: "circle-one")],
            recipients: benRecipients + circleRecipients)
        let again = await ada.circleShareNotices()
        XCTAssertEqual(again, [])
    }

    // MARK: Share keys name the item key

    func testAShareKeyForAWaitingRecipientNamesTheItemKeyItWasSealedFrom() async throws {
        let server = await makeServer([("user-ada", "Ada"), ("user-grace", "Grace")])
        await server.setCircle("circle-one", members: ["user-ada", "user-grace"])
        let flags = HookedServerFlags()
        let ada = makeSession(server, userId: "user-ada", flags: flags)
        try await setUp(ada, passphrase: "ada invented passphrase")
        let item = try await ada.save(fields: loginFields())
        // Grace has not set up Passwords: nothing is sealed yet.
        let early = try await ada.recipients(granteeCircleId: "circle-one")
        _ = try await ada.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeCircleId: "circle-one")], recipients: early)
        let grace = makeSession(server, userId: "user-grace")
        try await setUp(grace, passphrase: "grace invented passphrase")
        // Ada seals for Grace's key when she finishes the sharing of the item.
        try await ada.finishSharing(itemId: item.id)
        let stored = await server.item(item.id)
        XCTAssertEqual(flags.addedShareKeyCalls.count, 1)
        XCTAssertEqual(flags.addedShareKeyCalls.first?.first?.itemKey, stored?.itemKeyByAccountKey)
        try await grace.sync()
        let opened = try await grace.item(id: item.id)
        XCTAssertEqual(opened.fields.withAccess(nil), loginFields())
    }

    func testTheCopyIsReadInFullOnceWhenTheServerNamesSomebodyTheCopyDoesNotReach() async throws {
        let server = await makeServer([("user-ada", "Ada"), ("user-grace", "Grace")])
        await server.setCircle("circle-one", members: ["user-ada"])
        let ada = makeSession(server, userId: "user-ada")
        try await setUp(ada, passphrase: "ada invented passphrase")
        let grace = makeSession(server, userId: "user-grace")
        try await setUp(grace, passphrase: "grace invented passphrase")
        // Ada knows Grace's key from sharing another item with her.
        let other = try await ada.save(fields: loginFields(name: "Other"))
        let graceRecipients = try await ada.recipients(granteeUserId: "user-grace")
        _ = try await ada.setShares(
            itemId: other.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-grace")], recipients: graceRecipients)
        let item = try await ada.save(fields: loginFields())
        let recipients = try await ada.recipients(granteeCircleId: "circle-one")
        _ = try await ada.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeCircleId: "circle-one")], recipients: recipients)
        try await ada.sync()

        // Grace joins the circle later: the server lists her as waiting; the copy's shares do not list her yet.
        await server.setCircle("circle-one", members: ["user-ada", "user-grace"])
        await server.markListCalls()
        try await ada.finishSharing(itemId: item.id)
        let listCalls = await server.listCallsSinceMark()
        XCTAssertEqual(listCalls.filter { $0 == nil }.count, 1, "one full read")
        try await grace.sync()
        let opened = try await grace.item(id: item.id)
        XCTAssertEqual(opened.fields.withAccess(nil), loginFields())
    }

    // MARK: Keys and safety codes

    func testTheOwnSafetyCodeComesFromTheOpenedKeyPair() async throws {
        let server = await makeServer([("user-ada", "Ada")])
        let ada = makeSession(server, userId: "user-ada")
        try await setUp(ada, passphrase: "ada invented passphrase")
        let code = try await ada.ownSafetyCode()
        XCTAssertEqual(code.split(separator: " ").count, 8)
        let publicKeyText = await server.publicKey(of: "user-ada")
        let publicKey = try XCTUnwrap(Data(base64Encoded: publicKeyText))
        XCTAssertEqual(code, try PasswordsCrypto.safetyCode(publicKey: publicKey))
        await ada.lock()
        do {
            _ = try await ada.ownSafetyCode()
            XCTFail("locked")
        } catch {
            XCTAssertEqual(error as? PasswordsError, .locked)
        }
    }

    func testAnOwnersKeyIsRecordedAsKnownOnlyAfterAnItemOfTheirsOpened() async throws {
        let server = await makeServer([("user-ada", "Ada"), ("user-grace", "Grace")])
        let flags = HookedServerFlags()
        let ada = makeSession(server, userId: "user-ada")
        try await setUp(ada, passphrase: "ada invented passphrase")
        let graceCache = makeCache()
        let grace = makeSession(server, userId: "user-grace", flags: flags, cache: graceCache)
        try await setUp(grace, passphrase: "grace invented passphrase")
        let item = try await ada.save(fields: loginFields())
        let recipients = try await ada.recipients(granteeUserId: "user-grace")
        _ = try await ada.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-grace")], recipients: recipients)

        // The server hands Grace the item with its sealed fields damaged: it does not open, so nothing is recorded.
        flags.damagedFieldsByItemId[item.id] = "{}"
        try await grace.sync()
        let damaged = try await grace.list()
        XCTAssertEqual(damaged.items.count, 0)
        XCTAssertEqual(damaged.unreadableItemIds, [item.id])
        XCTAssertEqual(try graceCache.loadPins(userId: "user-grace"), PasswordsPins(userId: "user-grace"))

        // The real item opens, and then the key is recorded.
        flags.damagedFieldsByItemId = [:]
        try await grace.sync(isFull: true)
        let opened = try await grace.item(id: item.id)
        XCTAssertEqual(opened.fields.withAccess(nil), loginFields())
        let adaKey = await server.publicKey(of: "user-ada")
        XCTAssertEqual(try graceCache.loadPins(userId: "user-grace").knownPublicKeys, ["user-ada": adaKey])
        XCTAssertEqual(try graceCache.loadPins(userId: "user-grace").comparedPublicKeys, [:])
    }

    func testAChangedOwnerKeyThatOpensNothingIsNotRecordedAndComparingCodesMarksTheNewKeyCompared() async throws {
        let server = await makeServer([("user-ada", "Ada"), ("user-grace", "Grace")])
        let ada = makeSession(server, userId: "user-ada")
        try await setUp(ada, passphrase: "ada invented passphrase")
        let graceCache = makeCache()
        let grace = makeSession(server, userId: "user-grace", cache: graceCache)
        try await setUp(grace, passphrase: "grace invented passphrase")
        let item = try await ada.save(fields: loginFields())
        let recipients = try await ada.recipients(granteeUserId: "user-grace")
        _ = try await ada.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-grace")], recipients: recipients)
        try await grace.sync()
        let oldKey = await server.publicKey(of: "user-ada")
        // The server shows another key for Ada without sealing the item key for it: the item opens
        // with nothing, so the key is not recorded and there is no notice.
        let newKey = PasswordsCrypto.makeKeyPair().publicKey.base64EncodedString()
        await server.replacePublicKey(userId: "user-ada", publicKey: newKey)
        await server.setRevision(itemId: item.id, revision: 900)
        try await grace.sync(isFull: true)
        let listed = try await grace.list()
        XCTAssertEqual(listed.unreadableItemIds, [item.id])
        let noticeCount = await grace.keyChanges().count
        XCTAssertEqual(noticeCount, 0)
        XCTAssertEqual(try graceCache.loadPins(userId: "user-grace").knownPublicKeys["user-ada"], oldKey)

        // Comparing codes with the key the person was shown records it, marked as compared.
        try await grace.markCompared(userId: "user-ada", publicKey: newKey)
        let pins = try graceCache.loadPins(userId: "user-grace")
        XCTAssertEqual(pins.knownPublicKeys["user-ada"], newKey)
        XCTAssertEqual(pins.comparedPublicKeys["user-ada"], newKey)
        XCTAssertEqual(pins.changedPublicKeys, [:])
        let recipientsOfAda = await grace.recipients(
            of: [PasswordRecipientView(userId: "user-ada", name: "Ada", username: "ada", publicKey: newKey, hasKey: false)])
        XCTAssertEqual(recipientsOfAda.first?.keyTrust, .known)
        XCTAssertEqual(recipientsOfAda.first?.isCompared, true)
    }
}
