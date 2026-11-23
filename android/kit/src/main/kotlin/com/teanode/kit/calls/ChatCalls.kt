package com.teanode.kit.calls

import com.teanode.kit.Connection
import com.teanode.kit.notifications.AgentPhone
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive
import java.time.Instant
import java.time.OffsetDateTime

// Calls in chats: a person in a chat with people calls it, the others are rung, and whoever answers talks through the
// server's media server. The operations are in internal/api/v1api/apigraph/chat_call.go and
// docs/subsystems/chat-calls.md tells how a device goes on a call. This file is what the server answers, as the app
// reads it, and the operations. The feeds are in ChatCallFeed.kt, the order of the signaling in ChatCallSignaling.kt,
// and the words in ChatCallWords.kt.

/** ChatCallTime reads the times the server writes (RFC 3339, with up to nine digits of fractions of a second). */
object ChatCallTime {
    /** The moment a time stands for, in milliseconds since 1970; null for anything else. */
    fun epochMS(text: String?): Long? {
        if (text.isNullOrEmpty()) return null
        return try {
            OffsetDateTime.parse(dropFractions(text)).toInstant().toEpochMilli()
        } catch (_: Exception) {
            null
        }
    }

    /** Fractions of a second are dropped: no screen shows less than a second. */
    private fun dropFractions(text: String): String {
        val dot = text.indexOf('.')
        if (dot < 0) return text
        var end = dot + 1
        while (end < text.length && text[end].isDigit()) end++
        return text.removeRange(dot, end)
    }

    /** A moment in the server's form, whole seconds, UTC. */
    fun text(epochMS: Long): String = Instant.ofEpochMilli(epochMS - Math.floorMod(epochMS, 1000L)).toString()
}

/** ChatCallMemberView is one person's part in a call. */
@Serializable
data class ChatCallMemberView(
    val userId: String,
    val name: String = "",
    val avatarUrl: String? = null,
    val avatarColorIndex: Int = 0,
    /** ringing, answered, declined, missed, or none for whoever started it. */
    val ringState: String = "",
    /** Whether they are on the call now. */
    val isOn: Boolean = false,
    val isVideoOn: Boolean = false,
    /** Their microphone is off, shown beside their name. */
    val isMuted: Boolean = false,
    val joinedAt: String? = null,
    val leftAt: String? = null,
) {
    val id: String get() = userId
}

/**
 * ChatCallView is a call as the person asking sees it: what the server's ChatCallView says, which is also what
 * ChatCallEvents tells of each change.
 */
@Serializable
data class ChatCallView(
    val id: String,
    /** The person's own conversation for the chat, the chat's name as they see it, and whether it is a group. */
    val conversationId: String = "",
    val chatTitle: String = "",
    val isGroup: Boolean = false,
    val startedByUserId: String? = null,
    val startedByName: String = "",
    val startedAt: String = "",
    val answeredAt: String? = null,
    val endedAt: String? = null,
    /** ended, missed, declined or failed; empty while the call goes. */
    val endReason: String = "",
    val isGoing: Boolean = false,
    /** How long it went from the first answer, to its end or to now. */
    val durationMs: Long = 0,
    /**
     * The person's part: ringing, answered, declined, missed, or none for whoever started it; empty when they had
     * none. [isAskerOn] says they are on it now.
     */
    val askerRingState: String = "",
    val isAskerOn: Boolean = false,
    /** Who asked for a transcript (null when nobody did). */
    val transcriptionAskedByUserId: String? = null,
    /** How the transcript stands (waiting, done or failed, empty before the call ends). */
    val transcriptState: String = "",
    /** The summary's message in the chat, once there is one. */
    val summaryMessageId: String? = null,
    val members: List<ChatCallMemberView> = emptyList(),
) {
    val answeredEpochMS: Long? get() = ChatCallTime.epochMS(answeredAt)

    /** How many people are on the call now. */
    val onCount: Int get() = members.count { member -> member.isOn }

    /** The people on the call now. */
    val onMembers: List<ChatCallMemberView> get() = members.filter { member -> member.isOn }

    fun member(userId: String): ChatCallMemberView? = members.firstOrNull { member -> member.userId == userId }

    /** Whether the person who started the call is the one asking. */
    fun isStartedBy(userId: String?): Boolean {
        if (userId.isNullOrEmpty()) return askerRingState == "none"
        return startedByUserId == userId
    }
}

