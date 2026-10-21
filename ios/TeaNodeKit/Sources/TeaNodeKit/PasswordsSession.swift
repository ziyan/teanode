import Foundation

// The Passwords feature as the phone uses it: set up, unlock, read, save,
// share, let the agent use an item. This is the only place that joins the
// encryption (PasswordsCrypto.swift), the server (PasswordsAPI.swift) and the
// phone's copy (PasswordsCache.swift). Nothing here draws anything; the
// screens and the AutoFill extension are built on it.
//
// The account key is the one secret that opens everything. It is made on the
// device, sealed twice (by the passphrase and by the recovery key) and sent
// sealed; it comes back only to someone who proves one of the two. A session
// holds it in memory while unlocked, and the app keeps a copy behind Face ID
// (PasswordsKeyStore.swift). The AutoFill extension has no session: it reads
// the cache with PasswordsOpener and never touches the network.

// MARK: Errors

/// PasswordsError is why a Passwords call failed, where the person has
/// something to do about it. Anything else comes through as it was thrown
/// (`TeaNodeError` for the server, `PasswordsCryptoError` for damaged data).
public enum PasswordsError: Error, Equatable, Sendable, LocalizedError {
    /// The session has no account key: unlock first.
    case locked
    /// The cache or the kept key does not open Passwords any more (another
    /// person, or Passwords was set up again): the passphrase is needed.
    case needsPassphrase
    case notSetUp
    case alreadySetUp
    case wrongPassphrase
    case wrongRecoveryKey
    /// The item changed since it was read: read it again, then redo the change.
    case itemChangedOnAnotherDevice
    case itemNotFound
    /// Only the person who owns an item changes it, shares it, or lets the agent use it.
    case onlyOwnerCanChangeItem
    /// An item's kind is fixed when it is made.
    case itemKindCannotChange
    case cardCannotBeUsedByAgent
    /// A person's key changed between the moment the device read it and the
    /// moment it sealed an item's key for it: read their key again.
    case recipientKeyChangedSinceRead
    /// The server counted too many tries (wrong passphrases) and asks for a wait.
    case tooManyAttempts
    case unknownItemKind(String)
    /// The server sent a key or a value that is not the shape it should be.
    case invalidServerAnswer
    /// A shared item's owner has a different public key than the one this device
    /// knows for them. Nothing is opened with the new key until the person has
    /// compared the two safety codes and trusted it (`PasswordsSession.trustOwnerKey`).
    case ownerKeyChanged(userId: String, oldSafetyCode: String, newSafetyCode: String)
    /// This device has never seen the owner's key, so a shared item's owner cannot be checked
    /// yet. The app records the key the first time it syncs the item; the AutoFill extension skips the item until then.
    case ownerKeyUnknown(userId: String)
    /// Somebody was removed from an item's shares, but the item could not be given a new key
    /// afterwards. The removal stands; `giveNewKey(itemId:)` tries again, and so does the next save.
    case itemNeedsNewKey(itemId: String)

    public var errorDescription: String? {
        switch self {
        case .locked: return "Passwords is locked."
        case .needsPassphrase: return "Enter your Passwords passphrase."
        case .notSetUp: return "Passwords is not set up."
        case .alreadySetUp: return "Passwords is already set up."
        case .wrongPassphrase: return "That passphrase is not right."
        case .wrongRecoveryKey: return "That recovery key is not right."
        case .itemChangedOnAnotherDevice: return "This item changed on another device. Open it again."
        case .itemNotFound: return "That item is not here any more."
        case .onlyOwnerCanChangeItem: return "Only the person who owns an item can change it."
        case .itemKindCannotChange: return "An item cannot change from a login to a note or a card."
        case .cardCannotBeUsedByAgent: return "A card cannot be used by the agent."
        case .recipientKeyChangedSinceRead: return "That person's key changed since it was read. Read their key again."
        case .tooManyAttempts: return "Too many attempts. Wait a while and try again."
        case .unknownItemKind: return "This item is of a kind this version cannot open. Update the app."
        case .invalidServerAnswer: return "The server sent something this app cannot read."
        case .ownerKeyChanged: return "The owner of this item has a different key than before."
        case .ownerKeyUnknown: return "This device has not seen the owner of this item before."
        case .itemNeedsNewKey: return "Removed. The item could not be given a new key."
        }
    }

    /// The sentences the server refuses with, which have a case of their own.
    /// The server sends no code beside its sentence, so a refusal is told by
    /// the sentence (internal/api/v1api/apigraph/passwords.go and
    /// internal/api/api.go), found anywhere in the reason.
    private static let serverSentences: [(sentence: String, error: PasswordsError)] = [
        ("That passphrase is not right.", .wrongPassphrase),
        ("That recovery key is not right.", .wrongRecoveryKey),
        ("This item changed on another device.", .itemChangedOnAnotherDevice),
        ("That person's key changed since it was read.", .recipientKeyChangedSinceRead),
        ("A card cannot be used by the agent.", .cardCannotBeUsedByAgent),
        ("Passwords is already set up.", .alreadySetUp),
        ("Passwords is not set up yet.", .notSetUp),
        ("too many attempts, try again later", .tooManyAttempts),
    ]

    /// The server's refusals that have a case of their own; any other error is returned as it is.
    static func mapped(_ error: Error) -> Error {
        guard let teaNodeError = error as? TeaNodeError, case .refused(let reason) = teaNodeError else { return error }
        for known in serverSentences where reason.contains(known.sentence) { return known.error }
        return error
    }
}

// MARK: Opened items

/// PasswordItem is an item opened: its fields in the clear, and what the
/// server says about it. Nothing here is sealed, so keep it in memory only.
public struct PasswordItem: Identifiable, Equatable, Sendable {
    public let id: String
    public let fields: PasswordsItemFields
    public let ownerUserId: String
    public let ownerName: String
    /// Whether somebody else owns it and shared it with the person.
    public let isSharedWithMe: Bool
    public let revision: Int64
    public let isUsableByAgent: Bool
    public let lastUsedByAgentAt: Date?
    /// When the owner deleted it. A deleted item is kept 30 days.
    public let deletedAt: Date?
    public let createdAt: Date?
    public let updatedAt: Date?
    /// The owner's shares; empty for a shared item.
    public let shares: [PasswordShareView]

    public var itemKind: PasswordsItemKind { fields.itemKind }
    public var name: String { fields.name }
    public var isDeleted: Bool { deletedAt != nil }
}

/// PasswordsUnreadableReason is why one item did not open, where the person has
/// something to do about it.
public enum PasswordsUnreadableReason: Hashable, Sendable {
    /// The owner's key is not the one this device knows: compare both codes with the owner, then trust it.
    case ownerKeyChanged(userId: String, oldSafetyCode: String, newSafetyCode: String)
    /// This device has not seen the owner's key yet.
    case ownerKeyUnknown(userId: String)
    /// Damaged, or of a kind this build does not know.
    case cannotOpen

    static func reason(for error: Error) -> PasswordsUnreadableReason {
        switch error as? PasswordsError {
        case .ownerKeyChanged(let userId, let oldSafetyCode, let newSafetyCode):
            return .ownerKeyChanged(userId: userId, oldSafetyCode: oldSafetyCode, newSafetyCode: newSafetyCode)
        case .ownerKeyUnknown(let userId):
            return .ownerKeyUnknown(userId: userId)
        default:
            return .cannotOpen
        }
    }
}

/// PasswordsOpenedItems is every item that opened, and the ids of those that
/// did not (damaged, of a kind this build does not know, or shared by an owner
/// whose key changed), with the reason for each.
public struct PasswordsOpenedItems: Equatable, Sendable {
    public var items: [PasswordItem]
    public var unreadableItemIds: [String]
    /// Why each item in `unreadableItemIds` did not open.
    public var unreadableReasons: [String: PasswordsUnreadableReason] = [:]
}

