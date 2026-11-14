package com.teanode.kit.location

import kotlinx.serialization.Serializable
import java.time.Instant

/**
 * LocationDwellDetector notices that the person stayed somewhere. The iPhone app gets visits from the system's visit
 * monitor; Android has none, so the recorder works them out from the positions it keeps: a stay is a stretch of at least
 * [minimumSeconds] inside [stayRadiusMeters] of where it began. It is reported as an arrival when it has lasted long
 * enough, and as a departure, dated to the last position inside, when the person moves beyond [leaveRadiusMeters]. A
 * position between the two radii changes nothing, so a person standing at the edge does not leave and arrive again.
 */
class LocationDwellDetector(
    private val stayRadiusMeters: Double = STAY_RADIUS_METERS,
    private val leaveRadiusMeters: Double = LEAVE_RADIUS_METERS,
    private val minimumSeconds: Long = MINIMUM_SECONDS,
) {
    /** Where the person has been staying: kept in a file between positions, so a stay survives the process. */
    @Serializable
    data class Stay(
        val latitudeDegrees: Double,
        val longitudeDegrees: Double,
        @Serializable(with = InstantAsEpochMillisSerializer::class) val since: Instant,
        @Serializable(with = InstantAsEpochMillisSerializer::class) val lastInside: Instant,
        /** Whether the arrival was reported, so the departure follows it. */
        val isArrivalReported: Boolean = false,
    )

    /** What a position tells. */
    sealed class Event {
        data class Arrived(val latitudeDegrees: Double, val longitudeDegrees: Double, val arrivedAt: Instant) : Event()
        data class Departed(val latitudeDegrees: Double, val longitudeDegrees: Double, val arrivedAt: Instant, val departedAt: Instant) : Event()
    }

    /** The new stay and what happened, for a position measured at an instant. A reading too rough to place the person is ignored. */
    fun consume(stay: Stay?, latitudeDegrees: Double, longitudeDegrees: Double, accuracyMeters: Double, at: Instant): Pair<Stay?, List<Event>> {
        if (!LocationGeometry.isOnEarth(latitudeDegrees, longitudeDegrees) || !accuracyMeters.isFinite() || accuracyMeters > ROUGHEST_ACCURACY_METERS) return stay to emptyList()
        if (stay == null) return Stay(latitudeDegrees, longitudeDegrees, at, at) to emptyList()
        if (at.isBefore(stay.lastInside)) return stay to emptyList()
        val distance = LocationGeometry.distanceMeters(stay.latitudeDegrees, stay.longitudeDegrees, latitudeDegrees, longitudeDegrees)
        if (distance <= stayRadiusMeters) {
            val updated = stay.copy(lastInside = at)
            if (!stay.isArrivalReported && at.epochSecond - stay.since.epochSecond >= minimumSeconds) {
                return updated.copy(isArrivalReported = true) to listOf(Event.Arrived(stay.latitudeDegrees, stay.longitudeDegrees, stay.since))
            }
            return updated to emptyList()
        }
        if (distance <= leaveRadiusMeters) return stay to emptyList()
        val next = Stay(latitudeDegrees, longitudeDegrees, at, at)
        if (!stay.isArrivalReported) return next to emptyList()
        return next to listOf(Event.Departed(stay.latitudeDegrees, stay.longitudeDegrees, stay.since, stay.lastInside))
    }

    /** What the outbox is told of an event: a visit keyed by where and when it began, so the departure finds its arrival. */
    fun visitOf(event: Event): ReportedVisit = when (event) {
        is Event.Arrived -> ReportedVisit(
            clientVisitKey = LocationVisitKey.visit(event.latitudeDegrees, event.longitudeDegrees, event.arrivedAt, null), latitudeDegrees = event.latitudeDegrees,
            longitudeDegrees = event.longitudeDegrees, accuracyMeters = VISIT_ACCURACY_METERS, arrivedAt = event.arrivedAt, departedAt = null, visitSource = "visit",
        )
        is Event.Departed -> ReportedVisit(
            clientVisitKey = LocationVisitKey.visit(event.latitudeDegrees, event.longitudeDegrees, event.arrivedAt, event.departedAt), latitudeDegrees = event.latitudeDegrees,
            longitudeDegrees = event.longitudeDegrees, accuracyMeters = VISIT_ACCURACY_METERS, arrivedAt = event.arrivedAt, departedAt = event.departedAt, visitSource = "visit",
        )
    }

    companion object {
        const val STAY_RADIUS_METERS = 100.0
        const val LEAVE_RADIUS_METERS = 150.0

        /** How long a person has to stay for it to be a visit. */
        const val MINIMUM_SECONDS = 8L * 60

        /** A position less precise than this says too little about where the person is. */
        const val ROUGHEST_ACCURACY_METERS = 200.0
        const val VISIT_ACCURACY_METERS = 30.0
    }
}
