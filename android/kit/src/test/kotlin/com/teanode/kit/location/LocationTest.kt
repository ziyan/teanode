package com.teanode.kit.location

import com.teanode.kit.TeaNodeError
import com.teanode.kit.TeaNodeJson
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.Duration
import java.time.Instant
import java.time.ZoneId

/** What the phone keeps of where the person goes: what the server's answers decode to, what each call sends, which positions are kept, how a visit is named, and what waits in the outbox. */
class LocationTest {
    private val utc = ZoneId.of("UTC")

    private val placeAnswer = """
        {"id":"place-1","userId":"user-1","placeName":"Fixture Park","isNamedByPerson":false,"placeLabel":"","latitudeDegrees":10.5,
         "longitudeDegrees":20.5,"radiusMeters":75,"osmType":"way","osmId":123456789012,"osmName":"Fixture Park","osmCategory":"leisure=park",
         "addressText":"","isAddressTyped":false,"visitCount":3,"lastVisitAt":"2024-01-02T10:00:00.123456789Z","namingErrorMessage":"",
         "createdAt":"2024-01-01T00:00:00Z","updatedAt":"2024-01-02T10:00:00Z"}
        """

    private val visitAnswer = """
        {"id":"visit-1","arrivedAt":"2024-01-02T09:00:00Z","departedAt":null,"durationSeconds":null,"latitudeDegrees":10.5,
         "longitudeDegrees":20.5,"accuracyMeters":30,"visitSource":"visit","place":$placeAnswer}
        """

    // Reading

    @Test
    fun readsWhereThePersonIsWithTheVisitInProgress() = runBlocking {
        val (connection, fake) = connectionAnswering(
            """
            {"data":{"GetLocation":{"userId":"user-1","username":"robin","name":null,"avatarUrl":null,"avatarColorIndex":3,
             "shareLevel":"owner",
             "latestPoint":{"latitudeDegrees":10.5,"longitudeDegrees":20.5,"accuracyMeters":12.5,"altitudeMeters":null,
              "speedMetersPerSecond":null,"courseDegrees":null,"measuredAt":"2024-01-02T09:30:00Z","locationSource":"phone"},
             "openVisit":$visitAnswer}}}
            """,
        )
        val location = connection.location()
        assertEquals("robin", location.displayName)
        assertEquals(LocationShareLevel.Owner, location.level)
        assertEquals(12.5, location.latestPoint?.accuracyMeters)
        assertNotNull(location.latestPoint?.measuredInstant)
        val open = location.openVisit!!
        assertTrue(open.isOpen)
        assertNull(open.visitDuration)
        assertEquals(123_456_789_012L, open.place.osmId)
        assertEquals(3, open.place.visitCount)
        assertNotNull(open.place.lastVisitInstant)
        assertFalse(open.place.isOfPersonsOwn)
        assertEquals(emptyMap<String, Any>(), sentVariables(fake))
    }

    @Test
    fun aVisitWithBothEndsHasADuration() {
        val both = TeaNodeJson.decodeFromString(PlaceVisit.serializer(), visitAnswer.replace("\"departedAt\":null", "\"departedAt\":\"2024-01-02T10:25:00Z\""))
        assertFalse(both.isOpen)
        assertEquals(Duration.ofMinutes(85), both.visitDuration)
        val told = TeaNodeJson.decodeFromString(PlaceVisit.serializer(), visitAnswer.replace("\"durationSeconds\":null", "\"durationSeconds\":60.5"))
        assertEquals(Duration.ofMillis(60_500), told.visitDuration)
    }

