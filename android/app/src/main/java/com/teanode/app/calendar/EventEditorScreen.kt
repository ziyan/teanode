package com.teanode.app.calendar

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Place
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.knowledge.InMemoryCard
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.ErrorState
import com.teanode.kit.calendar.AgentCalendar
import com.teanode.kit.calendar.CalendarChange
import com.teanode.kit.calendar.CalendarDates
import com.teanode.kit.calendar.CalendarEventDetail
import com.teanode.kit.calendar.CalendarRepeat
import com.teanode.kit.calendar.ServerTime
import com.teanode.kit.calendar.calendarEvent
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonPrimitive
import java.time.Instant

/**
 * EventOpenerScreen reads an event whole before it is edited, since every field is written back (ios CalendarView.swift,
 * EventOpener). An event in a calendar shared below edit is read and not changed.
 */
@Composable
fun EventOpenerScreen(model: CalendarModel, calendarId: String, eventId: String) {
    val navigator = LocalAppNavigator.current
    var detail by remember { mutableStateOf<CalendarEventDetail?>(null) }
    var failure by remember { mutableStateOf<Throwable?>(null) }
    var attempt by remember { mutableStateOf(0) }
    LaunchedEffect(calendarId, eventId, attempt) {
        failure = null
        try {
            if (model.calendars.isEmpty()) model.load()
            detail = model.connection.calendarEvent(calendarId, eventId)
        } catch (error: kotlinx.coroutines.CancellationException) {
            throw error
        } catch (error: Throwable) {
            failure = error
        }
    }
    val read = detail
    val problem = failure
    val target = model.calendar(calendarId)
    when {
        read != null && target != null && !target.canWrite -> EventReaderScreen(model, target, read)
        read != null -> EventEditorScreen(model, EventDraft.of(read, calendarId, model.rules.zone))
        problem != null -> TeaNodeScreen(title = stringResource(R.string.calendar_event), onBack = { navigator.back() }) { contentPadding ->
            ErrorState(model.describeError(problem), onRetry = { attempt += 1 }, modifier = Modifier.padding(contentPadding))
        }
        else -> TeaNodeScreen(title = stringResource(R.string.calendar_event), onBack = { navigator.back() }) { contentPadding ->
            LoadingState(Modifier.padding(contentPadding))
        }
    }
}

/** The editor for a new event that starts at [startsAt]. */
@Composable
fun NewEventScreen(model: CalendarModel, startsAt: Instant) {
    EventEditorScreen(model, EventDraft(startsAt = startsAt))
}

