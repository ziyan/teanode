package com.teanode.kit.feed

import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.chats.AgentReference
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.mapNotNull
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

/** The agent feed's GraphQL, as `internal/api/v1api/apigraph/agent_feed.go` declares it. */
object AgentFeedDocuments {
    private const val FEED_ITEM_FIELDS = """feedItemId feedItemKind feedSourceKind feedSourceId feedSourceTitle runConversationId title body pictureSymbol
    factRows { factLabel factValue } sourceLinks { sourceTitle sourceUrl }
    recordReferences { itemId threadId subject from path name recordingId recordingTitle photoId photoTitle financeTransactionId postedOn amount currencyCode merchantName description }
    choices { choiceKey choiceLabel choiceRole } chosenChoiceKey chosenAt outcomeText isOutcomePending feedUrgency isLiked isRead isDismissed postedAt updatedAt
    feedStackKey feedStackTitle stackPosition stackItemCount stackUnreadCount feedCardLayout layoutContent"""

    const val LIST_AGENT_FEED = """query (${'$'}first: Int, ${'$'}after: String, ${'$'}includeDismissed: Boolean) {
  ListAgentFeed(first: ${'$'}first, after: ${'$'}after, includeDismissed: ${'$'}includeDismissed) {
    feedItems { $FEED_ITEM_FIELDS }
    nextCursor unreadCount
  }
}"""
    const val LIST_AGENT_FEED_STACK = """query (${'$'}feedStackKey: String!, ${'$'}first: Int, ${'$'}after: String) {
  ListAgentFeedStack(feedStackKey: ${'$'}feedStackKey, first: ${'$'}first, after: ${'$'}after) {
    feedItems { $FEED_ITEM_FIELDS }
    nextCursor unreadCount
  }
}"""
    const val GET_AGENT_FEED_ITEM = """query (${'$'}feedItemId: String!) {
  GetAgentFeedItem(feedItemId: ${'$'}feedItemId) { $FEED_ITEM_FIELDS }
}"""
    const val CHOOSE_AGENT_FEED_ITEM = """mutation (${'$'}feedItemId: String!, ${'$'}choiceKey: String!) {
  ChooseAgentFeedItem(feedItemId: ${'$'}feedItemId, choiceKey: ${'$'}choiceKey) { $FEED_ITEM_FIELDS }
}"""
    const val LIKE_AGENT_FEED_ITEM = """mutation (${'$'}feedItemId: String!, ${'$'}isLiked: Boolean!) {
  LikeAgentFeedItem(feedItemId: ${'$'}feedItemId, isLiked: ${'$'}isLiked) { $FEED_ITEM_FIELDS }
}"""
    const val SHOW_LESS_LIKE_AGENT_FEED_ITEM = """mutation (${'$'}feedItemId: String!) {
  ShowLessLikeAgentFeedItem(feedItemId: ${'$'}feedItemId) { $FEED_ITEM_FIELDS }
}"""
    const val DISMISS_AGENT_FEED_ITEM = """mutation (${'$'}feedItemId: String!) {
  DismissAgentFeedItem(feedItemId: ${'$'}feedItemId) { $FEED_ITEM_FIELDS }
}"""
    const val MARK_AGENT_FEED_READ = """mutation (${'$'}feedItemIds: [String!]!) { MarkAgentFeedRead(feedItemIds: ${'$'}feedItemIds) }"""
    const val DISCUSS_AGENT_FEED_ITEM = """mutation (${'$'}feedItemId: String!) { DiscussAgentFeedItem(feedItemId: ${'$'}feedItemId) }"""

    /** The name of the subscription's field, also the key its events arrive under. */
    const val FEED_CHANGED_FIELD = "AgentFeedChanged"
    const val AGENT_FEED_CHANGED = """subscription { AgentFeedChanged { feedItemId feedChange unreadCount } }"""
}

/** AgentFeedFactRow is one line of a card's short table of facts. */
@Serializable
data class AgentFeedFactRow(val factLabel: String, val factValue: String)

