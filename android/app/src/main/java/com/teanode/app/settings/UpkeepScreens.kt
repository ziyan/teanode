package com.teanode.app.settings

import androidx.compose.foundation.layout.Column
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.compose.foundation.layout.padding
import com.teanode.kit.agent.runDailyBrief
import com.teanode.kit.agent.setDailyBrief
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
import com.teanode.kit.agent.attachedComputers
import com.teanode.kit.agent.knowledgeSourceSecrets
import com.teanode.kit.agent.knowledgeSourceTypes
import com.teanode.kit.agent.saveKnowledgeSource
import com.teanode.kit.agent.setKnowledgeSourceSecret
import com.teanode.kit.agent.memoryCheckRuns
import com.teanode.kit.agent.memoryCheckAnswers
import com.teanode.kit.agent.updateMemoryCheckQuestion
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
    var editing by remember { mutableStateOf<AgentSchedule?>(null) }
    val ranText = stringResource(R.string.settings_schedules_ran)
    val savedText = stringResource(R.string.settings_saved)
    val removedText = stringResource(R.string.settings_access_removed)
    LoadedSettingsPage(
        stringResource(R.string.settings_schedules_title), onBack, loaded,
        floatingActionButton = { FloatingActionButton(onClick = { isAdding = true }) { Icon(Icons.Outlined.Add, stringResource(R.string.settings_schedules_new)) } },
    ) { schedules ->
        item { DailyBriefCard(schedules, actions, onChanged = { loaded.refresh() }) }
        if (schedules.none { schedule -> schedule.name != com.teanode.kit.agent.DailyBrief.SCHEDULE_NAME }) item { TeaNodeRow(headline = stringResource(R.string.settings_schedules_none)) }
        items(schedules.filter { schedule -> schedule.name != com.teanode.kit.agent.DailyBrief.SCHEDULE_NAME }.size) { index ->
            val schedule = schedules.filter { candidate -> candidate.name != com.teanode.kit.agent.DailyBrief.SCHEDULE_NAME }[index]
            val next = ServerTime.instant(schedule.nextRunAt)?.let { instant -> stringResource(R.string.settings_schedules_next, shortDateTime(instant)) }
            val last = ServerTime.instant(schedule.lastRunAt)?.let { instant -> stringResource(R.string.settings_last_used, relativeTimeText(instant.toEpochMilli())) }
            TeaNodeRow(
                onClick = { editing = schedule },
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
    if (isAdding || editing != null) {
        ScheduleEditorDialog(editing, actions, onDismiss = { isAdding = false; editing = null }, onSaved = { isAdding = false; editing = null; loaded.refresh() })
    }
}

/** The days of a weekly timing as chips, Monday first. */
@OptIn(androidx.compose.foundation.layout.ExperimentalLayoutApi::class)
@Composable
fun WeekdayChips(dayNumbers: List<Int>, onChange: (List<Int>) -> Unit) {
    androidx.compose.foundation.layout.FlowRow(androidx.compose.ui.Modifier.padding(horizontal = 16.dp)) {
        (1..7).forEach { day ->
            androidx.compose.material3.FilterChip(
                selected = dayNumbers.contains(day), modifier = androidx.compose.ui.Modifier.padding(end = 6.dp),
                onClick = { onChange(if (dayNumbers.contains(day)) dayNumbers - day else (dayNumbers + day).sorted()) },
                label = { Text(java.time.DayOfWeek.of(day).getDisplayName(java.time.format.TextStyle.SHORT, java.util.Locale.getDefault())) },
            )
        }
    }
}

/** Makes or changes a schedule: when (a time on some days, or when mail arrives), what it does, and where the answer goes. */
@Composable
private fun ScheduleEditorDialog(edited: AgentSchedule?, actions: ActionRunner, onDismiss: () -> Unit, onSaved: () -> Unit) {
    val connection = LocalConnection.current
    val savedText = stringResource(R.string.settings_saved)
    var name by remember(edited?.id) { mutableStateOf(edited?.name.orEmpty()) }
    var prompt by remember(edited?.id) { mutableStateOf(edited?.prompt.orEmpty()) }
    var isByMail by remember(edited?.id) { mutableStateOf(edited?.isDeliveredByMail == true) }
    var isOnMail by remember(edited?.id) { mutableStateOf(edited?.isTriggeredByMail == true) }
    val timing = remember(edited?.id) { edited?.cron?.let { cron -> com.teanode.kit.agent.ScheduleTiming.of(cron) } }
    var hasTiming by remember(edited?.id) { mutableStateOf(edited == null || timing != null || edited.isTriggeredByMail) }
    var time by remember(edited?.id) { mutableStateOf(timing?.let { found -> UpkeepClock.text(found.hour, found.minute) } ?: "08:00") }
    var days by remember(edited?.id) { mutableStateOf(timing?.dayNumbers ?: (1..5).toList()) }
    var rawCron by remember(edited?.id) { mutableStateOf(edited?.cron.orEmpty()) }
    var conditions by remember(edited?.id) { mutableStateOf(edited?.mailConditions ?: listOf(com.teanode.kit.agent.ScheduleMailCondition("any", value = null))) }
    val usesRawCron = edited != null && timing == null && !edited.isTriggeredByMail
    val clock = UpkeepClock.hourAndMinute(time)
    val cron = if (usesRawCron) rawCron.trim() else clock?.let { (hour, minute) -> com.teanode.kit.agent.ScheduleTiming(hour, minute, days).cron }.orEmpty()
    val isValid = name.isNotBlank() && prompt.isNotBlank() && days.isNotEmpty() && (if (isOnMail) com.teanode.kit.agent.ScheduleMailTrigger.isComplete(conditions) else cron.isNotEmpty())
    FormDialog(
        title = stringResource(if (edited == null) R.string.settings_schedules_new else R.string.settings_schedules_edit), confirmLabel = stringResource(R.string.settings_save), isConfirmEnabled = isValid, isWorking = actions.isWorking,
        onConfirm = {
            actions.run(savedText, onSuccess = onSaved) {
                connection.saveAgentSchedule(
                    scheduleId = edited?.id, name = name.trim(), cron = if (isOnMail) edited?.cron?.ifEmpty { null } ?: "0 0 * * *" else cron, prompt = prompt.trim(), deliver = if (isByMail) "mail" else "drawer",
                    mailConditions = if (isOnMail) conditions else if (edited?.isTriggeredByMail == true) emptyList() else null,
                )
            }
        },
        onDismiss = onDismiss,
    ) {
        Column(Modifier.verticalScroll(rememberScrollState())) {
            FormField(name, { name = it }, stringResource(R.string.settings_profile_name))
            ChoiceRow(
                stringResource(R.string.settings_schedules_when), listOf(false, true), isOnMail,
                label = { onMail -> stringResource(if (onMail) R.string.settings_schedules_on_mail else R.string.settings_schedules_at_a_time) }, onSelect = { isOnMail = it },
            )
            if (isOnMail) {
                conditions.forEachIndexed { index, condition ->
                    ChoiceRow(
                        stringResource(R.string.settings_schedules_field), com.teanode.kit.agent.ScheduleMailTrigger.fields, condition.mailField, label = { field -> mailFieldName(field) },
                        onSelect = { field -> conditions = conditions.toMutableList().also { list -> list[index] = condition.copy(mailField = field, value = if (field == "any") null else condition.value ?: "") } },
                    )
                    if (condition.mailField == "header") FormField(condition.header.orEmpty(), { typed -> conditions = conditions.toMutableList().also { list -> list[index] = condition.copy(header = typed) } }, stringResource(R.string.settings_schedules_header))
                    if (!condition.isBare) {
                        ChoiceRow(
                            stringResource(R.string.settings_schedules_compare), listOf("contains", "equals", "matches"), condition.comparisonOperator.ifEmpty { "contains" },
                            label = { operator -> stringResource(when (operator) { "equals" -> R.string.settings_schedules_is; "matches" -> R.string.settings_schedules_matches; else -> R.string.settings_schedules_contains }) },
                            onSelect = { operator -> conditions = conditions.toMutableList().also { list -> list[index] = condition.copy(comparisonOperator = operator) } },
                        )
                        FormField(condition.value.orEmpty(), { typed -> conditions = conditions.toMutableList().also { list -> list[index] = condition.copy(value = typed) } }, stringResource(R.string.settings_schedules_value))
                    }
                    if (conditions.size > 1) TeaNodeRow(headline = stringResource(R.string.settings_schedules_remove_condition), leadingIcon = Icons.Outlined.Delete, onClick = { conditions = conditions.toMutableList().also { list -> list.removeAt(index) } })
                }
                TeaNodeRow(headline = stringResource(R.string.settings_schedules_add_condition), leadingIcon = Icons.Outlined.Add, onClick = { conditions = conditions + com.teanode.kit.agent.ScheduleMailCondition("from") })
                FooterText(stringResource(R.string.settings_schedules_mail_footer))
            } else if (usesRawCron) {
                FormField(rawCron, { rawCron = it }, stringResource(R.string.settings_schedules_cron))
            } else {
                ClockField(stringResource(R.string.settings_schedules_time), time) { time = it }
                WeekdayChips(days) { days = it }
            }
            ChoiceRow(
                stringResource(R.string.settings_schedules_deliver), listOf(false, true), isByMail,
                label = { byMail -> stringResource(if (byMail) R.string.settings_schedules_by_mail else R.string.settings_schedules_into_conversation) }, onSelect = { isByMail = it },
            )
            FormField(prompt, { prompt = it }, stringResource(R.string.settings_schedules_prompt), isSingleLine = false, minLines = 3)
            FooterText(stringResource(R.string.settings_schedules_footer))
        }
    }
}

@Composable
private fun mailFieldName(field: String): String = stringResource(
    when (field) {
        "from" -> R.string.settings_schedules_field_from
        "to" -> R.string.settings_schedules_field_to
        "subject" -> R.string.settings_schedules_field_subject
        "header" -> R.string.settings_schedules_field_header
        else -> R.string.settings_schedules_field_any
    },
)

/** The morning brief: switch it on, choose when, and send one now. */
@Composable
private fun DailyBriefCard(schedules: List<AgentSchedule>, actions: ActionRunner, onChanged: () -> Unit) {
    val connection = LocalConnection.current
    val brief = com.teanode.kit.agent.DailyBrief.schedule(schedules)
    var time by remember(brief?.cron) { mutableStateOf(com.teanode.kit.agent.DailyBrief.time(brief?.cron)) }
    var days by remember(brief?.cron) { mutableStateOf(com.teanode.kit.agent.DailyBrief.dayNumbers(brief?.cron)) }
    val onText = stringResource(R.string.settings_brief_on)
    val offText = stringResource(R.string.settings_brief_off)
    val sentText = stringResource(R.string.settings_brief_sent)
    TeaNodeSectionHeader(stringResource(R.string.settings_brief_title))
    SwitchRow(stringResource(R.string.settings_brief_title), brief?.enabled == true, { isOn -> actions.run(if (isOn) onText else offText, onSuccess = onChanged) { connection.setDailyBrief(isOn, time, days) } })
    ClockField(stringResource(R.string.settings_schedules_time), time) { time = it }
    WeekdayChips(days) { days = it }
    if (UpkeepClock.hourAndMinute(time) != null && days.isNotEmpty() && (time != com.teanode.kit.agent.DailyBrief.time(brief?.cron) || days != com.teanode.kit.agent.DailyBrief.dayNumbers(brief?.cron))) {
        TeaNodeRow(headline = stringResource(R.string.settings_save), onClick = { actions.run(onText, onSuccess = onChanged) { connection.setDailyBrief(brief?.enabled ?: true, time, days) } })
    }
    TeaNodeRow(headline = stringResource(R.string.settings_brief_send_now), leadingIcon = Icons.Outlined.PlayArrow, onClick = { actions.run(sentText) { connection.runDailyBrief() } })
    FooterText(stringResource(R.string.settings_brief_footer))
    TeaNodeSectionHeader(stringResource(R.string.settings_schedules_title))
}

/** What the agent has learned and been corrected on, the questions that check it, and how each grading went. */
@Composable
fun AgentMemorySettingsScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad { MemoryPage(connection.learnedFacts(), connection.agentCorrections(), connection.memoryCheckQuestions(), connection.memoryCheckRuns()) }
    var editing by remember { mutableStateOf<com.teanode.kit.agent.MemoryCheckQuestion?>(null) }
    var viewingRun by remember { mutableStateOf<com.teanode.kit.agent.MemoryCheckRun?>(null) }
    var isShowingDropped by remember { mutableStateOf(false) }
    val startedText = stringResource(R.string.settings_memory_check_started)
    val gradedText = stringResource(R.string.settings_memory_graded)
    val savedText = stringResource(R.string.settings_saved)
    LoadedSettingsPage(stringResource(R.string.settings_memory_title), onBack, loaded) { page ->
        item {
            TeaNodeRow(headline = stringResource(R.string.settings_memory_check), leadingIcon = Icons.Outlined.PlayArrow, onClick = if (actions.isWorking) null else ({ actions.run(startedText) { connection.startMemoryCheck() } }))
            TeaNodeRow(headline = stringResource(R.string.settings_memory_grade), leadingIcon = Icons.Outlined.PlayArrow, onClick = if (actions.isWorking) null else ({ actions.run(gradedText, onSuccess = { loaded.refresh() }) { connection.gradeMemoryNow() } }))
            TeaNodeSectionHeader(stringResource(R.string.settings_memory_scores))
        }
        com.teanode.kit.agent.MemoryCheckScore.answerSources.forEach { source ->
            val history = com.teanode.kit.agent.MemoryCheckRun.scoreHistory(page.runs, source)
            if (history.isNotEmpty()) item {
                val change = com.teanode.kit.agent.MemoryCheckRun.scoreChangePoints(history)
                TeaNodeRow(
                    headline = memoryAnswerSourceName(source), supporting = listOfNotNull("${Math.round(history.last().scorePercent)}%", change?.let { points -> stringResource(R.string.settings_memory_change, points) }, history.takeIf { points -> points.size > 1 }?.joinToString(" → ") { point -> Math.round(point.scorePercent).toString() }).joinToString(" · "), supportingMaxLines = 4,
                )
            }
        }
        item { TeaNodeSectionHeader(stringResource(R.string.settings_memory_runs)) }
        if (page.runs.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.settings_memory_no_runs)) }
        items(page.runs.size) { index ->
            val run = page.runs[index]
            val whenText = ServerTime.instant(run.startedAt)?.let { instant -> shortDateTime(instant) }.orEmpty()
            val scores = com.teanode.kit.agent.MemoryCheckScore.answerSources.mapNotNull { source -> run.score(source)?.let { score -> "${memoryAnswerSourceName(source)} ${Math.round(score.scorePercent)}%" } }
            TeaNodeRow(
                headline = whenText, supporting = listOfNotNull(stringResource(R.string.settings_memory_questions_count, run.questionCount), scores.joinToString(", ").ifEmpty { null }, if (run.isFinished) null else stringResource(R.string.settings_memory_grading)).joinToString("\n"),
                supportingMaxLines = 5, onClick = if (run.isFinished) ({ viewingRun = run }) else null,
            )
        }
        item { TeaNodeSectionHeader(stringResource(R.string.settings_memory_questions)) }
        item { SwitchRow(stringResource(R.string.settings_memory_show_dropped), isShowingDropped, { isShowingDropped = it }) }
        val questions = com.teanode.kit.agent.MemoryCheckQuestions.visible(page.questions, isShowingDropped)
        if (questions.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.settings_memory_no_questions)) }
        items(questions.size) { index ->
            val question = questions[index]
            TeaNodeRow(headline = question.questionText, supporting = stringResource(R.string.settings_memory_expected, question.expectedAnswer) + "\n" + question.questionState, supportingMaxLines = 4, onClick = { editing = question })
        }
        item { TeaNodeSectionHeader(stringResource(R.string.settings_memory_learned)) }
        if (page.facts.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.settings_memory_nothing_learned)) }
        items(page.facts.size) { index ->
            val learned = page.facts[index]
            TeaNodeRow(headline = learned.fact.text, supporting = learned.name.ifEmpty { learned.path }.ifEmpty { null }, supportingMaxLines = 2)
        }
        item { TeaNodeSectionHeader(stringResource(R.string.settings_memory_corrections)) }
        if (page.corrections.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.settings_memory_no_corrections)) }
        items(page.corrections.size) { index ->
            val correction = page.corrections[index]
            TeaNodeRow(headline = correction.said, supporting = correction.kind.ifEmpty { null })
        }
        item { FooterText(stringResource(R.string.settings_memory_footer)) }
    }
    editing?.let { question ->
        var draft by remember(question.id) { mutableStateOf(com.teanode.kit.agent.MemoryCheckQuestionChange(question.questionKind, question.questionText, question.expectedAnswer, question.outdatedAnswer.orEmpty(), question.questionState)) }
        val change = com.teanode.kit.agent.MemoryCheckQuestionChange.between(question, draft)
        FormDialog(
            title = stringResource(R.string.settings_memory_edit_question), confirmLabel = stringResource(R.string.settings_save), isConfirmEnabled = !change.isEmpty && draft.questionText.orEmpty().isNotBlank() && draft.expectedAnswer.orEmpty().isNotBlank(), isWorking = actions.isWorking,
            onConfirm = { actions.run(savedText, onSuccess = { editing = null; loaded.refresh() }) { connection.updateMemoryCheckQuestion(question.id, change) } },
            onDismiss = { editing = null },
        ) {
            Column(Modifier.verticalScroll(rememberScrollState())) {
                FormField(draft.questionText.orEmpty(), { draft = draft.copy(questionText = it) }, stringResource(R.string.settings_memory_question), isSingleLine = false, minLines = 2)
                FormField(draft.expectedAnswer.orEmpty(), { draft = draft.copy(expectedAnswer = it) }, stringResource(R.string.settings_memory_expected_answer), isSingleLine = false, minLines = 2)
                FormField(draft.outdatedAnswer.orEmpty(), { draft = draft.copy(outdatedAnswer = it) }, stringResource(R.string.settings_memory_outdated_answer))
                ChoiceRow(stringResource(R.string.settings_memory_kind), com.teanode.kit.agent.MemoryCheckQuestions.questionKinds, draft.questionKind.orEmpty(), label = { kind -> kind }, onSelect = { draft = draft.copy(questionKind = it) })
                ChoiceRow(stringResource(R.string.settings_memory_state), com.teanode.kit.agent.MemoryCheckQuestions.questionStates, draft.questionState.orEmpty(), label = { state -> state }, onSelect = { draft = draft.copy(questionState = it) })
            }
        }
    }
    viewingRun?.let { run -> MemoryRunDialog(run, loaded.value?.questions.orEmpty(), onDismiss = { viewingRun = null }) }
}

