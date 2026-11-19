package com.teanode.app.feed

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.foundation.clickable
import kotlinx.coroutines.delay
import androidx.compose.runtime.setValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.getValue
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Icon
import androidx.compose.material.icons.outlined.KeyboardArrowUp
import androidx.compose.material.icons.Icons
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.kit.feed.AgentFeedItemView

/**
 * A stack drawn as a deck (ios FeedList, FeedStackHeader): its top card with two edges peeking out below it. Pressing the
 * row under it fans the deck out in place: the stack's name and how many cards it holds appear above it with "Show
 * Less", and the other cards slide down out of the deck one after another, the rest of the feed moving down to make
 * room. "Show Less", or pressing the name, gathers them back. The motion is a spring of about 0.35 seconds, and none
 * when the person turned animations off.
 */
@Composable
fun FeedDeck(top: AgentFeedItemView, model: FeedModel, actions: FeedActions, onShowInfo: (AgentFeedItemView) -> Unit) {
    val areAnimationsOff = rememberAreAnimationsOff()
    val isOpen = model.isDeckOpen(top.feedStackKey)
    val others = model.deckCards[top.feedStackKey].orEmpty().filter { card -> card.feedItemId != top.feedItemId }
    Column {
        AnimatedVisibility(
            visible = isOpen,
            enter = expandVertically(deckSpec<IntSize>(areAnimationsOff)) + fadeIn(deckSpec(areAnimationsOff)),
            exit = shrinkVertically(deckSpec<IntSize>(areAnimationsOff)) + fadeOut(deckSpec(areAnimationsOff)),
        ) {
            DeckHeader(top, cardCount = maxOf(top.stackItemCount, others.size + 1), onGather = { model.closeDeck(top.feedStackKey) })
        }
        FeedCard(top, actions, onShowInfo = { onShowInfo(top) }, onOpenStack = { model.openDeck(top.feedStackKey) }, isDeckOpen = isOpen)
        Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
            for ((index, card) in others.withIndex()) {
                // Each card slides out a little after the one before it.
                var isOut by remember(card.feedItemId, isOpen) { mutableStateOf(false) }
                LaunchedEffect(card.feedItemId, isOpen) {
                    if (isOpen && !areAnimationsOff) delay(index * STAGGER_MS)
                    isOut = isOpen
                }
                AnimatedVisibility(
                    visible = isOut,
                    enter = expandVertically(deckSpec<IntSize>(areAnimationsOff), expandFrom = Alignment.Top) + fadeIn(deckSpec(areAnimationsOff)),
                    exit = shrinkVertically(deckSpec<IntSize>(areAnimationsOff), shrinkTowards = Alignment.Top) + fadeOut(deckSpec(areAnimationsOff)),
                ) {
                    Column(Modifier.padding(top = 12.dp)) {
                        LaunchedEffect(card.feedItemId) { model.noteAppeared(card) }
                        FeedCard(card, actions, onShowInfo = { onShowInfo(card) })
                    }
                }
            }
        }
    }
}

private const val STAGGER_MS = 50L

/** The top of an open deck: the stack's name and how many cards it holds, which gathers it back when pressed, and "Show Less". */
@Composable
private fun DeckHeader(top: AgentFeedItemView, cardCount: Int, onGather: () -> Unit) {
    val showLess = stringResource(R.string.feed_stack_show_less)
    val hint = stringResource(R.string.feed_stack_gather_hint)
    Row(Modifier.fillMaxWidth().padding(bottom = 8.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        Column(Modifier.weight(1f).clickable(role = Role.Button, onClickLabel = hint, onClick = onGather).heightIn(min = 44.dp), verticalArrangement = Arrangement.Center) {
            Text(top.stackName.ifEmpty { stringResource(R.string.feed_more_cards) }, style = MaterialTheme.typography.titleMedium)
            Text(stringResource(R.string.feed_stack_cards, cardCount), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        OutlinedButton(onClick = onGather) {
            Icon(Icons.Outlined.KeyboardArrowUp, contentDescription = null, modifier = Modifier.size(18.dp))
            Spacer(Modifier.size(6.dp))
            Text(showLess)
        }
    }
}