/// PasswordsKeyTrust is what a device knows about a recipient's public key.
public enum PasswordsKeyTrust: Equatable, Hashable, Sendable {
    /// The key is the one the person already confirmed on this device by its safety code.
    case known
    /// The person has not confirmed a key for this recipient on this device (a key only seen on an
    /// incoming item does not count): the owner confirms its safety code before sealing.
    case new
    /// The recipient's key is not the one the person confirmed: both safety codes are shown and nothing is sealed until the owner trusts it.
    case changed
    /// The person has not set up Passwords, so there is no key.
    case noKey
}

/// PasswordRecipient is a person an item reaches or could reach, with the
/// code the owner compares with them to be sure of the key.
public struct PasswordRecipient: Identifiable, Hashable, Sendable {
    public let userId: String
    public let name: String
    public let username: String
    /// The person's public key (65 bytes); nil when they have not set up Passwords.
    public let publicKey: Data?
    /// For example "0412 9930 0007 5121 8800"; nil without a public key.
    public let safetyCode: String?
    /// Whether the item already has a key for this public key.
    public let hasKey: Bool
    /// Whether the key is the one this device knows for the person, a first sight, or a change.
    public let keyTrust: PasswordsKeyTrust
    /// The safety code of the key this device knew before, when `keyTrust` is `.changed`.
    public let knownSafetyCode: String?

    public var id: String { userId }

    /// `knownPublicKey` is the key the person confirmed on this device for this person (standard base64), if any.
    public init(view: PasswordRecipientView, knownPublicKey: String? = nil) {
        userId = view.userId
        name = view.name
        username = view.username
        hasKey = view.hasKey
        if let text = view.publicKey, let data = Data(base64Encoded: text), PasswordsCrypto.isPublicKeyValid(data),
            let code = try? PasswordsCrypto.safetyCode(publicKey: data)
        {
            publicKey = data
            safetyCode = code
            if let knownPublicKey {
                keyTrust = knownPublicKey == text ? .known : .changed
                knownSafetyCode = knownPublicKey == text ? nil : PasswordsOpener.safetyCodeText(publicKey: knownPublicKey)
            } else {
                keyTrust = .new
                knownSafetyCode = nil
            }
        } else {
            publicKey = nil
            safetyCode = nil
            keyTrust = .noKey
            knownSafetyCode = nil
        }
    }
}

/// PasswordWaitingShare is an item of the person's that a recipient cannot
/// open yet, because nobody has sealed its key for their current public key.
public struct PasswordWaitingShare: Hashable, Sendable {
    public let itemId: String
    /// The item's name, if the item is in the phone's copy.
    public let itemName: String?
    public let recipient: PasswordRecipient
}

/// PasswordsOwnerKeyChange is a person whose shared items this device cannot
/// open because their public key is not the one it knew: the screen shows both
/// safety codes and a Trust button (`PasswordsSession.trustOwnerKey`).
public struct PasswordsOwnerKeyChange: Identifiable, Hashable, Sendable {
    public let userId: String
    public let ownerName: String
    public let oldSafetyCode: String
    public let newSafetyCode: String
    /// The key the owner has now, standard base64: what `trustOwnerKey` is given,
    /// so it trusts the key whose code the person compared and no other.
    public let newPublicKey: String
    /// How many of their shared items are held back.
    public let itemCount: Int

    public var id: String { userId }
}

/// PasswordsCompletedShares is what `completeWaitingShares` did: the keys it
/// sealed, and the waiting recipients whose key it left alone because the
/// device does not know it yet or it changed. The screen shows those with
/// their safety codes and calls `confirmWaitingShare` for each one the person confirms.
public struct PasswordsCompletedShares: Equatable, Sendable {
    public let addedKeyCount: Int
    public let waitingForConfirmation: [PasswordWaitingShare]
}

/// PasswordsUnlocked is what a right passphrase or recovery key opened.
public struct PasswordsUnlocked: Equatable, Sendable {
    /// Keep this behind Face ID (PasswordsKeyStore) to open Passwords without the passphrase next time.
    public let accountKey: Data
    public let keyPair: PasswordsKeyPair
}

// MARK: Opening without the network

/// PasswordsOpener opens items of the cache with the account key. It is all the
/// AutoFill extension needs: it reads the cache file, takes the account key
/// from the keychain, and opens only the items it is asked about. It does no
/// slow key derivation and no networking.
public struct PasswordsOpener: Sendable {
    public let userId: String
    public let keyPair: PasswordsKeyPair
    let accountKey: Data
    /// The keys this device saw when each owner's first shared item arrived, by user id
    /// (standard base64). A shared item opens only with the owner key in here or in
    /// `confirmedPublicKeys`. They never allow sealing an item key.
    public var seenPublicKeys: [String: String]
    /// The keys the person confirmed by their safety codes, by user id. Only these allow
    /// sealing an item key for somebody without asking.
    public var confirmedPublicKeys: [String: String]

    public init(
        accountKey: Data, userId: String, keyPair: PasswordsKeyPair, seenPublicKeys: [String: String] = [:],
        confirmedPublicKeys: [String: String] = [:]
    ) {
        self.accountKey = accountKey
        self.userId = userId
        self.keyPair = keyPair
        self.seenPublicKeys = seenPublicKeys
        self.confirmedPublicKeys = confirmedPublicKeys
    }

    /// The key for opening and checking a person's shared items: the confirmed one, else the one first seen.
    func ownerPublicKeyText(forUserId otherUserId: String) -> String? {
        confirmedPublicKeys[otherUserId] ?? seenPublicKeys[otherUserId]
    }

    /// Opens the person's key pair with the account key. When
    /// `expectedPublicKey` (standard base64) is given, a key pair that does not
    /// match it is refused, which is what a wrong account key looks like if the
    /// seal somehow opened.
    public init(
        accountKey: Data, userId: String, privateKeyByAccountKey: String, expectedPublicKey: String? = nil,
        seenPublicKeys: [String: String] = [:], confirmedPublicKeys: [String: String] = [:]
    ) throws {
        let sealed = try PasswordsSealedValue(jsonText: privateKeyByAccountKey)
        let privateKey = try PasswordsCrypto.open(sealed, key: accountKey, associatedData: PasswordsCrypto.privateKeyAssociatedData)
        let keyPair = try PasswordsCrypto.keyPair(privateKey: privateKey)
        if let expectedPublicKey, keyPair.publicKey.base64EncodedString() != expectedPublicKey {
            throw PasswordsCryptoError.invalidKeyPair
        }
        self.init(
            accountKey: accountKey, userId: userId, keyPair: keyPair, seenPublicKeys: seenPublicKeys,
            confirmedPublicKeys: confirmedPublicKeys)
    }

    /// Opens the cache's key pair, and keeps the cache's pinned keys to check shared items against.
    public init(accountKey: Data, contents: PasswordsCacheContents) throws {
        try self.init(
            accountKey: accountKey, userId: contents.userId, privateKeyByAccountKey: contents.privateKeyByAccountKey,
            expectedPublicKey: contents.publicKey, seenPublicKeys: contents.seenPublicKeys,
            confirmedPublicKeys: contents.confirmedPublicKeys)
    }

    /// The safety code of a public key given as standard base64; empty when it is not a key.
    static func safetyCodeText(publicKey text: String) -> String {
        guard let data = Data(base64Encoded: text), let code = try? PasswordsCrypto.safetyCode(publicKey: data) else { return "" }
        return code
    }

    /// The owner's public key for a shared item, but only when it is the key this
    /// device knows for the owner: `ownerKeyUnknown` when it has never seen one,
    /// `ownerKeyChanged` (with both safety codes) when the item carries another.
    func ownerPublicKey(for view: PasswordItemView) throws -> Data {
        guard let known = ownerPublicKeyText(forUserId: view.ownerUserId) else {
            throw PasswordsError.ownerKeyUnknown(userId: view.ownerUserId)
        }
        guard known == view.ownerPublicKey else {
            throw PasswordsError.ownerKeyChanged(
                userId: view.ownerUserId, oldSafetyCode: Self.safetyCodeText(publicKey: known),
                newSafetyCode: Self.safetyCodeText(publicKey: view.ownerPublicKey))
        }
        guard let data = Data(base64Encoded: known), PasswordsCrypto.isPublicKeyValid(data) else { throw PasswordsError.invalidServerAnswer }
        return data
    }

