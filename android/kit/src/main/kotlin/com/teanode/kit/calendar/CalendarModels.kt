package com.teanode.kit.calendar

import kotlinx.serialization.Serializable
import java.time.LocalDate
import java.time.ZoneId
import java.time.ZoneOffset
import java.time.ZonedDateTime
import java.time.format.DateTimeFormatter

// The calendar and its reminders, as the dashboard's Calendar page reads and changes them.

/**
 * CalendarShareLevel is how much of a calendar somebody may see, and whether they may change it; the dashboard, the
 * command line and the agent use the same four words.
 */
enum class CalendarShareLevel(val wireName: String) {
    /** When the owner is busy, and nothing else. */
    Busy("busy"),

    /** The time, title and place of each event. */
    Titles("titles"),

    /** Everything in each event, guests included. */
    Details("details"),

    /** Details, and the reader may add, change and remove events. */
    Edit("edit"),

    /** The person's own calendar; nobody is given this. */
    Owner("owner");

    /** Whether events are listed, rather than only busy time. */
    val canRead: Boolean get() = this != Busy

    /** Whether events may be added, changed and removed. */
    val canWrite: Boolean get() = this == Edit || this == Owner

    companion object {
        /** The levels a calendar can be shared at, least to most. */
        val grantable: List<CalendarShareLevel> = listOf(Busy, Titles, Details, Edit)

        fun fromWireName(wireName: String?): CalendarShareLevel? = entries.firstOrNull { level -> level.wireName == wireName }
    }
}

/** AgentCalendar is one of the person's calendars: the events one, or the reminders one. */
@Serializable
data class AgentCalendar(
    val id: String,
    val name: String,
    /** What it is for; null when it has none written. */
    val description: String? = null,
    /** What its events are painted, as "#rrggbb"; null when none was chosen. */
    val colour: String? = null,
    val timezone: String? = null,
    /** "sunday" or "monday". */
    val weekStart: String? = null,
    /** "events" or "reminders". */
    val calendarKind: String? = null,
    /**
     * What the person may do with it: owner for their own, busy, titles, details or edit for one shared with them,
     * and edit for a circle's. Null from a server that does not know about sharing, which is their own.
     */
    val shareLevel: String? = null,
    /** Whose it is, for one shared with them; null for a circle's, which belongs to the circle. */
    val ownerUserId: String? = null,
    val ownerName: String? = null,
    val ownerAvatarUrl: String? = null,
    val ownerAvatarColorIndex: Int? = null,
    val circleId: String? = null,
    val circleName: String? = null,
    val isCircleCalendar: Boolean? = null,
    /** Which of eight colors it is painted in when it has none of its own, the same one the dashboard uses. */
    val colorIndex: Int? = null,
) {
    /** Whether it is the person's own calendar. */
    val isOwn: Boolean get() = shareLevel == null || shareLevel == CalendarShareLevel.Owner.wireName

    /**
     * How much of it the person may do. A server that does not say means their own; a level this app does not know is
     * the least, because guessing more would offer to change what the server refuses.
     */
    val level: CalendarShareLevel
        get() = if (shareLevel == null) CalendarShareLevel.Owner else CalendarShareLevel.fromWireName(shareLevel) ?: CalendarShareLevel.Busy

    val canWrite: Boolean get() = level.canWrite

    val canRead: Boolean get() = level.canRead

    /**
     * What a list calls it: its own name for the person's and for a circle's, and whose it is for another person's,
     * since everybody's is called "Calendar". The possessive is the screen's to write.
     */
    val isNamedForOwner: Boolean get() = !isOwn && isCircleCalendar != true && !ownerName.isNullOrEmpty()

    /**
     * Whose it is, to be said beside an event of it when several calendars are on one screen: the owner's name for one
     * shared with the person, the circle's name for a circle's, and null for their own.
     */
    val whoseName: String?
        get() {
            if (isOwn) return null
            val whose = if (isCircleCalendar == true) circleName ?: name else ownerName.orEmpty()
            return whose.ifEmpty { null }
        }
}

