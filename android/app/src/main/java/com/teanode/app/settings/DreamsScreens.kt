package com.teanode.app.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.NightsStay
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.calendar.TimeField
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.agent.AgentDream
import com.teanode.kit.agent.AgentDreamState
import com.teanode.kit.agent.AgentUpkeepPreferences
import com.teanode.kit.agent.ReadingProgress
import com.teanode.kit.agent.ReadingSpan
import com.teanode.kit.agent.UpkeepClock
import com.teanode.kit.agent.agentDreamState
import com.teanode.kit.agent.agentDreams
import com.teanode.kit.agent.agentUpkeep
import com.teanode.kit.agent.dreamNow
import com.teanode.kit.agent.readPercent
import com.teanode.kit.agent.readingProgress
import com.teanode.kit.agent.rereadDocuments
import com.teanode.kit.agent.setDreamBootstrap
import com.teanode.kit.agent.timeLeft
import com.teanode.kit.agent.total
import com.teanode.kit.agent.updateAgentUpkeep
import com.teanode.kit.photos.ServerTime
import kotlinx.coroutines.delay
import java.time.LocalTime
import java.text.NumberFormat

/** Everything the Dreams page reads at once. */
private class DreamsRead(val preferences: AgentUpkeepPreferences?, val dreams: List<AgentDream>, val dreamState: AgentDreamState?, val progress: ReadingProgress?)

private fun clockTime(text: String): LocalTime {
    val parts = UpkeepClock.hourAndMinute(text) ?: return LocalTime.MIDNIGHT
    return LocalTime.of(parts.first, parts.second)
}

@Composable
private fun readingSpanText(span: ReadingSpan): String = when (span) {
    is ReadingSpan.Minutes -> stringResource(R.string.settings_dreams_span_minutes, span.minutes)
    is ReadingSpan.Hours -> stringResource(R.string.settings_dreams_span_hours, span.hours)
    is ReadingSpan.Days -> stringResource(R.string.settings_dreams_span_days, span.days)
}

@Composable
private fun progressText(progress: ReadingProgress): String {
    if (progress.total <= 0) return stringResource(R.string.settings_dreams_nothing_indexed)
    val number = NumberFormat.getIntegerInstance()
    val parts = mutableListOf(stringResource(R.string.settings_dreams_read_of_total, number.format(progress.read), number.format(progress.total)))
    progress.timeLeft?.let { span ->
        var left = stringResource(R.string.settings_dreams_time_left, readingSpanText(span), Math.round(progress.perHour).toInt())
        if (!progress.bootstrapping) left += " " + stringResource(R.string.settings_dreams_without_pause)
        parts += left
    }
    if (progress.costLeft > 0) parts += stringResource(R.string.settings_dreams_cost_left, moneyText(progress.costLeft, progress.currency))
    return parts.joinToString(" · ")
}

/**
 * The Dreams page: whether the agent is bootstrapping and how much is read, the hours a dream may start in, and the
 * dreams it had, each opening the runs it made; dream now, and put back what a dream marked read.
 */
