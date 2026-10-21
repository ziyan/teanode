import Foundation

/// LinkOpening says where a link opened in the app goes: a web page opens in
/// the app's own browser, which comes up from the bottom, and everything the
/// system or another app does better opens there.
public enum LinkOpening {
    public enum Way: Equatable, Sendable {
        /// The in-app browser, over whatever is on screen.
        case inApp
        /// The system: another app, or Safari itself.
        case system
    }

    /// Hosts whose pages belong to an app of the system. Directions, and a
    /// place shared from Maps, open the Maps app.
    private static let systemHosts: Set<String> = ["maps.apple.com", "maps.apple"]

    /// Where this app's own universal links are: teanode.com/app/*, which iOS
    /// opens in the app when it is installed.
    private static let appPathPrefix = "/app"

    /// How a link opens. Only an http or https link opens in the app's
    /// browser; mailto:, tel:, sms:, teanode:// and any other scheme is the
    /// system's, and so are the web pages that something else handles:
    /// Apple Maps, this app's own universal links, and a configuration
    /// profile, which only Safari installs.
    public static func way(for link: URL) -> Way {
        guard let scheme = link.scheme?.lowercased(), scheme == "http" || scheme == "https",
            let host = link.host?.lowercased(), !host.isEmpty
        else { return .system }
        if systemHosts.contains(host) { return .system }
        if host == ConnectLink.host, link.path == appPathPrefix || link.path.hasPrefix(appPathPrefix + "/") { return .system }
        if link.path.lowercased().hasSuffix(".mobileconfig") { return .system }
        return .inApp
    }
}
