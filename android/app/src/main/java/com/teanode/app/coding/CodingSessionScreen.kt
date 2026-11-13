package com.teanode.app.coding

import android.content.Context
import android.net.Uri
import android.provider.OpenableColumns
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Send
import androidx.compose.material.icons.outlined.AttachFile
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Notifications
import androidx.compose.material.icons.outlined.NotificationsActive
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Cancel
import androidx.compose.material.icons.outlined.Computer
import androidx.compose.material.icons.outlined.ChatBubbleOutline
import androidx.compose.material.icons.outlined.AccountTree
import androidx.compose.material.icons.outlined.Terminal
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.FilledTonalIconButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.coding.CodingQuestion
import com.teanode.kit.coding.HerdrSession
import com.teanode.kit.coding.HerdrSubagents
import com.teanode.kit.coding.HerdrTurn
import com.teanode.kit.coding.answerCodingQuestion
import com.teanode.kit.coding.closeHerdrSession
import com.teanode.kit.coding.focusHerdrSession
import com.teanode.kit.coding.openHerdrSession
import com.teanode.kit.coding.readHerdrScreen
import com.teanode.kit.coding.readHerdrSession
import com.teanode.kit.coding.readHerdrSubagents
import com.teanode.kit.coding.sendHerdrSession
import com.teanode.kit.coding.terminalRuns
import com.teanode.kit.coding.watchHerdrSession
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/** What a session shows: what was said, the agents it started, or its screen. */
internal enum class CodingPart { Turns, Agents, Screen }

/** How many turns are read at first, and the most the server gives. */
private const val INITIAL_TURN_COUNT = 50
private const val GREATEST_TURN_COUNT = 100

/** The most the files of one message come to, which the server takes. */
private const val MOST_FILE_BYTES = 25 * 1024 * 1024

/** A file chosen to be sent with the next message. */
internal class PendingFile(val id: Int, val name: String, val contentType: String, val data: ByteArray)

/**
 * One coding session: what was said in it, the agents it started or its screen, the question it asks, and a field to type
 * into it. It reads again while it is on screen.
 */
