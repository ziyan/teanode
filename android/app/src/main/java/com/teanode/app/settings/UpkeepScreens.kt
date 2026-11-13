package com.teanode.app.settings

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Notifications
import androidx.compose.material.icons.outlined.NotificationsOff
import androidx.compose.material.icons.outlined.PlayArrow
import androidx.compose.material.icons.outlined.Stop
import androidx.compose.material.icons.outlined.NightsStay
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.relativeTimeText
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.agent.AgentAlert
import com.teanode.kit.agent.AgentBackgroundWork
import com.teanode.kit.agent.AgentDream
import com.teanode.kit.agent.AgentSchedule
import com.teanode.kit.agent.AgentUpkeepPreferences
import com.teanode.kit.agent.UpkeepClock
import com.teanode.kit.agent.agentAlerts
import com.teanode.kit.agent.agentCorrections
import com.teanode.kit.agent.agentDreamState
import com.teanode.kit.agent.agentDreams
import com.teanode.kit.agent.agentRuns
import com.teanode.kit.agent.agentSchedules
import com.teanode.kit.agent.agentUpkeep
import com.teanode.kit.agent.backgroundWork
import com.teanode.kit.agent.connectedServers
import com.teanode.kit.agent.deleteAgentSchedule
import com.teanode.kit.agent.disconnectServer
import com.teanode.kit.agent.dreamNow
import com.teanode.kit.agent.gradeMemoryNow
import com.teanode.kit.agent.learnedFacts
import com.teanode.kit.agent.knowledgeSourceDetails
import com.teanode.kit.agent.syncKnowledgeSource
import com.teanode.kit.agent.switchKnowledgeSource
import com.teanode.kit.agent.deleteKnowledgeSource
import com.teanode.kit.agent.memoryCheckQuestions
import com.teanode.kit.agent.muteAgentAlert
import com.teanode.kit.agent.readingProgress
import com.teanode.kit.agent.runAgentSchedule
import com.teanode.kit.agent.saveAgentSchedule
import com.teanode.kit.agent.startMemoryCheck
import com.teanode.kit.agent.stopBackgroundWork
import com.teanode.kit.agent.unmuteAgentAlert
import com.teanode.kit.agent.updateAgentUpkeep
import com.teanode.kit.photos.ServerTime

@Composable
private fun dreamOutcomeText(outcome: AgentDream.Outcome): String = stringResource(
    when (outcome.outcomeKind) {
        AgentDream.OutcomeKind.Read -> R.string.settings_dreams_read
        AgentDream.OutcomeKind.Filed -> R.string.settings_dreams_filed
        AgentDream.OutcomeKind.Rewritten -> R.string.settings_dreams_rewritten
        AgentDream.OutcomeKind.Merged -> R.string.settings_dreams_merged
        AgentDream.OutcomeKind.Moved -> R.string.settings_dreams_moved
        AgentDream.OutcomeKind.Retired -> R.string.settings_dreams_retired
        AgentDream.OutcomeKind.Embedded -> R.string.settings_dreams_embedded
        AgentDream.OutcomeKind.Strengthened -> R.string.settings_dreams_strengthened
        AgentDream.OutcomeKind.Associated -> R.string.settings_dreams_associated
        AgentDream.OutcomeKind.Rehearsed -> R.string.settings_dreams_rehearsed
        AgentDream.OutcomeKind.Gaps -> R.string.settings_dreams_gaps
        AgentDream.OutcomeKind.Unknown -> R.string.settings_dreams_unknown
        AgentDream.OutcomeKind.Revised -> R.string.settings_dreams_revised
        AgentDream.OutcomeKind.OverviewsWritten -> R.string.settings_dreams_overviews
        AgentDream.OutcomeKind.ThemesMade -> R.string.settings_dreams_themes_made
        AgentDream.OutcomeKind.ThemesUpdated -> R.string.settings_dreams_themes_updated
        AgentDream.OutcomeKind.ReflectionsWritten -> R.string.settings_dreams_reflections
    },
    outcome.outcomeCount,
)

/** A time of day typed as HH:MM, with the way it is wrong said when it is. */
@Composable
private fun ClockField(label: String, text: String, onChange: (String) -> Unit) {
    FormField(text, onChange, label, placeholder = "HH:MM", isError = UpkeepClock.hourAndMinute(text) == null, keyboardType = KeyboardType.Number)
}

