package com.teanode.app.calendar

import android.content.Context
import android.content.SharedPreferences
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.ui.graphics.Color
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.teanode.app.R
import com.teanode.app.session.ErrorWords
import com.teanode.app.ui.components.SnackbarController
import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.calendar.AgentCalendar
import com.teanode.kit.calendar.CalendarChange
import com.teanode.kit.calendar.CalendarColorComponents
import com.teanode.kit.calendar.CalendarDates
import com.teanode.kit.calendar.CalendarDay
import com.teanode.kit.calendar.CalendarEvent
import com.teanode.kit.calendar.CalendarMonth
import com.teanode.kit.calendar.CalendarPalette
import com.teanode.kit.calendar.CalendarRules
import com.teanode.kit.calendar.calendarEvents
import com.teanode.kit.calendar.calendars
import com.teanode.kit.calendar.cancel
import com.teanode.kit.calendar.eventsByDay
import com.teanode.kit.calendar.perform
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.async
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.serialization.json.JsonPrimitive
import java.time.DayOfWeek
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.temporal.WeekFields
import java.util.Locale

/** The views of the calendar: the month, a week, five days, a day, an agenda, and the reminders. */
enum class CalendarViewMode { Month, Week, Workweek, Day, Agenda, Reminders }

/** Eight colors for calendars, as many as the server spreads them over (the dashboard's calendar-tone-0 to 7). Green is left out: in this app green is what can be pressed. */
internal val calendarPalette: List<Color> = listOf(
    Color(0xFF2F7FE0), Color(0xFFF08A24), Color(0xFF8E5BD6), Color(0xFF1FA39A),
    Color(0xFF4F5BD5), Color(0xFFE0588F), Color(0xFF8C6A4A), Color(0xFF1AA7D9),
)

private val busyColor = Color(0xFF8E8E93)

/**
 * CalendarModel is the calendar as the dashboard's Calendar page shows it (ios CalendarModel.swift): the calendars of
 * events the person reaches (their own, their circles' and the ones shared with them), their events read a month at a
 * time and kept by day, and a change in flight that must be finished or stopped before another is made.
 *
 * One lives with the activity for each account, so every screen of the part reads the same months.
 */