    /// The item's key: opened with the account key for an item the person
    /// owns, with their key pair and the owner's known public key for one
    /// shared with them (the recipient id is their own user id, the owner id
    /// the item's owner).
    public func itemKey(for view: PasswordItemView) throws -> Data {
        if view.isSharedWithMe {
            let ownerPublicKey = try ownerPublicKey(for: view)
            let sealed = try PasswordsKeyPairSealedValue(jsonText: view.itemKey)
            return try PasswordsCrypto.openForKeyPair(
                sealed, recipientPrivateKey: keyPair.privateKey, ownerPublicKey: ownerPublicKey, ownerId: view.ownerUserId,
                recipientId: userId, itemId: view.id)
        }
        let sealed = try PasswordsSealedValue(jsonText: view.itemKey)
        return try PasswordsCrypto.open(
            sealed, key: accountKey, associatedData: PasswordsCrypto.itemKeyAssociatedData(itemId: view.id))
    }

    public func open(_ view: PasswordItemView) throws -> PasswordItem {
        guard let itemKind = PasswordsItemKind(rawValue: view.itemKind) else { throw PasswordsError.unknownItemKind(view.itemKind) }
        let itemKey = try itemKey(for: view)
        let sealedFields = try PasswordsSealedValue(jsonText: view.encryptedFields)
        let fields = try PasswordsCrypto.openItemFields(sealedFields, itemKey: itemKey, itemId: view.id, itemKind: itemKind)
        return PasswordItem(
            id: view.id, fields: fields, ownerUserId: view.ownerUserId, ownerName: view.ownerName, isSharedWithMe: view.isSharedWithMe,
            revision: view.revision, isUsableByAgent: view.isUsableByAgent, lastUsedByAgentAt: view.lastUsedByAgentDate,
            deletedAt: view.deletedDate, createdAt: view.createdDate, updatedAt: view.updatedDate, shares: view.shares)
    }

    /// Opens many; the ones that do not open are named, not hidden.
    public func openAll(_ views: [PasswordItemView]) -> PasswordsOpenedItems {
        var items: [PasswordItem] = []
        var unreadableItemIds: [String] = []
        var unreadableReasons: [String: PasswordsUnreadableReason] = [:]
        for view in views {
            do {
                items.append(try open(view))
            } catch {
                unreadableItemIds.append(view.id)
                unreadableReasons[view.id] = PasswordsUnreadableReason.reason(for: error)
            }
        }
        return PasswordsOpenedItems(items: items, unreadableItemIds: unreadableItemIds, unreadableReasons: unreadableReasons)
    }
}

// MARK: The session

