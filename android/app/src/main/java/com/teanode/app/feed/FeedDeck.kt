package com.teanode.app.feed

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
 * A stack drawn as a deck: its top card with two edges peeking out below it. Pressing the row under it fans the deck
 * out in place, the other cards sliding down out of the deck into a column with the rest of the feed moving down to
 * make room; "Show less" above the open stack, or pressing its title, gathers them back. The motion is a spring of about
 * 0.35 seconds, and none when the person turned animations off.
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
            DeckHeader(top, onGather = { model.closeDeck(top.feedStackKey) })
        }
        FeedCard(top, actions, onShowInfo = { onShowInfo(top) }, onOpenStack = { model.openDeck(top.feedStackKey) }, isDeckOpen = isOpen)
        AnimatedVisibility(
            visible = isOpen && others.isNotEmpty(),
            enter = expandVertically(deckSpec<IntSize>(areAnimationsOff), expandFrom = Alignment.Top) + fadeIn(deckSpec(areAnimationsOff)),
            exit = shrinkVertically(deckSpec<IntSize>(areAnimationsOff), shrinkTowards = Alignment.Top) + fadeOut(deckSpec(areAnimationsOff)),
        ) {
            Column(Modifier.padding(top = 12.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                for (card in others) {
                    LaunchedEffect(card.feedItemId) { model.noteAppeared(card) }
                    FeedCard(card, actions, onShowInfo = { onShowInfo(card) })
                }
            }
        }
    }
}

/** The line above an open deck: the stack's name (which gathers it back when pressed) and "Show less". */
@Composable
private fun DeckHeader(top: AgentFeedItemView, onGather: () -> Unit) {
    val showLess = stringResource(R.string.feed_stack_show_less)
    Row(Modifier.fillMaxWidth().padding(bottom = 4.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(
            top.stackName.ifEmpty { stringResource(R.string.feed_more_cards) }, style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.weight(1f).clickable(role = Role.Button, onClickLabel = showLess, onClick = onGather).padding(vertical = 8.dp),
        )
        TextButton(onClick = onGather) { Text(showLess) }
    }
}
