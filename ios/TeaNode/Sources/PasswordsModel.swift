import Foundation
import Observation
import TeaNodeKit
import UIKit

/// PasswordsPhase is where Passwords is for the person on this iPhone.
enum PasswordsPhase: Equatable {
    /// The first read of the setup has not finished.
    case loading
    /// The first read failed; `failedText` says why.
    case failed
    /// The person has not set Passwords up.
    case notSetUp
    /// Set up a moment ago: the recovery key is on screen until the person says they saved it.
    case recoveryKey
    /// Set up, and closed. A passphrase, the recovery key or Face ID opens it.
    case locked
    /// Open: the items are shown.
    case unlocked
}

/// PasswordsSharingProgress is how many items "Finish sharing" has done of how many it will do.
struct PasswordsSharingProgress: Equatable {
    let doneCount: Int
    let totalCount: Int
}

/// PasswordsFinishAsk is the question asked before sharing is finished: for which items, and what it
/// would do (`PasswordsWords.finishConfirmationLines`).
struct PasswordsFinishAsk: Identifiable {
    let id = UUID()
    let itemIds: [String]
    let plan: PasswordsFinishPlan
    let title: String

    var isOneItem: Bool { itemIds.count == 1 }
}

/// PasswordsModel is Passwords on this iPhone: whether it is set up, locked or
/// open, and the person's items while it is open. It owns the session
/// (TeaNodeKit's PasswordsSession), which holds the account key in memory only
/// while open, keeps the encrypted copy of the items in the app group's
/// container (which the AutoFill extension reads) and joins both to the
/// server. The account key can also wait behind Face ID in the shared keychain,
/// so the passphrase is typed once.
@MainActor
@Observable
final class PasswordsModel {
    let connection: Connection
    /// Whether this is the screenshots' fake server, which makes the key
    /// derivation cheaper and leaves the system's stores alone.
    let isDemo: Bool

    private(set) var phase = PasswordsPhase.loading
    private(set) var failedText: String?
    /// The recovery key as the person writes it down, while `phase` is `recoveryKey`.
    private(set) var recoveryKeyText: String?
    /// The items that are not deleted, by name.
    private(set) var items: [PasswordItem] = []
    /// The items deleted in the last 30 days, the latest first.
    private(set) var deletedItems: [PasswordItem] = []
    /// How many items could not be opened on this iPhone: damaged, or saved by a newer app.
    private(set) var unreadableItemCount = 0
    /// The people whose key changed from the one this iPhone knew, whether they shared items with the
    /// person or the person shared with them. The new key was accepted and nothing waits on it: each
    /// has a notice until the person dismisses it.
    private(set) var keyChanges: [PasswordsKeyChange] = []
    /// The people this iPhone sealed an item for through a circle for the first time ("<name> got
    /// <item> through <circle>"). Each stays until the person dismisses it; nothing waits on it.
    private(set) var circleShareNotices: [PasswordsCircleShareNotice] = []
    /// The items this iPhone left for the person to finish sharing (it has no memory of them from
    /// before, or somebody waiting is a person it never sealed for): the list says "Open this item
    /// to finish sharing it" and the item's screen has the button (`finishSharing`).
    private(set) var itemIdsNeedingYou: Set<String> = []
    /// The person's items the server marked as needing a new item key (somebody was removed, the
    /// agent was turned off, a member left a circle) that could not be given one yet: the item's
    /// screen says so and offers a retry.
    private(set) var itemIdsNeedingNewKey: [String] = []
    /// The items among those that this iPhone did not give a new key by itself: one was given a
    /// moment ago, or it has given the most it gives in a session. The item's screen says so.
    private(set) var itemIdsNeedingAttention: Set<String> = []
    /// The earlier versions of the items whose list the person opened, by item id. Their fields are
    /// in the clear, so they stay in memory only, and are dropped when Passwords locks.
    private(set) var versionsByItemId: [String: PasswordsOpenedVersions] = [:]
    /// How far "Finish sharing" for every waiting item has got, while it runs; nil otherwise.
    private(set) var sharingProgress: PasswordsSharingProgress?
    /// The names of people left out of the last check of waiting shares because they already have
    /// the most the server lets others share with them ("<name> has too much shared with them.").
    private(set) var namesWithTooMuchShared: [String] = []
    /// Whether the server said, at the last check, that more shares are waiting than it listed:
    /// the rest shows once the ones listed are finished.
    private(set) var hasMoreSharesWaiting = false
    /// The person's own safety code, made from the key pair this iPhone opened; nil while locked.
    /// Anybody who wants to check the person's key can compare it. Nothing waits on it.
    private(set) var ownSafetyCode: String?
    private(set) var isSyncing = false
    /// Whether the account key waits behind Face ID on this iPhone.
    private(set) var hasFaceIDKey = false
    private(set) var lockAfterMinutes = PasswordsSession.defaultLockAfterMinutes
    private(set) var ownUserId: String?
    /// Whether the screen is held covered after the app came back, until the
    /// lock time has been checked, so no frame of items that should have locked
    /// is shown (`sceneDidResignActive`, `sceneDidBecomeActive`).
    private(set) var isHoldingCover = false
    var problemText: String?
    var doneText: String?