/** AgentFeedSourceLink is a page a card was made from, listed on its info sheet. */
@Serializable
data class AgentFeedSourceLink(val sourceTitle: String, val sourceUrl: String)

/** AgentFeedChoice is one button of a card. */
@Serializable
data class AgentFeedChoice(val choiceKey: String, val choiceLabel: String, val choiceRole: String) {
    val role: AgentFeedChoiceRole get() = AgentFeedChoiceRole.of(choiceRole)
}

/**
 * AgentFeedChoiceRole is what a choice does: an action is carried out by the agent in the main conversation, a dismiss
 * closes the card, a discuss opens the main conversation about it, and an open goes to the thing the card is about (a
 * coding session). A role the app does not know is treated as an action, the one that asks the server.
 */
enum class AgentFeedChoiceRole(val roleName: String) {
    Action("action"),
    Dismiss("dismiss"),
    Discuss("discuss"),
    Open("open"),
    ;

    companion object {
        fun of(roleName: String): AgentFeedChoiceRole = entries.firstOrNull { role -> role.roleName == roleName } ?: Action
    }
}

/** AgentFeedItemView is one card of the agent's feed. */
@Serializable
data class AgentFeedItemView(
    val feedItemId: String,
    /** update, decision, alert, report, idea, news or mail_organize. */
    val feedItemKind: String = "update",
    /** goal, schedule, alert, speak_first, coding_session or agent. */
    val feedSourceKind: String = "agent",
    val feedSourceId: String = "",
    /** The goal's or schedule's name; empty otherwise. */
    val feedSourceTitle: String = "",
    val runConversationId: String = "",
    val title: String,
    /** Markdown, a few lines. */
    val body: String = "",
    /** An SF Symbol name, or an emoji. */
    val pictureSymbol: String = "",
    val factRows: List<AgentFeedFactRow> = emptyList(),
    val sourceLinks: List<AgentFeedSourceLink> = emptyList(),
    val recordReferences: List<AgentReference> = emptyList(),
    val choices: List<AgentFeedChoice> = emptyList(),
    val chosenChoiceKey: String? = null,
    val chosenAt: String? = null,
    val outcomeText: String = "",
    /** A choice sent the agent to work in the main conversation and it has not answered yet. */
    val isOutcomePending: Boolean = false,
    /** now, today or whenever. */
    val feedUrgency: String = "whenever",
    val isLiked: Boolean = false,
    val isRead: Boolean = false,
    val isDismissed: Boolean = false,
    val postedAt: String = "",
    val updatedAt: String = "",
    /** The stack this card belongs to; empty for a card that stands alone. */
    val feedStackKey: String = "",
    /** The stack's name ("Morning brief"), shown instead of the source's in "+N more from"; empty when the stack has none. */
    val feedStackTitle: String = "",
    val stackPosition: Int? = null,
    /** On the top card of a stack in the feed: how many cards the stack holds, that one included; 1 for a card alone. */
    val stackItemCount: Int = 1,
    val stackUnreadCount: Int = 0,
    /** text, list, weather or agenda; a layout the app does not know is drawn as text. */
    val feedCardLayout: String = "text",
    /** The layout's structured content, a JSON object written as text; empty for a text card. */
    val layoutContent: String = "",
) {
    /** How the card is drawn: its layout when the content reads, else the plain text of its body. */
    val layout: FeedLayout get() = FeedLayout.read(feedCardLayout, layoutContent)

    /** Whether this card is the top of a pile of cards from one source. */
    val isStack: Boolean get() = stackItemCount > 1 && feedStackKey.isNotEmpty()

    /** How many more cards the stack holds under this one. */
    val moreInStackCount: Int get() = maxOf(0, stackItemCount - 1)

    /** What the stack is called: its own name, else the goal's or schedule's. */
    val stackName: String get() = feedStackTitle.ifEmpty { feedSourceTitle }

    /** How many card edges show behind a stack's top card: one for a pair, two for more. */
    val stackEdgeCount: Int get() = if (!isStack) 0 else if (stackItemCount == 2) 1 else 2

    /** The choice that was made, when one was. */
    val chosenChoice: AgentFeedChoice? get() = chosenChoiceKey?.let { key -> choices.firstOrNull { choice -> choice.choiceKey == key } }

    /** Whether a choice was made, so the buttons give way to what came of it. */
    val isChosen: Boolean get() = !chosenChoiceKey.isNullOrEmpty()

    /** Whether the card still asks for a choice. */
    val isAwaitingChoice: Boolean get() = choices.isNotEmpty() && !isChosen && !isDismissed

    val isUrgent: Boolean get() = feedUrgency == "now"

    /** Whether the card is about a coding session, which "open" goes to. */
    val isCodingSession: Boolean get() = feedSourceKind == "coding_session"

    /** The computer and the pane of a coding session's card (`feedSourceId` is the two joined by a slash); null for any other card. */
    val codingSessionPlace: Pair<String, String>?
        get() {
            if (!isCodingSession) return null
            val computer = feedSourceId.substringBefore('/', "")
            val paneId = feedSourceId.substringAfter('/', "")
            return if (computer.isEmpty() || paneId.isEmpty()) null else computer to paneId
        }

    /** The words the card settled on: what the agent said came of the choice, or nothing yet while it works. */
    val settledText: String? get() = outcomeText.takeIf { text -> text.isNotEmpty() && isChosen && !isOutcomePending }

    /** Whether the main conversation holds the agent's work on this card's choice, so "Open chat" has something to show. */
    val canOpenChat: Boolean
        get() = isChosen && isOutcomePending && chosenChoice?.role == AgentFeedChoiceRole.Action
}