@Composable
fun CodingSessionScreen(initialId: String, onBack: () -> Unit) {
    val connection = LocalConnection.current
    val appSession = LocalAppSession.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val model = remember(connection) { CodingModel.of(connection) }
    val scope = rememberCoroutineScope()
    val lifecycleOwner = LocalLifecycleOwner.current

    var selectedId by remember(initialId) { mutableStateOf(initialId) }
    var part by remember { mutableStateOf(CodingPart.Turns) }
    var turns by remember { mutableStateOf<List<HerdrTurn>>(emptyList()) }
    var isTruncated by remember { mutableStateOf(false) }
    var turnCount by remember { mutableIntStateOf(INITIAL_TURN_COUNT) }
    var screenText by remember { mutableStateOf("") }
    var agentsRead by remember { mutableStateOf<HerdrSubagents?>(null) }
    var isRead by remember { mutableStateOf(false) }
    val drafts = remember { mutableStateMapOf<String, String>() }
    val pendingFilesBySession = remember { mutableStateMapOf<String, List<PendingFile>>() }
    var isSending by remember { mutableStateOf(false) }
    var isOpeningTab by remember { mutableStateOf(false) }
    var isConfirmingClose by remember { mutableStateOf(false) }
    var isToolDetailWanted by remember { mutableStateOf(false) }
    var isClosed by remember { mutableStateOf(false) }
    var failureText by remember { mutableStateOf<String?>(null) }

    val session = model.session(selectedId)
    // The computer of the session on screen, from its id, which stands whether or not the list has it. A pane's id has no
    // slash; a computer's name might.
    val selectedComputer = selectedId.substringBeforeLast('/')

    suspend fun refresh() {
        // Opened before the list was read (from a link or a notification): read it first.
        if (model.session(selectedId) == null) model.load()
        val current = model.session(selectedId) ?: return
        val reading = current.id
        val readingPart = part
        // A read for another tab or the other part, finishing late, says nothing about what is on screen now.
        val isCurrent = { reading == selectedId && readingPart == part }
        try {
            when {
                // A session that has not said anything yet has no history to read: its transcript is empty, which is not a failure.
                part == CodingPart.Turns && current.transcriptPath.isEmpty() -> {
                    turns = emptyList()
                    isTruncated = false
                }
                part == CodingPart.Turns -> {
                    val read = connection.readHerdrSession(current.computer, current.paneId, turnCount, isToolDetailWanted)
                    if (!isCurrent()) return
                    turns = read.turns
                    isTruncated = read.isTruncated
                    model.upsert(read.herdrSession)
                }
                part == CodingPart.Agents -> {
                    val read = connection.readHerdrSubagents(current.computer, current.paneId)
                    if (!isCurrent()) return
                    agentsRead = read
                    model.upsert(read.herdrSession)
                }
                else -> {
                    val read = connection.readHerdrScreen(current.computer, current.paneId)
                    if (!isCurrent()) return
                    screenText = read.screenText
                    model.upsert(read.herdrSession)
                }
            }
            isRead = true
            failureText = null
            // The list, for this session's state and the list a step back.
            model.load()
        } catch (cancelled: kotlinx.coroutines.CancellationException) {
            throw cancelled
        } catch (error: Throwable) {
            if (!isCurrent()) return
            val described = appSession.describe(error)
            if (described != failureText) snackbar.showProblem(described)
            failureText = described
            isRead = true
        }
    }

    // Read while this is on screen and the app is in front: quickly while the session works or asks, slowly while it is
    // idle. A demo reads once.
    LaunchedEffect(selectedId, part, turnCount, isToolDetailWanted) {
        lifecycleOwner.repeatOnLifecycle(Lifecycle.State.STARTED) {
            while (true) {
                refresh()
                if (appSession.isDemo) break
                // The Agents part changes while the session works, and is read every few seconds whatever the session says.
                val isBusy = model.session(selectedId)?.let { current -> current.isWorking || current.isAsking } == true || part == CodingPart.Agents
                delay(if (isBusy) 3_000L else 15_000L)
            }
        }
    }

    suspend fun answer(question: CodingQuestion, options: List<CodingQuestion.Option>, freeText: String?) {
        try {
            connection.answerCodingQuestion(question, options, freeText)
            refresh()
        } catch (cancelled: kotlinx.coroutines.CancellationException) {
            throw cancelled
        } catch (error: Throwable) {
            snackbar.showError(error)
            refresh()
        }
    }

    fun send(current: HerdrSession) {
        val sessionId = current.id
        val text = (drafts[sessionId] ?: "").trim()
        val pendingFiles = pendingFilesBySession[sessionId] ?: emptyList()
        if ((text.isEmpty() && pendingFiles.isEmpty()) || isSending) return
        if (pendingFiles.sumOf { file -> file.data.size } > MOST_FILE_BYTES) {
            snackbar.showProblem(context.getString(R.string.coding_files_too_big))
            return
        }
        isSending = true
        scope.launch {
            try {
                val attachmentIds = pendingFiles.map { file -> connection.upload(file.name, file.contentType, file.data) }
                val sent = connection.sendHerdrSession(current.computer, current.paneId, text, attachmentIds)
                model.upsert(sent)
                drafts.remove(sessionId)
                // Files added while these were sent stay for the next message.
                val sentIds = pendingFiles.map { file -> file.id }.toSet()
                pendingFilesBySession[sessionId] = (pendingFilesBySession[sessionId] ?: emptyList()).filter { file -> !sentIds.contains(file.id) }
                refresh()
            } catch (cancelled: kotlinx.coroutines.CancellationException) {
                throw cancelled
            } catch (error: Throwable) {
                snackbar.showError(error)
            } finally {
                isSending = false
            }
        }
    }

    fun show(current: HerdrSession) {
        scope.launch {
            try {
                model.upsert(connection.focusHerdrSession(current.computer, current.paneId))
                snackbar.show(context.getString(R.string.coding_shown_on, current.computer))
                model.load()
            } catch (error: Throwable) {
                snackbar.showError(error)
            }
        }
    }

    fun watch(current: HerdrSession) {
        scope.launch {
            try {
                model.upsert(connection.watchHerdrSession(current.computer, current.paneId))
                snackbar.show(context.getString(R.string.coding_will_tell_you, current.tabTitle))
            } catch (error: Throwable) {
                snackbar.showError(error)
            }
        }
    }

    fun close(current: HerdrSession) {
        scope.launch {
            try {
                connection.closeHerdrSession(current.computer, current.paneId)
                snackbar.show(context.getString(R.string.coding_closed, current.tabTitle))
                // Back to the list, which holds the workspace's other tabs. The screen draws nothing while it leaves.
                isClosed = true
                model.remove(current.id)
                onBack()
            } catch (error: Throwable) {
                snackbar.showError(error)
            }
        }
    }

    /** Opens a new tab in this session's workspace, in its directory, and shows it. */
    fun openTab(beside: HerdrSession, codingAgentKind: String) {
        isOpeningTab = true
        scope.launch {
            try {
                val opened = connection.openHerdrSession(beside.computer, paneId = beside.paneId, codingAgentKind = codingAgentKind)
                model.upsert(opened)
                selectedId = opened.id
                turns = emptyList(); screenText = ""; agentsRead = null; isRead = false; isTruncated = false; turnCount = INITIAL_TURN_COUNT
                model.load()
            } catch (error: Throwable) {
                snackbar.showError(error)
            } finally {
                isOpeningTab = false
            }
        }
    }

    TeaNodeScreen(
        title = session?.let { current -> "${current.workspaceTitle} · ${current.computer}" } ?: "",
        onBack = onBack,
        actions = {
            if (session != null) {
                CodingPartMenu(part, onChoose = { chosen ->
                    // The other part has not been read yet: a spinner until it is.
                    if (chosen != part) {
                        part = chosen
                        screenText = ""; agentsRead = null; isRead = false
                    }
                })
                CodingActionsMenu(
                    session, isOpeningTab,
                    onOpenTab = { kind -> openTab(session, kind) }, onShow = { show(session) }, onWatch = { watch(session) },
                    onClose = { isConfirmingClose = true },
                )
            }
        },
    ) { contentPadding ->
        Column(Modifier.fillMaxSize().padding(contentPadding)) {
            when {
                isClosed -> Box(Modifier.fillMaxSize())
                session != null -> Column(Modifier.weight(1f)) {
                    when (part) {
                        CodingPart.Turns -> CodingTranscript(
                            session, turns, isTruncated, turnCount, isRead,
                            onShowEarlier = { turnCount = GREATEST_TURN_COUNT }, onWantToolDetail = { isToolDetailWanted = true },
                            onAnswer = { question, options, freeText -> scope.launch { answer(question, options, freeText) } },
                        )
                        CodingPart.Agents -> CodingAgentsList(session, agentsRead, isRead)
                        CodingPart.Screen -> CodingScreenText(session, screenText, isRead, onAnswer = { question, options, freeText -> scope.launch { answer(question, options, freeText) } })
                    }
                }
                model.list != null && !model.list!!.failedComputerNames.contains(selectedComputer) -> Box(Modifier.weight(1f)) {
                    // The list was read and the session is not in it: closed on the computer, or its agent ended.
                    EmptyState(Icons.Outlined.Cancel, stringResource(R.string.coding_session_closed))
                }
                else -> Box(Modifier.weight(1f)) { LoadingState() }
            }
            // Nothing can be sent while the session asks, and its question takes a typed answer of its own, so the box gives its room to it.
            if (session != null && !isClosed && !session.isAsking) {
                val sessionId = session.id
                CodingComposer(
                    draft = drafts[sessionId] ?: "",
                    onDraftChange = { drafts[sessionId] = it },
                    pendingFiles = pendingFilesBySession[sessionId] ?: emptyList(),
                    onFilesAdded = { added -> pendingFilesBySession[sessionId] = (pendingFilesBySession[sessionId] ?: emptyList()) + added },
                    onFileRemoved = { id -> pendingFilesBySession[sessionId] = (pendingFilesBySession[sessionId] ?: emptyList()).filter { file -> file.id != id } },
                    isSending = isSending, isDisabled = session.isAsking, onSend = { send(session) },
                )
            }
        }
    }

    if (isConfirmingClose && session != null) {
        ConfirmationDialog(
            title = stringResource(R.string.coding_close_title),
            text = stringResource(R.string.coding_close_message, session.codingAgentName, session.tabTitle, session.computer),
            confirmLabel = stringResource(R.string.coding_close_tab), isDestructive = true,
            onConfirm = { isConfirmingClose = false; close(session) }, onDismiss = { isConfirmingClose = false },
        )
    }
}

