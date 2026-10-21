import Foundation
import XCTest

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

@testable import TeaNodeKit

/// AnswerSwap makes a server answer a request about one item with another item the person owns, as a
/// server that files its answer under another id would, and can inflate the count of keys it stored.
final class AnswerSwap: @unchecked Sendable {
    /// The item every swapped answer shows.
    var shownItemId: String?
    /// The requests about these item ids are answered with `shownItemId`.
    var swappedItemIds: Set<String> = []
    /// What `AddPasswordShareKeys` answers instead of the real count.
    var addedKeyCountOverride: Int?
}

struct SwappingAPI: PasswordsAPI {
    let inner: FakePasswordsAPI
    let swap: AnswerSwap

    private func answered(_ real: PasswordItemView, asked itemId: String) async throws -> PasswordItemView {
        guard swap.swappedItemIds.contains(itemId), let shown = swap.shownItemId else { return real }
        let list = try await inner.listPasswordItems(sinceRevision: nil)
        return list.items.first { $0.id == shown } ?? real
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
    func listPasswordItems(sinceRevision: Int64?) async throws -> PasswordItemListView { try await inner.listPasswordItems(sinceRevision: sinceRevision) }
    func listPasswordRecipients(granteeUserId: String?, granteeCircleId: String?) async throws -> [PasswordRecipientView] {
        try await inner.listPasswordRecipients(granteeUserId: granteeUserId, granteeCircleId: granteeCircleId)
    }
    func listWaitingPasswordShares() async throws -> [PasswordWaitingShareView] { try await inner.listWaitingPasswordShares() }
    func readPasswordsAgentKey() async throws -> String { try await inner.readPasswordsAgentKey() }
    func savePasswordItem(_ input: PasswordItemSaveInput) async throws -> PasswordItemView {
        try await answered(try await inner.savePasswordItem(input), asked: input.id)
    }
    func deletePasswordItem(id: String) async throws -> PasswordItemView {
        try await answered(try await inner.deletePasswordItem(id: id), asked: id)
    }
    func restorePasswordItem(id: String) async throws -> PasswordItemView {
        try await answered(try await inner.restorePasswordItem(id: id), asked: id)
    }
    func purgePasswordItem(id: String) async throws -> Bool { try await inner.purgePasswordItem(id: id) }
    func setPasswordItemShares(itemId: String, grantees: [PasswordShareGranteeInput], keys: [PasswordShareKeyInput]) async throws
        -> PasswordItemView
    {
        try await answered(try await inner.setPasswordItemShares(itemId: itemId, grantees: grantees, keys: keys), asked: itemId)
    }
    func addPasswordShareKeys(_ keys: [PasswordItemShareKeyInput]) async throws -> Int {
        let real = try await inner.addPasswordShareKeys(keys)
        return swap.addedKeyCountOverride ?? real
    }
    func setPasswordItemUsableByAgent(itemId: String, isUsableByAgent: Bool, itemKeyByAgentKey: String?) async throws -> PasswordItemView {
        let real = try await inner.setPasswordItemUsableByAgent(
            itemId: itemId, isUsableByAgent: isUsableByAgent, itemKeyByAgentKey: itemKeyByAgentKey)
        return try await answered(real, asked: itemId)
    }
    func rekeyPasswordItem(_ input: PasswordItemRekeyInput) async throws -> PasswordItemView {
        try await answered(try await inner.rekeyPasswordItem(input), asked: input.id)
    }
    func listPasswordItemVersions(itemId: String) async throws -> [PasswordItemVersionView] {
        try await inner.listPasswordItemVersions(itemId: itemId)
    }
    func restorePasswordItemVersion(itemId: String, versionId: String, baseRevision: Int64) async throws -> PasswordItemView {
        let real = try await inner.restorePasswordItemVersion(itemId: itemId, versionId: versionId, baseRevision: baseRevision)
        return try await answered(real, asked: itemId)
    }
}

/// Round 11, iPhone: an answer is about the item asked for; Finish sharing says what it will do and
/// puts the agent back; restoring is limited to what this device sealed; a first look at a deleted or
/// marked item is not remembered; earlier versions of an item can be read and put back.
final class PasswordsAnswerChecksTests: XCTestCase {
    private let iterationCount = PasswordsCrypto.minimumPassphraseIterationCount
    private let passphrase = "ada invented passphrase"
    private var directories: [URL] = []

    override func tearDown() {
        for directory in directories { try? FileManager.default.removeItem(at: directory) }
        directories = []
        super.tearDown()
    }

    private func makeCache(pinStore: PasswordsMemoryPinStore = PasswordsMemoryPinStore()) -> PasswordsCache {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("answer-checks-\(UUID().uuidString)")
        directories.append(directory)
        return PasswordsCache(fileURL: directory.appendingPathComponent("passwords.json"), pinStore: pinStore)
    }

