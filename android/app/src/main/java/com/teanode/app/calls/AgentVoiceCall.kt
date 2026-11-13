package com.teanode.app.calls

import android.annotation.SuppressLint
import android.content.Context
import android.media.AudioAttributes
import android.media.AudioFormat
import android.media.AudioRecord
import android.media.AudioTrack
import android.media.MediaRecorder
import android.media.audiofx.AcousticEchoCanceler
import android.util.Base64
import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.calls.ChatCallAudioRoute
import com.teanode.kit.calls.InterruptedAnswer
import com.teanode.kit.calls.VoiceAnswerQueue
import com.teanode.kit.calls.VoiceCallEvent
import com.teanode.kit.calls.VoiceCallSpeech
import com.teanode.kit.calls.VoiceCallState
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString.Companion.toByteString
import java.util.UUID
import java.util.concurrent.TimeUnit
import kotlin.math.sqrt

/**
 * AgentVoiceCall is a call with the agent: what the person says is heard by the server and becomes a turn, and the
 * agent's answers are read back to them. It is the Android side of the iPhone app's `VoiceCall`.
 *
 * The microphone is sent to the server a tenth of a second at a time over its voice socket, as mono 16-bit sound at the
 * rate the server takes; the answers come back as sound at 24000 Hz and are played as they arrive. The phone's echo
 * cancelling is asked for, which is what lets the person talk over an answer to cut in. The call is kept alive with the
 * screen locked by the call service, which shows its ongoing notification (Mute, Hang up).
 *
 * The turns themselves are the chats part's: [sendSpoken] asks the agent and returns the run's id, and [follow] is told
 * what the conversation's feed says of the turns this call started, so that the answers are read out.
 */
