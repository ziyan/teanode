package com.teanode.app.notifications

import android.Manifest
import android.content.Context
import android.content.SharedPreferences
import android.content.pm.PackageManager
import androidx.core.content.ContextCompat
import com.teanode.app.R
import com.teanode.app.calls.CallAudioRouter
import com.teanode.app.calls.CallCenter
import com.teanode.app.calls.CallEnvironment
import com.teanode.app.calls.CallProblem
import com.teanode.app.calls.CallService
import com.teanode.app.calls.TelecomCallSystem
import com.teanode.app.calls.WebRtcCallMedia
import com.teanode.app.session.AppSession
import com.teanode.app.session.ErrorWords
import com.teanode.kit.Connection
import com.teanode.kit.HTTPTransport
import com.teanode.kit.SecretAccountStore
import com.teanode.kit.SecretStore
import com.teanode.kit.SubscriptionTransport
import com.teanode.kit.notifications.PayloadKeyring
import java.util.UUID

/**
 * PushSystem is everything the phone does when a message arrives and nobody is looking at the app: the keys that open
 * a notification, the registration of the phone with the servers, the calls, and the connections to every account on
 * the phone. It lives as long as the process ([com.teanode.app.TeaNodeApplication] holds it), so the messaging
 * service, the receivers and the call service all reach the same one.
 */
class PushSystem(
    private val context: Context,
    secrets: SecretStore,
    private val transport: HTTPTransport,
    private val preferences: SharedPreferences,
    private val subscriptions: SubscriptionTransport?,
    private val sessionProvider: () -> AppSession,
    private val isAppInFront: () -> Boolean,
) {
    val accountStore = SecretAccountStore(secrets)
    val keyring = PayloadKeyring(secrets)
    private val keptConnections = HashMap<String, Connection>()

    /** The connection of an account on the phone: the one in use, or one kept for it, so that a ring or a button of an account not in use acts as that account. Null when the account is gone. */
    @Synchronized
    fun connection(accountKey: String): Connection? {
        val inUse = sessionProvider().connection.value
        if (inUse != null && inUse.accountKey == accountKey) return inUse
        keptConnections[accountKey]?.let { kept -> return kept }
        val account = accountStore.accounts().accounts.firstOrNull { candidate -> candidate.accountKey == accountKey } ?: return null
        return Connection(account, transport, accountStore, subscriptions).also { made -> keptConnections[accountKey] = made }
    }

    /** A name this install gives the turns it asks, which the server uses to tell where an answer goes. */
    @Synchronized
    fun drawerId(): String {
        preferences.getString(DRAWER_ID_KEY, null)?.let { saved -> return saved }
        return UUID.randomUUID().toString().also { made -> preferences.edit().putString(DRAWER_ID_KEY, made).apply() }
    }

    val registrar = PushRegistrar(context, transport, keyring, accountStore, ::connection, preferences)

    private val environment = object : CallEnvironment {
        override val activeAccountKey: String? get() = sessionProvider().connection.value?.accountKey

        override fun connection(accountKey: String): Connection? = this@PushSystem.connection(accountKey)

        override fun phoneId(accountKey: String): String? = registrar.phoneId(accountKey)

        override fun hasMicrophone(): Boolean = hasPermission(Manifest.permission.RECORD_AUDIO)

        override fun hasCamera(): Boolean = hasPermission(Manifest.permission.CAMERA)

        override fun isAppInFront(): Boolean = this@PushSystem.isAppInFront()

        override fun describe(error: Throwable): String = ErrorWords(context).describe(error)

        override fun words(problem: CallProblem): String = context.getString(
            when (problem) {
                CallProblem.MicrophoneRefused -> R.string.calls_problem_microphone
                CallProblem.CameraRefused -> R.string.calls_problem_camera
                CallProblem.AlreadyOnCall -> R.string.calls_problem_already_on_call
                CallProblem.NotSignedIn -> R.string.calls_problem_not_signed_in
                CallProblem.CouldNotStart -> R.string.calls_problem_could_not_start
                CallProblem.LostConnection -> R.string.calls_problem_lost_connection
            },
        )

        override fun callStateChanged() {
            if (calls.session.value != null || calls.ringing.value.isNotEmpty()) CallService.start(context)
        }
    }

    /** The calls of the phone. */
    val calls: CallCenter = CallCenter(environment, { WebRtcCallMedia(context) }, CallAudioRouter(context), TelecomCallSystem(context))

    /** Whether a newer app can be installed. */
    val appUpdates = AppUpdateModel(preferences)

    private fun hasPermission(permission: String): Boolean = ContextCompat.checkSelfPermission(context, permission) == PackageManager.PERMISSION_GRANTED

    private companion object {
        const val DRAWER_ID_KEY = "drawerId"
    }
}
