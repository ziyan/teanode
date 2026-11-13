package com.teanode.app.recordings

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.media.AudioFormat
import android.media.AudioManager
import android.media.AudioRecord
import android.media.MediaCodec
import android.media.MediaCodecInfo
import android.media.MediaFormat
import android.media.MediaMuxer
import android.media.MediaRecorder
import android.os.Build
import android.os.IBinder
import android.os.SystemClock
import androidx.core.app.NotificationCompat
import com.teanode.app.MainActivity
import com.teanode.app.R
import com.teanode.kit.recordings.LocalRecording
import com.teanode.kit.recordings.LocalRecordingMedia
import com.teanode.app.TeaNodeApplication
import com.teanode.kit.recordings.RecordingCutPolicy
import com.teanode.kit.recordings.RecordingLiveUtterance
import com.teanode.kit.recordings.RecordingLiveWords
import com.teanode.kit.recordings.RecordingPieceBoundary
import com.teanode.kit.recordings.createRecording
import com.teanode.kit.recordings.RecordingCutReason
import com.teanode.kit.recordings.RecordingLevelHistory
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import java.util.UUID
import kotlin.math.log10

/** What the recorder screen shows of a capture under way. */
data class RecordingCaptureState(
    val localRecordingId: String? = null,
    val isRecording: Boolean = false,
    val isPaused: Boolean = false,
    val elapsedMS: Long = 0,
    val levelBars: List<Double> = List(RecordingLevelHistory.BAR_COUNT) { 0.0 },
    val problemMessageId: Int? = null,
    /** Whether the live transcription stream is connected. */
    val isLiveConnected: Boolean = false,
    /** The words heard so far, newest last, with the ones still being said at the end. */
    val liveUtterances: List<RecordingLiveUtterance> = emptyList(),
    /** Whether the recording is paused because of a call or another app taking the microphone. */
    val isInterrupted: Boolean = false,
)

/** Writes mono 16-bit PCM as AAC in an m4a file, a piece of a recording. */
private class PieceWriter(file: java.io.File, private val sampleRate: Int) {
    private val codec = MediaCodec.createEncoderByType(MediaFormat.MIMETYPE_AUDIO_AAC)
    private val muxer = MediaMuxer(file.path, MediaMuxer.OutputFormat.MUXER_OUTPUT_MPEG_4)
    private var trackIndex = -1
    private var isMuxerStarted = false
    private var presentationFrames = 0L
    private val info = MediaCodec.BufferInfo()

    init {
        val format = MediaFormat.createAudioFormat(MediaFormat.MIMETYPE_AUDIO_AAC, sampleRate, 1).apply {
            setInteger(MediaFormat.KEY_AAC_PROFILE, MediaCodecInfo.CodecProfileLevel.AACObjectLC)
            setInteger(MediaFormat.KEY_BIT_RATE, 64_000)
            setInteger(MediaFormat.KEY_MAX_INPUT_SIZE, 16_384)
        }
        codec.configure(format, null, null, MediaCodec.CONFIGURE_FLAG_ENCODE)
        codec.start()
    }

    fun write(samples: ShortArray, count: Int) {
        var offset = 0
        while (offset < count) {
            val index = codec.dequeueInputBuffer(10_000)
            if (index < 0) { drain(); continue }
            val buffer = codec.getInputBuffer(index) ?: continue
            buffer.clear()
            val take = minOf(count - offset, buffer.capacity() / 2)
            for (position in 0 until take) buffer.putShort(samples[offset + position])
            codec.queueInputBuffer(index, 0, take * 2, presentationFrames * 1_000_000L / sampleRate, 0)
            presentationFrames += take
            offset += take
            drain()
        }
    }

