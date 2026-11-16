package com.teanode.kit.passwords

import com.teanode.kit.stringValue
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.longOrNull
import java.math.BigInteger
import java.security.AlgorithmParameters
import java.security.KeyFactory
import java.security.KeyPairGenerator
import java.security.MessageDigest
import java.security.SecureRandom
import java.security.interfaces.ECPrivateKey
import java.security.interfaces.ECPublicKey
import java.security.spec.ECGenParameterSpec
import java.security.spec.ECParameterSpec
import java.security.spec.ECPoint
import java.security.spec.ECPrivateKeySpec
import java.security.spec.ECPublicKeySpec
import java.text.Normalizer
import java.util.Base64
import javax.crypto.Cipher
import javax.crypto.KeyAgreement
import javax.crypto.Mac
import javax.crypto.SecretKeyFactory
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.PBEKeySpec
import javax.crypto.spec.SecretKeySpec

// Passwords are encrypted on the person's devices before they reach the server. This file is the byte-level format,
// written to match the Go package internal/passwords, web/src/passwords/crypto.ts and the iPhone app's
// PasswordsCrypto.swift exactly; one file of test vectors (internal/passwords/testdata/vectors.json) pins all of them.
// Every function that draws randomness takes it as an optional parameter, so the tests can pin it.

/** PasswordsCryptoFailure is why a Passwords encryption call failed. A wrong key and a damaged value look the same on purpose. */
enum class PasswordsCryptoFailure {
    /** A key was not 32 bytes. */
    InvalidKeyLength,

    /** A nonce was not 12 bytes. */
    InvalidNonceLength,

    /** A sealed value did not decrypt: the wrong key, the wrong associated data, or damage. */
    OpenFailed,

    /** A sealed value was not the JSON object the format describes. */
    InvalidSealedValue,

    /** A sealed value has a version this build does not know. */
    UnsupportedVersion,

    /** A recovery key did not read back as 32 bytes of base32. */
    InvalidRecoveryKey,

    /** A public or private key was not a valid P-256 key. */
    InvalidKeyPair,

    /** Key derivation from a passphrase failed. */
    KeyDerivationFailed,

    /** An access record could not be signed: the highest version seen is out of range. */
    InvalidAccessRecord,

    /** A passphrase iteration count below the minimum: it comes from the server, and a tiny one would make the proof cheap to guess from. */
    IterationCountTooLow,

    /** A passphrase iteration count above the maximum: an enormous one would make the phone spin until it was killed. */
    IterationCountTooHigh,
}

class PasswordsCryptoException(val failure: PasswordsCryptoFailure) : Exception(failure.name)

/** AES-256-GCM output: a 12 byte nonce and the ciphertext with its 16 byte tag appended. */
class PasswordsSealedValue(
    val version: Int = PasswordsCrypto.SEALED_VALUE_VERSION,
    val nonce: ByteArray,
    /** The encrypted bytes with the 16 byte tag at the end. */
    val ciphertext: ByteArray,
) {
    /** The JSON text that is stored and sent, keys in the order the format lists them. */
    val jsonText: String
        get() = "{\"version\":$version,\"nonce\":\"${base64(nonce)}\",\"ciphertext\":\"${base64(ciphertext)}\"}"

    override fun equals(other: Any?): Boolean =
        other is PasswordsSealedValue && version == other.version && nonce.contentEquals(other.nonce) &&
            ciphertext.contentEquals(other.ciphertext)

    override fun hashCode(): Int = version * 31 + nonce.contentHashCode() * 17 + ciphertext.contentHashCode()

    companion object {
        fun fromJsonText(jsonText: String): PasswordsSealedValue {
            try {
                val entries = Json.parseToJsonElement(jsonText) as JsonObject
                return PasswordsSealedValue(
                    version = (entries["version"] as JsonPrimitive).longOrNull!!.toInt(),
                    nonce = unbase64(entries["nonce"]?.stringValue!!),
                    ciphertext = unbase64(entries["ciphertext"]?.stringValue!!),
                )
            } catch (_: Exception) {
                throw PasswordsCryptoException(PasswordsCryptoFailure.InvalidSealedValue)
            }
        }
    }
}

/**
 * A key sealed for a person's (or the server's agent) key pair: an ephemeral public key, so only the holder of the
 * recipient's private key can open it, then a nonce and the ciphertext with its tag.
 */
