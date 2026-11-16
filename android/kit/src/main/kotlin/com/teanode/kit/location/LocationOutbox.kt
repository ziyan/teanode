package com.teanode.kit.location

import com.teanode.kit.TeaNodeError
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.isNotFoundRefusal
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import java.net.URLEncoder
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import kotlin.math.asin
import kotlin.math.cos
import kotlin.math.min
import kotlin.math.sin
import kotlin.math.sqrt

// What the phone does with where it has been, before and after the server is told: which positions are worth keeping,
// how a visit is named so that its arrival and its departure are one, what waits to be sent, and which places are
// watched for the person arriving. None of it needs the location services, so it is tested on the JVM; the recorder in
// the app feeds it. Mirrors ios/TeaNodeKit LocationOutbox.swift.

/** LocationGeometry is the arithmetic on coordinates the recorder needs. */
object LocationGeometry {
    /** Whether the coordinates are on the earth; the server refuses any that are not, and one bad point must not hold up a batch. */
    fun isOnEarth(latitudeDegrees: Double, longitudeDegrees: Double): Boolean =
        latitudeDegrees.isFinite() && longitudeDegrees.isFinite() && latitudeDegrees in -90.0..90.0 && longitudeDegrees in -180.0..180.0

    /** The distance along the earth's surface between two coordinates, in meters (the haversine formula). */
    fun distanceMeters(fromLatitudeDegrees: Double, fromLongitudeDegrees: Double, toLatitudeDegrees: Double, toLongitudeDegrees: Double): Double {
        val earthRadiusMeters = 6_371_000.0
        val fromLatitudeRadians = Math.toRadians(fromLatitudeDegrees)
        val toLatitudeRadians = Math.toRadians(toLatitudeDegrees)
        val latitudeChange = Math.toRadians(toLatitudeDegrees - fromLatitudeDegrees)
        val longitudeChange = Math.toRadians(toLongitudeDegrees - fromLongitudeDegrees)
        val half = sin(latitudeChange / 2) * sin(latitudeChange / 2) +
            cos(fromLatitudeRadians) * cos(toLatitudeRadians) * sin(longitudeChange / 2) * sin(longitudeChange / 2)
        return 2 * earthRadiusMeters * asin(min(1.0, sqrt(half)))
    }
}

/** LocationWords are the plain facts about OpenStreetMap's tags that the screens put into the person's language. */
object LocationWords {
    /** The part of an OpenStreetMap tag after the equals sign, as the kind of thing: "amenity=cafe" is "cafe". */
    fun categoryValue(osmCategory: String): String = if (osmCategory.contains('=')) osmCategory.substringAfter('=') else osmCategory
}

/** LocationVisitKey names a visit the way the phone does, so that a report sent twice, or an arrival followed by its departure, is one visit. */
object LocationVisitKey {
    /**
     * A visit the phone reported. The same visit comes back with its departure, carrying the same arrival, so the key is
     * made from where and when it began. A visit whose arrival was not seen is named from when it ended.
     */
    fun visit(latitudeDegrees: Double, longitudeDegrees: Double, arrivedAt: Instant?, departedAt: Instant?, now: Instant = Instant.now()): String {
        val place = String.format(java.util.Locale.ROOT, "%.3f:%.3f", latitudeDegrees, longitudeDegrees)
        if (arrivedAt != null) return "visit:$place:a${arrivedAt.epochSecond}"
        return "visit:$place:d${(departedAt ?: now).epochSecond}"
    }

    /** A visit to a watched place, begun when they came in, named by what the monitor knows the place by ([WatchedPlace.identifier]). */
    fun region(identifier: String, arrivedAt: Instant): String = "region:$identifier:a${arrivedAt.epochSecond}"
}

/**
 * LocationPointFilter decides which live positions are kept: the phone reports where the person has been, not every
 * reading, so a point is kept when they have moved at least 25 meters from the last one kept, or a minute has passed and
 * they are moving. Nothing is kept while the phone says it is stationary.
 */
class LocationPointFilter {
    private data class Kept(val latitudeDegrees: Double, val longitudeDegrees: Double, val measuredAt: Instant)

    private var lastKept: Kept? = null

    /** Forgets the last point kept, so the next reading is kept: when the app comes back to the foreground after a while. */
    fun reset() {
        lastKept = null
    }

