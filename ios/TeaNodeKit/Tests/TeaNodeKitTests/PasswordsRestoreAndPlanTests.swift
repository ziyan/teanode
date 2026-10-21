import Foundation
import XCTest

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

@testable import TeaNodeKit

/// A server that restores another version than the one asked for.
final class RestoreVersionSwap: @unchecked Sendable {
    var replacementVersionId: String?
}

struct RestoreVersionSwapAPI: PasswordsAPI {
    let inner: FakePasswordsAPI
    let swap: RestoreVersionSwap

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
    func savePasswordItem(_ input: PasswordItemSaveInput) async throws -> PasswordItemView { try await inner.savePasswordItem(input) }
    func deletePasswordItem(id: String) async throws -> PasswordItemView { try await inner.deletePasswordItem(id: id) }
    func restorePasswordItem(id: String) async throws -> PasswordItemView { try await inner.restorePasswordItem(id: id) }
    func purgePasswordItem(id: String) async throws -> Bool { try await inner.purgePasswordItem(id: id) }
    func setPasswordItemShares(itemId: String, grantees: [PasswordShareGranteeInput], keys: [PasswordShareKeyInput]) async throws
        -> PasswordItemView
    { try await inner.setPasswordItemShares(itemId: itemId, grantees: grantees, keys: keys) }
    func addPasswordShareKeys(_ keys: [PasswordItemShareKeyInput]) async throws -> Int { try await inner.addPasswordShareKeys(keys) }
    func setPasswordItemUsableByAgent(itemId: String, isUsableByAgent: Bool, itemKeyByAgentKey: String?) async throws -> PasswordItemView {
        try await inner.setPasswordItemUsableByAgent(itemId: itemId, isUsableByAgent: isUsableByAgent, itemKeyByAgentKey: itemKeyByAgentKey)
    }
    func rekeyPasswordItem(_ input: PasswordItemRekeyInput) async throws -> PasswordItemView { try await inner.rekeyPasswordItem(input) }
    func listPasswordItemVersions(itemId: String) async throws -> [PasswordItemVersionView] {
        try await inner.listPasswordItemVersions(itemId: itemId)
    }
    func restorePasswordItemVersion(itemId: String, versionId: String, baseRevision: Int64) async throws -> PasswordItemView {
        try await inner.restorePasswordItemVersion(
            itemId: itemId, versionId: swap.replacementVersionId ?? versionId, baseRevision: baseRevision)
    }
}

/// Restoring an earlier version brings back the content only, and a confirmed Finish sharing plan is all
/// that is sealed.
final class PasswordsRestoreAndPlanTests: XCTestCase {
    private let iterationCount = PasswordsCrypto.minimumPassphraseIterationCount
    private var directories: [URL] = []

    override func tearDown() {
        for directory in directories { try? FileManager.default.removeItem(at: directory) }
        directories = []
        super.tearDown()
    }

    private func makeCache() -> PasswordsCache {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("restore-plan-\(UUID().uuidString)")
        directories.append(directory)
        return PasswordsCache(fileURL: directory.appendingPathComponent("passwords.json"), pinStore: PasswordsMemoryPinStore())
    }

    private func makeSession(_ server: FakePasswordsServer, userId: String, swap: RestoreVersionSwap? = nil) -> PasswordsSession {
        let api = FakePasswordsAPI(server: server, userId: userId)
        if let swap { return PasswordsSession(api: RestoreVersionSwapAPI(inner: api, swap: swap), cache: makeCache(), userId: userId) }
        return PasswordsSession(api: api, cache: makeCache(), userId: userId)
    }

    private func loginFields(_ password: String = "correct horse") -> PasswordsItemFields {
        .login(PasswordsLoginFields(name: "Example", userName: "ada@example.com", password: password, sites: ["https://example.com"]))
    }

    private func people(_ server: FakePasswordsServer) async throws {
        for (userId, name) in [("user-ada", "Ada"), ("user-ben", "Ben"), ("user-cora", "Cora")] {
            await server.addPerson(userId: userId, name: name)
        }
        for (userId, name) in [("user-ben", "ben"), ("user-cora", "cora")] {
            _ = try await makeSession(server, userId: userId).setUp(passphrase: "\(name) invented passphrase", iterationCount: iterationCount)
        }
    }

