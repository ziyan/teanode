package com.teanode.kit.passwords

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.boolean
import kotlinx.serialization.json.long
import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows
import java.io.File

/**
 * Reads the test vectors the Go package writes (internal/passwords/testdata/vectors.json) and checks that this
 * implementation reproduces every derived value byte for byte, opens every sealed value, and agrees about signatures.
 * The web and iPhone tests read the same file.
 */
class PasswordsVectorsTest {
    private val vectors: JsonObject by lazy {
        // Gradle runs the tests in android/kit; the file is in the repository root.
        val file = File("../../internal/passwords/testdata/vectors.json")
        assertTrue(file.exists(), "vectors.json is not at ${file.absolutePath}")
        Json.parseToJsonElement(file.readText()) as JsonObject
    }

    private fun JsonObject.obj(key: String): JsonObject = this[key] as JsonObject

    private fun JsonObject.text(key: String): String = (this[key] as JsonPrimitive).content

    private fun JsonObject.list(key: String): List<JsonObject> = (this[key] as JsonArray).map { element -> element as JsonObject }

    /** Every byte value in the file is lower-case hex with a space between bytes. */
    private fun JsonObject.bytes(key: String): ByteArray {
        val pairs = text(key).split(" ").filter { pair -> pair.isNotEmpty() }
        for (pair in pairs) assertEquals(2, pair.length, "$key has a byte that is not two digits")
        return pairs.map { pair -> pair.toInt(16).toByte() }.toByteArray()
    }

    private fun JsonObject.sealed(key: String): PasswordsSealedValue = PasswordsSealedValue.fromJsonText(obj(key).toString())

    private fun JsonObject.access(key: String): PasswordsItemAccess = PasswordsItemAccess.fromJson(obj(key))

    private fun kind(text: String): PasswordsItemKind = PasswordsItemKind.fromRawValue(text)!!

    @Test fun `passphrase derivations`() {
        val section = vectors.obj("passphrase")
        val passphraseKey = PasswordsCrypto.passphraseKey(section.text("passphrase"), section.bytes("salt"), (section["iterationCount"] as JsonPrimitive).long.toInt())
        assertArrayEquals(section.bytes("passphraseKey"), passphraseKey)
        assertArrayEquals(section.bytes("wrapKey"), PasswordsCrypto.wrapKey(passphraseKey))
        val proof = PasswordsCrypto.proof(passphraseKey)
        assertArrayEquals(section.bytes("proof"), proof)
        assertArrayEquals(section.bytes("proofHash"), PasswordsCrypto.proofHash(proof))
    }

    @Test fun `recovery key`() {
        val section = vectors.obj("recoveryKey")
        val recoveryKey = section.bytes("bytes")
        assertEquals(section.text("text"), PasswordsCrypto.formatRecoveryKey(recoveryKey))
        assertArrayEquals(recoveryKey, PasswordsCrypto.parseRecoveryKey(section.text("text")))
        for (accepted in section["acceptedTexts"] as JsonArray) {
            assertArrayEquals(recoveryKey, PasswordsCrypto.parseRecoveryKey((accepted as JsonPrimitive).content), accepted.toString())
        }
        assertArrayEquals(section.bytes("wrapKey"), PasswordsCrypto.wrapKey(recoveryKey))
        val proof = PasswordsCrypto.proof(recoveryKey)
        assertArrayEquals(section.bytes("proof"), proof)
        assertArrayEquals(section.bytes("proofHash"), PasswordsCrypto.proofHash(proof))
    }

    @Test fun `every invalid recovery key text is refused`() {
        val texts = (vectors["recoveryKeyInvalid"] as JsonArray).map { element -> (element as JsonPrimitive).content }
        assertFalse(texts.isEmpty())
        for (text in texts) {
            val error = assertThrows<PasswordsCryptoException>(text) { PasswordsCrypto.parseRecoveryKey(text) }
            assertEquals(PasswordsCryptoFailure.InvalidRecoveryKey, error.failure, text)
        }
    }

