package com.teanode.app.recordings

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.media.MediaRecorder
import android.os.Build
import android.os.IBinder
import android.os.SystemClock
import androidx.core.app.NotificationCompat
import com.teanode.app.MainActivity
import com.teanode.app.R
import com.teanode.kit.recordings.LocalRecording
import com.teanode.kit.recordings.LocalRecordingMedia
import com.teanode.kit.recordings.RecordingCutPolicy
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
)

/**
 * The capture of a recording (ios/TeaNode/Sources/RecordingEngineCapture.swift and RecordingCaptureView.swift): a
 * foreground service so the microphone goes on with the screen off. Audio is written in pieces of twenty to sixty seconds
 * (cut where the speaker pauses, by [RecordingCutPolicy]), each a file the uploader can send while the next is recorded; a
 * pause or a call ends the piece and the gap is on the timeline, so the transcript lines up with the time that passed.
 */
class RecordingCaptureService : Service() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private lateinit var journal: RecordingJournal
    private var recorder: MediaRecorder? = null
    private var pieceMediaId: String? = null
    private var pieceStartedUptimeMS = 0L
    private var pieceStartTimelineMS = 0L
    private var timelineBeforePieceMS = 0L
    private var ticker: Job? = null
    private val cutPolicy = RecordingCutPolicy()
    private val levels = RecordingLevelHistory()

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
        if (!startPiece(local.id)) return
        RecordingUploadScheduler.schedule(this)
        ticker = scope.launch {
            while (isActive) {
                delay(100)
                tick(local.id)
            }
        }
    }

    private fun startPiece(localRecordingId: String): Boolean {
        val mediaId = UUID.randomUUID().toString()
        val file = journal.originalFile(localRecordingId, mediaId)
        val newRecorder = if (Build.VERSION.SDK_INT >= 31) MediaRecorder(this) else @Suppress("DEPRECATION") MediaRecorder()
        return try {
            newRecorder.setAudioSource(MediaRecorder.AudioSource.MIC)
            newRecorder.setOutputFormat(MediaRecorder.OutputFormat.MPEG_4)
            newRecorder.setAudioEncoder(MediaRecorder.AudioEncoder.AAC)
            newRecorder.setAudioSamplingRate(44_100)
            newRecorder.setAudioEncodingBitRate(64_000)
            newRecorder.setOutputFile(file.path)
            newRecorder.prepare()
            newRecorder.start()
            recorder = newRecorder
            pieceMediaId = mediaId
            pieceStartedUptimeMS = SystemClock.elapsedRealtime()
            pieceStartTimelineMS = timelineBeforePieceMS
            cutPolicy.startPiece(pieceStartTimelineMS)
            journal.update(localRecordingId) { current ->
                current.copy(localMedia = current.localMedia + LocalRecordingMedia(mediaId, null, "$mediaId.m4a", "audio/mp4", pieceStartTimelineMS))
            }
            true
        } catch (failure: Exception) {
            runCatching { newRecorder.release() }
            file.delete()
            stateFlow.value = stateFlow.value.copy(isRecording = false, problemMessageId = R.string.recordings_cannot_record)
            stopSelf()
            false
        }
    }

    /** Ends the piece being recorded: its length is how long it ran, and the timeline moves on by that much. */
    private fun endPiece(localRecordingId: String): Long {
        val active = recorder ?: return timelineBeforePieceMS
        val mediaId = pieceMediaId
        val lengthMS = SystemClock.elapsedRealtime() - pieceStartedUptimeMS
        runCatching { active.stop() }
        active.release()
        recorder = null
        pieceMediaId = null
        timelineBeforePieceMS = pieceStartTimelineMS + lengthMS
        journal.update(localRecordingId) { current ->
            current.copy(localMedia = current.localMedia.map { piece -> if (piece.id == mediaId) piece.copy(mediaDurationMS = lengthMS, originalSizeBytes = journal.originalFile(localRecordingId, piece.id).length()) else piece }, timelineDurationMS = timelineBeforePieceMS)
        }
        return timelineBeforePieceMS
    }

    private fun tick(localRecordingId: String) {
        val active = recorder ?: return
        val elapsedInPieceMS = SystemClock.elapsedRealtime() - pieceStartedUptimeMS
        val timelineMS = pieceStartTimelineMS + elapsedInPieceMS
        val amplitude = runCatching { active.maxAmplitude }.getOrDefault(0)
        val decibels = if (amplitude <= 0) -160f else (20 * log10(amplitude / 32767.0)).toFloat()
        levels.append(decibels)
        cutPolicy.recordLevel(decibels, timelineMS)
        stateFlow.value = stateFlow.value.copy(elapsedMS = timelineMS, levelBars = levels.bars)
        // No live stream here, so a piece is cut where the level pauses, or at a minute.
        if (cutPolicy.cutReason(timelineMS, isStreamConnected = false) != null) {
            endPiece(localRecordingId)
            startPiece(localRecordingId)
            RecordingUploadScheduler.schedule(this)
        }
    }

    private fun pause(isInterruption: Boolean) {
        val id = stateFlow.value.localRecordingId ?: return
        if (!stateFlow.value.isRecording || stateFlow.value.isPaused) return
        ticker?.cancel()
        val timelineMS = endPiece(id)
        journal.update(id) { current -> runCatching { current.paused(timelineMS, if (isInterruption) "interrupted" else "paused") }.getOrDefault(current) }
        stateFlow.value = stateFlow.value.copy(isPaused = true, elapsedMS = timelineMS)
        getSystemService(NotificationManager::class.java).notify(NOTIFICATION_ID, notification(true))
        RecordingUploadScheduler.schedule(this)
    }

    private fun resume() {
        val id = stateFlow.value.localRecordingId ?: return
        if (!stateFlow.value.isPaused) return
        // The gap is the time the capture was paused: the timeline does not move while nothing is recorded.
        journal.update(id) { current -> runCatching { current.resumed(timelineBeforePieceMS) }.getOrDefault(current) }
        if (!startPiece(id)) return
        stateFlow.value = stateFlow.value.copy(isPaused = false)
        getSystemService(NotificationManager::class.java).notify(NOTIFICATION_ID, notification(false))
        ticker = scope.launch { while (isActive) { delay(100); tick(id) } }
    }

    private fun stop() {
        val id = stateFlow.value.localRecordingId
        ticker?.cancel()
        if (id != null) {
            val timelineMS = if (recorder != null) endPiece(id) else timelineBeforePieceMS
            journal.update(id) { current -> runCatching { current.stopped(timelineMS) }.getOrDefault(current) }
            RecordingUploadScheduler.schedule(this)
        }
        stateFlow.value = RecordingCaptureState()
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    override fun onDestroy() {
        ticker?.cancel()
        // A service ended by the system leaves what was recorded: the pieces already closed are kept and the capture is stopped.
        if (recorder != null) stateFlow.value.localRecordingId?.let { id -> runCatching { val timelineMS = endPiece(id); journal.update(id) { current -> runCatching { current.stopped(timelineMS) }.getOrDefault(current) } } }
        stateFlow.value = RecordingCaptureState()
        scope.coroutineContext[Job]?.cancel()
        super.onDestroy()
    }

    companion object {
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
