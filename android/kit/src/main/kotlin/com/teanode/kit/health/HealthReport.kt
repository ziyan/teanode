package com.teanode.kit.health

import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeError
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.isNotFoundRefusal
import com.teanode.kit.location.LocationGeometry
import com.teanode.kit.location.ServerTime
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import java.time.Instant

// What the phone sends the server about Health, and what it reads back. The phone reads the platform's store from a
// saved position (HealthSyncState.kt), works out each day's summary (HealthDaySummaries.kt or the platform's
// statistics), and sends both with ReportHealth, a batch at a time. The names are the server's
// (internal/api/v1api/apigraph/health.go) and the dashboard's. Mirrors ios/TeaNodeKit HealthReport.swift.

private fun JsonObjectOf(vararg members: Pair<String, JsonElement?>): JsonObject = JsonObject(members.mapNotNull { (key, value) -> value?.let { key to it } }.toMap())

/**
 * HealthSampleInput is one reading, as the phone sends it. A quantity has a value and a unit, a category sample a raw
 * value, a workout its activity type; anything else about it goes in the metadata, which is sent as JSON text.
 */
class HealthSampleInput(
    /** The platform's id for the sample; the server keeps one sample for each, so a batch sent twice adds nothing. */
    val healthSampleId: String,
    val healthType: String,
    val startedAt: Instant,
    val endedAt: Instant,
    val quantityValue: Double? = null,
    val quantityUnit: String = "",
    val categoryValue: Int? = null,
    val workoutActivityType: String = "",
    sourceName: String = "",
    sourceBundleId: String = "",
    deviceName: String = "",
    sampleMetadata: Map<String, JsonElement> = emptyMap(),
) {
    // The server refuses a null character in any text, and metadata over its limit: the sample is kept and the least useful keys are not.
    val sourceName: String = HealthMetadataFit.withoutNull(sourceName)
    val sourceBundleId: String = HealthMetadataFit.withoutNull(sourceBundleId)
    val deviceName: String = HealthMetadataFit.withoutNull(deviceName)
    val sampleMetadata: Map<String, JsonElement> = HealthMetadataFit.fitting(sampleMetadata)

    /**
     * Whether the server would take the sample. It refuses a whole batch for one that is not, so such a sample is left
     * out of the batch: it has no identifier, no type, a value that is not a number, or ends before it begins.
     */
    val isAcceptable: Boolean
        get() = healthSampleId.isNotEmpty() && healthSampleId.toByteArray().size <= HEALTH_SAMPLE_ID_LENGTH_MOST && healthType.isNotEmpty() &&
            !endedAt.isBefore(startedAt) && (quantityValue?.isFinite() ?: true)

    val variables: JsonElement
        get() = JsonObjectOf(
            "healthSampleId" to JsonPrimitive(healthSampleId), "healthType" to JsonPrimitive(healthType),
            "startedAt" to JsonPrimitive(ServerTime.text(startedAt)), "endedAt" to JsonPrimitive(ServerTime.text(endedAt)),
            "quantityValue" to quantityValue?.let { JsonPrimitive(it) }, "quantityUnit" to JsonPrimitive(quantityUnit),
            "categoryValue" to categoryValue?.let { JsonPrimitive(it) }, "workoutActivityType" to JsonPrimitive(workoutActivityType),
            "sourceName" to JsonPrimitive(this.sourceName), "sourceBundleId" to JsonPrimitive(this.sourceBundleId),
            "deviceName" to JsonPrimitive(this.deviceName), "sampleMetadataJson" to JsonPrimitive(HealthJson.text(this.sampleMetadata)),
        )

    override fun equals(other: Any?): Boolean = other is HealthSampleInput && variables == other.variables
    override fun hashCode(): Int = variables.hashCode()
    override fun toString(): String = "HealthSampleInput($healthSampleId, $healthType)"

    companion object {
        /** The longest healthSampleId the server keeps. */
        const val HEALTH_SAMPLE_ID_LENGTH_MOST = 64
    }
}

/** HealthDaySummaryInput is one type on one day, worked out on the phone, where sources can be merged the way the Health app merges them. */
data class HealthDaySummaryInput(
    val healthType: String,
    /** The phone's calendar day, "YYYY-MM-DD". */
    val summaryDay: String,
    val quantityUnit: String = "",
    val sumValue: Double? = null,
    val averageValue: Double? = null,
    val minimumValue: Double? = null,
    val maximumValue: Double? = null,
    val sampleCount: Int,
    /** For categories and workouts: the time covered, sources overlapping counted once. */
    val durationSeconds: Double? = null,
    /** Seconds by sleep stage (and "asleep" for all the asleep stages together); for workouts, "energyKilocalories" and "distanceMeters". */
    val categorySeconds: Map<String, Double> = emptyMap(),
    /** The phone's time when it worked the day out. The server keeps the latest, so a late resend cannot overwrite a newer summary. */
    val workedOutAt: Instant? = null,
) {
    /** The same summary, worked out at this time. */
    fun workedOut(at: Instant): HealthDaySummaryInput = copy(workedOutAt = at)

    /** Whether the server would take the summary: a day written as "YYYY-MM-DD", no number that is not a number, and a type. */
    val isAcceptable: Boolean
        get() {
            val values = listOfNotNull(sumValue, averageValue, minimumValue, maximumValue, durationSeconds) + categorySeconds.values
            return healthType.isNotEmpty() && HealthDays.isDayText(summaryDay) && sampleCount >= 0 && values.all { value -> value.isFinite() }
        }

    val variables: JsonElement
        get() = JsonObjectOf(
            "healthType" to JsonPrimitive(healthType), "summaryDay" to JsonPrimitive(summaryDay), "quantityUnit" to JsonPrimitive(quantityUnit),
            "sumValue" to sumValue?.let { JsonPrimitive(it) }, "averageValue" to averageValue?.let { JsonPrimitive(it) },
            "minimumValue" to minimumValue?.let { JsonPrimitive(it) }, "maximumValue" to maximumValue?.let { JsonPrimitive(it) },
            "sampleCount" to JsonPrimitive(sampleCount), "durationSeconds" to durationSeconds?.let { JsonPrimitive(it) },
            "categorySecondsJson" to JsonPrimitive(HealthJson.text(categorySeconds.mapValues { (_, seconds) -> JsonPrimitive(seconds) })),
            "workedOutAt" to workedOutAt?.let { JsonPrimitive(ServerTime.text(it)) },
        )
}

