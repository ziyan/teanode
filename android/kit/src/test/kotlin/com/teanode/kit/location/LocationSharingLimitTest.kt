package com.teanode.kit.location

import com.teanode.kit.TeaNodeJson
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/** What limits the people the person sees: what the server's limit decodes to, which banner applies, and who is reduced by what the person shares back. */
class LocationSharingLimitTest {
    private fun person(userId: String, shareLevel: String, grantedShareLevel: String?) =
        PersonLocation(userId = userId, username = userId, name = userId.replaceFirstChar { first -> first.uppercase() }, shareLevel = shareLevel, grantedShareLevel = grantedShareLevel)

    private fun limit(level: String, reason: String) = LocationSharingLimit(shareLevelLimit = level, sharingLimitReason = reason)

    @Test
    fun theLimitDecodesWhatTheServerSends() {
        val decoded = TeaNodeJson.decodeFromString(
            LocationSharingLimit.serializer(),
            """{"shareLevelLimit":"current","sharingLimitReason":"phone_while_using","phonePermission":"while_using","isRecordingOn":true,"reportedAt":null}""",
        )
        assertEquals("current", decoded.shareLevelLimit)
        assertEquals("phone_while_using", decoded.sharingLimitReason)
        assertEquals("while_using", decoded.phonePermission)
        assertEquals(true, decoded.isRecordingOn)
        assertNull(decoded.reportedAt)
    }

    @Test
    fun aPersonDecodesWhatTheyGrantAndNothingShown() {
        val decoded = TeaNodeJson.decodeFromString(
            PersonLocation.serializer(),
            """{"userId":"sam","username":"sam","name":"Sam","shareLevel":"","grantedShareLevel":"history","latestPoint":null,"openVisit":null,"pause":null}""",
        )
        assertEquals("history", decoded.grantedShareLevel)
        assertTrue(decoded.isHiddenFromCaller)
        assertNull(decoded.latestPoint)
        assertNull(decoded.openVisit)
        assertNull(decoded.pause)
    }

    @Test
    fun whileUsingGivesItsOwnBanner() {
        val people = listOf(person("sam", "current", "history"))
        // The limit is why Sam's visits are missing, not what the person shares back.
        assertEquals(LocationSharingBanner.PhoneWhileUsing, LocationSharingNotice.banner(limit("current", "phone_while_using"), people))
        assertTrue(LocationSharingNotice.reducedPeople(limit("current", "phone_while_using"), people).isEmpty())
    }

    @Test
    fun phoneLocationOffRecordingOffAndPausedEachHaveABanner() {
        val people = listOf(person("sam", "", "history"))
        assertEquals(LocationSharingBanner.PhoneLocationOff, LocationSharingNotice.banner(limit("", "phone_location_off"), people))
        assertEquals(LocationSharingBanner.RecordingOff, LocationSharingNotice.banner(limit("", "recording_off"), people))
        assertEquals(LocationSharingBanner.Paused, LocationSharingNotice.banner(limit("", "paused"), people))
    }

    @Test
    fun nobodyIsReducedBySharingWhenTheLimitIsLower() {
        // Everyone is hidden by the limit; none of it is the person's sharing.
        val people = listOf(person("sam", "", "history"), person("alex", "", "current"))
        assertTrue(LocationSharingNotice.reducedPeople(limit("", "paused"), people).isEmpty())
    }

    @Test
    fun sharingBackOnlyReducesPeopleWhoseShownLevelIsBelowWhatIsPossible() {
        val people = listOf(
            person("sam", "", "history"), person("alex", "current", "history"),
            person("casey", "current", "current"), person("morgan", "history", "history"),
        )
        val reduced = LocationSharingNotice.reducedPeople(limit("history", ""), people)
        assertEquals(listOf("sam", "alex"), reduced.map { each -> each.userId })
        assertEquals(listOf(true, false), reduced.map { each -> each.isSeeingNothing })
        assertEquals(LocationSharingBanner.SharesLess(reduced), LocationSharingNotice.banner(limit("history", ""), people))
    }

    @Test
    fun aLimitAtCurrentOnlyCountsWhatSharingBackCostsBelowIt() {
        // With the limit at current, Alex shown at current is as much as is possible.
        assertTrue(LocationSharingNotice.reducedPeople(limit("current", ""), listOf(person("alex", "current", "history"))).isEmpty())
    }

    @Test
    fun noRestrictionGivesNoBanner() {
        val people = listOf(person("sam", "history", "history"), person("alex", "current", "current"))
        assertNull(LocationSharingNotice.banner(limit("history", ""), people))
        assertNull(LocationSharingNotice.banner(null, people))
        assertNull(LocationSharingNotice.banner(null, emptyList()))
    }

    @Test
    fun aServerThatDoesNotSayWhatIsGrantedReducesNobody() {
        assertNull(LocationSharingNotice.banner(limit("history", ""), listOf(person("sam", "", null))))
    }