class PasswordsKeyPairSealedValue(
    val version: Int = PasswordsCrypto.SEALED_VALUE_VERSION,
    /** The 65 byte uncompressed P-256 point the sender made for this one value. */
    val ephemeralPublicKey: ByteArray,
    val nonce: ByteArray,
    val ciphertext: ByteArray,
) {
    /** The JSON text that is stored and sent, keys in the order the format lists them. */
    val jsonText: String
        get() = "{\"version\":$version,\"ephemeralPublicKey\":\"${base64(ephemeralPublicKey)}\"," +
            "\"nonce\":\"${base64(nonce)}\",\"ciphertext\":\"${base64(ciphertext)}\"}"

    override fun equals(other: Any?): Boolean =
        other is PasswordsKeyPairSealedValue && version == other.version &&
            ephemeralPublicKey.contentEquals(other.ephemeralPublicKey) && nonce.contentEquals(other.nonce) &&
            ciphertext.contentEquals(other.ciphertext)

    override fun hashCode(): Int = version * 31 + ephemeralPublicKey.contentHashCode() + nonce.contentHashCode() + ciphertext.contentHashCode()

    companion object {
        fun fromJsonText(jsonText: String): PasswordsKeyPairSealedValue {
            try {
                val entries = Json.parseToJsonElement(jsonText) as JsonObject
                return PasswordsKeyPairSealedValue(
                    version = (entries["version"] as JsonPrimitive).longOrNull!!.toInt(),
                    ephemeralPublicKey = unbase64(entries["ephemeralPublicKey"]?.stringValue!!),
                    nonce = unbase64(entries["nonce"]?.stringValue!!),
                    ciphertext = unbase64(entries["ciphertext"]?.stringValue!!),
                )
            } catch (_: Exception) {
                throw PasswordsCryptoException(PasswordsCryptoFailure.InvalidSealedValue)
            }
        }
    }
}

/** A person's P-256 key pair for sharing: the 32 byte private scalar and the 65 byte uncompressed public point (04, X, Y). */
class PasswordsKeyPair(val privateKey: ByteArray, val publicKey: ByteArray) {
    override fun equals(other: Any?): Boolean =
        other is PasswordsKeyPair && privateKey.contentEquals(other.privateKey) && publicKey.contentEquals(other.publicKey)

    override fun hashCode(): Int = privateKey.contentHashCode() * 31 + publicKey.contentHashCode()
}

internal fun base64(bytes: ByteArray): String = Base64.getEncoder().encodeToString(bytes)

internal fun unbase64(text: String): ByteArray = Base64.getDecoder().decode(text)

/** PasswordsCrypto is the format of the Passwords feature as functions. Keys and secrets are plain 32 byte arrays. */
object PasswordsCrypto {
    const val KEY_BYTE_COUNT = 32
    const val NONCE_BYTE_COUNT = 12
    const val TAG_BYTE_COUNT = 16
    const val SEALED_VALUE_VERSION = 1
    const val PASSPHRASE_SALT_BYTE_COUNT = 16

    /** The PBKDF2 iteration count new setups use. A setup stores its own count, so this can be raised later. */
    const val PASSPHRASE_ITERATION_COUNT = 600_000
    const val MINIMUM_PASSPHRASE_ITERATION_COUNT = 600_000
    const val MAXIMUM_PASSPHRASE_ITERATION_COUNT = 10_000_000

    /** The fewest characters a passphrase has, on every device. */
    const val MINIMUM_PASSPHRASE_LENGTH = 12
    const val RECOVERY_KEY_BYTE_COUNT = 32
    const val PUBLIC_KEY_BYTE_COUNT = 65
    const val PRIVATE_KEY_BYTE_COUNT = 32

    /** The recipient id of the server's agent key in a share's associated data. */
    const val AGENT_RECIPIENT_ID = "agent"

    private const val WRAP_KEY_INFO = "teanode passwords wrap v1"
    private const val PROOF_KEY_INFO = "teanode passwords proof v1"
    private const val SHARE_KEY_INFO = "teanode passwords share v1"
    private const val ACCESS_RECORD_LABEL = "teanode passwords access record v1"
    private const val SAFETY_CODE_LABEL = "teanode passwords safety code v2"
    private const val SAFETY_CODE_GROUP_COUNT = 8

    /** The highest record version any implementation accepts: the largest integer a JavaScript number holds exactly. */
    const val MAXIMUM_RECORD_VERSION = 9_007_199_254_740_991L

    private val secureRandom = SecureRandom()

    // Associated data

    /** Associated data of the account key sealed with the wrap key. */
    const val ACCOUNT_KEY_ASSOCIATED_DATA = "account-key"

    /** Associated data of the private key sealed with the account key. */
    const val PRIVATE_KEY_ASSOCIATED_DATA = "private-key"

    /** Associated data of an item key sealed with the account key. */
    fun itemKeyAssociatedData(itemId: String): String = "item-key:$itemId"

    /** Associated data of an item's fields sealed with its item key. */
    fun itemFieldsAssociatedData(itemId: String, itemKind: PasswordsItemKind): String = "item:$itemId:${itemKind.rawValue}"

    /** Associated data of an item key sealed for a recipient: `share:<item id>:<owner id>:<recipient id>`. */
    fun shareAssociatedData(itemId: String, ownerId: String, recipientId: String): String = "share:$itemId:$ownerId:$recipientId"

