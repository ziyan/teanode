package com.teanode.app.notifications

import android.content.Context
import android.content.SharedPreferences
import android.os.Build
import android.provider.Settings
import com.teanode.kit.Connection
import com.teanode.kit.HTTPTransport
import com.teanode.kit.SecretAccountStore
import com.teanode.kit.notifications.AgentPhone
import com.teanode.kit.notifications.PayloadKeyring
import com.teanode.kit.notifications.PushRelay
import com.teanode.kit.notifications.registerPhone
import com.teanode.kit.notifications.updatePhone
import com.teanode.kit.calls.updatePhoneRinging
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

/**
 * PushRegistrar is how this phone is told things and rung for calls. After connecting, and each time the app starts,
 * it asks Firebase for the phone's token, has the push relay seal one registration for notifications and one for
 * rings (both made from the one token, as push types alert and voip), and gives both to the server of every account,
 * each with the key its notifications are encrypted with. A new token, or a registration the relay no longer takes,
 * heals the same way. It is the Android side of the iPhone app's `AppModel.register`.
 *
 * A phone that cannot receive push (the person refused notifications, or this build has no Firebase project) is
 * listed on the server all the same, as a phone that is told nothing.
 */
class PushRegistrar(
    private val context: Context,
    private val transport: HTTPTransport,
    private val keyring: PayloadKeyring,
    private val accountStore: SecretAccountStore,
    private val connectionFor: (accountKey: String) -> Connection?,
    private val preferences: SharedPreferences,
) {
    private val registeringLock = Mutex()
    private val phones = MutableStateFlow<Map<String, AgentPhone>>(emptyMap())

    /** The phone as each account's server lists it, by account key. */
    val phonesByAccount: StateFlow<Map<String, AgentPhone>> = phones.asStateFlow()

    private val problem = MutableStateFlow<String?>(null)

    /** Why registering for notifications failed, in the server's or the relay's words; null while nothing is wrong. */
    val problemText: StateFlow<String?> = problem.asStateFlow()

    private var alertRegistration: String? = null
    private var ringRegistration: String? = null

    /** The id this phone has on an account's server; null until it registered. Named when answering or declining a call, so that it is not told to stop ringing. */
    fun phoneId(accountKey: String): String? = phones.value[accountKey]?.id

    /** Whether this phone is told about a kind of notification. */
    fun wants(accountKey: String, notificationKind: String): Boolean {
        val phone = phones.value[accountKey] ?: return true
        return phone.notificationKinds.isEmpty() || phone.notificationKinds.contains(notificationKind)
    }

    /**
     * Turns one kind of notification on or off for this phone. At least one stays on: none chosen means every kind.
     */
    suspend fun setNotificationKind(connection: Connection, notificationKind: String, isOn: Boolean) {
        val phone = phones.value[connection.accountKey] ?: return
        if (wants(connection.accountKey, notificationKind) == isOn) return
        val kinds = (phone.notificationKinds.ifEmpty { AgentPhone.allNotificationKinds }).toMutableList()
        if (isOn) kinds.add(notificationKind) else kinds.remove(notificationKind)
        if (kinds.isEmpty()) return
        try {
            val updated = connection.updatePhone(phone.id, kinds)
            phones.update { known -> known + (connection.accountKey to updated) }
        } catch (error: Exception) {
            problem.value = error.message
        }
    }

    /** Registers the account in use, and then the others. Run after connecting and each time the app starts. */
    suspend fun setUp(connection: Connection) = registeringLock.withLock {
        val token = if (PushAvailability.areNotificationsAllowed(context)) PushAvailability.token(context) else null
        if (token == null) {
            // Listed all the same, as a phone that is told nothing. The key goes with it, so a ring that does arrive can be opened.
            registerWithoutPush(connection)
            return@withLock
        }
        registerWith(token, connection)
    }

    /** Firebase gave this install a new token: the registrations are made again, for every account. */
    suspend fun onNewToken(token: String, activeConnection: Connection?) = registeringLock.withLock {
        preferences.edit().putString(TOKEN_KEY, token).apply()
        if (!PushAvailability.areNotificationsAllowed(context)) return@withLock
        val connection = activeConnection ?: accountStore.load()?.let { account -> connectionFor(account.accountKey) } ?: return@withLock
        registerWith(token, connection)
    }

    private suspend fun registerWithoutPush(connection: Connection) {
        try {
            val phone = connection.registerPhone(phoneName(), phoneModel(), payloadKey = keyring.current(connection.accountKey))
            phones.update { known -> known + (connection.accountKey to phone) }
        } catch (_: Exception) {
            // Tried again the next time the app starts; nothing was promised.
        }
    }

    private suspend fun registerWith(token: String, connection: Connection) {
        val relay = PushRelay(transport)
        try {
            val alert = relay.register(token, pushType = "alert")
            alertRegistration = alert
            // A ring that cannot be had now (the relay is not up to date) is tried again at the next start, and says nothing: calls then reach the phone as notifications.
            ringRegistration = try {
                relay.register(token, pushType = "voip")
            } catch (_: Exception) {
                null
            }
            val phone = connection.registerPhone(
                phoneName(), phoneModel(), pushRegistration = alert, voipPushRegistration = ringRegistration,
                payloadKey = keyring.current(connection.accountKey),
            )
            phones.update { known -> known + (connection.accountKey to phone) }
            problem.value = null
            registerOtherAccounts(connection.accountKey)
        } catch (error: Exception) {
            problem.value = error.message
        }
    }

    /**
     * Registers the phone with the servers of the accounts not in use, each with its own key. One that cannot be
     * reached is tried again at the next start, and says nothing: the account in use is fine.
     */
    private suspend fun registerOtherAccounts(accountKeyInUse: String) {
        val alert = alertRegistration ?: return
        for (other in accountStore.accounts().accounts) {
            if (other.accountKey == accountKeyInUse) continue
            val connection = connectionFor(other.accountKey) ?: continue
            try {
                val phone = connection.registerPhone(
                    phoneName(), phoneModel(), pushRegistration = alert, voipPushRegistration = ringRegistration,
                    payloadKey = keyring.current(other.accountKey),
                )
                phones.update { known -> known + (other.accountKey to phone) }
            } catch (_: Exception) {
                // Next start.
            }
        }
    }

    /** Gives the server a registration for ringing on its own, for a phone registered already. */
    suspend fun updateRinging(connection: Connection, registration: String) {
        val known = phones.value[connection.accountKey] ?: return
        try {
            phones.update { map -> map + (connection.accountKey to connection.updatePhoneRinging(known.id, registration)) }
        } catch (_: Exception) {
            // Next start.
        }
    }

    /** What the phone calls itself: the name the person gave it in Android's settings, else its model. */
    private fun phoneName(): String =
        runCatching { Settings.Global.getString(context.contentResolver, "device_name") }.getOrNull()?.takeIf { name -> name.isNotBlank() } ?: phoneModel()

    private fun phoneModel(): String = Build.MODEL ?: "Android"

    private companion object {
        const val TOKEN_KEY = "firebaseToken"
    }
}
