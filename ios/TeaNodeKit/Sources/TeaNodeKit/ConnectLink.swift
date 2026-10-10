import Foundation

/// ConnectLink is the link the dashboard and the agent show as a QR code to
/// connect a phone: `https://teanode.com/app/connect?server=<address>`.
/// Opened on a phone it opens the app, or the App Store when the app is not
/// there; scanned from inside the app it fills in the server.
public enum ConnectLink {
    /// The host and path the link is served from. The app claims them, so
    /// iOS opens the app for them instead of Safari.
    public static let host = "teanode.com"
    public static let path = "/app/connect"

    /// The server a connect link names, or nil when the link is not one.
    public static func server(in link: URL) -> ServerAddress? {
        guard let components = URLComponents(url: link, resolvingAgainstBaseURL: false),
            components.scheme?.lowercased() == "https",
            components.host?.lowercased() == host,
            components.path == path,
            let server = components.queryItems?.first(where: { $0.name == "server" })?.value
        else {
            return nil
        }
        return try? ServerAddress(parsing: server)
    }

    /// The connect link for a server.
    public static func link(for server: ServerAddress) -> URL {
        var components = URLComponents()
        components.scheme = "https"
        components.host = host
        components.path = path
        components.queryItems = [URLQueryItem(name: "server", value: server.description)]
        return components.url!
    }
}
