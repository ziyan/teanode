package com.teanode.app.chats

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Check
import androidx.compose.material.icons.outlined.Group
import androidx.compose.material.icons.outlined.PersonAdd
import androidx.compose.material.icons.outlined.PersonRemove
import androidx.compose.material.icons.outlined.Settings
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.AvatarView
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.kit.Connection
import com.teanode.kit.chats.ChatMember
import com.teanode.kit.chats.ChatNotificationLevel
import com.teanode.kit.chats.ChatPerson
import com.teanode.kit.chats.ChatSummary
import com.teanode.kit.chats.chatPeople
import kotlinx.coroutines.launch

/** What each notification level is called. */
@Composable
fun notificationLevelName(level: ChatNotificationLevel): String = when (level) {
    ChatNotificationLevel.All -> stringResource(R.string.chats_level_all)
    ChatNotificationLevel.Mentions -> stringResource(R.string.chats_level_mentions)
    ChatNotificationLevel.Off -> stringResource(R.string.chats_level_off)
}

/** The three levels as rows with the chosen one marked, as Settings chooses among a few. */
@Composable
fun NotificationLevelRows(level: ChatNotificationLevel, onChoose: (ChatNotificationLevel) -> Unit) {
    Column {
        for (choice in ChatNotificationLevel.entries) {
            Row(
                Modifier.fillMaxWidth().heightIn(min = 48.dp).clickable { onChoose(choice) }.padding(horizontal = 8.dp), verticalAlignment = Alignment.CenterVertically,
            ) {
                RadioButton(selected = level == choice, onClick = { onChoose(choice) })
                Text(notificationLevelName(choice), style = MaterialTheme.typography.bodyLarge)
            }
        }
    }
}

