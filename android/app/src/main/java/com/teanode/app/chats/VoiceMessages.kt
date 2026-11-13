package com.teanode.app.chats

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.media.MediaDataSource
import android.media.MediaPlayer
import android.media.MediaRecorder
import android.os.Build
import android.os.SystemClock
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.core.content.ContextCompat
import com.teanode.app.R
import com.teanode.kit.Connection
import com.teanode.kit.chats.AgentAttachment
import com.teanode.kit.chats.download
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.io.File
import java.util.UUID
import kotlin.math.log10

/** VoiceRecording is a voice message recorded and not yet sent: the file, and how long it runs. */
class VoiceRecording(val file: File, val durationSeconds: Double) {
    val id: String = UUID.randomUUID().toString()
}

/**
 * VoiceMessageRecorder records what the person says while they hold the button in a chat: AAC in an m4a file, mono at
 * 44.1 kHz and 64 kbps, which the server takes as it is. Half a second is the least a message is and two minutes the
 * most; from 1:50 the time left is counted down.
 */
class VoiceMessageRecorder(private val context: Context) {
    var isRecording: Boolean by mutableStateOf(false)
        private set

    /** Whether the finger has slid far enough up that letting go discards. */
    var isCancelArmed: Boolean by mutableStateOf(false)
    var elapsedSeconds: Double by mutableStateOf(0.0)
        private set

    /** How loud the microphone is, for the meter: 0 to 1. */
    var level: Float by mutableFloatStateOf(0f)
        private set

    /** Counts the recordings begun, for the haptic. */
    var startedCount: Int by mutableStateOf(0)
        private set

    /** A word of guidance that shows for a moment in the strip. */
    var hintText: String? by mutableStateOf(null)
        private set

    /** Why a recording could not begin, for a snackbar. */
    var problemText: String? = null

    /** Called when the recording reaches its longest, to send it. */
    var onLongest: (() -> Unit)? = null

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private var recorder: MediaRecorder? = null
    private var file: File? = null
    private var startedAtMS = 0L
    private var tickJob: Job? = null
    private var hintJob: Job? = null

    /** Whether the microphone may be used. */
    fun hasPermission(): Boolean = ContextCompat.checkSelfPermission(context, Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED

    /** Whether the time left is counted down. */
    val isCountingDown: Boolean get() = isRecording && elapsedSeconds >= COUNTDOWN_FROM_SECONDS
    val secondsLeft: Double get() = maxOf(0.0, LONGEST_SECONDS - elapsedSeconds)

    /** Starts recording. Without the microphone's permission nothing starts; the screen asks for it, and the person holds again. */
    fun start() {
        if (isRecording) return
        if (!hasPermission()) {
            showHint(context.getString(R.string.chats_hold_again_to_talk))
            return
        }
        try {
            begin()
        } catch (failure: Exception) {
            stopRecording()
            problemText = context.getString(R.string.chats_microphone_not_started, failure.message.orEmpty())
        }
    }

    @Suppress("DEPRECATION")
    private fun begin() {
        val target = File(context.cacheDir, "Voice message ${UUID.randomUUID()}.m4a")
        val made = if (Build.VERSION.SDK_INT >= 31) MediaRecorder(context) else MediaRecorder()
        made.setAudioSource(MediaRecorder.AudioSource.MIC)
        made.setOutputFormat(MediaRecorder.OutputFormat.MPEG_4)
        made.setAudioEncoder(MediaRecorder.AudioEncoder.AAC)
        made.setAudioSamplingRate(44_100)
        made.setAudioChannels(1)
        made.setAudioEncodingBitRate(64_000)
        made.setOutputFile(target.absolutePath)
        made.prepare()
        made.start()
        recorder = made
        file = target
        startedAtMS = SystemClock.elapsedRealtime()
        isRecording = true
        isCancelArmed = false
        elapsedSeconds = 0.0
        level = 0f
        hintText = null
        startedCount += 1
        tickJob = scope.launch {
            while (true) {
                delay(50)
                tick()
            }
        }
    }

    private fun tick() {
        val active = recorder ?: return
        elapsedSeconds = (SystemClock.elapsedRealtime() - startedAtMS) / 1000.0
        val amplitude = runCatching { active.maxAmplitude }.getOrDefault(0)
        // Decibels from silence up to 0; speech is heard from about -50.
        val decibels = if (amplitude <= 0) -100.0 else 20 * log10(amplitude / 32767.0)
        level = ((decibels + 50) / 50).coerceIn(0.0, 1.0).toFloat()
        if (elapsedSeconds >= LONGEST_SECONDS) onLongest?.invoke()
    }

    /** Ends the recording and hands it over to be sent. One shorter than half a second is thrown away, with a word on how to record. */
    fun finish(): VoiceRecording? {
        if (!isRecording) return null
        val seconds = minOf((SystemClock.elapsedRealtime() - startedAtMS) / 1000.0, LONGEST_SECONDS)
        val kept = file
        stopRecording()
        if (kept == null) return null
        if (seconds < SHORTEST_SECONDS) {
            kept.delete()
            showHint(context.getString(R.string.chats_hold_to_record_hint))
            return null
        }
        return VoiceRecording(kept, seconds)
    }

    /** Ends the recording and throws it away. */
    fun cancel() {
        if (!isRecording) return
        val kept = file
        stopRecording()
        kept?.delete()
    }

    private fun stopRecording() {
        tickJob?.cancel()
        tickJob = null
        runCatching { recorder?.stop() }
        runCatching { recorder?.release() }
        recorder = null
        file = null
        isRecording = false
        isCancelArmed = false
        level = 0f
    }

    private fun showHint(text: String) {
        hintText = text
        hintJob?.cancel()
        hintJob = scope.launch {
            delay(2_500)
            hintText = null
        }
    }

    /** A recording as it looks seven seconds in, since a screenshot has no microphone and no finger. */
    fun showDemoHold() {
        isRecording = true
        elapsedSeconds = 7.4
        level = 0.6f
    }

    companion object {
        const val SHORTEST_SECONDS = 0.5
        const val LONGEST_SECONDS = 120.0
        const val COUNTDOWN_FROM_SECONDS = 110.0

        /** The time as minutes and seconds: 0:07, 1:50. */
        fun clockText(seconds: Double): String {
            val whole = maxOf(0, seconds.toInt())
            return "${whole / 60}:" + "%02d".format(whole % 60)
        }
    }
}

/**
 * VoiceMessagePlayer plays the voice messages of chats: one at a time, each read from the server once and kept in
 * memory while the app runs.
 */
object VoiceMessagePlayer {
    var loadedAttachmentId: String? by mutableStateOf(null)
        private set
    var loadingAttachmentId: String? by mutableStateOf(null)
        private set
    var isPlaying: Boolean by mutableStateOf(false)
        private set
    var progress: Float by mutableFloatStateOf(0f)
        private set
    var playedSeconds: Double by mutableStateOf(0.0)
        private set
    var failedAttachmentId: String? by mutableStateOf(null)
        private set

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private var player: MediaPlayer? = null
    private val dataByAttachmentId = mutableMapOf<String, ByteArray>()
    private var tickJob: Job? = null

