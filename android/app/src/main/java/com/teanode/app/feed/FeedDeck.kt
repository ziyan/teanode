package com.teanode.app.feed

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.CubicBezierEasing
import androidx.compose.animation.core.tween
import androidx.compose.foundation.layout.Column
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.graphics.drawscope.clipRect
import androidx.compose.ui.layout.Layout
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.dp
import androidx.compose.ui.zIndex
import com.teanode.kit.feed.AgentFeedItemView
import kotlin.math.roundToInt

/**
 * How long a deck takes to fan out and to gather. Every card and the room under the top card move on the same curve, so
 * the cards spread out of the deck evenly, each still partly under the next, and never cross.
 */
private const val OPEN_MS = 440
private const val CLOSE_MS = 340

/** How far up under the top card a card waits in the deck. */
private val TUCK = 24.dp

/** The gap between the cards of an open deck, the feed's own. */
private val CARD_GAP = 12.dp

/** A card in the deck is drawn a little smaller, and grows to its size as it slides out. */
private const val TUCKED_SCALE = 0.96f

/**
 * Neither curve overshoots: a room that grew past its cards and came back would push the card below the deck down and up
 * again.
 */
private val OPEN_EASING = CubicBezierEasing(0.45f, 0f, 0.2f, 1f)
private val CLOSE_EASING = CubicBezierEasing(0.5f, 0f, 0.3f, 1f)

/**
 * A stack drawn as a deck (ios FeedList): its top card with the edges of the cards behind it peeking out below. The row
 * under the top card fans the deck out in place and then says "Show Less", which gathers it back; the top card never
 * moves. Opening, the room under the top card grows to the cards' height while every card slides down from under the top
 * card into its place on the same curve, like a deck spread on a table. The cards stay opaque and each lower card is
 * drawn over the one above it, so what shows of each card while they spread is its header and headline. The room is cut
 * off at its growing bottom edge only, so a card is hidden under the top card until it slides out. Gathering is the same
 * the other way, and the cards leave the screen once it is over. With animations turned off the deck just opens and
 * closes.
 */
@Composable
fun FeedDeck(top: AgentFeedItemView, model: FeedModel, actions: FeedActions, onShowInfo: (AgentFeedItemView) -> Unit, modifier: Modifier = Modifier) {
    val areAnimationsOff = rememberAreAnimationsOff()
    val isOpen = model.isDeckOpen(top.feedStackKey)
    val others = model.deckCards[top.feedStackKey].orEmpty().filter { card -> card.feedItemId != top.feedItemId }
    // The cards stay on screen while they gather, and leave once they are back in the deck.
    var isRoomShown by remember { mutableStateOf(isOpen) }
    val progress = remember { Animatable(if (isOpen) 1f else 0f) }
    LaunchedEffect(isOpen) {
        val target = if (isOpen) 1f else 0f
        if (isOpen) isRoomShown = true
        if (areAnimationsOff) {
            progress.snapTo(target)
        } else {
            progress.animateTo(target, if (isOpen) tween(OPEN_MS, easing = OPEN_EASING) else tween(CLOSE_MS, easing = CLOSE_EASING))
        }
        if (!isOpen) isRoomShown = false
    }
    Column(modifier) {
        // The top card is drawn over the room, so the cards still in the deck are hidden under it.
        FeedCard(
            top, actions, onShowInfo = { onShowInfo(top) },
            modifier = Modifier.zIndex(1f),
            onOpenStack = { if (isOpen) model.closeDeck(top.feedStackKey) else model.openDeck(top.feedStackKey) },
            isDeckOpen = isOpen,
        )
        if (isRoomShown && others.isNotEmpty()) {
            DeckRoom(progress = { progress.value }) {
                for (card in others) {
                    LaunchedEffect(card.feedItemId) { model.noteAppeared(card) }
                    FeedCard(card, actions, onShowInfo = { onShowInfo(card) })
                }
            }
        }
    }
}

/**
 * The room under a deck's top card: its cards one under another, [CARD_GAP] apart and below the top card. The room is
 * as tall as [progress] of their height, cut off at its bottom edge only, and each card sits [progress] of the way from
 * under the top card to its place. The cards are measured once; the motion only resizes the room and moves their layers.
 */
@Composable
private fun DeckRoom(progress: () -> Float, content: @Composable () -> Unit) {
    Layout(
        content = content,
        modifier = Modifier.drawWithContent {
            // Only the bottom edge cuts. A card waiting in the deck starts a little above the room's top, under the top
            // card, which is drawn over it.
            val tuck = TUCK.toPx()
            clipRect(left = -size.width, top = -tuck * 2, right = size.width * 2, bottom = size.height) { this@drawWithContent.drawContent() }
        },
    ) { measurables, constraints ->
        val gap = CARD_GAP.roundToPx()
        val tuck = TUCK.toPx()
        val placeables = measurables.map { measurable -> measurable.measure(constraints.copy(minHeight = 0, maxHeight = Constraints.Infinity)) }
        val tops = placeables.runningFold(gap) { top, placeable -> top + placeable.height + gap }
        val fullHeight = tops.last() - gap
        val width = placeables.maxOfOrNull { placeable -> placeable.width } ?: constraints.minWidth
        layout(width, (fullHeight * progress()).roundToInt().coerceIn(0, fullHeight)) {
            // Placed in order, each lower card is drawn over the one above it, as in a fanned deck; the top card, outside
            // the room, is drawn over them all.
            for ((index, placeable) in placeables.withIndex()) {
                val top = tops[index]
                placeable.placeWithLayer(0, top) {
                    val shown = progress()
                    // From its place up to under the top card, a little higher still, and a little smaller.
                    translationY = -(top + tuck) * (1f - shown)
                    val scale = TUCKED_SCALE + (1f - TUCKED_SCALE) * shown
                    scaleX = scale
                    scaleY = scale
                }
            }
        }
    }
}
