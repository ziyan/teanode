package com.teanode.kit.location

import com.teanode.kit.TeaNodeError
import kotlinx.serialization.Contextual
import kotlinx.serialization.Serializable
import java.time.Duration
import java.time.Instant
import java.time.ZoneId
import java.time.ZonedDateTime

// Pausing where the phone records. A pause is the person's, kept by the server so that every device and every person who
// looks reads the same thing; the phone learns it from the server, applies a pause it makes itself at once, and tells
// the server until the server has it. Mirrors ios/TeaNodeKit LocationPause.swift.

/** LocationPause is a stretch of time in which the person stopped TeaNode recording where they go. */
@Serializable
data class LocationPause(
    val id: String,
    /** RFC 3339. */
    val startedAt: String,
    /** RFC 3339; null until the person resumes. */
    val endedAt: String? = null,
) {
    val startedInstant: Instant? get() = ServerTime.instant(startedAt)
    val endedInstant: Instant? get() = ServerTime.instant(endedAt)

    /** Whether the pause goes on until the person resumes. */
    val isOpenEnded: Boolean get() = endedAt == null

    /** Whether it is in effect at an instant: it has started, and has not ended. The same rule the server applies to what the phone reports. */
    fun isInEffect(at: Instant): Boolean {
        val started = startedInstant ?: return false
        if (started.isAfter(at)) return false
        if (isOpenEnded) return true
        val ended = endedInstant ?: return false
        return at.isBefore(ended)
    }

    /** The same pause ending at another time, or never. */
    fun ending(at: Instant?): LocationPause = LocationPause(id, startedAt, at?.let { ServerTime.text(it) })

    companion object {
        fun of(id: String, startedInstant: Instant, endedInstant: Instant?): LocationPause =
            LocationPause(id, ServerTime.text(startedInstant), endedInstant?.let { ServerTime.text(it) })
    }
}

/** LocationPauseChoice is what the person chooses from when they pause. */
enum class LocationPauseChoice {
    AnHour,
    UntilTomorrowMorning,
    UntilResumed;

    /**
     * When the pause ends if it begins at [now]; null for one that goes on until the person resumes. The server takes
     * only an end time, so the morning is worked out here, in the zone given: 08:00 the next day, or 08:00 the same
     * day before 05:00.
     */
    fun endInstant(now: Instant, zone: ZoneId = ZoneId.systemDefault()): Instant? = when (this) {
        AnHour -> now.plusSeconds(ANHOUR_SECONDS.toLong())
        UntilTomorrowMorning -> {
            val fallback = now.plusSeconds(24L * 3600)
            val zoned = ZonedDateTime.ofInstant(now, zone)
            val dayOfMorning = if (zoned.hour < EARLIEST_TOMORROW_HOUR) 0L else 1L
            runCatching { zoned.plusDays(dayOfMorning).withHour(MORNING_HOUR).withMinute(0).withSecond(0).withNano(0).toInstant() }.getOrDefault(fallback)
        }
        UntilResumed -> null
    }

    companion object {
        /** How long "for an hour" is. */
        const val ANHOUR_SECONDS = 3600.0

        /** The hour of the morning "until tomorrow morning" is, in the phone's own zone. */
        const val MORNING_HOUR = 8

        /** Before this hour, "until tomorrow morning" means this morning: a person pausing at half past midnight is not asking for a day and a half. */
        const val EARLIEST_TOMORROW_HOUR = 5
    }
}

/** A change made on the phone, waiting for the server. */
@Serializable
sealed class LocationPauseChange {
    /** Pause from this instant, or change the end of the pause in effect; a null end lasts until the person resumes. */
    @Serializable
    data class Pause(
        @Serializable(with = InstantAsEpochMillisSerializer::class) val startedAt: Instant,
        @Serializable(with = InstantAsEpochMillisSerializer::class) val endedAt: Instant?,
    ) : LocationPauseChange()

    /** End the pause in effect at this instant. */
    @Serializable
    data class Resume(@Serializable(with = InstantAsEpochMillisSerializer::class) val at: Instant) : LocationPauseChange()
}

/**
 * LocationPauseTracker is what the phone believes about the person's pause: the one the server last said was in
 * effect, and a change the person made on the phone that the server has not taken yet. Pausing and resuming apply here
 * at once, so the phone stops recording before the call is answered, and the change is sent until the server has it.
 * It is kept in a file, so a closed app that is woken for a visit still knows.
 */
