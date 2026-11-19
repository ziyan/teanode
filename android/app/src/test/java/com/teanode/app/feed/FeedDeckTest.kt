package com.teanode.app.feed

import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToIndex
import com.teanode.app.demo.DemoHost
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/** A stack drawn as a deck: pressing its row fans it out in place and "Show less" gathers it back. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class FeedDeckTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test
    fun aDeckFansOutInPlaceAndGathersBack() {
        composeRule.setContent { DemoHost { FeedSection() } }
        composeRule.waitUntil(15_000) { composeRule.onAllNodesWithText("New sign-in from a laptop").fetchSemanticsNodes().isNotEmpty() }
        composeRule.onNodeWithTag(FEED_LIST_TAG).performScrollToIndex(demoFeedCards.indexOfFirst { card -> card.feedItemId == FeedDemoIDs.BRIEF_AGENDA })
        composeRule.waitForIdle()
        composeRule.onNodeWithText("+6 more from Morning brief, Wed Nov 19").performClick()
        composeRule.waitForIdle()
        composeRule.mainClock.advanceTimeBy(1_000)
        composeRule.waitForIdle()
        composeRule.waitUntil(5_000) { composeRule.onAllNodesWithText("Show less", useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() }
        composeRule.onNodeWithText("Show less", useUnmergedTree = true).performClick()
        composeRule.waitForIdle()
        composeRule.mainClock.advanceTimeBy(1_000)
        composeRule.waitForIdle()
        composeRule.onAllNodesWithText("Show less", useUnmergedTree = true).assertCountEquals(0)
        composeRule.onNodeWithText("+6 more from Morning brief, Wed Nov 19").assertIsDisplayed()
    }
}
