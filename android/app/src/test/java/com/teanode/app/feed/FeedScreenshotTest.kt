package com.teanode.app.feed

import android.content.Context
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.performScrollToIndex
import androidx.compose.ui.test.performClick
import androidx.test.core.app.ApplicationProvider
import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.DemoHost
import com.teanode.app.demo.DemoResponse
import com.teanode.app.demo.DemoServer
import com.teanode.app.demo.DemoSubscriptions
import com.teanode.app.demo.DemoTransport
import com.teanode.app.session.AppSession
import com.teanode.app.session.MemorySecretStore
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

/** Photographs of the feed in demo mode: the list, every kind and state of card, and the empty and failed feed, light, dark and at the largest text. */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [34], qualifiers = PHONE_QUALIFIERS)
class FeedScreenshotTest {
    @get:Rule
    val composeRule = createComposeRule()

    private fun drawFeed(look: Look, fontScale: Float = 1f) {
        composeRule.setContent { DemoHost(appearance = look.appearance) { WithFontScale(fontScale) { FeedSection() } } }
        composeRule.waitForIdle()
    }

    private fun drawCard(feedItemId: String, look: Look, fontScale: Float = 1f) {
        composeRule.setContent { DemoHost(appearance = look.appearance) { WithFontScale(fontScale) { FeedCardHost(feedItemId) } } }
        composeRule.waitForIdle()
    }

    private fun sessionAnswering(answerer: DemoAnswerer): AppSession {
        val context = ApplicationProvider.getApplicationContext<Context>()
        val session = AppSession(
            context = context, secrets = MemorySecretStore(), transport = DemoTransport(listOf(answerer)), subscriptions = DemoSubscriptions,
            preferences = context.getSharedPreferences("teanode-demo-feed", Context.MODE_PRIVATE).also { kept -> kept.edit().clear().apply() }, isDemo = true,
        )
        session.enterDemo(DemoServer.account, DemoServer.facts)
        return session
    }

    @Test fun `feed light`() { drawFeed(Look.Light); composeRule.photograph("feed-list-light") }

    @Test fun `feed dark`() { drawFeed(Look.Dark); composeRule.photograph("feed-list-dark") }

    @Test fun `feed at the largest text`() { drawFeed(Look.Light, 2f); composeRule.photograph("feed-list-largest-text-light") }

    @Test fun `urgent decision light`() { drawCard(FeedDemoIDs.SIGN_IN, Look.Light); composeRule.photograph("feed-card-sign-in-light") }

    @Test fun `urgent decision dark`() { drawCard(FeedDemoIDs.SIGN_IN, Look.Dark); composeRule.photograph("feed-card-sign-in-dark") }

    @Test fun `urgent decision at the largest text`() { drawCard(FeedDemoIDs.SIGN_IN, Look.Light, 2f); composeRule.photograph("feed-card-sign-in-largest-text-light") }

    @Test fun `mail organize light`() { drawCard(FeedDemoIDs.MAIL_ORGANIZE, Look.Light); composeRule.photograph("feed-card-mail-organize-light") }

    @Test fun `mail organize dark`() { drawCard(FeedDemoIDs.MAIL_ORGANIZE, Look.Dark); composeRule.photograph("feed-card-mail-organize-dark") }

    @Test fun `mail organize at the largest text`() { drawCard(FeedDemoIDs.MAIL_ORGANIZE, Look.Light, 2f); composeRule.photograph("feed-card-mail-organize-largest-text-light") }

    @Test fun `choice pending with Open chat`() { drawCard(FeedDemoIDs.PENDING, Look.Light); composeRule.photograph("feed-card-pending-light") }

    @Test fun `choice pending dark`() { drawCard(FeedDemoIDs.PENDING, Look.Dark); composeRule.photograph("feed-card-pending-dark") }

    @Test fun `choice settled`() { drawCard(FeedDemoIDs.SETTLED, Look.Light); composeRule.photograph("feed-card-settled-light") }

    @Test fun `weather with facts`() { drawCard(FeedDemoIDs.WEATHER, Look.Light); composeRule.photograph("feed-card-weather-light") }

    @Test fun `weather dark`() { drawCard(FeedDemoIDs.WEATHER, Look.Dark); composeRule.photograph("feed-card-weather-dark") }

    @Test fun `news liked`() { drawCard(FeedDemoIDs.NEWS, Look.Light); composeRule.photograph("feed-card-news-light") }

    @Test fun `report with records`() { drawCard(FeedDemoIDs.REPORT, Look.Light); composeRule.photograph("feed-card-report-light") }

    @Test fun `coding question with options`() { drawCard(FeedDemoIDs.CODING_OPTIONS, Look.Light); composeRule.photograph("feed-card-coding-options-light") }

    @Test fun `coding question to open`() { drawCard(FeedDemoIDs.CODING_OPEN, Look.Light); composeRule.photograph("feed-card-coding-open-light") }

    @Test fun `idea with an emoji`() { drawCard(FeedDemoIDs.IDEA, Look.Light); composeRule.photograph("feed-card-idea-light") }

    @Test fun `unknown symbol is a newspaper`() { drawCard(FeedDemoIDs.UNKNOWN_SYMBOL, Look.Light); composeRule.photograph("feed-card-unknown-symbol-light") }

    private fun drawStack(feedStackKey: String, look: Look, fontScale: Float = 1f) {
        composeRule.setContent { DemoHost(appearance = look.appearance) { WithFontScale(fontScale) { FeedStackScreen(feedStackKey, onBack = {}) } } }
        composeRule.waitForIdle()
    }

    private fun scrollFeedTo(feedItemId: String) {
        val index = demoFeedCards.indexOfFirst { card -> card.feedItemId == feedItemId }
        composeRule.onNodeWithTag(FEED_LIST_TAG).performScrollToIndex(index)
        composeRule.waitForIdle()
    }

