package com.teanode.kit

import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Test

class GraphQLWebSocketMessageTest {
    @Test
    fun theAppsMessagesAreWhatTheServerReads() {
        assertEquals(
            """{"payload":{"Authorization":"Bearer a-token"},"type":"connection_init"}""",
            GraphQLWebSocketMessage.ConnectionInit("Bearer a-token").encoded(),
        )
        assertEquals(
            """{"id":"1","payload":{"query":"subscription(${'$'}conversationId: String!) { AgentConversationEvents(conversationId: ${'$'}conversationId) { kind } }","variables":{"conversationId":""}},"type":"start"}""",
            GraphQLWebSocketMessage.Start(
                "1", "subscription(\$conversationId: String!) { AgentConversationEvents(conversationId: \$conversationId) { kind } }",
                mapOf("conversationId" to JsonPrimitive("")),
            ).encoded(),
        )
        assertEquals("""{"id":"1","type":"stop"}""", GraphQLWebSocketMessage.Stop("1").encoded())
    }

    @Test
    fun theServersMessagesAreRead() {
        assertEquals(GraphQLWebSocketMessage.ConnectionAck, GraphQLWebSocketMessage.decode("""{"type":"connection_ack"}"""))
        assertEquals(GraphQLWebSocketMessage.KeepAlive, GraphQLWebSocketMessage.decode("""{"type":"ka"}"""))
        assertEquals(GraphQLWebSocketMessage.Complete("1"), GraphQLWebSocketMessage.decode("""{"id":"1","type":"complete"}"""))

        val data = GraphQLWebSocketMessage.decode(
            """{"id":"1","type":"data","payload":{"data":{"AgentConversationEvents":{"kind":"text","text":"Hel"}}}}""",
        ) as GraphQLWebSocketMessage.Data
        assertEquals("1", data.id)
        assertEquals("Hel", data.data!!["AgentConversationEvents"]!!["text"]!!.stringValue)
        assertEquals(emptyList<GraphQLError>(), data.errors)

        // The server sends one error as an object, not a list.
        assertEquals(
            GraphQLWebSocketMessage.Error("2", listOf(GraphQLError("no such conversation"))),
            GraphQLWebSocketMessage.decode("""{"id":"2","type":"error","payload":{"message":"no such conversation"}}"""),
        )
    }

    @Test
    fun aMessageItCannotReadSaysWhy() {
        assertEquals(
            GraphQLWebSocketMessageException.IsUnknown("next"),
            assertThrows(GraphQLWebSocketMessageException::class.java) { GraphQLWebSocketMessage.decode("""{"type":"next","id":"1"}""") },
        )
        assertEquals(
            GraphQLWebSocketMessageException.HasNoID("complete"),
            assertThrows(GraphQLWebSocketMessageException::class.java) { GraphQLWebSocketMessage.decode("""{"type":"complete"}""") },
        )
    }

    @Test
    fun whatItSendsItCanReadBack() {
        val answer = GraphQLWebSocketMessage.Data("7", JsonObject(mapOf("count" to JsonPrimitive(3), "isDone" to JsonPrimitive(true))), emptyList())
        assertEquals(answer, GraphQLWebSocketMessage.decode(answer.encoded()))
    }
}
