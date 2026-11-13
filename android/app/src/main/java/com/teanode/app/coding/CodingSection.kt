package com.teanode.app.coding

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.CloudOff
import androidx.compose.material.icons.outlined.Computer
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.Link
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.QuestionMark
import androidx.compose.material.icons.outlined.Sync
import androidx.compose.material.icons.outlined.TextSnippet
import androidx.compose.material.icons.outlined.Terminal
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.coding.CodingComputer
import com.teanode.kit.coding.CodingWorkspace
import com.teanode.kit.coding.HerdrSession
import com.teanode.kit.coding.captureCodingSession
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/**
 * The first screen of Coding: the coding sessions on the person's computers, by computer and workspace, each opening to
 * read and type into.
 */
@Composable
fun CodingSection() {
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val model = remember(connection) { CodingModel.of(connection) }
    val scope = rememberCoroutineScope()
    val lifecycleOwner = LocalLifecycleOwner.current
    val context = LocalContext.current
    val list = model.list
    val failure = model.loadFailure

    // Read while this list is what is on screen and the app is in front; a demo reads once.
    LaunchedEffect(model) {
        lifecycleOwner.repeatOnLifecycle(Lifecycle.State.STARTED) {
            while (true) {
                model.load()
                if (model.computerNames.isNotEmpty()) session.noteCodingComputers()
                session.setBadgeCount(com.teanode.kit.AppSection.Coding, model.list?.sessions?.count { entry -> entry.isAsking } ?: 0)
                if (session.isDemo) break
                delay(LIST_POLL_MS)
            }
        }
    }

    TeaNodeScreen(
        title = stringResource(R.string.coding_title),
        actions = {
            IconButton(onClick = { navigator.navigate("coding/open") }, enabled = model.computerNames.isNotEmpty()) {
                Icon(Icons.Outlined.Add, contentDescription = stringResource(R.string.coding_open_session))
            }
            CodingMenu(model.computerNames, onReadNewSessions = { computer, assistant, assistantName ->
                scope.launch {
                    try {
                        val isAsked = connection.captureCodingSession(computer, assistant)
                        if (isAsked) {
                            snackbar.show(context.getString(R.string.coding_reading_new_sessions, assistantName, computer))
                        } else {
                            snackbar.showProblem(context.getString(R.string.coding_no_source_reads, assistantName, computer))
                        }
                    } catch (error: Throwable) {
                        snackbar.showError(error)
                    }
                }
            })
        },
    ) { contentPadding ->
        Column(Modifier.fillMaxSize().padding(contentPadding)) {
            when {
                list != null && list.sessions.isEmpty() -> EmptyState(
                    Icons.Outlined.Terminal, stringResource(R.string.coding_empty_title),
                    description = stringResource(R.string.coding_empty_description),
                )
                list != null -> CodingList(model.computers, list.failedComputerNames, onOpen = { entry -> navigator.navigate(codingSessionRoute(entry.computer, entry.paneId)) })
                failure != null -> ErrorState(session.describeLoadProblem(failure), onRetry = { scope.launch { model.load() } })
                else -> LoadingState()
            }
        }
    }
}

private const val LIST_POLL_MS = 10_000L

@Composable
private fun CodingMenu(computerNames: List<String>, onReadNewSessions: (computer: String, assistant: String, assistantName: String) -> Unit) {
    val navigator = LocalAppNavigator.current
    var isOpen by remember { mutableStateOf(false) }
    var readingComputer by remember { mutableStateOf<String?>(null) }
    IconButton(onClick = { isOpen = true }) {
        Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.coding_actions))
    }
    DropdownMenu(expanded = isOpen, onDismissRequest = { isOpen = false; readingComputer = null }) {
        val chosenComputer = readingComputer
        if (chosenComputer == null) {
            DropdownMenuItem(
                text = { Text(stringResource(R.string.coding_set_up_hooks)) }, leadingIcon = { Icon(Icons.Outlined.Link, null) },
                onClick = { isOpen = false; navigator.navigate("coding/hooks") },
            )
            DropdownMenuItem(
                text = { Text(stringResource(R.string.coding_what_a_session_sees)) }, leadingIcon = { Icon(Icons.Outlined.TextSnippet, null) },
                onClick = { isOpen = false; navigator.navigate("coding/preview") },
            )
            // The computers are named, then the coding agent to read: two steps, as the iPhone's nested menus are.
            computerNames.forEach { computer ->
                DropdownMenuItem(
                    text = { Text(stringResource(R.string.coding_read_new_sessions) + ": " + computer) }, leadingIcon = { Icon(Icons.Outlined.Sync, null) },
                    onClick = { readingComputer = computer },
                )
            }
        } else {
            DropdownMenuItem(text = { Text("Claude Code") }, onClick = { isOpen = false; readingComputer = null; onReadNewSessions(chosenComputer, "claude-code", "Claude Code") })
            DropdownMenuItem(text = { Text("Codex") }, onClick = { isOpen = false; readingComputer = null; onReadNewSessions(chosenComputer, "codex", "Codex") })
        }
    }
}