class CalendarModel(
    val connection: Connection,
    internal val context: Context,
    private val preferences: SharedPreferences?,
    private val zone: ZoneId = ZoneId.systemDefault(),
) : ViewModel() {
    /** Where messages to the person go: set by the screens, which share the one the shell holds. */
    var snackbar: SnackbarController? = null

    /** Says why a call failed, in words that tell the person what to do next; the session's. */
    var describeError: (Throwable) -> String = { failure -> failure.message.orEmpty() }

    /** The person's own calendar of events, which a new event goes in unless they choose another. */
    var calendar: AgentCalendar? by mutableStateOf(null)
        private set

    /** Every calendar of events the person reaches, their own first, then their circles', then the ones shared with them. */
    var calendars: List<AgentCalendar> by mutableStateOf(emptyList())
        private set

    private val hiddenKey = "calendar-hidden.${connection.account.username}"
    private val pendingKey = "calendar-pending-change.${connection.account.username}"

    /** The calendars hidden on this phone. Remembered here and nowhere else: whether the family's calendar is in the way is not something to carry to the dashboard. */
    var hiddenIds: Set<String> by mutableStateOf(readPreferenceSet())
        private set

    /** The events of every month read so far, by day. Each month has its own, so a month read from the server redraws that month and no other. */
    val eventsByMonth = mutableStateMapOf<CalendarMonth, Map<CalendarDay, List<CalendarEvent>>>()

    var loadedMonths: Set<CalendarMonth> by mutableStateOf(emptySet())
        private set
    var loadingMonths: Set<CalendarMonth> by mutableStateOf(emptySet())
        private set

    /** The calendars that could not be read, which later months leave out rather than ask again on every scroll. A refresh asks again. */
    private var unreadableCalendarIds: Set<String> = emptySet()

    val isLoading: Boolean get() = loadingMonths.isNotEmpty()

    var viewMode: CalendarViewMode by mutableStateOf(CalendarViewMode.Month)
        private set

    /** The day the view is on. */
    var focus: LocalDate by mutableStateOf(LocalDate.now(zone))
        private set

    /** The months the month view has on screen, read again on a refresh. */
    var visibleMonths: Set<CalendarMonth> = emptySet()

    /** A change sent whose answer did not come back, kept on the phone until it is finished or stopped. */
    var pending: CalendarChange? by mutableStateOf(readPending())
        private set

    private val calendarsLock = Mutex()

    /** The reminders, which the calendar's Reminders view and its editor share. */
    val reminders: RemindersModel = RemindersModel(connection, context)

    /** The person's calendar, in the week they start on. */
    val rules: CalendarRules
        get() = CalendarRules(
            zone,
            if (calendar?.weekStart == "monday") DayOfWeek.MONDAY else DayOfWeek.SUNDAY,
            WeekFields.of(Locale.getDefault()).minimalDaysInFirstWeek,
        )

    // MARK: Words

    /** Hands the snackbar to the reminders too. */
    fun attach(snackbarController: SnackbarController, describe: (Throwable) -> String) {
        snackbar = snackbarController
        describeError = describe
        reminders.snackbar = snackbarController
    }

    private fun say(message: String) {
        snackbar?.show(message)
    }

    private fun complain(error: Throwable) {
        if (ErrorWords.isCancellation(error)) return
        snackbar?.showError(error)
    }

    private fun complain(message: String) {
        snackbar?.showProblem(message)
    }

    // MARK: Reading

    fun showViewMode(mode: CalendarViewMode) {
        viewMode = mode
        viewModelScope.launch { load() }
    }

    fun moveFocus(day: LocalDate) {
        focus = day
        viewModelScope.launch { load() }
    }

    /** Reads the months the view needs that are not read yet. */
    suspend fun load() {
        if (viewMode == CalendarViewMode.Reminders) return
        loadMonths(neededMonths(), isForced = false)
    }

    /** Reads the months on screen again, and forgets the others, so they are read again when they come back into view. */
    suspend fun reload() {
        if (viewMode == CalendarViewMode.Reminders) return
        val months = neededMonths()
        loadedMonths = loadedMonths.intersect(months)
        unreadableCalendarIds = emptySet()
        loadMonths(months, isForced = true)
    }

    /** Reads a month the month view is about to show, unless it is read. */
    fun ensureMonth(month: CalendarMonth) {
        if (month in loadedMonths || month in loadingMonths) return
        viewModelScope.launch { loadMonths(setOf(month), isForced = false) }
    }

    /** The months the current view draws. */
    private fun neededMonths(): Set<CalendarMonth> {
        if (viewMode == CalendarViewMode.Month) {
            return visibleMonths.ifEmpty { setOf(CalendarMonth(focus.year, focus.monthValue)) }
        }
        val days = days()
        val first = CalendarDay.of(days.first()).calendarMonth
        val last = CalendarDay.of(days.last()).calendarMonth
        return (0..maxOf(0, last.id - first.id)).map { offset -> first.adding(offset) }.toSet()
    }

    private suspend fun loadMonths(months: Set<CalendarMonth>, isForced: Boolean) {
        val wanted = months.filter { month -> month !in loadingMonths && (isForced || month !in loadedMonths) }.toSet()
        if (wanted.isEmpty()) return
        loadingMonths = loadingMonths + wanted
        try {
            if (eventsCalendar() == null) return
            val readRules = rules
            val read = shownCalendars
            for (month in wanted.sorted()) {
                val (from, until) = month.window(readRules.zone)
                // Every calendar shown, one after another. One that cannot be read (it was shared, and has just been
                // taken back) does not take the rest of the month with it: it is remembered, and the person is told
                // once, by its name.
                val events = ArrayList<CalendarEvent>()
                val failures = ArrayList<Pair<AgentCalendar, Throwable>>()
                val attempted = read.filter { each -> each.id !in unreadableCalendarIds }
                for (each in attempted) {
                    try {
                        events += connection.calendarEvents(each.id, from, until)
                    } catch (failure: kotlinx.coroutines.CancellationException) {
                        throw failure
                    } catch (failure: Throwable) {
                        failures.add(each to failure)
                    }
                }
                // Every one failing is the connection and not a calendar: nothing is remembered, and the month is read
                // again when it is next asked for.
                if (attempted.isNotEmpty() && failures.size == attempted.size) {
                    complain(failures.first().second)
                    continue
                }
                for ((failed, failure) in failures) {
                    unreadableCalendarIds = unreadableCalendarIds + failed.id
                    complain(context.getString(R.string.calendar_could_not_read_calendar, failed.name, describe(failure)))
                }
                eventsByMonth[month] = CalendarDates.eventsByDay(events, month, readRules.zone)
                loadedMonths = loadedMonths + month
            }
        } finally {
            loadingMonths = loadingMonths - wanted
        }
    }

    private fun describe(failure: Throwable): String = describeError(failure)

    private var calendarsRead: Deferred<List<AgentCalendar>?>? = null

    /** The calendars, read once however many months ask for them at once. Answers the person's own calendar of events. */
    private suspend fun eventsCalendar(): AgentCalendar? {
        calendar?.let { known -> return known }
        val found = calendarsLock.withLock {
            val reading = calendarsRead ?: viewModelScope.async {
                try {
                    connection.calendars()
                } catch (failure: kotlinx.coroutines.CancellationException) {
                    throw failure
                } catch (failure: Throwable) {
                    complain(failure)
                    null
                }
            }.also { started -> calendarsRead = started }
            reading.await()
        }
        if (found == null) {
            // Asked again next time, rather than remembered as having none.
            calendarsRead = null
            return null
        }
        val events = found.filter { each -> each.calendarKind != "reminders" }
        calendars = events
        calendar = events.firstOrNull { each -> each.isOwn } ?: events.firstOrNull() ?: found.firstOrNull()
        return calendar
    }

    /** The calendars whose events are on the screen. */
    val shownCalendars: List<AgentCalendar> get() = calendars.filter { each -> each.id !in hiddenIds }

    /** The calendars an event can be put in: the person's own, a circle's, and any shared with them at edit. */
    val writableCalendars: List<AgentCalendar> get() = calendars.filter { each -> each.canWrite }

    /** The calendar with this identifier, if the person reaches it. */
    fun calendar(calendarId: String): AgentCalendar? = calendars.firstOrNull { each -> each.id == calendarId } ?: calendar?.takeIf { own -> own.id == calendarId }

    /** Shows a calendar, or hides it, and reads the months again without it. */
    suspend fun setShown(calendarId: String, isShown: Boolean) {
        hiddenIds = if (isShown) hiddenIds - calendarId else hiddenIds + calendarId
        preferences?.edit()?.putStringSet(hiddenKey, hiddenIds)?.apply()
        loadedMonths = emptySet()
        reload()
    }

    /** Reads the calendars again: after the person shares theirs or a circle's is made, or when a refresh is asked for. */
    suspend fun reloadCalendars() {
        calendar = null
        calendarsRead = null
        eventsCalendar()
        loadedMonths = emptySet()
        reload()
    }

    // MARK: The days a view covers

    /** The days a view covers: the month, a week, five days from Monday, a day, or a month of agenda. */
    fun days(): List<LocalDate> {
        val readRules = rules
        return when (viewMode) {
            CalendarViewMode.Month -> {
                val first = focus.withDayOfMonth(1)
                (0 until first.lengthOfMonth()).map { offset -> first.plusDays(offset.toLong()) }
            }
            CalendarViewMode.Week -> weekOf(focus, readRules.firstDayOfWeek).let { start -> (0 until 7).map { offset -> start.plusDays(offset.toLong()) } }
            CalendarViewMode.Workweek -> weekOf(focus, DayOfWeek.MONDAY).let { start -> (0 until 5).map { offset -> start.plusDays(offset.toLong()) } }
            CalendarViewMode.Day -> listOf(focus)
            CalendarViewMode.Agenda, CalendarViewMode.Reminders -> (0 until 31).map { offset -> focus.plusDays(offset.toLong()) }
        }
    }

    /** Back or forward a step: a day, a week, a month, or a month of agenda. */
    fun step(direction: Int) {
        val moved = when (viewMode) {
            CalendarViewMode.Month -> focus.withDayOfMonth(1).plusMonths(direction.toLong())
            CalendarViewMode.Week, CalendarViewMode.Workweek -> focus.plusDays(7L * direction)
            CalendarViewMode.Day -> focus.plusDays(direction.toLong())
            CalendarViewMode.Agenda, CalendarViewMode.Reminders -> focus.plusDays(31L * direction)
        }
        moveFocus(moved)
    }

    /** The events on a day, every one it is part of: all-day ones first, then by when they start. */
    fun events(day: LocalDate): List<CalendarEvent> {
        val calendarDay = CalendarDay.of(day)
        return eventsByMonth[calendarDay.calendarMonth]?.get(calendarDay).orEmpty()
    }

    // MARK: Changes

    /** Saves or deletes an event, keeping the change on the phone until the server answers, so one whose answer is lost is never made twice. */
    suspend fun make(change: CalendarChange): Boolean {
        if (pending != null) {
            complain(context.getString(R.string.calendar_resolve_pending_first))
            return false
        }
        keep(change)
        return send(change, isRetry = false)
    }

    suspend fun retryPending() {
        val waiting = pending ?: return
        send(waiting, isRetry = true)
    }

    suspend fun stopPending() {
        val waiting = pending ?: return
        try {
            val hadHappened = connection.cancel(waiting)
            keep(null)
            say(context.getString(if (hadHappened) R.string.calendar_change_completed else R.string.calendar_change_stopped))
            reload()
        } catch (failure: kotlinx.coroutines.CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            complain(failure)
        }
    }

    private suspend fun send(change: CalendarChange, isRetry: Boolean): Boolean = try {
        connection.perform(change, isRetry)
        keep(null)
        say(savedWords(change))
        reload()
        true
    } catch (failure: kotlinx.coroutines.CancellationException) {
        throw failure
    } catch (failure: Throwable) {
        complain(failure)
        false
    }

    /**
     * What the message says after a change went through: that it was removed or saved, and, for an event put in a
     * circle's calendar or one shared with the person, which calendar it is in, as the dashboard says it.
     */
    private fun savedWords(change: CalendarChange): String {
        if (change.operation == "delete") return context.getString(R.string.calendar_event_removed, change.summary)
        val calendarId = (change.variables["calendarId"] as? JsonPrimitive)?.content
        val destination = calendarId?.let { id -> calendar(id) }
        if (destination == null || destination.isOwn) return context.getString(R.string.calendar_event_saved, change.summary)
        val calendarName = displayName(destination)
        val eventId = (change.variables["id"] as? JsonPrimitive)?.takeIf { value -> value.isString }?.content
        return if (!eventId.isNullOrEmpty()) context.getString(R.string.calendar_event_saved_in, change.summary, calendarName)
        else context.getString(R.string.calendar_event_is_in, change.summary, calendarName)
    }

    private fun keep(change: CalendarChange?) {
        pending = change
        val editor = preferences?.edit() ?: return
        if (change != null) editor.putString(pendingKey, TeaNodeJson.encodeToString(CalendarChange.serializer(), change)) else editor.remove(pendingKey)
        editor.apply()
    }

    private fun readPending(): CalendarChange? {
        val text = preferences?.getString(pendingKey, null) ?: return null
        return try {
            TeaNodeJson.decodeFromString(CalendarChange.serializer(), text)
        } catch (_: Exception) {
            null
        }
    }

    private fun readPreferenceSet(): Set<String> = try {
        preferences?.getStringSet("calendar-hidden.${connection.account.username}", emptySet())?.toSet().orEmpty()
    } catch (_: Exception) {
        emptySet()
    }

    // MARK: Names and colors

    /** What a list calls a calendar: its own name for the person's and a circle's, and whose it is for another person's, since everybody's is called "Calendar". */
    fun displayName(calendar: AgentCalendar): String {
        val owner = calendar.ownerName
        if (calendar.isNamedForOwner && owner != null) return context.getString(R.string.calendar_owners_calendar, owner)
        return calendar.name
    }

    /** Whose the calendar an event is in is, to be said beside it: the owner's name for a calendar shared with the person, the circle's for a circle's, and nothing for their own. */
    fun ownerName(event: CalendarEvent): String? = calendar(event.calendarId)?.whoseName

    /** An event's title as a list shows it. Time somebody else is busy says whose it is, so that several people's gray blocks can be told apart. */
    fun shownTitle(event: CalendarEvent): String {
        val owner = ownerName(event)
        if (event.isBusy && owner != null) return context.getString(R.string.calendar_busy_owner, owner)
        return eventTitle(context, event)
    }

    private fun spreadColor(calendar: AgentCalendar?, calendarId: String): Color {
        val colorIndex = calendar?.colorIndex
        if (colorIndex != null) return calendarPalette[CalendarPalette.spreadIndex(colorIndex)]
        return calendarPalette[CalendarPalette.index(calendarId, calendarPalette.size)]
    }

    /** The color an event is painted: its calendar's own when the server keeps one, or else the calendar's color in the spread, always the same one for the same calendar. */
    fun color(event: CalendarEvent): Color {
        // The times somebody is busy are gray, whose calendar they are in.
        if (event.isBusy) return busyColor
        val eventCalendar = calendar(event.calendarId)
        CalendarColorComponents.fromHexText(eventCalendar?.colour)?.let { parts -> return Color(parts.red.toFloat(), parts.green.toFloat(), parts.blue.toFloat()) }
        return spreadColor(eventCalendar, event.calendarId)
    }

    /** The color a calendar is painted in the list of calendars. */
    fun color(calendar: AgentCalendar): Color {
        CalendarColorComponents.fromHexText(calendar.colour)?.let { parts -> return Color(parts.red.toFloat(), parts.green.toFloat(), parts.blue.toFloat()) }
        return spreadColor(calendar, calendar.id)
    }

    companion object {
        /** The first day of the week a day is in, for the week starting on [firstDay]. */
        fun weekOf(day: LocalDate, firstDay: DayOfWeek): LocalDate =
            day.minusDays(Math.floorMod(day.dayOfWeek.value - firstDay.value, 7).toLong())

        /** A new event starts at the next hour on the day being looked at. */
        fun nextHour(day: LocalDate, now: java.time.ZonedDateTime = java.time.ZonedDateTime.now()): Instant {
            val hour = minOf(now.hour + 1, 23)
            return day.atStartOfDay(now.zone).plusHours(hour.toLong()).toInstant()
        }
    }
}

/** What to call an event on screen: its title, or "Busy" for time its owner is busy and nothing else was shared. */
fun eventTitle(context: Context, event: CalendarEvent): String {
    if (event.isBusy) return context.getString(R.string.calendar_busy)
    return event.summary?.takeIf { text -> text.isNotEmpty() } ?: context.getString(R.string.calendar_no_title)
}

