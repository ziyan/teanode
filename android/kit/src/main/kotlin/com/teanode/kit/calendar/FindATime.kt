package com.teanode.kit.calendar

import com.teanode.kit.Connection
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import java.time.Instant
import java.time.ZoneId

// Finding a time when several people are all free: the people who shared their calendar with the person, a run of days
// and the hours of the day to offer, and the stretches nobody has anything booked in. The server says when each is busy
// (ListCalendarFreeBusy, which the dashboard, the command line's `calendar free --with` and the agent's free_together
// ask too) and FreeTime does the sum they all do, so that the time one offers is the time the others offer.

/**
 * CalendarFreeBusy is when one person is busy, for a person they share their calendar with at busy or more. A person
 * who shared nothing has no periods and is not shared: nothing can be said of when they are free.
 */
@Serializable
data class CalendarFreeBusy(
    val userId: String,
    val username: String? = null,
    val name: String? = null,
    val isShared: Boolean,
    val periods: List<Period> = emptyList(),
) {
    @Serializable
    data class Period(val startsAt: String, val endsAt: String)
}

/** FreeStretch is a stretch of one day when nobody asked about is busy. */
data class FreeStretch(
    /** The day, at its start. */
    val day: Instant,
    val startsAt: Instant,
    val endsAt: Instant,
)

/** FreeTimeQuestion is what the Find a time screen asks, as the person typed it: the days, as the first and the last, and the hours of the day. */
data class FreeTimeQuestion(
    val firstDay: Instant,
    val lastDay: Instant,
    /** Minutes into the day, 540 for 09:00. */
    val earliestMinutes: Int = 9 * 60,
    val latestMinutes: Int = 17 * 60,
) {
    /** What is wrong with the question, for the screen to say in its own words. */
    enum class Problem { LastDayBeforeFirst, DayEndsBeforeItBegins }

    val problem: Problem?
        get() = when {
            lastDay.isBefore(firstDay) -> Problem.LastDayBeforeFirst
            latestMinutes <= earliestMinutes -> Problem.DayEndsBeforeItBegins
            else -> null
        }
}

object FreeTime {
    /** The most people one question may hold: the server takes 20 and the person is one of them. */
    const val MOST_PEOPLE_PER_QUESTION = 19

    /**
     * The stretches nobody in `busy` is busy, for each day from `first` up to but not including `dayAfterLast`,
     * between the earliest and the latest minute of the day. Overlapping periods are one, so what is left is when
     * everybody could meet.
     */
    fun freeByDay(
        busy: List<CalendarFreeBusy.Period>,
        first: Instant,
        dayAfterLast: Instant,
        earliestMinutes: Int,
        latestMinutes: Int,
        zone: ZoneId,
    ): List<FreeStretch> {
        val periods = busy.mapNotNull { period ->
            val starts = ServerTime.instant(period.startsAt)
            val ends = ServerTime.instant(period.endsAt)
            if (starts != null && ends != null) starts to ends else null
        }
        val free = ArrayList<FreeStretch>()
        var day = first.atZone(zone).toLocalDate().atStartOfDay(zone)
        while (day.toInstant().isBefore(dayAfterLast)) {
            val opens = day.plusMinutes(earliestMinutes.toLong()).toInstant()
            val closes = day.plusMinutes(latestMinutes.toLong()).toInstant()
            var cursor = opens
            val clipped = periods
                .map { period -> maxOf(period.first, opens) to minOf(period.second, closes) }
                .filter { period -> period.first.isBefore(period.second) }
                .sortedBy { period -> period.first }
            for ((starts, ends) in clipped) {
                if (starts.isAfter(cursor)) free.add(FreeStretch(day.toInstant(), cursor, starts))
                cursor = maxOf(cursor, ends)
            }
            if (cursor.isBefore(closes)) free.add(FreeStretch(day.toInstant(), cursor, closes))
            day = day.toLocalDate().plusDays(1).atStartOfDay(zone)
        }
        return free
    }

