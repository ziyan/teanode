package com.teanode.app.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Remove
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
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
import com.teanode.app.calendar.DateField
import com.teanode.app.calendar.DateWords
import com.teanode.app.calendar.TimeField
import com.teanode.app.finance.FinanceCategoryNames
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.relativeTimeText
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.agent.AgentAlert
import com.teanode.kit.agent.AgentAlertMute
import com.teanode.kit.agent.AgentFollowUp
import com.teanode.kit.agent.AgentUpkeepPreferences
import com.teanode.kit.agent.NewAgentFollowUp
import com.teanode.kit.agent.UpkeepClock
import com.teanode.kit.agent.addAgentFollowUp
import com.teanode.kit.agent.agentAlerts
import com.teanode.kit.agent.agentFollowUps
import com.teanode.kit.agent.agentUpkeep
import com.teanode.kit.agent.muteAgentAlert
import com.teanode.kit.agent.muteAgentAlertKind
import com.teanode.kit.agent.stopAgentFollowUp
import com.teanode.kit.agent.unmuteAgentAlert
import com.teanode.kit.agent.updateAgentUpkeep
import com.teanode.kit.finance.spendingCategories
import com.teanode.kit.photos.ServerTime
import java.time.DayOfWeek
import java.time.LocalDate
import java.time.LocalTime
import java.time.format.TextStyle
import java.util.Locale

/** Everything the Alerts page reads at once. */
private class AlertsRead(
    val preferences: AgentUpkeepPreferences?,
    val alerts: List<AgentAlert>,
    val mutes: List<AgentAlertMute>,
    val followUps: List<AgentFollowUp>,
    val categoryNames: Map<String, String>,
)

@Composable
private fun followUpKindName(kind: String, isSingular: Boolean = false): String = stringResource(
    when {
        kind == "bill" -> if (isSingular) R.string.settings_alerts_kind_bill else R.string.settings_alerts_kinds_bill
        kind == "parcel" -> if (isSingular) R.string.settings_alerts_kind_parcel else R.string.settings_alerts_kinds_parcel
        kind == "deadline" -> if (isSingular) R.string.settings_alerts_kind_deadline else R.string.settings_alerts_kinds_deadline
        kind == "event" -> if (isSingular) R.string.settings_alerts_kind_event else R.string.settings_alerts_kinds_event
        kind == "nearby" -> R.string.settings_alerts_kinds_nearby
        else -> R.string.settings_alerts_kinds_all
    },
)

/** The first letter in capitals, for a word that begins a line or a button; Chinese and Japanese have no case. */
private fun sentenceCase(text: String): String = text.replaceFirstChar { letter -> letter.titlecase(Locale.getDefault()) }

/** What a mute names, as a person reads it: a spending category by its name rather than its id, and budget alerts by what they are. */
@Composable
private fun targetLabel(muteScope: String, target: String, categoryNames: Map<String, String>): String {
    if (muteScope == "spendingCategory") return categoryNames[target] ?: stringResource(R.string.settings_alerts_deleted_category)
    if (muteScope == "kind" && target == "budget") return stringResource(R.string.settings_alerts_budget_kind)
    if (muteScope == "kind" && target in AgentFollowUp.muteKinds) return followUpKindName(target)
    return target
}

@Composable
private fun scopeLabel(muteScope: String, target: String, categoryNames: Map<String, String>): String {
    val shown = targetLabel(muteScope, target, categoryNames)
    return when (muteScope) {
        "subjectKey" -> stringResource(R.string.settings_alerts_scope_subject, shown)
        "sender" -> stringResource(R.string.settings_alerts_scope_sender, shown)
        "domain" -> stringResource(R.string.settings_alerts_scope_domain, shown)
        "kind" -> stringResource(R.string.settings_alerts_scope_kind, shown)
        "spendingCategory" -> stringResource(R.string.settings_alerts_scope_category, shown)
        else -> shown
    }
}

private fun weekdayName(weekday: String): String =
    runCatching { DayOfWeek.valueOf(weekday.uppercase()).getDisplayName(TextStyle.FULL, Locale.getDefault()) }.getOrDefault(weekday)