/// PasswordsSession is Passwords for one signed-in person on this phone.
public actor PasswordsSession {
    /// The lock time a person starts with, until the server says otherwise.
    public static let defaultLockAfterMinutes = 15

    private let api: any PasswordsAPI
    private let cache: PasswordsCache
    public nonisolated let userId: String

    private var contents: PasswordsCacheContents?
    private var hasLoadedContents = false
    private var opener: PasswordsOpener?
    private var openedItems: [String: PasswordItem] = [:]
    /// When Passwords was last used, for `isAskRequired`.
    public private(set) var lastUsedAt: Date?

    /// `userId` is the signed-in person's id, which the keys shared with them
    /// are bound to.
    public init(api: any PasswordsAPI, cache: PasswordsCache, userId: String) {
        self.api = api
        self.cache = cache
        self.userId = userId
    }

    // MARK: State

    public var isUnlocked: Bool { opener != nil }

    /// The lock time in minutes: the server's, as of the last unlock or change.
    public var lockAfterMinutes: Int { loadedContents()?.lockAfterMinutes ?? Self.defaultLockAfterMinutes }

    /// Whether Passwords must ask for the passphrase or Face ID again, by the lock time.
    public func isAskRequired(now: Date = Date()) -> Bool {
        guard opener != nil else { return true }
        return PasswordsLockPolicy.isAskRequired(lockAfterMinutes: lockAfterMinutes, lastUsedAt: lastUsedAt, now: now)
    }

    /// Forgets the account key and everything opened. The cache file stays.
    public func lock() {
        opener = nil
        openedItems = [:]
        lastUsedAt = nil
    }

    private func touch() {
        lastUsedAt = Date()
    }

    /// Notes that the person is using Passwords without asking it for anything
    /// (browsing, searching), so the lock time runs from now. Does nothing
    /// while locked.
    public func noteUse() {
        if opener != nil { touch() }
    }

    /// The opener, with the known keys as they are now.
    private func requireOpener() throws -> PasswordsOpener {
        guard var current = opener else { throw PasswordsError.locked }
        touch()
        let pins = currentPins()
        current.seenPublicKeys = pins.seenPublicKeys
        current.confirmedPublicKeys = pins.confirmedPublicKeys
        return current
    }

    /// The pins as they are now. After the copy was dropped (`forgetEverything`) they are
    /// read from the pins file, which the drop leaves alone.
    private func currentPins() -> PasswordsPins {
        if let loaded = loadedContents() { return loaded.pins }
        return cache.loadPins(userId: userId)
    }

    /// Records the owner key of each shared item whose owner this device has not
    /// seen before (trust on first use), as a seen key, so the item opens now and a later
    /// change of that key is noticed. Returns whether it recorded any. An owner
    /// the device already has a key for is never touched here: only `trustOwnerKey`
    /// replaces a key. A seen key is not a confirmed one: it never lets this device
    /// seal an item key, because the server chose it.
    private static func recordFirstSightOwnerKeys(in contents: inout PasswordsCacheContents) -> Bool {
        var isRecorded = false
        for view in contents.items where view.isSharedWithMe && contents.pins.ownerPublicKey(forUserId: view.ownerUserId) == nil {
            guard let data = Data(base64Encoded: view.ownerPublicKey), PasswordsCrypto.isPublicKeyValid(data) else { continue }
            contents.seenPublicKeys[view.ownerUserId] = view.ownerPublicKey
            isRecorded = true
        }
        return isRecorded
    }

    private func recordFirstSightOwnerKeys() throws {
        guard var updated = loadedContents(), Self.recordFirstSightOwnerKeys(in: &updated) else { return }
        try store(updated)
    }

    /// The cache as read from the file once, or as kept in memory since. A
    /// file that is damaged, or is another person's, counts as no cache.
    private func loadedContents() -> PasswordsCacheContents? {
        if !hasLoadedContents {
            hasLoadedContents = true
            if let stored = try? cache.load(), stored.userId == userId { contents = stored }
        }
        return contents
    }

    private func store(_ updated: PasswordsCacheContents) throws {
        contents = updated
        hasLoadedContents = true
        try cache.save(updated)
    }

    private func call<Value: Sendable>(_ operation: () async throws -> Value) async throws -> Value {
        do {
            return try await operation()
        } catch {
            throw PasswordsError.mapped(error)
        }
    }

    // MARK: Setting up

    public func readSetup() async throws -> PasswordsSetupView {
        try await call { try await api.readPasswordsSetup() }
    }

    /// Sets Passwords up with a passphrase: makes the account key, the key
    /// pair and a recovery key, sends them sealed, and leaves the session
    /// unlocked. Returns the recovery key as the person should write it down;
    /// it is not kept anywhere and cannot be shown again. `iterationCount` is
    /// the slow key derivation's cost; only a test lowers it.
    public func setUp(passphrase: String, iterationCount: Int = PasswordsCrypto.passphraseIterationCount) async throws -> String {
        let current = try await readSetup()
        guard !current.isSetUp else { throw PasswordsError.alreadySetUp }

        let accountKey = PasswordsCrypto.makeKey()
        let keyPair = PasswordsCrypto.makeKeyPair()
        let salt = PasswordsCrypto.randomBytes(count: PasswordsCrypto.passphraseSaltByteCount)
        let passphraseSecret = try PasswordsCrypto.passphraseKey(passphrase: passphrase, salt: salt, iterationCount: iterationCount)
        let recoveryKey = PasswordsCrypto.makeRecoveryKey()
        let privateKeySealed = try PasswordsCrypto.seal(
            keyPair.privateKey, key: accountKey, associatedData: PasswordsCrypto.privateKeyAssociatedData)
        let input = PasswordsSetUpInput(
            accountKeyByPassphrase: try Self.sealAccountKey(accountKey, secret: passphraseSecret),
            passphraseSalt: salt.base64EncodedString(), passphraseIterationCount: iterationCount,
            passphraseProofHash: try Self.proofHashText(secret: passphraseSecret),
            accountKeyByRecoveryKey: try Self.sealAccountKey(accountKey, secret: recoveryKey),
            recoveryKeyProofHash: try Self.proofHashText(secret: recoveryKey), publicKey: keyPair.publicKey.base64EncodedString(),
            privateKeyByAccountKey: privateKeySealed.jsonText)
        let setup = try await call { try await api.setUpPasswords(input) }

        let pins = currentPins()
        try store(
            PasswordsCacheContents(
                userId: userId, publicKey: input.publicKey, privateKeyByAccountKey: input.privateKeyByAccountKey,
                lockAfterMinutes: setup.lockAfterMinutes, seenPublicKeys: pins.seenPublicKeys,
                confirmedPublicKeys: pins.confirmedPublicKeys))
        opener = PasswordsOpener(accountKey: accountKey, userId: userId, keyPair: keyPair)
        openedItems = [:]
        touch()
        return try PasswordsCrypto.formatRecoveryKey(recoveryKey)
    }

    private static func sealAccountKey(_ accountKey: Data, secret: Data) throws -> String {
        try PasswordsCrypto.seal(
            accountKey, key: try PasswordsCrypto.wrapKey(secret: secret), associatedData: PasswordsCrypto.accountKeyAssociatedData
        ).jsonText
    }

    private static func proofHashText(secret: Data) throws -> String {
        PasswordsCrypto.proofHash(proof: try PasswordsCrypto.proof(secret: secret)).base64EncodedString()
    }

    // MARK: Unlocking

    /// Unlocks with the passphrase. Slow on purpose (the key derivation), so
    /// the caller shows that it is working. A wrong passphrase is
    /// `PasswordsError.wrongPassphrase`.
    @discardableResult
    public func unlock(passphrase: String) async throws -> PasswordsUnlocked {
        let setup = try await readSetup()
        guard setup.isSetUp else { throw PasswordsError.notSetUp }
        guard let saltText = setup.passphraseSalt, let salt = Data(base64Encoded: saltText), let iterationCount = setup.passphraseIterationCount
        else { throw PasswordsError.invalidServerAnswer }
        let secret = try PasswordsCrypto.passphraseKey(passphrase: passphrase, salt: salt, iterationCount: iterationCount)
        return try await finishUnlock(secret: secret, isRecoveryKey: false, setup: setup)
    }

    /// Unlocks with the recovery key as the person wrote it down (spaces,
    /// hyphens and letter case do not matter). A wrong one is
    /// `PasswordsError.wrongRecoveryKey`; one that is not a recovery key at
    /// all is `PasswordsCryptoError.invalidRecoveryKey`.
    @discardableResult
    public func unlock(recoveryKey: String) async throws -> PasswordsUnlocked {
        let secret = try PasswordsCrypto.parseRecoveryKey(recoveryKey)
        let setup = try await readSetup()
        guard setup.isSetUp else { throw PasswordsError.notSetUp }
        return try await finishUnlock(secret: secret, isRecoveryKey: true, setup: setup)
    }

    private func finishUnlock(secret: Data, isRecoveryKey: Bool, setup: PasswordsSetupView) async throws -> PasswordsUnlocked {
        let proof = try PasswordsCrypto.proof(secret: secret).base64EncodedString()
        let view = try await call { try await api.unlockPasswords(proof: proof, isRecoveryKey: isRecoveryKey) }
        let sealedAccountKey = try PasswordsSealedValue(jsonText: view.accountKeyWrapped)
        let accountKey = try PasswordsCrypto.open(
            sealedAccountKey, key: try PasswordsCrypto.wrapKey(secret: secret), associatedData: PasswordsCrypto.accountKeyAssociatedData)
        guard accountKey.count == PasswordsCrypto.keyByteCount else { throw PasswordsError.invalidServerAnswer }
        let newOpener = try PasswordsOpener(
            accountKey: accountKey, userId: userId, privateKeyByAccountKey: view.privateKeyByAccountKey, expectedPublicKey: view.publicKey)

        // Keep the items of a copy that is the same person's same setup; start over otherwise.
        var updated = PasswordsCacheContents(
            userId: userId, publicKey: view.publicKey, privateKeyByAccountKey: view.privateKeyByAccountKey,
            lockAfterMinutes: setup.lockAfterMinutes)
        if let existing = loadedContents(), existing.publicKey == view.publicKey {
            updated.revision = existing.revision
            updated.items = existing.items
            updated.itemIdsNeedingNewKey = existing.itemIdsNeedingNewKey
        }
        updated.pins = currentPins()
        try store(updated)
        opener = newOpener
        openedItems = [:]
        touch()
        return PasswordsUnlocked(accountKey: accountKey, keyPair: newOpener.keyPair)
    }

    /// Unlocks with an account key kept from an earlier unlock (read from the
    /// key store behind Face ID), using the phone's copy of the key pair.
    /// `PasswordsError.needsPassphrase` when there is no copy, it is another
    /// person's, or the key does not open it.
    @discardableResult
    public func open(accountKey: Data) throws -> PasswordsUnlocked {
        guard let contents = loadedContents() else { throw PasswordsError.needsPassphrase }
        let newOpener: PasswordsOpener
        do {
            newOpener = try PasswordsOpener(accountKey: accountKey, contents: contents)
        } catch {
            throw PasswordsError.needsPassphrase
        }
        opener = newOpener
        openedItems = [:]
        touch()
        return PasswordsUnlocked(accountKey: accountKey, keyPair: newOpener.keyPair)
    }

    // MARK: Reading

    /// Brings the phone's copy up to date with the server: fetches what
    /// changed since the copy's revision, replaces those items, drops the ones
    /// the person can no longer reach, and keeps the file. With `isFull` it
    /// fetches everything. If Passwords was erased or set up again elsewhere,
    /// the copy is dropped (the pinned keys of other people stay), the session
    /// locks, and this throws `notSetUp` or `needsPassphrase`.
    @discardableResult
    public func sync(isFull: Bool = false) async throws -> PasswordsMergeResult {
        _ = try requireOpener()
        let setup = try await readSetup()
        if !setup.isSetUp {
            try forgetEverything()
            throw PasswordsError.notSetUp
        }
        guard let before = loadedContents() else { throw PasswordsError.needsPassphrase }
        if setup.publicKey != before.publicKey {
            try forgetEverything()
            throw PasswordsError.needsPassphrase
        }
        var list = try await call { try await api.listPasswordItems(sinceRevision: isFull ? nil : before.revision) }
        // The copy may have changed while the answer was on its way.
        guard var updated = loadedContents() else { throw PasswordsError.needsPassphrase }
        // An incremental page carries only items newer than the copy's revision. If the
        // server lists an item the copy does not hold and the page did not bring (the
        // copy is ahead of it), the page is not enough: ask for everything, once.
        if !isFull, Self.isMissingItem(listed: list.itemIds, in: updated, brought: list.items) {
            list = try await call { try await api.listPasswordItems(sinceRevision: nil) }
            guard let refreshed = loadedContents() else { throw PasswordsError.needsPassphrase }
            updated = refreshed
        }
        updated.lockAfterMinutes = setup.lockAfterMinutes
        let result = updated.merge(list)
        _ = Self.recordFirstSightOwnerKeys(in: &updated)
        try store(updated)
        for itemId in result.changedItemIds + result.removedItemIds { openedItems[itemId] = nil }
        return result
    }

    /// Whether the server lists an item (`listed`) that neither the copy holds nor the page brought.
    private static func isMissingItem(listed itemIds: [String], in contents: PasswordsCacheContents, brought items: [PasswordItemView]) -> Bool {
        let heldItemIds = Set(contents.items.map(\.id)).union(items.map(\.id))
        return itemIds.contains { !heldItemIds.contains($0) }
    }

    /// Drops the copy and locks. The pinned keys of other people stay in their file: a
    /// server's answer alone (another public key, "not set up") must not make the phone
    /// forget whose key it had seen or confirmed. `erase()` and removing the sign-in remove them.
    private func forgetEverything(isKeepingPins: Bool = true) throws {
        if isKeepingPins {
            try cache.eraseKeepingPins()
        } else {
            try cache.erase()
        }
        contents = nil
        hasLoadedContents = true
        lock()
    }

    /// Every item in the phone's copy, opened (those deleted in the last 30
    /// days included: `isDeleted`), sorted by name. Opens each item once and
    /// remembers it until the item changes or the session locks.
    public func list() throws -> PasswordsOpenedItems {
        try recordFirstSightOwnerKeys()
        let opener = try requireOpener()
        guard let contents = loadedContents() else { return PasswordsOpenedItems(items: [], unreadableItemIds: []) }
        var items: [PasswordItem] = []
        var unreadableItemIds: [String] = []
        var unreadableReasons: [String: PasswordsUnreadableReason] = [:]
        for view in contents.items {
            if let known = openedItems[view.id], known.revision == view.revision {
                items.append(known)
                continue
            }
            do {
                let opened = try opener.open(view)
                openedItems[view.id] = opened
                items.append(opened)
            } catch {
                unreadableItemIds.append(view.id)
                unreadableReasons[view.id] = PasswordsUnreadableReason.reason(for: error)
            }
        }
        items.sort {
            let comparison = $0.name.localizedCaseInsensitiveCompare($1.name)
            return comparison == .orderedSame ? $0.id < $1.id : comparison == .orderedAscending
        }
        return PasswordsOpenedItems(items: items, unreadableItemIds: unreadableItemIds, unreadableReasons: unreadableReasons)
    }

    /// One item, opened. `PasswordsError.itemNotFound` when the copy has no such item,
    /// `PasswordsError.ownerKeyChanged` when its owner's key is not the one this device knows.
    public func item(id: String) throws -> PasswordItem {
        try recordFirstSightOwnerKeys()
        let opener = try requireOpener()
        guard let view = loadedContents()?.item(id: id) else { throw PasswordsError.itemNotFound }
        if let known = openedItems[id], known.revision == view.revision { return known }
        let opened = try opener.open(view)
        openedItems[id] = opened
        return opened
    }

    /// What the identity store (QuickType suggestions) should know about the
    /// person's logins and passkeys. Opens every item, so call it after a sync.
    public func identityRecords() throws -> PasswordsIdentityRecords {
        PasswordsIdentityRecords.records(items: try list().items)
    }

    // MARK: Changing items

    /// Saves an item. Without `id` it is a new item with a new item key; with
    /// one it is a change to an item the person owns, sealed with its
    /// existing key (so shares and the agent's use keep working), based on
    /// `baseRevision`, which is the item's revision when the person opened it
    /// (the copy's current one if left out). If the server's revision has
    /// moved on, this throws `PasswordsError.itemChangedOnAnotherDevice`:
    /// `sync()`, open the item again and redo the change. The kind cannot
    /// change, so `fields` must be the kind the item has.
    public func save(fields: PasswordsItemFields, id: String? = nil, baseRevision: Int64? = nil) async throws -> PasswordItem {
        let opener = try requireOpener()
        let itemId: String
        let itemKey: Data
        let sealedItemKey: String
        let revisionToBaseOn: Int64?
        if let id {
            guard let view = loadedContents()?.item(id: id) else { throw PasswordsError.itemNotFound }
            guard !view.isSharedWithMe else { throw PasswordsError.onlyOwnerCanChangeItem }
            guard view.itemKind == fields.itemKind.rawValue else { throw PasswordsError.itemKindCannotChange }
            // The key this item has was known to somebody who lost access to it: this save is the
            // chance to give it a new one, with the new fields sealed by it.
            if needsNewKey(itemId: id) {
                return try await rekey(itemId: id, replacingFields: fields, baseRevision: baseRevision ?? view.revision, isLeavingAgent: false)
            }
            itemId = id
            itemKey = try opener.itemKey(for: view)
            sealedItemKey = view.itemKey
            revisionToBaseOn = baseRevision ?? view.revision
        } else {
            // Lower case, as the server writes it back: the id is part of what is encrypted.
            itemId = UUID().uuidString.lowercased()
            itemKey = PasswordsCrypto.makeKey()
            sealedItemKey = try PasswordsCrypto.seal(
                itemKey, key: opener.accountKey, associatedData: PasswordsCrypto.itemKeyAssociatedData(itemId: itemId)
            ).jsonText
            revisionToBaseOn = nil
        }
        let sealedFields = try PasswordsCrypto.sealItemFields(fields, itemKey: itemKey, itemId: itemId)
        let input = PasswordItemSaveInput(
            id: itemId, itemKind: fields.itemKind.rawValue, encryptedFields: sealedFields.jsonText, itemKey: sealedItemKey,
            baseRevision: revisionToBaseOn)
        let view = try await call { try await api.savePasswordItem(input) }
        return try keep(view, opener: opener)
    }

    /// Deletes an item the person owns. It stays 30 days (`isDeleted`) and can be restored.
    public func delete(id: String) async throws -> PasswordItem {
        let opener = try requireOpener()
        let view = try await call { try await api.deletePasswordItem(id: id) }
        return try keep(view, opener: opener)
    }

    public func restore(id: String) async throws -> PasswordItem {
        let opener = try requireOpener()
        let view = try await call { try await api.restorePasswordItem(id: id) }
        return try keep(view, opener: opener)
    }

    /// Puts the item the server answered with into the copy and the file, and opens it.
    private func keep(_ view: PasswordItemView, opener: PasswordsOpener) throws -> PasswordItem {
        guard var updated = loadedContents() else { throw PasswordsError.needsPassphrase }
        updated.replace(view)
        try store(updated)
        openedItems[view.id] = nil
        let opened = try opener.open(view)
        openedItems[view.id] = opened
        return opened
    }

    private func ownedView(_ itemId: String) throws -> PasswordItemView {
        guard let view = loadedContents()?.item(id: itemId) else { throw PasswordsError.itemNotFound }
        guard !view.isSharedWithMe else { throw PasswordsError.onlyOwnerCanChangeItem }
        return view
    }

    // MARK: Sharing

    /// The people a share with this person or circle would reach, with the
    /// safety code of each one's public key and whether the person confirmed it
    /// on this device (`keyTrust`). Show the codes to the owner before `setShares`; a person
    /// who has not set up Passwords has no key yet.
    public func recipients(granteeUserId: String? = nil, granteeCircleId: String? = nil) async throws -> [PasswordRecipient] {
        _ = try requireOpener()
        let views = try await call { try await api.listPasswordRecipients(granteeUserId: granteeUserId, granteeCircleId: granteeCircleId) }
        let confirmedPublicKeys = currentPins().confirmedPublicKeys
        return views.map { PasswordRecipient(view: $0, knownPublicKey: confirmedPublicKeys[$0.userId]) }
    }

    /// Replaces the item's shares with `grantees`. When that removes somebody, the item is
    /// given a new item key (see `giveNewKey`) so the removed person's old copy opens nothing
    /// saved later; if that fails after the removal went through, this throws
    /// `PasswordsError.itemNeedsNewKey` and the removal stands. Seals the item key for
    /// the newly reached `recipients` (from `recipients(...)`, for the people
    /// the grantees reach). Only a recipient whose key the person already
    /// confirmed on this device, or whose user id is in `confirmedUserIds` (the
    /// person compared the safety code and confirmed), gets a key, and a newly
    /// confirmed key is recorded. A key merely seen on an incoming item does not count. A recipient that already has a key for their current
    /// public key is not sealed for again. Anybody else, and a recipient without
    /// a public key, is left out and shows up in `waitingShares()`. Only the
    /// owner shares; a recipient never shares onward.
    public func setShares(
        itemId: String, grantees: [PasswordShareGranteeInput], recipients: [PasswordRecipient], confirmedUserIds: [String] = []
    ) async throws -> PasswordItem {
        let opener = try requireOpener()
        let view = try ownedView(itemId)
        let itemKey = try opener.itemKey(for: view)
        let alreadyKeyedUserIds = Set(view.shares.flatMap(\.recipients).filter(\.hasKey).map(\.userId))
        var keys: [PasswordShareKeyInput] = []
        var confirmedKeys: [String: String] = [:]
        for recipient in recipients where recipient.userId != userId && !alreadyKeyedUserIds.contains(recipient.userId) {
            guard let publicKey = recipient.publicKey else { continue }
            let publicKeyText = publicKey.base64EncodedString()
            let isConfirmed = confirmedUserIds.contains(recipient.userId)
            guard isConfirmed || opener.confirmedPublicKeys[recipient.userId] == publicKeyText else { continue }
            keys.append(
                try Self.shareKey(
                    itemKey: itemKey, itemId: itemId, ownerKeyPair: opener.keyPair, ownerId: userId, recipientUserId: recipient.userId,
                    recipientPublicKey: publicKey))
            if isConfirmed { confirmedKeys[recipient.userId] = publicKeyText }
        }
        let answered = try await call { try await api.setPasswordItemShares(itemId: itemId, grantees: grantees, keys: keys) }
        try recordConfirmedPublicKeys(confirmedKeys)
        let kept = try keep(answered, opener: opener)
        let reachedBefore = Set(view.shares.flatMap(\.recipients).map(\.userId))
        let reachedNow = Set(answered.shares.flatMap(\.recipients).map(\.userId))
        guard !reachedBefore.subtracting(reachedNow).isEmpty else { return kept }
        // Somebody lost access, and they may hold the item key: it gets replaced.
        do {
            return try await rekey(itemId: itemId, replacingFields: nil, baseRevision: nil, isLeavingAgent: false)
        } catch {
            try? markNeedsNewKey(itemId: itemId, isNeeded: true)
            throw PasswordsError.itemNeedsNewKey(itemId: itemId)
        }
    }

    private static func shareKey(
        itemKey: Data, itemId: String, ownerKeyPair: PasswordsKeyPair, ownerId: String, recipientUserId: String, recipientPublicKey: Data
    ) throws -> PasswordShareKeyInput {
        let sealed = try PasswordsCrypto.sealForKeyPair(
            itemKey, ownerKeyPair: ownerKeyPair, ownerId: ownerId, recipientPublicKey: recipientPublicKey, recipientId: recipientUserId,
            itemId: itemId)
        return PasswordShareKeyInput(
            recipientUserId: recipientUserId, itemKeyByRecipientKey: sealed.jsonText,
            recipientPublicKey: recipientPublicKey.base64EncodedString())
    }

    /// The same recipients as `recipients(...)` for views the server already
    /// sent (the owner's `shares` of an item), marked with whether the person confirmed each key.
    public func recipients(of views: [PasswordRecipientView]) -> [PasswordRecipient] {
        let confirmedPublicKeys = currentPins().confirmedPublicKeys
        return views.map { PasswordRecipient(view: $0, knownPublicKey: confirmedPublicKeys[$0.userId]) }
    }

    /// The owners of shared items whose key is not the one this device knows,
    /// each once, with both safety codes.
    public func ownerKeyChanges() -> [PasswordsOwnerKeyChange] {
        guard let contents = loadedContents() else { return [] }
        let pins = contents.pins
        var changes: [PasswordsOwnerKeyChange] = []
        for view in contents.items where view.isSharedWithMe {
            guard let known = pins.ownerPublicKey(forUserId: view.ownerUserId), known != view.ownerPublicKey,
                Data(base64Encoded: view.ownerPublicKey).map(PasswordsCrypto.isPublicKeyValid) == true
            else { continue }
            if let index = changes.firstIndex(where: { $0.userId == view.ownerUserId }) {
                let earlier = changes[index]
                changes[index] = PasswordsOwnerKeyChange(
                    userId: earlier.userId, ownerName: earlier.ownerName, oldSafetyCode: earlier.oldSafetyCode,
                    newSafetyCode: earlier.newSafetyCode, newPublicKey: earlier.newPublicKey, itemCount: earlier.itemCount + 1)
            } else {
                changes.append(
                    PasswordsOwnerKeyChange(
                        userId: view.ownerUserId, ownerName: view.ownerName, oldSafetyCode: PasswordsOpener.safetyCodeText(publicKey: known),
                        newSafetyCode: PasswordsOpener.safetyCodeText(publicKey: view.ownerPublicKey), newPublicKey: view.ownerPublicKey,
                        itemCount: 1))
            }
        }
        return changes
    }

    /// Remembers these keys (user id to standard base64) as the ones the person confirmed.
    /// They also replace the seen key of that person, so their items are checked against it.
    private func recordConfirmedPublicKeys(_ keys: [String: String]) throws {
        guard !keys.isEmpty, var updated = loadedContents() else { return }
        updated.confirmedPublicKeys.merge(keys) { _, newKey in newKey }
        updated.seenPublicKeys.merge(keys) { _, newKey in newKey }
        try store(updated)
    }

    /// Accepts the key a shared item's owner has now, after the person compared
    /// the old and new safety codes with the owner (`ownerKeyChanged`). Items of
    /// that owner open with the new key from then on. `expectedPublicKey` is the
    /// key whose code the person was shown (`PasswordsOwnerKeyChange.newPublicKey`,
    /// standard base64): if the owner's items carry another key by now, nothing is
    /// trusted and `PasswordsError.ownerKeyChanged` is thrown with the code that
    /// was shown and the code of the key now, for the screen to show again.
    /// `PasswordsError.itemNotFound` when no shared item of that owner is in the
    /// phone's copy.
    public func trustOwnerKey(userId ownerUserId: String, expectedPublicKey: String) throws {
        _ = try requireOpener()
        guard var updated = loadedContents() else { throw PasswordsError.needsPassphrase }
        let ownerViews = updated.items.filter { $0.isSharedWithMe && $0.ownerUserId == ownerUserId }
        guard
            let view = ownerViews.first(where: { Data(base64Encoded: $0.ownerPublicKey).map(PasswordsCrypto.isPublicKeyValid) == true })
        else { throw PasswordsError.itemNotFound }
        guard ownerViews.allSatisfy({ $0.ownerPublicKey == expectedPublicKey }) else {
            throw PasswordsError.ownerKeyChanged(
                userId: ownerUserId, oldSafetyCode: PasswordsOpener.safetyCodeText(publicKey: expectedPublicKey),
                newSafetyCode: PasswordsOpener.safetyCodeText(publicKey: view.ownerPublicKey))
        }
        updated.confirmedPublicKeys[ownerUserId] = view.ownerPublicKey
        updated.seenPublicKeys[ownerUserId] = view.ownerPublicKey
        try store(updated)
        openedItems = [:]
    }

    /// The recipients of the person's items who cannot open them yet, each
    /// with whether the device knows their key.
    public func waitingShares() async throws -> [PasswordWaitingShare] {
        let opener = try requireOpener()
        let views = try await call { try await api.listWaitingPasswordShares() }
        return views.map { view in
            let name = loadedContents()?.item(id: view.itemId).flatMap { try? opener.open($0).name }
            let recipient = PasswordRecipient(view: view.recipient, knownPublicKey: opener.confirmedPublicKeys[view.recipient.userId])
            return PasswordWaitingShare(itemId: view.itemId, itemName: name, recipient: recipient)
        }
    }

    private func isReached(_ view: PasswordItemView, recipientUserId: String) -> Bool {
        view.shares.contains { share in share.recipients.contains { $0.userId == recipientUserId } }
    }

    /// Seals the keys of items whose recipients were waiting (they set up
    /// Passwords, or joined the circle, after the share was made) for their
    /// current public keys, but only when the item's shares reach the recipient
    /// and the person confirmed their key on this device. A recipient with a key the person
    /// has not confirmed (one only seen on an incoming item counts as not confirmed), or one
    /// that changed, is not sealed for: they come back in
    /// `waitingForConfirmation` for the screen to show with their safety codes,
    /// and `confirmWaitingShare` seals one after the person confirms. An item
    /// that is not in the phone's copy is skipped; `sync()` first.
    @discardableResult
    public func completeWaitingShares() async throws -> PasswordsCompletedShares {
        let opener = try requireOpener()
        let waiting = try await call { try await api.listWaitingPasswordShares() }
        var keys: [PasswordItemShareKeyInput] = []
        var waitingForConfirmation: [PasswordWaitingShare] = []
        for entry in waiting {
            let recipient = PasswordRecipient(view: entry.recipient, knownPublicKey: opener.confirmedPublicKeys[entry.recipient.userId])
            guard let publicKey = recipient.publicKey, recipient.userId != userId, let view = loadedContents()?.item(id: entry.itemId),
                !view.isSharedWithMe, isReached(view, recipientUserId: recipient.userId), let itemKey = try? opener.itemKey(for: view)
            else { continue }
            guard recipient.keyTrust == .known else {
                let name = try? opener.open(view).name
                waitingForConfirmation.append(PasswordWaitingShare(itemId: entry.itemId, itemName: name, recipient: recipient))
                continue
            }
            keys.append(
                try Self.itemShareKey(
                    itemKey: itemKey, itemId: entry.itemId, ownerKeyPair: opener.keyPair, ownerId: userId, recipient: recipient,
                    recipientPublicKey: publicKey))
        }
        guard !keys.isEmpty else { return PasswordsCompletedShares(addedKeyCount: 0, waitingForConfirmation: waitingForConfirmation) }
        let addedKeyCount = try await call { try await api.addPasswordShareKeys(keys) }
        return PasswordsCompletedShares(addedKeyCount: addedKeyCount, waitingForConfirmation: waitingForConfirmation)
    }

    private static func itemShareKey(
        itemKey: Data, itemId: String, ownerKeyPair: PasswordsKeyPair, ownerId: String, recipient: PasswordRecipient,
        recipientPublicKey: Data
    ) throws -> PasswordItemShareKeyInput {
        let key = try shareKey(
            itemKey: itemKey, itemId: itemId, ownerKeyPair: ownerKeyPair, ownerId: ownerId, recipientUserId: recipient.userId,
            recipientPublicKey: recipientPublicKey)
        return PasswordItemShareKeyInput(
            itemId: itemId, recipientUserId: key.recipientUserId, itemKeyByRecipientKey: key.itemKeyByRecipientKey,
            recipientPublicKey: key.recipientPublicKey)
    }

    /// Seals one waiting share the person has just confirmed by its safety
    /// code (an entry of `waitingForConfirmation` or `waitingShares()`), for
    /// exactly the key that was shown, and records that key as confirmed. The item
    /// must be the person's own and its shares must reach the recipient.
    public func confirmWaitingShare(_ waiting: PasswordWaitingShare) async throws {
        let opener = try requireOpener()
        guard let publicKey = waiting.recipient.publicKey, waiting.recipient.userId != userId else {
            throw PasswordsError.invalidServerAnswer
        }
        let view = try ownedView(waiting.itemId)
        guard isReached(view, recipientUserId: waiting.recipient.userId) else { throw PasswordsError.itemNotFound }
        let key = try Self.itemShareKey(
            itemKey: try opener.itemKey(for: view), itemId: waiting.itemId, ownerKeyPair: opener.keyPair, ownerId: userId,
            recipient: waiting.recipient, recipientPublicKey: publicKey)
        _ = try await call { try await api.addPasswordShareKeys([key]) }
        try recordConfirmedPublicKeys([waiting.recipient.userId: publicKey.base64EncodedString()])
    }

    // MARK: The agent

    /// Lets the agent use one item, or stops it. Turning it on seals the item
    /// key for the server's agent key, so the server can open this item and
    /// nothing else. Turning it off gives the item a new item key (`RekeyPasswordItem`),
    /// so a copy of the old agent key opens nothing saved later. A card cannot be used by
    /// the agent, and only the owner can allow it.
    public func setUsableByAgent(itemId: String, isUsable: Bool) async throws -> PasswordItem {
        let opener = try requireOpener()
        let view = try ownedView(itemId)
        // The server keeps no key for the agent on a deleted item; do not ask it to.
        guard view.deletedAt == nil else { throw PasswordsError.itemNotFound }
        guard isUsable else {
            // Turning it off gives the item a new key, so a copy of the old agent key opens nothing saved later.
            guard view.isUsableByAgent else { return try item(id: itemId) }
            return try await rekey(itemId: itemId, replacingFields: nil, baseRevision: nil, isLeavingAgent: true)
        }
        guard view.itemKind != PasswordsItemKind.card.rawValue else { throw PasswordsError.cardCannotBeUsedByAgent }
        let sealedItemKey = try await agentCopy(of: try opener.itemKey(for: view), itemId: itemId, opener: opener)
        let answered = try await call {
            try await api.setPasswordItemUsableByAgent(itemId: itemId, isUsableByAgent: true, itemKeyByAgentKey: sealedItemKey)
        }
        return try keep(answered, opener: opener)
    }

    /// The item key sealed for the server's agent key.
    private func agentCopy(of itemKey: Data, itemId: String, opener: PasswordsOpener) async throws -> String {
        let agentKeyText = try await call { try await api.readPasswordsAgentKey() }
        guard let agentPublicKey = Data(base64Encoded: agentKeyText), agentPublicKey.count == PasswordsCrypto.publicKeyByteCount else {
            throw PasswordsError.invalidServerAnswer
        }
        return try PasswordsCrypto.sealForKeyPair(
            itemKey, ownerKeyPair: opener.keyPair, ownerId: userId, recipientPublicKey: agentPublicKey,
            recipientId: PasswordsCrypto.agentRecipientId, itemId: itemId
        ).jsonText
    }

    // MARK: A new item key

    /// Whether the item was left with an old key that somebody who lost access may still hold.
    public func needsNewKey(itemId: String) -> Bool {
        loadedContents()?.itemIdsNeedingNewKey.contains(itemId) == true
    }

    /// The items waiting for a new key (`giveNewKey`).
    public var itemIdsNeedingNewKey: [String] { loadedContents()?.itemIdsNeedingNewKey ?? [] }

    /// Gives the item a new item key now: for the retry after a removal whose re-key failed.
    @discardableResult
    public func giveNewKey(itemId: String) async throws -> PasswordItem {
        _ = try ownedView(itemId)
        return try await rekey(itemId: itemId, replacingFields: nil, baseRevision: nil, isLeavingAgent: false)
    }

    private func markNeedsNewKey(itemId: String, isNeeded: Bool) throws {
        guard var updated = loadedContents() else { return }
        let isMarked = updated.itemIdsNeedingNewKey.contains(itemId)
        guard isMarked != isNeeded else { return }
        if isNeeded {
            updated.itemIdsNeedingNewKey.append(itemId)
        } else {
            updated.itemIdsNeedingNewKey.removeAll { $0 == itemId }
        }
        try store(updated)
    }

    /// Makes a new item key for an item the person owns and seals everything with it: the fields
    /// (`replacingFields` when a save brings new ones, else the item's own), the person's copy, a
    /// copy for each recipient who still has access and whose key the person confirmed on this
    /// device, and the agent's copy when the agent still uses the item. A recipient whose key is
    /// not confirmed is left waiting (`waitingShares()`), because sealing for it unasked is what
    /// the confirmation exists to prevent. When the server says the item changed on another
    /// device and the change was not based on a revision the caller gave, the copy is brought up
    /// to date and this is tried once more.
    private func rekey(
        itemId: String, replacingFields: PasswordsItemFields?, baseRevision: Int64?, isLeavingAgent: Bool
    ) async throws -> PasswordItem {
        do {
            return try await rekeyOnce(
                itemId: itemId, replacingFields: replacingFields, baseRevision: baseRevision, isLeavingAgent: isLeavingAgent)
        } catch PasswordsError.itemChangedOnAnotherDevice where baseRevision == nil {
            try await sync()
            return try await rekeyOnce(itemId: itemId, replacingFields: replacingFields, baseRevision: nil, isLeavingAgent: isLeavingAgent)
        }
    }

    private func rekeyOnce(
        itemId: String, replacingFields: PasswordsItemFields?, baseRevision: Int64?, isLeavingAgent: Bool
    ) async throws -> PasswordItem {
        let opener = try requireOpener()
        let view = try ownedView(itemId)
        guard view.deletedAt == nil else { throw PasswordsError.itemNotFound }
        let fields: PasswordsItemFields
        if let replacingFields {
            guard view.itemKind == replacingFields.itemKind.rawValue else { throw PasswordsError.itemKindCannotChange }
            fields = replacingFields
        } else {
            fields = try opener.open(view).fields
        }
        let newItemKey = PasswordsCrypto.makeKey()
        let sealedFields = try PasswordsCrypto.sealItemFields(fields, itemKey: newItemKey, itemId: itemId)
        let sealedItemKey = try PasswordsCrypto.seal(
            newItemKey, key: opener.accountKey, associatedData: PasswordsCrypto.itemKeyAssociatedData(itemId: itemId))
        var shareKeys: [PasswordItemShareKeyInput] = []
        var seenUserIds = Set<String>()
        for recipientView in view.shares.flatMap(\.recipients) where recipientView.userId != userId {
            guard seenUserIds.insert(recipientView.userId).inserted else { continue }
            let recipient = PasswordRecipient(view: recipientView, knownPublicKey: opener.confirmedPublicKeys[recipientView.userId])
            guard recipient.keyTrust == .known, let publicKey = recipient.publicKey else { continue }
            shareKeys.append(
                try Self.itemShareKey(
                    itemKey: newItemKey, itemId: itemId, ownerKeyPair: opener.keyPair, ownerId: userId, recipient: recipient,
                    recipientPublicKey: publicKey))
        }
        var agentCopyText: String?
        if view.isUsableByAgent && !isLeavingAgent {
            agentCopyText = try await agentCopy(of: newItemKey, itemId: itemId, opener: opener)
        }
        let input = PasswordItemRekeyInput(
            id: itemId, baseRevision: baseRevision ?? view.revision, encryptedFields: sealedFields.jsonText, itemKey: sealedItemKey.jsonText,
            shareKeys: shareKeys, itemKeyByAgentKey: agentCopyText)
        let answered = try await call { try await api.rekeyPasswordItem(input) }
        let kept = try keep(answered, opener: opener)
        try markNeedsNewKey(itemId: itemId, isNeeded: false)
        return kept
    }

    // MARK: Settings

    /// Changes the passphrase. The account key stays the same, so nothing
    /// else is re-sealed. The session must be unlocked.
    public func changePassphrase(
        currentPassphrase: String, newPassphrase: String, iterationCount: Int = PasswordsCrypto.passphraseIterationCount
    ) async throws {
        let opener = try requireOpener()
        let setup = try await readSetup()
        guard setup.isSetUp else { throw PasswordsError.notSetUp }
        guard let saltText = setup.passphraseSalt, let salt = Data(base64Encoded: saltText), let currentCount = setup.passphraseIterationCount
        else { throw PasswordsError.invalidServerAnswer }
        let currentSecret = try PasswordsCrypto.passphraseKey(passphrase: currentPassphrase, salt: salt, iterationCount: currentCount)
        try await changePassphrase(
            proofSecret: currentSecret, isRecoveryKey: false, newPassphrase: newPassphrase, iterationCount: iterationCount,
            accountKey: opener.accountKey)
    }

    /// Changes the passphrase, proving it with the recovery key: for a person who forgot the old one.
    public func changePassphrase(
        recoveryKey: String, newPassphrase: String, iterationCount: Int = PasswordsCrypto.passphraseIterationCount
    ) async throws {
        let opener = try requireOpener()
        try await changePassphrase(
            proofSecret: try PasswordsCrypto.parseRecoveryKey(recoveryKey), isRecoveryKey: true, newPassphrase: newPassphrase,
            iterationCount: iterationCount, accountKey: opener.accountKey)
    }

    private func changePassphrase(proofSecret: Data, isRecoveryKey: Bool, newPassphrase: String, iterationCount: Int, accountKey: Data)
        async throws
    {
        let salt = PasswordsCrypto.randomBytes(count: PasswordsCrypto.passphraseSaltByteCount)
        let newSecret = try PasswordsCrypto.passphraseKey(passphrase: newPassphrase, salt: salt, iterationCount: iterationCount)
        let input = PasswordsChangePassphraseInput(
            proof: try PasswordsCrypto.proof(secret: proofSecret).base64EncodedString(), isRecoveryKey: isRecoveryKey,
            accountKeyByPassphrase: try Self.sealAccountKey(accountKey, secret: newSecret), passphraseSalt: salt.base64EncodedString(),
            passphraseIterationCount: iterationCount, passphraseProofHash: try Self.proofHashText(secret: newSecret))
        _ = try await call { try await api.changePasswordsPassphrase(input) }
    }

    /// Makes a new recovery key (the old one stops working), proving the
    /// passphrase. Returns the new key as the person should write it down.
    public func replaceRecoveryKey(passphrase: String) async throws -> String {
        let opener = try requireOpener()
        let setup = try await readSetup()
        guard setup.isSetUp else { throw PasswordsError.notSetUp }
        guard let saltText = setup.passphraseSalt, let salt = Data(base64Encoded: saltText), let iterationCount = setup.passphraseIterationCount
        else { throw PasswordsError.invalidServerAnswer }
        let secret = try PasswordsCrypto.passphraseKey(passphrase: passphrase, salt: salt, iterationCount: iterationCount)
        let recoveryKey = PasswordsCrypto.makeRecoveryKey()
        let input = PasswordsReplaceRecoveryKeyInput(
            proof: try PasswordsCrypto.proof(secret: secret).base64EncodedString(),
            accountKeyByRecoveryKey: try Self.sealAccountKey(opener.accountKey, secret: recoveryKey),
            recoveryKeyProofHash: try Self.proofHashText(secret: recoveryKey))
        _ = try await call { try await api.replacePasswordsRecoveryKey(input) }
        return try PasswordsCrypto.formatRecoveryKey(recoveryKey)
    }

    /// Sets the lock time (1 to 10080 minutes). Returns what the server kept.
    @discardableResult
    public func updateLockAfterMinutes(_ minutes: Int) async throws -> Int {
        _ = try requireOpener()
        let setup = try await call { try await api.updatePasswordsSettings(lockAfterMinutes: minutes) }
        if var updated = loadedContents() {
            updated.lockAfterMinutes = setup.lockAfterMinutes
            try store(updated)
        }
        return setup.lockAfterMinutes
    }

    /// Erases Passwords on the server (setup, items, shares, keys), then the
    /// phone's copy and the pinned keys (the person asked to start over), and locks. The caller deletes the kept account key from
    /// the key store. This cannot be undone.
    public func erase() async throws {
        _ = try await call { try await api.erasePasswords() }
        try forgetEverything(isKeepingPins: false)
    }
}
