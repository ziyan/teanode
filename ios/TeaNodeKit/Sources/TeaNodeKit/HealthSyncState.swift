import Foundation

// What the phone remembers between runs about copying Health to the server:
// where it got to in each type (HealthKit's anchor, an opaque blob), which
// days of which types are waiting to be worked out again, when Health was
// turned on, and what has been sent. The phone keeps no copy of the health
// data itself. The file is readable after the first unlock, so a background
// wake just after a restart can go on, and is left out of backups: Apple
// forbids health data in iCloud, and the anchors say what was read.

/// HealthPendingDay is one type on one day whose summary must be worked out
/// again: a new sample landed on it, or the server deleted one from it.
public struct HealthPendingDay: Codable, Hashable, Comparable, Sendable {
    public let healthType: String
    /// "YYYY-MM-DD", the phone's calendar day.
    public let day: String

    public init(healthType: String, day: String) {
        self.healthType = healthType
        self.day = day
    }

    public static func < (first: HealthPendingDay, second: HealthPendingDay) -> Bool {
        (first.healthType, first.day) < (second.healthType, second.day)
    }
}

/// HealthSyncState is everything the phone keeps about Health syncing.
public struct HealthSyncState: Codable, Sendable, Equatable {
    /// Whose it is, like the location outbox: one file for each account.
    public var accountKey: String?
    /// HealthKit's anchor for each type, as it archived it. Opaque here.
    public private(set) var anchors: [String: Data] = [:]
    /// The days waiting to be worked out again.
    public private(set) var pendingDays: Set<HealthPendingDay> = []
    /// When the person turned Health on in the app; nil while it is off.
    public private(set) var healthTurnedOnAt: Date?
    /// When a batch was last taken by the server.
    public private(set) var lastSentAt: Date?
    public private(set) var sentSampleCount = 0
    public private(set) var sentDeletedSampleCount = 0
    public private(set) var sentDaySummaryCount = 0
    public private(set) var sentRouteCount = 0

    public init(accountKey: String? = nil) { self.accountKey = accountKey }

    public var isTurnedOn: Bool { healthTurnedOnAt != nil }

    // MARK: Turning on and off

    /// Notes that the person turned Health on. Anchors already kept stay, so
    /// turning it off and on does not read everything twice; but see
    /// `turnOff`.
    public mutating func turnOn(at date: Date) {
        if healthTurnedOnAt == nil { healthTurnedOnAt = date }
    }

    /// Forgets everything: anchors, waiting days, counts and when it was
    /// turned on. Turning Health on again reads from the beginning, which
    /// the server takes as the same samples.
    public mutating func turnOff() { reset() }

    /// The rule for an erase on the server: if its time is later than when
    /// Health was turned on here, what the phone remembers is of data that
    /// is gone, so the state resets and Health is off until the person turns
    /// it on again. Returns whether it reset. An erase from before Health was
    /// turned on (or at the same moment) changes nothing, and neither does
    /// one when Health is already off.
    @discardableResult
    public mutating func applyServerErase(healthErasedAt: Date?) -> Bool {
        guard let healthErasedAt, let healthTurnedOnAt, healthErasedAt > healthTurnedOnAt else { return false }
        reset()
        return true
    }

    private mutating func reset() {
        anchors = [:]
        pendingDays = []
        healthTurnedOnAt = nil
        lastSentAt = nil
        sentSampleCount = 0
        sentDeletedSampleCount = 0
        sentDaySummaryCount = 0
        sentRouteCount = 0
    }

    // MARK: Anchors

    public func anchor(for healthType: String) -> Data? { anchors[healthType] }

    /// Saves where reading got to, once the server has taken what was read.
    /// Nil forgets it, so the type is read from the beginning.
    public mutating func setAnchor(_ anchor: Data?, for healthType: String) { anchors[healthType] = anchor }

    // MARK: Days to work out again

    /// Notes that a type's summary must be worked out again for every day a
    /// reading from `start` to `end` touches.
    public mutating func markPending(healthType: String, from start: Date, to end: Date, calendar: Calendar) {
        for day in HealthDays.days(from: start, to: max(start, end), calendar: calendar) {
            pendingDays.insert(HealthPendingDay(healthType: healthType, day: day))
        }
    }

    /// Notes the days of the samples the server deleted, which it answered
    /// with their types and times.
    public mutating func markPending(deletedSamples: [HealthDeletedSample], calendar: Calendar) {
        for deleted in deletedSamples {
            guard let start = deleted.startedDate else { continue }
            markPending(healthType: deleted.healthType, from: start, to: deleted.endedDate ?? start, calendar: calendar)
        }
    }

    public mutating func markPending(_ day: HealthPendingDay) { pendingDays.insert(day) }

