package com.teanode.kit.notifications

import com.teanode.kit.Connection
import com.teanode.kit.ServiceCapabilities
import com.teanode.kit.serviceCapabilities
import kotlinx.coroutines.CancellationException
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonPrimitive
import java.time.Instant
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.ZoneOffset

/**
 * Glance is what a widget shows at a glance: the next events, the unread mail, the agent's name, and what waits for the
 * person. It is the Android side of the iPhone app's `Glance` (the widgets' reading).
 */
data class Glance(
    val events: List<GlanceEvent>,
    val unreadCount: Int,
    val agentName: String,
    /** How many questions and approvals wait for the person, and the newest of them. */
    val waitingCount: Int = 0,
    val newestWaiting: WaitingInteraction? = null,
)

/** GlanceEvent is one coming event as a widget lists it. */
data class GlanceEvent(
    val id: String,
    val title: String,
    val location: String,
    val startsAtEpochMS: Long,
    val endsAtEpochMS: Long,
    val isAllDay: Boolean,
)

/** WaitingInteraction is a question or an approval the agent waits on. */
@Serializable
data class WaitingInteraction(
    val id: String,
    val conversationId: String = "",
    /** "question" or "approval". */
    val interactionKind: String = "",
    val interactionText: String = "",
    val createdAt: String = "",
)

/** One event of a calendar, as the widget's reading asks for it. */
@Serializable
data class GlanceCalendarEvent(
    val id: String,
    val calendarId: String = "",
    val summary: String? = null,
    val location: String? = null,
    val startsAt: String,
    val endsAt: String,
    val allDay: Boolean = false,
    val status: String? = null,
    val isBusyOnly: Boolean? = null,
) {
    /** A stable identity for one occurrence of a repeating event. */
    val occurrenceId: String get() = "$calendarId-$id-$startsAt"
}

/** One calendar, as much of it as the widget's reading needs. */
@Serializable
data class GlanceCalendar(
    val id: String,
    val calendarKind: String? = null,
    val shareLevel: String? = null,
    val isCircleCalendar: Boolean? = null,
) {
    /** Whether it is the person's own calendar. */
    val isOwn: Boolean get() = shareLevel == null || shareLevel == "owner"
}

object GlanceDocuments {
    const val WAITING = """
        query (${'$'}first: Int) {
          ReadAgentWaiting(first: ${'$'}first) { waitingCount interactions { id conversationId interactionKind interactionText createdAt } }
        }
        """
    const val AGENT = """
        query { ReadAgent { agent { id enabled name } } }
        """

    /** The calendars, with the fields the widget reads (the rest of the iPhone app's document is the Calendar part's). */
    const val CALENDARS = """
        query { ListCalendars { id calendarKind shareLevel isCircleCalendar } }
        """
    const val CALENDAR_EVENTS = """
        query (${'$'}calendarId: String!, ${'$'}from: String!, ${'$'}until: String!) {
          ListCalendarEvents(calendarId: ${'$'}calendarId, from: ${'$'}from, until: ${'$'}until) {
            id calendarId summary location startsAt endsAt allDay status isBusyOnly
          }
        }
        """
    const val UNREAD = """
        query { ListMailboxes { unread } }
        """
}

@Serializable
private class WaitingAnswer(@SerialName("ReadAgentWaiting") val waiting: Waiting) {
    @Serializable
    class Waiting(val waitingCount: Int = 0, val interactions: List<WaitingInteraction> = emptyList())
}

@Serializable
private class AgentAnswer(@SerialName("ReadAgent") val read: Read) {
    @Serializable
    class Read(val agent: Agent? = null)

    @Serializable
    class Agent(val name: String? = null)
}

@Serializable
private class CalendarsAnswer(@SerialName("ListCalendars") val calendars: List<GlanceCalendar>)

@Serializable
private class EventsAnswer(@SerialName("ListCalendarEvents") val events: List<GlanceCalendarEvent>)

@Serializable
private class UnreadAnswer(@SerialName("ListMailboxes") val mailboxes: List<Unread>) {
    @Serializable
    class Unread(val unread: Int = 0)
}

