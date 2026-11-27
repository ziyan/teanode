package com.teanode.app.settings.manage

import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToNode
import androidx.compose.ui.test.hasScrollAction
import androidx.compose.ui.test.hasText
import com.teanode.app.demo.DemoHost
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import com.teanode.app.screenshots.WithFontScale
import com.teanode.app.screenshots.photograph
import com.teanode.app.settings.AuditLogScreen
import com.teanode.app.settings.LocalDialogsInline
import com.teanode.app.settings.PeopleScreen
import com.teanode.app.settings.ServerSettingsScreen
import com.teanode.app.ui.theme.AppearanceChoice
import com.teanode.kit.access.AuditEvent
import com.teanode.kit.TeaNodeJson
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Pictures of the operator's administration in the invented demo account: the agents page and everything it opens, a person's page, an audit event and the release notes. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class ManageAdminScreenshotTest {
    @get:Rule val composeRule = createComposeRule()

    private fun shoot(name: String, appearance: AppearanceChoice = AppearanceChoice.Light, fontScale: Float = 1f, before: () -> Unit = {}, content: @Composable () -> Unit) {
        composeRule.setContent { DemoHost(appearance = appearance) { WithFontScale(fontScale) { CompositionLocalProvider(LocalDialogsInline provides true) { content() } } } }
        composeRule.mainClock.advanceTimeBy(800)
        composeRule.waitForIdle()
        before()
        composeRule.waitForIdle()
        composeRule.photograph(name)
    }

    private fun scrollTo(text: String) {
        composeRule.onNode(hasScrollAction()).performScrollToNode(hasText(text, substring = true))
    }

    private fun click(text: String) {
        composeRule.onAllNodesWithText(text, substring = true)[0].performClick()
        composeRule.mainClock.advanceTimeBy(500)
        composeRule.waitForIdle()
    }

    @Composable private fun providers() = AgentProvidersScreen(onBack = {}, onOpenProvider = {}, onOpenModel = { _, _ -> }, onAddModel = {})

    @Test fun `agents hub light`() = shoot("manage-agents-hub-light") { AgentAdminHubScreen(onBack = {}, onOpen = {}) }
    @Test fun `agents hub dark`() = shoot("manage-agents-hub-dark", AppearanceChoice.Dark) { AgentAdminHubScreen(onBack = {}, onOpen = {}) }
    @Test fun `agents hub largest text`() = shoot("manage-agents-hub-largest-text", fontScale = 2f) { AgentAdminHubScreen(onBack = {}, onOpen = {}) }

    @Test fun `personal agents light`() = shoot("manage-personal-light") { AgentPersonalSettingsScreen(onBack = {}) }
    @Test fun `personal agents dark`() = shoot("manage-personal-dark", AppearanceChoice.Dark) { AgentPersonalSettingsScreen(onBack = {}) }

    @Test fun `providers light`() = shoot("manage-providers-light") { providers() }
    @Test fun `providers dark`() = shoot("manage-providers-dark", AppearanceChoice.Dark) { providers() }
    @Test fun `providers largest text`() = shoot("manage-providers-largest-text", fontScale = 2f) { providers() }

    @Test fun `model light`() = shoot("manage-model-light") { AgentModelScreen(providerIndex = 0, model = "model-large", onBack = {}) }
    @Test fun `model dark`() = shoot("manage-model-dark", AppearanceChoice.Dark) { AgentModelScreen(providerIndex = 0, model = "model-large", onBack = {}) }
    @Test fun `model largest text`() = shoot("manage-model-largest-text", fontScale = 2f) { AgentModelScreen(providerIndex = 0, model = "model-small", onBack = {}) }
    @Test fun `model with its own window light`() = shoot("manage-model-window-light") { AgentModelScreen(providerIndex = 0, model = "model-small", onBack = {}) }
    @Test fun `model used for light`() = shoot("manage-model-used-for-light", before = { scrollTo("Used for") }) { AgentModelScreen(providerIndex = 0, model = "model-large", onBack = {}) }
    @Test fun `model used for dark`() = shoot("manage-model-used-for-dark", AppearanceChoice.Dark, before = { scrollTo("Used for") }) { AgentModelScreen(providerIndex = 0, model = "model-large", onBack = {}) }
    @Test fun `add model light`() = shoot("manage-add-model-light") { AgentAddModelScreen(providerIndex = 0, onBack = {}, onAdded = {}) }
    @Test fun `add model dark`() = shoot("manage-add-model-dark", AppearanceChoice.Dark) { AgentAddModelScreen(providerIndex = 0, onBack = {}, onAdded = {}) }
    @Test fun `type a model id`() = shoot("manage-type-model-id-light", before = { click("Type an id") }) { AgentAddModelScreen(providerIndex = 0, onBack = {}, onAdded = {}) }
    @Test fun `role picker light`() = shoot("manage-role-picker-light", before = { click("Default") }) { AgentModelsScreen(onBack = {}) }

    @Test fun `provider editor light`() = shoot("manage-provider-editor-light") { AgentProviderFormScreen(providerIndex = 0, onBack = {}) }
    @Test fun `provider editor dark`() = shoot("manage-provider-editor-dark", AppearanceChoice.Dark) { AgentProviderFormScreen(providerIndex = 0, onBack = {}) }
    @Test fun `provider editor prices`() = shoot("manage-provider-prices-light", before = { scrollTo("Prices from the bill") }) { AgentProviderFormScreen(providerIndex = 0, onBack = {}) }
    @Test fun `provider editor prices dark`() = shoot("manage-provider-prices-dark", AppearanceChoice.Dark, before = { scrollTo("Prices from the bill") }) { AgentProviderFormScreen(providerIndex = 0, onBack = {}) }
    @Test fun `provider new light`() = shoot("manage-provider-new-light") { AgentProviderFormScreen(providerIndex = null, onBack = {}) }
    @Test fun `provider signed in light`() = shoot("manage-provider-signed-in-light") { AgentProviderFormScreen(providerIndex = 1, onBack = {}) }
    @Test fun `provider sign in code`() = shoot("manage-provider-sign-in-code-light", before = { click("Sign in again") }) { AgentProviderFormScreen(providerIndex = 1, onBack = {}) }

    @Test fun `models light`() = shoot("manage-models-light") { AgentModelsScreen(onBack = {}) }
    @Test fun `models dark`() = shoot("manage-models-dark", AppearanceChoice.Dark) { AgentModelsScreen(onBack = {}) }
    @Test fun `models transcription`() = shoot("manage-models-transcription-light", before = { scrollTo("Recording transcription") }) { AgentModelsScreen(onBack = {}) }
    @Test fun `models picker`() = shoot("manage-models-picker-light", before = { click("Fast") }) { AgentModelsScreen(onBack = {}) }
    @Test fun `models largest text`() = shoot("manage-models-largest-text", fontScale = 2f) { AgentModelsScreen(onBack = {}) }

    @Test fun `voice light`() = shoot("manage-voice-light") { AgentVoiceScreen(onBack = {}) }
    @Test fun `voice dark`() = shoot("manage-voice-dark", AppearanceChoice.Dark) { AgentVoiceScreen(onBack = {}) }
    @Test fun `recording light`() = shoot("manage-recording-light") { AgentRecordingScreen(onBack = {}) }
    @Test fun `recording dark`() = shoot("manage-recording-dark", AppearanceChoice.Dark) { AgentRecordingScreen(onBack = {}) }
    @Test fun `features light`() = shoot("manage-features-light") { AgentFeaturesScreen(onBack = {}) }
    @Test fun `features dark`() = shoot("manage-features-dark", AppearanceChoice.Dark) { AgentFeaturesScreen(onBack = {}) }
    @Test fun `features largest text`() = shoot("manage-features-largest-text", fontScale = 2f) { AgentFeaturesScreen(onBack = {}) }
    @Test fun `limits light`() = shoot("manage-limits-light") { AgentLimitsScreen(onBack = {}) }
    @Test fun `limits dark`() = shoot("manage-limits-dark", AppearanceChoice.Dark) { AgentLimitsScreen(onBack = {}) }
    @Test fun `limits recording quotas`() = shoot("manage-limits-recording-light", before = { scrollTo("Recording storage per account") }) { AgentLimitsScreen(onBack = {}) }

    @Test fun `tool policy light`() = shoot("manage-tool-policy-light") { AgentToolPolicyScreen(onBack = {}) }
    @Test fun `tool policy dark`() = shoot("manage-tool-policy-dark", AppearanceChoice.Dark) { AgentToolPolicyScreen(onBack = {}) }
    @Test fun `tool policy tools`() = shoot("manage-tool-policy-tools-light", before = { click("Each tool") }) { AgentToolPolicyScreen(onBack = {}) }
    @Test fun `web search light`() = shoot("manage-web-search-light") { AgentWebSearchScreen(onBack = {}) }
    @Test fun `browser light`() = shoot("manage-browser-light") { AgentBrowserScreen(onBack = {}) }
    @Test fun `browser dark`() = shoot("manage-browser-dark", AppearanceChoice.Dark) { AgentBrowserScreen(onBack = {}) }
    @Test fun `finance light`() = shoot("manage-finance-light") { AgentFinanceScreen(onBack = {}) }
    @Test fun `finance dark`() = shoot("manage-finance-dark", AppearanceChoice.Dark) { AgentFinanceScreen(onBack = {}) }

    @Test fun `connected servers light`() = shoot("manage-servers-light") { ConnectedServersScreen(onBack = {}, onOpenServer = {}) }
    @Test fun `connected servers dark`() = shoot("manage-servers-dark", AppearanceChoice.Dark) { ConnectedServersScreen(onBack = {}, onOpenServer = {}) }
    @Test fun `connected servers largest text`() = shoot("manage-servers-largest-text", fontScale = 2f) { ConnectedServersScreen(onBack = {}, onOpenServer = {}) }
    @Test fun `connected server http light`() = shoot("manage-server-http-light") { ConnectedServerFormScreen(serverIndex = 0, onBack = {}) }
    @Test fun `connected server stdio dark`() = shoot("manage-server-stdio-dark", AppearanceChoice.Dark) { ConnectedServerFormScreen(serverIndex = 1, onBack = {}) }
    @Test fun `connected server new light`() = shoot("manage-server-new-light") { ConnectedServerFormScreen(serverIndex = null, onBack = {}) }

    @Test fun `skills light`() = shoot("manage-skills-light") { AgentSkillsScreen(onBack = {}, onOpenSkill = {}) }
    @Test fun `skills dark`() = shoot("manage-skills-dark", AppearanceChoice.Dark) { AgentSkillsScreen(onBack = {}, onOpenSkill = {}) }
    @Test fun `skills registry`() = shoot("manage-skills-registry-light", before = { scrollTo("What the registry offers") }) { AgentSkillsScreen(onBack = {}, onOpenSkill = {}) }
    @Test fun `skills largest text`() = shoot("manage-skills-largest-text", fontScale = 2f) { AgentSkillsScreen(onBack = {}, onOpenSkill = {}) }
    @Test fun `skill detail light`() = shoot("manage-skill-detail-light") { InstalledSkillScreen("cameras", onBack = {}) }
    @Test fun `skill detail dark`() = shoot("manage-skill-detail-dark", AppearanceChoice.Dark) { InstalledSkillScreen("cameras", onBack = {}) }
    @Test fun `skill keys`() = shoot("manage-skill-keys-light", before = { click("1 of 1 set") }) { InstalledSkillScreen("weather", onBack = {}) }
    @Test fun `skill unreadable`() = shoot("manage-skill-unreadable-light") { InstalledSkillScreen("broken-skill", onBack = {}) }

    @Test fun `source types light`() = shoot("manage-source-types-light") { AgentSourceTypesScreen(onBack = {}) }
    @Test fun `source types dark`() = shoot("manage-source-types-dark", AppearanceChoice.Dark) { AgentSourceTypesScreen(onBack = {}) }
    @Test fun `source type guide`() = shoot("manage-source-type-guide-light", before = { click("A folder of notes.") }) { AgentSourceTypesScreen(onBack = {}) }

    @Test fun `runs light`() = shoot("manage-runs-light") { AgentRunsAdminScreen(onBack = {}, onOpenConversation = {}) }
    @Test fun `runs dark`() = shoot("manage-runs-dark", AppearanceChoice.Dark) { AgentRunsAdminScreen(onBack = {}, onOpenConversation = {}) }
    @Test fun `runs largest text`() = shoot("manage-runs-largest-text", fontScale = 2f) { AgentRunsAdminScreen(onBack = {}, onOpenConversation = {}) }

    @Test fun `person light`() = shoot("manage-person-light") { PersonDetailScreen("user-alex", onBack = {}) }
    @Test fun `person dark`() = shoot("manage-person-dark", AppearanceChoice.Dark) { PersonDetailScreen("user-alex", onBack = {}) }
    @Test fun `person largest text`() = shoot("manage-person-largest-text", fontScale = 2f) { PersonDetailScreen("user-alex", onBack = {}) }
    @Test fun `person add to circle`() = shoot("manage-person-add-to-circle-light", before = { scrollTo("Add to a circle"); click("Add to a circle") }) { PersonDetailScreen("user-alex", onBack = {}) }
    @Test fun `person disabled and no groups`() = shoot("manage-person-disabled-light") { PersonDetailScreen("user-pat", onBack = {}) }
    @Test fun `people list light`() = shoot("manage-people-light") { PeopleScreen(onBack = {}, onOpenPerson = {}) }

    private val auditEvent: AuditEvent = TeaNodeJson.decodeFromString(
        AuditEvent.serializer(),
        """{"id":"e9","createdAt":"2030-01-05T09:00:00Z","actorKind":"agent","actorLabel":"Robin Example","impersonatorLabel":"operator","sourceIp":"192.0.2.20","instance":"server-1",
          "resourceType":"user","resourceId":"user-sam","resourceLabel":"sam","action":"update","before":{"name":"","email":"sam@example.com","disabled":false},"after":{"name":"Sam Sample","email":"sam@example.org","disabled":true}}""",
    )
    @Test fun `audit event light`() = shoot("manage-audit-event-light") { AuditEventDetailScreen(auditEvent, onBack = {}) }
    @Test fun `audit event dark`() = shoot("manage-audit-event-dark", AppearanceChoice.Dark) { AuditEventDetailScreen(auditEvent, onBack = {}) }
    @Test fun `audit event largest text`() = shoot("manage-audit-event-largest-text", fontScale = 2f) { AuditEventDetailScreen(auditEvent, onBack = {}) }
    @Test fun `audit log opens an event`() = shoot("manage-audit-log-open-light", before = { click("Robin Example") }) { AuditLogScreen(onBack = {}) }

    @Test fun `release notes light`() = shoot("manage-release-notes-light") { ReleaseNotesScreen(onBack = {}) }
    @Test fun `release notes dark`() = shoot("manage-release-notes-dark", AppearanceChoice.Dark) { ReleaseNotesScreen(onBack = {}) }
    @Test fun `server about with release notes`() = shoot("manage-server-about-light") { ServerSettingsScreen(onBack = {}) }
}
