package com.teanode.app.coding

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AccountTree
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material.icons.outlined.HelpOutline
import androidx.compose.material.icons.outlined.NightsStay
import androidx.compose.material.icons.outlined.StopCircle
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.LoadingState
import com.teanode.kit.coding.HerdrSession
import com.teanode.kit.coding.HerdrSubagent
import com.teanode.kit.coding.HerdrSubagentNode
import com.teanode.kit.coding.HerdrSubagents

/**
 * The Agents part of a coding session: the session on top and the agents it started for its current request under it,
 * each under the agent that started it. The question a session waits on is answered in the Turns part.
 */
@Composable
internal fun CodingAgentsList(session: HerdrSession, read: HerdrSubagents?, isRead: Boolean) {
    var isSessionExpanded by remember { mutableStateOf(true) }
    val roots = read?.tree ?: emptyList()
    Box(Modifier.fillMaxSize()) {
        LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
            if (read != null && read.subagents.isNotEmpty()) {
                item {
                    val count = read.subagents.size
                    val agentsText = pluralStringResource(R.plurals.coding_agents_count, count, count)
                    val heading = if (read.runningCount == 0) agentsText else agentsText + " · " + stringResource(R.string.coding_agents_running_count, read.runningCount)
                    Text(heading, style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary, modifier = Modifier.padding(16.dp, 16.dp, 16.dp, 8.dp))
                }
            }
            item {
                Row(
                    Modifier.fillMaxWidth().then(if (roots.isNotEmpty()) Modifier.clickable { isSessionExpanded = !isSessionExpanded } else Modifier).padding(16.dp, 8.dp),
                    verticalAlignment = Alignment.Top,
                ) {
                    Box(Modifier.weight(1f)) { CodingSessionAgentRow(session, read?.sessionModelName ?: "") }
                    if (roots.isNotEmpty()) Icon(if (isSessionExpanded) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore, null)
                }
            }
            if (isSessionExpanded) roots.forEach { node -> item(key = "node-${node.id}") { CodingAgentNodeView(node, depth = 1) } }
            item {
                val footer = when {
                    session.herdrSessionState == "asking" -> stringResource(R.string.coding_agents_asking_footer)
                    read?.isTruncated == true -> stringResource(R.string.coding_agents_truncated_footer)
                    else -> null
                }
                if (footer != null) Text(footer, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(16.dp))
            }
            if (read != null && read.subagents.isEmpty() && isRead) {
                item { Box(Modifier.fillMaxWidth().padding(vertical = 24.dp)) { EmptyState(Icons.Outlined.AccountTree, stringResource(R.string.coding_agents_none_title), Modifier, stringResource(R.string.coding_agents_none_description)) } }
            } else if (read == null && isRead) {
                // Why is in the message shown with it: an older program on the computer, or the computer gone, says so in its own words.
                item { Box(Modifier.fillMaxWidth().padding(vertical = 24.dp)) { EmptyState(Icons.Outlined.ErrorOutline, stringResource(R.string.coding_agents_failed)) } }
            }
        }
        if (!isRead) LoadingState()
    }
}

/** An agent and, under it, the agents it started: a group open at first when it has any, a plain row when not. */
@Composable
private fun CodingAgentNodeView(node: HerdrSubagentNode, depth: Int) {
    var isExpanded by remember { mutableStateOf(true) }
    Column {
        Row(
            Modifier.fillMaxWidth().then(if (node.children.isNotEmpty()) Modifier.clickable { isExpanded = !isExpanded } else Modifier).padding(start = (16 + depth * 20).dp, end = 16.dp, top = 8.dp, bottom = 8.dp),
            verticalAlignment = Alignment.Top,
        ) {
            Box(Modifier.weight(1f)) { CodingAgentRow(node.subagent) }
            if (node.children.isNotEmpty()) Icon(if (isExpanded) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore, null)
        }
        if (isExpanded) node.children.forEach { child -> CodingAgentNodeView(child, depth + 1) }
    }
}

/** The session itself at the top of the tree: what it is about, which coding agent and model it runs, and whether it works, is idle or needs the person. */
@Composable
private fun CodingSessionAgentRow(session: HerdrSession, modelName: String) {
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(session.rowTitle, style = MaterialTheme.typography.titleSmall, maxLines = 2)
        Text(listOf(session.codingAgentName, modelName).filter { part -> part.isNotEmpty() }.joinToString(" · "), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        when {
            session.herdrSessionState == "asking" -> StateLabel(stringResource(R.string.coding_needs_you), askingColor, Icons.Outlined.HelpOutline)
            session.isWorking -> StateLabel(stringResource(R.string.coding_state_working), MaterialTheme.colorScheme.primary, null, isBusy = true)
            else -> StateLabel(stringResource(R.string.coding_state_idle), MaterialTheme.colorScheme.onSurfaceVariant, Icons.Outlined.NightsStay)
        }
    }
}

/** One agent: its kind and model, its task, whether it runs, is done or was stopped and for how long, and the tools it used. */
@Composable
private fun CodingAgentRow(subagent: HerdrSubagent) {
    val detail = listOf(subagent.subagentName ?: "", subagent.modelName).filter { part -> part.isNotEmpty() }.joinToString(" · ")
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(subagent.subagentType, style = MaterialTheme.typography.titleSmall, maxLines = 2)
        if (detail.isNotEmpty()) Text(detail, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        if (subagent.taskText.isNotEmpty()) Text(subagent.taskText, style = MaterialTheme.typography.bodyMedium, maxLines = 2)
        Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
            when {
                subagent.isRunning -> StateLabel(stringResource(R.string.coding_agent_running), MaterialTheme.colorScheme.primary, null, isBusy = true)
                subagent.isStopped -> StateLabel(stringResource(R.string.coding_agent_stopped), MaterialTheme.colorScheme.onSurfaceVariant, Icons.Outlined.StopCircle)
                else -> StateLabel(stringResource(R.string.coding_agent_done), doneColor, Icons.Outlined.CheckCircle)
            }
            durationText(subagent.durationSeconds(System.currentTimeMillis()))?.let { text ->
                Text(text, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        if (subagent.toolNames.isNotEmpty()) {
            Text(stringResource(R.string.coding_agent_used_tools, subagent.toolNames.joinToString(", ")), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2)
        }
    }
}

private val doneColor = Color(0xFF2E7D32)

@Composable
private fun StateLabel(text: String, color: Color, icon: androidx.compose.ui.graphics.vector.ImageVector?, isBusy: Boolean = false) {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
        if (isBusy) CircularProgressIndicator(Modifier.size(12.dp), strokeWidth = 1.5.dp, color = color) else if (icon != null) Icon(icon, null, tint = color, modifier = Modifier.size(14.dp))
        Text(text, style = MaterialTheme.typography.labelMedium.copy(fontWeight = FontWeight.SemiBold), color = color)
    }
}

/** "2m 14s": the two largest units of the time it ran, null when it has no start time. */
internal fun durationText(seconds: Double?): String? {
    if (seconds == null) return null
    val total = seconds.toLong()
    val hours = total / 3600
    val minutes = total % 3600 / 60
    val rest = total % 60
    val units = listOf(hours to "h", minutes to "m", rest to "s")
    val firstUsed = units.indexOfFirst { unit -> unit.first > 0 }
    if (firstUsed < 0) return "0s"
    return units.drop(firstUsed).take(2).joinToString(" ") { unit -> "${unit.first}${unit.second}" }
}
