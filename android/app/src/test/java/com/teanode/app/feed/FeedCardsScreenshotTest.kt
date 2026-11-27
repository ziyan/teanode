package com.teanode.app.feed

import androidx.compose.ui.test.junit4.createComposeRule
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

/** Photographs of the cards that carry a resolution, a chart, a camera still or a folded answer, light, dark and at the largest text. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class FeedCardsScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    private fun drawCard(feedItemId: String, look: Look, fontScale: Float = 1f) {
        composeRule.setContent { DemoHost(appearance = look.appearance) { WithFontScale(fontScale) { FeedCardHost(feedItemId) } } }
        composeRule.waitForIdle()
    }

    @Test fun `resolved card light`() { drawCard(FeedDemoIDs.RESOLVED, Look.Light); composeRule.photograph("feed-cards-resolved-light") }

    @Test fun `resolved card dark`() { drawCard(FeedDemoIDs.RESOLVED, Look.Dark); composeRule.photograph("feed-cards-resolved-dark") }

    @Test fun `resolved card at the largest text`() { drawCard(FeedDemoIDs.RESOLVED, Look.Light, 2f); composeRule.photograph("feed-cards-resolved-largest-text-light") }

    @Test fun `series chart light`() { drawCard(FeedDemoIDs.CHART_SERIES, Look.Light); composeRule.photograph("feed-cards-chart-series-light") }

    @Test fun `series chart dark`() { drawCard(FeedDemoIDs.CHART_SERIES, Look.Dark); composeRule.photograph("feed-cards-chart-series-dark") }

    @Test fun `series chart at the largest text`() { drawCard(FeedDemoIDs.CHART_SERIES, Look.Light, 2f); composeRule.photograph("feed-cards-chart-series-largest-text-light") }

    @Test fun `bars chart light`() { drawCard(FeedDemoIDs.CHART_BARS, Look.Light); composeRule.photograph("feed-cards-chart-bars-light") }

    @Test fun `bars chart dark`() { drawCard(FeedDemoIDs.CHART_BARS, Look.Dark); composeRule.photograph("feed-cards-chart-bars-dark") }

    @Test fun `bars chart at the largest text`() { drawCard(FeedDemoIDs.CHART_BARS, Look.Light, 2f); composeRule.photograph("feed-cards-chart-bars-largest-text-light") }

    @Test fun `camera still light`() { drawCard(FeedDemoIDs.CAMERA_STILL, Look.Light); composeRule.photograph("feed-cards-camera-still-light") }

    @Test fun `camera still dark`() { drawCard(FeedDemoIDs.CAMERA_STILL, Look.Dark); composeRule.photograph("feed-cards-camera-still-dark") }

    @Test fun `camera still at the largest text`() { drawCard(FeedDemoIDs.CAMERA_STILL, Look.Light, 2f); composeRule.photograph("feed-cards-camera-still-largest-text-light") }

    @Test fun `folded answered card light`() { drawCard(FeedDemoIDs.ANSWERED_FOLDED, Look.Light); composeRule.photograph("feed-cards-answered-folded-light") }

    @Test fun `folded answered card dark`() { drawCard(FeedDemoIDs.ANSWERED_FOLDED, Look.Dark); composeRule.photograph("feed-cards-answered-folded-dark") }

    @Test fun `folded answered card at the largest text`() { drawCard(FeedDemoIDs.ANSWERED_FOLDED, Look.Light, 2f); composeRule.photograph("feed-cards-answered-folded-largest-text-light") }

    @Test fun `folded answered card opens`() {
        drawCard(FeedDemoIDs.ANSWERED_FOLDED, Look.Light)
        composeRule.onNodeWithText("Show the card").performClick()
        composeRule.waitForIdle()
        composeRule.photograph("feed-cards-answered-opened-light")
    }
}
