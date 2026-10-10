import Foundation

/// ConnectLink is what a connect code holds: the link the dashboard, the
/// command line or the agent shows as a QR code to connect a phone,
/// `https://teanode.com/app/connect#server=<address>&code=<secret>`.
///
/// Scanned by the phone's camera it opens the app, which claims the address,
/// or the App Store when the app is not there. The server and the one-time
/// secret ride after the "#", which a browser never sends, so neither reaches
/// the project's web server.
public struct ConnectLink: Equatable, Sendable {
    /// The host and path the link is served from. The app claims them, so
    /// iOS opens the app for them instead of Safari.
    public static let host = "teanode.com"
    public static let path = "/app/connect"

    /// The server the link connects to.
    public let server: ServerAddress

    /// The one-time secret the app exchanges for its sign-in, when the link
    /// carries one. A link with only a server leaves signing in to the person.
    public let connectCode: String?

    public init(server: ServerAddress, connectCode: String? = nil) {
        self.server = server
        self.connectCode = connectCode
    }

    /// The connect link a URL is, or nil when it is not one.
    public init?(_ link: URL) {
        guard let components = URLComponents(url: link, resolvingAgainstBaseURL: false),
            components.scheme?.lowercased() == "https",
            components.host?.lowercased() == Self.host,
            components.path == Self.path,
            let fragment = components.percentEncodedFragment
        else {
            return nil
        }
        var parsed = URLComponents()
        parsed.percentEncodedQuery = fragment
        let items = parsed.queryItems ?? []
        guard let written = items.first(where: { $0.name == "server" })?.value,
            let server = try? ServerAddress(parsing: written)
        else {
            return nil
        }
        let code = items.first(where: { $0.name == "code" })?.value
        self.init(server: server, connectCode: code?.isEmpty == false ? code : nil)
    }

    /// The link, as the server writes it.
    public var url: URL {
        var fragment = URLComponents()
        fragment.queryItems = [URLQueryItem(name: "server", value: server.description)]
        if let connectCode {
            fragment.queryItems?.append(URLQueryItem(name: "code", value: connectCode))
        }
        var components = URLComponents()
        components.scheme = "https"
        components.host = Self.host
        components.path = Self.path
        components.percentEncodedFragment = fragment.percentEncodedQuery
        return components.url!
    }
}
