package com.teanode.app.chats

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AddComment
import androidx.compose.material.icons.outlined.Archive
import androidx.compose.material.icons.outlined.ChatBubbleOutline
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Edit
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material.icons.outlined.Group
import androidx.compose.material.icons.outlined.NotificationsOff
import androidx.compose.material.icons.outlined.AlternateEmail
import androidx.compose.material.icons.outlined.PersonAdd
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.Star
import androidx.compose.material.icons.outlined.Unarchive
import androidx.compose.material.icons.outlined.QuestionAnswer
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.relativeTimeText
import com.teanode.app.ui.components.TeaNodeRow
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import com.teanode.kit.chats.AgentConversation
import com.teanode.kit.chats.ChatNotificationLevel
import com.teanode.kit.chats.GroupChatDraft
import com.teanode.kit.chats.ServerTime
import com.teanode.kit.chats.conversations
import com.teanode.kit.chats.createGroupChat
import com.teanode.kit.chats.deleteConversation
import com.teanode.kit.chats.openDirectChat
import com.teanode.kit.chats.setChatNotificationLevel
import com.teanode.kit.chats.setMainConversation
import com.teanode.kit.chats.startConversation
import com.teanode.kit.chats.updateConversation
import com.teanode.kit.chats.waiting
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/**
 * ConversationsList is the first screen of Chats: the main conversation first, then the chats with people and the
 * others by when they were last used; search across what was said; the archived ones on request; and renaming,
 * archiving, deleting and making one the main conversation. A conversation chosen is opened by [onChoose]; null is the
 * main one.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ConversationsList(onChoose: (AgentConversation?) -> Unit) {
    val context = androidx.compose.ui.platform.LocalContext.current
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    val snackbar = LocalSnackbar.current
    val environment = remember(connection) { ChatsEnvironment.of(context, connection, session) }
    val scope = rememberCoroutineScope()
    var conversations by remember { mutableStateOf<List<AgentConversation>>(emptyList()) }
    var waitingByConversation by remember { mutableStateOf<Map<String, Int>>(emptyMap()) }
    var archived by remember { mutableStateOf<List<AgentConversation>?>(null) }
    var isShowingArchived by remember { mutableStateOf(false) }
    var isSearching by remember { mutableStateOf(false) }
    var words by remember { mutableStateOf("") }
    var found by remember { mutableStateOf<List<AgentConversation>?>(null) }
    var isLoaded by remember { mutableStateOf(false) }
    var isRefreshing by remember { mutableStateOf(false) }
    var problemText by remember { mutableStateOf<String?>(null) }
    var renaming by remember { mutableStateOf<AgentConversation?>(null) }
    var deleting by remember { mutableStateOf<AgentConversation?>(null) }
    var isMessagingSomeone by remember { mutableStateOf(false) }
    var isStartingGroup by remember { mutableStateOf(false) }
    var isNewMenuOpen by remember { mutableStateOf(false) }
    var reloadCount by remember { mutableStateOf(0) }

    fun report(error: Throwable) {
        if (conversations.isEmpty()) problemText = session.describeLoadProblem(error) else snackbar.showError(error)
    }

    suspend fun load() {
        try {
            conversations = connection.conversations()
            conversations.forEach { conversation -> environment.remember(conversation) }
            // The archived ones too, if they were read before, so an opened list of them is not left as it was.
            if (archived != null) archived = runCatching { connection.conversations(isArchived = true) }.getOrNull()?.filter { conversation -> !conversation.isMain } ?: archived
            problemText = null
            // The list reads without it; only the marks are missing.
            waitingByConversation = runCatching { connection.waiting(first = 50) }.getOrNull()?.countByConversation ?: emptyMap()
        } catch (cancelled: kotlinx.coroutines.CancellationException) {
            throw cancelled
        } catch (error: Throwable) {
            report(error)
        }
        isLoaded = true
    }

    suspend fun act(change: suspend () -> Unit) {
        try {
            change()
            archived = null
            if (isShowingArchived) archived = runCatching { connection.conversations(isArchived = true) }.getOrNull()?.filter { conversation -> !conversation.isMain } ?: emptyList()
            load()
        } catch (cancelled: kotlinx.coroutines.CancellationException) {
            throw cancelled
        } catch (error: Throwable) {
            report(error)
        }
    }

    // Read when the list comes into view, which includes coming back from a conversation, and every half minute while it stays.
    LaunchedEffect(reloadCount) {
        environment.loadAgentName()
        while (true) {
            load()
            environment.refreshChats()
            delay(30_000)
        }
    }
    LaunchedEffect(words) {
        val typed = words.trim()
        if (typed.isEmpty()) {
            found = null
            return@LaunchedEffect
        }
        // A quarter of a second after the last key, as the drawer waits.
        delay(250)
        found = runCatching { connection.conversations(matching = typed) }.getOrNull()
    }
    LaunchedEffect(isShowingArchived) {
        if (isShowingArchived && archived == null) archived = runCatching { connection.conversations(isArchived = true) }.getOrNull()?.filter { conversation -> !conversation.isMain } ?: emptyList()
    }

    TeaNodeScreen(
        title = stringResource(R.string.chats_title),
        actions = {
            IconButton(onClick = { isSearching = !isSearching; if (!isSearching) words = "" }) { Icon(if (isSearching) Icons.Outlined.Close else Icons.Outlined.Search, contentDescription = stringResource(R.string.chats_search_conversations)) }
        },
        floatingActionButton = {
            if (environment.canChat || environment.isAgentOn) {
                androidx.compose.foundation.layout.Box {
                    FloatingActionButton(onClick = {
                        if (environment.canChat) isNewMenuOpen = true else scope.launch { runCatching { connection.startConversation(null) }.onSuccess { started -> onChoose(started) }.onFailure { failure -> report(failure) } }
                    }) { Icon(Icons.Outlined.AddComment, contentDescription = stringResource(R.string.chats_new_conversation)) }
                    DropdownMenu(expanded = isNewMenuOpen, onDismissRequest = { isNewMenuOpen = false }) {
                        if (environment.isAgentOn) {
                            DropdownMenuItem(
                                text = { Text(stringResource(R.string.chats_new_side_chat)) }, leadingIcon = { Icon(Icons.Outlined.QuestionAnswer, null) },
                                onClick = {
                                    isNewMenuOpen = false
                                    scope.launch { runCatching { connection.startConversation(null) }.onSuccess { started -> onChoose(started) }.onFailure { failure -> report(failure) } }
                                },
                            )
                        }
                        DropdownMenuItem(text = { Text(stringResource(R.string.chats_message_someone)) }, leadingIcon = { Icon(Icons.Outlined.PersonAdd, null) }, onClick = { isNewMenuOpen = false; isMessagingSomeone = true })
                        DropdownMenuItem(text = { Text(stringResource(R.string.chats_new_group)) }, leadingIcon = { Icon(Icons.Outlined.Group, null) }, onClick = { isNewMenuOpen = false; isStartingGroup = true })
                    }
                }
            }
        },
    ) { contentPadding ->
        Column(Modifier.padding(contentPadding).fillMaxSize()) {
            if (isSearching) {
                OutlinedTextField(
                    value = words, onValueChange = { words = it }, singleLine = true, label = { Text(stringResource(R.string.chats_search_conversations)) },
                    keyboardOptions = KeyboardOptions.Default, modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
                )
            }
            val failure = problemText
            when {
                failure != null && conversations.isEmpty() -> ErrorState(failure, onRetry = { problemText = null; isLoaded = false; reloadCount += 1 })
                !isLoaded -> LoadingState()
                else -> PullToRefreshBox(isRefreshing = isRefreshing, onRefresh = { scope.launch { isRefreshing = true; load(); isRefreshing = false } }, modifier = Modifier.fillMaxSize()) {
                    LazyColumn(Modifier.fillMaxSize(), contentPadding = androidx.compose.foundation.layout.PaddingValues(bottom = 88.dp)) {
                        val results = found
                        if (results != null) {
                            if (results.isEmpty()) item { Text(stringResource(R.string.chats_nothing_matches), Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
                            items(results, key = { conversation -> "found-" + conversation.id }) { conversation ->
                                ConversationRow(conversation, environment, waitingByConversation, onChoose, onRename = { renaming = it }, onDelete = { deleting = it }, act = ::act, connection = connection)
                            }
                        } else {
                            // A person with no agent has no main chat to open.
                            if (environment.hasAgent) {
                                item(key = "main") {
                                    val main = conversations.firstOrNull { conversation -> conversation.isMain }
                                    TeaNodeRow(
                                        headline = stringResource(R.string.chats_main_chat), leadingIcon = Icons.Outlined.ChatBubbleOutline, onClick = { onChoose(null) },
                                        supporting = if (main != null && waitingByConversation[main.id] != null) stringResource(R.string.chats_waiting_for_you) else null,
                                    )
                                }
                            }
                            val chats = conversations.filter { conversation -> conversation.isChat }
                            if (chats.isNotEmpty()) {
                                item(key = "people-header") { TeaNodeSectionHeader(stringResource(R.string.chats_people)) }
                                items(chats, key = { conversation -> conversation.id }) { conversation ->
                                    ConversationRow(conversation, environment, waitingByConversation, onChoose, onRename = { renaming = it }, onDelete = { deleting = it }, act = ::act, connection = connection)
                                }
                            }
                            val sideChats = conversations.filter { conversation -> !conversation.isMain && !conversation.isChat }
                            item(key = "side-header") { TeaNodeSectionHeader(stringResource(R.string.chats_side_chats)) }
                            if (sideChats.isEmpty()) item(key = "side-empty") { Text(stringResource(R.string.chats_no_side_chats), Modifier.padding(horizontal = 16.dp, vertical = 8.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
                            items(sideChats, key = { conversation -> conversation.id }) { conversation ->
                                ConversationRow(conversation, environment, waitingByConversation, onChoose, onRename = { renaming = it }, onDelete = { deleting = it }, act = ::act, connection = connection)
                            }
                            item(key = "archived") {
                                HorizontalDivider(Modifier.padding(top = 8.dp))
                                Row(Modifier.fillMaxWidth().clickable { isShowingArchived = !isShowingArchived }.padding(horizontal = 16.dp, vertical = 14.dp), verticalAlignment = Alignment.CenterVertically) {
                                    Text(stringResource(R.string.chats_archived_chats), Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
                                    Icon(if (isShowingArchived) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore, contentDescription = null)
                                }
                            }
                            if (isShowingArchived) {
                                val shown = archived
                                if (shown == null) {
                                    item(key = "archived-loading") { CircularProgressIndicator(Modifier.padding(16.dp).size(24.dp)) }
                                } else {
                                    if (shown.isEmpty()) item(key = "archived-empty") { Text(stringResource(R.string.chats_nothing_archived), Modifier.padding(horizontal = 16.dp, vertical = 8.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
                                    items(shown, key = { conversation -> "archived-" + conversation.id }) { conversation ->
                                        ConversationRow(conversation, environment, waitingByConversation, onChoose, onRename = { renaming = it }, onDelete = { deleting = it }, act = ::act, connection = connection)
                                    }
                                }
                            }
                            if (conversations.none { conversation -> !conversation.isMain } && !environment.hasAgent && chats.isEmpty()) {
                                item(key = "nothing") { EmptyState(Icons.Outlined.ChatBubbleOutline, stringResource(R.string.chats_no_conversations_yet)) }
                            }
                        }
                    }
                }
            }
        }
    }

    renaming?.let { conversation ->
        var newTitle by remember(conversation.id) { mutableStateOf(conversation.title.orEmpty()) }
        AlertDialog(
            onDismissRequest = { renaming = null }, title = { Text(stringResource(R.string.chats_rename)) },
            text = { OutlinedTextField(value = newTitle, onValueChange = { newTitle = it }, label = { Text(stringResource(R.string.chats_title_field)) }, singleLine = true) },
            confirmButton = {
                TextButton(
                    onClick = {
                        val title = newTitle.trim()
                        renaming = null
                        if (title.isNotEmpty()) scope.launch { act { connection.updateConversation(conversation.id, title = title) } }
                    },
                    enabled = newTitle.trim().isNotEmpty(),
                ) { Text(stringResource(R.string.chats_save)) }
            },
            dismissButton = { TextButton(onClick = { renaming = null }) { Text(stringResource(R.string.chats_cancel)) } },
        )
    }
    deleting?.let { conversation ->
        ConfirmationDialog(
            title = stringResource(R.string.chats_delete_conversation_title), confirmLabel = stringResource(R.string.chats_delete),
            text = stringResource(R.string.chats_delete_conversation_detail, ConversationModel.titleOfConversation(conversation, environment.agentTitle, environment.ownUserId, environment.words)),
            isDestructive = true,
            onConfirm = {
                deleting = null
                scope.launch { act { connection.deleteConversation(conversation.id) } }
            },
            onDismiss = { deleting = null },
        )
    }
    if (isMessagingSomeone) {
        ChatPeopleSheet(connection, ChatPeoplePurpose.Direct, onDismiss = { isMessagingSomeone = false }) { userIds, _ ->
            val userId = userIds.firstOrNull() ?: return@ChatPeopleSheet
            val id = connection.openDirectChat(userId)
            load()
            onChoose(conversations.firstOrNull { conversation -> conversation.id == id } ?: AgentConversation(id = id, kind = "chat", title = null, summary = null, lastAt = null))
        }
    }
    if (isStartingGroup) {
        ChatPeopleSheet(connection, ChatPeoplePurpose.Group, onDismiss = { isStartingGroup = false }) { userIds, groupName ->
            val id = connection.createGroupChat(GroupChatDraft(groupName, userIds.toSet()))
            load()
            onChoose(conversations.firstOrNull { conversation -> conversation.id == id } ?: AgentConversation(id = id, kind = "chat", title = null, summary = null, lastAt = null))
        }
    }
}

/**
 * A conversation in the list. A chat has its people's pictures, its name, bold with a count while messages wait in it,
 * and a mark when the person is not told of all of it; a side chat has its name, a line of what it is about and when it
 * was last used. Everything else is in the menu at its end: a chat has mute and archive, a side chat rename, make main,
 * archive and delete.
 */
