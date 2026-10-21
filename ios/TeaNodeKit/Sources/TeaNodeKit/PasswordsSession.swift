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
    /// Somebody was removed from an item's shares, but the item could not be given a new key
    /// afterwards. The removal stands; `giveNewKey(itemId:)` tries again, and so does the next save.
    case itemNeedsNewKey(itemId: String)
    /// The item's access record is missing, not signed by the person's devices, or older than one
    /// this device has seen (`PasswordsSharingCheck`). Nothing is sealed for anybody from it and the
    /// agent copy is not made until the person sets sharing again.
    case sharingCouldNotBeChecked(itemId: String)
    /// A new key is wanted but was given a moment ago, or this session already gave the most it
    /// gives by itself. The item is listed in `PasswordsSession.itemIdsNeedingAttention`.
    case itemNeedsAttention(itemId: String)
    /// This device has no memory of the item from before this session or from something it did
    /// itself, so it does nothing to it by itself: the person opens the item and finishes the
    /// sharing (`PasswordsSession.finishSharing(itemId:)`).
    case itemNeedsYou(itemId: String)

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
        case .itemNeedsNewKey: return "Removed. The item could not be given a new key."
        case .sharingCouldNotBeChecked: return "Sharing could not be checked. Set sharing again."
        case .itemNeedsAttention: return "This item needs a new key, but one was given a moment ago."
        case .itemNeedsYou: return "Open this item to finish sharing it."
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
    /// Whether the agent can use the item: the server says so AND the owner's own record sealed in
    /// the item's fields says so (`access.isUsableByAgent`) AND that record is signed by the
    /// person's devices and not an older one. The server's word alone is not enough.
    public let isUsableByAgent: Bool
    public let lastUsedByAgentAt: Date?
    /// When the owner deleted it. A deleted item is kept 30 days.
    public let deletedAt: Date?
    public let createdAt: Date?
    public let updatedAt: Date?
    /// The owner's shares; empty for a shared item.
    public let shares: [PasswordShareView]
    /// The server marks an item whose access narrowed while it still has the key somebody held. Only
    /// the owner's device clears it, by giving the item a new key (`PasswordsSession.giveNewKey`).
    public let needsNewKey: Bool
    /// Whether the access record sealed in the fields is the person's own (`PasswordsSharingCheck`).
    public let sharingCheck: PasswordsSharingCheck

    /// The access record, when this device checked it: the person owns the item, the record is
    /// signed with their account key, and no older record than the one seen before is shown. Nil
    /// otherwise, and a nil record reaches nobody and does not let the agent use the item.
    public var verifiedAccess: PasswordsItemAccess? { sharingCheck == .verified ? fields.access : nil }

    /// The shares the server lists that the person's own record lists as well: the only ones
    /// that count as "shared with". Nothing is recorded for an item whose record was not checked.
    public var recordedShares: [PasswordShareView] {
        let record = verifiedAccess
        return shares.filter { $0.isListed(in: record) }
    }

    /// The shares the server lists that the person's own record does not list. The person did
    /// not share the item with them; saving the item's sharing removes them.
    public var unrecordedShares: [PasswordShareView] {
        let record = verifiedAccess
        return shares.filter { !$0.isListed(in: record) }
    }

    public var itemKind: PasswordsItemKind { fields.itemKind }
    public var name: String { fields.name }
    public var isDeleted: Bool { deletedAt != nil }
}

/// PasswordsSharingCheck says whether the access record inside an item, which decides who may
/// have the item and whether the agent may use it, is the person's own. Only the person's
/// devices can sign it (they hold the account key), so a record anybody else wrote, or the
/// server sent back from before, is not trusted.
public enum PasswordsSharingCheck: Equatable, Sendable {
    /// The item belongs to somebody else: only its owner can check the record.
    case ownedByAnotherPerson
    /// Signed with the person's account key, and not older than the newest this device has seen.
    case verified
    /// Missing, or signed by nobody who holds the account key: it reaches nobody.
    case unverified
    /// Signed, but older than a record this device saw before, or the same version with another
    /// mac than the one it saw: the server sent an old or another copy back.
    case replayed
}

extension PasswordShareView {
    /// Whether the person's record lists this share's person or circle as a grantee.
    public func isListed(in access: PasswordsItemAccess?) -> Bool {
        guard let access else { return false }
        if let circleId = granteeCircleId { return access.grantees.contains(.circle(circleId: circleId)) }
        if let userId = granteeUserId { return access.grantees.contains(.person(userId: userId)) }
        return false
    }
}

/// PasswordsOpenedItems is every item that opened, and the ids of those that
/// did not (damaged, or of a kind this build does not know).
public struct PasswordsOpenedItems: Equatable, Sendable {
    public var items: [PasswordItem]
    public var unreadableItemIds: [String]
}

/// PasswordsKeyTrust is what a device knows about a recipient's public key.
public enum PasswordsKeyTrust: Equatable, Hashable, Sendable {
    /// The key is the one this device knows for the person.
    case known
    /// This device has never seen a key for the person: the key the server gives is sealed for at
    /// once and recorded as the known one.
    case new
    /// The server now gives another key than the one this device knows: it is sealed for at
    /// once, recorded as the known one, and a notice is shown until the person dismisses it.
    case changed
    /// The person has not set up Passwords, so there is no key.
    case noKey
}

/// PasswordRecipient is a person an item reaches or could reach, with the
/// code the owner can compare with them to be sure of the key.
public struct PasswordRecipient: Identifiable, Hashable, Sendable {
    public let userId: String
    public let name: String
    public let username: String
    /// The person's public key (65 bytes); nil when they have not set up Passwords.
    public let publicKey: Data?
    /// For example "0412 9930 0007 5121 8800 0123 4567 8901" (eight groups); nil without a public key.
    public let safetyCode: String?
    /// Whether the item already has a key for this public key.
    public let hasKey: Bool
    /// Whether the person already has the most the server lets others share with them: no key is
    /// stored for them, so none is sealed (`PasswordsSession.setShares`, `completeWaitingShares`).
    public let hasTooMuchShared: Bool
    /// Whether the key is the one this device knows for the person, a first sight, or a change.
    public let keyTrust: PasswordsKeyTrust
    /// The safety code of the key this device knew before, when `keyTrust` is `.changed`.
    public let knownSafetyCode: String?
    /// Whether the person compared safety codes for this key. It gates nothing.
    public let isCompared: Bool
    /// The circles the server listed this person as a member of when the recipients were read:
    /// the circle the recipients were asked for. A circle in the owner's access record reaches
    /// somebody only when the server lists them as a member of that circle.
    public let circleIds: [String]

    public var id: String { userId }

