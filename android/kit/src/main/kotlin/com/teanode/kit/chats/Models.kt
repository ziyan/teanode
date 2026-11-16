package com.teanode.kit.chats

import kotlinx.serialization.Serializable
import java.time.Instant
import java.time.OffsetDateTime

// What the server's GraphQL API answers with, as the app reads it. The field names are the server's
// (internal/api/v1api/apigraph), so they decode without mapping; times stay the server's RFC 3339 text and are read
// with `ServerTime.instant` where a screen needs a moment.

/** AgentConversation is one conversation with the agent: the main one, or a named one. */
@Serializable
data class AgentConversation(
    val id: String,
    /** "main", "named", "run", "goal" or "chat". */
    val kind: String,
    /** For a run, what kind of job it was, such as "mcp". */
    val jobKind: String? = null,
    val title: String? = null,
    val summary: String? = null,
    val lastAt: String? = null,
    val archivedAt: String? = null,
    /** For a goal conversation: the goal, where it stands ("working", "waiting", "met", "dropped"), and its last note. */
    val goal: String? = null,
    val goalState: String? = null,
    val goalNote: String? = null,
    /** For a chat, the people in it and how it stands for the person; null on every other conversation. */
    val chat: ChatSummary? = null,
) {
    val isMain: Boolean get() = kind == "main"
    val isChat: Boolean get() = kind == "chat"
    val isArchived: Boolean get() = !archivedAt.isNullOrEmpty()
}

/** AgentUsage is what one round of a turn cost. */
@Serializable
data class AgentUsage(
    val promptTokens: Int = 0,
    val completionTokens: Int = 0,
    val cost: Double = 0.0,
    /** The prompt tokens read from the provider's cache, where the answer says; a turn's rounds do not. */
    val cacheReadTokens: Int? = null,
) {
    operator fun plus(other: AgentUsage): AgentUsage = AgentUsage(
        promptTokens = promptTokens + other.promptTokens,
        completionTokens = completionTokens + other.completionTokens,
        cost = cost + other.cost,
        cacheReadTokens = if (cacheReadTokens != null || other.cacheReadTokens != null) {
            (cacheReadTokens ?: 0) + (other.cacheReadTokens ?: 0)
        } else {
            null
        },
    )
}

/** AgentReference is something a message points at: a mail thread, a memory page, a finance transaction, a recording, or an earlier message replied to. */
@Serializable
data class AgentReference(
    val itemId: String? = null,
    val threadId: String? = null,
    val subject: String? = null,
    val from: String? = null,
    val path: String? = null,
    val name: String? = null,
    val recordingId: String? = null,
    val recordingTitle: String? = null,
    val photoId: String? = null,
    val photoTitle: String? = null,
    val financeTransactionId: String? = null,
    val postedOn: String? = null,
    val amount: String? = null,
    val currencyCode: String? = null,
    val merchantName: String? = null,
    val description: String? = null,
    val agentMessageId: String? = null,
    /** "user" or "assistant": whose message was replied to. */
    val quotedRole: String? = null,
    val quotedText: String? = null,
) {
    /** Whether this is a reply to an earlier message, not a thing pointed at. */
    val isReply: Boolean get() = !agentMessageId.isNullOrEmpty()

    /** What the reference's chip says. */
    val label: String
        get() {
            if (!recordingTitle.isNullOrEmpty()) return recordingTitle
            if (!photoTitle.isNullOrEmpty()) return photoTitle
            if (!subject.isNullOrEmpty()) return subject
            if (!name.isNullOrEmpty()) return name
            if (!path.isNullOrEmpty()) return path
            if (!merchantName.isNullOrEmpty()) return listOfNotNull(postedOn, merchantName, amount).filter { part -> part.isNotEmpty() }.joinToString(", ")
            return description.orEmpty()
        }

    companion object {
        /** A finance transaction the message asks about. */
        fun transaction(transactionId: String, postedOn: String?, amount: String?, currencyCode: String?, merchantName: String?, description: String?) =
            AgentReference(financeTransactionId = transactionId, postedOn = postedOn, amount = amount, currencyCode = currencyCode, merchantName = merchantName, description = description)

        /** A recording the message asks about. The server reads the title again from the stored recording. */
        fun recording(recordingId: String, recordingTitle: String) = AgentReference(recordingId = recordingId, recordingTitle = recordingTitle)

        /** A photo the message asks about. The server words the title again from the stored photo. */
        fun photo(photoId: String, photoTitle: String) = AgentReference(photoId = photoId, photoTitle = photoTitle)

        /** A page of the agent's knowledge the message asks about. */
        fun knowledgePage(path: String, name: String) = AgentReference(path = path, name = name)

        /** A mail the message asks about. */
        fun mail(itemId: String, threadId: String?, subject: String, from: String) = AgentReference(itemId = itemId, threadId = threadId, subject = subject, from = from)

        /** An earlier message the new one replies to. */
        fun reply(agentMessageId: String, quotedRole: String, quotedText: String) =
            AgentReference(agentMessageId = agentMessageId, quotedRole = quotedRole, quotedText = quotedText)
    }
}

