import Foundation

// MARK: - What the database and the files hold, for the operator

/// StorageBreakdown is how the current size is broken down: the database by
/// what its tables are for, the files by kind, or the files by person.
public enum StorageBreakdown: String, CaseIterable, Sendable {
    case area, kind, person
}

/// StorageKeySize is a size under a key: an area, a kind of file.
public struct StorageKeySize: Codable, Hashable, Sendable {
    public let sizeKey: String
    public let sizeBytes: Int64

    public init(sizeKey: String, sizeBytes: Int64) {
        self.sizeKey = sizeKey
        self.sizeBytes = sizeBytes
    }
}

/// StorageUsageRow is one row of the breakdown asked for.
public struct StorageUsageRow: Codable, Hashable, Sendable, Identifiable {
    /// The area, the kind of file, or the person's id; empty for the files
    /// no account owns.
    public let rowKey: String
    /// A person's name for a person row; the key otherwise.
    public let rowLabel: String
    public let rowSizeBytes: Int64
    /// How many tables an area holds; zero for the rest.
    public let rowTableCount: Int
    /// A person's files split by kind; empty for the rest.
    public let rowFileSizes: [StorageKeySize]

    public var id: String { rowKey }

    public init(rowKey: String, rowLabel: String, rowSizeBytes: Int64, rowTableCount: Int = 0, rowFileSizes: [StorageKeySize] = []) {
        self.rowKey = rowKey
        self.rowLabel = rowLabel
        self.rowSizeBytes = rowSizeBytes
        self.rowTableCount = rowTableCount
        self.rowFileSizes = rowFileSizes
    }
}

/// StorageHistoryDay is what one day's sample held.
public struct StorageHistoryDay: Codable, Hashable, Sendable, Identifiable {
    /// The day as YYYY-MM-DD, in UTC.
    public let sampledOn: String
    public let databaseSizeBytes: Int64
    public let fileSizeBytes: Int64
    public let totalSizeBytes: Int64
    /// That day's areas or kinds, as the breakdown asked for; empty for a
    /// person, which is not sampled.
    public let breakdownSizes: [StorageKeySize]

    public var id: String { sampledOn }

    public init(sampledOn: String, databaseSizeBytes: Int64, fileSizeBytes: Int64, breakdownSizes: [StorageKeySize] = []) {
        self.sampledOn = sampledOn
        self.databaseSizeBytes = databaseSizeBytes
        self.fileSizeBytes = fileSizeBytes
        self.totalSizeBytes = databaseSizeBytes + fileSizeBytes
        self.breakdownSizes = breakdownSizes
    }
}

/// StorageDisk is a disk the server writes to. An object store is not
/// measured: its sizes are nil, and nothing is said about when it fills.
public struct StorageDisk: Codable, Hashable, Sendable, Identifiable {
    /// dataDirectory, fileDirectory or objectStore.
    public let diskLabel: String
    public let isDiskMeasured: Bool
    public let diskTotalBytes: Int64?
    public let diskFreeBytes: Int64?
    public let diskUsedBytes: Int64?
    /// The day it fills at the recent growth, YYYY-MM-DD in UTC; nil when the
    /// server is not growing, there is too little history, or it is not
    /// measured.
    public let projectedFullOn: String?
    public let daysUntilFull: Double?
    /// Whether it fills within thirty days, which is when the operators are
    /// warned.
    public let isProjectedFull: Bool

    public var id: String { diskLabel }

    /// What share of the disk is used, from 0 to 1; nil when not measured.
    public var usedFraction: Double? {
        guard isDiskMeasured, let diskTotalBytes, let diskUsedBytes, diskTotalBytes > 0 else { return nil }
        return min(1, max(0, Double(diskUsedBytes) / Double(diskTotalBytes)))
    }
}

/// StorageGrowth is the line fitted through the last thirty days.
public struct StorageGrowth: Codable, Hashable, Sendable {
    public let growthSampleCount: Int
    public let isGrowthProjectable: Bool
    public let growthBytesPerDay: Double
    public let currentSizeBytes: Int64
    public let projectedSize30DaysBytes: Int64
    public let projectedSize90DaysBytes: Int64
    public let projectedSize365DaysBytes: Int64
    /// Whether the files are in the totals: they are when they are on a
    /// disk the server measures, and not when they are in an object store.
    public let areFilesCounted: Bool
}

/// StorageChartPoint is one stacked column's piece: a day, the series it
/// belongs to, and its size.
public struct StorageChartPoint: Hashable, Sendable, Identifiable {
    public let sampledOn: String
    public let seriesKey: String
    public let sizeBytes: Int64