    @Test fun `sealed values`() {
        val sections = vectors.list("sealed")
        assertFalse(sections.isEmpty())
        for (section in sections) {
            val name = section.text("name")
            val key = section.bytes("key")
            val associatedData = section.text("associatedData")
            val plaintext = section.bytes("plaintext")
            val expected = section.sealed("sealed")
            assertEquals(expected, PasswordsCrypto.seal(plaintext, key, associatedData, section.bytes("nonce")), name)
            assertArrayEquals(plaintext, PasswordsCrypto.open(expected, key, associatedData), name)
        }
    }

    @Test fun `item fields open as fields`() {
        val section = vectors.list("sealed").first { candidate -> candidate.text("name") == "item fields" }
        // The associated data is item:<id>:<kind>.
        val parts = section.text("associatedData").split(":")
        assertEquals(3, parts.size)
        val itemKind = kind(parts[2])
        val fields = PasswordsCrypto.openItemFields(section.sealed("sealed"), section.bytes("key"), parts[1], itemKind)
        assertEquals(itemKind, fields.itemKind)
        assertFalse(fields.name.isEmpty())
        val shareSection = vectors.obj("share")
        assertEquals(vectors.obj("accessRecord").access("signedAccess"), fields.access)
        assertEquals(7L, fields.access?.recordVersion)
        assertEquals(
            listOf(PasswordsGrantee.Person(shareSection.text("recipientId")), PasswordsGrantee.Circle("00000000-0000-4000-8000-000000000005")),
            fields.access?.grantees,
        )
        val record = vectors.obj("accessRecord")
        assertEquals(fields.access, PasswordsCrypto.verifyAccess(record.bytes("accountKey"), parts[1], itemKind, fields))
        // Sealing the fields again with the same nonce gives something that opens to the same fields.
        val resealed = PasswordsCrypto.sealItemFields(fields, section.bytes("key"), parts[1], section.bytes("nonce"))
        assertEquals(fields, PasswordsCrypto.openItemFields(resealed, section.bytes("key"), parts[1], itemKind))
    }

    @Test fun `access record signature`() {
        val section = vectors.obj("accessRecord")
        val accountKey = section.bytes("accountKey")
        val itemId = section.text("itemId")
        val itemKind = kind(section.text("itemKind"))
        val signed = section.access("signedAccess")
        assertArrayEquals(section.bytes("macKey"), PasswordsCrypto.accessMacKey(accountKey))
        assertArrayEquals(section.bytes("signedBytes"), PasswordsCrypto.accessSignedBytes(itemId, itemKind, signed))
        assertEquals(signed.recordMac, PasswordsCrypto.macOfAccess(accountKey, itemId, itemKind, signed))
        val highestVersionSeen = (section["highestVersionSeen"] as JsonPrimitive).long
        assertEquals(signed, PasswordsCrypto.signAccess(accountKey, itemId, itemKind, section.access("access"), highestVersionSeen))
        assertTrue(PasswordsCrypto.isAccessVerified(accountKey, itemId, itemKind, signed))
        val forgedRecords = section.list("forged")
        assertFalse(forgedRecords.isEmpty())
        for (forged in forgedRecords) {
            assertFalse(
                PasswordsCrypto.isAccessVerified(accountKey, forged.text("itemId"), kind(forged.text("itemKind")), forged.access("access")),
                forged.text("name"),
            )
        }
    }

