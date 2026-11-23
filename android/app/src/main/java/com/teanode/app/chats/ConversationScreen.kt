package com.teanode.app.chats

import android.Manifest
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
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.height
import androidx.compose.material3.Surface
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.nestedscroll.nestedScroll
import androidx.compose.ui.platform.LocalDensity
import com.teanode.kit.chats.EarlierRow
import com.teanode.kit.chats.FollowRule
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
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleResumeEffect
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import com.teanode.app.R
import com.teanode.app.contacts.ContactsRoutes
import com.teanode.kit.contacts.PersonCardTarget
import com.teanode.app.shell.ShownConversation
import com.teanode.app.shell.rememberAppLinkOpener
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.shell.Routes
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.AppSection
import com.teanode.kit.chats.AgentFile
import com.teanode.kit.chats.AgentReference
import com.teanode.kit.chats.ChatNotificationLevel
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

/** What other parts of the app may plug into a conversation. */
object ChatsHooks {
    /** What the main conversation's next message asks about, set by [askAbout] and taken by the conversation when it opens. */
    var pendingReference: AgentReference? by androidx.compose.runtime.mutableStateOf(null)
        private set

    /**
     * Opens the main conversation with [reference] (a mail, a transaction, a page, a recording or a photo) in its
     * reference strip, as the iPhone app does when a screen says "Ask the agent".
     */
    fun askAbout(navigator: com.teanode.app.shell.AppNavigator, reference: AgentReference) {
        pendingReference = reference
        navigator.navigate(Routes.child(AppSection.Agent, "main"))
    }

    internal fun takePendingReference(): AgentReference? = pendingReference.also { pendingReference = null }

    private val openingRequests = HashMap<String, String>()

    /**
     * Opens a conversation, and sends [openingRequest] in it once it is on screen, as the iPhone app does for an idea
     * that was started from another screen. Without a request it only opens the conversation.
     */
    fun openConversation(navigator: com.teanode.app.shell.AppNavigator, conversationId: String, openingRequest: String? = null) {
        if (!openingRequest.isNullOrEmpty()) openingRequests[conversationId] = openingRequest
        navigator.navigate(Routes.child(AppSection.Agent, "conversation/$conversationId"))
    }