private fun clockTime(text: String): LocalTime {
    val parts = UpkeepClock.hourAndMinute(text) ?: return LocalTime.MIDNIGHT
    return LocalTime.of(parts.first, parts.second)
}

private fun clockText(time: LocalTime): String = UpkeepClock.text(time.hour, time.minute)

/** A second line for a follow-up: what kind it is, its day, then what the kind has (who is paid and how much, the carrier and number, the place). */
@Composable
private fun followUpLine(followUp: AgentFollowUp): String {
    val parts = mutableListOf(sentenceCase(followUpKindName(followUp.followUpKind, isSingular = true)))
    val day = followUp.dueDate
    parts += if (day != null) DateWords.abbreviatedDate(day) else stringResource(R.string.settings_alerts_no_day)
    followUp.payee?.takeIf { text -> text.isNotEmpty() }?.let { text -> parts += text }
    followUp.amount?.takeIf { text -> text.isNotEmpty() }?.let { text ->
        val number = text.toDoubleOrNull()
        val currency = followUp.currencyCode.orEmpty()
        parts += if (number != null && currency.isNotEmpty()) runCatching { java.text.NumberFormat.getCurrencyInstance().also { format -> format.currency = java.util.Currency.getInstance(currency) }.format(number) }.getOrDefault(text) else text
    }
    listOfNotNull(followUp.carrier, followUp.trackingNumber).filter { text -> text.isNotEmpty() }.joinToString(" ").takeIf { text -> text.isNotEmpty() }?.let { text -> parts += text }
    followUp.place?.takeIf { text -> text.isNotEmpty() }?.let { text -> parts += text }
    return parts.joinToString(" · ")
}

@Composable
private fun StepperRow(headline: String, number: Int, range: IntRange, onChange: (Int) -> Unit) {
    TeaNodeRow(
        headline = headline,
        trailing = {
            Row(verticalAlignment = Alignment.CenterVertically) {
                IconButton(onClick = { onChange(number - 1) }, enabled = number > range.first) { Icon(Icons.Outlined.Remove, null) }
                Text(number.toString(), style = MaterialTheme.typography.bodyLarge)
                IconButton(onClick = { onChange(number + 1) }, enabled = number < range.last) { Icon(Icons.Outlined.Add, null) }
            }
        },
    )
}

/**
 * The Alerts page: whether the agent tells the person unasked what their mail says they should know now, the quiet
 * hours, the most a day, and the day and time it looks around their town; what it told them lately, each muted in a
 * scope they choose; what it is following for them, each stopped and a kind of them muted; and what they muted.
 */