    /// Ada's item with a history: a plain save, then shared with Ben and the agent let in, then Ben taken
    /// off and the agent turned off, then an edit. Returns the item and the ids of the versions that
    /// name nobody (`plainVersionId`) and that name Ben with the agent on (`wideVersionId`).
    private func itemWithHistory(
        _ server: FakePasswordsServer, ada: PasswordsSession
    ) async throws -> (item: PasswordItem, plainVersionId: String, wideVersionId: String) {
        let item = try await ada.save(fields: loginFields("first-password"))
        let recipients = try await ada.recipients(granteeUserId: "user-ben")
        _ = try await ada.setShares(itemId: item.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben")], recipients: recipients)
        _ = try await ada.setUsableByAgent(itemId: item.id, isUsable: true)
        _ = try await ada.save(fields: loginFields("second-password"), id: item.id, baseRevision: nil)
        _ = try await ada.setShares(itemId: item.id, grantees: [], recipients: [])
        _ = try await ada.setUsableByAgent(itemId: item.id, isUsable: false)
        _ = try await ada.save(fields: loginFields("third-password"), id: item.id, baseRevision: nil)
        let opened = try await ada.versions(of: item.id)
        var plainVersionId: String?
        var wideVersionId: String?
        for version in opened.versions {
            let access = version.fields.access
            let isWide = access?.isUsableByAgent == true && !(access?.grantees.isEmpty ?? true)
            let isPlain = (access?.grantees.isEmpty ?? true) && access?.isUsableByAgent != true
            if isWide, wideVersionId == nil { wideVersionId = version.id }
            if isPlain, plainVersionId == nil { plainVersionId = version.id }
        }
        return (item, try XCTUnwrap(plainVersionId), try XCTUnwrap(wideVersionId))
    }

    // MARK: The answer to a restore is the version the person chose

    /// The person looked at the plain version and taps Restore. The server restores the wide version (Ben
    /// and the agent on its record) and lists Ben as a recipient. The phone refuses the answer and seals
    /// nothing for Ben or the agent.
    func testRestoreAnsweredWithAnotherOldRecordIsRefusedAndSealsNothing() async throws {
        let server = FakePasswordsServer()
        try await people(server)
        let swap = RestoreVersionSwap()
        let ada = makeSession(server, userId: "user-ada", swap: swap)
        _ = try await ada.setUp(passphrase: "ada invented passphrase", iterationCount: iterationCount)
        let history = try await itemWithHistory(server, ada: ada)
        let before = try await ada.item(id: history.item.id)
        XCTAssertEqual(before.verifiedAccess?.grantees ?? [], [])
        XCTAssertEqual(before.verifiedAccess?.isUsableByAgent, false)

        // The server lists Ben, which the person's own record does not.
        _ = try await server.setShares(
            userId: "user-ada", itemId: history.item.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben")], keys: [])
        try await ada.sync(isFull: true)
        swap.replacementVersionId = history.wideVersionId
        do {
            _ = try await ada.restoreVersion(itemId: history.item.id, versionId: history.plainVersionId)
            XCTFail("a restore answered with another version went through")
        } catch PasswordsError.restoredAnotherVersion {
            // refused
        }
        let storedOptional = await server.item(history.item.id)
        let stored = try XCTUnwrap(storedOptional)
        XCTAssertNil(stored.keys["user-ben"], "Ben got no key")
        XCTAssertFalse(stored.isUsableByAgent, "the agent was not turned on")
        XCTAssertNil(stored.itemKeyByAgentKey)
        let held = try await ada.item(id: history.item.id)
        XCTAssertEqual(held.verifiedAccess?.grantees ?? [], [], "nothing was kept from the answer")
    }

    /// A version the server did not list in this session cannot be restored, and the server is not asked.
    func testRestoreOfAVersionNotListedInThisSessionIsRefused() async throws {
        let server = FakePasswordsServer()
        try await people(server)
        let ada = makeSession(server, userId: "user-ada")
        _ = try await ada.setUp(passphrase: "ada invented passphrase", iterationCount: iterationCount)
        let history = try await itemWithHistory(server, ada: ada)
        let other = makeSession(server, userId: "user-ada")
        _ = try await other.unlock(passphrase: "ada invented passphrase")
        let revisionBefore = await server.item(history.item.id)?.revision
        do {
            _ = try await other.restoreVersion(itemId: history.item.id, versionId: history.plainVersionId)
            XCTFail("a restore of a version nobody listed went through")
        } catch PasswordsError.itemNotFound {
            // refused
        }
        let revisionAfter = await server.item(history.item.id)?.revision
        XCTAssertEqual(revisionAfter, revisionBefore)
    }

    // MARK: Sharing stays as it is now

    /// An honest restore of the version in which the agent was on and Ben was shared: the content comes
    /// back, Cora (shared with since) keeps her key, Ben (only in the old record) gets none, and the
    /// agent stays off.
    func testRestoreOfAnOldVersionKeepsTheSharingAsItIsNow() async throws {
        let server = FakePasswordsServer()
        try await people(server)
        let ada = makeSession(server, userId: "user-ada")
        _ = try await ada.setUp(passphrase: "ada invented passphrase", iterationCount: iterationCount)
        let history = try await itemWithHistory(server, ada: ada)
        // Cora is shared with after all of that.
        let coraRecipients = try await ada.recipients(granteeUserId: "user-cora")
        _ = try await ada.setShares(
            itemId: history.item.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-cora")], recipients: coraRecipients)
        let beforeOptional = await server.item(history.item.id)
        XCTAssertNotNil(try XCTUnwrap(beforeOptional).keys["user-cora"])
        XCTAssertFalse(try XCTUnwrap(beforeOptional).isUsableByAgent)

        let restored = try await ada.restoreVersion(itemId: history.item.id, versionId: history.wideVersionId)
        guard case .login(let login) = restored.fields else { return XCTFail("not a login") }
        XCTAssertEqual(login.password, "second-password")
        let afterOptional = await server.item(history.item.id)
        let after = try XCTUnwrap(afterOptional)
        XCTAssertFalse(after.isUsableByAgent, "the restore does not turn the agent on")
        XCTAssertNil(after.itemKeyByAgentKey)
        XCTAssertNotNil(after.keys["user-cora"], "Cora, shared with now, has the new key")
        XCTAssertNil(after.keys["user-ben"], "Ben was only in the old version's record")
        XCTAssertFalse(after.needsNewKey)
        XCTAssertEqual(restored.verifiedAccess?.grantees, [.person(userId: "user-cora")])
        XCTAssertEqual(restored.verifiedAccess?.isUsableByAgent, false)
        let needingYou = await ada.itemIdsNeedingYou
        XCTAssertFalse(needingYou.contains(history.item.id))
    }

    // MARK: The plan is what is sealed

    /// The plan names Ben. Before the person confirms, the phone reads the server again and a second
    /// member is in the circle. Finishing seals for Ben alone.
    func testFinishSharingSealsOnlyForThePeopleThePlanNamed() async throws {
        let server = FakePasswordsServer()
        try await people(server)
        await server.setCircle("circle-one", members: ["user-ada", "user-ben"])
        let passphrase = "ada invented passphrase"
        let one = makeSession(server, userId: "user-ada")
        _ = try await one.setUp(passphrase: passphrase, iterationCount: iterationCount)
        let item = try await one.save(fields: loginFields())
        let circleRecipients = try await one.recipients(granteeCircleId: "circle-one")
        _ = try await one.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeCircleId: "circle-one")], recipients: circleRecipients)
        // A second phone with no memory of the item: it is marked, so this phone leaves it for the person.
        let two = makeSession(server, userId: "user-ada")
        _ = try await two.unlock(passphrase: passphrase)
        await server.markNeedsNewKey(itemId: item.id)
        try await two.sync(isFull: true)
        let plan = await two.finishPlan(itemIds: [item.id])
        XCTAssertEqual(plan.people.map(\.userId), ["user-ben"])
        XCTAssertEqual(plan.items.map(\.recipientUserIds), [["user-ben"]])

        // The dialog stays up; the server adds Cora to the circle; the phone reads again.
        await server.setCircle("circle-one", members: ["user-ada", "user-ben", "user-cora"])
        try await two.sync(isFull: true)
        try await two.finishSharing(itemId: item.id, plan: plan)
        let storedOptional = await server.item(item.id)
        let stored = try XCTUnwrap(storedOptional)
        XCTAssertNotNil(stored.keys["user-ben"])
        XCTAssertNil(stored.keys["user-cora"], "Cora was not in the plan the person confirmed")
    }

