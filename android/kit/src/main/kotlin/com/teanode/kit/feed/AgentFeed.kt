package com.teanode.kit.feed

import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeJson
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.mapNotNull
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

/** The agent feed's GraphQL: the same text as the iPhone app's (ios/TeaNodeKit AgentFeed.swift), which the server's tests check against the schema. */
object AgentFeedDocuments {
    private const val FEED_ITEM_FIELDS = """feedItemId feedItemKind feedSourceKind feedSourceId feedSourceTitle runConversationId title body pictureSymbol
factRows { factLabel factValue } sourceLinks { sourceTitle sourceUrl } choices { choiceKey choiceLabel choiceRole }
chosenChoiceKey chosenAt outcomeText isOutcomePending feedUrgency isLiked isRead isDismissed postedAt updatedAt
feedStackKey stackItemCount stackUnreadCount feedStackTitle stackPosition feedCardLayout layoutContent
feedMedia { mediaKey feedMediaKind imageUrl videoUrl linkUrl siteName mediaCaption widthPixels heightPixels listRowIndex }"""

    const val LIST_AGENT_FEED_STACK = """query (${'$'}feedStackKey: String!, ${'$'}first: Int, ${'$'}after: String) {
  ListAgentFeedStack(feedStackKey: ${'$'}feedStackKey, first: ${'$'}first, after: ${'$'}after) {
    feedItems { $FEED_ITEM_FIELDS }
    nextCursor
    unreadCount
  }
}"""
    const val LIST_AGENT_FEED = """query (${'$'}first: Int, ${'$'}after: String, ${'$'}includeDismissed: Boolean) {
  ListAgentFeed(first: ${'$'}first, after: ${'$'}after, includeDismissed: ${'$'}includeDismissed) {
    feedItems { $FEED_ITEM_FIELDS }
    nextCursor
    unreadCount
  }
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
    const val MARK_AGENT_FEED_READ = """mutation (${'$'}feedItemIds: [String!]!) {
  MarkAgentFeedRead(feedItemIds: ${'$'}feedItemIds)
}"""
    const val DISCUSS_AGENT_FEED_ITEM = """mutation (${'$'}feedItemId: String!) {
  DiscussAgentFeedItem(feedItemId: ${'$'}feedItemId)
}"""

    /** The name of the subscription's field, also the key its events arrive under. */
    const val FEED_CHANGED_FIELD = "AgentFeedChanged"
    const val AGENT_FEED_CHANGED = """subscription { AgentFeedChanged { feedItemId feedChange unreadCount } }"""
}

/** The kinds of picture a card carries: one of the person's photos, one of their videos, or the picture of a web page. */
enum class AgentFeedMediaKind { Photo, Video, Picture;

    companion object {
        /** A kind a newer server sends is drawn as a picture: its image is all the app can be sure to have. */
        fun of(name: String): AgentFeedMediaKind = when (name) {
            "photo" -> Photo
            "video" -> Video
            else -> Picture
        }
    }
}

/**
 * AgentFeedMediaView is one picture of a card. [imageUrl] and [videoUrl] are paths on the server that work for the
 * signed-in person only, so they are fetched with the session and never as a public address. [linkUrl] is the page a
 * picture is of. A [listRowIndex] makes it the thumbnail of that row of a list card instead of a picture of the card.
 */
@Serializable
data class AgentFeedMediaView(
    val mediaKey: String = "",
    /** photo, video or picture. */
    val feedMediaKind: String = "picture",
    /** The picture, or a video's poster. */
    val imageUrl: String = "",
    /** A video's file; missing for the others. */
    val videoUrl: String? = null,
    /** A picture's page; missing for the others. */
    val linkUrl: String? = null,
    val siteName: String? = null,
    val mediaCaption: String? = null,
    /** The size in pixels; missing when the server does not know it. */
    val widthPixels: Int? = null,
    val heightPixels: Int? = null,
    val listRowIndex: Int? = null,
) {
    val kind: AgentFeedMediaKind get() = AgentFeedMediaKind.of(feedMediaKind)

    val caption: String get() = mediaCaption.orEmpty()

    val site: String get() = siteName.orEmpty()

    /** Whether the picture is taller than wide, which a card shows at 4:5 instead of 16:9; false when its size is not known. */
    val isPortrait: Boolean get() = (widthPixels ?: 0) > 0 && (heightPixels ?: 0) > (widthPixels ?: 0)

    /** The width over the height one picture shown alone is cropped to: 16:9, or 4:5 for a portrait photo. */
    val singleAspectRatio: Float get() = if (isPortrait) 4f / 5f else 16f / 9f

    /** The address the image is read from: a path on this server, never a full address of another site. */
    val imagePath: String? get() = imageUrl.takeIf { path -> path.startsWith("/") }

    val videoPath: String? get() = videoUrl?.takeIf { path -> path.startsWith("/") }

    /**
     * The photo a video is, from its path (`/api/v1/photos/<id>/previews/video`), which the app asks a playable address
     * for as the Photos viewer does; null for anything else.
     */
    val videoPhotoId: String?
        get() {
            val segments = videoUrl?.substringBefore('?')?.split('/')?.filter { segment -> segment.isNotEmpty() } ?: return null
            val index = segments.indexOf("photos")
            return if (index >= 0 && index + 1 < segments.size) segments[index + 1] else null
        }

    /** The page a picture opens; only http and https are followed. */
    val pageAddress: String? get() = linkUrl?.takeIf { address -> address.lowercase().let { lowered -> lowered.startsWith("http://") || lowered.startsWith("https://") } }
}

/** How a card's pictures are laid out: one full width, two to four in a grid, more in a row that scrolls sideways. */
enum class AgentFeedMediaArrangement {
    None, Single, SinglePortrait, Grid, Row;

    companion object {
        const val GRID_MOST = 4

        fun of(media: List<AgentFeedMediaView>): AgentFeedMediaArrangement = when {
            media.isEmpty() -> None
            media.size == 1 -> if (media.first().isPortrait) SinglePortrait else Single
            media.size <= GRID_MOST -> Grid
            else -> Row
        }
    }
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
    /** The card's pictures, at most six. */
    val feedMedia: List<AgentFeedMediaView> = emptyList(),
) {
    /** The pictures of the whole card, not the thumbnails of a list's rows; one with no image to draw is left out. */
    val cardMedia: List<AgentFeedMediaView> get() = feedMedia.filter { media -> media.listRowIndex == null && media.imageUrl.isNotEmpty() }

    /** The thumbnail of each row of a list card, by the row's index; the first one given for a row wins. */
    val rowThumbnails: Map<Int, AgentFeedMediaView>
        get() {
            val thumbnails = LinkedHashMap<Int, AgentFeedMediaView>()
            for (media in feedMedia) {
                val rowIndex = media.listRowIndex ?: continue
                if (media.imageUrl.isNotEmpty() && rowIndex !in thumbnails) thumbnails[rowIndex] = media
            }
            return thumbnails
        }

    /** The small line above the headline, as a news app labels a story: the first picture's site, else the source's name; empty when there is neither. */
    val headerText: String get() = cardMedia.firstOrNull { media -> media.kind == AgentFeedMediaKind.Picture && media.site.isNotEmpty() }?.site?.takeIf { name -> name.isNotEmpty() } ?: feedSourceTitle

    /** How the card is drawn: its layout when the content reads, else the plain text of its body. */
    val layout: FeedLayout get() = FeedLayout.read(feedCardLayout, layoutContent)

    /** Whether this card is the top of a pile of cards from one source. */
    val isStack: Boolean get() = stackItemCount > 1 && feedStackKey.isNotEmpty()

    /** How many more cards the stack holds under this one. */
    val moreInStackCount: Int get() = maxOf(0, stackItemCount - 1)

    /** What the stack is called: its own name, else the goal's or schedule's; empty when it has neither. */
    val stackName: String get() = feedStackTitle.ifEmpty { feedSourceTitle }

    /** How many card edges show behind a stack's top card: one for a pair, two for more. */
    val stackEdgeCount: Int get() = if (!isStack) 0 else if (stackItemCount == 2) 1 else 2

    /** The choice that was made, when one was. */
    val chosenChoice: AgentFeedChoice? get() = chosenChoiceKey?.let { key -> choices.firstOrNull { choice -> choice.choiceKey == key } }

    /** Whether a choice was made, so the buttons give way to what came of it. */
    val isChosen: Boolean get() = !chosenChoiceKey.isNullOrEmpty()

    /** Whether the card still offers its choices. */
    val isAwaitingChoice: Boolean get() = choices.isNotEmpty() && !isChosen && !isDismissed

    /** Whether the card is a question the person has yet to answer: a card whose only choice is to discuss it asks nothing. */
    val isWaitingForAnswer: Boolean get() = !isChosen && !isDismissed && choices.any { choice -> choice.role != AgentFeedChoiceRole.Discuss }

    /** The choices in the order they are shown: the actions first, as the prominent button comes first. */
    val orderedChoices: List<AgentFeedChoice> get() = choices.filter { choice -> choice.role == AgentFeedChoiceRole.Action } + choices.filter { choice -> choice.role != AgentFeedChoiceRole.Action }

    /** When the card was last posted or replaced: a replaced card moves to the top, so this and not the first posting is the date shown. */
    val shownAtText: String get() = updatedAt.ifEmpty { postedAt }

    /** Whether the card is about a coding session, which "open" goes to. */
    val isCodingSession: Boolean get() = feedSourceKind == "coding_session"

    /** The computer and the pane of a coding session's card (`feedSourceId` is the two joined by a slash); null for any other card. */
    val codingSessionPlace: Pair<String, String>?
        get() {
            if (!isCodingSession) return null
            val computer = feedSourceId.substringBeforeLast('/', "")
            val paneId = feedSourceId.substringAfterLast('/', "")
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
        fun first(page: AgentFeedPage): AgentFeedList = AgentFeedList(page.feedItems, page.nextCursor, page.unreadCount)
    }

    /** The list with the next page below it; a card already shown is left as it is, and the cursor and the unread count are the page's. */
    fun appending(page: AgentFeedPage): AgentFeedList {
        val shownIds = feedItems.map { item -> item.feedItemId }.toSet()
        return AgentFeedList(feedItems + page.feedItems.filter { item -> item.feedItemId !in shownIds }, page.nextCursor, page.unreadCount)
    }

    /**
     * The list with this card in place of its old copy, wherever it is (a card answered stays on screen, settled on the
     * answer, even when the answer closed it). A card read on its own says it is alone, so the stack it tops keeps its counts.
     */
    fun replacing(item: AgentFeedItemView): AgentFeedList {
        val position = feedItems.indexOfFirst { shown -> shown.feedItemId == item.feedItemId }
        if (position < 0) return this
        val kept = feedItems[position]
        val replacement = if (kept.isStack && kept.feedStackKey == item.feedStackKey) item.copy(stackItemCount = kept.stackItemCount, stackUnreadCount = kept.stackUnreadCount) else item
        return copy(feedItems = feedItems.toMutableList().also { list -> list[position] = replacement })
    }

    /** Whether any card waits for a turn of the agent to say how its answer went, which is when the feed is read again every few seconds. */
    val hasPendingOutcome: Boolean get() = feedItems.any { item -> item.isOutcomePending }

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

    /** The cards shown that nobody has read yet, which the screen asks the server to mark as they are seen. */
    fun isUnread(feedItemId: String): Boolean = feedItems.firstOrNull { item -> item.feedItemId == feedItemId }?.isRead == false
}

@Serializable
private class ListAgentFeedAnswer(val ListAgentFeed: AgentFeedPage)

@Serializable
private class ListAgentFeedStackAnswer(val ListAgentFeedStack: AgentFeedPage)


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
const val AGENT_FEED_PAGE_SIZE = 30

/** A page of the feed, newest first; [after] is the previous page's `nextCursor`. */
suspend fun Connection.agentFeed(after: String? = null, first: Int = AGENT_FEED_PAGE_SIZE, includeDismissed: Boolean = false): AgentFeedPage {
    val variables = hashMapOf<String, JsonElement>("first" to JsonPrimitive(first))
    if (!after.isNullOrEmpty()) variables["after"] = JsonPrimitive(after)
    if (includeDismissed) variables["includeDismissed"] = JsonPrimitive(true)
    return execute<ListAgentFeedAnswer>(AgentFeedDocuments.LIST_AGENT_FEED, variables).ListAgentFeed
}

/** One stack's cards, newest first (or by their positions), a page at a time; [after] is the previous page's `nextCursor`. */
suspend fun Connection.agentFeedStack(feedStackKey: String, after: String? = null, first: Int = AGENT_FEED_PAGE_SIZE): AgentFeedPage {
    val variables = hashMapOf<String, JsonElement>("feedStackKey" to JsonPrimitive(feedStackKey), "first" to JsonPrimitive(first))
    if (!after.isNullOrEmpty()) variables["after"] = JsonPrimitive(after)
    return execute<ListAgentFeedStackAnswer>(AgentFeedDocuments.LIST_AGENT_FEED_STACK, variables).ListAgentFeedStack
}

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