    /** Whether to keep this reading, remembering it if so. */
    fun shouldKeep(latitudeDegrees: Double, longitudeDegrees: Double, speedMetersPerSecond: Double?, measuredAt: Instant, isStationary: Boolean): Boolean {
        if (isStationary) return false
        val previous = lastKept
        if (previous != null) {
            val movedMeters = LocationGeometry.distanceMeters(previous.latitudeDegrees, previous.longitudeDegrees, latitudeDegrees, longitudeDegrees)
            val waitedSeconds = (measuredAt.toEpochMilli() - previous.measuredAt.toEpochMilli()) / 1000.0
            val isMoving = (speedMetersPerSecond ?: 0.0) > SPEED_METERS_PER_SECOND_LEAST
            if (!(movedMeters >= DISTANCE_METERS_LEAST || (waitedSeconds >= SECONDS_LEAST && isMoving))) return false
        }
        lastKept = Kept(latitudeDegrees, longitudeDegrees, measuredAt)
        return true
    }

    companion object {
        const val DISTANCE_METERS_LEAST = 25.0
        const val SECONDS_LEAST = 60.0
        const val SPEED_METERS_PER_SECOND_LEAST = 0.5
    }
}

/** ReportedPoint is one position waiting to be reported. */
@Serializable
data class ReportedPoint(
    /** Its place in the outbox, which tells what a batch took with it. */
    val sequence: Int = 0,
    val latitudeDegrees: Double,
    val longitudeDegrees: Double,
    val accuracyMeters: Double,
    val altitudeMeters: Double? = null,
    val speedMetersPerSecond: Double? = null,
    val courseDegrees: Double? = null,
    @Serializable(with = InstantAsEpochMillisSerializer::class) val measuredAt: Instant,
) {
    val variables: JsonElement
        get() = JsonObject(
            buildMap {
                put("latitudeDegrees", JsonPrimitive(latitudeDegrees))
                put("longitudeDegrees", JsonPrimitive(longitudeDegrees))
                put("accuracyMeters", JsonPrimitive(accuracyMeters))
                put("measuredAt", JsonPrimitive(ServerTime.text(measuredAt)))
                altitudeMeters?.let { put("altitudeMeters", JsonPrimitive(it)) }
                speedMetersPerSecond?.let { put("speedMetersPerSecond", JsonPrimitive(it)) }
                courseDegrees?.let { put("courseDegrees", JsonPrimitive(it)) }
            },
        )
}

/** ReportedVisit is one arrival, departure or both waiting to be reported. */
@Serializable
data class ReportedVisit(
    val sequence: Int = 0,
    val clientVisitKey: String,
    val latitudeDegrees: Double,
    val longitudeDegrees: Double,
    val accuracyMeters: Double,
    @Serializable(with = InstantAsEpochMillisSerializer::class) val arrivedAt: Instant? = null,
    @Serializable(with = InstantAsEpochMillisSerializer::class) val departedAt: Instant? = null,
    /** "visit" from the platform's visit monitoring, "region" from entering a place the person named. */
    val visitSource: String,
    /** For "region": the place that was watched. */
    val placeId: String? = null,
    /** For "region": the store's branch that was watched, when it was one, so that the server finds or makes the person's place for it. */
    val storeBranch: WatchedStoreBranch? = null,
) {
    val variables: JsonElement
        get() = JsonObject(
            buildMap {
                put("clientVisitKey", JsonPrimitive(clientVisitKey))
                put("latitudeDegrees", JsonPrimitive(latitudeDegrees))
                put("longitudeDegrees", JsonPrimitive(longitudeDegrees))
                put("accuracyMeters", JsonPrimitive(accuracyMeters))
                put("visitSource", JsonPrimitive(visitSource))
                arrivedAt?.let { put("arrivedAt", JsonPrimitive(ServerTime.text(it))) }
                departedAt?.let { put("departedAt", JsonPrimitive(ServerTime.text(it))) }
                placeId?.let { put("placeId", JsonPrimitive(it)) }
                storeBranch?.let { branch ->
                    put("osmType", JsonPrimitive(branch.osmType))
                    put("osmId", JsonPrimitive(branch.osmId))
                    put("osmName", JsonPrimitive(branch.osmName))
                    put("osmCategory", JsonPrimitive(branch.osmCategory))
                }
            },
        )
}

/** ReportedActivity is one stretch of motion activity waiting to be reported. It lasts until the next one starts. */
@Serializable
data class ReportedActivity(
    val sequence: Int = 0,
    @Serializable(with = InstantAsEpochMillisSerializer::class) val startedAt: Instant,
    /** "stationary", "walking", "running", "cycling", "automotive" or "unknown". */
    val activityKind: String,
    /** "low", "medium" or "high". */
    val activityConfidence: String,
) {
    val variables: JsonElement
        get() = JsonObject(
            mapOf(
                "startedAt" to JsonPrimitive(ServerTime.text(startedAt)), "activityKind" to JsonPrimitive(activityKind),
                "activityConfidence" to JsonPrimitive(activityConfidence),
            ),
        )

    companion object {
        /** The kinds the server takes. */
        val activityKinds: Set<String> = setOf("stationary", "walking", "running", "cycling", "automotive", "unknown")

        /** The confidences the server takes. */
        val activityConfidences: Set<String> = setOf("low", "medium", "high")
    }
}

