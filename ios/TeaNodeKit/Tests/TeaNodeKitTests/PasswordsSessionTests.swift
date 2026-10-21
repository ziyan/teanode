import Foundation
import XCTest

@testable import TeaNodeKit

/// PasswordsSessionTests run the session against a fake server that keeps only
/// sealed text, so a pass here means two devices (or two people) really can
/// open each other's work through nothing but what the server holds.
final class PasswordsSessionTests: XCTestCase {
    /// The lowest count the format accepts, so the tests do not spend minutes in key derivation.
    private let iterationCount = PasswordsCrypto.minimumPassphraseIterationCount
    private var directories: [URL] = []

    override func tearDown() {
        for directory in directories { try? FileManager.default.removeItem(at: directory) }
        directories = []
        super.tearDown()
    }

    private func makeCache() -> PasswordsCache {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("passwords-test-\(UUID().uuidString)")
        directories.append(directory)
        return PasswordsCache(fileURL: directory.appendingPathComponent("passwords.json"))
    }

    private func makeServer(people: [(String, String)] = [("user-ada", "Ada")]) async -> FakePasswordsServer {
        let server = FakePasswordsServer()
        for (userId, name) in people { await server.addPerson(userId: userId, name: name) }
        return server
    }

    private func makeDevice(_ server: FakePasswordsServer, userId: String = "user-ada") -> (session: PasswordsSession, cache: PasswordsCache) {
        let cache = makeCache()
        return (PasswordsSession(api: FakePasswordsAPI(server: server, userId: userId), cache: cache, userId: userId), cache)
    }

    private func loginFields(name: String = "Example", password: String = "correct horse") -> PasswordsItemFields {
        .login(PasswordsLoginFields(name: name, userName: "ada@example.com", password: password, sites: ["https://example.com"]))
    }

    // MARK: Set up and unlock

    func testSetUpReturnsARecoveryKeyThatHasThirteenGroupsAndLeavesTheSessionUnlocked() async throws {
        let server = await makeServer()
        let device = makeDevice(server)
        let recoveryKey = try await device.session.setUp(passphrase: "a long invented passphrase", iterationCount: iterationCount)
        XCTAssertEqual(recoveryKey.split(separator: " ").count, 13)
        let isUnlocked = await device.session.isUnlocked
        XCTAssertTrue(isUnlocked)
        let setup = try await device.session.readSetup()
        XCTAssertTrue(setup.isSetUp)
        XCTAssertEqual(setup.passphraseIterationCount, iterationCount)
        let contents = try XCTUnwrap(try device.cache.load())
        XCTAssertEqual(contents.userId, "user-ada")
        XCTAssertEqual(contents.publicKey, setup.publicKey)
    }

    func testSettingUpTwiceIsRefused() async throws {
        let server = await makeServer()
        let device = makeDevice(server)
        _ = try await device.session.setUp(passphrase: "a long invented passphrase", iterationCount: iterationCount)
        do {
            _ = try await device.session.setUp(passphrase: "another one", iterationCount: iterationCount)
            XCTFail("a second set up should be refused")
        } catch {
            XCTAssertEqual(error as? PasswordsError, .alreadySetUp)
        }
    }

    func testAnotherDeviceUnlocksWithThePassphraseAndTheRecoveryKeyAndReadsWhatTheFirstSaved() async throws {
        let server = await makeServer()
        let first = makeDevice(server)
        let recoveryKey = try await first.session.setUp(passphrase: "a long invented passphrase", iterationCount: iterationCount)
        let saved = try await first.session.save(fields: loginFields())

        let second = makeDevice(server)
        let unlocked = try await second.session.unlock(passphrase: "a long invented passphrase")
        XCTAssertEqual(unlocked.accountKey.count, 32)
        try await second.session.sync()
        let opened = try await second.session.item(id: saved.id)
        XCTAssertEqual(opened.fields.withAccess(nil), loginFields())

        let third = makeDevice(server)
        _ = try await third.session.unlock(recoveryKey: recoveryKey.lowercased().replacingOccurrences(of: " ", with: "-"))
        try await third.session.sync()
        let openedByRecoveryKey = try await third.session.item(id: saved.id)
        XCTAssertEqual(openedByRecoveryKey.fields.withAccess(nil), loginFields())
    }

    func testAWrongPassphraseAndAWrongRecoveryKeyAreTyped() async throws {
        let server = await makeServer()
        let first = makeDevice(server)
        _ = try await first.session.setUp(passphrase: "a long invented passphrase", iterationCount: iterationCount)
        let second = makeDevice(server)
        do {
            _ = try await second.session.unlock(passphrase: "not the passphrase")
            XCTFail("a wrong passphrase should be refused")
        } catch {
            XCTAssertEqual(error as? PasswordsError, .wrongPassphrase)
        }
        do {
            _ = try await second.session.unlock(recoveryKey: try PasswordsCrypto.formatRecoveryKey(PasswordsCrypto.makeRecoveryKey()))
            XCTFail("a wrong recovery key should be refused")
        } catch {
            XCTAssertEqual(error as? PasswordsError, .wrongRecoveryKey)
        }
        let isUnlocked = await second.session.isUnlocked
        XCTAssertFalse(isUnlocked)
    }

    func testUnlockingBeforeSetUpSaysSo() async throws {
        let server = await makeServer()
        let device = makeDevice(server)
        do {
            _ = try await device.session.unlock(passphrase: "anything")
            XCTFail("nothing is set up")
        } catch {
            XCTAssertEqual(error as? PasswordsError, .notSetUp)
        }
    }