@Composable
private fun ConversationRow(
    conversation: AgentConversation,
    environment: ChatsEnvironment,
    waitingByConversation: Map<String, Int>,
    onChoose: (AgentConversation?) -> Unit,
    onRename: (AgentConversation) -> Unit,
    onDelete: (AgentConversation) -> Unit,
    act: suspend (suspend () -> Unit) -> Unit,
    connection: com.teanode.kit.Connection,
) {
    val scope = rememberCoroutineScope()
    var isMenuOpen by remember { mutableStateOf(false) }
    val name = ConversationModel.titleOfConversation(conversation, environment.agentTitle, environment.ownUserId, environment.words)
    val lastAt = ServerTime.instant(conversation.lastAt)?.toEpochMilli()?.let { moment -> relativeTimeText(moment) }
    val chat = conversation.chat
    val unreadCount = chat?.unreadCount ?: 0
    val trailing: @Composable () -> Unit = {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            // A chat not told of everything says so: the @ for mentions alone, a bell crossed out for nothing.
            val level = chat?.level
            if (level != null && level != ChatNotificationLevel.All) {
                Icon(
                    if (level == ChatNotificationLevel.Mentions) Icons.Outlined.AlternateEmail else Icons.Outlined.NotificationsOff,
                    contentDescription = stringResource(if (level == ChatNotificationLevel.Mentions) R.string.chats_only_mentions else R.string.chats_notifications_off),
                    modifier = Modifier.size(18.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            if (unreadCount > 0) UnreadBadge(unreadCount)
            androidx.compose.foundation.layout.Box {
                IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.chats_conversation_options, name)) }
                DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                    if (chat != null) {
                        for (level in ChatNotificationLevel.entries) {
                            DropdownMenuItem(
                                text = { Text(notificationLevelName(level) + if (chat.level == level) "  ✓" else "") },
                                onClick = {
                                    isMenuOpen = false
                                    scope.launch { act { connection.setChatNotificationLevel(conversation.id, level) } }
                                },
                            )
                        }
                        HorizontalDivider()
                    } else {
                        DropdownMenuItem(text = { Text(stringResource(R.string.chats_rename)) }, leadingIcon = { Icon(Icons.Outlined.Edit, null) }, onClick = { isMenuOpen = false; onRename(conversation) })
                        if (!conversation.isMain && !conversation.isArchived) {
                            DropdownMenuItem(
                                text = { Text(stringResource(R.string.chats_make_main)) }, leadingIcon = { Icon(Icons.Outlined.Star, null) },
                                onClick = { isMenuOpen = false; scope.launch { act { connection.setMainConversation(conversation.id) } } },
                            )
                        }
                    }
                    if (!conversation.isMain) {
                        DropdownMenuItem(
                            text = { Text(stringResource(if (conversation.isArchived) R.string.chats_unarchive else R.string.chats_archive)) },
                            leadingIcon = { Icon(if (conversation.isArchived) Icons.Outlined.Unarchive else Icons.Outlined.Archive, null) },
                            onClick = { isMenuOpen = false; scope.launch { act { connection.updateConversation(conversation.id, isArchived = !conversation.isArchived) } } },
                        )
                        if (chat == null) {
                            HorizontalDivider()
                            DropdownMenuItem(
                                text = { Text(stringResource(R.string.chats_delete), color = MaterialTheme.colorScheme.error) },
                                leadingIcon = { Icon(Icons.Outlined.Delete, null, tint = MaterialTheme.colorScheme.error) },
                                onClick = { isMenuOpen = false; onDelete(conversation) },
                            )
                        }
                    }
                }
            }
        }
    }
    if (chat != null) {
        TeaNodeRow(
            headline = name, onClick = { onChoose(conversation) }, supporting = lastAt, isHeadlineBold = unreadCount > 0,
            leading = { ChatAvatars(chat, environment.ownUserId, 36) }, trailing = trailing,
        )
    } else {
        val waiting = waitingByConversation[conversation.id] != null
        val summary = conversation.summary.orEmpty()
        TeaNodeRow(
            headline = name, onClick = { onChoose(if (conversation.isMain) null else conversation) },
            supporting = listOfNotNull(if (waiting) stringResource(R.string.chats_waiting_for_you) else null, summary.ifEmpty { null }, lastAt).joinToString("\n").ifEmpty { null },
            supportingMaxLines = 3, leadingIcon = Icons.Outlined.ChatBubbleOutline, trailing = trailing,
        )
    }
}
