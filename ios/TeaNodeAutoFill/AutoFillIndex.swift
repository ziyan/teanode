import Foundation
import TeaNodeKit

// What the list shows, worked out from the phone's copy of Passwords. The
// extension runs under a memory limit of about 120 MB, so it opens every login
// once, keeps only a short summary of it (name, user name, sites, which
// passkeys it has) and drops the opened item with its password and notes at
// once. The one login the person picks is opened again when it is needed.

/// AutoFillLocation is where the app keeps the phone's copy of Passwords for a
/// sign-in (PasswordsLocation names the file). The app writes it and the
/// extension reads it.
enum AutoFillLocation {
    /// Nil when the app group's container is not there, which is a build
    /// without the app group; the extension then has no copy to read.
    static func cacheFile(signInAccountKey: String) -> URL? {
        guard let group = Bundle.main.object(forInfoDictionaryKey: "TeaNodeAppGroup") as? String,
            let container = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: group)
        else { return nil }
        return PasswordsLocation.fileURL(groupContainer: container, signInAccountKey: signInAccountKey)
    }
}

/// AutoFillBase64URL reads and writes the unpadded base64url that websites
/// send credential ids and user handles in.
enum AutoFillBase64URL {
    static func decode(_ text: String) -> Data? {
        var standard = text.replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
        while standard.count % 4 != 0 { standard.append("=") }
        return Data(base64Encoded: standard)
    }

    static func encode(_ data: Data) -> String {
        data.base64EncodedString().replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }
}

/// AutoFillError is why the extension could not use an item.
enum AutoFillError: Error {
    case itemNotFound
    case nothingToFill
    case unreadablePasskey
    case relyingPartyDoesNotMatch
}

/// AutoFillPasskeySummary is a passkey in a login, without its private key.
struct AutoFillPasskeySummary: Equatable, Sendable {
    let credentialId: Data
    let relyingPartyId: String
    let userName: String
}

/// AutoFillLoginSummary is a login without its password, notes or keys.
struct AutoFillLoginSummary: Identifiable, Equatable, Sendable {
    let id: String
    let displayName: String
    let userName: String
    let sites: [String]
    let passkeys: [AutoFillPasskeySummary]
    let hasPassword: Bool
    let hasOneTimeCode: Bool
    let isSharedWithMe: Bool

    init(itemId: String, isSharedWithMe: Bool, login: PasswordsLoginFields) {
        id = itemId
        if !login.name.isEmpty {
            displayName = login.name
        } else if let host = login.sites.lazy.compactMap({ PasswordsSiteMatch.normalizedHost(of: $0) }).first {
            displayName = host
        } else {
            displayName = login.userName
        }
        userName = login.userName
        sites = login.sites
        passkeys = login.passkeys.compactMap { passkey in
            guard let credentialId = AutoFillBase64URL.decode(passkey.credentialId), !credentialId.isEmpty else { return nil }
            return AutoFillPasskeySummary(credentialId: credentialId, relyingPartyId: passkey.relyingPartyId, userName: passkey.userName)
        }
        hasPassword = !login.password.isEmpty
        hasOneTimeCode = !login.oneTimeCodeUri.isEmpty
        self.isSharedWithMe = isSharedWithMe
    }

    /// Whether the words typed in the search field are in the name, the user
    /// name or one of the sites.
    func matches(searchText: String) -> Bool {
        displayName.localizedCaseInsensitiveContains(searchText) || userName.localizedCaseInsensitiveContains(searchText)
            || sites.contains { $0.localizedCaseInsensitiveContains(searchText) }
    }
}

/// AutoFillRowKind is what choosing a row does.
enum AutoFillRowKind: Hashable {
    case password
    case oneTimeCode
    case passkey(credentialId: Data)
}

/// AutoFillRow is one line of the list.
struct AutoFillRow: Identifiable, Hashable {
    let id: String
    let itemId: String
    let rowTitle: String
    let rowSubtitle: String
    let rowKind: AutoFillRowKind

    var isPasskey: Bool {
        if case .passkey = rowKind { return true }
        return false
    }
}

/// AutoFillSection is a titled group of rows.
struct AutoFillSection: Identifiable {
    let id: String
    let title: String?
    let rows: [AutoFillRow]
}

enum AutoFillIndex {
    /// Opens each login in the copy once and keeps its summary. Notes, cards
    /// and deleted items are skipped without being opened. An item that does
    /// not open is left out, and so is a shared item whose owner key the
    /// opener does not know (the app records it when it first syncs the item)
    /// or that differs from the known one (the app asks the person to trust it).
    static func loginSummaries(views: [PasswordItemView], opener: PasswordsOpener) -> [AutoFillLoginSummary] {
        var summaries: [AutoFillLoginSummary] = []
        for view in views where view.itemKind == PasswordsItemKind.login.rawValue && view.deletedAt == nil {
            guard let item = try? opener.open(view), case .login(let login) = item.fields else { continue }
            summaries.append(AutoFillLoginSummary(itemId: view.id, isSharedWithMe: view.isSharedWithMe, login: login))
        }
        summaries.sort {
            let comparison = $0.displayName.localizedCaseInsensitiveCompare($1.displayName)
            return comparison == .orderedSame ? $0.id < $1.id : comparison == .orderedAscending
        }
        return summaries
    }

