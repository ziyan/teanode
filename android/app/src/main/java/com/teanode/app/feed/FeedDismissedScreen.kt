package com.teanode.app.feed

import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.feed.agentFeed

/**
 * The cards the person dismissed that have not expired, the most recently dismissed first, drawn by the same card as the
 * feed with nothing dimmed or hidden. Cards of one stack read one after another are a deck, opened from the cards already
 * read. A swipe, or Restore in a card's information sheet, puts a card back in the feed.
 */
@Composable
fun FeedDismissedScreen(onBack: () -> Unit) {
    val connection = LocalConnection.current
    val scope = rememberCoroutineScope()
    val model = remember(connection) {
        FeedModel(connection, scope, readPage = { after -> connection.agentFeed(after = after, isDismissedOnly = true) }, isDismissedOnly = true)
    }
    TeaNodeScreen(title = stringResource(R.string.feed_dismissed_title), onBack = onBack) { contentPadding ->
        FeedListBody(model, areDecksShown = true, modifier = Modifier.padding(contentPadding))
    }
}
