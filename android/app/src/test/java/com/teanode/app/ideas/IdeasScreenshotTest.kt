package com.teanode.app.ideas

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

/** Photographs of Ideas in demo mode: ideas for the person, the catalog by category and the history, light, dark and at the largest text. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class IdeasScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    private fun draw(look: Look, fontScale: Float = 1f) {
        composeRule.setContent { DemoHost(appearance = look.appearance) { WithFontScale(fontScale) { IdeasSection() } } }
        composeRule.waitForIdle()
    }

    @Test fun `ideas light`() { draw(Look.Light); composeRule.photograph("ideas-list-light") }

    @Test fun `ideas dark`() { draw(Look.Dark); composeRule.photograph("ideas-list-dark") }

    @Test fun `ideas at the largest text`() { draw(Look.Light, 2f); composeRule.photograph("ideas-list-largest-text-light") }
}
