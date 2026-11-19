package com.teanode.app.feed

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.teanode.kit.Connection
import com.teanode.kit.feed.AgentFeedChoice
import com.teanode.kit.feed.AgentFeedItemView
import com.teanode.kit.feed.AgentFeedList
import com.teanode.kit.feed.AgentFeedPage
import com.teanode.kit.feed.chooseAgentFeedItem
import com.teanode.kit.feed.discussAgentFeedItem
import com.teanode.kit.feed.dismissAgentFeedItem
import com.teanode.kit.feed.likeAgentFeedItem
import com.teanode.kit.feed.markAgentFeedRead
import com.teanode.kit.feed.showLessLikeAgentFeedItem
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/** How long cards are on screen before they are reported read together, so scrolling past a screenful is one call (ios FeedModel.noteAppeared). */
const val FEED_READ_DELAY_MS = 1_500L

private const val DECK_PAGES_MOST = 5

/**
 * FeedModel is the feed (ios FeedViews.swift, FeedModel): the cards read so far, how many are unread, and the calls
 * that answer, like, close or read a card. The list and every change to it are [AgentFeedList]'s, tested in the kit;
 * this class calls the server and keeps what a screen needs while a call is on its way. A stack's own screen has one of
 * its own, reading that stack's cards.
 */
