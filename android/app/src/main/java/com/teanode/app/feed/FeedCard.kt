package com.teanode.app.feed

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.animation.core.FiniteAnimationSpec
import androidx.compose.animation.core.animateDpAsState
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.snap
import androidx.compose.animation.core.spring
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Favorite
import androidx.compose.material.icons.outlined.FavoriteBorder
import androidx.compose.material.icons.outlined.Forum
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.outlined.KeyboardArrowDown
import androidx.compose.material.icons.outlined.KeyboardArrowUp
import androidx.compose.material.icons.outlined.Restore
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.SwipeToDismissBox
import androidx.compose.material3.SwipeToDismissBoxValue
import androidx.compose.material3.rememberSwipeToDismissBoxState
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.semantics.CustomAccessibilityAction
import androidx.compose.ui.semantics.customActions
import androidx.compose.ui.semantics.semantics
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.ButtonDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.platform.UriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.layout.Layout
import androidx.compose.ui.layout.layout
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import com.teanode.app.R
import com.teanode.app.chats.LocalLinkOpener
import com.teanode.app.chats.MarkdownView
import com.teanode.app.ui.components.relativeTimeText
import com.teanode.kit.feed.AgentFeedChoice
import com.teanode.kit.feed.AgentFeedChoiceRole
import com.teanode.kit.feed.AgentFeedFactRow
import com.teanode.kit.feed.AgentFeedItemView
import java.time.Instant

/** The font scale from which a card lays its words under the picture instead of beside it (ios: an accessibility text size). */
private const val LARGE_TEXT_SCALE = 1.5f

/** The radius of a card's corners; a deck's tucked cards are cut off this far below the top card's top edge. */
internal val FEED_CARD_CORNER = 20.dp
private val CARD_SHAPE = RoundedCornerShape(FEED_CARD_CORNER)

/** Whether the app is drawn dark, read from the theme rather than the system, since the person can choose either. */
@Composable
fun isFeedDark(): Boolean = MaterialTheme.colorScheme.background.luminance() < 0.5f

/** The page the cards sit on (ios systemGroupedBackground): light grey in light mode, near black in dark mode. */
@Composable
fun feedPageColor(): Color = if (isFeedDark()) MaterialTheme.colorScheme.background else MaterialTheme.colorScheme.surfaceContainer

/** A card on that page (ios secondarySystemGroupedBackground): white in light mode, dark grey in dark mode. */
@Composable
fun feedCardColor(): Color = if (isFeedDark()) MaterialTheme.colorScheme.surfaceContainerHigh else MaterialTheme.colorScheme.surfaceContainerLowest

/** The surface of a resolved card: quieter than a live one, between the card's and the page's. */
@Composable
private fun feedMutedCardColor(): Color = if (isFeedDark()) MaterialTheme.colorScheme.surfaceContainer else MaterialTheme.colorScheme.surfaceContainerLow

/** How much of its colors a resolved card's words and pictures keep. */
private const val RESOLVED_CONTENT_ALPHA = 0.78f

/** The hairline at a card's edge. */
@Composable
private fun feedCardEdgeColor(): Color = MaterialTheme.colorScheme.outlineVariant.copy(alpha = 0.6f)

/** When a time written by the server happened, in milliseconds; null for text that is not a time. */
fun feedEpochMS(text: String): Long? = runCatching { Instant.parse(text).toEpochMilli() }.getOrNull()

