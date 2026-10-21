import Foundation

// Deciding which logins belong to a page. AutoFill gives a domain or a URL
// (ASCredentialServiceIdentifier) and the person's logins carry the sites they
// typed, with or without a scheme. The rule is the host and nothing looser:
// `example.com` and `www.example.com` are the same site, `login.example.com`
// is not, and neither is `example.com.evil.test`. A login for a parent domain
// is offered below the exact ones (`.relatedHost`), never as a first choice, so
// a page cannot be handed a password just by sitting under a domain the
// person signed in to.

/// PasswordsSiteMatchLevel is how well a login's site fits the requested page.
public enum PasswordsSiteMatchLevel: Int, Comparable, Sendable {
    /// Nothing in common.
    case none = 0
    /// One host is a subdomain of the other (`login.example.com` and `example.com`).
    case relatedHost = 1
    /// The same host, ignoring a leading `www.`, the scheme, the port and the path.
    case exactHost = 2

    public static func < (first: PasswordsSiteMatchLevel, second: PasswordsSiteMatchLevel) -> Bool {
        first.rawValue < second.rawValue
    }
}

/// PasswordsSiteMatch compares sites.
public enum PasswordsSiteMatch {
    /// The host of a site or a requested page, lower case, without a leading
    /// `www.`, a trailing dot, a port, a path or a user name. A site typed
    /// without a scheme (`example.com/sign-in`) is read as if it had one. Nil
    /// when there is no host, or the scheme is not http or https.
    public static func normalizedHost(of site: String) -> String? {
        var text = site.trimmingCharacters(in: .whitespacesAndNewlines)
        if let schemeEnd = text.range(of: "://") {
            let scheme = text[text.startIndex..<schemeEnd.lowerBound].lowercased()
            guard scheme == "http" || scheme == "https" else { return nil }
            text = String(text[schemeEnd.upperBound...])
        } else if text.hasPrefix("//") {
            text = String(text.dropFirst(2))
        }
        if let authorityEnd = text.firstIndex(where: { $0 == "/" || $0 == "?" || $0 == "#" }) {
            text = String(text[text.startIndex..<authorityEnd])
        }
        // Anything before the last "@" is a user name and password, never the host.
        if let atSign = text.lastIndex(of: "@") {
            text = String(text[text.index(after: atSign)...])
        }
        if text.hasPrefix("[") {
            // An IPv6 address: the brackets and everything between them.
            guard let closing = text.firstIndex(of: "]") else { return nil }
            return String(text[text.startIndex...closing]).lowercased()
        }
        if let colon = text.lastIndex(of: ":") {
            let port = text[text.index(after: colon)...]
            guard port.allSatisfy(\.isNumber) else { return nil }
            text = String(text[text.startIndex..<colon])
        }
        var host = text.lowercased()
        while host.hasSuffix(".") { host.removeLast() }
        if host.hasPrefix("www."), host.dropFirst(4).contains(".") { host.removeFirst(4) }
        guard !host.isEmpty, !host.contains(" ") else { return nil }
        return host
    }

    /// How well one site fits the requested page.
    public static func level(site: String, requested: String) -> PasswordsSiteMatchLevel {
        guard let siteHost = normalizedHost(of: site), let requestedHost = normalizedHost(of: requested) else { return .none }
        if siteHost == requestedHost { return .exactHost }
        // A dot-less name (localhost, an intranet host) has no parent domain to share.
        if siteHost.contains(".") && requestedHost.contains(".")
            && (requestedHost.hasSuffix("." + siteHost) || siteHost.hasSuffix("." + requestedHost))
        {
            return .relatedHost
        }
        return .none
    }

    /// The best level any of the sites reaches.
    public static func level(sites: [String], requested: String) -> PasswordsSiteMatchLevel {
        sites.map { level(site: $0, requested: requested) }.max() ?? .none
    }

    /// Whether a login is for the requested page: one of its sites has the same host.
    public static func isMatch(sites: [String], requested: String) -> Bool {
        level(sites: sites, requested: requested) == .exactHost
    }

    /// Whether a login is for the requested page, or for a domain above or
    /// below it, for the "more logins" list under the exact ones.
    public static func isRelatedOrBetter(sites: [String], requested: String) -> Bool {
        level(sites: sites, requested: requested) >= .relatedHost
    }

