package com.teanode.kit.notifications

import com.teanode.kit.Account
import com.teanode.kit.AccountRing
import com.teanode.kit.HTTPRequest
import com.teanode.kit.HTTPTransport
import com.teanode.kit.SecretStore
import com.teanode.kit.ServerAddress
import com.teanode.kit.TeaNodeError
import com.teanode.kit.TeaNodeJson
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import java.security.SecureRandom
import java.util.Base64
import javax.crypto.Cipher
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.SecretKeySpec

// Notifications reach the phone through the push relay (internal/push on the server). The phone registers its Firebase
// token with the relay and hands the sealed registration to its own server, with a key of its own making; the server
// encrypts each notification's content with that key, and the app decrypts it before anything is shown: a message
// that Firebase carries holds only the content sealed for this phone, a push kind, and a hashed thread identifier.

/**
 * NotificationContent is what a notification says, as the server encrypts it for this phone (`push.Content` in
 * internal/push/payload.go).
 *
 * The evening question of the journal (a notificationKind of "journalEveningQuestion") is sent with the day it asks
 * about, `journalDay`, written YYYY-MM-DD in the person's own calendar. The phone shows it with five buttons, Awful,
 * Low, Okay, Good and Great, each of which sets that day's mood without opening the app (SetJournalMood); pressing the
 * notification itself opens that day in the journal.
 */
@Serializable
data class NotificationContent(
    /**
     * alert, herdr_question, goal_needs_you, speak_first, message, approval, question, answer, circle, chat,
     * skill_share_call, photo, location, shopping, journalEveningQuestion, test, or, in a push that rings, call (a call
     * rings, `callId` names it) and call_ended (it no longer rings this phone).
     */
    val notificationKind: String,
    val conversationId: String? = null,
    val runId: String? = null,
    val callId: String? = null,
    /** For a location notification, the person whose arrival or departure it is, so that tapping it opens Location with them. */
    val locationUserId: String? = null,
    /** For a shopping notification of arriving at a store, the store, so that tapping it opens shopping there. */
    val shoppingStoreName: String? = null,
    /** For a shopping notification that somebody started shopping a list, the list, so that tapping it opens the list. */
    val shoppingListId: String? = null,
    /**
     * For a coding session's question (herdr_question), the computer and the pane of the session that asks, and the
     * question's fingerprint, so that tapping it opens that session on the Coding screen.
     */
    val computer: String? = null,
    val paneId: String? = null,
    val questionFingerprint: String? = null,
    /** For the journal's evening question, the day it asks about. */
    val journalDay: String? = null,
    val title: String = "",
    val body: String = "",
    val buttons: List<NotificationButton>? = null,
    val threadId: String? = null,
)

/** NotificationButton is one action on a notification. */
@Serializable
data class NotificationButton(
    val label: String,
    /** approve, deny, answer:<option number>, reply or open. */
    val buttonAction: String,
)

/**
 * PayloadKey is the key an account's notifications are encrypted with: 256 random bits, made on the phone and given
 * only to that account's server. Each account has its own, so the key that opens a notification also says which
 * account sent it.
 */
object PayloadKey {
    private const val KEY_LENGTH_BYTES = 32
    private const val NONCE_LENGTH_BYTES = 12
    private const val TAG_LENGTH_BITS = 128

    /** A new key, as base64. */
    fun make(): String {
        val bytes = ByteArray(KEY_LENGTH_BYTES)
        SecureRandom().nextBytes(bytes)
        return Base64.getEncoder().encodeToString(bytes)
    }

    /** Decrypts a notification's sealed content: AES-256-GCM, the nonce in front and the tag behind. Throws when the key does not open it. */
    fun open(sealed: String, key: String): NotificationContent {
        val keyBytes = try {
            Base64.getDecoder().decode(key)
        } catch (_: IllegalArgumentException) {
            throw TeaNodeError.UnreadableAnswer
        }
        val combined = try {
            Base64.getDecoder().decode(sealed)
        } catch (_: IllegalArgumentException) {
            throw TeaNodeError.UnreadableAnswer
        }
        if (keyBytes.size != KEY_LENGTH_BYTES || combined.size <= NONCE_LENGTH_BYTES + TAG_LENGTH_BITS / 8) throw TeaNodeError.UnreadableAnswer
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.DECRYPT_MODE, SecretKeySpec(keyBytes, "AES"), GCMParameterSpec(TAG_LENGTH_BITS, combined, 0, NONCE_LENGTH_BYTES))
        val plain = cipher.doFinal(combined, NONCE_LENGTH_BYTES, combined.size - NONCE_LENGTH_BYTES)
        return try {
            TeaNodeJson.decodeFromString(NotificationContent.serializer(), plain.decodeToString())
        } catch (_: Exception) {
            throw TeaNodeError.UnreadableAnswer
        }
    }

    /** The account whose key opened a notification, and what it says. */
    data class Opened(val accountKey: String, val content: NotificationContent)

    /**
     * Decrypts a notification with each account's key in turn, and says whose key opened it: AES-GCM refuses every
     * other key, so that is the account the notification came from.
     */
    fun open(sealed: String, keys: List<AccountPayloadKey>): Opened? {
        for (key in keys) {
            val content = try {
                open(sealed, key.payloadKey)
            } catch (_: Exception) {
                continue
            }
            return Opened(key.accountKey, content)
        }
        return null
    }

    /** Encrypts content the way the server does (push.EncryptContent). For tests and for the demo's pushes. */
    fun seal(content: NotificationContent, key: String): String {
        val nonce = ByteArray(NONCE_LENGTH_BYTES).also { bytes -> SecureRandom().nextBytes(bytes) }
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, SecretKeySpec(Base64.getDecoder().decode(key), "AES"), GCMParameterSpec(TAG_LENGTH_BITS, nonce))
        val encrypted = cipher.doFinal(TeaNodeJson.encodeToString(NotificationContent.serializer(), content).toByteArray())
        return Base64.getEncoder().encodeToString(nonce + encrypted)
    }
}

