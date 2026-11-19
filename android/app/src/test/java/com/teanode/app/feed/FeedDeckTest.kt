package com.teanode.app.feed

import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollToIndex
import androidx.compose.ui.test.getUnclippedBoundsInRoot
import org.junit.Assert.assertEquals
import com.teanode.app.demo.DemoHost
import com.teanode.app.screenshots.PHONE_QUALIFIERS
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/**
 * A stack drawn as a deck: pressing its row fans it out in place, the row turns into "Show Less" and gathers it back, a
 * tap anywhere else in the feed gathers it too, and the top card stays where it is throughout.
 */
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
        composeRule.waitUntil(5_000) { composeRule.onAllNodesWithText("Show Less", useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() }
        composeRule.onNodeWithText("Show Less", useUnmergedTree = true).performClick()
        composeRule.waitForIdle()
        composeRule.mainClock.advanceTimeBy(1_000)
        composeRule.waitForIdle()
        composeRule.onAllNodesWithText("Show Less", useUnmergedTree = true).assertCountEquals(0)
        composeRule.onNodeWithText("+6 more from Morning brief, Wed Nov 19").assertIsDisplayed()
    }

    private val briefTitle = demoBriefCards.first().title

    private fun showBriefDeck() {
        composeRule.setContent { DemoHost { FeedSection() } }
        composeRule.waitUntil(15_000) { composeRule.onAllNodesWithText("New sign-in from a laptop").fetchSemanticsNodes().isNotEmpty() }
        composeRule.onNodeWithTag(FEED_LIST_TAG).performScrollToIndex(demoFeedCards.indexOfFirst { card -> card.feedItemId == FeedDemoIDs.BRIEF_AGENDA })
        composeRule.waitForIdle()
    }

    private fun settle() {
        composeRule.waitForIdle()
        composeRule.mainClock.advanceTimeBy(1_000)
        composeRule.waitForIdle()
    }

    @Test
    fun aTapOutsideAnOpenDeckGathersIt() {
        showBriefDeck()
        composeRule.onNodeWithText("+6 more from Morning brief, Wed Nov 19").performClick()
        settle()
        composeRule.waitUntil(5_000) { composeRule.onAllNodesWithText("Show Less", useUnmergedTree = true).fetchSemanticsNodes().isNotEmpty() }
        // A tap inside the deck, on one of its own cards, leaves it open.
        composeRule.onNodeWithText(demoBriefCards[1].title).performClick()
        settle()
        composeRule.onNodeWithText("Show Less", useUnmergedTree = true).assertIsDisplayed()
        // A tap on the card above it, outside the deck, gathers it.
        val aboveIndex = demoFeedCards.indexOfFirst { card -> card.feedItemId == FeedDemoIDs.BRIEF_AGENDA } - 1
        composeRule.onNodeWithTag(FEED_LIST_TAG).performScrollToIndex(aboveIndex)
        composeRule.waitForIdle()
        composeRule.onNodeWithText(demoFeedCards[aboveIndex].title).performClick()
        settle()
        composeRule.onAllNodesWithText("Show Less", useUnmergedTree = true).assertCountEquals(0)
    }

    @Test
    fun theTopCardStaysWhereItIsWhileTheDeckFansOut() {
        showBriefDeck()
        val before = composeRule.onNodeWithText(briefTitle).getUnclippedBoundsInRoot().top
        composeRule.mainClock.autoAdvance = false
        composeRule.onNodeWithText("+6 more from Morning brief, Wed Nov 19").performClick()
        // Halfway through the motion, and once it is over.
        var frameCount = 0
        while (composeRule.onAllNodesWithText("Show Less", useUnmergedTree = true).fetchSemanticsNodes().isEmpty() && frameCount < 200) {
            composeRule.mainClock.advanceTimeByFrame()
            frameCount += 1
        }
        composeRule.mainClock.advanceTimeBy(160)
        assertEquals(before.value, composeRule.onNodeWithText(briefTitle).getUnclippedBoundsInRoot().top.value, 0.5f)
        composeRule.mainClock.advanceTimeBy(1_000)
        assertEquals(before.value, composeRule.onNodeWithText(briefTitle).getUnclippedBoundsInRoot().top.value, 0.5f)
        composeRule.mainClock.autoAdvance = true
    }
}