@Composable
fun MutedAlertsScreen(onBack: () -> Unit, focusesFollowing: Boolean = false) {
    val connection = LocalConnection.current
    val context = LocalContext.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad {
        val preferences = connection.agentUpkeep()
        if (preferences?.isRunning != true) {
            AlertsRead(preferences, emptyList(), emptyList(), emptyList(), emptyMap())
        } else {
            val alerts = connection.agentAlerts()
            val isCategoryNamed = (alerts.mutes.map { mute -> mute.muteScope } + alerts.alerts.flatMap { alert -> alert.muteChoices.map { choice -> choice.muteScope } }).contains("spendingCategory")
            val categoryNames = if (isCategoryNamed) runCatching { connection.spendingCategories() }.getOrDefault(emptyList()).associate { category -> category.id to FinanceCategoryNames.shown(context, category) } else emptyMap()
            AlertsRead(preferences, alerts.alerts, alerts.mutes, connection.agentFollowUps(), categoryNames)
        }
    }
    var muting by remember { mutableStateOf<AgentAlert?>(null) }
    var isMutingFollowUps by remember { mutableStateOf(false) }
    var isAddingFollowUp by remember { mutableStateOf(false) }
    val savedText = stringResource(R.string.settings_saved)
    val enabledText = stringResource(R.string.settings_alerts_enabled_said)
    val disabledText = stringResource(R.string.settings_alerts_disabled_said)
    val noSubjectText = stringResource(R.string.settings_alerts_no_subject)
    val isRunning = loaded.value?.preferences?.isRunning == true
    LoadedSettingsPage(
        stringResource(R.string.settings_alerts_title), onBack, loaded,
        floatingActionButton = { if (isRunning) FloatingActionButton(onClick = { isAddingFollowUp = true }) { Icon(Icons.Outlined.Add, stringResource(R.string.settings_alerts_add)) } },
    ) { read ->
        val preferences = read.preferences
        if (preferences == null) {
            item { EmptyState(Icons.Outlined.Add, stringResource(R.string.settings_alerts_off_title), description = stringResource(R.string.settings_alerts_off_detail)) }
            return@LoadedSettingsPage
        }
        settingsRows(preferences, actions, savedText, enabledText, disabledText, { loaded.refresh() }, connection)
        if (!preferences.isRunning) return@LoadedSettingsPage
        item { TeaNodeSectionHeader(stringResource(R.string.settings_alerts_recent_header)) }
        if (read.alerts.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.settings_alerts_recent_none)) }
        items(read.alerts.size) { index ->
            val alert = read.alerts[index]
            val whenText = ServerTime.instant(alert.sentAt)?.let { instant -> relativeTimeText(instant.toEpochMilli()) }
            val coveredText = alert.covered.joinToString("\n") { mail -> "${mail.subject.ifEmpty { noSubjectText }} · ${mail.fromAddress}" }
            TeaNodeRow(
                headline = alert.alertText,
                overline = listOfNotNull(whenText, if (alert.isUrgent) stringResource(R.string.settings_alerts_urgent) else null).joinToString(" · ").ifEmpty { null },
                supporting = coveredText.ifEmpty { null }, supportingMaxLines = 4,
                trailing = if (alert.muteChoices.isNotEmpty()) ({ TextButton(onClick = { muting = alert }) { Text(stringResource(R.string.settings_alerts_mute_action)) } }) else null,
            )
        }
        item { FooterText(stringResource(R.string.settings_alerts_recent_footer)) }
        item { TeaNodeSectionHeader(stringResource(R.string.settings_alerts_following_header)) }
        if (read.followUps.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.settings_alerts_following_none)) }
        items(read.followUps.size) { index ->
            val followUp = read.followUps[index]
            val stoppedText = stringResource(R.string.settings_alerts_stopped_said, followUp.followUpTitle)
            val pageUrl = followUp.sourcePageUrl
            TeaNodeRow(
                headline = followUp.followUpTitle,
                overline = when {
                    followUp.isOverdue -> stringResource(R.string.settings_alerts_overdue)
                    followUp.isStalled -> stringResource(R.string.settings_alerts_stalled)
                    else -> null
                },
                supporting = listOfNotNull(followUpLine(followUp), if (pageUrl != null) followUp.sourcePageHost else null).joinToString("\n"), supportingMaxLines = 4,
                onClick = if (pageUrl != null) ({ com.teanode.app.chats.openLinkAsChosen(context, pageUrl) }) else null,
                trailing = { TextButton(onClick = { actions.run(stoppedText, onSuccess = { loaded.refresh() }) { connection.stopAgentFollowUp(followUp.id) } }) { Text(stringResource(R.string.settings_alerts_stop)) } },
            )
        }
        item { TeaNodeRow(headline = stringResource(R.string.settings_alerts_mute_followups), onClick = { isMutingFollowUps = true }) }
        item { FooterText(stringResource(R.string.settings_alerts_following_footer)) }
        item { TeaNodeSectionHeader(stringResource(R.string.settings_alerts_muted_header)) }
        if (read.mutes.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.settings_alerts_muted_none)) }
        items(read.mutes.size) { index ->
            val mute = read.mutes[index]
            val unmutedText = stringResource(R.string.settings_alerts_unmuted_said, targetLabel(mute.muteScope, mute.muteTarget, read.categoryNames))
            TeaNodeRow(
                headline = scopeLabel(mute.muteScope, mute.muteTarget, read.categoryNames),
                supporting = stringResource(R.string.settings_alerts_since, ServerTime.instant(mute.createdAt)?.let { instant -> DateWords.abbreviatedDate(instant.atZone(java.time.ZoneId.systemDefault()).toLocalDate()) }.orEmpty()),
                trailing = { TextButton(onClick = { actions.run(unmutedText, onSuccess = { loaded.refresh() }) { connection.unmuteAgentAlert(mute.id) } }) { Text(stringResource(R.string.settings_alerts_unmute_action)) } },
            )
        }
        item { FooterText(stringResource(R.string.settings_alerts_muted_footer)) }
    }
    val categoryNames = loaded.value?.categoryNames.orEmpty()
    muting?.let { alert ->
        FormDialog(title = stringResource(R.string.settings_alerts_mute_title), confirmLabel = stringResource(R.string.common_cancel), isConfirmEnabled = true, onConfirm = { muting = null }, onDismiss = { muting = null }) {
            Text(alert.alertText, Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.bodyMedium)
            alert.muteChoices.forEach { choice ->
                val mutedText = stringResource(R.string.settings_alerts_muted_said, targetLabel(choice.muteScope, choice.muteTarget, categoryNames))
                TeaNodeRow(
                    headline = scopeLabel(choice.muteScope, choice.muteTarget, categoryNames),
                    onClick = { muting = null; actions.run(mutedText, onSuccess = { loaded.refresh() }) { connection.muteAgentAlert(alert.id, choice.muteScope) } },
                )
            }
        }
    }
    if (isMutingFollowUps) {
        FormDialog(title = stringResource(R.string.settings_alerts_mute_followups_title), confirmLabel = stringResource(R.string.common_cancel), isConfirmEnabled = true, onConfirm = { isMutingFollowUps = false }, onDismiss = { isMutingFollowUps = false }) {
            AgentFollowUp.muteKinds.forEach { kind ->
                val mutedText = stringResource(R.string.settings_alerts_muted_said, targetLabel("kind", kind, categoryNames))
                TeaNodeRow(
                    headline = sentenceCase(followUpKindName(kind)),
                    onClick = { isMutingFollowUps = false; actions.run(mutedText, onSuccess = { loaded.refresh() }) { connection.muteAgentAlertKind(kind) } },
                )
            }
        }
    }
    if (isAddingFollowUp) {
        AddFollowUpDialog(actions, onDismiss = { isAddingFollowUp = false }, onAdded = { isAddingFollowUp = false; loaded.refresh() })
    }
}

