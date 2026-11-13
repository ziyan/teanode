package com.teanode.kit.notifications

import com.teanode.kit.Account
import com.teanode.kit.Connection
import com.teanode.kit.FakeServer
import com.teanode.kit.ServerAddress
import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.Instant
import java.time.ZoneId
import java.time.ZonedDateTime

class GlanceTest {
    private fun event(id: String, summary: String, startsAt: String, endsAt: String, isAllDay: Boolean = false, status: String? = null) =
        GlanceCalendarEvent(id, "c", summary, null, startsAt, endsAt, isAllDay, status)

    @Test
    fun onlyEventsNotOverAreShownSoonestFirst() {
        val zone = ZoneId.of("America/Los_Angeles")
        val now = Instant.parse("2026-03-10T17:00:00Z").toEpochMilli()
        val events = listOf(
            event("over", "Breakfast", "2026-03-10T15:00:00Z", "2026-03-10T16:00:00Z"),
            event("later", "Dentist", "2026-03-11T18:00:00Z", "2026-03-11T19:00:00Z"),
            event("now", "Standup", "2026-03-10T16:45:00Z", "2026-03-10T17:15:00Z"),
            event("off", "Called off", "2026-03-10T20:00:00Z", "2026-03-10T21:00:00Z", status = "CANCELLED"),
            event("day", "Holiday", "2026-03-11T00:00:00Z", "2026-03-12T00:00:00Z", isAllDay = true),
        )
        val upcoming = GlanceReading.upcoming(events, now, 3, zone)
        assertEquals(listOf("Standup", "Holiday", "Dentist"), upcoming.map { event -> event.title })
        // The all-day event starts at local midnight of its own day, not the evening before, as midnight UTC is west of Greenwich.
        val holiday = upcoming[1]
        assertTrue(holiday.isAllDay)
        val start = ZonedDateTime.ofInstant(Instant.ofEpochMilli(holiday.startsAtEpochMS), zone)
        assertEquals(listOf(3, 11, 0), listOf(start.monthValue, start.dayOfMonth, start.hour))
    }

    @Test
    fun theAnswersAreReadIntoAGlance() = runBlocking {
        val now = Instant.parse("2026-03-10T17:00:00Z").toEpochMilli()
        val fake = FakeServer { request ->
            val document = request.body!!.decodeToString()
            val body = when {
                document.contains("GetServiceCapabilities") ->
                    """{"data":{"GetServiceCapabilities":{"isMailRoutingEnabled":true,"isMailboxEnabled":true,"isCalendarEnabled":true,"isRemindersEnabled":true,"isContactsEnabled":true,"isPhotosEnabled":true,"isAssistantEnabled":true,"hasPendingServiceRestart":false,"hasCompletedSetup":true}}}"""
                document.contains("ReadAgent(") || document.contains("ReadAgent {") -> """{"data":{"ReadAgent":{"agent":{"id":"a","enabled":true,"name":"Tea"}}}}"""
                document.contains("ListCalendars") ->
                    """{"data":{"ListCalendars":[{"id":"own","calendarKind":"events"},{"id":"shared","shareLevel":"read"},{"id":"tasks","calendarKind":"reminders"},{"id":"circle","shareLevel":"read","isCircleCalendar":true}]}}"""
                document.contains("ListCalendarEvents") && document.contains("\"calendarId\":\"own\"") ->
                    """{"data":{"ListCalendarEvents":[{"id":"e1","calendarId":"own","summary":" Standup ","startsAt":"2026-03-10T18:00:00Z","endsAt":"2026-03-10T18:30:00Z","allDay":false},{"id":"e2","calendarId":"own","summary":"","startsAt":"2026-03-10T19:00:00Z","endsAt":"2026-03-10T20:00:00Z","allDay":false,"isBusyOnly":true}]}}"""
                document.contains("ListCalendarEvents") ->
                    """{"data":{"ListCalendarEvents":[{"id":"e3","calendarId":"circle","summary":"Picnic","startsAt":"2026-03-10T21:00:00Z","endsAt":"2026-03-10T22:00:00Z","allDay":false}]}}"""
                document.contains("ListMailboxes") -> """{"data":{"ListMailboxes":[{"unread":2},{"unread":3}]}}"""
                document.contains("ReadAgentWaiting") ->
                    """{"data":{"ReadAgentWaiting":{"waitingCount":3,"interactions":[{"id":"w1","conversationId":"c1","interactionKind":"question","interactionText":"Which day?","createdAt":"2026-03-10T16:00:00Z"}]}}}"""
                else -> """{"errors":[{"message":"unexpected"}]}"""
            }
            200 to body
        }
        val connection = Connection(Account(ServerAddress.parse("mail.example.com"), "a-client", "tnt_a", "", System.currentTimeMillis() + 3_600_000, "mira"), fake)
        val glance = connection.glance(now, zone = ZoneId.of("UTC"))
        assertEquals("Tea", glance.agentName)
        assertEquals(5, glance.unreadCount)
        assertEquals(3, glance.waitingCount)
        assertEquals("Which day?", glance.newestWaiting?.interactionText)
        // The reminders list and the calendar shared with the person are left out, and so is a busy block.
        assertEquals(listOf("Standup", "Picnic"), glance.events.map { event -> event.title })
    }

    @Test
    fun aPartThatCannotBeReadIsLeftEmptyRatherThanFailingTheRest() = runBlocking {
        val fake = FakeServer { 200 to """{"errors":[{"message":"nope"}]}""" }
        val connection = Connection(Account(ServerAddress.parse("mail.example.com"), "a-client", "tnt_a", "", System.currentTimeMillis() + 3_600_000, "mira"), fake)
        val glance = connection.glance(0)
        assertTrue(glance.events.isEmpty())
        assertEquals(0, glance.unreadCount)
        assertNull(glance.newestWaiting)
    }
}