/** HealthRoutePoint is one position of a workout's path. */
data class HealthRoutePoint(val secondsFromStart: Double, val latitudeDegrees: Double, val longitudeDegrees: Double, val altitudeMeters: Double) {
    val row: List<Double> get() = listOf(secondsFromStart, latitudeDegrees, longitudeDegrees, altitudeMeters)

    /** Whether the coordinates are on the earth and every number is a number. */
    val isAcceptable: Boolean
        get() = secondsFromStart.isFinite() && altitudeMeters.isFinite() && LocationGeometry.isOnEarth(latitudeDegrees, longitudeDegrees)

    companion object {
        /** From a row as sent: [secondsFromStart, latitudeDegrees, longitudeDegrees, altitudeMeters]. */
        fun ofRow(row: List<Double>): HealthRoutePoint? {
            if (row.size < 3) return null
            return HealthRoutePoint(row[0], row[1], row[2], if (row.size > 3) row[3] else 0.0)
        }
    }
}

/** HealthRouteInput is the path of a workout, sent after (or with) the workout. */
data class HealthRouteInput(
    /** The workout's healthSampleId. */
    val healthSampleId: String,
    /** [secondsFromStart, latitudeDegrees, longitudeDegrees, altitudeMeters] rows. */
    val routePoints: List<List<Double>>,
) {
    val points: List<HealthRoutePoint> get() = routePoints.mapNotNull { row -> HealthRoutePoint.ofRow(row) }

    /** Whether the server would take the route: a workout, at least one point, no more than the most, and every point a position. */
    val isAcceptable: Boolean
        get() = healthSampleId.isNotEmpty() && routePoints.isNotEmpty() && routePoints.size <= POINT_COUNT_MOST &&
            routePoints.all { row -> HealthRoutePoint.ofRow(row)?.isAcceptable ?: false }

    val variables: JsonElement
        get() = JsonObject(
            mapOf(
                "healthSampleId" to JsonPrimitive(healthSampleId),
                "routePoints" to JsonArray(routePoints.map { row -> JsonArray(row.map { number -> JsonPrimitive(number) }) }),
            ),
        )

    companion object {
        /** The most points the server keeps of one route. */
        const val POINT_COUNT_MOST = 20_000

        /** A route from its points, in order. Points not on the earth are left out; a path of more than 20,000 points is thinned evenly, keeping the first and the last. */
        fun of(healthSampleId: String, points: List<HealthRoutePoint>): HealthRouteInput {
            val usable = points.filter { point -> point.isAcceptable }
            var kept = usable
            if (usable.size > POINT_COUNT_MOST) {
                val step = (usable.size - 1).toDouble() / (POINT_COUNT_MOST - 1)
                kept = (0 until POINT_COUNT_MOST).map { index -> usable[Math.round(index * step).toInt()] }
            }
            return HealthRouteInput(healthSampleId, kept.map { point -> point.row })
        }
    }
}

/** HealthCharacteristicInput is a fact about the person: date of birth, biological sex, blood type, skin type, wheelchair use. */
data class HealthCharacteristicInput(
    val healthType: String,
    /** "1990-04-12", "female", "aPositive", ... */
    val characteristicValue: String,
) {
    val variables: JsonElement get() = JsonObject(mapOf("healthType" to JsonPrimitive(healthType), "characteristicValue" to JsonPrimitive(characteristicValue)))
}

