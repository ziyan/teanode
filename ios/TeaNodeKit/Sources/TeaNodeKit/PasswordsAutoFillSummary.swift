import Foundation

// What the AutoFill extension needs to list logins, so it never has to open
// them. The extension runs under a memory limit of about 120 MB, and a circle
// member can share items that are large (each up to half a megabyte sealed,
// many of them), so reading and opening every item to draw a list would end it.
// The app writes this summary, sealed with the account key, next to the cache
// each time it brings the copy up to date: for each login its id, name, user
// name, sites, the websites its passkeys are for, who shared it, and whether it
// has a password and a one-time code. Nothing in it is a secret value, but a
// user name and the sites someone logs in to are theirs, so it is sealed like
// the items are and only opens after Face ID. Every list in it is capped, so a
// login with thousands of sites, or thousands of logins, cannot make it large.

/// PasswordsAutoFillPasskey is a passkey in a login, without its private key.
public struct PasswordsAutoFillPasskey: Codable, Equatable, Hashable, Sendable {
    /// Base64url, as a website sends it and as the login's fields hold it.
    public var credentialId: String
    public var relyingPartyId: String
    public var userName: String

    public init(credentialId: String, relyingPartyId: String, userName: String) {
        self.credentialId = credentialId
        self.relyingPartyId = relyingPartyId
        self.userName = userName
    }
}

/// PasswordsAutoFillLogin is one login of the summary.
public struct PasswordsAutoFillLogin: Codable, Equatable, Hashable, Sendable, Identifiable {
    public var itemId: String
    /// The name to show: the login's name, else the host of its first site, else its user name.
    public var displayName: String
    public var userName: String
    /// At most `PasswordsAutoFillSummary.mostSitesPerLogin`, each cut to `mostSiteCharacters`.
    public var sites: [String]
    /// At most `PasswordsAutoFillSummary.mostPasskeysPerLogin`.
    public var passkeys: [PasswordsAutoFillPasskey]
    /// Who shared it, when `isSharedWithMe`.
    public var ownerName: String
    public var isSharedWithMe: Bool
    public var hasPassword: Bool
    public var hasOneTimeCode: Bool

    public var id: String { itemId }
}

/// PasswordsAutoFillSummary is the list of logins the extension shows.
public struct PasswordsAutoFillSummary: Codable, Equatable, Sendable {
    public static let currentFormatVersion = 1
    public static let mostSitesPerLogin = 50
    public static let mostPasskeysPerLogin = 20
    /// More logins than this are left out of the summary, the ones the person shared with
    /// others and their own before the ones shared with them.
    public static let mostLogins = 5000
    static let mostTextCharacters = 200
    static let mostSiteCharacters = 200
    static let mostCredentialIdCharacters = 2048
    static let associatedData = "autofill-summary"

    public var formatVersion: Int
    /// The signed-in person's id, so one person's summary is never read as another's.
    public var userId: String
    public var logins: [PasswordsAutoFillLogin]

    public init(userId: String, logins: [PasswordsAutoFillLogin] = []) {
        self.formatVersion = Self.currentFormatVersion
        self.userId = userId
        self.logins = logins
    }

    // MARK: Making

    private static func capped(_ text: String, to mostCharacters: Int) -> String {
        text.count <= mostCharacters ? text : String(text.prefix(mostCharacters))
    }

    /// The summary of one opened login.
    public static func login(itemId: String, ownerName: String, isSharedWithMe: Bool, fields: PasswordsLoginFields) -> PasswordsAutoFillLogin {
        let displayName: String
        if !fields.name.isEmpty {
            displayName = fields.name
        } else if let host = fields.sites.lazy.compactMap({ PasswordsSiteMatch.normalizedHost(of: $0) }).first {
            displayName = host
        } else {
            displayName = fields.userName
        }
        let sites = fields.sites.prefix(mostSitesPerLogin).map { capped($0, to: mostSiteCharacters) }
        var passkeys: [PasswordsAutoFillPasskey] = []
        for passkey in fields.passkeys where passkeys.count < mostPasskeysPerLogin {
            guard !passkey.credentialId.isEmpty, passkey.credentialId.count <= mostCredentialIdCharacters else { continue }
            passkeys.append(
                PasswordsAutoFillPasskey(
                    credentialId: passkey.credentialId, relyingPartyId: capped(passkey.relyingPartyId, to: mostTextCharacters),
                    userName: capped(passkey.userName, to: mostTextCharacters)))
        }
        return PasswordsAutoFillLogin(
            itemId: itemId, displayName: capped(displayName, to: mostTextCharacters), userName: capped(fields.userName, to: mostTextCharacters),
            sites: Array(sites), passkeys: passkeys, ownerName: capped(ownerName, to: mostTextCharacters), isSharedWithMe: isSharedWithMe,
            hasPassword: !fields.password.isEmpty, hasOneTimeCode: !fields.oneTimeCodeUri.isEmpty)
    }