    func testTheKeptAccountKeyOpensTheSessionAgainWithoutTheNetwork() async throws {
        let server = await makeServer()
        let device = makeDevice(server)
        _ = try await device.session.setUp(passphrase: "a long invented passphrase", iterationCount: iterationCount)
        let saved = try await device.session.save(fields: loginFields())
        let unlocked = try await device.session.unlock(passphrase: "a long invented passphrase")

        let store = PasswordsMemoryKeyStore()
        try await store.save(accountKey: unlocked.accountKey, signInAccountKey: "account-one")
        await device.session.lock()
        let isLockedAfterLock = await device.session.isUnlocked
        XCTAssertFalse(isLockedAfterLock)

        await server.setOffline(true)
        let kept = try await store.load(signInAccountKey: "account-one", promptReason: "Open Passwords")
        let lastReason = await store.lastPromptReason
        XCTAssertEqual(lastReason, "Open Passwords")
        _ = try await device.session.open(accountKey: try XCTUnwrap(kept))
        let item = try await device.session.item(id: saved.id)
        XCTAssertEqual(item.fields.withAccess(nil), loginFields())

        // A different key, or no copy at all, asks for the passphrase.
        await device.session.lock()
        do {
            _ = try await device.session.open(accountKey: PasswordsCrypto.makeKey())
            XCTFail("a wrong key should not open")
        } catch {
            XCTAssertEqual(error as? PasswordsError, .needsPassphrase)
        }
        let strangerSession = PasswordsSession(api: FakePasswordsAPI(server: server, userId: "user-ada"), cache: makeCache(), userId: "user-ada")
        do {
            _ = try await strangerSession.open(accountKey: unlocked.accountKey)
            XCTFail("with no copy there is nothing to open")
        } catch {
            XCTAssertEqual(error as? PasswordsError, .needsPassphrase)
        }
    }

    func testTheOpenerReadsTheCacheWithoutASession() async throws {
        let server = await makeServer()
        let device = makeDevice(server)
        _ = try await device.session.setUp(passphrase: "a long invented passphrase", iterationCount: iterationCount)
        let saved = try await device.session.save(fields: loginFields())
        let unlocked = try await device.session.unlock(passphrase: "a long invented passphrase")

        // What the AutoFill extension does: the file, the account key, nothing else.
        let contents = try XCTUnwrap(try device.cache.load())
        let opener = try PasswordsOpener(accountKey: unlocked.accountKey, contents: contents)
        let opened = opener.openAll(contents.items)
        XCTAssertEqual(opened.items.map(\.id), [saved.id])
        XCTAssertEqual(opened.items.first?.fields.withAccess(nil), loginFields())
        XCTAssertEqual(opened.unreadableItemIds, [])
        XCTAssertThrowsError(try PasswordsOpener(accountKey: PasswordsCrypto.makeKey(), contents: contents))
    }

    // MARK: Saving

    func testSaveThenSyncOnAnotherDeviceAndAnEditKeepsTheItemKey() async throws {
        let server = await makeServer()
        let first = makeDevice(server)
        _ = try await first.session.setUp(passphrase: "a long invented passphrase", iterationCount: iterationCount)
        let created = try await first.session.save(fields: loginFields())
        XCTAssertFalse(created.isSharedWithMe)
        XCTAssertEqual(created.id, created.id.lowercased())
        let storedBefore = await server.item(created.id)

        let edited = try await first.session.save(fields: loginFields(password: "new password"), id: created.id)
        XCTAssertGreaterThan(edited.revision, created.revision)
        let storedAfter = await server.item(created.id)
        XCTAssertEqual(storedBefore?.itemKeyByAccountKey, storedAfter?.itemKeyByAccountKey)

        let second = makeDevice(server)
        _ = try await second.session.unlock(passphrase: "a long invented passphrase")
        let result = try await second.session.sync()
        XCTAssertEqual(result.changedItemIds, [created.id])
        let list = try await second.session.list()
        XCTAssertEqual(list.items.count, 1)
        XCTAssertEqual(list.items.first?.fields.withAccess(nil), loginFields(password: "new password"))
    }

    func testASecondSyncAsksOnlyForWhatChanged() async throws {
        let server = await makeServer()
        let first = makeDevice(server)
        _ = try await first.session.setUp(passphrase: "a long invented passphrase", iterationCount: iterationCount)
        let created = try await first.session.save(fields: loginFields())
        try await first.session.sync()
        _ = try await first.session.save(fields: .note(PasswordsNoteFields(name: "Router", text: "invented")))
        let result = try await first.session.sync()
        XCTAssertEqual(result.changedItemIds.count, 1)
        XCTAssertNotEqual(result.changedItemIds.first, created.id)
        let calls = await server.listCalls
        XCTAssertEqual(calls.count, 2)
        XCTAssertEqual(calls[0], 0)
        XCTAssertEqual(calls[1], created.revision)
        let list = try await first.session.list()
        XCTAssertEqual(list.items.map(\.name), ["Example", "Router"])
    }

    func testASyncAsksForEverythingOnceWhenTheServerListsAnItemTheCopyMissed() async throws {
        let server = await makeServer()
        let first = makeDevice(server)
        _ = try await first.session.setUp(passphrase: "a long invented passphrase", iterationCount: iterationCount)
        _ = try await first.session.save(fields: loginFields())
        let second = makeDevice(server)
        _ = try await second.session.unlock(passphrase: "a long invented passphrase")
        try await second.session.sync()
        // An item the second copy has not seen, numbered below the copy's revision.
        let late = try await first.session.save(fields: loginFields(name: "Late"))
        await server.setRevision(itemId: late.id, revision: 1)
        let result = try await second.session.sync()
        XCTAssertTrue(result.changedItemIds.contains(late.id))
        let calls = await server.listCalls
        let expectedCalls: [Int64?] = [0, 1, nil]
        XCTAssertEqual(calls, expectedCalls)
        let list = try await second.session.list()
        XCTAssertEqual(list.items.map(\.name), ["Example", "Late"])
        // Nothing is missing now, so the next sync is one incremental read again.
        try await second.session.sync()
        let afterwards = await server.listCalls
        let expectedAfterwards: [Int64?] = [0, 1, nil, 1]
        XCTAssertEqual(afterwards, expectedAfterwards)
    }

    func testAStaleBaseRevisionIsATypedError() async throws {
        let server = await makeServer()
        let first = makeDevice(server)
        _ = try await first.session.setUp(passphrase: "a long invented passphrase", iterationCount: iterationCount)
        let created = try await first.session.save(fields: loginFields())

        let second = makeDevice(server)
        _ = try await second.session.unlock(passphrase: "a long invented passphrase")
        try await second.session.sync()

        _ = try await first.session.save(fields: loginFields(password: "changed on the first device"), id: created.id)
        do {
            _ = try await second.session.save(fields: loginFields(password: "changed on the second"), id: created.id, baseRevision: created.revision)
            XCTFail("the second device based its change on an old revision")
        } catch {
            XCTAssertEqual(error as? PasswordsError, .itemChangedOnAnotherDevice)
        }
        // After reading it again the change goes through.
        try await second.session.sync()
        let current = try await second.session.item(id: created.id)
        _ = try await second.session.save(fields: loginFields(password: "changed on the second"), id: created.id, baseRevision: current.revision)
    }

