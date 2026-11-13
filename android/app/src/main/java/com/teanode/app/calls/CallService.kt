package com.teanode.app.calls

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.media.AudioAttributes
import android.media.RingtoneManager
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import androidx.core.app.Person
import androidx.core.content.ContextCompat
import com.teanode.app.MainActivity
import com.teanode.app.R
import com.teanode.app.TeaNodeApplication
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.launch

/**
 * CallService keeps the app alive and visible while a call rings or is on. It is a foreground service of the phone
 * call type, and shows what [CallCenter] holds: a full-screen incoming call over the lock screen (Answer and Decline,
 * the ringtone on its channel), or an ongoing call (Mute and Hang up). It stops itself when neither is left. It is
 * the Android side of what CallKit does on the iPhone.
 */
class CallService : Service() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private var observeJob: Job? = null
    private var isForeground = false
    private val shownRingIds = mutableSetOf<String>()

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        createChannels(this)
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val center = (application as TeaNodeApplication).push.calls
        when (intent?.action) {
            ACTION_DECLINE -> intent.getStringExtra(EXTRA_ENTRY_ID)?.let { entryId -> center.declineRing(entryId) }
            ACTION_HANG_UP -> center.hangUp()
            ACTION_TOGGLE_MUTE -> center.session.value?.let { state -> center.setMuted(!state.isMuted) }
        }
        // The service has to be in the foreground at once after it is started this way, whatever the call is doing.
        render(forceForeground = true)
        if (observeJob == null) {
            observeJob = scope.launch {
                combine(center.session, center.ringing, center.agentVoice) { _, _, _ -> }.collect { render(forceForeground = false) }
            }
        }
        return START_NOT_STICKY
    }

    private fun render(forceForeground: Boolean) {
        val center = (application as TeaNodeApplication).push.calls
        val manager = getSystemService(NotificationManager::class.java)
        val state = center.session.value
        val rings = center.ringing.value
        // A ring that is gone takes its notification away.
        shownRingIds.filter { shown -> rings.none { ring -> ring.entryId == shown } }.forEach { gone ->
            manager.cancel(ringNotificationId(gone))
            shownRingIds.remove(gone)
        }
        when {
            state == null && rings.isEmpty() && center.agentVoice.value != null -> {
                val voice = center.agentVoice.value!!
                goForeground(ONGOING_NOTIFICATION_ID, agentVoiceNotification(voice), isOnCall = true, isCameraOn = false)
            }
            state != null -> {
                rings.forEach { ring -> manager.cancel(ringNotificationId(ring.entryId)) }
                goForeground(ONGOING_NOTIFICATION_ID, ongoingNotification(state), isOnCall = true, isCameraOn = state.isCameraOn)
            }
            rings.isNotEmpty() -> {
                val first = rings.first()
                rings.drop(1).forEach { ring -> manager.notify(ringNotificationId(ring.entryId), ringNotification(ring)) }
                shownRingIds.addAll(rings.map { ring -> ring.entryId })
                goForeground(ringNotificationId(first.entryId), ringNotification(first), isOnCall = false, isCameraOn = false)
            }
            else -> {
                if (forceForeground && !isForeground) goForeground(ONGOING_NOTIFICATION_ID, emptyNotification(), isOnCall = false, isCameraOn = false)
                stopSelfCleanly()
            }
        }
    }

    private fun goForeground(notificationId: Int, notification: Notification, isOnCall: Boolean, isCameraOn: Boolean) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            var type = ServiceInfo.FOREGROUND_SERVICE_TYPE_PHONE_CALL
            if (isOnCall && hasPermission(Manifest.permission.RECORD_AUDIO)) type = type or ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE
            if (isOnCall && isCameraOn && hasPermission(Manifest.permission.CAMERA)) type = type or ServiceInfo.FOREGROUND_SERVICE_TYPE_CAMERA
            try {
                startForeground(notificationId, notification, type)
            } catch (_: SecurityException) {
                // The system refused the microphone or camera type from where the call was started: the phone call type alone is kept.
                runCatching { startForeground(notificationId, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_PHONE_CALL) }
            }
        } else {
            startForeground(notificationId, notification)
        }
        isForeground = true
    }

    private fun hasPermission(permission: String): Boolean = ContextCompat.checkSelfPermission(this, permission) == PackageManager.PERMISSION_GRANTED

    private fun stopSelfCleanly() {
        observeJob?.cancel()
        observeJob = null
        stopForeground(STOP_FOREGROUND_REMOVE)
        isForeground = false
        stopSelf()
    }

    private fun emptyNotification(): Notification = NotificationCompat.Builder(this, ONGOING_CHANNEL_ID)
        .setSmallIcon(R.drawable.ic_notification)
        .setContentTitle(getString(R.string.calls_notification_call_in_progress))
        .build()

    private fun ringNotification(ring: IncomingRing): Notification {
        val caller = Person.Builder().setName(ring.callerName).setImportant(true).build()
        val answer = PendingIntent.getActivity(
            this, ring.entryId.hashCode() + 1, IncomingCallActivity.intent(this, ring.entryId, shouldAnswer = true),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val decline = PendingIntent.getService(
            this, ring.entryId.hashCode() + 2, Intent(this, CallService::class.java).setAction(ACTION_DECLINE).putExtra(EXTRA_ENTRY_ID, ring.entryId),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val fullScreen = PendingIntent.getActivity(
            this, ring.entryId.hashCode() + 3, IncomingCallActivity.intent(this, ring.entryId, shouldAnswer = false),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        return NotificationCompat.Builder(this, INCOMING_CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(ring.callerName)
            .setContentText(getString(R.string.calls_incoming_call))
            .setCategory(NotificationCompat.CATEGORY_CALL)
            .setPriority(NotificationCompat.PRIORITY_MAX)
            .setOngoing(true)
            .setAutoCancel(false)
            .setTimeoutAfter(RING_TIMEOUT_MS)
            .setFullScreenIntent(fullScreen, true)
            .setContentIntent(fullScreen)
            .setStyle(NotificationCompat.CallStyle.forIncomingCall(caller, decline, answer))
            .addPerson(caller)
            .build()
            .also { built -> built.flags = built.flags or Notification.FLAG_INSISTENT }
    }

    private fun agentVoiceNotification(voice: AgentVoiceCall): Notification {
        val open = PendingIntent.getActivity(
            this, 4, Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_REORDER_TO_FRONT),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val hangUp = PendingIntent.getService(
            this, 5, Intent(this, CallService::class.java).setAction(ACTION_HANG_UP), PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val person = Person.Builder().setName(voice.agentName).setImportant(true).build()
        return NotificationCompat.Builder(this, ONGOING_CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(voice.agentName)
            .setContentText(getString(R.string.calls_notification_call_in_progress))
            .setCategory(NotificationCompat.CATEGORY_CALL)
            .setOngoing(true)
            .setUsesChronometer(true)
            .setContentIntent(open)
            .setStyle(NotificationCompat.CallStyle.forOngoingCall(person, hangUp))
            .build()
    }

    private fun ongoingNotification(state: CallSessionState): Notification {
        val open = PendingIntent.getActivity(
            this, 4, Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_REORDER_TO_FRONT),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val hangUp = PendingIntent.getService(
            this, 5, Intent(this, CallService::class.java).setAction(ACTION_HANG_UP), PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val toggleMute = PendingIntent.getService(
            this, 6, Intent(this, CallService::class.java).setAction(ACTION_TOGGLE_MUTE), PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val person = Person.Builder().setName(state.title.ifEmpty { getString(R.string.calls_notification_call_in_progress) }).setImportant(true).build()
        val isConnected = state.phase == CallPhase.Connected
        return NotificationCompat.Builder(this, ONGOING_CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(state.title.ifEmpty { getString(R.string.calls_notification_call_in_progress) })
            .setContentText(
                getString(
                    when {
                        state.isMuted -> R.string.calls_muted
                        isConnected -> R.string.calls_notification_call_in_progress
                        state.phase == CallPhase.Reconnecting -> R.string.calls_reconnecting
                        else -> R.string.calls_connecting
                    },
                ),
            )
            .setCategory(NotificationCompat.CATEGORY_CALL)
            .setOngoing(true)
            .setUsesChronometer(isConnected)
            .apply { state.call?.answeredEpochMS?.let { answered -> if (isConnected) setWhen(answered) } }
            .setContentIntent(open)
            .addAction(0, getString(if (state.isMuted) R.string.calls_unmute else R.string.calls_mute), toggleMute)
            .setStyle(NotificationCompat.CallStyle.forOngoingCall(person, hangUp))
            .build()
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }

    companion object {
        const val INCOMING_CHANNEL_ID = "calls_incoming"
        const val ONGOING_CHANNEL_ID = "calls_ongoing"
        const val ONGOING_NOTIFICATION_ID = 7102
        const val ACTION_DECLINE = "com.teanode.app.calls.DECLINE"
        const val ACTION_HANG_UP = "com.teanode.app.calls.HANG_UP"
        const val ACTION_TOGGLE_MUTE = "com.teanode.app.calls.TOGGLE_MUTE"
        const val EXTRA_ENTRY_ID = "com.teanode.app.calls.ENTRY_ID"

        /** A ring is shown for as long as the server rings: a minute, as FCM keeps it. */
        private const val RING_TIMEOUT_MS = 60_000L

        private fun ringNotificationId(entryId: String): Int = 7200 + Math.floorMod(entryId.hashCode(), 800)

        /** Starts the service; from a high priority push or from the app in front, which is where Android allows it. */
        fun start(context: Context) {
            try {
                ContextCompat.startForegroundService(context, Intent(context, CallService::class.java))
            } catch (_: IllegalStateException) {
                // The system did not allow it from here (the app is in the background and was not woken by a high priority push): the state is kept, and the next change tries again.
            }
        }

        fun createChannels(context: Context) {
            val manager = context.getSystemService(NotificationManager::class.java)
            val ringtone = RingtoneManager.getDefaultUri(RingtoneManager.TYPE_RINGTONE)
            val incoming = NotificationChannel(INCOMING_CHANNEL_ID, context.getString(R.string.calls_channel_incoming), NotificationManager.IMPORTANCE_HIGH).apply {
                setSound(ringtone, AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_NOTIFICATION_RINGTONE).setContentType(AudioAttributes.CONTENT_TYPE_SONIFICATION).build())
                enableVibration(true)
                vibrationPattern = longArrayOf(0, 800, 600, 800, 600)
                lockscreenVisibility = Notification.VISIBILITY_PUBLIC
            }
            val ongoing = NotificationChannel(ONGOING_CHANNEL_ID, context.getString(R.string.calls_channel_ongoing), NotificationManager.IMPORTANCE_LOW)
            manager.createNotificationChannels(listOf(incoming, ongoing))
        }
    }
}