    @ObservationIgnored private var session: PasswordsSession?
    private let keyStore: any PasswordsKeyStore
    private let iterationCount: Int
    /// The passphrase typed at setup, held until the recovery key screen is done so Face ID can be set up.
    @ObservationIgnored private var passphraseOfSetUp: String?
    @ObservationIgnored private var isOpeningWithFaceID = false
    @ObservationIgnored private var isStarting = false

    init(connection: Connection) {
        self.connection = connection
        #if DEBUG
            let isDemoRequested = DemoServer.isRequested
        #else
            let isDemoRequested = false
        #endif
        if isDemoRequested {
            keyStore = PasswordsMemoryKeyStore()
            iterationCount = PasswordsCrypto.minimumPassphraseIterationCount
        } else {
            keyStore = PasswordsKeychainKeyStore.forThisBundle()
            iterationCount = PasswordsCrypto.passphraseIterationCount
        }
        isDemo = isDemoRequested
        // The lock time comes from the server, so it cannot be the only thing that locks Passwords:
        // when the phone itself locks, Passwords closes, whatever the lock time says.
        NotificationCenter.default.addObserver(
            forName: UIApplication.protectedDataWillBecomeUnavailableNotification, object: nil, queue: .main
        ) { [weak self] _ in
            Task { @MainActor in await self?.lockBecausePhoneLocked() }
        }
    }

    // MARK: Facts the screens ask

    /// Whether this iPhone can keep the account key behind Face ID.
    var canUseFaceID: Bool {
        if isDemo { return true }
        return PasswordsKeychainKeyStore.isBiometryAvailable
    }

    /// The folders the items are in, by name.
    var folders: [String] {
        Set(items.map(\.folder).filter { !$0.isEmpty }).sorted { $0.localizedStandardCompare($1) == .orderedAscending }
    }

    /// An item of the open Passwords, deleted ones included.
    func item(id: String) -> PasswordItem? {
        items.first { $0.id == id } ?? deletedItems.first { $0.id == id }
    }

    // MARK: Starting

    /// Reads whether Passwords is set up, and opens it with Face ID when the
    /// account key waits behind it. Called each time the screen appears.
    func start() async {
        guard !isStarting else { return }
        isStarting = true
        defer { isStarting = false }
        if session != nil, phase != .failed {
            await lockIfAskRequired()
            if phase == .locked, hasFaceIDKey, !isDemo { await unlockWithFaceID() }
            return
        }
        phase = .loading
        failedText = nil
        do {
            let accessSession = try await connection.accessSession()
            guard let userId = accessSession.userId, !userId.isEmpty else { throw PasswordsError.invalidServerAnswer }
            ownUserId = userId
            let newSession = PasswordsSession(api: connection, cache: Self.makeCache(signInAccountKey: connection.accountKey), userId: userId)
            let setup = try await newSession.readSetup()
            session = newSession
            lockAfterMinutes = setup.lockAfterMinutes
            hasFaceIDKey = UserDefaults.standard.bool(forKey: faceIDDefaultsKey)
            // Without the choice on record, a key still in the keychain is a leftover of an earlier
            // install (the keychain survives deleting the app, the choice does not): drop it.
            if !hasFaceIDKey, !isDemo { try? await keyStore.delete(signInAccountKey: connection.accountKey) }
            phase = setup.isSetUp ? .locked : .notSetUp
        } catch {
            failedText = PasswordsWords.problemText(error, orSay: String(localized: "Check your connection and try again."))
            phase = .failed
            return
        }
        #if DEBUG
            if isDemo {
                await startDemo()
                return
            }
        #endif
        if phase == .locked, hasFaceIDKey { await unlockWithFaceID() }
    }

