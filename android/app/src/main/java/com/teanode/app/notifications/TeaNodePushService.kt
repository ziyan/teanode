package com.teanode.app.notifications

import com.google.firebase.messaging.FirebaseMessagingService
import com.google.firebase.messaging.RemoteMessage
import com.teanode.app.TeaNodeApplication
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch

/**
 * TeaNodePushService receives Firebase's data messages and the new tokens it hands out. It is declared in the manifest
 * always, and does nothing in a build without a Firebase project: Firebase never starts then, so it never calls it.
 */
class TeaNodePushService : FirebaseMessagingService() {
    override fun onNewToken(token: String) {
        val application = application as TeaNodeApplication
        scope.launch { application.push.registrar.onNewToken(token, application.session.connection.value) }
    }

    override fun onMessageReceived(message: RemoteMessage) {
        PushHandler.handle(applicationContext, message.data)
    }

    private companion object {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    }
}
