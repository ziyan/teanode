package com.teanode.kit.passwords

import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows
import java.security.MessageDigest

/** The authenticator's bytes against the layout the plan fixes, and that what it signs verifies. PasswordsVectorsTest checks the same bytes against Go. */
class PasswordsPasskeyTest {
    private val relyingPartyId = "example.com"
    private val clientDataHash = ByteArray(32) { index -> (255 - index).toByte() }

    private fun failure(block: () -> Unit): PasswordsPasskeyFailure = assertThrows<PasswordsPasskeyException>(block).failure

    private fun sha256(text: String) = MessageDigest.getInstance("SHA-256").digest(text.toByteArray())

    @Test fun `the private key is PKCS8 not SEC1`() {
        val privateKey = PasswordsPasskey.makePrivateKey()
        assertEquals(138, privateKey.size)
        // SEQUENCE, then INTEGER 0 (PKCS#8). A bare SEC1 key would start with INTEGER 1.
        assertArrayEquals(byteArrayOf(0x30, 0x81.toByte(), 0x87.toByte(), 0x02, 0x01, 0x00), privateKey.copyOfRange(0, 6))
        // The platform's own parser accepts it, and gives back the same public key.
        val parsed = java.security.KeyFactory.getInstance("EC").generatePrivate(java.security.spec.PKCS8EncodedKeySpec(privateKey)) as java.security.interfaces.ECPrivateKey
        val scalar = PasswordsPasskey.scalarFromDer(privateKey)!!
        assertEquals(java.math.BigInteger(1, scalar), parsed.s)
        val (x, y) = PasswordsPasskey.publicKeyCoordinates(privateKey)
        assertEquals(65, byteArrayOf(4).size + x.size + y.size)
    }

    @Test fun `our PKCS8 matches the platforms own`() {
        val pair = PasswordsCrypto.makeKeyPair()
        val ours = PasswordsPasskey.pkcs8(pair.privateKey, pair.publicKey)
        assertArrayEquals(pair.privateKey, PasswordsPasskey.scalarFromDer(ours))
        val private = PasswordsEllipticCurve.privateKey(pair.privateKey)
        assertArrayEquals(pair.privateKey, PasswordsPasskey.scalarFromDer(private.encoded))
    }

    @Test fun `bare SEC1 keys are read too`() {
        val scalar = ByteArray(32) { index -> (index + 1).toByte() }
        val sec1 = byteArrayOf(0x30, 0x25, 0x02, 0x01, 0x01, 0x04, 0x20) + scalar
        assertArrayEquals(scalar, PasswordsPasskey.scalarFromDer(sec1))
    }

    @Test fun `garbage is not a private key`() {
        for (garbage in listOf(ByteArray(0), byteArrayOf(0x30), ByteArray(138), byteArrayOf(0x30, 0x03, 0x02, 0x01, 0x07))) {
            assertNull(PasswordsPasskey.scalarFromDer(garbage))
            assertEquals(PasswordsPasskeyFailure.InvalidPrivateKey, failure { PasswordsPasskey.publicKeyCoordinates(garbage) })
        }
    }

    @Test fun `cose public key layout`() {
        val x = ByteArray(32) { 0xAA.toByte() }
        val y = ByteArray(32) { 0xBB.toByte() }
        val cose = PasswordsPasskey.cosePublicKey(x, y)
        assertEquals(77, cose.size)
        assertArrayEquals(
            byteArrayOf(0xA5.toByte(), 0x01, 0x02, 0x03, 0x26, 0x20, 0x01, 0x21, 0x58, 0x20, 0xAA.toByte(), 0xAA.toByte(), 0xAA.toByte()),
            cose.copyOfRange(0, 13),
        )
        assertArrayEquals(x, cose.copyOfRange(10, 42))
        assertArrayEquals(byteArrayOf(0x22, 0x58, 0x20), cose.copyOfRange(42, 45))
        assertArrayEquals(y, cose.copyOfRange(45, 77))
        assertThrows<PasswordsPasskeyException> { PasswordsPasskey.cosePublicKey(ByteArray(31), y) }
    }