/** HealthBatch is what one ReportHealth call carries. */
data class HealthBatch(
    val samples: List<HealthSampleInput> = emptyList(),
    val deletedHealthSampleIds: List<String> = emptyList(),
    val daySummaries: List<HealthDaySummaryInput> = emptyList(),
    val routes: List<HealthRouteInput> = emptyList(),
    val characteristics: List<HealthCharacteristicInput> = emptyList(),
) {
    val isEmpty: Boolean get() = entryCount == 0

    /** How many samples, deleted identifiers, summaries, routes and characteristics it carries together. */
    val entryCount: Int get() = samples.size + deletedHealthSampleIds.size + daySummaries.size + routes.size + characteristics.size

    /** Whether it is within what one call takes. */
    val isWithinLimits: Boolean
        get() = samples.size <= SAMPLE_COUNT_MOST && deletedHealthSampleIds.size <= DELETED_SAMPLE_ID_COUNT_MOST &&
            daySummaries.size <= DAY_SUMMARY_COUNT_MOST && routes.size <= ROUTE_COUNT_MOST && characteristics.size <= CHARACTERISTIC_COUNT_MOST

    /** How many bytes of JSON the call carries. */
    val encodedByteCount: Int get() = JsonObject(variables).toString().toByteArray().size

    /** The batch, or its halves, and their halves, until each is within the most bytes; a single entry is never cut. */
    internal fun cutToFit(byteCountMost: Int): List<HealthBatch> {
        if (entryCount <= 1 || encodedByteCount <= byteCountMost) return listOf(this)
        val halves = split() ?: return listOf(this)
        return halves.first.cutToFit(byteCountMost) + halves.second.cutToFit(byteCountMost)
    }

    /**
     * The two smaller batches to send when the server calls this one invalid without saying which entry is wrong. If it
     * holds more than one kind of entry, the first kinds are apart from the rest, so a route still comes after its
     * workout; else the first half and the rest of the one kind it holds. Null for a single entry.
     */
    fun split(): Pair<HealthBatch, HealthBatch>? {
        if (entryCount <= 1) return null
        val kinds = listOf(samples.size, deletedHealthSampleIds.size, daySummaries.size, routes.size, characteristics.size)
        val present = kinds.indices.filter { index -> kinds[index] > 0 }
        if (present.size > 1) {
            val firstKinds = present.take(present.size / 2).toSet()
            fun part(keep: (Int) -> Boolean): HealthBatch = HealthBatch(
                samples = if (keep(0)) samples else emptyList(), deletedHealthSampleIds = if (keep(1)) deletedHealthSampleIds else emptyList(),
                daySummaries = if (keep(2)) daySummaries else emptyList(), routes = if (keep(3)) routes else emptyList(),
                characteristics = if (keep(4)) characteristics else emptyList(),
            )
            return part { kind -> firstKinds.contains(kind) } to part { kind -> !firstKinds.contains(kind) }
        }
        fun <Element> halves(elements: List<Element>): Pair<List<Element>, List<Element>> {
            val middle = elements.size / 2
            return elements.subList(0, middle) to elements.subList(middle, elements.size)
        }
        return when (present[0]) {
            0 -> halves(samples).let { (first, second) -> HealthBatch(samples = first) to HealthBatch(samples = second) }
            1 -> halves(deletedHealthSampleIds).let { (first, second) -> HealthBatch(deletedHealthSampleIds = first) to HealthBatch(deletedHealthSampleIds = second) }
            2 -> halves(daySummaries).let { (first, second) -> HealthBatch(daySummaries = first) to HealthBatch(daySummaries = second) }
            3 -> halves(routes).let { (first, second) -> HealthBatch(routes = first) to HealthBatch(routes = second) }
            else -> halves(characteristics).let { (first, second) -> HealthBatch(characteristics = first) to HealthBatch(characteristics = second) }
        }
    }

    /**
     * The batch without the one entry the server's reason names ("samples[2] ...", "daySummaries[0] ..."), or null if
     * the reason names none that this batch has. The server counts entries in the call it was sent, which is this batch.
     */
    fun removingEntry(named: String): HealthBatch? {
        fun indexOf(listName: String, count: Int): Int? {
            val opening = named.indexOf("$listName[")
            if (opening < 0) return null
            val afterOpening = named.substring(opening + listName.length + 1)
            val digits = afterOpening.takeWhile { character -> character.isDigit() }
            if (digits.isEmpty() || afterOpening.getOrNull(digits.length) != ']') return null
            val position = digits.toIntOrNull() ?: return null
            return if (position < count) position else null
        }
        fun <Element> without(elements: List<Element>, position: Int): List<Element> = elements.filterIndexed { index, _ -> index != position }
        indexOf("samples", samples.size)?.let { position -> return copy(samples = without(samples, position)) }
        indexOf("deletedHealthSampleIds", deletedHealthSampleIds.size)?.let { position -> return copy(deletedHealthSampleIds = without(deletedHealthSampleIds, position)) }
        indexOf("daySummaries", daySummaries.size)?.let { position -> return copy(daySummaries = without(daySummaries, position)) }
        indexOf("routes", routes.size)?.let { position -> return copy(routes = without(routes, position)) }
        indexOf("characteristics", characteristics.size)?.let { position -> return copy(characteristics = without(characteristics, position)) }
        return null
    }

    /** The GraphQL variables of the call (the phone's identifier is added by [reportHealth]). */
    val variables: Map<String, JsonElement>
        get() = mapOf(
            "samples" to JsonArray(samples.map { sample -> sample.variables }),
            "deletedHealthSampleIds" to JsonArray(deletedHealthSampleIds.map { id -> JsonPrimitive(id) }),
            "daySummaries" to JsonArray(daySummaries.map { summary -> summary.variables }),
            "routes" to JsonArray(routes.map { route -> route.variables }),
            "characteristics" to JsonArray(characteristics.map { characteristic -> characteristic.variables }),
        )

    /** The calls to make, and how many entries were left out because the server would refuse them. */
    data class Requests(val requests: List<HealthBatch>, val rejectedEntryCount: Int)

    companion object {
        const val SAMPLE_COUNT_MOST = 1000
        const val DELETED_SAMPLE_ID_COUNT_MOST = 2000
        const val DAY_SUMMARY_COUNT_MOST = 1000
        const val ROUTE_COUNT_MOST = 5
        const val CHARACTERISTIC_COUNT_MOST = 20

        /** The most bytes of JSON a call carries before it is cut in two. The signed-in endpoint refuses a body over 12 MB before it reads it. */
        const val ENCODED_BYTE_COUNT_MOST = 8 * 1024 * 1024

        /**
         * Splits what is waiting into the calls to make: each carries at most 1000 samples, 2000 deleted identifiers,
         * 1000 day summaries, 5 routes and 20 characteristics, the oldest first. A route is never sent in a call before
         * the one that carries its workout. What the server would refuse for one entry is left out rather than costing
         * the batch, and counted.
         */
        fun requests(
            samples: List<HealthSampleInput> = emptyList(), deletedHealthSampleIds: List<String> = emptyList(),
            daySummaries: List<HealthDaySummaryInput> = emptyList(), routes: List<HealthRouteInput> = emptyList(),
            characteristics: List<HealthCharacteristicInput> = emptyList(),
        ): Requests {
            val acceptableSamples = samples.filter { sample -> sample.isAcceptable }
            val acceptableSummaries = daySummaries.filter { summary -> summary.isAcceptable }
            val acceptableRoutes = routes.filter { route -> route.isAcceptable }
            val acceptableIds = deletedHealthSampleIds.filter { id -> id.isNotEmpty() }
            val rejectedEntryCount = (samples.size - acceptableSamples.size) + (daySummaries.size - acceptableSummaries.size) +
                (routes.size - acceptableRoutes.size) + (deletedHealthSampleIds.size - acceptableIds.size)

            val sampleSlices = acceptableSamples.chunked(SAMPLE_COUNT_MOST)
            val deletedSlices = acceptableIds.chunked(DELETED_SAMPLE_ID_COUNT_MOST)
            val summarySlices = acceptableSummaries.chunked(DAY_SUMMARY_COUNT_MOST)
            val characteristicSlices = characteristics.chunked(CHARACTERISTIC_COUNT_MOST)
            val requestCount = maxOf(sampleSlices.size, deletedSlices.size, summarySlices.size, characteristicSlices.size)

            // The call each sample is in, so that its route is not sent before it.
            val callOfSample = mutableMapOf<String, Int>()
            for ((index, slice) in sampleSlices.withIndex()) for (sample in slice) callOfSample[sample.healthSampleId] = index
            val routeSlices = MutableList(requestCount) { mutableListOf<HealthRouteInput>() }
            for (route in acceptableRoutes) {
                var index = callOfSample[route.healthSampleId] ?: 0
                while (true) {
                    if (index == routeSlices.size) routeSlices.add(mutableListOf())
                    if (routeSlices[index].size < ROUTE_COUNT_MOST) break
                    index += 1
                }
                routeSlices[index].add(route)
            }

            val total = maxOf(requestCount, routeSlices.size)
            val batches = (0 until total).map { index ->
                HealthBatch(
                    samples = sampleSlices.getOrElse(index) { emptyList() }, deletedHealthSampleIds = deletedSlices.getOrElse(index) { emptyList() },
                    daySummaries = summarySlices.getOrElse(index) { emptyList() }, routes = routeSlices.getOrElse(index) { emptyList() },
                    characteristics = characteristicSlices.getOrElse(index) { emptyList() },
                )
            }.filter { batch -> !batch.isEmpty }
            return Requests(batches.flatMap { batch -> batch.cutToFit(ENCODED_BYTE_COUNT_MOST) }, rejectedEntryCount)
        }
    }
}