    /// Where the encrypted copy of the items is kept: in the app group's
    /// container, where the AutoFill extension reads it.
    private static func cacheFileURL(signInAccountKey: String) -> URL {
        let fileManager = FileManager.default
        #if DEBUG
            if DemoServer.isRequested {
                // A copy of its own on each launch, so a demo never meets another's items.
                let directory = fileManager.temporaryDirectory.appendingPathComponent("passwords-demo-\(UUID().uuidString)", isDirectory: true)
                return PasswordsLocation.fileURL(groupContainer: directory, signInAccountKey: signInAccountKey)
            }
        #endif
        let group = Bundle.main.object(forInfoDictionaryKey: "TeaNodeAppGroup") as? String
        let container =
            group.flatMap { fileManager.containerURL(forSecurityApplicationGroupIdentifier: $0) }
            ?? fileManager.urls(for: .applicationSupportDirectory, in: .userDomainMask).first ?? fileManager.temporaryDirectory
        return PasswordsLocation.fileURL(groupContainer: container, signInAccountKey: signInAccountKey)
    }

    /// The copy of the items with the keys pinned for other people in the keychain group of Passwords,
    /// where the share extension and the widgets cannot reach them (the demo keeps them in memory).
    private static func makeCache(signInAccountKey: String) -> PasswordsCache {
        let fileURL = cacheFileURL(signInAccountKey: signInAccountKey)
        #if DEBUG
            if DemoServer.isRequested { return PasswordsCache(fileURL: fileURL, pinStore: PasswordsMemoryPinStore()) }
        #endif
        return PasswordsCache.forThisBundle(fileURL: fileURL, signInAccountKey: signInAccountKey)
    }

    private var faceIDDefaultsKey: String { "passwordsFaceID.\(connection.accountKey)" }

    private func setFaceIDKept(_ isKept: Bool) {
        UserDefaults.standard.set(isKept, forKey: faceIDDefaultsKey)
        hasFaceIDKey = isKept
    }

    private func requireSession() throws -> PasswordsSession {
        guard let session else { throw PasswordsError.locked }
        return session
    }

    // MARK: Setting up

    /// Sets Passwords up with a passphrase. The recovery key comes back on the
    /// model (`recoveryKeyText`, phase `recoveryKey`) and is shown once.
    func setUp(passphrase: String) async throws {
        let session = try requireSession()
        let recoveryKey = try await session.setUp(passphrase: passphrase, iterationCount: iterationCount)
        passphraseOfSetUp = passphrase
        recoveryKeyText = recoveryKey
        phase = .recoveryKey
    }

    /// Ends the setup once the person says they saved the recovery key, and
    /// opens Passwords. With `shouldRemember` the account key is also put behind
    /// Face ID, which needs the passphrase once more.
    func finishSetUp(shouldRemember: Bool) async {
        guard phase == .recoveryKey, let session else { return }
        let passphrase = passphraseOfSetUp
        passphraseOfSetUp = nil
        recoveryKeyText = nil
        if shouldRemember, let passphrase {
            if let unlocked = try? await session.unlock(passphrase: passphrase) { await keep(unlocked.accountKey) }
        }
        doneText = String(localized: "Passwords is set up and open.")
        await opened()
    }

    // MARK: Unlocking

    /// Opens Passwords with the passphrase. With `shouldRemember` the account key
    /// is also kept behind Face ID on this iPhone.
    func unlock(passphrase: String, shouldRemember: Bool) async throws {
        let session = try requireSession()
        let unlocked = try await session.unlock(passphrase: passphrase)
        if shouldRemember { await keep(unlocked.accountKey) }
        await opened()
    }

    /// Opens Passwords with the recovery key.
    func unlock(recoveryKey: String) async throws {
        let session = try requireSession()
        try await session.unlock(recoveryKey: recoveryKey)
        doneText = String(localized: "Opened with your recovery key. You can choose a new passphrase in Settings.")
        await opened()
    }