/** AgentFeedPage is one page of the feed, newest first, and how many cards are unread in all. */
@Serializable
data class AgentFeedPage(
    val feedItems: List<AgentFeedItemView> = emptyList(),
    val nextCursor: String? = null,
    val unreadCount: Int = 0,
)

/** AgentFeedChanged is what the server says happened to a card. [feedChange] is posted, replaced, settled or dismissed. */
@Serializable
data class AgentFeedChanged(val feedItemId: String, val feedChange: String, val unreadCount: Int = 0)

/**
 * AgentFeedList is the feed as a screen holds it: the cards read so far, newest first, where the next page starts, and
 * how many are unread. It changes only by the calls below, so what the screen shows is tested without a phone.
 */
data class AgentFeedList(
    val feedItems: List<AgentFeedItemView> = emptyList(),
    val nextCursor: String? = null,
    val unreadCount: Int = 0,
) {
    val hasMore: Boolean get() = !nextCursor.isNullOrEmpty()

    companion object {
        fun first(page: AgentFeedPage): AgentFeedList = AgentFeedList(page.feedItems.filter { item -> !item.isDismissed }, page.nextCursor, page.unreadCount)
    }

    /** The list with the next page below it; a card already shown keeps its place and takes its newer content. */
    fun appending(page: AgentFeedPage): AgentFeedList {
        val shownIds = feedItems.map { item -> item.feedItemId }.toSet()
        val newer = page.feedItems.associateBy { item -> item.feedItemId }
        val updated = feedItems.map { item -> newer[item.feedItemId] ?: item }
        val added = page.feedItems.filter { item -> item.feedItemId !in shownIds && !item.isDismissed }
        return AgentFeedList(updated + added, page.nextCursor, page.unreadCount)
    }

    /**
     * The list with this card in it: replaced where it is, a dismissed one taken out, and a posted or replaced one at
     * the top, since the agent moves a card it updates to the top.
     */
    fun with(item: AgentFeedItemView, isMovedToTop: Boolean = false): AgentFeedList {
        if (item.isDismissed) return removing(item.feedItemId)
        val position = feedItems.indexOfFirst { shown -> shown.feedItemId == item.feedItemId }
        // A card read on its own says it is alone; the list knows the stack it tops.
        val kept = feedItems.getOrNull(position)
        val shown = if (kept != null && kept.isStack && kept.feedStackKey == item.feedStackKey) item.copy(stackItemCount = kept.stackItemCount, stackUnreadCount = kept.stackUnreadCount) else item
        val without = if (position >= 0) feedItems.filterIndexed { index, _ -> index != position } else feedItems
        val inserted = when {
            isMovedToTop || position < 0 -> listOf(shown) + without
            else -> without.toMutableList().also { list -> list.add(position, shown) }
        }
        val nextUnread = if (position < 0 && !item.isRead) unreadCount + 1 else unreadCount
        return copy(feedItems = inserted, unreadCount = nextUnread)
    }

    fun removing(feedItemId: String): AgentFeedList {
        val removed = feedItems.firstOrNull { item -> item.feedItemId == feedItemId } ?: return this
        return copy(feedItems = feedItems.filter { item -> item.feedItemId != feedItemId }, unreadCount = if (removed.isRead) unreadCount else maxOf(0, unreadCount - 1))
    }

    /** The cards named, marked read, and the count the server said was newly read taken off the unread count. */
    fun markedRead(feedItemIds: Set<String>, newlyReadCount: Int): AgentFeedList =
        copy(
            feedItems = feedItems.map { item -> if (item.feedItemId in feedItemIds) item.copy(isRead = true) else item },
            unreadCount = maxOf(0, unreadCount - newlyReadCount),
        )

    fun withUnreadCount(count: Int): AgentFeedList = copy(unreadCount = count)

    /** The cards shown that nobody has read yet, which the screen asks the server to mark as they are seen. */
    fun isUnread(feedItemId: String): Boolean = feedItems.firstOrNull { item -> item.feedItemId == feedItemId }?.isRead == false

    /** Whether an event is about a card that is shown, so that it is read again (a posted card is new to the list). */
    fun shows(feedItemId: String): Boolean = feedItems.any { item -> item.feedItemId == feedItemId }
}

