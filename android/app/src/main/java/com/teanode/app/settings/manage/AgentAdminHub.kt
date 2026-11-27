package com.teanode.app.settings.manage

import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AccountBalance
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.BarChart
import androidx.compose.material.icons.outlined.Build
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.ExtensionOff
import androidx.compose.material.icons.outlined.Extension
import androidx.compose.material.icons.outlined.Language
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material.icons.outlined.Memory
import androidx.compose.material.icons.outlined.People
import androidx.compose.material.icons.outlined.RecordVoiceOver
import androidx.compose.material.icons.outlined.Speed
import androidx.compose.material.icons.outlined.Storage
import androidx.compose.material.icons.outlined.Tune
import androidx.compose.material.icons.outlined.ViewList
import androidx.compose.material.icons.outlined.FiberManualRecord
import androidx.compose.material.icons.outlined.Layers
import androidx.compose.material.icons.outlined.Public
import androidx.compose.material.icons.outlined.ToggleOn
import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.settings.LoadedSettingsPage
import com.teanode.app.settings.SettingsRoutes
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.TeaNodeNavigationRow
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.access.OperatorAccess
import com.teanode.kit.access.operatorAccess
import com.teanode.kit.manage.AgentAdminTab
import com.teanode.kit.manage.agentAdminTabs

/**
 * The dashboard's agents page: everybody's agents with their limits and switches, what they cost, what they are doing and have done (oversight), and the
 * operator's agent settings, skills and source types. Each tab is offered only to whoever holds the permission it needs.
 */
@Composable
fun AgentAdminHubScreen(onBack: () -> Unit, onOpen: (String) -> Unit) {
    val connection = LocalConnection.current
    val access = rememberLoad { connection.operatorAccess() }
    LoadedSettingsPage(
        stringResource(R.string.settings_agents_title), onBack, access, isEmpty = { operator: OperatorAccess -> operator.agentAdminTabs.isEmpty() },
        empty = { EmptyState(Icons.Outlined.Lock, stringResource(R.string.manage_agents_nothing)) },
    ) { operator ->
        operator.agentAdminTabs.forEach { tab -> tabSection(tab, onOpen) }
    }
}

private fun LazyListScope.tabSection(tab: AgentAdminTab, onOpen: (String) -> Unit) {
    when (tab) {
        AgentAdminTab.General -> {
            item { TeaNodeSectionHeader(stringResource(R.string.manage_tab_general)) }
            item { TeaNodeNavigationRow(stringResource(R.string.manage_agents), { onOpen(ManageRoutes.AGENT_LIST) }, leadingIcon = Icons.Outlined.People) }
            item { TeaNodeNavigationRow(stringResource(R.string.manage_personal_agents), { onOpen(ManageRoutes.AGENT_PERSONAL) }, leadingIcon = Icons.Outlined.AutoAwesome) }
        }
        AgentAdminTab.Usage -> item { TeaNodeNavigationRow(stringResource(R.string.manage_use_and_cost), { onOpen(SettingsRoutes.MANAGE_USAGE) }, leadingIcon = Icons.Outlined.BarChart) }
        AgentAdminTab.Runs -> item { TeaNodeNavigationRow(stringResource(R.string.manage_oversight), { onOpen(ManageRoutes.AGENT_RUNS) }, leadingIcon = Icons.Outlined.ViewList) }
        AgentAdminTab.Models -> {
            item { TeaNodeSectionHeader(stringResource(R.string.manage_tab_models)) }
            item { TeaNodeNavigationRow(stringResource(R.string.manage_providers), { onOpen(ManageRoutes.AGENT_PROVIDERS) }, leadingIcon = Icons.Outlined.Memory) }
            item { TeaNodeNavigationRow(stringResource(R.string.manage_models), { onOpen(ManageRoutes.AGENT_MODELS) }, leadingIcon = Icons.Outlined.Layers) }
            item { TeaNodeNavigationRow(stringResource(R.string.manage_voice), { onOpen(ManageRoutes.AGENT_VOICE) }, leadingIcon = Icons.Outlined.RecordVoiceOver) }
            item { TeaNodeNavigationRow(stringResource(R.string.manage_recording), { onOpen(ManageRoutes.AGENT_RECORDING) }, leadingIcon = Icons.Outlined.FiberManualRecord) }
            item { TeaNodeNavigationRow(stringResource(R.string.manage_features), { onOpen(ManageRoutes.AGENT_FEATURES) }, leadingIcon = Icons.Outlined.ToggleOn) }
            item { TeaNodeNavigationRow(stringResource(R.string.manage_limits), { onOpen(ManageRoutes.AGENT_LIMITS) }, leadingIcon = Icons.Outlined.Speed) }
        }
        AgentAdminTab.Tools -> {
            item { TeaNodeSectionHeader(stringResource(R.string.manage_tab_tools)) }
            item { TeaNodeNavigationRow(stringResource(R.string.manage_tool_policy), { onOpen(ManageRoutes.AGENT_TOOLS) }, leadingIcon = Icons.Outlined.Build) }
            item { TeaNodeNavigationRow(stringResource(R.string.manage_web_search), { onOpen(ManageRoutes.AGENT_WEB_SEARCH) }, leadingIcon = Icons.Outlined.Language) }
            item { TeaNodeNavigationRow(stringResource(R.string.manage_browser), { onOpen(ManageRoutes.AGENT_BROWSER) }, leadingIcon = Icons.Outlined.Public) }
            item { TeaNodeNavigationRow(stringResource(R.string.manage_connected_servers), { onOpen(ManageRoutes.AGENT_SERVERS) }, leadingIcon = Icons.Outlined.Dns) }
        }
        AgentAdminTab.Skills -> item { TeaNodeNavigationRow(stringResource(R.string.manage_skills), { onOpen(ManageRoutes.AGENT_SKILLS) }, leadingIcon = Icons.Outlined.Extension) }
        AgentAdminTab.Sources -> {
            item { TeaNodeSectionHeader(stringResource(R.string.manage_tab_sources)) }
            item { TeaNodeNavigationRow(stringResource(R.string.manage_source_types), { onOpen(ManageRoutes.AGENT_SOURCE_TYPES) }, leadingIcon = Icons.Outlined.Storage) }
            item { TeaNodeNavigationRow(stringResource(R.string.manage_finance), { onOpen(ManageRoutes.AGENT_FINANCE) }, leadingIcon = Icons.Outlined.AccountBalance) }
        }
    }
}