/** LocationBatch is what one ReportLocation call carries: the oldest points, visits and motion activities in the outbox, at most 500, 50 and 500. */
data class LocationBatch(
    val points: List<ReportedPoint>,
    val visits: List<ReportedVisit>,
    val activities: List<ReportedActivity> = emptyList(),
) {
    val isEmpty: Boolean get() = points.isEmpty() && visits.isEmpty() && activities.isEmpty()

    /** How many positions, visits and activities it carries together. */
    val entryCount: Int get() = points.size + visits.size + activities.size

    /**
     * The two smaller batches to send when the server calls this one invalid without saying which entry is wrong: the
     * first kind of entry it holds apart from the rest if it holds more than one kind, else the first half and the rest
     * of whichever it holds. Null for a single entry, which is the one the server refuses.
     */
    fun split(): Pair<LocationBatch, LocationBatch>? {
        if (entryCount <= 1) return null
        val kindCount = listOf(points.isEmpty(), visits.isEmpty(), activities.isEmpty()).count { isKindEmpty -> !isKindEmpty }
        if (kindCount > 1) {
            if (points.isNotEmpty()) return LocationBatch(points, emptyList()) to LocationBatch(emptyList(), visits, activities)
            return LocationBatch(emptyList(), visits) to LocationBatch(emptyList(), emptyList(), activities)
        }
        if (points.isNotEmpty()) {
            val middle = points.size / 2
            return LocationBatch(points.subList(0, middle), emptyList()) to LocationBatch(points.subList(middle, points.size), emptyList())
        }
        if (visits.isNotEmpty()) {
            val middle = visits.size / 2
            return LocationBatch(emptyList(), visits.subList(0, middle)) to LocationBatch(emptyList(), visits.subList(middle, visits.size))
        }
        val middle = activities.size / 2
        return LocationBatch(emptyList(), emptyList(), activities.subList(0, middle)) to
            LocationBatch(emptyList(), emptyList(), activities.subList(middle, activities.size))
    }

    /** The call's variables; "activities" only when there are some, since a server without the argument refuses a call that names it. */
    val variables: Map<String, JsonElement>
        get() = buildMap {
            put("points", JsonArray(points.map { point -> point.variables }))
            put("visits", JsonArray(visits.map { visit -> visit.variables }))
            if (activities.isNotEmpty()) put("activities", JsonArray(activities.map { activity -> activity.variables }))
        }
}

/** OpenRegionVisit is a visit to a watched place that has begun and not ended, kept so a departure after the app was closed finds its arrival. */
@Serializable
data class OpenRegionVisit(
    val clientVisitKey: String,
    val latitudeDegrees: Double,
    val longitudeDegrees: Double,
    @Serializable(with = InstantAsEpochMillisSerializer::class) val arrivedAt: Instant,
    /** The place it is at, and the store's branch when it was one. */
    val placeId: String? = null,
    val storeBranch: WatchedStoreBranch? = null,
)

/**
 * LocationOutbox is everything the phone has noticed that the server has not yet been told. It is kept in a file, so a
 * closed app, a lost connection or a refused sign-in loses nothing; a batch leaves it only after the server answered.
 */
