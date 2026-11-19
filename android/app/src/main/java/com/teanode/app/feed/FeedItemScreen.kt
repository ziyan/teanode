package com.teanode.app.feed

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.shell.rememberAppLinkOpener
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.feed.AgentFeedItemView
import com.teanode.kit.feed.agentFeedItem
import com.teanode.kit.feed.markAgentFeedRead

/**
 * One card on a screen of its own: where a pressed notification goes, so the person sees the card with its choices
 * (ios FeedViews.swift, FeedItemView). Back goes to the feed.
 */
@Composable
fun FeedItemScreen(feedItemId: String, onBack: () -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val navigator = LocalAppNavigator.current
    val context = LocalContext.current
    val linkOpener = rememberAppLinkOpener()
    val scope = rememberCoroutineScope()
    val loaded = rememberLoad(feedItemId) {
        connection.agentFeedItem(feedItemId).also { item -> if (!item.isRead) runCatching { connection.markAgentFeedRead(listOf(item.feedItemId)) } }
    }
    var infoItem: AgentFeedItemView? by remember { mutableStateOf(null) }
    val actions = remember(connection, navigator, linkOpener) {
        FeedActions(
            context, connection, scope, snackbar, navigator, linkOpener,
            onChanged = { item -> loaded.update { item } },
            onRemoved = { onBack() },
        )
    }
    TeaNodeScreen(title = stringResource(R.string.feed_item_title), onBack = onBack) { contentPadding ->
        LoadedContent(loaded, modifier = Modifier.fillMaxSize().padding(contentPadding)) { item ->
            androidx.compose.foundation.layout.Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp)) {
                FeedCard(item, actions, onShowInfo = { infoItem = item })
            }
            infoItem?.let { shown -> FeedInfoSheet(item.takeIf { current -> current.feedItemId == shown.feedItemId } ?: shown, actions, onDismiss = { infoItem = null }) }
        }
    }
}
