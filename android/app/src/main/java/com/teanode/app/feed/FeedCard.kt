package com.teanode.app.feed

import androidx.compose.foundation.layout.Arrangement
import com.teanode.kit.feed.FeedLayout
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.draw.drawBehind
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.background
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.Favorite
import androidx.compose.material.icons.outlined.FavoriteBorder
import androidx.compose.material.icons.outlined.Forum
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.platform.UriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.chats.LocalLinkOpener
import com.teanode.app.chats.MarkdownView
import com.teanode.app.ui.components.relativeTimeText
import com.teanode.kit.feed.AgentFeedChoice
import com.teanode.kit.feed.AgentFeedChoiceRole
import com.teanode.kit.feed.AgentFeedItemView
import com.teanode.kit.feed.feedLinkAddress
import java.time.Instant

/** The font scale from which a card lays its text under the picture instead of beside it. */
private const val LARGE_TEXT_SCALE = 1.5f

/** When a time written by the server happened, in milliseconds; null for text that is not a time. */
fun feedEpochMS(text: String): Long? = runCatching { Instant.parse(text).toEpochMilli() }.getOrNull()

/**
 * One card of the feed (ios FeedViews.swift): a picture, the headline, the text with its links, the short table of
 * facts, the records it points at, then the choices (or what came of the one made) and a footer with the heart,
 * Discuss, the date and the info button.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun FeedCard(
    item: AgentFeedItemView,
    actions: FeedActions,
    onShowInfo: () -> Unit,
    modifier: Modifier = Modifier,
    /** Opens the stack a top card stands for; null where the card is drawn on its own (a stack's own screen, a single card). */
    onOpenStack: ((AgentFeedItemView) -> Unit)? = null,
) {
    val isStackTop = onOpenStack != null && item.isStack
    val edgeCount = if (isStackTop) item.stackEdgeCount else 0
    val edgeColor = MaterialTheme.colorScheme.surfaceContainerHighest
    val edgeOutline = MaterialTheme.colorScheme.outline
    val layout = remember(item.feedCardLayout, item.layoutContent) { item.layout }
    val isLargeText = LocalDensity.current.fontScale >= LARGE_TEXT_SCALE
    val uriHandler = remember(actions) { object : UriHandler { override fun openUri(uri: String) = actions.followLink(uri) } }
    CompositionLocalProvider(LocalUriHandler provides uriHandler, LocalLinkOpener provides { address -> actions.followLink(address) }) {
        // The card edges of a stack show under the card, each a little narrower than the one above.
        Box(
            modifier.fillMaxWidth().drawBehind {
                val step = EDGE_STEP_DP.dp.toPx()
                val inset = EDGE_INSET_DP.dp.toPx()
                val cardBottom = size.height - step * edgeCount
                for (edge in edgeCount downTo 1) {
                    val left = inset * edge
                    val bottom = cardBottom + step * edge
                    drawRoundRect(edgeColor, Offset(left, cardBottom - 24.dp.toPx()), Size(size.width - 2 * left, bottom - cardBottom + 24.dp.toPx()), CornerRadius(16.dp.toPx()))
                    drawRoundRect(edgeOutline, Offset(left, cardBottom - 24.dp.toPx()), Size(size.width - 2 * left, bottom - cardBottom + 24.dp.toPx()), CornerRadius(16.dp.toPx()), style = Stroke(1.dp.toPx()))
                }
            }.padding(bottom = (EDGE_STEP_DP * edgeCount).dp),
        ) {
        Card(Modifier.fillMaxWidth(), colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainerLow)) {
            Column(Modifier.padding(start = 16.dp, end = 16.dp, top = 16.dp, bottom = 4.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Row(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.Top) {
                    FeedPictureTile(item.pictureSymbol)
                    Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            Text(item.title, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold, modifier = Modifier.weight(1f, fill = false))
                            if (!item.isRead) UnreadDot()
                        }
                        if (item.isUrgent && item.isAwaitingChoice) {
                            Text(stringResource(R.string.feed_urgent), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.error)
                        }
                        if (item.body.isNotEmpty() && layout is FeedLayout.Text && !isLargeText) MarkdownView(item.body)
                    }
                }
                // At the largest text sizes the column beside the picture is too narrow to read, so the text takes the card's width.
                if (item.body.isNotEmpty() && layout is FeedLayout.Text && isLargeText) MarkdownView(item.body)
                when (layout) {
                    is FeedLayout.Rows -> FeedListRows(layout.rows, actions)
                    is FeedLayout.Weather -> FeedWeather(layout.weather)
                    is FeedLayout.Agenda -> FeedAgenda(layout.entries, actions)
                    FeedLayout.Text -> Unit
                }
                if (item.factRows.isNotEmpty()) {
                    Column {
                        for ((index, row) in item.factRows.withIndex()) {
                            if (index > 0) HorizontalDivider()
                            Row(Modifier.fillMaxWidth().padding(vertical = 6.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                                Text(row.factLabel, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.weight(1f))
                                Text(row.factValue, style = MaterialTheme.typography.bodyMedium, fontWeight = FontWeight.Medium, modifier = Modifier.weight(1f))
                            }
                        }
                    }
                }
                if (item.recordReferences.isNotEmpty()) {
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        for (reference in item.recordReferences) {
                            val address = reference.feedLinkAddress
                            AssistChip(
                                onClick = { if (address != null) actions.followLink(address) },
                                enabled = address != null,
                                label = { Text(reference.label.ifEmpty { stringResource(R.string.feed_info_references) }, maxLines = 1) },
                            )
                        }
                    }
                }
                if (isStackTop) StackMoreRow(item) { onOpenStack?.invoke(item) }
                FeedChoices(item, actions)
                FeedFooter(item, actions, onShowInfo)
            }
        }
        }
    }
}

