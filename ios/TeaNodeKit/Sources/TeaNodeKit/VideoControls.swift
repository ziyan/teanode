import Foundation

/// VideoControls holds the rules of the controls floating over a video in the
/// photo viewer: when they hide by themselves, how far along the video is, and
/// how a time is written. They are plain numbers in and out, so that they can
/// be tested without a player.
public enum VideoControls {
    /// How long a playing video can be left alone before the controls, the
    /// bars and the strip of photos go.
    public static let hideDelaySeconds: Double = 3

    /// How far a swipe up or down on the progress bar moves a video with
    /// VoiceOver on, which cannot drag.
    public static let adjustStepSeconds: Double = 5

    /// Whether the controls hide once the delay has passed with nothing
    /// touched. Only a video that is playing hides them: a paused one or one
    /// that has ended keeps them to be pressed, a finger on the progress bar
    /// keeps them, and so does VoiceOver, since the person cannot find
    /// controls that come and go.
    public static func shouldHide(isPlaying: Bool, hasEnded: Bool, isScrubbing: Bool, isVoiceOverRunning: Bool) -> Bool {
        isPlaying && !hasEnded && !isScrubbing && !isVoiceOverRunning
    }

    /// The part of the video that has played, from 0 to 1. A video whose
    /// length is not known yet has played none of it.
    public static func playedFraction(elapsedSeconds: Double, durationSeconds: Double) -> Double {
        guard elapsedSeconds.isFinite, durationSeconds.isFinite, durationSeconds > 0 else { return 0 }
        return min(1, max(0, elapsedSeconds / durationSeconds))
    }

    /// The moment a point on the progress bar stands for, where `fraction` is
    /// how far along the bar it is. A point beyond either end is the end.
    public static func seconds(atFraction fraction: Double, durationSeconds: Double) -> Double {
        guard fraction.isFinite, durationSeconds.isFinite, durationSeconds > 0 else { return 0 }
        return min(1, max(0, fraction)) * durationSeconds
    }

    /// Whether playing from here would end at once, so that Play starts the
    /// video again from the beginning instead.
    public static func isAtEnd(elapsedSeconds: Double, durationSeconds: Double) -> Bool {
        guard elapsedSeconds.isFinite, durationSeconds.isFinite, durationSeconds > 0 else { return false }
        return elapsedSeconds >= durationSeconds - 0.05
    }

    /// The moment `stepSeconds` after or before `elapsedSeconds`, kept inside
    /// the video.
    public static func seconds(from elapsedSeconds: Double, movingBy stepSeconds: Double, durationSeconds: Double) -> Double {
        guard elapsedSeconds.isFinite, durationSeconds.isFinite, durationSeconds > 0 else { return 0 }
        return min(durationSeconds, max(0, elapsedSeconds + stepSeconds))
    }

    /// A time as m:ss, or h:mm:ss from an hour on. The seconds that have not
    /// been reached yet are not counted, as a clock does: 59.9 is 0:59.
    public static func timeText(seconds: Double) -> String {
        let wholeSeconds = seconds.isFinite ? max(0, Int(seconds.rounded(.down))) : 0
        let hours = wholeSeconds / 3600
        let minutes = (wholeSeconds % 3600) / 60
        let remainingSeconds = wholeSeconds % 60
        if hours > 0 { return String(format: "%ld:%02ld:%02ld", hours, minutes, remainingSeconds) }
        return String(format: "%ld:%02ld", minutes, remainingSeconds)
    }
}
