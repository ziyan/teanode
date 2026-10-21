import Foundation

// What is inside an item once it is decrypted. The names are the ones the plan
// fixes (docs/planning/passwords-execplan.md, "Item fields"); the Go, web and
// command line code spell them the same way, because the fields travel as one
// JSON text sealed with the item key. Another client may leave out an empty
// field, so decoding fills every missing one with an empty value.

/// PasswordsItemKind is what an item is. It is also part of the associated
/// data of the sealed fields, so a login cannot be opened as a note.
public enum PasswordsItemKind: String, Codable, CaseIterable, Sendable {
    case login
    case card
    case note
}

/// PasswordsGrantee is one person or one circle that may have an item. Exactly
/// one of the two is written, as a non-empty id: `{"userId": "..."}` or
/// `{"circleId": "..."}`.
public enum PasswordsGrantee: Codable, Equatable, Hashable, Sendable {
    case person(userId: String)
    case circle(circleId: String)

    private enum CodingKeys: String, CodingKey {
        case userId
        case circleId
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: PasswordsAnyCodingKey.self)
        guard container.allKeys.count == 1, let key = container.allKeys.first else {
            throw DecodingError.dataCorrupted(
                .init(codingPath: decoder.codingPath, debugDescription: "A grantee needs exactly one of userId and circleId"))
        }
        let identifier = try container.decode(String.self, forKey: key)
        guard !identifier.isEmpty else {
            throw DecodingError.dataCorrupted(.init(codingPath: decoder.codingPath, debugDescription: "A grantee needs a non-empty id"))
        }
        switch key.stringValue {
        case CodingKeys.userId.rawValue: self = .person(userId: identifier)
        case CodingKeys.circleId.rawValue: self = .circle(circleId: identifier)
        default:
            throw DecodingError.dataCorrupted(
                .init(codingPath: decoder.codingPath, debugDescription: "A grantee needs exactly one of userId and circleId"))
        }
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        switch self {
        case .person(let userId): try container.encode(userId, forKey: .userId)
        case .circle(let circleId): try container.encode(circleId, forKey: .circleId)
        }
    }
}

/// A coding key for any text, so a decoder can see which keys an object has and refuse the ones it does not know.
struct PasswordsAnyCodingKey: CodingKey {
    let stringValue: String
    var intValue: Int? { nil }

    init?(stringValue: String) { self.stringValue = stringValue }
    init?(intValue: Int) { return nil }
}

/// PasswordsItemAccess is who may have an item and whether the agent may use
/// it. It is a key inside the sealed fields of every kind of item, written only
/// by the owner's device, so the server cannot widen it. An item with no record
/// (`access == nil`) grants nobody and is not usable by the agent. A recipient's
/// copy may show the record but never acts on it.
///
/// `recordVersion` and `recordMac` are its signature: the version counts the
/// owner's changes, and the mac is the base64 of an HMAC-SHA256 under a key
/// made from the account key, which only the owner's devices hold (see
/// `PasswordsCrypto.macOfAccess`). A record may lack both, as one written before
/// they existed; `PasswordsCrypto.verifyAccess` does not trust such a record.
///
/// Decoding accepts exactly this shape: an object with `isUsableByAgent` (a
/// boolean) and `grantees` (a list of grantees), and optionally `recordVersion`
/// (a positive integer) and `recordMac` (canonical base64 of 32 bytes), and
/// nothing else; any other shape makes the item unreadable.
public struct PasswordsItemAccess: Codable, Equatable, Sendable {
    public var isUsableByAgent: Bool
    public var grantees: [PasswordsGrantee]
    /// `nil` for a record that has none, else a positive integer.
    public var recordVersion: Int?
    /// `nil` for a record that has none, else the canonical base64 of 32 bytes.
    public var recordMac: String?

    public init(isUsableByAgent: Bool = false, grantees: [PasswordsGrantee] = [], recordVersion: Int? = nil, recordMac: String? = nil) {
        self.isUsableByAgent = isUsableByAgent
        self.grantees = grantees
        self.recordVersion = recordVersion
        self.recordMac = recordMac
    }

