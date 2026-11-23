package com.teanode.app.journal

import android.content.Context
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.teanode.app.R
import com.teanode.app.session.ErrorWords
import com.teanode.app.ui.components.SnackbarController
import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeError
import com.teanode.kit.journal.JournalDay
import com.teanode.kit.journal.JournalDaySummary
import com.teanode.kit.journal.JournalDays
import com.teanode.kit.journal.JournalLink
import com.teanode.kit.journal.JournalMoment
import com.teanode.kit.journal.JournalMoodLevel
import com.teanode.kit.journal.JournalPeriod
import com.teanode.kit.journal.JournalPeriodKind
import com.teanode.kit.journal.JournalPolling
import com.teanode.kit.journal.JournalSearch
import com.teanode.kit.journal.JournalSearchHit
import com.teanode.kit.journal.JournalSettings
import com.teanode.kit.journal.JournalTarget
import com.teanode.kit.journal.JournalWeekGroup
import com.teanode.kit.journal.deleteJournalNarrative
import com.teanode.kit.journal.eraseJournal
import com.teanode.kit.journal.journalDay
import com.teanode.kit.journal.journalDays
import com.teanode.kit.journal.journalPeriod
import com.teanode.kit.journal.journalSettings
import com.teanode.kit.journal.pinJournalRecord
import com.teanode.kit.journal.removeJournalCorrection
import com.teanode.kit.journal.removeJournalLink
import com.teanode.kit.journal.searchJournal
import com.teanode.kit.journal.setJournalMood
import com.teanode.kit.journal.updateJournalSettings
import com.teanode.kit.journal.writeJournalNarrative
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import java.time.LocalDate
import java.time.ZoneId

/** The words a failed change says when the server gives none of its own: the server's reason when it refused, else the sentence for the call. */
internal fun reasonOrSay(failure: Throwable, fallback: String): String = ((failure as? TeaNodeError.Refused)?.reason?.takeIf { reason -> reason.isNotEmpty() } ?: fallback)

/**
 * JournalModel is the Journal's timeline: the person's days, newest first, with each week's headline, and today (ios
 * JournalModel.swift). One lives with the activity for each account, so the day and period screens read the same today
 * and the same week start.
 */
class JournalModel(val connection: Connection, private val context: Context, private val zone: ZoneId = ZoneId.systemDefault()) : ViewModel() {
    var snackbar: SnackbarController? = null
    var describeError: (Throwable) -> String = { failure -> failure.message.orEmpty() }

    var days: List<JournalDaySummary> by mutableStateOf(emptyList())
        private set
    var totalCount: Int by mutableStateOf(0)
        private set

    /** The person's today, as the server says it, in their zone. */
    var today: String by mutableStateOf("")
        private set

    /** The earliest day with anything in it, empty for none. */
    var firstDay: String by mutableStateOf("")
        private set
    var settings: JournalSettings? by mutableStateOf(null)
        private set

    /** Today whole, for the card at the top: its mood, weather and numbers. */
    var todayDay: JournalDay? by mutableStateOf(null)
        private set

    /** The headline of each week that has one, by the week's first day. */
    var weekHeadlines: Map<String, String> by mutableStateOf(emptyMap())
        private set
    var hasLoaded: Boolean by mutableStateOf(false)
        private set
    var loadFailure: Throwable? by mutableStateOf(null)
        private set
    var isLoadingMore: Boolean by mutableStateOf(false)
        private set
    private var requestedWeekStarts: Set<String> = emptySet()

    /** The day weeks start on: the server's word for the person's locale, the phone's own until it has been read. */
    val weekStartsOn: String
        get() = settings?.weekStartsOn ?: if (java.time.temporal.WeekFields.of(java.util.Locale.getDefault()).firstDayOfWeek == java.time.DayOfWeek.SUNDAY) "sunday" else "monday"

    /** Today, from the phone until the server has said it. */
    val shownToday: String get() = today.ifEmpty { JournalDays.text(LocalDate.now(zone)) }

    val weekGroups: List<JournalWeekGroup> get() = JournalDays.weekGroups(days, weekStartsOn)

    val canLoadMore: Boolean get() = days.size < totalCount

    fun say(message: String) {
        snackbar?.show(message)
    }

    fun complain(message: String) {
        snackbar?.showProblem(message)
    }

    fun complain(failure: Throwable) {
        if (!ErrorWords.isCancellation(failure)) snackbar?.showError(failure)
    }