    // Randomness

    fun randomBytes(count: Int): ByteArray = ByteArray(count).also { bytes -> secureRandom.nextBytes(bytes) }

    /** A new 32 byte key: an account key, an item key or a recovery key's bytes. */
    fun makeKey(): ByteArray = randomBytes(KEY_BYTE_COUNT)

    // Sealed values

    /** Seals bytes with AES-256-GCM. The associated data is text that must be given again to open. Pass `nonce` only in tests. */
    fun seal(plaintext: ByteArray, key: ByteArray, associatedData: String, nonce: ByteArray? = null): PasswordsSealedValue {
        requireKey(key)
        val nonceBytes = nonce ?: randomBytes(NONCE_BYTE_COUNT)
        if (nonceBytes.size != NONCE_BYTE_COUNT) throw PasswordsCryptoException(PasswordsCryptoFailure.InvalidNonceLength)
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, SecretKeySpec(key, "AES"), GCMParameterSpec(TAG_BYTE_COUNT * 8, nonceBytes))
        cipher.updateAAD(associatedData.toByteArray(Charsets.UTF_8))
        return PasswordsSealedValue(nonce = nonceBytes, ciphertext = cipher.doFinal(plaintext))
    }

    /** Opens a sealed value. Any failure (key, associated data or damage) is [PasswordsCryptoFailure.OpenFailed]. */
    fun open(sealed: PasswordsSealedValue, key: ByteArray, associatedData: String): ByteArray {
        if (sealed.version != SEALED_VALUE_VERSION) throw PasswordsCryptoException(PasswordsCryptoFailure.UnsupportedVersion)
        return openBytes(sealed.nonce, sealed.ciphertext, key, associatedData)
    }

    private fun openBytes(nonce: ByteArray, ciphertext: ByteArray, key: ByteArray, associatedData: String): ByteArray {
        requireKey(key)
        if (nonce.size != NONCE_BYTE_COUNT || ciphertext.size < TAG_BYTE_COUNT) {
            throw PasswordsCryptoException(PasswordsCryptoFailure.OpenFailed)
        }
        try {
            val cipher = Cipher.getInstance("AES/GCM/NoPadding")
            cipher.init(Cipher.DECRYPT_MODE, SecretKeySpec(key, "AES"), GCMParameterSpec(TAG_BYTE_COUNT * 8, nonce))
            cipher.updateAAD(associatedData.toByteArray(Charsets.UTF_8))
            return cipher.doFinal(ciphertext)
        } catch (_: Exception) {
            throw PasswordsCryptoException(PasswordsCryptoFailure.OpenFailed)
        }
    }

    private fun requireKey(key: ByteArray) {
        if (key.size != KEY_BYTE_COUNT) throw PasswordsCryptoException(PasswordsCryptoFailure.InvalidKeyLength)
    }

    // Keys from a passphrase or a recovery key

    /**
     * The passphrase key: PBKDF2-HMAC-SHA256 over the passphrase after Unicode NFC normalization, 32 bytes. This is slow
     * on purpose (600,000 iterations); the autofill service must never call it. The count must be from
     * [MINIMUM_PASSPHRASE_ITERATION_COUNT] to [MAXIMUM_PASSPHRASE_ITERATION_COUNT].
     */
    fun passphraseKey(passphrase: String, salt: ByteArray, iterationCount: Int): ByteArray {
        if (iterationCount < MINIMUM_PASSPHRASE_ITERATION_COUNT) throw PasswordsCryptoException(PasswordsCryptoFailure.IterationCountTooLow)
        if (iterationCount > MAXIMUM_PASSPHRASE_ITERATION_COUNT) throw PasswordsCryptoException(PasswordsCryptoFailure.IterationCountTooHigh)
        return derivePassphraseKey(passphrase, salt, iterationCount)
    }

    /** The derivation without the minimum, so the tests can check it against published vectors that use small counts. */
    internal fun derivePassphraseKey(passphrase: String, salt: ByteArray, iterationCount: Int): ByteArray {
        if (iterationCount <= 0) throw PasswordsCryptoException(PasswordsCryptoFailure.KeyDerivationFailed)
        val normalized = Normalizer.normalize(passphrase, Normalizer.Form.NFC)
        try {
            val specification = PBEKeySpec(normalized.toCharArray(), salt, iterationCount, KEY_BYTE_COUNT * 8)
            return SecretKeyFactory.getInstance("PBKDF2WithHmacSHA256").generateSecret(specification).encoded
        } catch (_: Exception) {
            throw PasswordsCryptoException(PasswordsCryptoFailure.KeyDerivationFailed)
        }
    }

    /** The key that seals the account key, from a 32 byte secret (the passphrase key, or a recovery key's bytes): HKDF-SHA256, empty salt. */
    fun wrapKey(secret: ByteArray): ByteArray = hkdf(secret, ByteArray(0), WRAP_KEY_INFO)

    /** The proof a device sends to unlock, from the same secret as the wrap key. The server stores the proof's SHA-256. */
    fun proof(secret: ByteArray): ByteArray = hkdf(secret, ByteArray(0), PROOF_KEY_INFO)

    /** SHA-256 of the proof bytes, which is what the server stores and compares. */
    fun proofHash(proof: ByteArray): ByteArray = MessageDigest.getInstance("SHA-256").digest(proof)

    private fun hmac(key: ByteArray, input: ByteArray): ByteArray {
        val mac = Mac.getInstance("HmacSHA256")
        mac.init(SecretKeySpec(key, "HmacSHA256"))
        return mac.doFinal(input)
    }

    /** HKDF-SHA256 of a key of any length, 32 bytes out (one block of the expansion). */
    private fun hkdfBytes(inputKeyMaterial: ByteArray, salt: ByteArray, info: ByteArray): ByteArray {
        val pseudoRandomKey = hmac(if (salt.isEmpty()) ByteArray(32) else salt, inputKeyMaterial)
        return hmac(pseudoRandomKey, info + byteArrayOf(1))
    }

    private fun hkdf(secret: ByteArray, salt: ByteArray, info: String): ByteArray {
        requireKey(secret)
        return hkdfBytes(secret, salt, info.toByteArray(Charsets.UTF_8))
    }

    // Recovery key

    /** A new recovery key's 32 bytes. */
    fun makeRecoveryKey(): ByteArray = randomBytes(RECOVERY_KEY_BYTE_COUNT)

    /** The recovery key as people see it: RFC 4648 base32 without padding in 13 groups of 4 letters, joined by single spaces. */
    fun formatRecoveryKey(recoveryKey: ByteArray): String {
        if (recoveryKey.size != RECOVERY_KEY_BYTE_COUNT) throw PasswordsCryptoException(PasswordsCryptoFailure.InvalidRecoveryKey)
        return PasswordsBase32.encode(recoveryKey).chunked(4).joinToString(" ")
    }

    /**
     * Reads a recovery key as the person typed it, ignoring spaces, tabs, line breaks, hyphens and the case of the
     * letters A to Z. Any other character is refused, and so is a last letter whose unused bits are not zero. Every
     * implementation refuses the same texts (`recoveryKeyInvalid` in the test vectors).
     */
    fun parseRecoveryKey(text: String): ByteArray {
        val letters = StringBuilder()
        for (byte in text.toByteArray(Charsets.UTF_8)) {
            val code = byte.toInt() and 0xFF
            when (code) {
                0x20, 0x09, 0x0A, 0x0D, 0x2D -> continue
                in 'a'.code..'z'.code -> letters.append((code - 'a'.code + 'A'.code).toChar())
                in 'A'.code..'Z'.code, in '2'.code..'7'.code -> letters.append(code.toChar())
                else -> throw PasswordsCryptoException(PasswordsCryptoFailure.InvalidRecoveryKey)
            }
        }
        val letterText = letters.toString()
        val bytes = PasswordsBase32.decode(letterText)
        if (letterText.length != PasswordsBase32.encodedLength(RECOVERY_KEY_BYTE_COUNT) || bytes == null ||
            bytes.size != RECOVERY_KEY_BYTE_COUNT || PasswordsBase32.encode(bytes) != letterText
        ) {
            throw PasswordsCryptoException(PasswordsCryptoFailure.InvalidRecoveryKey)
        }
        return bytes
    }

    // Person key pair

    /** A new P-256 key pair for sharing. */
    fun makeKeyPair(): PasswordsKeyPair {
        val generator = KeyPairGenerator.getInstance("EC")
        generator.initialize(ECGenParameterSpec("secp256r1"))
        val generated = generator.generateKeyPair()
        val scalar = (generated.private as ECPrivateKey).s
        val point = (generated.public as ECPublicKey).w
        return PasswordsKeyPair(fixedLength(scalar, 32), PasswordsEllipticCurve.encodePoint(point))
    }

    /** The key pair for a stored 32 byte private scalar. */
    fun keyPair(privateKey: ByteArray): PasswordsKeyPair {
        val scalar = PasswordsEllipticCurve.scalar(privateKey)
        return PasswordsKeyPair(fixedLength(scalar, 32), PasswordsEllipticCurve.encodePoint(PasswordsEllipticCurve.multiplyBase(scalar)))
    }

    /** The code two people compare out loud to be sure a public key is the one they think: eight groups of four digits. */
    fun safetyCode(publicKey: ByteArray): String {
        if (publicKey.size != PUBLIC_KEY_BYTE_COUNT) throw PasswordsCryptoException(PasswordsCryptoFailure.InvalidKeyPair)
        val input = SAFETY_CODE_LABEL.toByteArray(Charsets.UTF_8) + byteArrayOf(0) + publicKey
        val digest = MessageDigest.getInstance("SHA-256").digest(input)
        return (0 until SAFETY_CODE_GROUP_COUNT).joinToString(" ") { index ->
            val number = ((digest[index * 2].toInt() and 0xFF) shl 8 or (digest[index * 2 + 1].toInt() and 0xFF)) % 10000
            number.toString().padStart(4, '0')
        }
    }

    /**
     * Reads a safety code a person typed or pasted: 32 digits with white space anywhere or nowhere. Returns the code as
     * [safetyCode] writes it, so two codes compare as text, or null for anything else.
     */
    fun normalizeSafetyCode(text: String): String? {
        val digits = text.filter { character -> !character.isWhitespace() }
        if (digits.length != SAFETY_CODE_GROUP_COUNT * 4 || !digits.all { character -> character in '0'..'9' }) return null
        return digits.chunked(4).joinToString(" ")
    }

    // Access record signature

    /** The key an access record is signed with: HKDF-SHA256 of the account key, no salt, the label as the info. */
    fun accessMacKey(accountKey: ByteArray): ByteArray = hkdf(accountKey, ByteArray(0), ACCESS_RECORD_LABEL)

    private fun hasControlCharacter(text: String): Boolean =
        text.any { character -> Character.getType(character) == Character.CONTROL.toInt() }

    /**
     * What the signature covers: the label, a zero byte, the item id, a zero byte, the item kind, a zero byte, the record
     * version in decimal, a zero byte, "1" or "0" for agent use, a zero byte, then each grantee as "u:" and the user id or
     * "c:" and the circle id, in UTF-8 byte order, each followed by a zero byte. The record needs a version.
     */
    fun accessSignedBytes(itemId: String, itemKind: PasswordsItemKind, access: PasswordsItemAccess): ByteArray {
        val version = access.recordVersion
        if (version == null || version < 1 || version > MAXIMUM_RECORD_VERSION) {
            throw PasswordsCryptoException(PasswordsCryptoFailure.InvalidAccessRecord)
        }
        val granteeTexts = access.grantees.map { grantee ->
            val identifier: String
            val text: String
            when (grantee) {
                is PasswordsGrantee.Person -> {
                    identifier = grantee.userId
                    text = "u:${grantee.userId}"
                }
                is PasswordsGrantee.Circle -> {
                    identifier = grantee.circleId
                    text = "c:${grantee.circleId}"
                }
            }
            // An id with a control character is refused when signing and when verifying: the parts are separated by a
            // zero byte, so such an id could read as two.
            if (identifier.isEmpty() || hasControlCharacter(identifier)) throw PasswordsCryptoException(PasswordsCryptoFailure.InvalidAccessRecord)
            text.toByteArray(Charsets.UTF_8)
        }
        val granteeBytes = granteeTexts.sortedWith { left, right -> compareUnsigned(left, right) }
        val signed = java.io.ByteArrayOutputStream()
        for (part in listOf(ACCESS_RECORD_LABEL, itemId, itemKind.rawValue, version.toString(), if (access.isUsableByAgent) "1" else "0")) {
            signed.write(part.toByteArray(Charsets.UTF_8))
            signed.write(0)
        }
        for (bytes in granteeBytes) {
            signed.write(bytes)
            signed.write(0)
        }
        return signed.toByteArray()
    }

    private fun compareUnsigned(left: ByteArray, right: ByteArray): Int {
        val shared = minOf(left.size, right.size)
        for (index in 0 until shared) {
            val difference = (left[index].toInt() and 0xFF) - (right[index].toInt() and 0xFF)
            if (difference != 0) return difference
        }
        return left.size - right.size
    }

    /** The signature of an access record for an item: HMAC-SHA256 over [accessSignedBytes], under [accessMacKey], as base64 with padding. */
    fun macOfAccess(accountKey: ByteArray, itemId: String, itemKind: PasswordsItemKind, access: PasswordsItemAccess): String =
        base64(hmac(accessMacKey(accountKey), accessSignedBytes(itemId, itemKind, access)))

    /**
     * A copy of the record with its version set to one more than the highest version this device has seen with a valid
     * signature for the item (0 if none), and the signature for that version. The version and signature the given record
     * has are ignored: a version the server supplied is never a reason to count higher.
     */
    fun signAccess(
        accountKey: ByteArray,
        itemId: String,
        itemKind: PasswordsItemKind,
        access: PasswordsItemAccess?,
        highestVersionSeen: Long,
    ): PasswordsItemAccess {
        if (highestVersionSeen < 0 || highestVersionSeen >= MAXIMUM_RECORD_VERSION) {
            throw PasswordsCryptoException(PasswordsCryptoFailure.InvalidAccessRecord)
        }
        val unsigned = PasswordsItemAccess(
            isUsableByAgent = access?.isUsableByAgent ?: false,
            grantees = access?.grantees ?: emptyList(),
            recordVersion = highestVersionSeen + 1,
        )
        return unsigned.copy(recordMac = macOfAccess(accountKey, itemId, itemKind, unsigned))
    }

    /** The fields with the given access record signed in place of the one they had. Everything else is unchanged. */
    fun withSignedAccess(
        accountKey: ByteArray,
        itemId: String,
        itemKind: PasswordsItemKind,
        fields: PasswordsItemFields,
        access: PasswordsItemAccess?,
        highestVersionSeen: Long,
    ): PasswordsItemFields = fields.withAccess(signAccess(accountKey, itemId, itemKind, access, highestVersionSeen))

    /**
     * Whether the record is signed by the owner's devices for this item: it has a version and a signature, and the
     * signature is the one [macOfAccess] gives. The comparison takes the same time whatever the bytes.
     */
    fun isAccessVerified(accountKey: ByteArray, itemId: String, itemKind: PasswordsItemKind, access: PasswordsItemAccess?): Boolean {
        if (access == null || access.recordVersion == null) return false
        val text = access.recordMac ?: return false
        return try {
            val given = unbase64(text)
            if (given.size != KEY_BYTE_COUNT) return false
            val expected = hmac(accessMacKey(accountKey), accessSignedBytes(itemId, itemKind, access))
            MessageDigest.isEqual(given, expected)
        } catch (_: Exception) {
            false
        }
    }

    /**
     * The fields' access record when it is signed for this item, and null when there is none or it is not signed. A
     * device treats null as a record that reaches nobody. A recipient cannot call this: the key is the owner's.
     */
    fun verifyAccess(accountKey: ByteArray, itemId: String, itemKind: PasswordsItemKind, fields: PasswordsItemFields): PasswordsItemAccess? =
        if (isAccessVerified(accountKey, itemId, itemKind, fields.access)) fields.access else null

    // Sealed for a key pair

    /** The X coordinate of ECDH(private key, public key), 32 bytes. */
    fun sharedSecret(privateKey: ByteArray, publicKey: ByteArray): ByteArray {
        val privateJava = PasswordsEllipticCurve.privateKey(privateKey)
        val publicJava = PasswordsEllipticCurve.publicKey(publicKey)
        try {
            val agreement = KeyAgreement.getInstance("ECDH")
            agreement.init(privateJava)
            agreement.doPhase(publicJava, true)
            return fixedLength(BigInteger(1, agreement.generateSecret()), 32)
        } catch (_: Exception) {
            throw PasswordsCryptoException(PasswordsCryptoFailure.InvalidKeyPair)
        }
    }

    /** Whether the bytes are a public key: 65 bytes, starting with 04, that are a point on the P-256 curve. */
    fun isPublicKeyValid(publicKey: ByteArray): Boolean = PasswordsEllipticCurve.decodePoint(publicKey) != null

    /** The key an item key is sealed with for a recipient: HKDF-SHA256 of the ephemeral secret then the owner secret, the recipient's 65 public key bytes as salt. */
    fun shareKey(ephemeralSharedSecret: ByteArray, ownerSharedSecret: ByteArray, recipientPublicKey: ByteArray): ByteArray {
        if (ephemeralSharedSecret.size != KEY_BYTE_COUNT || ownerSharedSecret.size != KEY_BYTE_COUNT) {
            throw PasswordsCryptoException(PasswordsCryptoFailure.InvalidKeyLength)
        }
        return hkdfBytes(ephemeralSharedSecret + ownerSharedSecret, recipientPublicKey, SHARE_KEY_INFO.toByteArray(Charsets.UTF_8))
    }

    /**
     * Seals bytes (an item key) so only the holder of the recipient's private key can open them, and only when they came
     * from the owner. The key mixes two secrets: one from a fresh ephemeral key and one from the owner's own private key,
     * so the server cannot plant an item "shared by" somebody. `recipientId` is the recipient's user id, or
     * [AGENT_RECIPIENT_ID] for the server's agent key. Pass the ephemeral key and nonce only in tests.
     */
    fun sealForKeyPair(
        plaintext: ByteArray,
        ownerKeyPair: PasswordsKeyPair,
        ownerId: String,
        recipientPublicKey: ByteArray,
        recipientId: String,
        itemId: String,
        ephemeralPrivateKey: ByteArray? = null,
        nonce: ByteArray? = null,
    ): PasswordsKeyPairSealedValue {
        val ephemeralKeyPair = ephemeralPrivateKey?.let { scalar -> keyPair(scalar) } ?: makeKeyPair()
        val ephemeralShared = sharedSecret(ephemeralKeyPair.privateKey, recipientPublicKey)
        val ownerShared = sharedSecret(ownerKeyPair.privateKey, recipientPublicKey)
        val key = shareKey(ephemeralShared, ownerShared, recipientPublicKey)
        val sealed = seal(plaintext, key, shareAssociatedData(itemId, ownerId, recipientId), nonce)
        return PasswordsKeyPairSealedValue(ephemeralPublicKey = ephemeralKeyPair.publicKey, nonce = sealed.nonce, ciphertext = sealed.ciphertext)
    }

    /**
     * Opens a value sealed for this key pair by the owner whose public key is `ownerPublicKey`. A value sealed by anybody
     * else (the server, say) does not open. Any failure is OpenFailed, except a version this build does not know.
     */
    fun openForKeyPair(
        sealed: PasswordsKeyPairSealedValue,
        recipientPrivateKey: ByteArray,
        ownerPublicKey: ByteArray,
        ownerId: String,
        recipientId: String,
        itemId: String,
    ): ByteArray {
        if (sealed.version != SEALED_VALUE_VERSION) throw PasswordsCryptoException(PasswordsCryptoFailure.UnsupportedVersion)
        val recipientPublicKey = keyPair(recipientPrivateKey).publicKey
        if (!isPublicKeyValid(ownerPublicKey) || !isPublicKeyValid(sealed.ephemeralPublicKey)) {
            throw PasswordsCryptoException(PasswordsCryptoFailure.OpenFailed)
        }
        val ephemeralShared = runCatching { sharedSecret(recipientPrivateKey, sealed.ephemeralPublicKey) }.getOrNull()
        val ownerShared = runCatching { sharedSecret(recipientPrivateKey, ownerPublicKey) }.getOrNull()
        if (ephemeralShared == null || ownerShared == null) throw PasswordsCryptoException(PasswordsCryptoFailure.OpenFailed)
        val key = shareKey(ephemeralShared, ownerShared, recipientPublicKey)
        return openBytes(sealed.nonce, sealed.ciphertext, key, shareAssociatedData(itemId, ownerId, recipientId))
    }

    // Items

    /** Seals an item's fields with its item key. */
    fun sealItemFields(fields: PasswordsItemFields, itemKey: ByteArray, itemId: String, nonce: ByteArray? = null): PasswordsSealedValue =
        seal(fields.jsonBytes(), itemKey, itemFieldsAssociatedData(itemId, fields.itemKind), nonce)

    /** Opens an item's fields. The kind is part of the associated data, so a value cannot be opened as another kind. */
    fun openItemFields(sealed: PasswordsSealedValue, itemKey: ByteArray, itemId: String, itemKind: PasswordsItemKind): PasswordsItemFields {
        val plaintext = open(sealed, itemKey, itemFieldsAssociatedData(itemId, itemKind))
        return try {
            PasswordsItemFields.fromJsonBytes(itemKind, plaintext)
        } catch (_: PasswordsFieldsException) {
            throw PasswordsCryptoException(PasswordsCryptoFailure.InvalidSealedValue)
        }
    }

    internal fun fixedLength(value: BigInteger, length: Int): ByteArray {
        val raw = value.toByteArray()
        val stripped = if (raw.size > length && raw[0] == 0.toByte()) raw.copyOfRange(raw.size - length, raw.size) else raw
        return ByteArray(length - stripped.size) + stripped
    }
}