private class MemoryPage(
    val facts: List<com.teanode.kit.agent.AgentLearnedFact>, val corrections: List<com.teanode.kit.agent.AgentCorrection>,
    val questions: List<com.teanode.kit.agent.MemoryCheckQuestion>, val runs: List<com.teanode.kit.agent.MemoryCheckRun>,
)

@Composable
private fun memoryAnswerSourceName(source: String): String = stringResource(
    when (source) { "memory" -> R.string.settings_memory_from_memory; "sources" -> R.string.settings_memory_from_sources; else -> R.string.settings_memory_from_both },
)

/** What the agent answered to each question in one grading, from each place, and what the grader made of it. */
@Composable
private fun MemoryRunDialog(run: com.teanode.kit.agent.MemoryCheckRun, questions: List<com.teanode.kit.agent.MemoryCheckQuestion>, onDismiss: () -> Unit) {
    val connection = LocalConnection.current
    val loaded = rememberLoad(run.id) { connection.memoryCheckAnswers(run.id) }
    FormDialog(title = stringResource(R.string.settings_memory_run_answers), confirmLabel = stringResource(R.string.common_done), isConfirmEnabled = true, onConfirm = onDismiss, onDismiss = onDismiss) {
        Column(Modifier.verticalScroll(rememberScrollState())) {
            val answers = loaded.value
            if (answers == null) Text(stringResource(R.string.common_loading))
            answers?.sortedByDescending { answer -> answer.isMissedOrWrong }?.forEach { answer ->
                val question = questions.firstOrNull { candidate -> candidate.id == answer.questionId }
                TeaNodeRow(
                    headline = question?.questionText ?: answer.questionId,
                    supporting = listOfNotNull("${memoryAnswerSourceName(answer.answerFrom)}: ${answer.answerVerdict}", answer.answerText.ifEmpty { null }, answer.verdictReason.ifEmpty { null }).joinToString("\n"), supportingMaxLines = 8,
                )
            }
        }
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
    var isAddingSource by remember { mutableStateOf(false) }
    var editingSource by remember { mutableStateOf<com.teanode.kit.agent.KnowledgeSourceRow?>(null) }
    val syncedText = stringResource(R.string.settings_sources_syncing)
    val savedText = stringResource(R.string.settings_saved)
    val removedText = stringResource(R.string.settings_access_removed)
    LoadedSettingsPage(
        stringResource(R.string.settings_sources_title), onBack, loaded, isEmpty = { details -> details.sources.isEmpty() },
        floatingActionButton = { FloatingActionButton(onClick = { isAddingSource = true }) { Icon(Icons.Outlined.Add, stringResource(R.string.settings_sources_add)) } },
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
                onClick = { editingSource = source },
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
    if (isAddingSource || editingSource != null) {
        KnowledgeSourceEditorDialog(editingSource, actions, onDismiss = { isAddingSource = false; editingSource = null }, onSaved = { isAddingSource = false; editingSource = null; loaded.refresh() })
    }
    removing?.let { source ->
        ConfirmationDialog(
            title = stringResource(R.string.settings_sources_remove, source.name), confirmLabel = stringResource(R.string.settings_remove), isDestructive = true, text = stringResource(R.string.settings_sources_remove_detail),
            onConfirm = { removing = null; actions.run(removedText, onSuccess = { loaded.refresh() }) { connection.deleteKnowledgeSource(source.id) } }, onDismiss = { removing = null },
        )
    }
}

/** Makes or changes a source: its type, name, where it reads, the type's settings, and its secrets, which are sealed on the server and never shown again. */
@Composable
private fun KnowledgeSourceEditorDialog(edited: com.teanode.kit.agent.KnowledgeSourceRow?, actions: ActionRunner, onDismiss: () -> Unit, onSaved: () -> Unit) {
    val connection = LocalConnection.current
    val types = rememberLoad { connection.knowledgeSourceTypes() }
    val computers = rememberLoad { runCatching { connection.attachedComputers() }.getOrDefault(emptyList()) }
    val secretStates = rememberLoad(edited?.id) { edited?.id?.let { id -> runCatching { connection.knowledgeSourceSecrets(id) }.getOrDefault(emptyList()) } ?: emptyList() }
    val savedText = stringResource(R.string.settings_saved)
    val offered = com.teanode.kit.agent.KnowledgeSourceType.offered(types.value.orEmpty())
    val filesName = stringResource(R.string.settings_sources_files_type)
    var typeName by remember(edited?.id) { mutableStateOf(edited?.specification?.type?.ifEmpty { null } ?: if (edited != null) FILES_CHOICE else null) }
    val chosenType = offered.firstOrNull { type -> type.name == typeName }
    var name by remember(edited?.id) { mutableStateOf(edited?.name.orEmpty()) }
    var computer by remember(edited?.id) { mutableStateOf(edited?.specification?.computer.orEmpty()) }
    var path by remember(edited?.id) { mutableStateOf(edited?.specification?.path.orEmpty()) }
    var rootPath by remember(edited?.id) { mutableStateOf(edited?.rootPath.orEmpty()) }
    var cron by remember(edited?.id) { mutableStateOf(edited?.cron.orEmpty()) }
    var draft by remember(edited?.id, chosenType?.name) { mutableStateOf(chosenType?.draft(edited?.specification?.storedSettings.orEmpty()) ?: com.teanode.kit.agent.KnowledgeSourceType.Draft()) }
    var secretValues by remember(edited?.id, chosenType?.name) { mutableStateOf(mapOf<String, String>()) }
    val isFiles = typeName == FILES_CHOICE
    val isEditable = edited == null || edited.kind == "computer" || !edited.specification.type.isEmpty()
    val problem = chosenType?.firstProblem(draft)
    val isReady = isEditable && name.isNotBlank() && (isFiles && path.isNotBlank() && computer.isNotBlank() || chosenType != null && problem == null && (!chosenType.isReadOnComputer || computer.isNotBlank()))
    FormDialog(
        title = stringResource(if (edited == null) R.string.settings_sources_add else R.string.settings_sources_edit), confirmLabel = stringResource(R.string.settings_save), isConfirmEnabled = isReady, isWorking = actions.isWorking,
        onConfirm = {
            actions.run(savedText, onSuccess = onSaved) {
                val savedId = if (isFiles) {
                    connection.saveKnowledgeSource(com.teanode.kit.agent.KnowledgeSourceChange(sourceId = edited?.id, name = name.trim(), kind = "computer", computer = computer.trim(), path = path.trim(), format = "files", rootPath = rootPath.trim().ifEmpty { null }, cron = cron.trim().ifEmpty { null }))
                } else {
                    val type = chosenType ?: return@run
                    connection.saveKnowledgeSource(
                        com.teanode.kit.agent.KnowledgeSourceChange(
                            sourceId = edited?.id, name = name.trim(), computer = if (type.isReadOnComputer) computer.trim() else null, rootPath = rootPath.trim().ifEmpty { null }, cron = cron.trim().ifEmpty { null },
                            type = type.name, settings = type.values(draft),
                        ),
                    )
                }
                chosenType?.secrets?.forEach { secret ->
                    val value = secretValues[secret.key].orEmpty()
                    if (value.isNotEmpty()) connection.setKnowledgeSourceSecret(savedId, secret.key, value)
                }
            }
        },
        onDismiss = onDismiss,
    ) {
        Column(Modifier.verticalScroll(rememberScrollState())) {
            if (!isEditable) Text(stringResource(R.string.settings_sources_not_editable), style = MaterialTheme.typography.bodyMedium)
            if (edited == null) {
                ChoiceRow(
                    stringResource(R.string.settings_sources_type), listOf<String?>(null, FILES_CHOICE) + offered.filter { type -> type.reader != "files" }.map { type -> type.name }, typeName,
                    label = { choice -> when (choice) { null -> stringResource(R.string.settings_skills_choose); FILES_CHOICE -> filesName; else -> choice } }, onSelect = { typeName = it },
                )
            }
            chosenType?.description?.takeIf { text -> text.isNotEmpty() }?.let { text -> Text(com.teanode.kit.agent.KnowledgeSourceType.asSentence(text), style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(horizontal = 16.dp)) }
            if (typeName != null) {
                FormField(name, { name = it }, stringResource(R.string.settings_profile_name))
                if (isFiles || chosenType?.isReadOnComputer == true) {
                    val names = computers.value.orEmpty().map { attached -> attached.name }
                    ChoiceRow(stringResource(R.string.settings_sources_computer), listOf("") + names, computer, label = { choice -> choice.ifEmpty { stringResource(R.string.settings_skills_choose) } }, onSelect = { computer = it })
                }
                if (isFiles) FormField(path, { path = it }, stringResource(R.string.settings_sources_path))
                chosenType?.settings?.forEach { setting ->
                    val label = com.teanode.kit.agent.KnowledgeSourceType.label(setting.name)
                    if (setting.isBoolean) {
                        SwitchRow(label, draft.switches[setting.name] == true, { isOn -> draft = draft.copy(switches = draft.switches + (setting.name to isOn)) }, supporting = setting.description.ifEmpty { null }?.let { text -> com.teanode.kit.agent.KnowledgeSourceType.asSentence(text) })
                    } else if (setting.choices != null) {
                        ChoiceRow(label, listOf("") + setting.choices.orEmpty(), draft.texts[setting.name].orEmpty(), label = { choice -> choice.ifEmpty { stringResource(R.string.settings_sources_default) } }, onSelect = { chosen -> draft = draft.copy(texts = draft.texts + (setting.name to chosen)) })
                    } else {
                        FormField(
                            draft.texts[setting.name].orEmpty(), { typed -> draft = draft.copy(texts = draft.texts + (setting.name to typed)) }, label + if (setting.isRequired) " *" else "",
                            supporting = setting.description.ifEmpty { null }?.let { text -> com.teanode.kit.agent.KnowledgeSourceType.asSentence(text) }, isError = setting.problem(draft) != null,
                            keyboardType = if (setting.settingType == "integer") KeyboardType.Number else KeyboardType.Text,
                        )
                    }
                }
                chosenType?.secrets?.forEach { secret ->
                    val isKept = secretStates.value.orEmpty().any { state -> state.key == secret.key && state.isSet }
                    FormField(
                        secretValues[secret.key].orEmpty(), { typed -> secretValues = secretValues + (secret.key to typed) }, com.teanode.kit.agent.KnowledgeSourceType.label(secret.key), isPassword = true,
                        supporting = stringResource(if (isKept) R.string.settings_sources_secret_kept else R.string.settings_sources_secret_new) + (secret.description.ifEmpty { null }?.let { text -> " " + com.teanode.kit.agent.KnowledgeSourceType.asSentence(text) } ?: ""),
                    )
                }
                FormField(rootPath, { rootPath = it }, stringResource(R.string.settings_sources_root_path))
                FormField(cron, { cron = it }, stringResource(R.string.settings_schedules_cron))
                chosenType?.problem?.takeIf { text -> text.isNotEmpty() }?.let { text -> Text(text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(horizontal = 16.dp)) }
            }
        }
    }
}

private const val FILES_CHOICE = "__files__"
