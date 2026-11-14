package com.teanode.app.calendar

import android.content.Context
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.teanode.app.R
import com.teanode.app.session.ErrorWords
import com.teanode.app.ui.components.SnackbarController
import com.teanode.kit.Connection
import com.teanode.kit.calendar.AgentReminder
import com.teanode.kit.calendar.deleteReminder
import com.teanode.kit.calendar.reminders
import com.teanode.kit.calendar.saveReminder
import com.teanode.kit.calendar.setReminderDone
import kotlinx.serialization.json.JsonElement

/**
 * RemindersModel is the reminders as the dashboard's Reminders view keeps them (ios RemindersView.swift): what is still
 * to do, and what is done when asked for.
 */
class RemindersModel(private val connection: Connection, private val context: Context) {
    var snackbar: SnackbarController? = null

    var open: List<AgentReminder> by mutableStateOf(emptyList())
        private set
    var done: List<AgentReminder> by mutableStateOf(emptyList())
        private set
    var isLoaded: Boolean by mutableStateOf(false)
        private set
    var isShowingDone: Boolean by mutableStateOf(false)
        private set

    /** The reminders on the list now: to do, or done. */
    val shown: List<AgentReminder> get() = if (isShowingDone) done else open

    fun reminder(reminderId: String): AgentReminder? = (open + done).firstOrNull { each -> each.id == reminderId }

    suspend fun showDone(isDone: Boolean) {
        isShowingDone = isDone
        load()
    }

    suspend fun load() {
        try {
            open = connection.reminders(isDone = false)
            if (isShowingDone) done = connection.reminders(isDone = true)
            isLoaded = true
        } catch (failure: kotlinx.coroutines.CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            if (!ErrorWords.isCancellation(failure)) snackbar?.showError(failure)
        }
    }

    /** Runs a change, says so, and reads the lists again. */
    suspend fun act(said: String?, work: suspend () -> Unit): Boolean = try {
        work()
        if (said != null) snackbar?.show(said)
        load()
        true
    } catch (failure: kotlinx.coroutines.CancellationException) {
        throw failure
    } catch (failure: Throwable) {
        snackbar?.showError(failure)
        false
    }

    suspend fun setDone(reminder: AgentReminder, isDone: Boolean) {
        val said = if (isDone) context.getString(if (reminder.isRepeating == true) R.string.calendar_reminder_moved_on else R.string.calendar_reminder_marked_done) else null
        act(said) { connection.setReminderDone(reminder.id, isDone) }
    }

    suspend fun delete(reminder: AgentReminder) {
        act(context.getString(R.string.calendar_reminder_removed)) { connection.deleteReminder(reminder.id) }
    }

    suspend fun add(title: String) {
        act(context.getString(R.string.calendar_reminder_added)) { connection.saveReminder(null, mapOf("title" to kotlinx.serialization.json.JsonPrimitive(title))) }
    }

    suspend fun save(reminderId: String?, fields: Map<String, JsonElement>): Boolean =
        act(context.getString(if (reminderId == null) R.string.calendar_reminder_added else R.string.calendar_reminder_saved)) {
            connection.saveReminder(reminderId, fields)
        }
}
