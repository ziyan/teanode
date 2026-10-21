import Foundation
import XCTest

@testable import TeaNodeKit

/// PasswordsAutoFillSummaryTests cover what the AutoFill extension reads to list logins, and that the
/// extension and the app can use the same copy at the same time and within a memory limit.
final class PasswordsAutoFillSummaryTests: XCTestCase {
    private let iterationCount = PasswordsCrypto.minimumPassphraseIterationCount
    private var directories: [URL] = []

    override func tearDown() {
        for directory in directories { try? FileManager.default.removeItem(at: directory) }
        directories = []
        super.tearDown()
    }

    private func makeDirectory() -> URL {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("passwords-summary-\(UUID().uuidString)")
        directories.append(directory)
        return directory
    }

    private func makeServer(_ people: [(String, String)]) async -> FakePasswordsServer {
        let server = FakePasswordsServer()
        for (userId, name) in people { await server.addPerson(userId: userId, name: name) }
        return server
    }

    private func login(name: String, userName: String = "ada@example.com", sites: [String] = ["https://example.com"]) -> PasswordsItemFields {
        .login(PasswordsLoginFields(name: name, userName: userName, password: "correct horse", sites: sites))
    }

    // MARK: The summary

    func testEveryListInTheSummaryIsCappedAndNothingSecretIsInIt() throws {
        let sites = (0..<500).map { "https://host\($0).example.com/" + String(repeating: "p", count: 400) }
        let passkeys = (0..<100).map {
            PasswordsPasskeyFields(
                credentialId: "credential\($0)", relyingPartyId: "host\($0).example.com", userHandle: "handle", userName: "ada",
                userDisplayName: "Ada", privateKey: "PRIVATE-KEY-BYTES", createdAt: "2026-10-01T00:00:00Z")
        }
        let fields = PasswordsLoginFields(
            name: String(repeating: "n", count: 1000), userName: "ada@example.com", password: "correct horse", sites: sites,
            oneTimeCodeUri: "otpauth://totp/Example:ada?issuer=Example", notes: "private notes ONETIMEMARKER", passkeys: passkeys)
        let summary = PasswordsAutoFillSummary.login(itemId: "id-1", ownerName: "Grace", isSharedWithMe: true, fields: fields)
        // Somebody else's login lists fewer sites and passkeys than the person's own.
        XCTAssertEqual(summary.sites.count, PasswordsAutoFillSummary.mostSitesPerSharedLogin)
        XCTAssertEqual(summary.passkeys.count, PasswordsAutoFillSummary.mostPasskeysPerSharedLogin)
        let own = PasswordsAutoFillSummary.login(itemId: "id-2", ownerName: "", isSharedWithMe: false, fields: fields)
        XCTAssertEqual(own.sites.count, PasswordsAutoFillSummary.mostSitesPerLogin)
        XCTAssertEqual(own.passkeys.count, PasswordsAutoFillSummary.mostPasskeysPerLogin)
        XCTAssertLessThanOrEqual(summary.displayName.count, 200)
        XCTAssertTrue(summary.sites.allSatisfy { $0.count <= 200 })
        XCTAssertTrue(summary.hasPassword)
        XCTAssertTrue(summary.hasOneTimeCode)
        XCTAssertEqual(summary.ownerName, "Grace")
        XCTAssertTrue(summary.isSharedWithMe)
        let encoded = String(decoding: try JSONEncoder().encode(summary), as: UTF8.self)
        for secret in ["correct horse", "PRIVATE-KEY-BYTES", "ONETIMEMARKER", "private notes", "otpauth"] {
            XCTAssertFalse(encoded.contains(secret), secret)
        }
        // A login with no name is shown by its first site's host, else by its user name.
        XCTAssertEqual(
            PasswordsAutoFillSummary.login(itemId: "a", ownerName: "", isSharedWithMe: false, fields: PasswordsLoginFields(userName: "ada", sites: ["https://www.example.com/x"])).displayName,
            "example.com")
        XCTAssertEqual(
            PasswordsAutoFillSummary.login(itemId: "b", ownerName: "", isSharedWithMe: false, fields: PasswordsLoginFields(userName: "ada")).displayName,
            "ada")
    }