@Composable
private fun CodingPartMenu(part: CodingPart, onChoose: (CodingPart) -> Unit) {
    var isOpen by remember { mutableStateOf(false) }
    IconButton(onClick = { isOpen = true }) {
        Icon(part.icon, contentDescription = stringResource(R.string.coding_choose_what_to_show))
    }
    DropdownMenu(expanded = isOpen, onDismissRequest = { isOpen = false }) {
        CodingPart.entries.forEach { option ->
            DropdownMenuItem(
                text = { Text(option.title(), fontWeight = if (option == part) FontWeight.SemiBold else FontWeight.Normal) },
                leadingIcon = { Icon(option.icon, null) },
                onClick = { isOpen = false; onChoose(option) },
            )
        }
    }
}

private val CodingPart.icon get() = when (this) {
    CodingPart.Turns -> Icons.Outlined.ChatBubbleOutline
    CodingPart.Agents -> Icons.Outlined.AccountTree
    CodingPart.Screen -> Icons.Outlined.Terminal
}

@Composable
private fun CodingPart.title(): String = when (this) {
    CodingPart.Turns -> stringResource(R.string.coding_part_turns)
    CodingPart.Agents -> stringResource(R.string.coding_part_agents)
    CodingPart.Screen -> stringResource(R.string.coding_part_screen)
}

