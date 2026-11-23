package com.teanode.app.settings.manage

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AccountBalance
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.Build
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.Extension
import androidx.compose.material.icons.outlined.FiberManualRecord
import androidx.compose.material.icons.outlined.Language
import androidx.compose.material.icons.outlined.Layers
import androidx.compose.material.icons.outlined.Memory
import androidx.compose.material.icons.outlined.Public
import androidx.compose.material.icons.outlined.RecordVoiceOver
import androidx.compose.material.icons.outlined.Speed
import androidx.compose.material.icons.outlined.Storage
import androidx.compose.material.icons.outlined.ToggleOn
import androidx.compose.material.icons.outlined.ViewList
import androidx.compose.runtime.Composable
import androidx.navigation.NavBackStackEntry
import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavType
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import com.teanode.app.R
import com.teanode.app.chats.ChatsHooks
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.Routes

private fun NavGraphBuilder.managePage(route: String, content: @Composable (NavBackStackEntry) -> Unit) {
    composable(route, deepLinks = Routes.deepLinks(route)) { entry -> content(entry) }
}

/** An index in a route: a number, or "new" for the item that is added. */
private fun String?.asIndex(): Int? = this?.toIntOrNull()

/** The routes of the operator's administration that Settings plugs in: the agents page with its settings, skills and runs, and a person's page. */
fun NavGraphBuilder.manageRoutes(navigator: AppNavigator) {
    val back: () -> Unit = { navigator.back() }
    managePage(ManageRoutes.AGENT_PERSONAL) { AgentPersonalSettingsScreen(onBack = back) }
    managePage(ManageRoutes.AGENT_RUNS) { AgentRunsAdminScreen(onBack = back, onOpenConversation = { conversationId -> ChatsHooks.openConversation(navigator, conversationId) }) }
    managePage(ManageRoutes.AGENT_PROVIDERS) { AgentProvidersScreen(onBack = back, onOpenProvider = { index -> navigator.navigate(ManageRoutes.provider(index)) }) }
    composable(ManageRoutes.AGENT_PROVIDER, arguments = listOf(navArgument("providerIndex") { type = NavType.StringType }), deepLinks = Routes.deepLinks(ManageRoutes.AGENT_PROVIDER)) { entry ->
        AgentProviderFormScreen(entry.arguments?.getString("providerIndex").asIndex(), onBack = back)
    }
    managePage(ManageRoutes.AGENT_MODELS) { AgentModelsScreen(onBack = back) }
    managePage(ManageRoutes.AGENT_VOICE) { AgentVoiceScreen(onBack = back) }
    managePage(ManageRoutes.AGENT_RECORDING) { AgentRecordingScreen(onBack = back) }
    managePage(ManageRoutes.AGENT_FEATURES) { AgentFeaturesScreen(onBack = back) }
    managePage(ManageRoutes.AGENT_LIMITS) { AgentLimitsScreen(onBack = back) }
    managePage(ManageRoutes.AGENT_TOOLS) { AgentToolPolicyScreen(onBack = back) }
    managePage(ManageRoutes.AGENT_WEB_SEARCH) { AgentWebSearchScreen(onBack = back) }
    managePage(ManageRoutes.AGENT_BROWSER) { AgentBrowserScreen(onBack = back) }
    managePage(ManageRoutes.AGENT_FINANCE) { AgentFinanceScreen(onBack = back) }
    managePage(ManageRoutes.AGENT_SERVERS) { ConnectedServersScreen(onBack = back, onOpenServer = { index -> navigator.navigate(ManageRoutes.server(index)) }) }
    composable(ManageRoutes.AGENT_SERVER, arguments = listOf(navArgument("serverIndex") { type = NavType.StringType }), deepLinks = Routes.deepLinks(ManageRoutes.AGENT_SERVER)) { entry ->
        ConnectedServerFormScreen(entry.arguments?.getString("serverIndex").asIndex(), onBack = back)
    }
    managePage(ManageRoutes.AGENT_SKILLS) { AgentSkillsScreen(onBack = back, onOpenSkill = { name -> navigator.navigate(ManageRoutes.skill(name)) }) }
    composable(ManageRoutes.AGENT_SKILL, arguments = listOf(navArgument("skillName") { type = NavType.StringType }), deepLinks = Routes.deepLinks(ManageRoutes.AGENT_SKILL)) { entry ->
        InstalledSkillScreen(entry.arguments?.getString("skillName").orEmpty(), onBack = back)
    }
    managePage(ManageRoutes.AGENT_SOURCE_TYPES) { AgentSourceTypesScreen(onBack = back) }
    composable(ManageRoutes.PERSON, arguments = listOf(navArgument("userId") { type = NavType.StringType }), deepLinks = Routes.deepLinks(ManageRoutes.PERSON)) { entry ->
        PersonDetailScreen(entry.arguments?.getString("userId").orEmpty(), onBack = back)
    }
    managePage(ManageRoutes.RELEASE_NOTES) { ReleaseNotesScreen(onBack = back) }
}

/** The places Search offers by name: the pages of the agents page. */
fun managePlaces(): List<com.teanode.app.shell.AppPlace> {
    val parents = listOf(R.string.section_settings, R.string.settings_manage, R.string.settings_agents_title)
    fun place(id: String, titleResource: Int, route: String, icon: androidx.compose.ui.graphics.vector.ImageVector) = com.teanode.app.shell.AppPlace("settings-manage-agents-$id", titleResource, route, icon, parents)
    val icons = Icons.Outlined
    return listOf(
        place("personal", R.string.manage_personal_agents, ManageRoutes.AGENT_PERSONAL, icons.AutoAwesome),
        place("runs", R.string.manage_runs, ManageRoutes.AGENT_RUNS, icons.ViewList),
        place("providers", R.string.manage_providers, ManageRoutes.AGENT_PROVIDERS, icons.Memory),
        place("models", R.string.manage_models, ManageRoutes.AGENT_MODELS, icons.Layers),
        place("voice", R.string.manage_voice, ManageRoutes.AGENT_VOICE, icons.RecordVoiceOver),
        place("recording", R.string.manage_recording, ManageRoutes.AGENT_RECORDING, icons.FiberManualRecord),
        place("features", R.string.manage_features, ManageRoutes.AGENT_FEATURES, icons.ToggleOn),
        place("limits", R.string.manage_limits, ManageRoutes.AGENT_LIMITS, icons.Speed),
        place("tools", R.string.manage_tool_policy, ManageRoutes.AGENT_TOOLS, icons.Build),
        place("web-search", R.string.manage_web_search, ManageRoutes.AGENT_WEB_SEARCH, icons.Language),
        place("browser", R.string.manage_browser, ManageRoutes.AGENT_BROWSER, icons.Public),
        place("servers", R.string.manage_connected_servers, ManageRoutes.AGENT_SERVERS, icons.Dns),
        place("skills", R.string.manage_skills, ManageRoutes.AGENT_SKILLS, icons.Extension),
        place("source-types", R.string.manage_source_types, ManageRoutes.AGENT_SOURCE_TYPES, icons.Storage),
        place("finance", R.string.manage_finance, ManageRoutes.AGENT_FINANCE, icons.AccountBalance),
    )
}
