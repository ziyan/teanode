package com.teanode.app.chats

import android.Manifest
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ExitToApp
import androidx.compose.material.icons.automirrored.outlined.VolumeUp
import androidx.compose.material.icons.outlined.AdminPanelSettings
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.History
import androidx.compose.material.icons.outlined.KeyboardArrowDown
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.People
import androidx.compose.material.icons.outlined.PersonOff
import androidx.compose.material.icons.outlined.Tune
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.hapticfeedback.HapticFeedbackType
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.platform.LocalHapticFeedback
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.platform.UriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleResumeEffect
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.shell.Routes
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.openWebPage
import com.teanode.kit.AppSection
import com.teanode.kit.chats.AgentFile
import com.teanode.kit.chats.ChatNotificationLevel
import com.teanode.kit.chats.LinkOpening
import com.teanode.kit.chats.LinkPreview
import com.teanode.kit.chats.ServerTime
import com.teanode.kit.chats.TranscriptLine
import com.teanode.kit.chats.beginsRun
import com.teanode.kit.chats.reportPresence
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.time.ZoneId

/** The switches' keys for the lines a conversation hides unless asked, as the iPhone app keeps them. */
object TranscriptPreference {
    /** The kinds of working note the agent writes for itself, hidden unless the person asks to see them. */
    val workingNoteKinds = setOf(
        "queued", "compacting", "compacted", "depth", "call_unreadable", "repeated_failure", "round_limit",
    )
}

/** What the screen shows of one conversation: the lines, or a day between two of them. */
sealed interface ConversationItem {
    val key: String

    data class Line(val line: TranscriptLine) : ConversationItem {
        override val key: String get() = line.id
    }

    data class Day(val epochDay: Long, val startMS: Long) : ConversationItem {
        override val key: String get() = "day-$epochDay"
    }

    companion object {
        fun itemsOf(lines: List<TranscriptLine>, zone: ZoneId = ZoneId.systemDefault()): List<ConversationItem> {
            val items = mutableListOf<ConversationItem>()
            var lastDay: Long? = null
            for (line in lines) {
                val moment = ServerTime.instant(line.at)
                if (moment != null) {
                    val day = moment.atZone(zone).toLocalDate()
                    if (day.toEpochDay() != lastDay) {
                        items.add(Day(day.toEpochDay(), day.atStartOfDay(zone).toInstant().toEpochMilli()))
                        lastDay = day.toEpochDay()
                    }
                }
                items.add(Line(line))
            }
            return items
        }
    }
}

/** Opens a link as LinkOpening says: a web page in the app's own browser, anything else in whatever the system chooses. */
fun openLinkAsChosen(context: Context, address: String) {
    when (LinkOpening.way(address)) {
        LinkOpening.Way.InApp -> openWebPage(context, address)
        LinkOpening.Way.System -> try {
            context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(address)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        } catch (_: Exception) {
            // Nothing on the phone opens it.
        }
    }
}

/** What other parts of the app may plug into a conversation. */
object ChatsHooks {
    /** Opens a person's card; set by the part that has the cards. Until then a tap on a picture or a name does nothing. */
    var openPerson: ((String) -> Unit)? = null
}

