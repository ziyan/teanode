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
import com.teanode.kit.feed.dismissAgentFeedItems
import com.teanode.kit.feed.likeAgentFeedItem
import com.teanode.kit.feed.markAgentFeedRead
import com.teanode.kit.feed.restoreAgentFeedItems
import com.teanode.kit.feed.showLessLikeAgentFeedItem
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

/** How long cards are on screen before they are reported read together, so scrolling past a screenful is one call (ios FeedModel.noteAppeared). */
const val FEED_READ_DELAY_MS = 1_500L

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

    /** Called with the ids of the cards a dismissal took out, for the snackbar that offers Undo; never with an empty list. */
    var onDismissed: (List<String>) -> Unit = {}

    /**
     * The decks fanned out in place, by stack key, and the cards each holds (the top card first). A stack's cards are read
     * with the feed, since its deck under the top card is its own next cards.
     */
    val openDeckKeys = mutableStateMapOf<String, Boolean>()
    val deckCards = mutableStateMapOf<String, List<AgentFeedItemView>>()

    /** Whether a deck is fanned out. */
    fun isDeckOpen(feedStackKey: String): Boolean = openDeckKeys.containsKey(feedStackKey)

    /** Fans a stack's deck out in place once its cards are read (a deck whose cards cannot be read stays a deck). */
    fun openDeck(feedStackKey: String) {
        val read = readStackPage ?: return
        scope.launch {
            if (deckCards[feedStackKey] == null && !refreshDeck(feedStackKey, read)) return@launch
            openDeckKeys[feedStackKey] = true
        }
    }

    /** Gathers a deck back into its top card. */
    fun closeDeck(feedStackKey: String) {
        openDeckKeys.remove(feedStackKey)
    }

    /** Reads a shown stack's cards for its deck when they are not read yet; a failure is told only when the deck is opened. */
    suspend fun loadDeckIfNeeded(feedStackKey: String) {
        val read = readStackPage ?: return
        if (deckCards[feedStackKey] == null) refreshDeck(feedStackKey, read, isQuiet = true)
    }

    /** Reads the cards of a deck (one page of fifty, as the iPhone does); whether they were read. */
    private suspend fun refreshDeck(feedStackKey: String, read: suspend (String, String?) -> AgentFeedPage, isQuiet: Boolean = false): Boolean = try {
        deckCards[feedStackKey] = read(feedStackKey, null).feedItems.filter { card -> !isHidden(card) }
        true
    } catch (failure: CancellationException) {
        throw failure
    } catch (failure: Throwable) {
        if (!isQuiet) onProblem(failure)
        false
    }

    /** The hearts the person just pressed, shown before the server answers. */
    private val likedOverrides = mutableStateMapOf<String, Boolean>()
    private val readFeedItemIds = HashSet<String>()
    private val pendingReadFeedItemIds = LinkedHashSet<String>()
    private var readJob: Job? = null
    private var isLoading = false
    private val loadMutex = Mutex()

    /** Cards (and whole stacks) being dismissed, left out of what is shown until the server has answered, so a read in between does not bring them back. */
    private val hiddenFeedItemIds = HashSet<String>()
    private val hiddenFeedStackKeys = HashSet<String>()

    val hasLoaded: Boolean get() = list != null

    val hasPendingOutcome: Boolean get() = list?.hasPendingOutcome == true

    fun isLiked(item: AgentFeedItemView): Boolean = likedOverrides[item.feedItemId] ?: item.isLiked

    /** Reads the newest page again. What was shown stays when it cannot be read, and the cards past the first page are dropped. */
    suspend fun load() {
        if (loadMutex.isLocked) return
        loadMutex.withLock { readNewestPage() }
    }

    /**
     * The person pulled the list down: a read under way may have started before what they expect to see, so it is waited
     * for and the feed is read once more, rather than the pull ending at once with nothing new.
     */
    suspend fun refresh() {
        loadMutex.withLock { readNewestPage() }
    }

    private suspend fun readNewestPage() {
        isLoading = true
        try {
            show(AgentFeedList.first(readPage(null)))
            loadFailure = null
            // The stacks shown, open or tucked as decks, are read again with the feed, and those no longer in it are forgotten.
            val shownStackKeys = list?.feedItems.orEmpty().filter { item -> item.isStack }.map { item -> item.feedStackKey }.toSet()
            for (key in deckCards.keys.toList()) if (key !in shownStackKeys) deckCards.remove(key)
            readStackPage?.let { read -> for (key in deckCards.keys.toList()) refreshDeck(key, read, isQuiet = key !in openDeckKeys) }
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

    /** Dismisses one card: it leaves at once, and [onDismissed] says so when the server has answered. */
    fun dismiss(item: AgentFeedItemView) = dismissCards(listOf(item.feedItemId)) { ids -> connection.dismissAgentFeedItems(feedItemIds = ids) }

    /** Dismisses every card of the stack [top] tops. */
    fun dismissStack(top: AgentFeedItemView) {
        val feedStackKey = top.feedStackKey
        val shownIds = (list?.feedItems.orEmpty() + deckCards[feedStackKey].orEmpty()).filter { item -> item.feedStackKey == feedStackKey }.map { item -> item.feedItemId }
        dismissCards(shownIds, hiddenStackKey = feedStackKey) { connection.dismissAgentFeedItems(feedStackKey = feedStackKey) }
    }

    /** Whether any card shown is read and has no choice waiting, which "Dismiss read" would take out. */
    val hasDismissibleReadCard: Boolean get() = list?.feedItems.orEmpty().any { item -> isReadAndClosed(item) }

    /** Dismisses every card that is read and has no open choice. */
    fun dismissRead() {
        val shownIds = list?.feedItems.orEmpty().filter { item -> isReadAndClosed(item) }.map { item -> item.feedItemId }
        dismissCards(shownIds, isReloaded = true) { connection.dismissAgentFeedItems(isEveryReadCard = true) }
    }

    private fun isReadAndClosed(item: AgentFeedItemView): Boolean = item.isRead && !item.isAwaitingChoice

    /** Brings back cards a dismissal took out, then reads the feed so they are where they were. */
    fun restore(feedItemIds: List<String>) {
        scope.launch {
            try {
                connection.restoreAgentFeedItems(feedItemIds)
                load()
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                onProblem(failure)
            }
        }
    }

    /**
     * Takes [shownIds] out of what is shown now and asks the server to dismiss, by [call]. The server's answer names the
     * cards really dismissed, which is what is counted and what Undo restores. When the call fails the feed is read
     * again, so the cards come back. A dismissal of a stack or of a card that tops one, or of many at once, is followed by
     * a read, since which card tops a stack now, or what else was read, only the server's list says.
     */
    private fun dismissCards(shownIds: List<String>, hiddenStackKey: String? = null, isReloaded: Boolean = false, call: suspend (List<String>) -> List<String>) {
        if (shownIds.isEmpty() && hiddenStackKey == null && !isReloaded) return
        val shownItems = (list?.feedItems.orEmpty() + deckCards.values.flatten()).filter { item -> item.feedItemId in shownIds }
        val isStackTouched = hiddenStackKey != null || shownItems.any { item -> item.isStack || item.feedStackKey.isNotEmpty() }
        hiddenFeedItemIds.addAll(shownIds)
        hiddenStackKey?.let { key -> hiddenFeedStackKeys.add(key) }
        list?.let { current -> show(current) }
        hideInDecks()
        scope.launch {
            var isFailed = false
            try {
                val dismissedIds = call(shownIds)
                if (dismissedIds.isNotEmpty()) onDismissed(dismissedIds)
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                isFailed = true
                onProblem(failure)
            } finally {
                hiddenFeedItemIds.removeAll(shownIds.toSet())
                hiddenStackKey?.let { key -> hiddenFeedStackKeys.remove(key) }
                if (isFailed || isStackTouched || isReloaded) refresh()
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

    private fun isHidden(item: AgentFeedItemView): Boolean = item.feedItemId in hiddenFeedItemIds || (item.feedStackKey.isNotEmpty() && item.feedStackKey in hiddenFeedStackKeys)

    private fun hideInDecks() {
        for ((key, cards) in deckCards.toList()) if (cards.any { card -> isHidden(card) }) deckCards[key] = cards.filter { card -> !isHidden(card) }
    }

    /** Shows the list without the cards being dismissed, which leave the unread count as they leave the list. */
    private fun show(next: AgentFeedList) {
        var shown = next
        for (item in next.feedItems) if (isHidden(item)) shown = shown.removing(item.feedItemId)
        list = shown
        onUnreadCount(shown.unreadCount)
    }
}
