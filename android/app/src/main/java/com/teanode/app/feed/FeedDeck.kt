package com.teanode.app.feed

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.animation.core.tween
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.height
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.graphics.TransformOrigin
import androidx.compose.ui.graphics.drawscope.clipRect
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.Layout
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.zIndex
import com.teanode.kit.feed.AgentFeedItemView
import kotlin.math.roundToInt

/**
 * How long a deck takes to fan out and to gather. Every card and the column under the top card move on the same curve,
 * so the cards spread out of the deck evenly and never cross.
 */
private const val OPEN_MS = 440
private const val CLOSE_MS = 340

/** The gap between the cards of an open deck, the feed's own. */
private val CARD_GAP = 12.dp

/** How much of each foot of the deck shows below the one above it. */
private val FOOT_STEP = 6.dp

/** How much narrower than the top card each foot of the deck is at either side; the deck shows this many feet. */
private val FOOT_INSETS = listOf(22.dp, 34.dp)

/** How far below the column a tucked card's shadow may still be drawn. */
private val SHADOW_ROOM = 8.dp

/**
 * Neither curve overshoots: a column that grew past its cards and came back would push the card below the deck down and
 * up again.
 */
private val OPEN_EASING = CubicBezierEasing(0.45f, 0f, 0.2f, 1f)
private val CLOSE_EASING = CubicBezierEasing(0.5f, 0f, 0.3f, 1f)

/**
 * A stack drawn as a deck (ios FeedList): its top card, and under it the stack's own next cards, tucked behind it with
 * the feet of the first two showing below it. The row under the top card fans the deck out in place and then says "Show
 * Less", which gathers it back; the top card never moves. Opening and gathering move those same cards between tucked
 * and placed, on one curve with the column under the top card, which closed is only as tall as the feet. Each card is
 * laid out at its own full height whatever the column's. Each card is drawn over the cards under it, as in a real deck.
 * The stack is cut off below the top card's rounded top corners, so a tucked card taller than the top card never shows
 * above it or beside its corners, and at the column's bottom. Tucked cards cannot be pressed,
 * are not read out and are not counted as seen. With animations turned off the deck just opens and closes.
 */
@Composable
fun FeedDeck(top: AgentFeedItemView, model: FeedModel, actions: FeedActions, onShowInfo: (AgentFeedItemView) -> Unit, modifier: Modifier = Modifier) {
    val areAnimationsOff = rememberAreAnimationsOff()
    val isOpen = model.isDeckOpen(top.feedStackKey)
    val others = model.deckCards[top.feedStackKey].orEmpty().filter { card -> card.feedItemId != top.feedItemId }
    // The deck is the stack's own cards, so they are read with the feed.
    LaunchedEffect(top.feedStackKey) { model.loadDeckIfNeeded(top.feedStackKey) }
    val footCount = if (others.isEmpty()) top.stackEdgeCount else minOf(FOOT_INSETS.size, others.size)
    val progress = remember { Animatable(if (isOpen) 1f else 0f) }
    LaunchedEffect(isOpen) {
        val target = if (isOpen) 1f else 0f
        if (areAnimationsOff) {
            progress.snapTo(target)
        } else {
            progress.animateTo(target, if (isOpen) tween(OPEN_MS, easing = OPEN_EASING) else tween(CLOSE_MS, easing = CLOSE_EASING))
        }
    }
    var topCardHeight by remember { mutableIntStateOf(0) }
    val cornerPixels = with(LocalDensity.current) { FEED_CARD_CORNER.roundToPx() }
    Column(modifier) {
        // The top card is drawn over the column, so the tucked cards are hidden under it but for their feet.
        FeedCard(
            top, actions, onShowInfo = { onShowInfo(top) },
            modifier = Modifier.zIndex(1f).onSizeChanged { size -> topCardHeight = size.height },
            onOpenStack = { if (isOpen) model.closeDeck(top.feedStackKey) else model.openDeck(top.feedStackKey) },
            isDeckOpen = isOpen,
            isSwipeWholeDeck = !isOpen,
        )
        if (others.isEmpty()) {
            // Until the cards are read, the room their feet will take, so nothing below moves when they come.
            Spacer(Modifier.height(FOOT_STEP * footCount))
        } else {
            DeckColumn(progress = { progress.value }, footCount = footCount, clipAbove = { topCardHeight - cornerPixels }) {
                for (card in others) {
                    // The cards are seen once they are spread out, not while tucked.
                    if (isOpen) LaunchedEffect(card.feedItemId) { model.noteAppeared(card) }
                    Box(if (isOpen) Modifier else Modifier.clearAndSetSemantics {}) {
                        FeedCard(card, actions, onShowInfo = { onShowInfo(card) })
                        // A tucked card is inert: a press on its foot goes nowhere.
                        if (!isOpen) Box(Modifier.matchParentSize().pointerInput(Unit) { awaitPointerEventScope { while (true) awaitPointerEvent() } })
                    }
                }
            }
        }
    }
}