    /** Reads the first page of days, today and the settings. Pulling the screen down asks for the weeks' headlines again too. */
    suspend fun load(isRefreshing: Boolean = false) {
        try {
            try {
                val page = connection.journalDays(first = PAGE_SIZE)
                days = page.days
                totalCount = page.totalCount
                today = page.today
                firstDay = page.firstDay
                loadFailure = null
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                if (days.isEmpty()) loadFailure = failure else complain(failure)
                return
            }
            runCatching { connection.journalSettings() }.getOrNull()?.let { read -> settings = read }
            loadToday()
            if (isRefreshing) requestedWeekStarts = emptySet()
            loadWeekHeadlines()
        } finally {
            hasLoaded = true
        }
    }

    /** Reads what the day and period screens need of the timeline (today and the week start) when the screen was opened straight from a link. */
    suspend fun ensureContext() {
        if (today.isNotEmpty() && settings != null) return
        runCatching { connection.journalDays(first = 1) }.getOrNull()?.let { page ->
            today = page.today
            firstDay = page.firstDay
        }
        runCatching { connection.journalSettings() }.getOrNull()?.let { read -> settings = read }
    }

    private suspend fun loadToday() {
        runCatching { connection.journalDay(shownToday) }.getOrNull()?.let { read -> todayDay = read }
    }

    /** Reads the headline of each week on the screen, one after another: the list does not wait for them. */
    suspend fun loadWeekHeadlines() {
        for (group in weekGroups) {
            if (group.weekStart in requestedWeekStarts) continue
            requestedWeekStarts = requestedWeekStarts + group.weekStart
            val period = try {
                connection.journalPeriod(JournalPeriodKind.Week, group.weekStart)
            } catch (failure: CancellationException) {
                throw failure
            } catch (_: Throwable) {
                requestedWeekStarts = requestedWeekStarts - group.weekStart
                null
            }
            if (period != null) weekHeadlines = weekHeadlines + (group.weekStart to (period.narrative?.headline ?: ""))
        }
    }

    /** Reads the next page when the last day on the screen comes into view. */
    suspend fun loadMore(after: JournalDaySummary) {
        if (after.id != days.lastOrNull()?.id || !canLoadMore || isLoadingMore) return
        isLoadingMore = true
        try {
            val page = connection.journalDays(first = PAGE_SIZE, offset = days.size)
            val known = days.map { day -> day.id }.toSet()
            days = days + page.days.filter { day -> day.id !in known }
            totalCount = page.totalCount
        } catch (failure: CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            complain(failure)
            return
        } finally {
            isLoadingMore = false
        }
        loadWeekHeadlines()
    }

    /** Reads today again, and its row of the list, after the person changed something about it. */
    suspend fun refreshToday() {
        loadToday()
        val page = runCatching { connection.journalDays(fromDay = shownToday, untilDay = shownToday, first = 1) }.getOrNull() ?: return
        days = days.filter { day -> day.day != shownToday }.let { rest -> listOfNotNull(page.days.firstOrNull()) + rest }
    }

    /** Sets today's mood from the card. */
    suspend fun setTodayMood(level: JournalMoodLevel) {
        try {
            connection.setJournalMood(shownToday, level.moodLevel, todayDay?.mood?.moodWords)
        } catch (failure: CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            complain(reasonOrSay(failure, context.getString(R.string.journal_could_not_save_today)))
            return
        }
        refreshToday()
    }

    companion object {
        /** How many days one read brings. */
        const val PAGE_SIZE = 31
    }
}

/** JournalDayModel is one day of the journal (ios JournalModel.swift). */
class JournalDayModel(val connection: Connection, day: String, private val host: JournalModel, private val context: Context) {
    var day: String by mutableStateOf(day)
        private set
    var journalDay: JournalDay? by mutableStateOf(null)
        private set
    var hasLoaded: Boolean by mutableStateOf(false)
        private set
    var loadFailure: Throwable? by mutableStateOf(null)
        private set
    var isBusy: Boolean by mutableStateOf(false)
        private set

    /** Whether the touched-up words or the narrative are being written. */
    val isPending: Boolean get() = journalDay?.isPending ?: false

    /** Moves to another day, which is read at once. */
    suspend fun show(newDay: String) {
        if (newDay == day) return
        day = newDay
        journalDay = null
        hasLoaded = false
        loadFailure = null
        load()
    }

    suspend fun load(isQuiet: Boolean = false) {
        val requestedDay = day
        try {
            val read = connection.journalDay(requestedDay)
            // The person may have moved to another day while this was read.
            if (requestedDay != day) return
            journalDay = read
            loadFailure = null
        } catch (failure: CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            if (requestedDay != day || isQuiet) return
            if (journalDay == null) loadFailure = failure else host.complain(failure)
        } finally {
            if (requestedDay == day) hasLoaded = true
        }
    }

