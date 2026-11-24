package com.teanode.app.feed

import android.text.format.DateUtils
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Link
import androidx.compose.material.icons.outlined.ThumbDown
import androidx.compose.material.icons.outlined.Cancel
import androidx.compose.material.icons.outlined.AdsClick
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.feed.AgentFeedItemView

/**
 * The information sheet of a card (ios FeedInformationSheet): why it is in the feed (which goal or alert
 * made it, with "Open Goals" for a goal's), when it was posted and updated, its sources, and "Show less like this" and
 * Dismiss, either of which takes the card out of the feed.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun FeedInfoSheet(item: AgentFeedItemView, actions: FeedActions, onDismiss: () -> Unit) {
    val context = LocalContext.current
    val model = actions.model
    val fewerLikeItText = stringResource(R.string.feed_said_fewer)
    fun dated(text: String): String? = feedEpochMS(text)?.let { epochMS ->
        DateUtils.formatDateTime(context, epochMS, DateUtils.FORMAT_SHOW_DATE or DateUtils.FORMAT_SHOW_TIME or DateUtils.FORMAT_SHOW_YEAR or DateUtils.FORMAT_ABBREV_MONTH)
    }
    val sourceSentence = stringResource(
        when (item.feedSourceKind) {
            "goal" -> R.string.feed_source_goal
            "alert" -> R.string.feed_source_alert
            "coding_session" -> R.string.feed_source_coding_session
            else -> R.string.feed_source_agent
        },
    )
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).padding(bottom = 24.dp)) {
            Text(stringResource(R.string.feed_info), style = MaterialTheme.typography.titleLarge, modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp))
            TeaNodeSectionHeader(stringResource(R.string.feed_info_why))
            TeaNodeRow(headline = sourceSentence, supporting = item.feedSourceTitle.takeIf { title -> title.isNotEmpty() })
            if (item.feedSourceKind == "goal") {
                TeaNodeRow(headline = stringResource(R.string.feed_open_goals), leadingIcon = Icons.Outlined.AdsClick, onClick = { onDismiss(); actions.openGoals() })
            }
            val posted = dated(item.postedAt)
            val updated = dated(item.updatedAt)
            if (posted != null || (updated != null && updated != posted)) TeaNodeSectionHeader(stringResource(R.string.feed_info_when))
            posted?.let { text -> TeaNodeRow(headline = text, overline = stringResource(R.string.feed_info_posted)) }
            if (updated != null && updated != posted) TeaNodeRow(headline = updated, overline = stringResource(R.string.feed_info_updated))
            if (item.sourceLinks.isNotEmpty()) {
                TeaNodeSectionHeader(stringResource(R.string.feed_info_sources))
                for (link in item.sourceLinks) {
                    TeaNodeRow(headline = link.sourceTitle.ifEmpty { link.sourceUrl }, leadingIcon = Icons.Outlined.Link, onClick = { actions.followLink(link.sourceUrl) })
                }
            }
            TeaNodeRow(headline = stringResource(R.string.feed_info_show_less), leadingIcon = Icons.Outlined.ThumbDown, onClick = { onDismiss(); model.showLessLike(item, fewerLikeItText) })
            TeaNodeRow(headline = stringResource(R.string.feed_info_dismiss), leadingIcon = Icons.Outlined.Cancel, onClick = { onDismiss(); model.dismiss(item) })
            Text(
                stringResource(R.string.feed_info_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
            )
            Column(Modifier.padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                TextButton(onClick = onDismiss, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.feed_info_done)) }
            }
        }
    }
}