/**
 * One card of the feed (ios FeedViews.swift, FeedCard): its pictures, a header line of the card's badge, where it is from
 * and when, the headline, the words (or the structured layout), a short table of facts, the choices the agent offers or
 * what was chosen and came of it, the row that opens a stack, and a footer of heart, Discuss and information. The words
 * use the card's whole width. Cards are white on the grey page with a soft shadow in light mode; in dark mode the
 * lighter surface lifts them.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun FeedCard(
    item: AgentFeedItemView,
    actions: FeedActions,
    onShowInfo: () -> Unit,
    modifier: Modifier = Modifier,
    /** Fans out the deck a top card stands for; null inside an open stack's own screen, which shows its cards whole. */
    onOpenStack: ((AgentFeedItemView) -> Unit)? = null,
    /** Whether the deck this card tops is fanned out: its row then says "Show Less", which gathers the deck back. */
    isDeckOpen: Boolean = false,
    /** Whether a swipe takes the whole deck this card tops, as Dismiss All does: a closed deck's top card. */
    isSwipeWholeDeck: Boolean = false,
) {
    val isStackShown = onOpenStack != null && item.isStack
    val cardColor = feedCardColor()
    val edgeBorder = feedCardEdgeColor()
    // A resolved card is muted and sits flat on the page: no shadow, a quieter surface, and its words a little lighter.
    val shadowColor = if (isFeedDark() || item.isResolved) Color.Transparent else Color.Black.copy(alpha = 0.10f)
    val isLargeText = LocalDensity.current.fontScale >= LARGE_TEXT_SCALE
    val uriHandler = remember(actions) { object : UriHandler { override fun openUri(uri: String) = actions.followLink(uri) } }
    CompositionLocalProvider(LocalUriHandler provides uriHandler, LocalLinkOpener provides { address -> actions.followLink(address) }) {
        var isMenuOpen by remember { mutableStateOf(false) }
        // On the dismissed screen the swipe, the menu and the accessibility action put the card back.
        val isRestoring = actions.model.isDismissedOnly
        val isWholeDeck = isSwipeWholeDeck && isStackShown
        val dismissText = stringResource(
            when {
                isRestoring && isWholeDeck -> R.string.feed_restore_all
                isRestoring -> R.string.feed_restore
                isWholeDeck -> R.string.feed_swipe_dismiss_all
                else -> R.string.feed_info_dismiss
            },
        )
        val swipeAway = { if (isWholeDeck) actions.model.swipeAwayDeck(item) else actions.model.swipeAway(item) }
        // A card swipes away either way, as a mail does; the page under it shows what the swipe does. It goes only
        // past two fifths of its width, so a short flick while scrolling leaves it, and it is dismissed once
        // however often the swipe settles.
        var isSwipedAway by remember(item.feedItemId) { mutableStateOf(false) }
        val swipeState = rememberSwipeToDismissBoxState(
            confirmValueChange = { target ->
                val isSwiped = target != SwipeToDismissBoxValue.Settled
                if (isSwiped && !isSwipedAway) {
                    isSwipedAway = true
                    swipeAway()
                }
                isSwiped
            },
            positionalThreshold = { totalDistance -> totalDistance * 0.4f },
        )
        SwipeToDismissBox(
            swipeState,
            backgroundContent = {
                if (swipeState.dismissDirection != SwipeToDismissBoxValue.Settled) {
                    // The action's color under the card, in its shape: red to dismiss, the accent green to restore.
                    val swipeColor = if (isRestoring) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.error
                    val swipeContentColor = if (isRestoring) MaterialTheme.colorScheme.onPrimary else MaterialTheme.colorScheme.onError
                    Box(
                        Modifier.fillMaxSize().background(swipeColor, CARD_SHAPE).padding(horizontal = 24.dp),
                        contentAlignment = if (swipeState.dismissDirection == SwipeToDismissBoxValue.StartToEnd) Alignment.CenterStart else Alignment.CenterEnd,
                    ) {
                        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            Icon(if (isRestoring) Icons.Outlined.Restore else Icons.Outlined.Close, contentDescription = null, tint = swipeContentColor)
                            Text(dismissText, color = swipeContentColor, style = MaterialTheme.typography.labelLarge)
                        }
                    }
                }
            },
            modifier = modifier.fillMaxWidth(),
        ) {
            Box(Modifier.fillMaxWidth()) {
                Surface(
                    // A soft shadow lifts the card off the page in light mode; a long press opens the card's menu.
                    Modifier.fillMaxWidth().shadow(8.dp, CARD_SHAPE, ambientColor = shadowColor, spotColor = shadowColor)
                        .pointerInput(item.feedItemId) { detectTapGestures(onLongPress = { isMenuOpen = true }) }
                        .semantics { customActions = listOf(CustomAccessibilityAction(dismissText) { swipeAway(); true }) },
                    shape = CARD_SHAPE,
                    color = if (item.isResolved) feedMutedCardColor() else cardColor,
                    border = BorderStroke(0.5.dp, edgeBorder),
                ) {
                    // An answered card starts folded to its title and one line when it comes on screen already answered.
                    var isFolded by remember(item.feedItemId) { mutableStateOf(item.isAnswerFoldable) }
                    Column(if (item.isResolved) Modifier.alpha(RESOLVED_CONTENT_ALPHA) else Modifier) {
                        if (isFolded && item.isAnswerFoldable) {
                            Column(Modifier.padding(start = 16.dp, end = 16.dp, top = 14.dp, bottom = 6.dp)) { FeedFoldedAnswer(item) { isFolded = false } }
                        } else {
                            // The pictures sit at the card's top edge, edge to edge; the card's own shape rounds their corners.
                            if (item.plainCardMedia.isNotEmpty()) FeedMediaBlock(item.plainCardMedia, actions)
                            for (still in item.cameraStills) FeedCameraStill(still, actions)
                            // The footer's buttons are 48 tall, room enough under the last line.
                            Column(Modifier.padding(start = 16.dp, end = 16.dp, top = 14.dp, bottom = 2.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                                FeedCardContent(item, actions, isStackShown, isDeckOpen, isLargeText, onShowInfo) { onOpenStack?.invoke(item) }
                            }
                        }
                    }
                }
                DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                    DropdownMenuItem(
                        text = { Text(dismissText) },
                        leadingIcon = { Icon(if (isRestoring) Icons.Outlined.Restore else Icons.Outlined.Close, contentDescription = null) },
                        onClick = { isMenuOpen = false; actions.model.swipeAway(item) },
                    )
                    if (isStackShown && !isRestoring) {
                        DropdownMenuItem(
                            text = { Text(stringResource(R.string.feed_dismiss_all, item.stackItemCount)) },
                            leadingIcon = { Icon(Icons.Outlined.Close, contentDescription = null) },
                            onClick = { isMenuOpen = false; actions.model.dismissStack(item) },
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun FeedCardContent(item: AgentFeedItemView, actions: FeedActions, isStackShown: Boolean, isDeckOpen: Boolean, isLargeText: Boolean, onShowInfo: () -> Unit, onOpenStack: () -> Unit) {
    // A stack with a name of its own says it above its top card.
    if (isStackShown && item.feedStackTitle.isNotEmpty()) {
        Text(item.feedStackTitle, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
    FeedCardHeader(item, isLargeText)
    Text(item.title, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
    FeedLayoutBody(item, actions)
    FeedResolutionNote(item)
    FeedCharts(item.drawableCharts)
    FeedAnswerArea(item, actions)
    if (isStackShown) StackRow(item, isDeckOpen, onOpenStack)
    FeedFooter(item, actions, onShowInfo)
}

/** The card's words and facts, or its structured layout, which the body stands in for wherever the layout cannot be read. */
@Composable
private fun FeedLayoutBody(item: AgentFeedItemView, actions: FeedActions) {
    when (val layout = remember(item.feedCardLayout, item.layoutContent) { item.layout }) {
        com.teanode.kit.feed.FeedLayout.Text -> {
            if (item.body.isNotEmpty()) MarkdownView(item.body)
            if (item.factRows.isNotEmpty()) FeedFacts(item.factRows)
        }
        is com.teanode.kit.feed.FeedLayout.Rows -> FeedListRows(layout.rows, actions, thumbnails = item.rowThumbnails)
        is com.teanode.kit.feed.FeedLayout.Weather -> FeedWeather(layout.weather)
        is com.teanode.kit.feed.FeedLayout.Agenda -> FeedAgenda(layout.entries, actions)
    }
}

/** A short table: each label in secondary text at the left and its value in bold at the right, the lines told apart by a rule. */
@Composable
fun FeedFacts(factRows: List<AgentFeedFactRow>) {
    Column {
        for ((index, fact) in factRows.withIndex()) {
            if (index > 0) HorizontalDivider()
            Row(Modifier.fillMaxWidth().padding(vertical = 7.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(fact.factLabel, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.weight(1f))
                Text(fact.factValue, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Bold, textAlign = androidx.compose.ui.text.style.TextAlign.End, modifier = Modifier.weight(1f))
            }
        }
    }
}

/** The choices to press, or, once one was pressed, which it was and what the agent made of it. */
@Composable
private fun FeedAnswerArea(item: AgentFeedItemView, actions: FeedActions) {
    val chosen = item.chosenChoice
    when {
        chosen != null -> Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Icon(Icons.Outlined.CheckCircle, contentDescription = null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(18.dp))
                Text(chosen.choiceLabel, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary)
            }
            FeedOutcome(item, actions)
        }
        // The choice is no longer listed, but the answer is still given.
        item.isChosen -> FeedOutcome(item, actions)
        item.choices.isNotEmpty() -> FeedChoiceRow(Modifier.padding(top = 2.dp)) {
            for (choice in item.orderedChoices) FeedChoiceButton(choice, isDisabled = actions.isBusy(item)) { actions.choose(item, choice) }
        }
    }
}

@Composable
private fun FeedOutcome(item: AgentFeedItemView, actions: FeedActions) {
    if (item.isOutcomePending) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
            Text(stringResource(R.string.feed_working), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.weight(1f))
            // The agent works in the main conversation: a small way there while it does.
            if (item.canOpenChat) TextButton(onClick = { actions.openMainChat() }) { Text(stringResource(R.string.feed_open_chat)) }
        }
    } else if (item.outcomeText.isNotEmpty()) {
        Text(item.outcomeText, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** The gap between two choices, side by side or one under the other. */
private val CHOICE_GAP = 8.dp

/**
 * The choices side by side and of equal width when every label fits on one line at that width, else one under the other
 * as wide as the card (ios ViewThatFits).
 */
@Composable
private fun FeedChoiceRow(modifier: Modifier = Modifier, content: @Composable () -> Unit) {
    Layout(content, modifier.fillMaxWidth()) { measurables, constraints ->
        val gap = CHOICE_GAP.roundToPx()
        val width = constraints.maxWidth
        val count = measurables.size.coerceAtLeast(1)
        val sharedWidth = ((width - gap * (count - 1)) / count).coerceAtLeast(0)
        val isSideBySide = measurables.all { measurable -> measurable.maxIntrinsicWidth(Constraints.Infinity) <= sharedWidth }
        if (isSideBySide) {
            val placeables = measurables.map { measurable -> measurable.measure(Constraints.fixedWidth(sharedWidth)) }
            val height = placeables.maxOf { placeable -> placeable.height }
            layout(width, height) {
                for ((index, placeable) in placeables.withIndex()) placeable.place(index * (sharedWidth + gap), (height - placeable.height) / 2)
            }
        } else {
            val placeables = measurables.map { measurable -> measurable.measure(Constraints.fixedWidth(width)) }
            val height = placeables.sumOf { placeable -> placeable.height } + gap * (placeables.size - 1).coerceAtLeast(0)
            layout(width, height) {
                var top = 0
                for (placeable in placeables) {
                    placeable.place(0, top)
                    top += placeable.height + gap
                }
            }
        }
    }
}

/** A choice as a capsule button: the action filled, the others bordered. */
@Composable
private fun FeedChoiceButton(choice: AgentFeedChoice, isDisabled: Boolean, onPress: () -> Unit) {
    val modifier = Modifier.heightIn(min = 48.dp)
    if (choice.role == AgentFeedChoiceRole.Action) {
        Button(onClick = onPress, enabled = !isDisabled, modifier = modifier) { Text(choice.choiceLabel, textAlign = TextAlign.Center) }
    } else {
        OutlinedButton(onClick = onPress, enabled = !isDisabled, modifier = modifier) { Text(choice.choiceLabel, textAlign = TextAlign.Center) }
    }
}

/**
 * "+3 more from News about humanoid robots": the row under a stack's top card that fans the rest out. While they are
 * out it says "Show Less" in the same place and gathers them back, so the top card does not move.
 */
@Composable
private fun StackRow(item: AgentFeedItemView, isDeckOpen: Boolean, onClick: () -> Unit) {
    val words = when {
        isDeckOpen -> stringResource(R.string.feed_stack_show_less)
        item.stackName.isEmpty() -> stringResource(R.string.feed_more_in_stack_unnamed, item.moreInStackCount)
        else -> stringResource(R.string.feed_more_in_stack, item.moreInStackCount, item.stackName)
    }
    val hint = if (isDeckOpen) stringResource(R.string.feed_stack_gather_hint) else null
    Row(
        Modifier.fillMaxWidth().heightIn(min = 44.dp).clickable(role = Role.Button, onClickLabel = hint, onClick = onClick),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(words, style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary, modifier = Modifier.weight(1f))
        Icon(if (isDeckOpen) Icons.Outlined.KeyboardArrowUp else Icons.Outlined.KeyboardArrowDown, contentDescription = null, tint = MaterialTheme.colorScheme.outline, modifier = Modifier.size(20.dp))
    }
}

/**
 * Heart and Discuss at the left and information at the right. Each button reaches past the words' edges by the room
 * around its glyph, so the glyphs line up with the words above.
 */
@Composable
private fun FeedFooter(item: AgentFeedItemView, actions: FeedActions, onShowInfo: () -> Unit) {
    val isLiked = actions.model.isLiked(item)
    Row(Modifier.fillMaxWidth().outset(FOOTER_GLYPH_INSET).heightIn(min = 48.dp), verticalAlignment = Alignment.CenterVertically) {
        IconButton(onClick = { actions.model.toggleLike(item) }) {
            Icon(
                if (isLiked) Icons.Outlined.Favorite else Icons.Outlined.FavoriteBorder,
                contentDescription = stringResource(if (isLiked) R.string.feed_unlike else R.string.feed_like),
                tint = if (isLiked) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        TextButton(onClick = { actions.discuss(item) }, colors = ButtonDefaults.textButtonColors(contentColor = MaterialTheme.colorScheme.onSurfaceVariant)) {
            Icon(Icons.Outlined.Forum, contentDescription = null, modifier = Modifier.size(18.dp))
            Spacer(Modifier.size(6.dp))
            Text(stringResource(R.string.feed_discuss))
        }
        Spacer(Modifier.weight(1f))
        IconButton(onClick = onShowInfo) { Icon(Icons.Outlined.Info, contentDescription = stringResource(R.string.feed_info), tint = MaterialTheme.colorScheme.onSurfaceVariant) }
    }
}

/** The room between an icon button's edge and its glyph: a 48 wide button around a 24 wide icon. */
private val FOOTER_GLYPH_INSET = 12.dp

/** Lays the content out [inset] wider than its place on each side, so its edges reach past its neighbours'. */
private fun Modifier.outset(inset: Dp): Modifier = layout { measurable, constraints ->
    val extra = inset.roundToPx()
    val wider = if (constraints.hasBoundedWidth) constraints.copy(minWidth = constraints.minWidth + extra * 2, maxWidth = constraints.maxWidth + extra * 2) else constraints
    val placeable = measurable.measure(wider)
    layout(placeable.width - extra * 2, placeable.height) { placeable.place(-extra, 0) }
}

/**
 * The line above a card's headline, as a news app labels a story: the card's badge, where it is from (the first
 * picture's site, the source's name, else what kind of card it is) and when it was posted. At the largest text sizes the
 * time goes under it.
 */
@Composable
private fun FeedCardHeader(item: AgentFeedItemView, isLargeText: Boolean) {
    val dateText = feedEpochMS(item.shownAtText)?.let { epochMS -> relativeTimeText(epochMS) }.orEmpty()
    val source = item.headerText.ifEmpty { stringResource(feedKindWord(item.feedItemKind)) }
    val sourceLine: @Composable () -> Unit = {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            FeedKindBadge(item)
            Text(source, style = MaterialTheme.typography.labelLarge, fontWeight = FontWeight.SemiBold, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f, fill = false))
        }
    }
    val date: @Composable () -> Unit = {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            if (item.isResolved) FeedResolvedChip(item)
            if (dateText.isNotEmpty()) Text(dateText, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.outline, maxLines = 1)
        }
    }
    if (isLargeText) {
        Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
            sourceLine()
            date()
        }
    } else {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Box(Modifier.weight(1f)) { sourceLine() }
            date()
        }
    }
}
