package com.teanode.app.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.calendar.DateWords
import com.teanode.app.chats.ConversationScreen
import com.teanode.app.chats.MarkdownView
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.relativeTimeText
import com.teanode.kit.agent.AgentBackgroundWork
import com.teanode.kit.agent.AgentReply
import com.teanode.kit.agent.AgentRunKinds
import com.teanode.kit.agent.AgentRunSummary
import com.teanode.kit.agent.WorkCondition
import com.teanode.kit.agent.agentReplies
import com.teanode.kit.agent.agentRuns
import com.teanode.kit.agent.backgroundWork
import com.teanode.kit.agent.backgroundWorkResult
import com.teanode.kit.agent.condition
import com.teanode.kit.agent.mergeRunPages
import com.teanode.kit.agent.stopBackgroundWork
import com.teanode.kit.mail.cancelAgentReply
import com.teanode.kit.photos.ServerTime
import kotlinx.coroutines.delay
import java.time.ZoneId

/** How many runs a page holds. */
private const val RUNS_PAGE_SIZE = 50

@Composable
private fun runKindName(jobKind: String): String {
    val resources = LocalContext.current.resources
    val identifier = resources.getIdentifier("settings_activity_kind_$jobKind", "string", LocalContext.current.packageName)
    return if (identifier != 0) stringResource(identifier) else jobKind
}

@Composable
private fun WorkTag(work: AgentBackgroundWork) {
    when (work.condition) {
        WorkCondition.Queued -> UpkeepTag(stringResource(R.string.settings_activity_state_queued))
        WorkCondition.Running -> UpkeepTag(stringResource(R.string.settings_activity_state_running))
        WorkCondition.Finished -> UpkeepTag(stringResource(R.string.settings_activity_state_finished), TagTone.Good)
        WorkCondition.Failed -> UpkeepTag(stringResource(R.string.settings_activity_state_failed), TagTone.Bad)
        WorkCondition.Stopped -> UpkeepTag(stringResource(R.string.settings_activity_state_stopped), TagTone.Warning)
    }
}

@Composable
private fun ReplyTag(reply: AgentReply) {
    when (reply.status) {
        "held" -> UpkeepTag(stringResource(R.string.settings_activity_reply_held), TagTone.Warning)
        "sent" -> UpkeepTag(stringResource(R.string.settings_activity_reply_sent), TagTone.Good)
        "cancelled" -> UpkeepTag(stringResource(R.string.settings_activity_reply_cancelled))
        "refused" -> UpkeepTag(stringResource(R.string.settings_activity_reply_refused))
        else -> UpkeepTag(stringResource(R.string.settings_activity_reply_failed), TagTone.Bad)
    }
}

@Composable
private fun replyDetails(reply: AgentReply): String {
    val details = mutableListOf(reply.to.orEmpty(), whenText(reply.sentAt ?: reply.createdAt))
    reply.reason?.takeIf { text -> text.isNotEmpty() }?.let { text -> details += text }
    if (reply.isHeld && ServerTime.instant(reply.sendAfter.orEmpty()) != null) details += stringResource(R.string.settings_activity_reply_sends_at, whenText(reply.sendAfter))
    return details.filter { text -> text.isNotEmpty() }.joinToString(" · ")
}

/**
 * The Activity page: the work the agent started in the background, each stopped or opened; every run it made, newest
 * first, a page at a time, narrowed by kind or words or to one dream, each opening its transcript; and what it answered
 * for the person, a held reply cancelled.
 */
