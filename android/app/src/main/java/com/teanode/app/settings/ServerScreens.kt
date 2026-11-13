package com.teanode.app.settings

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.RestartAlt
import androidx.compose.material.icons.outlined.SystemUpdate
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ScrollableTabRow
import androidx.compose.material3.Tab
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.manage.AgentUsageGrouping
import com.teanode.kit.manage.ServerSettingField
import com.teanode.kit.manage.ServerSettingsForm
import com.teanode.kit.manage.ServerSettingsTab
import com.teanode.kit.manage.agentAdminOverview
import com.teanode.kit.manage.applyServerUpgrade
import com.teanode.kit.manage.restartServer
import com.teanode.kit.manage.serverSettings
import com.teanode.kit.manage.serverStatus
import com.teanode.kit.manage.serverUpgrade
import com.teanode.kit.manage.totalTokenCount
import com.teanode.kit.manage.updateServerSettingsSection
import com.teanode.kit.manage.usageBounds
import com.teanode.kit.photos.ServerTime
import kotlinx.serialization.json.JsonObject

/** The server page: what it is and its restart and upgrade, then the tabs of its settings. */
@Composable
fun ServerSettingsScreen(onBack: () -> Unit, initialTab: ServerSettingsTab? = null) {
    val tabs = ServerSettingsTab.entries
    var tabIndex by remember { mutableStateOf(initialTab?.let { tab -> tabs.indexOf(tab) + 1 } ?: 0) }
    TeaNodeScreen(title = stringResource(R.string.settings_server_title), onBack = onBack) { contentPadding ->
        Column(Modifier.padding(contentPadding)) {
            ScrollableTabRow(selectedTabIndex = tabIndex, edgePadding = 8.dp) {
                Tab(selected = tabIndex == 0, onClick = { tabIndex = 0 }, text = { Text(stringResource(R.string.settings_server_about)) })
                tabs.forEachIndexed { index, tab -> Tab(selected = tabIndex == index + 1, onClick = { tabIndex = index + 1 }, text = { Text(serverTabTitle(tab)) }) }
            }
            when {
                tabIndex == 0 -> ServerAboutTab()
                tabs[tabIndex - 1] == ServerSettingsTab.Setup -> ServerSetupTab()
                tabs[tabIndex - 1] == ServerSettingsTab.Sso -> SsoProvidersTab()
                else -> ServerSectionsTab(tabs[tabIndex - 1])
            }
        }
    }
}

@Composable
private fun serverTabTitle(tab: ServerSettingsTab): String = stringResource(
    when (tab) {
        ServerSettingsTab.Setup -> R.string.settings_server_setup
        ServerSettingsTab.Identity -> R.string.settings_server_identity
        ServerSettingsTab.Mail -> R.string.settings_server_mail
        ServerSettingsTab.Sending -> R.string.settings_server_sending
        ServerSettingsTab.Listeners -> R.string.settings_server_listeners
        ServerSettingsTab.Certificates -> R.string.settings_server_certificates
        ServerSettingsTab.Storage -> R.string.settings_server_storage
        ServerSettingsTab.Resolver -> R.string.settings_server_resolver
        ServerSettingsTab.Scanning -> R.string.settings_server_scanning
        ServerSettingsTab.Sessions -> R.string.settings_server_sessions
        ServerSettingsTab.Sso -> R.string.settings_server_sso
    },
)

