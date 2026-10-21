import Foundation

// What the phone sends the server about Passwords, and what it reads back.
// Every value that is "sealed" is text the device made (the JSON of
// PasswordsCrypto.swift); the server stores it and hands it back without
// reading it. The names are the server's
// (internal/api/v1api/apigraph/passwords.go) and the dashboard's.
// PasswordsSession.swift is the code that seals and opens them; this file is
// only the shapes and the calls.

// MARK: Views

/// PasswordsSetupView is whether the person has set Passwords up, and what a
/// device needs to make the passphrase key again.
public struct PasswordsSetupView: Codable, Equatable, Sendable {
    public let isSetUp: Bool
    /// The passphrase salt as standard base64; nil until set up.
    public let passphraseSalt: String?
    public let passphraseIterationCount: Int?
    /// The person's public key as standard base64 (65 bytes); nil until set up.
    public let publicKey: String?
    public let lockAfterMinutes: Int
    public let itemCount: Int

    public init(
        isSetUp: Bool, passphraseSalt: String? = nil, passphraseIterationCount: Int? = nil, publicKey: String? = nil,
        lockAfterMinutes: Int, itemCount: Int
    ) {
        self.isSetUp = isSetUp
        self.passphraseSalt = passphraseSalt
        self.passphraseIterationCount = passphraseIterationCount
        self.publicKey = publicKey
        self.lockAfterMinutes = lockAfterMinutes
        self.itemCount = itemCount
    }
}

/// PasswordsUnlockView is what a right proof opens: the account key sealed by
/// the secret the proof came from, and the person's key pair.
public struct PasswordsUnlockView: Codable, Equatable, Sendable {
    public let accountKeyWrapped: String
    public let privateKeyByAccountKey: String
    public let publicKey: String

    public init(accountKeyWrapped: String, privateKeyByAccountKey: String, publicKey: String) {
        self.accountKeyWrapped = accountKeyWrapped
        self.privateKeyByAccountKey = privateKeyByAccountKey
        self.publicKey = publicKey
    }
}

/// PasswordRecipientView is a person an item reaches (or could reach).
public struct PasswordRecipientView: Codable, Hashable, Sendable {
    public let userId: String
    public let name: String
    public let username: String
    /// Standard base64 of the 65 byte public key; nil when the person has not
    /// set up Passwords.
    public let publicKey: String?
    /// Whether the item already has a key sealed for this person's current public key.
    public let hasKey: Bool
    /// Whether the person already has the most the server lets others share with them, so a key
    /// for this item is not stored for them. An older server does not send it: then false.
    public let hasTooMuchShared: Bool

    public init(userId: String, name: String, username: String, publicKey: String?, hasKey: Bool, hasTooMuchShared: Bool = false) {
        self.userId = userId
        self.name = name
        self.username = username
        self.publicKey = publicKey
        self.hasKey = hasKey
        self.hasTooMuchShared = hasTooMuchShared
    }

    private enum CodingKeys: String, CodingKey {
        case userId, name, username, publicKey, hasKey, hasTooMuchShared
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        userId = try container.decode(String.self, forKey: .userId)
        name = try container.decode(String.self, forKey: .name)
        username = try container.decode(String.self, forKey: .username)
        publicKey = try container.decodeIfPresent(String.self, forKey: .publicKey)
        hasKey = try container.decode(Bool.self, forKey: .hasKey)
        hasTooMuchShared = try container.decodeIfPresent(Bool.self, forKey: .hasTooMuchShared) ?? false
    }
}

/// PasswordShareView is one share of an item: with a person or with a circle.
public struct PasswordShareView: Codable, Hashable, Identifiable, Sendable {
    public let id: String
    public let granteeUserId: String?
    public let granteeCircleId: String?
    public let granteeName: String
    /// Everyone the share reaches.
    public let recipients: [PasswordRecipientView]

    public init(id: String, granteeUserId: String?, granteeCircleId: String?, granteeName: String, recipients: [PasswordRecipientView]) {
        self.id = id
        self.granteeUserId = granteeUserId
        self.granteeCircleId = granteeCircleId
        self.granteeName = granteeName
        self.recipients = recipients
    }
}