@Serializable
data class LocationPauseTracker(
    /** The pause in effect, as the server last said, or null if it said there was none. */
    val confirmedPause: LocationPause? = null,
    val pendingChange: LocationPauseChange? = null,
) {
    /**
     * The pause as the person sees it: the server's, with the change made on the phone applied. Not necessarily in
     * effect now: a pause that has ended is kept so a position measured inside it, noticed late, is still recognized.
     */
    val pause: LocationPause?
        get() = when (val change = pendingChange) {
            null -> confirmedPause
            is LocationPauseChange.Pause -> {
                if (confirmedPause != null && confirmedPause.isInEffect(change.startedAt)) confirmedPause.ending(change.endedAt)
                else LocationPause.of(UNCONFIRMED_PAUSE_ID, change.startedAt, change.endedAt)
            }
            is LocationPauseChange.Resume ->
                if (confirmedPause == null || !confirmedPause.isInEffect(change.at)) confirmedPause else confirmedPause.ending(change.at)
        }

    /** Whether a change waits for the server. */
    val hasPendingChange: Boolean get() = pendingChange != null

    /** Whether recording is paused at an instant. */
    fun isPaused(at: Instant): Boolean = pause?.isInEffect(at) ?: false

    /** When the pause in effect ends by itself, if it does. */
    fun endInstant(at: Instant): Instant? {
        val current = pause
        if (current == null || !current.isInEffect(at)) return null
        return current.endedInstant
    }

    /** Whether to record a position measured at an instant: not if it falls in the pause. */
    fun isRecordable(pointMeasuredAt: Instant): Boolean = !isPaused(pointMeasuredAt)

    /** Whether to record a visit: not if it began (or, when the beginning was not seen, ended) in the pause. */
    fun isRecordable(visitArrivedAt: Instant?, visitDepartedAt: Instant?): Boolean {
        val anchor = visitArrivedAt ?: visitDepartedAt ?: return true
        return !isPaused(anchor)
    }

    /** When a departure noticed at an instant is told: where the pause began if the instant is in it, else the instant itself. */
    fun departureInstant(noticedAt: Instant): Instant {
        val current = pause
        if (current == null || !current.isInEffect(noticedAt)) return noticedAt
        return current.startedInstant ?: noticedAt
    }

    // Changing

    /** The person paused on the phone: for the length chosen, or until they resume ([endedAt] null). */
    fun startingPause(until: Instant?, now: Instant): LocationPauseTracker = copy(pendingChange = LocationPauseChange.Pause(now, until))

    /**
     * The person resumed on the phone. A pause the server has not answered for may already be on its way there, so the
     * resume waits to be sent after it; nothing is sent when no pause was ever sent.
     */
    fun endingPause(now: Instant): LocationPauseTracker {
        if (!isPaused(now)) return this
        if (confirmedPause != null && confirmedPause.isInEffect(now)) return copy(pendingChange = LocationPauseChange.Resume(now))
        if (pendingChange is LocationPauseChange.Pause) return copy(pendingChange = LocationPauseChange.Resume(now))
        return copy(pendingChange = null)
    }

    /** The server said what is in effect, from `GetLocation` or the answer to a report. A change made on the phone and not yet taken still applies over it. */
    fun learning(serverPause: LocationPause?, now: Instant): LocationPauseTracker = copy(confirmedPause = serverPause).settled(now)

    /** The server took the change. [answer] is the pause it now has in effect, null after a resume. */
    fun changeWasTaken(change: LocationPauseChange, answer: LocationPause?, now: Instant): LocationPauseTracker {
        var next = copy(confirmedPause = answer)
        val pending = pendingChange
        if (pending == change) {
            next = next.copy(pendingChange = null)
        } else if (change is LocationPauseChange.Pause && pending is LocationPauseChange.Resume) {
            val startedInstant = answer?.startedInstant
            // Resumed while the pause was on its way: the server began it a moment after the tap, and the resume is for that pause.
            if (startedInstant != null && pending.at.isBefore(startedInstant)) next = next.copy(pendingChange = LocationPauseChange.Resume(startedInstant))
        }
        return next.settled(now)
    }

    /** The server will not take the change, and asking again cannot help. */
    fun changeWasRefused(change: LocationPauseChange): LocationPauseTracker = if (pendingChange == change) copy(pendingChange = null) else this

    /**
     * Drops a change that no longer means anything: a pause whose end has passed, a resume of a pause the server says is
     * over. A resume while the server has no pause at all is kept: the pause it ends may still be on its way.
     */
    fun settled(now: Instant): LocationPauseTracker = when (val change = pendingChange) {
        is LocationPauseChange.Pause -> if (change.endedAt != null && !change.endedAt.isAfter(now)) copy(pendingChange = null) else this
        is LocationPauseChange.Resume ->
            if (confirmedPause != null && !confirmedPause.isInEffect(change.at)) copy(pendingChange = null) else this
        null -> this
    }

    companion object {
        /** The identifier of a pause the server does not know yet. */
        const val UNCONFIRMED_PAUSE_ID = "unconfirmed"

        /** The key an account's tracker is kept under: one for each account, so a pause made as one person is never read as another's. */
        fun storageKey(accountKey: String): String = "locationPauseTracker.$accountKey"
    }
}