/** EventEditorScreen is a new event, or one read whole: its title, when, where, how it repeats, who is invited and notes (ios CalendarView.swift, EventEditor). */
@Composable
fun EventEditorScreen(model: CalendarModel, initial: EventDraft) {
    val navigator = LocalAppNavigator.current
    val scope = rememberCoroutineScope()
    var draft by remember { mutableStateOf(initial.copy(calendarId = initial.calendarId ?: model.calendar?.id)) }
    var isSaving by remember { mutableStateOf(false) }
    var isConfirmingDelete by remember { mutableStateOf(false) }
    var isCustomRepeatOpen by remember { mutableStateOf(false) }
    val zone = model.rules.zone
    // The calendar the event is in or goes in: the person's own unless they chose another.
    val target: AgentCalendar? = draft.calendarId?.let { id -> model.calendar(id) } ?: model.calendar
    val isOwn = target?.isOwn ?: true
    val repeatContext = draft.repeatContext(target, zone)
    LaunchedEffect(model.calendar) { if (draft.calendarId == null) draft = draft.copy(calendarId = model.calendar?.id) }

    BackHandler(enabled = isCustomRepeatOpen) { isCustomRepeatOpen = false }
    if (isCustomRepeatOpen) {
        CustomRepeatScreen(draft.recurrence, repeatContext, { rule -> draft = draft.copy(recurrence = rule) }, onBack = { isCustomRepeatOpen = false })
        return
    }

    fun save() {
        val calendar = target ?: return
        isSaving = true
        scope.launch {
            if (model.make(draft.change(calendar, zone))) navigator.back()
            isSaving = false
        }
    }

    fun delete() {
        val calendar = target ?: return
        val eventId = draft.eventId ?: return
        isSaving = true
        scope.launch {
            val change = CalendarChange("delete", draft.summary, variables = mapOf("calendarId" to JsonPrimitive(calendar.id), "id" to JsonPrimitive(eventId)))
            if (model.make(change)) navigator.back()
            isSaving = false
        }
    }

    TeaNodeScreen(
        title = stringResource(if (draft.eventId == null) R.string.calendar_new_event else R.string.calendar_edit_event),
        onBack = { navigator.back() },
        actions = { TextButton(onClick = { save() }, enabled = !isSaving && draft.summary.isNotBlank() && target != null) { Text(stringResource(R.string.calendar_save)) } },
    ) { contentPadding ->
        Column(Modifier.fillMaxSize().padding(contentPadding).verticalScroll(rememberScrollState())) {
            // Which calendar a new event goes in, when there is a choice: their own, a circle's, or one shared with them at edit. An event already kept stays where it is.
            if (draft.eventId == null && model.writableCalendars.size > 1) {
                val writable = model.writableCalendars
                ChoiceRow(
                    stringResource(R.string.calendar_calendar), target?.let { chosen -> model.displayName(chosen) }.orEmpty(), writable.map { each -> model.displayName(each) },
                ) { index -> draft = draft.copy(calendarId = writable[index].id) }
                HorizontalDivider()
            }
            OutlinedTextField(
                value = draft.summary, onValueChange = { text -> draft = draft.copy(summary = text) }, label = { Text(stringResource(R.string.calendar_title)) },
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), singleLine = true,
            )
            OutlinedTextField(
                value = draft.location, onValueChange = { text -> draft = draft.copy(location = text) }, label = { Text(stringResource(R.string.calendar_location)) },
                leadingIcon = { Icon(Icons.Outlined.Place, contentDescription = null) },
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), singleLine = true,
            )
            SwitchRow(stringResource(R.string.calendar_all_day), draft.isAllDay, { isOn -> draft = draft.copy(isAllDay = isOn) })
            val startsLocal = draft.startsAt.atZone(zone)
            val endsLocal = draft.endsAt.atZone(zone)
            DateField(stringResource(R.string.calendar_starts), startsLocal.toLocalDate(), { date ->
                draft = moveStart(draft, date.atTime(startsLocal.toLocalTime()).atZone(zone).toInstant())
            })
            if (!draft.isAllDay) TimeField(stringResource(R.string.calendar_starts_at), startsLocal.toLocalTime(), { time ->
                draft = moveStart(draft, startsLocal.toLocalDate().atTime(time).atZone(zone).toInstant())
            })
            DateField(stringResource(R.string.calendar_ends), endsLocal.toLocalDate(), { date ->
                draft = draft.copy(endsAt = maxOf(draft.startsAt, date.atTime(endsLocal.toLocalTime()).atZone(zone).toInstant()))
            }, minimum = startsLocal.toLocalDate())
            if (!draft.isAllDay) TimeField(stringResource(R.string.calendar_ends_at), endsLocal.toLocalTime(), { time ->
                draft = draft.copy(endsAt = maxOf(draft.startsAt, endsLocal.toLocalDate().atTime(time).atZone(zone).toInstant()))
            })
            RepeatRows(draft, repeatContext, onRule = { rule -> draft = draft.copy(recurrence = rule) }, onOpenCustom = { isCustomRepeatOpen = true })
            HorizontalDivider()
            // Guests only in the person's own calendar: the invitations go out from, and the answers come back to, the owner.
            if (isOwn) {
                TeaNodeSectionHeader(stringResource(R.string.calendar_invited))
                OutlinedTextField(
                    value = draft.invited, onValueChange = { text -> draft = draft.copy(invited = text) }, label = { Text(stringResource(R.string.calendar_one_address_per_line)) },
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp), minLines = 2, keyboardOptions = androidx.compose.foundation.text.KeyboardOptions(keyboardType = KeyboardType.Email),
                )
                Text(stringResource(R.string.calendar_invitations_footer), modifier = Modifier.padding(16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                // Only in the person's own calendar: in a circle's, or one shared with them at edit, every reader is an editor and sees the event whatever it says.
                SwitchRow(stringResource(R.string.calendar_private), draft.isPrivate, { isOn -> draft = draft.copy(isPrivate = isOn) })
                Text(stringResource(R.string.calendar_private_footer), modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            TeaNodeSectionHeader(stringResource(R.string.calendar_notes))
            OutlinedTextField(
                value = draft.notes, onValueChange = { text -> draft = draft.copy(notes = text) }, label = { Text(stringResource(R.string.calendar_notes)) },
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp), minLines = 3,
            )
            InMemoryCard("calendar_event", draft.eventId.orEmpty())
            if (draft.eventId != null) {
                TextButton(onClick = { isConfirmingDelete = true }, modifier = Modifier.padding(horizontal = 8.dp, vertical = 8.dp)) {
                    Text(stringResource(R.string.calendar_delete_event), color = MaterialTheme.colorScheme.error)
                }
            }
        }
    }
    if (isConfirmingDelete) {
        ConfirmationDialog(
            title = stringResource(R.string.calendar_delete_quoted, draft.summary),
            text = stringResource(if (draft.recurrence.isEmpty()) R.string.calendar_delete_event_message else R.string.calendar_delete_repeating_message),
            confirmLabel = stringResource(R.string.calendar_delete_event),
            isDestructive = true,
            onConfirm = {
                isConfirmingDelete = false
                delete()
            },
            onDismiss = { isConfirmingDelete = false },
        )
    }
}

