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
import com.teanode.kit.manage.ServerSettingsTab
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** The server's settings forms with their pickers, footers and the restart shortcut, in the invented demo account. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class ServerChoicesScreenshotTest {
    @get:Rule val composeRule = createComposeRule()

    private fun shoot(name: String, appearance: AppearanceChoice = AppearanceChoice.Light, fontScale: Float = 1f, before: () -> Unit = {}, content: @Composable () -> Unit) {
        composeRule.setContent { DemoHost(appearance = appearance) { WithFontScale(fontScale) { CompositionLocalProvider(LocalDialogsInline provides true) { content() } } } }
        composeRule.mainClock.advanceTimeBy(500)
        composeRule.waitForIdle()
        before()
        composeRule.waitForIdle()
        composeRule.photograph(name)
    }

    @Test fun `identity has a log level picker and its footers`() = shoot("settings-server-identity-light", before = { composeRule.onNodeWithText("Identity").performClick() }) { ServerSettingsScreen(onBack = {}) }

    @Test fun `the log level picker lists the levels`() = shoot(
        "settings-server-log-level-picker-dark", AppearanceChoice.Dark,
        before = { composeRule.onNodeWithText("Identity").performClick(); composeRule.waitForIdle(); composeRule.onNodeWithText("Log level").performClick(); composeRule.waitForIdle(); composeRule.onNodeWithText("CRITICAL").assertExists() },
    ) { ServerSettingsScreen(onBack = {}) }

    @Test fun `sending has the encryption picker`() = shoot("settings-server-relay-light", before = {}) { ServerSettingsScreen(onBack = {}, initialTab = ServerSettingsTab.Sending) }

    @Test fun `certificates have the challenge picker`() = shoot("settings-server-certificates-dark", AppearanceChoice.Dark, before = {}) { ServerSettingsScreen(onBack = {}, initialTab = ServerSettingsTab.Certificates) }

    @Test fun `scanning has the spam filter picker`() = shoot("settings-server-scanning-light", before = {}) { ServerSettingsScreen(onBack = {}, initialTab = ServerSettingsTab.Scanning) }

    @Test fun `storage names the restart and offers the about page`() = shoot("settings-server-storage-light", before = {}) { ServerSettingsScreen(onBack = {}, initialTab = ServerSettingsTab.Storage) }

    @Test fun `services offer the restart shortcut at the largest text`() = shoot("settings-services-restart-largest-text", fontScale = 2f) { ServerServicesScreen(onBack = {}) }

    @Test fun `services show the shortcut to the about page`() {
        composeRule.setContent { DemoHost { ServerServicesScreen(onBack = {}) } }
        composeRule.mainClock.advanceTimeBy(500)
        composeRule.waitForIdle()
        composeRule.onNodeWithText("Restart it on the About page").assertExists()
    }
}