/** AgentTodo is one step of the agent's own list for a conversation. */
@Serializable
data class AgentTodo(val id: String, val text: String, val doneAt: String? = null) {
    val isDone: Boolean get() = !doneAt.isNullOrEmpty()
}

/** AgentToolCall is a tool the agent called in one round. */
@Serializable
data class AgentToolCall(val id: String, val name: String, val arguments: String? = null)

/** AgentAttachment is a file a person handed the agent with a message. */
@Serializable
data class AgentAttachment(
    val id: String,
    val name: String,
    val contentType: String,
    val size: Int,
    /** The file went with the account that held it; the message it came with is kept, and a screen says so where the file was. */
    val isRemoved: Boolean? = null,
    /** For a recording sent as a voice message, how long it runs and its loudness: 48 levels from 0 to 255, as base64. */
    val audioDurationMS: Long? = null,
    val audioWaveform: String? = null,
)

/** AgentMessage is one stored message of a conversation. */
@Serializable
data class AgentMessage(
    val id: String,
    val createdAt: String,
    /** "user", "assistant", "tool", "note" or "compaction". */
    val role: String,
    val content: String = "",
    val name: String? = null,
    val toolCallId: String? = null,
    val toolCalls: List<AgentToolCall>? = null,
    val attachments: List<AgentAttachment>? = null,
    val usage: AgentUsage? = null,
    val references: List<AgentReference>? = null,
    /** The person's and the agent's reactions to it. */
    val reactions: List<AgentMessageReaction>? = null,
    /** Who wrote it, "person" or "agent". In a chat, whoever it was; in a conversation with an agent, the owner and their agent. */
    val authorKind: String? = null,
    val authorUserId: String? = null,
    val authorName: String? = null,
    /** For an agent's message in a chat, the name of its owner. Most agents keep the name "Agent", so whose it is tells two apart. */
    val authorOwnerName: String? = null,
    val authorAvatarUrl: String? = null,
    val authorAvatarColorIndex: Int? = null,
    val authorAgentId: String? = null,
    /** Written by the person reading it, or by their agent. */
    val isMine: Boolean? = null,
    /** When its author last changed what it says, and whether they took it back; its content is then empty. */
    val editedAt: String? = null,
    val isDeleted: Boolean? = null,
    /** For an agent's message in a chat, the run of the turn that wrote it, so that the words drawn while it was being written are replaced by it. */
    val agentRunId: String? = null,
    /** In a chat, a row of what the person's own agent did to answer, which only they read, and not something said there. */
    val isAgentWork: Boolean? = null,
    /** In a chat, the members the message mentions by account, and whether it mentions everybody. */
    val mentionedUserIds: List<String>? = null,
    val isAllMentioned: Boolean? = null,
    /** In a chat, whether the message is a recording the person spoke, and its words: transcriptState is "waiting", "done", "failed" or "off". */
    val isVoiceMessage: Boolean? = null,
    val transcriptText: String? = null,
    val transcriptState: String? = null,
    /** In a chat, the call this message is the line of (it has no words), and the call as it stood when the message was read. */
    val callId: String? = null,
    val call: ChatCallRecord? = null,
)