/** HealthDeletedSample is a sample the server removed because the platform deleted it, with the time it covered. */
@Serializable
data class HealthDeletedSample(val healthType: String, val startedAt: String, val endedAt: String) {
    val startedInstant: Instant? get() = ServerTime.instant(startedAt)
    val endedInstant: Instant? get() = ServerTime.instant(endedAt)
}

/** ReportHealthResult is what the server took of a batch. */
@Serializable
data class ReportHealthResult(
    val newSampleCount: Int = 0,
    val deletedSampleCount: Int = 0,
    val updatedDayCount: Int = 0,
    val deletedSamples: List<HealthDeletedSample> = emptyList(),
    /** The workouts whose routes the server left out because it has no such workout yet; they are sent again after the workout. */
    val unmatchedRouteHealthSampleIds: List<String> = emptyList(),
    /** When the person last erased their Health data on the server, RFC 3339; null if never. */
    val healthErasedAt: String? = null,
) {
    /** The answers of two calls as one: the counts added, the deleted samples joined, and the later erase time. */
    fun merged(with: ReportHealthResult): ReportHealthResult {
        val later = if (ServerTime.isLater(with.healthErasedAt, healthErasedAt)) with.healthErasedAt else healthErasedAt
        return ReportHealthResult(
            newSampleCount + with.newSampleCount, deletedSampleCount + with.deletedSampleCount, updatedDayCount + with.updatedDayCount,
            deletedSamples + with.deletedSamples, unmatchedRouteHealthSampleIds + with.unmatchedRouteHealthSampleIds, later,
        )
    }

    companion object {
        val nothing = ReportHealthResult()
    }
}