    /// Opens Passwords with the account key kept behind Face ID. Nothing is
    /// said when the person cancels Face ID.
    func unlockWithFaceID() async {
        guard let session, phase == .locked, !isOpeningWithFaceID else { return }
        isOpeningWithFaceID = true
        defer { isOpeningWithFaceID = false }
        do {
            guard
                let accountKey = try await keyStore.load(
                    signInAccountKey: connection.accountKey, promptReason: String(localized: "Open your passwords"))
            else {
                setFaceIDKept(false)
                return
            }
            try await session.open(accountKey: accountKey)
            await opened()
        } catch PasswordsKeyStoreError.canceled {
            return
        } catch PasswordsError.needsPassphrase {
            // Passwords was set up again elsewhere: the kept key opens nothing.
            await forgetKeptKey()
            problemText = PasswordsWords.problemText(PasswordsError.needsPassphrase, orSay: "")
        } catch let keyStoreError as PasswordsKeyStoreError {
            // The key is forgotten only when it is gone for good (the system dropped it because the
            // faces or fingers changed, or refused its authentication), so the screen asks for the
            // passphrase, which can put it behind Face ID again. A failure that passes (the keychain
            // not reachable for a moment, Face ID locked out after too many tries) keeps it, and
            // the person types the passphrase this once.
            if PasswordsKeychainKeyStore.isKeptKeyLost(by: keyStoreError) { await forgetKeptKey() }
            problemText = PasswordsWords.problemText(
                keyStoreError, orSay: String(localized: "Face ID could not open Passwords. Type your passphrase instead."))
        } catch {
            problemText = PasswordsWords.problemText(error, orSay: String(localized: "Face ID could not open Passwords. Type your passphrase instead."))
        }
    }

    /// Puts the account key behind Face ID. Needs the passphrase, as the
    /// account key is only ever read from it.
    func enableFaceID(passphrase: String) async throws {
        let session = try requireSession()
        let unlocked = try await session.unlock(passphrase: passphrase)
        try await keyStore.save(accountKey: unlocked.accountKey, signInAccountKey: connection.accountKey)
        setFaceIDKept(true)
        doneText = String(localized: "Face ID will open Passwords on this iPhone.")
    }

    func disableFaceID() async {
        await forgetKeptKey()
        doneText = String(localized: "Passwords will ask for your passphrase on this iPhone.")
    }

    private func keep(_ accountKey: Data) async {
        do {
            try await keyStore.save(accountKey: accountKey, signInAccountKey: connection.accountKey)
            setFaceIDKept(true)
        } catch {
            setFaceIDKept(false)
            problemText = PasswordsWords.problemText(
                error, orSay: String(localized: "Face ID could not be set up. You can still use your passphrase."))
        }
    }

    private func forgetKeptKey() async {
        try? await keyStore.delete(signInAccountKey: connection.accountKey)
        setFaceIDKept(false)
    }

    /// Passwords is open: the items are shown from the copy at once, then brought up to date.
    private func opened() async {
        phase = .unlocked
        if let session { lockAfterMinutes = await session.lockAfterMinutes }
        await reload()
        await sync()
    }

    // MARK: Locking

    func lock() async {
        await session?.lock()
        items = []
        deletedItems = []
        unreadableItemCount = 0
        forgetNamesAndMarks()
        ownSafetyCode = nil
        if phase == .unlocked { phase = .locked }
    }

    /// The phone locked: lock Passwords now, even when the lock time has not passed.
    private func lockBecausePhoneLocked() async {
        guard phase == .unlocked else { return }
        await lock()
    }

    /// Locks when the lock time has passed since Passwords was last used.
    func lockIfAskRequired() async {
        guard phase == .unlocked, let session else { return }
        if await session.isAskRequired() { await lock() }
    }

    /// The person is using Passwords (searching, looking), so the lock time runs from now.
    func noteUse() {
        guard phase == .unlocked, let session else { return }
        Task { await session.noteUse() }
    }

    /// The app is leaving the front (the app switcher, another app, the lock
    /// screen): keep the screen covered until it is back and the lock time has
    /// been checked, and lock now if the time is already up.
    func sceneDidResignActive() async {
        guard phase == .unlocked || phase == .recoveryKey else { return }
        isHoldingCover = true
        await lockIfAskRequired()
    }

    /// The app came to the front: lock if the time is up, and only then uncover
    /// the screen, so that no frame of items that should have locked is shown;
    /// then bring the items up to date.
    func sceneDidBecomeActive() async {
        await lockIfAskRequired()
        isHoldingCover = false
        if phase == .unlocked { await sync() }
    }

    // MARK: Reading

