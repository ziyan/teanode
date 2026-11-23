package com.teanode.app.feed

import android.content.Context
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performScrollToNode
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.core.app.ApplicationProvider
import com.teanode.app.demo.DemoHost
import com.teanode.app.demo.DemoSession
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
import org.junit.Assert.assertNull
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
        return FeedModel(connection, scope, readPage = { after -> connection.agentFeedStack(feedStackKey, after = after) })
    }

    private fun model(): FeedModel {
        val session = DemoSession.create(ApplicationProvider.getApplicationContext<Context>())
        val connection = session.connection.value!!
        return FeedModel(connection, scope, readPage = { after -> connection.agentFeed(after = after) }, onUnreadCount = { count -> unreadCounts.add(count) })
    }

    @Test
    fun theFirstPageLoadsAndTheUnreadCountIsReported() {
        val feed = model()
        runBlocking { feed.load() }
        val list = feed.list
        assertNotNull(list)
        assertEquals(demoFeedCards.map { card -> card.feedItemId }, list!!.feedItems.map { card -> card.feedItemId })
        assertEquals(demoFeedCards.count { card -> !card.isRead }, unreadCounts.last())
    }

    @Test
    fun aCardSeenIsReportedReadAfterAMoment() {
        val feed = model()
        runBlocking { feed.load() }
        val before = feed.list!!.unreadCount
        feed.noteAppeared(feed.list!!.feedItems.first { card -> card.feedItemId == FeedDemoIDs.SIGN_IN })
        assertTrue(feed.list!!.isUnread(FeedDemoIDs.SIGN_IN))
        runBlocking { kotlinx.coroutines.delay(FEED_READ_DELAY_MS + 500) }
        assertFalse(feed.list!!.isUnread(FeedDemoIDs.SIGN_IN))
        // The demo server says one card was newly read.
        assertEquals(before - 1, feed.list!!.unreadCount)
    }

    @Test
    fun aCardAlreadyReportedIsNotReportedAgain() {
        val feed = model()
        runBlocking { feed.load() }
        val card = feed.list!!.feedItems.first { each -> each.feedItemId == FeedDemoIDs.SIGN_IN }
        feed.noteAppeared(card)
        runBlocking { kotlinx.coroutines.delay(FEED_READ_DELAY_MS + 500) }
        val countAfterFirst = feed.list!!.unreadCount
        // A fresh read gives the unread copy again; the model knows the server was told.
        runBlocking { feed.load() }
        feed.noteAppeared(feed.list!!.feedItems.first { each -> each.feedItemId == FeedDemoIDs.SIGN_IN })
        runBlocking { kotlinx.coroutines.delay(FEED_READ_DELAY_MS + 500) }
        assertEquals(demoFeedCards.count { each -> !each.isRead }, feed.list!!.unreadCount)
        assertTrue(countAfterFirst < feed.list!!.unreadCount)
    }

    @Test
    fun anAnsweredCardStaysOnScreenSettledOnItsChoice() {
        val feed = model()
        runBlocking { feed.load() }
        val card = feed.list!!.feedItems.first { each -> each.feedItemId == FeedDemoIDs.SIGN_IN }
        feed.choose(card, card.choices.first { choice -> choice.choiceKey == "me" })
        val after = feed.list!!.feedItems.first { each -> each.feedItemId == FeedDemoIDs.SIGN_IN }
        assertEquals("me", after.chosenChoiceKey)
        assertEquals(demoFeedCards.size, feed.list!!.feedItems.size)
        assertFalse(feed.busyFeedItemIds.containsKey(FeedDemoIDs.SIGN_IN))
    }

    @Test
    fun aHeartShowsAtOnceAndTheServersAnswerKeepsIt() {
        val feed = model()
        runBlocking { feed.load() }
        val card = feed.list!!.feedItems.first { each -> each.feedItemId == FeedDemoIDs.WEATHER }
        assertFalse(feed.isLiked(card))
        feed.toggleLike(card)
        assertTrue(feed.list!!.feedItems.first { each -> each.feedItemId == FeedDemoIDs.WEATHER }.isLiked)
    }

    @Test
    fun showingLessTakesTheCardOutAndSaysSo() {
        val feed = model()
        var said = ""
        feed.onDone = { text -> said = text }
        runBlocking { feed.load() }
        feed.showLessLike(feed.list!!.feedItems.first { each -> each.feedItemId == FeedDemoIDs.WEATHER }, "Fewer like it")
        assertFalse(feed.list!!.feedItems.any { each -> each.feedItemId == FeedDemoIDs.WEATHER })
        assertEquals("Fewer like it", said)
    }

    @Test
    fun dismissingTakesTheCardOutQuietly() {
        val feed = model()
        var said = ""
        feed.onDone = { text -> said = text }
        runBlocking { feed.load() }
        feed.dismiss(feed.list!!.feedItems.first { each -> each.feedItemId == FeedDemoIDs.WEATHER })
        assertFalse(feed.list!!.feedItems.any { each -> each.feedItemId == FeedDemoIDs.WEATHER })
        assertEquals("", said)
    }

    @Test
    fun aFeedThatCannotBeReadSaysWhyUntilThereIsSomethingToShow() {
        val session = DemoSession.create(ApplicationProvider.getApplicationContext<Context>())
        val feed = FeedModel(session.connection.value!!, scope, readPage = { throw IllegalStateException("no server") })
        runBlocking { feed.load() }
        assertNull(feed.list)
        assertNotNull(feed.loadFailure)
    }

    @Test
    fun theBriefStackKeepsTheServersOrder() {
        val feed = stackModel(FeedDemoIDs.BRIEF_STACK_KEY)
        runBlocking { feed.load() }
        assertEquals(
            listOf(FeedDemoIDs.BRIEF_AGENDA, FeedDemoIDs.BRIEF_WEATHER, FeedDemoIDs.BRIEF_TECH, FeedDemoIDs.BRIEF_HEADLINES, FeedDemoIDs.BRIEF_MENTIONS, FeedDemoIDs.BRIEF_REVIEWS, FeedDemoIDs.BRIEF_MAIL),
            feed.list!!.feedItems.map { card -> card.feedItemId },
        )
        assertEquals("Morning brief, Wed Nov 19", feed.list!!.feedItems.first().stackName)
    }

    @Test
    fun anActionChoiceShowsWorkingOnItWithAnOpenChatButton() {
        composeRule.setContent { DemoHost { FeedCardHost(FeedDemoIDs.SIGN_IN) } }
        composeRule.onNodeWithText("Secure my account").performClick()
        composeRule.waitForIdle()
        composeRule.onNodeWithText("Secure my account").assertIsDisplayed()
        composeRule.onNodeWithText("Your agent is on it…").assertIsDisplayed()
        composeRule.onNodeWithText("Open chat").assertIsDisplayed()
    }

    @Test
    fun aDismissChoiceSettlesTheCardOnItsLabel() {
        composeRule.setContent { DemoHost { FeedCardHost(FeedDemoIDs.SIGN_IN) } }
        composeRule.onNodeWithText("That's me").performClick()
        composeRule.waitForIdle()
        composeRule.onNodeWithText("That's me").assertIsDisplayed()
        composeRule.onAllNodesWithText("Secure my account").assertCountEquals(0)
    }

    @Test
    fun animationsTurnedOffInTheSystemSettingsAreNoticed() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        assertFalse(areAnimationsOff(context))
        android.provider.Settings.Global.putFloat(context.contentResolver, android.provider.Settings.Global.ANIMATOR_DURATION_SCALE, 0f)
        assertTrue(areAnimationsOff(context))
        android.provider.Settings.Global.putFloat(context.contentResolver, android.provider.Settings.Global.ANIMATOR_DURATION_SCALE, 1f)
    }
}