/** HealthSendOutcome is what sending a batch, with any splitting, came to. */
data class HealthSendOutcome(
    /** What the server answered over all the calls, added up. */
    val result: ReportHealthResult,
    /** How many entries the server refused on their own (a batch of one) and that were therefore dropped. */
    val droppedEntryCount: Int,
    /** The server's reasons for those, in the order met. */
    val droppedReasons: List<String>,
)

/** HealthBatchSender sends a batch the way the location recorder does. */
object HealthBatchSender {
    /**
     * Sends a batch. If the server calls it invalid and names the entry ("samples[2] ..."), that entry is dropped and
     * the rest sent again. If it names none, the batch is sent again in two smaller batches, down to the single entry
     * the server refuses on its own, which is dropped: one bad entry must not take the rest sent beside it. Any other
     * failure is thrown at once, and the caller keeps the whole batch, since the server takes a batch sent twice as the
     * same samples.
     */
    suspend fun sendSplittingInvalid(batch: HealthBatch, send: suspend (HealthBatch) -> ReportHealthResult): HealthSendOutcome {
        try {
            return HealthSendOutcome(send(batch), 0, emptyList())
        } catch (error: Exception) {
            val failure = HealthReportFailure.classify(error) as? HealthReportFailure.BatchInvalid ?: throw error
            val reason = failure.reason
            // A fault of the call and not of an entry (the erase time it carries): halving the batch cannot mend it.
            if (reason.contains("healthErasedAt")) throw error
            // The server names the entry it refuses: drop exactly that one and send the rest, rather than halving down to it.
            val rest = batch.removingEntry(reason)
            if (rest != null) {
                if (rest.isEmpty) return HealthSendOutcome(ReportHealthResult.nothing, 1, listOf(reason))
                val remainder = sendSplittingInvalid(rest, send)
                return HealthSendOutcome(remainder.result, remainder.droppedEntryCount + 1, listOf(reason) + remainder.droppedReasons)
            }
            val halves = batch.split() ?: return HealthSendOutcome(ReportHealthResult.nothing, batch.entryCount, listOf(reason))
            val first = sendSplittingInvalid(halves.first, send)
            val second = sendSplittingInvalid(halves.second, send)
            return HealthSendOutcome(first.result.merged(second.result), first.droppedEntryCount + second.droppedEntryCount, first.droppedReasons + second.droppedReasons)
        }
    }
}

/** HealthReportFailure is what the phone does about a batch the server did not take. */
sealed class HealthReportFailure {
    /** The call did not get through, or the sign-in needs renewing: keep the batch and try again later. */
    data object RetryLater : HealthReportFailure()

    /** The server named no phone of the person's with the identifier given: send the batch again without it. */
    data object PhoneNotFound : HealthReportFailure()

    /** The server will not take anything from this person (not allowed to use Health): retrying cannot change the answer. */
    data class Refused(val reason: String) : HealthReportFailure()

    /** The server called this batch invalid: sending it again cannot change the answer, but smaller batches may be fine. */
    data class BatchInvalid(val reason: String) : HealthReportFailure()

    companion object {
        fun classify(error: Throwable): HealthReportFailure {
            val refusal = error as? TeaNodeError.Refused ?: return RetryLater
            if (error.isNotFoundRefusal) return PhoneNotFound
            if (refusal.reason.contains("permission denied")) return Refused(refusal.reason)
            if (refusal.reason.contains("invalid arguments")) return BatchInvalid(refusal.reason)
            return RetryLater
        }
    }
}

