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

    public init(
        name: String = "", userName: String = "", password: String = "", sites: [String] = [], oneTimeCodeUri: String = "",
        notes: String = "", passkeys: [PasswordsPasskeyFields] = [], folder: String = ""
    ) {
        self.name = name
        self.userName = userName
        self.password = password
        self.sites = sites
        self.oneTimeCodeUri = oneTimeCodeUri
        self.notes = notes
        self.passkeys = passkeys
        self.folder = folder
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

    public init(
        name: String = "", cardholderName: String = "", number: String = "", expiryMonth: String = "", expiryYear: String = "",
        securityCode: String = "", brand: String = "", notes: String = "", folder: String = ""
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
    }
}

/// PasswordsNoteFields is a secure note.
public struct PasswordsNoteFields: Codable, Equatable, Sendable {
    public var name: String
    public var text: String
    public var folder: String

    public init(name: String = "", text: String = "", folder: String = "") {
        self.name = name
        self.text = text
        self.folder = folder
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        name = try container.decodeIfPresent(String.self, forKey: .name) ?? ""
        text = try container.decodeIfPresent(String.self, forKey: .text) ?? ""
        folder = try container.decodeIfPresent(String.self, forKey: .folder) ?? ""
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
