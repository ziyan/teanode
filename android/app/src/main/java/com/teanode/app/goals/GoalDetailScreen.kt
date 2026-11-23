package com.teanode.app.goals

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CalendarMonth
import androidx.compose.material.icons.outlined.CheckCircleOutline
import androidx.compose.material.icons.outlined.ChatBubbleOutline
import androidx.compose.material.icons.outlined.HighlightOff
import androidx.compose.material.icons.outlined.Replay
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.PauseCircleOutline
import androidx.compose.material.icons.outlined.PlayArrow
import androidx.compose.material.icons.outlined.PlayCircleOutline
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.settings.FormDialog
import com.teanode.app.settings.FormField
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeDivider
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.relativeTimeText
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.TeaNodeError
import com.teanode.kit.calendar.ServerTime
import com.teanode.kit.goals.GoalChange
import com.teanode.kit.goals.runGoal
import com.teanode.kit.goals.updateGoal
import com.teanode.kit.goals.goal
import com.teanode.kit.goals.setGoalState
import com.teanode.kit.goals.tellGoal
import kotlinx.coroutines.launch

/**
 * GoalDetailScreen is one goal: what it is for, what it did, when it runs (with the editor), telling it something, and
 * running it now, pausing, finishing, dropping or reopening it (ios GoalsView.swift, GoalDetail).
 */