/** HealthRetryGate keeps the phone from asking a server that just failed once for every change the platform reports: after a failure, events wait this long before they try again. */
class HealthRetryGate {
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

// Reading

/** HealthPeriod is how long each row of a summary covers. */
enum class HealthPeriod(val serverWord: String) {
    Day("day"),
    Week("week"),
    Month("month"),
}

/** HealthPeriodView is one type over one period (a day, a week starting on Monday, or a month), as the server adds up the day summaries. */
@Serializable
data class HealthPeriodView(
    /** "YYYY-MM-DD". */
    val startDay: String,
    val endDay: String,
    val sumValue: Double? = null,
    val averageValue: Double? = null,
    val minimumValue: Double? = null,
    val maximumValue: Double? = null,
    val sampleCount: Int = 0,
    val durationSeconds: Double? = null,
    /** How many days of the period have a day summary: what the amount of a week or a month is divided by to be read per day. Zero when the server did not say. */
    val dayCount: Int = 0,
    /** Seconds by sleep stage, and "asleep"; see [HealthDaySummaryInput]. The server sends an object, or JSON text of one. */
    private val categorySeconds: JsonElement? = null,
) {
    val categorySecondsByStage: Map<String, Double> get() = HealthJson.secondsOf(categorySeconds)
}

/** HealthDaySummaryView is one type on one day, as the server keeps it: the summary the phone sent. */
@Serializable
data class HealthDaySummaryView(
    /** "YYYY-MM-DD", the phone's calendar day. */
    val summaryDay: String,
    val quantityUnit: String = "",
    val sumValue: Double? = null,
    val averageValue: Double? = null,
    val minimumValue: Double? = null,
    val maximumValue: Double? = null,
    val sampleCount: Int = 0,
    val durationSeconds: Double? = null,
    private val categorySeconds: JsonElement? = null,
) {
    val categorySecondsByStage: Map<String, Double> get() = HealthJson.secondsOf(categorySeconds)
}

/** HealthTypeView is one type the person (or somebody sharing with them) has data for. */
@Serializable
data class HealthTypeView(
    val healthType: String,
    val healthCategory: String,
    /** The name in English; the screens put the types they know into the person's language and fall back to this. */
    val typeName: String,
    val valueKind: String,
    val quantityUnit: String = "",
    val summaryKind: String? = null,
    /** "YYYY-MM-DD". */
    val firstDay: String? = null,
    val lastDay: String? = null,
    val sampleCount: Int = 0,
    val latestDaySummary: HealthDaySummaryView? = null,
) {
    val id: String get() = healthType

    /** The group, if it is one the app knows. */
    val healthCategoryValue: HealthCategory get() = HealthCategory.fromServerWord(healthCategory) ?: HealthCategory.Other
    val valueKindValue: HealthValueKind? get() = HealthValueKind.fromServerWord(valueKind)
    val summaryKindValue: HealthSummaryKind? get() = summaryKind?.let { word -> HealthSummaryKind.fromServerWord(word) }
}

/** HealthSummaryView is one type over a range, a row for each period. */
@Serializable
data class HealthSummaryView(
    val healthType: String,
    val quantityUnit: String = "",
    val summaryKind: String? = null,
    val periods: List<HealthPeriodView> = emptyList(),
) {
    val summaryKindValue: HealthSummaryKind? get() = summaryKind?.let { word -> HealthSummaryKind.fromServerWord(word) }
}

/** HealthSampleView is one reading, as the server keeps it. Only the person's own; a share never reaches samples. */
@Serializable
data class HealthSampleView(
    val healthSampleId: String,
    val healthType: String,
    /** RFC 3339. */
    val startedAt: String,
    val endedAt: String,
    val quantityValue: Double? = null,
    val quantityUnit: String = "",
    val categoryValue: Int? = null,
    val workoutActivityType: String = "",
    val sourceName: String = "",
    val sourceBundleId: String = "",
    val deviceName: String = "",
    private val sampleMetadataJson: String = "",
    val receivedAt: String = "",
    val hasRoute: Boolean = false,
) {
    val id: String get() = healthSampleId
    val startedInstant: Instant? get() = ServerTime.instant(startedAt)
    val endedInstant: Instant? get() = ServerTime.instant(endedAt)
    val sampleMetadata: Map<String, JsonElement> get() = HealthJson.objectOf(sampleMetadataJson)
}

/** HealthSamplePage is a page of readings. */
@Serializable
data class HealthSamplePage(val samples: List<HealthSampleView>, val totalCount: Int = 0, val nextOffset: Int? = null)

/** HealthRouteView is the path of a workout. */
@Serializable
data class HealthRouteView(val healthSampleId: String, val routePoints: List<List<Double>> = emptyList()) {
    val points: List<HealthRoutePoint> get() = routePoints.mapNotNull { row -> HealthRoutePoint.ofRow(row) }
}

/** HealthCharacteristicView is one fact about the person, as kept. */
@Serializable
data class HealthCharacteristicView(val healthType: String, val characteristicValue: String)

/** HealthSettingsView is the person's Health settings and what the server holds. */
@Serializable
data class HealthSettingsView(
    /** Whether the agent may read Health data. Off until the person turns it on. */
    val canAgentReadHealth: Boolean = false,
    /** When the person last erased their Health data, RFC 3339; null if never. */
    val healthErasedAt: String? = null,
    val sampleCount: Int = 0,
    val lastReceivedAt: String? = null,
    /** The provider the agent's reading goes to, named in the switch's words. */
    val modelProviderName: String = "",
    // The server sends null for a list with nothing in it.
    val characteristics: List<HealthCharacteristicView>? = null,
) {
    val characteristicList: List<HealthCharacteristicView> get() = characteristics.orEmpty()
    val healthErasedInstant: Instant? get() = ServerTime.instant(healthErasedAt)
    val lastReceivedInstant: Instant? get() = ServerTime.instant(lastReceivedAt)
}

/** EraseHealthDataResult is what an erase removed. */
@Serializable
data class EraseHealthDataResult(val erasedSampleCount: Int)

/** HealthShareView is one share of Health data by category: a person sharing with the caller, or the caller sharing with a person or a circle. */
@Serializable
data class HealthShareView(
    val id: String,
    val ownerUserId: String,
    val ownerUsername: String,
    val ownerName: String? = null,
    val granteeUserId: String? = null,
    val granteeUsername: String? = null,
    val granteeName: String? = null,
    val granteeCircleId: String? = null,
    val granteeCircleName: String? = null,
    val healthCategories: List<String> = emptyList(),
    val createdAt: String = "",
) {
    /** The categories that are ones the app knows, in the Health app's order. */
    val sharedCategories: List<HealthCategory> get() = HealthCategory.entries.filter { category -> healthCategories.contains(category.serverWord) }
}

// Documents

/** The Health documents. Copied from the iPhone app's `Documents` (ios/TeaNodeKit HealthReport.swift), whose text the server's tests check. */
object HealthDocuments {
    private const val PERIOD_FIELDS = """
        startDay endDay dayCount sumValue averageValue minimumValue maximumValue sampleCount durationSeconds categorySeconds
        """
    private const val TYPE_FIELDS = """
        healthType healthCategory typeName valueKind quantityUnit summaryKind firstDay lastDay sampleCount
        latestDaySummary { summaryDay quantityUnit sumValue averageValue minimumValue maximumValue sampleCount durationSeconds categorySeconds }
        """
    private const val SAMPLE_FIELDS = """
        healthSampleId healthType startedAt endedAt quantityValue quantityUnit categoryValue workoutActivityType sourceName sourceBundleId deviceName
        sampleMetadataJson receivedAt hasRoute
        """
    private const val SETTINGS_FIELDS = """
        canAgentReadHealth healthErasedAt sampleCount lastReceivedAt modelProviderName characteristics { healthType characteristicValue }
        """
    private const val SHARE_FIELDS = """
        id ownerUserId ownerUsername ownerName granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName healthCategories createdAt
        """

