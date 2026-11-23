package com.teanode.app.screenshots

import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import com.teanode.app.AppRoot
import com.teanode.app.demo.rememberDemoSession
import com.teanode.app.ui.theme.AppearanceChoice
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** Photographs of the signed-in shell, in demo mode: the bottom bar, the drawer open, the tab bar editor, a part's placeholder. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class ShellScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    private fun draw(look: Look, fontScale: Float = 1f) {
        composeRule.setContent {
            val session = rememberDemoSession()
            session.setAppearance(look.appearance)
            WithFontScale(fontScale) { AppRoot(session) }
        }
        composeRule.waitForIdle()
    }

    private fun openDrawer() {
        composeRule.onNodeWithContentDescription("Open the menu").performClick()
        composeRule.waitForIdle()
    }

    @Test fun `shell light`() { draw(Look.Light); composeRule.photograph("shell-light") }

    @Test fun `shell dark`() { draw(Look.Dark); composeRule.photograph("shell-dark") }

    @Test fun `shell with the drawer open light`() { draw(Look.Light); openDrawer(); composeRule.photograph("shell-drawer-light") }

    @Test fun `shell with the drawer open dark`() { draw(Look.Dark); openDrawer(); composeRule.photograph("shell-drawer-dark") }

    @Test fun `drawer at the largest text`() { draw(Look.Light, fontScale = 2f); openDrawer(); composeRule.photograph("shell-drawer-largest-text-light") }

    @Test fun `tab bar editor`() {
        draw(Look.Light)
        openDrawer()
        composeRule.onNodeWithText("Edit the tab bar").performClick()
        composeRule.waitForIdle()
        composeRule.photograph("shell-tab-bar-editor-light")
    }

    @Test fun `another part opened from the drawer`() {
        draw(Look.Dark)
        openDrawer()
        composeRule.onNodeWithText("Calendar").performClick()
        composeRule.waitForIdle()
        composeRule.photograph("shell-calendar-placeholder-dark")
    }

    @Test fun `appearance choice is read from the session`() {
        draw(Look.Dark)
        check(AppearanceChoice.Dark.storedName == "dark")
    }
}
