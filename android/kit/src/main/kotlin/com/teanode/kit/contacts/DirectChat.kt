package com.teanode.kit.contacts

import com.teanode.kit.Connection
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonPrimitive

/** The call that opens the direct chat with somebody; the same document the iPhone app sends. */
object DirectChatDocuments {
    const val OPEN_DIRECT_CHAT = "mutation (\$userId: String!) { OpenDirectChat(userId: \$userId) { id } }"
}

@Serializable
private class OpenDirectChatAnswer(@SerialName("OpenDirectChat") val chat: Opened) {
    @Serializable
    class Opened(val id: String)
}

/** The person's direct chat with somebody, made when there is none and the same one every time: the id of their conversation for it. */
suspend fun Connection.openDirectChat(userId: String): String =
    execute<OpenDirectChatAnswer>(DirectChatDocuments.OPEN_DIRECT_CHAT, mapOf("userId" to JsonPrimitive(userId))).chat.id
