package com.teanode.app.settings

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AccountCircle
import androidx.compose.material.icons.outlined.Apps
import androidx.compose.material.icons.outlined.Devices
import androidx.compose.material.icons.outlined.Group
import androidx.compose.material.icons.outlined.IosShare
import androidx.compose.material.icons.outlined.Key
import androidx.compose.material.icons.outlined.PhoneAndroid
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material.icons.outlined.Terminal
import androidx.compose.material.icons.outlined.Tune
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.Groups
import androidx.compose.material.icons.outlined.History
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material.icons.outlined.Shield
import androidx.compose.material.icons.outlined.ToggleOn
import androidx.compose.material.icons.outlined.VerifiedUser
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.navigation.NavBackStackEntry
import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavType
import androidx.navigation.compose.composable
import androidx.navigation.navArgument
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.AppNavigator
import com.teanode.app.shell.AppPlace
import com.teanode.app.shell.Area
import com.teanode.app.shell.Routes
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.AppSection
import com.teanode.kit.account.pairedPhones

private fun NavGraphBuilder.settingsPage(route: String, content: @Composable (NavBackStackEntry) -> Unit) {
    composable(route, deepLinks = Routes.deepLinks(route)) { entry -> content(entry) }
}

/** The routes of Settings: the home, and every page under it. */
fun NavGraphBuilder.settingsRoutes(navigator: AppNavigator) {
    settingsPage(SettingsRoutes.HOME) { SettingsSection() }
    settingsPage(SettingsRoutes.PROFILE) { ProfileScreen(onBack = { navigator.back() }) }
    settingsPage(SettingsRoutes.CIRCLES) {
        CirclesScreen(onBack = { navigator.back() }, onOpenCircle = { circleId -> navigator.navigate(SettingsRoutes.circle(circleId)) }, onOpenSharing = { navigator.navigate(SettingsRoutes.SHARING) })
    }
    composable(SettingsRoutes.CIRCLE, arguments = listOf(navArgument("circleId") { type = NavType.StringType }), deepLinks = Routes.deepLinks(SettingsRoutes.CIRCLE)) { entry ->
        val facts by com.teanode.app.session.LocalAppSession.current.facts.collectAsState()
        CircleDetailScreen(
            entry.arguments?.getString("circleId").orEmpty(), onBack = { navigator.back() }, canOpenPhotos = facts.isAvailable(AppSection.Photos),
            onOpenMemberPhotos = { userId, name -> navigator.navigate(SettingsRoutes.memberPhotos(userId, name)) },
        )
    }
    composable(SettingsRoutes.MEMBER_PHOTOS, arguments = listOf(navArgument("userId") { type = NavType.StringType }, navArgument("name") { type = NavType.StringType })) { entry ->
        MemberPhotosScreen(entry.arguments?.getString("userId").orEmpty(), entry.arguments?.getString("name").orEmpty(), onBack = { navigator.back() })
    }
    settingsPage(SettingsRoutes.SHARING) { SharingScreen(onBack = { navigator.back() }) }
    settingsPage(SettingsRoutes.PASSWORD) { ChangePasswordScreen(onBack = { navigator.back() }) }
    settingsPage(SettingsRoutes.PASSKEYS) { PasskeysScreen(onBack = { navigator.back() }) }
    settingsPage(SettingsRoutes.API_TOKENS) { TokensScreen(onBack = { navigator.back() }) }
    settingsPage(SettingsRoutes.DEVICES) { DevicesScreen(onBack = { navigator.back() }, onOpenPhones = { navigator.navigate(SettingsRoutes.PHONES) }, onOpenSync = { navigator.navigate(SettingsRoutes.SYNC) }) }
    settingsPage(SettingsRoutes.PHONES) { PhonesScreen(onBack = { navigator.back() }, onOpenPhone = { phone, _ -> navigator.navigate(SettingsRoutes.phone(phone.id)) }) }
    composable(SettingsRoutes.PHONE, arguments = listOf(navArgument("phoneId") { type = NavType.StringType })) { entry ->
        val phoneId = entry.arguments?.getString("phoneId").orEmpty()
        val connection = LocalConnection.current
        val loaded = rememberLoad { connection.pairedPhones() }
        androidx.compose.material3.Surface {
            LoadedContent(loaded) { list ->
                val phone = list.phones.firstOrNull { candidate -> candidate.id == phoneId }
                if (phone != null) PhoneSettingsScreen(phone, list.notificationKinds, onBack = { navigator.back() })
            }
        }
    }
    settingsPage(SettingsRoutes.AGENT) { AgentSettingsScreen(onBack = { navigator.back() }, onOpenPart = { part -> navigator.navigate(SettingsRoutes.agentPart(part.routeName)) }) }
    composable(SettingsRoutes.AGENT_PART, arguments = listOf(navArgument("part") { type = NavType.StringType }), deepLinks = Routes.deepLinks(SettingsRoutes.AGENT_PART)) { entry ->
        val part = AgentSettingsPart.fromRouteName(entry.arguments?.getString("part").orEmpty())
        if (part != null) AgentSettingsPartScreen(part, onBack = { navigator.back() })
    }
    settingsPage(SettingsRoutes.MANAGE) { ManageScreen(onBack = { navigator.back() }, onOpen = { route -> navigator.navigate(route) }) }
    settingsPage(SettingsRoutes.MANAGE_PEOPLE) { PeopleScreen(onBack = { navigator.back() }) }
    settingsPage(SettingsRoutes.MANAGE_GROUPS) { GroupsScreen(onBack = { navigator.back() }) }
    settingsPage(SettingsRoutes.MANAGE_ROLES) { RolesScreen(onBack = { navigator.back() }) }
    settingsPage(SettingsRoutes.MANAGE_AUDIT) { AuditLogScreen(onBack = { navigator.back() }) }
    settingsPage(SettingsRoutes.MANAGE_SERVICES) { ServerServicesScreen(onBack = { navigator.back() }) }
    settingsPage(SettingsRoutes.SYNC) { SyncCredentialsScreen(onBack = { navigator.back() }) }
    settingsPage(SettingsRoutes.PHOTO_PEOPLE) { com.teanode.app.photos.PhotosSection(com.teanode.app.photos.PhotoSectionChoice.People) }
    settingsPage(SettingsRoutes.SOURCES) { KnowledgeSourcesScreen(onBack = { navigator.back() }) }
    settingsPage(SettingsRoutes.MEMORY) { AgentMemorySettingsScreen(onBack = { navigator.back() }) }
    settingsPage(SettingsRoutes.DREAMS) { DreamsScreen(onBack = { navigator.back() }) }
    settingsPage(SettingsRoutes.SCHEDULES) { SchedulesScreen(onBack = { navigator.back() }) }
    settingsPage(SettingsRoutes.ALERTS) { MutedAlertsScreen(onBack = { navigator.back() }) }
    settingsPage(SettingsRoutes.CONNECTIONS) { ConnectionsScreen(onBack = { navigator.back() }) }
    settingsPage(SettingsRoutes.ACTIVITY) { AgentActivityScreen(onBack = { navigator.back() }) }
    settingsPage(SettingsRoutes.SKILLS) { SkillSharingScreen(onBack = { navigator.back() }) }
    settingsPage(SettingsRoutes.MANAGE_AGENTS) { AgentAdminScreen(onBack = { navigator.back() }) }
    settingsPage(SettingsRoutes.MANAGE_SERVER) { ServerSettingsScreen(onBack = { navigator.back() }) }
    settingsPage(SettingsRoutes.MANAGE_USAGE) { AgentUsageScreen(onBack = { navigator.back() }) }
    settingsPage(SettingsRoutes.APPS) { ConnectedAppsScreen(onBack = { navigator.back() }) }
    settingsPage(SettingsRoutes.SESSIONS) { SessionsScreen(onBack = { navigator.back() }) }
}