/** The nights the agent dreams: when, what each did, and the way to dream now. */
@Composable
fun DreamsScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad { Triple(connection.agentDreams(), connection.agentDreamState(), connection.agentUpkeep()) }
    val progress = rememberLoad { connection.readingProgress() }
    var isEditingHours by remember { mutableStateOf(false) }
    val startedText = stringResource(R.string.settings_dreams_started)
    val savedText = stringResource(R.string.settings_saved)
    LoadedSettingsPage(stringResource(R.string.settings_dreams_title), onBack, loaded) { (dreams, state, preferences) ->
        item {
            val stateText = when {
                state.isRunning -> stringResource(R.string.settings_dreams_running)
                state.isQueued -> stringResource(R.string.settings_dreams_queued)
                else -> stringResource(R.string.settings_dreams_idle)
            }
            TeaNodeRow(headline = stringResource(R.string.settings_dreams_now), supporting = stateText, leadingIcon = Icons.Outlined.PlayArrow, onClick = if (state.isIdle && !actions.isWorking) ({ actions.run(startedText, onSuccess = { loaded.refresh() }) { connection.dreamNow() } }) else null)
            if (preferences != null) {
                TeaNodeRow(headline = stringResource(R.string.settings_dreams_hours), supporting = "${preferences.shownDreamFrom} – ${preferences.shownDreamUntil}", leadingIcon = Icons.Outlined.NightsStay, onClick = { isEditingHours = true })
            }
            progress.value?.let { reading ->
                TeaNodeRow(headline = stringResource(R.string.settings_dreams_reading), supporting = stringResource(R.string.settings_dreams_reading_detail, reading.read, reading.waiting), supportingMaxLines = 3)
            }
            TeaNodeSectionHeader(stringResource(R.string.settings_dreams_title))
        }
        if (dreams.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.settings_dreams_none)) }
        items(dreams.size) { index ->
            val dream = dreams[index]
            val whenText = ServerTime.instant(dream.startedAt)?.let { instant -> shortDateTime(instant) }.orEmpty()
            val lines = buildList {
                add(dream.outcomes.map { outcome -> dreamOutcomeText(outcome) }.joinToString(", ").ifEmpty { stringResource(R.string.settings_dreams_nothing_done) })
                dream.minutesTaken?.let { minutes -> add(stringResource(R.string.settings_dreams_minutes, minutes)) }
                if (dream.cost > 0) add("%.2f %s".format(dream.cost, dream.currency.orEmpty()).trim())
                dream.lastError?.takeIf { text -> text.isNotEmpty() }?.let { text -> add(text) }
            }
            TeaNodeRow(headline = whenText, supporting = lines.joinToString("\n"), supportingMaxLines = 14)
        }
        item { FooterText(stringResource(R.string.settings_dreams_footer)) }
    }
    val preferences = loaded.value?.third
    if (isEditingHours && preferences != null) {
        var from by remember { mutableStateOf(preferences.shownDreamFrom) }
        var until by remember { mutableStateOf(preferences.shownDreamUntil) }
        FormDialog(
            title = stringResource(R.string.settings_dreams_hours), confirmLabel = stringResource(R.string.settings_save),
            isConfirmEnabled = UpkeepClock.hourAndMinute(from) != null && UpkeepClock.hourAndMinute(until) != null, isWorking = actions.isWorking,
            onConfirm = { actions.run(savedText, onSuccess = { isEditingHours = false; loaded.refresh() }) { connection.updateAgentUpkeep(dreamFrom = from, dreamUntil = until) } },
            onDismiss = { isEditingHours = false },
        ) {
            ClockField(stringResource(R.string.settings_dreams_from), from) { from = it }
            ClockField(stringResource(R.string.settings_dreams_until), until) { until = it }
        }
    }
}

