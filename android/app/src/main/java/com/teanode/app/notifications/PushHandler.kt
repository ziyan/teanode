package com.teanode.app.notifications

import android.content.Context
import com.teanode.app.TeaNodeApplication
import com.teanode.kit.notifications.PayloadKey

/**
 * PushHandler is what happens to a message Firebase delivers. It is data only: `sealed` (the content, encrypted for
 * this phone), `pushKind` (`notification`, or `call` for a ring) and `threadId`. The sealed content is opened with the
 * key of each account on the phone in turn, as the iPhone's notification service extension opens its payload; the key
 * that opens it says which account sent it.
 */
object PushHandler {
    const val DATA_SEALED = "sealed"
    const val DATA_PUSH_KIND = "pushKind"
    const val DATA_THREAD_ID = "threadId"
    const val PUSH_KIND_NOTIFICATION = "notification"
    const val PUSH_KIND_CALL = "call"

    fun handle(context: Context, data: Map<String, String>) {
        val push = (context.applicationContext as TeaNodeApplication).push
        val sealed = data[DATA_SEALED]
        val opened = sealed?.let { text -> PayloadKey.open(text, push.keyring.keys(push.accountStore.accounts())) }
        if (data[DATA_PUSH_KIND] == PUSH_KIND_CALL) {
            // A ring that cannot be read, or is not for a call, shows nothing.
            opened?.let { found -> push.calls.receiveRing(found) }
            return
        }
        NotificationChannels.create(context)
        if (opened == null) {
            // One that cannot be opened is shown as it came: the words that carry nothing.
            NotificationBuilder.showUnreadable(context)
            return
        }
        // A call that stopped ringing arrives as an ordinary push of its own kind on a phone with no ring registration.
        if (opened.content.notificationKind == "call_ended") {
            push.calls.receiveRing(opened)
            return
        }
        NotificationBuilder.show(context, opened)
    }
}
