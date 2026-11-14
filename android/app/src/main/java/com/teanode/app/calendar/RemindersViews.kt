package com.teanode.app.calendar

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.outlined.AddCircleOutline
import androidx.compose.material.icons.outlined.Checklist
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.Repeat
import androidx.compose.material.icons.outlined.RadioButtonUnchecked
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
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
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.calendar.AgentReminder
import com.teanode.kit.calendar.CalendarDates
import com.teanode.kit.calendar.ServerTime
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import java.time.Instant
import java.time.LocalDate
import java.time.LocalTime
import java.time.ZoneId

/** When a reminder is due, in words, and whether that has passed. */
internal data class ReminderDue(val text: String, val isOverdue: Boolean)

internal fun reminderDue(reminder: AgentReminder, zone: ZoneId = ZoneId.systemDefault(), now: Instant = Instant.now()): ReminderDue? {
    val dueAt = reminder.dueAt ?: return null
    val moment = ServerTime.instant(dueAt) ?: return null
    if (reminder.isDueDate == true) {
        val day = CalendarDates.dayOfAllDay(dueAt)
        if (day != null) {
            val local = day.toLocalDate()
            return ReminderDue(DateWords.abbreviatedDate(local), !reminder.isDone && local.isBefore(LocalDate.now(zone)))
        }
    }
    return ReminderDue(DateWords.abbreviatedDateTime(moment, zone), !reminder.isDone && moment.isBefore(now))
}

/** The reminders: To do and Done, a tap to tick one off, and a tap on its words to change it (ios RemindersView.swift, RemindersList). */
@Composable
fun RemindersList(model: RemindersModel) {
    val navigator = LocalAppNavigator.current
    val scope = rememberCoroutineScope()
    var deleting by remember { mutableStateOf<AgentReminder?>(null) }
    var quickTitle by remember { mutableStateOf("") }
    LaunchedEffect(model) { model.load() }
    Column(Modifier.fillMaxSize()) {
        Row(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            FilterChip(selected = !model.isShowingDone, onClick = { scope.launch { model.showDone(false) } }, label = { Text(stringResource(R.string.calendar_to_do)) })
            FilterChip(selected = model.isShowingDone, onClick = { scope.launch { model.showDone(true) } }, label = { Text(stringResource(R.string.calendar_done)) })
        }
        if (!model.isLoaded) {
            LoadingState()
        } else {
            LazyColumn(Modifier.weight(1f)) {
                if (!model.isShowingDone) {
                    // Adding is the list's first line, as on the dashboard: type and press return.
                    item {
                        Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically) {
                            Icon(Icons.Outlined.AddCircleOutline, contentDescription = null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(28.dp))
                            OutlinedTextField(
                                value = quickTitle,
                                onValueChange = { text -> quickTitle = text },
                                placeholder = { Text(stringResource(R.string.calendar_add_reminder)) },
                                modifier = Modifier.weight(1f).padding(start = 12.dp),
                                singleLine = true,
                                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                                keyboardActions = KeyboardActions(onDone = {
                                    val title = quickTitle.trim()
                                    if (title.isNotEmpty()) {
                                        quickTitle = ""
                                        scope.launch { model.add(title) }
                                    }
                                }),
                            )
                        }
                    }
                }
                if (model.shown.isEmpty()) {
                    item {
                        Box(Modifier.fillMaxWidth().padding(top = 48.dp)) {
                            EmptyState(
                                icon = Icons.Outlined.Checklist,
                                title = stringResource(if (model.isShowingDone) R.string.calendar_nothing_done_yet else R.string.calendar_nothing_to_do),
                                description = if (model.isShowingDone) null else stringResource(R.string.calendar_add_reminder_hint),
                            )
                        }
                    }
                }
                items(model.shown, key = { reminder -> reminder.id }) { reminder ->
                    ReminderRow(
                        reminder,
                        onToggle = { scope.launch { model.setDone(reminder, !reminder.isDone) } },
                        onOpen = { navigator.navigate(CalendarRoutes.reminder(reminder.id)) },
                        onDelete = { deleting = reminder },
                    )
                }
            }
        }
    }
    deleting?.let { reminder ->
        ConfirmationDialog(
            title = stringResource(R.string.calendar_delete_quoted, reminder.title),
            confirmLabel = stringResource(R.string.calendar_delete_reminder),
            isDestructive = true,
            onConfirm = {
                deleting = null
                scope.launch { model.delete(reminder) }
            },
            onDismiss = { deleting = null },
        )
    }
}

