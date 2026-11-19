package com.teanode.app.feed

import androidx.compose.foundation.BorderStroke
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
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.Favorite
import androidx.compose.material.icons.outlined.FavoriteBorder
import androidx.compose.material.icons.outlined.Forum
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
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

private const val EDGE_STEP_DP = 6
private val EDGE_INSET_DP = listOf(14, 28)

private val CARD_SHAPE = RoundedCornerShape(18.dp)

/** When a time written by the server happened, in milliseconds; null for text that is not a time. */
fun feedEpochMS(text: String): Long? = runCatching { Instant.parse(text).toEpochMilli() }.getOrNull()

/**
 * One card of the feed (ios FeedViews.swift, FeedCard): its picture at the left, the headline, the words (or the
 * structured layout), a short table of facts, the choices the agent offers or what was chosen and came of it, the row
 * that opens a stack, and a footer of heart, Discuss, date and information. At the largest text sizes the picture sits
 * above the words.
 */
@Composable
fun FeedCard(
    item: AgentFeedItemView,
    actions: FeedActions,
    onShowInfo: () -> Unit,
    modifier: Modifier = Modifier,
    /** Fans out the deck a top card stands for; null inside an open stack's own screen, which shows its cards whole. */
    onOpenStack: ((AgentFeedItemView) -> Unit)? = null,
    /** Whether the deck this card tops is fanned out: its edges are gone and its row is not shown (the open stack has its own "Show less"). */
    isDeckOpen: Boolean = false,
) {
    val isStackShown = onOpenStack != null && item.isStack
    val edgeCount = if (isStackShown) item.stackEdgeCount else 0
    val areAnimationsOff = rememberAreAnimationsOff()
    // The edges peek out below the card and slip behind it as the deck fans out; with animations off they just go.
    val edgeAlphaRaw by animateFloatAsState(if (isDeckOpen) 0f else 1f, deckSpec(areAnimationsOff), label = "deck edges")
    val edgePaddingRaw by animateDpAsState(if (isDeckOpen) 0.dp else (EDGE_STEP_DP * edgeCount).dp, deckSpec(areAnimationsOff), label = "deck edge room")
    // A spring overshoots, and neither a padding nor an alpha may go below nothing.
    val edgeAlpha = edgeAlphaRaw.coerceIn(0f, 1f)
    val edgePadding = edgePaddingRaw.coerceAtLeast(0.dp)
    val edgeColor = MaterialTheme.colorScheme.surfaceContainerLow
    val edgeBorder = MaterialTheme.colorScheme.outlineVariant
    val isLargeText = LocalDensity.current.fontScale >= LARGE_TEXT_SCALE
    val uriHandler = remember(actions) { object : UriHandler { override fun openUri(uri: String) = actions.followLink(uri) } }
    CompositionLocalProvider(LocalUriHandler provides uriHandler, LocalLinkOpener provides { address -> actions.followLink(address) }) {
        // The edges of the cards under a stack's top one: the same card shape, each narrower and lower than the one before.
        Box(
            modifier.fillMaxWidth().drawBehind {
                val step = EDGE_STEP_DP.dp.toPx()
                val cardBottom = size.height - edgePadding.toPx()
                for (edge in edgeCount downTo 1) {
                    val inset = EDGE_INSET_DP[edge - 1].dp.toPx()
                    val top = cardBottom - 24.dp.toPx()
                    val edgeSize = Size(size.width - 2 * inset, cardBottom + step * edge - top)
                    drawRoundRect(edgeColor, Offset(inset, top), edgeSize, CornerRadius(18.dp.toPx()), alpha = edgeAlpha)
                    drawRoundRect(edgeBorder, Offset(inset, top), edgeSize, CornerRadius(18.dp.toPx()), style = Stroke(1.dp.toPx()), alpha = edgeAlpha)
                }
            }.padding(bottom = edgePadding),
        ) {
            Surface(Modifier.fillMaxWidth(), shape = CARD_SHAPE, color = MaterialTheme.colorScheme.surfaceContainerLow, border = BorderStroke(0.5.dp, MaterialTheme.colorScheme.outlineVariant)) {
                Column {
                    // The pictures sit at the card's top edge, edge to edge; the card's own shape rounds their corners.
                    if (item.cardMedia.isNotEmpty()) FeedMediaBlock(item.cardMedia, actions)
                    if (isLargeText) {
                        Column(Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                            FeedPictureTile(item.pictureSymbol)
                            FeedCardContent(item, actions, isStackShown && !isDeckOpen, isLargeText, onShowInfo) { onOpenStack?.invoke(item) }
                        }
                    } else {
                        Row(Modifier.padding(14.dp), horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.Top) {
                            FeedPictureTile(item.pictureSymbol)
                            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                                FeedCardContent(item, actions, isStackShown && !isDeckOpen, isLargeText, onShowInfo) { onOpenStack?.invoke(item) }
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun FeedCardContent(item: AgentFeedItemView, actions: FeedActions, isStackShown: Boolean, isLargeText: Boolean, onShowInfo: () -> Unit, onOpenStack: () -> Unit) {
    // A stack with a name of its own says it above its top card.
    if (isStackShown && item.feedStackTitle.isNotEmpty()) {
        Text(item.feedStackTitle, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
    // A small line above the headline names the site the picture comes from, as a news app labels a story.
    if (item.cardMedia.isNotEmpty() && item.headerText.isNotEmpty()) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            when (val picture = com.teanode.kit.feed.FeedPicture.of(item.pictureSymbol)) {
                is com.teanode.kit.feed.FeedPicture.Symbol -> Icon(picture.kind.icon(), contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.size(14.dp))
                is com.teanode.kit.feed.FeedPicture.Emoji -> Text(picture.text, style = MaterialTheme.typography.labelSmall)
            }
            Text(item.headerText, style = MaterialTheme.typography.labelMedium, fontWeight = FontWeight.SemiBold, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
        }
    }
    Text(item.title, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
    FeedLayoutBody(item, actions)
    FeedAnswerArea(item, actions)
    if (isStackShown) StackRow(item, onOpenStack)
    FeedFooter(item, actions, isLargeText, onShowInfo)
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
        item.choices.isNotEmpty() && !item.isDismissed -> Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
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

/** A choice as a button as wide as the card: the action filled, the others bordered, both in a capsule. */
@Composable
private fun FeedChoiceButton(choice: AgentFeedChoice, isDisabled: Boolean, onPress: () -> Unit) {
    val modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp)
    if (choice.role == AgentFeedChoiceRole.Action) {
        Button(onClick = onPress, enabled = !isDisabled, modifier = modifier) { Text(choice.choiceLabel) }
    } else {
        OutlinedButton(onClick = onPress, enabled = !isDisabled, modifier = modifier) { Text(choice.choiceLabel) }
    }
}

/** "+3 more from News about humanoid robots": the row under a stack's top card that opens the rest. */
@Composable
private fun StackRow(item: AgentFeedItemView, onClick: () -> Unit) {
    val words = if (item.stackName.isEmpty()) stringResource(R.string.feed_more_in_stack_unnamed, item.moreInStackCount) else stringResource(R.string.feed_more_in_stack, item.moreInStackCount, item.stackName)
    Row(
        Modifier.fillMaxWidth().heightIn(min = 44.dp).clickable(role = Role.Button, onClick = onClick),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(words, style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary, modifier = Modifier.weight(1f))
        Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = null, tint = MaterialTheme.colorScheme.outline, modifier = Modifier.size(20.dp))
    }
}

/** The heart, Discuss, the date and the info button; at the largest text sizes the date goes under the row. */
@Composable
private fun FeedFooter(item: AgentFeedItemView, actions: FeedActions, isLargeText: Boolean, onShowInfo: () -> Unit) {
    val isLiked = actions.model.isLiked(item)
    val dateText = (feedEpochMS(item.shownAtText))?.let { epochMS -> relativeTimeText(epochMS) }.orEmpty()
    Column {
        Row(Modifier.fillMaxWidth().heightIn(min = 48.dp), verticalAlignment = Alignment.CenterVertically) {
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
            if (!isLargeText) Text(dateText, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
            IconButton(onClick = onShowInfo) { Icon(Icons.Outlined.Info, contentDescription = stringResource(R.string.feed_info), tint = MaterialTheme.colorScheme.onSurfaceVariant) }
        }
        if (isLargeText) Text(dateText, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(bottom = 4.dp))
    }
}

/**
 * The motion of a deck fanning out or gathering back: a spring of about 0.35 seconds, and none at all when the person
 * turned animations off in the system settings.
 */
fun <Value> deckSpec(areAnimationsOff: Boolean): FiniteAnimationSpec<Value> =
    if (areAnimationsOff) snap() else spring(dampingRatio = 0.85f, stiffness = 420f)
