package com.teanode.app.goals

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.TrackChanges
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import com.teanode.app.R
import com.teanode.app.calendar.DateWords
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.relativeTimeText
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.calendar.ServerTime
import com.teanode.kit.goals.AgentGoal
import com.teanode.kit.goals.goals
import kotlinx.coroutines.delay
import java.time.Instant

/** How often the list reads the goals again while it is open: the agent works on its own, so they change under the person. */
private const val POLL_INTERVAL_MS = 10_000L

/**
 * GoalsSection is the dashboard's Goals tab (ios GoalsView.swift, GoalsScreen): what the agent works toward on its own,
 * those waiting for the person first, and starting a new one.
 */
@Composable
fun GoalsSection() {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val goals = rememberLoad { connection.goals() }
    // The list is read again every ten seconds without a spinner; a failed quiet read leaves what is shown.
    LaunchedEffect(goals, connection) {
        while (true) {
            delay(POLL_INTERVAL_MS)
            val fresh = runCatching { connection.goals() }.getOrNull() ?: continue
            goals.update { fresh }
        }
    }
    TeaNodeScreen(
        title = stringResource(R.string.goals_title),
        actions = {
            IconButton(onClick = { navigator.navigate(GoalRoutes.NEW) }) {
                Icon(Icons.Outlined.Add, contentDescription = stringResource(R.string.goals_new_goal))
            }
        },
    ) { contentPadding ->
        LoadedContent(
            state = goals,
            modifier = Modifier.fillMaxSize().padding(contentPadding),
            isEmpty = { list -> list.isEmpty() },
            empty = {
                EmptyState(
                    icon = Icons.Outlined.TrackChanges,
                    title = stringResource(R.string.goals_empty_title),
                    description = stringResource(R.string.goals_empty_description),
                    actionLabel = stringResource(R.string.goals_new_goal),
                    onAction = { navigator.navigate(GoalRoutes.NEW) },
                )
            },
        ) { list ->
            LazyColumn(Modifier.fillMaxSize()) {
                goalSection(R.string.goals_waiting_for_you, list.filter { goal -> goal.goalState == "waiting" }) { goal ->
                    navigator.navigate(GoalRoutes.detail(goal.conversationId))
                }
                goalSection(R.string.goals_working, list.filter { goal -> goal.goalState == "working" }) { goal ->
                    navigator.navigate(GoalRoutes.detail(goal.conversationId))
                }
                goalSection(R.string.goals_done, list.filter { goal -> !goal.isOpen }) { goal ->
                    navigator.navigate(GoalRoutes.detail(goal.conversationId))
                }
            }
        }
    }
}

private fun androidx.compose.foundation.lazy.LazyListScope.goalSection(titleResource: Int, shown: List<AgentGoal>, onOpen: (AgentGoal) -> Unit) {
    if (shown.isEmpty()) return
    item(key = "header-$titleResource") { TeaNodeSectionHeader(stringResource(titleResource)) }
    items(shown, key = { goal -> goal.conversationId }) { goal -> GoalRow(goal) { onOpen(goal) } }
}

/** GoalRow is a goal in the list: its title, what it is doing now, and when it looks next. */
@Composable
fun GoalRow(goal: AgentGoal, onClick: () -> Unit) {
    val next = if (goal.isOpen) ServerTime.instant(goal.goalNextAt) else null
    val nextText = when {
        next == null -> null
        // A check-in whose time has passed is about to run, not "next 17 hours ago".
        next.isAfter(Instant.now()) -> stringResource(R.string.goals_next_at, relativeTimeText(next.toEpochMilli()).replaceFirstChar { letter -> letter.lowercase() })
        else -> stringResource(R.string.goals_next_soon)
    }
    TeaNodeRow(
        headline = goal.goalTitle,
        supporting = listOfNotNull(goal.goalStatus.ifEmpty { null }, nextText).joinToString("\n").ifEmpty { null },
        supportingMaxLines = 3,
        onClick = onClick,
    )
}

/** The date of a goal's next check-in as the detail shows it. */
internal fun nextCheckInText(goal: AgentGoal): String? =
    if (goal.isOpen) ServerTime.instant(goal.goalNextAt)?.let { moment -> DateWords.abbreviatedDateTime(moment) } else null
