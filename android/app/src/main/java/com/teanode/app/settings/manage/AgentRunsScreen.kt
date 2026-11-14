package com.teanode.app.settings.manage

import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.FilterList
import androidx.compose.material.icons.outlined.ViewList
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
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
import com.teanode.app.settings.LoadedSettingsPage
import com.teanode.app.settings.rememberActionRunner
import com.teanode.app.settings.shortDateTime
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.manage.AgentAdminOverview
import com.teanode.kit.manage.AgentDeadJob
import com.teanode.kit.manage.AgentUsageGrouping
import com.teanode.kit.manage.ServerAgentRun
import com.teanode.kit.manage.ServerAgentRunFilter
import com.teanode.kit.manage.agentAdminOverview
import com.teanode.kit.manage.deadJobConversationId
import com.teanode.kit.manage.retryAgentJob
import com.teanode.kit.manage.serverAgentRuns
import com.teanode.kit.manage.usageBounds
import com.teanode.kit.photos.ServerTime
import kotlinx.coroutines.delay

@Composable
private fun runKindName(jobKind: String): String = runKindNameResource(jobKind)?.let { resource -> stringResource(resource) } ?: jobKind

/**
 * Every call any agent on the server made to a model, newest first, a page at a time, and the jobs given up on: each opens what it tried
 * or is queued again. [onOpenConversation] opens a run's transcript, which is a conversation of its own.
 */
@Composable
fun AgentRunsAdminScreen(onBack: () -> Unit, onOpenConversation: (String) -> Unit) {
    val connection = LocalConnection.current
    val actions = rememberActionRunner()
    val snackbar = LocalSnackbar.current
    val resources = LocalContext.current.resources
    val overview = rememberLoad { val bounds = usageBounds(30); connection.agentAdminOverview(AgentUsageGrouping.Day, bounds.first, bounds.second) }
    var filter by remember { mutableStateOf(ServerAgentRunFilter()) }
    var typedQuery by remember { mutableStateOf("") }
    var runs by remember { mutableStateOf<List<ServerAgentRun>?>(null) }
    var runTotal by remember { mutableStateOf(0) }
    var isLoadingMore by remember { mutableStateOf(false) }
    var isFiltering by remember { mutableStateOf(false) }
    // Typing narrows the runs as it goes, once the typing pauses.
    LaunchedEffect(typedQuery) {
        if (typedQuery != filter.query) {
            delay(300)
            filter = filter.copy(query = typedQuery)
        }
    }
    LaunchedEffect(filter) {
        try {
            val page = connection.serverAgentRuns(filter, 50)
            runs = page.runs
            runTotal = page.total
        } catch (cancelled: kotlinx.coroutines.CancellationException) {
            throw cancelled
        } catch (failure: Throwable) {
            snackbar.showError(failure)
            if (runs == null) runs = emptyList()
        }
    }
    val listState = rememberLazyListState()
    LoadedSettingsPage(stringResource(R.string.manage_runs), onBack, overview) { found: AgentAdminOverview ->
        item {
            TeaNodeSectionHeader(stringResource(R.string.manage_jobs_given_up))
            if (found.deadJobs.isEmpty()) TeaNodeRow(headline = stringResource(R.string.manage_none))
        }
        items(found.deadJobs.size) { index ->
            val job = found.deadJobs[index]
            DeadJobRow(
                job, found.username(job.agentId),
                onOpen = {
                    actions.run {
                        val conversationId = connection.deadJobConversationId(job)
                        if (conversationId != null) onOpenConversation(conversationId) else snackbar.showProblem(resources.getString(R.string.manage_job_never_ran))
                    }
                },
                onRuns = { filter = ServerAgentRunFilter(jobId = job.id) },
                onRetry = { actions.run(resources.getString(R.string.manage_queued_again), onSuccess = { overview.refresh() }) { connection.retryAgentJob(job.id) } },
            )
        }
        if (found.deadJobs.isNotEmpty()) item { FooterText(stringResource(R.string.manage_jobs_footer)) }
        item {
            TeaNodeSectionHeader(if (runTotal == 1) stringResource(R.string.manage_one_run) else stringResource(R.string.manage_runs_count, runTotal))
            FormField(typedQuery, { typed -> typedQuery = typed }, stringResource(R.string.manage_runs_what))
            ChoiceRow(
                stringResource(R.string.manage_runs_whose), listOf<String?>(null) + found.agents.map { agent -> agent.agentId }, filter.agentId,
                label = { agentId -> if (agentId == null) resources.getString(R.string.manage_everyone) else found.username(agentId) }, onSelect = { agentId -> filter = filter.copy(agentId = agentId, jobId = null) },
            )
            ChoiceRow(
                stringResource(R.string.manage_runs_kind), listOf<String?>(null) + RUN_JOB_KINDS, filter.jobKind,
                label = { kind -> if (kind == null) resources.getString(R.string.manage_any_kind) else runKindNameResource(kind)?.let { resource -> resources.getString(resource) } ?: kind }, onSelect = { kind -> filter = filter.copy(jobKind = kind, jobId = null) },
            )
            if (filter.jobId != null) TeaNodeRow(headline = stringResource(R.string.manage_all_runs), leadingIcon = Icons.Outlined.FilterList, onClick = { filter = filter.copy(jobId = null) })
        }
        val shownRuns = runs
        if (shownRuns != null && shownRuns.isEmpty()) item { TeaNodeRow(headline = stringResource(R.string.manage_nothing_yet)) }
        items(shownRuns?.size ?: 0) { index ->
            val run = shownRuns!![index]
            TeaNodeRow(
                headline = run.title, supportingMaxLines = 2, overline = run.jobKind?.takeIf { kind -> kind.isNotEmpty() }?.let { kind -> runKindName(kind) },
                supporting = listOfNotNull(
                    found.username(run.agentId), ServerTime.instant(run.lastAt)?.let { instant -> shortDateTime(instant) }, compactCount(run.tokenCount.toLong()),
                    if (run.usage.cost > 0) moneyText(run.usage.cost, null) else null,
                ).joinToString(" · "),
                onClick = { onOpenConversation(run.id) },
            )
            if (index == shownRuns.lastIndex && shownRuns.size < runTotal && !isLoadingMore) {
                LaunchedEffect(shownRuns.size, filter) {
                    isLoadingMore = true
                    try {
                        val page = connection.serverAgentRuns(filter, 50, shownRuns.size)
                        val knownIds = shownRuns.map { known -> known.id }.toSet()
                        runs = shownRuns + page.runs.filter { added -> !knownIds.contains(added.id) }
                        runTotal = page.total
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
        item {
            FooterText(stringResource(if (filter.jobId != null) R.string.manage_runs_job_footer else R.string.manage_runs_footer))
        }
    }
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
