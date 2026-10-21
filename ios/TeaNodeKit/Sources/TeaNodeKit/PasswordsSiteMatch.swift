import Foundation

// Deciding which logins belong to a page. AutoFill gives a domain or a URL
// (ASCredentialServiceIdentifier) and the person's logins carry the sites they
// typed, with or without a scheme. The rule is the host and nothing looser:
// `example.com` and `www.example.com` are the same site, `login.example.com`
// is not, and neither is `example.com.evil.test`. A login for a parent domain
// is offered below the exact ones (`.relatedHost`), never as a first choice, so
// a page cannot be handed a password just by sitting under a domain the
// person signed in to. Where the page's full address is known (a URL, not a
// bare domain), the port and the scheme count as well (`isOrigin`), so a
// password saved for a secure site is never offered to its plain-text twin or
// to another service on the same host.

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
    /// when there is no host, the scheme is not http or https, or the address
    /// holds a backslash or a control character (a browser reads those
    /// differently from a person, so no host is trusted from them). It follows
    /// `NormalizedHost` in internal/passwords/sitematch.go, and both pass the
    /// cases in internal/passwords/testdata/sitematch.json.
    public static func normalizedHost(of site: String) -> String? {
        var text = site.trimmingCharacters(in: .whitespacesAndNewlines)
        if text.unicodeScalars.contains(where: isBackslashOrControl) { return nil }
        if let split = splitScheme(text) {
            let scheme = split.scheme.lowercased()
            guard scheme == "http" || scheme == "https" else { return nil }
            text = split.rest
        } else if hasScalarPrefix(text, "//") {
            text = String(String.UnicodeScalarView(text.unicodeScalars.dropFirst(2)))
        } else if isSchemeWithoutSlashes(text) {
            // "javascript:", "mailto:", "about:blank": not a host and a port.
            return nil
        }
        // Every search below is over unicode scalars, not Characters: a
        // combining mark after a "/" or an "@" must not hide it, as it does not
        // for the Go rule that reads bytes.
        let scalars = Array(authority(of: text).unicodeScalars)
        if scalars.first == "[" {
            // An IPv6 address: the brackets and everything between them.
            guard let closing = scalars.firstIndex(of: "]") else { return nil }
            return string(of: scalars[0...closing]).lowercased()
        }
        var hostScalars = scalars[...]
        if let colon = scalars.lastIndex(of: ":") {
            guard scalars[(colon + 1)...].allSatisfy({ $0.value >= 48 && $0.value <= 57 }) else { return nil }
            hostScalars = scalars[..<colon]
        }
        var host = Array(string(of: hostScalars).lowercased().unicodeScalars)
        while host.last == "." { host.removeLast() }
        if host.starts(with: "www.".unicodeScalars), host.dropFirst(4).contains(".") { host.removeFirst(4) }
        guard !host.isEmpty, !host.contains(where: { " \t\r\n".unicodeScalars.contains($0) }) else { return nil }
        return string(of: host[...])
    }

    /// The part of an address after its scheme that holds a host: up to the
    /// first "/", "?" or "#", and after the last "@" (a user name and password).
    private static func authority(of text: String) -> String {
        var scalars = text.unicodeScalars[...]
        if let authorityEnd = scalars.firstIndex(where: { $0 == "/" || $0 == "?" || $0 == "#" }) {
            scalars = scalars[..<authorityEnd]
        }
        if let atSign = scalars.lastIndex(of: "@") {
            scalars = scalars[scalars.index(after: atSign)...]
        }
        return String(String.UnicodeScalarView(scalars))
    }

    private static func string(of scalars: ArraySlice<Unicode.Scalar>) -> String {
        var view = String.UnicodeScalarView()
        view.append(contentsOf: scalars)
        return String(view)
    }

    private static func hasScalarSuffix(_ text: String, _ suffix: String) -> Bool {
        text.unicodeScalars.reversed().starts(with: suffix.unicodeScalars.reversed())
    }

    private static func hasScalarPrefix(_ text: String, _ prefix: String) -> Bool {
        text.unicodeScalars.starts(with: prefix.unicodeScalars)
    }

    /// Whether two pieces of text are the same unicode scalars. `==` on a
    /// String also treats canonically equivalent text as equal, which the Go
    /// rule, comparing bytes, does not.
    private static func isSameText(_ first: String, _ second: String) -> Bool {
        first.unicodeScalars.elementsEqual(second.unicodeScalars)
    }

    /// Splits `scheme://rest` when the text starts with a scheme followed by two
    /// slashes. A `://` further on, in a path or a query, is not a scheme.
    static func splitScheme(_ text: String) -> (scheme: String, rest: String)? {
        let scalars = Array(text.unicodeScalars)
        let separator = Array("://".unicodeScalars)
        guard scalars.count >= separator.count else { return nil }
        for schemeEnd in 0...(scalars.count - separator.count) where scalars[schemeEnd...].starts(with: separator) {
            // Only the first "://" counts, and a scheme cannot be empty.
            guard schemeEnd > 0 else { return nil }
            let scheme = string(of: scalars[..<schemeEnd])
            guard isSchemeToken(scheme) else { return nil }
            return (scheme, string(of: scalars[(schemeEnd + separator.count)...]))
        }
        return nil
    }

    /// A letter followed by letters, digits, `+`, `-` and `.`.
    private static func isSchemeToken(_ text: String) -> Bool {
        guard !text.isEmpty else { return false }
        for (index, byte) in text.utf8.enumerated() {
            let isLetter = (byte >= 65 && byte <= 90) || (byte >= 97 && byte <= 122)
            let isOther = (byte >= 48 && byte <= 57) || byte == 43 || byte == 45 || byte == 46
            if !isLetter && (index == 0 || !isOther) { return false }
        }
        return true
    }

    /// A scheme and a colon that is not a host and a port: `mailto:ada@example.com` is,
    /// `localhost:8080/sign-in` is not.
    private static func isSchemeWithoutSlashes(_ text: String) -> Bool {
        let scalars = text.unicodeScalars
        guard let colon = scalars.firstIndex(of: ":"), colon > scalars.startIndex,
            isSchemeToken(String(scalars[scalars.startIndex..<colon]))
        else {
            return false
        }
        let rest = Array(String(scalars[scalars.index(after: colon)...]).utf8)
        var digitsEnd = 0
        while digitsEnd < rest.count, rest[digitsEnd] >= 48, rest[digitsEnd] <= 57 { digitsEnd += 1 }
        if digitsEnd > 0, digitsEnd == rest.count || [UInt8(ascii: "/"), UInt8(ascii: "?"), UInt8(ascii: "#")].contains(rest[digitsEnd]) {
            return false
        }
        return true
    }

    /// A backslash or an ASCII control character, a delete included.
    private static func isBackslashOrControl(_ scalar: Unicode.Scalar) -> Bool {
        scalar == "\\" || scalar.value < 0x20 || scalar.value == 0x7f
    }

    /// How well one site fits the requested page.
    public static func level(site: String, requested: String) -> PasswordsSiteMatchLevel {
        guard let siteHost = normalizedHost(of: site), let requestedHost = normalizedHost(of: requested) else { return .none }
        if isSameText(siteHost, requestedHost) { return .exactHost }
        // A dot-less name (localhost, an intranet host) has no parent domain to share.
        if siteHost.unicodeScalars.contains(".") && requestedHost.unicodeScalars.contains(".")
            && (hasScalarSuffix(requestedHost, "." + siteHost) || hasScalarSuffix(siteHost, "." + requestedHost))
        {
            return .relatedHost
        }
        return .none
    }

    /// The best level any of the sites reaches.
    public static func level(sites: [String], requested: String) -> PasswordsSiteMatchLevel {
        sites.map { level(site: $0, requested: requested) }.max() ?? .none
    }

    /// How well one site fits a requested page. With `isPageAddress` (the
    /// request is a full address, not a bare domain) the same host is an
    /// `exactHost` only when the origin is the same too (`isOrigin`); the same
    /// host on another port, or a secure site asked for by a plain-text page, is
    /// `none`, so it is listed with the rest rather than suggested. A related
    /// host is the same either way.
    public static func level(site: String, requested: String, isPageAddress: Bool) -> PasswordsSiteMatchLevel {
        let hostLevel = level(site: site, requested: requested)
        guard isPageAddress, hostLevel == .exactHost else { return hostLevel }
        return isOrigin(sites: [site], requested: requested) ? .exactHost : .none
    }

    /// The best level any of the sites reaches for a requested page; see `level(site:requested:isPageAddress:)`.
    public static func level(sites: [String], requested: String, isPageAddress: Bool) -> PasswordsSiteMatchLevel {
        sites.map { level(site: $0, requested: requested, isPageAddress: isPageAddress) }.max() ?? .none
    }

    // MARK: Origin

    /// Whether the page is at the origin of one of the sites, which is stricter
    /// than `isMatch` and is what a full address needs before a password is
    /// offered for it: the host is the same, and so is the port. A site that
    /// names a port (`example.com:8443`) matches only a page on that port; one
    /// that names none matches only a page on the default port of the site's
    /// scheme (443 for a site saved without a scheme or as https). A page on
    /// http matches only a site saved as http. It follows `IsOriginOfSites` in
    /// internal/passwords/sitematch.go, so the iPhone, the dashboard and the
    /// agent agree.
    public static func isOrigin(sites: [String], requested: String) -> Bool {
        guard let requestedHost = normalizedHost(of: requested) else { return false }
        let requestedScheme = scheme(of: requested)
        let requestedPort = effectivePort(of: requested)
        for site in sites {
            guard let siteHost = normalizedHost(of: site), isSameText(siteHost, requestedHost) else { continue }
            let siteScheme = scheme(of: site)
            if requestedScheme == "http" && siteScheme != "http" { continue }
            let sitePort = explicitPort(of: site) ?? defaultPort(ofScheme: siteScheme)
            if sitePort == requestedPort { return true }
        }
        return false
    }

    /// The port a page is on: the one its address names, or the default of its
    /// scheme. Zero when the address names a port that is not 1 to 65535.
    static func effectivePort(of page: String) -> Int {
        explicitPort(of: page) ?? defaultPort(ofScheme: scheme(of: page))
    }

    /// "http" when the address says so and "https" otherwise, which is what a
    /// site typed without a scheme means.
    static func scheme(of site: String) -> String {
        if let split = splitScheme(site.trimmingCharacters(in: .whitespacesAndNewlines)), split.scheme.lowercased() == "http" {
            return "http"
        }
        return "https"
    }

    static func defaultPort(ofScheme scheme: String) -> Int {
        scheme == "http" ? 80 : 443
    }

    /// The port an address names; nil when it names none. A port that is not a
    /// number from 1 to 65535 is named, and is zero, which matches no real port.
    static func explicitPort(of site: String) -> Int? {
        var text = site.trimmingCharacters(in: .whitespacesAndNewlines)
        if let split = splitScheme(text) {
            text = split.rest
        } else if hasScalarPrefix(text, "//") {
            text = String(String.UnicodeScalarView(text.unicodeScalars.dropFirst(2)))
        }
        var scalars = Array(authority(of: text).unicodeScalars)[...]
        if scalars.first == "[" {
            guard let closing = scalars.firstIndex(of: "]"), closing + 1 < scalars.endIndex, scalars[closing + 1] == ":" else { return nil }
            scalars = scalars[(closing + 1)...]
        }
        guard let colon = scalars.lastIndex(of: ":") else { return nil }
        let digits = scalars[(colon + 1)...]
        if digits.isEmpty { return nil }
        var port = 0
        for scalar in digits {
            guard scalar.value >= 48, scalar.value <= 57, port <= 65535 else { return 0 }
            port = port * 10 + Int(scalar.value - 48)
        }
        return port >= 1 && port <= 65535 ? port : 0
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

    /// Logins sorted for the requested page: the exact host first (the exact
    /// origin when `isPageAddress`), then the related hosts, then (when
    /// `isKeepingOthers`) the rest. Within a level
    /// they keep the order they came in, so a caller sorts them by name first.
    public static func sorted<Login>(
        _ logins: [Login], requested: String, isPageAddress: Bool = false, isKeepingOthers: Bool = false, sites: (Login) -> [String]
    ) -> [Login] {
        let ranked = logins.enumerated().map {
            (index: $0.offset, login: $0.element, level: level(sites: sites($0.element), requested: requested, isPageAddress: isPageAddress))
        }
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
    public var itemId: String
    /// The site's host (a domain identifier).
    public var serviceDomain: String
    public var userName: String
}

/// PasswordsPasskeyIdentity is what the identity store keeps about one
/// passkey, for `ASPasskeyCredentialIdentity`.
public struct PasswordsPasskeyIdentity: Equatable, Hashable, Sendable {
    public var itemId: String
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
                passwordIdentities.append(PasswordsPasswordIdentity(itemId: itemId, serviceDomain: host, userName: login.userName))
            }
        }
        var passkeyIdentities: [PasswordsPasskeyIdentity] = []
        for passkey in login.passkeys {
            guard !passkey.relyingPartyId.isEmpty, let credentialId = PasswordsBase64URL.decode(passkey.credentialId),
                !credentialId.isEmpty, let userHandle = PasswordsBase64URL.decode(passkey.userHandle)
            else { continue }
            passkeyIdentities.append(
                PasswordsPasskeyIdentity(
                    itemId: itemId, relyingPartyId: passkey.relyingPartyId,
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