/** The end moves with the start, keeping how long it lasts. */
private fun moveStart(draft: EventDraft, startsAt: Instant): EventDraft {
    val shift = startsAt.epochSecond - draft.startsAt.epochSecond
    return draft.copy(startsAt = startsAt, endsAt = maxOf(startsAt, draft.endsAt.plusSeconds(shift)))
}

/** The Repeat choice, and for a custom repeat the sentence it makes as a row that opens the screen to change it. */
@Composable
private fun RepeatRows(draft: EventDraft, context: CalendarRepeat.Context, onRule: (String) -> Unit, onOpenCustom: () -> Unit) {
    val parts = CalendarRepeat.parse(draft.recurrence, context)
    val preset = CalendarRepeat.Preset.of(draft.recurrence)
    val customTitle = parts?.let { read -> CalendarRepeatWords.shortText(read) } ?: stringResource(R.string.calendar_custom)
    val presets = CalendarRepeat.Preset.entries
    val labels = presets.map { each ->
        when (each) {
            CalendarRepeat.Preset.Never -> stringResource(R.string.calendar_never)
            CalendarRepeat.Preset.Daily -> stringResource(R.string.calendar_every_day)
            CalendarRepeat.Preset.Weekdays -> stringResource(R.string.calendar_every_weekday)
            CalendarRepeat.Preset.Weekly -> stringResource(R.string.calendar_every_week)
            CalendarRepeat.Preset.Monthly -> stringResource(R.string.calendar_every_month)
            CalendarRepeat.Preset.Yearly -> stringResource(R.string.calendar_every_year)
            CalendarRepeat.Preset.Custom -> customTitle
        }
    }
    ChoiceRow(stringResource(R.string.calendar_repeat), labels[presets.indexOf(preset)], labels) { index ->
        // Choosing Custom opens its screen and leaves the rule as it was, since the rule is only changed there.
        val rule = presets[index].rule
        if (rule != null) onRule(rule) else onOpenCustom()
    }
    if (preset == CalendarRepeat.Preset.Custom) {
        val sentence = parts?.let { read -> CalendarRepeatWords.sentenceText(CalendarRepeat.sentence(read)) } ?: stringResource(R.string.calendar_set_in_another_program)
        Text(sentence, modifier = Modifier.fillMaxWidth().clickable(onClick = onOpenCustom).padding(horizontal = 16.dp, vertical = 14.dp), color = MaterialTheme.colorScheme.primary)
    }
}

/** EventReaderScreen is an event in a calendar shared below edit: what the person is shown of it, to read and not to change (ios CalendarSharingViews.swift, EventReader). */
@Composable
fun EventReaderScreen(model: CalendarModel, calendar: AgentCalendar?, detail: CalendarEventDetail) {
    val navigator = LocalAppNavigator.current
    val zone = model.rules.zone
    val whenText = if (detail.allDay) {
        // The day it is on, read from the parts the server wrote and then placed in this zone, so that it is the same day everywhere.
        val first = CalendarDates.dayOfAllDay(detail.startsAt)?.toLocalDate()
        if (first == null) stringResource(R.string.calendar_all_day) else stringResource(R.string.calendar_day_all_day, DateWords.weekdayMonthDay(first))
    } else {
        val start = ServerTime.instant(detail.startsAt)
        val end = ServerTime.instant(detail.endsAt) ?: start
        if (start == null || end == null) "" else "${DateWords.weekdayMonthDay(start.atZone(zone).toLocalDate())} ${DateWords.shortTime(start, zone)} - ${DateWords.shortTime(maxOf(start, end), zone)}"
    }
    TeaNodeScreen(title = stringResource(R.string.calendar_event), onBack = { navigator.back() }) { contentPadding ->
        Column(Modifier.fillMaxSize().padding(contentPadding).verticalScroll(rememberScrollState()).padding(16.dp)) {
            Text(detail.summary?.takeIf { text -> text.isNotEmpty() } ?: stringResource(R.string.calendar_no_title), style = MaterialTheme.typography.titleLarge)
            Text(whenText, modifier = Modifier.padding(top = 8.dp), style = MaterialTheme.typography.bodyLarge)
            detail.location?.takeIf { place -> place.isNotEmpty() }?.let { place ->
                Text(place, modifier = Modifier.padding(top = 8.dp), style = MaterialTheme.typography.bodyLarge)
            }
            val guests = detail.attendees.orEmpty()
            if (guests.isNotEmpty()) {
                TeaNodeSectionHeader(stringResource(R.string.calendar_invited))
                for (guest in guests) Text(guest.name?.takeIf { text -> text.isNotEmpty() } ?: guest.address, modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
            }
            detail.description?.takeIf { text -> text.isNotEmpty() }?.let { notes ->
                TeaNodeSectionHeader(stringResource(R.string.calendar_notes))
                Text(notes, modifier = Modifier.padding(horizontal = 16.dp))
            }
            if (calendar != null) {
                Text(
                    stringResource(R.string.calendar_shared_with_you_in, model.displayName(calendar)),
                    modifier = Modifier.padding(top = 24.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