    /// The same for an item that was not marked: only the waiting people the plan names get a key.
    func testFinishSharingOfAnItemNotMarkedLeavesAWaitingPersonThePlanDidNotNameWithoutAKey() async throws {
        let server = FakePasswordsServer()
        try await people(server)
        await server.setCircle("circle-one", members: ["user-ada", "user-ben"])
        let passphrase = "ada invented passphrase"
        let one = makeSession(server, userId: "user-ada")
        _ = try await one.setUp(passphrase: passphrase, iterationCount: iterationCount)
        let item = try await one.save(fields: loginFields())
        let circleRecipients = try await one.recipients(granteeCircleId: "circle-one")
        _ = try await one.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeCircleId: "circle-one")], recipients: circleRecipients)
        // A second phone with no memory of the item leaves Ben's key for the person: the first phone
        // sealed for him, so take his key away to make him wait.
        await server.removeKey(itemId: item.id, userId: "user-ben")
        let two = makeSession(server, userId: "user-ada")
        _ = try await two.unlock(passphrase: passphrase)
        try await two.sync(isFull: true)
        let plan = await two.finishPlan(itemIds: [item.id])
        XCTAssertEqual(plan.items.map(\.recipientUserIds), [["user-ben"]])
        await server.setCircle("circle-one", members: ["user-ada", "user-ben", "user-cora"])
        try await two.sync(isFull: true)
        try await two.finishSharing(itemId: item.id, plan: plan)
        let storedOptional = await server.item(item.id)
        let stored = try XCTUnwrap(storedOptional)
        XCTAssertNotNil(stored.keys["user-ben"])
        XCTAssertNil(stored.keys["user-cora"])
    }

    func testFinishSharingOfAnItemTheConfirmedPlanDoesNotNameIsRefused() async throws {
        let server = FakePasswordsServer()
        try await people(server)
        let ada = makeSession(server, userId: "user-ada")
        _ = try await ada.setUp(passphrase: "ada invented passphrase", iterationCount: iterationCount)
        let item = try await ada.save(fields: loginFields())
        let plan = await ada.finishPlan(itemIds: [])
        do {
            try await ada.finishSharing(itemId: item.id, plan: plan)
            XCTFail("finished an item the plan did not name")
        } catch PasswordsError.itemNotInSharingPlan {
            // refused
        }
    }
}