/** ChatCallTranscriptSegment is one line of a transcribed call under its speaker, timed from the start of the call. */
@Serializable
data class ChatCallTranscriptSegment(
    val id: String,
    val callId: String = "",
    val userId: String? = null,
    val speakerName: String = "",
    val startMs: Long = 0,
    val endMs: Long = 0,
    val text: String = "",
) {
    /** Where in the call the line was said, as "4:12". */
    val startText: String get() = ChatCallDuration.text(startMs)
}

/** ChatCallDetailView is a call with its transcript. */
@Serializable
data class ChatCallDetailView(
    val call: ChatCallView,
    val transcript: List<ChatCallTranscriptSegment> = emptyList(),
    /** The recording the call became, when the person may open it: their own (in Recordings), or one shared with them. */
    val recordingId: String? = null,
    val isRecordingOwned: Boolean = false,
)

/** ChatCallSignalView is what the media server tells the device: an offer to answer, or who is speaking now (no offer). */
@Serializable
data class ChatCallSignalView(
    val offerSdp: String = "",
    val speakingUserId: String? = null,
)

/**
 * ChatCallMemberRecord is one person's part in a call as a chat's message carries it (`call` on a message): the call
 * as it stood when the message was read, without the names that [ChatCallMemberView] adds.
 */
@Serializable
data class ChatCallMemberRecord(
    val userId: String,
    val ringState: String = "",
    val joinedAt: String? = null,
    val leftAt: String? = null,
    val isVideoOn: Boolean = false,
) {
    /** Whether they are on the call: joined, and not left since. */
    val isOn: Boolean get() = joinedAt != null && leftAt == null
}

/**
 * ChatCallRecord is the call a message in a chat is the line of, as the message carries it. It does not change after
 * the message is read: the app keeps the latest [ChatCallView] of each call it hears of, and draws the line from that
 * when it has one.
 */
@Serializable
data class ChatCallRecord(
    val id: String,
    val startedByUserId: String? = null,
    val startedAt: String = "",
    val answeredAt: String? = null,
    val endedAt: String? = null,
    val endReason: String = "",
    val transcriptionAskedByUserId: String? = null,
    val transcriptState: String = "",
    val summaryMessageId: String? = null,
    val members: List<ChatCallMemberRecord> = emptyList(),
) {
    /** Whether the call has not ended. */
    val isGoing: Boolean get() = endedAt == null

    /** How many people are on the call now. */
    val onCount: Int get() = members.count { member -> member.isOn }

    /** How long the call went from its first answer, to its end or to now. Zero for a call nobody answered. */
    fun durationMs(nowEpochMS: Long = System.currentTimeMillis()): Long {
        val answered = ChatCallTime.epochMS(answeredAt) ?: return 0
        val end = ChatCallTime.epochMS(endedAt) ?: nowEpochMS
        return maxOf(0, end - answered)
    }

    /** The person's part: ringing, answered, declined, missed, or none for whoever started it; empty when they had none. */
    fun ringState(userId: String?): String {
        if (userId == null) return ""
        return members.firstOrNull { member -> member.userId == userId }?.ringState ?: ""
    }
}

// MARK: The documents

object ChatCallDocuments {
    /** What every read of a call answers with. Written out in full, since the server's test of the app's documents fills in one level of shared fields. */
    private const val CALL_FIELDS = """
        id conversationId chatTitle isGroup startedByUserId startedByName startedAt answeredAt endedAt endReason isGoing durationMs
        askerRingState isAskerOn transcriptionAskedByUserId transcriptState summaryMessageId
        members { userId name avatarUrl avatarColorIndex ringState isOn isVideoOn isMuted joinedAt leftAt }
        """

