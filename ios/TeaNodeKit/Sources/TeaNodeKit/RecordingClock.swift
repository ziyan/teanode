import Foundation

public enum RecordingClock {
    /// Invalid import metadata stays unknown rather than trapping or changing the original.
    public static func durationMS(seconds: Double) -> Int64 {
        let durationMS = seconds * 1000
        guard durationMS.isFinite, durationMS >= 0, durationMS < Double(Int64.max) else { return 0 }
        return Int64(durationMS)
    }
}
