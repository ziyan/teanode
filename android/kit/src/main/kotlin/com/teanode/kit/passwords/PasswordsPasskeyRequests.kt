package com.teanode.kit.passwords

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.intOrNull
import java.security.MessageDigest
import java.time.Instant

// What the Android Credential Manager asks a passkey provider, as plain values and functions, and what the provider hands
// back. The system sends a request as the JSON a website wrote (the WebAuthn options) and says who is asking; the answer is
// the JSON of a WebAuthn credential. Only the app's provider service imports androidx.credentials; this file turns those
// JSON texts into calls of the authenticator (PasswordsPasskey) and back, so it can be tested without a phone.

/** Why a request could not be answered with a passkey. */
enum class PasswordsPasskeyRequestFailure {
    /** The request is not JSON, or not an object. */
    NotJson,

    /** The request names no relying party id. */
    MissingRelyingPartyId,

    /** The request has no challenge. */
    MissingChallenge,

    /** A creation request without a user (an id and a name). */
    MissingUser,

    /** The website accepts no algorithm but the ones this authenticator cannot make (it makes ES256 only). */
    NoSupportedAlgorithm,

    /** The website lists a credential the person already has for this relying party: the person has this passkey. */
    CredentialAlreadyHere,

    /** The passkey is not for the relying party that asked, or the website's list of accepted credentials leaves it out. */
    PasskeyNotAllowed,

    /** The passkey's private key or user handle cannot be read. */
    UnreadablePasskey,

    /** The caller is neither a browser that named a page and a client data hash nor an app with a signing certificate. */
    UnknownCaller,
}

class PasswordsPasskeyRequestException(val failure: PasswordsPasskeyRequestFailure) : Exception(failure.name)

/**
 * Who is asking, as the Credential Manager says. A browser (a privileged caller) names the page's origin and brings the hash
 * of the client data it built itself; an app brings neither, so the provider builds the client data and names the app by the
 * hash of its signing certificate.
 */
class PasswordsPasskeyCaller(
    /** The origin of the page a privileged caller is on (`https://example.com`), or null for an app. */
    val webOrigin: String?,
    /** The Android package of the app asking (a browser's too). */
    val packageName: String,
    /** SHA-256 of the app's signing certificate, when it is an app asking. */
    val certificateSha256: ByteArray?,
    /** The hash a privileged caller built the client data with; null for an app. */
    val clientDataHash: ByteArray?,
)

/** A website's request to make a passkey, read from the WebAuthn creation options. */
class PasswordsPasskeyCreationRequest(
    val relyingPartyId: String,
    val relyingPartyName: String,
    /** The user's id as the website wrote it (base64url, no padding). */
    val userHandle: String,
    val userName: String,
    val userDisplayName: String,
    /** The challenge as the website wrote it (base64url, no padding). */
    val challenge: String,
    /** The credential ids (base64url) the website says the person has already. */
    val excludedCredentialIds: List<String>,
)

/** A website's request to sign in with a passkey, read from the WebAuthn request options. */
class PasswordsPasskeyAssertionRequest(
    val relyingPartyId: String,
    val challenge: String,
    /** The credential ids (base64url) the website accepts; empty when it accepts any. */
    val allowedCredentialIds: List<String>,
)

/** The client data the response carries: the JSON text (empty when a browser built it) and its SHA-256. */
class PasswordsPasskeyClientData(val json: ByteArray, val hash: ByteArray)

/** A passkey just made: what to keep in the person's login, and the credential JSON the website is sent. */
class PasswordsCreatedPasskey(val passkey: PasswordsPasskeyFields, val responseJson: String)

object PasswordsPasskeyRequests {
    private val json = Json

    private fun fail(failure: PasswordsPasskeyRequestFailure): Nothing = throw PasswordsPasskeyRequestException(failure)

    private fun parseObject(text: String): JsonObject {
        val element = try {
            json.parseToJsonElement(text)
        } catch (_: Exception) {
            fail(PasswordsPasskeyRequestFailure.NotJson)
        }
        return element as? JsonObject ?: fail(PasswordsPasskeyRequestFailure.NotJson)
    }

    private fun JsonElement?.text(): String? = (this as? JsonPrimitive)?.takeIf { primitive -> primitive.isString }?.contentOrNull

