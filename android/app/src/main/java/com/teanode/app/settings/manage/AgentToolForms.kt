package com.teanode.app.settings.manage

import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.settings.ChoiceRow
import com.teanode.app.settings.FooterText
import com.teanode.app.settings.FormField
import com.teanode.app.settings.LoadedSettingsPage
import com.teanode.app.settings.SwitchRow
import com.teanode.app.settings.rememberActionRunner
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.manage.AgentBrowserDraft
import com.teanode.kit.manage.AgentFinanceDraft
import com.teanode.kit.manage.AgentToolPolicyDraft
import com.teanode.kit.manage.AgentWebSearchDraft
import com.teanode.kit.manage.ConnectedServerDraft
import com.teanode.kit.manage.ConnectedServerSettings
import com.teanode.kit.manage.SettingsList
import com.teanode.kit.manage.agentServerSettings
import com.teanode.kit.manage.updateAgentServerSettings

@Composable
private fun policyWord(word: String): String = stringResource(
    when (word) {
        "confirm" -> R.string.manage_policy_confirm
        "off" -> R.string.manage_policy_off
        else -> R.string.manage_policy_allow
    },
)

/**
 * Every tool the agent can be given, by family, and one word for each: allowed, ask first, or off. A family's word covers its tools
 * unless one says otherwise.
 */
@Composable
fun AgentToolPolicyScreen(onBack: () -> Unit) {
    AgentSettingsFormScreen(stringResource(R.string.manage_tool_policy), onBack, { settings -> AgentToolPolicyDraft.of(settings) }, { draft -> draft.agentParameters }) { draft, onChange, settings ->
        FooterText(stringResource(R.string.manage_tool_policy_footer))
        settings.families.forEach { family ->
            val members = settings.tools.catalog.filter { tool -> tool.family == family }
            val familyWord = draft.word(family)
            var isShowingTools by remember(family) { mutableStateOf(false) }
            TeaNodeSectionHeader(family)
            ChoiceRow(
                family, AgentToolPolicyDraft.WORDS, familyWord, label = { word -> policyWord(word) }, onSelect = { word -> onChange(draft.setting(family, word)) },
            )
            TeaNodeRow(
                headline = stringResource(R.string.manage_each_tool), supporting = if (members.size == 1) stringResource(R.string.manage_one_tool) else stringResource(R.string.manage_tools_count, members.size),
                onClick = { isShowingTools = !isShowingTools },
            )
            if (isShowingTools) {
                members.forEach { tool ->
                    val lines = listOfNotNull(
                        if (tool.confirms) stringResource(R.string.manage_always_asks) else null,
                        tool.description.ifEmpty { null },
                        tool.actions?.takeIf { actions -> actions.isNotEmpty() }?.joinToString(" · "),
                    )
                    val asFamily = stringResource(R.string.manage_as_the_family, policyWord(familyWord))
                    ChoiceRow(
                        tool.name, AgentToolPolicyDraft.WORDS, draft.word(tool.name),
                        label = { word -> if (word == "allow" && familyWord != "allow") asFamily else policyWord(word) }, onSelect = { word -> onChange(draft.setting(tool.name, word)) },
                    )
                    if (lines.isNotEmpty()) FooterText(lines.joinToString(" · "))
                }
                if (members.isEmpty() || family == "servers") FooterText(stringResource(R.string.manage_tool_servers_footer))
            }
        }
    }
}

/** The service web_search asks. */
@Composable
fun AgentWebSearchScreen(onBack: () -> Unit) {
    AgentSettingsFormScreen(stringResource(R.string.manage_web_search), onBack, { settings -> AgentWebSearchDraft(settings.search.searchKind) }, { draft -> draft.agentParameters }) { draft, onChange, settings ->
        val none = stringResource(R.string.manage_web_search_none)
        ChoiceRow(stringResource(R.string.manage_provider), listOf("", "brave"), draft.searchKind, label = { kind -> if (kind.isEmpty()) none else "Brave" }, onSelect = { kind -> onChange(draft.copy(searchKind = kind)) })
        FooterText(stringResource(R.string.manage_web_search_footer))
        SecretField(stringResource(R.string.manage_api_key), settings.search.hasApiKey, draft.apiKey, { typed -> onChange(draft.copy(apiKey = typed)) })
    }
}