    /// Brings the copy up to date with the server and shows what changed.
    func sync(isFull: Bool = false) async {
        guard let session, phase == .unlocked, !isSyncing else { return }
        isSyncing = true
        defer { isSyncing = false }
        do {
            try await session.sync(isFull: isFull)
            lockAfterMinutes = await session.lockAfterMinutes
            await reload()
            // Items the server marked get a new key now, and people who set up Passwords since get theirs.
            var givenItemIds: [String] = []
            if !isDemo { givenItemIds = await session.giveNewKeysToMarkedItems() }
            if !givenItemIds.isEmpty { await reload() }
            itemIdsNeedingYou = await session.itemIdsNeedingYou
            itemIdsNeedingAttention = await session.itemIdsNeedingAttention
            await giveWaitingRecipientsTheirKeys()
        } catch {
            await handleSyncFailure(error)
        }
    }

    private func handleSyncFailure(_ error: Error) async {
        if let passwordsError = error as? PasswordsError {
            switch passwordsError {
            case .notSetUp:
                await forgetKeptKey()
                await forgetSuggestions()
                markClosed(to: .notSetUp)
                return
            case .needsPassphrase:
                await forgetKeptKey()
                await forgetSuggestions()
                markClosed(to: .locked)
                problemText = PasswordsWords.problemText(passwordsError, orSay: "")
                return
            default:
                break
            }
        }
        problemText = PasswordsWords.problemText(
            error, orSay: String(localized: "Passwords could not be brought up to date. What this iPhone has is shown."))
    }

    /// Takes the logins and passkeys out of the system's credential store (the suggestions above the
    /// keyboard), which would otherwise keep offering what this iPhone just forgot.
    private func forgetSuggestions() async {
        if !isDemo { await PasswordsIdentityStore.removeAll() }
    }

    private func markClosed(to closedPhase: PasswordsPhase) {
        items = []
        deletedItems = []
        unreadableItemCount = 0
        forgetNamesAndMarks()
        ownSafetyCode = nil
        phase = closedPhase
    }

    /// Drops what names people or items (the notices and the marks on items), so a locked screen
    /// holds none of them in memory.
    private func forgetNamesAndMarks() {
        keyChanges = []
        circleShareNotices = []
        itemIdsNeedingYou = []
        itemIdsNeedingNewKey = []
        itemIdsNeedingAttention = []
        versionsByItemId = [:]
        sharingProgress = nil
        namesWithTooMuchShared = []
        hasMoreSharesWaiting = false
    }

    /// Opens the items of the copy for the lists, and tells the system's
    /// credential store what to suggest.
    private func reload() async {
        guard let session else { return }
        do {
            let opened = try await session.list()
            items = opened.items.filter { !$0.isDeleted }
            deletedItems = opened.items.filter { $0.isDeleted }.sorted { ($0.deletedAt ?? .distantPast) > ($1.deletedAt ?? .distantPast) }
            unreadableItemCount = opened.unreadableItemIds.count
            keyChanges = await session.keyChanges()
            circleShareNotices = await session.circleShareNotices()
            itemIdsNeedingYou = await session.itemIdsNeedingYou
            itemIdsNeedingNewKey = await session.itemIdsNeedingNewKey
            itemIdsNeedingAttention = await session.itemIdsNeedingAttention
            ownSafetyCode = try? await session.ownSafetyCode()
        } catch {
            report(error, orSay: String(localized: "Your items could not be opened."))
            return
        }
        guard !isDemo, let records = try? await session.identityRecords() else { return }
        await PasswordsIdentityStore.replace(with: records)
    }

    /// The people who set up Passwords after an item was shared with them get the item's key now,
    /// sealed for the key the server gives, but only for items this iPhone remembers and people it
    /// sealed the item for before. The rest is listed in `itemIdsNeedingYou` for the person to
    /// finish. Nothing is asked about a key: one this iPhone has not seen is recorded as theirs, and
    /// one that differs from the known key is accepted with a notice (`keyChanges`).
    private func giveWaitingRecipientsTheirKeys() async {
        guard let session, !isDemo else { return }
        guard let completed = try? await session.completeWaitingShares() else { return }
        namesWithTooMuchShared = completed.tooMuchSharedNames
        hasMoreSharesWaiting = completed.hasMoreWaiting
        keyChanges = await session.keyChanges()
        circleShareNotices = await session.circleShareNotices()
        itemIdsNeedingYou = await session.itemIdsNeedingYou
        if completed.addedKeyCount > 0 {
            let givenCount = completed.addedKeyCount
            doneText = String(localized: "Gave \(givenCount) people access to the items you shared, now that they set up Passwords.")
        }
    }