@Composable
fun DreamsScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val loaded = com.teanode.app.ui.components.rememberLoad {
        DreamsRead(
            preferences = connection.agentUpkeep(),
            dreams = runCatching { connection.agentDreams() }.getOrDefault(emptyList()),
            dreamState = runCatching { connection.agentDreamState() }.getOrNull(),
            progress = runCatching { connection.readingProgress() }.getOrNull(),
        )
    }
    var isAskingReread by remember { mutableStateOf(false) }
    var openedDream by remember { mutableStateOf<Pair<String, String>?>(null) }
    val snackbar = com.teanode.app.ui.components.LocalSnackbar.current
    var rereadMinutes by remember { mutableStateOf("60") }
    val savedText = stringResource(R.string.settings_saved)
    val startedText = stringResource(R.string.settings_dreams_started_said)
    val bootstrapOnText = stringResource(R.string.settings_dreams_bootstrap_on_said)
    val bootstrapOffText = stringResource(R.string.settings_dreams_bootstrap_off_said)
    val context = LocalContext.current

    // A dream asked for waits for a worker, then runs: the list is read again as that changes, so the new row appears and a finished one gets its numbers.
    LaunchedEffect(Unit) {
        var lastStatus = loaded.value?.dreamState?.dreamJobStatus.orEmpty()
        while (true) {
            delay(10_000)
            val read = runCatching { connection.agentDreamState() }.getOrNull() ?: continue
            if (read.dreamJobStatus != lastStatus) {
                lastStatus = read.dreamJobStatus
                loaded.refresh()
            }
        }
    }

    openedDream?.let { (jobId, startedAt) ->
        AgentActivityScreen(onBack = { openedDream = null }, dreamJobId = jobId, dreamStartedAt = startedAt)
        return
    }
    val preferences = loaded.value?.preferences
    val dreamState = loaded.value?.dreamState
    LoadedSettingsPage(
        stringResource(R.string.settings_dreams_title), onBack, loaded,
        actions = {
            if (preferences != null) {
                BarTextAction(
                    stringResource(
                        when {
                            dreamState?.isQueued == true -> R.string.settings_dreams_state_queued
                            dreamState?.isRunning == true -> R.string.settings_dreams_state_dreaming
                            else -> R.string.settings_dreams_dream_now
                        },
                    ),
                    onClick = { actions.run(startedText, onSuccess = { loaded.refresh() }) { connection.dreamNow() } },
                    isEnabled = dreamState?.isIdle != false && !actions.isWorking, isWorking = false,
                )
                BarTextAction(stringResource(R.string.settings_dreams_reread_menu), onClick = { rereadMinutes = "60"; isAskingReread = true }, isEnabled = !actions.isWorking)
            }
        },
    ) { read ->
        val shownPreferences = read.preferences
        if (shownPreferences == null) {
            item { EmptyState(Icons.Outlined.NightsStay, stringResource(R.string.settings_alerts_off_title), description = stringResource(R.string.settings_alerts_off_detail)) }
            return@LoadedSettingsPage
        }
        val isBootstrapping = shownPreferences.dreamBootstrap == true
        item {
            Row(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(stringResource(R.string.settings_dreams_bootstrap_header), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
                if (isBootstrapping) UpkeepTag(stringResource(R.string.settings_dreams_bootstrapping), TagTone.Good)
            }
        }
        item {
            val progress = read.progress
            Column(Modifier.padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                if (progress != null) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        LinearProgressIndicator(progress = { progress.readPercent / 100f }, modifier = Modifier.weight(1f))
                        Text("${progress.readPercent}%", style = MaterialTheme.typography.bodySmall)
                    }
                    Text(progressText(progress), style = MaterialTheme.typography.bodySmall)
                    if (progress.spent > 0 && progress.dreams > 0) {
                        val spentText = moneyText(progress.spent, progress.currency)
                        Text(
                            if (progress.dreams == 1) stringResource(R.string.settings_dreams_measured_one, spentText) else stringResource(R.string.settings_dreams_measured_many, progress.dreams, spentText),
                            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    Text(
                        if (progress.waiting > 0) stringResource(R.string.settings_dreams_waiting_many, NumberFormat.getIntegerInstance().format(progress.waiting)) else stringResource(R.string.settings_dreams_waiting_none),
                        style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                } else {
                    CircularProgressIndicator(strokeWidth = 2.dp)
                }
            }
        }
        item {
            TeaNodeRow(
                headline = stringResource(if (isBootstrapping) R.string.settings_dreams_stop else R.string.settings_dreams_bootstrap_action),
                onClick = if (actions.isWorking) null else ({ actions.run(if (isBootstrapping) bootstrapOffText else bootstrapOnText, onSuccess = { loaded.refresh() }) { connection.setDreamBootstrap(!isBootstrapping) } }),
            )
        }
        item { TeaNodeSectionHeader(stringResource(R.string.settings_dreams_when_header)) }
        item {
            TimeField(stringResource(R.string.settings_dreams_earliest), clockTime(shownPreferences.shownDreamFrom), { time -> actions.run(savedText, onSuccess = { loaded.refresh() }) { connection.updateAgentUpkeep(dreamFrom = UpkeepClock.text(time.hour, time.minute)) } })
            TimeField(stringResource(R.string.settings_dreams_latest), clockTime(shownPreferences.shownDreamUntil), { time -> actions.run(savedText, onSuccess = { loaded.refresh() }) { connection.updateAgentUpkeep(dreamUntil = UpkeepClock.text(time.hour, time.minute)) } })
        }
        item { FooterText(stringResource(R.string.settings_dreams_hours_footer)) }
        item { TeaNodeSectionHeader(stringResource(R.string.settings_dreams_list_header)) }
        val state = read.dreamState
        if (state?.isQueued == true && state.queuedAt != null) {
            item {
                Column(Modifier.padding(horizontal = 16.dp, vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    UpkeepTag(stringResource(R.string.settings_dreams_state_queued), TagTone.Warning)
                    Text(stringResource(R.string.settings_dreams_asked_for, whenText(state.queuedAt)), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
        }
        if (read.dreams.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.settings_dreams_none_yet)) }
        items(read.dreams.size) { index ->
            val dream = read.dreams[index]
            DreamRow(dream, onClick = dream.jobId?.takeIf { jobId -> jobId.isNotEmpty() }?.let { jobId -> { openedDream = jobId to dream.startedAt } })
        }
        item { FooterText(stringResource(R.string.settings_dreams_list_footer)) }
    }
    if (isAskingReread) {
        val minutes = rereadMinutes.toIntOrNull() ?: 0
        AlertDialog(
            onDismissRequest = { isAskingReread = false },
            title = { Text(stringResource(R.string.settings_dreams_reread_title)) },
            text = {
                Column {
                    Text(stringResource(R.string.settings_dreams_reread_message), style = MaterialTheme.typography.bodyMedium)
                    FormField(rereadMinutes, { text -> rereadMinutes = text.filter { character -> character.isDigit() } }, stringResource(R.string.settings_dreams_reread_hint), keyboardType = KeyboardType.Number)
                }
            },
            confirmButton = {
                TextButton(
                    enabled = minutes > 0,
                    onClick = {
                        isAskingReread = false
                        actions.run(onSuccess = { loaded.refresh() }) {
                            val putBack = connection.rereadDocuments(minutes)
                            snackbar.show(context.getString(if (putBack == 1) R.string.settings_dreams_reread_one else R.string.settings_dreams_reread_many, putBack))
                        }
                    },
                ) { Text(stringResource(R.string.settings_dreams_reread_confirm)) }
            },
            dismissButton = { TextButton(onClick = { isAskingReread = false }) { Text(stringResource(R.string.common_cancel)) } },
        )
    }
}

/** One dream: when it started and how long it took, what matters most (what it read, filed and cost), then the rest of what it did and what was still waiting. */
@Composable
private fun DreamRow(dream: AgentDream, onClick: (() -> Unit)?) {
    val headlineKinds = setOf(AgentDream.OutcomeKind.Read, AgentDream.OutcomeKind.Filed)
    val headline = dream.outcomes.filter { outcome -> outcome.outcomeKind in headlineKinds }.map { outcome -> dreamOutcomeText(outcome) }.toMutableList()
    if (dream.cost > 0) headline += stringResource(R.string.settings_dreams_cost, moneyText(dream.cost, dream.currency))
    if (dream.outcomes.isEmpty() && dream.finishedAt != null) headline.add(0, stringResource(if (dream.lastError.isNullOrEmpty()) R.string.settings_dreams_nothing_needed else R.string.settings_dreams_cut_short))
    val more = dream.outcomes.filter { outcome -> outcome.outcomeKind !in headlineKinds }.map { outcome -> dreamOutcomeText(outcome) }.toMutableList()
    if (dream.backlog > 0) more += stringResource(R.string.settings_dreams_backlog, NumberFormat.getIntegerInstance().format(dream.backlog))
    val lines = listOfNotNull(headline.joinToString(" · ").ifEmpty { null }, dream.lastError?.ifEmpty { null }, more.joinToString(" · ").ifEmpty { null })
    TeaNodeRow(
        headline = whenText(dream.startedAt), supporting = lines.joinToString("\n").ifEmpty { null }, supportingMaxLines = 10, onClick = onClick,
        trailing = {
            val minutes = dream.minutesTaken
            if (minutes != null) Text(stringResource(R.string.settings_dreams_minutes_short, minutes), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            else UpkeepTag(stringResource(R.string.settings_dreams_still_working), TagTone.Good)
        },
    )
}
