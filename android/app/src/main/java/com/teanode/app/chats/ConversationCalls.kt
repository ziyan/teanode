package com.teanode.app.chats

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.collectAsState
import androidx.compose.material.icons.outlined.Phone
import com.teanode.app.calls.AgentVoiceCall
import com.teanode.app.calls.OpeningRequests
import com.teanode.app.notifications.TurnNotification
import com.teanode.kit.chats.SpokenText
import com.teanode.kit.chats.ask
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

/** The button that starts a call with the agent, in the main conversation, when the server offers voice. */
@Composable
fun ConversationAgentCallButton(model: ConversationModel) {
    if (model.conversationId != null || model.voiceSampleRate == null) return
    androidx.compose.material3.IconButton(onClick = { OpeningRequests.requestCall() }) {
        androidx.compose.material3.Icon(androidx.compose.material.icons.Icons.Outlined.Phone, contentDescription = androidx.compose.ui.res.stringResource(com.teanode.app.R.string.calls_call_agent))
    }
}

/**
 * What the calls part adds to a conversation: a call with the agent started from the icon menu, a widget or the button
 * (`OpeningRequests.isCallRequested`), whose answers are read out as the conversation's feed brings them; and the
 * notification of a turn left running while the app is not in front.
 */
@Composable
fun ConversationCallEffects(model: ConversationModel) {
    val context = LocalContext.current
    val center = LocalCallCenter.current
    val requestPermissions = rememberCallPermissionRequester()
    val lifecycle = androidx.lifecycle.compose.LocalLifecycleOwner.current.lifecycle
    val isRequested by OpeningRequests.isCallRequested.collectAsState()
    val connection = com.teanode.app.session.LocalConnection.current
    val drawerId = ChatsEnvironment.of(context, connection, com.teanode.app.session.LocalAppSession.current).drawerId
    androidx.compose.runtime.DisposableEffect(model, center) {
        model.onEvent = { event ->
            center?.agentVoice?.value?.follow(event.runId, event.kind, event.text, event.note)
            if (!model.isChat) {
                val isInFront = lifecycle.currentState.isAtLeast(androidx.lifecycle.Lifecycle.State.RESUMED)
                val state = when (event.kind) {
                    "asked" -> "working"
                    "confirmation", "question" -> "waiting"
                    "done" -> "done"
                    "error" -> "failed"
                    else -> null
                }
                if (state != null) {
                    if (isInFront) TurnNotification.end(context) else TurnNotification.show(context, "TeaNode", state)
                }
            }
        }
        onDispose { model.onEvent = null }
    }
    androidx.compose.runtime.LaunchedEffect(isRequested, model.voiceSampleRate, model.conversationId) {
        val sampleRate = model.voiceSampleRate ?: return@LaunchedEffect
        if (!isRequested || center == null || model.conversationId != null || model.isChat) return@LaunchedEffect
        OpeningRequests.consume()
        requestPermissions(false) {
            val call = AgentVoiceCall(
                context = context, connection = connection, sampleRate = sampleRate, agentName = model.title.ifEmpty { "TeaNode" },
                sendSpoken = { text, interrupted ->
                    runCatching {
                        connection.ask(
                            text, model.conversationId, drawerId, surface = com.teanode.kit.chats.Surface.VOICE,
                            interruptedAnswer = interrupted?.let { found -> com.teanode.kit.chats.InterruptedAnswer(found.heardText, found.unheardText) },
                        ).runId
                    }.getOrNull()
                },
                sentencesOf = { text -> SpokenText.sentences(text).sentences },
                spokenTextOf = { markdown -> SpokenText.of(markdown) },
            )
            center.beginAgentVoice(call)
            call.start()
        }
    }
}