@Composable
private fun CodingActionsMenu(session: HerdrSession, isOpeningTab: Boolean, onOpenTab: (String) -> Unit, onShow: () -> Unit, onWatch: () -> Unit, onClose: () -> Unit) {
    var isOpen by remember { mutableStateOf(false) }
    var isChoosingAgent by remember { mutableStateOf(false) }
    IconButton(onClick = { isOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.coding_session_actions)) }
    DropdownMenu(expanded = isOpen, onDismissRequest = { isOpen = false; isChoosingAgent = false }) {
        if (isChoosingAgent) {
            DropdownMenuItem(text = { Text("Claude Code") }, onClick = { isOpen = false; isChoosingAgent = false; onOpenTab("claude") })
            DropdownMenuItem(text = { Text("Codex") }, onClick = { isOpen = false; isChoosingAgent = false; onOpenTab("codex") })
        } else {
            // A computer program too old to say where the pane is cannot open beside it.
            DropdownMenuItem(
                text = { Text(stringResource(R.string.coding_new_tab)) }, leadingIcon = { Icon(Icons.Outlined.Add, null) },
                enabled = !isOpeningTab && session.workspaceId.isNotEmpty(), onClick = { isChoosingAgent = true },
            )
            DropdownMenuItem(text = { Text(stringResource(R.string.coding_show_on_computer)) }, leadingIcon = { Icon(Icons.Outlined.Computer, null) }, onClick = { isOpen = false; onShow() })
            if (session.isWatched) {
                DropdownMenuItem(text = { Text(stringResource(R.string.coding_telling_you)) }, leadingIcon = { Icon(Icons.Outlined.NotificationsActive, null) }, enabled = false, onClick = {})
            } else {
                DropdownMenuItem(text = { Text(stringResource(R.string.coding_tell_me)) }, leadingIcon = { Icon(Icons.Outlined.Notifications, null) }, onClick = { isOpen = false; onWatch() })
            }
            DropdownMenuItem(
                text = { Text(stringResource(R.string.coding_close_tab), color = MaterialTheme.colorScheme.error) },
                leadingIcon = { Icon(Icons.Outlined.Cancel, null, tint = MaterialTheme.colorScheme.error) },
                enabled = !session.isWorking, onClick = { isOpen = false; onClose() },
            )
        }
    }
}