    const val START_CHAT_CALL = """
        mutation (${'$'}conversationId: String!, ${'$'}isVideoOn: Boolean, ${'$'}userIds: [String!]) {
          StartChatCall(conversationId: ${'$'}conversationId, isVideoOn: ${'$'}isVideoOn, userIds: ${'$'}userIds) { $CALL_FIELDS }
        }
        """
    const val JOIN_CHAT_CALL = """
        mutation (${'$'}callId: String!, ${'$'}agentPhoneId: String) { JoinChatCall(callId: ${'$'}callId, agentPhoneId: ${'$'}agentPhoneId) { $CALL_FIELDS } }
        """
    const val DECLINE_CHAT_CALL = """
        mutation (${'$'}callId: String!, ${'$'}agentPhoneId: String) {
          DeclineChatCall(callId: ${'$'}callId, agentPhoneId: ${'$'}agentPhoneId) { $CALL_FIELDS }
        }
        """
    const val LEAVE_CHAT_CALL = """
        mutation (${'$'}callId: String!) { LeaveChatCall(callId: ${'$'}callId) { $CALL_FIELDS } }
        """
    const val JOIN_CHAT_CALL_MEDIA = """
        mutation (${'$'}callId: String!, ${'$'}offerSdp: String!) {
          JoinChatCallMedia(callId: ${'$'}callId, offerSdp: ${'$'}offerSdp, shouldTellSpeaker: true) { answerSdp }
        }
        """
    const val ANSWER_CHAT_CALL_MEDIA = """
        mutation (${'$'}callId: String!, ${'$'}answerSdp: String!) { AnswerChatCallMedia(callId: ${'$'}callId, answerSdp: ${'$'}answerSdp) }
        """
    const val SET_CHAT_CALL_VIDEO = """
        mutation (${'$'}callId: String!, ${'$'}isVideoOn: Boolean!) {
          SetChatCallVideo(callId: ${'$'}callId, isVideoOn: ${'$'}isVideoOn) { $CALL_FIELDS }
        }
        """
    const val SET_CHAT_CALL_FEATURED = """
        mutation (${'$'}callId: String!, ${'$'}userId: String) { SetChatCallFeatured(callId: ${'$'}callId, userId: ${'$'}userId) }
        """
    const val SET_CHAT_CALL_MUTED = """
        mutation (${'$'}callId: String!, ${'$'}isMuted: Boolean!) {
          SetChatCallMuted(callId: ${'$'}callId, isMuted: ${'$'}isMuted) { $CALL_FIELDS }
        }
        """
    const val ASK_CHAT_CALL_TRANSCRIPTION = """
        mutation (${'$'}callId: String!) { AskChatCallTranscription(callId: ${'$'}callId) { $CALL_FIELDS } }
        """
    const val LIST_GOING_CHAT_CALLS = """
        query { ListGoingChatCalls { $CALL_FIELDS } }
        """
    const val READ_GOING_CHAT_CALL = """
        query (${'$'}conversationId: String!) { ReadGoingChatCall(conversationId: ${'$'}conversationId) { $CALL_FIELDS } }
        """
    const val LIST_CHAT_CALLS = """
        query (${'$'}conversationId: String!, ${'$'}first: Int, ${'$'}before: DateTime) {
          ListChatCalls(conversationId: ${'$'}conversationId, first: ${'$'}first, before: ${'$'}before) { $CALL_FIELDS }
        }
        """
    const val READ_CHAT_CALL = """
        query (${'$'}callId: String!) {
          ReadChatCall(callId: ${'$'}callId) {
            call { $CALL_FIELDS }
            transcript { id callId userId speakerName startMs endMs text }
            recordingId isRecordingOwned
          }
        }
        """
    const val CHAT_CALL_EVENTS = """
        subscription { ChatCallEvents { $CALL_FIELDS } }
        """
    const val CHAT_CALL_SIGNALS = """
        subscription (${'$'}callId: String!) { ChatCallSignals(callId: ${'$'}callId) { offerSdp speakingUserId } }
        """
    const val UPDATE_PHONE_RINGING = """
        mutation (${'$'}phoneId: String!, ${'$'}voipPushRegistration: String) {
          UpdateAgentPhone(phoneId: ${'$'}phoneId, voipPushRegistration: ${'$'}voipPushRegistration) { id notificationKinds canBeNotified }
        }
        """
}

// MARK: The operations

@Serializable
private class StartAnswer(@SerialName("StartChatCall") val call: ChatCallView)

@Serializable
private class JoinAnswer(@SerialName("JoinChatCall") val call: ChatCallView)

@Serializable
private class DeclineAnswer(@SerialName("DeclineChatCall") val call: ChatCallView)