@Serializable
private class ListAgentFeedAnswer(val ListAgentFeed: AgentFeedPage)

@Serializable
private class ListAgentFeedStackAnswer(val ListAgentFeedStack: AgentFeedPage)

@Serializable
private class GetAgentFeedItemAnswer(val GetAgentFeedItem: AgentFeedItemView)

@Serializable
private class ChooseAgentFeedItemAnswer(val ChooseAgentFeedItem: AgentFeedItemView)

@Serializable
private class LikeAgentFeedItemAnswer(val LikeAgentFeedItem: AgentFeedItemView)

@Serializable
private class ShowLessLikeAgentFeedItemAnswer(val ShowLessLikeAgentFeedItem: AgentFeedItemView)

@Serializable
private class DismissAgentFeedItemAnswer(val DismissAgentFeedItem: AgentFeedItemView)

@Serializable
private class MarkAgentFeedReadAnswer(val MarkAgentFeedRead: Int)

@Serializable
private class DiscussAgentFeedItemAnswer(val DiscussAgentFeedItem: String)

/** How many cards a page of the feed holds. */
const val AGENT_FEED_PAGE_SIZE = 20

/** A page of the feed, newest first; [after] is the previous page's `nextCursor`. */
suspend fun Connection.agentFeed(after: String? = null, first: Int = AGENT_FEED_PAGE_SIZE, includeDismissed: Boolean = false): AgentFeedPage {
    val variables = hashMapOf<String, JsonElement>("first" to JsonPrimitive(first), "includeDismissed" to JsonPrimitive(includeDismissed))
    if (after != null) variables["after"] = JsonPrimitive(after)
    return execute<ListAgentFeedAnswer>(AgentFeedDocuments.LIST_AGENT_FEED, variables).ListAgentFeed
}

/** One stack's cards, newest first (or by their positions), a page at a time; [after] is the previous page's `nextCursor`. */
suspend fun Connection.agentFeedStack(feedStackKey: String, after: String? = null, first: Int = AGENT_FEED_PAGE_SIZE): AgentFeedPage {
    val variables = hashMapOf<String, JsonElement>("feedStackKey" to JsonPrimitive(feedStackKey), "first" to JsonPrimitive(first))
    if (after != null) variables["after"] = JsonPrimitive(after)
    return execute<ListAgentFeedStackAnswer>(AgentFeedDocuments.LIST_AGENT_FEED_STACK, variables).ListAgentFeedStack
}