    /// `knownPublicKey` is the key this device knows for this person (standard base64), if any.
    /// `comparedPublicKey` is the key the person compared safety codes for, if any.
    public init(view: PasswordRecipientView, knownPublicKey: String? = nil, comparedPublicKey: String? = nil, circleIds: [String] = []) {
        self.circleIds = circleIds
        userId = view.userId
        name = view.name
        username = view.username
        hasKey = view.hasKey
        hasTooMuchShared = view.hasTooMuchShared
        if let text = view.publicKey, let data = Data(base64Encoded: text), PasswordsCrypto.isPublicKeyValid(data),
            let code = try? PasswordsCrypto.safetyCode(publicKey: data)
        {
            publicKey = data
            safetyCode = code
            isCompared = comparedPublicKey == text
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
            isCompared = false
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
    /// Whether more are waiting than the server lists (the newest items first).
    public let hasMoreWaiting: Bool
}

/// PasswordsKeyChange is a person whose key changed from the one this device knew: the key was
/// accepted, and the screen shows a notice until the person dismisses it
/// (`PasswordsSession.dismissKeyChange`), with both safety codes under "Compare codes".
public struct PasswordsKeyChange: Identifiable, Hashable, Sendable {
    public let userId: String
    /// The person's name when the phone's copy knows it (as the owner of a shared item or a
    /// recipient of an item); empty otherwise.
    public let name: String
    public let oldSafetyCode: String
    public let newSafetyCode: String
    /// The key the person has now, standard base64: what `markCompared` is given.
    public let newPublicKey: String

    public var id: String { userId }
}

/// PasswordsCircleShareNotice says that this device sealed an item's key for somebody the item
/// reaches through a circle and had never sealed it for before: "<name> got <item> through
/// <circle>". Who is in a circle is the server's word, so the person is told. It stays until the
/// person dismisses it (`PasswordsSession.dismissCircleShareNotice`); nothing waits on it.
public struct PasswordsCircleShareNotice: Identifiable, Hashable, Sendable {
    public let itemId: String
    public let itemName: String
    public let recipientUserId: String
    public let recipientName: String
    public let circleName: String

    public var id: String { "\(itemId)|\(recipientUserId)" }
}

/// PasswordsCompletedShares is what `completeWaitingShares` did: how many keys it sealed for
/// recipients who were waiting.
public struct PasswordsCompletedShares: Equatable, Sendable {
    public let addedKeyCount: Int
    /// The names of waiting people who were left out because they already have the most the
    /// server lets others share with them ("<name> has too much shared with them."), sorted.
    public let tooMuchSharedNames: [String]
    /// Whether the server says more are waiting than it listed: the rest shows once these are done.
    public let hasMoreWaiting: Bool

    public init(addedKeyCount: Int, tooMuchSharedNames: [String] = [], hasMoreWaiting: Bool = false) {
        self.addedKeyCount = addedKeyCount
        self.tooMuchSharedNames = tooMuchSharedNames
        self.hasMoreWaiting = hasMoreWaiting
    }
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
    /// The one key this device knows for each person, by user id (standard base64). A shared
    /// item opens with the key the item carries, so this is only what the session compares
    /// a key against to tell a first sight or a change from a key it already has.
    public var knownPublicKeys: [String: String]
    /// The key the person compared safety codes for, by user id. It gates nothing.
    public var comparedPublicKeys: [String: String]
    /// What this device remembers about each item the person owns, by item id. A record with a
    /// lower version than the one remembered, or the same version and another mac, is an older or
    /// another one the server sent back (`PasswordsSharingCheck.replayed`).
    public var itemMemories: [String: PasswordsItemMemory]

    public init(
        accountKey: Data, userId: String, keyPair: PasswordsKeyPair, knownPublicKeys: [String: String] = [:],
        comparedPublicKeys: [String: String] = [:], itemMemories: [String: PasswordsItemMemory] = [:]
    ) {
        self.accountKey = accountKey
        self.userId = userId
        self.keyPair = keyPair
        self.knownPublicKeys = knownPublicKeys
        self.comparedPublicKeys = comparedPublicKeys
        self.itemMemories = itemMemories
    }

    /// Opens the person's key pair with the account key. When
    /// `expectedPublicKey` (standard base64) is given, a key pair that does not
    /// match it is refused, which is what a wrong account key looks like if the
    /// seal somehow opened.
    public init(
        accountKey: Data, userId: String, privateKeyByAccountKey: String, expectedPublicKey: String? = nil,
        knownPublicKeys: [String: String] = [:], comparedPublicKeys: [String: String] = [:], itemMemories: [String: PasswordsItemMemory] = [:]
    ) throws {
        let sealed = try PasswordsSealedValue(jsonText: privateKeyByAccountKey)
        let privateKey = try PasswordsCrypto.open(sealed, key: accountKey, associatedData: PasswordsCrypto.privateKeyAssociatedData)
        let keyPair = try PasswordsCrypto.keyPair(privateKey: privateKey)
        if let expectedPublicKey, keyPair.publicKey.base64EncodedString() != expectedPublicKey {
            throw PasswordsCryptoError.invalidKeyPair
        }
        self.init(
            accountKey: accountKey, userId: userId, keyPair: keyPair, knownPublicKeys: knownPublicKeys,
            comparedPublicKeys: comparedPublicKeys, itemMemories: itemMemories)
    }

    /// Opens the cache's key pair, and keeps the cache's pinned keys.
    public init(accountKey: Data, contents: PasswordsCacheContents) throws {
        try self.init(
            accountKey: accountKey, userId: contents.userId, privateKeyByAccountKey: contents.privateKeyByAccountKey,
            expectedPublicKey: contents.publicKey, knownPublicKeys: contents.knownPublicKeys,
            comparedPublicKeys: contents.comparedPublicKeys, itemMemories: contents.itemMemories)
    }

    /// The safety code of a person's public key given as standard base64; empty when it is not a key.
    static func safetyCodeText(publicKey text: String) -> String {
        guard let data = Data(base64Encoded: text), let code = try? PasswordsCrypto.safetyCode(publicKey: data) else { return "" }
        return code
    }

    /// The owner's public key for a shared item: the key the item carries. The item key was sealed
    /// with it, so only an owner who holds its private half makes an item that opens. A key that
    /// differs from the one this device knows is accepted (and noted by the session, which has the
    /// pin store); nobody is asked.
    func ownerPublicKey(for view: PasswordItemView) throws -> Data {
        guard let data = Data(base64Encoded: view.ownerPublicKey), PasswordsCrypto.isPublicKeyValid(data) else {
            throw PasswordsError.invalidServerAnswer
        }
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
        let sharingCheck = sharingCheck(of: view, fields: fields, itemKind: itemKind)
        // The agent can use an item only when the owner's own record says so as well, and the record is
        // the person's: a server that sets its flag on an item the owner never let the agent use, or
        // that sends an old record or one somebody else wrote, changes nothing here.
        let isUsableByAgent =
            sharingCheck == .verified && view.isUsableByAgent && fields.access?.isUsableByAgent == true
        return PasswordItem(
            id: view.id, fields: fields, ownerUserId: view.ownerUserId, ownerName: view.ownerName, isSharedWithMe: view.isSharedWithMe,
            revision: view.revision, isUsableByAgent: isUsableByAgent, lastUsedByAgentAt: view.lastUsedByAgentDate,
            deletedAt: view.deletedDate, createdAt: view.createdDate, updatedAt: view.updatedDate, shares: view.shares,
            needsNewKey: view.needsNewKey, sharingCheck: sharingCheck)
    }

    /// Whether the item's access record is the person's own. Only the owner's devices can check it
    /// (the signature key is made from the account key), so a shared item is never checked here.
    private func sharingCheck(of view: PasswordItemView, fields: PasswordsItemFields, itemKind: PasswordsItemKind) -> PasswordsSharingCheck {
        if view.isSharedWithMe { return .ownedByAnotherPerson }
        guard
            let record = PasswordsCrypto.verifyAccess(accountKey: accountKey, itemId: view.id, itemKind: itemKind, fields: fields),
            let version = record.recordVersion
        else { return .unverified }
        return itemMemories[view.id]?.isReplay(version: version, mac: record.recordMac) == true ? .replayed : .verified
    }

    /// Opens many; the ones that do not open are named, not hidden.
    public func openAll(_ views: [PasswordItemView]) -> PasswordsOpenedItems {
        var items: [PasswordItem] = []
        var unreadableItemIds: [String] = []
        for view in views {
            do {
                items.append(try open(view))
            } catch {
                unreadableItemIds.append(view.id)
            }
        }
        return PasswordsOpenedItems(items: items, unreadableItemIds: unreadableItemIds)
    }
}

// MARK: The session

/// PasswordsSession is Passwords for one signed-in person on this phone.
///
/// The app uses one that holds the whole copy in memory. The AutoFill extension, which runs under
/// a memory limit, uses one made with `isLoadingItemsOnDemand`: it reads the header of the copy
/// and only the items it is asked for, and it cannot sync (the app does that).
public actor PasswordsSession {
    /// The lock time a person starts with, until the server says otherwise.
    public static let defaultLockAfterMinutes = 15

    private let api: any PasswordsAPI
    private let cache: PasswordsCache
    public nonisolated let userId: String
    private let isLoadingItemsOnDemand: Bool
    private let now: @Sendable () -> Date

    /// How long an item waits after a new key this session gave it by itself (the server marked
    /// it, or a save found it marked) before it gives another one.
    public static let automaticRekeyCooldownSeconds: TimeInterval = 600
    /// The most new keys one session gives by itself. Past it, items are listed in
    /// `itemIdsNeedingAttention` and wait for the person.
    public static let mostAutomaticRekeysPerSession = 20

    private var contents: PasswordsCacheContents?
    private var hasLoadedContents = false
    private var opener: PasswordsOpener?
    private var openedItems: [String: PasswordItem] = [:]
    /// The pins as they were the last time the pin store could be read, kept for when it cannot
    /// be: a store that fails to read is never taken for one with no keys.
    private var lastReadPins: PasswordsPins?
    /// When Passwords was last used, for `isAskRequired`.
    public private(set) var lastUsedAt: Date?
    /// When this session last gave each item a new key by itself, by item id.
    private var lastAutomaticRekeyAt: [String: Date] = [:]
    private var automaticRekeyCount = 0
    private var heldBackItemIds: Set<String> = []
    /// The items this device had a memory of when the session read its pins for the first time:
    /// from an earlier session. A memory made by looking at an item during this session does not
    /// count, because a device that has just met an item cannot tell an old signed record from the
    /// newest one.
    private var itemIdsRememberedBefore: Set<String>?
    /// The items the person changed or shared on this device during this session: this device's own
    /// action is a memory too.
    private var itemIdsActedOn: Set<String> = []
    /// The marked items an automatic new key left for the person, because this device has no memory
    /// of them (`itemIdsNeedingYou`).
    private var keyItemIdsLeftForPerson: Set<String> = []
    /// The items with a waiting share that completing by itself would have sealed for somebody this
    /// device never sealed for, or for an item it has no memory of (`itemIdsNeedingYou`).
    private var shareItemIdsLeftForPerson: Set<String> = []

    /// `userId` is the signed-in person's id, which the keys shared with them
    /// are bound to. `now` is the clock; only a test replaces it.
    public init(
        api: any PasswordsAPI, cache: PasswordsCache, userId: String, isLoadingItemsOnDemand: Bool = false,
        now: @escaping @Sendable () -> Date = { Date() }
    ) {
        self.api = api
        self.cache = cache
        self.userId = userId
        self.isLoadingItemsOnDemand = isLoadingItemsOnDemand
        self.now = now
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
        current.knownPublicKeys = pins.knownPublicKeys
        current.comparedPublicKeys = pins.comparedPublicKeys
        current.itemMemories = pins.itemMemories
        return current
    }

    /// The pins as they are now, read from the pin store (the keychain on the phone) each time, so a
    /// key the app recorded is known to the AutoFill extension and the other way round. When the
    /// store cannot be read, the pins as they last were; never none, unless none was ever read.
    private func currentPins() -> PasswordsPins {
        if let read = try? cache.loadPins(userId: userId) {
            lastReadPins = read
            if itemIdsRememberedBefore == nil {
                itemIdsRememberedBefore = Set(read.itemMemories.filter { $0.value.highestRecordVersion > 0 }.keys)
            }
            return read
        }
        return lastReadPins ?? PasswordsPins(userId: userId)
    }

    /// Opens an item. A shared item opens with the key its owner carries on it; once it has opened
    /// (a key that opens nothing is not recorded), that key is recorded as the known one for the
    /// owner when none was known, and, when it differs from the known one, accepted with a notice
    /// the person can dismiss (`keyChanges()`).
    private func openItem(_ view: PasswordItemView, opener: inout PasswordsOpener) throws -> PasswordItem {
        let opened = try opener.open(view)
        if view.isSharedWithMe { noteKey(userId: view.ownerUserId, publicKey: view.ownerPublicKey, opener: &opener) }
        remember(opened)
        return opened
    }

    /// Records a key the server gave for a person and this device used: the first one seen becomes
    /// the known key; another one replaces it and leaves a notice. Best effort when the pin store
    /// cannot be written: the key is still used, and the next read records it.
    private func noteKey(userId otherUserId: String, publicKey: String, opener: inout PasswordsOpener) {
        guard let known = opener.knownPublicKeys[otherUserId] else {
            if (try? cache.recordKnownIfAbsent(userId: userId, otherUserId: otherUserId, publicKey: publicKey)) != nil {
                opener.knownPublicKeys[otherUserId] = publicKey
            }
            return
        }
        guard known != publicKey else { return }
        if (try? cache.recordChangedKey(userId: userId, otherUserId: otherUserId, publicKey: publicKey)) != nil {
            opener.knownPublicKeys[otherUserId] = publicKey
        }
    }

    /// Remembers the access record an opened item of the person's shows, in the pin store: its
    /// version and mac, once the signature is valid and it is not a replay. The version only goes
    /// up. Nothing about who holds a key is taken from the server's lists: that is remembered only
    /// from what this device seals itself (`recordSealed`). Best effort: a store that cannot be
    /// written is skipped.
    private func remember(_ item: PasswordItem) {
        guard item.sharingCheck == .verified, let access = item.fields.access, let version = access.recordVersion else { return }
        let current = lastReadPins?.itemMemories[item.id] ?? PasswordsItemMemory()
        let seen = current.seeingRecord(version: version, mac: access.recordMac)
        guard seen != current else { return }
        try? cache.recordSeenRecord(userId: userId, itemId: item.id, version: version, mac: access.recordMac)
        guard var pins = lastReadPins else { return }
        pins.itemMemories[item.id] = seen
        lastReadPins = pins
    }

    /// Whether this device may act on an item by itself (a new key given because the server marked
    /// it, a waiting share completed): it remembered the item before this session, or the person
    /// changed or shared it on this device in this session.
    private func isRememberedForAutomaticAction(itemId: String) -> Bool {
        _ = currentPins()
        return itemIdsActedOn.contains(itemId) || (itemIdsRememberedBefore?.contains(itemId) ?? false)
    }

    /// Records what this device sealed for an item itself. Best effort, after the server took the
    /// keys: a store that cannot be written makes a later automatic action stricter, never looser.
    private func recordSealed(itemId: String, change: PasswordsSealedChange) {
        try? cache.recordSealed(userId: userId, itemId: itemId, change: change)
        guard var pins = lastReadPins else { return }
        pins.itemMemories[itemId] = (pins.itemMemories[itemId] ?? PasswordsItemMemory()).sealing(change)
        lastReadPins = pins
    }

    /// The cache as read from the file once, or as kept in memory since. A
    /// file that is damaged, or is another person's, counts as no cache. An
    /// on-demand session holds the header and the items it was asked for.
    private func loadedContents() -> PasswordsCacheContents? {
        if !hasLoadedContents {
            hasLoadedContents = true
            let stored: PasswordsCacheContents?
            if isLoadingItemsOnDemand {
                stored = try? cache.loadHeader()
            } else {
                stored = try? cache.load()
            }
            if let stored, stored.userId == userId { contents = stored }
        }
        return contents
    }

    /// One item of the copy. An on-demand session reads it from its file the first time.
    private func itemView(id: String) -> PasswordItemView? {
        guard let current = loadedContents() else { return nil }
        if let found = current.item(id: id) { return found }
        guard isLoadingItemsOnDemand, let loaded = try? cache.loadItem(id: id) else { return nil }
        contents?.replace(loaded)
        return loaded
    }

    /// Keeps `updated` in memory and writes it. Only what differs from what this session held
    /// is written (one file for each changed item, and the removal of the files of items that
    /// are gone), and the cache merges it with what is on disk, so a write by the app and one by
    /// the AutoFill extension at the same time do not undo each other.
    private func store(_ updated: PasswordsCacheContents) throws {
        let previous = contents
        var changedItemIds: Set<String>?
        var removedItemIds: Set<String> = []
        if let previous {
            let previousById = Dictionary(previous.items.map { ($0.id, $0) }, uniquingKeysWith: { _, later in later })
            changedItemIds = Set(updated.items.filter { previousById[$0.id] != $0 }.map(\.id))
            if !isLoadingItemsOnDemand {
                removedItemIds = Set(previousById.keys).subtracting(updated.items.map(\.id))
            }
        }
        contents = updated
        hasLoadedContents = true
        let revision = try cache.save(updated, changedItemIds: changedItemIds, removedItemIds: removedItemIds)
        contents?.revision = revision
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
                lockAfterMinutes: setup.lockAfterMinutes, knownPublicKeys: pins.knownPublicKeys,
                comparedPublicKeys: pins.comparedPublicKeys, changedPublicKeys: pins.changedPublicKeys))
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
    /// the person can no longer reach, and keeps the files. With `isFull` it
    /// fetches everything. If Passwords was erased or set up again elsewhere,
    /// the copy is dropped (the pinned keys of other people stay), the session
    /// locks, and this throws `notSetUp` or `needsPassphrase`. A session made
    /// to load items on demand cannot sync (it throws `locked`): the app does.
    @discardableResult
    public func sync(isFull: Bool = false) async throws -> PasswordsMergeResult {
        guard !isLoadingItemsOnDemand else { throw PasswordsError.locked }
        var opener = try requireOpener()
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
        try store(updated)
        for itemId in result.changedItemIds + result.removedItemIds { openedItems[itemId] = nil }
        // Record the key of each owner whose shared item opens, then list the logins for AutoFill.
        recordOwnerKeys(opener: &opener)
        refreshSummary(opener: opener)
        return result
    }

    /// Whether the server lists an item (`listed`) that neither the copy holds nor the page brought.
    private static func isMissingItem(listed itemIds: [String], in contents: PasswordsCacheContents, brought items: [PasswordItemView]) -> Bool {
        let heldItemIds = Set(contents.items.map(\.id)).union(items.map(\.id))
        return itemIds.contains { !heldItemIds.contains($0) }
    }

    /// Opens one shared item of each owner whose key differs from the one this device knows (or
    /// whose key it has never seen), so that key is recorded once the item has opened (`openItem`),
    /// with a notice when it replaced another. An owner none of whose items open is not recorded.
    private func recordOwnerKeys(opener: inout PasswordsOpener) {
        guard let current = loadedContents() else { return }
        var checkedOwnerUserIds = Set<String>()
        for view in current.items where view.isSharedWithMe && !checkedOwnerUserIds.contains(view.ownerUserId) {
            guard opener.knownPublicKeys[view.ownerUserId] != view.ownerPublicKey else { continue }
            if (try? openItem(view, opener: &opener)) != nil { checkedOwnerUserIds.insert(view.ownerUserId) }
        }
    }

    /// Writes the summary AutoFill lists logins from, sealed with the account key, for the whole copy.
    private func refreshSummary(opener: PasswordsOpener) {
        guard !isLoadingItemsOnDemand, let current = loadedContents() else { return }
        let summary = PasswordsAutoFillSummary.make(userId: userId, views: current.items, opener: opener)
        guard let data = try? summary.sealedData(accountKey: opener.accountKey) else { return }
        try? cache.saveSummaryData(data)
    }

    /// Puts one item's login in the summary, or takes it out (a note, a card, a deleted item). A
    /// summary that is missing or cannot be opened is made again from the whole copy; a session
    /// that does not hold the whole copy leaves it alone.
    private func updateSummary(for item: PasswordItem, opener: PasswordsOpener) {
        let login = PasswordsAutoFillSummary.login(of: item)
        let accountKey = opener.accountKey
        let signedInUserId = userId
        let views = isLoadingItemsOnDemand ? nil : loadedContents()?.items
        try? cache.updateSummaryData { current in
            var summary: PasswordsAutoFillSummary
            if let current, let opened = try? PasswordsAutoFillSummary.open(current, accountKey: accountKey, userId: signedInUserId) {
                summary = opened
            } else if let views {
                summary = PasswordsAutoFillSummary.make(userId: signedInUserId, views: views, opener: opener)
            } else {
                return current
            }
            if let login {
                summary.upsert(login)
            } else {
                summary.remove(itemId: item.id)
            }
            return (try? summary.sealedData(accountKey: accountKey)) ?? current
        }
    }

    /// Drops the copy and locks. The pinned keys of other people stay in their store: a
    /// server's answer alone (another public key, "not set up") must not make the phone
    /// forget whose key it knew. `erase()` and removing the sign-in remove them.
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
        var opener = try requireOpener()
        guard let contents = loadedContents() else { return PasswordsOpenedItems(items: [], unreadableItemIds: []) }
        var items: [PasswordItem] = []
        var unreadableItemIds: [String] = []
        for view in contents.items {
            if let known = openedItems[view.id], known.revision == view.revision {
                items.append(known)
                continue
            }
            do {
                let opened = try openItem(view, opener: &opener)
                openedItems[view.id] = opened
                items.append(opened)
            } catch {
                unreadableItemIds.append(view.id)
            }
        }
        items.sort {
            let comparison = $0.name.localizedCaseInsensitiveCompare($1.name)
            return comparison == .orderedSame ? $0.id < $1.id : comparison == .orderedAscending
        }
        return PasswordsOpenedItems(items: items, unreadableItemIds: unreadableItemIds)
    }

