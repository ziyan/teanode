import Foundation

/// DevicePassword is a password made for one device, sync or mail. Both
/// lists show it the same way: when it was made, when it last signed
/// anything in, and whether it looks abandoned.
public protocol DevicePassword: Sendable {
    var passwordCreatedAt: String? { get }
    var passwordLastUsedAt: String? { get }
}

extension DevicePassword {
    /// A password nobody used within this long of being made was probably
    /// replaced by another one made right after it.
    public static var neverUsedGraceSeconds: TimeInterval { 60 * 60 }
    /// A password silent this long is more likely forgotten than occasional.
    public static var longUnusedSeconds: TimeInterval { 90 * 24 * 60 * 60 }

    /// The password was never used more than an hour after it was made, or
    /// has not been used for ninety days. Only a hint: nothing is deleted
    /// for the person.
    public func isMaybeSafeToDelete(now: Date = Date()) -> Bool {
        if let lastUsedAt = ServerTime.date(passwordLastUsedAt) {
            return now.timeIntervalSince(lastUsedAt) > Self.longUnusedSeconds
        }
        guard let createdAt = ServerTime.date(passwordCreatedAt) else { return false }
        return now.timeIntervalSince(createdAt) > Self.neverUsedGraceSeconds
    }
}

extension Array where Element: DevicePassword {
    /// The newest password first, so the one just made is at the top. A
    /// password with no readable creation time goes last.
    public func newestFirst() -> [Element] {
        sorted {
            (ServerTime.date($0.passwordCreatedAt) ?? .distantPast) > (ServerTime.date($1.passwordCreatedAt) ?? .distantPast)
        }
    }
}

extension SyncCredential: DevicePassword {
    public var passwordCreatedAt: String? { createdAt }
    public var passwordLastUsedAt: String? { lastUsedAt }
}

extension MailAppPassword: DevicePassword {
    public var passwordCreatedAt: String? { createdAt }
    public var passwordLastUsedAt: String? { lastUsedAt }
}