    private func makeSession(_ server: FakePasswordsServer, userId: String, cache: PasswordsCache? = nil, swap: AnswerSwap? = nil)
        -> PasswordsSession
    {
        let api = FakePasswordsAPI(server: server, userId: userId)
        if let swap {
            return PasswordsSession(api: SwappingAPI(inner: api, swap: swap), cache: cache ?? makeCache(), userId: userId)
        }
        return PasswordsSession(api: api, cache: cache ?? makeCache(), userId: userId)
    }

    private func loginFields(_ name: String = "Example", password: String = "correct horse") -> PasswordsItemFields {
        .login(PasswordsLoginFields(name: name, userName: "ada@example.com", password: password, sites: ["https://example.com"]))
    }

    private func adaAndBen(_ server: FakePasswordsServer) async throws {
        await server.addPerson(userId: "user-ada", name: "Ada")
        await server.addPerson(userId: "user-ben", name: "Ben")
        _ = try await makeSession(server, userId: "user-ben").setUp(passphrase: "ben invented passphrase", iterationCount: iterationCount)
    }

    private func expectRefusedAsAnotherItem(_ operation: () async throws -> Void, file: StaticString = #filePath, line: UInt = #line)
        async
    {
        do {
            try await operation()
            XCTFail("the answer about another item was accepted", file: file, line: line)
        } catch PasswordsError.answeredAboutAnotherItem {
            // refused, as it should be
        } catch {
            XCTFail("refused for another reason: \(error)", file: file, line: line)
        }
    }

    // MARK: An answer about another item

    /// A phone with two items, Y and Z, and a server that answers every request about Y with Z.
    private func yAndZ(sharedWithBen: Bool = false) async throws -> (
        server: FakePasswordsServer, phone: PasswordsSession, cache: PasswordsCache, swap: AnswerSwap, yItem: PasswordItem, zItem: PasswordItem
    ) {
        let server = FakePasswordsServer()
        try await adaAndBen(server)
        let swap = AnswerSwap()
        let cache = makeCache()
        let phone = makeSession(server, userId: "user-ada", cache: cache, swap: swap)
        _ = try await phone.setUp(passphrase: passphrase, iterationCount: iterationCount)
        let yItem = try await phone.save(fields: loginFields("Y"))
        let zItem = try await phone.save(fields: loginFields("Z"))
        if sharedWithBen {
            let recipients = try await phone.recipients(granteeUserId: "user-ben")
            _ = try await phone.setShares(
                itemId: yItem.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben")], recipients: recipients)
        }
        swap.shownItemId = zItem.id
        swap.swappedItemIds = [yItem.id]
        return (server, phone, cache, swap, yItem, zItem)
    }

    private func expectZUntouched(_ phone: PasswordsSession, cache: PasswordsCache, zItem: PasswordItem, yItem: PasswordItem) async throws {
        let zNow = try await phone.item(id: zItem.id)
        XCTAssertEqual(zNow, zItem, "Z in the copy is as it was")
        let pins = try cache.loadPins(userId: "user-ada")
        XCTAssertEqual(pins.itemMemories[zItem.id]?.sealedForUserIds ?? [], [], "nothing was credited to Z")
        XCTAssertEqual(pins.itemMemories[zItem.id]?.didMakeAgentCopy ?? false, false)
        XCTAssertEqual(pins.itemMemories[yItem.id]?.didMakeAgentCopy ?? false, false, "nothing was credited to Y either")
    }

    func testASaveAnsweredWithAnotherItemIsRefusedAndKeepsNothing() async throws {
        let setup = try await yAndZ()
        await expectRefusedAsAnotherItem {
            _ = try await setup.phone.save(fields: self.loginFields("Y changed"), id: setup.yItem.id, baseRevision: setup.yItem.revision)
        }
        try await expectZUntouched(setup.phone, cache: setup.cache, zItem: setup.zItem, yItem: setup.yItem)
        let yNow = try await setup.phone.item(id: setup.yItem.id)
        XCTAssertEqual(yNow.name, "Y", "the copy of Y did not take the answer")
    }

    func testADeleteAnsweredWithAnotherItemIsRefusedAndKeepsNothing() async throws {
        let setup = try await yAndZ()
        await expectRefusedAsAnotherItem { _ = try await setup.phone.delete(id: setup.yItem.id) }
        try await expectZUntouched(setup.phone, cache: setup.cache, zItem: setup.zItem, yItem: setup.yItem)
        let yNow = try await setup.phone.item(id: setup.yItem.id)
        XCTAssertFalse(yNow.isDeleted, "the copy of Y is not marked deleted by an answer about Z")
    }

    func testARestoreAnsweredWithAnotherItemIsRefusedAndRekeysNothing() async throws {
        let setup = try await yAndZ(sharedWithBen: true)
        setup.swap.swappedItemIds = []
        _ = try await setup.phone.delete(id: setup.yItem.id)
        setup.swap.swappedItemIds = [setup.yItem.id]
        let rekeyCountBefore = await setup.server.rekeyCallCount
        await expectRefusedAsAnotherItem { _ = try await setup.phone.restore(id: setup.yItem.id) }
        let rekeyCountAfter = await setup.server.rekeyCallCount
        XCTAssertEqual(rekeyCountAfter, rekeyCountBefore, "no new key was made on the strength of the answer")
        let zNow = try await setup.phone.item(id: setup.zItem.id)
        XCTAssertEqual(zNow, setup.zItem)
    }

    func testTheAgentSwitchAnsweredWithAnotherItemIsRefusedAndCreditsNoAgentCopy() async throws {
        let setup = try await yAndZ()
        await expectRefusedAsAnotherItem { _ = try await setup.phone.setUsableByAgent(itemId: setup.yItem.id, isUsable: true) }
        try await expectZUntouched(setup.phone, cache: setup.cache, zItem: setup.zItem, yItem: setup.yItem)
    }

    /// The iPhone files what it sealed under the id it asked about, not the id the server answers with.
    func testARekeyAnsweredWithAnotherItemPlantsNoMemory() async throws {
        let setup = try await yAndZ(sharedWithBen: true)
        await expectRefusedAsAnotherItem { _ = try await setup.phone.giveNewKey(itemId: setup.yItem.id) }
        let pins = try setup.cache.loadPins(userId: "user-ada")
        XCTAssertEqual(pins.itemMemories[setup.zItem.id]?.sealedForUserIds ?? [], [], "nothing was credited to Z")
        XCTAssertEqual(pins.itemMemories[setup.yItem.id]?.sealedForUserIds, ["user-ben"], "Y keeps what the share sealed, no more")
    }

    /// Removing a person from Y while the server answers the change of shares with Z: the change is
    /// refused, nothing is sealed or re-keyed on the answer, and Z is left as it was.
    func testNarrowingAnsweredWithAnotherItemIsRefusedAndTouchesNothingElse() async throws {
        let setup = try await yAndZ(sharedWithBen: true)
        let zBeforeOptional = await setup.server.item(setup.zItem.id)
        let zBefore = try XCTUnwrap(zBeforeOptional)
        let rekeyCountBefore = await setup.server.rekeyCallCount
        await expectRefusedAsAnotherItem {
            _ = try await setup.phone.setShares(itemId: setup.yItem.id, grantees: [], recipients: [])
        }
        let rekeyCountAfter = await setup.server.rekeyCallCount
        XCTAssertEqual(rekeyCountAfter, rekeyCountBefore, "no re-key was made from the answer")
        let zAfterOptional = await setup.server.item(setup.zItem.id)
        let zAfter = try XCTUnwrap(zAfterOptional)
        XCTAssertEqual(zAfter.encryptedFields, zBefore.encryptedFields, "Z was not rewritten")
        XCTAssertNil(zAfter.itemKeyByAgentKey)
        let pins = try setup.cache.loadPins(userId: "user-ada")
        XCTAssertEqual(pins.itemMemories[setup.zItem.id]?.sealedForUserIds ?? [], [])
    }

    func testAKeyCountPastTheKeysSentIsRefusedAndCreditsNobody() async throws {
        let server = FakePasswordsServer()
        await server.addPerson(userId: "user-ada", name: "Ada")
        await server.addPerson(userId: "user-ben", name: "Ben")
        let swap = AnswerSwap()
        let cache = makeCache()
        let phone = makeSession(server, userId: "user-ada", cache: cache, swap: swap)
        _ = try await phone.setUp(passphrase: passphrase, iterationCount: iterationCount)
        let item = try await phone.save(fields: loginFields())
        let benInput = PasswordShareGranteeInput(granteeUserId: "user-ben")
        let noKeyRecipients = try await phone.recipients(granteeUserId: "user-ben")
        _ = try await phone.setShares(itemId: item.id, grantees: [benInput], recipients: noKeyRecipients)
        // Ben sets up afterwards: his key is waiting for the person.
        _ = try await makeSession(server, userId: "user-ben").setUp(passphrase: "ben invented passphrase", iterationCount: iterationCount)

        swap.addedKeyCountOverride = 7
        try await phone.sync(isFull: true)
        do {
            try await phone.finishSharing(itemId: item.id, plan: await phone.finishPlan(itemIds: [item.id]))
            XCTFail("a count of seven keys for one key sent was accepted")
        } catch PasswordsError.invalidServerAnswer {
            // refused
        }
        let pins = try cache.loadPins(userId: "user-ada")
        XCTAssertEqual(pins.itemMemories[item.id]?.sealedForUserIds ?? [], [], "Ben was not credited")
    }

    // MARK: Restore

    /// The owner took Ben off an item and deleted it. A device with nothing stored is shown the item as
    /// it was while Ben had it. Restoring it (the person's tap on Restore, nothing else) gives Ben no
    /// key: only people this device sealed for get one, the rest is left to Finish sharing.
    func testRestoreOnADeviceThatNeverSawTheItemSealsForNobodyItDidNotSealFor() async throws {
        let server = FakePasswordsServer()
        try await adaAndBen(server)
        let one = makeSession(server, userId: "user-ada")
        _ = try await one.setUp(passphrase: passphrase, iterationCount: iterationCount)
        let item = try await one.save(fields: loginFields())
        let recipients = try await one.recipients(granteeUserId: "user-ben")
        let benInput = PasswordShareGranteeInput(granteeUserId: "user-ben")
        _ = try await one.setShares(itemId: item.id, grantees: [benInput], recipients: recipients)
        let stored = await server.item(item.id)
        let snapshot = try XCTUnwrap(stored)
        _ = try await one.setShares(itemId: item.id, grantees: [], recipients: [])
        _ = try await one.delete(id: item.id)

        let two = makeSession(server, userId: "user-ada")
        _ = try await two.unlock(passphrase: passphrase)
        await server.replaceStoredFields(
            itemId: item.id, encryptedFields: snapshot.encryptedFields, itemKeyByAccountKey: snapshot.itemKeyByAccountKey, needsNewKey: true)
        _ = try await server.setShares(userId: "user-ada", itemId: item.id, grantees: [benInput], keys: [])
        try await two.sync(isFull: true)
        let seen = try await two.item(id: item.id)
        XCTAssertEqual(seen.sharingCheck, .verified)
        XCTAssertTrue(seen.isDeleted)

        _ = try await two.restore(id: item.id)
        let afterOptional = await server.item(item.id)
        let after = try XCTUnwrap(afterOptional)
        XCTAssertNil(after.keys["user-ben"], "Ben has no key for the restored item's new key")
        XCTAssertFalse(after.needsNewKey, "the restored item was given a new key")

        // Nothing automatic gives Ben one either; the item waits for the person.
        _ = try await two.completeWaitingShares()
        let afterAutomatic = await server.item(item.id)
        XCTAssertNil(afterAutomatic?.keys["user-ben"])
        let needingYou = await two.itemIdsNeedingYou
        XCTAssertTrue(needingYou.contains(item.id))

        // The person finishes it: Ben gets his key.
        try await two.finishSharing(itemId: item.id, plan: await two.finishPlan(itemIds: [item.id]))
        let finished = await server.item(item.id)
        XCTAssertNotNil(finished?.keys["user-ben"])
    }

    /// On the device that sealed for Ben itself, restoring gives him the new key at once.
    func testRestoreOnTheDeviceThatSealedForSomebodyGivesThemTheNewKey() async throws {
        let server = FakePasswordsServer()
        try await adaAndBen(server)
        let phone = makeSession(server, userId: "user-ada")
        _ = try await phone.setUp(passphrase: passphrase, iterationCount: iterationCount)
        let item = try await phone.save(fields: loginFields())
        let recipients = try await phone.recipients(granteeUserId: "user-ben")
        _ = try await phone.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben")], recipients: recipients)
        _ = try await phone.delete(id: item.id)
        _ = try await phone.restore(id: item.id)
        let afterOptional = await server.item(item.id)
        XCTAssertNotNil(afterOptional?.keys["user-ben"])
    }

    // MARK: A first look

    func testAFirstLookAtADeletedItemIsNotRemembered() async throws {
        let server = FakePasswordsServer()
        await server.addPerson(userId: "user-ada", name: "Ada")
        let one = makeSession(server, userId: "user-ada")
        _ = try await one.setUp(passphrase: passphrase, iterationCount: iterationCount)
        let item = try await one.save(fields: loginFields())
        _ = try await one.delete(id: item.id)

        let cache = makeCache()
        let two = makeSession(server, userId: "user-ada", cache: cache)
        _ = try await two.unlock(passphrase: passphrase)
        try await two.sync(isFull: true)
        let seen = try await two.item(id: item.id)
        XCTAssertTrue(seen.isDeleted)
        XCTAssertEqual(seen.sharingCheck, .verified)
        let pins = try cache.loadPins(userId: "user-ada")
        XCTAssertEqual(pins.itemMemories[item.id]?.highestRecordVersion ?? 0, 0, "the record of a deleted first look is not remembered")
    }

    func testAFirstLookAtAMarkedItemIsNotRememberedButAnUnmarkedOneIs() async throws {
        let server = FakePasswordsServer()
        await server.addPerson(userId: "user-ada", name: "Ada")
        let one = makeSession(server, userId: "user-ada")
        _ = try await one.setUp(passphrase: passphrase, iterationCount: iterationCount)
        let markedItem = try await one.save(fields: loginFields("Marked"))
        let plainItem = try await one.save(fields: loginFields("Plain"))
        await server.markNeedsNewKey(itemId: markedItem.id)

        let cache = makeCache()
        let two = makeSession(server, userId: "user-ada", cache: cache)
        _ = try await two.unlock(passphrase: passphrase)
        try await two.sync(isFull: true)
        _ = try await two.list()
        let pins = try cache.loadPins(userId: "user-ada")
        XCTAssertEqual(pins.itemMemories[markedItem.id]?.highestRecordVersion ?? 0, 0, "the record of a marked first look is not remembered")
        XCTAssertGreaterThan(pins.itemMemories[plainItem.id]?.highestRecordVersion ?? 0, 0, "an unmarked first look is remembered")
    }

    // MARK: The agent after another device's automatic re-key

    /// A device that only looked at an item gives it a new key when the server marks it; the agent gets no
    /// copy (this device made none), the server clears its flag, and the record still says "on". The item
    /// is listed for Finish sharing, and finishing it puts the agent back.
    func testALostAgentCopyIsListedForFinishSharingAndFinishingPutsItBack() async throws {
        let server = FakePasswordsServer()
        await server.addPerson(userId: "user-ada", name: "Ada")
        let maker = makeSession(server, userId: "user-ada")
        _ = try await maker.setUp(passphrase: passphrase, iterationCount: iterationCount)
        let item = try await maker.save(fields: loginFields())
        _ = try await maker.setUsableByAgent(itemId: item.id, isUsable: true)

        let pinStore = PasswordsMemoryPinStore()
        let firstLook = makeSession(server, userId: "user-ada", cache: makeCache(pinStore: pinStore))
        _ = try await firstLook.unlock(passphrase: passphrase)
        try await firstLook.sync(isFull: true)
        _ = try await firstLook.item(id: item.id)

        await server.markNeedsNewKey(itemId: item.id)
        let second = makeSession(server, userId: "user-ada", cache: makeCache(pinStore: pinStore))
        _ = try await second.unlock(passphrase: passphrase)
        try await second.sync(isFull: true)
        let given = await second.giveNewKeysToMarkedItems()
        XCTAssertEqual(given, [item.id])
        try await second.sync(isFull: true)
        let after = try await second.item(id: item.id)
        XCTAssertEqual(after.fields.access?.isUsableByAgent, true, "the record still says the agent may use it")
        XCTAssertFalse(after.isUsableByAgent, "the server holds no copy for the agent")
        let needingYou = await second.itemIdsNeedingYou
        XCTAssertTrue(needingYou.contains(item.id), "the person is told")

        let plan = await second.finishPlan(itemIds: [item.id])
        XCTAssertEqual(plan.itemCount, 1)
        XCTAssertEqual(plan.agentItemCount, 1)
        XCTAssertEqual(plan.newKeyItemCount, 0)
        XCTAssertTrue(plan.people.isEmpty)

        try await second.sync(isFull: true)
        try await second.finishSharing(itemId: item.id, plan: await second.finishPlan(itemIds: [item.id]))
        try await second.sync(isFull: true)
        let finished = try await second.item(id: item.id)
        XCTAssertTrue(finished.isUsableByAgent, "Finish sharing put the agent back")
        let needingYouAfter = await second.itemIdsNeedingYou
        XCTAssertFalse(needingYouAfter.contains(item.id))
    }

    func testACardIsNeverListedForAnAgentCopy() async throws {
        let server = FakePasswordsServer()
        await server.addPerson(userId: "user-ada", name: "Ada")
        let phone = makeSession(server, userId: "user-ada")
        _ = try await phone.setUp(passphrase: passphrase, iterationCount: iterationCount)
        _ = try await phone.save(fields: .card(PasswordsCardFields(name: "Card", cardholderName: "Ada", number: "4111111111111111")))
        let lost = await phone.itemIdsWithLostAgentCopy
        XCTAssertTrue(lost.isEmpty)
    }

    // MARK: What Finish sharing will do

    func testTheFinishPlanNamesWhoGetsKeysForHowManyItemsAndSealsNothing() async throws {
        let server = FakePasswordsServer()
        try await adaAndBen(server)
        await server.addPerson(userId: "user-cora", name: "Cora")
        let phone = makeSession(server, userId: "user-ada")
        _ = try await phone.setUp(passphrase: passphrase, iterationCount: iterationCount)
        let first = try await phone.save(fields: loginFields("First"))
        let second = try await phone.save(fields: loginFields("Second"))
        let third = try await phone.save(fields: loginFields("Third"))
        let bensGrantee = PasswordShareGranteeInput(granteeUserId: "user-ben")
        let corasGrantee = PasswordShareGranteeInput(granteeUserId: "user-cora")
        let benAndCora = try await phone.recipients(granteeUserId: "user-ben") + phone.recipients(granteeUserId: "user-cora")
        _ = try await phone.setShares(itemId: first.id, grantees: [bensGrantee, corasGrantee], recipients: benAndCora)
        _ = try await phone.setShares(itemId: second.id, grantees: [corasGrantee], recipients: benAndCora)
        _ = try await phone.setShares(itemId: third.id, grantees: [bensGrantee], recipients: benAndCora)
        // Cora sets up Passwords afterwards: she is waiting for First and Second.
        _ = try await makeSession(server, userId: "user-cora").setUp(passphrase: "cora invented passphrase", iterationCount: iterationCount)
        try await phone.sync(isFull: true)

        let plan = await phone.finishPlan(itemIds: [first.id, second.id, third.id, first.id])
        XCTAssertEqual(plan.itemCount, 2, "Third has nothing waiting, and a repeated id counts once")
        XCTAssertEqual(plan.people.map(\.name), ["Cora"])
        XCTAssertEqual(plan.people.first?.itemCount, 2)
        XCTAssertEqual(plan.newKeyItemCount, 0)
        XCTAssertEqual(plan.agentItemCount, 0)
        let firstStored = await server.item(first.id)
        XCTAssertNil(firstStored?.keys["user-cora"], "the plan sealed nothing")

        let onlySecond = await phone.finishPlan(itemIds: [second.id])
        XCTAssertEqual(onlySecond.itemCount, 1)
        XCTAssertEqual(onlySecond.people.first?.itemCount, 1)
    }

    func testTheFinishPlanSaysANewKeyGoesToEveryoneTheRecordReaches() async throws {
        let server = FakePasswordsServer()
        try await adaAndBen(server)
        let phone = makeSession(server, userId: "user-ada")
        _ = try await phone.setUp(passphrase: passphrase, iterationCount: iterationCount)
        let item = try await phone.save(fields: loginFields())
        let recipients = try await phone.recipients(granteeUserId: "user-ben")
        _ = try await phone.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben")], recipients: recipients)
        await server.markNeedsNewKey(itemId: item.id)
        try await phone.sync(isFull: true)
        let plan = await phone.finishPlan(itemIds: [item.id])
        XCTAssertEqual(plan.newKeyItemCount, 1)
        XCTAssertEqual(plan.people.map(\.name), ["Ben"])
    }

    // MARK: Earlier versions

    func testEarlierVersionsAreListedNewestFirstOpenedAndSaidWhoSavedThem() async throws {
        let server = FakePasswordsServer()
        await server.addPerson(userId: "user-ada", name: "Ada")
        let phone = makeSession(server, userId: "user-ada")
        _ = try await phone.setUp(passphrase: passphrase, iterationCount: iterationCount)
        let first = try await phone.save(fields: loginFields("Example", password: "first password"))
        let second = try await phone.save(fields: loginFields("Example", password: "second password"), id: first.id, baseRevision: first.revision)
        await server.setSavesAreByPerson(false)
        _ = try await phone.save(fields: loginFields("Example", password: "third password"), id: first.id, baseRevision: second.revision)

        let opened = try await phone.versions(of: first.id)
        XCTAssertEqual(opened.unreadableVersionCount, 0)
        XCTAssertEqual(opened.versions.count, 2)
        guard case .login(let newest)? = opened.versions.first?.fields, case .login(let oldest)? = opened.versions.last?.fields else {
            return XCTFail("both versions are logins")
        }
        XCTAssertEqual(newest.password, "second password")
        XCTAssertEqual(oldest.password, "first password")
        XCTAssertEqual(opened.versions.first?.isSavedByPerson, false, "the third save came from a program")
        XCTAssertEqual(opened.versions.last?.isSavedByPerson, true)
        XCTAssertNotNil(opened.versions.first?.savedAt)
    }

    func testRestoringAVersionPutsItsFieldsBackAndGivesTheItemANewKeyAtOnce() async throws {
        let server = FakePasswordsServer()
        try await adaAndBen(server)
        let phone = makeSession(server, userId: "user-ada")
        _ = try await phone.setUp(passphrase: passphrase, iterationCount: iterationCount)
        let created = try await phone.save(fields: loginFields("Example", password: "first password"))
        let recipients = try await phone.recipients(granteeUserId: "user-ben")
        let shared = try await phone.setShares(
            itemId: created.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben")], recipients: recipients)
        let edited = try await phone.save(
            fields: loginFields("Example", password: "second password"), id: created.id, baseRevision: shared.revision)
        let keyBefore = await server.item(created.id)?.itemKeyByAccountKey

        // The version saved just before the edit: the first password, with Ben named in its record.
        let opened = try await phone.versions(of: created.id)
        let version = try XCTUnwrap(opened.versions.first)
        guard case .login(let versionLogin) = version.fields else { return XCTFail("a login") }
        XCTAssertEqual(versionLogin.password, "first password")

        let restored = try await phone.restoreVersion(itemId: created.id, versionId: version.id)
        guard case .login(let restoredLogin) = restored.fields else { return XCTFail("a login") }
        XCTAssertEqual(restoredLogin.password, "first password")
        XCTAssertGreaterThan(restored.revision, edited.revision)
        let stored = await server.item(created.id)
        let storedItem = try XCTUnwrap(stored)
        XCTAssertFalse(storedItem.needsNewKey, "the restore was followed by a new key")
        XCTAssertNotEqual(storedItem.itemKeyByAccountKey, keyBefore, "it is not the key of the version any more")
        XCTAssertNotNil(storedItem.keys["user-ben"], "the record held before the restore names Ben, so he gets the new key")
        // The replaced content stays as a version of its own.
        let versionsAfter = try await phone.versions(of: created.id)
        XCTAssertGreaterThan(versionsAfter.versions.count, opened.versions.count)
    }

    /// The restore brings back the content only: the record this device held before is written into the
    /// restored fields, signed above the highest seen, so who the item is shared with stays as it is now,
    /// it is not a replay and nothing waits for the person.
    func testRestoringAVersionWithAnOlderSharingRecordKeepsTheRecordHeldBeforeSignedAboveTheHighestSeen() async throws {
        let server = FakePasswordsServer()
        try await adaAndBen(server)
        let phone = makeSession(server, userId: "user-ada")
        _ = try await phone.setUp(passphrase: passphrase, iterationCount: iterationCount)
        let created = try await phone.save(fields: loginFields("Example", password: "first password"))
        let recipients = try await phone.recipients(granteeUserId: "user-ben")
        let shared = try await phone.setShares(
            itemId: created.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben")], recipients: recipients)
        let highestBefore = try XCTUnwrap(shared.fields.access?.recordVersion)
        // The oldest version holds the first signed record, which names nobody: older than the one now.
        let opened = try await phone.versions(of: created.id)
        let oldest = try XCTUnwrap(opened.versions.last)
        let oldestVersion = try XCTUnwrap(oldest.fields.access?.recordVersion)
        XCTAssertLessThan(oldestVersion, highestBefore)

        let restored = try await phone.restoreVersion(itemId: created.id, versionId: oldest.id)
        XCTAssertEqual(restored.sharingCheck, .verified, "a restore is not a replay")
        XCTAssertEqual(restored.fields.access?.recordVersion, highestBefore + 1)
        XCTAssertEqual(restored.fields.access?.grantees, [.person(userId: "user-ben")], "the restored record named nobody, the one held before names Ben")
        let stored = await server.item(created.id)
        XCTAssertNotNil(stored?.keys["user-ben"], "Ben is shared with now, so he gets the new key")
        XCTAssertEqual(stored?.needsNewKey, false, "the item was given a new key at once")
    }

    /// The server ended the agent's use with the restore, so the agent's copy is not made again by the
    /// restore, whatever the restored version's record said. The record held before still allows it,
    /// so "Finish sharing" offers it back.
    func testRestoringAVersionDoesNotTurnTheAgentOnAndOffersTheCopyOnFinishSharing() async throws {
        let server = FakePasswordsServer()
        try await adaAndBen(server)
        let phone = makeSession(server, userId: "user-ada")
        _ = try await phone.setUp(passphrase: passphrase, iterationCount: iterationCount)
        let created = try await phone.save(fields: loginFields("Example", password: "first password"))
        let recipients = try await phone.recipients(granteeUserId: "user-ben")
        let shared = try await phone.setShares(
            itemId: created.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben")], recipients: recipients)
        let agentOn = try await phone.setUsableByAgent(itemId: created.id, isUsable: true)
        let edited = try await phone.save(
            fields: loginFields("Example", password: "second password"), id: created.id, baseRevision: agentOn.revision)
        XCTAssertNotNil(shared)
        let opened = try await phone.versions(of: created.id)
        let version = try XCTUnwrap(opened.versions.first { $0.fields.access?.isUsableByAgent == true })
        XCTAssertGreaterThan(edited.revision, agentOn.revision)

        let restored = try await phone.restoreVersion(itemId: created.id, versionId: version.id)
        XCTAssertEqual(restored.sharingCheck, .verified)
        XCTAssertFalse(restored.isUsableByAgent, "the restore does not turn the agent on")
        let stored = await server.item(created.id)
        XCTAssertNil(stored?.itemKeyByAgentKey)
        XCTAssertNotNil(stored?.keys["user-ben"])
        XCTAssertEqual(stored?.needsNewKey, false)
        let lostAgentCopyItemIds = await phone.itemIdsWithLostAgentCopy
        XCTAssertTrue(lostAgentCopyItemIds.contains(created.id), "the record still allows the agent, so Finish sharing offers it")
        let plan = await phone.finishPlan(itemIds: [created.id])
        XCTAssertEqual(plan.items.map(\.isAgentIncluded), [true])
        try await phone.finishSharing(itemId: created.id, plan: plan)
        let afterOptional = await server.item(created.id)
        XCTAssertEqual(afterOptional?.isUsableByAgent, true)
    }

    /// The record the item has now has to be the person's own: with one that does not verify, a restore
    /// is refused before the server is asked, and nothing changes.
    func testRestoringAVersionOfAnItemWhoseCurrentRecordDoesNotVerifyIsRefusedBeforeAskingTheServer() async throws {
        let server = FakePasswordsServer()
        try await adaAndBen(server)
        let phone = makeSession(server, userId: "user-ada")
        _ = try await phone.setUp(passphrase: passphrase, iterationCount: iterationCount)
        let created = try await phone.save(fields: loginFields("Example", password: "first password"))
        let second = makeSession(server, userId: "user-ada")
        let unlocked = try await second.unlock(passphrase: passphrase)
        // The server holds a record naming Ben with a signature nobody made.
        let storedBefore = await server.item(created.id)
        let stored = try XCTUnwrap(storedBefore)
        let itemKey = try PasswordsCrypto.open(
            try PasswordsSealedValue(jsonText: stored.itemKeyByAccountKey), key: unlocked.accountKey,
            associatedData: PasswordsCrypto.itemKeyAssociatedData(itemId: created.id))
        let forgedAccess = PasswordsItemAccess(
            isUsableByAgent: false, grantees: [.person(userId: "user-ben")], recordVersion: 9,
            recordMac: Data(count: PasswordsCrypto.keyByteCount).base64EncodedString())
        let forged = try PasswordsCrypto.sealItemFields(loginFields("Example").withAccess(forgedAccess), itemKey: itemKey, itemId: created.id)
        await server.replaceStoredFields(
            itemId: created.id, encryptedFields: forged.jsonText, itemKeyByAccountKey: stored.itemKeyByAccountKey, needsNewKey: false)
        try await phone.sync(isFull: true)
        let current = try await phone.item(id: created.id)
        _ = try await phone.save(fields: loginFields("Example", password: "later"), id: created.id, baseRevision: current.revision)
        let opened = try await phone.versions(of: created.id)
        let version = try XCTUnwrap(opened.versions.first)
        let revisionBefore = await server.item(created.id)?.revision

        do {
            _ = try await phone.restoreVersion(itemId: created.id, versionId: version.id)
            XCTFail("a restore on a record that cannot be checked went through")
        } catch PasswordsError.sharingCouldNotBeChecked {
            // refused
        }
        let after = await server.item(created.id)
        XCTAssertEqual(after?.revision, revisionBefore, "the server was not asked")
        XCTAssertNil(after?.keys["user-ben"])
    }

    func testAVersionRestoreAnsweredWithAnotherItemIsRefused() async throws {
        let setup = try await yAndZ()
        setup.swap.swappedItemIds = []
        _ = try await setup.phone.save(fields: loginFields("Y changed"), id: setup.yItem.id, baseRevision: setup.yItem.revision)
        setup.swap.swappedItemIds = [setup.yItem.id]
        let opened = try await setup.phone.versions(of: setup.yItem.id)
        let version = try XCTUnwrap(opened.versions.first)
        await expectRefusedAsAnotherItem {
            _ = try await setup.phone.restoreVersion(itemId: setup.yItem.id, versionId: version.id)
        }
        let zNow = try await setup.phone.item(id: setup.zItem.id)
        XCTAssertEqual(zNow, setup.zItem)
    }

    func testAVersionRestoreOnARevisionThatMovedOnIsRefusedAsChangedOnAnotherDevice() async throws {
        let server = FakePasswordsServer()
        await server.addPerson(userId: "user-ada", name: "Ada")
        let phone = makeSession(server, userId: "user-ada")
        _ = try await phone.setUp(passphrase: passphrase, iterationCount: iterationCount)
        let created = try await phone.save(fields: loginFields("Example", password: "first password"))
        _ = try await phone.save(fields: loginFields("Example", password: "second password"), id: created.id, baseRevision: created.revision)
        let opened = try await phone.versions(of: created.id)
        let version = try XCTUnwrap(opened.versions.first)
        await server.touch(itemId: created.id)
        do {
            _ = try await phone.restoreVersion(itemId: created.id, versionId: version.id)
            XCTFail("a restore on an old revision went through")
        } catch PasswordsError.itemChangedOnAnotherDevice {
            // refused
        }
    }

    func testVersionsOfAnItemSharedWithThePersonAreRefused() async throws {
        let server = FakePasswordsServer()
        try await adaAndBen(server)
        let ada = makeSession(server, userId: "user-ada")
        _ = try await ada.setUp(passphrase: passphrase, iterationCount: iterationCount)
        let item = try await ada.save(fields: loginFields())
        let recipients = try await ada.recipients(granteeUserId: "user-ben")
        _ = try await ada.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben")], recipients: recipients)
        let ben = makeSession(server, userId: "user-ben")
        _ = try await ben.unlock(passphrase: "ben invented passphrase")
        try await ben.sync(isFull: true)
        do {
            _ = try await ben.versions(of: item.id)
            XCTFail("a recipient listed the owner's versions")
        } catch PasswordsError.onlyOwnerCanChangeItem {
            // refused
        }
    }
}