// The transcript

@Composable
private fun CodingTranscript(
    session: HerdrSession,
    turns: List<HerdrTurn>,
    isTruncated: Boolean,
    turnCount: Int,
    isRead: Boolean,
    onShowEarlier: () -> Unit,
    onWantToolDetail: () -> Unit,
    onAnswer: (CodingQuestion, List<CodingQuestion.Option>, String?) -> Unit,
) {
    val listState = rememberLazyListState()
    val itemCount = turns.size + 3
    // New turns keep the view at the end only when the person was there; one reading earlier turns is not pulled away.
    val isAtEnd by remember { androidx.compose.runtime.derivedStateOf { val last = listState.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0; last >= listState.layoutInfo.totalItemsCount - 2 } }
    LaunchedEffect(turns.size, session.question, session.isWorking, isRead) {
        if (isRead && itemCount > 0 && (isAtEnd || turns.size <= INITIAL_TURN_COUNT)) listState.scrollToItem(itemCount - 1)
    }
    Box(Modifier.fillMaxSize()) {
        LazyColumn(Modifier.fillMaxSize(), state = listState, contentPadding = androidx.compose.foundation.layout.PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            if (isTruncated && turnCount < GREATEST_TURN_COUNT) {
                item { Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) { OutlinedButton(onClick = onShowEarlier) { Text(stringResource(R.string.coding_show_earlier_turns)) } } }
            }
            items(turns.size) { index -> CodingTurnView(turns[index], onWantToolDetail) }
            item {
                if (session.isWorking && isRead) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
                        Text(stringResource(R.string.coding_state_working), color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                if (turns.isEmpty() && isRead) {
                    Text(
                        stringResource(R.string.coding_nothing_said_yet), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.fillMaxWidth().padding(vertical = 24.dp), textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                    )
                }
            }
            item { CodingQuestionCard(session, onAnswer) }
        }
        if (!isRead) LoadingState()
    }
}

@Composable
internal fun CodingQuestionCard(session: HerdrSession, onAnswer: (CodingQuestion, List<CodingQuestion.Option>, String?) -> Unit) {
    val question = session.codingQuestion
    if (session.isAsking && question != null) {
        Card(colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainerHigh)) {
            Column(Modifier.padding(16.dp)) {
                key(question.questionFingerprint) { CodingAnswerCard(question) { options, freeText -> onAnswer(question, options, freeText) } }
            }
        }
    }
}

/** One thing said or done in a session: the person's words on the right in a bubble, the agent's on the left, a tool as one quiet line. */
@Composable
private fun CodingTurnView(turn: HerdrTurn, onWantToolDetail: () -> Unit) {
    when (turn.herdrTurnRole) {
        "person" -> Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
            Box(Modifier.padding(start = 56.dp).background(MaterialTheme.colorScheme.primaryContainer, RoundedCornerShape(20.dp)).padding(horizontal = 14.dp, vertical = 10.dp)) {
                SelectionContainer { Text(turn.turnText, color = MaterialTheme.colorScheme.onPrimaryContainer) }
            }
        }
        "tool" -> CodingToolTurnView(turn, onWantToolDetail)
        else -> SelectionContainer { CodingMarkdown(turn.turnText) }
    }
}

