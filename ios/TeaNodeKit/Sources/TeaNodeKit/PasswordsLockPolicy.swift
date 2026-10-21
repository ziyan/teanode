import Foundation

/// PasswordsLockPolicy decides when Passwords asks for the passphrase (or Face
/// ID) again. The person chooses the time (`lockAfterMinutes`, 1 to 10080); the
/// time runs from the last use, not from the unlock, so a person who keeps
/// using Passwords is not asked in the middle of it.
public enum PasswordsLockPolicy {
    public static let minimumLockAfterMinutes = 1
    public static let maximumLockAfterMinutes = 10080
    /// How far in the future a last-used time may be before it is not believed.
    /// A phone's clock is corrected by a second or two now and then; one set
    /// back by hours must not keep Passwords open for those hours.
    public static let clockSkewToleranceSeconds: TimeInterval = 5

    /// The lock time kept within what the server accepts.
    public static func clamped(lockAfterMinutes: Int) -> Int {
        min(max(lockAfterMinutes, minimumLockAfterMinutes), maximumLockAfterMinutes)
    }

    /// Whether the person must be asked again: never used since the app
    /// started, used longer ago than the lock time, or used "in the future".
    public static func isAskRequired(lockAfterMinutes: Int, lastUsedAt: Date?, now: Date) -> Bool {
        guard let lastUsedAt else { return true }
        let elapsedSeconds = now.timeIntervalSince(lastUsedAt)
        if elapsedSeconds < -clockSkewToleranceSeconds { return true }
        return elapsedSeconds >= TimeInterval(clamped(lockAfterMinutes: lockAfterMinutes) * 60)
    }

    /// The moment Passwords locks if it is not used again.
    public static func lockDate(lockAfterMinutes: Int, lastUsedAt: Date) -> Date {
        lastUsedAt.addingTimeInterval(TimeInterval(clamped(lockAfterMinutes: lockAfterMinutes) * 60))
    }
}
