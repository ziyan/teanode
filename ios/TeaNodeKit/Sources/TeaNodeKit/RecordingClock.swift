import Foundation

public enum RecordingClock {
    /// Invalid import metadata stays unknown rather than trapping or changing the original.
    public static func durationMS(seconds: Double) -> Int64 {
        let durationMS = seconds * 1000
        guard durationMS.isFinite, durationMS >= 0, durationMS < Double(Int64.max) else { return 0 }
        return Int64(durationMS)
    }
}

/// RecordingPieceBoundary keeps the pieces of one continuous capture end to end on the timeline.
///
/// The microphone is captured once for the whole recording and written to a new file at each cut, without the audio stopping, so
/// a piece begins exactly where the one before it ended: at its start plus the length of what was written to it. Only after a
/// pause, an interruption or a change of route, when the capture really stops, does a piece begin later than the one before
/// ended, and the difference is the gap.
public enum RecordingPieceBoundary {
    /// The length in milliseconds of `writtenFrames` of audio at `sampleRate`, rounded to the nearest millisecond.
    public static func lengthMS(writtenFrames: Int64, sampleRate: Double) -> Int64 {
        guard writtenFrames > 0, sampleRate > 0 else { return 0 }
        return Int64((Double(writtenFrames) * 1000 / sampleRate).rounded())
    }

    /// Where the piece after one that began at `previousStartMS` and has `writtenFrames` in it begins.
    public static func nextStartMS(previousStartMS: Int64, writtenFrames: Int64, sampleRate: Double) -> Int64 {
        previousStartMS + lengthMS(writtenFrames: writtenFrames, sampleRate: sampleRate)
    }

    /// Milliseconds of the timeline that no piece covers between a piece ending at `previousEndMS` and one beginning at `startMS`.
    public static func gapMS(previousEndMS: Int64, startMS: Int64) -> Int64 {
        max(0, startMS - previousEndMS)
    }
}
