package com.teanode.app.search

import androidx.compose.runtime.Composable
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.performTextInput
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

/** Pictures of the Search screen: empty, with words that name places, and with what the invented server finds. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class SearchScreenshotTest {
    @get:Rule val composeRule = createComposeRule()

    private fun shoot(name: String, typed: String? = null, appearance: AppearanceChoice = AppearanceChoice.Light, fontScale: Float = 1f) {
        composeRule.setContent { DemoHost(appearance = appearance) { WithFontScale(fontScale) { SearchSection() } } }
        composeRule.waitForIdle()
        if (typed != null) {
            composeRule.onNode(hasSetTextAction()).performTextInput(typed)
            composeRule.mainClock.advanceTimeBy(1000)
        }
        composeRule.waitForIdle()
        composeRule.photograph(name)
    }

    @Test fun `empty light`() = shoot("search-empty-light")
    @Test fun `empty dark`() = shoot("search-empty-dark", appearance = AppearanceChoice.Dark)
    @Test fun `results light`() = shoot("search-results-light", "harbor")
    @Test fun `results dark`() = shoot("search-results-dark", "harbor", AppearanceChoice.Dark)
    @Test fun `results largest text`() = shoot("search-results-largest-text", "harbor", fontScale = 2f)
    @Test fun `places light`() = shoot("search-places-light", "pass")
}
