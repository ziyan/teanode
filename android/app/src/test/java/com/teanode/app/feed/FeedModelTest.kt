package com.teanode.app.feed

import android.content.Context
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.core.app.ApplicationProvider
import com.teanode.app.demo.DemoHost
import com.teanode.app.demo.DemoSession
import com.teanode.kit.feed.AgentFeedChanged
import com.teanode.kit.feed.agentFeed
import com.teanode.kit.feed.agentFeedStack
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/** FeedModel against the demo server, and the choices of a card drawn in a screen. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class FeedModelTest {
    @get:Rule
    val composeRule = createComposeRule()

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
    private val unreadCounts = ArrayList<Int>()

    @After
    fun stop() {
        scope.coroutineContext[kotlinx.coroutines.Job]?.cancel()
    }

    private fun stackModel(feedStackKey: String): FeedModel {
        val session = DemoSession.create(ApplicationProvider.getApplicationContext<Context>())
        val connection = session.connection.value!!
        return FeedModel(connection, scope, readPage = { after -> connection.agentFeedStack(feedStackKey, after = after) }, isStackScreen = true, onUnreadCount = { }, onProblem = { })
    }

    private fun model(): FeedModel {
        val session = DemoSession.create(ApplicationProvider.getApplicationContext<Context>())
        val connection = session.connection.value!!
        return FeedModel(connection, scope, readPage = { after -> connection.agentFeed(after = after) }, onUnreadCount = { count -> unreadCounts.add(count) }, onProblem = { })
    }

    @Test
    fun theFirstPageLoadsAndTheUnreadCountIsReported() {
        val feed = model()
        feed.load()
        val list = feed.list
        assertNotNull(list)
        assertEquals(demoFeedCards.map { card -> card.feedItemId }, list!!.feedItems.map { card -> card.feedItemId })
        assertEquals(demoFeedCards.count { card -> !card.isRead }, unreadCounts.last())
    }

    @Test
    fun aCardSeenIsMarkedReadAfterAMoment() {
        val feed = model()
        feed.load()
        val before = feed.list!!.unreadCount
        feed.noteSeen(FeedDemoIDs.SIGN_IN)
        assertTrue(feed.list!!.isUnread(FeedDemoIDs.SIGN_IN))
        runBlocking { kotlinx.coroutines.delay(FEED_SEEN_DELAY_MS + 400) }
        assertFalse(feed.list!!.isUnread(FeedDemoIDs.SIGN_IN))
        // The demo server says one card was newly read.
        assertEquals(before - 1, feed.list!!.unreadCount)
    }

    @Test
    fun aDismissedChangeTakesTheCardOut() {
        val feed = model()
        feed.load()
        runBlocking { feed.apply(AgentFeedChanged(FeedDemoIDs.WEATHER, "dismissed", 2)) }
        assertFalse(feed.list!!.shows(FeedDemoIDs.WEATHER))
        assertEquals(2, feed.list!!.unreadCount)
    }

    @Test
    fun aReplacedChangeReadsTheCardAgainAndPutsItAtTheTop() {
        val feed = model()
        feed.load()
        runBlocking { feed.apply(AgentFeedChanged(FeedDemoIDs.WEATHER, "replaced", 4)) }
        assertEquals(FeedDemoIDs.WEATHER, feed.list!!.feedItems.first().feedItemId)
        assertEquals(4, feed.list!!.unreadCount)
    }

    @Test
    fun aSettledChangeKeepsTheCardWhereItIs() {
        val feed = model()
        feed.load()
        val position = feed.list!!.feedItems.indexOfFirst { card -> card.feedItemId == FeedDemoIDs.PENDING }
        runBlocking { feed.apply(AgentFeedChanged(FeedDemoIDs.PENDING, "settled", 3)) }
        assertEquals(position, feed.list!!.feedItems.indexOfFirst { card -> card.feedItemId == FeedDemoIDs.PENDING })
    }

    @Test
    fun aCardJoiningAStackReadsTheListAgainInsteadOfSplicingIt() {
        val feed = model()
        feed.load()
        runBlocking { feed.apply(AgentFeedChanged(FeedDemoIDs.NEWS, "posted", 6)) }
        val robots = feed.list!!.feedItems.first { card -> card.feedItemId == FeedDemoIDs.NEWS }
        assertEquals(4, robots.stackItemCount)
        assertEquals(6, feed.list!!.unreadCount)
    }

    @Test
    fun theBriefStackKeepsTheServersOrder() {
        val feed = stackModel(FeedDemoIDs.BRIEF_STACK_KEY)
        feed.load()
        assertEquals(
            listOf(FeedDemoIDs.BRIEF_AGENDA, FeedDemoIDs.BRIEF_WEATHER, FeedDemoIDs.BRIEF_TECH, FeedDemoIDs.BRIEF_HEADLINES, FeedDemoIDs.BRIEF_MENTIONS, FeedDemoIDs.BRIEF_REVIEWS, FeedDemoIDs.BRIEF_MAIL),
            feed.list!!.feedItems.map { card -> card.feedItemId },
        )
        assertEquals("Morning brief, Wed Nov 19", feed.list!!.feedItems.first().stackName)
    }

    @Test
    fun aChangeInAStackScreenReadsItsPageAgain() {
        val feed = stackModel(FeedDemoIDs.BRIEF_STACK_KEY)
        feed.load()
        runBlocking { feed.apply(AgentFeedChanged(FeedDemoIDs.BRIEF_MAIL, "replaced", 3)) }
        // Read again from the server, which keeps the order; the card did not jump to the top.
        assertEquals(FeedDemoIDs.BRIEF_AGENDA, feed.list!!.feedItems.first().feedItemId)
    }

    @Test
    fun anActionChoiceShowsWorkingOnItWithAnOpenChatButton() {
        composeRule.setContent { DemoHost { FeedItemScreen(FeedDemoIDs.SIGN_IN, onBack = {}) } }
        composeRule.onNodeWithText("Secure my account").performClick()
        composeRule.waitForIdle()
        composeRule.onNodeWithText("You chose Secure my account").assertIsDisplayed()
        composeRule.onNodeWithText("Working on it").assertIsDisplayed()
        composeRule.onNodeWithText("Open chat").assertIsDisplayed()
    }

    @Test
    fun aDismissChoiceClosesTheCard() {
        var isClosed = false
        composeRule.setContent { DemoHost { FeedItemScreen(FeedDemoIDs.SIGN_IN, onBack = { isClosed = true }) } }
        composeRule.onNodeWithText("That's me").performClick()
        composeRule.waitForIdle()
        assertTrue(isClosed)
    }
}