    func testAnItemCannotChangeKindAndASessionThatIsLockedRefuses() async throws {
        let server = await makeServer()
        let device = makeDevice(server)
        _ = try await device.session.setUp(passphrase: "a long invented passphrase", iterationCount: iterationCount)
        let created = try await device.session.save(fields: loginFields())
        do {
            _ = try await device.session.save(fields: .note(PasswordsNoteFields(name: "x")), id: created.id)
            XCTFail("a login cannot become a note")
        } catch {
            XCTAssertEqual(error as? PasswordsError, .itemKindCannotChange)
        }
        await device.session.lock()
        do {
            _ = try await device.session.save(fields: loginFields())
            XCTFail("locked")
        } catch {
            XCTAssertEqual(error as? PasswordsError, .locked)
        }
    }

    func testDeleteAndRestore() async throws {
        let server = await makeServer()
        let device = makeDevice(server)
        _ = try await device.session.setUp(passphrase: "a long invented passphrase", iterationCount: iterationCount)
        let created = try await device.session.save(fields: loginFields())
        let deleted = try await device.session.delete(id: created.id)
        XCTAssertTrue(deleted.isDeleted)
        let listedDeleted = try await device.session.list()
        XCTAssertEqual(listedDeleted.items.first?.isDeleted, true)
        let restored = try await device.session.restore(id: created.id)
        XCTAssertFalse(restored.isDeleted)
        XCTAssertGreaterThan(restored.revision, deleted.revision)
    }

    // MARK: Sharing

    private func makeSharingWorld() async throws -> (
        server: FakePasswordsServer, ada: (session: PasswordsSession, cache: PasswordsCache),
        grace: (session: PasswordsSession, cache: PasswordsCache), itemId: String
    ) {
        let server = await makeServer(people: [("user-ada", "Ada"), ("user-grace", "Grace")])
        await server.setCircle("circle-one", members: ["user-ada", "user-grace"])
        let ada = makeDevice(server, userId: "user-ada")
        let grace = makeDevice(server, userId: "user-grace")
        _ = try await ada.session.setUp(passphrase: "ada invented passphrase", iterationCount: iterationCount)
        _ = try await grace.session.setUp(passphrase: "grace invented passphrase", iterationCount: iterationCount)
        let item = try await ada.session.save(fields: loginFields())
        return (server, ada, grace, item.id)
    }

    func testSharingSealsTheKeyForTheRecipientWhoOpensItWithTheirOwnKeyPair() async throws {
        let world = try await makeSharingWorld()
        let recipients = try await world.ada.session.recipients(granteeCircleId: "circle-one")
        XCTAssertEqual(recipients.map(\.userId), ["user-grace"])
        let grace = try XCTUnwrap(recipients.first)
        XCTAssertEqual(grace.safetyCode?.split(separator: " ").count, 8)
        XCTAssertFalse(grace.hasKey)
        XCTAssertEqual(grace.keyTrust, .new)

        let shared = try await world.ada.session.setShares(
            itemId: world.itemId, grantees: [PasswordShareGranteeInput(granteeCircleId: "circle-one")], recipients: recipients)
        XCTAssertEqual(shared.shares.count, 1)
        XCTAssertEqual(shared.shares.first?.recipients.map(\.hasKey), [true])

        let result = try await world.grace.session.sync()
        XCTAssertEqual(result.changedItemIds, [world.itemId])
        let opened = try await world.grace.session.item(id: world.itemId)
        XCTAssertTrue(opened.isSharedWithMe)
        XCTAssertEqual(opened.ownerName, "Ada")
        XCTAssertEqual(opened.fields.withAccess(nil), loginFields())
        XCTAssertEqual(opened.shares.count, 0)

        // A recipient cannot change it.
        do {
            _ = try await world.grace.session.save(fields: loginFields(password: "graces change"), id: world.itemId)
            XCTFail("only the owner changes an item")
        } catch {
            XCTAssertEqual(error as? PasswordsError, .onlyOwnerCanChangeItem)
        }
        // The owner's later edit reaches the recipient with the same item key.
        _ = try await world.ada.session.save(fields: loginFields(password: "new password"), id: world.itemId)
        try await world.grace.session.sync()
        let updated = try await world.grace.session.item(id: world.itemId)
        XCTAssertEqual(updated.fields.withAccess(nil), loginFields(password: "new password"))
    }

    func testUnsharingDropsTheItemFromTheRecipientsCopy() async throws {
        let world = try await makeSharingWorld()
        let recipients = try await world.ada.session.recipients(granteeUserId: "user-grace")
        _ = try await world.ada.session.setShares(
            itemId: world.itemId, grantees: [PasswordShareGranteeInput(granteeUserId: "user-grace")], recipients: recipients)
        try await world.grace.session.sync()
        let before = try await world.grace.session.list()
        XCTAssertEqual(before.items.count, 1)

        _ = try await world.ada.session.setShares(itemId: world.itemId, grantees: [], recipients: [])
        let result = try await world.grace.session.sync()
        XCTAssertEqual(result.removedItemIds, [world.itemId])
        let after = try await world.grace.session.list()
        XCTAssertEqual(after.items.count, 0)
    }

