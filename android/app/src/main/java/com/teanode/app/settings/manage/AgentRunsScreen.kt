package com.teanode.app.settings.manage

import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.FilterList
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.settings.ChoiceRow
import com.teanode.app.settings.FooterText
import com.teanode.app.settings.FormField
import com.teanode.app.settings.SettingsPage
import com.teanode.app.settings.rememberActionRunner
import com.teanode.app.settings.shortDateTime
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.manage.AgentAdminOverview
import com.teanode.kit.manage.AgentDeadJob
import com.teanode.kit.manage.AgentUsageGrouping
import com.teanode.kit.manage.AgentFailedTurn
import com.teanode.kit.manage.OversightRange
import com.teanode.kit.manage.OversightTab
import com.teanode.kit.manage.ServerAgentRun
import com.teanode.kit.manage.ServerAgentRunFilter
import com.teanode.kit.manage.agentAdminOverview
import com.teanode.kit.manage.deadJobConversationId
import com.teanode.kit.manage.retryAgentJob
import com.teanode.kit.manage.failedAgentTurns
import com.teanode.kit.manage.serverAgentRuns
import com.teanode.kit.manage.usageBounds
import com.teanode.kit.photos.ServerTime
import kotlinx.coroutines.delay
import java.time.Instant

@Composable
private fun runKindName(jobKind: String): String = runKindNameResource(jobKind)?.let { resource -> stringResource(resource) } ?: jobKind

/**
 * Oversight: every person's agent conversations and runs, read-only. Three parts: Running (a turn in flight now), Recent (by last activity,
 * within a range) and Failed (turns that ended in an error). Each shows its owner and is read a server page at a time. [onOpenConversation]
 * opens a conversation's transcript, which says it is observed and has no composer. The jobs given up on stay on the Failed part, where each
 * opens what it tried or is queued again.
 */