/** What the instance is, what waits for a restart, the restart, and the upgrade. */
@Composable
private fun ServerAboutTab() {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val status = rememberLoad { connection.serverStatus() }
    val upgrade = rememberLoad { connection.serverUpgrade() }
    var isConfirmingRestart by remember { mutableStateOf(false) }
    var upgradeToApply by remember { mutableStateOf<String?>(null) }
    val restartText = stringResource(R.string.settings_server_restarting)
    val upgradeText = stringResource(R.string.settings_server_upgrade_started)
    val checkedText = stringResource(R.string.settings_server_checked)
    com.teanode.app.ui.components.LoadedContent(status) { found ->
        androidx.compose.foundation.lazy.LazyColumn {
            item {
                TeaNodeRow(headline = stringResource(R.string.settings_server_instance), supporting = found.instance)
                TeaNodeRow(headline = stringResource(R.string.settings_version), supporting = listOf(found.version, found.shortCommit).filter { text -> text.isNotEmpty() }.joinToString(" · "))
                ServerTime.instant(found.startedAt)?.let { instant -> TeaNodeRow(headline = stringResource(R.string.settings_server_started), supporting = shortDateTime(instant)) }
                found.pendingRestart?.takeIf { list -> list.isNotEmpty() }?.let { list -> TeaNodeRow(headline = stringResource(R.string.settings_server_pending_restart), supporting = list.joinToString(", "), supportingMaxLines = 4) }
                if (found.isUnsupervised) FooterText(stringResource(R.string.settings_server_unsupervised))
                TeaNodeRow(
                    headline = stringResource(if (found.isRestarting) R.string.settings_server_restarting_now else R.string.settings_server_restart), leadingIcon = Icons.Outlined.RestartAlt,
                    onClick = if (found.isRestarting || actions.isWorking) null else ({ isConfirmingRestart = true }),
                )
                TeaNodeSectionHeader(stringResource(R.string.settings_server_upgrade))
                val release = upgrade.value
                if (release != null) {
                    TeaNodeRow(
                        headline = stringResource(if (release.isAvailable) R.string.settings_server_upgrade_available else R.string.settings_server_up_to_date), leadingIcon = Icons.Outlined.SystemUpdate,
                        supporting = listOfNotNull(if (release.isAvailable && release.latest != null) "${release.current} \u2192 ${release.latest}" else release.current, release.refusalReason?.takeIf { reason -> release.isAvailable && !release.isApplicable }, release.errorMessage, release.checkError).joinToString("\n"), supportingMaxLines = 5,
                    )
                    release.notes?.takeIf { notes -> notes.isNotEmpty() }?.let { notes -> FooterText(notes) }
                    TeaNodeRow(headline = stringResource(R.string.settings_server_check), onClick = if (actions.isWorking) null else ({ actions.run(checkedText, onSuccess = { upgrade.refresh() }) { connection.serverUpgrade(isChecking = true) } }))
                    if (release.canUpgrade && !release.isUpgrading) TeaNodeRow(headline = stringResource(R.string.settings_server_upgrade_to, release.latest.orEmpty()), onClick = { upgradeToApply = release.latest })
                    if (release.isUpgrading) TeaNodeRow(headline = stringResource(R.string.settings_server_upgrading))
                }
            }
        }
    }
    if (isConfirmingRestart) {
        ConfirmationDialog(
            title = stringResource(R.string.settings_server_restart), confirmLabel = stringResource(R.string.settings_server_restart), isDestructive = true, text = stringResource(R.string.settings_server_restart_detail),
            onConfirm = { isConfirmingRestart = false; actions.run(restartText, onSuccess = { status.refresh() }) { connection.restartServer() } }, onDismiss = { isConfirmingRestart = false },
        )
    }
    upgradeToApply?.let { version ->
        ConfirmationDialog(
            title = stringResource(R.string.settings_server_upgrade_to, version), confirmLabel = stringResource(R.string.settings_server_upgrade_confirm), text = stringResource(R.string.settings_server_upgrade_detail),
            onConfirm = { upgradeToApply = null; actions.run(upgradeText, onSuccess = { upgrade.refresh() }) { connection.applyServerUpgrade(version) } }, onDismiss = { upgradeToApply = null },
        )
    }
}

/** The sections of one tab of the settings, each as a form of the fields the server sent, saved a section at a time. */
@Composable
private fun ServerSectionsTab(tab: ServerSettingsTab) {
    val connection = LocalConnection.current
    val loaded = rememberLoad { connection.serverSettings() }
    com.teanode.app.ui.components.LoadedContent(loaded) { settings ->
        androidx.compose.foundation.lazy.LazyColumn(contentPadding = androidx.compose.foundation.layout.PaddingValues(bottom = 24.dp)) {
            val present = tab.sections.filter { name -> (settings[name] as? JsonObject) != null }
            if (present.isEmpty()) item { EmptyState(Icons.Outlined.Dns, stringResource(R.string.settings_server_nothing)) }
            present.forEach { name ->
                item(key = name) { ServerSectionForm(name, settings[name] as JsonObject, onSaved = { loaded.refresh() }) }
            }
        }
    }
}