    private fun JsonElement?.entries(): List<JsonObject> = (this as? JsonArray)?.mapNotNull { entry -> entry as? JsonObject } ?: emptyList()

    // Reading a request

    /**
     * Reads the creation options of a website. The website must accept ES256 (COSE -7): with no list at all, WebAuthn's own
     * default of ES256 and RS256 applies. A credential the website lists as excluded that this person already has is checked
     * by [isExcluded].
     */
    fun creation(requestJson: String): PasswordsPasskeyCreationRequest {
        val options = parseObject(requestJson)
        val relyingParty = options["rp"] as? JsonObject
        val relyingPartyId = relyingParty?.get("id").text()?.takeIf { text -> text.isNotEmpty() } ?: fail(PasswordsPasskeyRequestFailure.MissingRelyingPartyId)
        val challenge = options["challenge"].text()?.takeIf { text -> text.isNotEmpty() } ?: fail(PasswordsPasskeyRequestFailure.MissingChallenge)
        val user = options["user"] as? JsonObject ?: fail(PasswordsPasskeyRequestFailure.MissingUser)
        val userHandle = user["id"].text()?.takeIf { text -> text.isNotEmpty() } ?: fail(PasswordsPasskeyRequestFailure.MissingUser)
        val userName = user["name"].text() ?: fail(PasswordsPasskeyRequestFailure.MissingUser)
        val parameters = options["pubKeyCredParams"]
        if (parameters is JsonArray && parameters.isNotEmpty()) {
            val isEs256Accepted = parameters.entries().any { entry -> (entry["alg"] as? JsonPrimitive)?.intOrNull == PasswordsPasskey.COSE_ALGORITHM }
            if (!isEs256Accepted) fail(PasswordsPasskeyRequestFailure.NoSupportedAlgorithm)
        }
        return PasswordsPasskeyCreationRequest(
            relyingPartyId = relyingPartyId,
            relyingPartyName = relyingParty?.get("name").text().orEmpty(),
            userHandle = userHandle,
            userName = userName,
            userDisplayName = user["displayName"].text().orEmpty(),
            challenge = challenge,
            excludedCredentialIds = options["excludeCredentials"].entries().mapNotNull { entry -> entry["id"].text() },
        )
    }

    /** Reads the request options of a website. The relying party id may be left out by a website, in which case the origin's host would stand for it; the provider needs it named. */
    fun assertion(requestJson: String): PasswordsPasskeyAssertionRequest {
        val options = parseObject(requestJson)
        val relyingPartyId = options["rpId"].text()?.takeIf { text -> text.isNotEmpty() } ?: fail(PasswordsPasskeyRequestFailure.MissingRelyingPartyId)
        val challenge = options["challenge"].text()?.takeIf { text -> text.isNotEmpty() } ?: fail(PasswordsPasskeyRequestFailure.MissingChallenge)
        return PasswordsPasskeyAssertionRequest(relyingPartyId, challenge, options["allowCredentials"].entries().mapNotNull { entry -> entry["id"].text() })
    }

    // Which passkeys fit

    /** Whether a passkey the person has is one the website asked for: for its relying party, and on its list of accepted credentials if it sent one. */
    fun isAllowed(passkey: PasswordsPasskeyFields, request: PasswordsPasskeyAssertionRequest): Boolean =
        passkey.relyingPartyId == request.relyingPartyId &&
            (request.allowedCredentialIds.isEmpty() || request.allowedCredentialIds.any { allowed -> sameCredential(allowed, passkey.credentialId) })

    /** Whether the person already has a passkey the creation request lists as excluded for this relying party. */
    fun isExcluded(passkeys: List<PasswordsPasskeyFields>, request: PasswordsPasskeyCreationRequest): Boolean =
        passkeys.any { passkey -> passkey.relyingPartyId == request.relyingPartyId && request.excludedCredentialIds.any { excluded -> sameCredential(excluded, passkey.credentialId) } }

    /** Credential ids compare as bytes: a website may write the same id with or without padding. */
    private fun sameCredential(first: String, second: String): Boolean {
        val firstBytes = PasswordsBase64Url.decode(first) ?: return first == second
        val secondBytes = PasswordsBase64Url.decode(second) ?: return false
        return firstBytes.contentEquals(secondBytes)
    }

