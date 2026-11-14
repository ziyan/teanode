package com.teanode.app.coding

import androidx.compose.runtime.Composable
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import com.teanode.app.demo.DemoHost
import com.teanode.app.screenshots.Look
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import com.teanode.app.screenshots.WithFontScale
import com.teanode.app.screenshots.photograph
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Photographs of every Coding screen and state, in demo mode, light and dark, and the dense ones at the largest text. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class CodingScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    private fun draw(look: Look, fontScale: Float = 1f, content: @Composable () -> Unit) {
        composeRule.setContent { WithFontScale(fontScale) { DemoHost(appearance = look.appearance) { content() } } }
        composeRule.waitForIdle()
        composeRule.waitForIdle()
    }

    @Test fun `list light`() { draw(Look.Light) { CodingSection() }; composeRule.photograph("coding-list-light") }

    @Test fun `list dark`() { draw(Look.Dark) { CodingSection() }; composeRule.photograph("coding-list-dark") }

    @Test fun `list at the largest text`() { draw(Look.Light, 2f) { CodingSection() }; composeRule.photograph("coding-list-largest-text-light") }

    @Test fun `list menu`() {
        draw(Look.Light) { CodingSection() }
        composeRule.onNodeWithContentDescription("Coding actions").performClick()
        composeRule.waitForIdle()
        composeRule.photograph("coding-list-menu-light")
    }

    @Test fun `session asking light`() { draw(Look.Light) { CodingSessionScreen("laptop/p2", {}) }; composeRule.photograph("coding-session-asking-light") }

    @Test fun `session asking dark`() { draw(Look.Dark) { CodingSessionScreen("laptop/p2", {}) }; composeRule.photograph("coding-session-asking-dark") }

    @Test fun `session asking at the largest text`() { draw(Look.Light, 2f) { CodingSessionScreen("laptop/p2", {}) }; composeRule.photograph("coding-session-asking-largest-text-light") }

    @Test fun `session working light`() { draw(Look.Light) { CodingSessionScreen("laptop/p1", {}) }; composeRule.photograph("coding-session-working-light") }

    @Test fun `session working dark`() { draw(Look.Dark) { CodingSessionScreen("laptop/p1", {}) }; composeRule.photograph("coding-session-working-dark") }

    @Test fun `session idle light`() { draw(Look.Light) { CodingSessionScreen("desk/p4", {}) }; composeRule.photograph("coding-session-idle-light") }

    @Test fun `session actions menu`() {
        draw(Look.Light) { CodingSessionScreen("laptop/p1", {}) }
        composeRule.onNodeWithContentDescription("Actions").performClick()
        composeRule.waitForIdle()
        composeRule.photograph("coding-session-actions-light")
    }

    @Test fun `session no longer open`() { draw(Look.Light) { CodingSessionScreen("laptop/gone", {}) }; composeRule.photograph("coding-session-gone-light") }

    @Test fun `agents light`() {
        draw(Look.Light) { CodingSessionScreen("laptop/p1", {}) }
        composeRule.onNodeWithContentDescription("Choose what to show").performClick()
        composeRule.onNodeWithText("Agents").performClick()
        composeRule.waitForIdle()
        composeRule.waitForIdle()
        composeRule.photograph("coding-agents-light")
    }

    @Test fun `agents dark`() {
        draw(Look.Dark) { CodingSessionScreen("laptop/p1", {}) }
        composeRule.onNodeWithContentDescription("Choose what to show").performClick()
        composeRule.onNodeWithText("Agents").performClick()
        composeRule.waitForIdle()
        composeRule.waitForIdle()
        composeRule.photograph("coding-agents-dark")
    }

    @Test fun `screen light`() {
        draw(Look.Light) { CodingSessionScreen("laptop/p1", {}) }
        composeRule.onNodeWithContentDescription("Choose what to show").performClick()
        composeRule.onNodeWithText("Screen").performClick()
        composeRule.waitForIdle()
        composeRule.waitForIdle()
        composeRule.photograph("coding-screen-light")
    }

    @Test fun `hooks light`() { draw(Look.Light) { CodingHooksScreen({}) }; composeRule.photograph("coding-hooks-light") }

    @Test fun `hooks installed dark`() {
        draw(Look.Dark) { CodingHooksScreen({}) }
        composeRule.onAllNodesWithText("Install")[0].performClick()
        composeRule.waitForIdle()
        composeRule.waitForIdle()
        composeRule.photograph("coding-hooks-installed-dark")
    }

    @Test fun `preview light`() { draw(Look.Light) { CodingPreviewScreen({}) }; composeRule.photograph("coding-preview-light") }

    @Test fun `open light`() { draw(Look.Light) { CodingOpenScreen({}, { _, _ -> }) }; composeRule.photograph("coding-open-light") }

    @Test fun `open dark`() { draw(Look.Dark) { CodingOpenScreen({}, { _, _ -> }) }; composeRule.photograph("coding-open-dark") }
}

private class DemoDevicePassword(override val passwordCreatedAt: String?, override val passwordLastUsedAt: String?) : com.teanode.kit.passwords.DevicePassword

/** The row the Settings part draws its sync and mail password lists with. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class DevicePasswordRowScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test fun `device password rows`() {
        val now = java.time.Instant.parse("2030-06-01T12:00:00Z").toEpochMilli()
        composeRule.setContent {
            DemoHost(appearance = Look.Light.appearance) {
                androidx.compose.foundation.layout.Column {
                    com.teanode.app.passwords.DevicePasswordRow("Phone", DemoDevicePassword("2030-06-01T11:30:00Z", null), nowEpochMS = now)
                    com.teanode.app.passwords.DevicePasswordRow("Old laptop", DemoDevicePassword("2029-01-01T00:00:00Z", "2030-01-01T00:00:00Z"), scopes = "Calendar and contacts", nowEpochMS = now)
                    com.teanode.app.passwords.DevicePasswordRow("Tablet", DemoDevicePassword("2030-05-20T08:00:00Z", "2030-05-31T08:00:00Z"), nowEpochMS = now)
                }
            }
        }
        composeRule.waitForIdle()
        composeRule.photograph("coding-device-password-rows-light")
    }
}