    func testTheSummaryHoldsAtMostAFixedNumberOfLoginsAndKeepsTheOwnOnesFirst() throws {
        var summary = PasswordsAutoFillSummary(userId: "user-ada")
        for index in 0..<(PasswordsAutoFillSummary.mostLogins + 50) {
            let isShared = index % 2 == 0
            summary.logins.append(
                PasswordsAutoFillLogin(
                    itemId: "id-\(index)", displayName: "Login \(index)", userName: "", sites: [], passkeys: [], ownerName: "Grace",
                    isSharedWithMe: isShared, hasPassword: true, hasOneTimeCode: false))
        }
        summary.upsert(
            PasswordsAutoFillLogin(
                itemId: "own-last", displayName: "Zzz", userName: "", sites: [], passkeys: [], ownerName: "", isSharedWithMe: false,
                hasPassword: true, hasOneTimeCode: false))
        // The count and the byte budget both apply; the person's own logins are all kept whichever bites.
        XCTAssertLessThanOrEqual(summary.logins.count, PasswordsAutoFillSummary.mostLogins)
        XCTAssertTrue(summary.logins.contains { $0.itemId == "own-last" })
        XCTAssertEqual(summary.logins.filter { !$0.isSharedWithMe }.count, (PasswordsAutoFillSummary.mostLogins + 50) / 2 + 1)
        XCTAssertLessThanOrEqual(try JSONEncoder().encode(summary).count, PasswordsAutoFillSummary.mostBytes)
    }

    func testTheSealedSummaryOpensOnlyWithTheAccountKeyOfTheSamePerson() throws {
        let accountKey = PasswordsCrypto.makeKey()
        var summary = PasswordsAutoFillSummary(userId: "user-ada")
        summary.upsert(PasswordsAutoFillSummary.login(itemId: "id-1", ownerName: "", isSharedWithMe: false, fields: PasswordsLoginFields(name: "Example", userName: "ada")))
        let data = try summary.sealedData(accountKey: accountKey)
        XCTAssertFalse(String(decoding: data, as: UTF8.self).contains("Example"))
        XCTAssertEqual(try PasswordsAutoFillSummary.open(data, accountKey: accountKey, userId: "user-ada"), summary)
        XCTAssertThrowsError(try PasswordsAutoFillSummary.open(data, accountKey: PasswordsCrypto.makeKey(), userId: "user-ada"))
        XCTAssertThrowsError(try PasswordsAutoFillSummary.open(data, accountKey: accountKey, userId: "user-grace"))
        XCTAssertThrowsError(try PasswordsAutoFillSummary.open(Data("not sealed".utf8), accountKey: accountKey, userId: "user-ada"))
    }

    // MARK: The app writes it, the extension reads it

    func testSyncingWritesTheSummaryAndSavingDeletingAndRestoringKeepItCurrent() async throws {
        let server = await makeServer([("user-ada", "Ada")])
        let cache = PasswordsCache(fileURL: makeDirectory().appendingPathComponent("passwords.json"))
        let session = PasswordsSession(api: FakePasswordsAPI(server: server, userId: "user-ada"), cache: cache, userId: "user-ada")
        _ = try await session.setUp(passphrase: "ada invented passphrase", iterationCount: iterationCount)
        let unlocked = try await session.unlock(passphrase: "ada invented passphrase")
        let key = unlocked.accountKey

        func summary() throws -> PasswordsAutoFillSummary {
            try PasswordsAutoFillSummary.open(try XCTUnwrap(try cache.loadSummaryData()), accountKey: key, userId: "user-ada")
        }

        let first = try await session.save(fields: login(name: "Alpha"))
        _ = try await session.save(fields: .note(PasswordsNoteFields(name: "A note", text: "words")))
        _ = try await session.save(fields: .card(PasswordsCardFields(name: "A card", number: "4111")))
        XCTAssertEqual(try summary().logins.map(\.displayName), ["Alpha"], "a note and a card are not listed")

        // A sync rebuilds it from the whole copy.
        try FileManager.default.removeItem(at: cache.summaryFileURL)
        try await session.sync(isFull: true)
        XCTAssertEqual(try summary().logins.map(\.displayName), ["Alpha"])

        _ = try await session.save(fields: login(name: "Alpha renamed"), id: first.id)
        XCTAssertEqual(try summary().logins.map(\.displayName), ["Alpha renamed"])
        _ = try await session.delete(id: first.id)
        XCTAssertEqual(try summary().logins, [])
        _ = try await session.restore(id: first.id)
        XCTAssertEqual(try summary().logins.map(\.displayName), ["Alpha renamed"])
        // A missing summary is made again from the whole copy by the next change.
        try FileManager.default.removeItem(at: cache.summaryFileURL)
        _ = try await session.save(fields: login(name: "Beta"))
        XCTAssertEqual(try summary().logins.map(\.displayName), ["Alpha renamed", "Beta"])
    }

