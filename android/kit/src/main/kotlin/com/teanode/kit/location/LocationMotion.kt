package com.teanode.kit.location

import java.time.Instant

/**
 * LocationMotion turns what Android's activity transition updates say into the motion activities the server takes, the
 * same [ReportedActivity] the iPhone app builds from Core Motion. Android reports a change (the person entered or left an
 * activity), not stretches with a confidence: entering an activity starts a stretch of that kind, which lasts until the
 * next one starts, so leaving one says nothing by itself. The server's kinds are the iPhone's, so one person's
 * phones add up.
 */
object LocationMotion {
    // The values of com.google.android.gms.location.DetectedActivity, which do not change.
    const val IN_VEHICLE = 0
    const val ON_BICYCLE = 1
    const val ON_FOOT = 2
    const val STILL = 3
    const val WALKING = 7
    const val RUNNING = 8

    /** What a transition update gives no confidence for: the update is only sent once Android is sure of the change. */
    const val TRANSITION_CONFIDENCE = "medium"

    /** The kind the server knows an Android activity by; null for one the server has no word for (tilting, unknown). */
    fun activityKind(androidActivityType: Int): String? = when (androidActivityType) {
        IN_VEHICLE -> "automotive"
        ON_BICYCLE -> "cycling"
        ON_FOOT, WALKING -> "walking"
        RUNNING -> "running"
        STILL -> "stationary"
        else -> null
    }

    /** The stretch an entered activity begins, or null when the person left one, or the activity has no word. */
    fun reportedActivity(androidActivityType: Int, isEntering: Boolean, at: Instant): ReportedActivity? {
        if (!isEntering) return null
        val kind = activityKind(androidActivityType) ?: return null
        return ReportedActivity(startedAt = Instant.ofEpochSecond(at.epochSecond), activityKind = kind, activityConfidence = TRANSITION_CONFIDENCE)
    }

    /** The instant a transition happened, from how long ago it was on the phone's clock that counts from boot, so a late delivery is dated right. */
    fun transitionInstant(now: Instant, nowElapsedNanos: Long, eventElapsedNanos: Long): Instant =
        now.minusNanos(maxOf(0L, nowElapsedNanos - eventElapsedNanos))
}