@Serializable
private class LeaveAnswer(@SerialName("LeaveChatCall") val call: ChatCallView)

@Serializable
private class JoinMediaAnswer(@SerialName("JoinChatCallMedia") val media: MediaAnswer) {
    @Serializable
    class MediaAnswer(val answerSdp: String)
}

@Serializable
private class AnswerMediaAnswer(@SerialName("AnswerChatCallMedia") val isAccepted: Boolean)

@Serializable
private class VideoAnswer(@SerialName("SetChatCallVideo") val call: ChatCallView)

@Serializable
private class FeaturedAnswer(@SerialName("SetChatCallFeatured") val isSet: Boolean)

@Serializable
private class MutedAnswer(@SerialName("SetChatCallMuted") val call: ChatCallView)

@Serializable
private class TranscriptionAnswer(@SerialName("AskChatCallTranscription") val call: ChatCallView)

@Serializable
private class GoingAnswer(@SerialName("ListGoingChatCalls") val calls: List<ChatCallView>)

@Serializable
private class GoingInChatAnswer(@SerialName("ReadGoingChatCall") val call: ChatCallView? = null)

@Serializable
private class ListAnswer(@SerialName("ListChatCalls") val calls: List<ChatCallView>)

@Serializable
private class ReadAnswer(@SerialName("ReadChatCall") val detail: ChatCallDetailView)

@Serializable
private class RingingAnswer(@SerialName("UpdateAgentPhone") val phone: AgentPhone)

/**
 * Starts a call in the chat, ringing everybody else in it, or only the people in [userIds]. When a call is going in
 * the chat already, it is that one, to join.
 */
suspend fun Connection.startChatCall(conversationId: String, isVideoOn: Boolean = false, userIds: List<String>? = null): ChatCallView {
    val variables = mutableMapOf<String, JsonElement>("conversationId" to JsonPrimitive(conversationId), "isVideoOn" to JsonPrimitive(isVideoOn))
    if (userIds != null) variables["userIds"] = JsonArray(userIds.map { userId -> JsonPrimitive(userId) })
    return execute(ChatCallDocuments.START_CHAT_CALL, StartAnswer.serializer(), variables).call
}

private fun answerVariables(callId: String, agentPhoneId: String?): Map<String, JsonElement> {
    val variables = mutableMapOf<String, JsonElement>("callId" to JsonPrimitive(callId))
    if (!agentPhoneId.isNullOrEmpty()) variables["agentPhoneId"] = JsonPrimitive(agentPhoneId)
    return variables
}

/**
 * Answers a call ringing the person, or joins one going in their chat. The device then joins its media with
 * [joinChatCallMedia]. The phone that answers names itself, so that it is not told to stop ringing.
 */
suspend fun Connection.joinChatCall(callId: String, agentPhoneId: String? = null): ChatCallView =
    execute(ChatCallDocuments.JOIN_CHAT_CALL, JoinAnswer.serializer(), answerVariables(callId, agentPhoneId)).call

/** Says no to a call ringing the person; the phone that does names itself, as when it answers. */
suspend fun Connection.declineChatCall(callId: String, agentPhoneId: String? = null): ChatCallView =
    execute(ChatCallDocuments.DECLINE_CHAT_CALL, DeclineAnswer.serializer(), answerVariables(callId, agentPhoneId)).call

/** Takes the person off a call; it ends when nobody is left on it. */
suspend fun Connection.leaveChatCall(callId: String): ChatCallView =
    execute(ChatCallDocuments.LEAVE_CHAT_CALL, LeaveAnswer.serializer(), mapOf("callId" to JsonPrimitive(callId))).call

/**
 * Connects the device to the call's audio and video: sends its offer and returns the server's answer. The signals are
 * opened first ([ChatCallFeed.signals]), since the server's later offers come there.
 */
suspend fun Connection.joinChatCallMedia(callId: String, offerSdp: String): String =
    execute(
        ChatCallDocuments.JOIN_CHAT_CALL_MEDIA, JoinMediaAnswer.serializer(),
        mapOf("callId" to JsonPrimitive(callId), "offerSdp" to JsonPrimitive(offerSdp)),
    ).media.answerSdp