/// PasswordItemView is an item as the server holds it: everything in it is
/// sealed except who owns it, its kind and its times. `itemKey` is the item key
/// sealed for the owner with the account key, or for a recipient with their
/// key pair (`isSharedWithMe`). `ownerPublicKey` is the owner's current public
/// key (standard base64, 65 bytes): a recipient opens a shared item key only
/// when it equals the key the device already knows for that owner.
public struct PasswordItemView: Codable, Hashable, Identifiable, Sendable {
    public let id: String
    public let ownerUserId: String
    public let ownerName: String
    public let ownerPublicKey: String
    public let itemKind: String
    public let encryptedFields: String
    public let itemKey: String
    public let isSharedWithMe: Bool
    public let revision: Int64
    public let isUsableByAgent: Bool
    public let lastUsedByAgentAt: String?
    public let deletedAt: String?
    public let createdAt: String
    public let updatedAt: String
    /// The owner's shares; empty for a recipient.
    public let shares: [PasswordShareView]
    /// Set by the server when access to the item narrowed (somebody was removed, the agent
    /// was turned off, a member left a circle) and the item still has the key they held. Only
    /// `RekeyPasswordItem` clears it. Always false on an item shared with the person.
    public let needsNewKey: Bool

    public init(
        id: String, ownerUserId: String, ownerName: String, ownerPublicKey: String, itemKind: String, encryptedFields: String,
        itemKey: String, isSharedWithMe: Bool, revision: Int64, isUsableByAgent: Bool, lastUsedByAgentAt: String? = nil, deletedAt: String? = nil,
        createdAt: String, updatedAt: String, shares: [PasswordShareView] = [], needsNewKey: Bool = false
    ) {
        self.id = id
        self.ownerUserId = ownerUserId
        self.ownerName = ownerName
        self.ownerPublicKey = ownerPublicKey
        self.itemKind = itemKind
        self.encryptedFields = encryptedFields
        self.itemKey = itemKey
        self.isSharedWithMe = isSharedWithMe
        self.revision = revision
        self.isUsableByAgent = isUsableByAgent
        self.lastUsedByAgentAt = lastUsedByAgentAt
        self.deletedAt = deletedAt
        self.createdAt = createdAt
        self.updatedAt = updatedAt
        self.shares = shares
        self.needsNewKey = needsNewKey
    }

    private enum CodingKeys: String, CodingKey {
        case id, ownerUserId, ownerName, ownerPublicKey, itemKind, encryptedFields, itemKey, isSharedWithMe, revision
        case isUsableByAgent, lastUsedByAgentAt, deletedAt, createdAt, updatedAt, shares, needsNewKey
    }

    /// A phone's copy written before the owner's key was part of an item has none: it reads as empty,
    /// which no shared item opens with, and the next sync replaces it.
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        ownerUserId = try container.decode(String.self, forKey: .ownerUserId)
        ownerName = try container.decode(String.self, forKey: .ownerName)
        ownerPublicKey = try container.decodeIfPresent(String.self, forKey: .ownerPublicKey) ?? ""
        itemKind = try container.decode(String.self, forKey: .itemKind)
        encryptedFields = try container.decode(String.self, forKey: .encryptedFields)
        itemKey = try container.decode(String.self, forKey: .itemKey)
        isSharedWithMe = try container.decode(Bool.self, forKey: .isSharedWithMe)
        revision = try container.decode(Int64.self, forKey: .revision)
        isUsableByAgent = try container.decode(Bool.self, forKey: .isUsableByAgent)
        lastUsedByAgentAt = try container.decodeIfPresent(String.self, forKey: .lastUsedByAgentAt)
        deletedAt = try container.decodeIfPresent(String.self, forKey: .deletedAt)
        createdAt = try container.decode(String.self, forKey: .createdAt)
        updatedAt = try container.decode(String.self, forKey: .updatedAt)
        shares = try container.decode([PasswordShareView].self, forKey: .shares)
        needsNewKey = try container.decodeIfPresent(Bool.self, forKey: .needsNewKey) ?? false
    }

    public var deletedDate: Date? { ServerTime.date(deletedAt) }
    public var lastUsedByAgentDate: Date? { ServerTime.date(lastUsedByAgentAt) }
    public var createdDate: Date? { ServerTime.date(createdAt) }
    public var updatedDate: Date? { ServerTime.date(updatedAt) }
}

