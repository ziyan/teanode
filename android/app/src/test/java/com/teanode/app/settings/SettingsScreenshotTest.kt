package com.teanode.app.settings

import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import com.teanode.app.demo.DemoHost
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import com.teanode.app.screenshots.WithFontScale
import com.teanode.app.screenshots.photograph
import com.teanode.app.ui.theme.AppearanceChoice
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Pictures of every Settings screen and state in the invented demo account: light and dark, the dense ones at the largest text. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class SettingsScreenshotTest {
    @get:Rule val composeRule = createComposeRule()

    private fun shoot(name: String, appearance: AppearanceChoice = AppearanceChoice.Light, fontScale: Float = 1f, before: () -> Unit = {}, content: @Composable () -> Unit) {
        composeRule.setContent { DemoHost(appearance = appearance) { WithFontScale(fontScale) { CompositionLocalProvider(LocalDialogsInline provides true) { content() } } } }
        composeRule.mainClock.advanceTimeBy(500)
        composeRule.waitForIdle()
        before()
        composeRule.waitForIdle()
        composeRule.photograph(name)
    }

    @Test fun `sign out with unsent data light`() = shoot("settings-sign-out-unsent-light") {
        com.teanode.app.session.SignOutConfirmationContent(com.teanode.kit.session.UnsentData(unfinishedRecordingCount = 2, unsentLocationReportCount = 14, hasUnsentMail = true), {}, {})
    }
    @Test fun `sign out with unsent data dark`() = shoot("settings-sign-out-unsent-dark", AppearanceChoice.Dark) {
        com.teanode.app.session.SignOutConfirmationContent(com.teanode.kit.session.UnsentData(unfinishedRecordingCount = 2), {}, {})
    }
    @Test fun `sign out with nothing unsent keeps the plain question`() {
        composeRule.setContent { DemoHost { com.teanode.app.session.SignOutConfirmationContent(com.teanode.kit.session.UnsentData(), {}, {}) } }
        composeRule.waitForIdle()
        composeRule.onNodeWithText("This phone stops talking to your agent until you connect it again.").assertExists()
    }
    @Test fun `sign out with unsent data names it and offers sign out anyway`() {
        composeRule.setContent { DemoHost { com.teanode.app.session.SignOutConfirmationContent(com.teanode.kit.session.UnsentData(unfinishedRecordingCount = 2), {}, {}) } }
        composeRule.waitForIdle()
        composeRule.onNodeWithText("Sign out anyway").assertExists()
        composeRule.onNodeWithText("2 recordings have not finished uploading.", substring = true).assertExists()
        composeRule.onNodeWithText("Signing out deletes them from this phone.", substring = true).assertExists()
    }

    @Test fun `home light`() = shoot("settings-home-light") { SettingsSection() }
    @Test fun `home dark`() = shoot("settings-home-dark", AppearanceChoice.Dark) { SettingsSection() }
    @Test fun `home largest text`() = shoot("settings-home-largest-text", fontScale = 2f) { SettingsSection() }

    @Test fun `home notifications light`() = shoot("settings-home-notifications-light", before = { composeRule.onNode(androidx.compose.ui.test.hasScrollAction()).performScrollToNode(androidx.compose.ui.test.hasText("Notifications")) }) { SettingsSection() }

    @Test fun `profile light`() = shoot("settings-profile-light") { ProfileScreen(onBack = {}) }
    @Test fun `profile dark`() = shoot("settings-profile-dark", AppearanceChoice.Dark) { ProfileScreen(onBack = {}) }
    @Test fun `profile largest text`() = shoot("settings-profile-largest-text", fontScale = 2f) { ProfileScreen(onBack = {}) }

    @Test fun `password light`() = shoot("settings-password-light") { ChangePasswordScreen(onBack = {}) }
    @Test fun `password dark`() = shoot("settings-password-dark", AppearanceChoice.Dark) { ChangePasswordScreen(onBack = {}) }

    @Test fun `devices light`() = shoot("settings-devices-light") { DevicesScreen(onBack = {}, onOpenPhones = {}) }
    @Test fun `phones light`() = shoot("settings-phones-light") { PhonesScreen(onBack = {}, onOpenPhone = { _, _ -> }) }
    @Test fun `phones dark`() = shoot("settings-phones-dark", AppearanceChoice.Dark) { PhonesScreen(onBack = {}, onOpenPhone = { _, _ -> }) }
    @Test fun `phones largest text`() = shoot("settings-phones-largest-text", fontScale = 2f) { PhonesScreen(onBack = {}, onOpenPhone = { _, _ -> }) }

    @Test fun `passkeys light`() = shoot("settings-passkeys-light") { PasskeysScreen(onBack = {}) }
    @Test fun `passkeys dark`() = shoot("settings-passkeys-dark", AppearanceChoice.Dark) { PasskeysScreen(onBack = {}) }

    @Test fun `sessions light`() = shoot("settings-sessions-light") { SessionsScreen(onBack = {}) }
    @Test fun `sessions dark`() = shoot("settings-sessions-dark", AppearanceChoice.Dark) { SessionsScreen(onBack = {}) }
    @Test fun `sessions largest text`() = shoot("settings-sessions-largest-text", fontScale = 2f) { SessionsScreen(onBack = {}) }

    @Test fun `tokens light`() = shoot("settings-tokens-light") { TokensScreen(onBack = {}) }
    @Test fun `tokens dark`() = shoot("settings-tokens-dark", AppearanceChoice.Dark) { TokensScreen(onBack = {}) }
    @Test fun `tokens largest text`() = shoot("settings-tokens-largest-text", fontScale = 2f) { TokensScreen(onBack = {}) }

    @Test fun `apps light`() = shoot("settings-apps-light") { ConnectedAppsScreen(onBack = {}) }
    @Test fun `apps dark`() = shoot("settings-apps-dark", AppearanceChoice.Dark) { ConnectedAppsScreen(onBack = {}) }
    @Test fun `apps largest text`() = shoot("settings-apps-largest-text", fontScale = 2f) { ConnectedAppsScreen(onBack = {}) }

    @Test fun `circles light`() = shoot("settings-circles-light") { CirclesScreen(onBack = {}, onOpenCircle = {}, onOpenSharing = {}) }
    @Test fun `circles dark`() = shoot("settings-circles-dark", AppearanceChoice.Dark) { CirclesScreen(onBack = {}, onOpenCircle = {}, onOpenSharing = {}) }
    @Test fun `circles largest text`() = shoot("settings-circles-largest-text", fontScale = 2f) { CirclesScreen(onBack = {}, onOpenCircle = {}, onOpenSharing = {}) }
    @Test fun `circle admin light`() = shoot("settings-circle-admin-light") { CircleDetailScreen("circle-home", onBack = {}) }
    @Test fun `circle admin dark`() = shoot("settings-circle-admin-dark", AppearanceChoice.Dark) { CircleDetailScreen("circle-home", onBack = {}) }
    @Test fun `circle admin largest text`() = shoot("settings-circle-admin-largest-text", fontScale = 2f) { CircleDetailScreen("circle-home", onBack = {}) }
    @Test fun `circle member light`() = shoot("settings-circle-member-light") { CircleDetailScreen("circle-club", onBack = {}) }
    @Test fun `circle gone light`() = shoot("settings-circle-gone-light") { CircleDetailScreen("nothing-here", onBack = {}) }

    @Test fun `sharing light`() = shoot("settings-sharing-light") { SharingScreen(onBack = {}) }
    @Test fun `sharing dark`() = shoot("settings-sharing-dark", AppearanceChoice.Dark) { SharingScreen(onBack = {}) }
    @Test fun `sharing largest text`() = shoot("settings-sharing-largest-text", fontScale = 2f) { SharingScreen(onBack = {}) }

    @Test fun `agent light`() = shoot("settings-agent-light") { AgentSettingsScreen(onBack = {}, onOpenPart = {}) }
    @Test fun `agent dark`() = shoot("settings-agent-dark", AppearanceChoice.Dark) { AgentSettingsScreen(onBack = {}, onOpenPart = {}) }
    @Test fun `agent about light`() = shoot("settings-agent-about-light") { AgentSettingsPartScreen(AgentSettingsPart.About, onBack = {}) }
    @Test fun `agent instructions light`() = shoot("settings-agent-instructions-light") { AgentSettingsPartScreen(AgentSettingsPart.Instructions, onBack = {}) }
    @Test fun `agent language light`() = shoot("settings-agent-language-light") { AgentSettingsPartScreen(AgentSettingsPart.Language, onBack = {}) }
    @Test fun `agent voice light`() = shoot("settings-agent-voice-light") { AgentSettingsPartScreen(AgentSettingsPart.WritingVoice, onBack = {}) }
    @Test fun `agent speaking voice light`() = shoot("settings-agent-speaking-voice-light") { AgentSettingsPartScreen(AgentSettingsPart.SpeakingVoice, onBack = {}) }
    @Test fun `agent reach light`() = shoot("settings-agent-reach-light") { AgentSettingsPartScreen(AgentSettingsPart.Reach, onBack = {}) }
    @Test fun `agent reach dark`() = shoot("settings-agent-reach-dark", AppearanceChoice.Dark) { AgentSettingsPartScreen(AgentSettingsPart.Reach, onBack = {}) }
    @Test fun `agent categories light`() = shoot("settings-agent-categories-light") { AgentSettingsPartScreen(AgentSettingsPart.Categories, onBack = {}) }
    @Test fun `agent model light`() = shoot("settings-agent-model-light") { AgentSettingsPartScreen(AgentSettingsPart.Model, onBack = {}) }
    @Test fun `agent speaking first light`() = shoot("settings-agent-speaking-first-light") { AgentSettingsPartScreen(AgentSettingsPart.SpeakingFirst, onBack = {}) }
    @Test fun `agent ask first light`() = shoot("settings-agent-ask-first-light") { AgentSettingsPartScreen(AgentSettingsPart.AskFirst, onBack = {}) }
    @Test fun `agent ask first largest text`() = shoot("settings-agent-ask-first-largest-text", fontScale = 2f) { AgentSettingsPartScreen(AgentSettingsPart.AskFirst, onBack = {}) }
    @Test fun `agent when away light`() = shoot("settings-agent-when-away-light") { AgentSettingsPartScreen(AgentSettingsPart.WhenAway, onBack = {}) }

    @Test fun `manage light`() = shoot("settings-manage-light") { ManageScreen(onBack = {}, onOpen = {}) }
    @Test fun `people light`() = shoot("settings-people-light") { PeopleScreen(onBack = {}) }
    @Test fun `people dark`() = shoot("settings-people-dark", AppearanceChoice.Dark) { PeopleScreen(onBack = {}) }
    @Test fun `people largest text`() = shoot("settings-people-largest-text", fontScale = 2f) { PeopleScreen(onBack = {}) }
    @Test fun `groups light`() = shoot("settings-groups-light") { GroupsScreen(onBack = {}) }
    @Test fun `groups dark`() = shoot("settings-groups-dark", AppearanceChoice.Dark) { GroupsScreen(onBack = {}) }
    @Test fun `roles light`() = shoot("settings-roles-light") { RolesScreen(onBack = {}) }
    @Test fun `audit light`() = shoot("settings-audit-light") { AuditLogScreen(onBack = {}) }
    @Test fun `audit dark`() = shoot("settings-audit-dark", AppearanceChoice.Dark) { AuditLogScreen(onBack = {}) }
    @Test fun `audit largest text`() = shoot("settings-audit-largest-text", fontScale = 2f) { AuditLogScreen(onBack = {}) }
    @Test fun `services light`() = shoot("settings-services-light") { ServerServicesScreen(onBack = {}) }
    @Test fun `services dark`() = shoot("settings-services-dark", AppearanceChoice.Dark) { ServerServicesScreen(onBack = {}) }
    @Test fun `sync light`() = shoot("settings-sync-light") { SyncCredentialsScreen(onBack = {}) }

    @Test fun `sources light`() = shoot("settings-sources-light") { KnowledgeSourcesScreen(onBack = {}) }
    @Test fun `sources dark`() = shoot("settings-sources-dark", AppearanceChoice.Dark) { KnowledgeSourcesScreen(onBack = {}) }
    @Test fun `memory light`() = shoot("settings-memory-light") { AgentMemorySettingsScreen(onBack = {}) }
    @Test fun `memory dark`() = shoot("settings-memory-dark", AppearanceChoice.Dark) { AgentMemorySettingsScreen(onBack = {}) }
    @Test fun `dreams light`() = shoot("settings-dreams-light") { DreamsScreen(onBack = {}) }
    @Test fun `dreams dark`() = shoot("settings-dreams-dark", AppearanceChoice.Dark) { DreamsScreen(onBack = {}) }
    @Test fun `dreams largest text`() = shoot("settings-dreams-largest-text", fontScale = 2f) { DreamsScreen(onBack = {}) }
    @Test fun `schedules light`() = shoot("settings-schedules-light") { SchedulesScreen(onBack = {}) }
    @Test fun `schedules dark`() = shoot("settings-schedules-dark", AppearanceChoice.Dark) { SchedulesScreen(onBack = {}) }
    @Test fun `alerts light`() = shoot("settings-alerts-light") { MutedAlertsScreen(onBack = {}) }
    @Test fun `alerts dark`() = shoot("settings-alerts-dark", AppearanceChoice.Dark) { MutedAlertsScreen(onBack = {}) }
    @Test fun `connections light`() = shoot("settings-connections-light") { ConnectionsScreen(onBack = {}) }
    @Test fun `connections dark`() = shoot("settings-connections-dark", AppearanceChoice.Dark) { ConnectionsScreen(onBack = {}) }
    @Test fun `activity light`() = shoot("settings-activity-light") { AgentActivityScreen(onBack = {}) }
    @Test fun `activity dark`() = shoot("settings-activity-dark", AppearanceChoice.Dark) { AgentActivityScreen(onBack = {}) }

    @Test fun `skill sharing light`() = shoot("settings-skills-light") { SkillSharingScreen(onBack = {}) }
    @Test fun `skill sharing dark`() = shoot("settings-skills-dark", AppearanceChoice.Dark) { SkillSharingScreen(onBack = {}) }
    @Test fun `skill sharing largest text`() = shoot("settings-skills-largest-text", fontScale = 2f) { SkillSharingScreen(onBack = {}) }

    @Test fun `agents admin light`() = shoot("settings-agents-admin-light") { AgentAdminScreen(onBack = {}) }
    @Test fun `agents admin dark`() = shoot("settings-agents-admin-dark", AppearanceChoice.Dark) { AgentAdminScreen(onBack = {}) }

    private fun tap(description: String) = { composeRule.onNodeWithContentDescription(description).performClick(); Unit }

    @Test fun `server about light`() = shoot("settings-server-about-light") { ServerSettingsScreen(onBack = {}) }
    @Test fun `server about dark`() = shoot("settings-server-about-dark", AppearanceChoice.Dark) { ServerSettingsScreen(onBack = {}) }
    @Test fun `server storage light`() = shoot("settings-server-mail-light", before = { composeRule.onNodeWithText("Mail").performClick() }) { ServerSettingsScreen(onBack = {}) }
    @Test fun `server sending dark`() = shoot("settings-server-sending-dark", AppearanceChoice.Dark, before = { composeRule.onNodeWithText("Sending").performClick() }) { ServerSettingsScreen(onBack = {}) }
    @Test fun `usage light`() = shoot("settings-usage-light") { AgentUsageScreen(onBack = {}) }
    @Test fun `usage dark`() = shoot("settings-usage-dark", AppearanceChoice.Dark) { AgentUsageScreen(onBack = {}) }
    @Test fun `usage largest text`() = shoot("settings-usage-largest-text", fontScale = 2f) { AgentUsageScreen(onBack = {}) }

    @Test fun `token dialog light`() = shoot("settings-dialog-token-new-light", before = tap("New token")) { TokensScreen(onBack = {}) }
    @Test fun `source editor light`() = shoot("settings-dialog-source-new-light", before = tap("Add a source")) { KnowledgeSourcesScreen(onBack = {}) }
    @Test fun `schedule editor light`() = shoot("settings-dialog-schedule-new-light", before = tap("New schedule")) { SchedulesScreen(onBack = {}) }
    @Test fun `schedule editor dark`() = shoot("settings-dialog-schedule-new-dark", AppearanceChoice.Dark, before = tap("New schedule")) { SchedulesScreen(onBack = {}) }
    @Test fun `source editor typed light`() = shoot("settings-dialog-source-typed-light", before = { tap("Add a source")(); composeRule.onNodeWithText("Kind of source").performClick(); composeRule.waitForIdle(); composeRule.onNodeWithText("webfeed").performClick() }) { KnowledgeSourcesScreen(onBack = {}) }
    @Test fun `circle new dialog light`() = shoot("settings-dialog-circle-new-light", before = tap("New circle")) { CirclesScreen(onBack = {}, onOpenCircle = {}, onOpenSharing = {}) }
    @Test fun `skill lend dialog light`() = shoot("settings-dialog-skill-lend-light", before = tap("Lend a skill")) { SkillSharingScreen(onBack = {}) }
    @Test fun `circles offer largest text`() = shoot("settings-dialog-circle-new-largest-text", fontScale = 2f, before = tap("New circle")) { CirclesScreen(onBack = {}, onOpenCircle = {}, onOpenSharing = {}) }
}
