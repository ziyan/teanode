package com.teanode.app.feed

import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.foundation.layout.padding
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.AppSection
import com.teanode.kit.feed.agentFeedStack

/**
 * One stack's cards, in the order the server keeps them (the positioned ones first, then newest first), a page at a
 * time. It is titled with the stack's name once the first card is read (ios FeedViews.swift, FeedStackView). The
 * cards are marked read as they are seen, and the badge on the Feed tab goes down by how many were.
 */
@Composable
fun FeedStackScreen(feedStackKey: String, onBack: () -> Unit) {
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    val model = remember(connection, feedStackKey) {
        FeedModel(
            connection, scope, readPage = { after -> connection.agentFeedStack(feedStackKey, after = after) }, isStackScreen = true,
            // The stack's own count is not the badge's: only the cards read here come off it.
            onUnreadCount = {}, onProblem = { failure -> snackbar.showError(failure) },
            onNewlyRead = { newlyReadCount ->
                val kept = session.badgeCounts.value[AppSection.Feed] ?: 0
                session.setBadgeCount(AppSection.Feed, maxOf(0, kept - newlyReadCount))
            },
        )
    }
    FeedLiveChanges(model)
    val firstCard = model.list?.feedItems?.firstOrNull()
    val title = firstCard?.stackName?.takeIf { name -> name.isNotEmpty() } ?: stringResource(R.string.feed_title)
    TeaNodeScreen(title = title, onBack = onBack) { contentPadding ->
        FeedListBody(model, onOpenStack = null, modifier = Modifier.padding(contentPadding))
    }
}
