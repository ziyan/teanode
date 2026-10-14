import Foundation

/// RecordingCutReason is why a piece of a recording under way was closed.
public enum RecordingCutReason: Equatable, Sendable {
    /// The live stream reported the end of an utterance after the piece was long enough.
    case utteranceEnd
    /// The live stream is not connected and the input level stayed near its lowest for a moment.
    case levelPause
    /// The piece reached the longest it may be, wherever the speech is.
    case maximumLength
}

/// RecordingCutPolicy decides when the piece being recorded is closed and the next one begins.
///
/// A piece is never shorter than 20 seconds. After that it is closed at the end of the next utterance the live stream
/// reports (the streaming provider decides utterance ends from speech, so music or room noise does not fool it). While the
/// stream is not connected it is closed at the first 250 milliseconds where the metered level stays within 6 decibels of the
/// lowest level of the last 10 seconds. At 60 seconds it is closed whatever is heard. Where the cut falls is never what keeps
/// a sentence whole: the server reads a sentence that crosses a cut again with the next piece.
///
/// All times are milliseconds on the recording's timeline.
public struct RecordingCutPolicy: Sendable {
    public static let minimumPieceMS: Int64 = 20_000
    public static let maximumPieceMS: Int64 = 60_000
    /// How far back the lowest level is looked for.
    public static let levelWindowMS: Int64 = 10_000
    /// How long the level has to stay near the lowest for the room to count as paused.
    public static let pauseHoldMS: Int64 = 250
    public static let pauseMarginDecibels: Float = 6
    /// The meter is read about every 50 milliseconds; the first reading of a hold may fall this late and still cover it.
    static let levelReadingToleranceMS: Int64 = 100

    private struct LevelReading: Sendable {
        let atMS: Int64
        let decibels: Float
    }

    private var levelReadings: [LevelReading] = []
    private var pieceStartMS: Int64 = 0
    private var utteranceEndHeardAtMS: Int64?

    public init() {}

    /// A new piece begins; an utterance end heard before it does not count for it.
    public mutating func startPiece(atMS: Int64) {
        pieceStartMS = atMS
        utteranceEndHeardAtMS = nil
    }

    /// Takes a reading of the meter. Readings are kept across pieces, because the lowest level is looked for over the last
    /// 10 seconds wherever the piece began.
    public mutating func recordLevel(decibels: Float, atMS: Int64) {
        levelReadings.append(LevelReading(atMS: atMS, decibels: decibels.isFinite ? decibels : -160))
        let oldestMS = atMS - Self.levelWindowMS
        if let firstKept = levelReadings.firstIndex(where: { $0.atMS >= oldestMS }), firstKept > 0 {
            levelReadings.removeFirst(firstKept)
        }
    }

    /// The live stream said an utterance ended.
    public mutating func recordUtteranceEnd(atMS: Int64) {
        utteranceEndHeardAtMS = atMS
    }

    /// Why the piece should be closed now, or nil to go on.
    public func cutReason(atMS: Int64, isStreamConnected: Bool) -> RecordingCutReason? {
        let pieceMS = atMS - pieceStartMS
        if pieceMS >= Self.maximumPieceMS { return .maximumLength }
        guard pieceMS >= Self.minimumPieceMS else { return nil }
        if isStreamConnected {
            // Only an utterance that ended once the piece was long enough is the next one to cut at.
            if let heardAtMS = utteranceEndHeardAtMS, heardAtMS - pieceStartMS >= Self.minimumPieceMS { return .utteranceEnd }
            return nil
        }
        return isLevelPaused(atMS: atMS) ? .levelPause : nil
    }

    private func isLevelPaused(atMS: Int64) -> Bool {
        let windowReadings = levelReadings.filter { $0.atMS >= atMS - Self.levelWindowMS && $0.atMS <= atMS }
        guard let lowestDecibels = windowReadings.map(\.decibels).min() else { return false }
        let holdStartMS = atMS - Self.pauseHoldMS
        let holdReadings = windowReadings.filter { $0.atMS >= holdStartMS }
        guard let firstHold = holdReadings.first, firstHold.atMS <= holdStartMS + Self.levelReadingToleranceMS, holdReadings.count >= 2 else {
            return false
        }
        // The latest reading must be recent too, or the hold is of a meter that stopped reporting.
        guard let lastHold = holdReadings.last, atMS - lastHold.atMS <= Self.levelReadingToleranceMS else { return false }
        return holdReadings.allSatisfy { $0.decibels <= lowestDecibels + Self.pauseMarginDecibels }
    }
}

/// RecordingLiveBackoff says how long the live stream waits before it connects again after it failed.
public enum RecordingLiveBackoff {
    public static let longestDelaySeconds: Double = 60

    /// 1, 2, 4, 8, 16, 32 seconds and then 60, by how many times in a row the connection failed (1 for the first failure).
    public static func delaySeconds(afterFailureCount failureCount: Int) -> Double {
        guard failureCount > 0 else { return 0 }
        return min(longestDelaySeconds, Double(1 << min(failureCount - 1, 10)))
    }
}