/**
 * The column under a deck's top card: its cards laid out one under another, [CARD_GAP] apart and below the top card, and
 * drawn [progress] of the way from tucked to there. Tucked, a card is scaled from its foot to be narrower than the top
 * card by its foot's inset and moved so its foot shows [FOOT_STEP] under the foot above; the cards past the last foot
 * hide behind it. The column is as tall as the feet when closed and as its cards when open, cut off at its bottom and at
 * [clipAbove] above its top, the top card's top edge less its corner radius: above that the top card's corners are
 * rounded and would let a tucked card's edge show beside them. The cards are measured once; the motion only resizes the column
 * and moves their layers.
 */
@Composable
private fun DeckColumn(progress: () -> Float, footCount: Int, clipAbove: () -> Int, content: @Composable () -> Unit) {
    Layout(
        content = content,
        modifier = Modifier.drawWithContent {
            val shadowRoom = SHADOW_ROOM.toPx()
            clipRect(left = -size.width, top = -clipAbove().toFloat(), right = size.width * 2, bottom = size.height + shadowRoom) { this@drawWithContent.drawContent() }
        },
    ) { measurables, constraints ->
        val gap = CARD_GAP.roundToPx()
        val placeables = measurables.map { measurable -> measurable.measure(constraints.copy(minHeight = 0, maxHeight = Constraints.Infinity)) }
        val tops = placeables.runningFold(gap) { top, placeable -> top + placeable.height + gap }
        val openHeight = tops.last() - gap
        val closedHeight = (FOOT_STEP * footCount).roundToPx()
        val width = placeables.maxOfOrNull { placeable -> placeable.width } ?: constraints.minWidth
        val shown = progress()
        layout(width, (closedHeight + (openHeight - closedHeight) * shown).roundToInt().coerceAtLeast(0)) {
            for ((index, placeable) in placeables.withIndex()) {
                val top = tops[index]
                val footLevel = index.coerceAtMost((footCount - 1).coerceAtLeast(0)).coerceAtMost(FOOT_INSETS.lastIndex)
                val footY = (FOOT_STEP * (footLevel + 1)).toPx()
                val inset: Dp = FOOT_INSETS[footLevel]
                // Each card is drawn over the cards under it, as in a deck.
                placeable.placeWithLayer(0, top, zIndex = -index.toFloat()) {
                    val tucked = 1f - progress()
                    val cardWidth = placeable.width.coerceAtLeast(1).toFloat()
                    val footScale = (cardWidth - 2 * inset.toPx()) / cardWidth
                    transformOrigin = TransformOrigin(0.5f, 1f)
                    val scale = 1f - tucked * (1f - footScale)
                    scaleX = scale
                    scaleY = scale
                    // Scaled from its foot, the foot stays put; it moves up to just under the foot above.
                    translationY = tucked * (footY - (top + placeable.height))
                }
            }
        }
    }
}