    @Test
    fun pagesOfVisitsAskForTheOffsetTheLastOneGave() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"ListLocationVisits":{"placeVisits":[$visitAnswer],"nextOffset":50}}}""")
        assertEquals(50, connection.locationVisits().nextOffset)
        connection.locationVisits(from = instantOf("2024-01-02T00:00:00Z"), until = instantOf("2024-01-03T00:00:00Z"), placeId = "place-1", offset = 50, limitCount = 50)
        assertEquals(emptyMap<String, Any>(), sentVariables(fake, 0))
        assertEquals(
            mapOf(
                "from" to JsonPrimitive("2024-01-02T00:00:00Z"), "until" to JsonPrimitive("2024-01-03T00:00:00Z"), "placeId" to JsonPrimitive("place-1"),
                "offset" to JsonPrimitive(50), "limitCount" to JsonPrimitive(50),
            ),
            sentVariables(fake, 1),
        )
    }

    @Test
    fun aVisitCarriesTheCountAndFirstPhotosOnlyWhenTheServerSendsThem() {
        val photo = """
            {"id":"photo-1","createdAt":"2024-01-02T09:05:00Z","photoKind":"picture","takenAt":"2024-01-02T09:10:00Z","takenAtOffsetMinutes":null,
             "widthPixels":10,"heightPixels":10,"durationMS":0,"latitude":null,"longitude":null,"placeName":"","cameraMake":"","cameraModel":"",
             "originalFilename":"fixture.jpg","isFavorite":false,"isScreenshot":false,"photoProcessingState":"ready","trashedAt":null,
             "smallPreviewUrl":"/api/v1/photos/photo-1/previews/small?v=1","largePreviewUrl":"","videoPreviewUrl":""}
            """
        val plain = TeaNodeJson.decodeFromString(PlaceVisit.serializer(), visitAnswer)
        assertNull(plain.photoCount)
        assertNull(plain.photos)
        val withPhotos = visitAnswer.replace("\"visitSource\":\"visit\",", "\"visitSource\":\"visit\",\"photoCount\":6,\"photos\":[$photo],")
        val visit = TeaNodeJson.decodeFromString(PlaceVisit.serializer(), withPhotos)
        assertEquals(6, visit.photoCount)
        assertEquals(listOf("photo-1"), visit.photos?.map { each -> each.id })
        assertEquals("/api/v1/photos/photo-1/previews/small?v=1", visit.photos?.first()?.smallPreviewUrl)
        assertTrue(LocationDocuments.LOCATION_VISITS.contains("photoCount"))
        assertTrue(LocationDocuments.LOCATION_VISITS.contains("photos {"))
    }

    @Test
    fun aPlaceKnowsWhetherItsAddressWasTyped() {
        assertFalse(TeaNodeJson.decodeFromString(LocationPlace.serializer(), placeAnswer).isAddressTyped)
        val typedAnswer = placeAnswer.replace("\"addressText\":\"\",\"isAddressTyped\":false,", "\"addressText\":\"3 Fixture Row\",\"isAddressTyped\":true,")
        val typed = TeaNodeJson.decodeFromString(LocationPlace.serializer(), typedAnswer)
        assertTrue(typed.isAddressTyped)
        assertEquals("3 Fixture Row", typed.addressText)
        assertTrue(LocationDocuments.SAVE_PLACE.contains("isAddressTyped"))
    }

    @Test
    fun readsPlacesAndNearbyPlaces() = runBlocking {
        val (placesConnection, _) = connectionAnswering("""{"data":{"ListPlaces":{"places":[$placeAnswer],"nextOffset":null}}}""")
        val page = placesConnection.locationPlaces()
        assertEquals(listOf("place-1"), page.places.map { each -> each.id })
        assertNull(page.nextOffset)

        val (nearbyConnection, fake) = connectionAnswering(
            """
            {"data":{"ListNearbyPlaces":[{"placeName":"Fixture Cafe","osmType":"node","osmId":42,"osmCategory":"amenity=cafe",
             "addressText":"1 Fixture Street","latitudeDegrees":10.5,"longitudeDegrees":20.5,"distanceMeters":40.5,"placeId":null}]}}
            """,
        )
        val found = nearbyConnection.nearbyPlaces(10.5, 20.5)
        assertEquals("cafe", found.first().categoryValue)
        assertEquals("node/42", found.first().id)
        // An older server sends neither field.
        assertEquals("", found.first().placeLabel)
        assertFalse(found.first().isNamedByPerson)
        assertNull(found.first().label)

        val (labeledConnection, _) = connectionAnswering(
            """
            {"data":{"ListNearbyPlaces":[{"placeName":"Fixture Park","osmType":"way","osmId":7,"osmCategory":"leisure=park",
             "addressText":"","latitudeDegrees":10.5,"longitudeDegrees":20.5,"distanceMeters":10,"placeId":"place-1",
             "placeLabel":"gym","isNamedByPerson":false}]}}
            """,
        )
        val labeled = labeledConnection.nearbyPlaces(10.5, 20.5)
        assertEquals(PlaceLabel.Gym, labeled.first().label)
        assertEquals(mapOf("latitudeDegrees" to JsonPrimitive(10.5), "longitudeDegrees" to JsonPrimitive(20.5)), sentVariables(fake))
    }

    // Changing

    @Test
    fun savesOnlyTheFieldsGivenAndTrimsTheName() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"SavePlace":$placeAnswer,"ForgetPlace":true}}""")
        connection.savePlace(PlaceChange(placeId = "place-1", placeName = "  Fixture Home ", placeLabel = "home"))
        connection.savePlace(PlaceChange(placeId = "place-1", placeLabel = ""))
        connection.savePlace(PlaceChange(placeId = "place-1", radiusMeters = 150.0))
        connection.forgetPlace("place-1")
        assertEquals(mapOf("placeId" to JsonPrimitive("place-1"), "placeName" to JsonPrimitive("Fixture Home"), "placeLabel" to JsonPrimitive("home")), sentVariables(fake, 0))
        assertEquals(mapOf("placeId" to JsonPrimitive("place-1"), "placeLabel" to JsonPrimitive("")), sentVariables(fake, 1))
        assertEquals(mapOf("placeId" to JsonPrimitive("place-1"), "radiusMeters" to JsonPrimitive(150.0)), sentVariables(fake, 2))
        assertEquals(mapOf("placeId" to JsonPrimitive("place-1")), sentVariables(fake, 3))
    }

    @Test
    fun forgetsOneVisitByItsID() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"ForgetPlaceVisit":true}}""")
        connection.forgetPlaceVisit("visit-1")
        assertEquals(mapOf("placeVisitId" to JsonPrimitive("visit-1")), sentVariables(fake))
    }

    @Test
    fun savesAnAddressWithItsPosition() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"SavePlace":$placeAnswer}}""")
        connection.savePlace(PlaceChange(placeId = "place-1", latitudeDegrees = 10.5, longitudeDegrees = 20.5, addressText = "  12 Elm Street, Exampleton "))
        connection.savePlace(PlaceChange(placeLabel = "home", latitudeDegrees = 10.5, longitudeDegrees = 20.5, addressText = "12 Elm Street, Exampleton"))
        assertEquals(
            mapOf(
                "placeId" to JsonPrimitive("place-1"), "latitudeDegrees" to JsonPrimitive(10.5), "longitudeDegrees" to JsonPrimitive(20.5),
                "addressText" to JsonPrimitive("12 Elm Street, Exampleton"),
            ),
            sentVariables(fake, 0),
        )
        // No place identifier: the server makes the place, or uses the one holding the position.
        assertEquals(
            mapOf(
                "placeLabel" to JsonPrimitive("home"), "latitudeDegrees" to JsonPrimitive(10.5), "longitudeDegrees" to JsonPrimitive(20.5),
                "addressText" to JsonPrimitive("12 Elm Street, Exampleton"),
            ),
            sentVariables(fake, 1),
        )
        assertTrue(LocationDocuments.SAVE_PLACE.contains("\$addressText: String"))
        assertTrue(LocationDocuments.SAVE_PLACE.contains("addressText: \$addressText"))
    }

    @Test
    fun searchesAddressesAnywhere() = runBlocking {
        val (connection, fake) = connectionAnswering(
            """
            {"data":{"SearchAddresses":[{"placeName":"12 Elm Street","osmType":"way","osmId":9001,"osmCategory":"building=house",
             "addressText":"12 Elm Street, Exampleton","latitudeDegrees":10.5,"longitudeDegrees":20.5,"distanceMeters":0,
             "placeId":null,"placeLabel":"","isNamedByPerson":false}]}}
            """,
        )
        val found = connection.searchAddresses("12 Elm Street", limitCount = 8)
        assertEquals(listOf("12 Elm Street, Exampleton"), found.map { each -> each.addressText })
        assertEquals(10.5, found.first().latitudeDegrees)
        assertNull(found.first().placeId)
        assertEquals(mapOf("query" to JsonPrimitive("12 Elm Street"), "limitCount" to JsonPrimitive(8)), sentVariables(fake))
        assertTrue(LocationDocuments.SEARCH_ADDRESSES.contains("placeId: \$placeId, limitCount: \$limitCount)"))
    }

    @Test
    fun searchesAddressesNearestToAPlace() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"SearchAddresses":[]}}""")
        connection.searchAddresses("12 Elm Street", placeId = "place-1", limitCount = 8)
        assertEquals(mapOf("query" to JsonPrimitive("12 Elm Street"), "placeId" to JsonPrimitive("place-1"), "limitCount" to JsonPrimitive(8)), sentVariables(fake))
    }

    @Test
    fun keepsAnAddressAsTypedWithoutMovingThePlace() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"SavePlace":$placeAnswer}}""")
        connection.savePlace(PlaceChange(placeId = "place-1", addressText = " 3 Fixture Row, Exampleton "))
        assertEquals(mapOf("placeId" to JsonPrimitive("place-1"), "addressText" to JsonPrimitive("3 Fixture Row, Exampleton")), sentVariables(fake))
    }

    @Test
    fun checksInAtAPositionWithOnlyItsCoordinates() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"CheckIn":$visitAnswer}}""")
        connection.checkIn(CheckInTarget.Position(10.5, 20.5))
        assertEquals(mapOf("latitudeDegrees" to JsonPrimitive(10.5), "longitudeDegrees" to JsonPrimitive(20.5)), sentVariables(fake))
    }

    @Test
    fun checksInAtAPositionWithHowPreciseItIs() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"CheckIn":$visitAnswer}}""")
        connection.checkIn(CheckInTarget.Position(10.5, 20.5, 850.0))
        // The phone could not tell: negative, so nothing is claimed.
        connection.checkIn(CheckInTarget.Position(10.5, 20.5, -1.0))
        assertEquals(
            mapOf("latitudeDegrees" to JsonPrimitive(10.5), "longitudeDegrees" to JsonPrimitive(20.5), "accuracyMeters" to JsonPrimitive(850.0)),
            sentVariables(fake, 0),
        )
        assertEquals(mapOf("latitudeDegrees" to JsonPrimitive(10.5), "longitudeDegrees" to JsonPrimitive(20.5)), sentVariables(fake, 1))
        assertTrue(LocationDocuments.CHECK_IN.contains("accuracyMeters: \$accuracyMeters"))
    }

    @Test
    fun checksInAtAPlaceOrAFoundPlace() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"CheckIn":$visitAnswer}}""")
        connection.checkIn(CheckInTarget.OwnPlace("place-1"))
        val found = FoundPlace("Fixture Cafe", "node", 42, "amenity=cafe", "1 Fixture Street", 10.5, 20.5, 40.0)
        connection.checkIn(CheckInTarget.Found(found))
        assertEquals(mapOf("placeId" to JsonPrimitive("place-1")), sentVariables(fake, 0))
        assertEquals(
            mapOf(
                "latitudeDegrees" to JsonPrimitive(10.5), "longitudeDegrees" to JsonPrimitive(20.5), "placeName" to JsonPrimitive("Fixture Cafe"),
                "osmType" to JsonPrimitive("node"), "osmId" to JsonPrimitive(42L), "osmCategory" to JsonPrimitive("amenity=cafe"),
                "addressText" to JsonPrimitive("1 Fixture Street"),
            ),
            sentVariables(fake, 1),
        )
    }

    @Test
    fun erasesEverythingWithNoRange() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"EraseLocationHistory":{"pointCount":10,"visitCount":2,"placeCount":1}}}""")
        assertEquals(10, connection.eraseLocationHistory().pointCount)
        assertEquals(emptyMap<String, Any>(), sentVariables(fake))
    }

    @Test
    fun reportsABatchOfPointsAndVisits() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"ReportLocation":{"newPointCount":1,"newVisitCount":1,"updatedVisitCount":0}}}""")
        val outbox = LocationOutbox("account")
        val arrived = instantOf("2024-01-02T09:00:00Z")
        outbox.addPoint(ReportedPoint(latitudeDegrees = 10.5, longitudeDegrees = 20.5, accuracyMeters = 8.0, speedMetersPerSecond = 1.5, measuredAt = arrived), arrived)
        outbox.arrive(WatchedPlace.ofPlace("place-1", 10.5, 20.5, 100.0), arrived)
        val result = connection.reportLocation(outbox.nextBatch(), "phone-1")
        assertEquals(1, result.newVisitCount)
        val variables = sentVariables(fake)
        assertEquals(JsonPrimitive("phone-1"), variables["agentPhoneId"])
        assertEquals(
            JsonArray(
                listOf(
                    JsonObject(
                        mapOf(
                            "latitudeDegrees" to JsonPrimitive(10.5), "longitudeDegrees" to JsonPrimitive(20.5), "accuracyMeters" to JsonPrimitive(8.0),
                            "speedMetersPerSecond" to JsonPrimitive(1.5), "measuredAt" to JsonPrimitive("2024-01-02T09:00:00Z"),
                        ),
                    ),
                ),
            ),
            variables["points"],
        )
        assertEquals(
            JsonArray(
                listOf(
                    JsonObject(
                        mapOf(
                            "clientVisitKey" to JsonPrimitive("region:place-1:a1704186000"), "latitudeDegrees" to JsonPrimitive(10.5),
                            "longitudeDegrees" to JsonPrimitive(20.5), "accuracyMeters" to JsonPrimitive(0.0), "visitSource" to JsonPrimitive("region"),
                            "arrivedAt" to JsonPrimitive("2024-01-02T09:00:00Z"), "placeId" to JsonPrimitive("place-1"),
                        ),
                    ),
                ),
            ),
            variables["visits"],
        )
    }

    @Test
    fun aLocationNotificationEventCarriesTheUserWhoMoved() {
        assertEquals("user-2", TeaNodeJson.decodeFromString(LocationEvent.serializer(), """{"userId":"user-2"}""").userId)
        assertTrue(LocationDocuments.LOCATION_EVENTS.contains("LocationEvents"))
    }

    // Keeping

    @Test
    fun keepsAPositionAfter25MetersOrAMinuteOfMoving() {
        val filter = LocationPointFilter()
        val start = instantOf("2024-01-02T09:00:00Z")
        // About 11 meters per 0.0001 degrees of latitude.
        assertTrue(filter.shouldKeep(10.5, 20.5, null, start, false))
        assertFalse(filter.shouldKeep(10.5001, 20.5, 1.0, start.plusSeconds(10), false))
        assertTrue(filter.shouldKeep(10.5003, 20.5, 1.0, start.plusSeconds(20), false))
        // A minute later, 11 meters on and moving: kept. Not moving: not kept.
        assertFalse(filter.shouldKeep(10.5004, 20.5, 0.2, start.plusSeconds(90), false))
        assertTrue(filter.shouldKeep(10.5004, 20.5, 0.9, start.plusSeconds(100), false))
    }

    @Test
    fun keepsNothingWhileStationary() {
        val filter = LocationPointFilter()
        assertFalse(filter.shouldKeep(10.5, 20.5, null, Instant.now(), true))
        assertTrue(filter.shouldKeep(10.5, 20.5, null, Instant.now(), false))
        filter.reset()
        assertTrue(filter.shouldKeep(10.5, 20.5, null, Instant.now(), false))
    }

    @Test
    fun theDistanceBetweenTwoCoordinates() {
        assertEquals(11_119.0, LocationGeometry.distanceMeters(10.5, 20.5, 10.6, 20.5), 20.0)
        assertFalse(LocationGeometry.isOnEarth(91.0, 0.0))
        assertFalse(LocationGeometry.isOnEarth(Double.NaN, 0.0))
        assertTrue(LocationGeometry.isOnEarth(-90.0, 180.0))
    }

    @Test
    fun aVisitIsNamedFromWhereAndWhenItBegan() {
        val arrived = instantOf("2024-01-02T09:00:00Z")
        val departed = instantOf("2024-01-02T10:00:00Z")
        val arrival = LocationVisitKey.visit(10.5, 20.5, arrived, null)
        val departure = LocationVisitKey.visit(10.5, 20.5, arrived, departed)
        assertEquals(arrival, departure)
        assertEquals("visit:10.500:20.500:a1704186000", arrival)
        val unseen = LocationVisitKey.visit(10.5, 20.5, null, departed)
        assertEquals("visit:10.500:20.500:d1704189600", unseen)
        assertNotEquals(unseen, arrival)
    }

    // The outbox

    private fun visitToReport(key: String, arrivedAt: Instant?, departedAt: Instant? = null, accuracy: Double = 5.0, source: String = "visit", sequence: Int = 0) =
        ReportedVisit(sequence, key, 10.5, 20.5, accuracy, arrivedAt, departedAt, source)

    private fun pointToReport(measuredAt: Instant, sequence: Int = 0, latitudeDegrees: Double = 10.5, accuracy: Double = 5.0) =
        ReportedPoint(sequence, latitudeDegrees, 20.5, accuracy, measuredAt = measuredAt)

    @Test
    fun theOutboxHandsOutBatchesOfAtMost500PointsAnd50Visits() {
        val outbox = LocationOutbox("account")
        val start = instantOf("2024-01-02T00:00:00Z")
        for (index in 0 until 620) outbox.addPoint(pointToReport(start.plusSeconds(index.toLong())), start)
        for (index in 0 until 55) outbox.addVisit(visitToReport("key-$index", start), start)
        val first = outbox.nextBatch()
        assertEquals(500, first.points.size)
        assertEquals(50, first.visits.size)
        assertEquals(start, first.points.first().measuredAt)
        outbox.remove(first)
        val second = outbox.nextBatch()
        assertEquals(120, second.points.size)
        assertEquals((50 until 55).map { index -> "key-$index" }, second.visits.map { each -> each.clientVisitKey })
        outbox.remove(second)
        assertTrue(outbox.isEmpty)
    }

    @Test
    fun whatIsAddedWhileABatchIsInFlightStays() {
        val outbox = LocationOutbox("account")
        val start = instantOf("2024-01-02T00:00:00Z")
        outbox.addPoint(pointToReport(start), start)
        val inFlight = outbox.nextBatch()
        outbox.addPoint(pointToReport(start.plusSeconds(30), latitudeDegrees = 10.6), start)
        outbox.remove(inFlight)
        assertEquals(1, outbox.points.size)
        assertEquals(10.6, outbox.points.first().latitudeDegrees)
    }

    @Test
    fun theOutboxKeepsTheNewest20000PointsAndEveryVisit() {
        val outbox = LocationOutbox("account")
        val start = instantOf("2024-01-02T00:00:00Z")
        outbox.addVisit(visitToReport("first", start), start)
        for (index in 0 until (LocationOutbox.POINT_COUNT_MOST + 7)) outbox.addPoint(pointToReport(start.plusSeconds(index.toLong())), start)
        assertEquals(LocationOutbox.POINT_COUNT_MOST, outbox.points.size)
        assertEquals(start.plusSeconds(7), outbox.points.first().measuredAt)
        assertEquals(1, outbox.visits.size)
    }

    @Test
    fun aPositionThatIsNotOnTheEarthIsNotQueued() {
        val outbox = LocationOutbox("account")
        outbox.addPoint(pointToReport(Instant.now(), latitudeDegrees = 200.0))
        outbox.addPoint(pointToReport(Instant.now(), accuracy = -1.0))
        assertTrue(outbox.isEmpty)
    }

    @Test
    fun whatTheServerWouldRefuseIsNotQueued() {
        val now = Instant.now()
        val outbox = LocationOutbox("account")
        fun visit(key: String = "key", accuracy: Double = 10.0, arrivedAt: Instant? = now.minusSeconds(60), departedAt: Instant? = null, source: String = "visit") =
            visitToReport(key, arrivedAt, departedAt, accuracy, source)
        assertTrue(outbox.isAcceptable(pointToReport(now.plusSeconds(3600)), now))
        assertFalse(outbox.isAcceptable(pointToReport(now.plusSeconds(25 * 3600)), now))
        assertFalse(outbox.isAcceptable(pointToReport(now, accuracy = Double.NaN), now))

        assertTrue(outbox.isAcceptable(visit(), now))
        assertTrue(outbox.isAcceptable(visit(key = "k".repeat(128)), now))
        assertFalse(outbox.isAcceptable(visit(key = "k".repeat(129)), now))
        assertFalse(outbox.isAcceptable(visit(key = "  "), now))
        assertFalse(outbox.isAcceptable(visit(accuracy = -1.0), now))
        assertFalse(outbox.isAcceptable(visit(source = "check_in"), now))
        assertFalse(outbox.isAcceptable(visit(arrivedAt = null, departedAt = null), now))
        assertFalse(outbox.isAcceptable(visit(arrivedAt = now.plusSeconds(25 * 3600)), now))
        assertFalse(outbox.isAcceptable(visit(arrivedAt = null, departedAt = now.plusSeconds(25 * 3600)), now))
        assertFalse(outbox.isAcceptable(visit(arrivedAt = now.minusSeconds(60), departedAt = now.minusSeconds(120)), now))
        assertTrue(outbox.isAcceptable(visit(arrivedAt = now.minusSeconds(120), departedAt = now.minusSeconds(60)), now))

        outbox.addPoint(pointToReport(now.plusSeconds(25 * 3600)), now)
        outbox.addVisit(visit(source = "check_in"), now)
        assertTrue(outbox.isEmpty)
    }

    @Test
    fun whatWaitedPast400DaysIsTakenOutBeforeSending() {
        val queuedAt = Instant.now()
        val outbox = LocationOutbox("account")
        outbox.addPoint(pointToReport(queuedAt), queuedAt)
        outbox.addVisit(visitToReport("key", queuedAt), queuedAt)
        outbox.addActivities(listOf(ReportedActivity(startedAt = queuedAt, activityKind = "walking", activityConfidence = "low")), queuedAt)
        // A day to spare for the phone's clock.
        outbox.removeTooOld(queuedAt.plusSeconds(398 * 86_400L))
        assertFalse(outbox.points.isEmpty() || outbox.visits.isEmpty() || outbox.activities.isEmpty())
        outbox.removeTooOld(queuedAt.plusSeconds(400 * 86_400L))
        assertTrue(outbox.isEmpty)
    }

    @Test
    fun aBatchTheServerCallsInvalidIsSplitUntilTheOneEntryIsFound() {
        val batch = LocationBatch(
            (1..5).map { index -> pointToReport(Instant.now(), index) }, (6..8).map { index -> visitToReport("key-$index", Instant.now(), sequence = index) },
        )
        assertEquals(8, batch.entryCount)

        // Positions and visits go apart first.
        val apart = batch.split()!!
        assertEquals(5, apart.first.points.size)
        assertTrue(apart.first.visits.isEmpty())
        assertEquals(3, apart.second.visits.size)
        assertTrue(apart.second.points.isEmpty())

        // Then each is halved, with nothing lost or repeated.
        val halves = apart.first.split()!!
        assertEquals(listOf(1, 2), halves.first.points.map { each -> each.sequence })
        assertEquals(listOf(3, 4, 5), halves.second.points.map { each -> each.sequence })
        val visitHalves = apart.second.split()!!
        assertEquals(listOf(6), visitHalves.first.visits.map { each -> each.sequence })
        assertEquals(listOf(7, 8), visitHalves.second.visits.map { each -> each.sequence })

        // One entry cannot be split: it is the one that is refused.
        assertNull(LocationBatch(listOf(pointToReport(Instant.now(), 1)), emptyList()).split())
        assertNull(LocationBatch(emptyList(), listOf(visitToReport("key", Instant.now(), sequence = 1))).split())
        assertNull(LocationBatch(emptyList(), emptyList()).split())
    }

    @Test
    fun aDepartureFindsItsArrivalAfterTheOutboxIsReadBack() {
        val outbox = LocationOutbox("account")
        val arrived = instantOf("2024-01-02T09:00:00Z")
        outbox.arrive(WatchedPlace.ofPlace("place-1", 10.5, 20.5, 100.0), arrived)
        outbox.remove(outbox.nextBatch())

        val reread = LocationOutbox.decoded(outbox.encoded())
        assertEquals(outbox.encoded(), reread.encoded())
        assertTrue(reread.depart("place-1", arrived.plusSeconds(3600)))
        assertFalse(reread.depart("place-1", arrived.plusSeconds(3700)))
        val departure = reread.visits.first()
        assertEquals(LocationVisitKey.region("place-1", arrived), departure.clientVisitKey)
        assertNull(departure.arrivedAt)
        assertEquals(arrived.plusSeconds(3600), departure.departedAt)
        assertEquals("place-1", departure.placeId)
    }

    @Test
    fun anArrivalAtAPlaceWithAnOpenVisitEndsThatVisitFirst() {
        val outbox = LocationOutbox("account")
        val first = instantOf("2024-01-02T09:00:00Z")
        val second = first.plusSeconds(3 * 3600)
        outbox.arrive(WatchedPlace.ofPlace("place-1", 10.5, 20.5, 100.0), first)
        outbox.arrive(WatchedPlace.ofPlace("place-1", 10.5, 20.5, 100.0), second)
        assertEquals(3, outbox.visits.size)
        // The first visit is ended at the second arrival, under its own key.
        assertEquals(LocationVisitKey.region("place-1", first), outbox.visits[1].clientVisitKey)
        assertEquals(second, outbox.visits[1].departedAt)
        assertEquals(LocationVisitKey.region("place-1", second), outbox.visits[2].clientVisitKey)
        assertNotNull(outbox.openRegionVisits["place-1"])
    }

    @Test
    fun endingEveryVisitWhenMonitoringStopsLeavesNoneOpen() {
        val outbox = LocationOutbox("account")
        val arrived = instantOf("2024-01-02T09:00:00Z")
        outbox.arrive(WatchedPlace.ofPlace("place-1", 10.5, 20.5, 100.0), arrived)
        outbox.arrive(WatchedPlace.ofPlace("place-2", 10.6, 20.5, 100.0), arrived)
        outbox.departEverywhere(arrived.plusSeconds(600))
        assertTrue(outbox.openRegionVisits.isEmpty())
        assertEquals(2, outbox.visits.count { each -> each.departedAt != null })
    }

    @Test
    fun aVisitWithNeitherAnArrivalNorADepartureIsNotQueued() {
        val outbox = LocationOutbox("mail.example.com/robin")
        outbox.addVisit(visitToReport("undated", null, accuracy = 20.0))
        assertTrue(outbox.isEmpty)
        outbox.addVisit(visitToReport("dated", Instant.ofEpochSecond(1_000), accuracy = 20.0))
        assertEquals(1, outbox.visits.size)
    }

    @Test
    fun discardingWhatIsUnsentKeepsTheVisitsBegun() {
        val outbox = LocationOutbox("account")
        outbox.addPoint(pointToReport(Instant.now()))
        outbox.arrive(WatchedPlace.ofPlace("place-1", 10.5, 20.5, 100.0), Instant.now())
        outbox.discardUnsent()
        assertTrue(outbox.isEmpty)
        assertNotNull(outbox.openRegionVisits["place-1"])
        assertEquals("account", outbox.accountKey)
    }

    @Test
    fun discardingARangeKeepsWhatWasNoticedOutsideIt() {
        val outbox = LocationOutbox("account")
        val day = instantOf("2024-01-02T00:00:00Z")
        for (hour in listOf(1L, 30L)) outbox.addPoint(pointToReport(day.plusSeconds(hour * 3600)))
        outbox.addVisit(visitToReport("inside", day.plusSeconds(3600)))
        outbox.addVisit(visitToReport("outside", null, day.plusSeconds(30 * 3600)))
        outbox.discardUnsent(day, day.plusSeconds(86_400))
        assertEquals(1, outbox.points.size)
        assertEquals(listOf("outside"), outbox.visits.map { each -> each.clientVisitKey })
    }

    @Test
    fun eachAccountHasAnOutboxFileOfItsOwn() {
        val first = LocationOutbox.fileName("mail.example.com/robin")
        val second = LocationOutbox.fileName("mail.example.com/sam")
        assertNotEquals(first, second)
        assertEquals(first, LocationOutbox.fileName("mail.example.com/robin"))
        assertFalse(LocationOutbox.fileName("../../elsewhere").contains("/"))
        assertFalse(LocationOutbox.fileName("../../elsewhere").contains(".."))
    }

    @Test
    fun aFailedBatchIsRetriedOnlyForACallThatDidNotGetThrough() {
        assertEquals(LocationReportFailure.Refused("api: permission denied"), LocationReportFailure.classify(TeaNodeError.Refused("api: permission denied")))
        assertEquals(
            LocationReportFailure.BatchInvalid("api: invalid arguments: bad point"),
            LocationReportFailure.classify(TeaNodeError.Refused("api: invalid arguments: bad point")),
        )
        assertEquals(LocationReportFailure.PhoneNotFound, LocationReportFailure.classify(TeaNodeError.Refused("api: not found")))
        assertEquals(LocationReportFailure.RetryLater, LocationReportFailure.classify(TeaNodeError.SignedOut))
        assertEquals(LocationReportFailure.RetryLater, LocationReportFailure.classify(java.io.IOException("offline")))
    }

    @Test
    fun eventsWaitHalfAMinuteAfterAFailureButNotAfterASuccess() {
        val gate = LocationRetryGate()
        val now = instantOf("2024-01-02T09:00:00Z")
        assertTrue(gate.canTry(now))
        gate.didFail(now)
        assertFalse(gate.canTry(now.plusSeconds(10)))
        assertTrue(gate.canTry(now.plusSeconds(30)))
        gate.didFail(now)
        gate.didSucceed()
        assertTrue(gate.canTry(now.plusSeconds(1)))
    }

    // Watching places

    private fun place(id: String, latitude: Double, label: String = "", isNamed: Boolean = false, radius: Double = 75.0) =
        LocationPlace(id = id, placeName = id, isNamedByPerson = isNamed, placeLabel = label, latitudeDegrees = latitude, longitudeDegrees = 20.5, radiusMeters = radius)

    private fun store(osmId: Long, latitude: Double, placeId: String? = null) = WatchedStore(
        storeName = "Example Market", storeKey = "examplemarket", placeName = "Example Market", osmType = "node", osmId = osmId,
        osmCategory = "shop=supermarket", latitudeDegrees = latitude, longitudeDegrees = 20.5, radiusMeters = 100.0, distanceMeters = 0.0, placeId = placeId,
    )

    @Test
    fun watchesTheNearestTwentyOfTheLabeledAndNamedPlaces() {
        val places = (0 until 30).map { index -> place("named-$index", 10.5 + index * 0.01, isNamed = true) } +
            place("only-from-the-map", 10.5) + place("home", 11.5, label = "home")
        val watched = LocationPlaceWatch.choose(places, nearLatitudeDegrees = 10.5, nearLongitudeDegrees = 20.5)
        assertEquals(20, watched.size)
        assertEquals("named-0", watched.first().placeId)
        assertFalse(watched.any { each -> each.placeId == "only-from-the-map" })
        assertFalse(watched.any { each -> each.placeId == "home" })
    }

    @Test
    fun theStoresTakeSixCirclesAndThePersonsPlacesTheRest() {
        val places = (0 until 30).map { index -> place("named-$index", 10.5 + index * 0.01, isNamed = true) }
        val stores = (1..8).map { index -> store(index.toLong(), 10.6 + index * 0.01) }
        val watched = LocationPlaceWatch.choose(places, stores, 10.5, 20.5)
        assertEquals(20, watched.size)
        assertEquals((1..6).map { index -> "store:node:$index" }, watched.filter { each -> each.storeBranch != null }.map { each -> each.identifier })
        assertEquals(14, watched.count { each -> each.storeBranch == null })
        assertEquals("Example Market", watched.first { each -> each.storeBranch != null }.storeBranch?.osmName)
    }

    @Test
    fun aBranchThatIsAPlaceWatchedIsWatchedAsThatPlace() {
        val places = listOf(place("market", 10.5, isNamed = true), place("home", 10.51, label = "home"))
        val stores = listOf(store(1, 10.5, "market"), store(2, 10.52, "a-place-from-the-map"))
        val watched = LocationPlaceWatch.choose(places, stores, 10.5, 20.5)
        assertEquals(listOf("market", "home", "store:node:2"), watched.map { each -> each.identifier })
        assertEquals("a-place-from-the-map", watched.last().placeId)
    }

    private fun watchedBranch(osmId: Long, latitude: Double) = WatchedPlace.ofStore(store(osmId, latitude))

    @Test
    fun aCircleWithAnOpenVisitStaysWatchedWhenTheServerNoLongerListsIt() {
        val chosen = listOf(WatchedPlace.ofPlace("home", 10.5, 20.5, 100.0))
        val inTheStore = watchedBranch(5, 10.52)
        assertEquals(listOf("home", "store:node:5"), LocationPlaceWatch.keepOpen(chosen, listOf(inTheStore), 10.5, 20.5).map { each -> each.identifier })
        // Listed again, it is not watched twice.
        assertEquals(listOf("home", "store:node:5"), LocationPlaceWatch.keepOpen(chosen + inTheStore, listOf(inTheStore), 10.5, 20.5).map { each -> each.identifier })
        // With no visit open, nothing is kept beyond the chosen.
        assertEquals(listOf("home"), LocationPlaceWatch.keepOpen(chosen, emptyList(), null, null).map { each -> each.identifier })
    }

    @Test
    fun theOpenCirclesStayWhenTheyPushTheWatchPastTwentyAndTheFarthestOthersGo() {
        val chosen = (0 until 20).map { index -> WatchedPlace.ofPlace("named-$index", 10.5 + index * 0.01, 20.5, 100.0) }
        // One open visit at a circle far from the person, and one at a store no longer listed.
        val watched = LocationPlaceWatch.keepOpen(chosen, listOf(chosen[19], watchedBranch(5, 11.5)), 10.5, 20.5)
        assertEquals(LocationPlaceWatch.COUNT_MOST, watched.size)
        assertTrue(watched.any { each -> each.identifier == "named-19" })
        assertTrue(watched.any { each -> each.identifier == "store:node:5" })
        // Of the nineteen others, the farthest is dropped.
        assertFalse(watched.any { each -> each.identifier == "named-18" })
        assertTrue(watched.any { each -> each.identifier == "named-17" })
        assertTrue(watched.any { each -> each.identifier == "named-0" })
    }

    @Test
    fun withNoPositionTheLastChosenGoFirstWhenTheOpenCirclesNeedRoom() {
        val chosen = (0 until 20).map { index -> WatchedPlace.ofPlace("named-$index", 10.5 + index * 0.01, 20.5, 100.0) }
        val watched = LocationPlaceWatch.keepOpen(chosen, listOf(watchedBranch(5, 10.5)), null, null)
        assertEquals(LocationPlaceWatch.COUNT_MOST, watched.size)
        assertEquals("store:node:5", watched.last().identifier)
        assertTrue(watched.any { each -> each.identifier == "named-18" })
        assertFalse(watched.any { each -> each.identifier == "named-19" })
    }

    @Test
    fun aVisitToAStoresBranchNamesItsMapObject() {
        val outbox = LocationOutbox("account")
        val arrived = instantOf("2024-01-02T17:00:00Z")
        outbox.arrive(WatchedPlace.ofStore(store(12, 10.5)), arrived)
        val reread = LocationOutbox.decoded(outbox.encoded())
        assertTrue(reread.depart("store:node:12", arrived.plusSeconds(900)))
        assertEquals(2, reread.visits.size)
        for (visit in reread.visits) {
            assertEquals(LocationVisitKey.region("store:node:12", arrived), visit.clientVisitKey)
            assertNull(visit.placeId)
            val fields = visit.variables as JsonObject
            assertEquals(JsonPrimitive("node"), fields["osmType"])
            assertEquals(JsonPrimitive(12L), fields["osmId"])
            assertEquals(JsonPrimitive("Example Market"), fields["osmName"])
            assertEquals(JsonPrimitive("shop=supermarket"), fields["osmCategory"])
            assertEquals(JsonPrimitive("region"), fields["visitSource"])
            assertNull(fields["placeId"])
        }
    }

    @Test
    fun aVisitBegunBeforeStoresWereWatchedEndsAtItsPlace() {
        // An open visit kept by an earlier version: no place in it, kept under the place's id.
        val kept = """{"accountKey":"account","points":[],"visits":[],"openRegionVisits":{"place-1":{"clientVisitKey":"region:place-1:a1704186000","latitudeDegrees":10.5,"longitudeDegrees":20.5,"arrivedAt":1704186000000}}}"""
        val outbox = LocationOutbox.decoded(kept)
        assertTrue(outbox.depart("place-1", instantOf("2024-01-02T10:00:00Z")))
        assertEquals("place-1", outbox.visits.first().placeId)
        assertNull(outbox.visits.first().storeBranch)
    }

    @Test
    fun aBranchTheMonitorAloneKnowsIsToldByItsMapObject() {
        val branch = WatchedPlace.ofIdentifier("store:way:7", 10.5, 20.5, 100.0)
        assertNull(branch.placeId)
        assertEquals("way", branch.storeBranch?.osmType)
        assertEquals(7L, branch.storeBranch?.osmId)
        val own = WatchedPlace.ofIdentifier("place-1", 10.5, 20.5, 100.0)
        assertEquals("place-1", own.placeId)
        assertNull(own.storeBranch)
    }

    @Test
    fun aWatchedCircleIsAtLeast100MetersWide() {
        val places = listOf(place("small", 10.5, isNamed = true, radius = 25.0), place("large", 10.51, isNamed = true, radius = 400.0))
        assertEquals(listOf(100.0, 400.0), LocationPlaceWatch.choose(places, nearLatitudeDegrees = null, nearLongitudeDegrees = null).map { each -> each.radiusMeters }.sorted())
    }

    // Showing

    @Test
    fun visitsAreGroupedByTheDayTheyBeganNewestFirst() {
        fun visit(id: String, arrived: String?, departed: String?) =
            PlaceVisit(id = id, place = place("p", 10.5), arrivedAt = arrived, departedAt = departed, latitudeDegrees = 10.5, longitudeDegrees = 20.5)
        val days = LocationDays.group(
            listOf(
                visit("early", "2024-01-02T08:00:00Z", "2024-01-02T09:00:00Z"), visit("yesterday", "2024-01-01T23:00:00Z", "2024-01-02T07:00:00Z"),
                visit("late", "2024-01-02T18:00:00Z", null), visit("unseen arrival", null, "2024-01-01T10:00:00Z"), visit("no time", null, null),
            ),
            zone = utc,
        )
        assertEquals(2, days.size)
        assertEquals(listOf("late", "early"), days[0].visits.map { each -> each.id })
        assertEquals(listOf("yesterday", "unseen arrival"), days[1].visits.map { each -> each.id })
        val window = LocationDays.window(days[0].day, utc)
        assertEquals(instantOf("2024-01-02T00:00:00Z"), window.first)
        assertEquals(instantOf("2024-01-03T00:00:00Z"), window.second)
    }

    @Test
    fun aDaysPathGoesThroughThePlacesVisitedBetweenFewPositions() {
        fun point(latitude: Double, measuredAt: String) = LocationPoint(latitude, 20.5, 10.0, measuredAt = measuredAt)
        fun visit(latitude: Double, arrived: String?, departed: String?) =
            PlaceVisit(id = "v$latitude", place = place("p", latitude), arrivedAt = arrived, departedAt = departed, latitudeDegrees = latitude, longitudeDegrees = 20.5)
        val path = LocationDays.path(
            listOf(point(1.0, "2024-01-02T08:00:00Z"), point(5.0, "2024-01-02T20:00:00Z")),
            listOf(
                visit(3.0, "2024-01-02T12:00:00Z", "2024-01-02T13:00:00Z"),
                // Arrived the day before: only the departure is in the day.
                visit(2.0, "2024-01-01T22:00:00Z", "2024-01-02T09:00:00Z"), visit(6.0, "2024-01-02T21:00:00Z", null),
            ),
            instantOf("2024-01-02T00:00:00Z"), instantOf("2024-01-03T00:00:00Z"),
        )
        assertEquals(listOf(1.0, 2.0, 3.0, 3.0, 5.0, 6.0), path.map { each -> each.latitudeDegrees })
    }

    // Setting Location up

    private fun fixturePlace(label: String) = LocationPlace(id = "place-1", placeName = "Fixture", placeLabel = label, latitudeDegrees = 10.5, longitudeDegrees = 20.5)

    private fun fixtureCircle(circleKind: String, id: String = "circle-1") = PeopleCircle(id, "Fixture Home", circleKind, "2024-01-01T00:00:00Z", "admin", emptyList())

    private fun fixtureShare(circleId: String) = LocationShare("share-1", null, null, null, circleId, "Fixture Home", "history", "2024-01-01T00:00:00Z")

    @Test
    fun theSetupIsWorkedOutFromTheServerAndThePermission() {
        val begun = LocationSetup.from(false, emptyList(), listOf(fixtureCircle("family")), emptyList())
        assertEquals(listOf("circle-1"), begun.familyCircles.map { each -> each.id })
        assertTrue(begun.isShareOffered)
        assertFalse(begun.isFinished)
        assertTrue(begun.isShown(isDismissed = false, hasLatestPoint = false))
        assertFalse(begun.isShown(isDismissed = true, hasLatestPoint = false))
        assertFalse(begun.isShown(isDismissed = false, hasLatestPoint = true))

        val finished = LocationSetup.from(true, listOf(fixturePlace("home")), listOf(fixtureCircle("family")), listOf(fixtureShare("circle-1")))
        assertTrue(finished.isFinished)
        assertFalse(finished.isShown(isDismissed = false, hasLatestPoint = false))
    }

    @Test
    fun theShareRowIsOnlyForAFamilyAndAPlaceLabeledWorkIsNotHome() {
        val noFamily = LocationSetup.from(true, listOf(fixturePlace("home")), listOf(fixtureCircle("friends")), emptyList())
        assertFalse(noFamily.isShareOffered)
        assertTrue(noFamily.isFinished)
        assertFalse(LocationSetup.from(true, listOf(fixturePlace("work")), emptyList(), emptyList()).isHomeDone)
        val shareElsewhere = LocationSetup.from(true, listOf(fixturePlace("home")), listOf(fixtureCircle("family")), listOf(fixtureShare("circle-other")))
        assertFalse(shareElsewhere.isShareDone)
    }

    @Test
    fun theSetupListsEveryFamilyCircleAndIsDoneOnlyWhenAllAreShared() {
        val circles = listOf(fixtureCircle("family"), fixtureCircle("family", "circle-2"), fixtureCircle("friends", "circle-3"))
        val half = LocationSetup.from(true, listOf(fixturePlace("home")), circles, listOf(fixtureShare("circle-1")))
        assertEquals(listOf("circle-1", "circle-2"), half.familyCircles.map { each -> each.id })
        assertTrue(half.isShared(circles[0]))
        assertFalse(half.isShared(circles[1]))
        assertFalse(half.isShareDone)
        assertFalse(half.isFinished)
        val all = LocationSetup.from(true, listOf(fixturePlace("home")), circles, listOf(fixtureShare("circle-1"), fixtureShare("circle-2")))
        assertTrue(all.isFinished)
    }

    @Test
    fun everyLocationDocumentAsksForItsOwnOperation() {
        for ((document, operation) in mapOf(
            LocationDocuments.LOCATION to "GetLocation", LocationDocuments.LOCATION_VISITS to "ListLocationVisits", LocationDocuments.LOCATION_PLACES to "ListPlaces",
            LocationDocuments.REPORT_LOCATION to "ReportLocation", LocationDocuments.SAVE_PLACE to "SavePlace",
            LocationDocuments.SEARCH_ADDRESSES to "SearchAddresses", LocationDocuments.CHECK_IN to "CheckIn", LocationDocuments.LOCATION_EVENTS to "LocationEvents",
        )) {
            assertTrue(document.contains(operation), operation)
        }
    }
}