/** What the agent told the person unasked, the ways to quiet it, and what is muted. */
@Composable
fun MutedAlertsScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad { connection.agentAlerts() to connection.agentUpkeep() }
    var muting by remember { mutableStateOf<AgentAlert?>(null) }
    var isEditing by remember { mutableStateOf(false) }
    val mutedText = stringResource(R.string.settings_alerts_muted)
    val unmutedText = stringResource(R.string.settings_alerts_unmuted)
    val savedText = stringResource(R.string.settings_saved)
    LoadedSettingsPage(stringResource(R.string.settings_alerts_title), onBack, loaded) { (alerts, preferences) ->
        if (preferences != null) {
            item {
                SwitchRow(stringResource(R.string.settings_alerts_on), preferences.isAlertsEnabled, { isOn -> actions.run(savedText, onSuccess = { loaded.refresh() }) { connection.updateAgentUpkeep(isAlertsEnabled = isOn) } })
                TeaNodeRow(
                    headline = stringResource(R.string.settings_alerts_quiet), supporting = stringResource(R.string.settings_alerts_quiet_detail, preferences.shownAlertQuietStart, preferences.shownAlertQuietEnd, preferences.shownAlertDailyMost),
                    supportingMaxLines = 3, onClick = { isEditing = true },
                )
            }
        }
        item { TeaNodeSectionHeader(stringResource(R.string.settings_alerts_sent)) }
        if (alerts.alerts.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.settings_alerts_none)) }
        items(alerts.alerts.size) { index ->
            val alert = alerts.alerts[index]
            val whenText = ServerTime.instant(alert.sentAt)?.let { instant -> relativeTimeText(instant.toEpochMilli()) }
            TeaNodeRow(
                headline = alert.alertText, overline = whenText, supporting = alert.covered.joinToString("\n") { mail -> "${mail.subject} · ${mail.fromAddress}" }.ifEmpty { null }, supportingMaxLines = 4,
                leadingIcon = if (alert.isUrgent) Icons.Outlined.Notifications else null,
                trailing = if (alert.muteChoices.isNotEmpty()) ({ IconButton(onClick = { muting = alert }) { Icon(Icons.Outlined.NotificationsOff, stringResource(R.string.settings_alerts_mute)) } }) else null,
            )
        }
        item { TeaNodeSectionHeader(stringResource(R.string.settings_alerts_mutes)) }
        if (alerts.mutes.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.settings_alerts_no_mutes)) }
        items(alerts.mutes.size) { index ->
            val mute = alerts.mutes[index]
            TeaNodeRow(
                headline = mute.muteTarget.ifEmpty { mute.muteScope }, supporting = mute.muteScope,
                trailing = { IconButton(onClick = { actions.run(unmutedText, onSuccess = { loaded.refresh() }) { connection.unmuteAgentAlert(mute.id) } }) { Icon(Icons.Outlined.Delete, stringResource(R.string.settings_alerts_unmute)) } },
            )
        }
    }
    muting?.let { alert ->
        FormDialog(title = stringResource(R.string.settings_alerts_mute), confirmLabel = stringResource(R.string.common_cancel), isConfirmEnabled = true, onConfirm = { muting = null }, onDismiss = { muting = null }) {
            alert.muteChoices.forEach { choice ->
                TeaNodeRow(headline = choice.muteTarget.ifEmpty { choice.muteScope }, supporting = choice.muteScope, onClick = { muting = null; actions.run(mutedText, onSuccess = { loaded.refresh() }) { connection.muteAgentAlert(alert.id, choice.muteScope) } })
            }
        }
    }
    val preferences = loaded.value?.second
    if (isEditing && preferences != null) {
        var start by remember { mutableStateOf(preferences.shownAlertQuietStart) }
        var end by remember { mutableStateOf(preferences.shownAlertQuietEnd) }
        var most by remember { mutableStateOf(preferences.shownAlertDailyMost.toString()) }
        FormDialog(
            title = stringResource(R.string.settings_alerts_quiet), confirmLabel = stringResource(R.string.settings_save),
            isConfirmEnabled = UpkeepClock.hourAndMinute(start) != null && UpkeepClock.hourAndMinute(end) != null && (most.toIntOrNull() ?: 0) > 0, isWorking = actions.isWorking,
            onConfirm = { actions.run(savedText, onSuccess = { isEditing = false; loaded.refresh() }) { connection.updateAgentUpkeep(alertQuietStart = start, alertQuietEnd = end, alertDailyMost = most.toInt()) } },
            onDismiss = { isEditing = false },
        ) {
            ClockField(stringResource(R.string.settings_alerts_quiet_from), start) { start = it }
            ClockField(stringResource(R.string.settings_alerts_quiet_until), end) { end = it }
            FormField(most, { most = it.filter { character -> character.isDigit() } }, stringResource(R.string.settings_alerts_daily_most), keyboardType = KeyboardType.Number)
        }
    }
}

