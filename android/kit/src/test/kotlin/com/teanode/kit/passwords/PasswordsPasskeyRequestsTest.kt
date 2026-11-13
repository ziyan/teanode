package com.teanode.kit.passwords

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows
import java.security.MessageDigest
import java.time.Instant

/**
 * The mapping between the Credential Manager's JSON (what a website wrote, and what is answered) and the authenticator: the
 * request is read, the passkey is made or used, and the answer is checked here the way a website would check it.
 */
class PasswordsPasskeyRequestsTest {
    private val certificate = ByteArray(32) { index -> (index + 1).toByte() }
    private val appCaller = PasswordsPasskeyCaller(webOrigin = null, packageName = "org.example.app", certificateSha256 = certificate, clientDataHash = null)
    private val browserHash = ByteArray(32) { index -> (200 - index).toByte() }
    private val browserCaller = PasswordsPasskeyCaller(webOrigin = "https://example.com", packageName = "org.example.browser", certificateSha256 = null, clientDataHash = browserHash)
    private val challenge = PasswordsBase64Url.encode(ByteArray(32) { index -> (index * 3).toByte() })
    private val userHandle = PasswordsBase64Url.encode(byteArrayOf(9, 8, 7, 6))

    private fun creationJson(
        relyingPartyId: String = "example.com",
        algorithms: String = """[{"type":"public-key","alg":-7},{"type":"public-key","alg":-257}]""",
        excluded: String = "[]",
        user: String = """{"id":"$userHandle","name":"alex","displayName":"Alex Example"}""",
    ) = """{"rp":{"id":"$relyingPartyId","name":"Example"},"user":$user,"challenge":"$challenge","pubKeyCredParams":$algorithms,"excludeCredentials":$excluded,"authenticatorSelection":{"residentKey":"required","userVerification":"required"}}"""

    private fun assertionJson(relyingPartyId: String = "example.com", allowed: String = "[]") =
        """{"challenge":"$challenge","rpId":"$relyingPartyId","allowCredentials":$allowed,"userVerification":"required","timeout":1800000}"""

    private fun sha256(bytes: ByteArray) = MessageDigest.getInstance("SHA-256").digest(bytes)

    private fun field(response: String, vararg path: String): String {
        var node = Json.parseToJsonElement(response).jsonObject
        for (name in path.dropLast(1)) node = node.getValue(name).jsonObject
        return node.getValue(path.last()).jsonPrimitive.content
    }

    private fun bytes(response: String, vararg path: String): ByteArray = PasswordsBase64Url.decode(field(response, *path))!!

    private fun failure(block: () -> Unit): PasswordsPasskeyRequestFailure = assertThrows<PasswordsPasskeyRequestException>(block).failure

    // Reading the requests

    @Test fun `a creation request is read for its relying party user challenge and excluded credentials`() {
        val request = PasswordsPasskeyRequests.creation(creationJson(excluded = """[{"type":"public-key","id":"AQID"},{"type":"public-key","id":"BAUG"}]"""))
        assertEquals("example.com", request.relyingPartyId)
        assertEquals("Example", request.relyingPartyName)
        assertEquals(userHandle, request.userHandle)
        assertEquals("alex", request.userName)
        assertEquals("Alex Example", request.userDisplayName)
        assertEquals(challenge, request.challenge)
        assertEquals(listOf("AQID", "BAUG"), request.excludedCredentialIds)
    }

    @Test fun `a creation request must accept ES256 when it lists algorithms and may list none`() {
        PasswordsPasskeyRequests.creation(creationJson(algorithms = "[]"))
        PasswordsPasskeyRequests.creation(creationJson(algorithms = """[{"type":"public-key","alg":-7}]"""))
        assertEquals(PasswordsPasskeyRequestFailure.NoSupportedAlgorithm, failure { PasswordsPasskeyRequests.creation(creationJson(algorithms = """[{"type":"public-key","alg":-257}]""")) })
    }

    @Test fun `a request that is not usable is refused with the reason`() {
        assertEquals(PasswordsPasskeyRequestFailure.NotJson, failure { PasswordsPasskeyRequests.creation("not json") })
        assertEquals(PasswordsPasskeyRequestFailure.NotJson, failure { PasswordsPasskeyRequests.assertion("[]") })
        assertEquals(PasswordsPasskeyRequestFailure.MissingRelyingPartyId, failure { PasswordsPasskeyRequests.creation(creationJson(relyingPartyId = "")) })
        assertEquals(PasswordsPasskeyRequestFailure.MissingUser, failure { PasswordsPasskeyRequests.creation(creationJson(user = "{}")) })
        assertEquals(PasswordsPasskeyRequestFailure.MissingChallenge, failure { PasswordsPasskeyRequests.creation(creationJson().replace(challenge, "")) })
        assertEquals(PasswordsPasskeyRequestFailure.MissingRelyingPartyId, failure { PasswordsPasskeyRequests.assertion("""{"challenge":"$challenge"}""") })
        assertEquals(PasswordsPasskeyRequestFailure.MissingChallenge, failure { PasswordsPasskeyRequests.assertion("""{"rpId":"example.com"}""") })
    }

