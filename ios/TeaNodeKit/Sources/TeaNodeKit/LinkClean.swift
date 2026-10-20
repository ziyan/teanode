import Foundation

/// LinkClean shortens a link for people: the tracking parameters taken out of
/// its query string, and the short text shown for a link that has no words of
/// its own.
///
/// It is the same work as internal/util/linkclean on the server and
/// linkClean.ts in the dashboard, done on the link's text rather than on a
/// parsed URL, because each language's URL parser normalizes differently
/// (host case, percent escapes, dot segments). The cases in
/// internal/util/linkclean/testdata/links.json are read by the tests of all
/// three; a change here is a change there.
public enum LinkClean {
    /// The longest text shown for a link, in characters.
    public static let displayLength = 48

    /// The longest cleaned query, question mark included, that is shown with
    /// the link. A longer one is left out.
    public static let displayQueryLength = 32

    /// Query parameters that say where a visitor came from and nothing about
    /// what the page is. utm_ is a family, matched by prefix.
    private static let trackingParameters: Set<String> = [
        "gclid", "gclsrc", "gbraid", "wbraid", "dclid", "fbclid", "msclkid", "yclid", "twclid", "ttclid",
        "li_fat_id", "igshid", "mc_cid", "mc_eid", "_gl", "_ga", "_hsenc", "_hsmi", "mkt_tok",
        "oly_anon_id", "oly_enc_id", "vero_id", "rb_clickid", "s_cid", "ref_src",
    ]

    /// The parts of a web link, each as written. query and fragment keep their
    /// question mark and number sign, and are empty when the link has none.
    private struct Parts {
        let scheme: String
        let authority: String
        let path: String
        let query: String
        let fragment: String

        init?(_ link: String) {
            let scalars = link.unicodeScalars
            let schemeLength: Int
            if String(String.UnicodeScalarView(scalars.prefix(8))).lowercased() == "https://" {
                schemeLength = 5
            } else if String(String.UnicodeScalarView(scalars.prefix(7))).lowercased() == "http://" {
                schemeLength = 4
            } else {
                return nil
            }
            scheme = String(String.UnicodeScalarView(scalars.prefix(schemeLength)))
            let afterScheme = scalars.dropFirst(schemeLength + 3)
            let authorityEnd = afterScheme.firstIndex(where: { $0 == "/" || $0 == "?" || $0 == "#" }) ?? afterScheme.endIndex
            authority = String(String.UnicodeScalarView(afterScheme[..<authorityEnd]))
            let afterAuthority = afterScheme[authorityEnd...]
            let pathEnd = afterAuthority.firstIndex(where: { $0 == "?" || $0 == "#" }) ?? afterAuthority.endIndex
            path = String(String.UnicodeScalarView(afterAuthority[..<pathEnd]))
            let afterPath = afterAuthority[pathEnd...]
            if afterPath.first == "?" {
                let queryEnd = afterPath.firstIndex(of: "#") ?? afterPath.endIndex
                query = String(String.UnicodeScalarView(afterPath[..<queryEnd]))
                fragment = String(String.UnicodeScalarView(afterPath[queryEnd...]))
            } else {
                query = ""
                fragment = String(String.UnicodeScalarView(afterPath))
            }
        }
    }

    private static func isTracking(_ parameter: String) -> Bool {
        let name = parameter.prefix(while: { $0 != "=" }).lowercased()
        return trackingParameters.contains(name) || name.hasPrefix("utm_")
    }

    /// The link without its tracking parameters. Every other parameter stays,
    /// as written and in its order. Anything that is not an http or https
    /// link, and a link with nothing to remove, comes back exactly as given.
    public static func cleaned(_ link: String) -> String {
        guard let parts = Parts(link), !parts.query.isEmpty else { return link }
        let parameters = parts.query.dropFirst().components(separatedBy: "&")
        guard parameters.contains(where: isTracking) else { return link }
        // Dropping a tracking parameter rewrites the query, and the empty
        // parameters it leaves (a doubled ampersand) go with it.
        let kept = parameters.filter { !$0.isEmpty && !isTracking($0) }
        let query = kept.isEmpty ? "" : "?" + kept.joined(separator: "&")
        return parts.scheme + "://" + parts.authority + parts.path + query + parts.fragment
    }

    /// The short text for a link that has no words of its own: the host
    /// without "www.", the path, and the cleaned query only when it is short.
    /// No scheme, no credentials, no fragment; past `displayLength` characters
    /// the middle is cut and replaced with an ellipsis.
    public static func displayText(_ link: String) -> String {
        guard let parts = Parts(cleaned(link)) else { return shortened(link) }
        var host = parts.authority.lowercased()
        if let at = host.lastIndex(of: "@") { host = String(host[host.index(after: at)...]) }
        if host.hasPrefix("www.") { host = String(host.dropFirst(4)) }
        // Percent escapes are shown as the characters they stand for; text
        // that is not valid percent-encoding, or not UTF-8, is shown as written.
        let path = parts.path.removingPercentEncoding ?? parts.path
        var query = parts.query
        if query.unicodeScalars.count > displayQueryLength { query = "" }
        let shownPath = path == "/" && query.isEmpty ? "" : path
        return shortened(host + shownPath + query)
    }

    /// The text with the middle cut out when it is longer than `displayLength`
    /// characters, so that both the start, which says where the link goes, and
    /// the end, which says what it is, stay. Characters are Unicode scalars, as
    /// on the server.
    private static func shortened(_ text: String) -> String {
        let characters = Array(text.unicodeScalars)
        guard characters.count > displayLength else { return text }
        let headLength = displayLength / 2
        let tailLength = displayLength - headLength - 1
        var shortened = String.UnicodeScalarView(characters[..<headLength])
        shortened.append("\u{2026}")
        shortened.append(contentsOf: characters[(characters.count - tailLength)...])
        return String(shortened)
    }
}
