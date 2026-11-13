package com.teanode.kit.calendar

import com.teanode.kit.Connection
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import java.time.Instant
import java.util.UUID

/**
 * CalendarChange is a change to the calendar sent with an id of its own, so a change whose answer was lost is never
 * made twice: before sending again, the server is asked whether it already happened.
 */
@Serializable
data class CalendarChange(
    /** "save" or "delete". */
    val operation: String,
    /** What the change is to, for the person: the event's title. */
    val summary: String,
    val requestId: String = newRequestId(),
    val variables: Map<String, JsonElement>,
) {
    companion object {
        /** Sixteen random bytes as hexadecimal, the form the server's request ids take. */
        fun newRequestId(): String = UUID.randomUUID().toString().replace("-", "")
    }
}

@Serializable
private class CalendarsAnswer(val ListCalendars: List<AgentCalendar>)

@Serializable
private class SharesAnswer(val ListCalendarShares: List<CalendarShare>)

@Serializable
private class SharePeopleAnswer(val ListCirclePeople: List<CalendarSharePerson>)

@Serializable
private class ShareCirclesAnswer(val ListCircles: List<CalendarShareCircle>)

@Serializable
private class CircleRolesAnswer(val ListCircles: List<CalendarCircle>)

@Serializable
private class EventsAnswer(val ListCalendarEvents: List<CalendarEvent>)

@Serializable
private class EventAnswer(val GetCalendarEvent: CalendarEventDetail)

@Serializable
private class RemindersAnswer(val ListReminders: List<AgentReminder>)

@Serializable
private class SavedIdentifier(val id: String)

@Serializable
private class AskedAnswer(val GetCalendarRequest: RequestIdentifier? = null)

@Serializable
private class CancelledAnswer(val CancelCalendarRequest: RequestIdentifier? = null)

@Serializable
private class RequestIdentifier(val requestId: String)

@Serializable
private class SaveEventAnswer(val SaveCalendarEvent: SavedIdentifier)

@Serializable
private class DeleteEventAnswer(val DeleteCalendarEvent: Boolean? = null)

@Serializable
private class ShareAnswer(val ShareCalendar: SavedIdentifier)

@Serializable
private class UnshareAnswer(val UnshareCalendar: Boolean? = null)

@Serializable
private class SaveCalendarAnswer(val SaveCalendar: SavedIdentifier)

@Serializable
private class CreateCircleCalendarAnswer(val CreateCircleCalendar: SavedIdentifier)

@Serializable
private class DeleteCircleCalendarAnswer(val DeleteCircleCalendar: Boolean? = null)

@Serializable
private class SaveReminderAnswer(val SaveReminder: SavedIdentifier)

@Serializable
private class SetReminderDoneAnswer(val SetReminderDone: SavedIdentifier)

@Serializable
private class DeleteReminderAnswer(val DeleteReminder: Boolean? = null)

private fun text(value: String?): JsonElement = if (value == null) JsonNull else JsonPrimitive(value)

suspend fun Connection.calendars(): List<AgentCalendar> = execute<CalendarsAnswer>(CalendarDocuments.CALENDARS).ListCalendars

/** Who the person's own calendar is shared with, and at what level. */
suspend fun Connection.calendarShares(calendarId: String): List<CalendarShare> =
    execute<SharesAnswer>(CalendarDocuments.CALENDAR_SHARES, mapOf("calendarId" to JsonPrimitive(calendarId))).ListCalendarShares

/** Shares the person's calendar with a person or a circle (one of the two identifiers), or changes the level of a share already made. */
suspend fun Connection.shareCalendar(calendarId: String, granteeUserId: String?, granteeCircleId: String?, level: CalendarShareLevel) {
    execute<ShareAnswer>(
        CalendarDocuments.SHARE_CALENDAR,
        mapOf(
            "calendarId" to JsonPrimitive(calendarId), "granteeUserId" to text(granteeUserId),
            "granteeCircleId" to text(granteeCircleId), "shareLevel" to JsonPrimitive(level.wireName),
        ),
    )
}

/** Takes a share back. */
suspend fun Connection.unshareCalendar(shareId: String) {
    execute<UnshareAnswer>(CalendarDocuments.UNSHARE_CALENDAR, mapOf("shareId" to JsonPrimitive(shareId)))
}

/** The people the person shares a circle with, who their calendar can be shared with. */
suspend fun Connection.calendarSharePeople(): List<CalendarSharePerson> =
    execute<SharePeopleAnswer>(CalendarDocuments.CALENDAR_SHARE_PEOPLE).ListCirclePeople

/** The circles the person is in, which their calendar can be shared with. */
suspend fun Connection.calendarShareCircles(): List<CalendarShareCircle> =
    execute<ShareCirclesAnswer>(CalendarDocuments.CALENDAR_SHARE_CIRCLES).ListCircles