    const val REPORT_HEALTH = """
        mutation (${'$'}agentPhoneId: String, ${'$'}healthErasedAt: DateTime, ${'$'}samples: [HealthSampleInput!]!, ${'$'}deletedHealthSampleIds: [String!]!, ${'$'}daySummaries: [HealthDaySummaryInput!]!, ${'$'}routes: [HealthRouteInput!]!, ${'$'}characteristics: [HealthCharacteristicInput!]!) {
          ReportHealth(agentPhoneId: ${'$'}agentPhoneId, healthErasedAt: ${'$'}healthErasedAt, samples: ${'$'}samples, deletedHealthSampleIds: ${'$'}deletedHealthSampleIds, daySummaries: ${'$'}daySummaries, routes: ${'$'}routes, characteristics: ${'$'}characteristics) {
            newSampleCount deletedSampleCount updatedDayCount deletedSamples { healthType startedAt endedAt } unmatchedRouteHealthSampleIds healthErasedAt
          }
        }
        """
    const val HEALTH_TYPES = """
        query (${'$'}ownerUserId: String) { ListHealthTypes(ownerUserId: ${'$'}ownerUserId) { $TYPE_FIELDS } }
        """
    const val HEALTH_SUMMARY = """
        query (${'$'}healthType: String!, ${'$'}fromDay: String!, ${'$'}toDay: String!, ${'$'}period: String!, ${'$'}ownerUserId: String) {
          ReadHealthSummary(healthType: ${'$'}healthType, fromDay: ${'$'}fromDay, toDay: ${'$'}toDay, period: ${'$'}period, ownerUserId: ${'$'}ownerUserId) {
            healthType quantityUnit summaryKind periods { $PERIOD_FIELDS }
          }
        }
        """
    const val HEALTH_SAMPLES = """
        query (${'$'}healthType: String!, ${'$'}from: DateTime, ${'$'}to: DateTime, ${'$'}limitCount: Int, ${'$'}offset: Int) {
          ListHealthSamples(healthType: ${'$'}healthType, from: ${'$'}from, to: ${'$'}to, limitCount: ${'$'}limitCount, offset: ${'$'}offset) {
            samples { $SAMPLE_FIELDS } totalCount nextOffset
          }
        }
        """
    const val HEALTH_ROUTE = """
        query (${'$'}healthSampleId: String!) { ReadHealthRoute(healthSampleId: ${'$'}healthSampleId) { healthSampleId routePoints } }
        """
    const val HEALTH_SETTINGS = """
        query { ReadHealthSettings { $SETTINGS_FIELDS } }
        """
    const val UPDATE_HEALTH_SETTINGS = """
        mutation (${'$'}canAgentReadHealth: Boolean!) { UpdateHealthSettings(canAgentReadHealth: ${'$'}canAgentReadHealth) { $SETTINGS_FIELDS } }
        """
    const val ERASE_HEALTH_DATA = """
        mutation (${'$'}healthType: String) { EraseHealthData(healthType: ${'$'}healthType) { erasedSampleCount } }
        """