    @Test
    fun reportingThePermissionSendsBothValuesAndReadsTheLimit() = runBlocking {
        val (connection, fake) = connectionAnswering(
            """{"data":{"ReportLocationPermission":{"shareLevelLimit":"current","sharingLimitReason":"phone_while_using","phonePermission":"while_using","isRecordingOn":true,"reportedAt":"2024-01-02T14:00:00Z"}}}""",
        )
        val read = connection.reportLocationPermission(LocationPermissionReport(LocationPhonePermission.WhileUsing, true))
        assertEquals("phone_while_using", read.sharingLimitReason)
        val variables = sentVariables(fake)
        assertEquals(JsonPrimitive("while_using"), variables["phonePermission"])
        assertEquals(JsonPrimitive(true), variables["isRecordingOn"])
    }
}

/** Muting the arrived and left notices of one person sharing: what the server's mute decodes to, when it is in effect, and what each call sends. */
class LocationNotificationMuteTest {
    private fun decodePerson(notificationMute: String?): PersonLocation {
        val muteField = notificationMute?.let { text -> ""","notificationMute":$text""" }.orEmpty()
        return TeaNodeJson.decodeFromString(
            PersonLocation.serializer(),
            """{"userId":"user-2","username":"sam","name":"Sam","avatarUrl":null,"avatarColorIndex":3,"shareLevel":"current","latestPoint":null,"openVisit":null$muteField}""",
        )
    }

    @Test
    fun aPersonWithNoMuteDecodesWithNone() {
        assertNull(decodePerson(null).notificationMute)
        val person = decodePerson("null")
        assertNull(person.notificationMute)
        assertFalse(person.isNotificationMuted(instantOf("2024-01-02T14:00:00Z")))
    }

    @Test
    fun aPersonWithAMuteUntilATimeDecodesIt() {
        val person = decodePerson("""{"mutedUntil":"2024-01-02T15:00:00Z"}""")
        val mute = person.notificationMute!!
        assertEquals(instantOf("2024-01-02T15:00:00Z"), mute.mutedUntilInstant)
        assertFalse(mute.isOpenEnded)
        assertTrue(person.isNotificationMuted(instantOf("2024-01-02T14:59:59Z")))
        assertFalse(person.isNotificationMuted(instantOf("2024-01-02T15:00:00Z")))
    }

    @Test
    fun aMuteWithNoEndGoesOnUntilUnmuted() {
        val person = decodePerson("""{"mutedUntil":null}""")
        assertNull(person.notificationMute!!.mutedUntilInstant)
        assertTrue(person.notificationMute!!.isOpenEnded)
        assertTrue(person.isNotificationMuted(instantOf("2030-01-01T00:00:00Z")))
    }

    @Test
    fun mutesUntilTheEndGiven() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"MuteLocationNotifications":{"mutedUntil":"2024-01-02T15:00:00Z"}}}""")
        val made = connection.muteLocationNotifications("user-2", instantOf("2024-01-02T15:00:00Z"))
        assertEquals(instantOf("2024-01-02T15:00:00Z"), made?.mutedUntilInstant)
        assertEquals(mapOf("userId" to JsonPrimitive("user-2"), "mutedUntil" to JsonPrimitive("2024-01-02T15:00:00Z")), sentVariables(fake))
    }

    @Test
    fun mutesUntilUnmutedSendsNoEnd() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"MuteLocationNotifications":{"mutedUntil":null}}}""")
        assertEquals(true, connection.muteLocationNotifications("user-2")?.isOpenEnded)
        assertEquals(mapOf("userId" to JsonPrimitive("user-2")), sentVariables(fake))
    }

    @Test
    fun unmutes() = runBlocking {
        val (connection, fake) = connectionAnswering("""{"data":{"UnmuteLocationNotifications":true}}""")
        assertTrue(connection.unmuteLocationNotifications("user-2"))
        assertEquals(mapOf("userId" to JsonPrimitive("user-2")), sentVariables(fake))
        val (otherConnection, _) = connectionAnswering("""{"data":{"UnmuteLocationNotifications":false}}""")
        assertFalse(otherConnection.unmuteLocationNotifications("user-2"))
    }

    @Test
    fun theLocationDocumentsAskForTheMute() {
        assertTrue(LocationDocuments.SHARED_LOCATIONS.contains("notificationMute { mutedUntil }"))
        assertTrue(LocationDocuments.LOCATION.contains("notificationMute { mutedUntil }"))
        assertTrue(LocationDocuments.MUTE_LOCATION_NOTIFICATIONS.contains("\$userId: String!"))
        assertTrue(LocationDocuments.MUTE_LOCATION_NOTIFICATIONS.contains("\$mutedUntil: DateTime"))
    }
}
