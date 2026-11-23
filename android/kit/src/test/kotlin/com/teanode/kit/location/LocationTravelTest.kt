package com.teanode.kit.location

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.Instant
import java.time.ZoneId

/** The trips between visits, the ways of travel, the totals, and the motion activity the phone queues for the server. */
class LocationTravelTest {
    private val utc = ZoneId.of("UTC")

    private val legAnswer = """
        {"id":"leg-1","startedAt":"2024-01-02T08:05:00Z","endedAt":"2024-01-02T08:15:00Z","durationSeconds":600,"distanceMeters":640,
         "travelMode":"bus","detectedTravelMode":"car","isTravelModeCorrected":true,"detectionSource":"motion"}
        """

    private val placeAnswer = """
        {"id":"place-1","userId":"user-1","placeName":"Fixture Park","isNamedByPerson":false,"placeLabel":"","latitudeDegrees":10.5,
         "longitudeDegrees":20.5,"radiusMeters":75,"osmType":"way","osmId":123456789012,"osmName":"Fixture Park","osmCategory":"leisure=park",
         "addressText":"","isAddressTyped":false,"visitCount":3,"lastVisitAt":"2024-01-02T10:00:00Z","namingErrorMessage":"",
         "createdAt":"2024-01-01T00:00:00Z","updatedAt":"2024-01-02T10:00:00Z"}
        """

    private fun activity(offsetSeconds: Long, kind: String = "walking", confidence: String = "high") =
        ReportedActivity(startedAt = instantOf("2024-01-02T00:00:00Z").plusSeconds(offsetSeconds), activityKind = kind, activityConfidence = confidence)

    private val reportAnswer = """{"data":{"ReportLocation":{"newPointCount":0,"newVisitCount":0,"updatedVisitCount":0}}}"""

    // Ways of travel

    @Test
    fun eachWayOfTravelHasItsWord() {
        assertEquals(listOf("walk", "run", "bike", "car", "bus", "train", "boat", "plane"), TravelMode.entries.map { mode -> mode.serverWord })
        assertEquals(TravelMode.Bike, TravelMode.fromServerWord("bike"))
        assertNull(TravelMode.fromServerWord("hovercraft"))
    }

    // Reading

    @Test
    fun readsTripsWithTheirPlacesAndLegs() = runBlocking {
        val body = """
            {"data":{"ListTrips":{"trips":[{"id":"trip-1","startedAt":"2024-01-02T08:05:00Z","endedAt":"2024-01-02T08:40:00Z",
             "durationSeconds":2100,"distanceMeters":12400,"fromPlace":$placeAnswer,"toPlace":null,"legs":[$legAnswer]}],"nextOffset":null}}}
            """
        val (connection, fake) = connectionAnswering(body)
        val page = connection.trips(from = instantOf("2024-01-02T00:00:00Z"), until = instantOf("2024-01-03T00:00:00Z"))
        assertEquals(1, page.trips.size)
        val trip = page.trips[0]
        assertEquals("Fixture Park", trip.fromPlace?.placeName)
        assertNull(trip.toPlace)
        assertEquals(TravelMode.Bus, trip.legs[0].mode)
        assertEquals("car", trip.legs[0].detectedTravelMode)
        assertTrue(trip.legs[0].isTravelModeCorrected)
        assertEquals(instantOf("2024-01-02T08:05:00Z"), trip.startedInstant)
        val variables = sentVariables(fake)
        assertEquals(JsonPrimitive("2024-01-02T00:00:00Z"), variables["from"])
        assertEquals(JsonPrimitive("2024-01-03T00:00:00Z"), variables["until"])
        assertNull(variables["userId"])
        assertNull(variables["offset"])
    }

