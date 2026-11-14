package com.teanode.kit

import kotlinx.coroutines.flow.toList
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import kotlinx.serialization.json.JsonPrimitive
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.util.concurrent.CopyOnWriteArrayList

class SubscriptionsTest {
    private fun connectionTo(server: MockWebServer): Connection {
        val address = ServerAddress.parse("http://localhost:${server.port}")
        val account = Account(address, "a-client", "tnt_socket", "", Long.MAX_VALUE / 4, "robin")
        return Connection(account, FakeServer { 200 to "{}" }, subscriptions = OkHttpSubscriptions(silenceMostMS = 400, silenceCheckMS = 100))
    }

    private fun serving(onText: (WebSocket, String, MutableList<String>) -> Unit, onOpen: (WebSocket) -> Unit = {}): Pair<MockWebServer, MutableList<String>> {
        val received = CopyOnWriteArrayList<String>()
        val server = MockWebServer()
        server.enqueue(
            MockResponse().withWebSocketUpgrade(
                object : WebSocketListener() {
                    override fun onOpen(webSocket: WebSocket, response: Response) = onOpen(webSocket)
                    override fun onMessage(webSocket: WebSocket, text: String) = onText(webSocket, text, received)
                },
            ),
        )
        server.start()
        return server to received
    }

    @Test
    fun eachEventsDataIsStreamedUntilTheServerCompletes() = runBlocking {
        val (server, received) = serving(onText = { socket, text, received ->
            received.add(text)
            if (text.contains("\"start\"")) {
                socket.send("""{"type":"connection_ack"}""")
                socket.send("""{"type":"ka"}""")
                socket.send("""{"id":"1","type":"data","payload":{"data":{"Events":{"text":"Hel"}}}}""")
                socket.send("""{"id":"1","type":"data","payload":{"data":{"Events":{"text":"lo"}}}}""")
                socket.send("""{"id":"1","type":"complete"}""")
            }
        })
        try {
            val events = withTimeout(10_000) { connectionTo(server).subscribe("subscription { Events { text } }", mapOf("id" to JsonPrimitive("c1"))).toList() }
            assertEquals(listOf("Hel", "lo"), events.map { event -> event["Events"]!!["text"]!!.stringValue })
            assertTrue(received[0].contains("\"Authorization\":\"Bearer tnt_socket\""), received[0])
            assertTrue(received[1].contains("\"variables\":{\"id\":\"c1\"}"), received[1])
            assertEquals("graphql-ws", server.takeRequest().getHeader("Sec-WebSocket-Protocol"))
        } finally {
            server.shutdown()
        }
    }

    @Test
    fun aRefusalEndsTheStreamWithItsWords() = runBlocking {
        val (server, _) = serving(onText = { socket, text, _ ->
            if (text.contains("\"start\"")) socket.send("""{"id":"1","type":"error","payload":{"message":"no such conversation"}}""")
        })
        try {
            val failure = assertThrows(TeaNodeError::class.java) {
                runBlocking { withTimeout(10_000) { connectionTo(server).subscribe("subscription { Events { text } }").toList() } }
            }
            assertEquals(TeaNodeError.Refused("no such conversation"), failure)
        } finally {
            server.shutdown()
        }
    }

    @Test
    fun aFeedThatGoesQuietIsTakenForDead() = runBlocking {
        val (server, _) = serving(onText = { socket, text, _ -> if (text.contains("\"start\"")) socket.send("""{"type":"connection_ack"}""") })
        try {
            assertThrows(FeedSilentException::class.java) {
                runBlocking { withTimeout(10_000) { connectionTo(server).subscribe("subscription { Events { text } }").toList() } }
            }
            Unit
        } finally {
            server.shutdown()
        }
    }
}