/** A tool call: one line, which opens, when the read gave them, to the tool's whole input and what it answered. */
@Composable
private fun CodingToolTurnView(turn: HerdrTurn, onWantDetail: () -> Unit) {
    var isOpen by remember { mutableStateOf(false) }
    Column(Modifier.fillMaxWidth().padding(horizontal = 4.dp)) {
        Row(
            Modifier.fillMaxWidth().then(if (turn.hasToolDetail) Modifier.clickable { isOpen = !isOpen; if (isOpen && !turn.isToolDetailLoaded) onWantDetail() } else Modifier),
            verticalAlignment = Alignment.Top,
        ) {
            Text(
                turn.turnText, style = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace), color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = if (isOpen) Int.MAX_VALUE else 2, overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis, modifier = Modifier.weight(1f),
            )
            if (turn.hasToolDetail) Icon(if (isOpen) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore, null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        if (isOpen) {
            Column(Modifier.padding(top = 6.dp).fillMaxWidth().background(MaterialTheme.colorScheme.surfaceContainerHigh, RoundedCornerShape(10.dp)).padding(10.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                if (!turn.isToolDetailLoaded) Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) { CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp) }
                turn.toolInputText?.takeIf { text -> text.isNotEmpty() }?.let { text -> ToolDetail(stringResource(R.string.coding_tool_input), text) }
                turn.toolResultText?.takeIf { text -> text.isNotEmpty() }?.let { text -> ToolDetail(stringResource(R.string.coding_tool_result), text) }
            }
        }
    }
}

@Composable
private fun ToolDetail(title: String, text: String) {
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(title, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        SelectionContainer { Text(text, style = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace)) }
    }
}

// The screen

private val terminalBackground = Color(0xFF1E1E1E)
private val terminalText = Color(0xFFD4D4D4)

/** The screen as the program in the pane colored it, on a terminal's dark ground whatever the app's appearance. */
@Composable
private fun CodingScreenText(session: HerdrSession, screenText: String, isRead: Boolean, onAnswer: (CodingQuestion, List<CodingQuestion.Option>, String?) -> Unit) {
    val colored = remember(screenText) { coloredScreen(screenText) }
    Box(Modifier.fillMaxSize()) {
        Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Box(Modifier.horizontalScroll(rememberScrollState()).background(terminalBackground, RoundedCornerShape(8.dp)).padding(10.dp)) {
                SelectionContainer { Text(colored, style = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace), softWrap = false) }
            }
            CodingQuestionCard(session, onAnswer)
        }
        if (!isRead) LoadingState()
    }
}

internal fun coloredScreen(text: String): AnnotatedString = buildAnnotatedString {
    for (run in terminalRuns(text)) {
        var foreground = run.foreground?.let { color -> Color(color.red, color.green, color.blue) } ?: terminalText
        var background = run.background?.let { color -> Color(color.red, color.green, color.blue) }
        if (run.isInverse) {
            val swapped = background ?: terminalBackground
            background = foreground
            foreground = swapped
        }
        val style = SpanStyle(
            color = if (run.isDim) foreground.copy(alpha = 0.6f) else foreground,
            background = background ?: Color.Unspecified,
            fontWeight = if (run.isBold) FontWeight.Bold else null,
            fontStyle = if (run.isItalic) FontStyle.Italic else null,
            textDecoration = if (run.isUnderlined) TextDecoration.Underline else null,
        )
        withStyle(style) { append(run.text) }
    }
}

// The question

/**
 * The question a session waits on, with its options as rows to choose: one press answers, or with several allowed the
 * chosen ones are sent together. An option that takes typed text has a field.
 */