/** The headless Chrome beside the server. */
@Composable
fun AgentBrowserScreen(onBack: () -> Unit) {
    AgentSettingsFormScreen(stringResource(R.string.manage_browser), onBack, { settings -> AgentBrowserDraft.of(settings.browser) }, { draft -> draft.agentParameters }) { draft, onChange, _ ->
        SwitchRow(stringResource(R.string.manage_enabled), draft.isEnabled, { isOn -> onChange(draft.copy(isEnabled = isOn)) })
        FooterText(stringResource(R.string.manage_browser_footer))
        FormField(draft.cdpEndpoint, { typed -> onChange(draft.copy(cdpEndpoint = typed)) }, stringResource(R.string.manage_devtools_endpoint), placeholder = "chrome:9222")
        FormField(draft.idleTimeout, { typed -> onChange(draft.copy(idleTimeout = typed)) }, stringResource(R.string.manage_idle_timeout))
        FormField(draft.maxContextsText, { typed -> onChange(draft.copy(maxContextsText = SettingsList.digits(typed))) }, stringResource(R.string.manage_contexts_at_once), keyboardType = KeyboardType.Number)
        SwitchRow(stringResource(R.string.manage_attach_tabs), draft.canAttachTabs, { isOn -> onChange(draft.copy(canAttachTabs = isOn)) })
    }
}

/** Which services people may link their banks, cards, brokerages and loans through, and the operator's Plaid account. */
@Composable
fun AgentFinanceScreen(onBack: () -> Unit) {
    AgentSettingsFormScreen(stringResource(R.string.manage_finance), onBack, { settings -> AgentFinanceDraft.of(settings.finance) }, { draft -> draft.agentParameters }) { draft, onChange, settings ->
        SwitchRow(stringResource(R.string.manage_finance_plaid), draft.offeredProviders.contains("plaid"), { isOn -> onChange(draft.settingOffered("plaid", isOn)) })
        SwitchRow(stringResource(R.string.manage_finance_simplefin), draft.offeredProviders.contains("simplefin"), { isOn -> onChange(draft.settingOffered("simplefin", isOn)) })
        FooterText(stringResource(R.string.manage_finance_footer))
        if (draft.isOfferingPlaid) {
            val notChosen = stringResource(R.string.manage_plaid_not_chosen)
            val sandbox = stringResource(R.string.manage_plaid_sandbox)
            val production = stringResource(R.string.manage_plaid_production)
            ChoiceRow(
                stringResource(R.string.manage_plaid_environment), listOf("", "sandbox", "production"), draft.environment,
                label = { environment -> when (environment) { "sandbox" -> sandbox; "production" -> production; else -> notChosen } }, onSelect = { environment -> onChange(draft.copy(environment = environment)) },
            )
            FormField(draft.clientId, { typed -> onChange(draft.copy(clientId = typed)) }, stringResource(R.string.manage_plaid_client_id))
            FooterText(stringResource(R.string.manage_plaid_footer))
            SecretField(stringResource(R.string.manage_plaid_secret), settings.finance.plaid.hasSecret, draft.secret, { typed -> onChange(draft.copy(secret = typed)) })
            SettingField(
                stringResource(R.string.manage_plaid_countries), draft.countryCodesText, { typed -> onChange(draft.copy(countryCodesText = typed.uppercase())) },
                hint = stringResource(R.string.manage_plaid_countries_hint), prompt = "US",
            )
            SwitchRow(stringResource(R.string.manage_plaid_transactions), true, {}, isEnabled = false)
            SwitchRow(stringResource(R.string.manage_plaid_investments), draft.hasProduct("investments"), { isOn -> onChange(draft.settingProduct("investments", isOn)) })
            SwitchRow(stringResource(R.string.manage_plaid_liabilities), draft.hasProduct("liabilities"), { isOn -> onChange(draft.settingProduct("liabilities", isOn)) })
            FooterText(stringResource(R.string.manage_plaid_products_footer))
        }
    }
}

