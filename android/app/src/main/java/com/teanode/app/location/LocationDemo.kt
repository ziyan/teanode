package com.teanode.app.location

import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.unlessAsking
import com.teanode.app.demo.DemoRequest
import com.teanode.app.demo.DemoResponse
import com.teanode.app.demo.graphQLField
import com.teanode.app.demo.text
import com.teanode.kit.location.ServerTime
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.ZonedDateTime

/**
 * The invented places, visits, people and positions that demo mode and the screenshots show. Every name and coordinate is
 * made up, in one made-up area; no real map shows them but open water.
 */
object LocationDemo {
    /** Which state of the invented world is on: the screenshot tests change it, demo mode keeps the first. */
    private var activeScreenCount = 0

    /** Whether a Location screen is on screen, so that Location's demo answers apply. */
    val isActive: Boolean get() = activeScreenCount > 0

    /** A Location screen came on screen; the count lets the next route compose before the last one leaves. */
    @Synchronized fun activate() { activeScreenCount += 1 }

    @Synchronized fun deactivate() { activeScreenCount = maxOf(0, activeScreenCount - 1) }

    enum class Scenario {
        /** The person has a position, visits, places, two people sharing. */
        Everyday,

        /** The server has no position for the person yet: the setup shows in place of the lists. */
        Setup,

        /** The person and somebody sharing have paused, with an earlier pause among yesterday's visits. */
        Paused,

        /** Location is set to while-using on the phone: a banner says so and a person shows as nothing. */
        Limited,

        /** Nothing recorded at all. */
        Empty,
    }

    @Volatile var scenario: Scenario = Scenario.Everyday

    const val HOME_PLACE_ID = "place-home"
    const val PARK_PLACE_ID = "place-park"
    const val CAFE_PLACE_ID = "place-cafe"
    const val SCHOOL_PLACE_ID = "place-school"
    const val OFFICE_PLACE_ID = "place-office"
    const val HISTORY_PERSON_ID = "sam"
    const val CURRENT_PERSON_ID = "alex"

    private val zone: ZoneId get() = ZoneId.systemDefault()

    private fun today(): LocalDate = LocalDate.now(zone)

    /** A time on a day, as the server writes it. */
    private fun time(daysAgo: Long, hour: Int, minute: Int = 0): String =
        ServerTime.text(ZonedDateTime.of(today().minusDays(daysAgo), java.time.LocalTime.of(hour, minute), zone).toInstant())

    private fun agoText(minutesAgo: Long): String = ServerTime.text(Instant.now().minusSeconds(minutesAgo * 60))

    private class Place(
        val id: String, val name: String, val isNamedByPerson: Boolean, val label: String, val latitude: Double, val longitude: Double, val radius: Double,
        val category: String, val visitCount: Int, val lastVisitDaysAgo: Long, val isAddressTyped: Boolean = false,
    ) {
        fun json(userId: String = "robin"): String =
            """{"id":"$id","userId":"$userId","placeName":"$name","isNamedByPerson":$isNamedByPerson,"placeLabel":"$label","latitudeDegrees":$latitude,"longitudeDegrees":$longitude,"radiusMeters":$radius,"osmType":"way","osmId":1001,"osmName":"$name","osmCategory":"$category","addressText":"${if (isAddressTyped) "12 Elm Street, Exampleton" else ""}","isAddressTyped":$isAddressTyped,"visitCount":$visitCount,"lastVisitAt":"${time(lastVisitDaysAgo, 8, 5)}","namingErrorMessage":"","createdAt":"2026-09-01T08:00:00Z","updatedAt":"2026-09-01T08:00:00Z"}"""
    }