@Serializable
class LocationOutbox(
    /** Whose it is. What was noticed while another account was in use is not reported as the person now signed in. */
    var accountKey: String? = null,
) {
    private var nextSequence = 1

    var points: List<ReportedPoint> = emptyList()
        private set
    var visits: List<ReportedVisit> = emptyList()
        private set

    /** The motion activities, oldest first, one for each start time. */
    var activities: List<ReportedActivity> = emptyList()
        private set

    /** The visits begun and not ended, by what the monitor knows their place by ([WatchedPlace.identifier]). */
    var openRegionVisits: Map<String, OpenRegionVisit> = emptyMap()
        private set

    val isEmpty: Boolean get() = points.isEmpty() && visits.isEmpty() && activities.isEmpty()

    /** Whether a time is one the server takes: not more than a day ahead. */
    private fun isNotTooFarAhead(instant: Instant?, now: Instant): Boolean =
        instant == null || (instant.toEpochMilli() - now.toEpochMilli()) / 1000.0 <= FUTURE_SECONDS_MOST

    /** Whether the server would take the position. It refuses a whole batch for one that is not, so such a position is never queued. */
    fun isAcceptable(point: ReportedPoint, now: Instant = Instant.now()): Boolean =
        LocationGeometry.isOnEarth(point.latitudeDegrees, point.longitudeDegrees) && point.accuracyMeters.isFinite() &&
            point.accuracyMeters >= 0 && isNotTooFarAhead(point.measuredAt, now)

    /** Whether the server would take the visit, by the same checks it makes. */
    fun isAcceptable(visit: ReportedVisit, now: Instant = Instant.now()): Boolean {
        val key = visit.clientVisitKey.trim()
        if (key.isEmpty() || key.toByteArray().size > CLIENT_VISIT_KEY_LENGTH_MOST || !VISIT_SOURCES.contains(visit.visitSource) ||
            !LocationGeometry.isOnEarth(visit.latitudeDegrees, visit.longitudeDegrees) || !visit.accuracyMeters.isFinite() ||
            visit.accuracyMeters < 0 || (visit.arrivedAt == null && visit.departedAt == null) ||
            !isNotTooFarAhead(visit.arrivedAt, now) || !isNotTooFarAhead(visit.departedAt, now)
        ) return false
        if (visit.arrivedAt != null && visit.departedAt != null && visit.departedAt.isBefore(visit.arrivedAt)) return false
        return true
    }

    /** Whether the server would take the activity: a kind and a confidence it knows, and a time not more than a day ahead. */
    fun isAcceptable(activity: ReportedActivity, now: Instant = Instant.now()): Boolean =
        ReportedActivity.activityKinds.contains(activity.activityKind) && ReportedActivity.activityConfidences.contains(activity.activityConfidence) &&
            isNotTooFarAhead(activity.startedAt, now)

    private fun isTooOld(instant: Instant?, now: Instant): Boolean =
        instant != null && (now.toEpochMilli() - instant.toEpochMilli()) / 1000.0 > PAST_SECONDS_MOST

    /**
     * Takes out what has waited so long that the server would now refuse it (more than 400 days back), so that a phone
     * offline that long does not send batches the server refuses whole.
     */
    fun removeTooOld(now: Instant = Instant.now()) {
        points = points.filterNot { point -> isTooOld(point.measuredAt, now) }
        visits = visits.filterNot { visit -> isTooOld(visit.arrivedAt, now) || isTooOld(visit.departedAt, now) }
        activities = activities.filterNot { activity -> isTooOld(activity.startedAt, now) }
    }

    /** Queues a position, unless the server would refuse it: it would refuse the whole batch for it. */
    fun addPoint(point: ReportedPoint, now: Instant = Instant.now()) {
        if (!isAcceptable(point, now)) return
        val queued = point.copy(sequence = nextSequence)
        nextSequence += 1
        points = (points + queued).let { all -> if (all.size > POINT_COUNT_MOST) all.drop(all.size - POINT_COUNT_MOST) else all }
    }

    /** Queues an arrival, a departure or a whole visit, unless the server would refuse it. */
    fun addVisit(visit: ReportedVisit, now: Instant = Instant.now()) {
        if (!isAcceptable(visit, now)) return
        val queued = visit.copy(sequence = nextSequence)
        nextSequence += 1
        visits = visits + queued
    }

    /**
     * Queues motion activities, oldest first, unless the server would refuse one, or one with the same start is already
     * waiting. Past the most kept, the oldest go.
     */
    fun addActivities(added: List<ReportedActivity>, now: Instant = Instant.now()) {
        val startTimes = activities.map { activity -> activity.startedAt }.toMutableSet()
        val kept = activities.toMutableList()
        for (activity in added.sortedBy { each -> each.startedAt }) {
            // To the whole second, which is how the server and the file keep it.
            val startedAt = Instant.ofEpochSecond(activity.startedAt.epochSecond)
            val queued = ReportedActivity(startedAt = startedAt, activityKind = activity.activityKind, activityConfidence = activity.activityConfidence)
            if (!isAcceptable(queued, now) || !startTimes.add(startedAt)) continue
            kept.add(queued.copy(sequence = nextSequence))
            nextSequence += 1
        }
        activities = if (kept.size > ACTIVITY_COUNT_MOST) kept.drop(kept.size - ACTIVITY_COUNT_MOST) else kept
    }

    /**
     * Notes that the person came into a watched place, and queues the arrival. If they are already counted as there,
     * the departure was not seen, so that visit is ended at this arrival and a new one begins; leaving it open would
     * hide every later arrival.
     */
    fun arrive(place: WatchedPlace, at: Instant) {
        depart(place.identifier, at)
        val key = LocationVisitKey.region(place.identifier, at)
        openRegionVisits = openRegionVisits + (
            place.identifier to OpenRegionVisit(key, place.latitudeDegrees, place.longitudeDegrees, at, place.placeId, place.storeBranch)
            )
        addVisit(
            ReportedVisit(
                clientVisitKey = key, latitudeDegrees = place.latitudeDegrees, longitudeDegrees = place.longitudeDegrees, accuracyMeters = 0.0,
                arrivedAt = at, departedAt = null, visitSource = "region", placeId = place.placeId, storeBranch = place.storeBranch,
            ),
        )
    }

    /** Notes that the person went out of a watched place, and queues the departure under the key of the arrival. Does nothing if no arrival was seen. */
    fun depart(identifier: String, at: Instant): Boolean {
        val open = openRegionVisits[identifier] ?: return false
        openRegionVisits = openRegionVisits - identifier
        // Kept before stores were watched, the key is the place.
        val placeId = open.placeId ?: if (open.storeBranch == null) identifier else null
        addVisit(
            ReportedVisit(
                clientVisitKey = open.clientVisitKey, latitudeDegrees = open.latitudeDegrees, longitudeDegrees = open.longitudeDegrees,
                accuracyMeters = 0.0, arrivedAt = null, departedAt = at, visitSource = "region", placeId = placeId, storeBranch = open.storeBranch,
            ),
        )
        return true
    }

    /** Forgets the visits begun, for a place that is no longer watched. */
    fun forgetOpenRegionVisit(identifier: String) {
        openRegionVisits = openRegionVisits - identifier
    }

    /** Ends every visit begun, as of a date: when monitoring stops, nothing will say the person left. */
    fun departEverywhere(at: Instant) {
        for (identifier in openRegionVisits.keys.sorted()) depart(identifier, at)
    }

    /**
     * Throws away what waits to be sent in a range (with neither end, all of it), keeping the visits begun: the person
     * is still where they are. For after the history is erased on the server, so that nothing erased is sent back.
     */
    fun discardUnsent(from: Instant? = null, until: Instant? = null) {
        fun isInRange(instant: Instant?): Boolean {
            if (instant == null) return from == null && until == null
            if (from != null && instant.isBefore(from)) return false
            if (until != null && !instant.isBefore(until)) return false
            return true
        }
        points = points.filterNot { point -> isInRange(point.measuredAt) }
        visits = visits.filterNot { visit -> isInRange(visit.arrivedAt ?: visit.departedAt) }
        activities = activities.filterNot { activity -> isInRange(activity.startedAt) }
    }

    /** The oldest points, visits and activities, as many as one call takes. */
    fun nextBatch(): LocationBatch =
        LocationBatch(points.take(POINT_COUNT_PER_BATCH), visits.take(VISIT_COUNT_PER_BATCH), activities.take(ACTIVITY_COUNT_PER_BATCH))

    /**
     * Takes out what a batch carried, once the server has answered. Entries are known by their place in the queue, so
     * ones added meanwhile stay, and ones dropped meanwhile for being the oldest do not take newer ones with them.
     */
    fun remove(batch: LocationBatch) {
        batch.points.lastOrNull()?.sequence?.let { last -> points = points.filterNot { point -> point.sequence <= last } }
        batch.visits.lastOrNull()?.sequence?.let { last -> visits = visits.filterNot { visit -> visit.sequence <= last } }
        batch.activities.lastOrNull()?.sequence?.let { last -> activities = activities.filterNot { activity -> activity.sequence <= last } }
    }

    /** The file's contents. */
    fun encoded(): String = TeaNodeJson.encodeToString(serializer(), this)

    companion object {
        /** The most points kept; the oldest go first. Visits are never dropped. */
        const val POINT_COUNT_MOST = 20_000
        const val POINT_COUNT_PER_BATCH = 500
        const val VISIT_COUNT_PER_BATCH = 50

        /** The most motion activities kept, and sent in one call; the oldest go first. */
        const val ACTIVITY_COUNT_MOST = 5_000
        const val ACTIVITY_COUNT_PER_BATCH = 500

        /** The latest a time may be after now. The server refuses more than a day; an hour less leaves room for the phone's clock being ahead. */
        const val FUTURE_SECONDS_MOST = 23.0 * 3600

        /** The longest clientVisitKey the server takes. */
        const val CLIENT_VISIT_KEY_LENGTH_MOST = 128

        /** The visit sources the server takes from a phone. */
        val VISIT_SOURCES: Set<String> = setOf("visit", "region")

        /** The longest a time may be before now when it is sent. The server refuses more than 400 days; a day less leaves room for the clock being behind. */
        const val PAST_SECONDS_MOST = 399.0 * 24 * 3600

        /** The name of the file an account's outbox is kept in: one file for each account. The key is percent-encoded, so no key can name another file or a directory. */
        fun fileName(accountKey: String): String {
            val safe = URLEncoder.encode(accountKey, "UTF-8").replace("%", "_").replace("+", "_").replace(".", "_").replace("*", "_").replace("-", "_")
            return "outbox-$safe.json"
        }

        /** An outbox read from a file's contents. */
        fun decoded(text: String): LocationOutbox = TeaNodeJson.decodeFromString(serializer(), text)
    }
}