@Composable
fun AgentActivityScreen(onBack: () -> Unit, dreamJobId: String? = null, dreamStartedAt: String? = null) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    var dreamFilter by remember { mutableStateOf(dreamJobId) }
    var query by remember { mutableStateOf("") }
    var jobKind by remember { mutableStateOf("") }
    var works by remember { mutableStateOf<List<AgentBackgroundWork>>(emptyList()) }
    var runs by remember { mutableStateOf<List<AgentRunSummary>?>(null) }
    var runTotal by remember { mutableStateOf(0) }
    var replies by remember { mutableStateOf<List<AgentReply>>(emptyList()) }
    var isLoadingMore by remember { mutableStateOf(false) }
    var openedWork by remember { mutableStateOf<AgentBackgroundWork?>(null) }
    var openedRunId by remember { mutableStateOf<String?>(null) }
    var stopping by remember { mutableStateOf<AgentBackgroundWork?>(null) }
    var cancelling by remember { mutableStateOf<AgentReply?>(null) }
    var refreshCount by remember { mutableStateOf(0) }

    suspend fun loadWorks() { runCatching { connection.backgroundWork() }.onSuccess { read -> works = read } }
    suspend fun loadReplies() { runCatching { connection.agentReplies() }.onSuccess { read -> replies = read.replies } }
    suspend fun loadRuns() {
        runCatching { connection.agentRuns(RUNS_PAGE_SIZE, 0, dreamFilter, if (jobKind.isEmpty()) emptyList() else listOf(jobKind), query) }
            .onSuccess { page -> runs = page.runs; runTotal = page.total }
            .onFailure { if (runs == null) runs = emptyList() }
    }

    // A change of what the runs are narrowed by reads them again from the first page, after the person stops typing.
    LaunchedEffect(query, jobKind, dreamFilter, refreshCount) {
        if (runs != null) delay(400)
        loadRuns()
    }
    // Read again while it is open: runs are ordered by their last message, and work in the background finishes.
    LaunchedEffect(refreshCount) {
        loadWorks()
        loadReplies()
        while (true) {
            delay(15_000)
            loadWorks()
            if ((runs?.size ?: 0) <= RUNS_PAGE_SIZE) loadRuns()
        }
    }

    openedRunId?.let { runId ->
        ConversationScreen(conversationId = runId, onBack = { openedRunId = null }, onOpenConversation = { id -> openedRunId = id })
        return
    }
    openedWork?.let { work ->
        BackgroundWorkResultPage(work, onBack = { openedWork = null }, onOpenRun = { runId -> openedWork = null; openedRunId = runId })
        return
    }

    val context = LocalContext.current
    val cancelledText = stringResource(R.string.settings_activity_reply_cancelled_said)
    TeaNodeScreen(stringResource(R.string.settings_activity_title), onBack = onBack) { contentPadding ->
        androidx.compose.foundation.lazy.LazyColumn(Modifier.padding(contentPadding), contentPadding = androidx.compose.foundation.layout.PaddingValues(bottom = 24.dp)) {
            if (works.isNotEmpty()) {
                item { TeaNodeSectionHeader(stringResource(R.string.settings_activity_work_header)) }
                items(works.size) { index ->
                    val work = works[index]
                    val kindText = stringResource(if (work.isSurvey) R.string.settings_activity_survey else R.string.settings_activity_subagent)
                    val details = listOfNotNull(kindText, stringResource(R.string.settings_activity_started, relativeText(work.createdAt)), work.errorMessage.ifEmpty { null }).joinToString(" · ")
                    TeaNodeRow(
                        headline = work.title.ifEmpty { work.id }, supporting = details, supportingMaxLines = 3,
                        onClick = if (work.isOpenable) ({ openedWork = work }) else null,
                        trailing = {
                            Row(verticalAlignment = Alignment.CenterVertically) {
                                WorkTag(work)
                                RowMenu(
                                    buildList {
                                        if (work.isOpenable) add(stringResource(R.string.settings_activity_open) to { openedWork = work })
                                        if (work.isStoppable) add(stringResource(R.string.settings_activity_stop_action) to { stopping = work })
                                    },
                                )
                            }
                        },
                    )
                }
                item { FooterText(stringResource(R.string.settings_activity_work_footer)) }
            }
            item {
                TeaNodeSectionHeader(stringResource(if (runTotal == 1) R.string.settings_activity_runs_one else R.string.settings_activity_runs_many, runTotal))
                OutlinedTextField(
                    value = query, onValueChange = { text -> query = text }, singleLine = true, label = { Text(stringResource(R.string.settings_activity_search)) },
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp), keyboardOptions = KeyboardOptions.Default,
                )
                ChoiceRow(
                    stringResource(R.string.settings_activity_kind_filter), listOf("") + AgentRunKinds.jobKinds, jobKind,
                    label = { kind -> if (kind.isEmpty()) stringResource(R.string.settings_activity_every_kind) else runKindName(kind) }, onSelect = { kind -> jobKind = kind },
                )
            }
            if (dreamFilter != null) {
                item {
                    val dreamText = dreamStartedAt?.let { stamp -> whenText(stamp) } ?: dreamFilter.orEmpty()
                    TeaNodeRow(headline = stringResource(R.string.settings_activity_dream_at, dreamText))
                    TeaNodeRow(headline = stringResource(R.string.settings_activity_show_all), onClick = { dreamFilter = null })
                }
            }
            val shownRuns = runs
            if (shownRuns == null) {
                item { Row(Modifier.fillMaxWidth().padding(24.dp), horizontalArrangement = Arrangement.Center) { CircularProgressIndicator() } }
            } else {
                if (shownRuns.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.settings_activity_nothing)) }
                items(shownRuns.size) { index ->
                    val run = shownRuns[index]
                    val detailText = listOfNotNull(whenText(run.lastAt).ifEmpty { null }, compactNumber(run.tokenCount), if (run.usage.cost > 0) moneyText(run.usage.cost, null) else null).joinToString(" · ")
                    TeaNodeRow(
                        headline = run.title, overline = run.jobKind?.takeIf { kind -> kind.isNotEmpty() }?.let { kind -> runKindName(kind) }, supporting = detailText,
                        onClick = { openedRunId = run.id },
                    )
                    if (index == shownRuns.lastIndex) {
                        LaunchedEffect(shownRuns.size, runTotal) {
                            if (shownRuns.size < runTotal && !isLoadingMore) {
                                isLoadingMore = true
                                runCatching { connection.agentRuns(RUNS_PAGE_SIZE, shownRuns.size, dreamFilter, if (jobKind.isEmpty()) emptyList() else listOf(jobKind), query) }
                                    .onSuccess { page -> runs = mergeRunPages(shownRuns, page.runs); runTotal = page.total }
                                isLoadingMore = false
                            }
                        }
                    }
                }
                if (isLoadingMore) item { Row(Modifier.fillMaxWidth().padding(16.dp), horizontalArrangement = Arrangement.Center) { CircularProgressIndicator(strokeWidth = 2.dp) } }
                item {
                    FooterText(
                        if (dreamFilter != null) stringResource(R.string.settings_activity_dream_runs_footer, dreamStartedAt?.let { stamp -> whenText(stamp) }.orEmpty())
                        else stringResource(R.string.settings_activity_runs_footer),
                    )
                }
            }
            item { TeaNodeSectionHeader(stringResource(R.string.settings_activity_replies_header)) }
            if (replies.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.settings_activity_replies_none), supportingMaxLines = 3) }
            items(replies.size) { index ->
                val reply = replies[index]
                TeaNodeRow(
                    headline = reply.subject.ifEmpty { stringResource(R.string.settings_alerts_no_subject) }, supporting = replyDetails(reply), supportingMaxLines = 4,
                    trailing = {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            ReplyTag(reply)
                            if (reply.isHeld) TextButton(onClick = { cancelling = reply }) { Text(stringResource(R.string.settings_activity_reply_cancel_short)) }
                        }
                    },
                )
            }
            item { FooterText(stringResource(R.string.settings_activity_replies_footer)) }
        }
    }
    stopping?.let { work ->
        ConfirmationDialog(
            title = stringResource(R.string.settings_activity_stop_action), text = work.title.ifEmpty { work.id }, confirmLabel = stringResource(R.string.settings_activity_stop_action), isDestructive = true,
            onConfirm = { stopping = null; actions.run(context.getString(R.string.settings_activity_stopped_work, work.title), onSuccess = { refreshCount += 1 }) { connection.stopBackgroundWork(work.id) } }, onDismiss = { stopping = null },
        )
    }
    cancelling?.let { reply ->
        ConfirmationDialog(
            title = stringResource(R.string.settings_activity_reply_cancel), text = reply.subject, confirmLabel = stringResource(R.string.settings_activity_reply_cancel), isDestructive = true,
            onConfirm = { cancelling = null; actions.run(cancelledText, onSuccess = { refreshCount += 1 }) { connection.cancelAgentReply(reply.id) } }, onDismiss = { cancelling = null },
        )
    }
}