    // The client data

    /** The origin an app is named by: `android:apk-key-hash:` and the base64url of its certificate's SHA-256. */
    fun appOrigin(certificateSha256: ByteArray): String = "android:apk-key-hash:" + PasswordsBase64Url.encode(certificateSha256)

    /**
     * The client data of a response. A browser brings the hash it signed over, and builds the client data itself: the provider
     * answers with an empty text and that hash. For an app the provider writes the client data (type, challenge, the app as
     * origin and its package) and hashes it.
     */
    fun clientData(type: String, challenge: String, caller: PasswordsPasskeyCaller): PasswordsPasskeyClientData {
        caller.clientDataHash?.let { hash -> return PasswordsPasskeyClientData(ByteArray(0), hash) }
        val origin = caller.webOrigin ?: caller.certificateSha256?.let { certificate -> appOrigin(certificate) } ?: fail(PasswordsPasskeyRequestFailure.UnknownCaller)
        val entries = linkedMapOf<String, JsonElement>(
            "type" to JsonPrimitive(type),
            "challenge" to JsonPrimitive(challenge),
            "origin" to JsonPrimitive(origin),
        )
        if (caller.webOrigin != null) entries["crossOrigin"] = JsonPrimitive(false) else entries["androidPackageName"] = JsonPrimitive(caller.packageName)
        val text = JsonObject(entries).toString().toByteArray(Charsets.UTF_8)
        return PasswordsPasskeyClientData(text, MessageDigest.getInstance("SHA-256").digest(text))
    }

    // Making a passkey

    /**
     * Makes the passkey a creation request asks for. `isVerified` says the person was just verified (the fingerprint opened
     * the account key). `privateKey` and `credentialId` are for tests only.
     */
    fun createPasskey(
        request: PasswordsPasskeyCreationRequest,
        caller: PasswordsPasskeyCaller,
        createdAt: Instant,
        isVerified: Boolean = true,
        privateKey: ByteArray? = null,
        credentialId: ByteArray? = null,
    ): PasswordsCreatedPasskey {
        val registration = PasswordsPasskey.register(request.relyingPartyId, isVerified, privateKey, credentialId)
        val clientData = clientData("webauthn.create", request.challenge, caller)
        val identifier = PasswordsBase64Url.encode(registration.credentialId)
        val passkey = PasswordsPasskeyFields(
            credentialId = identifier,
            relyingPartyId = request.relyingPartyId,
            userHandle = request.userHandle,
            userName = request.userName,
            userDisplayName = request.userDisplayName.ifEmpty { request.userName },
            privateKey = java.util.Base64.getEncoder().encodeToString(registration.privateKey),
            createdAt = createdAt.toString(),
        )
        val response = JsonObject(
            mapOf(
                "clientDataJSON" to JsonPrimitive(PasswordsBase64Url.encode(clientData.json)),
                "attestationObject" to JsonPrimitive(PasswordsBase64Url.encode(registration.attestationObject)),
                "authenticatorData" to JsonPrimitive(PasswordsBase64Url.encode(registration.authenticatorData)),
                "transports" to JsonArray(listOf(JsonPrimitive("internal"), JsonPrimitive("hybrid"))),
                "publicKeyAlgorithm" to JsonPrimitive(PasswordsPasskey.COSE_ALGORITHM),
                "publicKey" to JsonPrimitive(PasswordsBase64Url.encode(subjectPublicKeyInfo(registration.publicKeyX, registration.publicKeyY))),
            ),
        )
        return PasswordsCreatedPasskey(passkey, credentialJson(identifier, response))
    }

    /** The public key as the DER SubjectPublicKeyInfo WebAuthn's `getPublicKey` gives: the P-256 prefix and the uncompressed point. */
    fun subjectPublicKeyInfo(publicKeyX: ByteArray, publicKeyY: ByteArray): ByteArray = SUBJECT_PUBLIC_KEY_INFO_PREFIX + byteArrayOf(4) + publicKeyX + publicKeyY

