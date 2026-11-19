package com.teanode.app.feed

import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.AppSection
import com.teanode.kit.feed.agentFeedStack

/**
 * One stack opened (ios FeedStackScreen): its cards in the order the server keeps them, each whole, titled with the
 * stack's name (its own, else its source's, else "More cards"). It reads the stack itself and follows the feed live,
 * as the feed does, and what was read here comes off the Feed tab's badge.
 */
@Composable
fun FeedStackScreen(feedStackKey: String, onBack: () -> Unit) {
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    val scope = rememberCoroutineScope()
    val model = remember(connection, feedStackKey) {
        FeedModel(
            connection, scope, readPage = { after -> connection.agentFeedStack(feedStackKey, after = after) },
            onNewlyRead = { newlyReadCount ->
                val kept = session.badgeCounts.value[AppSection.Feed] ?: 0
                session.setBadgeCount(AppSection.Feed, maxOf(0, kept - newlyReadCount))
            },
        )
    }
    val name = model.list?.feedItems?.firstOrNull()?.stackName?.takeIf { text -> text.isNotEmpty() } ?: stringResource(R.string.feed_more_cards)
    TeaNodeScreen(title = name, onBack = onBack) { contentPadding ->
        FeedListBody(model, onOpenStack = null, modifier = Modifier.padding(contentPadding))
    }
}