/** The connected servers the operator declares, a row each, edited one at a time because a server has a dozen fields and most are read once. */
@Composable
fun ConnectedServersScreen(onBack: () -> Unit, onOpenServer: (Int?) -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad { connection.agentServerSettings() }
    var removingIndex by remember { mutableStateOf<Int?>(null) }
    val removedText = stringResource(R.string.manage_removed)
    LoadedSettingsPage(
        stringResource(R.string.manage_connected_servers), onBack, loaded,
        floatingActionButton = { androidx.compose.material3.FloatingActionButton(onClick = { onOpenServer(null) }) { androidx.compose.material3.Icon(Icons.Outlined.Add, stringResource(R.string.manage_add_a_server)) } },
        isEmpty = { settings -> settings.mcpServers.isEmpty() }, empty = { EmptyState(Icons.Outlined.Dns, stringResource(R.string.manage_no_connected_servers)) },
    ) { settings ->
        items(settings.mcpServers.size) { index ->
            val server = settings.mcpServers[index]
            TeaNodeRow(
                headline = server.name,
                overline = listOfNotNull(server.effectiveTransport.ifEmpty { null }, if (server.isHeadless) stringResource(R.string.manage_headless) else null, if (!server.isEnabled) stringResource(R.string.manage_off) else null).joinToString(" · ").ifEmpty { null },
                supporting = serverDetails(server), supportingMaxLines = 4, onClick = { onOpenServer(index) },
                trailing = { androidx.compose.material3.TextButton(onClick = { removingIndex = index }) { Text(stringResource(R.string.settings_remove)) } },
            )
        }
        item { FooterText(stringResource(R.string.manage_servers_footer)) }
    }
    val settings = loaded.value
    removingIndex?.let { index ->
        val server = settings?.mcpServers?.getOrNull(index)
        if (server != null) ConfirmationDialog(
            title = stringResource(R.string.manage_remove_server), confirmLabel = stringResource(R.string.settings_remove), isDestructive = true, text = stringResource(R.string.manage_remove_server_detail, server.name),
            onConfirm = { removingIndex = null; actions.run(removedText, onSuccess = { loaded.refresh() }) { connection.updateAgentServerSettings(ConnectedServerDraft.agentParameters(index, settings)) } },
            onDismiss = { removingIndex = null },
        )
    }
}

@Composable
private fun serverDetails(server: ConnectedServerSettings): String {
    val authText = server.effectiveAuth.ifEmpty { "none" }
    return listOfNotNull(
        if (server.effectiveTransport == "stdio") (listOf(server.command) + server.args).joinToString(" ") else server.url,
        stringResource(R.string.manage_server_auth, authText),
        server.timeout.ifEmpty { null }?.let { timeout -> stringResource(R.string.manage_server_timeout, timeout) },
        server.readOnly.takeIf { tools -> tools.isNotEmpty() }?.let { tools -> stringResource(R.string.manage_server_read_only, SettingsList.text(tools)) },
        server.disabled.takeIf { tools -> tools.isNotEmpty() }?.let { tools -> stringResource(R.string.manage_server_disabled, SettingsList.text(tools)) },
    ).filter { part -> part.isNotEmpty() }.joinToString(" · ")
}

/** Adds a connected server or changes one. Which fields it asks for follows how the server is reached and how it signs in. */
@Composable
fun ConnectedServerFormScreen(serverIndex: Int?, onBack: () -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val loaded = rememberLoad { connection.agentServerSettings() }
    val settings = loaded.value
    var draft by remember(settings) { mutableStateOf(settings?.let { found -> serverIndex?.let { index -> found.mcpServers.getOrNull(index) }?.let { server -> ConnectedServerDraft.of(server) } ?: ConnectedServerDraft() }) }
    val savedText = stringResource(R.string.settings_saved)
    com.teanode.app.ui.components.TeaNodeScreen(
        title = stringResource(if (serverIndex == null) R.string.manage_add_a_server else R.string.manage_edit_server), onBack = onBack,
        actions = {
            val current = draft
            if (settings != null && current != null) com.teanode.app.settings.BarTextAction(
                stringResource(R.string.settings_save), onClick = { actions.run(savedText, onSuccess = onBack) { connection.updateAgentServerSettings(current.agentParameters(serverIndex, settings)) } },
                isEnabled = current.canSave, isWorking = actions.isWorking,
            )
        },
    ) { contentPadding ->
        com.teanode.app.ui.components.LoadedContent(loaded, androidx.compose.ui.Modifier.padding(contentPadding)) {
            val current = draft
            if (current != null) androidx.compose.foundation.layout.Column(androidx.compose.ui.Modifier.verticalScroll(rememberScrollState())) {
                ServerFormFields(current, { changed -> draft = changed })
                androidx.compose.foundation.layout.Spacer(androidx.compose.ui.Modifier.padding(bottom = 24.dp))
            }
        }
    }
}

