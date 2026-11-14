package com.teanode.app.notifications

import android.content.SharedPreferences
import com.teanode.app.BuildConfig
import com.teanode.kit.Connection
import com.teanode.kit.notifications.AppUpdate
import com.teanode.kit.notifications.appUpdate
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine

/**
 * AppUpdateModel knows whether a newer app can be installed: it asks the server what is newest on the Android channel
 * and keeps the person's "not now" for that update, so that the next one is offered again. A build made on a
 * developer's machine (debug) is offered nothing. It is the Android side of the iPhone app's `AppUpdateModel`.
 */
class AppUpdateModel(private val preferences: SharedPreferences, private val isDebugBuild: Boolean = BuildConfig.DEBUG) {
    private val newer = MutableStateFlow<AppUpdate?>(null)

    /** The newer app, whether or not the person put it off: Settings keeps offering it after the card has gone. */
    val update: StateFlow<AppUpdate?> = newer.asStateFlow()

    private val dismissedKey = MutableStateFlow(preferences.getString(DISMISSAL_KEY, null))
    private var askedAtMS = 0L

    /** The update to bring up, when the person has not put it off. */
    val updateToOffer = combine(newer, dismissedKey) { found, dismissed -> found?.takeIf { update -> update.dismissalKey != dismissed } }

    suspend fun refresh(connection: Connection, nowMS: Long = System.currentTimeMillis()) {
        if (isDebugBuild) return
        if (askedAtMS != 0L && nowMS - askedAtMS < ASK_EVERY_MS) return
        askedAtMS = nowMS
        // Nothing to say about a question that failed: the next one may not.
        val found = try {
            connection.appUpdate()
        } catch (_: kotlin.coroutines.cancellation.CancellationException) {
            askedAtMS = 0L
            return
        } catch (_: Exception) {
            null
        }
        newer.value = found?.takeIf { update -> update.isNewer(BuildConfig.VERSION_NAME, BuildConfig.VERSION_CODE.toString()) }
    }

    /** Offers an update that was found elsewhere (the demo's). */
    fun offer(update: AppUpdate) {
        newer.value = update
    }

    fun dismiss() {
        val shown = newer.value ?: return
        dismissedKey.value = shown.dismissalKey
        preferences.edit().putString(DISMISSAL_KEY, shown.dismissalKey).apply()
    }

    private companion object {
        const val DISMISSAL_KEY = "dismissedAppUpdate"

        /** How often it asks. The app comes to the front many times a day, and builds reach testers a few times a day at most. */
        const val ASK_EVERY_MS = 60L * 60L * 1000L
    }
}