/** What the agent does at a time: list, switch on and off, run now, delete, and make one. */
@Composable
fun SchedulesScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad { connection.agentSchedules() }
    var removing by remember { mutableStateOf<AgentSchedule?>(null) }
    var isAdding by remember { mutableStateOf(false) }
    val ranText = stringResource(R.string.settings_schedules_ran)
    val savedText = stringResource(R.string.settings_saved)
    val removedText = stringResource(R.string.settings_access_removed)
    LoadedSettingsPage(
        stringResource(R.string.settings_schedules_title), onBack, loaded,
        floatingActionButton = { FloatingActionButton(onClick = { isAdding = true }) { Icon(Icons.Outlined.Add, stringResource(R.string.settings_schedules_new)) } },
        isEmpty = { list -> list.isEmpty() }, empty = { EmptyState(Icons.Outlined.NightsStay, stringResource(R.string.settings_schedules_none), description = stringResource(R.string.settings_schedules_none_detail)) },
    ) { schedules ->
        items(schedules.size) { index ->
            val schedule = schedules[index]
            val next = ServerTime.instant(schedule.nextRunAt)?.let { instant -> stringResource(R.string.settings_schedules_next, shortDateTime(instant)) }
            val last = ServerTime.instant(schedule.lastRunAt)?.let { instant -> stringResource(R.string.settings_last_used, relativeTimeText(instant.toEpochMilli())) }
            TeaNodeRow(
                headline = schedule.name, supporting = listOfNotNull(if (schedule.isTriggeredByMail) stringResource(R.string.settings_schedules_on_mail) else schedule.cron, next, last, schedule.prompt.ifEmpty { null }).joinToString("\n"), supportingMaxLines = 5,
                trailing = {
                    androidx.compose.foundation.layout.Row(verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
                        IconButton(onClick = { actions.run(ranText) { connection.runAgentSchedule(schedule.id) } }) { Icon(Icons.Outlined.PlayArrow, stringResource(R.string.settings_schedules_run)) }
                        IconButton(onClick = { removing = schedule }) { Icon(Icons.Outlined.Delete, stringResource(R.string.settings_remove)) }
                        androidx.compose.material3.Switch(checked = schedule.enabled, onCheckedChange = { isOn -> actions.run(savedText, onSuccess = { loaded.refresh() }) { connection.saveAgentSchedule(scheduleId = schedule.id, isEnabled = isOn) } })
                    }
                },
            )
        }
    }
    removing?.let { schedule ->
        ConfirmationDialog(
            title = stringResource(R.string.settings_schedules_remove, schedule.name), confirmLabel = stringResource(R.string.settings_remove), isDestructive = true,
            onConfirm = { removing = null; actions.run(removedText, onSuccess = { loaded.refresh() }) { connection.deleteAgentSchedule(schedule.id) } }, onDismiss = { removing = null },
        )
    }
    if (isAdding) {
        var name by remember { mutableStateOf("") }
        var cron by remember { mutableStateOf("0 9 * * 1-5") }
        var prompt by remember { mutableStateOf("") }
        FormDialog(
            title = stringResource(R.string.settings_schedules_new), confirmLabel = stringResource(R.string.settings_create), isConfirmEnabled = name.isNotBlank() && cron.isNotBlank() && prompt.isNotBlank(), isWorking = actions.isWorking,
            onConfirm = { actions.run(savedText, onSuccess = { isAdding = false; loaded.refresh() }) { connection.saveAgentSchedule(name = name.trim(), cron = cron.trim(), prompt = prompt.trim()) } },
            onDismiss = { isAdding = false },
        ) {
            FormField(name, { name = it }, stringResource(R.string.settings_profile_name))
            FormField(cron, { cron = it }, stringResource(R.string.settings_schedules_cron))
            FormField(prompt, { prompt = it }, stringResource(R.string.settings_schedules_prompt), isSingleLine = false, minLines = 3)
        }
    }
}

