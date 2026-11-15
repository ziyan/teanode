package com.teanode.app.notifications

import android.app.NotificationManager
import android.content.Context
import com.teanode.kit.notifications.TurnUpdate
import android.content.Intent
import android.app.PendingIntent
import androidx.core.app.NotificationCompat
import com.teanode.app.MainActivity
import com.teanode.app.R

/**
 * TurnNotification is a turn left running, shown as an ongoing notification while the agent works on it: whose agent it
 * is and one word for where the turn is (working, waiting, done or failed). It is the Android side of the iPhone app's
 * Live Activity for a turn (`AgentTurnAttributes`). While the app's process lives the chats part, which follows the
 * conversation's feed, calls it as the turn changes; when the app leaves with a turn running it also asks the server to
 * push each change as a data message of push kind `turn` ([TurnFollowing], [handlePush]), which reaches a process the
 * system has stopped. The agent's name is kept when the turn is left, so a push can use it.
 */
object TurnNotification {
    private const val NOTIFICATION_ID = 7301

    private const val PREFERENCES_NAME = "turn_notification"
    private const val AGENT_TITLE_KEY = "agentTitle"
    private const val LAST_PUSH_CHANGED_AT_KEY = "lastPushChangedAt"

    /** Keeps the agent's name for the pushes that follow. */
    fun rememberAgentTitle(context: Context, agentTitle: String) {
        context.getSharedPreferences(PREFERENCES_NAME, Context.MODE_PRIVATE).edit().putString(AGENT_TITLE_KEY, agentTitle).apply()
    }

    /** A run is about to be followed by push: the order of the last run's updates no longer applies. */
    fun forgetPushOrder(context: Context) {
        context.getSharedPreferences(PREFERENCES_NAME, Context.MODE_PRIVATE).edit().putLong(LAST_PUSH_CHANGED_AT_KEY, 0L).apply()
    }

    /** The agent's name as kept, or the app's own name when none was. */
    fun rememberedAgentTitle(context: Context): String =
        context.getSharedPreferences(PREFERENCES_NAME, Context.MODE_PRIVATE).getString(AGENT_TITLE_KEY, null)?.takeIf { title -> title.isNotBlank() }
            ?: context.getString(R.string.app_name)

    /**
     * What a `turn` message does: shows the turn's state, unless the app is in front (the turn is on screen there) or
     * it is older than one already shown.
     */
    fun handlePush(context: Context, data: Map<String, String>, isAppInFront: Boolean) {
        val update = TurnUpdate.parse(data) ?: return
        if (isAppInFront) {
            end(context)
            return
        }
        val preferences = context.getSharedPreferences(PREFERENCES_NAME, Context.MODE_PRIVATE)
        if (!update.isNewerThan(preferences.getLong(LAST_PUSH_CHANGED_AT_KEY, 0L))) return
        preferences.edit().putLong(LAST_PUSH_CHANGED_AT_KEY, update.activityChangedAtEpochSeconds).apply()
        NotificationChannels.create(context)
        show(context, rememberedAgentTitle(context), update.activityState)
    }

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
