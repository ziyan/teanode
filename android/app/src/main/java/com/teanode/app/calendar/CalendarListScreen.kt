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
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.background
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CalendarMonth
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.People
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material.icons.outlined.AddBox
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.AvatarView
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.calendar.AgentCalendar
import com.teanode.kit.calendar.CalendarCircle
import com.teanode.kit.calendar.CalendarShareLevel
import com.teanode.kit.calendar.calendarCircles
import com.teanode.kit.calendar.createCircleCalendar
import com.teanode.kit.calendar.deleteCircleCalendar
import com.teanode.kit.calendar.saveCalendar
import kotlinx.coroutines.launch

/** What a share at this level is called. */
@Composable
fun CalendarShareLevel.title(): String = stringResource(
    when (this) {
        CalendarShareLevel.Busy -> R.string.calendar_level_busy
        CalendarShareLevel.Titles -> R.string.calendar_level_titles
        CalendarShareLevel.Details -> R.string.calendar_level_details
        CalendarShareLevel.Edit -> R.string.calendar_level_edit
        CalendarShareLevel.Owner -> R.string.calendar_level_owner
    },
)

/** What somebody a calendar is shared at this level can do. */
@Composable
fun CalendarShareLevel.explanation(): String = when (this) {
    CalendarShareLevel.Busy -> stringResource(R.string.calendar_explain_busy)
    CalendarShareLevel.Titles -> stringResource(R.string.calendar_explain_titles)
    CalendarShareLevel.Details -> stringResource(R.string.calendar_explain_details)
    CalendarShareLevel.Edit -> stringResource(R.string.calendar_explain_edit)
    CalendarShareLevel.Owner -> ""
}

/**
 * CalendarListScreen is the calendars the person reaches: their own, their circles' and the ones shared with them, each
 * with its color and whose it is, and a switch to show or hide it. What is hidden is hidden on this phone only. Below
 * the list are the ways to rename and share their own calendar and to make a circle's (ios CalendarSharingViews.swift,
 * CalendarListSheet).
 */
