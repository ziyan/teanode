import Foundation
import TeaNodeKit

// What the list shows, worked out from the summary of the logins that the app
// writes next to the phone's copy of Passwords (PasswordsAutoFillSummary): name,
// user name, sites, which passkeys each login has, who shared it. The extension
// runs under a memory limit of about 120 MB, so it never reads or opens the
// items to draw a list; the one login the person picks is read from its own
// file and opened when it is needed. How well each login's sites fit the page
// asking is worked out once, not each time the list is drawn.

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
    /// The login a passkey would be saved into has an old item key somebody may still hold; the
    /// app gives it a new one first.
    case loginNeedsNewKey
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
    /// Who shared it with the person; empty for the person's own logins.
    let ownerName: String

    init(_ login: PasswordsAutoFillLogin) {
        id = login.itemId
        displayName = login.displayName
        userName = login.userName
        sites = login.sites
        passkeys = login.passkeys.compactMap { passkey in
            guard let credentialId = AutoFillBase64URL.decode(passkey.credentialId), !credentialId.isEmpty else { return nil }
            return AutoFillPasskeySummary(credentialId: credentialId, relyingPartyId: passkey.relyingPartyId, userName: passkey.userName)
        }
        hasPassword = login.hasPassword
        hasOneTimeCode = login.hasOneTimeCode
        isSharedWithMe = login.isSharedWithMe
        ownerName = login.ownerName
    }

    /// The name of whoever shared the login, for the line under it; nil for the person's own.
    var sharedByName: String? {
        isSharedWithMe && !ownerName.isEmpty ? ownerName : nil
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
    /// Who shared the login, shown under it; nil for the person's own.
    let sharedByName: String?

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
    /// The logins of the summary, in the order it has them (by name). The extension reads them
    /// from the summary file and never opens an item for the list.
    static func loginSummaries(of summary: PasswordsAutoFillSummary) -> [AutoFillLoginSummary] {
        summary.logins.map(AutoFillLoginSummary.init)
    }

    /// Whether what the system names is a site. An app's bundle id is not one.
    static func isUsable(site: String) -> Bool {
        PasswordsSiteMatch.normalizedHost(of: site) != nil
    }

    // MARK: Sections

    /// How well each login's sites fit the sites the system named, worked out once for a request
    /// (the sites do not change while the list is on screen). A login with a very long list of sites
    /// costs time to match, so this is not repeated for each letter typed in the search field.
    static func levels(logins: [AutoFillLoginSummary], listing: AutoFillListing) -> [String: PasswordsSiteMatchLevel] {
        var levels: [String: PasswordsSiteMatchLevel] = [:]
        let usableSites = listing.requestedSites.filter { isUsable(site: $0.address) }
        guard !usableSites.isEmpty else { return levels }
        for login in logins {
            levels[login.id] =
                usableSites.map {
                    PasswordsSiteMatch.level(sites: login.sites, requested: $0.address, isPageAddress: $0.isPageAddress)
                }.max() ?? PasswordsSiteMatchLevel.none
        }
        return levels
    }

    /// The sections of the list: passkeys for the website that asked, the
    /// logins for the requested site (the exact origin when the system gave a
    /// full address, so the same host on another port or over plain http is not
    /// among them), then the logins for a related site (a parent or a child
    /// domain) under their own heading, then the rest. With words typed in the
    /// search field, the logins that contain them, across all sites. `levels` is `levels(logins:listing:)`.
    static func sections(
        logins: [AutoFillLoginSummary], levels: [String: PasswordsSiteMatchLevel], listing: AutoFillListing, searchText: String
    ) -> [AutoFillSection] {
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
                switch levels[login.id] ?? PasswordsSiteMatchLevel.none {
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

    /// The site the system named for the page or app asking, as a host; nil when it named none that is a site.
    static func requestedSiteText(listing: AutoFillListing) -> String? {
        listing.requestedSites.lazy.compactMap { PasswordsSiteMatch.normalizedHost(of: $0.address) }.first
    }

    /// Whether the login is not saved for the exact site asking, so it is filled only after the person
    /// says yes: a login for a related site (a parent or a child domain) is asked about like any other
    /// site, the same as on the dashboard and in the extension. Never when the system named no site
    /// (an app is not a site), so there is nothing to compare. `level` is the login's entry of `levels(...)`.
    static func isSiteMismatch(level: PasswordsSiteMatchLevel, listing: AutoFillListing) -> Bool {
        guard requestedSiteText(listing: listing) != nil else { return false }
        return level != .exactHost
    }

    /// The hosts a login is saved for, for the question asked before filling it elsewhere.
    static func savedSitesText(login: AutoFillLoginSummary) -> String {
        login.sites.compactMap { PasswordsSiteMatch.normalizedHost(of: $0) }.joined(separator: ", ")
    }

    private static func row(login: AutoFillLoginSummary, listing: AutoFillListing) -> AutoFillRow {
        AutoFillRow(
            id: (listing.isOneTimeCode ? "code:" : "password:") + login.id, itemId: login.id, rowTitle: login.displayName,
            rowSubtitle: login.userName, rowKind: listing.isOneTimeCode ? .oneTimeCode : .password, sharedByName: login.sharedByName)
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
                        rowKind: .passkey(credentialId: passkey.credentialId), sharedByName: login.sharedByName))
            }
        }
        return rows
    }
}
