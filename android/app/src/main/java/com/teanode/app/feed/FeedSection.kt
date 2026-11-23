package com.teanode.app.feed

import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.input.pointer.PointerEventPass
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.LayoutCoordinates
import androidx.compose.ui.layout.boundsInRoot
import androidx.compose.ui.layout.onGloballyPositioned
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.background
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
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
import com.teanode.kit.feed.agentFeedStack
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import androidx.compose.ui.platform.LocalContext
import com.teanode.app.notifications.FeedAppUpdateCard
import com.teanode.app.notifications.rememberOfferedAppUpdate
import com.teanode.app.ui.components.openWebPage

/** How many cards of a stack are read when its deck fans out, as the iPhone reads them. */
private const val DECK_PAGE_SIZE = 50

/** The test tag of the list of cards. */
const val FEED_LIST_TAG = "feed-list"

/** How often the feed is read again while it is open, in case the live stream is down (ios FeedFollowing). */
private const val FEED_POLL_MS = 60_000L

/** How often it is read while a card waits for the agent to say how its answer went. */
private const val FEED_PENDING_POLL_MS = 4_000L

/**
 * FeedSection is the first tab: the agent's feed, cards newest first (ios FeedViews.swift, FeedScreen). A stack of
 * cards is its top card with edges behind it; tapping its row opens the stack.
 */
@Composable
fun FeedSection() {
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    val navigator = LocalAppNavigator.current
    val scope = rememberCoroutineScope()
    val model = remember(connection) {
        FeedModel(
            connection, scope, readPage = { after -> connection.agentFeed(after = after) }, onUnreadCount = { count -> session.setBadgeCount(AppSection.Feed, count) },
            readStackPage = { feedStackKey, after -> connection.agentFeedStack(feedStackKey, after = after, first = DECK_PAGE_SIZE) },
        )
    }
    TeaNodeScreen(title = stringResource(R.string.feed_title)) { contentPadding ->
        FeedListBody(model, areDecksShown = true, modifier = Modifier.padding(contentPadding), isAppUpdateShown = true)
    }
}

/**
 * Keeps [model] current while the screen is open: it is read now, again each time the server says a card was posted,
 * replaced, answered or dismissed, and every minute in case the stream is down (ios FeedFollowing).
 */
@Composable
internal fun FeedFollowing(model: FeedModel) {
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    LaunchedEffect(connection, model) {
        model.load()
        if (session.isDemo) return@LaunchedEffect
        coroutineScope {
            launch {
                var pauseMS = 1_000L
                while (true) {
                    try {
                        AgentFeedEvents.changes(connection).collect {
                            model.load()
                            pauseMS = 1_000L
                        }
                    } catch (failure: CancellationException) {
                        throw failure
                    } catch (_: Throwable) {
                        // The stream dropped; what was missed is read again below.
                    }
                    delay(pauseMS)
                    pauseMS = minOf(pauseMS * 2, 30_000L)
                    model.load()
                }
            }
            while (true) {
                delay(FEED_POLL_MS)
                model.load()
            }
        }
    }
}