    private enum CodingKeys: String, CodingKey {
        case isUsableByAgent
        case grantees
        case recordVersion
        case recordMac
    }

    public init(from decoder: Decoder) throws {
        let keyContainer = try decoder.container(keyedBy: PasswordsAnyCodingKey.self)
        let givenKeys = Set(keyContainer.allKeys.map(\.stringValue))
        let requiredKeys = Set([CodingKeys.isUsableByAgent.rawValue, CodingKeys.grantees.rawValue])
        let allowedKeys = requiredKeys.union([CodingKeys.recordVersion.rawValue, CodingKeys.recordMac.rawValue])
        guard givenKeys.isSuperset(of: requiredKeys), givenKeys.isSubset(of: allowedKeys) else {
            throw DecodingError.dataCorrupted(
                .init(
                    codingPath: decoder.codingPath,
                    debugDescription: "An access record is isUsableByAgent and grantees, and may have recordVersion and recordMac"))
        }
        let container = try decoder.container(keyedBy: CodingKeys.self)
        isUsableByAgent = try container.decode(Bool.self, forKey: .isUsableByAgent)
        grantees = try container.decode([PasswordsGrantee].self, forKey: .grantees)
        if givenKeys.contains(CodingKeys.recordVersion.rawValue) {
            let version = try container.decode(Int.self, forKey: .recordVersion)
            guard version >= 1, version <= PasswordsCrypto.maximumRecordVersion else {
                throw DecodingError.dataCorruptedError(
                    forKey: .recordVersion, in: container, debugDescription: "A record version is a positive integer")
            }
            recordVersion = version
        } else {
            recordVersion = nil
        }
        if givenKeys.contains(CodingKeys.recordMac.rawValue) {
            let text = try container.decode(String.self, forKey: .recordMac)
            guard let bytes = Data(base64Encoded: text), bytes.count == PasswordsCrypto.keyByteCount, bytes.base64EncodedString() == text else {
                throw DecodingError.dataCorruptedError(
                    forKey: .recordMac, in: container, debugDescription: "A record mac is canonical base64 of 32 bytes")
            }
            recordMac = text
        } else {
            recordMac = nil
        }
    }

    /// Whether the record grants an item to a recipient: a person grantee by
    /// user id, or a circle grantee when the server lists the recipient as a
    /// member of that circle (`circleIdsOfRecipient`). The recipient still needs
    /// a key confirmed on this device before anything is sealed for them. No
    /// record (`nil`) reaches nobody.
    public static func isReachedByAccess(_ access: PasswordsItemAccess?, recipientUserId: String, circleIdsOfRecipient: [String]) -> Bool {
        guard let access, !recipientUserId.isEmpty else { return false }
        return access.grantees.contains { grantee in
            switch grantee {
            case .person(let userId): return userId == recipientUserId
            case .circle(let circleId): return circleIdsOfRecipient.contains(circleId)
            }
        }
    }

    /// A record with these grantees and the same agent use, without a version or
    /// signature: sign it again (`PasswordsCrypto.withSignedAccess`) before it is
    /// sealed. No record counts as one that grants nobody and is not usable by
    /// the agent.
    public static func accessWithGrantees(_ access: PasswordsItemAccess?, grantees: [PasswordsGrantee]) -> PasswordsItemAccess {
        PasswordsItemAccess(isUsableByAgent: access?.isUsableByAgent ?? false, grantees: grantees)
    }

    /// A record with agent use turned on or off and the same grantees, without a
    /// version or signature: sign it again (`PasswordsCrypto.withSignedAccess`)
    /// before it is sealed.
    public static func accessWithAgentUse(_ access: PasswordsItemAccess?, isUsableByAgent: Bool) -> PasswordsItemAccess {
        PasswordsItemAccess(isUsableByAgent: isUsableByAgent, grantees: access?.grantees ?? [])
    }
}

/// PasswordsPasskeyFields is a passkey kept inside a login. `privateKey` is
/// PKCS#8 DER (ES256) in base64; `credentialId` and `userHandle` are base64url,
/// as a website sends them; `createdAt` is an ISO 8601 time.
public struct PasswordsPasskeyFields: Codable, Equatable, Sendable {
    public var credentialId: String
    public var relyingPartyId: String
    public var userHandle: String
    public var userName: String
    public var userDisplayName: String
    public var privateKey: String
    public var createdAt: String

