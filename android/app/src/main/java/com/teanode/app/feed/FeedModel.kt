package com.teanode.app.feed

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.teanode.kit.Connection
import com.teanode.kit.feed.AgentFeedChanged
import com.teanode.kit.feed.AgentFeedItemView
import com.teanode.kit.feed.AgentFeedList
import com.teanode.kit.feed.AgentFeedPage
import com.teanode.kit.feed.agentFeedItem
import com.teanode.kit.feed.markAgentFeedRead
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/** How long a card is on screen before it is marked read, so a card flicked past is not. */
const val FEED_SEEN_DELAY_MS = 800L

/**
 * FeedModel holds the feed a screen shows: the cards read so far, paging, a pull to refresh, the live changes the
 * server reports, and the cards seen so far, which are marked read in one call. The list itself and every change to it
 * are [AgentFeedList]'s, tested in the kit; this class only calls the server and keeps the screen's loading states.
 */
class FeedModel(
    private val connection: Connection,
    private val scope: CoroutineScope,
    /** Reads a page of the cards this model shows: the whole feed, or one stack's cards; [after] is the previous page's cursor. */
    private val readPage: suspend (after: String?) -> AgentFeedPage,
    /** Whether the cards are one stack's, which is read again whenever any of them changes, since the server keeps their order. */
    private val isStackScreen: Boolean = false,
    /** Called with the unread count whenever it changes, for the badge of the Feed tab. */
    private val onUnreadCount: (Int) -> Unit,
    /** Called with a failure of something the person did not ask for by name (reading a page, marking read), to tell them. */
    private val onProblem: (Throwable) -> Unit,
    /** Called with how many cards were newly read, for a screen whose own count is not the badge's. */
    private val onNewlyRead: (Int) -> Unit = {},
) {
    var list: AgentFeedList? by mutableStateOf(null)
        private set

    /** Why the first read failed; null otherwise. */
    var error: Throwable? by mutableStateOf(null)
        private set

    var isLoading: Boolean by mutableStateOf(false)
        private set

    var isLoadingMore: Boolean by mutableStateOf(false)
        private set

    /** Whether reading the next page failed, so that the list offers to try again rather than asking at once and failing in a loop. */
    var hasMoreFailed: Boolean by mutableStateOf(false)
        private set

    private val unreadSeenIds = LinkedHashSet<String>()
    private var seenJob: Job? = null

    fun load() {
        if (isLoading || list != null) return
        isLoading = true
        error = null
        scope.launch {
            try {
                show(AgentFeedList.first(readPage(null)))
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                error = failure
            } finally {
                isLoading = false
            }
        }
    }

    fun retry() {
        list = null
        load()
    }

    /** Reads the first page again over the cards shown; the old cards stay while it comes. */
    suspend fun refresh() {
        try {
            show(AgentFeedList.first(readPage(null)))
            hasMoreFailed = false
        } catch (failure: CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            if (list == null) error = failure else onProblem(failure)
        }
    }

    fun loadMore() {
        val current = list ?: return
        val cursor = current.nextCursor
        if (cursor.isNullOrEmpty() || isLoadingMore || hasMoreFailed) return
        isLoadingMore = true
        scope.launch {
            try {
                show((list ?: current).appending(readPage(cursor)))
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                hasMoreFailed = true
                onProblem(failure)
            } finally {
                isLoadingMore = false
            }
        }
    }

    fun retryMore() {
        hasMoreFailed = false
        loadMore()
    }

    /** Puts a card the person changed, or the server answered, where it belongs. */
    fun change(item: AgentFeedItemView) {
        list?.let { current -> show(current.with(item)) }
    }

    fun remove(feedItemId: String) {
        val current = list ?: return
        val removed = current.feedItems.firstOrNull { item -> item.feedItemId == feedItemId }
        show(current.removing(feedItemId))
        // The card under a stack's top takes its place, which only the server knows.
        if (removed?.isStack == true) scope.launch { refresh() }
    }

    /** What the server says happened to a card: a card it changed is read again, one that was closed leaves. */
    suspend fun apply(changed: AgentFeedChanged) {
        val current = list ?: return
        val shown = current.feedItems.firstOrNull { item -> item.feedItemId == changed.feedItemId }
        if (changed.feedChange == "dismissed") {
            show(current.removing(changed.feedItemId).withUnreadCount(changed.unreadCount))
            if (shown?.isStack == true || isStackScreen) refresh()
            return
        }
        if (isStackScreen) {
            refresh()
            return
        }
        try {
            val item = connection.agentFeedItem(changed.feedItemId)
            val movesToTop = changed.feedChange == "posted" || changed.feedChange == "replaced"
            // A card that joins a stack changes the stack's top and count, which the server's list says.
            if (item.feedStackKey.isNotEmpty() && movesToTop) {
                refresh()
                list?.let { latest -> show(latest.withUnreadCount(changed.unreadCount)) }
                return
            }
            show((list ?: current).with(item, isMovedToTop = movesToTop).withUnreadCount(changed.unreadCount))
        } catch (failure: CancellationException) {
            throw failure
        } catch (_: Throwable) {
            // A card that cannot be read now is read with the next refresh; the count still says what the server counted.
            show((list ?: current).withUnreadCount(changed.unreadCount))
        }
    }

    /** A card is on screen: it is marked read once it has stayed there a moment. */
    fun noteSeen(feedItemId: String) {
        if (list?.isUnread(feedItemId) != true) return
        unreadSeenIds.add(feedItemId)
        if (seenJob?.isActive == true) return
        seenJob = scope.launch {
            delay(FEED_SEEN_DELAY_MS)
            val ids = unreadSeenIds.toList()
            unreadSeenIds.clear()
            if (ids.isEmpty()) return@launch
            try {
                val newlyReadCount = connection.markAgentFeedRead(ids)
                list?.let { current -> show(current.markedRead(ids.toSet(), newlyReadCount)) }
                onNewlyRead(newlyReadCount)
            } catch (failure: CancellationException) {
                throw failure
            } catch (_: Throwable) {
                // Marking read is a courtesy: the cards stay unread and are marked when seen again.
            }
        }
    }

    private fun show(next: AgentFeedList) {
        list = next
        onUnreadCount(next.unreadCount)
    }
}