    /// One item, opened. `PasswordsError.itemNotFound` when the copy has no such item.
    public func item(id: String) throws -> PasswordItem {
        var opener = try requireOpener()
        guard let view = itemView(id: id) else { throw PasswordsError.itemNotFound }
        if let known = openedItems[id], known.revision == view.revision { return known }
        let opened = try openItem(view, opener: &opener)
        openedItems[id] = opened
        return opened
    }

    /// What the identity store (QuickType suggestions) should know about the
    /// person's logins and passkeys. Opens every item, so call it after a sync.
    public func identityRecords() throws -> PasswordsIdentityRecords {
        PasswordsIdentityRecords.records(items: try list().items)
    }

    /// The person's own safety code, from the key pair this session opened at unlock, never from
    /// what the server reports. The person reads it to somebody who wants to compare codes. `PasswordsError.locked` while locked.
    public func ownSafetyCode() throws -> String {
        let opener = try requireOpener()
        return try PasswordsCrypto.safetyCode(publicKey: opener.keyPair.publicKey)
    }

    // MARK: Changing items

    /// Saves an item. Without `id` it is a new item with a new item key; with
    /// one it is a change to an item the person owns, sealed with its
    /// existing key (so shares and the agent's use keep working), based on
    /// `baseRevision`, which is the item's revision when the person opened it
    /// (the copy's current one if left out). If the server's revision has
    /// moved on, this throws `PasswordsError.itemChangedOnAnotherDevice`:
    /// `sync()`, open the item again and redo the change. The kind cannot
    /// change, so `fields` must be the kind the item has. An edit never changes who may
    /// have the item or whether the agent may use it: the access record of the fields
    /// given is replaced by the item's own (a new item starts with a signed record that
    /// grants nobody), because only `setShares` and `setUsableByAgent` change it, in the
    /// order that keeps it from ever being wider than what the person chose.
    public func save(fields: PasswordsItemFields, id: String? = nil, baseRevision: Int64? = nil) async throws -> PasswordItem {
        let opener = try requireOpener()
        let itemId: String
        let itemKey: Data
        let sealedItemKey: String
        let revisionToBaseOn: Int64?
        let fieldsToSeal: PasswordsItemFields
        if let id {
            guard let view = itemView(id: id) else { throw PasswordsError.itemNotFound }
            guard !view.isSharedWithMe else { throw PasswordsError.onlyOwnerCanChangeItem }
            guard view.itemKind == fields.itemKind.rawValue else { throw PasswordsError.itemKindCannotChange }
            fieldsToSeal = fields.withAccess(try opener.open(view).fields.access)
            // The key this item has was known to somebody who lost access to it: this save is the
            // chance to give it a new one, with the new fields sealed by it, as far as a new key
            // given by itself may go. When it may not (the record is not the person's, or one was
            // given a moment ago), the fields are sealed with the key the item has.
            if view.needsNewKey {
                do {
                    return try await rekey(
                        itemId: id, replacing: fieldsToSeal, baseRevision: baseRevision ?? view.revision, accessEdit: .none,
                        origin: .automatic)
                } catch PasswordsError.itemNeedsAttention, PasswordsError.sharingCouldNotBeChecked, PasswordsError.itemNeedsYou {
                    // Saved below, with the key the item has.
                }
            }
            itemId = id
            itemKey = try opener.itemKey(for: view)
            sealedItemKey = view.itemKey
            revisionToBaseOn = baseRevision ?? view.revision
        } else {
            // Lower case, as the server writes it back: the id is part of what is encrypted.
            itemId = UUID().uuidString.lowercased()
            itemIdsActedOn.insert(itemId)
            // A record that grants nobody, signed: an item with none could not be told from one
            // whose record somebody removed.
            fieldsToSeal = try PasswordsCrypto.withSignedAccess(
                accountKey: opener.accountKey, itemId: itemId, itemKind: fields.itemKind, fields: fields, access: nil, highestVersionSeen: 0)
            itemKey = PasswordsCrypto.makeKey()
            sealedItemKey = try PasswordsCrypto.seal(
                itemKey, key: opener.accountKey, associatedData: PasswordsCrypto.itemKeyAssociatedData(itemId: itemId)
            ).jsonText
            revisionToBaseOn = nil
        }
        let sealedFields = try PasswordsCrypto.sealItemFields(fieldsToSeal, itemKey: itemKey, itemId: itemId)
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

    /// Deletes an item the person deleted earlier, and its old versions, for good. It cannot be
    /// brought back. The server needs the person's own tap (the app's sign-in). The item leaves the
    /// phone's copy, its files and the AutoFill summary. `PasswordsError.itemNotFound` for an item
    /// that is not in the person's recently deleted ones.
    public func purge(id: String) async throws {
        // Only the app, which holds the whole copy, does this (a session that loads items on demand cannot sync either).
        guard !isLoadingItemsOnDemand else { throw PasswordsError.locked }
        _ = try requireOpener()
        let view = try ownedView(id)
        guard view.deletedAt != nil else { throw PasswordsError.itemNotFound }
        _ = try await call { try await api.purgePasswordItem(id: id) }
        guard var updated = loadedContents() else { return }
        updated.items.removeAll { $0.id == id }
        try store(updated)
        openedItems[id] = nil
        removeFromSummary(itemId: id)
    }

    /// Takes one login out of the summary AutoFill lists logins from.
    private func removeFromSummary(itemId: String) {
        guard let current = opener else { return }
        let accountKey = current.accountKey
        let signedInUserId = userId
        try? cache.updateSummaryData { data in
            guard let data, var summary = try? PasswordsAutoFillSummary.open(data, accountKey: accountKey, userId: signedInUserId) else {
                return data
            }
            summary.remove(itemId: itemId)
            return (try? summary.sealedData(accountKey: accountKey)) ?? data
        }
    }

    /// Brings a deleted item back. The server keeps its mark when the item had to be given a new
    /// key (it had shares or an agent copy when it was deleted), and the item is given one now; if
    /// that fails the restored item is returned and `giveNewKey` or the next save tries again.
    public func restore(id: String) async throws -> PasswordItem {
        let opener = try requireOpener()
        let view = try await call { try await api.restorePasswordItem(id: id) }
        let kept = try keep(view, opener: opener)
        guard view.needsNewKey, !view.isSharedWithMe else { return kept }
        return (try? await rekey(itemId: id, replacing: nil, baseRevision: nil, accessEdit: .none, origin: .request)) ?? kept
    }

    /// Puts the item the server answered with into the copy and the files, opens it, and
    /// updates its entry in the summary AutoFill lists logins from.
    private func keep(_ view: PasswordItemView, opener: PasswordsOpener) throws -> PasswordItem {
        guard var updated = loadedContents() else { throw PasswordsError.needsPassphrase }
        updated.replace(view)
        try store(updated)
        openedItems[view.id] = nil
        let opened = try opener.open(view)
        openedItems[view.id] = opened
        remember(opened)
        updateSummary(for: opened, opener: opener)
        return opened
    }

    private func ownedView(_ itemId: String) throws -> PasswordItemView {
        guard let view = itemView(id: itemId) else { throw PasswordsError.itemNotFound }
        guard !view.isSharedWithMe else { throw PasswordsError.onlyOwnerCanChangeItem }
        return view
    }

    /// Saves fields over an owned item with the key it has, based on the revision the copy holds.
    private func saveFields(_ fields: PasswordsItemFields, of view: PasswordItemView, opener: PasswordsOpener) async throws -> PasswordItem {
        let sealedFields = try PasswordsCrypto.sealItemFields(fields, itemKey: try opener.itemKey(for: view), itemId: view.id)
        let input = PasswordItemSaveInput(
            id: view.id, itemKind: view.itemKind, encryptedFields: sealedFields.jsonText, itemKey: view.itemKey, baseRevision: view.revision)
        let answered = try await call { try await api.savePasswordItem(input) }
        return try keep(answered, opener: opener)
    }

    // MARK: Sharing

    /// The people a share with this person or circle would reach, with the
    /// safety code of each one's public key, whether it is the key this device
    /// knows (`keyTrust`) and whether the person compared codes (`isCompared`). A person
    /// who has not set up Passwords has no key yet.
    public func recipients(granteeUserId: String? = nil, granteeCircleId: String? = nil) async throws -> [PasswordRecipient] {
        _ = try requireOpener()
        let views = try await call { try await api.listPasswordRecipients(granteeUserId: granteeUserId, granteeCircleId: granteeCircleId) }
        return makeRecipients(views, circleIds: granteeCircleId.map { [$0] } ?? [])
    }

    /// Recipients marked with what this device knows of each key.
    private func makeRecipients(_ views: [PasswordRecipientView], circleIds: [String]) -> [PasswordRecipient] {
        let pins = currentPins()
        return views.map {
            PasswordRecipient(
                view: $0, knownPublicKey: pins.knownPublicKeys[$0.userId], comparedPublicKey: pins.comparedPublicKeys[$0.userId],
                circleIds: circleIds)
        }
    }

    /// The grantee of the access record that a share target is.
    private static func grantee(of input: PasswordShareGranteeInput) -> PasswordsGrantee? {
        switch (input.granteeUserId, input.granteeCircleId) {
        case (let userId?, nil) where !userId.isEmpty: return .person(userId: userId)
        case (nil, let circleId?) where !circleId.isEmpty: return .circle(circleId: circleId)
        default: return nil
        }
    }

    /// The circles the server lists a person as a member of among the shares the copy holds of an item.
    private static func circleIds(listing recipientUserId: String, in view: PasswordItemView) -> [String] {
        view.shares.compactMap { share in
            guard let circleId = share.granteeCircleId, share.recipients.contains(where: { $0.userId == recipientUserId }) else { return nil }
            return circleId
        }
    }

    /// The people among `userIds` who are reached only through a circle of the record and whom this
    /// device never sealed the item for before: the ones the person is told about ("<name> got <item>
    /// through <circle>"). Who is in a circle is the server's word, so this is the one place the
    /// person is shown where it was relied on. A person the record names itself is not listed.
    private static func circleShares(
        sealingUserIds userIds: [String], previouslySealedUserIds: Set<String>, in view: PasswordItemView, access: PasswordsItemAccess?
    ) -> [PasswordsCircleShare] {
        guard let access else { return [] }
        var circleShares: [PasswordsCircleShare] = []
        for recipientUserId in userIds.sorted() where !previouslySealedUserIds.contains(recipientUserId) {
            if access.grantees.contains(.person(userId: recipientUserId)) { continue }
            for share in view.shares {
                guard let circleId = share.granteeCircleId, access.grantees.contains(.circle(circleId: circleId)),
                    let member = share.recipients.first(where: { $0.userId == recipientUserId })
                else { continue }
                circleShares.append(
                    PasswordsCircleShare(recipientUserId: recipientUserId, recipientName: member.name, circleName: share.granteeName))
                break
            }
        }
        return circleShares
    }

    /// Replaces who the item is shared with by `grantees`. The owner's own record decides who may
    /// have the item: the list of grantees is sealed inside the item's fields (`access`, signed with
    /// the account key), and a key is sealed for a person only if that record reaches them and
    /// they have a key. The calls go in the order that keeps the sealed record from ever
    /// being wider than what the person chose:
    ///
    /// - When somebody is added, the fields are saved with the new, signed record first, then the
    ///   server is told (`SetPasswordItemShares`) with the keys sealed for the new recipients.
    /// - When somebody is removed (the record before has a grantee the record now lacks, or the
    ///   server lists a share the new list does not have, whatever the server answers), the server
    ///   is told, then the item is given a new item key (`RekeyPasswordItem`) whose fields carry the
    ///   narrower record, so the removed person's old copy opens nothing saved later. If that fails
    ///   after the removal went through, this throws `PasswordsError.itemNeedsNewKey`: the removal
    ///   stands, the server marks the item (`PasswordItem.needsNewKey`), and `giveNewKey(itemId:)`
    ///   tries again.
    ///
    /// An item whose record was not checked (`PasswordsSharingCheck.unverified` or `.replayed`) is
    /// treated as shared with nobody: this call writes a fresh signed record with the grantees
    /// given, which is how the person sets sharing again, and gives the item a new key when the
    /// server lists any share or agent copy for it, because whoever wrote the record may hold the
    /// old key.
    ///
    /// `recipients` are the people the grantees reach (from `recipients(...)`). Each one the record
    /// reaches who has a key gets a key sealed for the public key the server gives, at once: a
    /// first-seen key is recorded as the known one, and a key that differs from the known one is
    /// accepted and recorded with a notice (`keyChanges()`). Nobody is asked. A recipient that
    /// already has a key for their current public key is not sealed for again. A recipient without a
    /// public key is left out and shows up in `waitingShares()`. Only the owner shares; a
    /// recipient never shares onward.
    public func setShares(
        itemId: String, grantees: [PasswordShareGranteeInput], recipients: [PasswordRecipient]
    ) async throws -> PasswordItem {
        var opener = try requireOpener()
        var view = try ownedView(itemId)
        guard view.deletedAt == nil else { throw PasswordsError.itemNotFound }
        itemIdsActedOn.insert(itemId)
        let previouslySealedUserIds = Set(currentPins().itemMemories[itemId]?.sealedForUserIds ?? [])
        // An item that still has the key somebody lost access to is given a new one before anything
        // else, as far as one given by itself may go.
        if view.needsNewKey, try await rekeyIfAllowed(itemId: itemId) {
            opener = try requireOpener()
            view = try ownedView(itemId)
        }
        let opened = try opener.open(view)
        let isRecordChecked = opened.sharingCheck == .verified
        let oldGrantees = opened.verifiedAccess?.grantees ?? []
        var newGrantees: [PasswordsGrantee] = []
        var chosenInputs: [PasswordShareGranteeInput] = []
        for input in grantees {
            guard let grantee = Self.grantee(of: input), !newGrantees.contains(grantee) else { continue }
            newGrantees.append(grantee)
            chosenInputs.append(input)
        }
        let newAccess = PasswordsItemAccess.accessWithGrantees(opened.verifiedAccess, grantees: newGrantees)
        let isNarrowing = oldGrantees.contains { !newGrantees.contains($0) }
        let isWidening = newGrantees.contains { !oldGrantees.contains($0) }
        // The server lists somebody the new list leaves out, whatever the record said.
        let isRemovingListedShare = view.shares.contains { share in
            guard let grantee = Self.grantee(of: share), !newGrantees.contains(grantee) else { return false }
            return !share.recipients.isEmpty
        }
        // A record that was not checked may have been written by somebody who holds the item key.
        let isReplacingUntrustedRecord = !isRecordChecked && (!view.shares.isEmpty || view.isUsableByAgent)

        if isWidening || !isRecordChecked {
            let signed = try PasswordsCrypto.withSignedAccess(
                accountKey: opener.accountKey, itemId: itemId, itemKind: opened.itemKind, fields: opened.fields, access: newAccess,
                highestVersionSeen: highestRecordVersion(of: opened))
            _ = try await saveFields(signed, of: view, opener: opener)
            view = try ownedView(itemId)
        }

        let itemKey = try opener.itemKey(for: view)
        let alreadyKeyedUserIds = Set(view.shares.flatMap(\.recipients).filter(\.hasKey).map(\.userId))
        var circleIdsByUserId: [String: [String]] = [:]
        for recipient in recipients { circleIdsByUserId[recipient.userId, default: []] += recipient.circleIds }
        var keys: [PasswordShareKeyInput] = []
        var sealedKeys: [String: String] = [:]
        var sealedUserIds = Set<String>()
        for recipient in recipients where recipient.userId != userId && !alreadyKeyedUserIds.contains(recipient.userId) {
            guard let publicKey = recipient.publicKey, !recipient.hasTooMuchShared, sealedUserIds.insert(recipient.userId).inserted else {
                continue
            }
            // The record the owner sealed decides, not the server's list of shares.
            let circleIds = (circleIdsByUserId[recipient.userId] ?? []) + Self.circleIds(listing: recipient.userId, in: view)
            guard PasswordsItemAccess.isReachedByAccess(newAccess, recipientUserId: recipient.userId, circleIdsOfRecipient: circleIds) else {
                continue
            }
            keys.append(
                try Self.shareKey(
                    itemKey: itemKey, itemId: itemId, ownerKeyPair: opener.keyPair, ownerId: userId, recipientUserId: recipient.userId,
                    recipientPublicKey: publicKey, accountSealedItemKey: view.itemKey))
            sealedKeys[recipient.userId] = publicKey.base64EncodedString()
        }
        // The keys sealed for are recorded before the server is told, so a pin store that cannot be
        // written stops the share before anything is sealed for them.
        try recordSealedKeys(sealedKeys)
        let answered = try await call { try await api.setPasswordItemShares(itemId: itemId, grantees: chosenInputs, keys: keys) }
        let kept = try keep(answered, opener: opener)
        recordSealed(
            itemId: itemId,
            change: PasswordsSealedChange(
                userIds: Array(sealedKeys.keys), isReplacing: false,
                chosenUserIds: newGrantees.compactMap { grantee -> String? in
                    if case .person(let chosenUserId) = grantee { return chosenUserId }
                    return nil
                },
                circleShares: Self.circleShares(
                    sealingUserIds: Array(sealedKeys.keys), previouslySealedUserIds: previouslySealedUserIds, in: answered, access: newAccess)))
        // Somebody lost access, and they may hold the item key: it gets replaced. The person's own
        // record says so; a server that answers with the shares unchanged cannot prevent it. A mark
        // the server set on its own is followed as well, which only ever re-keys more.
        guard isNarrowing || isRemovingListedShare || isReplacingUntrustedRecord || answered.needsNewKey else { return kept }
        do {
            return try await rekey(
                itemId: itemId, replacing: nil, baseRevision: nil, accessEdit: .grantees(newGrantees), origin: .choice)
        } catch {
            throw PasswordsError.itemNeedsNewKey(itemId: itemId)
        }
    }

    /// The highest access record version this device has seen for an item with a valid signature,
    /// counting the record the item shows now.
    private func highestRecordVersion(of item: PasswordItem) -> Int {
        max(currentPins().itemMemories[item.id]?.highestRecordVersion ?? 0, item.verifiedAccess?.recordVersion ?? 0)
    }

    /// The grantee of a share the server lists.
    private static func grantee(of share: PasswordShareView) -> PasswordsGrantee? {
        if let circleId = share.granteeCircleId { return .circle(circleId: circleId) }
        if let userId = share.granteeUserId { return .person(userId: userId) }
        return nil
    }

    private static func sealedForRecipient(
        itemKey: Data, itemId: String, ownerKeyPair: PasswordsKeyPair, ownerId: String, recipientUserId: String, recipientPublicKey: Data
    ) throws -> String {
        try PasswordsCrypto.sealForKeyPair(
            itemKey, ownerKeyPair: ownerKeyPair, ownerId: ownerId, recipientPublicKey: recipientPublicKey, recipientId: recipientUserId,
            itemId: itemId
        ).jsonText
    }

    /// The key for `SetPasswordItemShares`. `accountSealedItemKey` is the item key sealed with the
    /// account key, exactly as the item showed it when this device opened it to seal this key: the
    /// server refuses the key when the item has another by now.
    private static func shareKey(
        itemKey: Data, itemId: String, ownerKeyPair: PasswordsKeyPair, ownerId: String, recipientUserId: String, recipientPublicKey: Data,
        accountSealedItemKey: String
    ) throws -> PasswordShareKeyInput {
        PasswordShareKeyInput(
            recipientUserId: recipientUserId,
            itemKeyByRecipientKey: try sealedForRecipient(
                itemKey: itemKey, itemId: itemId, ownerKeyPair: ownerKeyPair, ownerId: ownerId, recipientUserId: recipientUserId,
                recipientPublicKey: recipientPublicKey),
            recipientPublicKey: recipientPublicKey.base64EncodedString(), itemKey: accountSealedItemKey)
    }

    /// The same recipients as `recipients(...)` for views the server already sent (the owner's
    /// `shares` of an item), marked with what this device knows of each key. `circleId` is the
    /// circle of the share they came from, if it is a circle's.
    public func recipients(of views: [PasswordRecipientView], circleId: String? = nil) -> [PasswordRecipient] {
        makeRecipients(views, circleIds: circleId.map { [$0] } ?? [])
    }

    /// The people whose key changed from the one this device knew (as an item's owner, or as a
    /// recipient the person shared with), each once, until the person dismisses the notice. The
    /// new key was accepted: nothing waits on the notice.
    public func keyChanges() -> [PasswordsKeyChange] {
        let pins = currentPins()
        var names: [String: String] = [:]
        for view in loadedContents()?.items ?? [] {
            if view.isSharedWithMe { names[view.ownerUserId] = view.ownerName }
            for recipient in view.shares.flatMap(\.recipients) { names[recipient.userId] = recipient.name }
        }
        var changes: [PasswordsKeyChange] = []
        for (otherUserId, previousPublicKey) in pins.changedPublicKeys {
            guard let currentPublicKey = pins.knownPublicKeys[otherUserId] else { continue }
            changes.append(
                PasswordsKeyChange(
                    userId: otherUserId, name: names[otherUserId] ?? "", oldSafetyCode: PasswordsOpener.safetyCodeText(publicKey: previousPublicKey),
                    newSafetyCode: PasswordsOpener.safetyCodeText(publicKey: currentPublicKey), newPublicKey: currentPublicKey))
        }
        return changes.sorted { ($0.name, $0.userId) < ($1.name, $1.userId) }
    }

    /// The people this device sealed an item for through a circle for the first time, each until the
    /// person dismisses the notice. An item that is not in the copy any more is left out.
    public func circleShareNotices() -> [PasswordsCircleShareNotice] {
        let pins = currentPins()
        guard let opener, let current = loadedContents() else { return [] }
        var notices: [PasswordsCircleShareNotice] = []
        for (itemId, memory) in pins.itemMemories where !memory.circleShares.isEmpty {
            guard let view = current.item(id: itemId), view.deletedAt == nil, let opened = try? opener.open(view) else { continue }
            for share in memory.circleShares {
                notices.append(
                    PasswordsCircleShareNotice(
                        itemId: itemId, itemName: opened.name, recipientUserId: share.recipientUserId, recipientName: share.recipientName,
                        circleName: share.circleName))
            }
        }
        return notices.sorted { ($0.recipientName, $0.itemName, $0.id) < ($1.recipientName, $1.itemName, $1.id) }
    }

    /// Dismisses the notice that somebody got an item through a circle.
    public func dismissCircleShareNotice(itemId: String, recipientUserId: String) throws {
        _ = try requireOpener()
        try cache.dismissCircleShare(userId: userId, itemId: itemId, recipientUserId: recipientUserId)
        guard var pins = lastReadPins else { return }
        pins.itemMemories[itemId]?.circleShares.removeAll { $0.recipientUserId == recipientUserId }
        lastReadPins = pins
    }

    /// Dismisses the notice of a changed key. The key stays accepted.
    public func dismissKeyChange(userId otherUserId: String) throws {
        _ = try requireOpener()
        try cache.dismissKeyChange(userId: userId, otherUserId: otherUserId)
    }

    /// Marks the key as compared ("Codes compared") after the person read the safety codes with
    /// its owner. `publicKey` is the key whose code the person was shown (standard base64); it
    /// is recorded as the known key when the device does not know it yet or knew another, and the
    /// person's changed-key notice is cleared. It never gates sharing or opening.
    public func markCompared(userId otherUserId: String, publicKey: String) throws {
        _ = try requireOpener()
        guard let data = Data(base64Encoded: publicKey), PasswordsCrypto.isPublicKeyValid(data), otherUserId != userId else {
            throw PasswordsError.invalidServerAnswer
        }
        if let known = currentPins().knownPublicKeys[otherUserId], known != publicKey {
            try cache.recordChangedKey(userId: userId, otherUserId: otherUserId, publicKey: publicKey)
        }
        try cache.recordCompared(userId: userId, otherUserId: otherUserId, publicKey: publicKey)
        openedItems = [:]
    }

    /// Records the keys (user id to standard base64) this device is about to seal item keys for:
    /// the first one seen for a person becomes their known key, and one that differs from the
    /// known key replaces it and leaves a notice (`keyChanges()`). Nothing is asked.
    private func recordSealedKeys(_ keys: [String: String]) throws {
        let pins = currentPins()
        for (otherUserId, publicKey) in keys.sorted(by: { $0.key < $1.key }) {
            guard let known = pins.knownPublicKeys[otherUserId] else {
                try cache.recordKnownIfAbsent(userId: userId, otherUserId: otherUserId, publicKey: publicKey)
                continue
            }
            if known != publicKey { try cache.recordChangedKey(userId: userId, otherUserId: otherUserId, publicKey: publicKey) }
        }
    }

    /// The recipients of the person's items who cannot open them yet, each
    /// with what the device knows of their key.
    public func waitingShares() async throws -> [PasswordWaitingShare] {
        let opener = try requireOpener()
        let views = try await call { try await api.listWaitingPasswordShares() }
        return views.map { view in
            let name = itemView(id: view.itemId).flatMap { try? opener.open($0).name }
            return PasswordWaitingShare(
                itemId: view.itemId, itemName: name, recipient: Self.recipient(view.recipient, opener: opener), hasMoreWaiting: view.hasMoreWaiting)
        }
    }

    /// A recipient marked with what the opener knows of their key.
    private static func recipient(_ view: PasswordRecipientView, opener: PasswordsOpener) -> PasswordRecipient {
        PasswordRecipient(
            view: view, knownPublicKey: opener.knownPublicKeys[view.userId], comparedPublicKey: opener.comparedPublicKeys[view.userId])
    }

    /// What `completeWaitingShares` works out from the server's list of waiting recipients and the copy.
    private struct WaitingPlan {
        var keys: [PasswordItemShareKeyInput] = []
        /// The key each recipient sealed for was given, user id to standard base64.
        var sealedKeys: [String: String] = [:]
        /// The people sealed for in each item, by item id.
        var sealedUserIdsByItemId: [String: [String]] = [:]
        /// The people sealed for through a circle for the first time, by item id.
        var circleSharesByItemId: [String: [PasswordsCircleShare]] = [:]
        /// The items with somebody waiting that this plan did not seal for because it is not the
        /// person's own action and this device cannot vouch for it (`itemIdsNeedingYou`).
        var itemIdsLeftForPerson: Set<String> = []
        /// The server names an item the copy does not hold, or a person the owner's record would
        /// reach but the copy's shares do not list (they joined a circle after the copy was made).
        var isCopyBehind = false
        /// The names of waiting people who have too much shared with them already: skipped.
        var tooMuchSharedNames: Set<String> = []
    }

    /// `isByPerson` is whether the person started this (they opened the item and pressed the
    /// button): then every waiting recipient the owner's verified record reaches is sealed for.
    /// Otherwise the device acts by itself and seals only for an item it remembers and for people
    /// it sealed that item for before; the rest is left for the person.
    private func waitingPlan(_ waiting: [PasswordWaitingShareView], opener: PasswordsOpener, isByPerson: Bool) throws -> WaitingPlan {
        var plan = WaitingPlan()
        let memories = currentPins().itemMemories
        for entry in waiting {
            let recipient = Self.recipient(entry.recipient, opener: opener)
            guard let publicKey = recipient.publicKey, recipient.userId != userId else { continue }
            guard let view = itemView(id: entry.itemId) else {
                plan.isCopyBehind = true
                continue
            }
            guard !view.isSharedWithMe, view.deletedAt == nil, let opened = try? opener.open(view) else { continue }
            // A record that is not the person's own (missing, not signed by them, or an older one the
            // server sent back) reaches nobody: nothing is sealed from it.
            guard let access = opened.verifiedAccess else { continue }
            let circleIds = Self.circleIds(listing: recipient.userId, in: view)
            guard PasswordsItemAccess.isReachedByAccess(access, recipientUserId: recipient.userId, circleIdsOfRecipient: circleIds) else {
                // The server lists somebody the owner's record does not reach: nothing is sealed. But if the
                // record would reach them as a member of one of its circles, the copy of the circle may be old.
                let circleIdsOfRecord = access.grantees.compactMap { grantee -> String? in
                    if case .circle(let circleId) = grantee { return circleId }
                    return nil
                }
                if PasswordsItemAccess.isReachedByAccess(access, recipientUserId: recipient.userId, circleIdsOfRecipient: circleIdsOfRecord) {
                    plan.isCopyBehind = true
                }
                continue
            }
            guard let itemKey = try? opener.itemKey(for: view) else { continue }
            // The server stores no key for somebody who has too much shared with them: sealing one
            // would only be tried again on every pass.
            if recipient.hasTooMuchShared {
                plan.tooMuchSharedNames.insert(recipient.name)
                continue
            }
            let memory = memories[entry.itemId] ?? PasswordsItemMemory()
            if !isByPerson,
                !(isRememberedForAutomaticAction(itemId: entry.itemId)
                    && (memory.sealedForUserIds.contains(recipient.userId) || memory.chosenUserIds.contains(recipient.userId)))
            {
                plan.itemIdsLeftForPerson.insert(entry.itemId)
                continue
            }
            plan.sealedKeys[recipient.userId] = publicKey.base64EncodedString()
            plan.sealedUserIdsByItemId[entry.itemId, default: []].append(recipient.userId)
            plan.circleSharesByItemId[entry.itemId, default: []] += Self.circleShares(
                sealingUserIds: [recipient.userId], previouslySealedUserIds: Set(memory.sealedForUserIds), in: view, access: access)
            plan.keys.append(
                try Self.itemShareKey(
                    itemKey: itemKey, itemId: entry.itemId, ownerKeyPair: opener.keyPair, ownerId: userId, recipient: recipient,
                    recipientPublicKey: publicKey, accountSealedItemKey: view.itemKey))
        }
        return plan
    }

    /// Seals the keys of items whose recipients were waiting (they set up
    /// Passwords, or set it up again with a new key, after the share was made) for their
    /// current public keys, at once, but only when the owner's sealed record reaches the recipient (a
    /// person grantee by user id, a circle grantee when the server lists them as a member). The key
    /// given for a first-seen person is recorded as their known key; one that differs from the known
    /// key replaces it and leaves a notice (`keyChanges()`). Nobody is asked about a key.
    ///
    /// Done by itself, this seals only for an item the device remembers from before this session or
    /// from something it did itself, and only for people it sealed that item for before or that the
    /// person named directly when they set who it is shared with on this device (a circle's
    /// members are the server's word, so a member who joined later is not one of them). Everything
    /// else is left for the person (`itemIdsNeedingYou`, `finishSharing(itemId:)`): a device that has
    /// never seen an item cannot tell an old signed record from the newest one, and a person added
    /// to an old record is exactly what an old record would bring back. A waiting recipient the
    /// server lists that the record does not reach is ignored. When the server names an item the
    /// copy does not hold, or somebody the record would reach but the copy's shares do not
    /// list, the copy is read in full once and the list checked again.
    @discardableResult
    public func completeWaitingShares() async throws -> PasswordsCompletedShares {
        try await completeWaitingShares(onlyItemId: nil, isByPerson: false)
    }

    /// The same for one item, started by the person: every waiting recipient the owner's verified
    /// record reaches gets a key, whether or not this device sealed for them before. A circle
    /// member sealed for for the first time is shown as a notice (`circleShareNotices()`).
    @discardableResult
    func completeWaitingShares(onlyItemId itemId: String) async throws -> PasswordsCompletedShares {
        itemIdsActedOn.insert(itemId)
        return try await completeWaitingShares(onlyItemId: itemId, isByPerson: true)
    }

    private func completeWaitingShares(onlyItemId: String?, isByPerson: Bool) async throws -> PasswordsCompletedShares {
        var isFullReadDone = false
        var isRetriedAfterChange = false
        while true {
            let opener = try requireOpener()
            var waiting = try await call { try await api.listWaitingPasswordShares() }
            let hasMoreWaiting = waiting.contains { $0.hasMoreWaiting }
            if let onlyItemId { waiting = waiting.filter { $0.itemId == onlyItemId } }
            let plan = try waitingPlan(waiting, opener: opener, isByPerson: isByPerson)
            let tooMuchSharedNames = plan.tooMuchSharedNames.sorted()
            if plan.isCopyBehind && !isFullReadDone {
                isFullReadDone = true
                try await sync(isFull: true)
                continue
            }
            if let onlyItemId {
                shareItemIdsLeftForPerson.remove(onlyItemId)
            } else {
                shareItemIdsLeftForPerson = plan.itemIdsLeftForPerson
            }
            guard !plan.keys.isEmpty else {
                return PasswordsCompletedShares(addedKeyCount: 0, tooMuchSharedNames: tooMuchSharedNames, hasMoreWaiting: hasMoreWaiting)
            }
            try recordSealedKeys(plan.sealedKeys)
            do {
                let addedKeyCount = try await call { try await api.addPasswordShareKeys(plan.keys) }
                for (sealedItemId, sealedUserIds) in plan.sealedUserIdsByItemId {
                    recordSealed(
                        itemId: sealedItemId,
                        change: PasswordsSealedChange(
                            userIds: sealedUserIds, isReplacing: false, circleShares: plan.circleSharesByItemId[sealedItemId] ?? []))
                }
                // The copy's shares still say these people have no key: read the changed items again.
                if addedKeyCount > 0 { _ = try? await sync() }
                return PasswordsCompletedShares(
                    addedKeyCount: addedKeyCount, tooMuchSharedNames: tooMuchSharedNames, hasMoreWaiting: hasMoreWaiting)
            } catch PasswordsError.itemChangedOnAnotherDevice where !isRetriedAfterChange {
                // An item got a new key since the copy was made: read it again, then seal from that.
                isRetriedAfterChange = true
                try await sync()
            }
        }
    }

    private static func itemShareKey(
        itemKey: Data, itemId: String, ownerKeyPair: PasswordsKeyPair, ownerId: String, recipient: PasswordRecipient,
        recipientPublicKey: Data, accountSealedItemKey: String? = nil
    ) throws -> PasswordItemShareKeyInput {
        PasswordItemShareKeyInput(
            itemId: itemId, recipientUserId: recipient.userId,
            itemKeyByRecipientKey: try sealedForRecipient(
                itemKey: itemKey, itemId: itemId, ownerKeyPair: ownerKeyPair, ownerId: ownerId, recipientUserId: recipient.userId,
                recipientPublicKey: recipientPublicKey),
            recipientPublicKey: recipientPublicKey.base64EncodedString(), itemKey: accountSealedItemKey)
    }

    // MARK: The agent

    /// Lets the agent use one item, or stops it. The owner's record sealed in the item's fields
    /// (`access.isUsableByAgent`) is what says so; the server's flag follows it, and the agent's
    /// opener refuses an item whose record says no. The calls go in the order that keeps the record
    /// from ever being wider than what the person chose:
    ///
    /// - Turning it on saves the fields with the new record first, then seals the item key for the
    ///   server's agent key and tells the server (`SetPasswordItemUsableByAgent`), so the server can
    ///   open this item and nothing else.
    /// - Turning it off tells the server first, which stops the agent at once, then gives the item a
    ///   new item key (`RekeyPasswordItem`) whose fields carry the record with the agent off, so a
    ///   copy of the old agent key opens nothing saved later. If that fails after the server was
    ///   told, this throws `PasswordsError.itemNeedsNewKey`; the server marks the item and
    ///   `giveNewKey(itemId:)` tries again.
    ///
    /// A card cannot be used by the agent, and only the owner can allow it. An item whose record was
    /// not checked (`PasswordItem.sharingCheck`) cannot be turned on: `PasswordsError.sharingCouldNotBeChecked`,
    /// until the person sets sharing again with `setShares`.
    public func setUsableByAgent(itemId: String, isUsable: Bool) async throws -> PasswordItem {
        var opener = try requireOpener()
        var view = try ownedView(itemId)
        // The server keeps no key for the agent on a deleted item; do not ask it to.
        guard view.deletedAt == nil else { throw PasswordsError.itemNotFound }
        itemIdsActedOn.insert(itemId)
        if view.needsNewKey, try await rekeyIfAllowed(itemId: itemId) {
            opener = try requireOpener()
            view = try ownedView(itemId)
        }
        let opened = try opener.open(view)
        let isRecordOn = opened.verifiedAccess?.isUsableByAgent == true
        guard isUsable else {
            // Off. Nothing to do when neither the record nor the server says the agent may use it.
            guard isRecordOn || view.isUsableByAgent else { return opened }
            if view.isUsableByAgent {
                let answered = try await call {
                    try await api.setPasswordItemUsableByAgent(itemId: itemId, isUsableByAgent: false, itemKeyByAgentKey: nil)
                }
                _ = try keep(answered, opener: opener)
            }
            do {
                return try await rekey(
                    itemId: itemId, replacing: nil, baseRevision: nil, accessEdit: .agentUse(false), origin: .choice)
            } catch {
                throw PasswordsError.itemNeedsNewKey(itemId: itemId)
            }
        }
        guard view.itemKind != PasswordsItemKind.card.rawValue else { throw PasswordsError.cardCannotBeUsedByAgent }
        // The record is the person's consent: one that was not checked cannot be built on. The
        // person sets sharing again first, which writes a record that can.
        guard opened.sharingCheck == .verified else { throw PasswordsError.sharingCouldNotBeChecked(itemId: itemId) }
        if !isRecordOn {
            let newAccess = PasswordsItemAccess.accessWithAgentUse(opened.verifiedAccess, isUsableByAgent: true)
            let signed = try PasswordsCrypto.withSignedAccess(
                accountKey: opener.accountKey, itemId: itemId, itemKind: opened.itemKind, fields: opened.fields, access: newAccess,
                highestVersionSeen: highestRecordVersion(of: opened))
            _ = try await saveFields(signed, of: view, opener: opener)
            view = try ownedView(itemId)
        }
        let sealedItemKey = try await agentCopy(of: try opener.itemKey(for: view), itemId: itemId, opener: opener)
        let answered = try await call {
            try await api.setPasswordItemUsableByAgent(itemId: itemId, isUsableByAgent: true, itemKeyByAgentKey: sealedItemKey)
        }
        let kept = try keep(answered, opener: opener)
        recordSealed(itemId: itemId, change: PasswordsSealedChange(userIds: [], isReplacing: false, didMakeAgentCopy: true))
        return kept
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

    /// The items that still need a new key but were held back by the 10 minute wait or the limit
    /// per session. The screen shows them as needing attention; the person can ask for the key
    /// (`giveNewKey(itemId:)`), which is not held back.
    public var itemIdsNeedingAttention: Set<String> { heldBackItemIds.intersection(itemIdsNeedingNewKey) }

    /// Whether the server marked the item as left with an old key that somebody who lost access may still hold.
    public func needsNewKey(itemId: String) -> Bool {
        guard let view = itemView(id: itemId) else { return false }
        return !view.isSharedWithMe && view.needsNewKey
    }

    /// The person's own items the server marked as needing a new key (`giveNewKey`), deleted ones not included.
    public var itemIdsNeedingNewKey: [String] {
        (loadedContents()?.items ?? []).filter { !$0.isSharedWithMe && $0.needsNewKey && $0.deletedAt == nil }.map(\.id)
    }

    /// Gives the item a new item key now, because the person asked: the retry after a removal whose
    /// re-key failed, or the button on an item this device would not give a key by itself
    /// (`itemIdsNeedingYou`). It is the person's own action, so it seals for everyone the owner's
    /// verified record reaches who has a key, and for the agent if the record says so.
    /// `PasswordsError.sharingCouldNotBeChecked` when the item's record is not the person's own.
    @discardableResult
    public func giveNewKey(itemId: String) async throws -> PasswordItem {
        _ = try ownedView(itemId)
        return try await rekey(itemId: itemId, replacing: nil, baseRevision: nil, accessEdit: .none, origin: .request)
    }

    /// Finishes the sharing of one item the person opened: gives it a new key when the server
    /// marked it, and a key to everybody waiting whom the owner's verified record reaches. This is
    /// what "Open this item to finish sharing it" asks for (`itemIdsNeedingYou`).
    public func finishSharing(itemId: String) async throws {
        let view = try ownedView(itemId)
        guard view.deletedAt == nil else { throw PasswordsError.itemNotFound }
        if view.needsNewKey { try await giveNewKey(itemId: itemId) }
        try await completeWaitingShares(onlyItemId: itemId)
        keyItemIdsLeftForPerson.remove(itemId)
        shareItemIdsLeftForPerson.remove(itemId)
    }

    /// The items an automatic action left for the person, because this device has no memory of them
    /// from before this session or from something it did itself, or because a waiting person is
    /// somebody it never sealed for: the screen says "Open this item to finish sharing it" for
    /// each, and `finishSharing(itemId:)` does it.
    public var itemIdsNeedingYou: Set<String> {
        keyItemIdsLeftForPerson.intersection(itemIdsNeedingNewKey).union(shareItemIdsLeftForPerson)
    }

    /// Gives a new key to every one of the person's items the server marked, after a sync, as far
    /// as a new key given by itself may go: only for an item this device remembers from before this
    /// session or from something it did itself, only to people this device sealed it for before who
    /// are still in the record it checked, at most once in ten minutes for an item and
    /// `mostAutomaticRekeysPerSession` in a session. Returns the ids of those that got one; an item
    /// that could not is left marked for the next time (`itemIdsNeedingAttention` names the ones
    /// held back by the wait or the limit, `itemIdsNeedingYou` the ones left for the person).
    public func giveNewKeysToMarkedItems() async -> [String] {
        var givenItemIds: [String] = []
        keyItemIdsLeftForPerson = []
        for itemId in itemIdsNeedingNewKey {
            if (try? await rekey(itemId: itemId, replacing: nil, baseRevision: nil, accessEdit: .none, origin: .automatic)) != nil {
                givenItemIds.append(itemId)
            }
        }
        return givenItemIds
    }

    /// Gives the item a new key first when it was marked, as far as one given by itself may go.
    /// False when it may not (the record is not the person's own, or one was given a moment ago),
    /// so the caller goes on with the key the item has; any other failure is thrown.
    private func rekeyIfAllowed(itemId: String) async throws -> Bool {
        do {
            _ = try await rekey(itemId: itemId, replacing: nil, baseRevision: nil, accessEdit: .none, origin: .automatic)
            return true
        } catch PasswordsError.itemNeedsAttention, PasswordsError.sharingCouldNotBeChecked, PasswordsError.itemNeedsYou {
            return false
        }
    }

    /// What a re-key changes in the item's access record, on top of the fields it re-seals.
    private enum AccessEdit {
        case none
        /// The grantees the person now chose.
        case grantees([PasswordsGrantee])
        /// Whether the agent may use the item.
        case agentUse(Bool)
    }

    /// Who asked for a new key, which decides who it is sealed for.
    private enum RekeyOrigin {
        /// The person chose who has access (they removed somebody, or turned the agent off): the
        /// key is sealed for everyone the new record reaches who has a key.
        case choice
        /// The person asked for a new key without choosing who (the button on the item, the retry
        /// after a removal, restoring an item): sealed for everyone the verified record reaches who
        /// has a key, as the record is the person's own word.
        case request
        /// Nobody asked (the server marked the item, a save found it marked): only for an item this
        /// device remembers, only for people this device sealed for before, and at most once in
        /// ten minutes for an item and 20 in a session.
        case automatic
    }

    /// Makes a new item key for an item the person owns and seals everything with it: the fields
    /// (`replacing` when a save brings new ones, else the item's own, with `accessEdit` applied to
    /// their access record and signed again), the person's copy, a copy for each recipient the
    /// owner's record still reaches and who has a key, and the
    /// agent's copy when the record says the agent may use the item.
    ///
    /// For the automatic origin the new key never reaches further than what this device did itself:
    /// the item has to be one the device remembers, a recipient has to be somebody this device
    /// sealed the item for before (`PasswordsItemMemory.sealedForUserIds`), and the agent copy is
    /// made only if this device made one (`didMakeAgentCopy`) and only from a record that is the
    /// person's own. Anybody else waits for the person. One the record does not reach gets
    /// nothing, whatever the server's list of shares says. When the server
    /// says the item changed on another device and the change was not based on a revision the caller
    /// gave, the copy is brought up to date and this is tried once more.
    private func rekey(
        itemId: String, replacing: PasswordsItemFields?, baseRevision: Int64?, accessEdit: AccessEdit, origin: RekeyOrigin
    ) async throws -> PasswordItem {
        if origin == .automatic {
            // An item whose record is not the person's own is not given a key by itself, and does not
            // use up a slot.
            guard try requireOpener().open(try ownedView(itemId)).sharingCheck == .verified else {
                throw PasswordsError.sharingCouldNotBeChecked(itemId: itemId)
            }
            // An item this device has no memory of is left for the person.
            guard isRememberedForAutomaticAction(itemId: itemId) else {
                keyItemIdsLeftForPerson.insert(itemId)
                throw PasswordsError.itemNeedsYou(itemId: itemId)
            }
            try reserveAutomaticRekey(itemId: itemId)
        }
        do {
            return try await rekeyOnce(itemId: itemId, replacing: replacing, baseRevision: baseRevision, accessEdit: accessEdit, origin: origin)
        } catch PasswordsError.itemChangedOnAnotherDevice where baseRevision == nil && !isLoadingItemsOnDemand {
            try await sync()
            return try await rekeyOnce(itemId: itemId, replacing: nil, baseRevision: nil, accessEdit: accessEdit, origin: origin)
        }
    }

    /// Counts one new key given by itself, or refuses it: not for the same item within ten minutes,
    /// and not more than `mostAutomaticRekeysPerSession` in all. A refused item is listed in
    /// `itemIdsNeedingAttention`.
    private func reserveAutomaticRekey(itemId: String) throws {
        let current = now()
        if let last = lastAutomaticRekeyAt[itemId], current.timeIntervalSince(last) < Self.automaticRekeyCooldownSeconds {
            heldBackItemIds.insert(itemId)
            throw PasswordsError.itemNeedsAttention(itemId: itemId)
        }
        guard automaticRekeyCount < Self.mostAutomaticRekeysPerSession else {
            heldBackItemIds.insert(itemId)
            throw PasswordsError.itemNeedsAttention(itemId: itemId)
        }
        lastAutomaticRekeyAt[itemId] = current
        automaticRekeyCount += 1
        heldBackItemIds.remove(itemId)
    }

    private func rekeyOnce(
        itemId: String, replacing: PasswordsItemFields?, baseRevision: Int64?, accessEdit: AccessEdit, origin: RekeyOrigin
    ) async throws -> PasswordItem {
        let opener = try requireOpener()
        let view = try ownedView(itemId)
        guard view.deletedAt == nil else { throw PasswordsError.itemNotFound }
        let opened = try opener.open(view)
        // A new key given without the person choosing who has access builds on the record, so the
        // record has to be the person's own.
        if origin != .choice, opened.sharingCheck != .verified { throw PasswordsError.sharingCouldNotBeChecked(itemId: itemId) }
        var fields: PasswordsItemFields
        if let replacing {
            guard view.itemKind == replacing.itemKind.rawValue else { throw PasswordsError.itemKindCannotChange }
            fields = replacing
        } else {
            fields = opened.fields
        }
        // What the record is edited from is the record the person owns; a record that was not
        // checked counts as one that grants nobody and does not let the agent in.
        switch accessEdit {
        case .none:
            break
        case .grantees(let grantees):
            let edited = PasswordsItemAccess.accessWithGrantees(opened.verifiedAccess, grantees: grantees)
            fields = try PasswordsCrypto.withSignedAccess(
                accountKey: opener.accountKey, itemId: itemId, itemKind: opened.itemKind, fields: fields, access: edited,
                highestVersionSeen: highestRecordVersion(of: opened))
        case .agentUse(let isUsableByAgent):
            let edited = PasswordsItemAccess.accessWithAgentUse(opened.verifiedAccess, isUsableByAgent: isUsableByAgent)
            fields = try PasswordsCrypto.withSignedAccess(
                accountKey: opener.accountKey, itemId: itemId, itemKind: opened.itemKind, fields: fields, access: edited,
                highestVersionSeen: highestRecordVersion(of: opened))
        }
        let memory = currentPins().itemMemories[itemId] ?? PasswordsItemMemory()
        if origin != .automatic { itemIdsActedOn.insert(itemId) }
        let newItemKey = PasswordsCrypto.makeKey()
        let sealedFields = try PasswordsCrypto.sealItemFields(fields, itemKey: newItemKey, itemId: itemId)
        let sealedItemKey = try PasswordsCrypto.seal(
            newItemKey, key: opener.accountKey, associatedData: PasswordsCrypto.itemKeyAssociatedData(itemId: itemId))
        var shareKeys: [PasswordItemShareKeyInput] = []
        var sealedKeys: [String: String] = [:]
        var seenUserIds = Set<String>()
        for recipientView in view.shares.flatMap(\.recipients) where recipientView.userId != userId {
            guard seenUserIds.insert(recipientView.userId).inserted else { continue }
            let circleIds = Self.circleIds(listing: recipientView.userId, in: view)
            guard PasswordsItemAccess.isReachedByAccess(fields.access, recipientUserId: recipientView.userId, circleIdsOfRecipient: circleIds)
            else { continue }
            if origin == .automatic, !memory.sealedForUserIds.contains(recipientView.userId) { continue }
            let recipient = Self.recipient(recipientView, opener: opener)
            guard let publicKey = recipient.publicKey else { continue }
            sealedKeys[recipient.userId] = publicKey.base64EncodedString()
            shareKeys.append(
                try Self.itemShareKey(
                    itemKey: newItemKey, itemId: itemId, ownerKeyPair: opener.keyPair, ownerId: userId, recipient: recipient,
                    recipientPublicKey: publicKey))
        }
        var agentCopyText: String?
        let isAgentCopyAllowed = origin != .automatic || memory.didMakeAgentCopy
        if isAgentCopyAllowed && fields.access?.isUsableByAgent == true && view.isUsableByAgent
            && view.itemKind != PasswordsItemKind.card.rawValue
        {
            agentCopyText = try await agentCopy(of: newItemKey, itemId: itemId, opener: opener)
        }
        try recordSealedKeys(sealedKeys)
        let input = PasswordItemRekeyInput(
            id: itemId, baseRevision: baseRevision ?? view.revision, encryptedFields: sealedFields.jsonText, itemKey: sealedItemKey.jsonText,
            shareKeys: shareKeys, itemKeyByAgentKey: agentCopyText)
        let answered = try await call { try await api.rekeyPasswordItem(input) }
        let kept = try keep(answered, opener: opener)
        recordSealed(
            itemId: itemId,
            change: PasswordsSealedChange(
                userIds: Array(sealedKeys.keys), isReplacing: true, didMakeAgentCopy: agentCopyText != nil,
                circleShares: Self.circleShares(
                    sealingUserIds: Array(sealedKeys.keys), previouslySealedUserIds: Set(memory.sealedForUserIds), in: view,
                    access: fields.access)))
        keyItemIdsLeftForPerson.remove(itemId)
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