/**
 * ChatMembersSheet lists who is in a chat. A group's admins can add people and take them out; a circle's chat follows
 * the circle, so there it is a list. Below the people are what the person's phone is told of here, and the two
 * switches for their agent.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ChatMembersSheet(model: ConversationModel, environment: ChatsEnvironment, onOpenPerson: (String) -> Unit, onOpenSettings: () -> Unit, onDismiss: () -> Unit) {
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    var isAdding by remember { mutableStateOf(false) }
    var removing by remember { mutableStateOf<ChatMember?>(null) }
    val chat = model.chat
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        LazyColumn(Modifier.navigationBarsPadding().padding(bottom = 16.dp)) {
            item { Text(stringResource(R.string.chats_members), style = MaterialTheme.typography.titleLarge, modifier = Modifier.padding(horizontal = 20.dp, vertical = 8.dp)) }
            if (chat != null) {
                val canChange = chat.canChangeMembers(model.ownUserId)
                items(chat.presentMembers, key = { member -> member.identity }) { member ->
                    MemberRow(member, chat, model.ownUserId, canRemove = canChange && member.userId != model.ownUserId, environment, onOpen = {
                        if (member.userId.isNotEmpty() && member.userId != model.ownUserId) {
                            onDismiss()
                            onOpenPerson(member.userId)
                        }
                    }, onRemove = { removing = member })
                }
                if (canChange) {
                    item {
                        Row(
                            Modifier.fillMaxWidth().heightIn(min = 52.dp).clickable { isAdding = true }.padding(horizontal = 20.dp), verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(16.dp),
                        ) {
                            Icon(Icons.Outlined.PersonAdd, contentDescription = null, tint = MaterialTheme.colorScheme.primary)
                            Text(stringResource(R.string.chats_add_people), color = MaterialTheme.colorScheme.primary)
                        }
                    }
                }
                val own = chat.member(model.ownUserId.orEmpty())
                if (own != null && !own.isRemoved && !chat.isRemoved) {
                    item {
                        Column(Modifier.padding(horizontal = 20.dp, vertical = 8.dp)) {
                            Text(stringResource(R.string.chats_notifications), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
                            NotificationLevelRows(chat.level) { level -> model.setNotificationLevel(level) }
                            Text(stringResource(R.string.chats_notifications_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                    item { AgentSection(own, environment, model, onOpenSettings = { onDismiss(); onOpenSettings() }) }
                }
            }
        }
    }
    if (isAdding) {
        ChatPeopleSheet(
            connection = model.connection, purpose = ChatPeoplePurpose.Add, leftOut = chat?.presentMembers?.map { member -> member.userId }?.toSet() ?: emptySet(),
            onDismiss = { isAdding = false },
        ) { userIds, _ -> model.addMembers(userIds) }
    }
    removing?.let { person ->
        com.teanode.app.ui.components.ConfirmationDialog(
            title = stringResource(R.string.chats_remove_person_title), confirmLabel = stringResource(R.string.chats_remove),
            text = stringResource(R.string.chats_remove_person_message, ChatSummary.nameOf(person, environment.words.deletedAccountNote)),
            isDestructive = true,
            onConfirm = {
                removing = null
                scope.launch { model.removeMember(person)?.let { problem -> snackbar.showProblem(problem) } }
            },
            onDismiss = { removing = null },
        )
    }
}

@Composable
private fun AgentSection(own: ChatMember, environment: ChatsEnvironment, model: ConversationModel, onOpenSettings: () -> Unit) {
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    Column(Modifier.padding(horizontal = 20.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(stringResource(R.string.chats_your_agent_in_chat), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
        if (own.isAgentActive == false) {
            // Switches for an agent that is off would be accepted and do nothing, so the section says it is off, as the dashboard does.
            Text(stringResource(R.string.chats_your_agent_is_off), style = MaterialTheme.typography.bodyLarge)
            if (environment.hasAgent) {
                TextButton(onClick = onOpenSettings) {
                    Icon(Icons.Outlined.Settings, contentDescription = null)
                    Text(stringResource(R.string.chats_open_settings), modifier = Modifier.padding(start = 8.dp))
                }
            }
        } else {
            SwitchRow(stringResource(R.string.chats_agent_remembers), own.isAgentReading) { isOn -> scope.launch { model.setAgentReading(isOn)?.let { problem -> snackbar.showProblem(problem) } } }
            SwitchRow(stringResource(R.string.chats_others_may_ask), own.isAgentAskableByOthers == true) { isOn -> scope.launch { model.setAgentAskableByOthers(isOn)?.let { problem -> snackbar.showProblem(problem) } } }
            Text(stringResource(R.string.chats_agent_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
private fun SwitchRow(title: String, isOn: Boolean, onChange: (Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth().heightIn(min = 48.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(title, Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
        Switch(checked = isOn, onCheckedChange = onChange)
    }
}

@Composable
private fun MemberRow(member: ChatMember, chat: ChatSummary, ownUserId: String?, canRemove: Boolean, environment: ChatsEnvironment, onOpen: () -> Unit, onRemove: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().heightIn(min = 56.dp).clickable(enabled = member.userId.isNotEmpty() && member.userId != ownUserId, onClick = onOpen).padding(horizontal = 20.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        AvatarView(member.displayName, member.avatarUrl, member.avatarColorIndex, size = 36.dp)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            // The person's own row says so, as a circle's members do and as the dashboard's list of a chat's members does.
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(ChatSummary.nameOf(member, environment.words.deletedAccountNote), style = MaterialTheme.typography.bodyLarge)
                if (member.userId == ownUserId) Text(stringResource(R.string.chats_you), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (member.isAdmin && chat.isGroup) Text(stringResource(R.string.chats_admin), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            // The person's own agent is said by the switches below.
            if (member.userId != ownUserId) {
                if (member.isAgentReading) Text(stringResource(R.string.chats_their_agent_remembers), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                if (member.isAgentAskableByOthers == true) Text(stringResource(R.string.chats_others_may_ask_theirs), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        if (canRemove) IconButton(onClick = onRemove) { Icon(Icons.Outlined.PersonRemove, contentDescription = stringResource(R.string.chats_remove), tint = MaterialTheme.colorScheme.error) }
    }
}

/** What the one sheet for choosing people is for: to message one, to start a group with several and a name, or to add to a group that is there. */
enum class ChatPeoplePurpose { Direct, Group, Add }

