package com.teanode.app.settings

import androidx.compose.runtime.Composable
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
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
        composeRule.setContent { DemoHost(appearance = appearance) { WithFontScale(fontScale) { content() } } }
        composeRule.mainClock.advanceTimeBy(500)
        composeRule.waitForIdle()
        before()
        composeRule.waitForIdle()
        composeRule.photograph(name)
    }

    @Test fun `home light`() = shoot("settings-home-light") { SettingsSection() }
    @Test fun `home dark`() = shoot("settings-home-dark", AppearanceChoice.Dark) { SettingsSection() }
    @Test fun `home largest text`() = shoot("settings-home-largest-text", fontScale = 2f) { SettingsSection() }

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
    @Test fun `new token light`() = shoot("settings-tokens-new-light", before = { composeRule.mainClock.autoAdvance = false; composeRule.onNodeWithContentDescription("New token").performClick(); composeRule.mainClock.advanceTimeBy(500) }) { TokensScreen(onBack = {}) }

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
}
