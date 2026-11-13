package com.teanode.kit

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.Flow
import kotlinx.serialization.DeserializationStrategy
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import java.util.Locale
import java.util.TimeZone
import java.util.UUID

/**
 * Connection is the app talking to its server as the person: GraphQL over HTTPS with the account's token, renewed
 * before it runs out and kept wherever the store keeps it.
 */
class Connection(
    account: Account,
    private val transport: HTTPTransport,
    private val store: AccountStore? = null,
    private val subscriptions: SubscriptionTransport? = null,
    private val clock: () -> Long = System::currentTimeMillis,
) {
    /** The account identity remains the same when its tokens are renewed. */
    val accountKey: String = account.accountKey

    private val lock = Any()
    private val renewalScope = CoroutineScope(SupervisorJob() + Dispatchers.Default)

    @Volatile
    var account: Account = account
        private set

    /**
     * The renewal under way. Whoever needs a token meanwhile waits for it rather than renewing again: the server
     * retires a refresh token the first time it is used, so a second renewal with it is refused.
     */
    private var renewal: Deferred<Account>? = null

    /**
     * Whether the server has ended this sign-in: it refused the refresh token, or refused a request again with a
     * token loaded afresh. The app signs out of an account only when its own connection says so.
     */
    @Volatile
    var isSignedOut: Boolean = false
        private set

    /** The token to send now, renewed first when it is close to running out. */
    suspend fun accessToken(): String {
        synchronized(lock) { renewal }?.let { running -> return outcome(running).accessToken }
        // The app and its services share the stored sign-in. One that renewed it has stored a newer one, whose refresh
        // token is the only one still good: it is taken up before renewing again.
        val now = clock()
        if (account.expiresAtEpochMS - now < RENEW_BEFORE_MS) {
            val stored = runCatching { store?.load(account.accountKey) }.getOrNull()
            if (stored != null && stored.expiresAtEpochMS > account.expiresAtEpochMS) {
                requireSameRecordingOwner(account, stored)
                account = stored
            }
        }
        if (account.expiresAtEpochMS - now < RENEW_BEFORE_MS && account.refreshToken.isNotEmpty()) {
            return renew(now).accessToken
        }
        return account.accessToken
    }

    /** Renews the sign-in once, however many callers ask while it is under way, and stores it. */
    private suspend fun renew(now: Long): Account {
        val (running, isOwner) = synchronized(lock) {
            val existing = renewal
            if (existing != null) {
                existing to false
            } else {
                val held = account
                val created = renewalScope.async { renewed(held, transport, store, now) }
                renewal = created
                created to true
            }
        }
        try {
            return outcome(running)
        } finally {
            if (isOwner) synchronized(lock) { if (renewal === running) renewal = null }
        }
    }

    /**
     * Waits for a renewal and keeps what it gave. Every caller waiting keeps it, so a refusal has ended the sign-in by
     * the time any of them hears of it.
     */
    private suspend fun outcome(renewing: Deferred<Account>): Account {
        try {
            val renewed = renewing.await()
            requireSameRecordingOwner(account, renewed)
            // A caller that wakes late does not put back an older sign-in.
            synchronized(lock) { if (renewed.expiresAtEpochMS >= account.expiresAtEpochMS) account = renewed }
            return renewed
        } catch (error: TeaNodeError) {
            if (error == TeaNodeError.SignedOut) isSignedOut = true
            throw error
        }
    }

    /**
     * Sends a request with the account's token. A 401 is tried once more with a token loaded afresh, since another
     * process may have renewed the sign-in and so retired the token sent; a second 401 means the sign-in has ended.
     */
    suspend fun send(request: HTTPRequest): HTTPResponse {
        val token = accessToken()
        // The server learns the person's time zone from what their devices send, since a journal day is a calendar
        // day in it.
        val prepared = request.with("X-Timezone", shownTimeZone())
        val response = transport.send(prepared.with("Authorization", "Bearer $token"))
        if (response.statusCode != 401) return response
        val freshToken = accessToken(replacing = token)
        val retried = transport.send(prepared.with("Authorization", "Bearer $freshToken"))
        if (retried.statusCode == 401) {
            isSignedOut = true
            throw TeaNodeError.SignedOut
        }
        return retried
    }

    /** A token to use in place of one the server refused: a newer one this connection or another process already has, or else a renewal. */
    private suspend fun accessToken(replacing: String): String {
        synchronized(lock) { renewal }?.let { running -> return outcome(running).accessToken }
        if (account.accessToken != replacing) return account.accessToken
        val stored = runCatching { store?.load(account.accountKey) }.getOrNull()
        if (stored != null && stored.accessToken != replacing && stored.expiresAtEpochMS >= account.expiresAtEpochMS) {
            requireSameRecordingOwner(account, stored)
            account = stored
            return stored.accessToken
        }
        if (account.refreshToken.isEmpty()) {
            isSignedOut = true
            throw TeaNodeError.SignedOut
        }
        return renew(clock()).accessToken
    }

    /** Runs a GraphQL document and decodes its data. */
    suspend fun <Answer> execute(
        document: String,
        deserializer: DeserializationStrategy<Answer>,
        variables: Map<String, JsonElement> = emptyMap(),
        requestTimeoutMS: Long? = null,
    ): Answer {
        val body = JsonObject(mapOf("query" to JsonPrimitive(document), "variables" to JsonObject(variables)))
        val response = send(
            HTTPRequest(
                account.server.endpoint("/api/v1/graphql").toString(), "POST",
                mapOf("Content-Type" to "application/json", LANGUAGE_HEADER to shownLanguage()),
                body.toString().toByteArray(), requestTimeoutMS,
            ),
        )
        // The errors are read on their own first: a refused field comes back as null beside them, which the answer's
        // own shape does not allow.
        val parsed = try {
            TeaNodeJson.parseToJsonElement(response.text)
        } catch (_: Exception) {
            throw TeaNodeError.UnreadableAnswer
        }
        val firstError = (parsed["errors"] as? kotlinx.serialization.json.JsonArray)?.firstOrNull()
        if (firstError != null) throw TeaNodeError.Refused(firstError["message"]?.stringValue ?: "unknown error")
        val data = parsed["data"]
        if (data == null || data is JsonNull) throw TeaNodeError.UnreadableAnswer
        return try {
            TeaNodeJson.decodeFromJsonElement(deserializer, data)
        } catch (_: Exception) {
            throw TeaNodeError.UnreadableAnswer
        }
    }

    suspend inline fun <reified Answer> execute(
        document: String,
        variables: Map<String, JsonElement> = emptyMap(),
        requestTimeoutMS: Long? = null,
    ): Answer = execute(document, kotlinx.serialization.serializer<Answer>(), variables, requestTimeoutMS)

    /** Opens a GraphQL subscription and streams each event's data until the server completes it or the connection drops. */
    fun subscribe(document: String, variables: Map<String, JsonElement> = emptyMap()): Flow<JsonElement> {
        val transportForSubscriptions = subscriptions ?: throw IllegalStateException("this connection has no websocket transport")
        return transportForSubscriptions.subscribe(this, document, variables)
    }

    /**
     * Uploads a file to hand the agent with a message, and returns its id. A voice message is a recording the server
     * prepares and measures; it refuses one shorter than half a second or longer than two minutes with a sentence,
     * which is the error. A tax document (a paystub or a W-2) is kept apart: its text holds the person's taxpayer
     * number, so the server does not keep it.
     */
    suspend fun upload(
        name: String,
        contentType: String,
        data: ByteArray,
        isTaxDocument: Boolean = false,
        isVoiceMessage: Boolean = false,
    ): String {
        val boundary = "teanode-${UUID.randomUUID()}"
        val response = send(
            HTTPRequest(
                account.server.endpoint(uploadPath(isTaxDocument, isVoiceMessage)).toString(), "POST",
                mapOf("Content-Type" to "multipart/form-data; boundary=$boundary"),
                Upload.body(boundary, name, contentType, data),
            ),
        )
        if (response.statusCode != 200) {
            if (response.statusCode == 413) throw TeaNodeError.Refused("That file is larger than the server takes.")
            throw TeaNodeError.Refused(response.text.take(300))
        }
        val uploaded = try {
            TeaNodeJson.decodeFromString(UploadedAttachments.serializer(), response.text)
        } catch (_: Exception) {
            throw TeaNodeError.UnreadableAnswer
        }
        return uploaded.attachments.firstOrNull()?.id ?: throw TeaNodeError.UnreadableAnswer
    }

    @Serializable
    private class UploadedAttachments(val attachments: List<UploadedFile>)

    @Serializable
    private class UploadedFile(val id: String)

    companion object {
        /** How long before a token runs out it is renewed. */
        internal const val RENEW_BEFORE_MS: Long = 24L * 60 * 60 * 1000

        /**
         * The header that says which language the app is shown in, as the dashboard's does: the server writes the
         * person's notifications in it, and what the agent says when they have set no language of their own.
         */
        const val LANGUAGE_HEADER = "X-Language"

        /** The phone's time zone, named as the server names zones: an IANA identifier such as Europe/Paris. */
        fun shownTimeZone(timeZone: TimeZone = TimeZone.getDefault()): String = timeZone.id

        /**
         * The language the app is shown in, named as the dashboard names its own: en, zh or ja. A phone in another
         * language shows English.
         */
        fun shownLanguage(preferredLanguageTags: List<String> = listOf(Locale.getDefault().toLanguageTag())): String {
            val language = preferredLanguageTags.firstOrNull()?.lowercase()?.split('-', '_')?.firstOrNull().orEmpty()
            return if (language == "zh" || language == "ja") language else "en"
        }

        /** The address uploads go to: a tax document and a voice message are told apart by a parameter. */
        fun uploadPath(isTaxDocument: Boolean, isVoiceMessage: Boolean): String = when {
            isVoiceMessage -> "/api/v1/agent/attachments?isVoiceMessage=true"
            isTaxDocument -> "/api/v1/agent/attachments?isTaxDocument=true"
            else -> "/api/v1/agent/attachments"
        }

        /**
         * The sign-in renewed and stored. A refused refresh token is checked against the store before the sign-in
         * counts as ended: another process may have renewed it a moment before, which retired the token this
         * connection holds, and stored the one to go on with.
         */
        internal suspend fun renewed(account: Account, transport: HTTPTransport, store: AccountStore?, nowEpochMS: Long): Account {
            try {
                val renewed = SignIn(account.server, transport).refresh(account, nowEpochMS).copy(cachedRecordingUserId = account.cachedRecordingUserId)
                if (store != null && account.cachedRecordingUserId != null) {
                    val retained = store.load(account.accountKey) ?: throw TeaNodeError.SignedOut
                    requireSameRecordingOwner(account, retained)
                    if (retained.refreshToken != account.refreshToken) {
                        if (retained.expiresAtEpochMS < renewed.expiresAtEpochMS) throw TeaNodeError.SignedOut
                        return retained
                    }
                    store.save(renewed)
                } else {
                    runCatching { store?.save(renewed) }
                }
                return renewed
            } catch (error: TeaNodeError.SignedOut) {
                val stored = runCatching { store?.load(account.accountKey) }.getOrNull()
                if (stored != null && stored.refreshToken.isNotEmpty() && stored.refreshToken != account.refreshToken) {
                    requireSameRecordingOwner(account, stored)
                    return stored
                }
                throw TeaNodeError.SignedOut
            }
        }

        private fun requireSameRecordingOwner(current: Account, replacement: Account) {
            val ownerId = current.cachedRecordingUserId ?: return
            if (replacement.cachedRecordingUserId != ownerId) throw TeaNodeError.SignedOut
        }
    }
}

/** GraphQLError is one error in a GraphQL answer. */
@Serializable
data class GraphQLError(val message: String)

/** Upload writes a file as the multipart body the attachments endpoint takes. */
object Upload {
    fun body(boundary: String, name: String, contentType: String, data: ByteArray): ByteArray {
        val safeName = name.replace("\"", "'").replace("\r", "").replace("\n", "")
        val head = "--$boundary\r\nContent-Disposition: form-data; name=\"file\"; filename=\"$safeName\"\r\nContent-Type: $contentType\r\n\r\n"
        val tail = "\r\n--$boundary--\r\n"
        return head.toByteArray() + data + tail.toByteArray()
    }
}
