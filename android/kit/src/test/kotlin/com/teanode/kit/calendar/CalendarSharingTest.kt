package com.teanode.kit.calendar

import com.teanode.kit.TeaNodeJson
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class CalendarSharingTest {
    private fun calendar(json: String): AgentCalendar = TeaNodeJson.decodeFromString(AgentCalendar.serializer(), json)

    private fun event(json: String): CalendarEvent = TeaNodeJson.decodeFromString(CalendarEvent.serializer(), json)

    @Test
    fun aCalendarFromAServerThatKnowsNothingOfSharingIsTheirOwn() {
        val found = calendar("""{"id":"c","name":"Calendar","calendarKind":"events"}""")
        assertTrue(found.isOwn)
        assertTrue(found.canWrite)
        assertTrue(found.canRead)
        assertFalse(found.isNamedForOwner)
    }

    @Test
    fun aCalendarSharedAtTitlesIsReadOnlyAndNamedForItsOwner() {
        val found = calendar("""{"id":"c","name":"Calendar","calendarKind":"events","shareLevel":"titles","ownerUserId":"u","ownerName":"Ben","colorIndex":3}""")
        assertFalse(found.isOwn)
        assertTrue(found.canRead)
        assertFalse(found.canWrite)
        assertTrue(found.isNamedForOwner)
        assertEquals(CalendarShareLevel.Titles, found.level)
    }

    @Test
    fun aCalendarSharedAtBusyHasNoEventsToRead() {
        val found = calendar("""{"id":"c","name":"Calendar","calendarKind":"events","shareLevel":"busy","ownerName":"Cora"}""")
        assertFalse(found.canRead)
        assertFalse(found.canWrite)
    }

    @Test
    fun aLevelThisAppDoesNotKnowIsTheLeast() {
        assertEquals(CalendarShareLevel.Busy, calendar("""{"id":"c","name":"Calendar","shareLevel":"everything"}""").level)
    }

    @Test
    fun aCircleCalendarIsWrittenByEveryMemberAndNamedForTheCircle() {
        val found = calendar(
            """{"id":"c","name":"The household","calendarKind":"events","shareLevel":"edit","circleId":"circle","circleName":"Home","isCircleCalendar":true}""",
        )
        assertFalse(found.isOwn)
        assertTrue(found.canWrite)
        assertFalse(found.isNamedForOwner)
        assertEquals("circle", found.circleId)
    }

    @Test
    fun aSharedCalendarSaysWhoseItIsBesideItsEventsAndTheirOwnSaysNothing() {
        assertNull(calendar("""{"id":"c","name":"Calendar"}""").whoseName)
        assertEquals("Ben Example", calendar("""{"id":"c","name":"Calendar","shareLevel":"busy","ownerName":"Ben Example"}""").whoseName)
        assertEquals(
            "The household",
            calendar("""{"id":"c","name":"Calendar","shareLevel":"edit","isCircleCalendar":true,"circleName":"The household"}""").whoseName,
        )
        assertNull(calendar("""{"id":"c","name":"Calendar","shareLevel":"titles","ownerName":""}""").whoseName)
    }

    @Test
    fun aPickerNamesAPersonWithTheirUsernameInBrackets() {
        assertEquals("Ben Example (ben)", PersonWords.pickerLabel("Ben Example", "ben"))
        assertEquals("Ben Example (ben2)", PersonWords.pickerLabel("Ben Example", "ben2"))
        assertEquals("cora", PersonWords.pickerLabel(null, "cora"))
        assertEquals("cora", PersonWords.pickerLabel("  ", "cora"))
        assertEquals("cora", PersonWords.pickerLabel("cora", "cora"))
        assertEquals("Ben Example (ben)", CalendarSharePerson("u", "ben", "Ben Example").pickerLabel)
    }

    @Test
    fun aShareTargetIsAPersonOrACircleAndNeverBoth() {
        assertEquals("u1", ShareTarget.Person("u1").granteeUserId)
        assertNull(ShareTarget.Person("u1").granteeCircleId)
        assertEquals("c1", ShareTarget.Circle("c1").granteeCircleId)
        assertNull(ShareTarget.Circle("c1").granteeUserId)
    }

    @Test
    fun onlyTheFourLevelsCanBeGiven() {
        assertEquals(listOf(CalendarShareLevel.Busy, CalendarShareLevel.Titles, CalendarShareLevel.Details, CalendarShareLevel.Edit), CalendarShareLevel.grantable)
        assertFalse(CalendarShareLevel.grantable.contains(CalendarShareLevel.Owner))
        assertEquals(listOf(CalendarShareLevel.Edit, CalendarShareLevel.Owner), CalendarShareLevel.entries.filter { level -> level.canWrite })
        assertEquals(
            listOf(CalendarShareLevel.Titles, CalendarShareLevel.Details, CalendarShareLevel.Edit, CalendarShareLevel.Owner),
            CalendarShareLevel.entries.filter { level -> level.canRead },
        )
    }

    @Test
    fun aBusyBlockHasNothingToOpenAndNoneAreTheSameOccurrence() {
        val first = event("""{"id":"","calendarId":"ben","startsAt":"2030-01-02T10:00:00Z","endsAt":"2030-01-02T11:00:00Z","allDay":false,"recurring":false,"isBusyOnly":true}""")
        val second = event("""{"id":"","calendarId":"cora","startsAt":"2030-01-02T10:00:00Z","endsAt":"2030-01-02T11:00:00Z","allDay":false,"recurring":false,"isBusyOnly":true}""")
        val ordinary = event("""{"id":"e","calendarId":"ben","summary":"Lunch","startsAt":"2030-01-02T10:00:00Z","endsAt":"2030-01-02T11:00:00Z","allDay":false,"recurring":false}""")
        assertTrue(first.isBusy)
        assertFalse(ordinary.isBusy)
        // Two people busy at the same time are two blocks, which a list keyed by occurrence has to tell apart.
        assertNotEquals(first.occurrenceId, second.occurrenceId)
    }

    @Test
    fun aPrivateEventIsMarkedAndAnOlderServerSaysNothing() {
        val marked = event("""{"id":"e","calendarId":"own","summary":"Checkup","startsAt":"2030-01-02T10:00:00Z","endsAt":"2030-01-02T11:00:00Z","allDay":false,"recurring":false,"isPrivate":true}""")
        val plain = event("""{"id":"f","calendarId":"own","summary":"Lunch","startsAt":"2030-01-02T12:00:00Z","endsAt":"2030-01-02T13:00:00Z","allDay":false,"recurring":false,"isPrivate":false}""")
        val older = event("""{"id":"g","calendarId":"own","summary":"Walk","startsAt":"2030-01-02T14:00:00Z","endsAt":"2030-01-02T15:00:00Z","allDay":false,"recurring":false}""")
        assertTrue(marked.isMarkedPrivate)
        assertFalse(plain.isMarkedPrivate)
        assertFalse(older.isMarkedPrivate)

        val detail = TeaNodeJson.decodeFromString(
            CalendarEventDetail.serializer(),
            """{"id":"e","summary":"Checkup","startsAt":"2030-01-02T10:00:00Z","endsAt":"2030-01-02T11:00:00Z","allDay":false,"recurring":false,"isPrivate":true}""",
        )
        assertEquals(true, detail.isPrivate)

        // The documents carry the field both ways.
        assertTrue(CalendarDocuments.CALENDAR_EVENTS.contains("isPrivate"))
        assertTrue(CalendarDocuments.CALENDAR_EVENT.contains("isPrivate"))
        assertTrue(CalendarDocuments.SAVE_CALENDAR_EVENT.contains("isPrivate: \$isPrivate"))
    }

    @Test
    fun aShareNamesWhoItIsWith() {
        fun share(json: String): CalendarShare = TeaNodeJson.decodeFromString(CalendarShare.serializer(), json)
        val person = share("""{"id":"s1","calendarId":"c","granteeUserId":"u","granteeUsername":"ben","granteeName":"Ben","shareLevel":"titles"}""")
        assertEquals("Ben", person.granteeDisplayName)
        assertFalse(person.isWithCircle)
        assertEquals(CalendarShareLevel.Titles, person.level)
        assertEquals("ben", share("""{"id":"s2","calendarId":"c","granteeUserId":"u","granteeUsername":"ben","granteeName":"","shareLevel":"busy"}""").granteeDisplayName)
        val circle = share("""{"id":"s3","calendarId":"c","granteeCircleId":"k","granteeCircleName":"Home","shareLevel":"edit"}""")
        assertTrue(circle.isWithCircle)
        assertEquals("Home", circle.granteeDisplayName)
    }

    @Test
    fun aCircleToShareWithSaysHowManyPeopleItIs() {
        val circle = TeaNodeJson.decodeFromString(
            CalendarShareCircle.serializer(), """{"id":"c1","name":"Home","members":[{"userId":"a"},{"userId":"b"},{"userId":"c"},{"userId":"d"}]}""",
        )
        assertEquals(4, circle.memberCount)
        assertTrue(CalendarDocuments.CALENDAR_SHARE_CIRCLES.contains("members"))
    }

    /** A share is changed to another level by sharing again with the same person or circle, which the server keeps one share for. */
    @Test
    fun changingTheLevelOfAShareSharesAgainWithTheSameGrantee() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"ShareCalendar":{"id":"s1"}}}""" }
        val share = TeaNodeJson.decodeFromString(
            CalendarShare.serializer(), """{"id":"s1","calendarId":"c","granteeCircleId":"k","granteeCircleName":"Home","shareLevel":"busy"}""",
        )
        connection.shareCalendar(share.calendarId, share.granteeUserId, share.granteeCircleId, CalendarShareLevel.Edit)
        assertEquals(
            mapOf("calendarId" to JsonPrimitive("c"), "granteeUserId" to JsonNull, "granteeCircleId" to JsonPrimitive("k"), "shareLevel" to JsonPrimitive("edit")),
            sentVariables(fake),
        )
    }

    @Test
    fun theDocumentsAskForWhatTheModelsRead() {
        assertTrue(CalendarDocuments.CALENDARS.contains("shareLevel"))
        assertTrue(CalendarDocuments.CALENDAR_EVENTS.contains("isBusyOnly"))
    }
}
