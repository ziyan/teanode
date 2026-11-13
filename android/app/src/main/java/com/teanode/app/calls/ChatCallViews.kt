package com.teanode.app.calls

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Call
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.PhoneCallback
import androidx.compose.material.icons.filled.PhoneDisabled
import androidx.compose.material.icons.filled.PhoneMissed
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material.icons.outlined.Call
import androidx.compose.material.icons.outlined.Groups
import androidx.compose.material.icons.outlined.Videocam
import androidx.compose.material3.Button
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnectionOrNull
import com.teanode.app.ui.components.AvatarView
import com.teanode.kit.calls.ChatCallLine
import com.teanode.kit.calls.ChatCallLineKind
import com.teanode.kit.calls.ChatCallRecord
import kotlinx.coroutines.launch

// The pieces of a call that sit in a chat: the call button in its bar, the sheet that chooses who to call, the banner
// of a chat with a call going, and a call's line in the conversation. The chats part places them; they take what they
// need as plain values so that they are drawn the same in the app and in a screenshot.

/** ChatCallPerson is a person of a chat a call can ring. */
data class ChatCallPerson(val userId: String, val displayName: String, val avatarUrl: String?, val avatarColorIndex: Int)

/**
 * ChatCallButton is the call button in the bar of a chat with people: a menu of a voice call, a video call, and in a
 * group, choosing who to call. A call going in the chat is joined instead (the call center does it for a chat with one
 * going).
 *
 * @param onChoose opens the sheet that chooses who a call rings; the chat shows [ChatCallPeopleSheet].
 * @param canCall false while the chat cannot be written to, or has no conversation yet.
 */
@Composable
fun ChatCallButton(
    conversationId: String?,
    title: String,
    isGroup: Boolean,
    canCall: Boolean,
    onChoose: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val center = LocalCallCenter.current
    val requestPermissions = rememberCallPermissionRequester()
    var isOpen by remember { mutableStateOf(false) }
    fun call(isVideoOn: Boolean) {
        val id = conversationId ?: return
        requestPermissions(isVideoOn) { center?.startCall(id, title, isVideoOn) }
    }
    Column(modifier) {
        IconButton(onClick = { isOpen = true }, enabled = canCall && conversationId != null) {
            Icon(Icons.Outlined.Call, contentDescription = stringResource(R.string.calls_start_a_call))
        }
        DropdownMenu(expanded = isOpen, onDismissRequest = { isOpen = false }) {
            DropdownMenuItem(
                text = { Text(stringResource(R.string.calls_voice_call)) },
                leadingIcon = { Icon(Icons.Outlined.Call, contentDescription = null) },
                onClick = {
                    isOpen = false
                    call(isVideoOn = false)
                },
            )
            DropdownMenuItem(
                text = { Text(stringResource(R.string.calls_video_call)) },
                leadingIcon = { Icon(Icons.Outlined.Videocam, contentDescription = null) },
                onClick = {
                    isOpen = false
                    call(isVideoOn = true)
                },
            )
            if (isGroup) {
                DropdownMenuItem(
                    text = { Text(stringResource(R.string.calls_choose_who_to_call)) },
                    leadingIcon = { Icon(Icons.Outlined.Groups, contentDescription = null) },
                    onClick = {
                        isOpen = false
                        onChoose()
                    },
                )
            }
        }
    }
}