/** LocationReportFailure is what the phone does about a batch the server did not take. */
sealed class LocationReportFailure {
    /** The call did not get through, or the sign-in needs renewing: keep the batch and try again later. */
    data object RetryLater : LocationReportFailure()

    /** The server named no phone of the person's with the identifier given: send the batch again without it. */
    data object PhoneNotFound : LocationReportFailure()

    /** The server will not take anything from this person (not allowed to use Location): retrying cannot change the answer. */
    data class Refused(val reason: String) : LocationReportFailure()

    /** The server called this batch invalid: it is dropped and recording goes on. */
    data class BatchInvalid(val reason: String) : LocationReportFailure()

    companion object {
        fun classify(error: Throwable): LocationReportFailure {
            val refusal = error as? TeaNodeError.Refused ?: return RetryLater
            if (error.isNotFoundRefusal) return PhoneNotFound
            if (refusal.reason.contains("permission denied")) return Refused(refusal.reason)
            if (refusal.reason.contains("invalid arguments")) return BatchInvalid(refusal.reason)
            return RetryLater
        }
    }
}

/**
 * LocationRetryGate keeps the phone from asking a server that just failed once for every position noticed: after a
 * failure, events wait this long before they try again. Leaving the app, coming back to it, and signing in are tried at once.
 */