/** What the agent has learned and been corrected on, and the questions that check it. */
@Composable
fun AgentMemorySettingsScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad { Triple(connection.learnedFacts(), connection.agentCorrections(), connection.memoryCheckQuestions()) }
    val startedText = stringResource(R.string.settings_memory_check_started)
    val gradedText = stringResource(R.string.settings_memory_graded)
    LoadedSettingsPage(stringResource(R.string.settings_memory_title), onBack, loaded) { (facts, corrections, questions) ->
        item {
            TeaNodeRow(headline = stringResource(R.string.settings_memory_check), leadingIcon = Icons.Outlined.PlayArrow, onClick = if (actions.isWorking) null else ({ actions.run(startedText) { connection.startMemoryCheck() } }))
            TeaNodeRow(headline = stringResource(R.string.settings_memory_grade), leadingIcon = Icons.Outlined.PlayArrow, onClick = if (actions.isWorking) null else ({ actions.run(gradedText) { connection.gradeMemoryNow() } }))
            TeaNodeSectionHeader(stringResource(R.string.settings_memory_learned))
        }
        if (facts.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.settings_memory_nothing_learned)) }
        items(facts.size) { index ->
            val learned = facts[index]
            TeaNodeRow(headline = learned.fact.text, supporting = learned.name.ifEmpty { learned.path }.ifEmpty { null }, supportingMaxLines = 2)
        }
        item { TeaNodeSectionHeader(stringResource(R.string.settings_memory_corrections)) }
        if (corrections.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.settings_memory_no_corrections)) }
        items(corrections.size) { index ->
            val correction = corrections[index]
            TeaNodeRow(headline = correction.said, supporting = correction.kind.ifEmpty { null })
        }
        item { TeaNodeSectionHeader(stringResource(R.string.settings_memory_questions)) }
        if (questions.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.settings_memory_no_questions)) }
        items(questions.size) { index ->
            val question = questions[index]
            TeaNodeRow(headline = question.questionText, supporting = stringResource(R.string.settings_memory_expected, question.expectedAnswer), supportingMaxLines = 3)
        }
        item { FooterText(stringResource(R.string.settings_memory_footer)) }
    }
}

/** The servers the agent is connected to, and which of them answered. */
@Composable
fun ConnectionsScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad { connection.connectedServers() }
    var disconnecting by remember { mutableStateOf<com.teanode.kit.agent.AgentConnectedServer?>(null) }
    val disconnectedText = stringResource(R.string.settings_apps_disconnected)
    LoadedSettingsPage(
        stringResource(R.string.settings_connections_title), onBack, loaded, isEmpty = { list -> list.isEmpty() },
        empty = { EmptyState(Icons.Outlined.NightsStay, stringResource(R.string.settings_connections_none), description = stringResource(R.string.settings_connections_none_detail)) },
    ) { servers ->
        items(servers.size) { index ->
            val server = servers[index]
            TeaNodeRow(
                headline = server.name,
                supporting = listOfNotNull(
                    stringResource(if (server.isConnected) R.string.settings_connections_connected else R.string.settings_connections_not_connected),
                    stringResource(R.string.settings_connections_tools, server.tools), server.lastError?.ifEmpty { null },
                ).joinToString(" · "),
                supportingMaxLines = 4,
                trailing = if (server.isPersonal && server.isConnected) ({ IconButton(onClick = { disconnecting = server }) { Icon(Icons.Outlined.Delete, stringResource(R.string.settings_apps_disconnect)) } }) else null,
            )
        }
        item { FooterText(stringResource(R.string.settings_connections_footer)) }
    }
    disconnecting?.let { server ->
        ConfirmationDialog(
            title = stringResource(R.string.settings_connections_disconnect, server.name), confirmLabel = stringResource(R.string.settings_apps_disconnect), isDestructive = true,
            onConfirm = { disconnecting = null; actions.run(disconnectedText, onSuccess = { loaded.refresh() }) { connection.disconnectServer(server.name) } }, onDismiss = { disconnecting = null },
        )
    }
}

