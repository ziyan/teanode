package com.teanode.app.calendar

import com.teanode.kit.calendar.AgentCalendar
import com.teanode.kit.calendar.CalendarChange
import com.teanode.kit.calendar.CalendarDates
import com.teanode.kit.calendar.CalendarDay
import com.teanode.kit.calendar.CalendarEventDetail
import com.teanode.kit.calendar.CalendarRepeat
import com.teanode.kit.calendar.ServerTime
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import java.time.Instant
import java.time.ZoneId

/** EventDraft is the event editor's fields, read from the event whole or made for a new one, and written back as every field (ios CalendarModel.swift, EventDraft). */
data class EventDraft(
    val eventId: String? = null,
    val summary: String = "",
    val location: String = "",
    val notes: String = "",
    val isAllDay: Boolean = false,
    val startsAt: Instant,
    val endsAt: Instant = startsAt.plusSeconds(3600),
    val recurrence: String = "",
    val status: String = "",
    /** Whether the owner marked it private, so that people the calendar is shared with see only that they are busy. */
    val isPrivate: Boolean = false,
    val invited: String = "",
    /** The calendar it is in or goes in: the person's own unless they chose another. Null until the calendars are read. */
    val calendarId: String? = null,
) {
    /** What the repeat is read against: the day it starts, the zone its days are counted in (the calendar's own, as the change below sends) and whether it is a whole-day event. */
    fun repeatContext(calendar: AgentCalendar?, deviceZone: ZoneId = ZoneId.systemDefault()): CalendarRepeat.Context {
        val zoneName = calendar?.timezone?.takeIf { name -> name.isNotEmpty() } ?: deviceZone.id
        val zone = try {
            ZoneId.of(zoneName)
        } catch (_: Exception) {
            deviceZone
        }
        return CalendarRepeat.Context(CalendarDay.containing(startsAt, deviceZone), zone, isAllDay)
    }

    /** The change that saves it: every field, an empty one clearing it. */
    fun change(calendar: AgentCalendar, deviceZone: ZoneId = ZoneId.systemDefault()): CalendarChange {
        val variables = LinkedHashMap<String, JsonElement>()
        variables["calendarId"] = JsonPrimitive(calendar.id)
        variables["summary"] = JsonPrimitive(summary.trim())
        variables["location"] = JsonPrimitive(location.trim())
        variables["description"] = JsonPrimitive(notes.trim())
        variables["allDay"] = JsonPrimitive(isAllDay)
        variables["recurrence"] = JsonPrimitive(recurrence.trim())
        variables["status"] = JsonPrimitive(status)
        // Only in the person's own calendar: in a circle's, or one shared with them at edit, every reader is an editor
        // and sees the event whatever it says.
        variables["isPrivate"] = if (calendar.isOwn) JsonPrimitive(isPrivate) else JsonNull
        // Only in the person's own calendar: the invitations go out from, and the answers come back to, the owner, so
        // a circle's calendar or one shared with them has none.
        variables["attendees"] = if (calendar.isOwn) {
            JsonArray(invited.lines().map { line -> line.trim() }.filter { line -> line.isNotEmpty() }.map { line -> JsonPrimitive(line) })
        } else {
            JsonNull
        }
        variables["id"] = eventId?.let { id -> JsonPrimitive(id) } ?: JsonNull
        if (isAllDay) {
            val first = CalendarDay.containing(startsAt, deviceZone)
            val last = CalendarDay.containing(maxOf(startsAt, endsAt), deviceZone)
            variables["startsAt"] = JsonPrimitive(CalendarDates.allDayText(first))
            variables["endsAt"] = JsonPrimitive(CalendarDates.allDayText(CalendarDates.dayAfter(last)))
            variables["timezone"] = JsonPrimitive("")
        } else {
            variables["startsAt"] = JsonPrimitive(CalendarDates.text(startsAt))
            variables["endsAt"] = JsonPrimitive(CalendarDates.text(maxOf(endsAt, startsAt)))
            variables["timezone"] = JsonPrimitive(calendar.timezone?.takeIf { name -> name.isNotEmpty() } ?: deviceZone.id)
        }
        return CalendarChange("save", summary, variables = variables)
    }

    companion object {
        /** A draft read from the event whole. */
        fun of(detail: CalendarEventDetail, calendarId: String?, deviceZone: ZoneId = ZoneId.systemDefault()): EventDraft {
            val startsAt: Instant
            val endsAt: Instant
            if (detail.allDay) {
                val start = CalendarDates.dayOfAllDay(detail.startsAt)
                val last = CalendarDates.lastDay(detail.startsAt, detail.endsAt)
                startsAt = start?.firstMoment(deviceZone) ?: Instant.now()
                endsAt = last?.firstMoment(deviceZone) ?: startsAt
            } else {
                startsAt = ServerTime.instant(detail.startsAt) ?: Instant.now()
                endsAt = ServerTime.instant(detail.endsAt) ?: startsAt.plusSeconds(3600)
            }
            return EventDraft(
                eventId = detail.id,
                summary = detail.summary.orEmpty(),
                location = detail.location.orEmpty(),
                notes = detail.description.orEmpty(),
                isAllDay = detail.allDay,
                startsAt = startsAt,
                endsAt = endsAt,
                recurrence = detail.recurrence.orEmpty(),
                status = detail.status.orEmpty(),
                isPrivate = detail.isPrivate ?: false,
                invited = detail.attendees.orEmpty().joinToString("\n") { attendee -> attendee.address },
                calendarId = calendarId,
            )
        }
    }
}
