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

/** Pictures of the "Your agent" screens: Connections, Alerts and follow-ups, and Activity, in the invented demo account. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class SettingsAgentScreenshotTest {
    @get:Rule val composeRule = createComposeRule()

    private fun shoot(name: String, appearance: AppearanceChoice = AppearanceChoice.Light, fontScale: Float = 1f, before: () -> Unit = {}, content: @Composable () -> Unit) {
        composeRule.setContent { DemoHost(appearance = appearance) { WithFontScale(fontScale) { CompositionLocalProvider(LocalDialogsInline provides true) { content() } } } }
        composeRule.mainClock.advanceTimeBy(500)
        composeRule.waitForIdle()
        before()
        composeRule.waitForIdle()
        composeRule.photograph(name)
    }

    private fun scrollTo(text: String) = composeRule.onNode(androidx.compose.ui.test.hasScrollAction()).performScrollToNode(androidx.compose.ui.test.hasText(text))

    private fun tap(text: String): () -> Unit = { composeRule.onNodeWithText(text).performClick() }

    @Test fun `connections light`() = shoot("agent-connections-light") { ConnectionsScreen(onBack = {}) }
    @Test fun `connections dark`() = shoot("agent-connections-dark", AppearanceChoice.Dark) { ConnectionsScreen(onBack = {}) }
    @Test fun `connections largest text`() = shoot("agent-connections-largest-text", fontScale = 2f) { ConnectionsScreen(onBack = {}) }
    @Test fun `connections skills and reach light`() = shoot("agent-connections-reach-light", before = { scrollTo("Reach") }) { ConnectionsScreen(onBack = {}) }
    @Test fun `connections chat apps light`() = shoot("agent-connections-chat-light", before = { scrollTo("Chat apps") }) { ConnectionsScreen(onBack = {}) }
    @Test fun `connections chat apps dark`() = shoot("agent-connections-chat-dark", AppearanceChoice.Dark, before = { scrollTo("Chat apps") }) { ConnectionsScreen(onBack = {}) }
    @Test fun `connections bot token dialog light`() = shoot("agent-connections-token-light", before = { scrollTo("Chat apps"); composeRule.onNodeWithText("Telegram").performClick() }) { ConnectionsScreen(onBack = {}) }
    @Test fun `connections secret dialog light`() = shoot("agent-connections-secret-light", before = { scrollTo("Your keys for skills"); composeRule.onNodeWithText("TRACKER_TOKEN").performClick() }) { ConnectionsScreen(onBack = {}) }

    @Test fun `alerts light`() = shoot("agent-alerts-light") { MutedAlertsScreen(onBack = {}) }
    @Test fun `alerts dark`() = shoot("agent-alerts-dark", AppearanceChoice.Dark) { MutedAlertsScreen(onBack = {}) }
    @Test fun `alerts following light`() = shoot("agent-alerts-following-light", before = { scrollTo("Following") }) { MutedAlertsScreen(onBack = {}) }
    @Test fun `alerts following dark`() = shoot("agent-alerts-following-dark", AppearanceChoice.Dark, before = { scrollTo("Following") }) { MutedAlertsScreen(onBack = {}) }
    @Test fun `alerts largest text`() = shoot("agent-alerts-largest-text", fontScale = 2f) { MutedAlertsScreen(onBack = {}) }
    @Test fun `alerts add a follow-up dialog light`() = shoot("agent-alerts-add-light", before = { composeRule.onNodeWithContentDescription("Add a follow-up").performClick() }) { MutedAlertsScreen(onBack = {}) }

    @Test fun `activity light`() = shoot("agent-activity-light") { AgentActivityScreen(onBack = {}) }
    @Test fun `activity dark`() = shoot("agent-activity-dark", AppearanceChoice.Dark) { AgentActivityScreen(onBack = {}) }
    @Test fun `activity answered for you light`() = shoot("agent-activity-replies-light", before = { scrollTo("Answered for you") }) { AgentActivityScreen(onBack = {}) }
    @Test fun `activity largest text`() = shoot("agent-activity-largest-text", fontScale = 2f) { AgentActivityScreen(onBack = {}) }
    @Test fun `activity work result light`() = shoot("agent-activity-result-light", before = { composeRule.onNodeWithText("Trip ideas").performClick() }) { AgentActivityScreen(onBack = {}) }

    @Test fun `dreams light`() = shoot("agent-dreams-light") { DreamsScreen(onBack = {}) }
    @Test fun `dreams dark`() = shoot("agent-dreams-dark", AppearanceChoice.Dark) { DreamsScreen(onBack = {}) }
    @Test fun `dreams largest text`() = shoot("agent-dreams-largest-text", fontScale = 2f) { DreamsScreen(onBack = {}) }
    @Test fun `sources light`() = shoot("agent-sources-light") { KnowledgeSourcesScreen(onBack = {}) }
    @Test fun `sources dark`() = shoot("agent-sources-dark", AppearanceChoice.Dark) { KnowledgeSourcesScreen(onBack = {}) }
    @Test fun `sources largest text`() = shoot("agent-sources-largest-text", fontScale = 2f) { KnowledgeSourcesScreen(onBack = {}) }
    @Test fun `memory light`() = shoot("agent-memory-light") { AgentMemorySettingsScreen(onBack = {}) }
    @Test fun `memory dark`() = shoot("agent-memory-dark", AppearanceChoice.Dark) { AgentMemorySettingsScreen(onBack = {}) }
    @Test fun `memory scores light`() = shoot("agent-memory-scores-light", before = { scrollTo("Scores") }) { AgentMemorySettingsScreen(onBack = {}) }
    @Test fun `memory learned light`() = shoot("agent-memory-learned-light", before = { scrollTo("Learned lately") }) { AgentMemorySettingsScreen(onBack = {}) }
    @Test fun `memory largest text`() = shoot("agent-memory-largest-text", fontScale = 2f) { AgentMemorySettingsScreen(onBack = {}) }
    @Test fun `sessions with toggle light`() = shoot("agent-sessions-light") { SessionsScreen(onBack = {}) }
    @Test fun `tokens with toggle light`() = shoot("agent-tokens-light") { TokensScreen(onBack = {}) }
}
