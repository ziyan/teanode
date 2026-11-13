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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.calendar.DateWords
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
import com.teanode.kit.goals.GoalMailCondition
import com.teanode.kit.goals.goal
import com.teanode.kit.goals.setGoalState
import com.teanode.kit.goals.tellGoal
import kotlinx.coroutines.launch

/**
 * GoalDetailScreen is one goal: what it is for, what it did, when it runs, telling it something, and marking it met,
 * dropping or reopening it (ios GoalsView.swift, GoalDetail).
 */
@Composable
fun GoalDetailScreen(conversationId: String) {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    val goal = rememberLoad(conversationId) { connection.goal(conversationId) ?: throw TeaNodeError.UnreadableAnswer }
    var told by remember { mutableStateOf("") }
    var isConfirmingDrop by remember { mutableStateOf(false) }
    val titleText = goal.value?.goalTitle.orEmpty()
    val saidTold = stringResource(R.string.goals_said_told)
    val saidMet = stringResource(R.string.goals_said_met)
    val saidDropped = stringResource(R.string.goals_said_dropped)
    val saidReopened = stringResource(R.string.goals_said_reopened)

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

    TeaNodeScreen(title = titleText, onBack = { navigator.back() }) { contentPadding ->
        LoadedContent(goal, modifier = Modifier.fillMaxSize().padding(contentPadding)) { found ->
            LazyColumn(Modifier.fillMaxSize()) {
                item {
                    Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Text(found.goalDescription, style = MaterialTheme.typography.bodyLarge)
                        if (found.goalStatus.isNotEmpty()) LabeledLine(stringResource(R.string.goals_now), found.goalStatus)
                        nextCheckInText(found)?.let { next -> LabeledLine(stringResource(R.string.goals_next_check_in), next) }
                    }
                }
                item {
                    TeaNodeRow(
                        headline = stringResource(R.string.goals_open_conversation),
                        leadingIcon = Icons.Outlined.ChatBubbleOutline,
                        onClick = { navigator.navigate(ChatHandoff.conversation(found.conversationId)) },
                    )
                }
                if (found.isOpen) {
                    item {
                        TeaNodeRow(
                            headline = stringResource(R.string.goals_use_schedule),
                            leadingIcon = Icons.Outlined.CalendarMonth,
                            onClick = { navigator.navigate(GoalRoutes.schedule(conversationId)) },
                        )
                    }
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
                val schedules = found.schedules.orEmpty().filterNotNull()
                if (schedules.isNotEmpty()) {
                    item { TeaNodeSectionHeader(stringResource(R.string.goals_schedules)) }
                    items(schedules, key = { schedule -> schedule.id }) { schedule ->
                        TeaNodeRow(
                            headline = schedule.name,
                            supporting = if (schedule.isTriggeredByMail) mailConditionsText(schedule.mailConditions.orEmpty()) else schedule.cron,
                            trailing = {
                                Text(
                                    if (schedule.enabled) ServerTime.instant(schedule.nextRunAt)?.let { moment -> DateWords.abbreviatedDateTime(moment) }.orEmpty()
                                    else stringResource(R.string.goals_schedule_off),
                                    style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                            },
                        )
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
                if (found.isOpen) {
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

/** When a schedule that runs on mail comes round, in a person's words: "When mail arrives: From contains news@school.example". */
@Composable
internal fun mailConditionsText(conditions: List<GoalMailCondition>): String {
    val texts = conditions.filter { condition -> !(condition.field == "any") }.map { condition -> mailConditionText(condition) }
    if (texts.isEmpty()) return stringResource(R.string.goals_mail_any)
    return stringResource(R.string.goals_mail_when, texts.joinToString(", "))
}

@Composable
private fun mailConditionText(condition: GoalMailCondition): String {
    val field = stringResource(
        when (condition.field) {
            "from" -> R.string.goals_mail_field_from
            "to" -> R.string.goals_mail_field_to
            "subject" -> R.string.goals_mail_field_subject
            "header" -> R.string.goals_mail_field_header
            "score" -> R.string.goals_mail_field_score
            "sender-known" -> R.string.goals_mail_field_sender_known
            "category" -> R.string.goals_mail_field_category
            "priority" -> R.string.goals_mail_field_priority
            "needs-reply" -> R.string.goals_mail_field_needs_reply
            else -> R.string.goals_mail_field_any
        },
    )
    val comparison = stringResource(
        when (condition.comparisonOperator) {
            "equals" -> R.string.goals_mail_comparison_equals
            "matches" -> R.string.goals_mail_comparison_matches
            "above" -> R.string.goals_mail_comparison_above
            "below" -> R.string.goals_mail_comparison_below
            else -> R.string.goals_mail_comparison_contains
        },
    )
    val header = condition.header?.takeIf { name -> name.isNotEmpty() && condition.field == "header" }
    return listOfNotNull(field, header, comparison, condition.value?.takeIf { text -> text.isNotEmpty() }).joinToString(" ")
}
