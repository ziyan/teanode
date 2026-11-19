package com.teanode.app.feed

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.shell.rememberAppLinkOpener
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.feed.AgentFeedPage

/**
 * One demo card on a screen of its own, alone in a feed of one, so a test can press its choices and photograph each kind
 * and state of card. [onBack] is called when the card goes (a closed card is taken out of the list).
 */
@Composable
fun FeedCardHost(feedItemId: String, onBack: () -> Unit = {}) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val navigator = LocalAppNavigator.current
    val linkOpener = rememberAppLinkOpener()
    val scope = rememberCoroutineScope()
    val model = remember(feedItemId) { FeedModel(connection, scope, readPage = { AgentFeedPage(listOf(demoFeedCard(feedItemId)), null, 1) }) }
    model.onProblem = { failure -> snackbar.showError(failure) }
    val actions = remember(model) { FeedActions(model, scope, navigator, linkOpener) }
    LaunchedEffect(model) { model.load() }
    TeaNodeScreen(title = "Feed", onBack = onBack) { contentPadding ->
        Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(contentPadding).padding(16.dp)) {
            model.list?.feedItems?.forEach { item -> FeedCard(item, actions, onShowInfo = {}) }
        }
    }
}

/**
 * A closed deck whose top card is [topFeedItemId], cut down to a one-line text card, over the brief's other cards, on
 * the feed's page with room above it, so a test can see that tucked cards taller than the top card show neither above
 * the top card nor as text in the deck's feet.
 */
@Composable
fun FeedDeckHost(topFeedItemId: String) {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val linkOpener = rememberAppLinkOpener()
    val scope = rememberCoroutineScope()
    val cards = demoBriefCards.map { card -> card.copy(stackItemCount = demoBriefCards.size, stackUnreadCount = demoBriefCards.size) }
    val top = cards.first { card -> card.feedItemId == topFeedItemId }.copy(feedCardLayout = "text", layoutContent = "")
    val model = remember(topFeedItemId) {
        FeedModel(connection, scope, readPage = { AgentFeedPage(listOf(top), null, 1) }).also { built ->
            built.deckCards[FeedDemoIDs.BRIEF_STACK_KEY] = listOf(top) + cards.filter { card -> card.feedItemId != topFeedItemId }
        }
    }
    val actions = remember(model) { FeedActions(model, scope, navigator, linkOpener) }
    TeaNodeScreen(title = "Feed") { contentPadding ->
        Column(Modifier.fillMaxSize().background(feedPageColor()).padding(contentPadding).padding(horizontal = 16.dp, vertical = 48.dp)) {
            FeedDeck(top, model, actions, onShowInfo = {})
        }
    }
}
