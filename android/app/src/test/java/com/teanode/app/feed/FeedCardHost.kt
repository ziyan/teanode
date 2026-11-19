package com.teanode.app.feed

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