/// PasswordItemListView is a page of changes: the items whose revision is above
/// the one asked for, and the id of every item the person can reach now, so a
/// device drops what is gone.
public struct PasswordItemListView: Codable, Equatable, Sendable {
    public let items: [PasswordItemView]
    public let itemIds: [String]
    /// The highest revision among everything visible.
    public let revision: Int64

    public init(items: [PasswordItemView], itemIds: [String], revision: Int64) {
        self.items = items
        self.itemIds = itemIds
        self.revision = revision
    }
}

/// PasswordWaitingShareView is a recipient of one of the caller's items who has
/// set up Passwords but has no key for their current public key yet.
public struct PasswordWaitingShareView: Codable, Hashable, Sendable {
    public let itemId: String
    public let recipient: PasswordRecipientView
    /// Set on every entry when more are waiting than the server lists (the newest items first).
    /// An older server does not send it: then false.
    public let hasMoreWaiting: Bool

    public init(itemId: String, recipient: PasswordRecipientView, hasMoreWaiting: Bool = false) {
        self.itemId = itemId
        self.recipient = recipient
        self.hasMoreWaiting = hasMoreWaiting
    }

    private enum CodingKeys: String, CodingKey {
        case itemId, recipient, hasMoreWaiting
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        itemId = try container.decode(String.self, forKey: .itemId)
        recipient = try container.decode(PasswordRecipientView.self, forKey: .recipient)
        hasMoreWaiting = try container.decodeIfPresent(Bool.self, forKey: .hasMoreWaiting) ?? false
    }
}

// MARK: Inputs

/// PasswordsSetUpInput is everything a device makes when the person sets
/// Passwords up. The salt is standard base64; the proof hashes are base64 of
/// the SHA-256 of the proof bytes.
public struct PasswordsSetUpInput: Codable, Equatable, Sendable {
    public var accountKeyByPassphrase: String
    public var passphraseSalt: String
    public var passphraseIterationCount: Int
    public var passphraseProofHash: String
    public var accountKeyByRecoveryKey: String
    public var recoveryKeyProofHash: String
    public var publicKey: String
    public var privateKeyByAccountKey: String
}

/// PasswordsChangePassphraseInput proves the old passphrase (or the recovery
/// key) and carries the account key sealed by the new passphrase.
public struct PasswordsChangePassphraseInput: Codable, Equatable, Sendable {
    public var proof: String
    public var isRecoveryKey: Bool
    public var accountKeyByPassphrase: String
    public var passphraseSalt: String
    public var passphraseIterationCount: Int
    public var passphraseProofHash: String
}

/// PasswordsReplaceRecoveryKeyInput proves the passphrase and carries the
/// account key sealed by the new recovery key.
public struct PasswordsReplaceRecoveryKeyInput: Codable, Equatable, Sendable {
    public var proof: String
    public var accountKeyByRecoveryKey: String
    public var recoveryKeyProofHash: String
}

/// PasswordItemSaveInput is a new or changed item. The id is made by the
/// device because the encryption names it.
public struct PasswordItemSaveInput: Codable, Equatable, Sendable {
    public var id: String
    public var itemKind: String
    public var encryptedFields: String
    public var itemKey: String
    /// The revision the change is based on; nil for a new item.
    public var baseRevision: Int64?
}

/// PasswordShareGranteeInput is who an item is shared with: a person or a
/// circle, one of the two.
public struct PasswordShareGranteeInput: Codable, Hashable, Sendable {
    public var granteeUserId: String?
    public var granteeCircleId: String?

    public init(granteeUserId: String? = nil, granteeCircleId: String? = nil) {
        self.granteeUserId = granteeUserId
        self.granteeCircleId = granteeCircleId
    }
}

/// PasswordShareKeyInput is the item key sealed for one recipient. The
/// recipient's public key is sent so the server can refuse a key made for a
/// key the person no longer has.
public struct PasswordShareKeyInput: Codable, Hashable, Sendable {
    public var recipientUserId: String
    public var itemKeyByRecipientKey: String
    public var recipientPublicKey: String
    /// The item key sealed with the account key, exactly as the item showed it when this device
    /// opened it to seal this key. `SetPasswordItemShares` requires it whenever keys are given and
    /// refuses the key when the item has another key by now ("changed on another device").
    public var itemKey: String?