/** ChatCallMemberRecord is one member of a call as a message of a chat carries it. */
@Serializable
data class ChatCallMemberRecord(
    val userId: String = "",
    val ringState: String = "",
    val joinedAt: String? = null,
    val leftAt: String? = null,
    val isVideoOn: Boolean = false,
) {
    /** Whether they are on the call now. */
    val isOn: Boolean get() = !joinedAt.isNullOrEmpty() && leftAt.isNullOrEmpty()
}

/**
 * ChatCallRecord is a call as a message of a chat carries it, enough to draw its line. The calls part of the app reads
 * the same record for the call itself; this one is what the transcript needs.
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
}

/** AgentInteraction is a card left open for the person: an approval, or a question the agent asked. */
@Serializable
data class AgentInteraction(
    val runId: String,
    val callId: String,
    /** "approval" or "question". */
    val interactionKind: String,
    val toolName: String? = null,
    val interactionText: String,
    val interactionChoices: List<String>? = null,
    val toolRisk: String? = null,
    val createdAt: String? = null,
)

/** AgentMessageReaction is an emoji the person or the agent put on a message, one each at most. */
@Serializable
data class AgentMessageReaction(
    val emoji: String,
    /** "person" or "agent". */
    val reactorKind: String,
    val reactedAt: String? = null,
    /** Which person, in a chat, where more than one can react. */
    val reactorUserId: String? = null,
    val reactorName: String? = null,
) {
    /**
     * One reaction for each reactor, so several people's tell apart. Accounts that are gone name nobody, so the name and
     * the time keep their reactions apart.
     */
    val identity: String get() = "$reactorKind:${reactorUserId.orEmpty()}:${reactorName.orEmpty()}:${reactedAt.orEmpty()}"

    /** Who reacted, when the reaction names a person at all. */
    val namedUserId: String? get() = reactorUserId?.takeIf { text -> text.isNotEmpty() }
}

/** AgentEvent is one event of a running turn, as the conversation's feed streams it. */
@Serializable
data class AgentEvent(
    /**
     * asked, text, message, tool_call, tool_result, confirmation, question, note, titled, navigate, locate, done or
     * error; and in a chat said, edited, deleted, reacted, read, typing or members.
     */
    val kind: String,
    val runId: String = "",
    val sequence: Int = 0,
    val at: String? = null,
    val text: String? = null,
    val tool: String? = null,
    val callId: String? = null,
    val arguments: String? = null,
    val risk: String? = null,
    val note: String? = null,
    val noteKind: String? = null,
    val noteDetail: String? = null,
    val error: String? = null,
    val drawerId: String? = null,
    /** For "reacted": the message, the emoji (empty when taken away) and whose. */
    val messageId: String? = null,
    val emoji: String? = null,
    val reactorKind: String? = null,
    val reactorUserId: String? = null,
    val reactorName: String? = null,
    /** For a chat's "read": whose mark moved, to `messageId`; and for "typing": who is writing and what to call them. */
    val userId: String? = null,
    val name: String? = null,
    /** For a chat's "typing" of an agent, and for the "text" of an answer being written there: the agent, named by `name`. */
    val authorAgentId: String? = null,
    /** With `authorAgentId`, the name of that agent's owner. */
    val authorOwnerName: String? = null,
    /** For a chat's "said" and "edited": the message as the person reads it. */
    val message: AgentMessage? = null,
    /** The message was too long to travel whole: the conversation is read again. */
    val isMessageCut: Boolean? = null,
)

/** ServerTime reads the server's times. */
object ServerTime {
    /**
     * The moment an RFC 3339 time stands for; null for anything else. Go writes up to nine digits of fractions of a
     * second, which `Instant.parse` takes, so they are kept: no screen shows less than a second.
     */
    fun instant(text: String?): Instant? {
        if (text.isNullOrEmpty()) return null
        return try {
            OffsetDateTime.parse(text).toInstant()
        } catch (_: Exception) {
            null
        }
    }

    /**
     * Whether the first RFC 3339 time is later than the second, down to the fractions of a second Go writes. A time
     * that is not one is no time at all: it is never later, and anything is later than it.
     */
    fun isLater(text: String?, otherText: String?): Boolean {
        val moment = instant(text) ?: return false
        val otherMoment = instant(otherText) ?: return true
        return moment.isAfter(otherMoment)
    }
}