@Composable
fun CalendarListScreen(model: CalendarModel) {
    val navigator = LocalAppNavigator.current
    val connection = model.connection
    val scope = rememberCoroutineScope()
    var circles by remember { mutableStateOf<List<CalendarCircle>>(emptyList()) }
    var isWorking by remember { mutableStateOf(false) }
    var isRenaming by remember { mutableStateOf(false) }
    var newName by remember { mutableStateOf("") }
    var renamingCircleCalendar by remember { mutableStateOf<AgentCalendar?>(null) }
    var newCircleCalendarName by remember { mutableStateOf("") }
    var confirmingDelete by remember { mutableStateOf<AgentCalendar?>(null) }
    val saidRenamed = stringResource(R.string.calendar_renamed)
    val saidDeleted = stringResource(R.string.calendar_deleted)
    val saidMade = stringResource(R.string.calendar_made)
    val failedText = stringResource(R.string.calendar_did_not_work)

    suspend fun findCircles() {
        circles = try {
            connection.calendarCircles()
        } catch (failure: kotlinx.coroutines.CancellationException) {
            throw failure
        } catch (_: Exception) {
            emptyList()
        }
    }
    LaunchedEffect(Unit) {
        if (model.calendars.isEmpty()) model.load()
        findCircles()
    }
    val adminCircleIds = circles.filter { circle -> circle.isAdmin }.map { circle -> circle.id }.toSet()
    val withCalendar = model.calendars.mapNotNull { calendar -> calendar.circleId }.toSet()
    val circlesWithoutCalendar = circles.filter { circle -> circle.isAdmin && circle.id !in withCalendar }
    val managedCircleCalendars = model.calendars.filter { calendar -> calendar.isCircleCalendar == true && calendar.circleId in adminCircleIds }

    fun run(said: String, work: suspend () -> Unit) {
        isWorking = true
        scope.launch {
            try {
                work()
                model.snackbar?.show(said)
                model.reloadCalendars()
                findCircles()
            } catch (failure: kotlinx.coroutines.CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                model.snackbar?.showProblem(model.describeError(failure).ifEmpty { failedText })
            }
            isWorking = false
        }
    }

    TeaNodeScreen(title = stringResource(R.string.calendar_calendars), onBack = { navigator.back() }) { contentPadding ->
        LazyColumn(Modifier.fillMaxSize().padding(contentPadding)) {
            items(model.calendars, key = { calendar -> calendar.id }) { calendar ->
                CalendarListRow(model, calendar, isShown = calendar.id !in model.hiddenIds) { isOn -> scope.launch { model.setShown(calendar.id, isOn) } }
            }
            item {
                Text(
                    stringResource(R.string.calendar_hidden_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(16.dp),
                )
            }
            val own = model.calendar
            if (own != null) {
                item {
                    TeaNodeRow(
                        headline = stringResource(R.string.calendar_rename_my_calendar), leadingIcon = Icons.Outlined.Edit,
                        onClick = if (isWorking) null else ({ newName = own.name; isRenaming = true }),
                    )
                }
                item { TeaNodeRow(headline = stringResource(R.string.calendar_share_my_calendar), leadingIcon = Icons.Outlined.People, onClick = { navigator.navigate(CalendarRoutes.SHARE) }) }
                if (own.ownerUserId != null) {
                    item { TeaNodeRow(headline = stringResource(R.string.calendar_find_a_time), leadingIcon = Icons.Outlined.Schedule, onClick = { navigator.navigate(CalendarRoutes.FIND_A_TIME) }) }
                }
                item {
                    Text(
                        stringResource(R.string.calendar_share_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(16.dp),
                    )
                }
            }
            if (managedCircleCalendars.isNotEmpty()) {
                items(managedCircleCalendars, key = { calendar -> "managed-${calendar.id}" }) { calendar ->
                    val circleName = calendar.circleName ?: calendar.name
                    TeaNodeRow(
                        headline = stringResource(R.string.calendar_rename_calendar_of, circleName), leadingIcon = Icons.Outlined.Edit,
                        onClick = if (isWorking) null else ({ newCircleCalendarName = calendar.name; renamingCircleCalendar = calendar }),
                    )
                    TeaNodeRow(
                        headline = stringResource(R.string.calendar_delete_calendar_of, circleName), leadingIcon = Icons.Outlined.Delete,
                        onClick = if (isWorking) null else ({ confirmingDelete = calendar }),
                    )
                }
                item {
                    Text(
                        stringResource(R.string.calendar_admin_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(16.dp),
                    )
                }
            }
            if (circlesWithoutCalendar.isNotEmpty()) {
                items(circlesWithoutCalendar, key = { circle -> "make-${circle.id}" }) { circle ->
                    TeaNodeRow(
                        headline = stringResource(if (circle.isFamily) R.string.calendar_make_family_calendar else R.string.calendar_make_shared_calendar, circle.name),
                        leadingIcon = Icons.Outlined.AddBox,
                        onClick = if (isWorking) null else ({ run(saidMade) { connection.createCircleCalendar(circle.id) } }),
                    )
                }
                item {
                    Text(
                        stringResource(R.string.calendar_make_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(16.dp),
                    )
                }
            }
        }
    }
    if (isRenaming) {
        val own = model.calendar
        NameDialog(stringResource(R.string.calendar_rename_your_calendar), newName, { text -> newName = text }, onDismiss = { isRenaming = false }) {
            isRenaming = false
            val name = newName.trim()
            // Nothing is sent for an empty name or the name it already has.
            if (own != null && name.isNotEmpty() && name != own.name) run(saidRenamed) { connection.saveCalendar(own, name) }
        }
    }
    renamingCircleCalendar?.let { calendar ->
        NameDialog(stringResource(R.string.calendar_rename_the_calendar), newCircleCalendarName, { text -> newCircleCalendarName = text }, onDismiss = { renamingCircleCalendar = null }) {
            renamingCircleCalendar = null
            val name = newCircleCalendarName.trim()
            if (name.isNotEmpty() && name != calendar.name) run(saidRenamed) { connection.saveCalendar(calendar, name) }
        }
    }
    confirmingDelete?.let { calendar ->
        val circleName = calendar.circleName ?: calendar.name
        ConfirmationDialog(
            title = stringResource(R.string.calendar_delete_calendar_of_question, circleName),
            text = stringResource(R.string.calendar_delete_calendar_message),
            confirmLabel = stringResource(R.string.calendar_delete_the_calendar),
            isDestructive = true,
            onConfirm = {
                confirmingDelete = null
                val circleId = calendar.circleId
                if (circleId != null) run(saidDeleted) { connection.deleteCircleCalendar(circleId) }
            },
            onDismiss = { confirmingDelete = null },
        )
    }
}

@Composable
private fun NameDialog(title: String, name: String, onName: (String) -> Unit, onDismiss: () -> Unit, onSave: () -> Unit) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = { OutlinedTextField(value = name, onValueChange = onName, label = { Text(stringResource(R.string.calendar_calendar_name)) }, singleLine = true) },
        confirmButton = { TextButton(onClick = onSave) { Text(stringResource(R.string.calendar_save)) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.calendar_cancel)) } },
    )
}

/** One calendar in the list: its color, the owner's picture when it is another person's, its name and how much of it the person may use. */
@Composable
fun CalendarListRow(model: CalendarModel, calendar: AgentCalendar, isShown: Boolean, onShown: (Boolean) -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable { onShown(!isShown) }.padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Box(Modifier.size(12.dp).background(model.color(calendar), CircleShape))
        if (!calendar.isOwn && calendar.isCircleCalendar != true) {
            AvatarView(calendar.ownerName.orEmpty(), calendar.ownerAvatarUrl, calendar.ownerAvatarColorIndex, size = 24.dp)
        }
        Column(Modifier.weight(1f)) {
            Text(model.displayName(calendar), style = MaterialTheme.typography.bodyLarge)
            // What the person may do with it, for a calendar that is not their own.
            if (!calendar.isOwn) Text(calendar.level.title(), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        Switch(checked = isShown, onCheckedChange = onShown)
    }
}
