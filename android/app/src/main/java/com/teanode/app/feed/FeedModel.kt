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
import com.teanode.kit.feed.foldDismissedFeedItems
import com.teanode.kit.feed.likeAgentFeedItem
import com.teanode.kit.feed.markAgentFeedRead
import com.teanode.kit.feed.resolveAgentFeedItem
import com.teanode.kit.feed.restoreAgentFeedItems
import com.teanode.kit.feed.showLessLikeAgentFeedItem
import com.teanode.kit.feed.unresolveAgentFeedItem
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
    /**
     * Whether this model is the Dismissed screen's: [readPage] reads the dismissed cards, the cards of one stack are shown
     * as a deck made from the entries already read, and a swipe or the info sheet restores a card instead of dismissing it.
     */
    val isDismissedOnly: Boolean = false,
) {
    /** The cards as read, newest first; the dismissed screen's [list] folds those of one stack into a deck. */
    private var entries: AgentFeedList? by mutableStateOf(null)

    /** The cards shown: the feed's own list, or in the dismissed screen the entries with each stack folded into its top card. */
    var list: AgentFeedList? by mutableStateOf(null)
        private set

    /** Whether the list was emptied by what the person just did and is being read again, so the screen shows loading and not "empty". */
    var isReadingAgain: Boolean by mutableStateOf(false)
        private set

    /** The reader of a stack's cards: the server's, or in the dismissed screen the entries already read. */
    private val readDeckPage: (suspend (feedStackKey: String, after: String?) -> AgentFeedPage)? =
        if (isDismissedOnly) { feedStackKey, _ -> AgentFeedPage(entries?.feedItems.orEmpty().filter { card -> card.feedStackKey == feedStackKey }, null, 0) } else readStackPage

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

    /** Called when cards were brought back from the dismissed screen, for the "Card restored" snackbar. */
    var onRestored: () -> Unit = {}

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
        val read = readDeckPage ?: return
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
        val read = readDeckPage ?: return
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

    val hasLoaded: Boolean get() = entries != null

    val hasPendingOutcome: Boolean get() = entries?.hasPendingOutcome == true

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
            readDeckPage?.let { read -> for (key in deckCards.keys.toList()) refreshDeck(key, read, isQuiet = key !in openDeckKeys) }
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
        val current = entries ?: return
        val cursor = current.nextCursor
        if (cursor.isNullOrEmpty() || isLoadingMore || isLoading) return
        isLoadingMore = true
        scope.launch {
            try {
                show((entries ?: current).appending(readPage(cursor)))
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
                entries?.let { current -> show(current.markedRead(feedItemIds.toSet(), newlyReadCount)) }
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

    /** Marks a card resolved, with an optional one line note; the card stays where it is and comes back muted. [doneText] is told when it is done. */
    fun resolve(item: AgentFeedItemView, note: String, doneText: String) = changeResolution(item, doneText) { connection.resolveAgentFeedItem(item.feedItemId, note) }

    /** Takes the resolution off a card. */
    fun unresolve(item: AgentFeedItemView, doneText: String) = changeResolution(item, doneText) { connection.unresolveAgentFeedItem(item.feedItemId) }

    private fun changeResolution(item: AgentFeedItemView, doneText: String, call: suspend () -> AgentFeedItemView) {
        if (busyFeedItemIds.containsKey(item.feedItemId)) return
        busyFeedItemIds[item.feedItemId] = true
        scope.launch {
            try {
                replace(call())
                onDone(doneText)
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
                // Kept out of the read below, which may start before the server's list is without it.
                hiddenFeedItemIds.add(item.feedItemId)
                remove(item)
                onDone(doneText)
                readAgainIfEmptied(item.feedItemId)
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                onProblem(failure)
            }
        }
    }

    /** What a swipe or the card's menu does: dismisses the card, or in the dismissed screen restores it. */
    fun swipeAway(item: AgentFeedItemView) {
        if (isDismissedOnly) restoreCards(listOf(item.feedItemId)) else dismiss(item)
    }

    /** What a swipe on a closed deck's top card does: the whole stack is dismissed, or on the dismissed screen restored. */
    fun swipeAwayDeck(top: AgentFeedItemView) {
        if (!isDismissedOnly) {
            dismissStack(top)
            return
        }
        val stackCardIds = (listOf(top) + deckCards[top.feedStackKey].orEmpty()).map { card -> card.feedItemId }.distinct()
        restoreCards(stackCardIds)
    }

    /**
     * When what the person just did left the list with no cards, the loading mark is shown and the list is read again, and
     * only that read can say the list is empty. The cards this model took off stay hidden from it.
     */
    private fun readAgainIfEmptied(hiddenFeedItemId: String) {
        if (list?.feedItems?.isEmpty() != true) {
            hiddenFeedItemIds.remove(hiddenFeedItemId)
            return
        }
        isReadingAgain = true
        scope.launch {
            try {
                refresh()
            } finally {
                isReadingAgain = false
                hiddenFeedItemIds.remove(hiddenFeedItemId)
            }
        }
    }

    /** Dismisses one card: it leaves at once, and [onDismissed] says so when the server has answered. */
    fun dismiss(item: AgentFeedItemView) = dismissCards(listOf(item.feedItemId)) { ids -> connection.dismissAgentFeedItems(feedItemIds = ids) }

    /** Dismisses every card of the stack [top] tops. */
    fun dismissStack(top: AgentFeedItemView) {
        val feedStackKey = top.feedStackKey
        val shownIds = (entries?.feedItems.orEmpty() + deckCards[feedStackKey].orEmpty()).filter { item -> item.feedStackKey == feedStackKey }.map { item -> item.feedItemId }
        dismissCards(shownIds, hiddenStackKey = feedStackKey) { connection.dismissAgentFeedItems(feedStackKey = feedStackKey) }
    }

    /** Whether any card shown is read and has no choice waiting, which "Dismiss read" would take out. */
    val hasDismissibleReadCard: Boolean get() = entries?.feedItems.orEmpty().any { item -> isReadAndClosed(item) }

    /** Dismisses every card that is read and has no open choice. */
    fun dismissRead() {
        val shownIds = entries?.feedItems.orEmpty().filter { item -> isReadAndClosed(item) }.map { item -> item.feedItemId }
        dismissCards(shownIds, isReloaded = true) { connection.dismissAgentFeedItems(isEveryReadCard = true) }
    }

    private fun isReadAndClosed(item: AgentFeedItemView): Boolean = item.isRead && !item.isAwaitingChoice

    /** Brings back cards a dismissal took out, then reads the feed so they are where they were. */
    fun restore(feedItemIds: List<String>) {
        // A feed with no cards is read again before it says anything.
        if (list?.feedItems?.isEmpty() == true) isReadingAgain = true
        scope.launch {
            try {
                connection.restoreAgentFeedItems(feedItemIds)
                load()
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                onProblem(failure)
            } finally {
                isReadingAgain = false
            }
        }
    }

    /**
     * Restores cards from the dismissed screen: they leave the list at once and stay out of any read already on its way,
     * then [onRestored] says so. When the call fails they are shown again by reading the list.
     */
    private fun restoreCards(feedItemIds: List<String>) {
        hiddenFeedItemIds.addAll(feedItemIds)
        entries?.let { current -> show(current) }
        hideInDecks()
        val isEmptied = list?.feedItems?.isEmpty() == true
        if (isEmptied) isReadingAgain = true
        scope.launch {
            var isFailed = false
            try {
                connection.restoreAgentFeedItems(feedItemIds)
                onRestored()
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                isFailed = true
                onProblem(failure)
            } finally {
                // A card restored stays out of this list for good: the server no longer lists it as dismissed.
                if (isFailed) hiddenFeedItemIds.removeAll(feedItemIds.toSet())
                if (isFailed || isEmptied) {
                    try {
                        refresh()
                    } finally {
                        isReadingAgain = false
                    }
                }
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
        val shownItems = (entries?.feedItems.orEmpty() + deckCards.values.flatten()).filter { item -> item.feedItemId in shownIds }
        val isStackTouched = hiddenStackKey != null || shownItems.any { item -> item.isStack || item.feedStackKey.isNotEmpty() }
        hiddenFeedItemIds.addAll(shownIds)
        hiddenStackKey?.let { key -> hiddenFeedStackKeys.add(key) }
        entries?.let { current -> show(current) }
        hideInDecks()
        // A list this leaves with no cards is read again before it says it is empty.
        val isEmptied = list?.feedItems?.isEmpty() == true
        if (isEmptied) isReadingAgain = true
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
                if (isFailed || isStackTouched || isReloaded || isEmptied) {
                    try {
                        refresh()
                    } finally {
                        isReadingAgain = false
                    }
                }
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
        entries?.let { current -> show(current.replacing(item)) }
        // A card of a fanned-out deck is changed there too.
        for ((key, cards) in deckCards.toList()) {
            if (cards.any { card -> card.feedItemId == item.feedItemId }) deckCards[key] = cards.map { card -> if (card.feedItemId == item.feedItemId) item.copy(stackItemCount = card.stackItemCount, stackUnreadCount = card.stackUnreadCount) else card }
        }
    }

    private fun remove(item: AgentFeedItemView) {
        entries?.let { current -> show(current.removing(item.feedItemId)) }
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
        entries = shown
        list = if (isDismissedOnly) shown.copy(feedItems = foldDismissedFeedItems(shown.feedItems)) else shown
        onUnreadCount(shown.unreadCount)
    }
}
