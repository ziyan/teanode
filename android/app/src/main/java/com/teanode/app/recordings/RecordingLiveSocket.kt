package com.teanode.app.recordings

import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.recordings.RecordingLiveBackoff
import com.teanode.kit.recordings.RecordingLiveSession
import com.teanode.kit.recordings.RecordingLiveUtterance
import com.teanode.kit.recordings.RecordingLiveWordsState
import com.teanode.kit.recordings.RecordingVoiceEvent
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.suspendCancellableCoroutine
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString.Companion.toByteString
import kotlin.coroutines.resume

/**
 * The live transcription stream of a recording being made (ios/TeaNode/Sources/RecordingLiveStream.swift): a socket to
 * `/api/v1/recordings/{id}/live` that takes the microphone's audio (mono 16-bit PCM) and answers with what it hears as it
 * is said. It reconnects with a backoff when it drops (a minute at most), and renews itself every 25 minutes. What it
 * hears is shown beside the transcript until the server's own passages cover it, and the end of a sentence lets a piece
 * be cut there.
 */
class RecordingLiveSocket(
    private val scope: CoroutineScope,
    private val connection: Connection,
    private val recordingId: String,
    private val timelineMS: () -> Long,
    private val onUtteranceEnd: (Long) -> Unit,
    private val onChange: (isConnected: Boolean, heard: List<RecordingLiveUtterance>, partialText: String, partialStartMS: Long) -> Unit,
) {
    private val client = OkHttpClient()
    private val words = RecordingLiveWordsState()
    private var job: Job? = null
    @Volatile private var socket: WebSocket? = null

    /** Whether audio sent now is heard: the stream is connected. */
    val isConnected: Boolean get() = socket != null && words.isConnected

    fun begin() {
        if (job != null) return
        job = scope.launch {
            var failureCount = 0
            while (isActive) {
                val wasListening = runSession()
                if (!isActive) return@launch
                failureCount = if (wasListening) 1 else failureCount + 1
                delay((RecordingLiveBackoff.delaySeconds(failureCount) * 1000).toLong())
            }
        }
    }

    fun end() {
        job?.cancel()
        job = null
        socket?.send("""{"voiceEvent":"stop"}""")
        socket?.close(1001, null)
        socket = null
        words.disconnected()
        onChange(false, words.heardUtterances, "", 0)
    }

    /** Sends some microphone audio (mono 16-bit PCM, little endian) when the stream is up. */
    fun send(pcm: ByteArray) {
        if (words.isConnected) socket?.send(pcm.toByteString())
    }

    private suspend fun runSession(): Boolean {
        var wasListening = false
        try {
            val token = connection.accessToken()
            val request = Request.Builder().url(connection.account.server.endpoint("/api/v1/recordings/$recordingId/live")).build()
            val recycle = scope.launch { delay((RecordingLiveSession.RECYCLE_AFTER_SECONDS * 1000).toLong()); socket?.close(1001, null) }
            suspendCancellableCoroutine { continuation ->
                val opened = client.newWebSocket(
                    request,
                    object : WebSocketListener() {
                        override fun onOpen(webSocket: WebSocket, response: Response) {
                            webSocket.send("""{"voiceEvent":"hello","authorization":"Bearer $token","streamStartMS":${timelineMS()}}""")
                        }

                        override fun onMessage(webSocket: WebSocket, text: String) {
                            val event = runCatching { TeaNodeJson.decodeFromString(RecordingVoiceEvent.serializer(), text) }.getOrNull() ?: return
                            if (event.voiceEvent == "ready") wasListening = true
                            try {
                                if (words.handle(event, timelineMS())) onUtteranceEnd(timelineMS())
                            } catch (refusal: Exception) {
                                webSocket.close(1000, null)
                                return
                            }
                            onChange(words.isConnected, words.heardUtterances, words.partialText, words.partialStartMS)
                        }

                        override fun onClosed(webSocket: WebSocket, code: Int, reason: String) { if (continuation.isActive) continuation.resume(Unit) }

                        override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) { if (continuation.isActive) continuation.resume(Unit) }
                    },
                )
                socket = opened
                continuation.invokeOnCancellation { opened.cancel() }
            }
            recycle.cancel()
        } catch (failure: kotlinx.coroutines.CancellationException) {
            throw failure
        } catch (_: Exception) {
        }
        socket = null
        words.disconnected()
        onChange(false, words.heardUtterances, "", 0)
        return wasListening
    }
}
