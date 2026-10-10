import Foundation

/// ServerAddress is the TeaNode server a person typed or scanned, made into
/// the address the app talks to: a scheme, a host and perhaps a port, and
/// nothing else.
///
/// People type `mail.example.com`, paste the dashboard's address with a page
/// on the end, or scan a link that names the server. All of these mean the
/// same server.
public struct ServerAddress: Equatable, Hashable, Sendable, CustomStringConvertible {
    /// The server's origin, such as `https://mail.example.com:10443`.
    public let origin: URL

    /// Reads what a person typed. Without a scheme it is taken to be https.
    /// Plain http is refused except on the person's own machine, where a
    /// development server has no certificate.
    public init(parsing text: String) throws {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty {
            throw ServerAddressError.isEmpty
        }
        let written = trimmed.contains("://") ? trimmed : "https://" + trimmed
        guard var components = URLComponents(string: written),
            let scheme = components.scheme?.lowercased(),
            let host = components.host?.lowercased(), !host.isEmpty
        else {
            throw ServerAddressError.isNotAnAddress
        }
        if components.user != nil || components.password != nil {
            throw ServerAddressError.hasCredentials
        }
        switch scheme {
        case "https":
            break
        case "http":
            if !Self.isLoopback(host) {
                throw ServerAddressError.isNotEncrypted
            }
        default:
            throw ServerAddressError.isNotAnAddress
        }
        if host.contains(" ") || host.hasPrefix(".") || host.hasSuffix(".") {
            throw ServerAddressError.isNotAnAddress
        }

        // Only where the server is: a page, a query or a fragment someone
        // pasted along with it is not part of it.
        components.scheme = scheme
        components.host = host
        components.path = ""
        components.query = nil
        components.fragment = nil
        if let port = components.port, port == 443 && scheme == "https" || port == 80 && scheme == "http" {
            components.port = nil
        }
        guard let origin = components.url else {
            throw ServerAddressError.isNotAnAddress
        }
        self.origin = origin
    }

    /// The address of one of the server's endpoints, such as
    /// `/api/v1/graphql`.
    public func endpoint(_ path: String) -> URL {
        var components = URLComponents(url: origin, resolvingAgainstBaseURL: false)!
        components.path = path.hasPrefix("/") ? path : "/" + path
        return components.url!
    }

    /// The websocket address of one of the server's endpoints: `wss` for
    /// `https`, `ws` for `http`.
    public func websocketEndpoint(_ path: String) -> URL {
        var components = URLComponents(url: endpoint(path), resolvingAgainstBaseURL: false)!
        components.scheme = components.scheme == "http" ? "ws" : "wss"
        return components.url!
    }

    public var description: String {
        origin.absoluteString
    }

    static func isLoopback(_ host: String) -> Bool {
        host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]"
            || host.hasSuffix(".localhost")
    }
}

/// ServerAddressError is why what a person typed is not a server's address.
public enum ServerAddressError: Error, Equatable, Sendable {
    case isEmpty
    case isNotAnAddress
    case isNotEncrypted
    case hasCredentials
}