@Composable
fun GoalDetailScreen(conversationId: String) {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    val resources = LocalContext.current.resources
    val goal = rememberLoad(conversationId) { connection.goal(conversationId) ?: throw TeaNodeError.UnreadableAnswer }
    var told by remember { mutableStateOf("") }
    var isConfirmingDrop by remember { mutableStateOf(false) }
    var isEditingWhen by remember { mutableStateOf(false) }
    var isEditingText by remember { mutableStateOf(false) }
    var isSaving by remember { mutableStateOf(false) }
    var whenDraft by remember { mutableStateOf(GoalWhenDraft()) }
    var titleDraft by remember { mutableStateOf("") }
    var descriptionDraft by remember { mutableStateOf("") }
    val titleText = goal.value?.goalTitle.orEmpty()
    val saidTold = stringResource(R.string.goals_said_told)
    val saidMet = stringResource(R.string.goals_said_met)
    val saidDropped = stringResource(R.string.goals_said_dropped)
    val saidReopened = stringResource(R.string.goals_said_reopened)
    val saidPaused = stringResource(R.string.goals_said_paused)
    val saidResumed = stringResource(R.string.goals_said_resumed)
    val saidRun = stringResource(R.string.goals_said_run)
    val saidSaved = stringResource(R.string.goals_said_saved)

    fun setState(goalState: String, said: String) {
        scope.launch {
            try {
                connection.setGoalState(conversationId, goalState)
                snackbar.show(said)
                goal.refresh()
            } catch (failure: Exception) {
                snackbar.showError(failure)
            }
        }
    }

    fun save(change: GoalChange) {
        isSaving = true
        scope.launch {
            try {
                connection.updateGoal(conversationId, change)
                snackbar.show(saidSaved)
                isEditingWhen = false
                isEditingText = false
                goal.refresh()
            } catch (failure: Exception) {
                snackbar.showError(failure)
            }
            isSaving = false
        }
    }

    TeaNodeScreen(
        title = titleText,
        onBack = { navigator.back() },
        actions = {
            val found = goal.value
            if (found != null && (found.isOpen || found.isPaused)) {
                IconButton(onClick = { titleDraft = found.goalTitle; descriptionDraft = found.goalDescription; isEditingText = true }) {
                    Icon(Icons.Outlined.Edit, contentDescription = stringResource(R.string.goals_edit))
                }
            }
        },
    ) { contentPadding ->
        LoadedContent(goal, modifier = Modifier.fillMaxSize().padding(contentPadding)) { found ->
            LazyColumn(Modifier.fillMaxSize()) {
                item {
                    Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Text(found.goalDescription, style = MaterialTheme.typography.bodyLarge)
                        if (found.goalStatus.isNotEmpty()) LabeledLine(stringResource(R.string.goals_now), found.goalStatus)
                        nextTimeText(found)?.let { next -> LabeledLine(stringResource(R.string.goals_next_check_in), next) }
                    }
                }
                item {
                    TeaNodeRow(
                        headline = stringResource(R.string.goals_open_conversation),
                        leadingIcon = Icons.Outlined.ChatBubbleOutline,
                        onClick = { ChatHandoff.open(navigator, found.conversationId) },
                    )
                }
                item { TeaNodeSectionHeader(stringResource(R.string.goals_when)) }
                val whenLines = GoalWords.whenLines(resources, found)
                item {
                    TeaNodeRow(
                        headline = if (whenLines.isEmpty()) stringResource(R.string.goals_when_own_pace_words) else whenLines.joinToString("\n"),
                        supporting = if (found.goalTimes.isNotEmpty() && found.isGoalNotifiedWhenPosted) stringResource(R.string.goals_notified_when_posted) else null,
                        leadingIcon = Icons.Outlined.CalendarMonth,
                    )
                }
                if (found.isOpen || found.isPaused) {
                    item {
                        TeaNodeRow(
                            headline = stringResource(R.string.goals_edit_when),
                            leadingIcon = Icons.Outlined.Edit,
                            onClick = { whenDraft = GoalWhenDraft.of(found); isEditingWhen = true },
                        )
                    }
                }
                if (found.isOpen) {
                    item {
                        Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
                            OutlinedTextField(
                                value = told,
                                onValueChange = { text -> told = text },
                                label = { Text(stringResource(R.string.goals_tell_hint)) },
                                modifier = Modifier.fillMaxWidth(),
                                minLines = 2,
                            )
                            Text(
                                stringResource(R.string.goals_tell_footer),
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                modifier = Modifier.padding(top = 4.dp),
                            )
                            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                                Button(
                                    enabled = told.isNotBlank(),
                                    onClick = {
                                        val text = told.trim()
                                        scope.launch {
                                            try {
                                                connection.tellGoal(conversationId, text)
                                                told = ""
                                                snackbar.show(saidTold)
                                                goal.refresh()
                                            } catch (failure: Exception) {
                                                snackbar.showError(failure)
                                            }
                                        }
                                    },
                                ) { Text(stringResource(R.string.goals_send)) }
                            }
                        }
                    }
                }
                val activity = found.activity.orEmpty().filterNotNull()
                if (activity.isNotEmpty()) {
                    item { TeaNodeSectionHeader(stringResource(R.string.goals_what_it_did)) }
                    items(activity, key = { entry -> entry.id }) { entry ->
                        TeaNodeRow(
                            headline = entry.activityHeadline,
                            supporting = listOf(
                                entry.activityDetail,
                                ServerTime.instant(entry.createdAt)?.let { moment -> relativeTimeText(moment.toEpochMilli()) }.orEmpty(),
                            ).filter { line -> line.isNotEmpty() }.joinToString("\n"),
                            supportingMaxLines = 5,
                        )
                    }
                }
                item { TeaNodeDivider(Modifier.padding(top = 8.dp)) }
                if (found.goalState == "working") {
                    item {
                        TeaNodeRow(
                            headline = stringResource(R.string.goals_run_now),
                            leadingIcon = Icons.Outlined.PlayArrow,
                            onClick = {
                                scope.launch {
                                    try {
                                        connection.runGoal(conversationId)
                                        snackbar.show(saidRun)
                                        goal.refresh()
                                    } catch (failure: Exception) {
                                        snackbar.showError(failure)
                                    }
                                }
                            },
                        )
                    }
                }
                if (found.isOpen) {
                    item {
                        TeaNodeRow(
                            headline = stringResource(R.string.goals_pause),
                            leadingIcon = Icons.Outlined.PauseCircleOutline,
                            onClick = { setState("paused", saidPaused) },
                        )
                    }
                }
                if (found.isPaused) {
                    item {
                        TeaNodeRow(
                            headline = stringResource(R.string.goals_resume),
                            leadingIcon = Icons.Outlined.PlayCircleOutline,
                            onClick = { setState("working", saidResumed) },
                        )
                    }
                }
                if (found.isOpen || found.isPaused) {
                    item {
                        TeaNodeRow(
                            headline = stringResource(R.string.goals_mark_met),
                            leadingIcon = Icons.Outlined.CheckCircleOutline,
                            onClick = { setState("met", saidMet) },
                        )
                    }
                    item {
                        TeaNodeRow(
                            headline = stringResource(R.string.goals_drop),
                            leadingIcon = Icons.Outlined.HighlightOff,
                            onClick = { isConfirmingDrop = true },
                        )
                    }
                } else {
                    item {
                        TeaNodeRow(
                            headline = stringResource(R.string.goals_reopen),
                            leadingIcon = Icons.Outlined.Replay,
                            onClick = { setState("working", saidReopened) },
                        )
                    }
                }
            }
        }
    }
    if (isEditingWhen) {
        FormDialog(
            title = stringResource(R.string.goals_when),
            confirmLabel = stringResource(R.string.goals_save),
            isConfirmEnabled = whenDraft.isComplete,
            isWorking = isSaving,
            onConfirm = { save(whenDraft.change()) },
            onDismiss = { isEditingWhen = false },
        ) {
            Column(Modifier.verticalScroll(rememberScrollState())) { GoalWhenEditor(whenDraft) { changed -> whenDraft = changed } }
        }
    }
    if (isEditingText) {
        FormDialog(
            title = stringResource(R.string.goals_edit),
            confirmLabel = stringResource(R.string.goals_save),
            isConfirmEnabled = titleDraft.isNotBlank() && descriptionDraft.isNotBlank(),
            isWorking = isSaving,
            onConfirm = { save(GoalChange(goalTitle = titleDraft.trim(), goalDescription = descriptionDraft.trim())) },
            onDismiss = { isEditingText = false },
        ) {
            Column(Modifier.verticalScroll(rememberScrollState())) {
                FormField(titleDraft, { text -> titleDraft = text }, stringResource(R.string.goals_starter_title_hint))
                FormField(descriptionDraft, { text -> descriptionDraft = text }, stringResource(R.string.goals_starter_details_hint), isSingleLine = false, minLines = 4)
            }
        }
    }
    if (isConfirmingDrop) {
        ConfirmationDialog(
            title = stringResource(R.string.goals_drop_title),
            text = stringResource(R.string.goals_drop_message),
            confirmLabel = stringResource(R.string.goals_drop),
            isDestructive = true,
            onConfirm = {
                isConfirmingDrop = false
                setState("dropped", saidDropped)
            },
            onDismiss = { isConfirmingDrop = false },
        )
    }
}

@Composable
private fun LabeledLine(label: String, value: String) {
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
        Text(label, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(value, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.padding(start = 16.dp))
    }
}
