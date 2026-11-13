package com.teanode.app.chats

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.kit.Connection
import com.teanode.kit.chats.AgentBackgroundCommand
import com.teanode.kit.chats.AgentStatus
import com.teanode.kit.chats.ServerTime
import com.teanode.kit.chats.backgroundCommand
import com.teanode.kit.chats.backgroundCommands
import com.teanode.kit.chats.status
import com.teanode.kit.chats.stopBackgroundCommand
import com.teanode.kit.asReasonSentence
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.text.DateFormat
import java.util.Date

/** What the drawer's head shows, read every ten seconds while a conversation is on screen: the attached tab and computers, the day's budget, and the commands still running. */
class StatusModel {
    var status: AgentStatus? by mutableStateOf(null)
        private set
    var runningCommands: List<AgentBackgroundCommand> by mutableStateOf(emptyList())
        private set

    suspend fun follow(connection: Connection, environment: ChatsEnvironment, conversationId: () -> String?) {
        while (true) {
            runCatching { connection.status() }.getOrNull()?.let { read ->
                status = read
                read.budget?.currency?.takeIf { code -> code.isNotEmpty() }?.let { code -> environment.currencyCode = code }
            }
            runCatching { connection.backgroundCommands(conversationId()) }.getOrNull()?.let { commands -> runningCommands = commands.filter { command -> command.isRunning } }
            delay(10_000)
        }
    }

    fun showDemo(read: AgentStatus, commands: List<AgentBackgroundCommand>) {
        status = read
        runningCommands = commands
    }
}