    /// Says why something failed on the screen behind any sheet, and notes a
    /// session that turned out to be closed.
    func report(_ error: Error, orSay fallback: String) {
        if let passwordsError = error as? PasswordsError, passwordsError == .locked, phase == .unlocked {
            markClosed(to: .locked)
        }
        problemText = PasswordsWords.problemText(error, orSay: fallback)
    }

    // MARK: Copying

    /// Copies a value to the clipboard, where it stays for a minute and a half
    /// and only on this iPhone, and says so.
    func copy(_ text: String, fieldName: String) {
        PasswordsClipboard.copy(text)
        doneText = PasswordsWords.copiedText(fieldName)
        noteUse()
    }

    // MARK: Changing items

    /// Saves a new item, or a change to one the person owns.
    @discardableResult
    func save(fields: PasswordsItemFields, id: String? = nil, baseRevision: Int64? = nil) async throws -> PasswordItem {
        let session = try requireSession()
        // An item the server marked is given a new key as it is saved.
        let wasMarked = id.map { itemIdsNeedingNewKey.contains($0) } ?? false
        do {
            let saved = try await session.save(fields: fields, id: id, baseRevision: baseRevision)
            await reload()
            if wasMarked { await giveWaitingRecipientsTheirKeys() }
            return saved
        } catch {
            if let passwordsError = error as? PasswordsError, passwordsError == .itemChangedOnAnotherDevice { await sync() }
            throw error
        }
    }

    func delete(_ item: PasswordItem) async throws {
        let session = try requireSession()
        _ = try await session.delete(id: item.id)
        await reload()
    }

    func restore(_ item: PasswordItem) async throws {
        let session = try requireSession()
        _ = try await session.restore(id: item.id)
        await reload()
        await giveWaitingRecipientsTheirKeys()
    }

    /// Deletes an item from Recently deleted for good. It cannot be brought back.
    func purge(_ item: PasswordItem) async throws {
        let session = try requireSession()
        try await session.purge(id: item.id)
        await reload()
    }

    func setUsableByAgent(_ item: PasswordItem, isUsable: Bool) async throws {
        let session = try requireSession()
        do {
            _ = try await session.setUsableByAgent(itemId: item.id, isUsable: isUsable)
        } catch {
            // The agent was stopped but the new key could not be made yet: the item says so.
            await reload()
            throw error
        }
        await reload()
        // Turning the agent off gives the item a new key; the people who had no key before get theirs now.
        if !isUsable { await giveWaitingRecipientsTheirKeys() }
    }

    /// What finishing the sharing of these items would do, for the question asked first. The copy is
    /// brought up to date first, so the plan names the people the server lists now; what the person
    /// confirms is then exactly what is sealed.
    func finishPlan(itemIds: [String]) async -> PasswordsFinishPlan {
        guard let session else { return PasswordsFinishPlan(itemCount: 0, people: [], newKeyItemCount: 0, agentItemCount: 0) }
        await sync(isFull: true)
        return await session.finishPlan(itemIds: itemIds)
    }

    /// Reads the earlier versions of an item the person owns from the server and opens them.
    /// False when they could not be read (`problemText` says why).
    @discardableResult
    func loadVersions(of item: PasswordItem) async -> Bool {
        guard let session else { return false }
        do {
            versionsByItemId[item.id] = try await session.versions(of: item.id)
            noteUse()
            return true
        } catch {
            report(error, orSay: String(localized: "The earlier versions could not be read."))
            return false
        }
    }

    /// Puts an earlier version's content back as the item's own. Who it is shared with stays as it is
    /// now, and the item is given a new key at once.
    func restoreVersion(_ version: PasswordItemVersion, of item: PasswordItem) async throws {
        let session = try requireSession()
        do {
            _ = try await session.restoreVersion(itemId: item.id, versionId: version.id)
        } catch {
            if let passwordsError = error as? PasswordsError, passwordsError == .itemChangedOnAnotherDevice { await sync() }
            throw error
        }
        await reload()
        await giveWaitingRecipientsTheirKeys()
    }

    /// Drops the earlier versions read for an item, so the next time the list opens it reads them again.
    func forgetVersions(ofItemId itemId: String) {
        versionsByItemId[itemId] = nil
    }

    /// Finishes the sharing of an item the person opened, as the plan they confirmed says: gives it a
    /// new key when it needs one, and a key to the people the plan names whom the item's own record
    /// reaches, and nobody else.
    func finishSharing(_ item: PasswordItem, plan: PasswordsFinishPlan) async {
        guard let session else { return }
        do {
            try await session.finishSharing(itemId: item.id, plan: plan)
            doneText = String(localized: "Finished sharing \(item.name).")
            await reload()
        } catch {
            report(error, orSay: String(localized: "The sharing could not be finished."))
        }
    }