    /** The day after the last the person asked about, which is where the question ends. */
    fun dayAfter(lastDay: Instant, zone: ZoneId): Instant =
        lastDay.atZone(zone).toLocalDate().plusDays(1).atStartOfDay(zone).toInstant()
}

/**
 * FreeTimeAnswer is what asking for a time came to: the stretches, or the people who stopped sharing since the list was
 * made, since a time that leaves somebody out does not suit them and none is offered.
 */
sealed class FreeTimeAnswer {
    data class Free(val stretches: List<FreeStretch>) : FreeTimeAnswer()

    data class NotShared(val usernames: List<String>) : FreeTimeAnswer()
}

/** The people who shared their calendar with the person, and whether somebody they share a circle with was left out. */
data class PeopleSharingTheirCalendar(val people: List<CalendarSharePerson>, val isAnyoneLeftOut: Boolean)

@Serializable
private class FreeBusyAnswer(val ListCalendarFreeBusy: List<CalendarFreeBusy>)

/** When the people are busy between two moments, a question at a time when there are more of them than one question holds. */
suspend fun Connection.calendarFreeBusy(userIds: List<String>, from: Instant, until: Instant): List<CalendarFreeBusy> {
    val answers = ArrayList<CalendarFreeBusy>()
    for (chunk in userIds.chunked(FreeTime.MOST_PEOPLE_PER_QUESTION)) {
        val variables: Map<String, JsonElement> = mapOf(
            "userIds" to JsonArray(chunk.map { userId -> JsonPrimitive(userId) }),
            "from" to JsonPrimitive(CalendarDates.text(from)),
            "until" to JsonPrimitive(CalendarDates.text(until)),
        )
        answers += execute<FreeBusyAnswer>(CalendarDocuments.CALENDAR_FREE_BUSY, variables).ListCalendarFreeBusy
    }
    return answers
}

/**
 * The people who shared their calendar with the person, learned by asking when everybody is busy tomorrow, which says
 * isShared for each; and whether somebody they share a circle with was left out.
 */
suspend fun Connection.peopleSharingTheirCalendar(now: Instant = Instant.now(), zone: ZoneId = ZoneId.systemDefault()): PeopleSharingTheirCalendar {
    val everybody = calendarSharePeople()
    if (everybody.isEmpty()) return PeopleSharingTheirCalendar(emptyList(), false)
    val tomorrow = FreeTime.dayAfter(now, zone)
    val answers = calendarFreeBusy(everybody.map { person -> person.userId }, now, tomorrow)
    val sharing = answers.filter { answer -> answer.isShared }.map { answer -> answer.userId }.toSet()
    val people = everybody.filter { person -> person.userId in sharing }
    return PeopleSharingTheirCalendar(people, people.size < everybody.size)
}

/** When the person and the people chosen are all free. */
suspend fun Connection.freeTime(
    ownUserId: String,
    chosen: List<CalendarSharePerson>,
    question: FreeTimeQuestion,
    zone: ZoneId = ZoneId.systemDefault(),
): FreeTimeAnswer {
    val first = question.firstDay.atZone(zone).toLocalDate().atStartOfDay(zone).toInstant()
    val dayAfterLast = FreeTime.dayAfter(question.lastDay, zone)
    val answers = calendarFreeBusy(listOf(ownUserId) + chosen.map { person -> person.userId }, first, dayAfterLast)
    val missing = chosen.filter { person -> answers.none { answer -> answer.userId == person.userId && answer.isShared } }
    if (missing.isNotEmpty()) return FreeTimeAnswer.NotShared(missing.map { person -> person.username })
    return FreeTimeAnswer.Free(
        FreeTime.freeByDay(answers.flatMap { answer -> answer.periods }, first, dayAfterLast, question.earliestMinutes, question.latestMinutes, zone),
    )
}