private fun place(id: String, titleResource: Int, route: String, icon: androidx.compose.ui.graphics.vector.ImageVector) =
    AppPlace("settings-$id", titleResource, route, icon, listOf(R.string.section_settings))

private fun managePlace(id: String, titleResource: Int, route: String, icon: androidx.compose.ui.graphics.vector.ImageVector) =
    AppPlace("settings-manage-$id", titleResource, route, icon, listOf(R.string.section_settings, R.string.settings_manage))

/** The places Search offers by name: the pages of Settings, as the iPhone app's Search offers them. */
fun settingsPlaces(): List<AppPlace> = listOf(
    place("profile", R.string.settings_profile_title, SettingsRoutes.PROFILE, Icons.Outlined.AccountCircle),
    place("circles", R.string.settings_circles_title, SettingsRoutes.CIRCLES, Icons.Outlined.Group),
    place("sharing", R.string.settings_sharing_title, SettingsRoutes.SHARING, Icons.Outlined.IosShare),
    place("password", R.string.settings_password_title, SettingsRoutes.PASSWORD, Icons.Outlined.Key),
    place("passkeys", R.string.settings_passkeys_title, SettingsRoutes.PASSKEYS, Icons.Outlined.VerifiedUser),
    place("api-tokens", R.string.settings_tokens_title, SettingsRoutes.API_TOKENS, Icons.Outlined.Terminal),
    place("devices", R.string.settings_devices_title, SettingsRoutes.DEVICES, Icons.Outlined.PhoneAndroid),
    place("apps", R.string.settings_apps_title, SettingsRoutes.APPS, Icons.Outlined.Apps),
    place("sessions", R.string.settings_sessions_title, SettingsRoutes.SESSIONS, Icons.Outlined.Devices),
    place("agent-settings", R.string.settings_agent_title, SettingsRoutes.AGENT, Icons.Outlined.Tune),
    place("sources", R.string.settings_sources_title, SettingsRoutes.SOURCES, Icons.Outlined.Tune),
    place("memory", R.string.settings_memory_title, SettingsRoutes.MEMORY, Icons.Outlined.Tune),
    place("dreams", R.string.settings_dreams_title, SettingsRoutes.DREAMS, Icons.Outlined.Tune),
    place("schedules", R.string.settings_schedules_title, SettingsRoutes.SCHEDULES, Icons.Outlined.Tune),
    place("alerts", R.string.settings_alerts_title, SettingsRoutes.ALERTS, Icons.Outlined.Tune),
    place("connections", R.string.settings_connections_title, SettingsRoutes.CONNECTIONS, Icons.Outlined.Tune),
    place("activity", R.string.settings_activity_title, SettingsRoutes.ACTIVITY, Icons.Outlined.Tune),
    place("skill-sharing", R.string.settings_skills_title, SettingsRoutes.SKILLS, Icons.Outlined.Tune),
    place("manage", R.string.settings_manage, SettingsRoutes.MANAGE, Icons.Outlined.Dns),
    managePlace("people", R.string.settings_people_title, SettingsRoutes.MANAGE_PEOPLE, Icons.Outlined.Person),
    managePlace("groups", R.string.settings_groups_title, SettingsRoutes.MANAGE_GROUPS, Icons.Outlined.Groups),
    managePlace("roles", R.string.settings_roles_title, SettingsRoutes.MANAGE_ROLES, Icons.Outlined.Shield),
    managePlace("audit-log", R.string.settings_audit_title, SettingsRoutes.MANAGE_AUDIT, Icons.Outlined.History),
    managePlace("agents", R.string.settings_agents_title, SettingsRoutes.MANAGE_AGENTS, Icons.Outlined.Person),
    managePlace("server", R.string.settings_server_title, SettingsRoutes.MANAGE_SERVER, Icons.Outlined.Dns),
    managePlace("usage", R.string.settings_usage_title, SettingsRoutes.MANAGE_USAGE, Icons.Outlined.History),
    managePlace("services", R.string.settings_services_title, SettingsRoutes.MANAGE_SERVICES, Icons.Outlined.ToggleOn),
) + listOf(
    AgentSettingsPart.About to R.string.settings_agent_about, AgentSettingsPart.Instructions to R.string.settings_agent_instructions, AgentSettingsPart.Language to R.string.settings_agent_language,
    AgentSettingsPart.WritingVoice to R.string.settings_agent_voice, AgentSettingsPart.SpeakingVoice to R.string.settings_agent_speaking_voice, AgentSettingsPart.Reach to R.string.settings_agent_reach,
    AgentSettingsPart.Categories to R.string.settings_agent_categories, AgentSettingsPart.Model to R.string.settings_agent_model, AgentSettingsPart.SpeakingFirst to R.string.settings_agent_speaking_first,
    AgentSettingsPart.AskFirst to R.string.settings_agent_ask_first, AgentSettingsPart.WhenAway to R.string.settings_agent_when_away,
).map { (part, title) -> AppPlace("settings-agent-${part.routeName}", title, SettingsRoutes.agentPart(part.routeName), Icons.Outlined.Tune, listOf(R.string.section_settings, R.string.settings_agent_title)) }

/** What Settings plugs into the shell; listed in `Areas`. */
val settingsArea = Area(
    section = AppSection.Settings,
    routes = NavGraphBuilder::settingsRoutes,
    places = ::settingsPlaces,
    demoAnswerers = settingsDemoAnswerers,
)