/** The day's use against its limit, what it cost, and when the budget starts again. */
@Composable
fun BudgetSummary(budget: AgentStatus.Budget) {
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        val fraction = budget.fraction
        if (fraction != null) {
            LinearProgressIndicator(
                progress = { minOf(1f, fraction.toFloat()) }, modifier = Modifier.fillMaxWidth(),
                color = when {
                    fraction >= 0.85 -> MaterialTheme.colorScheme.error
                    fraction >= 0.6 -> Color(0xFFF59E0B)
                    else -> MaterialTheme.colorScheme.primary
                },
            )
            if (budget.limit > 0) {
                Text(stringResource(R.string.chats_budget_of, UsageWords.count(budget.used.toInt()), UsageWords.count(budget.limit.toInt())))
            } else {
                Text(stringResource(R.string.chats_budget_of, UsageWords.money(budget.cost, budget.currency ?: "USD"), UsageWords.money(budget.costLimit, budget.currency ?: "USD")))
            }
        } else {
            Text(stringResource(R.string.chats_no_limit_today))
        }
        Text(stringResource(R.string.chats_spent_today, UsageWords.money(budget.cost, budget.currency ?: "USD")), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        ServerTime.instant(budget.resetsAt)?.let { resetsAt ->
            Text(
                stringResource(R.string.chats_starts_again, DateFormat.getTimeInstance(DateFormat.SHORT).format(Date.from(resetsAt))),
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

/** The head's dropdowns in one place: the day's usage, the switches for what a conversation shows, what is attached, and the commands left running. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun StatusSheet(status: StatusModel, environment: ChatsEnvironment, connection: Connection, onDismiss: () -> Unit) {
    var openCommand by remember { mutableStateOf<AgentBackgroundCommand?>(null) }
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        val command = openCommand
        if (command != null) {
            BackgroundCommandView(command, connection, onBack = { openCommand = null })
            return@ModalBottomSheet
        }
        Column(Modifier.navigationBarsPadding().verticalScroll(rememberScrollState()).padding(horizontal = 20.dp).padding(bottom = 16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(stringResource(R.string.chats_usage_today), style = MaterialTheme.typography.titleLarge)
            val budget = status.status?.budget
            if (budget != null) BudgetSummary(budget) else CircularProgressIndicator(Modifier.padding(8.dp))
            SwitchLine(stringResource(R.string.chats_show_tool_calls), environment.isShowingToolCalls) { isOn -> environment.chooseShowingToolCalls(isOn) }
            SwitchLine(stringResource(R.string.chats_show_usage), environment.isShowingUsage) { isOn -> environment.chooseShowingUsage(isOn) }
            SwitchLine(stringResource(R.string.chats_show_working_notes), environment.isShowingWorkingNotes) { isOn -> environment.chooseShowingWorkingNotes(isOn) }
            status.status?.tab?.takeIf { tab -> tab.attached }?.let { tab ->
                HorizontalDivider()
                Text(stringResource(R.string.chats_attached_tab), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
                Row(Modifier.fillMaxWidth()) {
                    Text(tab.title.orEmpty(), Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
                    Text(runCatching { java.net.URI(tab.url.orEmpty()).host }.getOrNull().orEmpty(), color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
            val computers = status.status?.computers.orEmpty()
            if (computers.isNotEmpty()) {
                HorizontalDivider()
                Text(stringResource(R.string.chats_attached_computers), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
                for (computer in computers) {
                    Column {
                        Text(computer.name)
                        Text(listOfNotNull(computer.description, computer.system).filter { part -> part.isNotEmpty() }.joinToString(" · "), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
            if (status.runningCommands.isNotEmpty()) {
                HorizontalDivider()
                Text(stringResource(R.string.chats_background_commands), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
                for (running in status.runningCommands) {
                    Text(
                        running.command, fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.bodySmall, maxLines = 1, overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp).clickable { openCommand = running }.padding(vertical = 14.dp),
                    )
                }
            }
        }
    }
}

@Composable
private fun SwitchLine(title: String, isOn: Boolean, onChange: (Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth().heightIn(min = 48.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(title, Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
        Switch(checked = isOn, onCheckedChange = onChange)
    }
}

/** One command left running: where it runs, what it printed, read again every two seconds while it runs, and a way to stop it. */
@Composable
fun BackgroundCommandView(initial: AgentBackgroundCommand, connection: Connection, onBack: () -> Unit) {
    var command by remember { mutableStateOf(initial) }
    var isStopping by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    LaunchedEffect(initial.id) {
        while (true) {
            runCatching { connection.backgroundCommand(command.computer, command.id) }.getOrNull()?.let { read -> command = read }
            if (!command.isRunning) return@LaunchedEffect
            delay(2_000)
        }
    }
    Column(Modifier.navigationBarsPadding().verticalScroll(rememberScrollState()).padding(horizontal = 20.dp).padding(bottom = 16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            TextButton(onClick = onBack) { Text(stringResource(R.string.chats_back)) }
            Text(stringResource(R.string.chats_background_command), style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
            if (command.isRunning) {
                TextButton(
                    onClick = {
                        isStopping = true
                        scope.launch {
                            runCatching { connection.stopBackgroundCommand(command.computer, command.id) }.onFailure { failure -> snackbar.showError(failure) }
                            isStopping = false
                        }
                    },
                    enabled = !isStopping,
                ) { Text(stringResource(R.string.chats_stop), color = MaterialTheme.colorScheme.error) }
            }
        }
        LabeledLine(stringResource(R.string.chats_computer), command.computer)
        command.directory?.takeIf { directory -> directory.isNotEmpty() }?.let { directory -> LabeledLine(stringResource(R.string.chats_folder), directory) }
        Text(command.command, fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.bodySmall)
        Text(
            when {
                command.isRunning -> stringResource(R.string.chats_running)
                command.exitCode != null -> stringResource(R.string.chats_exited, command.exitCode!!)
                else -> command.stopReason?.asReasonSentence() ?: stringResource(R.string.chats_stopped)
            },
            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(stringResource(R.string.chats_output), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
        CodeBlock(if (command.stdout.isNullOrEmpty()) stringResource(R.string.chats_nothing_printed) else command.stdout!!)
        command.stderr?.takeIf { text -> text.isNotEmpty() }?.let { errors ->
            Text(stringResource(R.string.chats_errors), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
            CodeBlock(errors, maxHeightDp = 300)
        }
    }
}

@Composable
private fun LabeledLine(label: String, text: String) {
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
        Text(label, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(text, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.padding(start = 16.dp))
    }
}