/** RFC 4648 base32, written by hand. It is used for recovery keys and for the secrets in one-time code links. */
internal object PasswordsBase32 {
    private const val ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"

    /** The letters for bytes, without padding. */
    fun encode(bytes: ByteArray): String {
        val letters = StringBuilder()
        var buffer = 0
        var bufferBitCount = 0
        for (byte in bytes) {
            buffer = (buffer shl 8) or (byte.toInt() and 0xFF)
            bufferBitCount += 8
            while (bufferBitCount >= 5) {
                letters.append(ALPHABET[(buffer shr (bufferBitCount - 5)) and 31])
                bufferBitCount -= 5
            }
            buffer = buffer and ((1 shl bufferBitCount) - 1)
        }
        if (bufferBitCount > 0) letters.append(ALPHABET[(buffer shl (5 - bufferBitCount)) and 31])
        return letters.toString()
    }

    /** How many letters [byteCount] bytes make without padding. */
    fun encodedLength(byteCount: Int): Int = (byteCount * 8 + 4) / 5

    /** The bytes for letters, ignoring case, spaces and `=` padding; null for any other character. Bits left over at the end are dropped. */
    fun decode(text: String): ByteArray? {
        val bytes = java.io.ByteArrayOutputStream()
        var buffer = 0
        var bufferBitCount = 0
        for (character in text.uppercase()) {
            if (character == '=' || character == ' ') continue
            val value = ALPHABET.indexOf(character)
            if (value < 0) return null
            buffer = (buffer shl 5) or value
            bufferBitCount += 5
            if (bufferBitCount >= 8) {
                bytes.write((buffer shr (bufferBitCount - 8)) and 0xFF)
                bufferBitCount -= 8
                buffer = buffer and ((1 shl bufferBitCount) - 1)
            }
        }
        return bytes.toByteArray()
    }
}

