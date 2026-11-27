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
import java.time.Instant
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter

/** The agent feed's GraphQL: the same text as the iPhone app's (ios/TeaNodeKit AgentFeed.swift), which the server's tests check against the schema. */
object AgentFeedDocuments {
    private const val FEED_ITEM_FIELDS = """feedItemId feedItemKind feedSourceKind feedSourceId feedSourceTitle runConversationId title body pictureSymbol
factRows { factLabel factValue } sourceLinks { sourceTitle sourceUrl } choices { choiceKey choiceLabel choiceRole }
chosenChoiceKey chosenAt outcomeText isOutcomePending feedUrgency isLiked isRead isDismissed dismissedAt postedAt updatedAt
feedStackKey stackItemCount stackUnreadCount feedStackTitle stackPosition feedCardLayout layoutContent
isResolved resolvedAt resolutionNote resolvedByKind
chartBlocks { chartKind chartTitle unit provenance xLabels series { seriesName seriesRole values } markers { markerAt markerLabel } bars { barLabel barValue } }
feedMedia { mediaKey feedMediaKind imageUrl videoUrl linkUrl siteName mediaCaption widthPixels heightPixels listRowIndex cameraName capturedAt stillReason playbackUrl liveUrl }"""

    const val LIST_AGENT_FEED_STACK = """query (${'$'}feedStackKey: String!, ${'$'}first: Int, ${'$'}after: String) {
  ListAgentFeedStack(feedStackKey: ${'$'}feedStackKey, first: ${'$'}first, after: ${'$'}after) {
    feedItems { $FEED_ITEM_FIELDS }
    nextCursor
    unreadCount
  }
}"""
    const val LIST_AGENT_FEED = """query (${'$'}first: Int, ${'$'}after: String, ${'$'}includeDismissed: Boolean, ${'$'}isDismissedOnly: Boolean) {
  ListAgentFeed(first: ${'$'}first, after: ${'$'}after, includeDismissed: ${'$'}includeDismissed, isDismissedOnly: ${'$'}isDismissedOnly) {
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
    const val DISMISS_AGENT_FEED_ITEMS = """mutation (${'$'}feedItemIds: [String!], ${'$'}feedStackKey: String, ${'$'}isEveryReadCard: Boolean) {
  DismissAgentFeedItems(feedItemIds: ${'$'}feedItemIds, feedStackKey: ${'$'}feedStackKey, isEveryReadCard: ${'$'}isEveryReadCard)
}"""
    const val RESTORE_AGENT_FEED_ITEMS = """mutation (${'$'}feedItemIds: [String!]!) {
  RestoreAgentFeedItems(feedItemIds: ${'$'}feedItemIds)
}"""
    const val MARK_AGENT_FEED_READ = """mutation (${'$'}feedItemIds: [String!]!) {
  MarkAgentFeedRead(feedItemIds: ${'$'}feedItemIds)
}"""
    const val RESOLVE_AGENT_FEED_ITEM = """mutation (${'$'}feedItemId: String!, ${'$'}resolutionNote: String) {
  ResolveAgentFeedItem(feedItemId: ${'$'}feedItemId, resolutionNote: ${'$'}resolutionNote) { $FEED_ITEM_FIELDS }
}"""
    const val UNRESOLVE_AGENT_FEED_ITEM = """mutation (${'$'}feedItemId: String!) {
  UnresolveAgentFeedItem(feedItemId: ${'$'}feedItemId) { $FEED_ITEM_FIELDS }
}"""
    const val DISCUSS_AGENT_FEED_ITEM = """mutation (${'$'}feedItemId: String!) {
  DiscussAgentFeedItem(feedItemId: ${'$'}feedItemId)
}"""

    /** The name of the subscription's field, also the key its events arrive under. */
    const val FEED_CHANGED_FIELD = "AgentFeedChanged"
    const val AGENT_FEED_CHANGED = """subscription { AgentFeedChanged { feedItemId feedChange unreadCount } }"""
}

/** The kinds of picture a card carries: one of the person's photos, one of their videos, the picture of a web page, or a still from one of their cameras. */
enum class AgentFeedMediaKind { Photo, Video, Picture, CameraStill;

    companion object {
        /** A kind a newer server sends is drawn as a picture: its image is all the app can be sure to have. */
        fun of(name: String): AgentFeedMediaKind = when (name) {
            "photo" -> Photo
            "video" -> Video
            "camera_still" -> CameraStill
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
    /** A camera still's camera, such as "Front door"; missing for the others. */
    val cameraName: String? = null,
    /** When the still was taken (RFC 3339), on the camera's own clock with its offset; missing for the others. */
    val capturedAt: String? = null,
    /** Why this camera was chosen, one line; missing for the others. */
    val stillReason: String? = null,
    /** Where the recording of that moment plays (https or the app's own scheme); missing when there is none. */
    val playbackUrl: String? = null,
    /** Where the camera's live view opens (https or the app's own scheme); missing when there is none. */
    val liveUrl: String? = null,
) {
    val kind: AgentFeedMediaKind get() = AgentFeedMediaKind.of(feedMediaKind)

    val isCameraStill: Boolean get() = kind == AgentFeedMediaKind.CameraStill

    val cameraLabel: String get() = cameraName.orEmpty()

    val reasonText: String get() = stillReason.orEmpty()

    /** The address of the recording, when it is one the app may follow. */
    val playbackAddress: String? get() = playbackUrl?.takeIf { address -> isFollowable(address) }

    /** The address of the live view, when it is one the app may follow. */
    val liveAddress: String? get() = liveUrl?.takeIf { address -> isFollowable(address) }

    /**
     * The still's time as the camera's own clock says it: `14:02:11`, and when that clock is not [deviceZone]'s at that moment,
     * the offset after it (`14:02:11 UTC+09:00`); empty when there is no readable time.
     */
    fun capturedAtText(deviceZone: ZoneId = ZoneId.systemDefault()): String = AgentFeedClock.cameraTime(capturedAt, deviceZone)

    private fun isFollowable(address: String): Boolean = address.lowercase().let { lowered -> lowered.startsWith("https://") || lowered.startsWith("teanode://") }

    val caption: String get() = mediaCaption.orEmpty()

    val siteLabel: String get() = siteName.orEmpty()

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

/** How a card's clock times are written for the person: the device's zone is never named, any other zone is. */
object AgentFeedClock {
    private val hoursAndMinutes = DateTimeFormatter.ofPattern("HH:mm")
    private val hoursMinutesSeconds = DateTimeFormatter.ofPattern("HH:mm:ss")

    /** The zone's name as a short offset, such as `UTC+09:00`, or `UTC` for none. */
    fun zoneName(offsetSeconds: Int): String {
        if (offsetSeconds == 0) return "UTC"
        val total = Math.abs(offsetSeconds) / 60
        return "UTC" + (if (offsetSeconds < 0) "-" else "+") + "%02d:%02d".format(total / 60, total % 60)
    }

    /** A camera's `HH:mm:ss` on its own clock, with its zone's name after it when that is not [deviceZone] at that moment. */
    fun cameraTime(text: String?, deviceZone: ZoneId): String {
        val moment = runCatching { OffsetDateTime.parse(text.orEmpty()) }.getOrNull() ?: return ""
        val clock = moment.format(hoursMinutesSeconds)
        val deviceOffsetSeconds = deviceZone.rules.getOffset(moment.toInstant()).totalSeconds
        return if (deviceOffsetSeconds == moment.offset.totalSeconds) clock else clock + " " + zoneName(moment.offset.totalSeconds)
    }

    /** An instant's `HH:mm` in [deviceZone], the person's own; empty when the text is not a time. */
    fun resolvedTime(text: String?, deviceZone: ZoneId): String {
        val instant = runCatching { Instant.parse(text.orEmpty()) }.getOrNull() ?: runCatching { OffsetDateTime.parse(text.orEmpty()).toInstant() }.getOrNull() ?: return ""
        return instant.atZone(deviceZone).format(hoursAndMinutes)
    }
}

/** What a line of a chart stands for: what was measured, a line to compare with (last month, the average), or what is expected ahead. */
enum class AgentFeedSeriesRole(val roleName: String) {
    Measured("measured"),
    Reference("reference"),
    Projection("projection"),
    ;

    companion object {
        /** A role this app does not know is drawn as measured. */
        fun of(roleName: String): AgentFeedSeriesRole = entries.firstOrNull { role -> role.roleName == roleName } ?: Measured
    }
}

/** AgentFeedChartSeries is one line of a series chart: a number for each label of the axis, or null where there is none. */
@Serializable
data class AgentFeedChartSeries(
    val seriesName: String = "",
    val seriesRole: String = "measured",
    val values: List<Double?> = emptyList(),
) {
    val role: AgentFeedSeriesRole get() = AgentFeedSeriesRole.of(seriesRole)
}

/** AgentFeedChartMarker is a labeled vertical rule at one of the axis labels. */
@Serializable
data class AgentFeedChartMarker(val markerAt: String = "", val markerLabel: String = "")

/** AgentFeedChartBar is one bar of a bars chart. */
@Serializable
data class AgentFeedChartBar(val barLabel: String = "", val barValue: Double = 0.0)

/**
 * AgentFeedChartBlock is a chart on a card: a series over time, or bars. [provenance] says where and when the numbers were read.
 * A [chartKind] this app does not know is drawn as nothing.
 */
@Serializable
data class AgentFeedChartBlock(
    /** series or bars. */
    val chartKind: String = "series",
    val chartTitle: String = "",
    /** What the numbers count, such as "USD" or "steps". */
    val unit: String = "",
    val provenance: String = "",
    val xLabels: List<String> = emptyList(),
    val series: List<AgentFeedChartSeries> = emptyList(),
    val markers: List<AgentFeedChartMarker> = emptyList(),
    val bars: List<AgentFeedChartBar> = emptyList(),
) {
    val isSeries: Boolean get() = chartKind == "series" && xLabels.size >= 2 && series.isNotEmpty()

    val isBars: Boolean get() = chartKind == "bars" && bars.isNotEmpty()

    /** Whether the chart has anything to draw. */
    val isDrawable: Boolean get() = isSeries || isBars

    /** The bars largest first, whatever order they came in. */
    val orderedBars: List<AgentFeedChartBar> get() = bars.sortedByDescending { bar -> bar.barValue }

    /** The markers that sit on the axis, with the position of each label; one that is not on the axis is left out. */
    val placedMarkers: List<Pair<Int, AgentFeedChartMarker>> get() = markers.mapNotNull { marker -> xLabels.indexOf(marker.markerAt).takeIf { position -> position >= 0 }?.let { position -> position to marker } }

    /** Whether a series chart has a reference line or a projection, which the legend names. */
    val hasLegend: Boolean get() = series.size > 1

    /** A sentence for a screen reader: the title, then each line's first and last numbers, or each bar. */
    fun description(): String {
        val unitText = if (unit.isEmpty()) "" else " $unit"
        val parts = ArrayList<String>()
        if (chartTitle.isNotEmpty()) parts.add(chartTitle)
        if (isBars) {
            parts.add(orderedBars.joinToString(", ") { bar -> "${bar.barLabel} ${trimmed(bar.barValue)}$unitText" })
        } else if (isSeries) {
            for (line in series) {
                val known = line.values.withIndex().filter { entry -> entry.value != null && entry.index < xLabels.size }
                if (known.isEmpty()) continue
                val first = known.first()
                val last = known.last()
                parts.add("${line.seriesName.ifEmpty { line.seriesRole }}: ${xLabels[first.index]} ${trimmed(first.value!!)}$unitText to ${xLabels[last.index]} ${trimmed(last.value!!)}$unitText")
            }
            for ((position, marker) in placedMarkers) parts.add("${marker.markerLabel} at ${xLabels[position]}")
        }
        if (provenance.isNotEmpty()) parts.add(provenance)
        return parts.joinToString(". ")
    }

    companion object {
        /** A number as a person writes it: no trailing zeros, thousands apart. */
        fun trimmed(value: Double): String {
            val isWhole = value == Math.rint(value) && Math.abs(value) < 1e15
            return if (isWhole) "%,d".format(value.toLong()) else "%,.2f".format(value).trimEnd('0').trimEnd('.')
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
    /** When the card was dismissed (RFC 3339); null for a card that is not dismissed. */
    val dismissedAt: String? = null,
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
    /** Whether the problem the card told of has cleared; the card is then drawn muted, with a "Resolved" chip. */
    val isResolved: Boolean = false,
    /** When the card was resolved (RFC 3339); null for a card that is not resolved. */
    val resolvedAt: String? = null,
    /** One line saying how it cleared; empty when none was given. */
    val resolutionNote: String = "",
    /** goal, person or agent; empty for a card that is not resolved. */
    val resolvedByKind: String = "",
    /** The card's charts, at most two. */
    val chartBlocks: List<AgentFeedChartBlock> = emptyList(),
) {
    /** The charts the app can draw, in order. */
    val drawableCharts: List<AgentFeedChartBlock> get() = chartBlocks.filter { chart -> chart.isDrawable }

    /** The camera stills of the card, which have their own look: a caption, the camera's time and why. */
    val cameraStills: List<AgentFeedMediaView> get() = cardMedia.filter { media -> media.isCameraStill }

    /** The pictures that are not camera stills, which the media block draws. */
    val plainCardMedia: List<AgentFeedMediaView> get() = cardMedia.filter { media -> !media.isCameraStill }

    /** The time the card was resolved, `HH:mm` in [deviceZone]; empty when it is not resolved or the time cannot be read. */
    fun resolvedTimeText(deviceZone: ZoneId = ZoneId.systemDefault()): String = if (isResolved) AgentFeedClock.resolvedTime(resolvedAt, deviceZone) else ""

    /** Whether the card was answered and the agent's work on it is done, so it folds to its title and one line. */
    val isAnswerFoldable: Boolean get() = isChosen && !isOutcomePending

    /** The one muted line a folded answered card shows: the choice made, then what came of it. */
    val foldedAnswerText: String get() = listOf(chosenChoice?.choiceLabel.orEmpty(), outcomeText).filter { part -> part.isNotEmpty() }.joinToString(" · ")

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
    val headerText: String get() = cardMedia.firstOrNull { media -> media.kind == AgentFeedMediaKind.Picture && media.siteLabel.isNotEmpty() }?.siteLabel?.takeIf { name -> name.isNotEmpty() } ?: feedSourceTitle

    /** How the card is drawn: its layout when the content reads, else the plain text of its body. */
    val layout: FeedLayout get() = FeedLayout.read(feedCardLayout, layoutContent)

    /** Whether this card is the top of a pile of cards from one source. */
    val isStack: Boolean get() = stackItemCount > 1 && feedStackKey.isNotEmpty()

    /** How many more cards the stack holds under this one. */
    val moreInStackCount: Int get() = maxOf(0, stackItemCount - 1)

    /** What the stack is called: its own name, else the goal's or schedule's; empty when it has neither. */
    val stackName: String get() = feedStackTitle.ifEmpty { feedSourceTitle }

    /** How many feet of a stack's deck show below its top card: one for a pair, two for more. */
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

/** AgentFeedChanged is what the server says happened to a card. [feedChange] is posted, replaced, settled, dismissed, restored, resolved or unresolved. */
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

/** One entry of the dismissed cards as a screen draws it: a card alone, or the consecutive cards of one stack as a deck. */
sealed interface AgentFeedDismissedEntry {
    /** The card drawn on top: the only card of an entry alone, the newest of a deck. */
    val topCard: AgentFeedItemView

    data class Single(override val topCard: AgentFeedItemView) : AgentFeedDismissedEntry

    /** [feedItems] are the deck's cards in the order they were read, the top card first. */
    data class Deck(val feedStackKey: String, val feedItems: List<AgentFeedItemView>) : AgentFeedDismissedEntry {
        override val topCard: AgentFeedItemView get() = feedItems.first()
    }
}

/**
 * Groups dismissed cards into entries: consecutive cards with one non-empty `feedStackKey` become a deck, every other card
 * is an entry alone. The order is kept, so a deck sits where its newest card was.
 */
fun groupDismissedFeedItems(feedItems: List<AgentFeedItemView>): List<AgentFeedDismissedEntry> {
    val entries = ArrayList<AgentFeedDismissedEntry>()
    var index = 0
    while (index < feedItems.size) {
        val feedStackKey = feedItems[index].feedStackKey
        var end = index + 1
        if (feedStackKey.isNotEmpty()) while (end < feedItems.size && feedItems[end].feedStackKey == feedStackKey) end++
        entries.add(if (end - index > 1) AgentFeedDismissedEntry.Deck(feedStackKey, feedItems.subList(index, end).toList()) else AgentFeedDismissedEntry.Single(feedItems[index]))
        index = end
    }
    return entries
}

/**
 * The dismissed cards as the feed's list draws them: each deck is its top card standing for the stack, with the count of
 * the deck's cards and how many of them are unread, as the server gives a stack's top card in the feed.
 */
fun foldDismissedFeedItems(feedItems: List<AgentFeedItemView>): List<AgentFeedItemView> =
    groupDismissedFeedItems(feedItems).map { entry ->
        when (entry) {
            is AgentFeedDismissedEntry.Single -> entry.topCard
            is AgentFeedDismissedEntry.Deck -> entry.topCard.copy(stackItemCount = entry.feedItems.size, stackUnreadCount = entry.feedItems.count { card -> !card.isRead })
        }
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
private class DismissAgentFeedItemsAnswer(val DismissAgentFeedItems: List<String>)

@Serializable
private class RestoreAgentFeedItemsAnswer(val RestoreAgentFeedItems: List<String>)

@Serializable
private class MarkAgentFeedReadAnswer(val MarkAgentFeedRead: Int)

@Serializable
private class ResolveAgentFeedItemAnswer(val ResolveAgentFeedItem: AgentFeedItemView)

@Serializable
private class UnresolveAgentFeedItemAnswer(val UnresolveAgentFeedItem: AgentFeedItemView)

@Serializable
private class DiscussAgentFeedItemAnswer(val DiscussAgentFeedItem: String)

/** How many cards a page of the feed holds. */
const val AGENT_FEED_PAGE_SIZE = 30

/**
 * A page of the feed, newest first; [after] is the previous page's `nextCursor`. With [isDismissedOnly] the page lists
 * the dismissed cards that have not expired instead, the most recently dismissed first, each one an entry of its own.
 */
suspend fun Connection.agentFeed(after: String? = null, first: Int = AGENT_FEED_PAGE_SIZE, includeDismissed: Boolean = false, isDismissedOnly: Boolean = false): AgentFeedPage {
    val variables = hashMapOf<String, JsonElement>("first" to JsonPrimitive(first))
    if (!after.isNullOrEmpty()) variables["after"] = JsonPrimitive(after)
    if (includeDismissed) variables["includeDismissed"] = JsonPrimitive(true)
    if (isDismissedOnly) variables["isDismissedOnly"] = JsonPrimitive(true)
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

/**
 * Dismisses cards and answers the ids actually dismissed (cards already dismissed are left out). Exactly one of the
 * three is given: [feedItemIds] (at most 500), every card of the stack [feedStackKey], or with [isEveryReadCard] every
 * card that is read and has no open choice.
 */
suspend fun Connection.dismissAgentFeedItems(feedItemIds: List<String>? = null, feedStackKey: String? = null, isEveryReadCard: Boolean = false): List<String> {
    require(listOf(feedItemIds != null, !feedStackKey.isNullOrEmpty(), isEveryReadCard).count { isGiven -> isGiven } == 1) { "give exactly one of feedItemIds, feedStackKey and isEveryReadCard" }
    val variables = hashMapOf<String, JsonElement>()
    if (feedItemIds != null) variables["feedItemIds"] = JsonArray(feedItemIds.map { feedItemId -> JsonPrimitive(feedItemId) })
    if (!feedStackKey.isNullOrEmpty()) variables["feedStackKey"] = JsonPrimitive(feedStackKey)
    if (isEveryReadCard) variables["isEveryReadCard"] = JsonPrimitive(true)
    return execute<DismissAgentFeedItemsAnswer>(AgentFeedDocuments.DISMISS_AGENT_FEED_ITEMS, variables).DismissAgentFeedItems
}

/** Brings back cards that were dismissed (at most 500), except those a choice settled; answers the ids restored. */
suspend fun Connection.restoreAgentFeedItems(feedItemIds: List<String>): List<String> =
    execute<RestoreAgentFeedItemsAnswer>(
        AgentFeedDocuments.RESTORE_AGENT_FEED_ITEMS,
        mapOf("feedItemIds" to JsonArray(feedItemIds.map { feedItemId -> JsonPrimitive(feedItemId) })),
    ).RestoreAgentFeedItems

/** Marks cards read; answers how many were newly read. */
suspend fun Connection.markAgentFeedRead(feedItemIds: List<String>): Int =
    execute<MarkAgentFeedReadAnswer>(
        AgentFeedDocuments.MARK_AGENT_FEED_READ,
        mapOf("feedItemIds" to JsonArray(feedItemIds.map { feedItemId -> JsonPrimitive(feedItemId) })),
    ).MarkAgentFeedRead

/** Marks a card resolved, with an optional one line [resolutionNote]. A card already resolved is refused by the server, so the first note stands. */
suspend fun Connection.resolveAgentFeedItem(feedItemId: String, resolutionNote: String? = null): AgentFeedItemView {
    val variables = hashMapOf<String, JsonElement>("feedItemId" to JsonPrimitive(feedItemId))
    if (!resolutionNote.isNullOrBlank()) variables["resolutionNote"] = JsonPrimitive(resolutionNote.trim())
    return execute<ResolveAgentFeedItemAnswer>(AgentFeedDocuments.RESOLVE_AGENT_FEED_ITEM, variables).ResolveAgentFeedItem
}

/** Takes the resolution off a card. */
suspend fun Connection.unresolveAgentFeedItem(feedItemId: String): AgentFeedItemView =
    execute<UnresolveAgentFeedItemAnswer>(AgentFeedDocuments.UNRESOLVE_AGENT_FEED_ITEM, mapOf("feedItemId" to JsonPrimitive(feedItemId))).UnresolveAgentFeedItem

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