    @Test fun `access record refuses control characters and reads other spellings`() {
        val section = vectors.obj("accessRecord")
        val accountKey = section.bytes("accountKey")
        val highestVersionSeen = (section["highestVersionSeen"] as JsonPrimitive).long
        val unsignable = section.list("unsignable")
        assertFalse(unsignable.isEmpty())
        for (record in unsignable) {
            assertThrows<PasswordsCryptoException>(record.text("name")) {
                PasswordsCrypto.signAccess(accountKey, record.text("itemId"), kind(record.text("itemKind")), record.access("access"), highestVersionSeen)
            }
        }
        val readCases = section.list("readCases")
        assertFalse(readCases.isEmpty())
        for (readCase in readCases) {
            val name = readCase.text("name")
            val isReadable = (readCase["isReadable"] as JsonPrimitive).boolean
            val read = runCatching { PasswordsItemAccess.fromJson(Json.parseToJsonElement(readCase.text("accessJson"))) }.getOrNull()
            if (read == null) {
                assertFalse(isReadable, name)
                continue
            }
            assertTrue(isReadable, name)
            assertEquals(
                (readCase["isVerified"] as JsonPrimitive).boolean,
                PasswordsCrypto.isAccessVerified(accountKey, readCase.text("itemId"), kind(readCase.text("itemKind")), read),
                name,
            )
            assertEquals((readCase["recordVersion"] as JsonPrimitive).long, read.recordVersion ?: 0L, name)
        }
    }

    @Test fun `person key`() {
        val section = vectors.obj("personKey")
        val keyPair = PasswordsCrypto.keyPair(section.bytes("privateKey"))
        assertArrayEquals(section.bytes("publicKey"), keyPair.publicKey)
        val code = section.text("safetyCode")
        assertEquals(code, PasswordsCrypto.safetyCode(keyPair.publicKey))
        val typedCodes = (section["typedSafetyCodes"] as JsonArray).map { element -> (element as JsonPrimitive).content }
        for (typed in listOf(code) + typedCodes) assertEquals(code, PasswordsCrypto.normalizeSafetyCode(typed), typed)
        val invalidCodes = (section["invalidSafetyCodes"] as JsonArray).map { element -> (element as JsonPrimitive).content }
        assertFalse(invalidCodes.isEmpty())
        for (invalid in invalidCodes) assertNull(PasswordsCrypto.normalizeSafetyCode(invalid), invalid)
    }

    @Test fun `share`() {
        val section = vectors.obj("share")
        val ownerId = section.text("ownerId")
        val recipientId = section.text("recipientId")
        val itemId = section.text("itemId")
        val senderPrivateKey = section.bytes("senderPrivateKey")
        val senderPublicKey = section.bytes("senderPublicKey")
        val recipientPrivateKey = section.bytes("recipientPrivateKey")
        val recipientPublicKey = section.bytes("recipientPublicKey")
        val ephemeralPrivateKey = section.bytes("ephemeralPrivateKey")
        val ephemeralPublicKey = section.bytes("ephemeralPublicKey")
        val sender = PasswordsCrypto.keyPair(senderPrivateKey)
        assertArrayEquals(senderPublicKey, sender.publicKey)
        assertArrayEquals(recipientPublicKey, PasswordsCrypto.keyPair(recipientPrivateKey).publicKey)
        assertArrayEquals(ephemeralPublicKey, PasswordsCrypto.keyPair(ephemeralPrivateKey).publicKey)

        val ephemeralShared = PasswordsCrypto.sharedSecret(ephemeralPrivateKey, recipientPublicKey)
        val ownerShared = PasswordsCrypto.sharedSecret(senderPrivateKey, recipientPublicKey)
        assertArrayEquals(section.bytes("ephemeralShared"), ephemeralShared)
        assertArrayEquals(section.bytes("ownerShared"), ownerShared)
        // The recipient reaches the same two secrets from its own side.
        assertArrayEquals(ephemeralShared, PasswordsCrypto.sharedSecret(recipientPrivateKey, ephemeralPublicKey))
        assertArrayEquals(ownerShared, PasswordsCrypto.sharedSecret(recipientPrivateKey, senderPublicKey))
        assertArrayEquals(section.bytes("key"), PasswordsCrypto.shareKey(ephemeralShared, ownerShared, recipientPublicKey))

        val itemKey = section.bytes("itemKey")
        val expected = PasswordsKeyPairSealedValue.fromJsonText(section.obj("sealed").toString())
        val sealed = PasswordsCrypto.sealForKeyPair(
            itemKey, sender, ownerId, recipientPublicKey, recipientId, itemId, ephemeralPrivateKey, section.bytes("nonce"),
        )
        assertEquals(expected, sealed)
        assertArrayEquals(itemKey, PasswordsCrypto.openForKeyPair(expected, recipientPrivateKey, senderPublicKey, ownerId, recipientId, itemId))

        // Another owner key, owner id, recipient id or item id does not open it.
        val otherOwner = PasswordsCrypto.makeKeyPair()
        val error = assertThrows<PasswordsCryptoException> {
            PasswordsCrypto.openForKeyPair(expected, recipientPrivateKey, otherOwner.publicKey, ownerId, recipientId, itemId)
        }
        assertEquals(PasswordsCryptoFailure.OpenFailed, error.failure)
        for ((otherOwnerId, otherRecipientId, otherItemId) in listOf(
            Triple("someone-else", recipientId, itemId),
            Triple(ownerId, "someone-else", itemId),
            Triple(ownerId, recipientId, "another-item"),
        )) {
            assertThrows<PasswordsCryptoException> {
                PasswordsCrypto.openForKeyPair(expected, recipientPrivateKey, senderPublicKey, otherOwnerId, otherRecipientId, otherItemId)
            }
        }
    }