    func testARecipientWhoSetsUpLaterIsSealedForAtOnceWhenTheOwnersDeviceCompletesTheWaitingShares() async throws {
        let server = await makeServer(people: [("user-ada", "Ada"), ("user-grace", "Grace")])
        await server.setCircle("circle-one", members: ["user-ada", "user-grace"])
        let ada = makeDevice(server, userId: "user-ada")
        _ = try await ada.session.setUp(passphrase: "ada invented passphrase", iterationCount: iterationCount)
        let item = try await ada.session.save(fields: loginFields())

        // Grace has not set up Passwords: she has no key, so the share is made without one.
        let recipients = try await ada.session.recipients(granteeCircleId: "circle-one")
        XCTAssertNil(recipients.first?.publicKey)
        XCTAssertNil(recipients.first?.safetyCode)
        _ = try await ada.session.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeCircleId: "circle-one")], recipients: recipients)
        let noneYet = try await ada.session.waitingShares()
        XCTAssertEqual(noneYet.count, 0)

        let grace = makeDevice(server, userId: "user-grace")
        _ = try await grace.session.setUp(passphrase: "grace invented passphrase", iterationCount: iterationCount)
        let waiting = try await ada.session.waitingShares()
        XCTAssertEqual(waiting.map(\.itemId), [item.id])
        XCTAssertEqual(waiting.first?.itemName, "Example")
        XCTAssertEqual(waiting.first?.recipient.userId, "user-grace")
        XCTAssertEqual(waiting.first?.recipient.keyTrust, .new)

        // Ada's device has never seen Grace's key, and seals for it at once without asking.
        let completed = try await ada.session.completeWaitingShares()
        XCTAssertEqual(completed.addedKeyCount, 1)
        let stored = await server.item(item.id)
        XCTAssertNotNil(stored?.keys["user-grace"])
        let graceKey = await server.publicKey(of: "user-grace")
        XCTAssertEqual(try XCTUnwrap(try ada.cache.load()).knownPublicKeys, ["user-grace": graceKey])
        let stillWaiting = try await ada.session.waitingShares()
        XCTAssertEqual(stillWaiting.count, 0)
        try await grace.session.sync()
        let opened = try await grace.session.item(id: item.id)
        XCTAssertEqual(opened.fields.withAccess(nil), loginFields())
    }

    // MARK: Known keys

    private func grantGrace(_ world: (server: FakePasswordsServer, ada: (session: PasswordsSession, cache: PasswordsCache), grace: (session: PasswordsSession, cache: PasswordsCache), itemId: String)) async throws -> PasswordItem {
        let recipients = try await world.ada.session.recipients(granteeUserId: "user-grace")
        return try await world.ada.session.setShares(
            itemId: world.itemId, grantees: [PasswordShareGranteeInput(granteeUserId: "user-grace")], recipients: recipients)
    }

    func testSharingWithAFirstSeenPersonSealsAtOnceAndRecordsTheirKeyAsKnown() async throws {
        let world = try await makeSharingWorld()
        let recipients = try await world.ada.session.recipients(granteeUserId: "user-grace")
        XCTAssertEqual(recipients.first?.keyTrust, .new)
        XCTAssertEqual(recipients.first?.isCompared, false)

        _ = try await world.ada.session.setShares(
            itemId: world.itemId, grantees: [PasswordShareGranteeInput(granteeUserId: "user-grace")], recipients: recipients)
        let stored = await world.server.item(world.itemId)
        XCTAssertNotNil(stored?.keys["user-grace"])
        let graceKey = try XCTUnwrap(recipients.first?.publicKey).base64EncodedString()
        let contents = try XCTUnwrap(try world.ada.cache.load())
        XCTAssertEqual(contents.knownPublicKeys, ["user-grace": graceKey])
        XCTAssertEqual(contents.changedPublicKeys, [:])
        let noticeCount = await world.ada.session.keyChanges().count
        XCTAssertEqual(noticeCount, 0)
        let again = try await world.ada.session.recipients(granteeUserId: "user-grace")
        XCTAssertEqual(again.first?.keyTrust, .known)

        // Grace opens it without anything having been confirmed.
        try await world.grace.session.sync()
        let opened = try await world.grace.session.item(id: world.itemId)
        XCTAssertEqual(opened.fields.withAccess(nil), loginFields())
    }

    func testAKnownRecipientIsSealedOnAnotherItemAndExistingRecipientsAreNotSealedAgain() async throws {
        let world = try await makeSharingWorld()
        _ = try await grantGrace(world)
        let first = await world.server.item(world.itemId)
        let firstKey = try XCTUnwrap(first?.keys["user-grace"]?.itemKeyByRecipientKey)

        // A second item: Grace's key is known now, and it is sealed for.
        let second = try await world.ada.session.save(fields: loginFields(name: "Second"))
        let recipients = try await world.ada.session.recipients(granteeUserId: "user-grace")
        XCTAssertEqual(recipients.first?.keyTrust, .known)
        _ = try await world.ada.session.setShares(
            itemId: second.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-grace")], recipients: recipients)
        let storedSecond = await world.server.item(second.id)
        XCTAssertNotNil(storedSecond?.keys["user-grace"])

        // Sharing the first item again (a second grantee) does not seal Grace's key again.
        await world.server.addPerson(userId: "user-linus", name: "Linus")
        _ = try await world.ada.session.setShares(
            itemId: world.itemId,
            grantees: [PasswordShareGranteeInput(granteeUserId: "user-grace"), PasswordShareGranteeInput(granteeUserId: "user-linus")],
            recipients: recipients)
        let after = await world.server.item(world.itemId)
        XCTAssertEqual(after?.keys["user-grace"]?.itemKeyByRecipientKey, firstKey)
    }

    func testAChangedRecipientKeyIsSealedForAtOnceAndLeavesANoticeThatCanBeDismissed() async throws {
        let world = try await makeSharingWorld()
        _ = try await grantGrace(world)
        let oldKey = await world.server.publicKey(of: "user-grace")
        let newKeyPair = PasswordsCrypto.makeKeyPair()
        let newKey = newKeyPair.publicKey.base64EncodedString()
        await world.server.replacePublicKey(userId: "user-grace", publicKey: newKey)

        let recipients = try await world.ada.session.recipients(granteeUserId: "user-grace")
        let grace = try XCTUnwrap(recipients.first)
        XCTAssertEqual(grace.keyTrust, .changed)
        XCTAssertEqual(grace.knownSafetyCode, PasswordsOpener.safetyCodeText(publicKey: oldKey))
        XCTAssertEqual(grace.safetyCode, try PasswordsCrypto.safetyCode(publicKey: newKeyPair.publicKey))
        XCTAssertNotEqual(grace.knownSafetyCode, grace.safetyCode)

        // Nothing is asked: on an item Grace has no key for yet, the new key is sealed for and recorded, with a notice.
        let second = try await world.ada.session.save(fields: loginFields(name: "Second"))
        _ = try await world.ada.session.setShares(
            itemId: second.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-grace")], recipients: recipients)
        let stored = await world.server.item(second.id)
        XCTAssertEqual(stored?.keys["user-grace"]?.recipientPublicKey, newKey)
        let contents = try XCTUnwrap(try world.ada.cache.load())
        XCTAssertEqual(contents.knownPublicKeys["user-grace"], newKey)
        XCTAssertEqual(contents.changedPublicKeys["user-grace"], oldKey)
        let notices = await world.ada.session.keyChanges()
        XCTAssertEqual(
            notices,
            [
                PasswordsKeyChange(
                    userId: "user-grace", name: "Grace", oldSafetyCode: PasswordsOpener.safetyCodeText(publicKey: oldKey),
                    newSafetyCode: try PasswordsCrypto.safetyCode(publicKey: newKeyPair.publicKey), newPublicKey: newKey)
            ])

        // The first item, sealed for her old key, is waiting for the new one: completing seals it at once.
        let waiting = try await world.ada.session.waitingShares()
        XCTAssertEqual(waiting.map(\.itemId), [world.itemId])
        let completed = try await world.ada.session.completeWaitingShares()
        XCTAssertEqual(completed.addedKeyCount, 1)
        let firstStored = await world.server.item(world.itemId)
        XCTAssertEqual(firstStored?.keys["user-grace"]?.recipientPublicKey, newKey)

        // The notice stays until it is dismissed, and dismissing it does not undo the new key.
        let stillThere = await world.ada.session.keyChanges()
        XCTAssertEqual(stillThere.count, 1)
        try await world.ada.session.dismissKeyChange(userId: "user-grace")
        let dismissed = await world.ada.session.keyChanges()
        XCTAssertEqual(dismissed, [])
        XCTAssertEqual(try XCTUnwrap(try world.ada.cache.load()).knownPublicKeys["user-grace"], newKey)
    }

    func testComparingCodesMarksTheKeyComparedAndGatesNothing() async throws {
        let world = try await makeSharingWorld()
        _ = try await grantGrace(world)
        let graceKey = await world.server.publicKey(of: "user-grace")
        let before = try await world.ada.session.recipients(granteeUserId: "user-grace")
        XCTAssertEqual(before.first?.isCompared, false)

        try await world.ada.session.markCompared(userId: "user-grace", publicKey: graceKey)
        let after = try await world.ada.session.recipients(granteeUserId: "user-grace")
        XCTAssertEqual(after.first?.keyTrust, .known)
        XCTAssertEqual(after.first?.isCompared, true)

        // A new key is accepted as before, and it is no longer shown as compared.
        let newKey = PasswordsCrypto.makeKeyPair().publicKey.base64EncodedString()
        await world.server.replacePublicKey(userId: "user-grace", publicKey: newKey)
        let changed = try await world.ada.session.recipients(granteeUserId: "user-grace")
        XCTAssertEqual(changed.first?.keyTrust, .changed)
        XCTAssertEqual(changed.first?.isCompared, false)
        let second = try await world.ada.session.save(fields: loginFields(name: "Second"))
        _ = try await world.ada.session.setShares(
            itemId: second.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-grace")], recipients: changed)
        let stored = await world.server.item(second.id)
        XCTAssertEqual(stored?.keys["user-grace"]?.recipientPublicKey, newKey)
        let notices = await world.ada.session.keyChanges()
        XCTAssertEqual(notices.map(\.userId), ["user-grace"])

        // Comparing the new codes clears the notice and shows "compared" again.
        try await world.ada.session.markCompared(userId: "user-grace", publicKey: newKey)
        let cleared = await world.ada.session.keyChanges()
        XCTAssertEqual(cleared, [])
        let compared = try await world.ada.session.recipients(granteeUserId: "user-grace")
        XCTAssertEqual(compared.first?.isCompared, true)
    }

    func testCompletingWaitingSharesSealsEveryItemOfARecipientWithNoConfirmation() async throws {
        let server = await makeServer(people: [("user-ada", "Ada"), ("user-grace", "Grace")])
        await server.setCircle("circle-one", members: ["user-ada", "user-grace"])
        let ada = makeDevice(server, userId: "user-ada")
        _ = try await ada.session.setUp(passphrase: "ada invented passphrase", iterationCount: iterationCount)
        let first = try await ada.session.save(fields: loginFields(name: "First"))
        let second = try await ada.session.save(fields: loginFields(name: "Second"))
        let grantees = [PasswordShareGranteeInput(granteeCircleId: "circle-one")]
        _ = try await ada.session.setShares(itemId: first.id, grantees: grantees, recipients: [])
        _ = try await ada.session.setShares(itemId: second.id, grantees: grantees, recipients: [])
        let grace = makeDevice(server, userId: "user-grace")
        _ = try await grace.session.setUp(passphrase: "grace invented passphrase", iterationCount: iterationCount)

        let completed = try await ada.session.completeWaitingShares()
        XCTAssertEqual(completed.addedKeyCount, 2)
        try await grace.session.sync()
        let opened = try await grace.session.list()
        XCTAssertEqual(opened.items.map(\.name), ["First", "Second"])
    }

    func testWaitingSharesOfKnownKeysAreSealedByCompleteWaitingShares() async throws {
        let world = try await makeSharingWorld()
        _ = try await grantGrace(world)
        // Another item is shared with Grace while her key is known but before the key is sealed for it:
        // the share is made without a key because Ada's call listed no recipients.
        let second = try await world.ada.session.save(fields: loginFields(name: "Second"))
        _ = try await world.ada.session.setShares(
            itemId: second.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-grace")], recipients: [])
        let completed = try await world.ada.session.completeWaitingShares()
        XCTAssertEqual(completed.addedKeyCount, 1)
        try await world.grace.session.sync()
        let opened = try await world.grace.session.item(id: second.id)
        XCTAssertEqual(opened.fields.withAccess(nil), loginFields(name: "Second"))
    }

    func testARecipientRecordsTheOwnersKeyOnFirstSightWithNoNotice() async throws {
        let world = try await makeSharingWorld()
        _ = try await grantGrace(world)
        try await world.grace.session.sync()
        let adaKey = await world.server.publicKey(of: "user-ada")
        // Recorded in the pin store (the AutoFill extension reads it there).
        let contents = try XCTUnwrap(try world.grace.cache.load())
        XCTAssertEqual(contents.knownPublicKeys, ["user-ada": adaKey])
        XCTAssertEqual(contents.changedPublicKeys, [:])
        let noticeCount = await world.grace.session.keyChanges().count
        XCTAssertEqual(noticeCount, 0)
        let opened = try await world.grace.session.item(id: world.itemId)
        XCTAssertEqual(opened.fields.withAccess(nil), loginFields())
    }

    func testAChangedOwnerKeyIsAcceptedTheItemKeepsOpeningAndANoticeStaysUntilDismissed() async throws {
        let world = try await makeSharingWorld()
        _ = try await grantGrace(world)
        try await world.grace.session.sync()
        let oldKey = await world.server.publicKey(of: "user-ada")

        // Ada's key changes, and her new key seals the item key again for Grace.
        let adaUnlocked = try await makeDevice(world.server, userId: "user-ada").session.unlock(passphrase: "ada invented passphrase")
        let storedItemOptional = await world.server.item(world.itemId)
        let storedItem = try XCTUnwrap(storedItemOptional)
        let itemKey = try PasswordsCrypto.open(
            try PasswordsSealedValue(jsonText: storedItem.itemKeyByAccountKey), key: adaUnlocked.accountKey,
            associatedData: PasswordsCrypto.itemKeyAssociatedData(itemId: world.itemId))
        let newOwner = PasswordsCrypto.makeKeyPair()
        let newOwnerKey = newOwner.publicKey.base64EncodedString()
        let gracePublicKeyText = await world.server.publicKey(of: "user-grace")
        let gracePublicKey = try XCTUnwrap(Data(base64Encoded: gracePublicKeyText))
        let resealed = try PasswordsCrypto.sealForKeyPair(
            itemKey, ownerKeyPair: newOwner, ownerId: "user-ada", recipientPublicKey: gracePublicKey, recipientId: "user-grace",
            itemId: world.itemId)
        await world.server.replacePublicKey(userId: "user-ada", publicKey: newOwnerKey)
        await world.server.replaceShareKey(itemId: world.itemId, recipientUserId: "user-grace", itemKeyByRecipientKey: resealed.jsonText)
        try await world.grace.session.sync()

        // The item opens with the new key, nothing waits on the person, and the notice carries both codes.
        let listed = try await world.grace.session.list()
        XCTAssertEqual(listed.items.map(\.id), [world.itemId])
        XCTAssertEqual(listed.unreadableItemIds, [])
        let opened = try await world.grace.session.item(id: world.itemId)
        XCTAssertEqual(opened.fields.withAccess(nil), loginFields())
        let notices = await world.grace.session.keyChanges()
        XCTAssertEqual(
            notices,
            [
                PasswordsKeyChange(
                    userId: "user-ada", name: "Ada", oldSafetyCode: PasswordsOpener.safetyCodeText(publicKey: oldKey),
                    newSafetyCode: try PasswordsCrypto.safetyCode(publicKey: newOwner.publicKey), newPublicKey: newOwnerKey)
            ])
        // The file holds the new key, which the AutoFill extension reads.
        XCTAssertEqual(try XCTUnwrap(try world.grace.cache.load()).knownPublicKeys["user-ada"], newOwnerKey)

        // A later sync and a relaunch keep the notice; dismissing it removes only the notice.
        try await world.grace.session.sync()
        let afterSync = await world.grace.session.keyChanges()
        XCTAssertEqual(afterSync.count, 1)
        try await world.grace.session.dismissKeyChange(userId: "user-ada")
        let dismissed = await world.grace.session.keyChanges()
        XCTAssertEqual(dismissed, [])
        let stillOpens = try await world.grace.session.item(id: world.itemId)
        XCTAssertEqual(stillOpens.fields.withAccess(nil), loginFields())
    }

    func testAnItemSealedByTheServerForAKeyItCallsTheOwnersDoesNotOpen() async throws {
        let world = try await makeSharingWorld()
        _ = try await grantGrace(world)
        // The server seals a made-up item key as if Ada had, with its own key pair standing in for the owner's.
        let forged = PasswordsCrypto.makeKeyPair()
        let gracePublicKeyText = await world.server.publicKey(of: "user-grace")
        let gracePublicKey = try XCTUnwrap(Data(base64Encoded: gracePublicKeyText))
        let sealed = try PasswordsCrypto.sealForKeyPair(
            PasswordsCrypto.makeKey(), ownerKeyPair: forged, ownerId: "user-ada", recipientPublicKey: gracePublicKey,
            recipientId: "user-grace", itemId: world.itemId)
        await world.server.replaceShareKey(itemId: world.itemId, recipientUserId: "user-grace", itemKeyByRecipientKey: sealed.jsonText)
        try await world.grace.session.sync()
        let listed = try await world.grace.session.list()
        XCTAssertEqual(listed.items.count, 0)
        XCTAssertEqual(listed.unreadableItemIds, [world.itemId])
        // A key that opens nothing is not recorded as a change.
        let noticeCount = await world.grace.session.keyChanges().count
        XCTAssertEqual(noticeCount, 0)
    }

    func testTheOpenerForTheAutoFillExtensionOpensSharedItemsWhateverKeysItKnows() async throws {
        let world = try await makeSharingWorld()
        _ = try await grantGrace(world)
        let adaUnlocked = try await world.grace.session.unlock(passphrase: "grace invented passphrase")
        try await world.grace.session.sync()
        var contents = try XCTUnwrap(try world.grace.cache.load())
        let view = try XCTUnwrap(contents.item(id: world.itemId))

        let knowing = try PasswordsOpener(accountKey: adaUnlocked.accountKey, contents: contents)
        XCTAssertEqual(try knowing.open(view).fields.withAccess(nil), loginFields())

        contents.knownPublicKeys = [:]
        let unaware = try PasswordsOpener(accountKey: adaUnlocked.accountKey, contents: contents)
        XCTAssertEqual(try unaware.open(view).fields.withAccess(nil), loginFields())

        contents.knownPublicKeys = ["user-ada": PasswordsCrypto.makeKeyPair().publicKey.base64EncodedString()]
        let mistaken = try PasswordsOpener(accountKey: adaUnlocked.accountKey, contents: contents)
        XCTAssertEqual(mistaken.openAll([view]).items.map(\.id), [world.itemId])
    }

    // MARK: The agent

    func testTheAgentSealOpensWithTheAgentPrivateKeyAndOnlyThatItemKeyIsInIt() async throws {
        let server = await makeServer()
        let device = makeDevice(server)
        _ = try await device.session.setUp(passphrase: "a long invented passphrase", iterationCount: iterationCount)
        let login = try await device.session.save(fields: loginFields())
        let note = try await device.session.save(fields: .note(PasswordsNoteFields(name: "Router", text: "invented")))

        let allowed = try await device.session.setUsableByAgent(itemId: login.id, isUsable: true)
        XCTAssertTrue(allowed.isUsableByAgent)

        let storedOptional = await server.item(login.id)
        let stored = try XCTUnwrap(storedOptional)
        let sealedForAgent = try PasswordsKeyPairSealedValue(jsonText: try XCTUnwrap(stored.itemKeyByAgentKey))
        let agentPrivateKey = await server.agentKeyPair.privateKey
        // The agent opens it with the owner's public key and user id: the person's own key pair sealed it.
        let ownerPublicKeyText = await server.publicKey(of: "user-ada")
        let ownerPublicKey = try XCTUnwrap(Data(base64Encoded: ownerPublicKeyText))
        let itemKey = try PasswordsCrypto.openForKeyPair(
            sealedForAgent, recipientPrivateKey: agentPrivateKey, ownerPublicKey: ownerPublicKey, ownerId: "user-ada",
            recipientId: PasswordsCrypto.agentRecipientId, itemId: login.id)
        let fields = try PasswordsCrypto.openItemFields(
            try PasswordsSealedValue(jsonText: stored.encryptedFields), itemKey: itemKey, itemId: login.id, itemKind: .login)
        XCTAssertEqual(fields.withAccess(nil), loginFields())
        XCTAssertEqual(fields.access?.isUsableByAgent, true)
        // The agent's key cannot open an item that was not allowed.
        let storedNoteOptional = await server.item(note.id)
        let storedNote = try XCTUnwrap(storedNoteOptional)
        XCTAssertNil(storedNote.itemKeyByAgentKey)

        let stopped = try await device.session.setUsableByAgent(itemId: login.id, isUsable: false)
        XCTAssertFalse(stopped.isUsableByAgent)
        let storedAfterOptional = await server.item(login.id)
        let storedAfter = try XCTUnwrap(storedAfterOptional)
        XCTAssertNil(storedAfter.itemKeyByAgentKey)
    }

    func testACardCannotBeUsedByTheAgent() async throws {
        let server = await makeServer()
        let device = makeDevice(server)
        _ = try await device.session.setUp(passphrase: "a long invented passphrase", iterationCount: iterationCount)
        let card = try await device.session.save(fields: .card(PasswordsCardFields(name: "Invented card", number: "4000 0000 0000 0000")))
        do {
            _ = try await device.session.setUsableByAgent(itemId: card.id, isUsable: true)
            XCTFail("a card is never the agent's")
        } catch {
            XCTAssertEqual(error as? PasswordsError, .cardCannotBeUsedByAgent)
        }
    }

    func testADeletedItemCannotBeGivenToTheAgentOrTakenBack() async throws {
        let server = await makeServer()
        let device = makeDevice(server)
        _ = try await device.session.setUp(passphrase: "a long invented passphrase", iterationCount: iterationCount)
        let login = try await device.session.save(fields: loginFields())
        _ = try await device.session.delete(id: login.id)
        for isUsable in [true, false] {
            do {
                _ = try await device.session.setUsableByAgent(itemId: login.id, isUsable: isUsable)
                XCTFail("a deleted item is not the agent's to use")
            } catch {
                XCTAssertEqual(error as? PasswordsError, .itemNotFound)
            }
        }
    }

    // MARK: Settings

    func testChangingThePassphraseKeepsTheItemsAndTheRecoveryKey() async throws {
        let server = await makeServer()
        let first = makeDevice(server)
        let recoveryKey = try await first.session.setUp(passphrase: "the old passphrase", iterationCount: iterationCount)
        let saved = try await first.session.save(fields: loginFields())
        try await first.session.changePassphrase(
            currentPassphrase: "the old passphrase", newPassphrase: "the new passphrase", iterationCount: iterationCount)

        let second = makeDevice(server)
        do {
            _ = try await second.session.unlock(passphrase: "the old passphrase")
            XCTFail("the old passphrase should be refused")
        } catch {
            XCTAssertEqual(error as? PasswordsError, .wrongPassphrase)
        }
        _ = try await second.session.unlock(passphrase: "the new passphrase")
        try await second.session.sync()
        let opened = try await second.session.item(id: saved.id)
        XCTAssertEqual(opened.fields.withAccess(nil), loginFields())
        _ = try await makeDevice(server).session.unlock(recoveryKey: recoveryKey)
    }

    func testChangingThePassphraseWithTheRecoveryKey() async throws {
        let server = await makeServer()
        let first = makeDevice(server)
        let recoveryKey = try await first.session.setUp(passphrase: "forgotten passphrase", iterationCount: iterationCount)
        let second = makeDevice(server)
        _ = try await second.session.unlock(recoveryKey: recoveryKey)
        try await second.session.changePassphrase(recoveryKey: recoveryKey, newPassphrase: "a fresh passphrase", iterationCount: iterationCount)
        _ = try await makeDevice(server).session.unlock(passphrase: "a fresh passphrase")
    }

    func testReplacingTheRecoveryKeyStopsTheOldOne() async throws {
        let server = await makeServer()
        let device = makeDevice(server)
        let oldKey = try await device.session.setUp(passphrase: "the passphrase", iterationCount: iterationCount)
        let newKey = try await device.session.replaceRecoveryKey(passphrase: "the passphrase")
        XCTAssertNotEqual(oldKey, newKey)
        do {
            _ = try await makeDevice(server).session.unlock(recoveryKey: oldKey)
            XCTFail("the old recovery key should be refused")
        } catch {
            XCTAssertEqual(error as? PasswordsError, .wrongRecoveryKey)
        }
        _ = try await makeDevice(server).session.unlock(recoveryKey: newKey)
    }

    func testTheLockTimeIsKeptOnTheServerAndInTheCache() async throws {
        let server = await makeServer()
        let device = makeDevice(server)
        _ = try await device.session.setUp(passphrase: "the passphrase", iterationCount: iterationCount)
        let kept = try await device.session.updateLockAfterMinutes(60)
        XCTAssertEqual(kept, 60)
        let lockAfterMinutes = await device.session.lockAfterMinutes
        XCTAssertEqual(lockAfterMinutes, 60)
        XCTAssertEqual(try device.cache.load()?.lockAfterMinutes, 60)
        let isAskRequiredSoon = await device.session.isAskRequired(now: Date().addingTimeInterval(59 * 60))
        XCTAssertFalse(isAskRequiredSoon)
        let isAskRequiredLater = await device.session.isAskRequired(now: Date().addingTimeInterval(61 * 60))
        XCTAssertTrue(isAskRequiredLater)
    }

    func testNotingUseMovesTheLockTimeOnAndDoesNothingWhileLocked() async throws {
        let server = await makeServer()
        let device = makeDevice(server)
        await device.session.noteUse()
        let lastUsedBefore = await device.session.lastUsedAt
        XCTAssertNil(lastUsedBefore)
        _ = try await device.session.setUp(passphrase: "the passphrase", iterationCount: iterationCount)
        let firstUseOrNil = await device.session.lastUsedAt
        let firstUse = try XCTUnwrap(firstUseOrNil)
        try await Task.sleep(nanoseconds: 20_000_000)
        await device.session.noteUse()
        let secondUseOrNil = await device.session.lastUsedAt
        let secondUse = try XCTUnwrap(secondUseOrNil)
        XCTAssertGreaterThan(secondUse, firstUse)
        await device.session.lock()
        await device.session.noteUse()
        let lastUsedAfter = await device.session.lastUsedAt
        XCTAssertNil(lastUsedAfter)
    }

    func testErasingRemovesTheServersCopyAndThePhones() async throws {
        let server = await makeServer()
        let device = makeDevice(server)
        _ = try await device.session.setUp(passphrase: "the passphrase", iterationCount: iterationCount)
        _ = try await device.session.save(fields: loginFields())
        try await device.session.erase()
        let isUnlocked = await device.session.isUnlocked
        XCTAssertFalse(isUnlocked)
        XCTAssertNil(try device.cache.load())
        let setup = try await device.session.readSetup()
        XCTAssertFalse(setup.isSetUp)
    }

    func testASyncAfterPasswordsWasErasedAndSetUpAgainElsewhereAsksForThePassphrase() async throws {
        let server = await makeServer()
        let phone = makeDevice(server)
        _ = try await phone.session.setUp(passphrase: "the passphrase", iterationCount: iterationCount)
        let other = makeDevice(server)
        try await other.session.erase()
        _ = try await other.session.setUp(passphrase: "a new passphrase", iterationCount: iterationCount)
        do {
            try await phone.session.sync()
            XCTFail("the phone's copy belongs to the old setup")
        } catch {
            XCTAssertEqual(error as? PasswordsError, .needsPassphrase)
        }
        XCTAssertNil(try phone.cache.load())
    }

    func testIdentityRecordsComeFromTheOpenedLogins() async throws {
        let server = await makeServer()
        let device = makeDevice(server)
        _ = try await device.session.setUp(passphrase: "the passphrase", iterationCount: iterationCount)
        let login = try await device.session.save(fields: loginFields())
        _ = try await device.session.save(fields: .note(PasswordsNoteFields(name: "Router", text: "invented")))
        let records = try await device.session.identityRecords()
        XCTAssertEqual(
            records.passwordIdentities,
            [PasswordsPasswordIdentity(itemId: login.id, serviceDomain: "example.com", userName: "ada@example.com")])
    }

    // MARK: What the server says

    func testTheServersSentencesHaveACaseOfTheirOwn() {
        let sentences: [(String, PasswordsError)] = [
            ("That passphrase is not right.", .wrongPassphrase),
            ("api: invalid credential: That recovery key is not right.", .wrongRecoveryKey),
            ("api: conflict: This item changed on another device. Open it again.", .itemChangedOnAnotherDevice),
            ("api: invalid arguments: That person's key changed since it was read. Read their key again and seal the item for it.",
                .recipientKeyChangedSinceRead),
            ("api: invalid arguments: A card cannot be used by the agent.", .cardCannotBeUsedByAgent),
            ("api: already exists: Passwords is already set up.", .alreadySetUp),
            ("api: not found: Passwords is not set up yet.", .notSetUp),
            ("api: too many attempts, try again later", .tooManyAttempts),
        ]
        for (sentence, expected) in sentences {
            XCTAssertEqual(PasswordsError.mapped(TeaNodeError.refused(sentence)) as? PasswordsError, expected, sentence)
        }
        let other = TeaNodeError.refused("api: not found")
        XCTAssertEqual(PasswordsError.mapped(other) as? TeaNodeError, other)
    }

    func testTooManyWrongTriesComeBackTyped() async throws {
        let server = await makeServer()
        let device = makeDevice(server)
        _ = try await device.session.setUp(passphrase: "a long invented passphrase", iterationCount: iterationCount)
        await server.setRateLimited(true)
        let other = makeDevice(server)
        do {
            _ = try await other.session.unlock(passphrase: "a long invented passphrase")
            XCTFail("the server asked to wait")
        } catch {
            XCTAssertEqual(error as? PasswordsError, .tooManyAttempts)
        }
    }

    func testAKeyThatChangedWhileSharingComesBackTyped() async throws {
        let world = try await makeSharingWorld()
        let recipients = try await world.ada.session.recipients(granteeUserId: "user-grace")
        // Grace's key changes after Ada's device read it and before it sealed for it.
        await world.server.replacePublicKey(userId: "user-grace", publicKey: PasswordsCrypto.makeKeyPair().publicKey.base64EncodedString())
        do {
            _ = try await world.ada.session.setShares(
                itemId: world.itemId, grantees: [PasswordShareGranteeInput(granteeUserId: "user-grace")], recipients: recipients)
            XCTFail("the key was stale")
        } catch {
            XCTAssertEqual(error as? PasswordsError, .recipientKeyChangedSinceRead)
        }
    }
}