    public var id: String { "\(sampledOn)/\(seriesKey)" }
}

/// StorageUsage is one answer of the server: the sizes now, the days
/// sampled, the disks and the growth.
public struct StorageUsage: Codable, Hashable, Sendable {
    public let breakdownBy: String
    public let databaseSizeBytes: Int64
    public let fileSizeBytes: Int64
    public let totalSizeBytes: Int64
    public let usageRows: [StorageUsageRow]
    public let historyDays: [StorageHistoryDay]
    public let disks: [StorageDisk]
    public let growth: StorageGrowth

    /// The most series the chart draws apart: the largest few, and the rest
    /// as one.
    public static let chartSeriesLimit = 5
    /// The series that holds what the largest few leave out.
    public static let restSeriesKey = "rest"
    /// The series of a person view, which is not sampled: the database and
    /// the files.
    public static let databaseSeriesKey = "database"
    public static let filesSeriesKey = "files"

    /// The disks that fill within thirty days, soonest first: what the
    /// banner on top of the screen is about.
    public var fillingDisks: [StorageDisk] {
        disks.filter { $0.isProjectedFull && $0.projectedFullOn != nil }.sorted { ($0.daysUntilFull ?? 0) < ($1.daysUntilFull ?? 0) }
    }

    /// The days as stacked columns: the largest areas or kinds of the newest
    /// day apart and the rest together, or the database and the files for a
    /// person.
    public var chartPoints: [StorageChartPoint] {
        guard let newest = historyDays.last else { return [] }
        if breakdownBy == StorageBreakdown.person.rawValue {
            return historyDays.flatMap { day in
                [
                    StorageChartPoint(sampledOn: day.sampledOn, seriesKey: Self.databaseSeriesKey, sizeBytes: day.databaseSizeBytes),
                    StorageChartPoint(sampledOn: day.sampledOn, seriesKey: Self.filesSeriesKey, sizeBytes: day.fileSizeBytes),
                ]
            }
        }
        let drawn = newest.breakdownSizes.sorted { $0.sizeBytes > $1.sizeBytes }.prefix(Self.chartSeriesLimit).map(\.sizeKey)
        var points: [StorageChartPoint] = []
        for day in historyDays {
            var restBytes: Int64 = 0
            for key in drawn {
                points.append(
                    StorageChartPoint(
                        sampledOn: day.sampledOn, seriesKey: key,
                        sizeBytes: day.breakdownSizes.first { $0.sizeKey == key }?.sizeBytes ?? 0))
            }
            for size in day.breakdownSizes where !drawn.contains(size.sizeKey) { restBytes += size.sizeBytes }
            if restBytes > 0 || day.breakdownSizes.count > drawn.count {
                points.append(StorageChartPoint(sampledOn: day.sampledOn, seriesKey: Self.restSeriesKey, sizeBytes: restBytes))
            }
        }
        return points
    }
}

extension Documents {
    public static let storageUsage = """
        query ($by: String, $since: DateTime, $until: DateTime) {
          StorageUsage(by: $by, since: $since, until: $until) {
            breakdownBy databaseSizeBytes fileSizeBytes totalSizeBytes
            usageRows { rowKey rowLabel rowSizeBytes rowTableCount rowFileSizes { sizeKey sizeBytes } }
            historyDays { sampledOn databaseSizeBytes fileSizeBytes totalSizeBytes breakdownSizes { sizeKey sizeBytes } }
            disks { diskLabel isDiskMeasured diskTotalBytes diskFreeBytes diskUsedBytes projectedFullOn daysUntilFull isProjectedFull }
            growth { growthSampleCount isGrowthProjectable growthBytesPerDay currentSizeBytes projectedSize30DaysBytes projectedSize90DaysBytes projectedSize365DaysBytes areFilesCounted }
          }
        }
        """
}

extension Connection {
    /// What the database and the files hold now, broken down as asked, with
    /// the days sampled over the range, the disks and the growth. Needs
    /// server:manage.
    public func storageUsage(breakdown: StorageBreakdown, range: AgentUsageRange) async throws -> StorageUsage {
        struct Answer: Decodable, Sendable { let StorageUsage: StorageUsage }
        let bounds = range.bounds()
        return try await execute(
            Documents.storageUsage,
            variables: ["by": .string(breakdown.rawValue), "since": .string(bounds.since), "until": .string(bounds.until)], as: Answer.self
        ).StorageUsage
    }
}