    /// Finishes the sharing of the items the person confirmed (`ask.itemIds`, with the people and
    /// the agent in `ask.plan`), one after the other, on the person's one tap. `sharingProgress`
    /// says how far it has got, and a count is said when it is done. The server lists at most 2000
    /// waiting shares at once, so the shares that were not listed show once these are finished
    /// (`hasMoreSharesWaiting`).
    func finishSharingEveryItem(_ ask: PasswordsFinishAsk) async {
        guard let session, sharingProgress == nil else { return }
        let waitingItems = items.filter { ask.itemIds.contains($0.id) }
        guard !waitingItems.isEmpty else { return }
        let totalCount = waitingItems.count
        var finishedCount = 0
        var lastError: Error?
        sharingProgress = PasswordsSharingProgress(doneCount: 0, totalCount: totalCount)
        for (index, item) in waitingItems.enumerated() {
            do {
                try await session.finishSharing(itemId: item.id, plan: ask.plan)
                finishedCount += 1
            } catch {
                lastError = error
                // A locked session fails every item the same way: stop.
                if let passwordsError = error as? PasswordsError, passwordsError == .locked { break }
            }
            sharingProgress = PasswordsSharingProgress(doneCount: index + 1, totalCount: totalCount)
        }
        sharingProgress = nil
        await reload()
        await giveWaitingRecipientsTheirKeys()
        if finishedCount > 0 { doneText = String(localized: "Finished sharing \(finishedCount) items.") }
        if let lastError {
            report(lastError, orSay: String(localized: "Sharing could not be finished for some items. Try again."))
        }
    }

    /// Hides the notice that somebody got an item through a circle.
    func dismissCircleShareNotice(_ notice: PasswordsCircleShareNotice) async {
        guard let session else { return }
        do {
            try await session.dismissCircleShareNotice(itemId: notice.itemId, recipientUserId: notice.recipientUserId)
            circleShareNotices = await session.circleShareNotices()
        } catch {
            report(error, orSay: String(localized: "The notice could not be dismissed."))
        }
    }

    /// Tries again to give an item a new key, after a removal whose re-key failed.
    func giveNewKey(_ item: PasswordItem) async {
        guard let session else { return }
        do {
            _ = try await session.giveNewKey(itemId: item.id)
            doneText = String(localized: "The item was given a new key.")
            await reload()
            await giveWaitingRecipientsTheirKeys()
        } catch {
            report(error, orSay: String(localized: "The item could not be given a new key."))
        }
    }

    // MARK: Sharing

    func recipients(for target: ShareTarget) async throws -> [PasswordRecipient] {
        let session = try requireSession()
        return try await session.recipients(granteeUserId: target.granteeUserId, granteeCircleId: target.granteeCircleId)
    }

    /// The people an item's existing share reaches, marked with what this iPhone knows of each key.
    func recipients(of share: PasswordShareView) async throws -> [PasswordRecipient] {
        let session = try requireSession()
        return await session.recipients(of: share.recipients, circleId: share.granteeCircleId)
    }

    /// Saves who the item is shared with. A key is sealed at once for each new recipient who has
    /// one, for the key the server gives, and only for somebody the item's own sealed record
    /// reaches. A first-seen key is recorded as the person's; a key that differs from the known
    /// one is accepted and shows as a notice (`keyChanges`).
    func setShares(of item: PasswordItem, grantees: [PasswordShareGranteeInput], recipients: [PasswordRecipient]) async throws {
        let session = try requireSession()
        do {
            _ = try await session.setShares(itemId: item.id, grantees: grantees, recipients: recipients)
        } catch {
            // A removal that went through but could not be given a new key still changed the item.
            await reload()
            throw error
        }
        await reload()
        await giveWaitingRecipientsTheirKeys()
    }

    /// Hides the notice of a changed key. The key stays accepted.
    func dismissKeyChange(_ change: PasswordsKeyChange) async {
        guard let session else { return }
        do {
            try await session.dismissKeyChange(userId: change.userId)
            keyChanges = await session.keyChanges()
        } catch {
            report(error, orSay: String(localized: "The notice could not be dismissed."))
        }
    }

