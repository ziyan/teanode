import Foundation

#if canImport(FoundationNetworking)
    import FoundationNetworking
#endif

/// AppUpdateChannel is where this copy of the app was installed from, and so
/// where its updates come from.
public enum AppUpdateChannel: String, Sendable {
    case testFlight = "testflight"
    case appStore = "appstore"
}

/// AppUpdate is the newest app on a channel, as the server reads it.
public struct AppUpdate: Codable, Hashable, Sendable {
    public let updateChannel: String
    public let version: String
    /// Empty on the App Store, which shows people a version and nothing else.
    public let buildNumber: String
    public let installUrl: String

    public init(updateChannel: String, version: String, buildNumber: String, installUrl: String) {
        self.updateChannel = updateChannel
        self.version = version
        self.buildNumber = buildNumber
        self.installUrl = installUrl
    }

    /// The update as the person reads it: 1.2.0, or 1.2.0 (345) on TestFlight.
    public var versionText: String {
        buildNumber.isEmpty ? version : "\(version) (\(buildNumber))"
    }

    /// What a dismissal remembers: this update and no other, so that the next
    /// one is offered again.
    public var dismissalKey: String {
        "\(updateChannel) \(versionText)"
    }

    /// Whether this is newer than the app that is running. The version comes
    /// first; the build only tells builds of one version apart.
    public func isNewer(thanVersion runningVersion: String, build runningBuild: String) -> Bool {
        let order = Self.order(of: version, against: runningVersion)
        if order != 0 { return order > 0 }
        guard let build = Int(buildNumber), let running = Int(runningBuild) else { return false }
        return build > running
    }

    /// Below zero, zero or above, as one version comes before, with or after
    /// another: 1.10.0 is after 1.9.0, and 1.2 is 1.2.0.
    private static func order(of left: String, against right: String) -> Int {
        let leftParts = left.split(separator: ".").map { Int($0) ?? 0 }
        let rightParts = right.split(separator: ".").map { Int($0) ?? 0 }
        for index in 0..<max(leftParts.count, rightParts.count) {
            let leftPart = index < leftParts.count ? leftParts[index] : 0
            let rightPart = index < rightParts.count ? rightParts[index] : 0
            if leftPart != rightPart { return leftPart - rightPart }
        }
        return 0
    }
}

extension Documents {
    public static let appUpdate = """
        query ($updateChannel: String!) {
          GetAppUpdate(updateChannel: $updateChannel) { updateChannel version buildNumber installUrl }
        }
        """
}

extension Connection {
    /// The newest app on a channel; nil when there is none to offer, or this
    /// server does not look for releases.
    public func appUpdate(channel: AppUpdateChannel) async throws -> AppUpdate? {
        struct Answer: Decodable, Sendable { let GetAppUpdate: AppUpdate? }
        return try await execute(Documents.appUpdate, variables: ["updateChannel": .string(channel.rawValue)], as: Answer.self).GetAppUpdate
    }
}