@Composable
private fun ServerFormFields(draft: ConnectedServerDraft, onChange: (ConnectedServerDraft) -> Unit) {
    FormField(draft.name, { typed -> onChange(draft.copy(name = typed)) }, stringResource(R.string.manage_server_name))
    val inferred = stringResource(R.string.manage_inferred)
    ChoiceRow(stringResource(R.string.manage_transport), listOf("", "http", "stdio"), draft.transport, label = { transport -> transport.ifEmpty { inferred } }, onSelect = { transport -> onChange(draft.copy(transport = transport)) })
    if (draft.asksLocation) {
        val onServer = stringResource(R.string.manage_location_server)
        val onComputer = stringResource(R.string.manage_location_computer)
        ChoiceRow(stringResource(R.string.manage_where_command_runs), listOf("", "computer"), draft.location, label = { place -> if (place == "computer") onComputer else onServer }, onSelect = { place -> onChange(draft.settingLocation(place)) })
    }
    if (draft.showsUrl) FormField(draft.url, { typed -> onChange(draft.copy(url = typed)) }, stringResource(R.string.manage_server_url), placeholder = "https://", keyboardType = KeyboardType.Uri)
    if (draft.showsCommand) {
        FormField(draft.command, { typed -> onChange(draft.copy(command = typed)) }, stringResource(R.string.manage_server_command))
        FormField(draft.argsText, { typed -> onChange(draft.copy(argsText = typed)) }, stringResource(R.string.manage_server_arguments))
        if (draft.isStdio) {
            FormField(draft.workingDir, { typed -> onChange(draft.copy(workingDir = typed)) }, stringResource(R.string.manage_server_working_directory))
            FormField(draft.envText, { typed -> onChange(draft.copy(envText = typed)) }, stringResource(R.string.manage_server_environment), isSingleLine = false, minLines = 2, supporting = stringResource(R.string.manage_server_environment_hint))
        }
    }
    val authNames = listOf("" to inferred, "none" to stringResource(R.string.manage_auth_none), "static" to stringResource(R.string.manage_auth_static), "user" to stringResource(R.string.manage_auth_user), "oauth" to stringResource(R.string.manage_auth_oauth))
    ChoiceRow(stringResource(R.string.manage_auth), authNames.map { pair -> pair.first }, draft.auth, label = { auth -> authNames.first { pair -> pair.first == auth }.second }, onSelect = { auth -> onChange(draft.copy(auth = auth)) })
    FormField(draft.timeout, { typed -> onChange(draft.copy(timeout = typed)) }, stringResource(R.string.manage_server_timeout_label), placeholder = "30s")
    if (draft.showsAuthorization) SecretField(stringResource(R.string.manage_server_authorization), draft.hasAuthorization, draft.authorization, { typed -> onChange(draft.copy(authorization = typed)) })
    if (draft.isOAuth) {
        TeaNodeSectionHeader("OAuth")
        FormField(draft.oauthClientId, { typed -> onChange(draft.copy(oauthClientId = typed)) }, stringResource(R.string.manage_oauth_client_id))
        FormField(draft.oauthScopesText, { typed -> onChange(draft.copy(oauthScopesText = typed)) }, stringResource(R.string.manage_oauth_scopes))
        FormField(draft.oauthAuthorizationUrl, { typed -> onChange(draft.copy(oauthAuthorizationUrl = typed)) }, stringResource(R.string.manage_oauth_authorization_url), placeholder = stringResource(R.string.manage_oauth_discovered))
        FormField(draft.oauthTokenUrl, { typed -> onChange(draft.copy(oauthTokenUrl = typed)) }, stringResource(R.string.manage_oauth_token_url), placeholder = stringResource(R.string.manage_oauth_discovered))
        val thisServer = stringResource(R.string.manage_redirect_server)
        val yourComputer = stringResource(R.string.manage_redirect_computer)
        ChoiceRow(stringResource(R.string.manage_redirect), listOf("", "computer"), draft.oauthRedirect, label = { place -> if (place == "computer") yourComputer else thisServer }, onSelect = { place -> onChange(draft.copy(oauthRedirect = place)) })
        SecretField(stringResource(R.string.manage_oauth_client_secret), draft.hasOauthClientSecret, draft.oauthClientSecret, { typed -> onChange(draft.copy(oauthClientSecret = typed)) })
    }
    FormField(draft.readOnlyText, { typed -> onChange(draft.copy(readOnlyText = typed)) }, stringResource(R.string.manage_read_only_tools))
    FormField(draft.disabledText, { typed -> onChange(draft.copy(disabledText = typed)) }, stringResource(R.string.manage_disabled_tools))
    if (draft.canBeHeadless) SwitchRow(stringResource(R.string.manage_headless_usable), draft.isHeadless, { isOn -> onChange(draft.copy(isHeadless = isOn)) })
    SwitchRow(stringResource(R.string.manage_enabled), draft.isEnabled, { isOn -> onChange(draft.copy(isEnabled = isOn)) })
}
