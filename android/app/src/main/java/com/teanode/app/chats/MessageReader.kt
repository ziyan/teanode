package com.teanode.app.chats

import android.content.Context
import android.media.AudioAttributes
import android.media.MediaDataSource
import android.media.MediaPlayer
import android.speech.tts.TextToSpeech
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.teanode.app.R
import com.teanode.kit.Connection
import com.teanode.kit.chats.TranscriptLine
import com.teanode.kit.chats.speechAudio
import com.teanode.kit.chats.speechPieces
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import java.util.UUID

/**
 * MessageReader reads one message aloud at a time: the server's voice, a piece at a time with the next piece asked for
 * while one plays, and the phone's own voice when the server cannot. A banner stops it.
 */
class MessageReader(private val context: Context) {
    var readingLineId: String? by mutableStateOf(null)
        private set

    /** Told why a message could not be read, for a snackbar. */
    var onProblem: ((String) -> Unit)? = null

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private var job: Job? = null
    private var player: MediaPlayer? = null
    private var speech: TextToSpeech? = null

    val isReading: Boolean get() = readingLineId != null

    fun isReading(line: TranscriptLine): Boolean = readingLineId == line.id

    fun read(line: TranscriptLine, connection: Connection) {
        stop()
        readingLineId = line.id
        val text = line.readableText
        job = scope.launch {
            play(text, connection)
            if (isActive) finish()
        }
    }

    fun stop() {
        job?.cancel()
        job = null
        runCatching { player?.stop() }
        runCatching { player?.release() }
        player = null
        runCatching { speech?.stop() }
        finish()
    }

    /** Shows the banner for a screenshot, since a screenshot has no sound. */
    fun showReading(line: TranscriptLine) {
        readingLineId = line.id
    }

    private fun finish() {
        readingLineId = null
    }

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

    private suspend fun play(text: String, connection: Connection) {
        val pieces = runCatching { connection.speechPieces(text) }.getOrNull() ?: listOf(text)
        if (pieces.isEmpty()) return
        var next = scope.async { runCatching { connection.speechAudio(pieces[0]) }.getOrNull() }
        for (index in pieces.indices) {
            val audio = next.await()
            if (!scope.isActive) return
            if (audio == null) {
                speakOnThePhone(pieces.drop(index))
                return
            }
            next = if (index + 1 < pieces.size) scope.async { runCatching { connection.speechAudio(pieces[index + 1]) }.getOrNull() } else scope.async { null }
            val didPlay = playPiece(audio)
            if (!scope.isActive) return
            if (!didPlay) {
                onProblem?.invoke(context.getString(R.string.chats_could_not_play_message))
                speakOnThePhone(pieces.drop(index))
                return
            }
        }
    }

    private suspend fun playPiece(audio: ByteArray): Boolean {
        val finished = CompletableDeferred<Boolean>()
        val made = MediaPlayer()
        return try {
            made.setAudioAttributes(AudioAttributes.Builder().setContentType(AudioAttributes.CONTENT_TYPE_SPEECH).setUsage(AudioAttributes.USAGE_MEDIA).build())
            made.setDataSource(BytesSource(audio))
            made.setOnCompletionListener { finished.complete(true) }
            made.setOnErrorListener { _, _, _ ->
                finished.complete(false)
                true
            }
            made.prepare()
            player = made
            made.start()
            finished.await()
        } catch (_: Exception) {
            false
        } finally {
            runCatching { made.release() }
            if (player === made) player = null
        }
    }

    private suspend fun speakOnThePhone(pieces: List<String>) {
        val ready = CompletableDeferred<Boolean>()
        val engine = TextToSpeech(context) { status -> ready.complete(status == TextToSpeech.SUCCESS) }
        speech = engine
        if (!ready.await()) {
            onProblem?.invoke(context.getString(R.string.chats_could_not_play_message))
            return
        }
        for (piece in pieces) {
            if (!scope.isActive) return
            val done = CompletableDeferred<Unit>()
            val id = UUID.randomUUID().toString()
            engine.setOnUtteranceProgressListener(object : android.speech.tts.UtteranceProgressListener() {
                override fun onStart(utteranceId: String?) = Unit
                override fun onDone(utteranceId: String?) {
                    done.complete(Unit)
                }

                @Deprecated("Deprecated in Java")
                override fun onError(utteranceId: String?) {
                    done.complete(Unit)
                }
            })
            engine.speak(piece, TextToSpeech.QUEUE_FLUSH, null, id)
            while (!done.isCompleted && scope.isActive) delay(100)
        }
        engine.shutdown()
        speech = null
    }
}