/**
 * ConversationScreen is one conversation, laid out like a messaging app: the person's words on the right, the agent's
 * and other people's on the left, cards that wait for an answer, and a composer at the bottom. [conversationId] is null
 * for the main conversation.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ConversationScreen(conversationId: String?, onBack: () -> Unit, onOpenConversation: (String) -> Unit) {
    val context = LocalContext.current
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    val environment = remember(connection) { ChatsEnvironment.of(context, connection, session) }
    val facts by session.facts.collectAsState()
    val model = remember(connection, conversationId) {
        ConversationModel(environment, conversationId, conversationId?.let { id -> environment.known(id) })
    }
    DisposableEffect(model) { onDispose { model.close() } }
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    val haptics = LocalHapticFeedback.current
    val reader = remember { MessageReader(context) }
    val dictation = remember { Dictation(context) }
    val status = remember { StatusModel() }
    val listState = rememberLazyListState()
    var actionLine by remember { mutableStateOf<TranscriptLine?>(null) }
    var isShowingStatus by remember { mutableStateOf(false) }
    var isShowingMembers by remember { mutableStateOf(false) }
    var isRenaming by remember { mutableStateOf(false) }
    var isConfirmingLeave by remember { mutableStateOf(false) }
    var isShowingGoal by remember { mutableStateOf(false) }
    var isMenuOpen by remember { mutableStateOf(false) }
    val openLink: (String) -> Unit = { address -> openLinkAsChosen(context, address) }
    val uriHandler = remember(context) { object : UriHandler { override fun openUri(uri: String) = openLinkAsChosen(context, uri) } }
    DisposableEffect(reader) { onDispose { reader.stop() } }

    // The conversation is read and followed while the screen is in front, and let go when it is not.
    LifecycleResumeEffect(model) {
        model.startFollowing()
        onPauseOrDispose { model.stopFollowing() }
    }
    LifecycleResumeEffect(model, status) {
        val job = scope.launch { status.follow(connection, environment) { model.conversationId } }
        onPauseOrDispose { job.cancel() }
    }
    val lifecycleOwner = LocalLifecycleOwner.current
    LaunchedEffect(model, facts) {
        if (session.isDemo) return@LaunchedEffect
        lifecycleOwner.lifecycle.repeatOnLifecycle(Lifecycle.State.RESUMED) {
            try {
                while (true) {
                    // A message in a chat is held back from the phone only while that chat is on screen.
                    runCatching { connection.reportPresence(true, if (model.isChat) model.conversationId.orEmpty() else "") }
                    delay(30_000)
                }
            } finally {
                kotlinx.coroutines.withContext(kotlinx.coroutines.NonCancellable) { runCatching { connection.reportPresence(false, "") } }
            }
        }
    }

    LaunchedEffect(environment) { if (!environment.hasReadAgentFacts) environment.loadAgentName() }

    // Things that went wrong are told in a snackbar, once.
    LaunchedEffect(model.problemText) {
        model.problemText?.let { text ->
            snackbar.showProblem(text)
            model.problemText = null
        }
    }
    LaunchedEffect(model.voiceProblemText) {
        model.voiceProblemText?.let { text ->
            snackbar.showProblem(text)
            model.voiceProblemText = null
        }
    }
    LaunchedEffect(dictation.problemText) {
        dictation.problemText?.let { text ->
            snackbar.showProblem(text)
            dictation.problemText = null
        }
    }
    LaunchedEffect(model.sentCount) {
        if (model.sentCount > 0) {
            haptics.performHapticFeedback(HapticFeedbackType.TextHandleMove)
            dictation.stop()
            listState.scrollToItem(0)
        }
    }

    // The agent asked where the person is.
    val locationPermission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { _ ->
        model.locationRequestEvent?.let { event -> scope.launch { model.answerLocation(event, LocationReader.current(context)) } }
    }
    LaunchedEffect(model.locationRequestEvent) {
        val event = model.locationRequestEvent ?: return@LaunchedEffect
        when {
            session.isDemo -> model.answerLocation(event, LocationReader.demoReading())
            LocationReader.hasPermission(context) -> model.answerLocation(event, LocationReader.current(context))
            else -> locationPermission.launch(Manifest.permission.ACCESS_FINE_LOCATION)
        }
    }

    val shownLines = remember(model.lines, environment.isShowingToolCalls, environment.isShowingWorkingNotes, model.conversation?.jobKind) {
        val isMCPRecord = model.conversation?.jobKind == "mcp"
        model.lines.filter { line ->
            when (line.kind) {
                TranscriptLine.Kind.Tool -> environment.isShowingToolCalls || isMCPRecord || AgentFile.of(line.text).isNotEmpty()
                TranscriptLine.Kind.CheckIn -> environment.isShowingWorkingNotes || line.detail == com.teanode.kit.chats.CheckInOrigin.GoalNeedsYou.marker || line.detail == com.teanode.kit.chats.CheckInOrigin.GoalDone.marker
                TranscriptLine.Kind.Note -> environment.isShowingWorkingNotes || line.detail !in TranscriptPreference.workingNoteKinds
                else -> true
            }
        }
    }
    val items = remember(shownLines) { ConversationItem.itemsOf(shownLines) }
    val chat = model.chat
    val readers = remember(model.lines, chat?.members) { chat?.let { summary -> readersFromLines(model.lines, summary.members, model.ownUserId) } ?: emptyMap() }

    // The cards of the messages read so far, asked for before the list draws them.
    LaunchedEffect(model.conversationId, model.lines.size) {
        val id = model.conversationId ?: return@LaunchedEffect
        for (link in LinkPreview.linksAhead(model.lines)) launch { environment.linkPreview(link, id) }
    }

    // New words at the end keep the end in view when it was in view.
    val lastLine = model.lines.lastOrNull()
    LaunchedEffect(lastLine?.id, lastLine?.text?.length, model.runningRunIds.size) {
        if (listState.firstVisibleItemIndex <= 2) listState.scrollToItem(0)
    }
    LaunchedEffect(model.isLoaded) { if (model.isLoaded) listState.scrollToItem(0) }

    CompositionLocalProvider(
        LocalUriHandler provides uriHandler, LocalLinkOpener provides openLink, LocalPictureConversationId provides model.conversationId, LocalChatsEnvironment provides environment,
    ) {
        TeaNodeScreen(
            title = model.title.ifEmpty { environment.agentTitle },
            onBack = onBack,
            actions = {
                if (model.isChat) {
                    ConversationCallButton(model)
                    Box {
                        IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.chats_chat_options)) }
                        DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                            DropdownMenuItem(
                                text = { Text(stringResource(R.string.chats_members)) }, leadingIcon = { Icon(Icons.Outlined.People, null) },
                                onClick = { isMenuOpen = false; isShowingMembers = true },
                            )
                            if (chat?.canBeRenamed == true) {
                                DropdownMenuItem(
                                    text = { Text(stringResource(R.string.chats_rename_group)) }, leadingIcon = { Icon(Icons.Outlined.Edit, null) },
                                    onClick = { isMenuOpen = false; isRenaming = true },
                                )
                            }
                            for (level in ChatNotificationLevel.entries) {
                                DropdownMenuItem(
                                    text = { Text(notificationLevelName(level) + if (chat?.level == level) "  ✓" else "") }, leadingIcon = { Icon(Icons.Outlined.Tune, null) },
                                    onClick = { isMenuOpen = false; model.setNotificationLevel(level) },
                                )
                            }
                            if (chat?.canBeLeft == true) {
                                DropdownMenuItem(
                                    text = { Text(stringResource(R.string.chats_leave_group), color = MaterialTheme.colorScheme.error) },
                                    leadingIcon = { Icon(Icons.AutoMirrored.Outlined.ExitToApp, null, tint = MaterialTheme.colorScheme.error) },
                                    onClick = { isMenuOpen = false; isConfirmingLeave = true },
                                )
                            }
                        }
                    }
                } else {
                    IconButton(onClick = { isShowingStatus = true }) { Icon(Icons.Outlined.Tune, contentDescription = stringResource(R.string.chats_usage_and_devices)) }
                }
            },
        ) { contentPadding ->
            Column(Modifier.padding(contentPadding).fillMaxSize().imePadding()) {
                if (model.isChat) ConversationCallBanner(model)
                ConversationBanners(model, reader, onBackToMain = { navigator.navigate(Routes.child(AppSection.Agent, "main")) }, onOpenGoal = { isShowingGoal = true })
                Box(Modifier.weight(1f).fillMaxWidth()) {
                    if (!model.isLoaded) {
                        val problem = model.loadProblemText
                        if (problem != null) ErrorState(problem, onRetry = { model.startFollowing() }) else LoadingState()
                    }
                    // The list is drawn from its bottom up, so the newest line is at the foot and the keyboard lifts it.
                    LazyColumn(Modifier.fillMaxSize().testTag("conversation-list"), state = listState, reverseLayout = true, contentPadding = PaddingValues(horizontal = 16.dp, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
                        if (model.isLoaded && model.lines.isEmpty()) {
                            item(key = "empty") {
                                if (model.isChat) {
                                    Text(stringResource(R.string.chats_nothing_said_yet), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(top = 40.dp))
                                } else {
                                    EmptyConversation(model, onOpenConversation)
                                }
                            }
                        }
                        if (model.runningRunId == null && model.offeredSuggestions.isNotEmpty()) {
                            item(key = "suggestions") { SuggestionsRow(model.offeredSuggestions) { suggestion -> model.sendSuggestion(suggestion) } }
                        }
                        if (model.runningRunId != null && model.lines.lastOrNull()?.isStreaming != true) item(key = "typing-bubble") { TypingBubble() }
                        if (model.isChat) {
                            item(key = "typing-line") {
                                // Read again whenever somebody starts or stops typing.
                                val version = model.typingVersion
                                ChatTypingLine(remember(version) { model.typingNames() }, remember(version) { model.thinkingNames() }, environment.words)
                            }
                        }
                        items(items.asReversed(), key = { item -> item.key }) { item ->
                            val position = items.indexOf(item)
                            ConversationItemView(
                                item = item, previous = if (position > 0) items[position - 1] else null, readers = readers, model = model, environment = environment,
                                isShowingToolCalls = environment.isShowingToolCalls || model.conversation?.jobKind == "mcp", onLongPress = { line ->
                                    if (line.messageId != null) {
                                        haptics.performHapticFeedback(HapticFeedbackType.LongPress)
                                        actionLine = line
                                    }
                                },
                            )
                        }
                        if (model.earlierCount > 0) {
                            item(key = "earlier") {
                                // Reaching the top reads the page before it.
                                LaunchedEffect(Unit) { if (listState.firstVisibleItemIndex > 0) model.loadEarlier() }
                                Box(Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) {
                                    OutlinedButton(onClick = { model.loadEarlier() }) {
                                        if (model.isLoadingEarlier) androidx.compose.material3.CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                                        else Text(stringResource(R.string.chats_earlier_messages, model.earlierCount))
                                    }
                                }
                            }
                        }
                    }
                    // The jump to the end is offered only when the end is well out of sight.
                    if (listState.firstVisibleItemIndex > 3) {
                        Box(
                            Modifier.align(Alignment.BottomEnd).padding(16.dp).size(44.dp).clip(CircleShape).background(MaterialTheme.colorScheme.surfaceContainerHighest)
                                .clickable { scope.launch { listState.animateScrollToItem(0) } },
                            contentAlignment = Alignment.Center,
                        ) { Icon(Icons.Outlined.KeyboardArrowDown, contentDescription = stringResource(R.string.chats_jump_to_end)) }
                    }
                }
                // The message replied to, and whatever else the next message refers to.
                for (reference in model.references) ReferenceBar(reference, model, environment.agentTitle)
                if (model.mentionSuggestions.isNotEmpty()) MentionMenu(model.mentionSuggestions, environment.words) { choice -> model.choose(choice) }
                if (model.todos.isNotEmpty() && (model.runningRunId != null || model.todos.any { todo -> !todo.isDone })) TodoPanel(model.todos)
                Composer(model, environment, dictation)
            }
        }

        actionLine?.let { line -> MessageActionsSheet(line, model, reader, onDismiss = { actionLine = null }) }
        model.selectingText?.let { text -> SelectTextSheet(text, onDismiss = { model.selectingText = null }) }
        model.readingText?.let { text -> SelectTextSheet(text, stringResource(R.string.chats_the_whole_message), onDismiss = { model.readingText = null }) }
        model.editingLine?.let { line -> EditMessageSheet(line, model, onDismiss = { model.editingLine = null }) }
        model.deletingLine?.let { line ->
            ConfirmationDialog(
                title = stringResource(R.string.chats_delete_message_title), confirmLabel = stringResource(R.string.chats_delete), text = stringResource(R.string.chats_delete_message_detail),
                isDestructive = true, onConfirm = { model.deletingLine = null; model.delete(line) }, onDismiss = { model.deletingLine = null },
            )
        }
        if (isShowingStatus) StatusSheet(status, environment, connection, onDismiss = { isShowingStatus = false })
        if (isShowingMembers) {
            ChatMembersSheet(
                model, environment, onOpenPerson = { userId -> ChatsHooks.openPerson?.invoke(userId) },
                onOpenSettings = { navigator.openSection(AppSection.Settings) }, onDismiss = { isShowingMembers = false },
            )
        }
        if (isRenaming) RenameDialog(initial = model.chat?.title.orEmpty(), onSave = { title -> model.rename(title) }, onDismiss = { isRenaming = false })
        if (isConfirmingLeave) {
            ConfirmationDialog(
                title = stringResource(R.string.chats_leave_group_title), confirmLabel = stringResource(R.string.chats_leave_group), text = stringResource(R.string.chats_leave_group_detail),
                isDestructive = true,
                onConfirm = {
                    isConfirmingLeave = false
                    scope.launch { if (model.leave()) onBack() }
                },
                onDismiss = { isConfirmingLeave = false },
            )
        }
        if (isShowingGoal) {
            val goalConversation = model.conversation
            if (goalConversation != null && !goalConversation.goal.isNullOrEmpty()) GoalSheet(goalConversation, onOpenGoals = { isShowingGoal = false; navigator.openSection(AppSection.Goals) }, onDismiss = { isShowingGoal = false })
        }
    }
}

private fun readersFromLines(lines: List<TranscriptLine>, members: List<com.teanode.kit.chats.ChatMember>, ownUserId: String?): Map<String, List<com.teanode.kit.chats.ChatMember>> {
    val readers = linkedMapOf<String, MutableList<com.teanode.kit.chats.ChatMember>>()
    val stored = lines.mapNotNull { line ->
        val author = line.author
        val messageId = line.messageId
        if (author == null || messageId == null || line.isBeingSent) null else messageId to author
    }
    for (member in members) {
        if (member.userId == ownUserId || member.isRemoved) continue
        val readThrough = member.readThroughMessageId
        if (readThrough.isNullOrEmpty()) continue
        val found = stored.lastOrNull { (messageId, author) -> messageId <= readThrough && !(author.isPerson && author.authorUserId == member.userId) } ?: continue
        readers.getOrPut(found.first) { mutableListOf() }.add(member)
    }
    return readers
}

@Composable
private fun ConversationBanners(model: ConversationModel, reader: MessageReader, onBackToMain: () -> Unit, onOpenGoal: () -> Unit) {
    Column(Modifier.padding(horizontal = 16.dp, vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        if (reader.isReading) Banner(Icons.AutoMirrored.Outlined.VolumeUp, stringResource(R.string.chats_reading_aloud), onClick = { reader.stop() })
        model.actingAs?.let { actingAs -> Banner(Icons.Outlined.AdminPanelSettings, stringResource(R.string.chats_acting_as, actingAs, actingAs)) }
        if (model.conversation?.kind == "run") Banner(Icons.Outlined.History, stringResource(R.string.chats_run_record), onClick = onBackToMain)
        val found = model.conversation
        if (found != null && found.kind == "goal" && !found.goal.isNullOrEmpty()) GoalHeader(found, found.goal!!, onOpenGoal)
        if (model.isChat && !model.canWrite) {
            Banner(
                Icons.Outlined.PersonOff,
                stringResource(if (model.chat?.isRemoved == true) R.string.chats_not_in_chat_any_more else R.string.chats_no_shared_circle),
            )
        }
    }
}

@Composable
private fun ConversationItemView(
    item: ConversationItem,
    previous: ConversationItem?,
    readers: Map<String, List<com.teanode.kit.chats.ChatMember>>,
    model: ConversationModel,
    environment: ChatsEnvironment,
    isShowingToolCalls: Boolean,
    onLongPress: (TranscriptLine) -> Unit,
) {
    when (item) {
        is ConversationItem.Day -> Text(
            dayText(item.startMS, stringResource(R.string.chats_today), stringResource(R.string.chats_yesterday)), Modifier.fillMaxWidth().padding(top = 6.dp), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = androidx.compose.ui.text.style.TextAlign.Center,
        )
        is ConversationItem.Line -> {
            val line = item.line
            val previousLine = (previous as? ConversationItem.Line)?.line
            val holdable = Modifier.pointerInput(line.id) { detectTapGestures(onLongPress = { onLongPress(line) }) }
            when {
                line.kind == TranscriptLine.Kind.Person && line.isCallLine -> ConversationCallLine(line, model)
                line.kind == TranscriptLine.Kind.Person -> {
                    val author = line.author
                    if (author != null) {
                        ChatLineFrame(author, line.beginsRun(previousLine), line.messageId?.let { id -> readers[id] } ?: emptyList(), environment.words, onOpenPerson = { id -> ChatsHooks.openPerson?.invoke(id) }) {
                            LinkPreviewBelow(model.conversationId, line.text, false, environment, isReady = !line.isBeingSent && !line.isDeleted, modifier = holdable) {
                                PersonBubble(line, mentions = model.mentionWordsOf(line), onTranscribe = { model.transcribe(line) }) { model.readingText = line.text }
                            }
                        }
                    } else {
                        LinkPreviewBelow(model.conversationId, line.text, false, environment, isReady = !line.isBeingSent, modifier = holdable) {
                            PersonBubble(line) { model.readingText = line.text }
                        }
                    }
                }
                line.kind == TranscriptLine.Kind.Answer && line.author != null -> {
                    // An agent's words said in a chat: under its name, in front of the people in it.
                    val author = line.author!!
                    ChatLineFrame(author, line.beginsRun(previousLine), line.messageId?.let { id -> readers[id] } ?: emptyList(), environment.words, onOpenPerson = { id -> ChatsHooks.openPerson?.invoke(id) }) {
                        if (line.isDeleted) {
                            DeletedMessageBubble()
                        } else {
                            LinkPreviewBelow(model.conversationId, line.text, true, environment, isReady = !line.isStreaming, modifier = holdable) {
                                AgentBubble(reactions = line.reactions) { MarkdownView(line.text, mentions = model.mentionWordsOf(line), conversationId = model.conversationId) }
                            }
                        }
                    }
                }
                line.kind == TranscriptLine.Kind.Answer -> Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    LinkPreviewBelow(model.conversationId, line.text, true, environment, isReady = !line.isStreaming, modifier = holdable) {
                        AgentBubble(reactions = line.reactions) { MarkdownView(line.text, conversationId = model.conversationId) }
                    }
                    val usage = line.usage
                    if (environment.isShowingUsage && usage != null) {
                        Text(UsageWords.turn(usage, environment.currencyCode), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.outline, modifier = Modifier.padding(start = 8.dp))
                    }
                }
                line.kind == TranscriptLine.Kind.Tool -> Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    if (isShowingToolCalls) ToolLine(line)
                    ToolFiles(line.text, line.detail)
                }
                line.kind == TranscriptLine.Kind.CheckIn -> CheckInLine(line)
                line.kind == TranscriptLine.Kind.Note -> NoteLine(line)
                line.kind == TranscriptLine.Kind.Approval || line.kind == TranscriptLine.Kind.Question -> CardSurface { CardView(line, model, environment.agentTitle) }
                line.kind == TranscriptLine.Kind.CodingQuestion -> CardSurface(paddingDp = if (line.isDone) 12 else 16) { CodingQuestionCard(line, model) }
            }
        }
    }
}

private fun dayText(startMS: Long, today: String, yesterday: String): String {
    val now = System.currentTimeMillis()
    val dayMS = 24 * 60 * 60 * 1000L
    val zone = java.util.TimeZone.getDefault()
    fun dayNumber(ms: Long) = (ms + zone.getOffset(ms)) / dayMS
    return when (dayNumber(now) - dayNumber(startMS)) {
        0L -> today
        1L -> yesterday
        else -> android.text.format.DateUtils.formatDateTime(null, startMS, android.text.format.DateUtils.FORMAT_SHOW_WEEKDAY or android.text.format.DateUtils.FORMAT_ABBREV_ALL or android.text.format.DateUtils.FORMAT_SHOW_DATE)
    }
}

@Composable
private fun RenameDialog(initial: String, onSave: (String) -> Unit, onDismiss: () -> Unit) {
    var text by remember { mutableStateOf(initial) }
    androidx.compose.material3.AlertDialog(
        onDismissRequest = onDismiss, title = { Text(stringResource(R.string.chats_rename_group)) },
        text = { OutlinedTextField(value = text, onValueChange = { text = it }, label = { Text(stringResource(R.string.chats_group_name)) }, singleLine = true) },
        confirmButton = {
            TextButton(
                onClick = {
                    val title = text.trim()
                    onDismiss()
                    if (title.isNotEmpty()) onSave(title)
                },
                enabled = text.trim().isNotEmpty(),
            ) { Text(stringResource(R.string.chats_save)) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.chats_cancel)) } },
    )
}

/** A goal's whole text and what it waits on, in a sheet: pinned whole over the messages, a long goal covered half the screen. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun GoalSheet(conversation: com.teanode.kit.chats.AgentConversation, onOpenGoals: () -> Unit, onDismiss: () -> Unit) {
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = false)) {
        Column(Modifier.padding(horizontal = 20.dp).padding(bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(conversation.title ?: stringResource(R.string.chats_goal), style = MaterialTheme.typography.titleLarge)
            Text(goalStateWords(conversation.goalState), style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
            androidx.compose.foundation.text.selection.SelectionContainer { Text(conversation.goal.orEmpty(), style = MaterialTheme.typography.bodyLarge) }
            val note = conversation.goalNote
            if (conversation.goalState == "waiting" && !note.isNullOrEmpty()) {
                Text(stringResource(R.string.chats_waiting_for_you), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
                Text(note, style = MaterialTheme.typography.bodyLarge)
            }
            TextButton(onClick = onOpenGoals) { Text(stringResource(R.string.chats_open_goals)) }
        }
    }
}
