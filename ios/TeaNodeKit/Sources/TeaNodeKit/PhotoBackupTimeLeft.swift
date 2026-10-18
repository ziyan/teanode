import Foundation

// How long the backup of a photo library has left, worked out from how fast
// photos have been going up lately. A pure value, fed what the status reads,
// so that it runs and is tested without a phone.

/// PhotoBackupTimeLeft estimates the time left to back up the library from the
/// recent rate of backed up items. It is told, as the backup goes on, how many
/// items are backed up at what time, and answers how many seconds the items
/// still to back up will take at the rate of the last few minutes.
///
/// Only a sliding window of recent samples counts, so that a burst at the start
/// (photos the server already has are marked backed up in bulk while the
/// library is listed) or a long stop does not decide the answer for the rest of
/// the backup. The rate is smoothed, so that the answer does not jump from one
/// reading to the next. It starts fresh each launch.
public struct PhotoBackupTimeLeft: Sendable {
    /// How far back the samples that count reach.
    public static let windowSeconds: TimeInterval = 15 * 60
    /// How much of the window the samples must span before the rate is trusted:
    /// a few seconds of progress says little about the next hours.
    public static let minimumSpanSeconds: TimeInterval = 2 * 60
    /// How many items must have gone up in the window before the rate is trusted.
    public static let minimumFinishedItemCount = 5
    /// How long the smoothed rate takes to follow a change in the windowed rate
    /// by about two thirds. The smoothing is by time, not by reading, so that
    /// reading the status more often does not change it.
    public static let smoothingSeconds: TimeInterval = 2 * 60
    /// The least time ever answered while items remain: "less than a minute"
    /// is a promise the backup cannot keep, as one more file can take that long.
    public static let minimumEstimateSeconds = 60
    /// The most samples kept, whatever the rate.
    static let maximumSampleCount = 2_000

    private struct Sample {
        let date: Date
        let backedUpItemCount: Int
    }

    /// Oldest first, all inside the window of the newest.
    private var samples: [Sample] = []
    private var smoothedItemsPerSecond: Double?
    private var smoothedAt: Date?

    public init() {}

    /// Notes how many items are backed up at `date`. When the backup is not
    /// uploading (paused, waiting for the network, off) everything noted so far
    /// is forgotten, so that the pause does not count against the rate once the
    /// backup goes on.
    public mutating func record(date: Date, backedUpItemCount: Int, isUploading: Bool) {
        guard isUploading else {
            forgetEverything()
            return
        }
        // Fewer items backed up than before means the library was counted
        // again, and an earlier date means the clock moved back: the samples
        // no longer describe the same run.
        if let newest = samples.last, backedUpItemCount < newest.backedUpItemCount || date < newest.date {
            forgetEverything()
        }
        let windowStart = date.addingTimeInterval(-Self.windowSeconds)
        if let firstInWindow = samples.firstIndex(where: { $0.date >= windowStart }) {
            samples.removeFirst(firstInWindow)
        } else {
            samples.removeAll()
        }
        // While nothing finishes the status is read again and again with the
        // same count: keep the first and the latest of such a run, not each.
        if samples.count >= 2, samples[samples.count - 1].backedUpItemCount == backedUpItemCount,
            samples[samples.count - 2].backedUpItemCount == backedUpItemCount
        {
            samples.removeLast()
        }
        samples.append(Sample(date: date, backedUpItemCount: backedUpItemCount))
        if samples.count > Self.maximumSampleCount {
            samples.removeFirst(samples.count - Self.maximumSampleCount)
        }
        updateSmoothedRate(date: date)
    }

    /// The seconds the `itemsLeftCount` items still to back up are estimated to
    /// take, or nil when there is nothing to go on: too little progress seen
    /// yet, no item finished in the window, nothing left, or the backup is not
    /// uploading.
    public func estimatedSecondsLeft(itemsLeftCount: Int) -> Int? {
        guard itemsLeftCount > 0, let smoothedItemsPerSecond, smoothedItemsPerSecond > 0 else { return nil }
        let seconds = (Double(itemsLeftCount) / smoothedItemsPerSecond).rounded()
        guard seconds.isFinite, seconds < Double(Int.max) / 2 else { return nil }
        return max(Self.minimumEstimateSeconds, Int(seconds))
    }

    private mutating func updateSmoothedRate(date: Date) {
        guard let oldest = samples.first, let newest = samples.last else { return }
        let spanSeconds = newest.date.timeIntervalSince(oldest.date)
        let finishedItemCount = newest.backedUpItemCount - oldest.backedUpItemCount
        guard spanSeconds >= Self.minimumSpanSeconds, finishedItemCount >= Self.minimumFinishedItemCount else {
            smoothedItemsPerSecond = nil
            smoothedAt = nil
            return
        }
        let windowedItemsPerSecond = Double(finishedItemCount) / spanSeconds
        if let previousItemsPerSecond = smoothedItemsPerSecond, let previousAt = smoothedAt {
            let elapsedSeconds = max(0, date.timeIntervalSince(previousAt))
            let weight = 1 - exp(-elapsedSeconds / Self.smoothingSeconds)
            smoothedItemsPerSecond = previousItemsPerSecond + weight * (windowedItemsPerSecond - previousItemsPerSecond)
        } else {
            smoothedItemsPerSecond = windowedItemsPerSecond
        }
        smoothedAt = date
    }

    private mutating func forgetEverything() {
        samples.removeAll()
        smoothedItemsPerSecond = nil
        smoothedAt = nil
    }
}

// MARK: Saying it the way people do

/// PhotoBackupSpokenDurationUnit is the unit a time left is said in.
public enum PhotoBackupSpokenDurationUnit: Equatable, Sendable {
    case minutes
    case hours
    case days
}

/// PhotoBackupSpokenDuration is a time left rounded as a person says it:
/// "3 hours", "40 minutes", "2 days", never "2 hours 41 minutes".
public struct PhotoBackupSpokenDuration: Equatable, Sendable {
    public let count: Int
    public let unit: PhotoBackupSpokenDurationUnit

    public init(count: Int, unit: PhotoBackupSpokenDurationUnit) {
        self.count = count
        self.unit = unit
    }

    /// Minutes up to the hour (to the nearest five from ten minutes on, since
    /// the estimate is not that exact), then hours up to two days, then days.
    /// Never less than one minute.
    public init(seconds: Int) {
        let minuteCount = (Double(max(0, seconds)) / 60).rounded()
        let spokenMinuteCount = minuteCount < 10 ? minuteCount : (minuteCount / 5).rounded() * 5
        if spokenMinuteCount < 60 {
            self.init(count: max(1, Int(spokenMinuteCount)), unit: .minutes)
            return
        }
        let hourCount = (Double(seconds) / 3600).rounded()
        if hourCount < 48 {
            self.init(count: max(1, Int(hourCount)), unit: .hours)
            return
        }
        self.init(count: Int((hourCount / 24).rounded()), unit: .days)
    }
}