/**
 * ChatCallPeopleSheet chooses who in a group a call rings, then starts it with the camera off or on. Everybody is
 * chosen to begin with; whoever is left out is not rung, and can still join from the chat.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ChatCallPeopleSheet(
    people: List<ChatCallPerson>,
    onCall: (userIds: List<String>, isVideoOn: Boolean) -> Unit,
    onDismiss: () -> Unit,
    modifier: Modifier = Modifier,
) {
    var chosen by rememberSaveable { mutableStateOf(people.map { person -> person.userId }.toSet().toList()) }
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = sheetState, modifier = modifier) {
        ChatCallPeopleContent(
            people = people, chosenUserIds = chosen,
            onToggle = { userId -> chosen = if (userId in chosen) chosen - userId else chosen + userId },
            onCall = { isVideoOn -> onCall(people.map { person -> person.userId }.filter { userId -> userId in chosen }, isVideoOn) },
        )
    }
}

/** What the sheet of people holds: the title, the people with their marks, the footer, and the two ways to call. */
@Composable
fun ChatCallPeopleContent(people: List<ChatCallPerson>, chosenUserIds: List<String>, onToggle: (String) -> Unit, onCall: (isVideoOn: Boolean) -> Unit) {
    Column(Modifier.padding(bottom = 16.dp)) {
        Text(stringResource(R.string.calls_who_to_call), style = MaterialTheme.typography.titleLarge, modifier = Modifier.padding(horizontal = 24.dp, vertical = 8.dp))
        people.forEach { person ->
            val isChosen = person.userId in chosenUserIds
            Row(
                Modifier.fillMaxWidth().clickable { onToggle(person.userId) }.semantics { selected = isChosen }.padding(horizontal = 24.dp, vertical = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                AvatarView(name = person.displayName, avatarUrl = person.avatarUrl, colorIndex = person.avatarColorIndex, size = 36.dp)
                Text(person.displayName, Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
                if (isChosen) Icon(Icons.Filled.Check, contentDescription = null, tint = MaterialTheme.colorScheme.primary)
            }
        }
        Text(
            stringResource(R.string.calls_not_rung_footer),
            style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(horizontal = 24.dp, vertical = 8.dp),
        )
        Row(Modifier.fillMaxWidth().padding(horizontal = 24.dp, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Button(onClick = { onCall(false) }, enabled = chosenUserIds.isNotEmpty(), modifier = Modifier.weight(1f)) {
                Icon(Icons.Outlined.Call, contentDescription = null, modifier = Modifier.size(18.dp))
                Text(stringResource(R.string.calls_voice_call), Modifier.padding(start = 8.dp))
            }
            OutlinedButton(onClick = { onCall(true) }, enabled = chosenUserIds.isNotEmpty(), modifier = Modifier.weight(1f)) {
                Icon(Icons.Outlined.Videocam, contentDescription = null, modifier = Modifier.size(18.dp))
                Text(stringResource(R.string.calls_video_call), Modifier.padding(start = 8.dp))
            }
        }
    }
}

/**
 * ChatCallBanner is the line at the top of a chat that has a call going: how many are on it, and Join. It is hidden
 * while this phone is on that call. It reads the calls the center heard of, and asks the server once when the chat
 * opens.
 */
@Composable
fun ChatCallBanner(conversationId: String?, title: String, modifier: Modifier = Modifier) {
    val center = LocalCallCenter.current ?: return
    val connection = LocalConnectionOrNull.current
    LaunchedEffect(conversationId, connection?.accountKey) {
        if (conversationId != null && connection != null) center.readGoingCall(conversationId, connection)
    }
    val goingCallIds by center.goingCallIds.collectAsState()
    val views by center.callViews.collectAsState()
    val session by center.session.collectAsState()
    val callId = conversationId?.let { id -> goingCallIds[id] } ?: return
    val call = views[callId]?.takeIf { view -> view.isGoing } ?: return
    if (session?.callId == callId) return
    val requestPermissions = rememberCallPermissionRequester()
    ChatCallBannerContent(call.onCount, onJoin = { requestPermissions(false) { center.join(callId, conversationId, title) } }, modifier = modifier)
}

/** The banner as drawn: a phone, how many are on the call, and Join. */
@Composable
fun ChatCallBannerContent(onCount: Int, onJoin: () -> Unit, modifier: Modifier = Modifier) {
    Surface(modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 4.dp), shape = RoundedCornerShape(14.dp), color = MaterialTheme.colorScheme.surfaceContainerHigh) {
        Row(Modifier.padding(start = 12.dp, end = 8.dp, top = 6.dp, bottom = 6.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Icon(Icons.Filled.Call, contentDescription = null, modifier = Modifier.size(18.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
            Text(
                stringResource(R.string.calls_banner_going, onCount),
                Modifier.weight(1f), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Button(onClick = onJoin) { Text(stringResource(R.string.calls_join)) }
        }
    }
}

// MARK: A call's line in a conversation

/** The symbol of a call's line. */
fun iconOf(kind: ChatCallLineKind): ImageVector = when (kind) {
    ChatCallLineKind.Going -> Icons.Filled.Call
    is ChatCallLineKind.Ended -> Icons.Outlined.Call
    ChatCallLineKind.Missed, ChatCallLineKind.NoAnswer -> Icons.Filled.PhoneMissed
    ChatCallLineKind.Declined -> Icons.Filled.PhoneDisabled
    ChatCallLineKind.Failed -> Icons.Filled.Warning
}

/** What a call's line says, in the person's language. */
@Composable
fun titleOf(kind: ChatCallLineKind): String = when (kind) {
    ChatCallLineKind.Going -> stringResource(R.string.calls_line_going)
    is ChatCallLineKind.Ended -> stringResource(R.string.calls_line_ended, kind.durationText)
    ChatCallLineKind.Missed -> stringResource(R.string.calls_line_missed)
    ChatCallLineKind.NoAnswer -> stringResource(R.string.calls_line_no_answer)
    ChatCallLineKind.Declined -> stringResource(R.string.calls_line_declined)
    ChatCallLineKind.Failed -> stringResource(R.string.calls_line_failed)
}

/**
 * ChatCallLineCard is a call's line in the conversation, drawn from the call: how it went, who was on it, Join while it
 * goes, and the transcript once there is one.
 */
@Composable
fun ChatCallLineCard(
    line: ChatCallLine,
    detailText: String?,
    isOnThisCall: Boolean,
    onJoin: () -> Unit,
    onOpenTranscript: () -> Unit,
    modifier: Modifier = Modifier,
) {
    // A missed call is red, with its symbol and words as well.
    val isProblem = line.kind == ChatCallLineKind.Missed || line.kind == ChatCallLineKind.NoAnswer || line.kind == ChatCallLineKind.Failed
    Surface(modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 4.dp), shape = RoundedCornerShape(18.dp), color = MaterialTheme.colorScheme.surfaceContainer) {
        Row(Modifier.padding(horizontal = 14.dp, vertical = 12.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Icon(iconOf(line.kind), contentDescription = null, tint = if (isProblem) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.onSurface)
            Column(Modifier.weight(1f)) {
                Text(titleOf(line.kind), style = MaterialTheme.typography.titleSmall.copy(fontWeight = FontWeight.SemiBold), color = if (isProblem) MaterialTheme.colorScheme.error else Color.Unspecified)
                if (detailText != null) Text(detailText, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (line.isGoing) {
                if (!isOnThisCall) OutlinedButton(onClick = onJoin) { Text(stringResource(R.string.calls_join)) }
            } else if (line.isTranscriptDone) {
                OutlinedButton(onClick = onOpenTranscript) { Text(stringResource(R.string.calls_transcript)) }
            }
        }
    }
}

/**
 * A call's line as the conversation draws it: from the newest the app has heard of the call, or from the call as the
 * message carried it. [nameOf] gives a person's name in the chat; the person's own name is "You".
 */
@Composable
fun ChatCallLineCard(
    callId: String,
    record: ChatCallRecord?,
    ownUserId: String?,
    conversationId: String,
    title: String,
    nameOf: (userId: String) -> String?,
    onOpenTranscript: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val center = LocalCallCenter.current
    val views by (center?.callViews ?: remember { kotlinx.coroutines.flow.MutableStateFlow(emptyMap()) }).collectAsState()
    val session by (center?.session ?: remember { kotlinx.coroutines.flow.MutableStateFlow(null) }).collectAsState()
    val requestPermissions = rememberCallPermissionRequester()
    val view = views[callId]
    val line = when {
        view != null -> ChatCallLine.of(view, ownUserId)
        record != null -> ChatCallLine.of(record, ownUserId)
        else -> return
    }
    val you = stringResource(R.string.calls_you)
    fun name(userId: String?): String? = when {
        userId.isNullOrEmpty() -> null
        userId == ownUserId -> you
        else -> nameOf(userId)
    }
    val starterId = view?.startedByUserId ?: record?.startedByUserId
    val detailText = when (line.kind) {
        ChatCallLineKind.Missed -> name(starterId)?.let { caller -> stringResource(R.string.calls_line_called, caller) }
        ChatCallLineKind.NoAnswer, ChatCallLineKind.Declined, ChatCallLineKind.Failed -> null
        ChatCallLineKind.Going, is ChatCallLineKind.Ended -> {
            val userIds = if (view != null) view.members.filter { member -> member.joinedAt != null }.map { member -> member.userId } else record?.members?.filter { member -> member.joinedAt != null }?.map { member -> member.userId }.orEmpty()
            userIds.mapNotNull { userId -> name(userId) }.takeIf { names -> names.isNotEmpty() }?.let { names -> joinNames(names) }
        }
    }
    ChatCallLineCard(
        line = line, detailText = detailText, isOnThisCall = session?.callId == callId,
        onJoin = { requestPermissions(false) { center?.join(callId, conversationId, title) } },
        onOpenTranscript = onOpenTranscript, modifier = modifier,
    )
}

/** "Robin, Sam and Alex": the names of the people on a call, as a list. */
fun joinNames(names: List<String>): String = when (names.size) {
    0 -> ""
    1 -> names[0]
    else -> android.icu.text.ListFormatter.getInstance(java.util.Locale.getDefault()).format(names)
}