/** What came of one piece of work: the report or the answer to read, or why it failed, and the runs it made, each opening its transcript. */
@Composable
private fun BackgroundWorkResultPage(work: AgentBackgroundWork, onBack: () -> Unit, onOpenRun: (String) -> Unit) {
    val connection = LocalConnection.current
    val loaded = com.teanode.app.ui.components.rememberLoad { connection.backgroundWorkResult(work.id) }
    LoadedSettingsPage(work.title.ifEmpty { work.id }, onBack, loaded) { result ->
        val finished = result ?: work
        item {
            Row(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                WorkTag(finished)
                Text(
                    stringResource(if (finished.isSurvey) R.string.settings_activity_survey else R.string.settings_activity_subagent) + " · " + stringResource(R.string.settings_activity_started, relativeText(finished.createdAt)),
                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        if (finished.workStatus == "failed") {
            item { TeaNodeSectionHeader(stringResource(R.string.settings_activity_why_failed)) }
            item { Text(finished.errorMessage.ifEmpty { stringResource(R.string.settings_activity_no_reason) }, Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.bodyMedium, fontFamily = androidx.compose.ui.text.font.FontFamily.Monospace) }
        } else {
            item { TeaNodeSectionHeader(stringResource(if (finished.isSurvey) R.string.settings_activity_report else R.string.settings_activity_answer)) }
            item {
                val text = finished.resultText.orEmpty()
                if (text.isNotBlank()) MarkdownView(text, Modifier.padding(horizontal = 16.dp))
                else Text(stringResource(R.string.settings_activity_no_report), Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        if (finished.runIds.isNotEmpty()) {
            item { TeaNodeSectionHeader(stringResource(R.string.settings_activity_its_runs)) }
            items(finished.runIds.size) { index -> TeaNodeRow(headline = stringResource(R.string.settings_activity_run_number, index + 1), onClick = { onOpenRun(finished.runIds[index]) }) }
        }
    }
}