    @Test fun `an assertion request is read for its relying party challenge and accepted credentials`() {
        val request = PasswordsPasskeyRequests.assertion(assertionJson(allowed = """[{"type":"public-key","id":"AQID"}]"""))
        assertEquals("example.com", request.relyingPartyId)
        assertEquals(challenge, request.challenge)
        assertEquals(listOf("AQID"), request.allowedCredentialIds)
        assertTrue(PasswordsPasskeyRequests.assertion(assertionJson()).allowedCredentialIds.isEmpty())
    }

    // Making a passkey

    @Test fun `the answer to a creation request is what a website checks`() {
        val request = PasswordsPasskeyRequests.creation(creationJson())
        val created = PasswordsPasskeyRequests.createPasskey(request, appCaller, Instant.parse("2026-10-20T10:00:00Z"))
        val response = created.responseJson
        assertEquals("public-key", field(response, "type"))
        assertEquals(field(response, "id"), field(response, "rawId"))
        assertEquals("platform", field(response, "authenticatorAttachment"))
        // The id is the credential id, as the website reads it back.
        val credentialId = PasswordsBase64Url.decode(field(response, "id"))!!
        assertEquals(PasswordsPasskey.CREDENTIAL_ID_BYTE_COUNT, credentialId.size)
        assertEquals(created.passkey.credentialId, field(response, "id"))

        // The client data names the app by its certificate and the package, and the challenge unchanged.
        val clientDataText = String(bytes(response, "response", "clientDataJSON"), Charsets.UTF_8)
        assertEquals(
            """{"type":"webauthn.create","challenge":"$challenge","origin":"android:apk-key-hash:${PasswordsBase64Url.encode(certificate)}","androidPackageName":"org.example.app"}""",
            clientDataText,
        )

        // The authenticator data: the hash of the relying party id, flags 0x5D, counter 0, our authenticator id, the credential id and the key.
        val authenticatorData = bytes(response, "response", "authenticatorData")
        assertArrayEquals(sha256("example.com".toByteArray()), authenticatorData.copyOfRange(0, 32))
        assertEquals(0x5D, authenticatorData[32].toInt() and 0xFF)
        assertArrayEquals(ByteArray(4), authenticatorData.copyOfRange(33, 37))
        assertArrayEquals(PasswordsPasskey.aaguid, authenticatorData.copyOfRange(37, 53))
        assertEquals(credentialId.size, ((authenticatorData[53].toInt() and 0xFF) shl 8) or (authenticatorData[54].toInt() and 0xFF))
        assertArrayEquals(credentialId, authenticatorData.copyOfRange(55, 55 + credentialId.size))

        // The attestation object is `none` and carries that authenticator data.
        val attestation = bytes(response, "response", "attestationObject")
        assertEquals(0xA3, attestation[0].toInt() and 0xFF)
        assertTrue(String(attestation, Charsets.ISO_8859_1).startsWith("£cfmtdnone"))
        assertTrue(String(attestation, Charsets.ISO_8859_1).contains(String(authenticatorData, Charsets.ISO_8859_1)))

        // The public key is a valid P-256 SubjectPublicKeyInfo for the key the passkey signs with.
        val spki = bytes(response, "response", "publicKey")
        val publicKey = java.security.KeyFactory.getInstance("EC").generatePublic(java.security.spec.X509EncodedKeySpec(spki)) as java.security.interfaces.ECPublicKey
        val (x, y) = PasswordsPasskey.publicKeyCoordinates(java.util.Base64.getDecoder().decode(created.passkey.privateKey))
        assertEquals(java.math.BigInteger(1, x), publicKey.w.affineX)
        assertEquals(java.math.BigInteger(1, y), publicKey.w.affineY)
        assertEquals("-7", field(response, "response", "publicKeyAlgorithm"))
        assertEquals(listOf("internal", "hybrid"), (Json.parseToJsonElement(response).jsonObject.getValue("response").jsonObject.getValue("transports") as JsonArray).map { entry -> entry.jsonPrimitive.content })

        // What is kept in the login.
        assertEquals("example.com", created.passkey.relyingPartyId)
        assertEquals(userHandle, created.passkey.userHandle)
        assertEquals("alex", created.passkey.userName)
        assertEquals("Alex Example", created.passkey.userDisplayName)
        assertEquals("2026-10-20T10:00:00Z", created.passkey.createdAt)
    }

