import Foundation
import Observation
import TeaNodeKit

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
    /// The people who shared items with this person whose key is not the one this
    /// iPhone knew: their items stay closed until the person trusts the new key.
    private(set) var ownerKeyChanges: [PasswordsOwnerKeyChange] = []
    /// Waiting recipients of the person's items whose key this iPhone has not seen
    /// or that changed: nothing is sealed for them until the person confirms the key.
    private(set) var waitingConfirmations: [PasswordWaitingShare] = []
    private(set) var isSyncing = false
    /// Whether the account key waits behind Face ID on this iPhone.
    private(set) var hasFaceIDKey = false
    private(set) var lockAfterMinutes = PasswordsSession.defaultLockAfterMinutes
    private(set) var ownUserId: String?
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
            keyStore = PasswordsKeychainKeyStore()
            iterationCount = PasswordsCrypto.passphraseIterationCount
        }
        isDemo = isDemoRequested
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
            let newSession = PasswordsSession(
                api: connection, cache: PasswordsCache(fileURL: Self.cacheFileURL(signInAccountKey: connection.accountKey)), userId: userId)
            let setup = try await newSession.readSetup()
            session = newSession
            lockAfterMinutes = setup.lockAfterMinutes
            hasFaceIDKey = UserDefaults.standard.bool(forKey: faceIDDefaultsKey)
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
        ownerKeyChanges = []
        waitingConfirmations = []
        if phase == .unlocked { phase = .locked }
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

    /// The app came to the front: lock if the time is up, else bring the items up to date.
    func sceneDidBecomeActive() async {
        guard phase == .unlocked else { return }
        await lockIfAskRequired()
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
                markClosed(to: .notSetUp)
                return
            case .needsPassphrase:
                await forgetKeptKey()
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

    private func markClosed(to closedPhase: PasswordsPhase) {
        items = []
        deletedItems = []
        unreadableItemCount = 0
        ownerKeyChanges = []
        waitingConfirmations = []
        phase = closedPhase
    }

    /// Opens the items of the copy for the lists, and tells the system's
    /// credential store what to suggest.
    private func reload() async {
        guard let session else { return }
        do {
            let opened = try await session.list()
            items = opened.items.filter { !$0.isDeleted }
            deletedItems = opened.items.filter { $0.isDeleted }.sorted { ($0.deletedAt ?? .distantPast) > ($1.deletedAt ?? .distantPast) }
            unreadableItemCount = opened.unreadableReasons.values.filter { $0 == .cannotOpen }.count
            ownerKeyChanges = await session.ownerKeyChanges()
        } catch {
            report(error, orSay: String(localized: "Your items could not be opened."))
            return
        }
        guard !isDemo, let records = try? await session.identityRecords() else { return }
        await PasswordsIdentityStore.replace(with: records)
    }

    /// The people who set up Passwords after an item was shared with them get
    /// the item's key now, when this iPhone already knows their key. A key it
    /// has not seen, or one that changed, is listed in `waitingConfirmations`
    /// for the person to confirm first.
    private func giveWaitingRecipientsTheirKeys() async {
        guard let session, !isDemo else { return }
        guard let completed = try? await session.completeWaitingShares() else { return }
        waitingConfirmations = completed.waitingForConfirmation
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
        do {
            let saved = try await session.save(fields: fields, id: id, baseRevision: baseRevision)
            await reload()
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
    }

    func setUsableByAgent(_ item: PasswordItem, isUsable: Bool) async throws {
        let session = try requireSession()
        _ = try await session.setUsableByAgent(itemId: item.id, isUsable: isUsable)
        await reload()
    }

    // MARK: Sharing

    func recipients(for target: ShareTarget) async throws -> [PasswordRecipient] {
        let session = try requireSession()
        return try await session.recipients(granteeUserId: target.granteeUserId, granteeCircleId: target.granteeCircleId)
    }

    /// The people an item's existing share reaches, marked with what this iPhone knows of each key.
    func recipients(of share: PasswordShareView) async throws -> [PasswordRecipient] {
        let session = try requireSession()
        return await session.recipients(of: share.recipients)
    }

    /// Saves who the item is shared with. A key is sealed only for a new
    /// recipient whose key this iPhone knows or whose user id is in `confirmedUserIds`.
    func setShares(
        of item: PasswordItem, grantees: [PasswordShareGranteeInput], recipients: [PasswordRecipient], confirmedUserIds: [String]
    ) async throws {
        let session = try requireSession()
        _ = try await session.setShares(
            itemId: item.id, grantees: grantees, recipients: recipients, confirmedUserIds: confirmedUserIds)
        await reload()
    }

    /// Seals one waiting share after the person confirmed the recipient's key.
    func confirmWaitingShare(_ waiting: PasswordWaitingShare) async {
        guard let session else { return }
        do {
            try await session.confirmWaitingShare(waiting)
            waitingConfirmations.removeAll { $0 == waiting }
            doneText = String(localized: "\(waiting.recipient.name) can open the item now.")
        } catch {
            report(error, orSay: String(localized: "The key could not be given."))
        }
    }

    /// Accepts the new key of a person who shared items, after the person compared the codes with them.
    func trustOwnerKey(of change: PasswordsOwnerKeyChange) async {
        guard let session else { return }
        do {
            try await session.trustOwnerKey(userId: change.userId)
            await reload()
            doneText = String(localized: "You trust the new key of \(change.ownerName).")
        } catch {
            report(error, orSay: String(localized: "The new key could not be trusted."))
        }
    }

    func waitingShares() async throws -> [PasswordWaitingShare] {
        let session = try requireSession()
        return try await session.waitingShares()
    }

    // MARK: Settings

    func changePassphrase(currentPassphrase: String, newPassphrase: String) async throws {
        let session = try requireSession()
        try await session.changePassphrase(currentPassphrase: currentPassphrase, newPassphrase: newPassphrase, iterationCount: iterationCount)
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

    /// Erases Passwords on the server and on this iPhone. Works while locked,
    /// which is the way out for a person who lost both the passphrase and
    /// the recovery key.
    func erase() async throws {
        let session = try requireSession()
        try await session.erase()
        await forgetKeptKey()
        if !isDemo { await PasswordsIdentityStore.removeAll() }
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
            default:
                try? await unlock(passphrase: PasswordsDemoStore.passphrase, shouldRemember: false)
            }
        }
    #endif
}