    public init(recipientUserId: String, itemKeyByRecipientKey: String, recipientPublicKey: String, itemKey: String? = nil) {
        self.recipientUserId = recipientUserId
        self.itemKeyByRecipientKey = itemKeyByRecipientKey
        self.recipientPublicKey = recipientPublicKey
        self.itemKey = itemKey
    }
}

/// PasswordItemShareKeyInput is a share key for one item, for adding keys to
/// shares that already exist.
public struct PasswordItemShareKeyInput: Codable, Hashable, Sendable {
    public var itemId: String
    public var recipientUserId: String
    public var itemKeyByRecipientKey: String
    public var recipientPublicKey: String
    /// The item key sealed with the account key, exactly as the item showed it when this device
    /// opened it to seal this key. `AddPasswordShareKeys` requires it and refuses the key when the
    /// item has another key by now ("changed on another device"). A re-key leaves it out: its
    /// keys are sealed from the new key in the same call.
    public var itemKey: String?

    public init(
        itemId: String, recipientUserId: String, itemKeyByRecipientKey: String, recipientPublicKey: String, itemKey: String? = nil
    ) {
        self.itemId = itemId
        self.recipientUserId = recipientUserId
        self.itemKeyByRecipientKey = itemKeyByRecipientKey
        self.recipientPublicKey = recipientPublicKey
        self.itemKey = itemKey
    }
}

/// PasswordItemRekeyInput gives an item a new item key when access narrows (the agent
/// stops using it, or somebody is removed from its shares): the fields sealed with the
/// new key, the new key sealed for the owner, for each recipient who keeps access, and for
/// the agent if it still may use the item. The server drops every old share key.
public struct PasswordItemRekeyInput: Codable, Equatable, Sendable {
    public var id: String
    /// The revision the device read; a newer one is refused as "changed on another device".
    public var baseRevision: Int64
    public var encryptedFields: String
    /// The new item key sealed with the account key.
    public var itemKey: String
    public var shareKeys: [PasswordItemShareKeyInput]
    /// The new item key sealed for the agent key; nil when the agent is not to use the item.
    public var itemKeyByAgentKey: String?

    public init(
        id: String, baseRevision: Int64, encryptedFields: String, itemKey: String, shareKeys: [PasswordItemShareKeyInput],
        itemKeyByAgentKey: String?
    ) {
        self.id = id
        self.baseRevision = baseRevision
        self.encryptedFields = encryptedFields
        self.itemKey = itemKey
        self.shareKeys = shareKeys
        self.itemKeyByAgentKey = itemKeyByAgentKey
    }
}

// MARK: The API

/// PasswordsAPI is the server's side of Passwords as calls. `Connection` is
/// the real one; the tests stand in a server of their own.
public protocol PasswordsAPI: Sendable {
    func readPasswordsSetup() async throws -> PasswordsSetupView
    func setUpPasswords(_ input: PasswordsSetUpInput) async throws -> PasswordsSetupView
    /// A right proof opens the account key. A wrong one is refused with
    /// "That passphrase is not right." or "That recovery key is not right.".
    func unlockPasswords(proof: String, isRecoveryKey: Bool) async throws -> PasswordsUnlockView
    func changePasswordsPassphrase(_ input: PasswordsChangePassphraseInput) async throws -> PasswordsSetupView
    func replacePasswordsRecoveryKey(_ input: PasswordsReplaceRecoveryKeyInput) async throws -> PasswordsSetupView
    func updatePasswordsSettings(lockAfterMinutes: Int) async throws -> PasswordsSetupView
    func erasePasswords() async throws -> Bool

    func listPasswordItems(sinceRevision: Int64?) async throws -> PasswordItemListView
    func listPasswordRecipients(granteeUserId: String?, granteeCircleId: String?) async throws -> [PasswordRecipientView]
    func listWaitingPasswordShares() async throws -> [PasswordWaitingShareView]
    /// The server agent key's public key, standard base64 of 65 bytes.
    func readPasswordsAgentKey() async throws -> String