/**
 * Renames a calendar of the person's own, or a circle's they are an admin of. The server replaces every field it is
 * sent, so the ones the app does not change go back as the calendar holds them: leaving them out would clear its
 * description, its color, the zone a new event is written in and the day its weeks start on.
 */
suspend fun Connection.saveCalendar(calendar: AgentCalendar, name: String) {
    execute<SaveCalendarAnswer>(
        CalendarDocuments.SAVE_CALENDAR,
        mapOf(
            "id" to JsonPrimitive(calendar.id), "name" to JsonPrimitive(name),
            "description" to JsonPrimitive(calendar.description.orEmpty()), "colour" to JsonPrimitive(calendar.colour.orEmpty()),
            "timezone" to JsonPrimitive(calendar.timezone.orEmpty()), "weekStart" to JsonPrimitive(calendar.weekStart.orEmpty()),
        ),
    )
}

/** The circles the person is in with their role in each: the ones they are an admin of may have a calendar made, renamed and deleted. */
suspend fun Connection.calendarCircles(): List<CalendarCircle> = execute<CircleRolesAnswer>(CalendarDocuments.CIRCLE_ROLES).ListCircles

/** Makes the calendar a circle keeps for itself. Admins only. */
suspend fun Connection.createCircleCalendar(circleId: String) {
    execute<CreateCircleCalendarAnswer>(CalendarDocuments.CREATE_CIRCLE_CALENDAR, mapOf("circleId" to JsonPrimitive(circleId)))
}

/** Deletes the calendar a circle keeps for itself, with every event in it, for everyone in the circle. Admins only. */
suspend fun Connection.deleteCircleCalendar(circleId: String) {
    execute<DeleteCircleCalendarAnswer>(CalendarDocuments.DELETE_CIRCLE_CALENDAR, mapOf("circleId" to JsonPrimitive(circleId)))
}

suspend fun Connection.calendarEvents(calendarId: String, from: Instant, until: Instant): List<CalendarEvent> =
    execute<EventsAnswer>(
        CalendarDocuments.CALENDAR_EVENTS,
        mapOf("calendarId" to JsonPrimitive(calendarId), "from" to JsonPrimitive(CalendarDates.text(from)), "until" to JsonPrimitive(CalendarDates.text(until))),
    ).ListCalendarEvents

suspend fun Connection.calendarEvent(calendarId: String, id: String): CalendarEventDetail =
    execute<EventAnswer>(CalendarDocuments.CALENDAR_EVENT, mapOf("calendarId" to JsonPrimitive(calendarId), "id" to JsonPrimitive(id))).GetCalendarEvent

/** Makes a calendar change. A change sent before whose answer was lost is asked about first, and sent again only if it did not happen. */
suspend fun Connection.perform(change: CalendarChange, isRetry: Boolean) {
    if (isRetry) {
        val asked = execute<AskedAnswer>(CalendarDocuments.CALENDAR_REQUEST, mapOf("requestId" to JsonPrimitive(change.requestId)))
        if (asked.GetCalendarRequest != null) return
    }
    val variables = change.variables + ("requestId" to JsonPrimitive(change.requestId))
    if (change.operation == "delete") {
        execute<DeleteEventAnswer>(CalendarDocuments.DELETE_CALENDAR_EVENT, variables)
    } else {
        execute<SaveEventAnswer>(CalendarDocuments.SAVE_CALENDAR_EVENT, variables)
    }
}

/** Stops trying a change; true when it had already happened, which stopping does not undo. */
suspend fun Connection.cancel(change: CalendarChange): Boolean =
    execute<CancelledAnswer>(CalendarDocuments.CANCEL_CALENDAR_REQUEST, mapOf("requestId" to JsonPrimitive(change.requestId))).CancelCalendarRequest != null

suspend fun Connection.reminders(isDone: Boolean): List<AgentReminder> =
    execute<RemindersAnswer>(CalendarDocuments.REMINDERS, mapOf("isDone" to JsonPrimitive(isDone))).ListReminders

/** Saves a reminder: only the fields given change. */
suspend fun Connection.saveReminder(reminderId: String?, fields: Map<String, JsonElement>) {
    val variables = if (reminderId == null) fields else fields + ("reminderId" to JsonPrimitive(reminderId))
    execute<SaveReminderAnswer>(CalendarDocuments.SAVE_REMINDER, variables)
}

suspend fun Connection.setReminderDone(reminderId: String, isDone: Boolean) {
    execute<SetReminderDoneAnswer>(
        CalendarDocuments.SET_REMINDER_DONE, mapOf("reminderId" to JsonPrimitive(reminderId), "isDone" to JsonPrimitive(isDone)),
    )
}

suspend fun Connection.deleteReminder(reminderId: String) {
    execute<DeleteReminderAnswer>(CalendarDocuments.DELETE_REMINDER, mapOf("reminderId" to JsonPrimitive(reminderId)))
}