    @Test
    fun tripsOfAnotherPersonAskForThemAndPage() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"ListTrips":{"trips":[],"nextOffset":null}}}""")
        connection.trips("user-2", instantOf("2024-01-02T00:00:00Z"), instantOf("2024-01-03T00:00:00Z"), 20, 20)
        val variables = sentVariables(fake)
        assertEquals(JsonPrimitive("user-2"), variables["userId"])
        assertEquals(JsonPrimitive(20), variables["offset"])
        assertEquals(JsonPrimitive(20), variables["limitCount"])
    }

    @Test
    fun correctingALegSendsTheWayAndClearingSendsNull() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"SetTripLegTravelMode":$legAnswer}}""")
        assertEquals("bus", connection.setTripLegTravelMode("leg-1", TravelMode.Bus).travelMode)
        assertEquals(mapOf("tripLegId" to JsonPrimitive("leg-1"), "travelMode" to JsonPrimitive("bus")), sentVariables(fake, 0))
        connection.setTripLegTravelMode("leg-1", null)
        assertEquals(mapOf("tripLegId" to JsonPrimitive("leg-1"), "travelMode" to JsonNull), sentVariables(fake, 1))
    }

    @Test
    fun readsTravelTotalsForAPeriod() = runBlocking {
        val body = """
            {"data":{"ListTravelTotals":[{"periodStartDay":"2024-01-01","travelMode":"car","distanceMeters":11800,"durationSeconds":1080,"legCount":1},
             {"periodStartDay":"2024-01-01","travelMode":"walk","distanceMeters":600,"durationSeconds":480,"legCount":2}]}}
            """
        val (connection, fake) = connectionAnswering(body)
        val totals = connection.travelTotals(period = TravelPeriodKind.Week, from = instantOf("2024-01-01T00:00:00Z"), until = instantOf("2024-01-08T00:00:00Z"))
        assertEquals(2, totals.size)
        assertEquals(2, totals[1].legCount)
        val variables = sentVariables(fake)
        assertEquals(JsonPrimitive("week"), variables["period"])
        assertEquals(JsonPrimitive("2024-01-01T00:00:00Z"), variables["from"])
        assertNull(variables["userId"])
        connection.travelTotals("user-2", TravelPeriodKind.Year, instantOf("2023-01-01T00:00:00Z"), instantOf("2024-01-01T00:00:00Z"))
        assertEquals(JsonPrimitive("user-2"), sentVariables(fake, 1)["userId"])
    }

    @Test
    fun totalsAreGatheredByPeriodNewestFirstWithTheFarthestWayFirst() {
        val totals = listOf(
            TravelTotal("2024-01-01", "walk", 600.0, 480.0, 2), TravelTotal("2024-01-08", "bike", 5000.0, 1200.0, 1),
            TravelTotal("2024-01-01", "car", 11800.0, 1080.0, 1), TravelTotal("2024-01-08", "bus", 0.0, 0.0, 0),
        )
        val periods = TravelPeriods.group(totals)
        assertEquals(listOf("2024-01-08", "2024-01-01"), periods.map { period -> period.periodStartDay })
        assertEquals(listOf("bike"), periods[0].totals.map { total -> total.travelMode })
        assertEquals(listOf("car", "walk"), periods[1].totals.map { total -> total.travelMode })
        assertEquals(12400.0, periods[1].distanceMeters)
        assertEquals(1560.0, periods[1].durationSeconds)
    }

    @Test
    fun aPeriodStartsAtItsFirstDay() {
        assertEquals(java.time.LocalDate.of(2024, 1, 8), TravelPeriod("2024-01-08", emptyList()).startDay())
        assertNull(TravelPeriod("soon", emptyList()).startDay())
    }

    @Test
    fun pagesOfPeriodsEndWithTheCurrentOneAndMoveBackWholePages() {
        // A Wednesday.
        val now = instantOf("2024-01-10T15:00:00Z")
        val week = TravelPeriodKind.Week.pageRange(0, now, utc)
        assertEquals(instantOf("2024-01-15T00:00:00Z"), week.second)
        assertEquals(instantOf("2023-10-23T00:00:00Z"), week.first)
        assertEquals(week.first, TravelPeriodKind.Week.pageRange(1, now, utc).second)
        val day = TravelPeriodKind.Day.pageRange(0, now, utc)
        assertEquals(instantOf("2024-01-11T00:00:00Z"), day.second)
        assertEquals(instantOf("2023-12-28T00:00:00Z"), day.first)
        val month = TravelPeriodKind.Month.pageRange(0, now, utc)
        assertEquals(instantOf("2024-02-01T00:00:00Z"), month.second)
        assertEquals(instantOf("2023-02-01T00:00:00Z"), month.first)
        val year = TravelPeriodKind.Year.pageRange(0, now, utc)
        assertEquals(instantOf("2025-01-01T00:00:00Z"), year.second)
        assertEquals(instantOf("2020-01-01T00:00:00Z"), year.first)
    }

    @Test
    fun theTravelDocumentsAskForTheirFields() {
        assertTrue(LocationDocuments.REPORT_LOCATION.contains("activities: \$activities"))
        assertTrue(TravelDocuments.TRIPS.contains("legs { id startedAt endedAt"))
        assertTrue(TravelDocuments.SET_TRIP_LEG_TRAVEL_MODE.contains("SetTripLegTravelMode"))
        assertTrue(TravelDocuments.TRAVEL_TOTALS.contains("ListTravelTotals"))
    }

    @Test
    fun aBatchWithoutActivitiesSendsNoActivitiesVariableOrArgument() = runBlocking {
        val (connection, fake) = connectionAnswering(reportAnswer)
        val batch = LocationBatch(emptyList(), emptyList())
        assertNull(batch.variables["activities"])
        assertFalse(LocationDocuments.REPORT_LOCATION_WITHOUT_ACTIVITIES.contains("activities"))
        connection.reportLocation(batch)
        assertNull(sentVariables(fake)["activities"])
        assertFalse(fake.sent[0].body!!.toString(Charsets.UTF_8).contains("activities"))
    }

    // The outbox

    @Test
    fun anActivityIsQueuedOncePerStartAndKeptInOrder() {
        val outbox = LocationOutbox("account")
        val now = instantOf("2024-01-02T00:00:00Z")
        outbox.addActivities(listOf(activity(600, "automotive"), activity(0), activity(300, "running")), now)
        assertEquals(listOf("walking", "running", "automotive"), outbox.activities.map { each -> each.activityKind })
        assertFalse(outbox.isEmpty)
        // Asking again from where it last asked brings the same ones back, and the platform's times have fractions the server and the file drop.
        outbox.addActivities(listOf(activity(300, "running").copy(startedAt = instantOf("2024-01-02T00:05:00Z").plusMillis(700)), activity(900, "stationary")), now)
        assertEquals(listOf("walking", "running", "automotive", "stationary"), outbox.activities.map { each -> each.activityKind })
    }

    @Test
    fun anActivityTheServerWouldRefuseIsNotQueued() {
        val outbox = LocationOutbox("account")
        outbox.addActivities(
            listOf(
                activity(0, "flying"), activity(10, confidence = "certain"),
                ReportedActivity(startedAt = Instant.now().plusSeconds(2 * 86_400), activityKind = "walking", activityConfidence = "low"),
            ),
        )
        assertTrue(outbox.activities.isEmpty())
        assertTrue(outbox.isEmpty)
    }

    @Test
    fun theOutboxKeepsTheNewest5000Activities() {
        val outbox = LocationOutbox("account")
        outbox.addActivities((0 until (LocationOutbox.ACTIVITY_COUNT_MOST + 7)).map { index -> activity(index * 10L) }, instantOf("2024-01-02T00:00:00Z"))
        assertEquals(LocationOutbox.ACTIVITY_COUNT_MOST, outbox.activities.size)
        assertEquals(instantOf("2024-01-02T00:00:00Z").plusSeconds(70), outbox.activities.first().startedAt)
    }

    @Test
    fun activitiesLeaveInBatchesOf500AndStayUntilTheServerAnswered() {
        val outbox = LocationOutbox("account")
        val now = instantOf("2024-01-02T00:00:00Z")
        outbox.addActivities((0 until 620).map { index -> activity(index * 10L) }, now)
        outbox.addPoint(ReportedPoint(latitudeDegrees = 10.5, longitudeDegrees = 20.5, accuracyMeters = 5.0, measuredAt = now), now)
        val first = outbox.nextBatch()
        assertEquals(500, first.activities.size)
        assertEquals(1, first.points.size)
        // What is added while the batch is on its way stays.
        outbox.addActivities(listOf(activity(99_000)))
        outbox.remove(first)
        assertTrue(outbox.points.isEmpty())
        val second = outbox.nextBatch()
        assertEquals(121, second.activities.size)
        assertEquals(now.plusSeconds(5_000), second.activities.first().startedAt)
        outbox.remove(second)
        assertTrue(outbox.isEmpty)
    }

    @Test
    fun reportingActivitiesSendsThemWithTheirTimes() = runBlocking {
        val (connection, fake) = connectionAnswering(reportAnswer)
        val outbox = LocationOutbox("account")
        outbox.addActivities(listOf(activity(0, "cycling", "medium")), instantOf("2024-01-02T00:00:00Z"))
        connection.reportLocation(outbox.nextBatch())
        val variables = sentVariables(fake)
        assertEquals(
            JsonArray(
                listOf(
                    JsonObject(
                        mapOf(
                            "startedAt" to JsonPrimitive("2024-01-02T00:00:00Z"), "activityKind" to JsonPrimitive("cycling"),
                            "activityConfidence" to JsonPrimitive("medium"),
                        ),
                    ),
                ),
            ),
            variables["activities"],
        )
        assertEquals(JsonArray(emptyList()), variables["points"])
        assertEquals(JsonArray(emptyList()), variables["visits"])
    }

    @Test
    fun discardingWhatIsUnsentDiscardsActivitiesToo() {
        val outbox = LocationOutbox("account")
        outbox.addActivities(listOf(activity(3600), activity(30 * 3600)))
        outbox.discardUnsent(instantOf("2024-01-02T00:00:00Z"), instantOf("2024-01-03T00:00:00Z"))
        assertEquals(1, outbox.activities.size)
        outbox.discardUnsent()
        assertTrue(outbox.activities.isEmpty())
    }

    @Test
    fun anOutboxKeptBeforeActivitiesStillReadsBack() {
        val old = """{"accountKey":"account","points":[],"visits":[],"openRegionVisits":{}}"""
        val outbox = LocationOutbox.decoded(old)
        assertEquals("account", outbox.accountKey)
        assertTrue(outbox.activities.isEmpty())
        outbox.addActivities(listOf(activity(0)), instantOf("2024-01-02T00:00:00Z"))
        val readBack = LocationOutbox.decoded(outbox.encoded())
        assertEquals(outbox.activities, readBack.activities)
        assertEquals(outbox.encoded(), readBack.encoded())
    }

    @Test
    fun aBatchWithActivitiesIsSplitUntilTheOneEntryIsFound() {
        fun sequenced(index: Int) = ReportedActivity(sequence = index, startedAt = Instant.ofEpochSecond(index.toLong()), activityKind = "walking", activityConfidence = "low")
        val point = ReportedPoint(sequence = 1, latitudeDegrees = 10.5, longitudeDegrees = 20.5, accuracyMeters = 5.0, measuredAt = Instant.now())
        val batch = LocationBatch(listOf(point), emptyList(), (2..5).map { index -> sequenced(index) })
        assertEquals(5, batch.entryCount)
        val apart = batch.split()!!
        assertEquals(1, apart.first.points.size)
        assertTrue(apart.first.activities.isEmpty())
        assertEquals(4, apart.second.activities.size)
        val halves = apart.second.split()!!
        assertEquals(listOf(2, 3), halves.first.activities.map { each -> each.sequence })
        assertEquals(listOf(4, 5), halves.second.activities.map { each -> each.sequence })
        assertNull(LocationBatch(emptyList(), emptyList(), listOf(sequenced(1))).split())
    }

    // The timeline

    private fun visit(id: String, arrivedAt: String, departedAt: String?): PlaceVisit {
        val place = LocationPlace(id = "place-1", placeName = "Fixture Park", latitudeDegrees = 10.5, longitudeDegrees = 20.5)
        return PlaceVisit(id = id, place = place, arrivedAt = arrivedAt, departedAt = departedAt, latitudeDegrees = 10.5, longitudeDegrees = 20.5)
    }

    private fun trip(id: String, startedAt: String, endedAt: String) = Trip(
        id = id, startedAt = startedAt, endedAt = endedAt, durationSeconds = 600.0, distanceMeters = 1200.0,
        legs = listOf(TripLeg("$id-leg", startedAt, endedAt, 600.0, 1200.0, "walk")),
    )

    @Test
    fun aTripSitsBetweenTheTwoVisitsItJoinsOnTheDayItBegan() {
        val visits = listOf(visit("visit-1", "2024-01-02T09:00:00Z", "2024-01-02T10:00:00Z"), visit("visit-2", "2024-01-02T10:20:00Z", null))
        val trips = listOf(trip("trip-1", "2024-01-02T10:00:00Z", "2024-01-02T10:20:00Z"))
        val days = LocationDays.group(visits, trips = trips, zone = utc)
        assertEquals(1, days.size)
        assertEquals(listOf("visit:visit-2", "trip:trip-1", "visit:visit-1"), days[0].entries.map { entry -> entry.id })
        assertEquals(listOf("visit-2", "visit-1"), days[0].visits.map { each -> each.id })
        assertEquals(listOf("visit:visit-2", "trip:trip-1", "visit:visit-1"), LocationDays.entries(visits, emptyList(), trips).map { entry -> entry.id })
    }

    // A visit known only by its departure sits at that departure, the moment the trip leaving it starts: the trip still goes above it.
    @Test
    fun aTripGoesAboveTheVisitItLeavesWhenThatVisitHasOnlyADeparture() {
        val place = LocationPlace(id = "place-1", placeName = "Fixture Park", latitudeDegrees = 10.5, longitudeDegrees = 20.5)
        val visits = listOf(
            PlaceVisit(id = "visit-1", place = place, arrivedAt = null, departedAt = "2024-01-02T07:25:50Z", latitudeDegrees = 10.5, longitudeDegrees = 20.5),
            visit("visit-2", "2024-01-02T07:29:37Z", "2024-01-02T07:35:52Z"),
        )
        val trips = listOf(trip("trip-1", "2024-01-02T07:25:50Z", "2024-01-02T07:29:37Z"))
        assertEquals(listOf("visit:visit-2", "trip:trip-1", "visit:visit-1"), LocationDays.group(visits, trips = trips, zone = utc)[0].entries.map { entry -> entry.id })
        assertEquals(listOf("visit:visit-2", "trip:trip-1", "visit:visit-1"), LocationDays.entries(visits, emptyList(), trips).map { entry -> entry.id })
    }

    @Test
    fun aTripOfADayWithNoVisitStillMakesTheDay() {
        val days = LocationDays.group(
            listOf(visit("visit-1", "2024-01-01T20:00:00Z", "2024-01-02T07:00:00Z")),
            trips = listOf(trip("trip-1", "2024-01-02T07:00:00Z", "2024-01-02T07:20:00Z")), zone = utc,
        )
        assertEquals(listOf(listOf("trip:trip-1"), listOf("visit:visit-1")), days.map { day -> day.entries.map { entry -> entry.id } })
    }

    @Test
    fun tripsOfARangeAreThoseThatBeganInItNewestFirst() {
        val trips = listOf(
            trip("early", "2024-01-02T08:00:00Z", "2024-01-02T08:10:00Z"), trip("late", "2024-01-02T17:00:00Z", "2024-01-02T17:10:00Z"),
            trip("next-day", "2024-01-03T00:00:00Z", "2024-01-03T00:10:00Z"),
        )
        val kept = TravelPeriods.trips(trips, instantOf("2024-01-02T00:00:00Z"), instantOf("2024-01-03T00:00:00Z"))
        assertEquals(listOf("late", "early"), kept.map { each -> each.id })
        assertNotNull(kept)
    }
}