/**
 * ChatPeopleSheet is the one sheet for choosing people. Those in a group already are left out of the list. A refusal
 * from [perform] is said in a snackbar and the sheet stays open.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ChatPeopleSheet(
    connection: Connection,
    purpose: ChatPeoplePurpose,
    onDismiss: () -> Unit,
    leftOut: Set<String> = emptySet(),
    perform: suspend (userIds: List<String>, groupName: String) -> Unit,
) {
    val scope = rememberCoroutineScope()
    val snackbar = LocalSnackbar.current
    var people by remember { mutableStateOf<List<ChatPerson>?>(null) }
    var loadProblemText by remember { mutableStateOf<String?>(null) }
    var chosen by remember { mutableStateOf(setOf<String>()) }
    var groupName by remember { mutableStateOf("") }
    var isBusy by remember { mutableStateOf(false) }
    var attempt by remember { mutableStateOf(0) }
    val session = com.teanode.app.session.LocalAppSession.current
    LaunchedEffect(attempt) {
        try {
            people = connection.chatPeople()
            loadProblemText = null
        } catch (failure: kotlinx.coroutines.CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            loadProblemText = session.describeLoadProblem(failure)
        }
    }
    val choices = (people ?: emptyList()).filter { person -> person.userId !in leftOut }
    val canSubmit = chosen.isNotEmpty() && (purpose != ChatPeoplePurpose.Group || groupName.trim().isNotEmpty())
    fun submit(userIds: Set<String>) {
        if (isBusy || userIds.isEmpty()) return
        isBusy = true
        scope.launch {
            try {
                perform(userIds.sorted(), groupName.trim())
                onDismiss()
            } catch (failure: kotlinx.coroutines.CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                snackbar.showError(failure)
            } finally {
                isBusy = false
            }
        }
    }
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.navigationBarsPadding().padding(bottom = 16.dp)) {
            Row(Modifier.fillMaxWidth().padding(horizontal = 20.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(
                    stringResource(
                        when (purpose) {
                            ChatPeoplePurpose.Direct -> R.string.chats_message_someone
                            ChatPeoplePurpose.Group -> R.string.chats_new_group
                            ChatPeoplePurpose.Add -> R.string.chats_add_people
                        },
                    ),
                    style = MaterialTheme.typography.titleLarge, modifier = Modifier.weight(1f),
                )
                if (purpose != ChatPeoplePurpose.Direct) {
                    Button(onClick = { submit(chosen) }, enabled = canSubmit && !isBusy) { Text(stringResource(if (purpose == ChatPeoplePurpose.Group) R.string.chats_create else R.string.chats_add)) }
                }
            }
            if (purpose == ChatPeoplePurpose.Group) {
                OutlinedTextField(
                    value = groupName, onValueChange = { groupName = it }, label = { Text(stringResource(R.string.chats_group_name)) }, singleLine = true,
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 20.dp, vertical = 8.dp),
                )
            }
            Box(Modifier.fillMaxWidth().heightIn(min = 160.dp, max = 460.dp)) {
                val failure = loadProblemText
                when {
                    failure != null && people == null -> ErrorState(failure, onRetry = { attempt += 1 })
                    people == null -> LoadingState()
                    choices.isEmpty() && purpose == ChatPeoplePurpose.Add && !people.isNullOrEmpty() ->
                        EmptyState(icon = Icons.Outlined.Group, title = stringResource(R.string.chats_nobody_left_to_add), description = stringResource(R.string.chats_everyone_already_in_group))
                    choices.isEmpty() ->
                        EmptyState(icon = Icons.Outlined.Group, title = stringResource(R.string.chats_no_one_to_message), description = stringResource(R.string.chats_no_one_to_message_detail))
                    else -> LazyColumn(Modifier.fillMaxSize()) {
                        items(choices, key = { person -> person.userId }) { person ->
                            Row(
                                Modifier.fillMaxWidth().heightIn(min = 56.dp).clickable(enabled = !isBusy) {
                                    if (purpose == ChatPeoplePurpose.Direct) {
                                        chosen = setOf(person.userId)
                                        submit(chosen)
                                    } else {
                                        chosen = if (person.userId in chosen) chosen - person.userId else chosen + person.userId
                                    }
                                }.padding(horizontal = 20.dp, vertical = 6.dp),
                                verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp),
                            ) {
                                AvatarView(person.displayName, person.avatarUrl, person.avatarColorIndex, size = 36.dp)
                                Column(Modifier.weight(1f)) {
                                    Text(person.displayName, style = MaterialTheme.typography.bodyLarge)
                                    if (person.displayName != person.username) Text(person.username, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                                }
                                if (purpose != ChatPeoplePurpose.Direct && person.userId in chosen) Icon(Icons.Outlined.Check, contentDescription = stringResource(R.string.chats_selected), tint = MaterialTheme.colorScheme.primary)
                            }
                            HorizontalDivider(Modifier.padding(start = 68.dp))
                        }
                    }
                }
            }
        }
    }
}