/** The switch, the quiet hours, the most a day and when it looks around town. Each change is saved at once. */
private fun LazyListScope.settingsRows(
    preferences: AgentUpkeepPreferences,
    actions: ActionRunner,
    savedText: String,
    enabledText: String,
    disabledText: String,
    refresh: () -> Unit,
    connection: com.teanode.kit.Connection,
) {
    item {
        SwitchRow(
            stringResource(R.string.settings_alerts_toggle), preferences.isAlertsEnabled,
            { isOn -> actions.run(if (isOn) enabledText else disabledText, onSuccess = refresh) { connection.updateAgentUpkeep(isAlertsEnabled = isOn) } },
        )
    }
    item {
        val isOn = preferences.isAlertsEnabled
        androidx.compose.foundation.layout.Column(Modifier.fillMaxWidth()) {
            TimeField(stringResource(R.string.settings_alerts_quiet_from_label), clockTime(preferences.shownAlertQuietStart), { time -> if (isOn) actions.run(savedText, onSuccess = refresh) { connection.updateAgentUpkeep(alertQuietStart = clockText(time)) } })
            TimeField(stringResource(R.string.settings_alerts_quiet_until_label), clockTime(preferences.shownAlertQuietEnd), { time -> if (isOn) actions.run(savedText, onSuccess = refresh) { connection.updateAgentUpkeep(alertQuietEnd = clockText(time)) } })
            StepperRow(stringResource(R.string.settings_alerts_at_most), preferences.shownAlertDailyMost, 1..50) { most -> if (isOn) actions.run(savedText, onSuccess = refresh) { connection.updateAgentUpkeep(alertDailyMost = most) } }
            ChoiceRow(
                stringResource(R.string.settings_alerts_look_around_on), AgentUpkeepPreferences.lookAroundWeekdays, preferences.shownLookAroundWeekday,
                label = { weekday -> weekdayName(weekday) }, onSelect = { weekday -> actions.run(savedText, onSuccess = refresh) { connection.updateAgentUpkeep(lookAroundWeekday = weekday) } }, isEnabled = isOn,
            )
            TimeField(stringResource(R.string.settings_alerts_look_around_at), clockTime(preferences.shownLookAroundTime), { time -> if (isOn) actions.run(savedText, onSuccess = refresh) { connection.updateAgentUpkeep(lookAroundTime = clockText(time)) } })
        }
    }
    item { FooterText(stringResource(R.string.settings_alerts_footer_one)) }
    item { FooterText(stringResource(R.string.settings_alerts_footer_two)) }
    item { FooterText(stringResource(R.string.settings_alerts_footer_three)) }
}

