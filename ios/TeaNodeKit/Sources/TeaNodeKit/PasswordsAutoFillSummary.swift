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
// login with thousands of sites, or thousands of logins, cannot make it large; and the whole
// summary has a byte budget (`mostBytes`), because the caps per list still let a circle member's
// shares add up to tens of megabytes, which the extension could not open under its limit. The
// person's own logins come first and shared ones are the first to be dropped (they stay in the
// app, just not in AutoFill's list); a summary over the budget is never written.

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
    /// The user id of whoever shared it, when `isSharedWithMe`; nil for the person's own and for a
    /// summary written before this was kept.
    public var ownerUserId: String?
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
    /// A login somebody shared lists fewer sites and passkeys than the person's own: its owner
    /// chose them, and a circle member can share many.
    public static let mostSitesPerSharedLogin = 5
    public static let mostPasskeysPerSharedLogin = 5
    /// The most logins one owner can put in the summary by sharing.
    public static let mostSharedLoginsPerOwner = 200
    /// The most bytes of JSON the summary holds, counted before it is sealed (1 MiB). Opening it
    /// takes a few times this in memory, which the extension's limit allows.
    public static let mostBytes = 1_048_576
    /// The most bytes a sealed summary file can have: the budget as sealed text, with the
    /// nonce and the wrapping around it. A bigger file is not read.
    public static let mostSealedBytes = (mostBytes + 64) / 3 * 4 + 1024
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
    public static func login(
        itemId: String, ownerName: String, isSharedWithMe: Bool, fields: PasswordsLoginFields, ownerUserId: String? = nil
    ) -> PasswordsAutoFillLogin {
        let displayName: String
        if !fields.name.isEmpty {
            displayName = fields.name
        } else if let host = fields.sites.lazy.compactMap({ PasswordsSiteMatch.normalizedHost(of: $0) }).first {
            displayName = host
        } else {
            displayName = fields.userName
        }
        let mostSites = isSharedWithMe ? mostSitesPerSharedLogin : mostSitesPerLogin
        let mostPasskeys = isSharedWithMe ? mostPasskeysPerSharedLogin : mostPasskeysPerLogin
        let sites = fields.sites.prefix(mostSites).map { capped($0, to: mostSiteCharacters) }
        var passkeys: [PasswordsAutoFillPasskey] = []
        for passkey in fields.passkeys where passkeys.count < mostPasskeys {
            guard !passkey.credentialId.isEmpty, passkey.credentialId.count <= mostCredentialIdCharacters else { continue }
            passkeys.append(
                PasswordsAutoFillPasskey(
                    credentialId: passkey.credentialId, relyingPartyId: capped(passkey.relyingPartyId, to: mostTextCharacters),
                    userName: capped(passkey.userName, to: mostTextCharacters)))
        }
        return PasswordsAutoFillLogin(
            itemId: itemId, displayName: capped(displayName, to: mostTextCharacters), userName: capped(fields.userName, to: mostTextCharacters),
            sites: Array(sites), passkeys: passkeys, ownerName: capped(ownerName, to: mostTextCharacters),
            ownerUserId: ownerUserId.map { capped($0, to: mostTextCharacters) }, isSharedWithMe: isSharedWithMe,
            hasPassword: !fields.password.isEmpty, hasOneTimeCode: !fields.oneTimeCodeUri.isEmpty)
    }

    /// The summary of an opened item: its login, or nil for a card, a note and a deleted item, which
    /// the extension never lists.
    public static func login(of item: PasswordItem) -> PasswordsAutoFillLogin? {
        guard !item.isDeleted, case .login(let fields) = item.fields else { return nil }
        return login(
            itemId: item.id, ownerName: item.ownerName, isSharedWithMe: item.isSharedWithMe, fields: fields,
            ownerUserId: item.isSharedWithMe ? item.ownerUserId : nil)
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

    private static func isBefore(_ first: PasswordsAutoFillLogin, _ second: PasswordsAutoFillLogin) -> Bool {
        let comparison = first.displayName.localizedCaseInsensitiveCompare(second.displayName)
        return comparison == .orderedSame ? first.itemId < second.itemId : comparison == .orderedAscending
    }

    private static func encodedSize(of value: some Encodable) -> Int {
        (try? JSONEncoder().encode(value).count) ?? Int.max
    }

    /// Sorts the logins by name and keeps the summary inside its limits: at most
    /// `mostSharedLoginsPerOwner` shared by one owner (the first by name), at most `mostLogins` in
    /// all (the person's own before the shared ones), and at most `mostBytes` of JSON, dropping the
    /// largest shared logins first and then, only if the person's own logins alone are over, the
    /// largest of those.
    mutating func sortAndCap() {
        logins.sort(by: Self.isBefore)
        var keptByOwner: [String: Int] = [:]
        logins = logins.filter { login in
            guard login.isSharedWithMe else { return true }
            let owner = login.ownerUserId ?? login.ownerName
            let kept = keptByOwner[owner, default: 0]
            guard kept < Self.mostSharedLoginsPerOwner else { return false }
            keptByOwner[owner] = kept + 1
            return true
        }
        if logins.count > Self.mostLogins {
            let own = logins.filter { !$0.isSharedWithMe }
            let shared = logins.filter(\.isSharedWithMe)
            logins = Array((own + shared).prefix(Self.mostLogins))
            logins.sort(by: Self.isBefore)
        }
        fitInBudget()
    }

    /// Drops logins until the JSON is within `mostBytes`: the largest shared ones first.
    private mutating func fitInBudget() {
        let envelopeBytes = Self.encodedSize(of: PasswordsAutoFillSummary(userId: userId)) - 2
        let sizes = logins.map { Self.encodedSize(of: $0) }
        var totalBytes = envelopeBytes + 2 + sizes.reduce(0, +) + max(0, logins.count - 1)
        guard totalBytes > Self.mostBytes else { return }
        // Shared logins before the person's own, the largest first.
        let dropOrder = logins.indices.sorted { first, second in
            let isFirstShared = logins[first].isSharedWithMe
            if isFirstShared != logins[second].isSharedWithMe { return isFirstShared }
            return sizes[first] != sizes[second] ? sizes[first] > sizes[second] : first < second
        }
        var droppedIndexes = Set<Int>()
        for index in dropOrder where totalBytes > Self.mostBytes {
            droppedIndexes.insert(index)
            totalBytes -= sizes[index] + 1
        }
        logins = logins.indices.filter { !droppedIndexes.contains($0) }.map { logins[$0] }
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
    /// A summary over `mostBytes` is not written: `PasswordsCacheError.tooLarge`.
    public func sealedData(accountKey: Data) throws -> Data {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        let plain = try encoder.encode(self)
        guard plain.count <= Self.mostBytes else { throw PasswordsCacheError.tooLarge }
        let sealed = try PasswordsCrypto.seal(plain, key: accountKey, associatedData: Self.associatedData)
        return Data(sealed.jsonText.utf8)
    }

    /// Opens the file's data with the account key. A summary that is another person's, or of a
    /// format this build does not know, is unreadable.
    public static func open(_ data: Data, accountKey: Data, userId: String) throws -> PasswordsAutoFillSummary {
        // A file this build could not have written (an earlier build wrote summaries of any size)
        // is not read, so it cannot take the extension's memory.
        guard data.count <= mostSealedBytes else { throw PasswordsCacheError.unreadable }
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