class LocationRetryGate {
    private var lastFailedAt: Instant? = null

    /** Whether an event may start a request now. */
    fun canTry(at: Instant): Boolean {
        val failedAt = lastFailedAt ?: return true
        return (at.toEpochMilli() - failedAt.toEpochMilli()) / 1000.0 >= WAIT_SECONDS
    }

    fun didFail(at: Instant) {
        lastFailedAt = at
    }

    fun didSucceed() {
        lastFailedAt = null
    }

    companion object {
        const val WAIT_SECONDS = 30.0
    }
}

/** WatchedStoreBranch is the OpenStreetMap object of a store's branch the phone watches, as the server found it ([WatchedStore]). */
@Serializable
data class WatchedStoreBranch(val osmType: String, val osmId: Long, val osmName: String, val osmCategory: String)

/** WatchedPlace is a place the phone asks the system to tell it about the person entering and leaving: one of their own, or a branch of a store their lists need something from. */
@Serializable
data class WatchedPlace(
    /** What the monitor knows it by: the place's id, or "store:" and the map object for a store's branch. */
    val identifier: String,
    val placeId: String? = null,
    val latitudeDegrees: Double,
    val longitudeDegrees: Double,
    val radiusMeters: Double,
    val storeBranch: WatchedStoreBranch? = null,
) {
    companion object {
        /** One of the person's own places. */
        fun ofPlace(placeId: String, latitudeDegrees: Double, longitudeDegrees: Double, radiusMeters: Double): WatchedPlace =
            WatchedPlace(placeId, placeId, latitudeDegrees, longitudeDegrees, radiusMeters, null)

        /** A store's branch, with the person's place for it when they have one. */
        fun ofStore(store: WatchedStore): WatchedPlace = WatchedPlace(
            identifier = storeIdentifier(store.osmType, store.osmId), placeId = store.placeId, latitudeDegrees = store.latitudeDegrees,
            longitudeDegrees = store.longitudeDegrees, radiusMeters = maxOf(store.radiusMeters, LocationPlaceWatch.RADIUS_METERS_LEAST),
            storeBranch = WatchedStoreBranch(store.osmType, store.osmId, store.placeName, store.osmCategory),
        )

        /** What the monitor alone knows, when the app was launched for an event before the places were read again. */
        fun ofIdentifier(identifier: String, latitudeDegrees: Double, longitudeDegrees: Double, radiusMeters: Double): WatchedPlace {
            val storeObject = storeObject(identifier)
            return if (storeObject != null) {
                WatchedPlace(identifier, null, latitudeDegrees, longitudeDegrees, radiusMeters, WatchedStoreBranch(storeObject.first, storeObject.second, "", ""))
            } else {
                WatchedPlace(identifier, identifier, latitudeDegrees, longitudeDegrees, radiusMeters, null)
            }
        }

        fun storeIdentifier(osmType: String, osmId: Long): String = "store:$osmType:$osmId"

        internal fun storeObject(identifier: String): Pair<String, Long>? {
            val parts = identifier.split(":")
            if (parts.size != 3 || parts[0] != "store" || parts[1].isEmpty()) return null
            val osmId = parts[2].toLongOrNull() ?: return null
            return parts[1] to osmId
        }
    }
}