    @Test fun `stack of seven with two edges light`() { drawFeed(Look.Light); scrollFeedTo(FeedDemoIDs.BRIEF_AGENDA); composeRule.photograph("feed-list-brief-stack-light") }

    @Test fun `stack of seven with two edges dark`() { drawFeed(Look.Dark); scrollFeedTo(FeedDemoIDs.BRIEF_AGENDA); composeRule.photograph("feed-list-brief-stack-dark") }

    @Test fun `stack of four`() { drawFeed(Look.Light); scrollFeedTo(FeedDemoIDs.NEWS); composeRule.photograph("feed-list-news-stack-light") }

    @Test fun `stack of two with one edge`() { drawFeed(Look.Light); scrollFeedTo(FeedDemoIDs.IDEA); composeRule.photograph("feed-list-idea-pair-light") }

    @Test fun `stack screen of the brief light`() { drawStack(FeedDemoIDs.BRIEF_STACK_KEY, Look.Light); composeRule.photograph("feed-stack-brief-light") }

    @Test fun `stack screen of the brief dark`() { drawStack(FeedDemoIDs.BRIEF_STACK_KEY, Look.Dark); composeRule.photograph("feed-stack-brief-dark") }

    @Test fun `stack screen of the brief at the largest text`() { drawStack(FeedDemoIDs.BRIEF_STACK_KEY, Look.Light, 2f); composeRule.photograph("feed-stack-brief-largest-text-light") }

    @Test fun `stack screen of a goal`() { drawStack(FeedDemoIDs.ROBOTS_STACK_KEY, Look.Light); composeRule.photograph("feed-stack-robots-light") }

    @Test fun `agenda light`() { drawCard(FeedDemoIDs.BRIEF_AGENDA, Look.Light); composeRule.photograph("feed-layout-agenda-light") }

    @Test fun `agenda dark`() { drawCard(FeedDemoIDs.BRIEF_AGENDA, Look.Dark); composeRule.photograph("feed-layout-agenda-dark") }

    @Test fun `agenda at the largest text`() { drawCard(FeedDemoIDs.BRIEF_AGENDA, Look.Light, 2f); composeRule.photograph("feed-layout-agenda-largest-text-light") }

    @Test fun `weather layout light`() { drawCard(FeedDemoIDs.BRIEF_WEATHER, Look.Light); composeRule.photograph("feed-layout-weather-light") }

    @Test fun `weather layout dark`() { drawCard(FeedDemoIDs.BRIEF_WEATHER, Look.Dark); composeRule.photograph("feed-layout-weather-dark") }

    @Test fun `weather layout at the largest text`() { drawCard(FeedDemoIDs.BRIEF_WEATHER, Look.Light, 2f); composeRule.photograph("feed-layout-weather-largest-text-light") }

    @Test fun `tech news list light`() { drawCard(FeedDemoIDs.BRIEF_TECH, Look.Light); composeRule.photograph("feed-layout-list-tech-light") }

    @Test fun `tech news list dark`() { drawCard(FeedDemoIDs.BRIEF_TECH, Look.Dark); composeRule.photograph("feed-layout-list-tech-dark") }

    @Test fun `tech news list at the largest text`() { drawCard(FeedDemoIDs.BRIEF_TECH, Look.Light, 2f); composeRule.photograph("feed-layout-list-tech-largest-text-light") }

    @Test fun `headlines list light`() { drawCard(FeedDemoIDs.BRIEF_HEADLINES, Look.Light); composeRule.photograph("feed-layout-list-headlines-light") }

    @Test fun `mentions list light`() { drawCard(FeedDemoIDs.BRIEF_MENTIONS, Look.Light); composeRule.photograph("feed-layout-list-mentions-light") }

    @Test fun `mentions list dark`() { drawCard(FeedDemoIDs.BRIEF_MENTIONS, Look.Dark); composeRule.photograph("feed-layout-list-mentions-dark") }

    @Test fun `review items list light`() { drawCard(FeedDemoIDs.BRIEF_REVIEWS, Look.Light); composeRule.photograph("feed-layout-list-reviews-light") }

    @Test fun `mail list light`() { drawCard(FeedDemoIDs.BRIEF_MAIL, Look.Light); composeRule.photograph("feed-layout-list-mail-light") }

    @Test fun `mail list dark`() { drawCard(FeedDemoIDs.BRIEF_MAIL, Look.Dark); composeRule.photograph("feed-layout-list-mail-dark") }

    @Test fun `unknown layout shows the body`() { drawCard(FeedDemoIDs.BAD_LAYOUT, Look.Light); composeRule.photograph("feed-layout-unknown-light") }

    @Test fun `info sheet`() {
        drawFeed(Look.Light)
        composeRule.onAllNodesWithContentDescription("About this card")[1].performClick()
        composeRule.waitForIdle()
        composeRule.photograph("feed-info-sheet-light")
    }

    @Test fun `empty feed`() {
        val session = sessionAnswering(DemoAnswerer { DemoResponse("""{"data":{"ListAgentFeed":{"feedItems":[],"nextCursor":null,"unreadCount":0}}}""") })
        composeRule.setContent { DemoHost(session = session, appearance = Look.Light.appearance) { FeedSection() } }
        composeRule.waitForIdle()
        composeRule.photograph("feed-empty-light")
    }

    @Test fun `feed that could not be read`() {
        val session = sessionAnswering(DemoAnswerer { DemoResponse("""{"errors":[{"message":"The server could not be reached."}]}""") })
        composeRule.setContent { DemoHost(session = session, appearance = Look.Dark.appearance) { FeedSection() } }
        composeRule.waitForIdle()
        composeRule.photograph("feed-error-dark")
    }
}