    private class BytesSource(private val bytes: ByteArray) : MediaDataSource() {
        override fun readAt(position: Long, buffer: ByteArray, offset: Int, size: Int): Int {
            if (position >= bytes.size) return -1
            val count = minOf(size, bytes.size - position.toInt())
            System.arraycopy(bytes, position.toInt(), buffer, offset, count)
            return count
        }

        override fun getSize(): Long = bytes.size.toLong()
        override fun close() = Unit
    }

    /** Starts, pauses or resumes this recording; another one being played stops. */
    fun toggle(attachment: AgentAttachment, connection: Connection) {
        val active = player
        if (loadedAttachmentId == attachment.id && active != null) {
            if (isPlaying) {
                pause()
            } else {
                active.start()
                isPlaying = true
                startTicking()
            }
            return
        }
        stop()
        if (loadingAttachmentId != null) return
        failedAttachmentId = null
        loadingAttachmentId = attachment.id
        scope.launch {
            try {
                val bytes = dataByAttachmentId[attachment.id] ?: connection.download("/api/v1/agent/attachments/${attachment.id}").also { read ->
                    dataByAttachmentId[attachment.id] = read
                }
                val made = MediaPlayer()
                made.setDataSource(BytesSource(bytes))
                made.setOnCompletionListener { finished() }
                made.prepare()
                made.start()
                player = made
                loadedAttachmentId = attachment.id
                isPlaying = true
                startTicking()
            } catch (_: Exception) {
                player = null
                loadedAttachmentId = null
                failedAttachmentId = attachment.id
            } finally {
                loadingAttachmentId = null
            }
        }
    }

    fun pause() {
        runCatching { player?.pause() }
        isPlaying = false
        tickJob?.cancel()
        tickJob = null
    }

    fun stop() {
        runCatching { player?.stop() }
        runCatching { player?.release() }
        player = null
        loadedAttachmentId = null
        isPlaying = false
        progress = 0f
        playedSeconds = 0.0
        tickJob?.cancel()
        tickJob = null
    }

    private fun finished() {
        isPlaying = false
        progress = 0f
        playedSeconds = 0.0
        runCatching { player?.seekTo(0) }
        tickJob?.cancel()
        tickJob = null
    }

    private fun startTicking() {
        tickJob?.cancel()
        tickJob = scope.launch {
            while (true) {
                delay(100)
                val active = player ?: continue
                val duration = runCatching { active.duration }.getOrDefault(0)
                if (duration <= 0) continue
                playedSeconds = active.currentPosition / 1000.0
                progress = minOf(1f, active.currentPosition.toFloat() / duration)
            }
        }
    }
}