@Composable
private fun CodingList(computers: List<CodingComputer>, failedComputerNames: List<String>, onOpen: (HerdrSession) -> Unit) {
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
        computers.forEach { computer ->
            item(key = "computer-${computer.name}") { CodingComputerHeader(computer) }
            computer.workspaces.forEach { workspace ->
                if (workspace.title.isNotEmpty()) item(key = "workspace-${workspace.id}") { CodingWorkspaceHeader(workspace) }
                items(workspace.sessions, key = { entry -> "session-${entry.id}" }) { entry -> CodingSessionRow(entry, onClick = { onOpen(entry) }) }
            }
        }
        if (failedComputerNames.isNotEmpty()) {
            item(key = "failed") {
                Row(Modifier.fillMaxWidth().padding(16.dp), horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
                    Icon(Icons.Outlined.CloudOff, null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
                    Text(
                        stringResource(R.string.coding_computers_did_not_answer, failedComputerNames.joinToString(", ")),
                        style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}

/** A computer's heading: its name, and how many of its sessions ask and work. */
@Composable
internal fun CodingComputerHeader(computer: CodingComputer) {
    val parts = buildList {
        if (computer.askingCount > 0) add(stringResource(R.string.coding_asking_count, computer.askingCount))
        if (computer.workingCount > 0) add(stringResource(R.string.coding_working_count, computer.workingCount))
    }
    Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 20.dp, bottom = 4.dp), verticalAlignment = Alignment.CenterVertically) {
        Icon(Icons.Outlined.Computer, null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.size(20.dp))
        Text(computer.name, style = MaterialTheme.typography.titleMedium, modifier = Modifier.padding(start = 8.dp).weight(1f), maxLines = 1)
        if (parts.isNotEmpty()) Text(parts.joinToString(" · "), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** A workspace's name above its tabs. */
@Composable
internal fun CodingWorkspaceHeader(workspace: CodingWorkspace) {
    Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 12.dp, bottom = 2.dp), verticalAlignment = Alignment.CenterVertically) {
        Icon(Icons.Outlined.Folder, null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.size(16.dp))
        Text(workspace.title, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(start = 6.dp))
    }
}

/** One coding session in the list: its title, its tab and agent, and what it is doing. */
@Composable
internal fun CodingSessionRow(session: HerdrSession, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).androidClickable(onClick).padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(session.rowTitle, style = MaterialTheme.typography.titleSmall, maxLines = 1)
            if (session.rowDetail.isNotEmpty()) {
                Text(session.rowDetail, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
            }
        }
        CodingStateTag(session, Modifier.padding(start = 8.dp))
    }
}

private fun Modifier.androidClickable(onClick: () -> Unit): Modifier = this.then(Modifier.clickable(onClick = onClick))

/**
 * A session's state as a small tag: idle on a quiet ground, working in the accent, asking in orange with a question mark,
 * so color is never the only thing that says it.
 */
@Composable
internal fun CodingStateTag(session: HerdrSession, modifier: Modifier = Modifier) {
    when {
        session.isAsking -> Tag(stringResource(R.string.coding_state_asking), askingColor, isFilled = true, modifier = modifier, icon = Icons.Outlined.QuestionMark)
        session.isWorking -> Tag(stringResource(R.string.coding_state_working), MaterialTheme.colorScheme.primary, isFilled = true, modifier = modifier)
        else -> Tag(stringResource(R.string.coding_state_idle), MaterialTheme.colorScheme.onSurfaceVariant, isFilled = false, modifier = modifier)
    }
}

internal val askingColor = Color(0xFFE08A00)

@Composable
internal fun Tag(text: String, color: Color, isFilled: Boolean, modifier: Modifier = Modifier, icon: androidx.compose.ui.graphics.vector.ImageVector? = null) {
    Surface(
        modifier = modifier,
        shape = CircleShape,
        color = if (isFilled) color.copy(alpha = 0.15f) else Color.Transparent,
        border = if (isFilled) null else androidx.compose.foundation.BorderStroke(1.dp, MaterialTheme.colorScheme.outlineVariant),
    ) {
        Row(Modifier.padding(horizontal = 8.dp, vertical = 3.dp), verticalAlignment = Alignment.CenterVertically) {
            if (icon != null) Icon(icon, null, tint = color, modifier = Modifier.size(12.dp))
            Text(text, style = MaterialTheme.typography.labelMedium.copy(fontWeight = FontWeight.SemiBold), color = color, maxLines = 1)
        }
    }
}
