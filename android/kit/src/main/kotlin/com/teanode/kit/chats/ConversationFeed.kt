package com.teanode.kit.chats

import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.get
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.mapNotNull
import kotlinx.serialization.json.JsonPrimitive

/**
 * ConversationFeed streams a conversation's events as they happen: a turn's words as they are written, its tools, its
 * cards. A turn already running when the feed opens is replayed from its start, so a screen that opens mid-turn shows
 * all of it. A chat's feed carries its own events beside these: a message said, edited or deleted, a member's read
 * mark, someone typing, and a reaction by name (`AgentEvent`).
 *
 * The stream ends with an error when the connection drops (or goes silent for longer than the foundation's watchdog
 * allows); the screen reloads the conversation and opens a new feed.
 */
object ConversationFeed {
    fun events(connection: Connection, conversationId: String): Flow<AgentEvent> =
        connection.subscribe(ChatDocuments.CONVERSATION_EVENTS, mapOf("conversationId" to JsonPrimitive(conversationId))).mapNotNull { data ->
            val event = data["AgentConversationEvents"] ?: return@mapNotNull null
            TeaNodeJson.decodeFromJsonElement(AgentEvent.serializer(), event)
        }
}