    @Test fun `one time codes`() {
        val sections = vectors.list("oneTimeCodes")
        assertFalse(sections.isEmpty())
        for (section in sections) {
            val uri = section.text("uri")
            val settings = PasswordsOneTimeCode.parse(uri)
            val unixSeconds = (section["unixSeconds"] as JsonPrimitive).long
            assertEquals(section.text("code"), PasswordsOneTimeCode.code(settings, unixSeconds), uri)
        }
    }

    @Test fun `passkey`() {
        val section = vectors.obj("passkey")
        val privateKey = section.bytes("privateKey")
        val publicKeyX = section.bytes("publicKeyX")
        val publicKeyY = section.bytes("publicKeyY")
        val relyingPartyId = section.text("relyingPartyId")
        val credentialId = section.bytes("credentialId")
        val isVerified = (section["isVerified"] as JsonPrimitive).boolean

        val (x, y) = PasswordsPasskey.publicKeyCoordinates(privateKey)
        assertArrayEquals(publicKeyX, x)
        assertArrayEquals(publicKeyY, y)
        // The key is read from Go's PKCS#8, and written back in the same bytes.
        val scalar = PasswordsPasskey.scalarFromDer(privateKey)!!
        assertArrayEquals(privateKey, PasswordsPasskey.pkcs8(scalar, byteArrayOf(4) + publicKeyX + publicKeyY))

        assertArrayEquals(section.bytes("cosePublicKey"), PasswordsPasskey.cosePublicKey(publicKeyX, publicKeyY))
        val registrationData = PasswordsPasskey.registrationAuthenticatorData(relyingPartyId, credentialId, publicKeyX, publicKeyY, isVerified)
        assertArrayEquals(section.bytes("registrationAuthenticatorData"), registrationData)
        assertArrayEquals(section.bytes("attestationObject"), PasswordsPasskey.attestationObject(registrationData))
        val registration = PasswordsPasskey.register(relyingPartyId, isVerified, privateKey, credentialId)
        assertArrayEquals(registrationData, registration.authenticatorData)

        val assertionData = PasswordsPasskey.assertionAuthenticatorData(relyingPartyId, isVerified)
        assertArrayEquals(section.bytes("assertionAuthenticatorData"), assertionData)
        val clientDataHash = section.bytes("clientDataHash")

        // The signature Go made verifies here, and one made here verifies too.
        assertTrue(PasswordsPasskey.isSignatureValid(section.bytes("signature"), publicKeyX, publicKeyY, assertionData, clientDataHash))
        val ownSignature = PasswordsPasskey.sign(privateKey, assertionData, clientDataHash)
        assertTrue(PasswordsPasskey.isSignatureValid(ownSignature, publicKeyX, publicKeyY, assertionData, clientDataHash))
    }
}