/** The cards of a [FeedModel] with its loading, failed and empty states, paging and pull to refresh; the feed and a stack's own screen both draw it. */
@Composable
internal fun FeedListBody(model: FeedModel, areDecksShown: Boolean, modifier: Modifier = Modifier, isAppUpdateShown: Boolean = false) {
    val session = LocalAppSession.current
    val snackbar = LocalSnackbar.current
    val navigator = LocalAppNavigator.current
    val linkOpener = rememberAppLinkOpener()
    val scope = rememberCoroutineScope()
    val actions = remember(model, navigator, linkOpener) { FeedActions(model, scope, navigator, linkOpener) }
    model.onProblem = { failure -> snackbar.showError(failure) }
    model.onDone = { text -> snackbar.show(text) }
    FeedFollowing(model)
    // While a card waits for the agent to say how its answer went, the feed is read every few seconds.
    LaunchedEffect(model.hasPendingOutcome) {
        if (!model.hasPendingOutcome) return@LaunchedEffect
        while (true) {
            delay(FEED_PENDING_POLL_MS)
            model.load()
        }
    }
    // The sheet is presented from the screen, not from a card: the cards are rebuilt as the feed changes.
    var infoItem: AgentFeedItemView? by remember { mutableStateOf(null) }
    val list = model.list
    val failure = model.loadFailure
    // A newer app stays above every card of the feed until it is installed or put off; a stack's own screen does not show it.
    val (offeredUpdate, dismissUpdate) = if (isAppUpdateShown) rememberOfferedAppUpdate() else null to {}
    val context = LocalContext.current
    val appUpdateCard: @Composable () -> Unit = {
        offeredUpdate?.let { update -> FeedAppUpdateCard(update, onInstall = { openWebPage(context, update.installUrl) }, onDismiss = dismissUpdate) }
    }
    // Where each deck is on screen, so a tap outside an open one gathers it.
    val deckBounds = remember(model) { HashMap<String, Rect>() }
    var listCoordinates: LayoutCoordinates? by remember { mutableStateOf(null) }
    // The cards sit on the grouped page, so in light mode they are white on grey.
    Box(modifier.fillMaxSize().background(feedPageColor()).onGloballyPositioned { coordinates -> listCoordinates = coordinates }.gatherDecksOnTapOutside(model, deckBounds) { listCoordinates }) {
        when {
            list != null && list.feedItems.isEmpty() -> PullToRefresh(onRefresh = { model.refresh() }, Modifier.fillMaxSize()) {
                // Scrollable, or the pull could not be made on an empty feed.
                BoxWithConstraints(Modifier.fillMaxSize()) {
                    Column(Modifier.verticalScroll(rememberScrollState())) {
                        if (offeredUpdate != null) Box(Modifier.padding(16.dp)) { appUpdateCard() }
                        EmptyState(AppSection.Feed.icon, stringResource(R.string.feed_empty_title), Modifier.height(this@BoxWithConstraints.maxHeight), description = stringResource(R.string.feed_empty_description))
                    }
                }
            }
            list != null -> PullToRefresh(onRefresh = { model.refresh() }, Modifier.fillMaxSize()) {
                LazyColumn(Modifier.fillMaxSize().testTag(FEED_LIST_TAG), contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    if (offeredUpdate != null) item(key = "app-update") { appUpdateCard() }
                    items(list.feedItems, key = { item -> item.feedItemId }) { item ->
                        LaunchedEffect(item.feedItemId) {
                            model.noteAppeared(item)
                            if (item.feedItemId == list.feedItems.last().feedItemId) model.loadMore()
                        }
                        if (areDecksShown && item.isStack) {
                            FeedDeck(
                                item, model, actions, onShowInfo = { shown -> infoItem = shown },
                                modifier = Modifier.onGloballyPositioned { coordinates -> deckBounds[item.feedStackKey] = coordinates.boundsInRoot() },
                            )
                        } else {
                            FeedCard(item, actions, onShowInfo = { infoItem = item })
                        }
                    }
                    if (model.isLoadingMore) {
                        item(key = "more") {
                            Box(Modifier.fillMaxWidth().padding(8.dp), contentAlignment = Alignment.Center) {
                                val description = stringResource(R.string.common_loading)
                                CircularProgressIndicator(Modifier.semantics { contentDescription = description })
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
        val current = (model.list?.feedItems.orEmpty() + model.deckCards.values.flatten()).firstOrNull { item -> item.feedItemId == shown.feedItemId } ?: shown
        FeedInfoSheet(current, actions, onDismiss = { infoItem = null })
    }
}

/**
 * A tap anywhere in the feed outside an open deck gathers it back, as on the dashboard. A drag is a scroll and leaves it
 * open, and a tap inside it is the deck's own. A dialog, sheet or menu opened from a card is a window of its own, so a
 * tap in one never reaches here.
 */
private fun Modifier.gatherDecksOnTapOutside(model: FeedModel, deckBounds: Map<String, Rect>, listCoordinates: () -> LayoutCoordinates?): Modifier =
    pointerInput(model) {
        awaitEachGesture {
            val down = awaitFirstDown(requireUnconsumed = false, pass = PointerEventPass.Final)
            if (model.openDeckKeys.isEmpty()) return@awaitEachGesture
            var isTap = true
            while (true) {
                val change = awaitPointerEvent(PointerEventPass.Final).changes.firstOrNull { change -> change.id == down.id } ?: break
                if ((change.position - down.position).getDistance() > viewConfiguration.touchSlop) isTap = false
                if (!change.pressed) break
            }
            if (!isTap) return@awaitEachGesture
            val point = listCoordinates()?.takeIf { coordinates -> coordinates.isAttached }?.localToRoot(down.position) ?: return@awaitEachGesture
            for (feedStackKey in model.openDeckKeys.keys.toList()) {
                if (deckBounds[feedStackKey]?.contains(point) != true) model.closeDeck(feedStackKey)
            }
        }
    }