/** CalendarShare is one share of the person's calendar, with one person or every member of one circle. */
@Serializable
data class CalendarShare(
    val id: String,
    val calendarId: String,
    val granteeUserId: String? = null,
    val granteeUsername: String? = null,
    val granteeName: String? = null,
    val granteeCircleId: String? = null,
    val granteeCircleName: String? = null,
    /** busy, titles, details or edit. */
    val shareLevel: String,
) {
    val level: CalendarShareLevel? get() = CalendarShareLevel.fromWireName(shareLevel)

    /** Whether it is with a circle rather than a person. */
    val isWithCircle: Boolean get() = granteeCircleId != null

    /** Who it is with: the person's name, or the username when they have no name, or the circle's name. */
    val granteeDisplayName: String
        get() {
            if (isWithCircle) return granteeCircleName.orEmpty()
            if (!granteeName.isNullOrEmpty()) return granteeName
            return granteeUsername.orEmpty()
        }
}

/** CalendarSharePerson is somebody the person shares a circle with, and so may share their calendar with. */
@Serializable
data class CalendarSharePerson(
    val userId: String,
    val username: String,
    val name: String? = null,
    val avatarUrl: String? = null,
    val avatarColorIndex: Int? = null,
) {
    val displayName: String get() = if (name.isNullOrEmpty()) username else name

    /** How a picker names them: the name and, in brackets, the username, so two people with one name tell apart. */
    val pickerLabel: String get() = PersonWords.pickerLabel(name, username)
}

/** PersonWords are the words for a person that more than one screen shares. */
object PersonWords {
    /** "Ada Example (ada)": the name and the username; the username alone for a person with no name, or whose name is their username. */
    fun pickerLabel(name: String?, username: String): String {
        val trimmed = name?.trim().orEmpty()
        return if (trimmed.isEmpty() || trimmed == username) username else "$trimmed ($username)"
    }
}

/** ShareTarget is who a calendar or a page is shared with, as one choice: a person, or a whole circle. The server takes one of the two. */
sealed class ShareTarget {
    data class Person(val userId: String) : ShareTarget()

    data class Circle(val circleId: String) : ShareTarget()

    val granteeUserId: String? get() = (this as? Person)?.userId

    val granteeCircleId: String? get() = (this as? Circle)?.circleId
}

/** CalendarShareCircle is a circle the person is in, which their calendar can be shared with whole. */
@Serializable
data class CalendarShareCircle(
    val id: String,
    val name: String,
    /** Who is in it, so a picker can say how many people a circle is. */
    val members: List<Member>,
) {
    val memberCount: Int get() = members.size

    @Serializable
    data class Member(val userId: String)
}

/** CalendarEvent is one occurrence of an event in a window. */
@Serializable
data class CalendarEvent(
    val id: String,
    val calendarId: String,
    val summary: String? = null,
    val location: String? = null,
    /** RFC 3339: the occurrence's times. */
    val startsAt: String,
    val endsAt: String,
    val allDay: Boolean,
    val recurring: Boolean,
    /** Whether this is one time of a repeating event. */
    val occurrence: Boolean? = null,
    val status: String? = null,
    /** Whether all the person is told of it is that the owner is busy. Its title is empty and the screen says "Busy". */
    val isBusyOnly: Boolean? = null,
    /** Whether the owner marked it private, which the server only says of the person's own events. */
    val isPrivate: Boolean? = null,
) {
    /** A stable identity for one occurrence of a repeating event. The calendar is part of it: a busy block has no identifier of its own. */
    val occurrenceId: String get() = "$calendarId-$id-$startsAt"

    /** Whether there is nothing in it to open. */
    val isBusy: Boolean get() = isBusyOnly == true

    /** Whether to draw the lock beside it. */
    val isMarkedPrivate: Boolean get() = isPrivate == true

    /** Whether it was called off; the server writes the iCalendar status, "CANCELLED". */
    val isCancelled: Boolean get() = status?.uppercase() == "CANCELLED"
}