    /// Marks a key as compared, after the person read the safety codes with its owner. It gates nothing.
    func markCompared(userId otherUserId: String, publicKey: String) async {
        guard let session else { return }
        do {
            try await session.markCompared(userId: otherUserId, publicKey: publicKey)
            keyChanges = await session.keyChanges()
            await reload()
            doneText = String(localized: "Codes compared.")
        } catch {
            report(error, orSay: String(localized: "That could not be saved."))
        }
    }

    // MARK: Settings

    func changePassphrase(currentPassphrase: String, newPassphrase: String) async throws {
        let session = try requireSession()
        try await session.changePassphrase(currentPassphrase: currentPassphrase, newPassphrase: newPassphrase, iterationCount: iterationCount)
    }

    /// Changes the passphrase for a person who forgot the old one, proving it with the recovery key.
    func changePassphrase(recoveryKey: String, newPassphrase: String) async throws {
        let session = try requireSession()
        try await session.changePassphrase(recoveryKey: recoveryKey, newPassphrase: newPassphrase, iterationCount: iterationCount)
    }

    /// Makes a new recovery key (the old one stops working) and returns it as the person writes it down.
    func replaceRecoveryKey(passphrase: String) async throws -> String {
        let session = try requireSession()
        return try await session.replaceRecoveryKey(passphrase: passphrase)
    }

    func updateLockAfterMinutes(_ minutes: Int) async throws {
        let session = try requireSession()
        lockAfterMinutes = try await session.updateLockAfterMinutes(minutes)
    }

    /// Asks for Face ID, or the passcode when Face ID is not set up or does not
    /// recognize the person, before something that must be them: true when they
    /// passed. Only a phone with no passcode at all cannot ask, and then the
    /// confirmation dialog is the only check.
    func confirmItIsThePerson(reason: String) async -> Bool {
        guard PasswordsKeychainKeyStore.canConfirmDeviceOwner, !isDemo else { return true }
        return await PasswordsKeychainKeyStore.confirmDeviceOwner(reason: reason)
    }

    /// Erases Passwords on the server and on this iPhone. Works while locked,
    /// which is the way out for a person who lost both the passphrase and
    /// the recovery key.
    func erase() async throws {
        let session = try requireSession()
        try await session.erase()
        await forgetKeptKey()
        await forgetSuggestions()
        markClosed(to: .notSetUp)
        doneText = String(localized: "Passwords was erased.")
    }

    // MARK: Demo

    #if DEBUG
        /// Opens the demo Passwords for the screen a screenshot names:
        /// `passwords-unlock` and the setup screens stay closed, the others open.
        private func startDemo() async {
            switch DemoServer.screen {
            case "passwords-unlock":
                hasFaceIDKey = true
            case "passwords-recovery-key":
                try? await setUp(passphrase: PasswordsDemoStore.passphrase)
            case "passwords-setup", "passwords-intro":
                break
            case "passwords-share":
                try? await unlock(passphrase: PasswordsDemoStore.passphrase, shouldRemember: false)
                await makeDemoRecipientKeyChange()
            default:
                try? await unlock(passphrase: PasswordsDemoStore.passphrase, shouldRemember: false)
            }
        }

        /// The share screen's shot shows a key that changed: the owner shared the item
        /// with the circle, so this iPhone knows Alex's key, and Alex has since set Passwords
        /// up again, so the key is another one and a notice shows. Goes through the real
        /// calls, so this iPhone really knows the old key and the server has the new.
        private func makeDemoRecipientKeyChange() async {
            let alexUserId = "alex"
            guard let item = item(id: PasswordsDemoStore.sharedByMeItemId), let share = item.shares.first,
                let granteeCircleId = share.granteeCircleId
            else { return }
            let grantees = item.shares.map {
                PasswordShareGranteeInput(granteeUserId: $0.granteeUserId, granteeCircleId: $0.granteeCircleId)
            }
            PasswordsDemoStore.shared.changeKey(of: alexUserId)
            guard let recipients = try? await recipients(for: .circle(granteeCircleId)),
                (try? await setShares(of: item, grantees: grantees, recipients: recipients)) != nil
            else { return }
            PasswordsDemoStore.shared.changeKey(of: alexUserId)
            // Alex's items wait for the new key; finishing the sharing seals for it and notes the change.
            if let session {
                let plan = await session.finishPlan(itemIds: [item.id])
                _ = try? await session.finishSharing(itemId: item.id, plan: plan)
            }
            await sync(isFull: true)
        }
    #endif
}