    func savePasswordItem(_ input: PasswordItemSaveInput) async throws -> PasswordItemView
    func deletePasswordItem(id: String) async throws -> PasswordItemView
    func restorePasswordItem(id: String) async throws -> PasswordItemView
    /// Deletes an item the person deleted, and its old versions, for good (`PurgePasswordItem`).
    /// The server needs the person's own tap (the app's sign-in), not a token.
    func purgePasswordItem(id: String) async throws -> Bool
    func setPasswordItemShares(itemId: String, grantees: [PasswordShareGranteeInput], keys: [PasswordShareKeyInput]) async throws
        -> PasswordItemView
    func addPasswordShareKeys(_ keys: [PasswordItemShareKeyInput]) async throws -> Int
    func setPasswordItemUsableByAgent(itemId: String, isUsableByAgent: Bool, itemKeyByAgentKey: String?) async throws -> PasswordItemView
    /// Gives the item a new item key (`RekeyPasswordItem`).
    func rekeyPasswordItem(_ input: PasswordItemRekeyInput) async throws -> PasswordItemView
}

// MARK: Documents

extension Documents {
    // Each shared list of fields is written out in full, nested ones too: the
    // server's test of the app's documents fills in one level of them.
    static let passwordsSetupFields = """
        isSetUp passphraseSalt passphraseIterationCount publicKey lockAfterMinutes itemCount
        """
    static let passwordsUnlockFields = """
        accountKeyWrapped privateKeyByAccountKey publicKey
        """
    static let passwordRecipientFields = """
        userId name username publicKey hasKey hasTooMuchShared
        """
    static let passwordItemFields = """
        id ownerUserId ownerName ownerPublicKey itemKind encryptedFields itemKey isSharedWithMe revision isUsableByAgent lastUsedByAgentAt deletedAt createdAt updatedAt needsNewKey
        shares { id granteeUserId granteeCircleId granteeName recipients { userId name username publicKey hasKey hasTooMuchShared } }
        """

    public static let passwordsSetup = """
        query { ReadPasswordsSetup { \(passwordsSetupFields) } }
        """
    public static let setUpPasswords = """
        mutation ($accountKeyByPassphrase: String!, $passphraseSalt: String!, $passphraseIterationCount: Int!, $passphraseProofHash: String!, $accountKeyByRecoveryKey: String!, $recoveryKeyProofHash: String!, $publicKey: String!, $privateKeyByAccountKey: String!) {
          SetUpPasswords(input: { accountKeyByPassphrase: $accountKeyByPassphrase, passphraseSalt: $passphraseSalt, passphraseIterationCount: $passphraseIterationCount, passphraseProofHash: $passphraseProofHash, accountKeyByRecoveryKey: $accountKeyByRecoveryKey, recoveryKeyProofHash: $recoveryKeyProofHash, publicKey: $publicKey, privateKeyByAccountKey: $privateKeyByAccountKey }) { \(passwordsSetupFields) }
        }
        """
    public static let unlockPasswords = """
        mutation ($proof: String!, $isRecoveryKey: Boolean) {
          UnlockPasswords(proof: $proof, isRecoveryKey: $isRecoveryKey) { \(passwordsUnlockFields) }
        }
        """
    public static let changePasswordsPassphrase = """
        mutation ($proof: String!, $isRecoveryKey: Boolean, $accountKeyByPassphrase: String!, $passphraseSalt: String!, $passphraseIterationCount: Int!, $passphraseProofHash: String!) {
          ChangePasswordsPassphrase(input: { proof: $proof, isRecoveryKey: $isRecoveryKey, accountKeyByPassphrase: $accountKeyByPassphrase, passphraseSalt: $passphraseSalt, passphraseIterationCount: $passphraseIterationCount, passphraseProofHash: $passphraseProofHash }) { \(passwordsSetupFields) }
        }
        """
    public static let replacePasswordsRecoveryKey = """
        mutation ($proof: String!, $accountKeyByRecoveryKey: String!, $recoveryKeyProofHash: String!) {
          ReplacePasswordsRecoveryKey(input: { proof: $proof, accountKeyByRecoveryKey: $accountKeyByRecoveryKey, recoveryKeyProofHash: $recoveryKeyProofHash }) { \(passwordsSetupFields) }
        }
        """
    public static let updatePasswordsSettings = """
        mutation ($lockAfterMinutes: Int!) { UpdatePasswordsSettings(lockAfterMinutes: $lockAfterMinutes) { \(passwordsSetupFields) } }
        """
    public static let erasePasswords = """
        mutation { ErasePasswords }
        """