    private fun drain(isEnd: Boolean = false) {
        while (true) {
            val index = codec.dequeueOutputBuffer(info, if (isEnd) 10_000 else 0)
            when {
                index == MediaCodec.INFO_OUTPUT_FORMAT_CHANGED -> { trackIndex = muxer.addTrack(codec.outputFormat); muxer.start(); isMuxerStarted = true }
                index >= 0 -> {
                    val buffer = codec.getOutputBuffer(index)
                    if (buffer != null && info.size > 0 && isMuxerStarted && info.flags and MediaCodec.BUFFER_FLAG_CODEC_CONFIG == 0) muxer.writeSampleData(trackIndex, buffer, info)
                    codec.releaseOutputBuffer(index, false)
                    if (info.flags and MediaCodec.BUFFER_FLAG_END_OF_STREAM != 0) return
                }
                else -> if (!isEnd) return else if (index == MediaCodec.INFO_TRY_AGAIN_LATER) return
            }
        }
    }

    fun finish() {
        runCatching {
            val index = codec.dequeueInputBuffer(10_000)
            if (index >= 0) codec.queueInputBuffer(index, 0, 0, presentationFrames * 1_000_000L / sampleRate, MediaCodec.BUFFER_FLAG_END_OF_STREAM)
            drain(isEnd = true)
        }
        runCatching { codec.stop() }
        codec.release()
        if (isMuxerStarted) runCatching { muxer.stop() }
        runCatching { muxer.release() }
    }
}

/**
 * The capture of a recording (ios/TeaNode/Sources/RecordingEngineCapture.swift and RecordingCaptureView.swift): a
 * foreground service so the microphone goes on with the screen off. The microphone is read continuously and written in
 * pieces of twenty to sixty seconds (cut at the end of a sentence the live stream hears, or where the level pauses), each
 * a file the uploader can send while the next is recorded; a pause, or a call, ends the piece and the gap is on the
 * timeline. The same audio goes to the live transcription stream while the phone is online.
 */