    private val places = listOf(
        Place(HOME_PLACE_ID, "Home", true, "home", 10.5000, 20.5000, 75.0, "", 41, 0),
        Place(OFFICE_PLACE_ID, "Office", true, "work", 10.5150, 20.5200, 120.0, "office=company", 22, 0),
        Place(SCHOOL_PLACE_ID, "Hillside School", true, "school", 10.5080, 20.4950, 150.0, "amenity=school", 18, 1),
        Place(PARK_PLACE_ID, "Riverside Park", false, "", 10.5040, 20.5060, 200.0, "leisure=park", 9, 0),
        Place(CAFE_PLACE_ID, "Corner Café", false, "", 10.5025, 20.5030, 60.0, "amenity=cafe", 5, 0, isAddressTyped = true),
    )

    private fun placeById(id: String): Place = places.firstOrNull { place -> place.id == id } ?: places.first()

    private fun visit(id: String, place: Place, arrived: String?, departed: String?, photoJson: String = ""): String {
        val duration = if (arrived != null && departed != null) {
            (ServerTime.instant(departed)!!.epochSecond - ServerTime.instant(arrived)!!.epochSecond).toString()
        } else {
            "null"
        }
        return """{"id":"$id","arrivedAt":${arrived?.let { "\"$it\"" } ?: "null"},"departedAt":${departed?.let { "\"$it\"" } ?: "null"},"durationSeconds":$duration,"latitudeDegrees":${place.latitude},"longitudeDegrees":${place.longitude},"accuracyMeters":30,"visitSource":"visit"$photoJson,"place":${place.json()}}"""
    }

    private val photosJson = ""","photoCount":7,"photos":[
        {"id":"photo-1","createdAt":"2026-09-01T08:00:00Z","photoKind":"picture","takenAt":"2026-09-01T08:10:00Z","widthPixels":10,"heightPixels":10,"durationMS":0,"placeName":"","cameraMake":"","cameraModel":"","originalFilename":"a.jpg","isFavorite":false,"isScreenshot":false,"photoProcessingState":"ready","smallPreviewUrl":"","largePreviewUrl":"","videoPreviewUrl":"","photoFileRoles":[],"processingErrorMessage":"","isOwnedByCaller":true,"ownerName":""},
        {"id":"photo-2","createdAt":"2026-09-01T08:00:00Z","photoKind":"picture","takenAt":"2026-09-01T08:12:00Z","widthPixels":10,"heightPixels":10,"durationMS":0,"placeName":"","cameraMake":"","cameraModel":"","originalFilename":"b.jpg","isFavorite":false,"isScreenshot":false,"photoProcessingState":"ready","smallPreviewUrl":"","largePreviewUrl":"","videoPreviewUrl":"","photoFileRoles":[],"processingErrorMessage":"","isOwnedByCaller":true,"ownerName":""},
        {"id":"photo-3","createdAt":"2026-09-01T08:00:00Z","photoKind":"picture","takenAt":"2026-09-01T08:15:00Z","widthPixels":10,"heightPixels":10,"durationMS":0,"placeName":"","cameraMake":"","cameraModel":"","originalFilename":"c.jpg","isFavorite":false,"isScreenshot":false,"photoProcessingState":"ready","smallPreviewUrl":"","largePreviewUrl":"","videoPreviewUrl":"","photoFileRoles":[],"processingErrorMessage":"","isOwnedByCaller":true,"ownerName":""}]"""

    private fun pauseJson(id: String, startedAt: String, endedAt: String?): String =
        """{"id":"$id","startedAt":"$startedAt","endedAt":${endedAt?.let { "\"$it\"" } ?: "null"}}"""

    private fun ownPause(): String? = if (scenario == Scenario.Paused) pauseJson("pause-now", agoText(20), ServerTime.text(Instant.now().plusSeconds(40 * 60))) else null

    private fun pointJson(latitude: Double, longitude: Double, measuredAt: String): String =
        """{"latitudeDegrees":$latitude,"longitudeDegrees":$longitude,"accuracyMeters":8,"altitudeMeters":null,"speedMetersPerSecond":1.3,"courseDegrees":null,"measuredAt":"$measuredAt","locationSource":"phone"}"""