/** One reminder: a circle to tick, its title, notes and when it is due, red once overdue. */
@Composable
fun ReminderRow(reminder: AgentReminder, onToggle: () -> Unit, onOpen: () -> Unit, onDelete: () -> Unit) {
    var isMenuOpen by remember { mutableStateOf(false) }
    val due = reminderDue(reminder)
    Row(Modifier.fillMaxWidth().padding(horizontal = 4.dp), verticalAlignment = Alignment.Top) {
        IconButton(onClick = onToggle) {
            Icon(
                if (reminder.isDone) Icons.Filled.CheckCircle else Icons.Outlined.RadioButtonUnchecked,
                contentDescription = stringResource(if (reminder.isDone) R.string.calendar_mark_not_done else R.string.calendar_mark_done),
                tint = if (reminder.isDone) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Column(Modifier.weight(1f).clickable(onClick = onOpen).padding(vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                val priority = reminder.priority
                if (priority != null && priority > 0 && priority <= 4) {
                    val highPriority = stringResource(R.string.calendar_high_priority)
                    Text("!", color = MaterialTheme.colorScheme.error, modifier = Modifier.semantics { contentDescription = highPriority })
                }
                Text(reminder.title, style = MaterialTheme.typography.bodyLarge, color = if (reminder.isDone) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface)
            }
            reminder.notes?.takeIf { text -> text.isNotEmpty() }?.let { notes ->
                Text(notes, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2)
            }
            if (due != null) {
                Row(horizontalArrangement = Arrangement.spacedBy(4.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text(due.text, style = MaterialTheme.typography.bodySmall, color = if (due.isOverdue) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurfaceVariant)
                    if (reminder.isRepeating == true) {
                        Icon(Icons.Outlined.Repeat, contentDescription = stringResource(R.string.calendar_repeats), modifier = Modifier.size(14.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
        }
        Box {
            IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.calendar_more_actions)) }
            DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                DropdownMenuItem(text = { Text(stringResource(R.string.calendar_delete)) }, onClick = {
                    isMenuOpen = false
                    onDelete()
                })
            }
        }
    }
}

/** The reminder editor adds a reminder or changes one: its title, notes, and a day it is due, with a time or without (ios ReminderEditor). */
@Composable
fun ReminderEditorScreen(model: CalendarModel, reminderId: String?) {
    val navigator = LocalAppNavigator.current
    val scope = rememberCoroutineScope()
    val reminders = model.reminders
    val reminder = reminderId?.let { id -> reminders.reminder(id) }
    val zone = model.rules.zone
    var title by remember { mutableStateOf(reminder?.title.orEmpty()) }
    var notes by remember { mutableStateOf(reminder?.notes.orEmpty()) }
    val dueMoment = reminder?.dueAt?.let { text -> ServerTime.instant(text) }
    val isDueDate = reminder?.isDueDate == true
    var isDue by remember { mutableStateOf(dueMoment != null) }
    var hasTime by remember { mutableStateOf(dueMoment != null && !isDueDate) }
    var dueDay by remember {
        mutableStateOf(
            if (dueMoment == null) LocalDate.now(zone)
            else if (isDueDate) CalendarDates.dayOfAllDay(reminder?.dueAt)?.toLocalDate() ?: dueMoment.atZone(zone).toLocalDate()
            else dueMoment.atZone(zone).toLocalDate(),
        )
    }
    var dueTime by remember { mutableStateOf(if (dueMoment != null && !isDueDate) dueMoment.atZone(zone).toLocalTime().withSecond(0).withNano(0) else LocalTime.of(9, 0)) }
    var isSaving by remember { mutableStateOf(false) }
    var isConfirmingDelete by remember { mutableStateOf(false) }
    LaunchedEffect(Unit) { if (!reminders.isLoaded) reminders.load() }

    /** The title, the notes and the due time, as the dashboard sends them: a day alone is due that day, a day and a time a moment where the phone is. */
    fun save() {
        val fields = HashMap<String, JsonElement>()
        fields["title"] = JsonPrimitive(title.trim())
        fields["notes"] = JsonPrimitive(notes.trim())
        if (!isDue) {
            if (reminder?.dueAt != null) fields["isDueCleared"] = JsonPrimitive(true)
        } else if (hasTime) {
            fields["dueAt"] = JsonPrimitive(CalendarDates.text(dueDay.atTime(dueTime).atZone(zone).toInstant()))
        } else {
            fields["dueOn"] = JsonPrimitive("%04d-%02d-%02d".format(dueDay.year, dueDay.monthValue, dueDay.dayOfMonth))
        }
        isSaving = true
        scope.launch {
            val isSaved = reminders.save(reminder?.id, fields)
            isSaving = false
            if (isSaved) navigator.back()
        }
    }

    TeaNodeScreen(
        title = stringResource(if (reminder == null) R.string.calendar_new_reminder else R.string.calendar_edit_reminder),
        onBack = { navigator.back() },
        actions = { TextButton(onClick = { save() }, enabled = !isSaving && title.isNotBlank()) { Text(stringResource(R.string.calendar_save)) } },
    ) { contentPadding ->
        Column(Modifier.fillMaxSize().padding(contentPadding).verticalScroll(rememberScrollState())) {
            OutlinedTextField(
                value = title, onValueChange = { text -> title = text }, label = { Text(stringResource(R.string.calendar_title)) },
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), singleLine = true,
            )
            OutlinedTextField(
                value = notes, onValueChange = { text -> notes = text }, label = { Text(stringResource(R.string.calendar_notes)) },
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), minLines = 2,
            )
            SwitchRow(stringResource(R.string.calendar_due), isDue, { isOn -> isDue = isOn })
            if (isDue) {
                SwitchRow(stringResource(R.string.calendar_at_a_time), hasTime, { isOn -> hasTime = isOn })
                DateField(stringResource(R.string.calendar_when), dueDay, { day -> dueDay = day })
                if (hasTime) TimeField(stringResource(R.string.calendar_time), dueTime, { time -> dueTime = time })
            }
            if (reminder != null) {
                if (reminder.isRepeating == true) {
                    Text(
                        stringResource(R.string.calendar_reminder_repeats_note),
                        modifier = Modifier.padding(16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                TextButton(onClick = { isConfirmingDelete = true }, modifier = Modifier.padding(horizontal = 8.dp)) {
                    Text(stringResource(R.string.calendar_delete_reminder), color = MaterialTheme.colorScheme.error)
                }
            }
        }
    }
    if (isConfirmingDelete && reminder != null) {
        ConfirmationDialog(
            title = stringResource(R.string.calendar_delete_quoted, title),
            confirmLabel = stringResource(R.string.calendar_delete_reminder),
            isDestructive = true,
            onConfirm = {
                isConfirmingDelete = false
                scope.launch {
                    reminders.delete(reminder)
                    navigator.back()
                }
            },
            onDismiss = { isConfirmingDelete = false },
        )
    }
}