    @Test fun `a browser that brings the hash gets an empty client data and that hash`() {
        val request = PasswordsPasskeyRequests.creation(creationJson())
        val created = PasswordsPasskeyRequests.createPasskey(request, browserCaller, Instant.parse("2026-10-20T10:00:00Z"))
        assertEquals("", field(created.responseJson, "response", "clientDataJSON"))
        val clientData = PasswordsPasskeyRequests.clientData("webauthn.create", challenge, browserCaller)
        assertArrayEquals(browserHash, clientData.hash)
        assertEquals(0, clientData.json.size)
    }

    @Test fun `client data for an app is hashed and for a web origin says so`() {
        val appData = PasswordsPasskeyRequests.clientData("webauthn.get", challenge, appCaller)
        assertArrayEquals(sha256(appData.json), appData.hash)
        assertTrue(String(appData.json, Charsets.UTF_8).contains("\"type\":\"webauthn.get\""))
        val webData = PasswordsPasskeyRequests.clientData("webauthn.get", challenge, PasswordsPasskeyCaller("https://example.com", "org.example.browser", null, null))
        assertEquals(
            """{"type":"webauthn.get","challenge":"$challenge","origin":"https://example.com","crossOrigin":false}""",
            String(webData.json, Charsets.UTF_8),
        )
        assertEquals("android:apk-key-hash:" + PasswordsBase64Url.encode(certificate), PasswordsPasskeyRequests.appOrigin(certificate))
        assertEquals(PasswordsPasskeyRequestFailure.UnknownCaller, failure { PasswordsPasskeyRequests.clientData("webauthn.get", challenge, PasswordsPasskeyCaller(null, "x", null, null)) })
    }

    @Test fun `a passkey the person already has for the relying party is excluded`() {
        val created = PasswordsPasskeyRequests.createPasskey(PasswordsPasskeyRequests.creation(creationJson()), appCaller, Instant.now())
        val padded = created.passkey.credentialId + "=="
        val request = PasswordsPasskeyRequests.creation(creationJson(excluded = """[{"type":"public-key","id":"$padded"}]"""))
        assertTrue(PasswordsPasskeyRequests.isExcluded(listOf(created.passkey), request))
        assertFalse(PasswordsPasskeyRequests.isExcluded(listOf(created.passkey.copy(relyingPartyId = "other.example")), request))
        assertFalse(PasswordsPasskeyRequests.isExcluded(emptyList(), request))
    }

    // Signing in

    @Test fun `the answer to an assertion request carries a signature a website verifies`() {
        val created = PasswordsPasskeyRequests.createPasskey(PasswordsPasskeyRequests.creation(creationJson()), appCaller, Instant.now())
        val request = PasswordsPasskeyRequests.assertion(assertionJson(allowed = """[{"type":"public-key","id":"${created.passkey.credentialId}"}]"""))
        val response = PasswordsPasskeyRequests.assertionResponse(created.passkey, request, appCaller)

        assertEquals(created.passkey.credentialId, field(response, "id"))
        val clientDataJson = bytes(response, "response", "clientDataJSON")
        assertTrue(String(clientDataJson, Charsets.UTF_8).startsWith("""{"type":"webauthn.get","challenge":"$challenge","origin":"android:apk-key-hash:"""))
        val authenticatorData = bytes(response, "response", "authenticatorData")
        assertArrayEquals(sha256("example.com".toByteArray()), authenticatorData.copyOfRange(0, 32))
        assertEquals(0x1D, authenticatorData[32].toInt() and 0xFF)
        assertEquals(37, authenticatorData.size)
        assertArrayEquals(byteArrayOf(9, 8, 7, 6), bytes(response, "response", "userHandle"))

        // A website's check: the signature is over the authenticator data and the hash of the client data, under the public key it stored.
        val (x, y) = PasswordsPasskey.publicKeyCoordinates(java.util.Base64.getDecoder().decode(created.passkey.privateKey))
        assertTrue(PasswordsPasskey.isSignatureValid(bytes(response, "response", "signature"), x, y, authenticatorData, sha256(clientDataJson)))
        // Another challenge's hash is not what it signed.
        assertFalse(PasswordsPasskey.isSignatureValid(bytes(response, "response", "signature"), x, y, authenticatorData, sha256(clientDataJson + 1)))
    }

    @Test fun `a browser that brings the hash has that hash signed`() {
        val created = PasswordsPasskeyRequests.createPasskey(PasswordsPasskeyRequests.creation(creationJson()), appCaller, Instant.now())
        val response = PasswordsPasskeyRequests.assertionResponse(created.passkey, PasswordsPasskeyRequests.assertion(assertionJson()), browserCaller)
        assertEquals("", field(response, "response", "clientDataJSON"))
        val (x, y) = PasswordsPasskey.publicKeyCoordinates(java.util.Base64.getDecoder().decode(created.passkey.privateKey))
        assertTrue(PasswordsPasskey.isSignatureValid(bytes(response, "response", "signature"), x, y, bytes(response, "response", "authenticatorData"), browserHash))
    }