    @Test fun `registration authenticator data layout`() {
        val registration = PasswordsPasskey.register(relyingPartyId, true)
        val data = registration.authenticatorData
        assertEquals(32 + 1 + 4 + 16 + 2 + 16 + 77, data.size)
        assertArrayEquals(sha256(relyingPartyId), data.copyOfRange(0, 32))
        assertEquals(0x5D, data[32].toInt())
        assertArrayEquals(byteArrayOf(0, 0, 0, 0), data.copyOfRange(33, 37))
        assertArrayEquals("TEANODEPASSWORDS".toByteArray(), data.copyOfRange(37, 53))
        assertArrayEquals(byteArrayOf(0, 16), data.copyOfRange(53, 55))
        assertArrayEquals(registration.credentialId, data.copyOfRange(55, 71))
        assertArrayEquals(registration.cosePublicKey, data.copyOfRange(data.size - 77, data.size))
        assertEquals(0x59, PasswordsPasskey.register(relyingPartyId, false).authenticatorData[32].toInt())
    }

    @Test fun `the AAGUID is the documented bytes`() {
        assertArrayEquals(
            intArrayOf(0x54, 0x45, 0x41, 0x4E, 0x4F, 0x44, 0x45, 0x50, 0x41, 0x53, 0x53, 0x57, 0x4F, 0x52, 0x44, 0x53).map { value -> value.toByte() }.toByteArray(),
            PasswordsPasskey.aaguid,
        )
    }

    @Test fun `attestation object layout`() {
        val registration = PasswordsPasskey.register(relyingPartyId, true)
        val expected = byteArrayOf(0xA3.toByte()) + byteArrayOf(0x63) + "fmt".toByteArray() + byteArrayOf(0x64) + "none".toByteArray() +
            byteArrayOf(0x67) + "attStmt".toByteArray() + byteArrayOf(0xA0.toByte()) + byteArrayOf(0x68) + "authData".toByteArray() +
            byteArrayOf(0x58, registration.authenticatorData.size.toByte()) + registration.authenticatorData
        assertArrayEquals(expected, registration.attestationObject)
    }

    @Test fun `assertion authenticator data layout`() {
        val verified = PasswordsPasskey.assertionAuthenticatorData(relyingPartyId, true)
        assertEquals(37, verified.size)
        assertArrayEquals(sha256(relyingPartyId), verified.copyOfRange(0, 32))
        assertEquals(0x1D, verified[32].toInt())
        assertArrayEquals(byteArrayOf(0, 0, 0, 0), verified.copyOfRange(33, 37))
        assertEquals(0x19, PasswordsPasskey.assertionAuthenticatorData(relyingPartyId, false)[32].toInt())
    }

    @Test fun `a signature verifies and fails for another message or key`() {
        val registration = PasswordsPasskey.register(relyingPartyId, true)
        val authenticatorData = PasswordsPasskey.assertionAuthenticatorData(relyingPartyId, true)
        val signature = PasswordsPasskey.sign(registration.privateKey, authenticatorData, clientDataHash)
        assertEquals(0x30, signature[0].toInt())
        assertTrue(PasswordsPasskey.isSignatureValid(signature, registration.publicKeyX, registration.publicKeyY, authenticatorData, clientDataHash))
        assertFalse(PasswordsPasskey.isSignatureValid(signature, registration.publicKeyX, registration.publicKeyY, authenticatorData, ByteArray(32)))
        val other = PasswordsPasskey.register(relyingPartyId, true)
        assertFalse(PasswordsPasskey.isSignatureValid(signature, other.publicKeyX, other.publicKeyY, authenticatorData, clientDataHash))
        assertEquals(
            PasswordsPasskeyFailure.InvalidSignature,
            failure { PasswordsPasskey.isSignatureValid(byteArrayOf(1, 2, 3), registration.publicKeyX, registration.publicKeyY, authenticatorData, clientDataHash) },
        )
    }

    @Test fun `registration with a given key and credential is repeatable`() {
        val privateKey = PasswordsPasskey.makePrivateKey()
        val credentialId = ByteArray(16) { 3 }
        val first = PasswordsPasskey.register(relyingPartyId, true, privateKey, credentialId)
        val second = PasswordsPasskey.register(relyingPartyId, true, privateKey, credentialId)
        assertEquals(first, second)
        assertArrayEquals(privateKey, first.privateKey)
    }
}