/** Asks the agent to follow a bill, parcel, deadline or event. The day is set on purpose, since a picker that started on today would file a bill due today at one tap. */
@Composable
private fun AddFollowUpDialog(actions: ActionRunner, onDismiss: () -> Unit, onAdded: () -> Unit) {
    val connection = LocalConnection.current
    var followUp by remember { mutableStateOf(NewAgentFollowUp()) }
    var hasDay by remember { mutableStateOf(false) }
    var day by remember { mutableStateOf(LocalDate.now()) }
    val toAdd = followUp.copy(dueOn = if (hasDay) day.toString() else "")
    val addedText = stringResource(R.string.settings_alerts_added_said, followUp.followUpTitle.trim())
    FormDialog(
        title = stringResource(R.string.settings_alerts_add_title), confirmLabel = stringResource(R.string.settings_alerts_add_confirm), isConfirmEnabled = toAdd.canAdd, isWorking = actions.isWorking,
        onConfirm = { actions.run(addedText, onSuccess = onAdded) { connection.addAgentFollowUp(toAdd) } },
        onDismiss = onDismiss,
    ) {
        androidx.compose.foundation.layout.Column(Modifier.verticalScroll(androidx.compose.foundation.rememberScrollState())) {
            ChoiceRow(
                stringResource(R.string.settings_alerts_add_kind), NewAgentFollowUp.kinds, followUp.followUpKind,
                label = { kind -> sentenceCase(followUpKindName(kind, isSingular = true)) }, onSelect = { kind -> followUp = followUp.copy(followUpKind = kind) },
            )
            FormField(followUp.followUpTitle, { text -> followUp = followUp.copy(followUpTitle = text) }, stringResource(R.string.settings_alerts_add_what))
            SwitchRow(stringResource(R.string.settings_alerts_add_set_day), hasDay, { isOn -> hasDay = isOn })
            if (hasDay) DateField(stringResource(R.string.settings_alerts_add_day), day, { picked -> day = picked })
            when (followUp.followUpKind) {
                "bill" -> {
                    FormField(followUp.payee, { text -> followUp = followUp.copy(payee = text) }, stringResource(R.string.settings_alerts_add_payee))
                    FormField(followUp.amount, { text -> followUp = followUp.copy(amount = text) }, stringResource(R.string.settings_alerts_add_amount), keyboardType = KeyboardType.Decimal)
                    FormField(followUp.currencyCode, { text -> followUp = followUp.copy(currencyCode = text.uppercase()) }, stringResource(R.string.settings_alerts_add_currency), placeholder = "USD")
                }
                "parcel" -> {
                    FormField(followUp.carrier, { text -> followUp = followUp.copy(carrier = text) }, stringResource(R.string.settings_alerts_add_carrier))
                    FormField(followUp.trackingNumber, { text -> followUp = followUp.copy(trackingNumber = text.uppercase()) }, stringResource(R.string.settings_alerts_add_tracking))
                }
                "event" -> FormField(followUp.place, { text -> followUp = followUp.copy(place = text) }, stringResource(R.string.settings_alerts_add_place))
            }
        }
    }
}