    /** Reads the day again every few seconds while something is being written for it, for as long as the screen would wait. */
    suspend fun readWhilePending() {
        var attemptCount = 0
        while (JournalPolling.shouldRead(isPending, attemptCount)) {
            delay(JournalPolling.INTERVAL_MS)
            attemptCount += 1
            load(isQuiet = true)
        }
    }

    /** Does a change and reads the day again; whether it was done. */
    private suspend fun change(failureText: String, operation: suspend () -> Unit): Boolean {
        isBusy = true
        try {
            operation()
        } catch (failure: CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            host.complain(reasonOrSay(failure, failureText))
            return false
        } finally {
            isBusy = false
        }
        load(isQuiet = true)
        return true
    }

    suspend fun setMood(level: JournalMoodLevel) {
        val words = journalDay?.mood?.moodWords
        change(context.getString(R.string.journal_could_not_save_day)) { connection.setJournalMood(day, level.moodLevel, words) }
    }

    suspend fun clearMood() {
        change(context.getString(R.string.journal_could_not_clear_day)) { connection.setJournalMood(day, null) }
    }

    suspend fun writeAgain() {
        change(context.getString(R.string.journal_could_not_ask_written_again_day)) { connection.writeJournalNarrative(JournalTarget.Day(day)) }
    }

    suspend fun removeCorrection(index: Int) {
        change(context.getString(R.string.journal_could_not_remove_correction)) { connection.removeJournalCorrection(JournalTarget.Day(day), index) }
    }

    /** Takes the story away. The corrections stay, and a later dream writes a new one. */
    suspend fun removeNarrative() {
        if (change(context.getString(R.string.journal_could_not_remove_story)) { connection.deleteJournalNarrative(JournalTarget.Day(day)) }) {
            host.say(context.getString(R.string.journal_story_removed))
        }
    }

    /** Pins the record of a moment of the day to the day. */
    suspend fun pin(moment: JournalMoment) {
        if (change(context.getString(R.string.journal_could_not_pin)) { connection.pinJournalRecord(day, moment.recordKind, moment.recordId) }) {
            host.say(context.getString(R.string.journal_pinned_to_day))
        }
    }

    /** Takes a pin off the day, or hides a choice of the agent, which the server keeps hidden. */
    suspend fun remove(link: JournalLink) {
        if (change(context.getString(R.string.journal_could_not_remove_from_day)) { connection.removeJournalLink(link.linkId) }) {
            host.say(context.getString(R.string.journal_removed_from_day))
        }
    }
}

/** JournalPeriodModel is one week, month or year of the journal (ios JournalModel.swift). */
class JournalPeriodModel(val connection: Connection, val periodKind: JournalPeriodKind, anchorDay: String, private val host: JournalModel, private val context: Context) {
    /** Any day in the period shown: the period that holds it is read. */
    var anchorDay: String by mutableStateOf(anchorDay)
        private set
    var period: JournalPeriod? by mutableStateOf(null)
        private set
    var hasLoaded: Boolean by mutableStateOf(false)
        private set
    var loadFailure: Throwable? by mutableStateOf(null)
        private set
    var isBusy: Boolean by mutableStateOf(false)
        private set

    val isPending: Boolean get() = period?.isPending ?: false

    /** What the period's words and narrative are changed by: the period that holds the anchor day. */
    val target: JournalTarget get() = JournalTarget.Period(periodKind, anchorDay)

    suspend fun show(newAnchorDay: String) {
        if (newAnchorDay == anchorDay) return
        anchorDay = newAnchorDay
        period = null
        hasLoaded = false
        loadFailure = null
        load()
    }

    suspend fun load(isQuiet: Boolean = false) {
        val requestedDay = anchorDay
        try {
            val read = connection.journalPeriod(periodKind, requestedDay)
            if (requestedDay != anchorDay) return
            period = read
            loadFailure = null
        } catch (failure: CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            if (requestedDay != anchorDay || isQuiet) return
            if (period == null) loadFailure = failure else host.complain(failure)
        } finally {
            if (requestedDay == anchorDay) hasLoaded = true
        }
    }

    suspend fun readWhilePending() {
        var attemptCount = 0
        while (JournalPolling.shouldRead(isPending, attemptCount)) {
            delay(JournalPolling.INTERVAL_MS)
            attemptCount += 1
            load(isQuiet = true)
        }
    }

    private suspend fun change(failureText: String, operation: suspend () -> Unit): Boolean {
        isBusy = true
        try {
            operation()
        } catch (failure: CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            host.complain(reasonOrSay(failure, failureText))
            return false
        } finally {
            isBusy = false
        }
        load(isQuiet = true)
        return true
    }

    suspend fun writeAgain() {
        change(context.getString(R.string.journal_could_not_ask_written_again)) { connection.writeJournalNarrative(target) }
    }

