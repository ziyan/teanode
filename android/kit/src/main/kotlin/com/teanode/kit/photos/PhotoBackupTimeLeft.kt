package com.teanode.kit.photos

import kotlin.math.exp
import kotlin.math.roundToInt
import kotlin.math.roundToLong

/**
 * How long the backup of a photo library has left, worked out from how fast photos have been going up lately. A pure
 * value, fed what the status reads, so it runs and is tested without a phone.
 */
class PhotoBackupTimeLeft {
    private class Sample(val epochSeconds: Double, val backedUpItemCount: Int)

    private var samples = mutableListOf<Sample>()
    private var smoothedItemsPerSecond: Double? = null
    private var smoothedAtSeconds: Double? = null

    /** Takes a reading. A reading while nothing uploads forgets the past: the run is over. */
    fun record(epochSeconds: Double, backedUpItemCount: Int, isUploading: Boolean) {
        if (!isUploading) {
            forgetEverything()
            return
        }
        // Fewer items backed up than before means the library was counted again, and an earlier time means the clock
        // moved back: the samples no longer describe the same run.
        samples.lastOrNull()?.let { newest ->
            if (backedUpItemCount < newest.backedUpItemCount || epochSeconds < newest.epochSeconds) forgetEverything()
        }
        val windowStart = epochSeconds - WINDOW_SECONDS
        val firstInWindow = samples.indexOfFirst { sample -> sample.epochSeconds >= windowStart }
        samples = if (firstInWindow >= 0) samples.subList(firstInWindow, samples.size).toMutableList() else mutableListOf()
        // While nothing finishes the status is read again and again with the same count: keep the first and the latest
        // of such a run, not each.
        if (samples.size >= 2 && samples[samples.size - 1].backedUpItemCount == backedUpItemCount && samples[samples.size - 2].backedUpItemCount == backedUpItemCount) {
            samples.removeAt(samples.size - 1)
        }
        samples.add(Sample(epochSeconds, backedUpItemCount))
        if (samples.size > MAXIMUM_SAMPLE_COUNT) samples = samples.subList(samples.size - MAXIMUM_SAMPLE_COUNT, samples.size).toMutableList()
        updateSmoothedRate(epochSeconds)
    }

    /** How many seconds are left for this many items, or null while the rate is not known. Never under a minute. */
    fun estimatedSecondsLeft(itemsLeftCount: Int): Int? {
        val rate = smoothedItemsPerSecond
        if (itemsLeftCount <= 0 || rate == null || rate <= 0) return null
        val seconds = (itemsLeftCount / rate).roundToLong()
        if (seconds >= Int.MAX_VALUE / 2) return null
        return maxOf(MINIMUM_ESTIMATE_SECONDS, seconds.toInt())
    }

    private fun updateSmoothedRate(nowSeconds: Double) {
        val oldest = samples.firstOrNull() ?: return
        val newest = samples.lastOrNull() ?: return
        val spanSeconds = newest.epochSeconds - oldest.epochSeconds
        val finishedItemCount = newest.backedUpItemCount - oldest.backedUpItemCount
        if (spanSeconds < MINIMUM_SPAN_SECONDS || finishedItemCount < MINIMUM_FINISHED_ITEM_COUNT) {
            smoothedItemsPerSecond = null
            smoothedAtSeconds = null
            return
        }
        val windowedItemsPerSecond = finishedItemCount / spanSeconds
        val previousRate = smoothedItemsPerSecond
        val previousAt = smoothedAtSeconds
        smoothedItemsPerSecond = if (previousRate != null && previousAt != null) {
            val elapsedSeconds = maxOf(0.0, nowSeconds - previousAt)
            val weight = 1 - exp(-elapsedSeconds / SMOOTHING_SECONDS)
            previousRate + weight * (windowedItemsPerSecond - previousRate)
        } else {
            windowedItemsPerSecond
        }
        smoothedAtSeconds = nowSeconds
    }

    private fun forgetEverything() {
        samples = mutableListOf()
        smoothedItemsPerSecond = null
        smoothedAtSeconds = null
    }

    companion object {
        const val WINDOW_SECONDS = 15.0 * 60
        const val MINIMUM_SPAN_SECONDS = 2.0 * 60
        const val MINIMUM_FINISHED_ITEM_COUNT = 5
        const val SMOOTHING_SECONDS = 2.0 * 60
        const val MINIMUM_ESTIMATE_SECONDS = 60
        private const val MAXIMUM_SAMPLE_COUNT = 2_000
    }
}

enum class PhotoBackupSpokenDurationUnit { Minutes, Hours, Days }

/** A time left said the way people do: "about 5 minutes", "about 3 hours", "about 4 days". */
data class PhotoBackupSpokenDuration(val count: Int, val unit: PhotoBackupSpokenDurationUnit) {
    companion object {
        fun ofSeconds(seconds: Int): PhotoBackupSpokenDuration {
            val minuteCount = maxOf(0, seconds) / 60.0
            val roundedMinuteCount = minuteCount.roundToInt().toDouble()
            val spokenMinuteCount = if (roundedMinuteCount < 10) roundedMinuteCount else (roundedMinuteCount / 5).roundToInt() * 5.0
            if (spokenMinuteCount < 60) return PhotoBackupSpokenDuration(maxOf(1, spokenMinuteCount.toInt()), PhotoBackupSpokenDurationUnit.Minutes)
            val hourCount = (seconds / 3600.0).roundToInt()
            if (hourCount < 48) return PhotoBackupSpokenDuration(maxOf(1, hourCount), PhotoBackupSpokenDurationUnit.Hours)
            return PhotoBackupSpokenDuration((hourCount / 24.0).roundToInt(), PhotoBackupSpokenDurationUnit.Days)
        }
    }
}