/** P-256 arithmetic the JCA does not offer: a public key from a private scalar, a point checked to be on the curve. */
internal object PasswordsEllipticCurve {
    private val parameters: ECParameterSpec = AlgorithmParameters.getInstance("EC").run {
        init(ECGenParameterSpec("secp256r1"))
        getParameterSpec(ECParameterSpec::class.java)
    }
    private val prime: BigInteger = (parameters.curve.field as java.security.spec.ECFieldFp).p
    private val coefficientB: BigInteger = parameters.curve.b
    private val order: BigInteger = parameters.order
    private val generator: ECPoint = parameters.generator

    /** The scalar of a stored 32 byte private key, which must be from 1 up to the group order. */
    fun scalar(privateKey: ByteArray): BigInteger {
        if (privateKey.size != PasswordsCrypto.PRIVATE_KEY_BYTE_COUNT) throw PasswordsCryptoException(PasswordsCryptoFailure.InvalidKeyPair)
        val scalar = BigInteger(1, privateKey)
        if (scalar.signum() == 0 || scalar >= order) throw PasswordsCryptoException(PasswordsCryptoFailure.InvalidKeyPair)
        return scalar
    }

    fun privateKey(privateKey: ByteArray): java.security.PrivateKey = try {
        KeyFactory.getInstance("EC").generatePrivate(ECPrivateKeySpec(scalar(privateKey), parameters))
    } catch (error: PasswordsCryptoException) {
        throw error
    } catch (_: Exception) {
        throw PasswordsCryptoException(PasswordsCryptoFailure.InvalidKeyPair)
    }

