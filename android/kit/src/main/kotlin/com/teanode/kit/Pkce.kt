package com.teanode.kit

import java.security.MessageDigest
import java.security.SecureRandom
import java.util.Base64

/** Pkce makes the secret pair a sign-in on the server's page is protected with (RFC 7636, method S256). */
object Pkce {
    private val random = SecureRandom()

    /** Thirty-two random bytes, as the URL-safe text a code verifier and a state take. */
    fun randomText(): String {
        val bytes = ByteArray(32)
        random.nextBytes(bytes)
        return base64Url(bytes)
    }

    /** The code challenge for a verifier: the hash the server's page is given, so only the app can redeem the code. */
    fun challenge(verifier: String): String = base64Url(MessageDigest.getInstance("SHA-256").digest(verifier.toByteArray(Charsets.UTF_8)))

    private fun base64Url(bytes: ByteArray): String = Base64.getUrlEncoder().withoutPadding().encodeToString(bytes)
}
