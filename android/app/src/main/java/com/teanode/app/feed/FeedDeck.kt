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
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlin.math.roundToInt

/** How long a deck takes to fan out or gather, and how much later each card starts than the one above it, at most [STAGGER_LIMIT] steps. */
private const val DECK_MOTION_MS = 380
private const val STAGGER_MS = 45L
private const val STAGGER_LIMIT = 5

/** How far up under the top card a card waits in the deck. */
private val TUCK = 24.dp

/** The gap between the cards of an open deck, the feed's own. */
private val CARD_GAP = 12.dp

/** A card in the deck is drawn a little smaller, and grows to its size as it slides out. */
private const val TUCKED_SCALE = 0.96f

/** Fanning out overshoots a little and settles; gathering speeds up as the cards go. */
private val OPEN_EASING = CubicBezierEasing(0.34f, 1.2f, 0.64f, 1f)
private val CLOSE_EASING = CubicBezierEasing(0.4f, 0f, 0.9f, 0.6f)

/**
 * A stack drawn as a deck (ios FeedList): its top card with the edges of the cards behind it peeking out below. The row
 * under the top card fans the deck out in place and then says "Show Less", which gathers it back; the top card never
 * moves. Opening, the room under the top card grows to the cards' height while each card slides down from under the top
 * card into its place, a moment after the one above it; the room is cut off at its growing bottom edge only, so a card
 * is hidden under the top card until it slides out. Gathering is the same the other way, and the cards leave the screen
 * once it is over. With animations turned off the deck just opens and closes.
 */
@Composable
fun FeedDeck(top: AgentFeedItemView, model: FeedModel, actions: FeedActions, onShowInfo: (AgentFeedItemView) -> Unit, modifier: Modifier = Modifier) {
    val areAnimationsOff = rememberAreAnimationsOff()
    val isOpen = model.isDeckOpen(top.feedStackKey)
    val others = model.deckCards[top.feedStackKey].orEmpty().filter { card -> card.feedItemId != top.feedItemId }
    // The cards stay on screen while they gather, and leave once they are back in the deck.
    var isRoomShown by remember { mutableStateOf(isOpen) }
    val roomProgress = remember { Animatable(if (isOpen) 1f else 0f) }
    val cardProgress = remember { HashMap<String, Animatable<Float, *>>() }
    fun progressOf(card: AgentFeedItemView): Animatable<Float, *> = cardProgress.getOrPut(card.feedItemId) { Animatable(roomProgress.targetValue) }
    LaunchedEffect(isOpen) {
        val target = if (isOpen) 1f else 0f
        if (isOpen) isRoomShown = true
        if (areAnimationsOff) {
            roomProgress.snapTo(target)
            for (progress in cardProgress.values) progress.snapTo(target)
        } else {
            val spec = tween<Float>(DECK_MOTION_MS, easing = if (isOpen) OPEN_EASING else CLOSE_EASING)
            coroutineScope {
                launch { roomProgress.animateTo(target, spec) }
                for ((index, card) in others.withIndex()) {
                    // Opening, the first card goes first; gathering, the last one does.
                    val step = minOf(if (isOpen) index else others.size - 1 - index, STAGGER_LIMIT)
                    val progress = progressOf(card)
                    launch {
                        delay(step * STAGGER_MS)
                        progress.animateTo(target, spec)
                    }
                }
            }
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
            DeckRoom(roomProgress = { roomProgress.value }, cardProgress = { index -> progressOf(others[index]).value }) {
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
 * as tall as [roomProgress] of their height, cut off at its bottom edge only; each card sits [cardProgress] of the way
 * from under the top card to its place. Both are read while placing and drawing, so the motion lays out nothing again.
 */
@Composable
private fun DeckRoom(roomProgress: () -> Float, cardProgress: (Int) -> Float, content: @Composable () -> Unit) {
    Layout(
        content = content,
        modifier = Modifier.drawWithContent {
            // Only the bottom edge cuts. A card waiting in the deck starts a little above the room's top, under the top
            // card, which is drawn over it; the sides are left open for the overshoot of the scale.
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
        layout(width, (fullHeight * roomProgress()).roundToInt().coerceIn(0, fullHeight * 2)) {
            for ((index, placeable) in placeables.withIndex()) {
                val top = tops[index]
                // The card nearer the top card is drawn over the ones under it, as in a deck.
                placeable.placeWithLayer(0, top, zIndex = -index.toFloat()) {
                    val progress = cardProgress(index)
                    val hidden = 1f - progress
                    // From its place up to under the top card, a little higher still, and a little smaller.
                    translationY = -(top + tuck) * hidden
                    val scale = TUCKED_SCALE + (1f - TUCKED_SCALE) * progress
                    scaleX = scale
                    scaleY = scale
                    alpha = progress.coerceIn(0f, 1f)
                }
            }
        }
    }
}