    fun publicKey(publicKey: ByteArray): java.security.PublicKey {
        val point = decodePoint(publicKey) ?: throw PasswordsCryptoException(PasswordsCryptoFailure.InvalidKeyPair)
        return try {
            KeyFactory.getInstance("EC").generatePublic(ECPublicKeySpec(point, parameters))
        } catch (_: Exception) {
            throw PasswordsCryptoException(PasswordsCryptoFailure.InvalidKeyPair)
        }
    }

    fun encodePoint(point: ECPoint): ByteArray =
        byteArrayOf(4) + PasswordsCrypto.fixedLength(point.affineX, 32) + PasswordsCrypto.fixedLength(point.affineY, 32)

    /** The point of 65 uncompressed bytes (04, X, Y), or null when it is not a point on the curve. */
    fun decodePoint(bytes: ByteArray): ECPoint? {
        if (bytes.size != PasswordsCrypto.PUBLIC_KEY_BYTE_COUNT || bytes[0] != 4.toByte()) return null
        val x = BigInteger(1, bytes.copyOfRange(1, 33))
        val y = BigInteger(1, bytes.copyOfRange(33, 65))
        if (x >= prime || y >= prime) return null
        val left = y.multiply(y).mod(prime)
        val right = x.pow(3).subtract(x.multiply(BigInteger.valueOf(3))).add(coefficientB).mod(prime)
        return if (left == right) ECPoint(x, y) else null
    }