    /// The waiting days of a type, earliest first.
    public func pendingDays(for healthType: String) -> [String] {
        pendingDays.filter { $0.healthType == healthType }.map(\.day).sorted()
    }

    /// Takes days out once their summaries have been sent.
    public mutating func clearPending(_ days: some Sequence<HealthPendingDay>) { pendingDays.subtract(days) }

    // MARK: Sending

    /// Notes that the server took a batch.
    public mutating func recordSent(_ batch: HealthBatch, result: ReportHealthResult, at date: Date) {
        lastSentAt = date
        sentSampleCount += batch.samples.count
        sentDeletedSampleCount += result.deletedSampleCount
        sentDaySummaryCount += batch.daySummaries.count
        sentRouteCount += batch.routes.count
    }

    // MARK: The file

    private enum CodingKeys: String, CodingKey {
        case accountKey, anchors, pendingDays, healthTurnedOnAt, lastSentAt, sentSampleCount, sentDeletedSampleCount, sentDaySummaryCount
        case sentRouteCount
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        accountKey = try container.decodeIfPresent(String.self, forKey: .accountKey)
        anchors = try container.decodeIfPresent([String: Data].self, forKey: .anchors) ?? [:]
        pendingDays = Set(try container.decodeIfPresent([HealthPendingDay].self, forKey: .pendingDays) ?? [])
        healthTurnedOnAt = try container.decodeIfPresent(Date.self, forKey: .healthTurnedOnAt)
        lastSentAt = try container.decodeIfPresent(Date.self, forKey: .lastSentAt)
        sentSampleCount = try container.decodeIfPresent(Int.self, forKey: .sentSampleCount) ?? 0
        sentDeletedSampleCount = try container.decodeIfPresent(Int.self, forKey: .sentDeletedSampleCount) ?? 0
        sentDaySummaryCount = try container.decodeIfPresent(Int.self, forKey: .sentDaySummaryCount) ?? 0
        sentRouteCount = try container.decodeIfPresent(Int.self, forKey: .sentRouteCount) ?? 0
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encodeIfPresent(accountKey, forKey: .accountKey)
        try container.encode(anchors, forKey: .anchors)
        try container.encode(pendingDays.sorted(), forKey: .pendingDays)
        try container.encodeIfPresent(healthTurnedOnAt, forKey: .healthTurnedOnAt)
        try container.encodeIfPresent(lastSentAt, forKey: .lastSentAt)
        try container.encode(sentSampleCount, forKey: .sentSampleCount)
        try container.encode(sentDeletedSampleCount, forKey: .sentDeletedSampleCount)
        try container.encode(sentDaySummaryCount, forKey: .sentDaySummaryCount)
        try container.encode(sentRouteCount, forKey: .sentRouteCount)
    }

    /// The name of the file an account's state is kept in: one file for each
    /// account, so that switching accounts neither sends one person's Health
    /// as another's nor loses where it got to. The key is percent-encoded, so
    /// no key can name another file or a directory.
    public static func fileName(accountKey: String) -> String {
        let safe = accountKey.addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? ""
        return "health-\(safe).json"
    }

    static func makeEncoder() -> JSONEncoder {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        encoder.outputFormatting = [.sortedKeys]
        return encoder
    }

    static func makeDecoder() -> JSONDecoder {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }

    /// The file's contents.
    public func encoded() throws -> Data { try Self.makeEncoder().encode(self) }

    /// A state read from a file's contents.
    public static func decoded(from data: Data) throws -> HealthSyncState { try makeDecoder().decode(HealthSyncState.self, from: data) }

    /// Writes the state to the account's file in a directory (the app group's),
    /// readable after the first unlock and left out of backups.
    public func save(inDirectory directory: URL) throws {
        guard let accountKey else { return }
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let url = directory.appendingPathComponent(Self.fileName(accountKey: accountKey))
        #if os(iOS)
            try encoded().write(to: url, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
        #else
            try encoded().write(to: url, options: [.atomic])
        #endif
        #if canImport(Darwin)
            // A new file each time (the write is atomic), so set on each.
            var values = URLResourceValues()
            values.isExcludedFromBackup = true
            var excludedURL = url
            try excludedURL.setResourceValues(values)
        #endif
    }

    /// The account's state from a directory; a new one if there is no file,
    /// it cannot be read, or it is of another account.
    public static func load(accountKey: String, inDirectory directory: URL) -> HealthSyncState {
        let url = directory.appendingPathComponent(fileName(accountKey: accountKey))
        guard let data = try? Data(contentsOf: url), let state = try? decoded(from: data), state.accountKey == accountKey else {
            return HealthSyncState(accountKey: accountKey)
        }
        return state
    }
}