class RecordingCaptureService : Service() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private lateinit var journal: RecordingJournal
    private var audioThread: Thread? = null
    @Volatile private var isCapturing = false
    private var liveSocket: RecordingLiveSocket? = null
    private var focusRequest: android.media.AudioFocusRequest? = null
    private var timelineBeforePieceMS = 0L
    private var framesInPiece = 0L
    private var pieceStartTimelineMS = 0L
    private var pieceMediaId: String? = null
    private var pieceWriter: PieceWriter? = null
    private val cutPolicy = RecordingCutPolicy()
    private val levels = RecordingLevelHistory()
    @Volatile private var lastTimelineMS = 0L
    private var lastPublishedAtMS = 0L

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        journal = RecordingJournal(this)
        val manager = getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(NotificationChannel(CHANNEL, getString(R.string.recordings_channel_name), NotificationManager.IMPORTANCE_LOW))
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_START -> start(intent.getStringExtra(EXTRA_ACCOUNT_KEY).orEmpty(), intent.getStringExtra(EXTRA_USER_ID), intent.getStringExtra(EXTRA_TITLE).orEmpty())
            ACTION_PAUSE -> pause(isInterruption = false)
            ACTION_RESUME -> resume()
            ACTION_STOP -> stop()
        }
        return START_NOT_STICKY
    }

    private fun notification(isPaused: Boolean): Notification {
        val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP), PendingIntent.FLAG_IMMUTABLE)
        fun action(actionName: String, labelId: Int) = NotificationCompat.Action(
            0, getString(labelId), PendingIntent.getService(this, actionName.hashCode(), Intent(this, RecordingCaptureService::class.java).setAction(actionName), PendingIntent.FLAG_IMMUTABLE),
        )
        return NotificationCompat.Builder(this, CHANNEL).setSmallIcon(R.mipmap.ic_launcher).setContentTitle(getString(if (isPaused) R.string.recordings_notification_paused else R.string.recordings_notification_recording))
            .setContentIntent(open).setOngoing(true).setOnlyAlertOnce(true)
            .addAction(if (isPaused) action(ACTION_RESUME, R.string.recordings_resume) else action(ACTION_PAUSE, R.string.recordings_pause))
            .addAction(action(ACTION_STOP, R.string.recordings_stop)).build()
    }

    private fun start(accountKey: String, userId: String?, title: String) {
        if (stateFlow.value.isRecording) return
        if (Build.VERSION.SDK_INT >= 29) startForeground(NOTIFICATION_ID, notification(false), ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE) else startForeground(NOTIFICATION_ID, notification(false))
        val local = LocalRecording(id = UUID.randomUUID().toString(), accountKey = accountKey, recordingUserId = userId, recordingTitle = title, recordingOrigin = "capture", startedAtEpochMS = System.currentTimeMillis())
        journal.create(local)
        timelineBeforePieceMS = 0
        stateFlow.value = RecordingCaptureState(localRecordingId = local.id, isRecording = true)
        requestFocus()
        if (!beginCapture(local.id)) return
        RecordingUploadScheduler.schedule(this)
        startLiveStream(local)
    }

    /** Makes the recording on the server now, when online, so the live stream has a recording to listen for. */
    private fun startLiveStream(local: LocalRecording) {
        val connection = (application as TeaNodeApplication).session.connection.value ?: return
        scope.launch {
            val remoteId = try {
                connection.createRecording(local.clientRecordingId, local.recordingTitle, local.recordingOrigin).id
            } catch (failure: kotlinx.coroutines.CancellationException) {
                throw failure
            } catch (_: Throwable) {
                return@launch
            }
            journal.update(local.id) { current -> current.copy(recordingId = remoteId) }
            val socket = RecordingLiveSocket(
                scope, connection, remoteId, { lastTimelineMS }, { atMS -> cutPolicy.recordUtteranceEnd(atMS) },
                { isConnected, heard, partialText, partialStartMS ->
                    stateFlow.value = stateFlow.value.copy(isLiveConnected = isConnected, liveUtterances = RecordingLiveWords.shown(emptyList(), heard, partialText, partialStartMS, emptyList()).takeLast(6))
                },
            )
            liveSocket = socket
            if (stateFlow.value.isRecording && !stateFlow.value.isPaused) socket.begin()
        }
    }

    private fun requestFocus() {
        val manager = getSystemService(AudioManager::class.java)
        val request = android.media.AudioFocusRequest.Builder(AudioManager.AUDIOFOCUS_GAIN)
            .setOnAudioFocusChangeListener { change ->
                // A call, or another app taking the microphone, ends the piece; the person resumes when it is over.
                if (change == AudioManager.AUDIOFOCUS_LOSS || change == AudioManager.AUDIOFOCUS_LOSS_TRANSIENT) scope.launch { pause(isInterruption = true) }
            }.build()
        focusRequest = request
        manager.requestAudioFocus(request)
    }

    private fun beginCapture(localRecordingId: String): Boolean {
        val minimum = AudioRecord.getMinBufferSize(SAMPLE_RATE, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT)
        val record = try {
            AudioRecord(MediaRecorder.AudioSource.MIC, SAMPLE_RATE, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT, maxOf(minimum, SAMPLE_RATE))
        } catch (failure: Exception) {
            return fail()
        }
        if (record.state != AudioRecord.STATE_INITIALIZED) { record.release(); return fail() }
        if (!openPiece(localRecordingId)) { record.release(); return fail() }
        record.startRecording()
        isCapturing = true
        audioThread = Thread {
            val samples = ShortArray(SAMPLE_RATE / 10)
            while (isCapturing) {
                val count = record.read(samples, 0, samples.size)
                if (count <= 0) continue
                handleSamples(localRecordingId, samples, count)
            }
            record.stop()
            record.release()
        }.apply { name = "recording-capture"; start() }
        return true
    }

    private fun fail(): Boolean {
        stateFlow.value = stateFlow.value.copy(isRecording = false, problemMessageId = R.string.recordings_cannot_record)
        stopSelf()
        return false
    }

    private fun openPiece(localRecordingId: String): Boolean {
        val mediaId = UUID.randomUUID().toString()
        return try {
            pieceWriter = PieceWriter(journal.originalFile(localRecordingId, mediaId), SAMPLE_RATE)
            pieceMediaId = mediaId
            framesInPiece = 0
            pieceStartTimelineMS = timelineBeforePieceMS
            cutPolicy.startPiece(pieceStartTimelineMS)
            journal.update(localRecordingId) { current ->
                current.copy(localMedia = current.localMedia + LocalRecordingMedia(mediaId, null, "$mediaId.m4a", "audio/mp4", pieceStartTimelineMS))
            }
            true
        } catch (failure: Exception) {
            false
        }
    }

    /** Closes the piece: its length is the audio written, and the timeline moves on by that much. */
    private fun closePiece(localRecordingId: String) {
        val writer = pieceWriter ?: return
        val mediaId = pieceMediaId
        writer.finish()
        pieceWriter = null
        pieceMediaId = null
        val lengthMS = RecordingPieceBoundary.lengthMS(framesInPiece, SAMPLE_RATE.toDouble())
        timelineBeforePieceMS = RecordingPieceBoundary.nextStartMS(pieceStartTimelineMS, framesInPiece, SAMPLE_RATE.toDouble())
        journal.update(localRecordingId) { current ->
            current.copy(
                localMedia = current.localMedia.map { piece -> if (piece.id == mediaId) piece.copy(mediaDurationMS = lengthMS, originalSizeBytes = journal.originalFile(localRecordingId, piece.id).length()) else piece },
                timelineDurationMS = timelineBeforePieceMS,
            )
        }
    }

    private fun handleSamples(localRecordingId: String, samples: ShortArray, count: Int) {
        pieceWriter?.write(samples, count)
        framesInPiece += count
        val timelineMS = pieceStartTimelineMS + RecordingPieceBoundary.lengthMS(framesInPiece, SAMPLE_RATE.toDouble())
        lastTimelineMS = timelineMS
        var sumSquares = 0.0
        for (position in 0 until count) sumSquares += samples[position].toDouble() * samples[position]
        val root = Math.sqrt(sumSquares / count) / 32768.0
        val decibels = if (root <= 0.0) -160f else (20 * log10(root)).toFloat()
        levels.append(decibels)
        cutPolicy.recordLevel(decibels, timelineMS)
        liveSocket?.let { socket -> if (socket.isConnected) socket.send(downsampled(samples, count)) }
        val now = SystemClock.elapsedRealtime()
        if (now - lastPublishedAtMS >= 100) {
            lastPublishedAtMS = now
            stateFlow.value = stateFlow.value.copy(elapsedMS = timelineMS, levelBars = levels.bars)
        }
        if (cutPolicy.cutReason(timelineMS, liveSocket?.isConnected == true) != null) {
            closePiece(localRecordingId)
            openPiece(localRecordingId)
            RecordingUploadScheduler.schedule(this)
        }
    }

    /** The live stream takes 24 kHz: a simple linear resampling of the 44.1 kHz microphone. */
    private fun downsampled(samples: ShortArray, count: Int): ByteArray {
        val outputCount = (count.toLong() * LIVE_SAMPLE_RATE / SAMPLE_RATE).toInt()
        val bytes = ByteArray(outputCount * 2)
        for (position in 0 until outputCount) {
            val source = position.toDouble() * SAMPLE_RATE / LIVE_SAMPLE_RATE
            val before = source.toInt().coerceAtMost(count - 1)
            val after = (before + 1).coerceAtMost(count - 1)
            val fraction = source - before
            val value = (samples[before] * (1 - fraction) + samples[after] * fraction).toInt().toShort()
            bytes[position * 2] = (value.toInt() and 0xff).toByte()
            bytes[position * 2 + 1] = (value.toInt() shr 8 and 0xff).toByte()
        }
        return bytes
    }

    private fun stopCapturing() {
        isCapturing = false
        audioThread?.join(2_000)
        audioThread = null
    }

    private fun pause(isInterruption: Boolean) {
        val id = stateFlow.value.localRecordingId ?: return
        if (!stateFlow.value.isRecording || stateFlow.value.isPaused) return
        stopCapturing()
        liveSocket?.end()
        closePiece(id)
        journal.update(id) { current -> runCatching { current.paused(timelineBeforePieceMS, if (isInterruption) "interrupted" else "paused") }.getOrDefault(current) }
        stateFlow.value = stateFlow.value.copy(isPaused = true, isInterrupted = isInterruption, elapsedMS = timelineBeforePieceMS)
        getSystemService(NotificationManager::class.java).notify(NOTIFICATION_ID, notification(true))
        RecordingUploadScheduler.schedule(this)
    }

    private fun resume() {
        val id = stateFlow.value.localRecordingId ?: return
        if (!stateFlow.value.isPaused) return
        // The gap is the time the capture was paused: the timeline does not move while nothing is recorded.
        journal.update(id) { current -> runCatching { current.resumed(timelineBeforePieceMS) }.getOrDefault(current) }
        if (!beginCapture(id)) return
        stateFlow.value = stateFlow.value.copy(isPaused = false, isInterrupted = false)
        liveSocket?.begin()
        getSystemService(NotificationManager::class.java).notify(NOTIFICATION_ID, notification(false))
    }

    private fun stop() {
        val id = stateFlow.value.localRecordingId
        stopCapturing()
        liveSocket?.end()
        liveSocket = null
        if (id != null) {
            closePiece(id)
            journal.update(id) { current -> runCatching { current.stopped(timelineBeforePieceMS) }.getOrDefault(current) }
            RecordingUploadScheduler.schedule(this)
        }
        focusRequest?.let { request -> getSystemService(AudioManager::class.java).abandonAudioFocusRequest(request) }
        stateFlow.value = RecordingCaptureState()
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    override fun onDestroy() {
        // A service ended by the system leaves what was recorded: the pieces already closed are kept and the capture is stopped.
        if (isCapturing) stateFlow.value.localRecordingId?.let { id -> stopCapturing(); runCatching { closePiece(id); journal.update(id) { current -> runCatching { current.stopped(timelineBeforePieceMS) }.getOrDefault(current) } } }
        liveSocket?.end()
        stateFlow.value = RecordingCaptureState()
        scope.coroutineContext[Job]?.cancel()
        super.onDestroy()
    }

    companion object {
        private const val SAMPLE_RATE = 44_100
        private const val LIVE_SAMPLE_RATE = 24_000
        private const val CHANNEL = "recording"
        private const val NOTIFICATION_ID = 4101
        const val ACTION_START = "com.teanode.app.recording.START"
        const val ACTION_PAUSE = "com.teanode.app.recording.PAUSE"
        const val ACTION_RESUME = "com.teanode.app.recording.RESUME"
        const val ACTION_STOP = "com.teanode.app.recording.STOP"
        private const val EXTRA_ACCOUNT_KEY = "accountKey"
        private const val EXTRA_USER_ID = "userId"
        private const val EXTRA_TITLE = "title"

        private val stateFlow = MutableStateFlow(RecordingCaptureState())

        /** What the recorder screen shows, read from the service while it runs. */
        val state: StateFlow<RecordingCaptureState> = stateFlow.asStateFlow()

        fun start(context: Context, accountKey: String, userId: String?, title: String) {
            context.startForegroundService(
                Intent(context, RecordingCaptureService::class.java).setAction(ACTION_START).putExtra(EXTRA_ACCOUNT_KEY, accountKey).putExtra(EXTRA_USER_ID, userId).putExtra(EXTRA_TITLE, title),
            )
        }

        fun send(context: Context, action: String) {
            context.startService(Intent(context, RecordingCaptureService::class.java).setAction(action))
        }
    }
}