@Composable
private fun UnreadDot() {
    val description = stringResource(R.string.feed_unread)
    Box(Modifier.size(8.dp).background(MaterialTheme.colorScheme.primary, CircleShape).semantics { contentDescription = description })
}

/** The buttons of a card that awaits its answer, or what came of the answer: working on it, or the agent's line. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun FeedChoices(item: AgentFeedItemView, actions: FeedActions) {
    if (item.isAwaitingChoice) {
        val busyKey = actions.busyChoice?.takeIf { busy -> busy.first == item.feedItemId }?.second
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            for (choice in item.choices) FeedChoiceButton(choice, isBusy = busyKey == choice.choiceKey, isEnabled = busyKey == null) { actions.choose(item, choice) }
        }
        return
    }
    val chosen = item.chosenChoice ?: return
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            if (item.isOutcomePending) {
                CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
            } else {
                Icon(Icons.Outlined.CheckCircle, contentDescription = null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(18.dp))
            }
            Text(stringResource(R.string.feed_you_chose, chosen.choiceLabel), style = MaterialTheme.typography.labelLarge)
        }
        if (item.isOutcomePending) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(stringResource(R.string.feed_working), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.weight(1f))
                if (item.canOpenChat) TextButton(onClick = { actions.openMainChat() }) { Text(stringResource(R.string.feed_open_chat)) }
            }
        }
        item.settledText?.let { text -> Text(text, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant) }
    }
}

/** A choice as a button: an action is the filled one, the others quieter, and the one pressed shows that it is working. */
@Composable
private fun FeedChoiceButton(choice: AgentFeedChoice, isBusy: Boolean, isEnabled: Boolean, onClick: () -> Unit) {
    val content: @Composable () -> Unit = {
        if (isBusy) {
            CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
            Spacer(Modifier.size(8.dp))
        }
        Text(choice.choiceLabel)
    }
    when (choice.role) {
        AgentFeedChoiceRole.Action -> Button(onClick = onClick, enabled = isEnabled) { content() }
        AgentFeedChoiceRole.Discuss -> FilledTonalButton(onClick = onClick, enabled = isEnabled) { content() }
        AgentFeedChoiceRole.Dismiss, AgentFeedChoiceRole.Open -> OutlinedButton(onClick = onClick, enabled = isEnabled) { content() }
    }
}

/** The heart, Discuss, the date and the info button. */
@Composable
private fun FeedFooter(item: AgentFeedItemView, actions: FeedActions, onShowInfo: () -> Unit) {
    val date = (feedEpochMS(item.updatedAt) ?: feedEpochMS(item.postedAt))?.let { epochMS -> relativeTimeText(epochMS) }.orEmpty()
    Row(Modifier.fillMaxWidth().height(48.dp), verticalAlignment = Alignment.CenterVertically) {
        IconButton(onClick = { actions.toggleLike(item) }) {
            Icon(
                if (item.isLiked) Icons.Outlined.Favorite else Icons.Outlined.FavoriteBorder,
                contentDescription = stringResource(if (item.isLiked) R.string.feed_unlike else R.string.feed_like),
                tint = if (item.isLiked) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        TextButton(onClick = { actions.discuss(item) }) {
            Icon(Icons.Outlined.Forum, contentDescription = null, modifier = Modifier.size(18.dp))
            Spacer(Modifier.size(6.dp))
            Text(stringResource(R.string.feed_discuss))
        }
        Spacer(Modifier.weight(1f))
        Text(date, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
        IconButton(onClick = onShowInfo) { Icon(Icons.Outlined.Info, contentDescription = stringResource(R.string.feed_info)) }
    }
}

/** "+3 more from News about humanoid robots": the pile under a stack's top card, which opens it. */
@Composable
private fun StackMoreRow(item: AgentFeedItemView, onClick: () -> Unit) {
    val words = if (item.stackName.isEmpty()) stringResource(R.string.feed_more_in_stack_unnamed, item.moreInStackCount) else stringResource(R.string.feed_more_in_stack, item.moreInStackCount, item.stackName)
    val label = stringResource(R.string.feed_open_stack)
    Row(
        Modifier.fillMaxWidth().clickable(role = Role.Button, onClickLabel = label, onClick = onClick).padding(vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(words, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary, modifier = Modifier.weight(1f), maxLines = 2)
        if (item.stackUnreadCount > 0) UnreadDot()
        Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = null, tint = MaterialTheme.colorScheme.primary)
    }
}

private const val EDGE_STEP_DP = 10
private const val EDGE_INSET_DP = 12
