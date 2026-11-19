package com.teanode.app.feed

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.shell.icon
import com.teanode.app.shell.rememberAppLinkOpener
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.PullToRefresh
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.AppSection
import com.teanode.kit.feed.AgentFeedEvents
import com.teanode.kit.feed.AgentFeedItemView
import com.teanode.kit.feed.agentFeed
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay

/** The test tag of the list of cards. */
const val FEED_LIST_TAG = "feed-list"

/** How long a feed waits before it opens its live connection again after it dropped. */
internal const val FEED_REOPEN_DELAY_MS = 5_000L

/**
 * FeedSection is the first tab: the agent's feed, cards newest first (ios FeedViews.swift). A stack of cards is its
 * top card with edges behind it; tapping it opens the stack. Cards are marked read once they have been on screen a
 * moment, the server's changes arrive live, and a pull reads the newest again.
 */
@Composable
fun FeedSection() {
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    val snackbar = LocalSnackbar.current
    val navigator = LocalAppNavigator.current
    val scope = rememberCoroutineScope()
    val model = remember(connection) {
        FeedModel(
            connection, scope, readPage = { after -> connection.agentFeed(after = after) },
            onUnreadCount = { count -> session.setBadgeCount(AppSection.Feed, count) }, onProblem = { failure -> snackbar.showError(failure) },
        )
    }
    FeedLiveChanges(model)
    TeaNodeScreen(title = stringResource(R.string.feed_title)) { contentPadding ->
        FeedListBody(model, onOpenStack = { top -> navigator.navigate(FeedRoutes.stack(top.feedStackKey)) }, modifier = Modifier.padding(contentPadding))
    }
}

/** Applies the server's changes to [model] for as long as the screen is open; a dropped connection is opened again after a pause. */
@Composable
internal fun FeedLiveChanges(model: FeedModel) {
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    LaunchedEffect(connection, model) {
        if (session.isDemo) return@LaunchedEffect
        while (true) {
            try {
                AgentFeedEvents.changes(connection).collect { changed -> model.apply(changed) }
            } catch (failure: CancellationException) {
                throw failure
            } catch (_: Throwable) {
                // Opened again below.
            }
            delay(FEED_REOPEN_DELAY_MS)
        }
    }
}

/** The cards of a [FeedModel] in a list with its loading, error and empty states, paging and pull to refresh; the feed and a stack's own screen both draw it. */
@Composable
internal fun FeedListBody(model: FeedModel, onOpenStack: ((AgentFeedItemView) -> Unit)?, modifier: Modifier = Modifier) {
    val session = LocalAppSession.current
    val context = LocalContext.current
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val navigator = LocalAppNavigator.current
    val linkOpener = rememberAppLinkOpener()
    val scope = rememberCoroutineScope()
    val actions = remember(connection, navigator, linkOpener, model) {
        FeedActions(context, connection, scope, snackbar, navigator, linkOpener, onChanged = { item -> model.change(item) }, onRemoved = { feedItemId -> model.remove(feedItemId) })
    }
    var infoItem: AgentFeedItemView? by remember { mutableStateOf(null) }
    LaunchedEffect(model) { model.load() }
    val list = model.list
    val failure = model.error
    Box(modifier.fillMaxSize()) {
        when {
            list != null && list.feedItems.isEmpty() -> PullToRefresh(onRefresh = { model.refresh() }, Modifier.fillMaxSize()) {
                // Scrollable, or the pull could not be made on an empty feed.
                BoxWithConstraints(Modifier.fillMaxSize()) {
                    Column(Modifier.verticalScroll(rememberScrollState())) {
                        EmptyState(AppSection.Feed.icon, stringResource(R.string.feed_empty_title), Modifier.height(this@BoxWithConstraints.maxHeight), description = stringResource(R.string.feed_empty_description))
                    }
                }
            }
            list != null -> PullToRefresh(onRefresh = { model.refresh() }, Modifier.fillMaxSize()) {
                LazyColumn(Modifier.fillMaxSize().testTag(FEED_LIST_TAG), contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    items(list.feedItems, key = { item -> item.feedItemId }) { item ->
                        LaunchedEffect(item.feedItemId, item.isRead) { if (!item.isRead) model.noteSeen(item.feedItemId) }
                        FeedCard(item, actions, onShowInfo = { infoItem = item }, onOpenStack = onOpenStack)
                    }
                    if (list.hasMore) {
                        item(key = "more") {
                            if (model.hasMoreFailed) {
                                TextButton(onClick = { model.retryMore() }, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.common_try_again)) }
                            } else {
                                LaunchedEffect(list.nextCursor) { model.loadMore() }
                                Box(Modifier.fillMaxWidth().padding(8.dp), contentAlignment = Alignment.Center) {
                                    val description = stringResource(R.string.feed_loading_more)
                                    CircularProgressIndicator(Modifier.semantics { contentDescription = description })
                                }
                            }
                        }
                    }
                }
            }
            failure != null -> ErrorState(session.describeLoadProblem(failure), onRetry = { model.retry() })
            else -> LoadingState()
        }
    }
    infoItem?.let { shown ->
        // The card as it is now, so a like pressed under the open sheet shows.
        val current = model.list?.feedItems?.firstOrNull { item -> item.feedItemId == shown.feedItemId } ?: shown
        FeedInfoSheet(current, actions, onDismiss = { infoItem = null })
    }
}
