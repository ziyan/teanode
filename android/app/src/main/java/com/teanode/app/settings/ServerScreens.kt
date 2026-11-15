package com.teanode.app.settings

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AutoFixHigh
import androidx.compose.material.icons.outlined.Description
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
import com.teanode.app.ui.components.TeaNodeNavigationRow
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.manage.AgentUsageGrouping
import com.teanode.kit.manage.ServerSettingChoices
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
    val navigator = com.teanode.app.shell.LocalAppNavigator.current
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
                TeaNodeSectionHeader(stringResource(R.string.settings_server_about_header))
                TeaNodeRow(headline = stringResource(R.string.settings_server_instance), supporting = found.instance)
                TeaNodeRow(headline = stringResource(R.string.settings_version), supporting = listOf(found.version, found.shortCommit).filter { text -> text.isNotEmpty() }.joinToString(" · "))
                ServerTime.instant(found.startedAt)?.let { instant -> TeaNodeRow(headline = stringResource(R.string.settings_server_started), supporting = shortDateTime(instant)) }
                found.pendingRestart?.takeIf { list -> list.isNotEmpty() }?.let { list -> TeaNodeRow(headline = stringResource(R.string.settings_server_pending_restart), supporting = stringResource(R.string.settings_server_about_pending, list.joinToString(", ")), supportingMaxLines = 6) }
                    ?: FooterText(stringResource(R.string.settings_server_about_nothing_pending))
                if (found.isUnsupervised) FooterText(stringResource(R.string.settings_server_unsupervised))
                TeaNodeRow(
                    headline = stringResource(if (found.isRestarting) R.string.settings_server_restarting_now else R.string.settings_server_restart), leadingIcon = Icons.Outlined.RestartAlt,
                    onClick = if (found.isRestarting || actions.isWorking) null else ({ isConfirmingRestart = true }),
                )
                FooterText(stringResource(R.string.settings_server_about_restart_footer))
                TeaNodeSectionHeader(stringResource(R.string.settings_server_upgrade))
                val release = upgrade.value
                if (release != null) {
                    TeaNodeRow(
                        headline = stringResource(if (release.isAvailable) R.string.settings_server_upgrade_available else R.string.settings_server_up_to_date), leadingIcon = Icons.Outlined.SystemUpdate,
                        supporting = listOfNotNull(if (release.isAvailable && release.latest != null) "${release.current} \u2192 ${release.latest}" else release.current, release.refusalReason?.takeIf { reason -> release.isAvailable && !release.isApplicable }, release.errorMessage, release.checkError).joinToString("\n"), supportingMaxLines = 5,
                    )
                    // Shown whenever there are notes, not only when behind: a development build is ahead of every release and still has a version worth reading about.
                    release.notes?.takeIf { notes -> notes.isNotEmpty() }?.let {
                        TeaNodeNavigationRow(stringResource(R.string.manage_release_notes_title, release.latest.orEmpty()), { navigator.navigate(com.teanode.app.settings.manage.ManageRoutes.RELEASE_NOTES) }, leadingIcon = Icons.Outlined.Description)
                    }
                    TeaNodeRow(headline = stringResource(R.string.settings_server_check), onClick = if (actions.isWorking) null else ({ actions.run(checkedText, onSuccess = { upgrade.refresh() }) { connection.serverUpgrade(isChecking = true) } }))
                    if (release.canUpgrade && !release.isUpgrading) TeaNodeRow(headline = stringResource(R.string.settings_server_upgrade_to, release.latest.orEmpty()), onClick = { upgradeToApply = release.latest })
                    if (release.isUpgrading) TeaNodeRow(headline = stringResource(R.string.settings_server_upgrading))
                    if (release.isEnabled == false) FooterText(stringResource(R.string.settings_server_about_checking_off))
                    if (release.isAutomatic == true) FooterText(release.upgradeWindow?.takeIf { window -> window.isNotEmpty() }?.let { window -> stringResource(R.string.settings_server_about_automatic_window, window) } ?: stringResource(R.string.settings_server_about_automatic))
                    if (release.canUpgrade) FooterText(stringResource(R.string.settings_server_about_upgrade_footer))
                }
            }
        }
    }
    if (isConfirmingRestart) {
        ConfirmationDialog(
            title = stringResource(R.string.settings_server_about_restart_title), confirmLabel = stringResource(R.string.settings_server_about_restart_confirm), isDestructive = true,
            text = stringResource(R.string.settings_server_about_restart_question, status.value?.instance.orEmpty()),
            onConfirm = { isConfirmingRestart = false; actions.run(restartText, onSuccess = { status.refresh() }) { connection.restartServer() } }, onDismiss = { isConfirmingRestart = false },
        )
    }
    upgradeToApply?.let { version ->
        ConfirmationDialog(
            title = stringResource(R.string.settings_server_about_upgrade_title, version), confirmLabel = stringResource(R.string.settings_server_about_upgrade_confirm), text = stringResource(R.string.settings_server_about_upgrade_footer),
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
    val fields = remember(original) { ServerSettingsForm.fields(original, section) }
    var texts by remember(original) { mutableStateOf(fields.mapNotNull { field -> when (field) { is ServerSettingField.Choice -> field.name to field.text; is ServerSettingField.Text -> field.name to field.text; is ServerSettingField.Number -> field.name to field.text; is ServerSettingField.TextList -> field.name to field.text; else -> null } }.toMap()) }
    var switches by remember(original) { mutableStateOf(fields.filterIsInstance<ServerSettingField.Switch>().associate { field -> field.name to field.isOn }) }
    var secrets by remember(original) { mutableStateOf(mapOf<String, String>()) }
    var cleared by remember(original) { mutableStateOf(setOf<String>()) }
    val savedText = stringResource(R.string.settings_saved)
    var isConfirmingSave by remember { mutableStateOf(false) }
    val changes = ServerSettingsForm.changes(original, texts, switches, secrets, cleared, section)
    val isValid = fields.filterIsInstance<ServerSettingField.Number>().all { field -> ServerSettingsForm.isNumberValid(texts[field.name].orEmpty(), field.isWhole) }
    TeaNodeSectionHeader(
        ServerFieldWords.section(section)?.let { words -> stringResource(words) }
            ?: com.teanode.kit.agent.KnowledgeSourceType.label(section).split(" ").joinToString(" ") { word -> if (word.lowercase() in setOf("smtp", "dns", "tls", "imap", "s3", "sso", "url", "api")) word.uppercase() else word },
    )
    serverSectionFooter(section)?.let { footer -> FooterText(stringResource(footer)) }
    fields.forEach { field ->
        val label = ServerFieldWords.label(section, field.name)?.let { words -> stringResource(words) } ?: com.teanode.kit.agent.KnowledgeSourceType.label(field.name)
        val hint = ServerFieldWords.hint(section, field.name)?.let { words -> stringResource(words) }
        when (field) {
            is ServerSettingField.Choice -> {
                ChoiceRow(
                    stringResource(serverChoiceLabel(section, field.name)), field.options, texts[field.name].orEmpty(),
                    label = { option -> serverChoiceOptionText(section, field.name, option) }, onSelect = { option -> texts = texts + (field.name to option) },
                )
                serverChoiceFooter(section, field.name)?.let { footer -> FooterText(stringResource(footer)) }
            }
            is ServerSettingField.Switch -> {
                SwitchRow(label, switches[field.name] == true, { isOn -> switches = switches + (field.name to isOn) })
                hint?.let { text -> FooterText(text) }
            }
            is ServerSettingField.Number -> FormField(texts[field.name].orEmpty(), { typed -> texts = texts + (field.name to typed) }, label, keyboardType = KeyboardType.Decimal, isError = !ServerSettingsForm.isNumberValid(texts[field.name].orEmpty(), field.isWhole), supporting = hint)
            is ServerSettingField.Text -> FormField(texts[field.name].orEmpty(), { typed -> texts = texts + (field.name to typed) }, label, supporting = hint)
            is ServerSettingField.TextList -> FormField(texts[field.name].orEmpty(), { typed -> texts = texts + (field.name to typed) }, label, supporting = hint ?: stringResource(R.string.settings_server_list_hint))
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
    if (ServerSettingChoices.isReadAtStart(section)) {
        val navigator = com.teanode.app.shell.LocalAppNavigator.current
        TeaNodeRow(
            headline = stringResource(R.string.settings_server_restart_on_about), leadingIcon = Icons.Outlined.RestartAlt,
            onClick = { navigator.navigate(SettingsRoutes.MANAGE_SERVER) },
        )
        FooterText(stringResource(R.string.settings_server_footer_restart))
    }
    if (section == "relay") {
        var isPickingPreset by remember { mutableStateOf(false) }
        TeaNodeRow(headline = stringResource(R.string.settings_server_relay_preset_button), leadingIcon = Icons.Outlined.AutoFixHigh, onClick = { isPickingPreset = true })
        if (isPickingPreset) {
            androidx.compose.material3.AlertDialog(
                onDismissRequest = { isPickingPreset = false }, title = { Text(stringResource(R.string.settings_server_relay_preset_button)) },
                text = {
                    Column {
                        RelayPreset.entries.forEach { preset ->
                            TeaNodeRow(
                                headline = preset.label,
                                onClick = { isPickingPreset = false; texts = texts + ("host" to preset.host) + ("port" to preset.port.toString()) + ("security" to preset.security) },
                            )
                        }
                    }
                },
                confirmButton = {}, dismissButton = { androidx.compose.material3.TextButton(onClick = { isPickingPreset = false }) { Text(stringResource(R.string.common_cancel)) } },
            )
        }
        FooterText(stringResource(R.string.settings_server_relay_preset_footer))
    }
    Button(
        onClick = { if (section == "listen") isConfirmingSave = true else actions.run(savedText, onSuccess = onSaved) { connection.updateServerSettingsSection(section, changes) } }, enabled = changes.isNotEmpty() && isValid && !actions.isWorking,
        modifier = Modifier.padding(16.dp).fillMaxWidth(),
    ) { Text(stringResource(R.string.settings_save)) }
    if (isConfirmingSave) {
        ConfirmationDialog(
            title = stringResource(R.string.settings_server_listen_confirm_title, texts["https"].orEmpty()), text = stringResource(R.string.settings_server_listen_confirm_message),
            confirmLabel = stringResource(R.string.settings_server_listen_confirm_button),
            onConfirm = { isConfirmingSave = false; actions.run(savedText, onSuccess = onSaved) { connection.updateServerSettingsSection(section, changes) } }, onDismiss = { isConfirmingSave = false },
        )
    }
}

/** A provider's host, port and encryption, to fill in the relay form with, as the iPhone's list does. */
private enum class RelayPreset(val label: String, val host: String, val port: Int, val security: String) {
    Gmail("Gmail", "smtp.gmail.com", 587, "starttls"),
    AmazonSes("Amazon SES", "email-smtp.us-east-1.amazonaws.com", 587, "starttls"),
    Postmark("Postmark", "smtp.postmarkapp.com", 587, "starttls"),
    Resend("Resend", "smtp.resend.com", 465, "tls"),
}

/** What a section of the server's settings is for, as the iPhone's form says under its fields; null for a section it has no words for. */
private fun serverSectionFooter(section: String): Int? = when (section) {
    "identity" -> R.string.settings_server_footer_identity
    "smtp" -> R.string.settings_server_footer_smtp
    "resolver" -> R.string.settings_server_footer_resolver
    "session" -> R.string.settings_server_footer_session
    "passkey" -> R.string.settings_server_footer_passkey
    "listen" -> R.string.settings_server_footer_listen
    "storage" -> R.string.settings_server_footer_storage
    "geoip" -> R.string.settings_server_footer_geoip
    "places" -> R.string.settings_server_footer_places
    "relay" -> R.string.settings_server_footer_relay
    "proxy" -> R.string.settings_server_footer_proxy
    "s3" -> R.string.settings_server_footer_s3
    "route53" -> R.string.settings_server_footer_route53
    "antivirus" -> R.string.settings_server_footer_antivirus
    "antispam" -> R.string.settings_server_footer_antispam
    "sso" -> R.string.settings_server_footer_sso
    else -> null
}

/** The label of a field that is a choice. */
private fun serverChoiceLabel(section: String, field: String): Int = when (section to field) {
    ("identity" to "logLevel") -> R.string.settings_server_label_log_level
    ("places" to "providerKind") -> R.string.settings_server_label_provider
    ("relay" to "security") -> R.string.settings_server_label_encryption
    ("certificates" to "acmeChallenge") -> R.string.settings_server_label_challenge
    else -> R.string.settings_server_label_engine
}

/** What a choice is called: the log levels and the challenge types are shown as the server names them, the rest in words. */
@Composable
private fun serverChoiceOptionText(section: String, field: String, option: String): String = when (section to field) {
    ("places" to "providerKind") -> stringResource(when (option) { "nominatim" -> R.string.settings_server_choice_places_nominatim; "photon" -> R.string.settings_server_choice_places_photon; else -> R.string.settings_server_choice_places_off })
    ("relay" to "security") -> stringResource(when (option) { "tls" -> R.string.settings_server_choice_relay_tls; "none" -> R.string.settings_server_choice_relay_none; else -> R.string.settings_server_choice_relay_starttls })
    ("antispam" to "engine") -> stringResource(if (option == "spamd") R.string.settings_server_choice_engine_spamd else R.string.settings_server_choice_engine_builtin)
    else -> option
}

private fun serverChoiceFooter(section: String, field: String): Int? = when (section to field) {
    ("identity" to "logLevel") -> R.string.settings_server_footer_log_level
    ("certificates" to "acmeChallenge") -> R.string.settings_server_footer_challenge
    else -> null
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
