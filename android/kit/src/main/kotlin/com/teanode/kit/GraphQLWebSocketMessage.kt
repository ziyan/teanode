package com.teanode.kit

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

/**
 * GraphQLWebSocketMessage is one message of the websocket protocol the server speaks at `/api/v1/graphql` for
 * subscriptions: the `graphql-ws` subprotocol, the older Apollo one, which the dashboard speaks too
 * (`internal/api/v1api/apigraph/websocket.go`).
 *
 * The client opens with `connection_init`, carrying its token, since a program has no dashboard session; the server
 * answers `connection_ack` and then `ka` every second. Each subscription is a `start` with an id, answered by `data`
 * messages with that id until `complete`; `stop` ends one early.
 */
sealed class GraphQLWebSocketMessage {
    // From the app.
    data class ConnectionInit(val authorization: String) : GraphQLWebSocketMessage()
    data class Start(val id: String, val query: String, val variables: Map<String, JsonElement>) : GraphQLWebSocketMessage()
    data class Stop(val id: String) : GraphQLWebSocketMessage()
    data object ConnectionTerminate : GraphQLWebSocketMessage()

    // From the server.
    data object ConnectionAck : GraphQLWebSocketMessage()
    data class ConnectionError(val errorMessage: String) : GraphQLWebSocketMessage()
    data object KeepAlive : GraphQLWebSocketMessage()
    data class Data(val id: String, val data: JsonElement?, val errors: List<GraphQLError>) : GraphQLWebSocketMessage()
    data class Error(val id: String, val errors: List<GraphQLError>) : GraphQLWebSocketMessage()
    data class Complete(val id: String) : GraphQLWebSocketMessage()

    /** The message as the text frame sent on the websocket, its keys in alphabetical order. */
    fun encoded(): String {
        val envelope = LinkedHashMap<String, JsonElement>()
        when (this) {
            is ConnectionInit -> {
                envelope["type"] = JsonPrimitive("connection_init")
                envelope["payload"] = JsonObject(mapOf("Authorization" to JsonPrimitive(authorization)))
            }
            is Start -> {
                envelope["type"] = JsonPrimitive("start")
                envelope["id"] = JsonPrimitive(id)
                envelope["payload"] = JsonObject(mapOf("query" to JsonPrimitive(query), "variables" to JsonObject(variables)))
            }
            is Stop -> {
                envelope["type"] = JsonPrimitive("stop")
                envelope["id"] = JsonPrimitive(id)
            }
            ConnectionTerminate -> envelope["type"] = JsonPrimitive("connection_terminate")
            ConnectionAck -> envelope["type"] = JsonPrimitive("connection_ack")
            is ConnectionError -> {
                envelope["type"] = JsonPrimitive("connection_error")
                envelope["payload"] = JsonObject(mapOf("message" to JsonPrimitive(errorMessage)))
            }
            KeepAlive -> envelope["type"] = JsonPrimitive("ka")
            is Data -> {
                envelope["type"] = JsonPrimitive("data")
                envelope["id"] = JsonPrimitive(id)
                val payload = LinkedHashMap<String, JsonElement>()
                payload["data"] = data ?: JsonNull
                if (errors.isNotEmpty()) payload["errors"] = errorsJson(errors)
                envelope["payload"] = JsonObject(payload)
            }
            is Error -> {
                envelope["type"] = JsonPrimitive("error")
                envelope["id"] = JsonPrimitive(id)
                envelope["payload"] = errorsJson(errors)
            }
            is Complete -> {
                envelope["type"] = JsonPrimitive("complete")
                envelope["id"] = JsonPrimitive(id)
            }
        }
        return JsonObject(envelope).sortedByKeys().toString()
    }

    companion object {
        /** The subprotocol to ask for when opening the websocket. */
        const val SUBPROTOCOL = "graphql-ws"

        /** Reads a text frame the server sent. */
        fun decode(text: String): GraphQLWebSocketMessage {
            val envelope = TeaNodeJson.parseToJsonElement(text) as? JsonObject ?: throw GraphQLWebSocketMessageException.IsUnknown("")
            val type = envelope["type"]?.stringValue ?: ""
            val payload = envelope["payload"]
            return when (type) {
                "connection_ack" -> ConnectionAck
                "connection_error" -> ConnectionError(payload?.get("message")?.stringValue ?: "the server refused the connection")
                "ka" -> KeepAlive
                "data" -> Data(idOf(envelope, type), payload?.get("data"), errorsIn(payload?.get("errors")))
                "error" -> Error(idOf(envelope, type), errorsIn(payload))
                "complete" -> Complete(idOf(envelope, type))
                else -> throw GraphQLWebSocketMessageException.IsUnknown(type)
            }
        }

        private fun idOf(envelope: JsonObject, type: String): String =
            envelope["id"]?.stringValue ?: throw GraphQLWebSocketMessageException.HasNoID(type)

        private fun errorsJson(errors: List<GraphQLError>): JsonElement =
            JsonArray(errors.map { error -> JsonObject(mapOf("message" to JsonPrimitive(error.message))) })

        /** The errors in a payload: a list of them, or one alone. */
        private fun errorsIn(payload: JsonElement?): List<GraphQLError> = when (payload) {
            is JsonArray -> payload.map { error -> GraphQLError(error["message"]?.stringValue ?: "unknown error") }
            is JsonObject -> listOf(GraphQLError(payload["message"]?.stringValue ?: "unknown error"))
            else -> emptyList()
        }
    }
}

/** GraphQLWebSocketMessageException is why a frame from the server could not be read. */
sealed class GraphQLWebSocketMessageException(message: String) : Exception(message) {
    data class IsUnknown(val type: String) : GraphQLWebSocketMessageException("unknown message type $type")
    data class HasNoID(val type: String) : GraphQLWebSocketMessageException("message of type $type has no id")
}
