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
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.AppSection
import com.teanode.kit.feed.AgentFeedItemView

/** The words for where a card came from. */
@Composable
fun feedSourceWords(item: AgentFeedItemView): String {
    val kindWords = stringResource(
        when (item.feedSourceKind) {
            "goal" -> R.string.feed_source_goal
            "schedule" -> R.string.feed_source_schedule
            "alert" -> R.string.feed_source_alert
            "coding_session" -> R.string.feed_source_coding_session
            else -> R.string.feed_source_agent
        },
    )
    return if (item.feedSourceTitle.isEmpty()) kindWords else "$kindWords: ${item.feedSourceTitle}"
}

/**
 * The info sheet of a card: where it came from (a goal, a schedule or an alert, which opens), when it was posted and
 * last updated, the pages it was made from, and "Show less like this", which records the opposite of a like and closes
 * the card.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun FeedInfoSheet(item: AgentFeedItemView, actions: FeedActions, onDismiss: () -> Unit) {
    val navigator = LocalAppNavigator.current
    val context = LocalContext.current
    fun dated(text: String): String? = feedEpochMS(text)?.let { epochMS ->
        DateUtils.formatDateTime(context, epochMS, DateUtils.FORMAT_SHOW_DATE or DateUtils.FORMAT_SHOW_TIME or DateUtils.FORMAT_SHOW_YEAR or DateUtils.FORMAT_ABBREV_MONTH)
    }
    val sourceOpening: (() -> Unit)? = when {
        item.feedSourceKind == "goal" -> { { onDismiss(); navigator.openSection(AppSection.Goals) } }
        item.codingSessionPlace != null -> { { onDismiss(); actions.open(item) } }
        else -> null
    }
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).padding(bottom = 24.dp)) {
            Text(stringResource(R.string.feed_info_title), style = MaterialTheme.typography.titleLarge, modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp))
            TeaNodeRow(headline = feedSourceWords(item), overline = stringResource(R.string.feed_info_source), onClick = sourceOpening)
            dated(item.postedAt)?.let { text -> TeaNodeRow(headline = text, overline = stringResource(R.string.feed_info_posted)) }
            if (item.updatedAt != item.postedAt) dated(item.updatedAt)?.let { text -> TeaNodeRow(headline = text, overline = stringResource(R.string.feed_info_updated)) }
            if (item.sourceLinks.isNotEmpty()) {
                TeaNodeSectionHeader(stringResource(R.string.feed_info_links))
                for (link in item.sourceLinks) {
                    TeaNodeRow(
                        headline = link.sourceTitle.ifEmpty { link.sourceUrl },
                        supporting = link.sourceUrl.takeIf { address -> link.sourceTitle.isNotEmpty() },
                        supportingMaxLines = 1,
                        leadingIcon = Icons.Outlined.Link,
                        onClick = { actions.followLink(link.sourceUrl) },
                    )
                }
            }
            Column(Modifier.padding(horizontal = 16.dp, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedButton(onClick = { onDismiss(); actions.showLessLikeThis(item) }, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.feed_info_show_less)) }
                TextButton(onClick = { onDismiss(); actions.dismiss(item) }, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.feed_info_dismiss)) }
            }
        }
    }
}
