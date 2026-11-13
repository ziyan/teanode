package com.teanode.app.notifications

import android.app.Notification
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.os.Build
import android.widget.RemoteViews
import androidx.core.app.NotificationCompat
import androidx.core.app.RemoteInput
import com.teanode.app.MainActivity
import com.teanode.app.R
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.notifications.FixedButtonWords
import com.teanode.kit.notifications.NotificationButtonSpec
import com.teanode.kit.notifications.NotificationCategories
import com.teanode.kit.notifications.NotificationChannelKind
import com.teanode.kit.notifications.NotificationContent
import com.teanode.kit.notifications.NotificationInfo
import com.teanode.kit.notifications.PayloadKey
import com.teanode.kit.notifications.ReplyHint

/**
 * NotificationBuilder draws a notification the phone opened: its title, its words, and its buttons. What each press
 * means is decided in the kit ([com.teanode.kit.notifications.NotificationPress]); the receiver and the activity carry
 * it out.
 */
object NotificationBuilder {
    const val EXTRA_INFO = "com.teanode.app.extra.NOTIFICATION_INFO"
    const val EXTRA_ACTION = "com.teanode.app.extra.NOTIFICATION_ACTION"
    const val EXTRA_NOTIFICATION_ID = "com.teanode.app.extra.NOTIFICATION_ID"
    const val REPLY_KEY = "reply"

    /** Android shows three buttons beside a notification; a question with four, or the evening question's five, are drawn in a row of their own. */
    private const val STANDARD_BUTTONS_MOST = 3

    private val buttonSlots = intArrayOf(R.id.notification_button_1, R.id.notification_button_2, R.id.notification_button_3, R.id.notification_button_4, R.id.notification_button_5)

    private var counter = 0

    /** Shows a notification that arrived sealed, once opened. */
    fun show(context: Context, opened: PayloadKey.Opened) {
        val manager = context.getSystemService(NotificationManager::class.java) ?: return
        val notificationId = nextNotificationId()
        manager.notify(notificationId, build(context, opened.content, opened.accountKey, notificationId))
    }

    /** What is shown for a notification that could not be opened: the same line iOS shows for one it cannot read. */
    fun showUnreadable(context: Context) {
        val manager = context.getSystemService(NotificationManager::class.java) ?: return
        val notification = NotificationCompat.Builder(context, NotificationChannelKind.Other.channelId)
            .setSmallIcon(R.drawable.ic_notification)
            .setContentTitle(context.getString(R.string.app_name))
            .setContentText(context.getString(R.string.notifications_new_message))
            .setAutoCancel(true)
            .setContentIntent(openIntent(context, null, nextNotificationId(), NotificationInfo()))
            .build()
        manager.notify(nextNotificationId(), notification)
    }

    @Synchronized
    private fun nextNotificationId(): Int {
        counter = (counter + 1) % 100_000
        return NOTIFICATION_ID_BASE + counter
    }

    fun build(context: Context, content: NotificationContent, accountKey: String, notificationId: Int): Notification {
        val info = NotificationInfo.of(content, accountKey)
        val builder = NotificationCompat.Builder(context, NotificationChannelKind.of(content.notificationKind).channelId)
            .setSmallIcon(R.drawable.ic_notification)
            .setColor(context.getColor(R.color.teanode_leaf))
            .setContentTitle(content.title)
            .setContentText(content.body)
            .setStyle(NotificationCompat.BigTextStyle().bigText(content.body))
            .setAutoCancel(true)
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setContentIntent(openIntent(context, "default", notificationId, info))
        // Messages of one conversation or kind are gathered under one, as iOS gathers them by their thread.
        content.threadId?.takeIf { thread -> thread.isNotEmpty() }?.let { thread -> builder.setGroup(thread) }
        val specs = NotificationCategories.buttons(content)
        if (specs.size <= STANDARD_BUTTONS_MOST) {
            specs.forEach { spec -> builder.addAction(actionOf(context, spec, info, notificationId)) }
        } else {
            // The choices that need no typing share a row of their own; a field to type in stays a button.
            val choices = specs.filter { spec -> !spec.isTextInput }
            specs.filter { spec -> spec.isTextInput }.forEach { spec -> builder.addAction(actionOf(context, spec, info, notificationId)) }
            builder.setStyle(NotificationCompat.DecoratedCustomViewStyle())
            builder.setCustomBigContentView(choicesRow(context, choices, info, notificationId, content))
        }
        return builder.build()
    }