    /// Logins sorted for the requested page: the exact host first, then the
    /// related hosts, then (when `isKeepingOthers`) the rest. Within a level
    /// they keep the order they came in, so a caller sorts them by name first.
    public static func sorted<Login>(
        _ logins: [Login], requested: String, isKeepingOthers: Bool = false, sites: (Login) -> [String]
    ) -> [Login] {
        let ranked = logins.enumerated().map { (index: $0.offset, login: $0.element, level: level(sites: sites($0.element), requested: requested)) }
        return ranked.filter { isKeepingOthers || $0.level != .none }
            .sorted { first, second in
                if first.level != second.level { return first.level > second.level }
                return first.index < second.index
            }
            .map(\.login)
    }
}

// MARK: Identity store records

/// PasswordsPasswordIdentity is what the system's credential identity store
/// keeps about one login on one site, so QuickType can suggest it. No secret.
/// The app maps it to `ASPasswordCredentialIdentity` with a domain service
/// identifier, which Linux does not have.
public struct PasswordsPasswordIdentity: Equatable, Hashable, Sendable {
    /// The item's id, handed back to the AutoFill extension when the person picks it.
    public var recordId: String
    /// The site's host (a domain identifier).
    public var serviceDomain: String
    public var userName: String
}

/// PasswordsPasskeyIdentity is what the identity store keeps about one
/// passkey, for `ASPasskeyCredentialIdentity`.
public struct PasswordsPasskeyIdentity: Equatable, Hashable, Sendable {
    public var recordId: String
    public var relyingPartyId: String
    public var userName: String
    public var credentialId: Data
    public var userHandle: Data
}

/// PasswordsIdentityRecords is everything one login adds to the identity store.
public struct PasswordsIdentityRecords: Equatable, Sendable {
    public var passwordIdentities: [PasswordsPasswordIdentity]
    public var passkeyIdentities: [PasswordsPasskeyIdentity]

    public init(passwordIdentities: [PasswordsPasswordIdentity] = [], passkeyIdentities: [PasswordsPasskeyIdentity] = []) {
        self.passwordIdentities = passwordIdentities
        self.passkeyIdentities = passkeyIdentities
    }

    /// The records of one login: a password identity for each different host
    /// among its sites (only when it has a password), and an identity for each
    /// passkey that reads back. The record id is the item's id.
    public static func records(itemId: String, login: PasswordsLoginFields) -> PasswordsIdentityRecords {
        var passwordIdentities: [PasswordsPasswordIdentity] = []
        if !login.password.isEmpty {
            var seenHosts = Set<String>()
            for site in login.sites {
                guard let host = PasswordsSiteMatch.normalizedHost(of: site), seenHosts.insert(host).inserted else { continue }
                passwordIdentities.append(PasswordsPasswordIdentity(recordId: itemId, serviceDomain: host, userName: login.userName))
            }
        }
        var passkeyIdentities: [PasswordsPasskeyIdentity] = []
        for passkey in login.passkeys {
            guard !passkey.relyingPartyId.isEmpty, let credentialId = PasswordsBase64URL.decode(passkey.credentialId),
                !credentialId.isEmpty, let userHandle = PasswordsBase64URL.decode(passkey.userHandle)
            else { continue }
            passkeyIdentities.append(
                PasswordsPasskeyIdentity(
                    recordId: itemId, relyingPartyId: passkey.relyingPartyId,
                    userName: passkey.userName.isEmpty ? login.userName : passkey.userName, credentialId: credentialId,
                    userHandle: userHandle))
        }
        return PasswordsIdentityRecords(passwordIdentities: passwordIdentities, passkeyIdentities: passkeyIdentities)
    }

    /// The records of every login among opened items. Deleted items and
    /// notes and cards add nothing.
    public static func records(items: [PasswordItem]) -> PasswordsIdentityRecords {
        var combined = PasswordsIdentityRecords()
        for item in items where item.deletedAt == nil {
            guard case .login(let login) = item.fields else { continue }
            let own = records(itemId: item.id, login: login)
            combined.passwordIdentities += own.passwordIdentities
            combined.passkeyIdentities += own.passkeyIdentities
        }
        return combined
    }
}

/// PasswordsBase64URL reads the unpadded base64url a website sends credential
/// ids and user handles in.
enum PasswordsBase64URL {
    static func decode(_ text: String) -> Data? {
        var standard = text.replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
        while standard.count % 4 != 0 { standard.append("=") }
        return Data(base64Encoded: standard)
    }
}