    /// The summary of an opened item: its login, or nil for a card, a note and a deleted item, which
    /// the extension never lists.
    public static func login(of item: PasswordItem) -> PasswordsAutoFillLogin? {
        guard !item.isDeleted, case .login(let fields) = item.fields else { return nil }
        return login(itemId: item.id, ownerName: item.ownerName, isSharedWithMe: item.isSharedWithMe, fields: fields)
    }

    /// The summary of everything the copy holds: opens each login once, keeps its short form and
    /// drops the opened item at once. Notes, cards and deleted items are skipped without being
    /// opened, and an item that does not open (damaged, or by an owner whose key is not the one
    /// known) is left out.
    public static func make(userId: String, views: [PasswordItemView], opener: PasswordsOpener) -> PasswordsAutoFillSummary {
        var logins: [PasswordsAutoFillLogin] = []
        for view in views where view.itemKind == PasswordsItemKind.login.rawValue && view.deletedAt == nil {
            guard let item = try? opener.open(view), let login = login(of: item) else { continue }
            logins.append(login)
        }
        var summary = PasswordsAutoFillSummary(userId: userId, logins: logins)
        summary.sortAndCap()
        return summary
    }

    private mutating func sortAndCap() {
        logins.sort {
            let comparison = $0.displayName.localizedCaseInsensitiveCompare($1.displayName)
            return comparison == .orderedSame ? $0.itemId < $1.itemId : comparison == .orderedAscending
        }
        if logins.count > Self.mostLogins {
            let own = logins.filter { !$0.isSharedWithMe }
            let shared = logins.filter(\.isSharedWithMe)
            logins = Array((own + shared).prefix(Self.mostLogins))
            logins.sort {
                let comparison = $0.displayName.localizedCaseInsensitiveCompare($1.displayName)
                return comparison == .orderedSame ? $0.itemId < $1.itemId : comparison == .orderedAscending
            }
        }
    }

    /// Puts a login in, replacing the one with the same item id.
    public mutating func upsert(_ login: PasswordsAutoFillLogin) {
        if let index = logins.firstIndex(where: { $0.itemId == login.itemId }) {
            logins[index] = login
        } else {
            logins.append(login)
        }
        sortAndCap()
    }

    /// Takes a login out.
    public mutating func remove(itemId: String) {
        logins.removeAll { $0.itemId == itemId }
    }

    // MARK: Sealing

    /// The summary as the file holds it: JSON sealed with the account key.
    public func sealedData(accountKey: Data) throws -> Data {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        let sealed = try PasswordsCrypto.seal(try encoder.encode(self), key: accountKey, associatedData: Self.associatedData)
        return Data(sealed.jsonText.utf8)
    }

    /// Opens the file's data with the account key. A summary that is another person's, or of a
    /// format this build does not know, is unreadable.
    public static func open(_ data: Data, accountKey: Data, userId: String) throws -> PasswordsAutoFillSummary {
        guard let text = String(data: data, encoding: .utf8) else { throw PasswordsCacheError.unreadable }
        let sealed = try PasswordsSealedValue(jsonText: text)
        let plain = try PasswordsCrypto.open(sealed, key: accountKey, associatedData: associatedData)
        let summary: PasswordsAutoFillSummary
        do {
            summary = try JSONDecoder().decode(PasswordsAutoFillSummary.self, from: plain)
        } catch {
            throw PasswordsCacheError.unreadable
        }
        guard summary.formatVersion == currentFormatVersion, summary.userId == userId else { throw PasswordsCacheError.unreadable }
        return summary
    }
}
