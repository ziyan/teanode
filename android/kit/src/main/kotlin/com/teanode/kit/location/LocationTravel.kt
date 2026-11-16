package com.teanode.kit.location

import com.teanode.kit.Connection
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import java.time.DayOfWeek
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.temporal.IsoFields
import java.time.temporal.TemporalAdjusters

// How the person got from one place to the next: the trips between visits, the legs a trip is made of, each with a way
// of travel, and the distance traveled by each way over days, weeks, months and years. Mirrors LocationTravel.swift.

/** TravelMode is a way of travel. */
enum class TravelMode(val serverWord: String) {
    Walk("walk"),
    Run("run"),
    Bike("bike"),
    Car("car"),
    Bus("bus"),
    Train("train"),
    Boat("boat"),
    Plane("plane");

    companion object {
        fun fromServerWord(word: String): TravelMode? = entries.firstOrNull { mode -> mode.serverWord == word }
    }
}

/** TripLeg is a part of a trip with one way of travel. */
@Serializable
data class TripLeg(
    val id: String,
    /** RFC 3339. */
    val startedAt: String,
    val endedAt: String,
    val durationSeconds: Double,
    val distanceMeters: Double,
    /** One of [TravelMode]'s words, as the person corrected it if they did. */
    val travelMode: String,
    /** The way the server worked out, whatever the person corrected. */
    val detectedTravelMode: String = travelMode,
    val isTravelModeCorrected: Boolean = false,
    /** "motion" or "speed". */
    val detectionSource: String = "motion",
) {
    val startedInstant: Instant? get() = ServerTime.instant(startedAt)
    val endedInstant: Instant? get() = ServerTime.instant(endedAt)

    /** The way of travel, when this version knows it. */
    val mode: TravelMode? get() = TravelMode.fromServerWord(travelMode)
}

/** Trip is the travel between two visits. */
@Serializable
data class Trip(
    val id: String,
    val startedAt: String,
    val endedAt: String,
    val durationSeconds: Double,
    val distanceMeters: Double,
    /** The places of the visits either side; null when one is gone. */
    val fromPlace: LocationPlace? = null,
    val toPlace: LocationPlace? = null,
    val legs: List<TripLeg>,
) {
    val startedInstant: Instant? get() = ServerTime.instant(startedAt)
    val endedInstant: Instant? get() = ServerTime.instant(endedAt)
}

@Serializable
data class TripPage(val trips: List<Trip>, val nextOffset: Int? = null)

/** TravelTotal is how far the person traveled one way in one period. */
@Serializable
data class TravelTotal(
    /** The first day of the period, as YYYY-MM-DD. */
    val periodStartDay: String,
    val travelMode: String,
    val distanceMeters: Double,
    val durationSeconds: Double,
    val legCount: Int,
)

/** TravelPeriod is one period of the Travel screen: the distance by each way. */
data class TravelPeriod(
    /** The first day of the period, as YYYY-MM-DD. */
    val periodStartDay: String,
    /** The ways traveled, farthest first. */
    val totals: List<TravelTotal>,
) {
    val distanceMeters: Double get() = totals.sumOf { total -> total.distanceMeters }
    val durationSeconds: Double get() = totals.sumOf { total -> total.durationSeconds }

    /** The first day of the period, or null when the text is not a day. */
    fun startDay(): LocalDate? = runCatching { LocalDate.parse(periodStartDay) }.getOrNull()
}

/** TravelPeriodKind is the length of the periods the totals are added up in. */
enum class TravelPeriodKind(val serverWord: String) {
    Day("day"),
    Week("week"),
    Month("month"),
    Year("year");

    /** How many periods one page of the Travel screen covers. */
    val periodCountPerPage: Int
        get() = when (this) {
            Day -> 14
            Week -> 12
            Month -> 12
            Year -> 5
        }

    private fun startOf(day: LocalDate): LocalDate = when (this) {
        Day -> day
        Week -> day.with(TemporalAdjusters.previousOrSame(DayOfWeek.MONDAY))
        Month -> day.withDayOfMonth(1)
        Year -> day.withDayOfYear(1)
    }

    private fun plus(day: LocalDate, count: Long): LocalDate = when (this) {
        Day -> day.plusDays(count)
        Week -> day.plusWeeks(count)
        Month -> day.plusMonths(count)
        Year -> day.plusYears(count)
    }

    /**
     * The range a page covers: [periodCountPerPage] whole periods ending with the one that holds [now], moved back by
     * [pageBack] pages. Weeks begin on Monday, as the server counts them.
     */
    fun pageRange(pageBack: Int, now: Instant = Instant.now(), zone: ZoneId = ZoneId.systemDefault()): Pair<Instant, Instant> {
        val today = now.atZone(zone).toLocalDate()
        val currentEnd = plus(startOf(today), 1)
        val count = periodCountPerPage.toLong()
        val until = plus(currentEnd, -count * pageBack)
        val from = plus(until, -count)
        return from.atStartOfDay(zone).toInstant() to until.atStartOfDay(zone).toInstant()
    }
}