    public static let passwordItems = """
        query ($sinceRevision: Int64) {
          ListPasswordItems(sinceRevision: $sinceRevision) {
            items { \(passwordItemFields) }
            itemIds
            revision
          }
        }
        """
    public static let passwordRecipients = """
        query ($granteeUserId: String, $granteeCircleId: String) {
          ListPasswordRecipients(granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId) { \(passwordRecipientFields) }
        }
        """
    public static let waitingPasswordShares = """
        query { ListWaitingPasswordShares { itemId hasMoreWaiting recipient { userId name username publicKey hasKey hasTooMuchShared } } }
        """
    public static let passwordsAgentKey = """
        query { ReadPasswordsAgentKey }
        """

    public static let savePasswordItem = """
        mutation ($id: String!, $itemKind: String!, $encryptedFields: String!, $itemKey: String!, $baseRevision: Int64) {
          SavePasswordItem(input: { id: $id, itemKind: $itemKind, encryptedFields: $encryptedFields, itemKey: $itemKey, baseRevision: $baseRevision }) { \(passwordItemFields) }
        }
        """
    public static let deletePasswordItem = """
        mutation ($id: String!) { DeletePasswordItem(id: $id) { \(passwordItemFields) } }
        """
    public static let restorePasswordItem = """
        mutation ($id: String!) { RestorePasswordItem(id: $id) { \(passwordItemFields) } }
        """
    public static let purgePasswordItem = """
        mutation ($id: String!) { PurgePasswordItem(id: $id) }
        """
    public static let setPasswordItemShares = """
        mutation ($itemId: String!, $grantees: [PasswordShareGranteeInput!]!, $keys: [PasswordShareKeyInput!]!) {
          SetPasswordItemShares(input: { itemId: $itemId, grantees: $grantees, keys: $keys }) { \(passwordItemFields) }
        }
        """
    public static let addPasswordShareKeys = """
        mutation ($keys: [PasswordItemShareKeyInput!]!) { AddPasswordShareKeys(keys: $keys) }
        """
    public static let setPasswordItemUsableByAgent = """
        mutation ($itemId: String!, $isUsableByAgent: Boolean!, $itemKeyByAgentKey: String) {
          SetPasswordItemUsableByAgent(input: { itemId: $itemId, isUsableByAgent: $isUsableByAgent, itemKeyByAgentKey: $itemKeyByAgentKey }) { \(passwordItemFields) }
        }
        """
    public static let rekeyPasswordItem = """
        mutation ($id: String!, $baseRevision: Int64!, $encryptedFields: String!, $itemKey: String!, $shareKeys: [PasswordItemShareKeyInput!]!, $itemKeyByAgentKey: String) {
          RekeyPasswordItem(input: { id: $id, baseRevision: $baseRevision, encryptedFields: $encryptedFields, itemKey: $itemKey, shareKeys: $shareKeys, itemKeyByAgentKey: $itemKeyByAgentKey }) { \(passwordItemFields) }
        }
        """

    /// The Passwords documents, for the server's schema check.
    public static let passwordsDocuments: [String: String] = [
        "passwordsSetup": passwordsSetup, "setUpPasswords": setUpPasswords, "unlockPasswords": unlockPasswords,
        "changePasswordsPassphrase": changePasswordsPassphrase, "replacePasswordsRecoveryKey": replacePasswordsRecoveryKey,
        "updatePasswordsSettings": updatePasswordsSettings, "erasePasswords": erasePasswords, "passwordItems": passwordItems,
        "passwordRecipients": passwordRecipients, "waitingPasswordShares": waitingPasswordShares, "passwordsAgentKey": passwordsAgentKey,
        "savePasswordItem": savePasswordItem, "deletePasswordItem": deletePasswordItem, "restorePasswordItem": restorePasswordItem,
        "purgePasswordItem": purgePasswordItem,
        "setPasswordItemShares": setPasswordItemShares, "addPasswordShareKeys": addPasswordShareKeys,
        "setPasswordItemUsableByAgent": setPasswordItemUsableByAgent, "rekeyPasswordItem": rekeyPasswordItem,
    ]
}