    private val SUBJECT_PUBLIC_KEY_INFO_PREFIX = byteArrayOf(
        0x30, 0x59, 0x30, 0x13, 0x06, 0x07, 0x2a, 0x86.toByte(), 0x48, 0xce.toByte(), 0x3d, 0x02, 0x01, 0x06, 0x08, 0x2a, 0x86.toByte(), 0x48, 0xce.toByte(), 0x3d, 0x03, 0x01, 0x07, 0x03, 0x42, 0x00,
    )

    /**
     * The login a new passkey is kept in: the login for this relying party and user name when there is one (an older passkey
     * of the same user there is replaced), else a new login named for the relying party. `existing` is the person's own login
     * (never one shared with them) that [isLoginFor] chose, or null.
     */
    fun loginKeeping(passkey: PasswordsPasskeyFields, request: PasswordsPasskeyCreationRequest, existing: PasswordsLoginFields?): PasswordsLoginFields {
        if (existing == null) {
            return PasswordsLoginFields(
                name = request.relyingPartyName.ifEmpty { request.relyingPartyId },
                userName = request.userName,
                sites = listOf("https://" + request.relyingPartyId),
                passkeys = listOf(passkey),
            )
        }
        val kept = existing.passkeys.filter { held -> !(held.relyingPartyId == passkey.relyingPartyId && held.userHandle == passkey.userHandle) }
        return existing.copy(passkeys = kept + passkey)
    }

    /** Whether a login of the person's is the one a new passkey belongs in: the same user name (any letter case) on a site that fits the relying party. */
    fun isLoginFor(login: PasswordsLoginFields, request: PasswordsPasskeyCreationRequest): Boolean =
        login.userName.equals(request.userName, ignoreCase = true) && PasswordsSiteMatch.isMatch(login.sites, request.relyingPartyId)

    // Signing in

    /**
     * The credential JSON of a sign-in with a passkey: the authenticator data, the signature over it and the client data's
     * hash, the user handle, and the credential id. The passkey must be for the relying party that asked and on the website's
     * list of accepted credentials.
     */
    fun assertionResponse(
        passkey: PasswordsPasskeyFields,
        request: PasswordsPasskeyAssertionRequest,
        caller: PasswordsPasskeyCaller,
        isVerified: Boolean = true,
    ): String {
        if (!isAllowed(passkey, request)) fail(PasswordsPasskeyRequestFailure.PasskeyNotAllowed)
        val privateKey = runCatching { java.util.Base64.getDecoder().decode(passkey.privateKey) }.getOrNull() ?: fail(PasswordsPasskeyRequestFailure.UnreadablePasskey)
        val userHandle = PasswordsBase64Url.decode(passkey.userHandle) ?: fail(PasswordsPasskeyRequestFailure.UnreadablePasskey)
        val clientData = clientData("webauthn.get", request.challenge, caller)
        val authenticatorData = PasswordsPasskey.assertionAuthenticatorData(request.relyingPartyId, isVerified)
        val signature = try {
            PasswordsPasskey.sign(privateKey, authenticatorData, clientData.hash)
        } catch (_: PasswordsPasskeyException) {
            fail(PasswordsPasskeyRequestFailure.UnreadablePasskey)
        }
        val response = JsonObject(
            mapOf(
                "clientDataJSON" to JsonPrimitive(PasswordsBase64Url.encode(clientData.json)),
                "authenticatorData" to JsonPrimitive(PasswordsBase64Url.encode(authenticatorData)),
                "signature" to JsonPrimitive(PasswordsBase64Url.encode(signature)),
                "userHandle" to JsonPrimitive(PasswordsBase64Url.encode(userHandle)),
            ),
        )
        val credentialId = PasswordsBase64Url.decode(passkey.credentialId)?.let { bytes -> PasswordsBase64Url.encode(bytes) } ?: fail(PasswordsPasskeyRequestFailure.UnreadablePasskey)
        return credentialJson(credentialId, response)
    }

    private fun credentialJson(credentialId: String, response: JsonObject): String = JsonObject(
        mapOf(
            "id" to JsonPrimitive(credentialId),
            "rawId" to JsonPrimitive(credentialId),
            "type" to JsonPrimitive("public-key"),
            "authenticatorAttachment" to JsonPrimitive("platform"),
            "response" to response,
            "clientExtensionResults" to JsonObject(emptyMap()),
        ),
    ).toString()
}