@Composable
fun AgentRunsAdminScreen(onBack: () -> Unit, onOpenConversation: (String) -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val snackbar = LocalSnackbar.current
    val resources = LocalContext.current.resources
    // The jobs given up on and the agents' names come from the audit overview, which an observer without agent:audit may not read: then they are left out.
    var overview by remember { mutableStateOf<AgentAdminOverview?>(null) }
    var overviewReadCount by remember { mutableStateOf(0) }
    var tab by rememberSaveable { mutableStateOf(OversightTab.Recent) }
    var range by rememberSaveable { mutableStateOf(OversightRange.Week) }
    var filter by remember { mutableStateOf(ServerAgentRunFilter()) }
    var typedQuery by remember { mutableStateOf("") }
    var runs by remember { mutableStateOf<List<ServerAgentRun>?>(null) }
    var failedTurns by remember { mutableStateOf<List<AgentFailedTurn>?>(null) }
    var matchCount by remember { mutableStateOf(0) }
    var isLoadingMore by remember { mutableStateOf(false) }
    val now = remember { Instant.now() }
    LaunchedEffect(overviewReadCount) {
        try {
            val bounds = usageBounds(30)
            overview = connection.agentAdminOverview(AgentUsageGrouping.Day, bounds.first, bounds.second)
        } catch (cancelled: kotlinx.coroutines.CancellationException) {
            throw cancelled
        } catch (failure: Throwable) {
            overview = null
        }
    }
    // Typing narrows the list as it goes, once the typing pauses.
    LaunchedEffect(typedQuery) {
        if (typedQuery != filter.query) {
            delay(300)
            filter = filter.copy(query = typedQuery)
        }
    }
    // What the server is asked for: the Running part ignores the range, a job's own runs ignore it too, and Failed reads another list.
    val sinceText = if (filter.jobId == null) range.since(now) else null
    val request = remember(tab, range, filter) { Triple(tab, filter, sinceText) }
    LaunchedEffect(request) {
        runs = null
        failedTurns = null
        try {
            if (tab == OversightTab.Failed) {
                val page = connection.failedAgentTurns(since = range.since(now), agentId = filter.agentId, first = 50)
                failedTurns = page.failedTurns
                matchCount = page.totalCount
            } else {
                val page = connection.serverAgentRuns(filter.copy(since = if (tab == OversightTab.Recent) sinceText else null, isRunningOnly = tab == OversightTab.Running), 50)
                runs = page.runs
                matchCount = page.matchCount
            }
        } catch (cancelled: kotlinx.coroutines.CancellationException) {
            throw cancelled
        } catch (failure: Throwable) {
            snackbar.showError(failure)
            if (tab == OversightTab.Failed) failedTurns = emptyList() else runs = emptyList()
        }
    }
    SettingsPage(stringResource(R.string.manage_oversight), onBack) {
        item {
            val parts = listOf(OversightTab.Running to R.string.manage_oversight_running, OversightTab.Recent to R.string.manage_oversight_recent, OversightTab.Failed to R.string.manage_oversight_failed)
            SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
                parts.forEachIndexed { index, (part, label) ->
                    SegmentedButton(tab == part, { tab = part }, SegmentedButtonDefaults.itemShape(index, parts.size)) { Text(stringResource(label), maxLines = 1) }
                }
            }
            if (tab != OversightTab.Running) {
                ChoiceRow(
                    stringResource(R.string.manage_oversight_range), OversightRange.entries, range,
                    label = { choice -> resources.getString(oversightRangeResource(choice)) }, onSelect = { choice -> range = choice },
                )
            }
            val found = overview
            if (tab != OversightTab.Failed) FormField(typedQuery, { typed -> typedQuery = typed }, stringResource(R.string.manage_runs_what))
            if (found != null) {
                ChoiceRow(
                    stringResource(R.string.manage_runs_whose), listOf<String?>(null) + found.agents.map { agent -> agent.agentId }, filter.agentId,
                    label = { agentId -> if (agentId == null) resources.getString(R.string.manage_everyone) else found.username(agentId) }, onSelect = { agentId -> filter = filter.copy(agentId = agentId, jobId = null) },
                )
            }
            if (tab != OversightTab.Failed) {
                ChoiceRow(
                    stringResource(R.string.manage_runs_kind), listOf<String?>(null) + OVERSIGHT_KINDS, filter.jobKind,
                    label = { kind -> if (kind == null) resources.getString(R.string.manage_any_kind) else runKindNameResource(kind)?.let { resource -> resources.getString(resource) } ?: kind }, onSelect = { kind -> filter = filter.copy(jobKind = kind, jobId = null) },
                )
            }
            if (filter.jobId != null) TeaNodeRow(headline = stringResource(R.string.manage_all_runs), leadingIcon = Icons.Outlined.FilterList, onClick = { filter = filter.copy(jobId = null) })
        }
        val deadJobs = overview?.deadJobs.orEmpty()
        if (tab == OversightTab.Failed && deadJobs.isNotEmpty()) {
            item { TeaNodeSectionHeader(stringResource(R.string.manage_jobs_given_up)) }
            items(deadJobs.size) { index ->
                val job = deadJobs[index]
                DeadJobRow(
                    job, overview?.username(job.agentId) ?: job.agentId,
                    onOpen = {
                        actions.run {
                            val conversationId = connection.deadJobConversationId(job)
                            if (conversationId != null) onOpenConversation(conversationId) else snackbar.showProblem(resources.getString(R.string.manage_job_never_ran))
                        }
                    },
                    onRuns = { tab = OversightTab.Recent; filter = ServerAgentRunFilter(jobId = job.id) },
                    onRetry = {
                        actions.run(resources.getString(R.string.manage_queued_again), onSuccess = { overviewReadCount += 1 }) { connection.retryAgentJob(job.id) }
                    },
                )
            }
            item { FooterText(stringResource(R.string.manage_jobs_footer)) }
        }
        item {
            val countText = when {
                tab == OversightTab.Failed -> if (matchCount == 1) stringResource(R.string.manage_oversight_failed_one) else stringResource(R.string.manage_oversight_failed_count, matchCount)
                matchCount == 1 -> stringResource(R.string.manage_one_run)
                else -> stringResource(R.string.manage_runs_count, matchCount)
            }
            TeaNodeSectionHeader(countText)
        }
        if (tab == OversightTab.Failed) {
            val shownTurns = failedTurns
            if (shownTurns != null && shownTurns.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.manage_oversight_nothing_failed)) }
            items(shownTurns?.size ?: 0) { index ->
                val turn = shownTurns!![index]
                TeaNodeRow(
                    headline = turn.conversationTitle?.takeIf { title -> title.isNotEmpty() } ?: runKindName(turn.conversationKind), supportingMaxLines = 3,
                    overline = turn.ownerLabel.ifEmpty { null },
                    supporting = listOfNotNull(ServerTime.instant(turn.failedAt)?.let { instant -> shortDateTime(instant) }, turn.errorText.ifEmpty { null }).joinToString(" · "),
                    onClick = { onOpenConversation(turn.conversationId) },
                )
                if (index == shownTurns.lastIndex && shownTurns.size < matchCount && !isLoadingMore) {
                    LaunchedEffect(shownTurns.size, request) {
                        isLoadingMore = true
                        try {
                            val page = connection.failedAgentTurns(since = range.since(now), agentId = filter.agentId, first = 50, offset = shownTurns.size)
                            failedTurns = shownTurns + page.failedTurns
                            matchCount = page.totalCount
                        } catch (cancelled: kotlinx.coroutines.CancellationException) {
                            throw cancelled
                        } catch (failure: Throwable) {
                            snackbar.showError(failure)
                        } finally {
                            isLoadingMore = false
                        }
                    }
                }
            }
        } else {
            val shownRuns = runs
            if (shownRuns != null && shownRuns.isEmpty()) item { TeaNodeRow(headline = stringResource(if (tab == OversightTab.Running) R.string.manage_oversight_nothing_running else R.string.manage_nothing_yet)) }
            items(shownRuns?.size ?: 0) { index ->
                val run = shownRuns!![index]
                TeaNodeRow(
                    headline = run.title.ifEmpty { run.jobKind?.let { kind -> runKindName(kind) }.orEmpty() }, supportingMaxLines = 2,
                    overline = listOfNotNull(run.ownerLabel.ifEmpty { null }, run.jobKind?.takeIf { kind -> kind.isNotEmpty() }?.let { kind -> runKindName(kind) }).joinToString(" · ").ifEmpty { null },
                    supporting = listOfNotNull(
                        if (run.isRunning) stringResource(R.string.manage_oversight_running_now) else null,
                        ServerTime.instant(run.lastAt)?.let { instant -> shortDateTime(instant) }, compactCount(run.tokenCount.toLong()),
                        if (run.usage.cost > 0) moneyText(run.usage.cost, null) else null,
                    ).joinToString(" · "),
                    onClick = { onOpenConversation(run.id) },
                )
                if (index == shownRuns.lastIndex && shownRuns.size < matchCount && !isLoadingMore) {
                    LaunchedEffect(shownRuns.size, request) {
                        isLoadingMore = true
                        try {
                            val page = connection.serverAgentRuns(filter.copy(since = if (tab == OversightTab.Recent) sinceText else null, isRunningOnly = tab == OversightTab.Running), 50, shownRuns.size)
                            val knownIds = shownRuns.map { known -> known.id }.toSet()
                            runs = shownRuns + page.runs.filter { added -> !knownIds.contains(added.id) }
                            matchCount = page.matchCount
                        } catch (cancelled: kotlinx.coroutines.CancellationException) {
                            throw cancelled
                        } catch (failure: Throwable) {
                            snackbar.showError(failure)
                        } finally {
                            isLoadingMore = false
                        }
                    }
                }
            }
        }
        item {
            FooterText(
                stringResource(
                    when {
                        tab == OversightTab.Failed -> R.string.manage_oversight_failed_footer
                        filter.jobId != null -> R.string.manage_runs_job_footer
                        tab == OversightTab.Running -> R.string.manage_oversight_running_footer
                        else -> R.string.manage_oversight_footer
                    },
                ),
            )
        }
    }
}

private fun oversightRangeResource(range: OversightRange): Int = when (range) {
    OversightRange.Day -> R.string.manage_oversight_range_day
    OversightRange.Week -> R.string.manage_oversight_range_week
    OversightRange.Month -> R.string.manage_oversight_range_month
}

@Composable
private fun DeadJobRow(job: AgentDeadJob, username: String, onOpen: () -> Unit, onRuns: () -> Unit, onRetry: () -> Unit) {
    val finished = ServerTime.instant(job.finishedAt)?.let { instant -> shortDateTime(instant) }.orEmpty()
    TeaNodeRow(
        headline = stringResource(R.string.manage_job_for, job.kind, username),
        supporting = listOfNotNull(if (finished.isEmpty()) stringResource(R.string.settings_usage_attempts, job.attempts) else stringResource(R.string.manage_job_after_attempts, job.attempts, finished), job.error.ifEmpty { null }).joinToString(" · "), supportingMaxLines = 4,
        onClick = onOpen,
        trailing = {
            androidx.compose.foundation.layout.Row {
                TextButton(onClick = onRuns) { Text(stringResource(R.string.manage_runs)) }
                TextButton(onClick = onRetry) { Text(stringResource(R.string.manage_retry)) }
            }
        },
    )
}