// MARK: The real API

/// PasswordsJSON turns the input shapes into the variables the calls send.
enum PasswordsJSON {
    static func variables<Value: Encodable>(_ value: Value) -> [String: JSONValue] {
        let encoder = JSONEncoder()
        guard let data = try? encoder.encode(value), let decoded = try? JSONDecoder().decode(JSONValue.self, from: data),
            case .object(let object) = decoded
        else { return [:] }
        return object
    }

    static func value<Value: Encodable>(_ value: Value) -> JSONValue {
        guard let data = try? JSONEncoder().encode(value), let decoded = try? JSONDecoder().decode(JSONValue.self, from: data) else {
            return .null
        }
        return decoded
    }
}

extension Connection: PasswordsAPI {
    public func readPasswordsSetup() async throws -> PasswordsSetupView {
        struct Answer: Decodable, Sendable { let ReadPasswordsSetup: PasswordsSetupView }
        return try await execute(Documents.passwordsSetup, as: Answer.self).ReadPasswordsSetup
    }

    public func setUpPasswords(_ input: PasswordsSetUpInput) async throws -> PasswordsSetupView {
        struct Answer: Decodable, Sendable { let SetUpPasswords: PasswordsSetupView }
        return try await execute(Documents.setUpPasswords, variables: PasswordsJSON.variables(input), as: Answer.self).SetUpPasswords
    }

    public func unlockPasswords(proof: String, isRecoveryKey: Bool) async throws -> PasswordsUnlockView {
        struct Answer: Decodable, Sendable { let UnlockPasswords: PasswordsUnlockView }
        let variables: [String: JSONValue] = ["proof": .string(proof), "isRecoveryKey": .bool(isRecoveryKey)]
        return try await execute(Documents.unlockPasswords, variables: variables, as: Answer.self).UnlockPasswords
    }

    public func changePasswordsPassphrase(_ input: PasswordsChangePassphraseInput) async throws -> PasswordsSetupView {
        struct Answer: Decodable, Sendable { let ChangePasswordsPassphrase: PasswordsSetupView }
        return try await execute(Documents.changePasswordsPassphrase, variables: PasswordsJSON.variables(input), as: Answer.self)
            .ChangePasswordsPassphrase
    }

    public func replacePasswordsRecoveryKey(_ input: PasswordsReplaceRecoveryKeyInput) async throws -> PasswordsSetupView {
        struct Answer: Decodable, Sendable { let ReplacePasswordsRecoveryKey: PasswordsSetupView }
        return try await execute(Documents.replacePasswordsRecoveryKey, variables: PasswordsJSON.variables(input), as: Answer.self)
            .ReplacePasswordsRecoveryKey
    }

    public func updatePasswordsSettings(lockAfterMinutes: Int) async throws -> PasswordsSetupView {
        struct Answer: Decodable, Sendable { let UpdatePasswordsSettings: PasswordsSetupView }
        return try await execute(
            Documents.updatePasswordsSettings, variables: ["lockAfterMinutes": .number(Double(lockAfterMinutes))], as: Answer.self
        ).UpdatePasswordsSettings
    }

    public func erasePasswords() async throws -> Bool {
        struct Answer: Decodable, Sendable { let ErasePasswords: Bool }
        return try await execute(Documents.erasePasswords, as: Answer.self).ErasePasswords
    }

    public func listPasswordItems(sinceRevision: Int64?) async throws -> PasswordItemListView {
        struct Answer: Decodable, Sendable { let ListPasswordItems: PasswordItemListView }
        var variables: [String: JSONValue] = [:]
        if let sinceRevision { variables["sinceRevision"] = .number(Double(sinceRevision)) }
        return try await execute(Documents.passwordItems, variables: variables, as: Answer.self).ListPasswordItems
    }