/**
 * LocationPlaceWatch chooses which places are watched. Android allows a hundred geofences per app; the app keeps to the
 * same twenty as the iPhone app so that the two behave alike. The branches of the stores the person's lists need
 * something from take up to [STORE_COUNT_MOST] of them, and the person's labeled and named places nearest to where
 * they last were take the rest, each at least 100 meters wide, since a smaller circle is not reliably noticed.
 */
object LocationPlaceWatch {
    const val COUNT_MOST = 20
    const val STORE_COUNT_MOST = 6
    const val RADIUS_METERS_LEAST = 100.0

    fun choose(
        places: List<LocationPlace>, stores: List<WatchedStore> = emptyList(), nearLatitudeDegrees: Double?, nearLongitudeDegrees: Double?,
    ): List<WatchedPlace> {
        val candidates = places.filter { place -> place.isOfPersonsOwn && LocationGeometry.isOnEarth(place.latitudeDegrees, place.longitudeDegrees) }
        val ordered = if (nearLatitudeDegrees != null && nearLongitudeDegrees != null) {
            candidates.sortedBy { place ->
                LocationGeometry.distanceMeters(nearLatitudeDegrees, nearLongitudeDegrees, place.latitudeDegrees, place.longitudeDegrees)
            }
        } else {
            // With no position yet, the labeled ones first: home and work.
            candidates.sortedWith(compareBy<LocationPlace> { place -> if (place.label != null) 0 else 1 }.thenBy { place -> place.id })
        }
        val branches = stores.filter { store -> LocationGeometry.isOnEarth(store.latitudeDegrees, store.longitudeDegrees) }.take(STORE_COUNT_MOST)
        val own = ordered.take(COUNT_MOST - branches.size).map { place ->
            WatchedPlace.ofPlace(place.id, place.latitudeDegrees, place.longitudeDegrees, maxOf(place.radiusMeters, RADIUS_METERS_LEAST))
        }
        val ownIds = own.mapNotNull { place -> place.placeId }.toSet()
        val seen = mutableSetOf<String>()
        val watchedBranches = branches.map { store -> WatchedPlace.ofStore(store) }.filter { branch ->
            if (!seen.add(branch.identifier)) false else branch.placeId?.let { placeId -> !ownIds.contains(placeId) } ?: true
        }
        return own + watchedBranches
    }

    /**
     * The places to watch once the circles of the visits still open are kept. A circle is the only thing that says the
     * person left, so one with an open visit stays watched when the server no longer lists it, until its exit closes
     * the visit. When the open ones push the watch past [COUNT_MOST], the others farthest from where the person last
     * was are dropped, or the last chosen ones when no position is known.
     */
    fun keepOpen(chosen: List<WatchedPlace>, open: List<WatchedPlace>, nearLatitudeDegrees: Double?, nearLongitudeDegrees: Double?): List<WatchedPlace> {
        val openIdentifiers = open.map { place -> place.identifier }.toSet()
        val chosenIdentifiers = chosen.map { place -> place.identifier }.toSet()
        val seen = mutableSetOf<String>()
        val unlisted = open.filter { place -> !chosenIdentifiers.contains(place.identifier) && seen.add(place.identifier) }
        val keptOpen = chosen.filter { place -> openIdentifiers.contains(place.identifier) } + unlisted
        val others = chosen.withIndex().filter { indexed -> !openIdentifiers.contains(indexed.value.identifier) }
        val room = maxOf(0, COUNT_MOST - keptOpen.size)
        val keptOthers = if (others.size <= room) {
            others
        } else if (nearLatitudeDegrees != null && nearLongitudeDegrees != null) {
            // The nearest first, the order they were chosen in between equals.
            others.sortedWith(
                compareBy<IndexedValue<WatchedPlace>> { indexed ->
                    LocationGeometry.distanceMeters(nearLatitudeDegrees, nearLongitudeDegrees, indexed.value.latitudeDegrees, indexed.value.longitudeDegrees)
                }.thenBy { indexed -> indexed.index },
            ).take(room)
        } else {
            others.take(room)
        }
        val keptOtherIdentifiers = keptOthers.map { indexed -> indexed.value.identifier }.toSet()
        val keptChosen = chosen.filter { place -> openIdentifiers.contains(place.identifier) || keptOtherIdentifiers.contains(place.identifier) }
        return (keptChosen + unlisted).take(COUNT_MOST)
    }
}

