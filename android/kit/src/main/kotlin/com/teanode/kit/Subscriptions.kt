package com.teanode.kit

import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonElement
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import java.io.IOException
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLong

/** SubscriptionTransport opens a GraphQL subscription for a connection. The app uses OkHttp; demo mode and tests stand in their own. */
interface SubscriptionTransport {
    /**
     * Emits the `data` of each event of the subscription. The flow ends when the server completes the subscription,
     * and fails when the connection drops; the screen reloads and opens a new one.
     */
    fun subscribe(connection: Connection, document: String, variables: Map<String, JsonElement>): Flow<JsonElement>
}

/** FeedSilentException is a feed that said nothing for so long that it was taken for dead. */
class FeedSilentException : IOException("the server's feed went quiet")

/** OkHttpSubscriptions speaks `graphql-ws` over an OkHttp websocket. */
class OkHttpSubscriptions(
    private val client: OkHttpClient = OkHttpClient.Builder().pingInterval(20, TimeUnit.SECONDS).build(),
    private val silenceMostMS: Long = SILENCE_MOST_MS,
    private val silenceCheckMS: Long = SILENCE_CHECK_MS,
) : SubscriptionTransport {
    override fun subscribe(connection: Connection, document: String, variables: Map<String, JsonElement>): Flow<JsonElement> = callbackFlow {
        val token = connection.accessToken()
        val address = connection.account.server.websocketEndpoint("/api/v1/graphql")
        val lastHeardMS = AtomicLong(System.currentTimeMillis())
        val request = Request.Builder().url(address.replaceFirst("ws", "http")).header("Sec-WebSocket-Protocol", GraphQLWebSocketMessage.SUBPROTOCOL).build()
        val socket = client.newWebSocket(request, object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                lastHeardMS.set(System.currentTimeMillis())
                webSocket.send(GraphQLWebSocketMessage.ConnectionInit("Bearer $token").encoded())
                webSocket.send(GraphQLWebSocketMessage.Start("1", document, variables).encoded())
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                lastHeardMS.set(System.currentTimeMillis())
                val message = try {
                    GraphQLWebSocketMessage.decode(text)
                } catch (_: Exception) {
                    return
                }
                when (message) {
                    is GraphQLWebSocketMessage.ConnectionError -> close(TeaNodeError.Refused(message.errorMessage))
                    is GraphQLWebSocketMessage.Error ->
                        close(TeaNodeError.Refused(message.errors.firstOrNull()?.message ?: "The server stopped the feed."))
                    is GraphQLWebSocketMessage.Complete -> close()
                    is GraphQLWebSocketMessage.Data -> {
                        val firstError = message.errors.firstOrNull()
                        if (firstError != null) {
                            close(TeaNodeError.Refused(firstError.message))
                        } else if (message.data != null) {
                            trySend(message.data)
                        }
                    }
                    else -> Unit
                }
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                close()
            }

            @Suppress("PARAMETER_NAME_CHANGED_ON_OVERRIDE")
            override fun onFailure(webSocket: WebSocket, failure: Throwable, response: Response?) {
                close(failure)
            }
        })
        // A connection a phone lost while asleep can stay open without saying so; the server sends a keep-alive every
        // second, so a long silence means it is gone.
        val watchdog = launch {
            while (true) {
                delay(silenceCheckMS)
                if (System.currentTimeMillis() - lastHeardMS.get() > silenceMostMS) {
                    close(FeedSilentException())
                    return@launch
                }
            }
        }
        awaitClose {
            watchdog.cancel()
            socket.cancel()
        }
    }

    companion object {
        const val SILENCE_MOST_MS = 15_000L
        const val SILENCE_CHECK_MS = 5_000L
    }
}