    /** The generator point times a scalar, by double and add with modular inverses. Used for the occasional public key, not in a loop. */
    fun multiplyBase(scalar: BigInteger): ECPoint {
        var result: ECPoint = ECPoint.POINT_INFINITY
        var addend: ECPoint = generator
        for (bit in 0 until scalar.bitLength()) {
            if (scalar.testBit(bit)) result = add(result, addend)
            addend = add(addend, addend)
        }
        return result
    }

    private fun add(left: ECPoint, right: ECPoint): ECPoint {
        if (left == ECPoint.POINT_INFINITY) return right
        if (right == ECPoint.POINT_INFINITY) return left
        val slope: BigInteger
        if (left.affineX == right.affineX) {
            if (left.affineY != right.affineY || left.affineY.signum() == 0) return ECPoint.POINT_INFINITY
            slope = left.affineX.pow(2).multiply(BigInteger.valueOf(3)).subtract(BigInteger.valueOf(3))
                .multiply(left.affineY.shiftLeft(1).modInverse(prime)).mod(prime)
        } else {
            slope = right.affineY.subtract(left.affineY).multiply(right.affineX.subtract(left.affineX).modInverse(prime)).mod(prime)
        }
        val x = slope.pow(2).subtract(left.affineX).subtract(right.affineX).mod(prime)
        val y = slope.multiply(left.affineX.subtract(x)).subtract(left.affineY).mod(prime)
        return ECPoint(x, y)
    }
}