/** LocationPauseFailure is what the phone does about a pause or a resume the server did not take. */
sealed class LocationPauseFailure {
    /** The call did not get through, or the sign-in needs renewing: keep the change and send it again. */
    data object RetryLater : LocationPauseFailure()

    /** The server refused it (not allowed to use Location, or an end time it does not take): asking again cannot change the answer. */
    data class Refused(val reason: String) : LocationPauseFailure()

    companion object {
        fun classify(error: Throwable): LocationPauseFailure {
            val refusal = error as? TeaNodeError.Refused ?: return RetryLater
            if (refusal.reason.contains("permission denied") || refusal.reason.contains("invalid arguments")) return Refused(refusal.reason)
            return RetryLater
        }
    }
}

/** LocationPauseRetry is how long the phone waits before it sends a change of pause again: it doubles after each failure, up to a quarter of an hour. */
object LocationPauseRetry {
    const val FIRST_SECONDS = 15.0
    const val MOST_SECONDS = 900.0

    /** The wait after this many failures in a row (the first is 1). */
    fun delaySeconds(afterFailureCount: Int): Double {
        if (afterFailureCount <= 1) return FIRST_SECONDS
        return minOf(FIRST_SECONDS * Math.pow(2.0, minOf(afterFailureCount - 1, 10).toDouble()), MOST_SECONDS)
    }
}

/** LocationTimelineEntry is one row of a timeline: a visit, a pause, which shows as the gap it is, or a trip between two visits. */
sealed class LocationTimelineEntry {
    data class Visit(val visit: PlaceVisit) : LocationTimelineEntry()
    data class Pause(val pause: LocationPause) : LocationTimelineEntry()
    data class TripEntry(val trip: Trip) : LocationTimelineEntry()

    val id: String
        get() = when (this) {
            is Visit -> "visit:${visit.id}"
            is Pause -> "pause:${pause.id}"
            is TripEntry -> "trip:${trip.id}"
        }

    /** Where it is placed on a day: a visit's arrival (else its departure), a pause's start, a trip's start. */
    val anchorInstant: Instant?
        get() = when (this) {
            is Visit -> visit.anchorInstant
            is Pause -> pause.startedInstant
            is TripEntry -> trip.startedInstant
        }

    val isVisit: Boolean get() = this is Visit

    companion object {
        /**
         * Whether the first entry goes above the second in a newest-first timeline. At the same moment a trip or a
         * pause goes above a visit: a visit known only by its departure is placed at that departure, which is when the
         * trip leaving it starts.
         */
        fun isNewer(first: LocationTimelineEntry, second: LocationTimelineEntry): Boolean {
            val firstInstant = first.anchorInstant ?: Instant.MIN
            val secondInstant = second.anchorInstant ?: Instant.MIN
            if (firstInstant != secondInstant) return firstInstant.isAfter(secondInstant)
            return !first.isVisit && second.isVisit
        }

        /** The comparator that sorts newest first. */
        val newestFirst: Comparator<LocationTimelineEntry> = Comparator { first, second ->
            when {
                isNewer(first, second) -> -1
                isNewer(second, first) -> 1
                else -> 0
            }
        }
    }
}