/** LocationVisitDay is the visits that began on one day, newest first, and the pauses and trips that began on it. */
data class LocationVisitDay(
    /** The day, in the zone it was grouped in. */
    val day: LocalDate,
    val visits: List<PlaceVisit>,
    /** The visits, the pauses and the trips together, newest first: what the day's timeline lists. */
    val entries: List<LocationTimelineEntry>,
) {
    val id: String get() = day.toString()
}

object LocationDays {
    /**
     * Groups visits by the day they began (or ended, when the arrival was not seen), newest day first and newest visit
     * first within it. A visit with no time at all is left out: it cannot be placed. A pause is placed on the day it
     * began, among the visits of that day, and so is a trip.
     */
    fun group(
        visits: List<PlaceVisit>, pauses: List<LocationPause> = emptyList(), trips: List<Trip> = emptyList(), zone: ZoneId = ZoneId.systemDefault(),
    ): List<LocationVisitDay> {
        val visitsByDay = visits.mapNotNull { visit -> visit.anchorInstant?.let { anchor -> anchor.atZone(zone).toLocalDate() to visit } }.groupBy({ pair -> pair.first }, { pair -> pair.second })
        val pausesByDay = pauses.mapNotNull { pause -> pause.startedInstant?.let { started -> started.atZone(zone).toLocalDate() to pause } }.groupBy({ pair -> pair.first }, { pair -> pair.second })
        // A trip with no leg has nothing to draw.
        val tripsByDay = trips.filter { trip -> trip.legs.isNotEmpty() }
            .mapNotNull { trip -> trip.startedInstant?.let { started -> started.atZone(zone).toLocalDate() to trip } }.groupBy({ pair -> pair.first }, { pair -> pair.second })
        val days = visitsByDay.keys + pausesByDay.keys + tripsByDay.keys
        return days.sortedDescending().map { day ->
            val sortedVisits = visitsByDay[day].orEmpty().sortedByDescending { visit -> visit.anchorInstant ?: Instant.MIN }
            val entries = (
                sortedVisits.map { visit -> LocationTimelineEntry.Visit(visit) as LocationTimelineEntry } +
                    pausesByDay[day].orEmpty().map { pause -> LocationTimelineEntry.Pause(pause) } +
                    tripsByDay[day].orEmpty().map { trip -> LocationTimelineEntry.TripEntry(trip) }
                ).sortedWith(LocationTimelineEntry.newestFirst)
            LocationVisitDay(day, sortedVisits, entries)
        }
    }

    /** The visits, pauses and trips of one stretch as a single timeline, newest first. */
    fun entries(visits: List<PlaceVisit>, pauses: List<LocationPause>, trips: List<Trip> = emptyList()): List<LocationTimelineEntry> =
        (
            visits.map { visit -> LocationTimelineEntry.Visit(visit) as LocationTimelineEntry } + pauses.map { pause -> LocationTimelineEntry.Pause(pause) } +
                trips.filter { trip -> trip.legs.isNotEmpty() }.map { trip -> LocationTimelineEntry.TripEntry(trip) }
            ).filter { entry -> entry.anchorInstant != null }.sortedWith(LocationTimelineEntry.newestFirst)

    /**
     * A day's path as a map draws it: the positions, and each visit's position at its arrival and at its departure,
     * oldest first. A phone in the background sends few positions on the way, so the line goes through the places the
     * person went even when it sent none between them. Only moments from [from] up to, not including, [until] count.
     */
    fun path(points: List<LocationPoint>, visits: List<PlaceVisit>, from: Instant, until: Instant): List<LocationPoint> {
        val visitPoints = visits.flatMap { visit ->
            listOfNotNull(visit.arrivedAt, visit.departedAt).map { moment ->
                LocationPoint(visit.latitudeDegrees, visit.longitudeDegrees, visit.accuracyMeters, measuredAt = moment)
            }
        }
        return (points + visitPoints).mapNotNull { point ->
            val instant = point.measuredInstant ?: return@mapNotNull null
            if (instant.isBefore(from) || !instant.isBefore(until)) null else instant to point
        }.sortedBy { pair -> pair.first }.map { pair -> pair.second }
    }

    /** The span of one day, from its midnight to the next. */
    fun window(day: LocalDate, zone: ZoneId = ZoneId.systemDefault()): Pair<Instant, Instant> =
        day.atStartOfDay(zone).toInstant() to day.plusDays(1).atStartOfDay(zone).toInstant()
}