    @Test fun `a passkey of another relying party or off the websites list is not used`() {
        val created = PasswordsPasskeyRequests.createPasskey(PasswordsPasskeyRequests.creation(creationJson()), appCaller, Instant.now())
        val other = PasswordsPasskeyRequests.createPasskey(PasswordsPasskeyRequests.creation(creationJson(relyingPartyId = "other.example")), appCaller, Instant.now())
        val request = PasswordsPasskeyRequests.assertion(assertionJson())
        assertTrue(PasswordsPasskeyRequests.isAllowed(created.passkey, request))
        assertFalse(PasswordsPasskeyRequests.isAllowed(other.passkey, request))
        assertEquals(PasswordsPasskeyRequestFailure.PasskeyNotAllowed, failure { PasswordsPasskeyRequests.assertionResponse(other.passkey, request, appCaller) })
        val listed = PasswordsPasskeyRequests.assertion(assertionJson(allowed = """[{"type":"public-key","id":"${other.passkey.credentialId}"}]"""))
        assertFalse(PasswordsPasskeyRequests.isAllowed(created.passkey, listed))
        assertEquals(PasswordsPasskeyRequestFailure.PasskeyNotAllowed, failure { PasswordsPasskeyRequests.assertionResponse(created.passkey, listed, appCaller) })
    }

    @Test fun `a passkey whose key or user handle cannot be read is refused`() {
        val created = PasswordsPasskeyRequests.createPasskey(PasswordsPasskeyRequests.creation(creationJson()), appCaller, Instant.now())
        val request = PasswordsPasskeyRequests.assertion(assertionJson())
        assertEquals(PasswordsPasskeyRequestFailure.UnreadablePasskey, failure { PasswordsPasskeyRequests.assertionResponse(created.passkey.copy(privateKey = "%%%"), request, appCaller) })
        assertEquals(
            PasswordsPasskeyRequestFailure.UnreadablePasskey,
            failure { PasswordsPasskeyRequests.assertionResponse(created.passkey.copy(privateKey = java.util.Base64.getEncoder().encodeToString(ByteArray(10))), request, appCaller) },
        )
    }

    // Where a new passkey is kept

    @Test fun `a new passkey goes into the login for that user and site replacing the same user's older one`() {
        val request = PasswordsPasskeyRequests.creation(creationJson())
        val first = PasswordsPasskeyRequests.createPasskey(request, appCaller, Instant.parse("2026-10-20T10:00:00Z")).passkey
        val fresh = PasswordsPasskeyRequests.loginKeeping(first, request, null)
        assertEquals("Example", fresh.name)
        assertEquals("alex", fresh.userName)
        assertEquals(listOf("https://example.com"), fresh.sites)
        assertEquals(listOf(first), fresh.passkeys)

        val second = PasswordsPasskeyRequests.createPasskey(request, appCaller, Instant.parse("2026-10-21T10:00:00Z")).passkey
        val unrelated = first.copy(relyingPartyId = "other.example", credentialId = "AQID")
        val existing = fresh.copy(password = "kept", passkeys = listOf(first, unrelated))
        val updated = PasswordsPasskeyRequests.loginKeeping(second, request, existing)
        assertEquals("kept", updated.password)
        assertEquals(listOf(unrelated, second), updated.passkeys)
    }

    @Test fun `the login for a new passkey is the same user on a site that fits the relying party`() {
        val request = PasswordsPasskeyRequests.creation(creationJson())
        assertTrue(PasswordsPasskeyRequests.isLoginFor(PasswordsLoginFields(userName = "Alex", sites = listOf("https://example.com")), request))
        assertFalse(PasswordsPasskeyRequests.isLoginFor(PasswordsLoginFields(userName = "sam", sites = listOf("https://example.com")), request))
        assertFalse(PasswordsPasskeyRequests.isLoginFor(PasswordsLoginFields(userName = "alex", sites = listOf("https://example.org")), request))
        assertFalse(PasswordsPasskeyRequests.isLoginFor(PasswordsLoginFields(userName = "alex", sites = listOf("https://example.com.attacker.test")), request))
    }

    @Test fun `a response for a JSON element is an object with the fields a credential has`() {
        val created = PasswordsPasskeyRequests.createPasskey(PasswordsPasskeyRequests.creation(creationJson()), appCaller, Instant.now())
        val element = Json.parseToJsonElement(created.responseJson) as JsonObject
        assertEquals(setOf("id", "rawId", "type", "authenticatorAttachment", "response", "clientExtensionResults"), element.keys)
        assertEquals(JsonObject(emptyMap()), element.getValue("clientExtensionResults"))
        assertEquals(JsonPrimitive("public-key"), element.getValue("type"))
    }
}
