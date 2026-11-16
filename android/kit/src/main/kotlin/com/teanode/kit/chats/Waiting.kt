package com.teanode.kit.chats

import com.teanode.kit.Connection
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonPrimitive

/** AgentWaitingInteraction is a question or an approval the agent put to the person and still waits on. */
@Serializable
data class AgentWaitingInteraction(
    val id: String,
    val conversationId: String,
    /** "question" or "approval". */
    val interactionKind: String,
    val interactionText: String,
    val createdAt: String,
)

/** AgentWaiting is what waits for the person across every conversation. */
@Serializable
data class AgentWaiting(val waitingCount: Int, val interactions: List<AgentWaitingInteraction>) {
    /** How many wait in each conversation, among those read. */
    val countByConversation: Map<String, Int> get() = interactions.groupingBy { interaction -> interaction.conversationId }.eachCount()
}

@Serializable
private class WaitingAnswer(@SerialName("ReadAgentWaiting") val waiting: AgentWaiting)

/** How many questions and approvals wait for the person, and the newest `first` of them. */
suspend fun Connection.waiting(first: Int = 5): AgentWaiting =
    execute(ChatDocuments.WAITING, WaitingAnswer.serializer(), mapOf("first" to JsonPrimitive(first))).waiting