    internal fun takeOpeningRequest(conversationId: String): String? = openingRequests.remove(conversationId)

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
    ConversationCallEffects(model)
    val navigator = LocalAppNavigator.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    val haptics = LocalHapticFeedback.current
    val reader = remember { MessageReader(context) }
    val dictation = remember { Dictation(context) }
    val status = remember { StatusModel() }
    val listState = rememberLazyListState()
    val density = LocalDensity.current
    // The first message shown, remembered when earlier ones are asked for so the reading place can be put back.
    var readingAnchorKey by remember { mutableStateOf<String?>(null) }
    var isShowingEnd by remember { mutableStateOf(true) }
    var hasArrivedWhileAway by remember { mutableStateOf(false) }
    // The oldest message in view, as the list shows it from the bottom up.
    fun oldestShownKey(): String? = listState.layoutInfo.visibleItemsInfo.lastOrNull { info -> (info.key as? String)?.let { key -> model.lines.any { line -> line.id == key } } == true }?.key as? String
    val pullTracker = remember(model, density) {
        EarlierPullTracker(
            density,
            onArmed = { haptics.performHapticFeedback(HapticFeedbackType.TextHandleMove) },
            onLetGoArmed = {
                if (model.earlierCount > 0 && !model.isLoadingEarlier) {
                    readingAnchorKey = oldestShownKey()
                    model.loadEarlier()
                }
            },
        )
    }
    var actionLine by remember { mutableStateOf<TranscriptLine?>(null) }
    var isShowingStatus by remember { mutableStateOf(false) }
    var isShowingMembers by remember { mutableStateOf(false) }
    var isRenaming by remember { mutableStateOf(false) }
    var isConfirmingLeave by remember { mutableStateOf(false) }
    var isShowingGoal by remember { mutableStateOf(false) }
    var isMenuOpen by remember { mutableStateOf(false) }
    val linkOpener = rememberAppLinkOpener()
    val openLink: (String) -> Unit = { address -> linkOpener.followLink(address) }
    val uriHandler = remember(linkOpener) { object : UriHandler { override fun openUri(uri: String) = linkOpener.followLink(uri) } }
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
                // The shell repeats this every half minute while the app is in front (ShellPolling).
                ShownConversation.show(if (model.isChat) model.conversationId.orEmpty() else "")
                while (true) {
                    // A message in a chat is held back from the phone only while that chat is on screen.
                    runCatching { connection.reportPresence(true, if (model.isChat) model.conversationId.orEmpty() else "") }
                    delay(30_000)
                }
            } finally {
                ShownConversation.clear()
                kotlinx.coroutines.withContext(kotlinx.coroutines.NonCancellable) { runCatching { connection.reportPresence(false, "") } }
            }
        }
    }

    LaunchedEffect(environment) { if (!environment.hasReadAgentFacts) environment.loadAgentName() }
    // A screen asked about something: it goes in the main conversation's reference strip.
    LaunchedEffect(model, ChatsHooks.pendingReference) {
        if (conversationId == null) ChatsHooks.takePendingReference()?.let { reference -> model.refer(reference) }
    }
    LaunchedEffect(model) {
        if (conversationId != null) ChatsHooks.takeOpeningRequest(conversationId)?.let { request -> model.sendSuggestion(request) }
    }

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

    // Whether the end is in view, read when a scroll ends: new messages put in below move the list's own position,
    // so reading it when they arrive would say the end was not shown.
    LaunchedEffect(listState) {
        snapshotFlow { listState.isScrollInProgress }.collect { isScrolling ->
            if (!isScrolling) {
                isShowingEnd = listState.firstVisibleItemIndex == 0 && listState.firstVisibleItemScrollOffset <= 24
                if (isShowingEnd) hasArrivedWhileAway = false
            }
        }
    }
    // New words at the end are followed only while the end is shown and no finger is on the list; otherwise they wait
    // behind the New messages button.
    val lastLine = model.lines.lastOrNull()
    var hasShownFirstArrival by remember { mutableStateOf(false) }
    LaunchedEffect(lastLine?.id, lastLine?.text?.length, model.runningRunIds.size, model.typingVersion) {
        if (!hasShownFirstArrival) {
            hasShownFirstArrival = true
            return@LaunchedEffect
        }
        if (FollowRule.shouldFollow(isShowingEnd, pullTracker.isFingerDown)) {
            listState.scrollToItem(0)
        } else {
            hasArrivedWhileAway = true
        }
    }
    LaunchedEffect(model.isLoaded) { if (model.isLoaded) listState.scrollToItem(0) }

    // After a page of earlier messages is laid in, the message that was first shown goes back near the top, with the end of
    // the new page above it; once more after pictures and cards have settled.
    LaunchedEffect(model.earlierPageCount) {
        val anchorKey = readingAnchorKey ?: return@LaunchedEffect
        suspend fun putBack() {
            val position = items.indexOfFirst { item -> item.key == anchorKey }
            if (position < 0) return
            val leadingCount = listState.layoutInfo.totalItemsCount - items.size - (if (model.isLoaded && model.lines.isNotEmpty()) 1 else 0)
            val index = leadingCount + (items.size - 1 - position)
            listState.scrollToItem(index)
            val info = listState.layoutInfo.visibleItemsInfo.firstOrNull { visible -> visible.key == anchorKey } ?: return
            val spare = (listState.layoutInfo.viewportSize.height - info.size).coerceAtLeast(0)
            listState.scrollToItem(index, -(spare * 0.85f).toInt())
        }
        putBack()
        delay(400)
        putBack()
    }

    CompositionLocalProvider(
        LocalUriHandler provides uriHandler, LocalLinkOpener provides openLink, LocalPictureConversationId provides model.conversationId, LocalChatsEnvironment provides environment,
    ) {
        // In a chat with one other person, the title opens that person's card.
        val directPersonUserId = chat?.takeIf { summary -> !summary.isGroup }?.otherMembers(model.ownUserId)?.firstOrNull()?.userId?.takeIf { userId -> userId.isNotEmpty() }
        TeaNodeScreen(
            title = model.title.ifEmpty { environment.agentTitle },
            onBack = onBack,
            onTitleClick = directPersonUserId?.let { userId -> { navigator.navigate(ContactsRoutes.person(PersonCardTarget.Account(userId))) } },
            titleClickLabel = if (directPersonUserId != null) stringResource(R.string.chats_opens_their_card) else null,
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
                    LazyColumn(Modifier.fillMaxSize().testTag("conversation-list").then(pullTracker.fingerWatcher).nestedScroll(pullTracker.nestedScrollConnection).graphicsLayer { translationY = pullTracker.pullDp * density.density * 0.4f }, state = listState, reverseLayout = true, contentPadding = PaddingValues(horizontal = 16.dp, vertical = 12.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
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
                        if (model.isLoaded && model.lines.isNotEmpty()) {
                            item(key = "earlier-row") {
                                EarlierMessagesRow(
                                    state = EarlierRow.stateOf(model.earlierCount > 0, model.isLoadingEarlier, model.earlierFailureText != null, pullTracker.isArmed),
                                    pullDp = pullTracker.pullDp, failureText = model.earlierFailureText,
                                    onLoad = {
                                        readingAnchorKey = oldestShownKey()
                                        model.loadEarlier()
                                    },
                                )
                            }
                        }
                    }
                    // New messages wait here while the list is not following them; the jump to the end is offered only when the end is well out of sight.
                    when (FollowRule.endButtonFor(hasArrivedWhileAway, isShowingEnd)) {
                        FollowRule.EndButton.NewMessages -> Box(Modifier.align(Alignment.BottomCenter).padding(16.dp)) {
                            NewMessagesButton {
                                hasArrivedWhileAway = false
                                scope.launch { listState.animateScrollToItem(0) }
                            }
                        }
                        FollowRule.EndButton.JumpToEnd -> Box(Modifier.align(Alignment.BottomEnd).padding(16.dp)) {
                            JumpToEndButton { scope.launch { listState.animateScrollToItem(0) } }
                        }
                        FollowRule.EndButton.None -> {}
                    }
                }
                // The message replied to, and whatever else the next message refers to.
                for (reference in model.references) ReferenceBar(reference, model, environment.agentTitle)
                if (model.mentionSuggestions.isNotEmpty()) MentionMenu(model.mentionSuggestions, environment.words) { choice -> model.choose(choice) }
                if (model.todos.isNotEmpty() && (model.runningRunId != null || model.todos.any { todo -> !todo.isDone })) TodoPanel(model.todos)
                val voiceCall = model.agentVoiceCall
                val activeVoiceCall by (com.teanode.app.calls.LocalCallCenter.current?.agentVoice ?: kotlinx.coroutines.flow.MutableStateFlow(null)).collectAsState()
                if (voiceCall != null && voiceCall === activeVoiceCall) com.teanode.app.calls.AgentVoiceCallBar(voiceCall) else Composer(model, environment, dictation)
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
                line.kind == TranscriptLine.Kind.Note && line.detail == "skill_share_call_waiting" -> SkillShareCallWaitingCard(line.text)
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