@Composable
private fun ServerSectionForm(section: String, original: JsonObject, onSaved: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val fields = remember(original) { ServerSettingsForm.fields(original) }
    var texts by remember(original) { mutableStateOf(fields.mapNotNull { field -> when (field) { is ServerSettingField.Text -> field.name to field.text; is ServerSettingField.Number -> field.name to field.text; is ServerSettingField.TextList -> field.name to field.text; else -> null } }.toMap()) }
    var switches by remember(original) { mutableStateOf(fields.filterIsInstance<ServerSettingField.Switch>().associate { field -> field.name to field.isOn }) }
    var secrets by remember(original) { mutableStateOf(mapOf<String, String>()) }
    var cleared by remember(original) { mutableStateOf(setOf<String>()) }
    val savedText = stringResource(R.string.settings_saved)
    val changes = ServerSettingsForm.changes(original, texts, switches, secrets, cleared)
    val isValid = fields.filterIsInstance<ServerSettingField.Number>().all { field -> ServerSettingsForm.isNumberValid(texts[field.name].orEmpty(), field.isWhole) }
    TeaNodeSectionHeader(com.teanode.kit.agent.KnowledgeSourceType.label(section).split(" ").joinToString(" ") { word -> if (word.lowercase() in setOf("smtp", "dns", "tls", "imap", "s3", "sso", "url", "api")) word.uppercase() else word })
    fields.forEach { field ->
        val label = com.teanode.kit.agent.KnowledgeSourceType.label(field.name)
        when (field) {
            is ServerSettingField.Switch -> SwitchRow(label, switches[field.name] == true, { isOn -> switches = switches + (field.name to isOn) })
            is ServerSettingField.Number -> FormField(texts[field.name].orEmpty(), { typed -> texts = texts + (field.name to typed) }, label, keyboardType = KeyboardType.Decimal, isError = !ServerSettingsForm.isNumberValid(texts[field.name].orEmpty(), field.isWhole))
            is ServerSettingField.Text -> FormField(texts[field.name].orEmpty(), { typed -> texts = texts + (field.name to typed) }, label)
            is ServerSettingField.TextList -> FormField(texts[field.name].orEmpty(), { typed -> texts = texts + (field.name to typed) }, label, supporting = stringResource(R.string.settings_server_list_hint))
            is ServerSettingField.Secret -> {
                FormField(
                    secrets[field.name].orEmpty(), { typed -> secrets = secrets + (field.name to typed); cleared = cleared - field.name }, label, isPassword = true,
                    supporting = stringResource(if (cleared.contains(field.name)) R.string.settings_server_secret_clearing else if (field.isKept) R.string.settings_server_secret_kept else R.string.settings_server_secret_none),
                )
                if (field.isKept && !cleared.contains(field.name)) TeaNodeRow(headline = stringResource(R.string.settings_server_secret_clear), onClick = { cleared = cleared + field.name; secrets = secrets - field.name })
            }
            is ServerSettingField.Shown -> TeaNodeRow(headline = label, supporting = field.text, supportingMaxLines = 6)
        }
    }
    Button(
        onClick = { actions.run(savedText, onSuccess = onSaved) { connection.updateServerSettingsSection(section, changes) } }, enabled = changes.isNotEmpty() && isValid && !actions.isWorking,
        modifier = Modifier.padding(16.dp).fillMaxWidth(),
    ) { Text(stringResource(R.string.settings_save)) }
}

/** What the agents used over the last days, added up by day, kind, model or agent, and the jobs given up on. */
@Composable
fun AgentUsageScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    var grouping by remember { mutableStateOf(AgentUsageGrouping.Day) }
    var days by remember { mutableStateOf(30) }
    val loaded = rememberLoad(grouping, days) { val bounds = usageBounds(days); connection.agentAdminOverview(grouping, bounds.first, bounds.second) }
    LoadedSettingsPage(stringResource(R.string.settings_usage_title), onBack, loaded) { overview ->
        item {
            ChoiceRow(stringResource(R.string.settings_usage_group_by), AgentUsageGrouping.entries, grouping, label = { choice -> usageGroupingName(choice) }, onSelect = { grouping = it })
            ChoiceRow(stringResource(R.string.settings_usage_range), listOf(7, 30, 90), days, label = { choice -> stringResource(R.string.settings_usage_last_days, choice) }, onSelect = { days = it })
        }
        if (overview.usageRows.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.settings_usage_none)) }
        items(overview.usageRows.size) { index ->
            val row = overview.usageRows[index]
            val name = if (grouping == AgentUsageGrouping.Agent) overview.username(row.usageKey) else row.usageKey
            val billed = row.billedAmount?.let { amount -> stringResource(R.string.settings_usage_billed, "%.2f %s".format(amount, row.billedCurrencyCode.orEmpty()).trim()) }
            TeaNodeRow(
                headline = name, supporting = listOfNotNull(stringResource(R.string.settings_usage_tokens_calls, row.totals.totalTokenCount, row.totals.calls), "%.2f %s".format(row.cost, row.currency.orEmpty()).trim(), billed).joinToString("\n"), supportingMaxLines = 4,
            )
        }
        if (overview.deadJobs.isNotEmpty()) {
            item { TeaNodeSectionHeader(stringResource(R.string.settings_usage_dead_jobs)) }
            items(overview.deadJobs.size) { index ->
                val job = overview.deadJobs[index]
                TeaNodeRow(headline = "${job.kind} · ${overview.username(job.agentId)}", supporting = listOfNotNull(stringResource(R.string.settings_usage_attempts, job.attempts), job.error.ifEmpty { null }).joinToString("\n"), supportingMaxLines = 4)
            }
        }
    }
}

@Composable
private fun usageGroupingName(grouping: AgentUsageGrouping): String = stringResource(
    when (grouping) { AgentUsageGrouping.Day -> R.string.settings_usage_by_day; AgentUsageGrouping.Kind -> R.string.settings_usage_by_kind; AgentUsageGrouping.Model -> R.string.settings_usage_by_model; AgentUsageGrouping.Agent -> R.string.settings_usage_by_agent },
)