class AgentVoiceCall(
    private val context: Context,
    private val connection: Connection,
    private val sampleRate: Int,
    val agentName: String,
    /** Asks the agent what the person said, as a spoken turn; returns the run's id, or null when it could not. */
    private val sendSpoken: suspend (text: String, interruptedAnswer: InterruptedAnswer?) -> String?,
    /** The whole sentences of a text being spoken (`SpokenText.sentences`), and the words of an answer as they are read out (`SpokenText.of`). */
    private val sentencesOf: (text: String) -> List<String>,
    private val spokenTextOf: (markdown: String) -> String,
    private val audio: CallAudio = CallAudioRouter(context),
    private val scope: CoroutineScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate),
) {
    private val currentState = MutableStateFlow(VoiceCallState.Starting)

    /** What the call is doing. */
    val callState: StateFlow<VoiceCallState> = currentState.asStateFlow()

    private val caption = MutableStateFlow("")

    /** What the person is saying, as it is heard. */
    val captionText: StateFlow<String> = caption.asStateFlow()

    private val problem = MutableStateFlow<String?>(null)

    /** What went wrong, in words for the person. */
    val problemText: StateFlow<String?> = problem.asStateFlow()

    private val loudness = MutableStateFlow(0f)

    /** How loud the microphone is, for the meter: 0 to 1. */
    val level: StateFlow<Float> = loudness.asStateFlow()

    private val muted = MutableStateFlow(false)
    val isMuted: StateFlow<Boolean> = muted.asStateFlow()

    private var socket: WebSocket? = null
    private var recorder: AudioRecord? = null
    private var player: AudioTrack? = null
    private var echoCanceler: AcousticEchoCanceler? = null
    private var captureJob: Job? = null
    private var meterJob: Job? = null
    private var stillHereJob: Job? = null
    private val callName = UUID.randomUUID().toString()
    private val answers = VoiceAnswerQueue(callName)
    private val spokenRuns = mutableSetOf<String>()
    private var isVoiceProcessing = false
    private var framesWritten = 0L
    private val framesEndOfSegment = mutableMapOf<String, Long>()

    @Volatile
    private var isHoldingForAnswer = false

    @Volatile
    private var loudestLevel = 0f

    // MARK: Starting and ending

    /** Starts the call: the audio first, then the socket. The person has been asked for the microphone. */
    @SuppressLint("MissingPermission")
    fun start() {
        audio.start()
        // The call is heard on the speaker, as a speakerphone, unless something private to the person is connected.
        if (!audio.route.value.isPrivate) audio.choose(ChatCallAudioRoute.Speaker)
        try {
            startAudio()
        } catch (error: Exception) {
            end(context.getString(com.teanode.app.R.string.calls_voice_microphone_failed, error.message.orEmpty()))
            return
        }
        openSocket()
        meterJob = scope.launch {
            var listeningSince = 0L
            var answerHeardUntil = 0L
            while (isActive) {
                delay(METER_INTERVAL_MS)
                loudness.value = measuredLevel
                listeningSince = watchForSilence(listeningSince)
                playedSegments()
                // Without the phone's echo cancelling, the answer from the speaker reaches the microphone and is heard as the person speaking. Nothing is sent while it plays, and for a moment after, for the room to go quiet.
                val now = System.currentTimeMillis()
                if (answers.isSpeakingAnswer) answerHeardUntil = now + ANSWER_ECHO_HOLD_MS
                isHoldingForAnswer = !isVoiceProcessing && now < answerHeardUntil
            }
        }
        // While muted nothing is sent; the server is told every twenty seconds that the call is still there.
        stillHereJob = scope.launch {
            while (isActive) {
                delay(STILL_HERE_INTERVAL_MS)
                if (muted.value) sendEvent("stillHere")
            }
        }
    }

    /** Ends the call: hung up by the person, or by a problem. */
    fun hangUp() = end(null)

    private fun end(problemText: String?) {
        if (currentState.value == VoiceCallState.Ended) return
        if (problemText != null) problem.value = problemText
        currentState.value = VoiceCallState.Ended
        meterJob?.cancel()
        stillHereJob?.cancel()
        captureJob?.cancel()
        sendEvent("stop")
        socket?.close(NORMAL_CLOSE, null)
        socket = null
        runCatching { recorder?.stop() }
        recorder?.release()
        recorder = null
        echoCanceler?.release()
        echoCanceler = null
        runCatching { player?.stop() }
        player?.release()
        player = null
        audio.stop()
        scope.cancel()
    }

    /** Mutes or unmutes the microphone: nothing is sent while muted. */
    fun setMuted(isMuted: Boolean) {
        muted.value = isMuted
    }

    // MARK: The audio

    @SuppressLint("MissingPermission")
    private fun startAudio() {
        val minimum = AudioRecord.getMinBufferSize(sampleRate, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT)
        val tenthOfASecondBytes = sampleRate / 10 * 2
        val record = AudioRecord(
            MediaRecorder.AudioSource.VOICE_COMMUNICATION, sampleRate, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT,
            maxOf(minimum, tenthOfASecondBytes * 4),
        )
        check(record.state == AudioRecord.STATE_INITIALIZED) { "the microphone did not start" }
        recorder = record
        if (AcousticEchoCanceler.isAvailable()) {
            echoCanceler = AcousticEchoCanceler.create(record.audioSessionId)?.also { canceler -> canceler.enabled = true }
        }
        isVoiceProcessing = echoCanceler != null
        val track = AudioTrack.Builder()
            .setAudioAttributes(AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_VOICE_COMMUNICATION).setContentType(AudioAttributes.CONTENT_TYPE_SPEECH).build())
            .setAudioFormat(AudioFormat.Builder().setSampleRate(ANSWER_SAMPLE_RATE).setChannelMask(AudioFormat.CHANNEL_OUT_MONO).setEncoding(AudioFormat.ENCODING_PCM_16BIT).build())
            .setBufferSizeInBytes(AudioTrack.getMinBufferSize(ANSWER_SAMPLE_RATE, AudioFormat.CHANNEL_OUT_MONO, AudioFormat.ENCODING_PCM_16BIT) * 4)
            .setTransferMode(AudioTrack.MODE_STREAM)
            .build()
        player = track
        track.play()
        record.startRecording()
        captureJob = CoroutineScope(Dispatchers.IO).launch {
            val buffer = ByteArray(tenthOfASecondBytes)
            while (isActive) {
                val read = record.read(buffer, 0, buffer.size)
                if (read <= 0) continue
                measure(buffer, read)
                val target = socket
                if (target != null && !muted.value && !isHoldingForAnswer) target.send(buffer.toByteString(0, read))
            }
        }
    }

    private var measuredLevel = 0f

    /** How loud a tenth of a second was, for the meter, and how loud the microphone has been since it started. */
    private fun measure(buffer: ByteArray, length: Int) {
        var sum = 0.0
        var index = 0
        while (index + 1 < length) {
            val sample = (buffer[index].toInt() and 0xFF or (buffer[index + 1].toInt() shl 8)).toShort() / 32768.0
            sum += sample * sample
            index += 2
        }
        val rootMeanSquare = sqrt(sum / maxOf(1, length / 2)).toFloat()
        measuredLevel = minOf(1f, rootMeanSquare * 8)
        loudestLevel = maxOf(loudestLevel, rootMeanSquare)
    }

    /** A microphone that gives nothing for a long time once the server listens is told to the person. */
    private fun watchForSilence(listeningSince: Long): Long {
        if ((currentState.value != VoiceCallState.Listening && currentState.value != VoiceCallState.Hearing) || muted.value) return 0
        val since = if (listeningSince == 0L) System.currentTimeMillis() else listeningSince
        // A person who waits before speaking is not a dead microphone: only a long silence from the start of the call is.
        if (System.currentTimeMillis() - since > SILENCE_WAIT_MS && loudestLevel < SILENT_LEVEL && problem.value == null) {
            problem.value = context.getString(com.teanode.app.R.string.calls_voice_silent_microphone)
            return 0
        }
        return since
    }

    // MARK: The socket

    private fun openSocket() {
        scope.launch {
            try {
                val token = connection.accessToken()
                val address = connection.account.server.websocketEndpoint("/api/v1/agent/voice")
                val request = Request.Builder().url(address.replaceFirst("ws", "http")).build()
                val client = OkHttpClient.Builder().pingInterval(20, TimeUnit.SECONDS).build()
                socket = client.newWebSocket(request, object : WebSocketListener() {
                    override fun onOpen(webSocket: WebSocket, response: Response) {
                        val hello = JsonObject(
                            mapOf(
                                "voiceEvent" to JsonPrimitive("hello"),
                                "authorization" to JsonPrimitive("Bearer $token"),
                                "captureSettings" to JsonObject(
                                    mapOf(
                                        "echoCancellation" to JsonPrimitive(isVoiceProcessing),
                                        "noiseSuppression" to JsonPrimitive(isVoiceProcessing),
                                        "autoGainControl" to JsonPrimitive(isVoiceProcessing),
                                        "sampleRate" to JsonPrimitive(sampleRate),
                                    ),
                                ),
                            ),
                        )
                        webSocket.send(hello.toString())
                    }

                    override fun onMessage(webSocket: WebSocket, text: String) {
                        val event = runCatching { TeaNodeJson.decodeFromString(VoiceCallEvent.serializer(), text) }.getOrNull() ?: return
                        scope.launch { handle(event) }
                    }

                    override fun onFailure(webSocket: WebSocket, failure: Throwable, response: Response?) {
                        scope.launch { if (currentState.value != VoiceCallState.Ended) end(context.getString(com.teanode.app.R.string.calls_problem_lost_connection)) }
                    }

                    override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                        scope.launch { if (currentState.value != VoiceCallState.Ended) end(context.getString(com.teanode.app.R.string.calls_problem_lost_connection)) }
                    }
                })
            } catch (_: Exception) {
                if (currentState.value != VoiceCallState.Ended) end(context.getString(com.teanode.app.R.string.calls_problem_lost_connection))
            }
        }
    }

    private fun sendEvent(name: String, extra: Map<String, kotlinx.serialization.json.JsonElement> = emptyMap()) {
        socket?.send(JsonObject(mapOf("voiceEvent" to JsonPrimitive(name)) + extra).toString())
    }

    private suspend fun handle(event: VoiceCallEvent) {
        when (event.voiceEvent) {
            "ready" -> currentState.value = VoiceCallState.Listening
            "speechStarted" -> {
                currentState.value = VoiceCallState.Hearing
                // Talking over an answer turns it down at once; the words that follow decide whether it stops.
                if (answers.isSpeakingAnswer) player?.setVolume(0.15f)
            }
            "speechStopped" -> if (currentState.value == VoiceCallState.Hearing) currentState.value = if (spokenRuns.isEmpty()) VoiceCallState.Listening else VoiceCallState.Working
            "transcriptDelta" -> caption.value += event.transcriptText.orEmpty()
            "transcriptFinal" -> {
                val text = (event.transcriptText ?: caption.value).trim()
                caption.value = ""
                if (text.isEmpty()) {
                    player?.setVolume(1f)
                } else {
                    send(text)
                }
            }
            "transcriptFailed" -> {
                caption.value = ""
                player?.setVolume(1f)
            }
            "answerAudio" -> {
                val segmentId = event.answerSegmentId
                val audioText = event.answerAudio
                if (segmentId != null && audioText != null) play(audioText, segmentId)
            }
            "answerAudioDone", "answerAudioFailed" -> {
                answers.segmentDone(event.answerSegmentId).forEach { request -> ask(request) }
            }
            "problem" -> problem.value = event.errorMessage
            "refused", "error" -> end(event.errorMessage ?: context.getString(com.teanode.app.R.string.calls_problem_lost_connection))
        }
    }

    // MARK: Turns

    /** Sends what the person said as a turn. Said over an answer, the answer stops, and the agent is told how much of it was heard. */
    private suspend fun send(text: String) {
        var interrupted: InterruptedAnswer? = null
        if (answers.isBusy) {
            interrupted = answers.interruption()
            stopSpeaking()
        }
        player?.setVolume(1f)
        currentState.value = VoiceCallState.Working
        sendSpoken(text, interrupted)?.let { runId -> spokenRuns.add(runId) }
    }

    /** What the conversation's feed says, for the turns this call started. */
    fun follow(runId: String, kind: String, text: String?, note: String?) {
        if (runId !in spokenRuns || currentState.value == VoiceCallState.Ended) return
        when (kind) {
            "message" -> speak(spokenTextOf(text.orEmpty()))
            "question" -> speak(spokenTextOf(VoiceCallSpeech.questionText(note, text)))
            "confirmation" -> speak(context.getString(com.teanode.app.R.string.calls_voice_approval_on_screen, spokenTextOf(note.orEmpty())))
            "done", "error" -> {
                spokenRuns.remove(runId)
                if (!answers.isBusy && currentState.value == VoiceCallState.Working) currentState.value = VoiceCallState.Listening
            }
        }
    }

    // MARK: Speaking

    private fun speak(text: String) {
        answers.speak(sentencesOf(text + "\n")).forEach { request -> ask(request) }
    }

    private fun ask(request: VoiceAnswerQueue.SpeakRequest) {
        sendEvent("speakAnswer", mapOf("answerSegmentId" to JsonPrimitive(request.segmentId), "answerText" to JsonPrimitive(request.text)))
    }

    private fun play(base64: String, segmentId: String) {
        val data = runCatching { Base64.decode(base64, Base64.DEFAULT) }.getOrNull() ?: return
        if (data.size < 2 || !answers.isKnown(segmentId)) return
        currentState.value = VoiceCallState.Speaking
        val track = player ?: return
        track.write(data, 0, data.size)
        framesWritten += data.size / 2
        framesEndOfSegment[segmentId] = framesWritten
    }

    /** A segment has played once the speaker has reached the end of its sound and the server has sent all of it. */
    private fun playedSegments() {
        val track = player ?: return
        val head = track.playbackHeadPosition.toLong()
        for ((segmentId, endFrame) in framesEndOfSegment.toList()) {
            if (head >= endFrame) {
                framesEndOfSegment.remove(segmentId)
                if (answers.played(segmentId) && currentState.value != VoiceCallState.Ended) {
                    currentState.value = if (spokenRuns.isEmpty()) VoiceCallState.Listening else VoiceCallState.Working
                }
            }
        }
    }

    /** Cuts an answer short, as a tap on the dashboard's meter does. */
    fun cutAnswer() {
        if (!answers.isBusy) return
        stopSpeaking()
        player?.setVolume(1f)
        currentState.value = if (spokenRuns.isEmpty()) VoiceCallState.Listening else VoiceCallState.Working
    }

    private fun stopSpeaking() {
        val pending = answers.stop()
        if (pending.isNotEmpty()) sendEvent("cancelAnswer", mapOf("answerSegmentIds" to JsonArray(pending.map { id -> JsonPrimitive(id) })))
        framesEndOfSegment.clear()
        player?.let { track ->
            track.pause()
            track.flush()
            track.play()
        }
        framesWritten = 0
    }

    private companion object {
        const val ANSWER_SAMPLE_RATE = 24_000
        const val METER_INTERVAL_MS = 100L
        const val ANSWER_ECHO_HOLD_MS = 400L
        const val STILL_HERE_INTERVAL_MS = 20_000L
        const val SILENCE_WAIT_MS = 8_000L
        const val SILENT_LEVEL = 0.00001f
        const val NORMAL_CLOSE = 1000
    }
}