@Composable
internal fun CodingAnswerCard(question: CodingQuestion, onAnswer: suspend (List<CodingQuestion.Option>, String?) -> Unit) {
    var chosen by remember { mutableStateOf(setOf<Int>()) }
    var typed by remember { mutableStateOf("") }
    var isAnswering by remember { mutableStateOf(false) }
    var isShowingDetails by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()

    fun submit(options: List<CodingQuestion.Option>, freeText: String?) {
        isAnswering = true
        scope.launch {
            onAnswer(options, freeText)
            isAnswering = false
        }
    }

    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Icon(Icons.Outlined.Terminal, null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.size(18.dp))
            Text(
                listOf(question.codingAgentName, question.sessionName).filter { part -> part.isNotEmpty() }.joinToString(" · "),
                style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2,
                overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis, modifier = Modifier.padding(start = 6.dp).weight(1f),
            )
            TextButton(onClick = { isShowingDetails = !isShowingDetails }) {
                Text(stringResource(if (isShowingDetails) R.string.coding_hide_details else R.string.coding_details))
            }
        }
        Text(question.headline, maxLines = if (isShowingDetails) 3 else Int.MAX_VALUE, overflow = androidx.compose.ui.text.style.TextOverflow.Ellipsis)
        if (isShowingDetails) {
            Box(Modifier.fillMaxWidth().background(MaterialTheme.colorScheme.surfaceContainerHighest, RoundedCornerShape(10.dp)).padding(10.dp)) {
                SelectionContainer { Text(question.shownText, style = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace)) }
            }
        }
        Column {
            question.choices.forEachIndexed { index, option ->
                if (index > 0) HorizontalDivider()
                Row(
                    Modifier.fillMaxWidth().clickable(enabled = !isAnswering) {
                        if (question.isMultipleChoice) {
                            chosen = if (chosen.contains(option.optionNumber)) chosen - option.optionNumber else chosen + option.optionNumber
                        } else {
                            submit(listOf(option), null)
                        }
                    }.padding(vertical = 10.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    if (question.isMultipleChoice) Checkbox(checked = chosen.contains(option.optionNumber), onCheckedChange = null, modifier = Modifier.padding(end = 12.dp))
                    Column(Modifier.weight(1f)) {
                        Text(option.optionLabel, style = MaterialTheme.typography.bodyLarge)
                        if (option.optionDescription.isNotEmpty()) Text(option.optionDescription, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
        }
        if (question.isMultipleChoice) {
            Button(
                onClick = { submit(question.choices.filter { option -> chosen.contains(option.optionNumber) }, null) },
                enabled = chosen.isNotEmpty() && !isAnswering, modifier = Modifier.fillMaxWidth(),
            ) { Text(stringResource(R.string.coding_answer_with_these)) }
        }
        if (question.options.any { option -> option.herdrOptionKind == "freeText" }) {
            OutlinedTextField(typed, { typed = it }, Modifier.fillMaxWidth(), placeholder = { Text(stringResource(R.string.coding_type_an_answer)) }, minLines = 1, maxLines = 4)
            OutlinedButton(onClick = { submit(emptyList(), typed.trim()) }, enabled = typed.trim().isNotEmpty() && !isAnswering, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.coding_answer))
            }
        }
    }
}

// The composer

/**
 * Where the person types into a session: a field with the words, adding files and sending. It is off while the session
 * asks, since a typed line would answer the question by accident.
 */
@Composable
internal fun CodingComposer(
    draft: String,
    onDraftChange: (String) -> Unit,
    pendingFiles: List<PendingFile>,
    onFilesAdded: (List<PendingFile>) -> Unit,
    onFileRemoved: (Int) -> Unit,
    isSending: Boolean,
    isDisabled: Boolean,
    onSend: () -> Unit,
) {
    val context = LocalContext.current
    val canSend = !isDisabled && !isSending && (draft.trim().isNotEmpty() || pendingFiles.isNotEmpty())
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.GetMultipleContents()) { addresses ->
        onFilesAdded(addresses.mapNotNull { address -> readPendingFile(context, address, pendingFiles.size + addresses.indexOf(address)) })
    }
    // A picture from the camera is written to the app's cache first (the chat's file provider), then read as a file.
    var cameraTarget by remember { mutableStateOf<java.io.File?>(null) }
    val camera = rememberLauncherForActivityResult(ActivityResultContracts.TakePicture()) { isTaken ->
        val target = cameraTarget
        if (isTaken && target != null) {
            val address = androidx.core.content.FileProvider.getUriForFile(context, "${context.packageName}.chatfiles", target)
            readPendingFile(context, address, pendingFiles.size)?.let { file -> onFilesAdded(listOf(file)) }
        }
        target?.delete()
    }
    var isAttachMenuOpen by remember { mutableStateOf(false) }
    Column(Modifier.fillMaxWidth().imePadding().padding(horizontal = 12.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        if (pendingFiles.isNotEmpty()) {
            Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                pendingFiles.forEach { file ->
                    AssistChip(
                        onClick = { onFileRemoved(file.id) }, label = { Text(file.name, maxLines = 1) },
                        trailingIcon = { Icon(Icons.Outlined.Close, stringResource(R.string.coding_remove_file), Modifier.size(16.dp)) },
                    )
                }
            }
        }
        Row(verticalAlignment = Alignment.Bottom, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            Box {
                IconButton(onClick = { isAttachMenuOpen = true }, enabled = !isDisabled) {
                    Icon(Icons.Outlined.AttachFile, contentDescription = stringResource(R.string.coding_add_files))
                }
                DropdownMenu(isAttachMenuOpen, { isAttachMenuOpen = false }) {
                    DropdownMenuItem(text = { Text(stringResource(R.string.coding_photo_library)) }, onClick = { isAttachMenuOpen = false; picker.launch("image/*") })
                    DropdownMenuItem(text = { Text(stringResource(R.string.coding_camera)) }, onClick = {
                        isAttachMenuOpen = false
                        val directory = java.io.File(context.cacheDir, "chat-files").apply { mkdirs() }
                        val target = java.io.File(directory, "coding-picture-${System.currentTimeMillis()}.jpg")
                        cameraTarget = target
                        runCatching { camera.launch(androidx.core.content.FileProvider.getUriForFile(context, "${context.packageName}.chatfiles", target)) }
                    })
                    DropdownMenuItem(text = { Text(stringResource(R.string.coding_files)) }, onClick = { isAttachMenuOpen = false; picker.launch("*/*") })
                    // A picture copied elsewhere, a screenshot say, attached as the dashboard attaches a pasted one.
                    val clipboard = context.getSystemService(android.content.ClipboardManager::class.java)
                    val pastedAddress = clipboard?.primaryClip?.takeIf { clip -> clip.itemCount > 0 && clip.description.hasMimeType("image/*") }?.getItemAt(0)?.uri
                    if (pastedAddress != null) {
                        DropdownMenuItem(text = { Text(stringResource(R.string.coding_paste_picture)) }, onClick = {
                            isAttachMenuOpen = false
                            readPendingFile(context, pastedAddress, pendingFiles.size)?.let { file -> onFilesAdded(listOf(file)) }
                        })
                    }
                }
            }
            OutlinedTextField(
                value = draft, onValueChange = onDraftChange, modifier = Modifier.weight(1f), enabled = !isDisabled,
                placeholder = { Text(stringResource(if (isDisabled) R.string.coding_answer_first else R.string.coding_type_instruction)) },
                minLines = 1, maxLines = 8, shape = RoundedCornerShape(24.dp),
            )
            FilledTonalIconButton(onClick = onSend, enabled = canSend) {
                if (isSending) CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp) else Icon(Icons.AutoMirrored.Outlined.Send, contentDescription = stringResource(R.string.coding_send))
            }
        }
    }
}

internal fun readPendingFile(context: Context, address: Uri, id: Int): PendingFile? = runCatching {
    val resolver = context.contentResolver
    val name = resolver.query(address, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { cursor -> if (cursor.moveToFirst()) cursor.getString(0) else null }
        ?: "file-$id"
    val data = resolver.openInputStream(address)?.use { stream -> stream.readBytes() } ?: return null
    PendingFile(id, name, resolver.getType(address) ?: "application/octet-stream", data)
}.getOrNull()
