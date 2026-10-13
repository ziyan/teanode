/// Uploads continue only after a verified receipt or an explicit foreground retry.
public struct RecordingUploadScheduling: Sendable {
    private var pendingAccountKeys: [String] = []
    public init() {}
    public var isEmpty: Bool { pendingAccountKeys.isEmpty }
    public mutating func enqueue(_ accountKey: String) {
        if !pendingAccountKeys.contains(accountKey) { pendingAccountKeys.append(accountKey) }
    }
    public mutating func next() -> String? {
        guard !pendingAccountKeys.isEmpty else { return nil }
        return pendingAccountKeys.removeFirst()
    }
    public mutating func remove(_ accountKey: String) {
        pendingAccountKeys.removeAll { $0 == accountKey }
    }
}