object GlanceReading {
    /**
     * The events not over yet, soonest first, at most [limit]. An all-day event is on its day wherever the phone is: its
     * stored midnight UTC is read as the day, and placed at local midnight.
     */
    fun upcoming(events: List<GlanceCalendarEvent>, nowEpochMS: Long, limit: Int, zone: ZoneId = ZoneId.systemDefault()): List<GlanceEvent> {
        fun localDay(text: String): Long? = epochMS(text)?.let { moment ->
            Instant.ofEpochMilli(moment).atZone(ZoneOffset.UTC).toLocalDate().atStartOfDay(zone).toInstant().toEpochMilli()
        }
        val read = events.mapNotNull { event ->
            if (event.status?.uppercase() == "CANCELLED") return@mapNotNull null
            val startsAt = if (event.allDay) localDay(event.startsAt) else epochMS(event.startsAt)
            val endsAt = if (event.allDay) localDay(event.endsAt) else epochMS(event.endsAt)
            if (startsAt == null || endsAt == null || endsAt <= nowEpochMS) return@mapNotNull null
            GlanceEvent(event.occurrenceId, (event.summary ?: "").trim(), event.location ?: "", startsAt, endsAt, event.allDay)
        }
        return read.sortedWith(compareBy<GlanceEvent> { event -> event.startsAtEpochMS }.thenBy { event -> event.title }).take(limit)
    }

    private fun epochMS(text: String): Long? = try {
        OffsetDateTime.parse(text.replace(Regex("""\.\d+"""), "")).toInstant().toEpochMilli()
    } catch (_: Exception) {
        null
    }
}

/**
 * Reads what a widget shows: the events of the next [hours] from the person's own calendar and their circles', and the
 * unread mail in every mailbox. What other people shared with them stays in the Calendar: a widget on a lock screen
 * shows what the person is doing, not what somebody else shared with them. A part the person cannot read is left empty
 * rather than failing the rest.
 */
suspend fun Connection.glance(nowEpochMS: Long = System.currentTimeMillis(), hours: Int = 48, limit: Int = 6, zone: ZoneId = ZoneId.systemDefault()): Glance {
    suspend fun <Value> orNull(read: suspend () -> Value): Value? = try {
        read()
    } catch (cancellation: CancellationException) {
        throw cancellation
    } catch (_: Exception) {
        null
    }
    val services = orNull { serviceCapabilities() } ?: ServiceCapabilities.unavailable
    val agentName = if (services.isAssistantEnabled) orNull {
        execute(GlanceDocuments.AGENT, AgentAnswer.serializer()).read.agent?.name
    } ?: "" else ""
    val events = mutableListOf<GlanceCalendarEvent>()
    if (services.isCalendarEnabled) {
        val calendars = orNull { execute(GlanceDocuments.CALENDARS, CalendarsAnswer.serializer()).calendars }
        for (calendar in calendars.orEmpty()) {
            if (calendar.calendarKind == "reminders" || !(calendar.isOwn || calendar.isCircleCalendar == true)) continue
            val read = orNull {
                execute(
                    GlanceDocuments.CALENDAR_EVENTS, EventsAnswer.serializer(),
                    mapOf(
                        "calendarId" to JsonPrimitive(calendar.id),
                        "from" to JsonPrimitive(Instant.ofEpochMilli(nowEpochMS - nowEpochMS % 1000).toString()),
                        "until" to JsonPrimitive(Instant.ofEpochMilli(nowEpochMS - nowEpochMS % 1000 + hours * 3_600_000L).toString()),
                    ),
                ).events
            }
            events += read.orEmpty().filter { event -> event.isBusyOnly != true }
        }
    }
    val unreadCount = if (services.isMailboxEnabled) orNull { execute(GlanceDocuments.UNREAD, UnreadAnswer.serializer()).mailboxes.sumOf { mailbox -> mailbox.unread } } ?: 0 else 0
    val waiting = if (services.isAssistantEnabled) orNull {
        execute(GlanceDocuments.WAITING, WaitingAnswer.serializer(), mapOf("first" to JsonPrimitive(1))).waiting
    } else null
    return Glance(
        events = GlanceReading.upcoming(events, nowEpochMS, limit, zone), unreadCount = unreadCount, agentName = agentName,
        waitingCount = waiting?.waitingCount ?: 0, newestWaiting = waiting?.interactions?.firstOrNull(),
    )
}