    public init(
        credentialId: String = "", relyingPartyId: String = "", userHandle: String = "", userName: String = "",
        userDisplayName: String = "", privateKey: String = "", createdAt: String = ""
    ) {
        self.credentialId = credentialId
        self.relyingPartyId = relyingPartyId
        self.userHandle = userHandle
        self.userName = userName
        self.userDisplayName = userDisplayName
        self.privateKey = privateKey
        self.createdAt = createdAt
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        credentialId = try container.decodeIfPresent(String.self, forKey: .credentialId) ?? ""
        relyingPartyId = try container.decodeIfPresent(String.self, forKey: .relyingPartyId) ?? ""
        userHandle = try container.decodeIfPresent(String.self, forKey: .userHandle) ?? ""
        userName = try container.decodeIfPresent(String.self, forKey: .userName) ?? ""
        userDisplayName = try container.decodeIfPresent(String.self, forKey: .userDisplayName) ?? ""
        privateKey = try container.decodeIfPresent(String.self, forKey: .privateKey) ?? ""
        createdAt = try container.decodeIfPresent(String.self, forKey: .createdAt) ?? ""
    }
}

/// PasswordsLoginFields is a login: a user name and password for some sites,
/// with an optional one-time code secret and passkeys.
public struct PasswordsLoginFields: Codable, Equatable, Sendable {
    public var name: String
    public var userName: String
    public var password: String
    /// Web addresses the login is for, like `https://example.com/sign-in`.
    public var sites: [String]
    /// An `otpauth://totp/...` link, or empty.
    public var oneTimeCodeUri: String
    public var notes: String
    public var passkeys: [PasswordsPasskeyFields]
    public var folder: String
    /// Who may have this login and whether the agent may use it; `nil` is nobody and not the agent.
    public var access: PasswordsItemAccess?

    public init(
        name: String = "", userName: String = "", password: String = "", sites: [String] = [], oneTimeCodeUri: String = "",
        notes: String = "", passkeys: [PasswordsPasskeyFields] = [], folder: String = "", access: PasswordsItemAccess? = nil
    ) {
        self.name = name
        self.userName = userName
        self.password = password
        self.sites = sites
        self.oneTimeCodeUri = oneTimeCodeUri
        self.notes = notes
        self.passkeys = passkeys
        self.folder = folder
        self.access = access
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        name = try container.decodeIfPresent(String.self, forKey: .name) ?? ""
        userName = try container.decodeIfPresent(String.self, forKey: .userName) ?? ""
        password = try container.decodeIfPresent(String.self, forKey: .password) ?? ""
        sites = try container.decodeIfPresent([String].self, forKey: .sites) ?? []
        oneTimeCodeUri = try container.decodeIfPresent(String.self, forKey: .oneTimeCodeUri) ?? ""
        notes = try container.decodeIfPresent(String.self, forKey: .notes) ?? ""
        passkeys = try container.decodeIfPresent([PasswordsPasskeyFields].self, forKey: .passkeys) ?? []
        folder = try container.decodeIfPresent(String.self, forKey: .folder) ?? ""
        access = try container.decodeIfPresent(PasswordsItemAccess.self, forKey: .access)
    }
}

/// PasswordsCardFields is a payment card. Cards are kept, shown and copied;
/// nothing fills them.
public struct PasswordsCardFields: Codable, Equatable, Sendable {
    public var name: String
    public var cardholderName: String
    public var number: String
    public var expiryMonth: String
    public var expiryYear: String
    public var securityCode: String
    public var brand: String
    public var notes: String
    public var folder: String
    /// Who may have this card; `nil` is nobody. The agent never uses a card, whatever the record says.
    public var access: PasswordsItemAccess?