/** The device's answer to the latest offer the signals brought. */
suspend fun Connection.answerChatCallMedia(callId: String, answerSdp: String): Boolean =
    execute(
        ChatCallDocuments.ANSWER_CHAT_CALL_MEDIA, AnswerMediaAnswer.serializer(),
        mapOf("callId" to JsonPrimitive(callId), "answerSdp" to JsonPrimitive(answerSdp)),
    ).isAccepted

/** Tells the others the person's camera is on or off; the device starts or stops sending its picture itself. */
suspend fun Connection.setChatCallVideo(callId: String, isVideoOn: Boolean): ChatCallView =
    execute(
        ChatCallDocuments.SET_CHAT_CALL_VIDEO, VideoAnswer.serializer(),
        mapOf("callId" to JsonPrimitive(callId), "isVideoOn" to JsonPrimitive(isVideoOn)),
    ).call

/**
 * Chooses whose camera the device is sent whole, the others' coming small once several are on: the person given, or
 * whoever is speaking for null.
 */
suspend fun Connection.setChatCallFeatured(callId: String, userId: String?) {
    execute(
        ChatCallDocuments.SET_CHAT_CALL_FEATURED, FeaturedAnswer.serializer(),
        mapOf("callId" to JsonPrimitive(callId), "userId" to (userId?.let { JsonPrimitive(it) } ?: JsonNull)),
    )
}

/** Tells the others the person's microphone is off or on; the device stops or starts sending its sound itself. */
suspend fun Connection.setChatCallMuted(callId: String, isMuted: Boolean): ChatCallView =
    execute(
        ChatCallDocuments.SET_CHAT_CALL_MUTED, MutedAnswer.serializer(),
        mapOf("callId" to JsonPrimitive(callId), "isMuted" to JsonPrimitive(isMuted)),
    ).call

/** Asks for the call to be transcribed. Everybody on it is told, and every voice on it is transcribed. */
suspend fun Connection.askChatCallTranscription(callId: String): ChatCallView =
    execute(ChatCallDocuments.ASK_CHAT_CALL_TRANSCRIPTION, TranscriptionAnswer.serializer(), mapOf("callId" to JsonPrimitive(callId))).call

/** The calls going that the person has a part in: ringing them, or that they are on. A phone that opens reads these to ring. */
suspend fun Connection.listGoingChatCalls(): List<ChatCallView> =
    execute(ChatCallDocuments.LIST_GOING_CHAT_CALLS, GoingAnswer.serializer()).calls

/** The call going in a chat, null when none is: what an open chat shows as a banner to join, whether or not the call rang the person. */
suspend fun Connection.readGoingChatCall(conversationId: String): ChatCallView? =
    execute(ChatCallDocuments.READ_GOING_CHAT_CALL, GoingInChatAnswer.serializer(), mapOf("conversationId" to JsonPrimitive(conversationId))).call

/** A chat's calls, newest first, a page of [first] (the server's 50 when left out) started before the time given when there is one. */
suspend fun Connection.listChatCalls(conversationId: String, first: Int? = null, beforeEpochMS: Long? = null): List<ChatCallView> {
    val variables = mutableMapOf<String, JsonElement>("conversationId" to JsonPrimitive(conversationId))
    if (first != null) variables["first"] = JsonPrimitive(first)
    if (beforeEpochMS != null) variables["before"] = JsonPrimitive(Instant.ofEpochMilli(beforeEpochMS).toString())
    return execute(ChatCallDocuments.LIST_CHAT_CALLS, ListAnswer.serializer(), variables).calls
}

/** One call, with its transcript when it was transcribed. */
suspend fun Connection.readChatCall(callId: String): ChatCallDetailView =
    execute(ChatCallDocuments.READ_CHAT_CALL, ReadAnswer.serializer(), mapOf("callId" to JsonPrimitive(callId))).detail

/** Gives the server the registration the relay made for the phone's ringing token. An empty one takes the registration away. */
suspend fun Connection.updatePhoneRinging(phoneId: String, voipPushRegistration: String): AgentPhone =
    execute(
        ChatCallDocuments.UPDATE_PHONE_RINGING, RingingAnswer.serializer(),
        mapOf("phoneId" to JsonPrimitive(phoneId), "voipPushRegistration" to JsonPrimitive(voipPushRegistration)),
    ).phone
