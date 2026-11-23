package com.teanode.app.notifications

import android.content.Context
import androidx.core.app.NotificationManagerCompat
import com.google.firebase.FirebaseApp
import com.google.firebase.messaging.FirebaseMessaging
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlin.coroutines.resume

/**
 * PushAvailability says whether this build can receive push at all. Firebase is optional at build time: the Gradle
 * plugin that reads `google-services.json` is applied only when that file exists, so a checkout without it builds and
 * runs with push off. Firebase Messaging is therefore only asked for anything once the default FirebaseApp has
 * initialized; without it, `FirebaseMessaging.getInstance()` would throw.
 */
object PushAvailability {
    /** Whether the default FirebaseApp initialized, which is the case only in a build made with the Firebase config file. */
    fun isFirebaseReady(context: Context): Boolean = try {
        FirebaseApp.getApps(context).isNotEmpty()
    } catch (_: Throwable) {
        false
    }

    /** Whether the person lets TeaNode show notifications (the permission on Android 13 and later, the app's switch before). */
    fun areNotificationsAllowed(context: Context): Boolean = NotificationManagerCompat.from(context).areNotificationsEnabled()

    /** This install's Firebase token; null when Firebase is not set up here or could not give one. */
    suspend fun token(context: Context): String? {
        if (!isFirebaseReady(context)) return null
        return suspendCancellableCoroutine { continuation ->
            try {
                FirebaseMessaging.getInstance().token.addOnCompleteListener { task ->
                    if (continuation.isActive) continuation.resume(if (task.isSuccessful) task.result else null)
                }
            } catch (_: Throwable) {
                if (continuation.isActive) continuation.resume(null)
            }
        }
    }
}