    // Sharing Health by category with a circle or a person, and seeing the people who share with the caller.
    const val HEALTH_SHARES = """
        query { ListHealthShares { $SHARE_FIELDS } }
        """
    const val SET_HEALTH_SHARE = """
        mutation (${'$'}granteeUserId: String, ${'$'}granteeCircleId: String, ${'$'}healthCategories: [String!]!) {
          SetHealthShare(granteeUserId: ${'$'}granteeUserId, granteeCircleId: ${'$'}granteeCircleId, healthCategories: ${'$'}healthCategories) { $SHARE_FIELDS }
        }
        """
}

@Serializable private class ReportHealthAnswer(@SerialName("ReportHealth") val result: ReportHealthResult)
@Serializable private class ListHealthTypesAnswer(@SerialName("ListHealthTypes") val types: List<HealthTypeView>)
@Serializable private class ReadHealthSummaryAnswer(@SerialName("ReadHealthSummary") val summary: HealthSummaryView)
@Serializable private class ListHealthSamplesAnswer(@SerialName("ListHealthSamples") val page: HealthSamplePage)
@Serializable private class ReadHealthRouteAnswer(@SerialName("ReadHealthRoute") val route: HealthRouteView)
@Serializable private class ReadHealthSettingsAnswer(@SerialName("ReadHealthSettings") val settings: HealthSettingsView)
@Serializable private class UpdateHealthSettingsAnswer(@SerialName("UpdateHealthSettings") val settings: HealthSettingsView)
@Serializable private class EraseHealthDataAnswer(@SerialName("EraseHealthData") val result: EraseHealthDataResult)
@Serializable private class ListHealthSharesAnswer(@SerialName("ListHealthShares") val shares: List<HealthShareView>)
@Serializable private class SetHealthShareAnswer(@SerialName("SetHealthShare") val share: HealthShareView? = null)

/**
 * Hands the server a batch. Safe to send twice: the server knows a sample by the platform's identifier and replaces a
 * day's summary. With every call goes the last erase of all the person's Health that the phone knows of
 * ([healthErasedAt], as the server wrote it, null if it knows of none): a person who erased everything after that is
 * not undone by a batch still on its way, so the server stores nothing from a phone that has not heard of its latest
 * erase, and answers with the time of that erase.
 */
suspend fun Connection.reportHealth(batch: HealthBatch, agentPhoneId: String? = null, healthErasedAt: String?): ReportHealthResult {
    val variables = batch.variables.toMutableMap()
    agentPhoneId?.let { variables["agentPhoneId"] = JsonPrimitive(it) }
    variables["healthErasedAt"] = healthErasedAt?.let { text -> JsonPrimitive(text) } ?: JsonNull
    return execute(HealthDocuments.REPORT_HEALTH, ReportHealthAnswer.serializer(), variables).result
}

/** The types the person has data for, or somebody sharing with them has (only the ones in the categories shared). */
suspend fun Connection.healthTypes(ownerUserId: String? = null): List<HealthTypeView> {
    val variables = buildMap { ownerUserId?.let { put("ownerUserId", JsonPrimitive(it)) } }
    return execute(HealthDocuments.HEALTH_TYPES, ListHealthTypesAnswer.serializer(), variables).types
}

/** One type over a range of days ("YYYY-MM-DD"), a row for each period. */
suspend fun Connection.healthSummary(
    healthType: String, fromDay: String, toDay: String, period: HealthPeriod = HealthPeriod.Day, ownerUserId: String? = null,
): HealthSummaryView {
    val variables = buildMap {
        put("healthType", JsonPrimitive(healthType))
        put("fromDay", JsonPrimitive(fromDay))
        put("toDay", JsonPrimitive(toDay))
        put("period", JsonPrimitive(period.serverWord))
        ownerUserId?.let { put("ownerUserId", JsonPrimitive(it)) }
    }
    return execute(HealthDocuments.HEALTH_SUMMARY, ReadHealthSummaryAnswer.serializer(), variables).summary
}

/** A page of the person's own readings of a type, newest first. */
suspend fun Connection.healthSamples(healthType: String, from: Instant? = null, to: Instant? = null, limitCount: Int? = null, offset: Int = 0): HealthSamplePage {
    val variables = buildMap {
        put("healthType", JsonPrimitive(healthType))
        put("offset", JsonPrimitive(offset))
        from?.let { put("from", JsonPrimitive(ServerTime.text(it))) }
        to?.let { put("to", JsonPrimitive(ServerTime.text(it))) }
        limitCount?.let { put("limitCount", JsonPrimitive(it)) }
    }
    return execute(HealthDocuments.HEALTH_SAMPLES, ListHealthSamplesAnswer.serializer(), variables).page
}

/** The path of one of the person's own workouts. */
suspend fun Connection.healthRoute(healthSampleId: String): HealthRouteView =
    execute(HealthDocuments.HEALTH_ROUTE, ReadHealthRouteAnswer.serializer(), mapOf("healthSampleId" to JsonPrimitive(healthSampleId))).route

suspend fun Connection.healthSettings(): HealthSettingsView =
    execute(HealthDocuments.HEALTH_SETTINGS, ReadHealthSettingsAnswer.serializer()).settings

/** Lets the agent read Health data, or stops it. */
suspend fun Connection.updateHealthSettings(canAgentReadHealth: Boolean): HealthSettingsView =
    execute(HealthDocuments.UPDATE_HEALTH_SETTINGS, UpdateHealthSettingsAnswer.serializer(), mapOf("canAgentReadHealth" to JsonPrimitive(canAgentReadHealth))).settings

/** Erases one type's samples, summaries and routes, or with no type everything Health holds for the person (and the phone then stops). */
suspend fun Connection.eraseHealthData(healthType: String? = null): EraseHealthDataResult {
    val variables = buildMap { healthType?.let { put("healthType", JsonPrimitive(it)) } }
    return execute(HealthDocuments.ERASE_HEALTH_DATA, EraseHealthDataAnswer.serializer(), variables).result
}

/** The caller's own shares, and the ones made with them. */
suspend fun Connection.healthShares(): List<HealthShareView> =
    execute(HealthDocuments.HEALTH_SHARES, ListHealthSharesAnswer.serializer()).shares

/** Shares the categories with a person or a circle (one of the two); an empty list takes the share back, and then the answer is null. */
suspend fun Connection.setHealthShare(granteeUserId: String?, granteeCircleId: String?, healthCategories: List<HealthCategory>): HealthShareView? {
    val variables = mapOf(
        "granteeUserId" to (granteeUserId?.let { JsonPrimitive(it) } ?: JsonNull),
        "granteeCircleId" to (granteeCircleId?.let { JsonPrimitive(it) } ?: JsonNull),
        "healthCategories" to JsonArray(healthCategories.map { category -> JsonPrimitive(category.serverWord) }),
    )
    return execute(HealthDocuments.SET_HEALTH_SHARE, SetHealthShareAnswer.serializer(), variables).share
}
