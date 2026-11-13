package com.teanode.app.notifications

import android.app.NotificationManager
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import androidx.core.app.RemoteInput
import com.teanode.app.TeaNodeApplication
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.notifications.NotificationInfo
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch

/**
 * NotificationActionReceiver does what a button pressed on a notification says without opening the app: approve, deny,
 * a typed reply, a choice, a mood. The work is done as the account the notification came from, and a failure is
 * posted as a notification, since nobody is looking at the app.
 */
class NotificationActionReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val info = intent.getStringExtra(NotificationBuilder.EXTRA_INFO)
            ?.let { text -> runCatching { TeaNodeJson.decodeFromString(NotificationInfo.serializer(), text) }.getOrNull() } ?: return
        val actionId = intent.getStringExtra(NotificationBuilder.EXTRA_ACTION) ?: return
        val typedText = RemoteInput.getResultsFromIntent(intent)?.getCharSequence(NotificationBuilder.REPLY_KEY)?.toString()
        val notificationId = intent.getIntExtra(NotificationBuilder.EXTRA_NOTIFICATION_ID, -1)
        val push = (context.applicationContext as TeaNodeApplication).push
        val pending = goAsync()
        scope.launch {
            try {
                val outcome = NotificationActionRunner(context.applicationContext, push).perform(info, actionId, typedText)
                // Answered: the notification goes, as a pressed button of an iPhone notification takes it away.
                if (outcome.isDone && notificationId >= 0) context.getSystemService(NotificationManager::class.java)?.cancel(notificationId)
            } finally {
                pending.finish()
            }
        }
    }

    private companion object {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    }
}
