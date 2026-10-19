import Foundation

// What the phone remembers between runs about copying Health to the server:
// where it got to in each type (HealthKit's anchor, an opaque blob), which
// days of which types are waiting to be worked out again, whether Health is
// turned on and the last erase of the server's copy the phone knows of, and
// what has been sent. The phone keeps no copy of the health data itself. The
// file is readable after the first unlock, so a background wake just after a
// restart can go on, and is left out of backups: Apple forbids health data in
// iCloud, and the anchors say what was read.

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
    /// Whether the person has Health turned on in the app.
    public private(set) var isTurnedOn = false
    /// The time the person last erased all of their Health on the server, as
    /// far as this phone knows, written exactly as the server wrote it (RFC
    /// 3339, with its fractions of a second: the server compares what it is
    /// told with its own to the fraction). Read from the server when Health
    /// is turned on, and told to it with every report; nil if the person never
    /// erased, or while Health is off.
    public private(set) var healthErasedAt: String?
    /// Whether Health was turned off because the server told of an erase newer
    /// than `healthErasedAt`. Kept so that the settings can say why the switch
    /// is off, even after the app was closed; the next turn-on clears it.
    public private(set) var isStoppedByErase = false
    /// How many readings and summaries the server refused one by one, so were
    /// left out for good since Health was turned on.
    public private(set) var droppedEntryCount = 0
    /// When a batch was last taken by the server.
    public private(set) var lastSentAt: Date?
    public private(set) var sentSampleCount = 0
    public private(set) var sentDeletedSampleCount = 0
    public private(set) var sentDaySummaryCount = 0
    public private(set) var sentRouteCount = 0
    /// Whether the first full read of everything has finished.
    public private(set) var isHistoryDone = false
    /// The workouts whose route the server took, the latest last, so that the
    /// look at recent workouts for a route saved late does not send one
    /// again. Only the latest are kept.
    public private(set) var sentRouteHealthSampleIds: [String] = []

    /// How many workouts the state remembers a route sent for.
    public static let sentRouteCountMost = 200

    public init(accountKey: String? = nil) { self.accountKey = accountKey }

    // MARK: Turning on and off

    /// Notes that the person turned Health on, and the last erase of the
    /// server's copy the server told of (nil if never). Anchors already kept
    /// stay, so turning it off and on does not read everything twice; but see
    /// `turnOff`. Does nothing when Health is already on.
    public mutating func turnOn(healthErasedAt: String?) {
        guard !isTurnedOn else { return }
        isTurnedOn = true
        isStoppedByErase = false
        self.healthErasedAt = healthErasedAt
    }

    /// Forgets everything: anchors, waiting days, counts and the erase time.
    /// Turning Health on again reads from the beginning, which the server
    /// takes as the same samples.
    public mutating func turnOff() {
        reset()
        isStoppedByErase = false
    }

    /// Whether an answer names an erase on the server that is later than the
    /// one this phone knows of (any erase, if it knows of none). The server
    /// stored nothing of the batch that was answered with one.
    public func isErasedLater(than answer: String?) -> Bool {
        ServerTime.isLater(answer, than: healthErasedAt)
    }

    /// The rule for an erase on the server. The phone tells the server the
    /// erase time it knows with every report. If the answer names a later
    /// one, the person erased everything after the phone last heard of it:
    /// what the phone remembers is of data that is gone, so the state resets
    /// (no anchor, no count) and Health is off until the person turns it on
    /// again, with `isStoppedByErase` set for the notice. Returns whether it
    /// reset. An answer that names the erase the phone knows of (or none)
    /// changes nothing, and neither does one when Health is already off.
    @discardableResult
    public mutating func applyServerErase(healthErasedAt answer: String?) -> Bool {
        guard isTurnedOn, isErasedLater(than: answer) else { return false }
        reset()
        isStoppedByErase = true
        return true
    }

    private mutating func reset() {
        anchors = [:]
        pendingDays = []
        isTurnedOn = false
        healthErasedAt = nil
        lastSentAt = nil
        sentSampleCount = 0
        sentDeletedSampleCount = 0
        sentDaySummaryCount = 0
        sentRouteCount = 0
        droppedEntryCount = 0
        isHistoryDone = false
        sentRouteHealthSampleIds = []
    }

    /// Notes that every type was read to its end.
    public mutating func markHistoryDone() { isHistoryDone = true }

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

    /// Notes entries of batches that the server refused one by one, which
    /// were left out.
    public mutating func recordDropped(entryCount: Int) { droppedEntryCount += max(0, entryCount) }

    /// Notes that the server took a batch. The readings counted are the ones
    /// that were new to it, so a page read again after a later failure is not
    /// counted twice.
    public mutating func recordSent(_ batch: HealthBatch, result: ReportHealthResult, at date: Date) {
        lastSentAt = date
        sentSampleCount += result.newSampleCount
        sentDeletedSampleCount += result.deletedSampleCount
        sentDaySummaryCount += batch.daySummaries.count
        sentRouteCount += batch.routes.count
    }

    /// Takes the server's answer to a batch. If the answer names an erase of
    /// all of the person's Health later than the one this phone knows of, the
    /// server stored nothing: the state is reset (Health is off, no anchor is
    /// kept, and nothing of the batch is counted) and this answers false. Else
    /// the batch is noted as sent and this answers true, and the caller may
    /// move the anchors of what the batch carried.
    @discardableResult
    public mutating func accept(_ result: ReportHealthResult, for batch: HealthBatch, at date: Date) -> Bool {
        if applyServerErase(healthErasedAt: result.healthErasedAt) { return false }
        recordSent(batch, result: result, at: date)
        return true
    }

    // MARK: Routes

    public func hasSentRoute(healthSampleId: String) -> Bool { sentRouteHealthSampleIds.contains(healthSampleId) }

    /// Notes that the server took a workout's route. A route the server could
    /// not match to a workout yet is not noted, so it is sent again.
    public mutating func recordRouteSent(healthSampleId: String) {
        guard !sentRouteHealthSampleIds.contains(healthSampleId) else { return }
        sentRouteHealthSampleIds.append(healthSampleId)
        if sentRouteHealthSampleIds.count > Self.sentRouteCountMost {
            sentRouteHealthSampleIds.removeFirst(sentRouteHealthSampleIds.count - Self.sentRouteCountMost)
        }
    }

    // MARK: The file

    private enum CodingKeys: String, CodingKey {
        case accountKey, anchors, pendingDays, isTurnedOn, healthErasedAt, isStoppedByErase, lastSentAt, sentSampleCount
        case sentDeletedSampleCount, sentDaySummaryCount, sentRouteCount, droppedEntryCount, isHistoryDone, sentRouteHealthSampleIds
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        accountKey = try container.decodeIfPresent(String.self, forKey: .accountKey)
        anchors = try container.decodeIfPresent([String: Data].self, forKey: .anchors) ?? [:]
        pendingDays = Set(try container.decodeIfPresent([HealthPendingDay].self, forKey: .pendingDays) ?? [])
        isTurnedOn = try container.decodeIfPresent(Bool.self, forKey: .isTurnedOn) ?? false
        healthErasedAt = try container.decodeIfPresent(String.self, forKey: .healthErasedAt)
        isStoppedByErase = try container.decodeIfPresent(Bool.self, forKey: .isStoppedByErase) ?? false
        lastSentAt = try container.decodeIfPresent(Date.self, forKey: .lastSentAt)
        sentSampleCount = try container.decodeIfPresent(Int.self, forKey: .sentSampleCount) ?? 0
        sentDeletedSampleCount = try container.decodeIfPresent(Int.self, forKey: .sentDeletedSampleCount) ?? 0
        sentDaySummaryCount = try container.decodeIfPresent(Int.self, forKey: .sentDaySummaryCount) ?? 0
        sentRouteCount = try container.decodeIfPresent(Int.self, forKey: .sentRouteCount) ?? 0
        droppedEntryCount = try container.decodeIfPresent(Int.self, forKey: .droppedEntryCount) ?? 0
        isHistoryDone = try container.decodeIfPresent(Bool.self, forKey: .isHistoryDone) ?? false
        sentRouteHealthSampleIds = try container.decodeIfPresent([String].self, forKey: .sentRouteHealthSampleIds) ?? []
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encodeIfPresent(accountKey, forKey: .accountKey)
        try container.encode(anchors, forKey: .anchors)
        try container.encode(pendingDays.sorted(), forKey: .pendingDays)
        try container.encode(isTurnedOn, forKey: .isTurnedOn)
        try container.encodeIfPresent(healthErasedAt, forKey: .healthErasedAt)
        try container.encode(isStoppedByErase, forKey: .isStoppedByErase)
        try container.encodeIfPresent(lastSentAt, forKey: .lastSentAt)
        try container.encode(sentSampleCount, forKey: .sentSampleCount)
        try container.encode(sentDeletedSampleCount, forKey: .sentDeletedSampleCount)
        try container.encode(sentDaySummaryCount, forKey: .sentDaySummaryCount)
        try container.encode(sentRouteCount, forKey: .sentRouteCount)
        try container.encode(droppedEntryCount, forKey: .droppedEntryCount)
        try container.encode(isHistoryDone, forKey: .isHistoryDone)
        try container.encode(sentRouteHealthSampleIds, forKey: .sentRouteHealthSampleIds)
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

    /// The accounts Health is turned on for, from the state files in a
    /// directory. Launch needs this before any account is known, to decide
    /// whether to watch HealthKit for new data, so it is kept nowhere but
    /// in the files that say what to forget when Health is turned off.
    public static func accountKeysTurnedOn(inDirectory directory: URL) -> Set<String> {
        let names = (try? FileManager.default.contentsOfDirectory(atPath: directory.path)) ?? []
        var accountKeys: Set<String> = []
        for name in names where name.hasPrefix("health-") && name.hasSuffix(".json") {
            guard let data = try? Data(contentsOf: directory.appendingPathComponent(name)), let state = try? decoded(from: data),
                let accountKey = state.accountKey, state.isTurnedOn, fileName(accountKey: accountKey) == name
            else { continue }
            accountKeys.insert(accountKey)
        }
        return accountKeys
    }

    /// Deletes an account's state file, which turns Health off for it.
    public static func remove(accountKey: String, inDirectory directory: URL) {
        try? FileManager.default.removeItem(at: directory.appendingPathComponent(fileName(accountKey: accountKey)))
    }
}