class FeedModel(
    private val connection: Connection,
    private val scope: CoroutineScope,
    /** Reads a page of the cards this model shows: the feed itself, or one stack's cards; [after] is the previous page's cursor. */
    private val readPage: suspend (after: String?) -> AgentFeedPage,
    /** Called with the unread count whenever it changes, for the badge of the Feed tab. */
    private val onUnreadCount: (Int) -> Unit = {},
    /** Called with how many cards were newly read, for a screen whose own count is not the badge's. */
    private val onNewlyRead: (Int) -> Unit = {},
    /** Reads a page of one stack's cards, for a deck fanned out in place; null where decks do not open (a stack's own screen). */
    private val readStackPage: (suspend (feedStackKey: String, after: String?) -> AgentFeedPage)? = null,
) {
    var list: AgentFeedList? by mutableStateOf(null)
        private set

    /** Why the first read failed, when there is nothing to show instead. */
    var loadFailure: Throwable? by mutableStateOf(null)
        private set

    var isLoadingMore: Boolean by mutableStateOf(false)
        private set

    /** The cards a call is in flight for, so a second tap does nothing. */
    val busyFeedItemIds = mutableStateMapOf<String, Boolean>()

    /** What happened that the person is told in a snackbar: a call that failed, or "You will see fewer like it". */
    var onProblem: (Throwable) -> Unit = {}
    var onDone: (String) -> Unit = {}

    /** The decks fanned out in place, by stack key, and the cards each holds (the top card first), read when it opened. */
    val openDeckKeys = mutableStateMapOf<String, Boolean>()
    val deckCards = mutableStateMapOf<String, List<AgentFeedItemView>>()

    /** Whether a deck is fanned out. */
    fun isDeckOpen(feedStackKey: String): Boolean = openDeckKeys.containsKey(feedStackKey)

    /** Fans a stack's deck out in place: it opens at once and its cards slide out as they are read. */
    fun openDeck(feedStackKey: String) {
        val read = readStackPage ?: return
        openDeckKeys[feedStackKey] = true
        scope.launch { refreshDeck(feedStackKey, read) }
    }

    /** Gathers a deck back into its top card. */
    fun closeDeck(feedStackKey: String) {
        openDeckKeys.remove(feedStackKey)
    }

    private suspend fun refreshDeck(feedStackKey: String, read: suspend (String, String?) -> AgentFeedPage) {
        try {
            val cards = ArrayList<AgentFeedItemView>()
            var cursor: String? = null
            var pageCount = 0
            // A deck is a few cards; the pages are read until it is whole, up to a handful of them.
            do {
                val page = read(feedStackKey, cursor)
                cards.addAll(page.feedItems.filter { card -> cards.none { known -> known.feedItemId == card.feedItemId } })
                cursor = page.nextCursor
                pageCount++
            } while (!cursor.isNullOrEmpty() && pageCount < DECK_PAGES_MOST)
            deckCards[feedStackKey] = cards
        } catch (failure: CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            openDeckKeys.remove(feedStackKey)
            onProblem(failure)
        }
    }

    /** The hearts the person just pressed, shown before the server answers. */
    private val likedOverrides = mutableStateMapOf<String, Boolean>()
    private val readFeedItemIds = HashSet<String>()
    private val pendingReadFeedItemIds = LinkedHashSet<String>()
    private var readJob: Job? = null
    private var isLoading = false

    val hasLoaded: Boolean get() = list != null

    val hasPendingOutcome: Boolean get() = list?.hasPendingOutcome == true

    fun isLiked(item: AgentFeedItemView): Boolean = likedOverrides[item.feedItemId] ?: item.isLiked

    /** Reads the newest page again. What was shown stays when it cannot be read, and the cards past the first page are dropped. */
    suspend fun load() {
        if (isLoading) return
        isLoading = true
        try {
            show(AgentFeedList.first(readPage(null)))
            loadFailure = null
            readStackPage?.let { read -> for (key in openDeckKeys.keys.toList()) refreshDeck(key, read) }
        } catch (failure: CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            if (hasLoaded) onProblem(failure) else loadFailure = failure
        } finally {
            isLoading = false
        }
    }

    suspend fun loadFirstIfNeeded() {
        if (!hasLoaded) load()
    }

    fun retry() {
        scope.launch { load() }
    }

    /** Reads the page after the last card shown. */
    fun loadMore() {
        val current = list ?: return
        val cursor = current.nextCursor
        if (cursor.isNullOrEmpty() || isLoadingMore || isLoading) return
        isLoadingMore = true
        scope.launch {
            try {
                show((list ?: current).appending(readPage(cursor)))
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                onProblem(failure)
            } finally {
                isLoadingMore = false
            }
        }
    }

    /** A card came on screen: cards are reported read together after a moment. */
    fun noteAppeared(item: AgentFeedItemView) {
        if (item.isRead || item.feedItemId in readFeedItemIds) return
        pendingReadFeedItemIds.add(item.feedItemId)
        if (readJob?.isActive == true) return
        readJob = scope.launch {
            delay(FEED_READ_DELAY_MS)
            val feedItemIds = pendingReadFeedItemIds.sorted()
            pendingReadFeedItemIds.clear()
            if (feedItemIds.isEmpty()) return@launch
            try {
                val newlyReadCount = connection.markAgentFeedRead(feedItemIds)
                readFeedItemIds.addAll(feedItemIds)
                list?.let { current -> show(current.markedRead(feedItemIds.toSet(), newlyReadCount)) }
                onNewlyRead(newlyReadCount)
            } catch (failure: CancellationException) {
                throw failure
            } catch (_: Throwable) {
                // Left unread: the card is reported again when it next appears.
            }
        }
    }

    /** Answers a card with one of its choices. The card comes back settled on the choice and stays on screen, with what the agent made of it. */
    fun choose(item: AgentFeedItemView, choice: AgentFeedChoice) {
        if (busyFeedItemIds.containsKey(item.feedItemId)) return
        busyFeedItemIds[item.feedItemId] = true
        scope.launch {
            try {
                replace(connection.chooseAgentFeedItem(item.feedItemId, choice.choiceKey))
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                onProblem(failure)
            } finally {
                busyFeedItemIds.remove(item.feedItemId)
            }
        }
    }

    fun toggleLike(item: AgentFeedItemView) {
        val isLikedNow = !isLiked(item)
        likedOverrides[item.feedItemId] = isLikedNow
        scope.launch {
            try {
                replace(connection.likeAgentFeedItem(item.feedItemId, isLikedNow))
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                onProblem(failure)
            } finally {
                likedOverrides.remove(item.feedItemId)
            }
        }
    }

    /** Records that the person wants fewer cards like this one; the card goes. */
    fun showLessLike(item: AgentFeedItemView, doneText: String) {
        scope.launch {
            try {
                connection.showLessLikeAgentFeedItem(item.feedItemId)
                remove(item)
                onDone(doneText)
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                onProblem(failure)
            }
        }
    }

    fun dismiss(item: AgentFeedItemView) {
        scope.launch {
            try {
                connection.dismissAgentFeedItem(item.feedItemId)
                remove(item)
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                onProblem(failure)
            }
        }
    }

    /** Opens the main conversation about a card: the server attaches the card to the next message there. Whether it did. */
    suspend fun discuss(item: AgentFeedItemView): Boolean = try {
        connection.discussAgentFeedItem(item.feedItemId)
        true
    } catch (failure: CancellationException) {
        throw failure
    } catch (failure: Throwable) {
        onProblem(failure)
        false
    }

    private fun replace(item: AgentFeedItemView) {
        list?.let { current -> show(current.replacing(item)) }
        // A card of a fanned-out deck is changed there too.
        for ((key, cards) in deckCards.toList()) {
            if (cards.any { card -> card.feedItemId == item.feedItemId }) deckCards[key] = cards.map { card -> if (card.feedItemId == item.feedItemId) item.copy(stackItemCount = card.stackItemCount, stackUnreadCount = card.stackUnreadCount) else card }
        }
    }

    private fun remove(item: AgentFeedItemView) {
        list?.let { current -> show(current.removing(item.feedItemId)) }
        for ((key, cards) in deckCards.toList()) {
            if (cards.any { card -> card.feedItemId == item.feedItemId }) {
                deckCards[key] = cards.filter { card -> card.feedItemId != item.feedItemId }
                // The top card went: the next one tops the deck, which only the server's list says.
                if (list?.feedItems?.any { top -> top.feedStackKey == key } != true || item.isStack) scope.launch { load() }
            }
        }
    }

    private fun show(next: AgentFeedList) {
        list = next
        onUnreadCount(next.unreadCount)
    }
}