    private fun ownVisits(): List<String> = when (scenario) {
        Scenario.Setup, Scenario.Empty -> emptyList()
        else -> listOf(
            visit("visit-1", places[0], time(0, 17, 40), null),
            visit("visit-2", places[3], time(0, 16, 5), time(0, 17, 10), photosJson),
            visit("visit-3", places[1], time(0, 9, 5), time(0, 15, 30)),
            visit("visit-4", places[4], time(0, 8, 5), time(0, 8, 40)),
            visit("visit-5", places[0], time(1, 18, 0), time(1, 22, 0)),
            visit("visit-6", places[2], time(1, 8, 30), time(1, 15, 20)),
        )
    }

    private fun personJson(
        userId: String, username: String, name: String, shareLevel: String, granted: String, latitude: Double?, longitude: Double?, minutesAgo: Long, openVisit: String?,
        pause: String? = null, mute: String? = null, colorIndex: Int = 3,
    ): String {
        val point = if (latitude == null || longitude == null) "null" else pointJson(latitude, longitude, agoText(minutesAgo))
        return """{"userId":"$userId","username":"$username","name":"$name","avatarUrl":null,"avatarColorIndex":$colorIndex,"shareLevel":"$shareLevel","grantedShareLevel":"$granted","latestPoint":$point,"openVisit":${openVisit ?: "null"},"pause":${pause ?: "null"},"notificationMute":${mute ?: "null"}}"""
    }

    private fun samPlaces(): Pair<Place, Place> = Pair(
        Place("place-sam-school", "Hillside School", true, "school", 10.5080, 20.4950, 100.0, "", 12, 0),
        Place("place-sam-gym", "Harbor Gym", true, "gym", 10.4960, 20.5120, 100.0, "", 12, 0),
    )

    private fun samVisits(): List<String> {
        val (school, gym) = samPlaces()
        return listOf(
            visit("visit-sam-1", school, agoText(50), null),
            visit("visit-sam-2", gym, agoText(4 * 60), agoText(3 * 60)),
        ).map { json -> json.replace("\"place\":" + school.json(), "\"place\":" + school.json("sam")) }
    }

    private fun people(): List<String> {
        val (school, _) = samPlaces()
        return when (scenario) {
            Scenario.Setup, Scenario.Empty -> emptyList()
            Scenario.Limited -> listOf(
                personJson("sam", "sam", "Sam Example", "current", "history", 10.5080, 20.4950, 2, visit("visit-sam-open", school, agoText(50), null)),
                personJson("alex", "alex", "Alex Sample", "", "history", null, null, 0, null, colorIndex = 7),
            )
            Scenario.Paused -> listOf(
                personJson("sam", "sam", "Sam Example", "history", "history", null, null, 0, null, pause = pauseJson("pause-sam", agoText(30), ServerTime.text(Instant.now().plusSeconds(90 * 60)))),
                personJson("alex", "alex", "Alex Sample", "current", "current", 10.5100, 20.5100, 8, null, colorIndex = 7),
            )
            Scenario.Everyday -> listOf(
                personJson("sam", "sam", "Sam Example", "history", "history", 10.5080, 20.4950, 2, visit("visit-sam-open", school, agoText(50), null)),
                personJson("alex", "alex", "Alex Sample", "current", "current", 10.5100, 20.5100, 8, null, colorIndex = 7),
            )
        }
    }

    private fun ownLocation(): String {
        val latest = when (scenario) {
            Scenario.Setup, Scenario.Empty -> "null"
            else -> pointJson(10.5001, 20.5001, agoText(3))
        }
        val open = ownVisits().firstOrNull()?.takeIf { scenario != Scenario.Paused }
        return """{"userId":"robin","username":"robin","name":"Robin Example","avatarUrl":null,"avatarColorIndex":5,"shareLevel":"owner","grantedShareLevel":null,"latestPoint":$latest,"openVisit":${open ?: "null"},"pause":${ownPause() ?: "null"},"notificationMute":null}"""
    }

