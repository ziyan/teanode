package com.teanode.app.notifications

import android.app.NotificationManager
import android.content.Context
import android.content.Intent
import android.app.PendingIntent
import androidx.core.app.NotificationCompat
import com.teanode.app.MainActivity
import com.teanode.app.R

/**
 * TurnNotification is a turn left running, shown as an ongoing notification while the agent works on it: whose agent it
 * is and one word for where the turn is (working, waiting, done or failed). It is the Android side of the iPhone app's
 * Live Activity for a turn (`AgentTurnAttributes`); the chats part, which follows the conversation's feed, calls it as
 * the turn changes, since Android has no push for it.
 */
object TurnNotification {
    private const val NOTIFICATION_ID = 7301

    /** The words of a turn's state. */
    fun wordsOf(context: Context, activityState: String): String = context.getString(
        when (activityState) {
            "waiting" -> R.string.notifications_turn_waiting
            "done" -> R.string.notifications_turn_done
            "failed" -> R.string.notifications_turn_failed
            else -> R.string.notifications_turn_working
        },
    )

    /** Whether the notification stays while the turn goes on (working, waiting), or goes by itself (done, failed). */
    fun isOngoing(activityState: String): Boolean = activityState != "done" && activityState != "failed"

    /** Shows or updates the notification of the turn. */
    fun show(context: Context, agentTitle: String, activityState: String) {
        val manager = context.getSystemService(NotificationManager::class.java) ?: return
        val open = PendingIntent.getActivity(
            context, 0, Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_REORDER_TO_FRONT),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val notification = NotificationCompat.Builder(context, NotificationChannels.TURNS_CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(agentTitle)
            .setContentText(wordsOf(context, activityState))
            .setOngoing(isOngoing(activityState))
            .setAutoCancel(!isOngoing(activityState))
            .setTimeoutAfter(if (isOngoing(activityState)) 0 else DONE_TIMEOUT_MS)
            .setOnlyAlertOnce(true)
            .setContentIntent(open)
            .build()
        manager.notify(NOTIFICATION_ID, notification)
    }

    /** Takes the notification away, when the turn is over and nothing is left to say. */
    fun end(context: Context) {
        context.getSystemService(NotificationManager::class.java)?.cancel(NOTIFICATION_ID)
    }

    private const val DONE_TIMEOUT_MS = 10_000L
}