/** What the agent is doing in the background and what it did lately. */
@Composable
fun AgentActivityScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad { connection.backgroundWork() to connection.agentRuns() }
    val stoppedText = stringResource(R.string.settings_activity_stopped)
    LoadedSettingsPage(stringResource(R.string.settings_activity_title), onBack, loaded) { (work, runs) ->
        item { TeaNodeSectionHeader(stringResource(R.string.settings_activity_background)) }
        if (work.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.settings_activity_no_background)) }
        items(work.size) { index ->
            val item: AgentBackgroundWork = work[index]
            TeaNodeRow(
                headline = item.title.ifEmpty { item.workKind }, supporting = listOfNotNull(item.workStatus, item.errorMessage.ifEmpty { null }, item.resultText?.ifEmpty { null }).joinToString("\n"), supportingMaxLines = 4,
                trailing = if (item.isStoppable) ({ IconButton(onClick = { actions.run(stoppedText, onSuccess = { loaded.refresh() }) { connection.stopBackgroundWork(item.id) } }) { Icon(Icons.Outlined.Stop, stringResource(R.string.settings_activity_stop)) } }) else null,
            )
        }
        item { TeaNodeSectionHeader(stringResource(R.string.settings_activity_runs, runs.total)) }
        items(runs.runs.size) { index ->
            val run = runs.runs[index]
            val whenText = ServerTime.instant(run.lastAt)?.let { instant -> relativeTimeText(instant.toEpochMilli()) }
            TeaNodeRow(headline = run.title.ifEmpty { run.jobKind.orEmpty() }, overline = whenText, supporting = listOfNotNull(run.jobKind, stringResource(R.string.settings_activity_tokens, run.tokenCount), if (run.usage.cost > 0) "%.4f".format(run.usage.cost) else null).joinToString(" · "))
        }
    }
}

/** The sources the agent reads, with what each found: switch, read again now, and forget. */
@Composable
fun KnowledgeSourcesScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad { connection.knowledgeSourceDetails() }
    var removing by remember { mutableStateOf<com.teanode.kit.agent.KnowledgeSourceRow?>(null) }
    val syncedText = stringResource(R.string.settings_sources_syncing)
    val savedText = stringResource(R.string.settings_saved)
    val removedText = stringResource(R.string.settings_access_removed)
    LoadedSettingsPage(
        stringResource(R.string.settings_sources_title), onBack, loaded, isEmpty = { details -> details.sources.isEmpty() },
        empty = { EmptyState(Icons.Outlined.NightsStay, stringResource(R.string.settings_sources_none), description = stringResource(R.string.settings_sources_none_detail)) },
    ) { details ->
        items(details.sources.size) { index ->
            val source = details.sources[index]
            val waiting = details.attachments.firstOrNull { attachment -> attachment.sourceId == source.id }
            val lines = listOfNotNull(
                stringResource(R.string.settings_sources_counts, source.documentCount, source.chunkCount), source.lastRunAt?.let { stamp -> ServerTime.instant(stamp)?.let { instant -> stringResource(R.string.settings_last_used, relativeTimeText(instant.toEpochMilli())) } },
                if (source.refusedCount > 0) stringResource(R.string.settings_sources_refused, source.refusedCount) else null,
                waiting?.takeIf { attachment -> attachment.undecided > 0 }?.let { attachment -> stringResource(R.string.settings_sources_undecided, attachment.undecided) },
                source.lastError.ifEmpty { null },
            )
            TeaNodeRow(
                headline = source.name, overline = source.kind.ifEmpty { null }, supporting = lines.joinToString("\n"), supportingMaxLines = 5,
                trailing = {
                    androidx.compose.foundation.layout.Row(verticalAlignment = androidx.compose.ui.Alignment.CenterVertically) {
                        IconButton(onClick = { actions.run(syncedText) { connection.syncKnowledgeSource(source.id) } }) { Icon(Icons.Outlined.PlayArrow, stringResource(R.string.settings_sources_sync)) }
                        IconButton(onClick = { removing = source }) { Icon(Icons.Outlined.Delete, stringResource(R.string.settings_remove)) }
                        androidx.compose.material3.Switch(checked = source.enabled, onCheckedChange = { isOn -> actions.run(savedText, onSuccess = { loaded.refresh() }) { connection.switchKnowledgeSource(source.id, isOn) } })
                    }
                },
            )
        }
        item { FooterText(stringResource(R.string.settings_sources_footer)) }
    }
    removing?.let { source ->
        ConfirmationDialog(
            title = stringResource(R.string.settings_sources_remove, source.name), confirmLabel = stringResource(R.string.settings_remove), isDestructive = true, text = stringResource(R.string.settings_sources_remove_detail),
            onConfirm = { removing = null; actions.run(removedText, onSuccess = { loaded.refresh() }) { connection.deleteKnowledgeSource(source.id) } }, onDismiss = { removing = null },
        )
    }
}