/** AccountPayloadKey is one account's payload key. */
data class AccountPayloadKey(val accountKey: String, val payloadKey: String)

/** PayloadKeyring keeps a payload key for each account on the phone, in the store the app keeps its sign-ins in. */
class PayloadKeyring(private val secrets: SecretStore) {
    /**
     * The account's key, made with [make] the first time it is asked for. It is kept for as long as the account is on
     * the phone: a new one would leave every notification already on its way unreadable.
     */
    fun current(accountKey: String, make: () -> String = PayloadKey::make): String {
        stored(accountKey)?.let { saved -> return saved }
        val made = make()
        secrets.write(nameOf(accountKey), made.toByteArray())
        return made
    }

    /** The account's key if one was made, for the code that shows a notification, which never makes one. */
    fun stored(accountKey: String): String? = secrets.read(nameOf(accountKey))?.decodeToString()

    /** Forgets an account's key, when the phone signs out of it. */
    fun forget(accountKey: String) = secrets.delete(nameOf(accountKey))

    /**
     * Gives the key made before each account had its own to the account in use, which is the one that registered it
     * with its server last. It is given once and then no longer kept on its own; with no account on the phone it is
     * left where it is.
     */
    fun moveSingleKey(accountKey: String?) {
        if (accountKey == null) return
        val single = secrets.read(SINGLE_NAME) ?: return
        if (secrets.read(nameOf(accountKey)) == null) secrets.write(nameOf(accountKey), single)
        secrets.delete(SINGLE_NAME)
    }

    /** The key of every account kept, in the order they are kept, after the single key has gone to the account in use. */
    fun keys(ring: AccountRing): List<AccountPayloadKey> {
        moveSingleKey(ring.active?.accountKey)
        return ring.accounts.mapNotNull { account: Account ->
            stored(account.accountKey)?.let { payloadKey -> AccountPayloadKey(account.accountKey, payloadKey) }
        }
    }

    companion object {
        /** Where the one key every server was given was kept, before each account had its own. */
        const val SINGLE_NAME = "payloadKey"

        fun nameOf(accountKey: String): String = "$SINGLE_NAME|$accountKey"
    }
}

/** PushRelay is the relay the published app's notifications go through. */
class PushRelay(
    private val transport: HTTPTransport,
    private val relay: ServerAddress = published,
) {
    /**
     * Registers the phone's Firebase token with the relay and returns the sealed registration to hand the phone's own
     * server. [pushType] is "alert" for the notifications and "voip" for the rings of calls: both are made from the
     * same token.
     */
    suspend fun register(firebaseToken: String, pushType: String = "alert"): String {
        val body = JsonObject(
            mapOf(
                "deviceToken" to JsonPrimitive(firebaseToken),
                "isSandbox" to JsonPrimitive(false),
                "pushType" to JsonPrimitive(pushType),
                "platform" to JsonPrimitive(PLATFORM),
            ),
        )
        val response = transport.send(
            HTTPRequest(relay.endpoint("/api/v1/push/registrations").toString(), "POST", mapOf("Content-Type" to "application/json"), body.toString().toByteArray()),
        )
        if (response.statusCode != 200) {
            val reason = try {
                TeaNodeJson.parseToJsonElement(response.text).let { element -> (element as? JsonObject)?.get("errorMessage")?.let { text -> (text as? JsonPrimitive)?.content } }
            } catch (_: Exception) {
                null
            }
            throw TeaNodeError.Refused(reason ?: "The notification relay refused this device.")
        }
        val registered = try {
            ((TeaNodeJson.parseToJsonElement(response.text) as JsonObject)["pushRegistration"] as JsonPrimitive).content
        } catch (_: Exception) {
            throw TeaNodeError.UnreadableAnswer
        }
        return registered
    }

    companion object {
        /** The platform the relay seals a registration for. */
        const val PLATFORM = "android"

        /** The relay for the app as published. Every TeaNode server sends through it unless its operator names another. */
        val published: ServerAddress = ServerAddress.parse("https://mail.teanode.com")
    }
}