    public func listPasswordRecipients(granteeUserId: String?, granteeCircleId: String?) async throws -> [PasswordRecipientView] {
        struct Answer: Decodable, Sendable { let ListPasswordRecipients: [PasswordRecipientView] }
        var variables: [String: JSONValue] = [:]
        if let granteeUserId { variables["granteeUserId"] = .string(granteeUserId) }
        if let granteeCircleId { variables["granteeCircleId"] = .string(granteeCircleId) }
        return try await execute(Documents.passwordRecipients, variables: variables, as: Answer.self).ListPasswordRecipients
    }

    public func listWaitingPasswordShares() async throws -> [PasswordWaitingShareView] {
        struct Answer: Decodable, Sendable { let ListWaitingPasswordShares: [PasswordWaitingShareView] }
        return try await execute(Documents.waitingPasswordShares, as: Answer.self).ListWaitingPasswordShares
    }

    public func readPasswordsAgentKey() async throws -> String {
        struct Answer: Decodable, Sendable { let ReadPasswordsAgentKey: String }
        return try await execute(Documents.passwordsAgentKey, as: Answer.self).ReadPasswordsAgentKey
    }

    public func savePasswordItem(_ input: PasswordItemSaveInput) async throws -> PasswordItemView {
        struct Answer: Decodable, Sendable { let SavePasswordItem: PasswordItemView }
        return try await execute(Documents.savePasswordItem, variables: PasswordsJSON.variables(input), as: Answer.self).SavePasswordItem
    }

    public func deletePasswordItem(id: String) async throws -> PasswordItemView {
        struct Answer: Decodable, Sendable { let DeletePasswordItem: PasswordItemView }
        return try await execute(Documents.deletePasswordItem, variables: ["id": .string(id)], as: Answer.self).DeletePasswordItem
    }

    public func restorePasswordItem(id: String) async throws -> PasswordItemView {
        struct Answer: Decodable, Sendable { let RestorePasswordItem: PasswordItemView }
        return try await execute(Documents.restorePasswordItem, variables: ["id": .string(id)], as: Answer.self).RestorePasswordItem
    }

    public func purgePasswordItem(id: String) async throws -> Bool {
        struct Answer: Decodable, Sendable { let PurgePasswordItem: Bool }
        return try await execute(Documents.purgePasswordItem, variables: ["id": .string(id)], as: Answer.self).PurgePasswordItem
    }

    public func setPasswordItemShares(itemId: String, grantees: [PasswordShareGranteeInput], keys: [PasswordShareKeyInput]) async throws
        -> PasswordItemView
    {
        struct Answer: Decodable, Sendable { let SetPasswordItemShares: PasswordItemView }
        let variables: [String: JSONValue] = [
            "itemId": .string(itemId), "grantees": PasswordsJSON.value(grantees), "keys": PasswordsJSON.value(keys),
        ]
        return try await execute(Documents.setPasswordItemShares, variables: variables, as: Answer.self).SetPasswordItemShares
    }

    public func addPasswordShareKeys(_ keys: [PasswordItemShareKeyInput]) async throws -> Int {
        struct Answer: Decodable, Sendable { let AddPasswordShareKeys: Int }
        return try await execute(Documents.addPasswordShareKeys, variables: ["keys": PasswordsJSON.value(keys)], as: Answer.self)
            .AddPasswordShareKeys
    }

    public func setPasswordItemUsableByAgent(itemId: String, isUsableByAgent: Bool, itemKeyByAgentKey: String?) async throws
        -> PasswordItemView
    {
        struct Answer: Decodable, Sendable { let SetPasswordItemUsableByAgent: PasswordItemView }
        var variables: [String: JSONValue] = ["itemId": .string(itemId), "isUsableByAgent": .bool(isUsableByAgent)]
        if let itemKeyByAgentKey { variables["itemKeyByAgentKey"] = .string(itemKeyByAgentKey) }
        return try await execute(Documents.setPasswordItemUsableByAgent, variables: variables, as: Answer.self)
            .SetPasswordItemUsableByAgent
    }

    public func rekeyPasswordItem(_ input: PasswordItemRekeyInput) async throws -> PasswordItemView {
        struct Answer: Decodable, Sendable { let RekeyPasswordItem: PasswordItemView }
        return try await execute(Documents.rekeyPasswordItem, variables: PasswordsJSON.variables(input), as: Answer.self).RekeyPasswordItem
    }
}
