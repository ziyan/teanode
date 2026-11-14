package com.teanode.app.notifications

import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import com.teanode.app.R
import com.teanode.kit.notifications.NotificationChannelKind

/** NotificationChannels makes the groups a person can quiet or turn off in Android's settings, one per kind of notification. */
object NotificationChannels {
    /** A turn left running, shown quietly while it goes. */
    const val TURNS_CHANNEL_ID = "turns"

    /** Something that went wrong where a button was pressed and nobody is looking at the app. */
    const val PROBLEMS_CHANNEL_ID = "problems"

    fun create(context: Context) {
        val manager = context.getSystemService(NotificationManager::class.java) ?: return
        val channels = NotificationChannelKind.entries.map { kind ->
            NotificationChannel(kind.channelId, context.getString(nameOf(kind)), NotificationManager.IMPORTANCE_HIGH)
        } + listOf(
            NotificationChannel(TURNS_CHANNEL_ID, context.getString(R.string.notifications_channel_turns), NotificationManager.IMPORTANCE_LOW),
            NotificationChannel(PROBLEMS_CHANNEL_ID, context.getString(R.string.notifications_channel_problems), NotificationManager.IMPORTANCE_DEFAULT),
        )
        manager.createNotificationChannels(channels)
    }

    private fun nameOf(kind: NotificationChannelKind): Int = when (kind) {
        NotificationChannelKind.Agent -> R.string.notifications_channel_agent
        NotificationChannelKind.Chats -> R.string.notifications_channel_chats
        NotificationChannelKind.Circles -> R.string.notifications_channel_circles
        NotificationChannelKind.Places -> R.string.notifications_channel_places
        NotificationChannelKind.Coding -> R.string.notifications_channel_coding
        NotificationChannelKind.Journal -> R.string.notifications_channel_journal
        NotificationChannelKind.Photos -> R.string.notifications_channel_photos
        NotificationChannelKind.Other -> R.string.notifications_channel_other
    }
}
