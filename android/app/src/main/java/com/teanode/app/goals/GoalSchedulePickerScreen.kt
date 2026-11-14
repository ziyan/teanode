package com.teanode.app.goals

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.goals.AgentSchedule
import com.teanode.kit.goals.agentSchedules
import com.teanode.kit.goals.goals
import com.teanode.kit.goals.moveScheduleToGoal
import kotlinx.coroutines.launch

/**
 * GoalSchedulePickerScreen lists the agent's schedules and gives one to a goal: each of its runs becomes a turn of the
 * goal, and the goal takes none of its own. A schedule that already runs a goal, this one or another, is not offered:
 * moving it would quietly take it from that goal (ios GoalsView.swift, GoalSchedulePicker).
 */
@Composable
fun GoalSchedulePickerScreen(conversationId: String) {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    var isMoving by remember { mutableStateOf(false) }
    val saidMoved = stringResource(R.string.goals_said_schedule_moved)
    val schedules = rememberLoad(conversationId) {
        val goalIds = connection.goals().map { goal -> goal.conversationId }.toSet() + conversationId
        connection.agentSchedules().filter { schedule -> schedule.conversationId == null || schedule.conversationId !in goalIds }
    }

    fun move(schedule: AgentSchedule) {
        isMoving = true
        scope.launch {
            try {
                connection.moveScheduleToGoal(schedule.id, conversationId)
                snackbar.show(saidMoved)
                navigator.back()
            } catch (failure: Exception) {
                snackbar.showError(failure)
            }
            isMoving = false
        }
    }

    TeaNodeScreen(title = stringResource(R.string.goals_picker_title), onBack = { navigator.back() }) { contentPadding ->
        LoadedContent(schedules, modifier = Modifier.fillMaxSize().padding(contentPadding)) { list ->
            LazyColumn(Modifier.fillMaxSize()) {
                if (list.isEmpty()) {
                    item {
                        Text(
                            stringResource(R.string.goals_picker_empty),
                            modifier = Modifier.padding(16.dp),
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
                items(list, key = { schedule -> schedule.id }) { schedule ->
                    TeaNodeRow(headline = schedule.name, supporting = schedule.cron, onClick = if (isMoving) null else ({ move(schedule) }))
                }
                item {
                    Text(
                        stringResource(R.string.goals_picker_footer),
                        modifier = Modifier.padding(16.dp),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}
