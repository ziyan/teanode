import Foundation

/// Pending capture changes are drained after durable journal writes. Stop always wins.
public struct RecordingCaptureTransitions: Sendable {
    public enum Command: Equatable, Sendable {
        case stop
        case pause(isInterruption: Bool)
        case rollover(recorderId: UUID, wasSuccessful: Bool)
    }
    private var hasPendingStop = false
    private var hasPendingPause = false
    private var hasPendingInterruption = false
    private var pendingRollover: Command?

    public init() {}
    public mutating func enqueue(_ command: Command) {
        switch command {
        case .stop: hasPendingStop = true
        case .pause(let isInterruption):
            hasPendingPause = true
            hasPendingInterruption = hasPendingInterruption || isInterruption
        case .rollover: pendingRollover = command
        }
    }
    public var shouldStartNextSegment: Bool { !hasPendingStop && !hasPendingPause }
    public mutating func next() -> Command? {
        if hasPendingStop {
            self = Self()
            return .stop
        }
        if hasPendingPause {
            let isInterruption = hasPendingInterruption
            self = Self()
            return .pause(isInterruption: isInterruption)
        }
        let command = pendingRollover
        pendingRollover = nil
        return command
    }
}