    /// Whether what the system names is a site. An app's bundle id is not one.
    static func isUsable(site: String) -> Bool {
        PasswordsSiteMatch.normalizedHost(of: site) != nil
    }

    // MARK: Sections

    /// The sections of the list: passkeys for the website that asked, the
    /// logins for the requested site (the exact origin when the system gave a
    /// full address, so the same host on another port or over plain http is not
    /// among them), then the logins for a related site (a parent or a child
    /// domain) under their own heading, then the rest. With words typed in the
    /// search field, the logins that contain them, across all sites.
    static func sections(logins: [AutoFillLoginSummary], listing: AutoFillListing, searchText: String) -> [AutoFillSection] {
        let words = searchText.trimmingCharacters(in: .whitespacesAndNewlines)
        let isSearching = !words.isEmpty
        let candidates = isSearching ? logins.filter { $0.matches(searchText: words) } : logins
        var sections: [AutoFillSection] = []

        let passkeySectionRows = passkeyRows(logins: candidates, listing: listing)
        if !passkeySectionRows.isEmpty {
            sections.append(AutoFillSection(id: "passkeys", title: String(localized: "Passkeys"), rows: passkeySectionRows))
        }

        let fillable = candidates.filter { listing.isOneTimeCode ? $0.hasOneTimeCode : $0.hasPassword }
        var suggested: [AutoFillLoginSummary] = []
        var related: [AutoFillLoginSummary] = []
        if !isSearching {
            for login in fillable {
                switch bestLevel(of: login, listing: listing) {
                case .exactHost: suggested.append(login)
                case .relatedHost: related.append(login)
                case .none: break
                }
            }
        }
        if !suggested.isEmpty {
            sections.append(
                AutoFillSection(id: "suggested", title: String(localized: "Suggested"), rows: suggested.map { row(login: $0, listing: listing) }))
        }
        if !related.isEmpty {
            sections.append(
                AutoFillSection(id: "related", title: String(localized: "Related sites"), rows: related.map { row(login: $0, listing: listing) }))
        }
        let shownIds = Set((suggested + related).map(\.id))
        let rest = fillable.filter { !shownIds.contains($0.id) }
        if !rest.isEmpty {
            // A list with nothing above it needs no heading.
            var restTitle: String?
            if !shownIds.isEmpty || !passkeySectionRows.isEmpty { restTitle = String(localized: "All logins") }
            sections.append(AutoFillSection(id: "all", title: restTitle, rows: rest.map { row(login: $0, listing: listing) }))
        }
        return sections
    }

    /// How well the login's sites fit the best of the sites the system named.
    private static func bestLevel(of login: AutoFillLoginSummary, listing: AutoFillListing) -> PasswordsSiteMatchLevel {
        listing.requestedSites.filter { isUsable(site: $0.address) }.map {
            PasswordsSiteMatch.level(sites: login.sites, requested: $0.address, isPageAddress: $0.isPageAddress)
        }.max() ?? .none
    }

    private static func row(login: AutoFillLoginSummary, listing: AutoFillListing) -> AutoFillRow {
        AutoFillRow(
            id: (listing.isOneTimeCode ? "code:" : "password:") + login.id, itemId: login.id, rowTitle: login.displayName,
            rowSubtitle: login.userName, rowKind: listing.isOneTimeCode ? .oneTimeCode : .password)
    }

    /// The passkeys the website that asked can use: for its relying party, and
    /// among the credential ids it allows when it names any.
    private static func passkeyRows(logins: [AutoFillLoginSummary], listing: AutoFillListing) -> [AutoFillRow] {
        guard let assertion = listing.passkeyAssertion else { return [] }
        var rows: [AutoFillRow] = []
        for login in logins {
            for passkey in login.passkeys where passkey.relyingPartyId == assertion.relyingPartyId {
                if !assertion.allowedCredentialIds.isEmpty && !assertion.allowedCredentialIds.contains(passkey.credentialId) { continue }
                rows.append(
                    AutoFillRow(
                        id: "passkey:" + login.id + ":" + passkey.credentialId.base64EncodedString(), itemId: login.id,
                        rowTitle: login.displayName, rowSubtitle: passkey.userName.isEmpty ? login.userName : passkey.userName,
                        rowKind: .passkey(credentialId: passkey.credentialId)))
            }
        }
        return rows
    }
}