    private fun wordsOf(context: Context, spec: NotificationButtonSpec): String = spec.literalLabel ?: when (spec.fixedWords) {
        FixedButtonWords.DoIt -> context.getString(R.string.notifications_button_do_it)
        FixedButtonWords.Dont -> context.getString(R.string.notifications_button_dont)
        FixedButtonWords.Reply -> context.getString(R.string.notifications_button_reply)
        FixedButtonWords.Open -> context.getString(R.string.notifications_button_open)
        FixedButtonWords.Awful -> context.getString(R.string.notifications_mood_awful)
        FixedButtonWords.Low -> context.getString(R.string.notifications_mood_low)
        FixedButtonWords.Okay -> context.getString(R.string.notifications_mood_okay)
        FixedButtonWords.Good -> context.getString(R.string.notifications_mood_good)
        FixedButtonWords.Great -> context.getString(R.string.notifications_mood_great)
        null -> ""
    }

    private fun actionOf(context: Context, spec: NotificationButtonSpec, info: NotificationInfo, notificationId: Int): NotificationCompat.Action {
        val words = wordsOf(context, spec)
        val pendingIntent = if (spec.doesOpenApp) openIntent(context, spec.actionId, notificationId, info) else broadcastIntent(context, spec, info, notificationId)
        val action = NotificationCompat.Action.Builder(0, words, pendingIntent)
        if (spec.isTextInput) {
            val hint = when (spec.replyHint) {
                ReplyHint.Message -> context.getString(R.string.notifications_reply_hint_message)
                else -> context.getString(R.string.notifications_reply_hint_agent)
            }
            action.addRemoteInput(RemoteInput.Builder(REPLY_KEY).setLabel(hint).build())
            action.setShowsUserInterface(false)
        }
        // An action that does something for the person is not given to a lock screen.
        if (spec.requiresUnlock && Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) action.setAuthenticationRequired(true)
        return action.build()
    }

    private fun choicesRow(context: Context, choices: List<NotificationButtonSpec>, info: NotificationInfo, notificationId: Int, content: NotificationContent): RemoteViews {
        val row = RemoteViews(context.packageName, R.layout.notification_choices)
        row.setTextViewText(R.id.notification_choices_title, content.title)
        row.setTextViewText(R.id.notification_choices_body, content.body)
        buttonSlots.forEachIndexed { index, slot ->
            val spec = choices.getOrNull(index)
            if (spec == null) {
                row.setViewVisibility(slot, android.view.View.GONE)
            } else {
                row.setViewVisibility(slot, android.view.View.VISIBLE)
                row.setTextViewText(slot, wordsOf(context, spec))
                row.setOnClickPendingIntent(
                    slot,
                    if (spec.doesOpenApp) openIntent(context, spec.actionId, notificationId, info) else broadcastIntent(context, spec, info, notificationId),
                )
            }
        }
        return row
    }

    private fun extrasOf(context: Context, intent: Intent, actionId: String?, info: NotificationInfo, notificationId: Int): Intent = OwnIntentSeal.mark(context, intent)
        .putExtra(EXTRA_INFO, TeaNodeJson.encodeToString(NotificationInfo.serializer(), info))
        .putExtra(EXTRA_ACTION, actionId)
        .putExtra(EXTRA_NOTIFICATION_ID, notificationId)

    private fun broadcastIntent(context: Context, spec: NotificationButtonSpec, info: NotificationInfo, notificationId: Int): PendingIntent {
        val intent = extrasOf(context, Intent(context, NotificationActionReceiver::class.java), spec.actionId, info, notificationId)
        // A typed answer is written into the intent by the system, so the intent must be mutable.
        val mutability = if (spec.isTextInput && Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) PendingIntent.FLAG_MUTABLE else PendingIntent.FLAG_IMMUTABLE
        return PendingIntent.getBroadcast(context, requestCode(notificationId, spec.actionId), intent, PendingIntent.FLAG_UPDATE_CURRENT or mutability)
    }

    private fun openIntent(context: Context, actionId: String?, notificationId: Int, info: NotificationInfo): PendingIntent {
        val intent = extrasOf(context, Intent(context, MainActivity::class.java), actionId ?: "default", info, notificationId)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP)
        return PendingIntent.getActivity(context, requestCode(notificationId, actionId ?: "default"), intent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
    }

    private fun requestCode(notificationId: Int, actionId: String): Int = notificationId * 31 + actionId.hashCode()

    private const val NOTIFICATION_ID_BASE = 10_000
}