    func testASharedLoginIsInTheSummaryWithItsOwnersNameOnceTheKeyIsKnown() async throws {
        let server = await makeServer([("user-ada", "Ada"), ("user-grace", "Grace")])
        let ada = PasswordsSession(
            api: FakePasswordsAPI(server: server, userId: "user-ada"),
            cache: PasswordsCache(fileURL: makeDirectory().appendingPathComponent("passwords.json")), userId: "user-ada")
        let graceCache = PasswordsCache(fileURL: makeDirectory().appendingPathComponent("passwords.json"))
        let grace = PasswordsSession(api: FakePasswordsAPI(server: server, userId: "user-grace"), cache: graceCache, userId: "user-grace")
        _ = try await ada.setUp(passphrase: "ada invented passphrase", iterationCount: iterationCount)
        _ = try await grace.setUp(passphrase: "grace invented passphrase", iterationCount: iterationCount)
        let item = try await ada.save(fields: login(name: "Shared by Ada"))
        let recipients = try await ada.recipients(granteeUserId: "user-grace")
        _ = try await ada.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-grace")], recipients: recipients,
            confirmedUserIds: ["user-grace"])
        let graceKey = try await grace.unlock(passphrase: "grace invented passphrase").accountKey
        try await grace.sync()
        let summary = try PasswordsAutoFillSummary.open(
            try XCTUnwrap(try graceCache.loadSummaryData()), accountKey: graceKey, userId: "user-grace")
        XCTAssertEqual(summary.logins.map(\.displayName), ["Shared by Ada"])
        XCTAssertEqual(summary.logins.first?.ownerName, "Ada")
        XCTAssertEqual(summary.logins.first?.isSharedWithMe, true)
    }

    func testAnOnDemandSessionOpensOneItemAndSavesWithoutReadingTheOthers() async throws {
        let server = await makeServer([("user-ada", "Ada")])
        let directory = makeDirectory()
        let pinStore = PasswordsMemoryPinStore()
        let appCache = PasswordsCache(fileURL: directory.appendingPathComponent("passwords.json"), pinStore: pinStore)
        let app = PasswordsSession(api: FakePasswordsAPI(server: server, userId: "user-ada"), cache: appCache, userId: "user-ada")
        _ = try await app.setUp(passphrase: "ada invented passphrase", iterationCount: iterationCount)
        let unlocked = try await app.unlock(passphrase: "ada invented passphrase")
        let alpha = try await app.save(fields: login(name: "Alpha"))
        let beta = try await app.save(fields: login(name: "Beta", userName: "other"))

        // Damage Beta's file: an on-demand session that never asks for it does not notice.
        try Data("not json".utf8).write(to: appCache.itemFileURL(id: beta.id))
        let extensionCache = PasswordsCache(fileURL: appCache.fileURL, pinStore: pinStore)
        let autoFill = PasswordsSession(
            api: FakePasswordsAPI(server: server, userId: "user-ada"), cache: extensionCache, userId: "user-ada",
            isLoadingItemsOnDemand: true)
        try await autoFill.open(accountKey: unlocked.accountKey)
        let opened = try await autoFill.item(id: alpha.id)
        XCTAssertEqual(opened.fields.withAccess(nil), login(name: "Alpha"))
        do {
            _ = try await autoFill.item(id: beta.id)
            XCTFail("its file is damaged")
        } catch {
            XCTAssertEqual(error as? PasswordsError, .itemNotFound)
        }
        // It cannot sync: the app does.
        do {
            try await autoFill.sync()
            XCTFail("an on-demand session does not sync")
        } catch {
            XCTAssertEqual(error as? PasswordsError, .locked)
        }

        // It saves a new login, and the app's copy and the summary have it.
        let saved = try await autoFill.save(fields: login(name: "Gamma"))
        let loaded = try XCTUnwrap(try appCache.load())
        XCTAssertNotNil(loaded.item(id: saved.id))
        XCTAssertNotNil(loaded.item(id: alpha.id))
        let summary = try PasswordsAutoFillSummary.open(
            try XCTUnwrap(try appCache.loadSummaryData()), accountKey: unlocked.accountKey, userId: "user-ada")
        XCTAssertTrue(summary.logins.contains { $0.itemId == saved.id })
        XCTAssertTrue(summary.logins.contains { $0.itemId == alpha.id })
    }

    /// The app and the extension each hold the copy as they read it, and each saves. The later save
    /// must not undo the earlier: not the item, not the revision, not a pinned key.
    func testTwoProcessesWritingTheSameCacheDoNotUndoEachOther() async throws {
        let server = await makeServer([("user-ada", "Ada"), ("user-ben", "Ben")])
        let directory = makeDirectory()
        let pinStore = PasswordsMemoryPinStore()
        let appCache = PasswordsCache(fileURL: directory.appendingPathComponent("passwords.json"), pinStore: pinStore)
        let app = PasswordsSession(api: FakePasswordsAPI(server: server, userId: "user-ada"), cache: appCache, userId: "user-ada")
        _ = try await app.setUp(passphrase: "ada invented passphrase", iterationCount: iterationCount)
        let unlocked = try await app.unlock(passphrase: "ada invented passphrase")
        let alpha = try await app.save(fields: login(name: "Alpha"))
        try await app.sync()

        let autoFill = PasswordsSession(
            api: FakePasswordsAPI(server: server, userId: "user-ada"), cache: PasswordsCache(fileURL: appCache.fileURL, pinStore: pinStore),
            userId: "user-ada", isLoadingItemsOnDemand: true)
        try await autoFill.open(accountKey: unlocked.accountKey)

        // The app confirms Ben's key and shares with him; the extension still holds the copy from before.
        let ben = PasswordsSession(
            api: FakePasswordsAPI(server: server, userId: "user-ben"),
            cache: PasswordsCache(fileURL: directory.appendingPathComponent("ben.json")), userId: "user-ben")
        _ = try await ben.setUp(passphrase: "ben invented passphrase", iterationCount: iterationCount)
        let recipients = try await app.recipients(granteeUserId: "user-ben")
        _ = try await app.setShares(
            itemId: alpha.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-ben")], recipients: recipients,
            confirmedUserIds: ["user-ben"])
        let revisionBefore = try XCTUnwrap(try appCache.loadHeader()).revision

        // The extension saves a passkey into a new login.
        let fromExtension = try await autoFill.save(fields: login(name: "From the extension"))
        // The app, whose copy lacks that login, saves another.
        let fromApp = try await app.save(fields: login(name: "From the app"))

        let loaded = try XCTUnwrap(try appCache.load())
        XCTAssertEqual(Set(loaded.items.map(\.id)), [alpha.id, fromExtension.id, fromApp.id])
        XCTAssertGreaterThanOrEqual(loaded.revision, revisionBefore)
        XCTAssertNotNil(loaded.confirmedPublicKeys["user-ben"], "the confirmed key survives both saves")
        let summary = try PasswordsAutoFillSummary.open(
            try XCTUnwrap(try appCache.loadSummaryData()), accountKey: unlocked.accountKey, userId: "user-ada")
        XCTAssertEqual(Set(summary.logins.map(\.itemId)), [alpha.id, fromExtension.id, fromApp.id])

        // The app reads the server and finds the extension's login there too.
        try await app.sync()
        let listed = try await app.list()
        XCTAssertEqual(Set(listed.items.map(\.id)), [alpha.id, fromExtension.id, fromApp.id])
    }

    // MARK: Pinned keys

    func testAFileInTheSharedFolderCannotPlantAConfirmedKeyWhenThePinsAreKeptElsewhere() async throws {
        let server = await makeServer([("user-ada", "Ada"), ("user-mallory", "Mallory")])
        await server.setCircle("circle-one", members: ["user-ada", "user-mallory"])
        let cache = PasswordsCache(fileURL: makeDirectory().appendingPathComponent("passwords.json"), pinStore: PasswordsMemoryPinStore())
        let ada = PasswordsSession(api: FakePasswordsAPI(server: server, userId: "user-ada"), cache: cache, userId: "user-ada")
        _ = try await ada.setUp(passphrase: "ada invented passphrase", iterationCount: iterationCount)
        let item = try await ada.save(fields: login(name: "Example"))
        let recipients = try await ada.recipients(granteeCircleId: "circle-one")
        _ = try await ada.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeCircleId: "circle-one")], recipients: recipients)
        let operatorDevice = PasswordsSession(
            api: FakePasswordsAPI(server: server, userId: "user-mallory"),
            cache: PasswordsCache(fileURL: makeDirectory().appendingPathComponent("passwords.json")), userId: "user-mallory")
        _ = try await operatorDevice.setUp(passphrase: "operator passphrase", iterationCount: iterationCount)
        let operatorKey = await server.publicKey(of: "user-mallory")

        // What a process that shares the app group folder can do: write the pins file.
        let planted = PasswordsPins(userId: "user-ada", confirmedPublicKeys: ["user-mallory": operatorKey])
        try JSONEncoder().encode(planted).write(to: cache.pinsFileURL, options: .atomic)

        let relaunched = PasswordsSession(api: FakePasswordsAPI(server: server, userId: "user-ada"), cache: cache, userId: "user-ada")
        _ = try await relaunched.unlock(passphrase: "ada invented passphrase")
        let completed = try await relaunched.completeWaitingShares()
        XCTAssertEqual(completed.addedKeyCount, 0)
        XCTAssertEqual(completed.waitingForConfirmation.count, 1)
        let stored = await server.item(item.id)
        XCTAssertNil(stored?.keys["user-mallory"])
    }

    func testPinsThatCannotBeReadAreNotTakenForNoneAndAreNotWrittenOver() async throws {
        let server = await makeServer([("user-ada", "Ada"), ("user-grace", "Grace"), ("user-linus", "Linus")])
        let cache = PasswordsCache(fileURL: makeDirectory().appendingPathComponent("passwords.json"))
        let ada = PasswordsSession(api: FakePasswordsAPI(server: server, userId: "user-ada"), cache: cache, userId: "user-ada")
        _ = try await ada.setUp(passphrase: "ada invented passphrase", iterationCount: iterationCount)
        for (userId, name) in [("user-grace", "grace"), ("user-linus", "linus")] {
            let device = PasswordsSession(
                api: FakePasswordsAPI(server: server, userId: userId),
                cache: PasswordsCache(fileURL: makeDirectory().appendingPathComponent("passwords.json")), userId: userId)
            _ = try await device.setUp(passphrase: "\(name) invented passphrase", iterationCount: iterationCount)
        }
        let item = try await ada.save(fields: login(name: "Example"))
        let graceRecipients = try await ada.recipients(granteeUserId: "user-grace")
        _ = try await ada.setShares(
            itemId: item.id, grantees: [PasswordShareGranteeInput(granteeUserId: "user-grace")], recipients: graceRecipients,
            confirmedUserIds: ["user-grace"])
        let known = try await ada.recipients(granteeUserId: "user-grace")
        XCTAssertEqual(known.first?.keyTrust, .known)

        // The pins file is damaged (by anything, or half written): Grace is still known, nothing is written over it.
        let damaged = Data("{\"formatVersion\":1,\"userId\":\"user-ada\",\"seenPublicKeys\":5}".utf8)
        try damaged.write(to: cache.pinsFileURL)
        let stillKnown = try await ada.recipients(granteeUserId: "user-grace")
        XCTAssertEqual(stillKnown.first?.keyTrust, .known)
        let linusRecipients = try await ada.recipients(granteeUserId: "user-linus")
        XCTAssertEqual(linusRecipients.first?.keyTrust, .new)
        do {
            _ = try await ada.setShares(
                itemId: item.id,
                grantees: [PasswordShareGranteeInput(granteeUserId: "user-grace"), PasswordShareGranteeInput(granteeUserId: "user-linus")],
                recipients: graceRecipients + linusRecipients, confirmedUserIds: ["user-linus"])
            XCTFail("the confirmation could not be written")
        } catch {
            XCTAssertEqual(error as? PasswordsPinStoreError, .unreadable)
        }
        XCTAssertEqual(try Data(contentsOf: cache.pinsFileURL), damaged)
        let stored = await server.item(item.id)
        XCTAssertNil(stored?.keys["user-linus"], "nothing was sealed for the key that could not be recorded")
    }

    // MARK: The byte budget

    /// A login as a hostile circle member makes it: every list as long as the item allows, with the
    /// longest ids and names. `weight` makes some of them bigger than others.
    private func hostileFields(index: Int, weight: Int) -> PasswordsLoginFields {
        let sites = (0..<PasswordsAutoFillSummary.mostSitesPerLogin).map { number in
            "https://" + String(repeating: "a", count: 150) + "\(index)-\(number).example.test/" + String(repeating: "p", count: 30)
        }
        let passkeys = (0..<PasswordsAutoFillSummary.mostPasskeysPerLogin).map { number in
            PasswordsPasskeyFields(
                credentialId: String(repeating: "c", count: 1000 + weight * 200) + "\(index)-\(number)",
                relyingPartyId: String(repeating: "r", count: 190) + "\(index)", userHandle: "dXNlcg",
                userName: String(repeating: "u", count: 190) + "\(number)", userDisplayName: "Ada",
                privateKey: String(repeating: "k", count: 200), createdAt: "2026-10-21T00:00:00Z")
        }
        return PasswordsLoginFields(
            name: String(repeating: "n", count: 190) + "\(index)", userName: String(repeating: "x", count: 190) + "\(index)", password: "pw",
            sites: sites, passkeys: passkeys)
    }

    private func jsonBytes(_ summary: PasswordsAutoFillSummary) throws -> Int {
        try JSONEncoder().encode(summary).count
    }

    func testSharedLoginsAreCappedPerOwnerAndAHostileCircleStaysInsideTheByteBudget() throws {
        var logins: [PasswordsAutoFillLogin] = []
        // Ten of the person's own logins, each as long as a login can be.
        for index in 0..<10 {
            logins.append(
                PasswordsAutoFillSummary.login(
                    itemId: "own-\(index)", ownerName: "", isSharedWithMe: false, fields: hostileFields(index: index, weight: 0)))
        }
        // Twelve owners share 250 hostile logins each, of different sizes: with their own caps
        // that is still tens of megabytes.
        for owner in 0..<12 {
            for index in 0..<250 {
                logins.append(
                    PasswordsAutoFillSummary.login(
                        itemId: "shared-\(owner)-\(index)", ownerName: "Owner \(owner)", isSharedWithMe: true,
                        fields: hostileFields(index: owner * 1000 + index, weight: index % 5), ownerUserId: "user-owner-\(owner)"))
        }
        }
        XCTAssertGreaterThan(try JSONEncoder().encode(logins).count, 20 * 1024 * 1024)
        var summary = PasswordsAutoFillSummary(userId: "user-ada", logins: logins)
        summary.sortAndCap()

        XCTAssertLessThanOrEqual(try jsonBytes(summary), PasswordsAutoFillSummary.mostBytes)
        // The person's own logins all stay.
        XCTAssertEqual(summary.logins.filter { !$0.isSharedWithMe }.count, 10)
        // No owner has more than the cap, and no shared login lists more than five sites and five passkeys.
        let sharedByOwner = Dictionary(grouping: summary.logins.filter(\.isSharedWithMe), by: { $0.ownerUserId ?? "" })
        XCTAssertTrue(sharedByOwner.values.allSatisfy { $0.count <= PasswordsAutoFillSummary.mostSharedLoginsPerOwner })
        XCTAssertTrue(summary.logins.filter(\.isSharedWithMe).allSatisfy { $0.sites.count <= 5 && $0.passkeys.count <= 5 })
        // What was dropped is the largest of the shared logins: nothing kept is bigger than anything dropped.
        let keptItemIds = Set(summary.logins.map(\.itemId))
        let sharedLogins = logins.filter(\.isSharedWithMe)
        let afterOwnerCap = Dictionary(grouping: sharedLogins, by: { $0.ownerUserId ?? "" }).values.flatMap {
            Array($0.sorted { $0.displayName < $1.displayName }.prefix(PasswordsAutoFillSummary.mostSharedLoginsPerOwner))
        }
        let keptSizes = afterOwnerCap.filter { keptItemIds.contains($0.itemId) }.map { (try? JSONEncoder().encode($0).count) ?? 0 }
        let droppedSizes = afterOwnerCap.filter { !keptItemIds.contains($0.itemId) }.map { (try? JSONEncoder().encode($0).count) ?? 0 }
        XCTAssertFalse(droppedSizes.isEmpty)
        XCTAssertLessThanOrEqual(keptSizes.max() ?? 0, droppedSizes.min() ?? 0)

        // It seals, within the size a sealed summary may have, and opens.
        let accountKey = PasswordsCrypto.makeKey()
        let sealed = try summary.sealedData(accountKey: accountKey)
        XCTAssertLessThanOrEqual(sealed.count, PasswordsAutoFillSummary.mostSealedBytes)
        XCTAssertEqual(try PasswordsAutoFillSummary.open(sealed, accountKey: accountKey, userId: "user-ada"), summary)
    }

    func testOpeningTheSummaryOfAHostileCircleTakesAFewMegabytes() throws {
        var logins: [PasswordsAutoFillLogin] = []
        for owner in 0..<12 {
            for index in 0..<250 {
                logins.append(
                    PasswordsAutoFillSummary.login(
                        itemId: "shared-\(owner)-\(index)", ownerName: "Owner \(owner)", isSharedWithMe: true,
                        fields: hostileFields(index: owner * 1000 + index, weight: index % 5), ownerUserId: "user-owner-\(owner)"))
            }
        }
        var summary = PasswordsAutoFillSummary(userId: "user-ada", logins: logins)
        logins = []
        summary.sortAndCap()
        let accountKey = PasswordsCrypto.makeKey()
        let sealed = try summary.sealedData(accountKey: accountKey)
        summary = PasswordsAutoFillSummary(userId: "user-ada")

        guard resetPeak() else { throw XCTSkip("the peak of this process cannot be reset here") }
        let baselineKilobytes = residentKilobytes("VmHWM")
        let opened = try PasswordsAutoFillSummary.open(sealed, accountKey: accountKey, userId: "user-ada")
        let growthMiB = (residentKilobytes("VmHWM") - baselineKilobytes) / 1024
        print("Passwords memory: hostile summary of \(sealed.count / 1024) KiB sealed opened, \(opened.logins.count) logins: peak grew by \(growthMiB) MiB")
        XCTAssertFalse(opened.logins.isEmpty)
        // The summary the audit found needed about 200 MB to open.
        XCTAssertLessThan(growthMiB, 20)
    }

    func testASummaryOverTheBudgetIsNeverWritten() throws {
        var summary = PasswordsAutoFillSummary(userId: "user-ada")
        for index in 0..<200 {
            summary.logins.append(
                PasswordsAutoFillSummary.login(
                    itemId: "own-\(index)", ownerName: "", isSharedWithMe: false, fields: hostileFields(index: index, weight: 4)))
        }
        XCTAssertGreaterThan(try jsonBytes(summary), PasswordsAutoFillSummary.mostBytes)
        XCTAssertThrowsError(try summary.sealedData(accountKey: PasswordsCrypto.makeKey())) { error in
            XCTAssertEqual(error as? PasswordsCacheError, .tooLarge)
        }
        // Made the proper way, the person's own logins alone over the budget lose the largest first and it fits.
        summary.sortAndCap()
        XCTAssertLessThanOrEqual(try jsonBytes(summary), PasswordsAutoFillSummary.mostBytes)
        XCTAssertNoThrow(try summary.sealedData(accountKey: PasswordsCrypto.makeKey()))
    }

    func testAnOldSummaryFileOfAnySizeIsNotRead() throws {
        let directory = makeDirectory()
        let cache = PasswordsCache(fileURL: directory.appendingPathComponent("passwords.json"), pinStore: PasswordsMemoryPinStore())
        // A file bigger than any summary this build writes, as an earlier build could have left.
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let oversized = Data(repeating: 0x61, count: PasswordsAutoFillSummary.mostSealedBytes + 1)
        try oversized.write(to: cache.summaryFileURL)
        XCTAssertNil(try cache.loadSummaryData())
        XCTAssertThrowsError(try PasswordsAutoFillSummary.open(oversized, accountKey: PasswordsCrypto.makeKey(), userId: "user-ada")) { error in
            XCTAssertEqual(error as? PasswordsCacheError, .unreadable)
        }
        // The app's update starts over from nothing instead of reading it.
        var sawData: Data?
        var wasCalled = false
        try cache.updateSummaryData { current in
            wasCalled = true
            sawData = current
            return nil
        }
        XCTAssertTrue(wasCalled)
        XCTAssertNil(sawData)
    }

    func testASharedLoginIsSummarizedWithItsOwnerAndTheSmallerCaps() throws {
        let fields = PasswordsItemFields.login(hostileFields(index: 1, weight: 0))
        let item = PasswordItem(
            id: "item-1", fields: fields, ownerUserId: "user-grace", ownerName: "Grace", isSharedWithMe: true, revision: 1,
            isUsableByAgent: false, lastUsedByAgentAt: nil, deletedAt: nil, createdAt: nil, updatedAt: nil, shares: [], needsNewKey: false,
            sharingCheck: .ownedByAnotherPerson)
        let login = try XCTUnwrap(PasswordsAutoFillSummary.login(of: item))
        XCTAssertEqual(login.ownerUserId, "user-grace")
        XCTAssertEqual(login.sites.count, PasswordsAutoFillSummary.mostSitesPerSharedLogin)
        XCTAssertEqual(login.passkeys.count, PasswordsAutoFillSummary.mostPasskeysPerSharedLogin)
    }

    // MARK: Memory

    private func residentKilobytes(_ field: String) -> Int {
        let status = (try? String(contentsOfFile: "/proc/self/status", encoding: .utf8)) ?? ""
        for line in status.split(separator: "\n") where line.hasPrefix(field + ":") {
            return Int(line.split(separator: " ").dropFirst().first ?? "0") ?? 0
        }
        return 0
    }

    /// Starts the peak (VmHWM) over from the current size, as `echo 5 > /proc/self/clear_refs` does.
    private func resetPeak() -> Bool {
        guard let handle = FileHandle(forWritingAtPath: "/proc/self/clear_refs") else { return false }
        defer { try? handle.close() }
        do {
            try handle.write(contentsOf: Data("5".utf8))
            return true
        } catch {
            return false
        }
    }

    /// A 64 MiB copy of sealed items (128 of half a megabyte, as a circle member can share):
    /// listing logins and opening the chosen one must stay well under what the extension may use
    /// (about 120 MB), where reading the whole copy at once went far past it.
    func testListingAndOpeningOneItemFromA64MiBCopyStaysWellUnderTheExtensionLimit() async throws {
        let itemCount = 128
        let directory = makeDirectory()
        let cache = PasswordsCache(fileURL: directory.appendingPathComponent("passwords.json"), pinStore: PasswordsMemoryPinStore())
        let accountKey = PasswordsCrypto.makeKey()
        let keyPair = PasswordsCrypto.makeKeyPair()
        let sealedPrivateKey = try PasswordsCrypto.seal(
            keyPair.privateKey, key: accountKey, associatedData: PasswordsCrypto.privateKeyAssociatedData)
        let header = PasswordsCacheContents(
            userId: "user-ada", publicKey: keyPair.publicKey.base64EncodedString(), privateKeyByAccountKey: sealedPrivateKey.jsonText,
            revision: 1, lockAfterMinutes: 15)

        // One real item (sealed by its own key), and 127 others carrying half a megabyte of sealed-looking text each.
        let itemId = UUID().uuidString.lowercased()
        let itemKey = PasswordsCrypto.makeKey()
        let sealedFields = try PasswordsCrypto.sealItemFields(login(name: "The chosen one"), itemKey: itemKey, itemId: itemId)
        let sealedItemKey = try PasswordsCrypto.seal(
            itemKey, key: accountKey, associatedData: PasswordsCrypto.itemKeyAssociatedData(itemId: itemId))
        func view(id: String, fields: String, itemKey: String) -> PasswordItemView {
            PasswordItemView(
                id: id, ownerUserId: "user-ada", ownerName: "Ada", ownerPublicKey: header.publicKey, itemKind: "login",
                encryptedFields: fields, itemKey: itemKey, isSharedWithMe: false, revision: 1, isUsableByAgent: false,
                createdAt: "2026-10-20T10:00:00Z", updatedAt: "2026-10-20T10:00:00Z")
        }
        let chosen = view(id: itemId, fields: sealedFields.jsonText, itemKey: sealedItemKey.jsonText)
        let filler = Data(PasswordsCrypto.randomBytes(count: 384 * 1024)).base64EncodedString()
        var summary = PasswordsAutoFillSummary(userId: "user-ada")
        summary.upsert(PasswordsAutoFillSummary.login(itemId: itemId, ownerName: "Ada", isSharedWithMe: false, fields: PasswordsLoginFields(name: "The chosen one", userName: "ada", password: "correct horse", sites: ["https://example.com"])))
        try cache.save(header, changedItemIds: [], removedItemIds: [])
        try cache.save(PasswordsCacheContents(userId: "user-ada", publicKey: header.publicKey, privateKeyByAccountKey: header.privateKeyByAccountKey, revision: 1, lockAfterMinutes: 15, items: [chosen]), changedItemIds: [itemId])
        for start in stride(from: 0, to: itemCount - 1, by: 8) {
            var batch: [PasswordItemView] = []
            for index in start..<min(start + 8, itemCount - 1) {
                batch.append(view(id: "filler-\(index)", fields: filler, itemKey: "{}"))
            }
            try cache.save(
                PasswordsCacheContents(userId: "user-ada", publicKey: header.publicKey, privateKeyByAccountKey: header.privateKeyByAccountKey, revision: 1, lockAfterMinutes: 15, items: batch),
                changedItemIds: Set(batch.map(\.id)))
        }
        try cache.saveSummaryData(try summary.sealedData(accountKey: accountKey))
        var bytes = 0
        for name in try FileManager.default.contentsOfDirectory(atPath: cache.itemsDirectoryURL.path) {
            let attributes = try FileManager.default.attributesOfItem(atPath: cache.itemsDirectoryURL.appendingPathComponent(name).path)
            bytes += (attributes[.size] as? Int) ?? 0
        }
        XCTAssertGreaterThan(bytes, 48 * 1024 * 1024)

        guard resetPeak() else { throw XCTSkip("the peak of this process cannot be reset here") }
        let baselineKilobytes = residentKilobytes("VmHWM")

        // What the extension does: the header, the summary, the one item.
        let extensionCache = PasswordsCache(fileURL: cache.fileURL, pinStore: PasswordsMemoryPinStore())
        let loadedHeader = try XCTUnwrap(try extensionCache.loadHeader())
        let opener = try PasswordsOpener(accountKey: accountKey, contents: loadedHeader)
        let openedSummary = try PasswordsAutoFillSummary.open(
            try XCTUnwrap(try extensionCache.loadSummaryData()), accountKey: accountKey, userId: "user-ada")
        XCTAssertEqual(openedSummary.logins.map(\.displayName), ["The chosen one"])
        let chosenView = try XCTUnwrap(try extensionCache.loadItem(id: itemId))
        let opened = try opener.open(chosenView)
        XCTAssertEqual(opened.name, "The chosen one")
        let lightPeakKilobytes = residentKilobytes("VmHWM")
        let lightGrowthMiB = (lightPeakKilobytes - baselineKilobytes) / 1024
        print("Passwords memory: 64 MiB copy, summary and one item: peak grew by \(lightGrowthMiB) MiB")
        XCTAssertLessThan(lightGrowthMiB, 30)

        // For comparison: reading the whole copy at once, which the extension used to do.
        guard resetPeak() else { return }
        let heavyBaselineKilobytes = residentKilobytes("VmHWM")
        let whole = try XCTUnwrap(try extensionCache.load())
        XCTAssertEqual(whole.items.count, itemCount)
        let heavyGrowthMiB = (residentKilobytes("VmHWM") - heavyBaselineKilobytes) / 1024
        print("Passwords memory: 64 MiB copy read whole: peak grew by \(heavyGrowthMiB) MiB")
        XCTAssertGreaterThan(heavyGrowthMiB, lightGrowthMiB)
    }
}