object TravelPeriods {
    /** The totals gathered by period, newest period first, the ways in each from the farthest. A way with no distance is left out. */
    fun group(totals: List<TravelTotal>): List<TravelPeriod> {
        val byDay = totals.filter { total -> total.distanceMeters > 0 }.groupBy { total -> total.periodStartDay }
        return byDay.keys.sortedDescending().map { day ->
            TravelPeriod(
                day,
                byDay.getValue(day).sortedWith(compareByDescending<TravelTotal> { total -> total.distanceMeters }.thenByDescending { total -> total.travelMode }),
            )
        }
    }

    /** The trips that lie between visits, placed on a day by when they began, newest first. */
    fun trips(trips: List<Trip>, from: Instant, until: Instant): List<Trip> =
        trips.filter { trip ->
            val started = trip.startedInstant ?: return@filter false
            !started.isBefore(from) && started.isBefore(until)
        }.sortedByDescending { trip -> trip.startedInstant ?: Instant.MIN }
}

object TravelDocuments {
    const val TRIPS = """
        query (${'$'}userId: String, ${'$'}from: DateTime!, ${'$'}until: DateTime!, ${'$'}offset: Int, ${'$'}limitCount: Int) {
          ListTrips(userId: ${'$'}userId, from: ${'$'}from, until: ${'$'}until, offset: ${'$'}offset, limitCount: ${'$'}limitCount) {
            trips {
              id startedAt endedAt durationSeconds distanceMeters
              fromPlace {
                id userId placeName isNamedByPerson placeLabel latitudeDegrees longitudeDegrees radiusMeters osmType osmId osmName osmCategory addressText isAddressTyped
                visitCount lastVisitAt namingErrorMessage createdAt updatedAt
              }
              toPlace {
                id userId placeName isNamedByPerson placeLabel latitudeDegrees longitudeDegrees radiusMeters osmType osmId osmName osmCategory addressText isAddressTyped
                visitCount lastVisitAt namingErrorMessage createdAt updatedAt
              }
              legs { id startedAt endedAt durationSeconds distanceMeters travelMode detectedTravelMode isTravelModeCorrected detectionSource }
            }
            nextOffset
          }
        }
        """
    const val SET_TRIP_LEG_TRAVEL_MODE = """
        mutation (${'$'}tripLegId: String!, ${'$'}travelMode: String) {
          SetTripLegTravelMode(tripLegId: ${'$'}tripLegId, travelMode: ${'$'}travelMode) {
            id startedAt endedAt durationSeconds distanceMeters travelMode detectedTravelMode isTravelModeCorrected detectionSource
          }
        }
        """
    const val TRAVEL_TOTALS = """
        query (${'$'}userId: String, ${'$'}period: String!, ${'$'}from: DateTime!, ${'$'}until: DateTime!) {
          ListTravelTotals(userId: ${'$'}userId, period: ${'$'}period, from: ${'$'}from, until: ${'$'}until) {
            periodStartDay travelMode distanceMeters durationSeconds legCount
          }
        }
        """
}

@Serializable private class ListTripsAnswer(@SerialName("ListTrips") val page: TripPage)
@Serializable private class SetTripLegTravelModeAnswer(@SerialName("SetTripLegTravelMode") val leg: TripLeg)
@Serializable private class ListTravelTotalsAnswer(@SerialName("ListTravelTotals") val totals: List<TravelTotal>)

/** The trips that began in a range, newest first, a page at a time. The person's own, or somebody sharing their history with the caller. */
suspend fun Connection.trips(userId: String? = null, from: Instant, until: Instant, offset: Int = 0, limitCount: Int? = null): TripPage {
    val variables = buildMap {
        put("from", JsonPrimitive(ServerTime.text(from)))
        put("until", JsonPrimitive(ServerTime.text(until)))
        userId?.let { put("userId", JsonPrimitive(it)) }
        if (offset > 0) put("offset", JsonPrimitive(offset))
        limitCount?.let { put("limitCount", JsonPrimitive(it)) }
    }
    return execute(TravelDocuments.TRIPS, ListTripsAnswer.serializer(), variables).page
}

/** Corrects the way of travel of one of the person's own legs; with none, takes the correction back so the way the server detected counts again. */
suspend fun Connection.setTripLegTravelMode(tripLegId: String, travelMode: TravelMode?): TripLeg {
    val variables = mapOf("tripLegId" to JsonPrimitive(tripLegId), "travelMode" to (travelMode?.let { JsonPrimitive(it.serverWord) } ?: JsonNull))
    return execute(TravelDocuments.SET_TRIP_LEG_TRAVEL_MODE, SetTripLegTravelModeAnswer.serializer(), variables).leg
}

/** How far was traveled by each way in each period of a range: the person's own, or somebody sharing their history with the caller. */
suspend fun Connection.travelTotals(userId: String? = null, period: TravelPeriodKind, from: Instant, until: Instant): List<TravelTotal> {
    val variables = buildMap {
        put("period", JsonPrimitive(period.serverWord))
        put("from", JsonPrimitive(ServerTime.text(from)))
        put("until", JsonPrimitive(ServerTime.text(until)))
        userId?.let { put("userId", JsonPrimitive(it)) }
    }
    return execute(TravelDocuments.TRAVEL_TOTALS, ListTravelTotalsAnswer.serializer(), variables).totals
}
