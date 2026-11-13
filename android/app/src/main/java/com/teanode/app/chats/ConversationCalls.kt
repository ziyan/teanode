package com.teanode.app.chats

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.runtime.saveable.rememberSaveable
import com.teanode.app.calls.CallsRoutes
import com.teanode.app.calls.ChatCallBanner
import com.teanode.app.calls.ChatCallButton
import com.teanode.app.calls.ChatCallLineCard
import com.teanode.app.calls.ChatCallPeopleSheet
import com.teanode.app.calls.ChatCallPerson
import com.teanode.app.calls.LocalCallCenter
import com.teanode.app.calls.rememberCallPermissionRequester
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.kit.calls.ChatCallMemberRecord
import com.teanode.kit.calls.ChatCallRecord
import com.teanode.kit.chats.TranscriptLine

// Where calls sit in a conversation: the call button in the bar of a chat with people, the banner of a chat with a call
// going, and a call's line among the messages. The pieces are the calls part's (`com.teanode.app.calls`); this
// hands them what a conversation knows.

/** The call button of a chat with people, with the sheet that chooses who a group's call rings. */
@Composable
fun ConversationCallButton(model: ConversationModel) {
    val center = LocalCallCenter.current
    val requestPermissions = rememberCallPermissionRequester()
    var isChoosing by rememberSaveable { mutableStateOf(false) }
    val chat = model.chat
    val people = (chat?.presentMembers ?: emptyList())
        .filter { member -> !member.isDeletedAccount && member.userId.isNotEmpty() && member.userId != model.ownUserId }
        .map { member -> ChatCallPerson(member.userId, member.displayName, member.avatarUrl, member.avatarColorIndex ?: 0) }
    ChatCallButton(
        conversationId = model.conversationId, title = model.title, isGroup = chat?.isGroup == true,
        canCall = model.canWrite, onChoose = { isChoosing = true },
    )
    if (isChoosing) {
        ChatCallPeopleSheet(
            people = people,
            onCall = { userIds, isVideoOn ->
                isChoosing = false
                val id = model.conversationId ?: return@ChatCallPeopleSheet
                requestPermissions(isVideoOn) { center?.startCall(id, model.title, isVideoOn, userIds) }
            },
            onDismiss = { isChoosing = false },
        )
    }
}

/** The line at the top of a chat that has a call going: how many are on it, and Join. */
@Composable
fun ConversationCallBanner(model: ConversationModel) {
    ChatCallBanner(conversationId = model.conversationId, title = model.title)
}

/** A call's line among the messages, drawn from the call. */
@Composable
fun ConversationCallLine(line: TranscriptLine, model: ConversationModel) {
    val navigator = LocalAppNavigator.current
    val chat = model.chat
    val record = line.chatCall?.let { call ->
        ChatCallRecord(
            id = call.id, startedByUserId = call.startedByUserId, startedAt = call.startedAt, answeredAt = call.answeredAt, endedAt = call.endedAt,
            endReason = call.endReason, transcriptionAskedByUserId = call.transcriptionAskedByUserId, transcriptState = call.transcriptState,
            summaryMessageId = call.summaryMessageId,
            members = call.members.map { member -> ChatCallMemberRecord(member.userId, member.ringState, member.joinedAt, member.leftAt, member.isVideoOn) },
        )
    }
    val title = model.title
    ChatCallLineCard(
        callId = line.chatCallId, record = record, ownUserId = model.ownUserId, conversationId = model.conversationId.orEmpty(), title = title,
        nameOf = { userId -> chat?.member(userId)?.displayName },
        onOpenTranscript = { navigator.navigate(CallsRoutes.transcript(line.chatCallId, title)) },
    )
}