    private fun trips(): List<String> {
        if (scenario == Scenario.Setup || scenario == Scenario.Empty) return emptyList()
        val home = places[0].json()
        val office = places[1].json()
        fun leg(id: String, mode: String, start: String, end: String, meters: Int, seconds: Int, corrected: Boolean = false, detected: String = mode) =
            """{"id":"$id","startedAt":"$start","endedAt":"$end","durationSeconds":$seconds,"distanceMeters":$meters,"travelMode":"$mode","detectedTravelMode":"$detected","isTravelModeCorrected":$corrected,"detectionSource":"motion"}"""
        return listOf(
            """{"id":"trip-1","startedAt":"${time(0, 15, 30)}","endedAt":"${time(0, 16, 5)}","durationSeconds":2100,"distanceMeters":12400,"fromPlace":$office,"toPlace":${places[3].json()},"legs":[${leg("leg-1", "bus", time(0, 15, 35), time(0, 15, 50), 9800, 900, corrected = true, detected = "car")},${leg("leg-2", "walk", time(0, 15, 50), time(0, 16, 5), 1100, 900)}]}""",
            """{"id":"trip-2","startedAt":"${time(0, 8, 40)}","endedAt":"${time(0, 9, 5)}","durationSeconds":1500,"distanceMeters":6200,"fromPlace":${places[4].json()},"toPlace":$office,"legs":[${leg("leg-3", "car", time(0, 8, 41), time(0, 9, 4), 6200, 1380)}]}""",
            """{"id":"trip-3","startedAt":"${time(0, 17, 10)}","endedAt":"${time(0, 17, 40)}","durationSeconds":1800,"distanceMeters":2100,"fromPlace":${places[3].json()},"toPlace":$home,"legs":[${leg("leg-4", "bike", time(0, 17, 10), time(0, 17, 40), 2100, 1800)}]}""",
        )
    }

    private fun totals(period: String): String {
        val days = when (period) {
            "day" -> listOf(today().toString(), today().minusDays(1).toString())
            "week" -> listOf(today().with(java.time.temporal.TemporalAdjusters.previousOrSame(java.time.DayOfWeek.MONDAY)).toString(), today().with(java.time.temporal.TemporalAdjusters.previousOrSame(java.time.DayOfWeek.MONDAY)).minusWeeks(1).toString())
            "month" -> listOf(today().withDayOfMonth(1).toString(), today().withDayOfMonth(1).minusMonths(1).toString())
            else -> listOf(today().withDayOfYear(1).toString())
        }
        return days.mapIndexed { index, day ->
            val scale = 1.0 - index * 0.35
            listOf(
                """{"periodStartDay":"$day","travelMode":"car","distanceMeters":${(118_000 * scale).toInt()},"durationSeconds":${(9000 * scale).toInt()},"legCount":${(14 * scale).toInt() + 1}}""",
                """{"periodStartDay":"$day","travelMode":"walk","distanceMeters":${(14_500 * scale).toInt()},"durationSeconds":${(11_000 * scale).toInt()},"legCount":${(22 * scale).toInt() + 1}}""",
                """{"periodStartDay":"$day","travelMode":"bike","distanceMeters":${(26_000 * scale).toInt()},"durationSeconds":${(5400 * scale).toInt()},"legCount":${(6 * scale).toInt() + 1}}""",
                """{"periodStartDay":"$day","travelMode":"train","distanceMeters":${(61_000 * scale).toInt()},"durationSeconds":${(3000 * scale).toInt()},"legCount":${(3 * scale).toInt() + 1}}""",
            )
        }.flatten().joinToString(",", "[", "]")
    }

    private fun sharingLimit(): String = when (scenario) {
        Scenario.Paused -> """{"shareLevelLimit":"","sharingLimitReason":"paused","phonePermission":"always","isRecordingOn":true,"reportedAt":"${agoText(5)}"}"""
        Scenario.Limited -> """{"shareLevelLimit":"current","sharingLimitReason":"phone_while_using","phonePermission":"while_using","isRecordingOn":true,"reportedAt":"${agoText(5)}"}"""
        else -> """{"shareLevelLimit":"history","sharingLimitReason":"","phonePermission":"always","isRecordingOn":true,"reportedAt":"${agoText(5)}"}"""
    }