    suspend fun removeCorrection(index: Int) {
        change(context.getString(R.string.journal_could_not_remove_correction)) { connection.removeJournalCorrection(target, index) }
    }

    suspend fun removeNarrative() {
        if (change(context.getString(R.string.journal_could_not_remove_story)) { connection.deleteJournalNarrative(target) }) {
            host.say(context.getString(R.string.journal_story_removed))
        }
    }
}

/** JournalSearchModel is a search of the journal: the words typed, and the days and periods that hold them, a page at a time, newest first. */
class JournalSearchModel(val connection: Connection, private val host: JournalModel) {
    /** The words as typed, which the search field edits. */
    var query: String by mutableStateOf("")
    var hits: List<JournalSearchHit> by mutableStateOf(emptyList())
        private set
    var totalCount: Int by mutableStateOf(0)
        private set

    /** The words the hits were found for, empty before the first read. */
    var searchedQuery: String by mutableStateOf("")
        private set
    var isLoadingMore: Boolean by mutableStateOf(false)
        private set
    var failure: Throwable? by mutableStateOf(null)
        private set

    /** The words to search for: the typed words trimmed and cut to what the server takes; null while there are none. */
    val searchWords: String? get() = JournalSearch.normalized(query)

    val canLoadMore: Boolean get() = hits.size < totalCount

    /** Reads the first page for the words typed, a moment after the last key. Words already read are not read again unless the read failed. */
    suspend fun search(isDebounced: Boolean = true) {
        val words = searchWords
        if (words == null) {
            hits = emptyList()
            totalCount = 0
            searchedQuery = ""
            failure = null
            return
        }
        if (words == searchedQuery && failure == null) return
        if (isDebounced) delay(300)
        try {
            val page = connection.searchJournal(words)
            // The person may have typed more while this was read.
            if (words != searchWords) return
            hits = page.hits
            totalCount = page.totalCount
            searchedQuery = words
            failure = null
        } catch (failed: CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            if (words != searchWords) return
            hits = emptyList()
            totalCount = 0
            searchedQuery = words
            failure = failed
        }
    }

    /** Reads the next page when the last hit on the screen comes into view. */
    suspend fun loadMore(after: JournalSearchHit) {
        val words = searchWords ?: return
        if (after.id != hits.lastOrNull()?.id || !canLoadMore || isLoadingMore || words != searchedQuery) return
        isLoadingMore = true
        try {
            val page = connection.searchJournal(words, offset = hits.size)
            if (words != searchWords) return
            hits = JournalSearch.appending(page.hits, hits)
            totalCount = page.totalCount
        } catch (failed: CancellationException) {
            throw failed
        } catch (failed: Throwable) {
            host.complain(failed)
        } finally {
            isLoadingMore = false
        }
    }
}

/** JournalSettingsModel is what the person decided about their journal (ios JournalModel.swift). */
class JournalSettingsModel(val connection: Connection, private val host: JournalModel, private val context: Context) {
    var settings: JournalSettings? by mutableStateOf(null)
        private set
    var loadFailure: Throwable? by mutableStateOf(null)
        private set
    var isBusy: Boolean by mutableStateOf(false)
        private set

    suspend fun load() {
        try {
            settings = connection.journalSettings()
            loadFailure = null
        } catch (failure: CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            if (settings == null) loadFailure = failure else host.complain(failure)
        }
    }

    suspend fun setWeather(isOn: Boolean) = save(context.getString(R.string.journal_could_not_change_weather)) { connection.updateJournalSettings(isWeatherOn = isOn) }

    suspend fun setEveningQuestion(isOn: Boolean) = save(context.getString(R.string.journal_could_not_change_evening_question)) { connection.updateJournalSettings(isEveningQuestionOn = isOn) }

    suspend fun setEveningQuestionTime(time: String) = save(context.getString(R.string.journal_could_not_change_evening_time)) { connection.updateJournalSettings(eveningQuestionTime = time) }

    private suspend fun save(failureText: String, operation: suspend () -> JournalSettings) {
        isBusy = true
        try {
            settings = operation()
        } catch (failure: CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            host.complain(reasonOrSay(failure, failureText))
            // What the server holds is what is shown, so a refused change puts the switch back.
            load()
        } finally {
            isBusy = false
        }
    }

    /** Erases the whole journal; whether it did. */
    suspend fun erase(): Boolean {
        isBusy = true
        return try {
            val erased = connection.eraseJournal()
            host.say(context.getString(R.string.journal_days_erased, erased.dayCount))
            load()
            true
        } catch (failure: CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            host.complain(reasonOrSay(failure, context.getString(R.string.journal_could_not_erase)))
            false
        } finally {
            isBusy = false
        }
    }
}