suspend fun Connection.agentFeedItem(feedItemId: String): AgentFeedItemView =
    execute<GetAgentFeedItemAnswer>(AgentFeedDocuments.GET_AGENT_FEED_ITEM, mapOf("feedItemId" to JsonPrimitive(feedItemId))).GetAgentFeedItem

/** Answers a card with one of its choices, without opening any conversation. A card already answered is refused by the server. */
suspend fun Connection.chooseAgentFeedItem(feedItemId: String, choiceKey: String): AgentFeedItemView =
    execute<ChooseAgentFeedItemAnswer>(
        AgentFeedDocuments.CHOOSE_AGENT_FEED_ITEM,
        mapOf("feedItemId" to JsonPrimitive(feedItemId), "choiceKey" to JsonPrimitive(choiceKey)),
    ).ChooseAgentFeedItem

/** Says "more like this" (or takes it back); the agent keeps it as a preference about the card's subject. */
suspend fun Connection.likeAgentFeedItem(feedItemId: String, isLiked: Boolean): AgentFeedItemView =
    execute<LikeAgentFeedItemAnswer>(
        AgentFeedDocuments.LIKE_AGENT_FEED_ITEM,
        mapOf("feedItemId" to JsonPrimitive(feedItemId), "isLiked" to JsonPrimitive(isLiked)),
    ).LikeAgentFeedItem

/** Records the opposite preference and dismisses the card. */
suspend fun Connection.showLessLikeAgentFeedItem(feedItemId: String): AgentFeedItemView =
    execute<ShowLessLikeAgentFeedItemAnswer>(AgentFeedDocuments.SHOW_LESS_LIKE_AGENT_FEED_ITEM, mapOf("feedItemId" to JsonPrimitive(feedItemId))).ShowLessLikeAgentFeedItem

suspend fun Connection.dismissAgentFeedItem(feedItemId: String): AgentFeedItemView =
    execute<DismissAgentFeedItemAnswer>(AgentFeedDocuments.DISMISS_AGENT_FEED_ITEM, mapOf("feedItemId" to JsonPrimitive(feedItemId))).DismissAgentFeedItem

/** Marks cards read; answers how many were newly read. */
suspend fun Connection.markAgentFeedRead(feedItemIds: List<String>): Int =
    execute<MarkAgentFeedReadAnswer>(
        AgentFeedDocuments.MARK_AGENT_FEED_READ,
        mapOf("feedItemIds" to JsonArray(feedItemIds.map { feedItemId -> JsonPrimitive(feedItemId) })),
    ).MarkAgentFeedRead

/** Opens the main conversation about a card (the card is attached to the next message there) and answers its id. */
suspend fun Connection.discussAgentFeedItem(feedItemId: String): String =
    execute<DiscussAgentFeedItemAnswer>(AgentFeedDocuments.DISCUSS_AGENT_FEED_ITEM, mapOf("feedItemId" to JsonPrimitive(feedItemId))).DiscussAgentFeedItem

/**
 * AgentFeedEvents streams what happens to the person's cards (posted, replaced, settled, dismissed) so that an open
 * feed and the unread count stay current. The stream ends with an error when the connection drops; its owner opens a
 * new one.
 */
object AgentFeedEvents {
    fun changes(connection: Connection): Flow<AgentFeedChanged> =
        connection.subscribe(AgentFeedDocuments.AGENT_FEED_CHANGED).mapNotNull { data -> read(data) }

    internal fun read(data: JsonElement): AgentFeedChanged? {
        val element = (data as? JsonObject)?.get(AgentFeedDocuments.FEED_CHANGED_FIELD) ?: return null
        if (element is JsonNull) return null
        return TeaNodeJson.decodeFromJsonElement(AgentFeedChanged.serializer(), element)
    }
}
