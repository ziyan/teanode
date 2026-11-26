package com.teanode.app.feed

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import com.teanode.app.demo.DemoSession
import com.teanode.kit.feed.AgentFeedPage
import com.teanode.kit.feed.agentFeed
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/** The Dismissed screen's model against the demo server, and the feed's own read after a dismissal empties it. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class FeedDismissedTest {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)

    @After
    fun stop() {
        scope.coroutineContext[kotlinx.coroutines.Job]?.cancel()
    }

    private fun dismissedModel(): FeedModel {
        val session = DemoSession.create(ApplicationProvider.getApplicationContext<Context>())
        val connection = session.connection.value!!
        return FeedModel(connection, scope, readPage = { after -> connection.agentFeed(after = after, isDismissedOnly = true) }, isDismissedOnly = true)
    }

    @Test
    fun theDismissedCardsOfOneStackAreOneDeckOfTwo() {
        val model = dismissedModel()
        runBlocking { model.load() }
        val shown = model.list!!.feedItems
        assertEquals(listOf(FeedDemoIDs.DISMISSED_CHOICES, FeedDemoIDs.DISMISSED_WATERING_NEW, FeedDemoIDs.DISMISSED_NEWS), shown.map { card -> card.feedItemId })
        val deck = shown.first { card -> card.feedItemId == FeedDemoIDs.DISMISSED_WATERING_NEW }
        assertTrue(deck.isStack)
        assertEquals(2, deck.stackItemCount)
    }

    @Test
    fun theDeckOpensFromTheEntriesAlreadyRead() {
        val model = dismissedModel()
        runBlocking { model.load() }
        model.openDeck(FeedDemoIDs.WATERING_STACK_KEY)
        assertTrue(model.isDeckOpen(FeedDemoIDs.WATERING_STACK_KEY))
        assertEquals(listOf(FeedDemoIDs.DISMISSED_WATERING_NEW, FeedDemoIDs.DISMISSED_WATERING_OLD), model.deckCards[FeedDemoIDs.WATERING_STACK_KEY]!!.map { card -> card.feedItemId })
    }

    @Test
    fun aSwipeRestoresTheCardAtOnceAndItStaysOutOfALaterRead() {
        val model = dismissedModel()
        var restoredCount = 0
        model.onRestored = { restoredCount++ }
        runBlocking { model.load() }
        val card = model.list!!.feedItems.first { each -> each.feedItemId == FeedDemoIDs.DISMISSED_NEWS }
        model.swipeAway(card)
        assertFalse(model.list!!.feedItems.any { each -> each.feedItemId == FeedDemoIDs.DISMISSED_NEWS })
        assertEquals(1, restoredCount)
        // The demo server still lists it, as a read already on its way would.
        runBlocking { model.load() }
        assertFalse(model.list!!.feedItems.any { each -> each.feedItemId == FeedDemoIDs.DISMISSED_NEWS })
    }

    @Test
    fun restoringTheTopOfADeckLeavesTheNextCardAlone() {
        val model = dismissedModel()
        runBlocking { model.load() }
        model.swipeAway(model.list!!.feedItems.first { each -> each.feedItemId == FeedDemoIDs.DISMISSED_WATERING_NEW })
        val remaining = model.list!!.feedItems.first { each -> each.feedItemId == FeedDemoIDs.DISMISSED_WATERING_OLD }
        assertFalse(remaining.isStack)
    }

    @Test
    fun aDismissalThatEmptiesTheFeedReadsAgainAndOnlyThatReadSaysEmpty() {
        val session = DemoSession.create(ApplicationProvider.getApplicationContext<Context>())
        val connection = session.connection.value!!
        var readCount = 0
        lateinit var model: FeedModel
        var isReadingAgainDuringRead = false
        model = FeedModel(connection, scope, readPage = {
            readCount++
            if (readCount == 1) {
                AgentFeedPage(listOf(demoFeedCard(FeedDemoIDs.WEATHER)), null, 1)
            } else {
                isReadingAgainDuringRead = model.isReadingAgain
                AgentFeedPage(emptyList(), null, 0)
            }
        })
        runBlocking { model.load() }
        model.dismiss(model.list!!.feedItems.single())
        assertEquals(2, readCount)
        assertTrue(isReadingAgainDuringRead)
        assertTrue(model.list!!.feedItems.isEmpty())
        assertFalse(model.isReadingAgain)
    }
}

