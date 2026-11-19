package com.teanode.app.feed

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onFirst
import androidx.compose.ui.test.performClick
import com.teanode.app.AppRoot
import com.teanode.app.demo.rememberDemoSession
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import com.teanode.app.screenshots.photograph

/** Discuss on a card opens the main conversation with the card on a chip in its box, as on the dashboard. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = "w393dp-h852dp-xxhdpi")
class FeedDiscussTest {
    @get:Rule
    val composeRule = createComposeRule()

    @Test
    fun discussOpensTheMainChatWithTheCardAttached() {
        composeRule.setContent { AppRoot(rememberDemoSession()) }
        val first = demoFeedCards.first()
        composeRule.waitUntil(15_000) { composeRule.onAllNodesWithText(first.title).fetchSemanticsNodes().isNotEmpty() }
        composeRule.onAllNodesWithText("Discuss").onFirst().performClick()
        composeRule.waitUntil(15_000) { composeRule.onAllNodesWithText("Card: ${first.title}").fetchSemanticsNodes().isNotEmpty() }
        composeRule.onAllNodesWithText("Card: ${first.title}").onFirst().assertIsDisplayed()
        composeRule.photograph("feed-discuss-chip-light")
    }
}