    private fun shares(): String = when (scenario) {
        Scenario.Setup, Scenario.Empty -> "[]"
        else -> """[{"id":"share-1","granteeUserId":null,"granteeUsername":null,"granteeName":null,"granteeCircleId":"circle-1","granteeCircleName":"Example Household","shareLevel":"history","createdAt":"2026-09-01T08:00:00Z"},{"id":"share-2","granteeUserId":"alex","granteeUsername":"alex","granteeName":"Alex Sample","granteeCircleId":null,"granteeCircleName":null,"shareLevel":"current","createdAt":"2026-09-02T08:00:00Z"}]"""
    }

    private fun circles(request: DemoRequest): String =
        if (request.document.contains("circleKind")) {
            """[{"id":"circle-1","name":"Example Household","circleKind":"family","createdAt":"2026-09-01T08:00:00Z","memberRole":"admin","members":[{"userId":"robin","username":"robin","name":"Robin Example","avatarUrl":null,"avatarColorIndex":5,"memberRole":"admin"},{"userId":"sam","username":"sam","name":"Sam Example","avatarUrl":null,"avatarColorIndex":3,"memberRole":"member"}],"pendingInvitations":[]}]"""
        } else {
            """[{"id":"circle-1","name":"Example Household","members":[{"userId":"robin"},{"userId":"sam"},{"userId":"alex"}]}]"""
        }

    private fun foundPlace(name: String, category: String, distance: Double, placeId: String? = null, label: String = "", isNamed: Boolean = false, id: Long = 4200): String =
        """{"placeName":"$name","osmType":"node","osmId":$id,"osmCategory":"$category","addressText":"","latitudeDegrees":10.5,"longitudeDegrees":20.5,"distanceMeters":$distance,"placeId":${placeId?.let { "\"$it\"" } ?: "null"},"placeLabel":"$label","isNamedByPerson":$isNamed}"""

    private fun addressMatches(): String = """[
        {"placeName":"12 Elm Street","osmType":"way","osmId":9001,"osmCategory":"building=house","addressText":"12 Elm Street, Exampleton","latitudeDegrees":10.5,"longitudeDegrees":20.5,"distanceMeters":120,"placeId":null,"placeLabel":"","isNamedByPerson":false},
        {"placeName":"12 Elm Street","osmType":"way","osmId":9002,"osmCategory":"building=apartments","addressText":"12 Elm Street, Sampleford","latitudeDegrees":10.52,"longitudeDegrees":20.52,"distanceMeters":2400,"placeId":null,"placeLabel":"","isNamedByPerson":false}]"""

    private fun variablesText(request: DemoRequest, key: String): String? = request.variables.text(key)