    public init(
        name: String = "", cardholderName: String = "", number: String = "", expiryMonth: String = "", expiryYear: String = "",
        securityCode: String = "", brand: String = "", notes: String = "", folder: String = "", access: PasswordsItemAccess? = nil
    ) {
        self.name = name
        self.cardholderName = cardholderName
        self.number = number
        self.expiryMonth = expiryMonth
        self.expiryYear = expiryYear
        self.securityCode = securityCode
        self.brand = brand
        self.notes = notes
        self.folder = folder
        self.access = access
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        name = try container.decodeIfPresent(String.self, forKey: .name) ?? ""
        cardholderName = try container.decodeIfPresent(String.self, forKey: .cardholderName) ?? ""
        number = try container.decodeIfPresent(String.self, forKey: .number) ?? ""
        expiryMonth = try container.decodeIfPresent(String.self, forKey: .expiryMonth) ?? ""
        expiryYear = try container.decodeIfPresent(String.self, forKey: .expiryYear) ?? ""
        securityCode = try container.decodeIfPresent(String.self, forKey: .securityCode) ?? ""
        brand = try container.decodeIfPresent(String.self, forKey: .brand) ?? ""
        notes = try container.decodeIfPresent(String.self, forKey: .notes) ?? ""
        folder = try container.decodeIfPresent(String.self, forKey: .folder) ?? ""
        access = try container.decodeIfPresent(PasswordsItemAccess.self, forKey: .access)
    }
}

/// PasswordsNoteFields is a secure note.
public struct PasswordsNoteFields: Codable, Equatable, Sendable {
    public var name: String
    public var text: String
    public var folder: String
    /// Who may have this note and whether the agent may use it; `nil` is nobody and not the agent.
    public var access: PasswordsItemAccess?

    public init(name: String = "", text: String = "", folder: String = "", access: PasswordsItemAccess? = nil) {
        self.name = name
        self.text = text
        self.folder = folder
        self.access = access
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        name = try container.decodeIfPresent(String.self, forKey: .name) ?? ""
        text = try container.decodeIfPresent(String.self, forKey: .text) ?? ""
        folder = try container.decodeIfPresent(String.self, forKey: .folder) ?? ""
        access = try container.decodeIfPresent(PasswordsItemAccess.self, forKey: .access)
    }
}

/// PasswordsItemFields is the fields of one item, whichever kind it is.
public enum PasswordsItemFields: Equatable, Sendable {
    case login(PasswordsLoginFields)
    case card(PasswordsCardFields)
    case note(PasswordsNoteFields)

    public var itemKind: PasswordsItemKind {
        switch self {
        case .login: return .login
        case .card: return .card
        case .note: return .note
        }
    }

    /// The name every kind has, shown in lists.
    public var name: String {
        switch self {
        case .login(let fields): return fields.name
        case .card(let fields): return fields.name
        case .note(let fields): return fields.name
        }
    }

    /// The access record of any kind; `nil` is nobody and not the agent.
    public var access: PasswordsItemAccess? {
        switch self {
        case .login(let fields): return fields.access
        case .card(let fields): return fields.access
        case .note(let fields): return fields.access
        }
    }

    /// The same fields with this access record (everything else unchanged), for
    /// whichever kind they are.
    public func withAccess(_ access: PasswordsItemAccess?) -> PasswordsItemFields {
        switch self {
        case .login(var fields):
            fields.access = access
            return .login(fields)
        case .card(var fields):
            fields.access = access
            return .card(fields)
        case .note(var fields):
            fields.access = access
            return .note(fields)
        }
    }

    /// The fields as the JSON text that is sealed.
    public func jsonData() throws -> Data {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
        switch self {
        case .login(let fields): return try encoder.encode(fields)
        case .card(let fields): return try encoder.encode(fields)
        case .note(let fields): return try encoder.encode(fields)
        }
    }

    /// Reads fields of the given kind from the JSON text that was sealed.
    public init(itemKind: PasswordsItemKind, jsonData: Data) throws {
        let decoder = JSONDecoder()
        switch itemKind {
        case .login: self = .login(try decoder.decode(PasswordsLoginFields.self, from: jsonData))
        case .card: self = .card(try decoder.decode(PasswordsCardFields.self, from: jsonData))
        case .note: self = .note(try decoder.decode(PasswordsNoteFields.self, from: jsonData))
        }
    }
}
