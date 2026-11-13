package com.teanode.app.chats

import android.Manifest
import android.annotation.SuppressLint
import android.content.Context
import android.content.pm.PackageManager
import android.media.AudioFormat
import android.media.AudioRecord
import android.media.MediaRecorder
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.core.content.ContextCompat
import com.teanode.app.R
import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.chats.VoiceEvent
import com.teanode.kit.chats.appendingDictation
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString.Companion.toByteString

/**
 * Dictation puts what the person says into a box as they speak: the microphone is streamed to the server's voice
 * socket (`/api/v1/agent/voice`, the one a call uses) with the purpose "dictation", which only transcribes. Each
 * utterance's final words are appended to the text it was started on; nothing is sent or saved, and the words stay
 * editable.
 */
class Dictation(private val context: Context) {
    enum class State { Idle, Starting, Listening, Hearing }

    /** Where the words go, as the server words its transcription prompt. */
    enum class Place(val wireName: String) { Agent("agent"), Chat("chat") }

    var state: State by mutableStateOf(State.Idle)
        private set

    /** The words being heard, before they are final. */
    var captionText: String by mutableStateOf("")
        private set

    /** Why dictation stopped by itself, for a snackbar; null when it did not. */
    var problemText: String? by mutableStateOf(null)

    val isDictating: Boolean get() = state != State.Idle

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private var socket: WebSocket? = null
    private var recorder: AudioRecord? = null
    private var captureJob: Job? = null
    private var watchJob: Job? = null
    private var lastHeardAtMS = 0L
    private var readText: (() -> String)? = null
    private var writeText: ((String) -> Unit)? = null

    fun hasPermission(): Boolean = ContextCompat.checkSelfPermission(context, Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED

    /** Starts dictating into the text, or stops when it is going already. */
    fun toggle(read: () -> String, write: (String) -> Unit, place: Place, connection: Connection, sampleRate: Int) {
        if (isDictating) stop() else start(read, write, place, connection, sampleRate)
    }

    @SuppressLint("MissingPermission")
    fun start(read: () -> String, write: (String) -> Unit, place: Place, connection: Connection, sampleRate: Int) {
        if (state != State.Idle) return
        sampleRateInUse = sampleRate
        if (!hasPermission()) {
            problemText = context.getString(R.string.chats_microphone_not_allowed)
            return
        }
        readText = read
        writeText = write
        captionText = ""
        problemText = null
        state = State.Starting
        lastHeardAtMS = System.currentTimeMillis()
        scope.launch {
            try {
                val minimum = AudioRecord.getMinBufferSize(sampleRate, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT)
                val made = AudioRecord(MediaRecorder.AudioSource.MIC, sampleRate, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT, maxOf(minimum, sampleRate / 5 * 2))
                if (made.state != AudioRecord.STATE_INITIALIZED) {
                    made.release()
                    finish(context.getString(R.string.chats_microphone_not_ready))
                    return@launch
                }
                recorder = made
                made.startRecording()
                watchForSilence()
                listen(connection, place, sampleRate)
            } catch (failure: Exception) {
                finish(context.getString(R.string.chats_microphone_not_started, failure.message.orEmpty()))
            }
        }
    }

    /** Stops listening. Words already put in the box stay. */
    fun stop() = finish(null)

    private fun finish(problem: String?) {
        if (problem != null) problemText = problem
        if (state == State.Idle && socket == null && recorder == null) return
        state = State.Idle
        captionText = ""
        watchJob?.cancel()
        watchJob = null
        captureJob?.cancel()
        captureJob = null
        socket?.send("""{"voiceEvent":"stop"}""")
        socket?.close(1001, null)
        socket = null
        runCatching { recorder?.stop() }
        runCatching { recorder?.release() }
        recorder = null
        readText = null
        writeText = null
    }

    /** Stops after a long silence, so a microphone left on is not left on. */
    private fun watchForSilence() {
        watchJob = scope.launch {
            while (true) {
                delay(1_000)
                if (state == State.Listening && System.currentTimeMillis() - lastHeardAtMS > SILENT_MS_MOST) {
                    stop()
                    return@launch
                }
            }
        }
    }

    private suspend fun listen(connection: Connection, place: Place, sampleRate: Int) {
        val token = connection.accessToken()
        val address = connection.account.server.websocketEndpoint("/api/v1/agent/voice").replaceFirst("ws", "http")
        val client = OkHttpClient()
        val hello = buildJsonObject {
            put("voiceEvent", "hello")
            put("authorization", "Bearer $token")
            put("purpose", "dictation")
            put("dictationPlace", place.wireName)
            put(
                "captureSettings",
                buildJsonObject {
                    put("echoCancellation", false)
                    put("noiseSuppression", false)
                    put("autoGainControl", false)
                    put("sampleRate", sampleRate)
                },
            )
        }
        val opened = client.newWebSocket(
            Request.Builder().url(address).build(),
            object : WebSocketListener() {
                override fun onOpen(webSocket: WebSocket, response: Response) {
                    webSocket.send(hello.toString())
                }

                override fun onMessage(webSocket: WebSocket, text: String) {
                    val event = runCatching { TeaNodeJson.decodeFromString(VoiceEvent.serializer(), text) }.getOrNull() ?: return
                    scope.launch { handle(event) }
                }

                override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                    scope.launch { if (state != State.Idle) finish(context.getString(R.string.chats_dictation_lost_connection)) }
                }

                override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                    scope.launch { if (state != State.Idle) finish(context.getString(R.string.chats_dictation_lost_connection)) }
                }
            },
        )
        socket = opened
    }

    private fun startCapture(sampleRate: Int) {
        val active = recorder ?: return
        captureJob?.cancel()
        captureJob = scope.launch(Dispatchers.IO) {
            // A tenth of a second at a time, as a call and the dashboard send it.
            val buffer = ByteArray(sampleRate / 10 * 2)
            while (true) {
                val read = active.read(buffer, 0, buffer.size)
                if (read <= 0) {
                    delay(20)
                    continue
                }
                socket?.send(buffer.toByteString(0, read))
            }
        }
    }

    private var sampleRateInUse = 24_000

    private fun handle(event: VoiceEvent) {
        when (event.voiceEvent) {
            "ready" -> {
                state = State.Listening
                lastHeardAtMS = System.currentTimeMillis()
                startCapture(sampleRateInUse)
            }
            "speechStarted" -> {
                state = State.Hearing
                lastHeardAtMS = System.currentTimeMillis()
            }
            "speechStopped" -> {
                if (state == State.Hearing) state = State.Listening
                lastHeardAtMS = System.currentTimeMillis()
            }
            "transcriptDelta" -> {
                captionText += event.transcriptText.orEmpty()
                lastHeardAtMS = System.currentTimeMillis()
            }
            "transcriptFinal" -> {
                val words = (event.transcriptText ?: captionText).trim()
                captionText = ""
                lastHeardAtMS = System.currentTimeMillis()
                if (words.isNotEmpty()) {
                    val read = readText
                    val write = writeText
                    if (read != null && write != null) write(appendingDictation(words, read()))
                }
            }
            "transcriptFailed" -> captionText = ""
            "problem" -> problemText = event.errorMessage
            "refused", "error" -> finish(event.errorMessage ?: context.getString(R.string.chats_dictation_lost_connection))
        }
    }

    companion object {
        /** How long without a word before dictation stops by itself, as the dashboard's does. */
        const val SILENT_MS_MOST = 30_000L
    }
}
