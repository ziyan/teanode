package com.teanode.kit.calls

import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeJson
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.mapNotNull
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive

/**
 * ChatCallFeed streams what happens to calls: [events] is each change to a call in a chat the person is in (it rings
 * them, somebody joined or left, a camera went on, it ended), and [signals] is what the media server tells the device
 * on a call (offers, to be answered in order).
 *
 * Like the conversation feed, a stream ends with an error when the connection drops, and its owner opens a new one.
 */
object ChatCallFeed {
    /** Each change to a call in the person's chats, as they see it. */
    fun events(connection: Connection): Flow<ChatCallView> =
        connection.subscribe(ChatCallDocuments.CHAT_CALL_EVENTS).mapNotNull { data -> read(data, "ChatCallEvents", ChatCallView.serializer()) }

    /** The offers of the media server's for a call the person is on, one at a time. Opened before the device joins the call's media. */
    fun signals(connection: Connection, callId: String): Flow<ChatCallSignalView> =
        connection.subscribe(ChatCallDocuments.CHAT_CALL_SIGNALS, mapOf("callId" to JsonPrimitive(callId)))
            .mapNotNull { data -> read(data, "ChatCallSignals", ChatCallSignalView.serializer()) }

    /** What the event says of the field, or null for an event that says nothing of it. */
    private fun <Element> read(data: JsonElement, field: String, deserializer: kotlinx.serialization.DeserializationStrategy<Element>): Element? {
        val element = (data as? kotlinx.serialization.json.JsonObject)?.get(field) ?: return null
        if (element is kotlinx.serialization.json.JsonNull) return null
        return TeaNodeJson.decodeFromJsonElement(deserializer, element)
    }
}
