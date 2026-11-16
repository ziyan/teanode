package com.teanode.kit.photos

/** What the video player's controls do: when they hide, where a drag lands, and how times are written. */
object VideoControls {
    const val HIDE_DELAY_SECONDS = 3.0
    const val ADJUST_STEP_SECONDS = 5.0

    /** The controls fade while the video plays and nobody is touching them or a screen reader is not in use. */
    fun shouldHide(isPlaying: Boolean, hasEnded: Boolean, isScrubbing: Boolean, isScreenReaderRunning: Boolean): Boolean =
        isPlaying && !hasEnded && !isScrubbing && !isScreenReaderRunning

    fun playedFraction(elapsedSeconds: Double, durationSeconds: Double): Double {
        if (!elapsedSeconds.isFinite() || !durationSeconds.isFinite() || durationSeconds <= 0) return 0.0
        return minOf(1.0, maxOf(0.0, elapsedSeconds / durationSeconds))
    }

    fun seconds(fraction: Double, durationSeconds: Double): Double {
        if (!fraction.isFinite() || !durationSeconds.isFinite() || durationSeconds <= 0) return 0.0
        return minOf(1.0, maxOf(0.0, fraction)) * durationSeconds
    }

    fun isAtEnd(elapsedSeconds: Double, durationSeconds: Double): Boolean {
        if (!elapsedSeconds.isFinite() || !durationSeconds.isFinite() || durationSeconds <= 0) return false
        return elapsedSeconds >= durationSeconds - 0.05
    }

    /** Where a skip of a few seconds lands, kept inside the video. */
    fun seconds(elapsedSeconds: Double, movingBySeconds: Double, durationSeconds: Double): Double {
        if (!elapsedSeconds.isFinite() || !durationSeconds.isFinite() || durationSeconds <= 0) return 0.0
        return minOf(durationSeconds, maxOf(0.0, elapsedSeconds + movingBySeconds))
    }

    fun timeText(seconds: Double): String {
        val wholeSeconds = if (seconds.isFinite()) maxOf(0, Math.floor(seconds).toInt()) else 0
        val hours = wholeSeconds / 3600
        val minutes = (wholeSeconds % 3600) / 60
        val remainingSeconds = wholeSeconds % 60
        return if (hours > 0) "%d:%02d:%02d".format(hours, minutes, remainingSeconds) else "%d:%02d".format(minutes, remainingSeconds)
    }
}
