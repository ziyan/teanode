package com.teanode.app.feed

import androidx.compose.ui.test.junit4.createComposeRule
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

/** Photographs of the Dismissed screen in demo mode: light, dark and at the largest text. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class FeedDismissedScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    private fun drawDismissed(look: Look, fontScale: Float = 1f) {
        composeRule.setContent { DemoHost(appearance = look.appearance) { WithFontScale(fontScale) { FeedDismissedScreen(onBack = {}) } } }
        composeRule.waitForIdle()
    }

    @Test fun `dismissed cards light`() { drawDismissed(Look.Light); composeRule.photograph("feed-dismissed-light") }

    @Test fun `dismissed cards dark`() { drawDismissed(Look.Dark); composeRule.photograph("feed-dismissed-dark") }

    @Test fun `dismissed cards at the largest text`() { drawDismissed(Look.Light, 2f); composeRule.photograph("feed-dismissed-largest-text-light") }
}