    /** What demo mode answers for Location: one answerer for each document the screens send. Most specific first. */
    val answerers: List<DemoAnswerer> = listOf(
        graphQLField("GetLocation") { variables ->
            val userId = variables.text("userId")
            when {
                userId == null -> ownLocation()
                else -> people().firstOrNull { json -> json.contains("\"userId\":\"$userId\"") } ?: ownLocation()
            }
        },
        graphQLField("ListLocationVisits") { variables ->
            val userId = variables.text("userId")
            val placeId = variables.text("placeId")
            val from = variables.text("from")?.let { text -> ServerTime.instant(text) }
            val until = variables.text("until")?.let { text -> ServerTime.instant(text) }
            var rows = if (userId == HISTORY_PERSON_ID) samVisits() else ownVisits()
            if (placeId != null) rows = ownVisits().filter { json -> json.contains("\"place\":{\"id\":\"$placeId\"") }.ifEmpty { listOf(visit("visit-place-1", placeById(placeId), time(2, 9, 0), time(2, 10, 20)), visit("visit-place-2", placeById(placeId), time(5, 18, 0), time(5, 19, 5))) }
            if (from != null && until != null) {
                rows = rows.filter { json ->
                    val arrived = Regex("\"arrivedAt\":\"([^\"]+)\"").find(json)?.groupValues?.get(1)?.let { text -> ServerTime.instant(text) }
                    val departed = Regex("\"departedAt\":\"([^\"]+)\"").find(json)?.groupValues?.get(1)?.let { text -> ServerTime.instant(text) }
                    val anchor = arrived ?: departed
                    anchor != null && !anchor.isBefore(from) && anchor.isBefore(until)
                }
            }
            """{"placeVisits":${rows.joinToString(",", "[", "]")},"nextOffset":null}"""
        },
        graphQLField("ListPlaces") { variables ->
            val userId = variables.text("userId")
            val label = variables.text("placeLabel")
            val query = variables.text("query")?.lowercase()
            val shown = if (scenario == Scenario.Setup || scenario == Scenario.Empty) emptyList() else if (userId == HISTORY_PERSON_ID) samPlaces().toList() else places
            val filtered = shown.filter { place -> (label == null || place.label == label) && (query == null || place.name.lowercase().contains(query)) }
            """{"places":${filtered.joinToString(",", "[", "]") { place -> place.json(userId ?: "robin") }},"nextOffset":null}"""
        },
        graphQLField("ListNearbyPlaces") {
            """[${foundPlace("Corner Café", "amenity=cafe", 40.5, id = 1)},${foundPlace("Home", "", 8.0, CAFE_PLACE_ID, "home", true, id = 2)},${foundPlace("Riverside Park", "leisure=park", 190.0, id = 3)},${foundPlace("Elm Street Bakery", "shop=bakery", 260.0, id = 4)}]"""
        },
        graphQLField("SearchPlaces") { """{"foundPlaces":[${foundPlace("Harbor Library", "amenity=library", 800.0, id = 11)},${foundPlace("Harbor Pharmacy", "amenity=pharmacy", 950.0, id = 12)}],"nextOffset":null}""" },
        graphQLField("SearchAddresses") { addressMatches() },
        graphQLField("ListLocationPoints") { variables ->
            val userId = variables.text("userId")
            val steps = 30
            val now = Instant.now()
            val rows = (0 until steps).map { step ->
                val fraction = step / (steps - 1.0)
                val latitude = (if (userId == HISTORY_PERSON_ID) 10.4960 + (10.5080 - 10.4960) * fraction else 10.5000 + (10.5150 - 10.5000) * fraction)
                val longitude = (if (userId == HISTORY_PERSON_ID) 20.5120 + (20.4950 - 20.5120) * fraction else 20.5000 + (20.5200 - 20.5000) * fraction) + Math.sin(fraction * 5) * 0.0012
                pointJson(latitude, longitude, ServerTime.text(now.minusSeconds((steps - step) * 240L)))
            }
            rows.joinToString(",", "[", "]")
        },
        graphQLField("ListLocationPauses") { variables ->
            if (variables.text("userId") == HISTORY_PERSON_ID || scenario == Scenario.Empty) "[]"
            else """[${listOfNotNull(ownPause(), pauseJson("pause-earlier", time(1, 12, 0), time(1, 13, 0)))
                .joinToString(",")}]"""
        },
        graphQLField("ListTrips") { variables -> if (variables.text("userId") == HISTORY_PERSON_ID) """{"trips":[],"nextOffset":null}""" else """{"trips":${trips().joinToString(",", "[", "]")},"nextOffset":null}""" },
        graphQLField("ListTravelTotals") { variables -> totals(variables.text("period") ?: "week") },
        graphQLField("SetTripLegTravelMode") { variables ->
            val mode = variables.text("travelMode")
            """{"id":"${variables.text("tripLegId")}","startedAt":"${time(0, 15, 35)}","endedAt":"${time(0, 15, 50)}","durationSeconds":900,"distanceMeters":9800,"travelMode":"${mode ?: "car"}","detectedTravelMode":"car","isTravelModeCorrected":${mode != null},"detectionSource":"motion"}"""
        },
        graphQLField("ListSharedLocations") { people().joinToString(",", "[", "]") },
        graphQLField("ReadLocationSharingLimit") { sharingLimit() },
        graphQLField("ReportLocationPermission") { sharingLimit() },
        graphQLField("ListLocationShares") { shares() },
        graphQLField("ShareLocation") { variables ->
            """{"id":"share-new","granteeUserId":${variables.text("granteeUserId")?.let { "\"$it\"" } ?: "null"},"granteeUsername":null,"granteeName":null,"granteeCircleId":${variables.text("granteeCircleId")?.let { "\"$it\"" } ?: "null"},"granteeCircleName":"Example Household","shareLevel":"${variables.text("shareLevel")}","createdAt":"${agoText(0)}"}"""
        },
        graphQLField("UnshareLocation") { "true" },
        graphQLField("ListCirclePeople") { """[{"userId":"sam","username":"sam","name":"Sam Example","avatarUrl":null,"avatarColorIndex":3},{"userId":"alex","username":"alex","name":"Alex Sample","avatarUrl":null,"avatarColorIndex":7}]""" },
        DemoAnswerer { request ->
            if (Regex("""\bListCircles\b\s*[({]""").containsMatchIn(request.document)) DemoResponse("""{"data":{"ListCircles":${circles(request)}}}""") else null
        }.unlessAsking("pendingInvitations"),
        graphQLField("SavePlace") { variables ->
            val place = variables.text("placeId")?.let { id -> placeById(id) } ?: places[0]
            val label = variables.text("placeLabel") ?: place.label
            val name = variables.text("placeName") ?: place.name
            Place(place.id, name, variables.text("placeName") != null || place.isNamedByPerson, label, place.latitude, place.longitude, place.radius, place.category, place.visitCount, place.lastVisitDaysAgo, variables.text("addressText")?.isNotEmpty() ?: place.isAddressTyped).json()
        },
        graphQLField("ForgetPlaceVisit") { "true" },
        graphQLField("ForgetPlace") { "true" },
        graphQLField("CheckIn") { visit("visit-checked-in", places[4], agoText(0), null) },
        graphQLField("PauseLocation") { """${pauseJson("pause-now", agoText(0), ServerTime.text(Instant.now().plusSeconds(3600)))}""" },
        graphQLField("ResumeLocation") { "true" },
        graphQLField("MuteLocationNotifications") { """{"mutedUntil":"${ServerTime.text(Instant.now().plusSeconds(3600))}"}""" },
        graphQLField("UnmuteLocationNotifications") { "true" },
        graphQLField("EraseLocationHistory") { """{"pointCount":120,"visitCount":6,"placeCount":2}""" },
        graphQLField("ListWatchedStores") { "[]" },
        graphQLField("ReportLocation") { """{"newPointCount":0,"newVisitCount":0,"updatedVisitCount":0,"pausedPointCount":0,"pausedVisitCount":0,"pause":null}""" },
    )

    /** The demo's answer for a document whose answer is JSON text (used by the screenshot tests to check what was asked). */
    fun describe(request: DemoRequest): String = request.document.take(40)
}

/** What demo mode answers for Location, so every screen and state can be reached and photographed without a server. */
/**
 * Location's answers are given only while a Location screen is on screen ([LocationDemo.activate]). Contacts answers some of
 * the same documents (visits, places, circles) with its own small fixtures for a person's card, and the answerers are
 * asked in the order of `Areas`, Location's first: without this, a Contacts screen would be answered by Location's data.
 */
val locationDemoAnswerers: List<DemoAnswerer> = LocationDemo.answerers.map { answerer ->
    DemoAnswerer { request -> if (LocationDemo.isActive) answerer.answer(request) else null }
}
