package com.teanode.app.calendar

import com.teanode.kit.calendar.AgentCalendar
import com.teanode.kit.calendar.AgentReminder
import com.teanode.kit.calendar.CalendarEvent
import com.teanode.kit.calendar.FreeStretch
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.ZoneOffset

class CalendarLogicTest {
    private val utc: ZoneId = ZoneOffset.UTC
    private val day: LocalDate = LocalDate.of(2026, 3, 14)

    private fun event(id: String, startsAt: String, endsAt: String) =
        CalendarEvent(id = id, calendarId = "calendar-1", summary = id, startsAt = startsAt, endsAt = endsAt, allDay = false, recurring = false)

    @Test fun `events that overlap share the width and ones apart do not`() {
        val placed = placeEvents(
            listOf(
                event("a", "2026-03-14T09:00:00Z", "2026-03-14T10:30:00Z"),
                event("b", "2026-03-14T10:00:00Z", "2026-03-14T11:00:00Z"),
                event("c", "2026-03-14T13:00:00Z", "2026-03-14T14:00:00Z"),
            ),
            day, utc,
        )
        assertEquals(listOf(0, 1, 0), placed.map { found -> found.column })
        assertEquals(listOf(2, 2, 1), placed.map { found -> found.columns })
        assertEquals(9f, placed[0].top)
        assertEquals(1.5f, placed[0].height)
    }

    @Test fun `an event with no end is half an hour and a short one is a quarter at least`() {
        val placed = placeEvents(listOf(event("a", "2026-03-14T09:00:00Z", "2026-03-14T09:05:00Z")), day, utc)
        assertEquals(0.25f, placed[0].height)
    }

    @Test fun `a new event with an all day span is saved by its first and the day after its last`() {
        val calendar = AgentCalendar(id = "calendar-1", name = "Home", timezone = "UTC")
        val draft = EventDraft(summary = " Trip ", isAllDay = true, startsAt = Instant.parse("2026-03-14T10:00:00Z"), endsAt = Instant.parse("2026-03-15T10:00:00Z"))
        val change = draft.change(calendar, utc)
        assertEquals("save", change.operation)
        assertEquals("Trip", change.variables.getValue("summary").jsonPrimitive.content)
        assertEquals("2026-03-14", change.variables.getValue("startsAt").jsonPrimitive.content.take(10))
        assertEquals("2026-03-16", change.variables.getValue("endsAt").jsonPrimitive.content.take(10))
        assertEquals("", change.variables.getValue("timezone").jsonPrimitive.content)
    }

    @Test fun `a draft sends only the invited people of a calendar that is the person's own`() {
        val own = AgentCalendar(id = "calendar-1", name = "Home")
        val shared = AgentCalendar(id = "calendar-2", name = "Shared", shareLevel = "edit", ownerUserId = "user-2")
        val draft = EventDraft(summary = "Lunch", startsAt = Instant.parse("2026-03-14T12:00:00Z"), invited = "sam@example.com\n\n  ada@example.com ")
        val ownChange = draft.change(own, utc)
        assertEquals(2, (ownChange.variables.getValue("attendees") as JsonArray).size)
        assertEquals(JsonNull, draft.change(shared, utc).variables.getValue("attendees"))
        assertEquals(JsonNull, draft.change(shared, utc).variables.getValue("isPrivate"))
        assertEquals(JsonPrimitive(false), ownChange.variables.getValue("isPrivate"))
    }

    @Test fun `an end before the start is saved as the start`() {
        val calendar = AgentCalendar(id = "calendar-1", name = "Home", timezone = "UTC")
        val draft = EventDraft(summary = "Odd", startsAt = Instant.parse("2026-03-14T12:00:00Z"), endsAt = Instant.parse("2026-03-14T11:00:00Z"))
        val change = draft.change(calendar, utc)
        assertEquals(change.variables.getValue("startsAt"), change.variables.getValue("endsAt"))
    }

    @Test fun `a reminder with a past time is overdue until it is done`() {
        val now = Instant.parse("2026-03-14T12:00:00Z")
        val reminder = AgentReminder(id = "r1", title = "Call", dueAt = "2026-03-14T09:00:00Z", isDone = false)
        assertTrue(reminderDue(reminder, utc, now)!!.isOverdue)
        assertFalse(reminderDue(reminder.copy(isDone = true), utc, now)!!.isOverdue)
        assertFalse(reminderDue(reminder.copy(dueAt = "2026-03-14T18:00:00Z"), utc, now)!!.isOverdue)
    }

    @Test fun `a reminder with no time has no due text`() {
        assertNull(reminderDue(AgentReminder(id = "r1", title = "Call", isDone = false), utc, Instant.now()))
    }

    @Test fun `free stretches are gathered under their days in order`() {
        val first = Instant.parse("2026-03-14T00:00:00Z")
        val second = Instant.parse("2026-03-15T00:00:00Z")
        val groups = freeDays(
            listOf(
                FreeStretch(first, first.plusSeconds(9 * 3600), first.plusSeconds(10 * 3600)),
                FreeStretch(first, first.plusSeconds(13 * 3600), first.plusSeconds(14 * 3600)),
                FreeStretch(second, second.plusSeconds(9 * 3600), second.plusSeconds(10 * 3600)),
            ),
        )
        assertEquals(listOf(2, 1), groups.map { group -> group.stretches.size })
        assertNotNull(groups.firstOrNull())
        assertEquals(first, groups[0].day)
    }
}