/** CalendarEventDetail is one event whole, read before it is changed: the dashboard sends every field back. */
@Serializable
data class CalendarEventDetail(
    val id: String,
    val summary: String? = null,
    val location: String? = null,
    val description: String? = null,
    val startsAt: String,
    val endsAt: String,
    val allDay: Boolean,
    val recurring: Boolean,
    /** The RRULE without its name, such as FREQ=WEEKLY;BYDAY=MO. */
    val recurrence: String? = null,
    val status: String? = null,
    val timezone: String? = null,
    val isPrivate: Boolean? = null,
    val organizer: String? = null,
    val attendees: List<Attendee>? = null,
) {
    @Serializable
    data class Attendee(
        val address: String,
        val name: String? = null,
        /** NEEDS-ACTION, ACCEPTED, DECLINED or TENTATIVE. */
        val participation: String? = null,
        val role: String? = null,
    )
}

/** AgentReminder is one reminder, from the reminders calendar. */
@Serializable
data class AgentReminder(
    val id: String,
    val title: String,
    val notes: String? = null,
    val dueAt: String? = null,
    /** Due on a day rather than at a time: the day is read from UTC parts. */
    val isDueDate: Boolean? = null,
    val isDone: Boolean,
    val doneAt: String? = null,
    val priority: Int? = null,
    val isRepeating: Boolean? = null,
)

/** CalendarDates are the calendar's rules for writing and reading times. */
object CalendarDates {
    /**
     * The day an all-day event or a dated reminder is on. It is stored as midnight UTC of that day, so it is read from
     * UTC parts: read in local time, anyone west of Greenwich would see the day before.
     */
    fun dayOfAllDay(text: String?): CalendarDay? {
        val moment = ServerTime.instant(text) ?: return null
        return CalendarDay.of(moment.atOffset(ZoneOffset.UTC).toLocalDate())
    }

    /** The UTC midnight a day is written as. */
    fun allDayText(day: CalendarDay): String = "%04d-%02d-%02dT00:00:00Z".format(day.year, day.month, day.day)

    /** The day after, which an all-day event's end is: the end is exclusive. */
    fun dayAfter(day: CalendarDay): CalendarDay = CalendarDay.of(day.toLocalDate().plusDays(1))

    /** The last day an all-day event covers: the day before its exclusive end, never before its start. */
    fun lastDay(startsAt: String, endsAt: String): CalendarDay? {
        val start = dayOfAllDay(startsAt) ?: return null
        val end = dayOfAllDay(endsAt) ?: return start
        val last = end.toLocalDate().minusDays(1)
        val chosen = if (last.isBefore(start.toLocalDate())) start.toLocalDate() else last
        return CalendarDay.of(chosen)
    }

    /** A time as the server takes it: RFC 3339 in UTC. */
    fun text(moment: java.time.Instant): String = DateTimeFormatter.ISO_INSTANT.format(moment.truncatedTo(java.time.temporal.ChronoUnit.SECONDS))

    /** The day an event appears on in this time zone: an all-day event's own day, a timed one's local start. */
    fun localDay(event: CalendarEvent, zone: ZoneId): CalendarDay? {
        if (event.allDay) return dayOfAllDay(event.startsAt)
        val moment = ServerTime.instant(event.startsAt) ?: return null
        return CalendarDay.of(ZonedDateTime.ofInstant(moment, zone).toLocalDate())
    }

    internal fun localDate(day: CalendarDay): LocalDate = day.toLocalDate()
}

/** CalendarCircle is a circle the person is in, as far as the calendar needs it: whether they are an admin, who may rename and delete its calendar and make one. */
@Serializable
data class CalendarCircle(
    val id: String,
    val name: String,
    /** family, friends or colleagues. */
    val circleKind: String,
    /** The caller's own role in it: admin or member. */
    val memberRole: String,
) {
    val isAdmin: Boolean get() = memberRole == "admin"

    val isFamily: Boolean get() = circleKind == "family"
}
