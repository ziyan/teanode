package com.teanode.kit.passwords

import java.security.MessageDigest
import java.security.Signature

// A passkey authenticator that holds its keys in an item's fields. ES256 only (ECDSA over P-256 with SHA-256, COSE
// algorithm -7). Everything is written to match the Go package internal/passwords and web/src/passwords/ byte for byte
// (docs/planning/passwords-execplan.md, "Passkeys"); the CBOR is written by hand because the pieces are small and fixed.
//
// A provider can only hold synced passkeys, so the flags say backup eligible and backed up, the sign count is always 0,
// and the attestation is "none".

/** Why a passkey call failed. */
enum class PasswordsPasskeyFailure {
    /** The private key is not ES256 PKCS#8 (or SEC1) DER. */
    InvalidPrivateKey,

    /** A public key coordinate was not 32 bytes, or is not a point on the curve. */
    InvalidPublicKey,

    /** A credential id that does not fit the 2 byte length in the authenticator data. */
    InvalidCredentialId,

    /** A signature that is not DER ECDSA. */
    InvalidSignature,
}

class PasswordsPasskeyException(val failure: PasswordsPasskeyFailure) : Exception(failure.name)

/** Everything a new passkey produces: the private key to keep in the login, and what the website is sent. */
class PasswordsPasskeyRegistration(
    /** The private key as PKCS#8 DER, to keep (as base64) in the login's passkeys. */
    val privateKey: ByteArray,
    val credentialId: ByteArray,
    val publicKeyX: ByteArray,
    val publicKeyY: ByteArray,
    /** The public key as a COSE key, CBOR encoded. */
    val cosePublicKey: ByteArray,
    val authenticatorData: ByteArray,
    /** The attestation object, CBOR encoded, with format "none". */
    val attestationObject: ByteArray,
) {
    override fun equals(other: Any?): Boolean =
        other is PasswordsPasskeyRegistration && privateKey.contentEquals(other.privateKey) && credentialId.contentEquals(other.credentialId) &&
            publicKeyX.contentEquals(other.publicKeyX) && publicKeyY.contentEquals(other.publicKeyY) &&
            cosePublicKey.contentEquals(other.cosePublicKey) && authenticatorData.contentEquals(other.authenticatorData) &&
            attestationObject.contentEquals(other.attestationObject)

    override fun hashCode(): Int = privateKey.contentHashCode() * 31 + credentialId.contentHashCode()
}

/** The passkey authenticator as functions. */
object PasswordsPasskey {
    /** COSE algorithm identifier of ES256. */
    const val COSE_ALGORITHM = -7

    /** TeaNode's authenticator id: the ASCII of "TEANODEPASSWORDS". */
    val aaguid: ByteArray = "TEANODEPASSWORDS".toByteArray(Charsets.US_ASCII)

    const val CREDENTIAL_ID_BYTE_COUNT = 16

    // Flag bits of the authenticator data.
    private const val USER_PRESENT_FLAG = 0x01
    private const val USER_VERIFIED_FLAG = 0x04
    private const val BACKUP_ELIGIBLE_FLAG = 0x08
    private const val BACKED_UP_FLAG = 0x10
    private const val ATTESTED_CREDENTIAL_DATA_FLAG = 0x40

    private fun fail(failure: PasswordsPasskeyFailure): Nothing = throw PasswordsPasskeyException(failure)

    // Keys

    /** A new passkey private key, as PKCS#8 DER. */
    fun makePrivateKey(): ByteArray {
        val pair = PasswordsCrypto.makeKeyPair()
        return pkcs8(pair.privateKey, pair.publicKey)
    }

    /** A new random credential id of 16 bytes. */
    fun makeCredentialId(): ByteArray = PasswordsCrypto.randomBytes(CREDENTIAL_ID_BYTE_COUNT)

    /** The public key's coordinates, 32 bytes each, from a private key. */
    fun publicKeyCoordinates(privateKey: ByteArray): Pair<ByteArray, ByteArray> {
        val point = publicPoint(scalarOf(privateKey))
        return point.copyOfRange(1, 33) to point.copyOfRange(33, 65)
    }

    private fun scalarOf(privateKey: ByteArray): ByteArray = scalarFromDer(privateKey) ?: fail(PasswordsPasskeyFailure.InvalidPrivateKey)

    private fun publicPoint(scalar: ByteArray): ByteArray = try {
        PasswordsCrypto.keyPair(scalar).publicKey
    } catch (_: PasswordsCryptoException) {
        fail(PasswordsPasskeyFailure.InvalidPrivateKey)
    }

    // Registration

    /** The COSE public key: the CBOR map `{1: 2, 3: -7, -1: 1, -2: X, -3: Y}` with its keys in the order 1, 3, -1, -2, -3. */
    fun cosePublicKey(publicKeyX: ByteArray, publicKeyY: ByteArray): ByteArray {
        if (publicKeyX.size != 32 || publicKeyY.size != 32) fail(PasswordsPasskeyFailure.InvalidPublicKey)
        return byteArrayOf(0xA5.toByte()) + // map of 5
            byteArrayOf(0x01, 0x02) + // kty: EC2
            byteArrayOf(0x03, 0x26) + // alg: -7
            byteArrayOf(0x20, 0x01) + // crv: P-256
            byteArrayOf(0x21, 0x58, 0x20) + publicKeyX + // x: byte string of 32
            byteArrayOf(0x22, 0x58, 0x20) + publicKeyY // y: byte string of 32
    }

    /**
     * The authenticator data of a registration: SHA-256 of the relying party id, flags, sign count 0, the AAGUID, the
     * credential id (with a 2 byte length) and the COSE public key. Flags are 0x5D (user present, verified, backup
     * eligible, backed up, attested data) when the person was verified, else 0x59.
     */
    fun registrationAuthenticatorData(
        relyingPartyId: String,
        credentialId: ByteArray,
        publicKeyX: ByteArray,
        publicKeyY: ByteArray,
        isVerified: Boolean,
    ): ByteArray {
        if (credentialId.isEmpty() || credentialId.size > 1023) fail(PasswordsPasskeyFailure.InvalidCredentialId)
        var flags = USER_PRESENT_FLAG or BACKUP_ELIGIBLE_FLAG or BACKED_UP_FLAG or ATTESTED_CREDENTIAL_DATA_FLAG
        if (isVerified) flags = flags or USER_VERIFIED_FLAG
        return relyingPartyHash(relyingPartyId) + byteArrayOf(flags.toByte()) + byteArrayOf(0, 0, 0, 0) + aaguid +
            byteArrayOf((credentialId.size shr 8).toByte(), (credentialId.size and 0xFF).toByte()) + credentialId +
            cosePublicKey(publicKeyX, publicKeyY)
    }

    /** The attestation object: the CBOR map `{"fmt": "none", "attStmt": {}, "authData": <bytes>}` in that key order. */
    fun attestationObject(authenticatorData: ByteArray): ByteArray =
        byteArrayOf(0xA3.toByte()) + cborText("fmt") + cborText("none") + cborText("attStmt") + byteArrayOf(0xA0.toByte()) +
            cborText("authData") + cborBytesHeader(authenticatorData.size) + authenticatorData

    /** Makes a passkey for a relying party. Pass `privateKey` and `credentialId` only in tests. */
    fun register(
        relyingPartyId: String,
        isVerified: Boolean,
        privateKey: ByteArray? = null,
        credentialId: ByteArray? = null,
    ): PasswordsPasskeyRegistration {
        val key = privateKey ?: makePrivateKey()
        val identifier = credentialId ?: makeCredentialId()
        val (x, y) = publicKeyCoordinates(key)
        val authenticatorData = registrationAuthenticatorData(relyingPartyId, identifier, x, y, isVerified)
        return PasswordsPasskeyRegistration(key, identifier, x, y, cosePublicKey(x, y), authenticatorData, attestationObject(authenticatorData))
    }

    // Sign-in

    /** The authenticator data of a sign-in: SHA-256 of the relying party id, flags (0x1D verified, 0x19 not) and sign count 0. */
    fun assertionAuthenticatorData(relyingPartyId: String, isVerified: Boolean): ByteArray {
        var flags = USER_PRESENT_FLAG or BACKUP_ELIGIBLE_FLAG or BACKED_UP_FLAG
        if (isVerified) flags = flags or USER_VERIFIED_FLAG
        return relyingPartyHash(relyingPartyId) + byteArrayOf(flags.toByte()) + byteArrayOf(0, 0, 0, 0)
    }

    /**
     * The sign-in signature: ECDSA P-256 over SHA-256 of the authenticator data followed by the client data hash, DER
     * encoded. ECDSA signatures are randomized, so two calls differ; both verify.
     */
    fun sign(privateKey: ByteArray, authenticatorData: ByteArray, clientDataHash: ByteArray): ByteArray {
        val signer = Signature.getInstance("SHA256withECDSA")
        signer.initSign(PasswordsEllipticCurve.privateKey(scalarOf(privateKey)))
        signer.update(authenticatorData + clientDataHash)
        return signer.sign()
    }

    /** Checks a sign-in signature against a public key. */
    fun isSignatureValid(signature: ByteArray, publicKeyX: ByteArray, publicKeyY: ByteArray, authenticatorData: ByteArray, clientDataHash: ByteArray): Boolean {
        if (publicKeyX.size != 32 || publicKeyY.size != 32) fail(PasswordsPasskeyFailure.InvalidPublicKey)
        val publicKey = try {
            PasswordsEllipticCurve.publicKey(byteArrayOf(4) + publicKeyX + publicKeyY)
        } catch (_: PasswordsCryptoException) {
            fail(PasswordsPasskeyFailure.InvalidPublicKey)
        }
        if (!isDerSignature(signature)) fail(PasswordsPasskeyFailure.InvalidSignature)
        return try {
            val verifier = Signature.getInstance("SHA256withECDSA")
            verifier.initVerify(publicKey)
            verifier.update(authenticatorData + clientDataHash)
            verifier.verify(signature)
        } catch (_: java.security.SignatureException) {
            false
        }
    }

    /** Whether the bytes are a DER SEQUENCE of two INTEGERs, which is what an ECDSA signature is. */
    private fun isDerSignature(signature: ByteArray): Boolean {
        val outer = DerReader(signature)
        val sequence = outer.readNested(0x30) ?: return false
        if (!outer.isAtEnd) return false
        return sequence.readContents(0x02) != null && sequence.readContents(0x02) != null && sequence.isAtEnd
    }

    // Pieces

    private fun relyingPartyHash(relyingPartyId: String): ByteArray =
        MessageDigest.getInstance("SHA-256").digest(relyingPartyId.toByteArray(Charsets.UTF_8))

    private fun cborText(text: String): ByteArray {
        // Every text here is shorter than 24 bytes, so the length is in the first byte.
        val bytes = text.toByteArray(Charsets.UTF_8)
        return byteArrayOf((0x60 or bytes.size).toByte()) + bytes
    }

    private fun cborBytesHeader(count: Int): ByteArray = when {
        count < 24 -> byteArrayOf((0x40 or count).toByte())
        count < 256 -> byteArrayOf(0x58, count.toByte())
        else -> byteArrayOf(0x59, (count shr 8).toByte(), (count and 0xFF).toByte())
    }

    // The private key travels as PKCS#8 DER in the exact shape Go's x509.MarshalPKCS8PrivateKey writes for P-256 (138
    // bytes: the algorithm identifiers, then an ECPrivateKey holding the scalar and the public point). It is written and
    // read here by hand so that no platform's own DER output matters.
    private val pkcs8Prefix = intArrayOf(
        0x30, 0x81, 0x87, 0x02, 0x01, 0x00, 0x30, 0x13, 0x06, 0x07, 0x2A, 0x86, 0x48, 0xCE, 0x3D, 0x02, 0x01, 0x06, 0x08, 0x2A, 0x86,
        0x48, 0xCE, 0x3D, 0x03, 0x01, 0x07, 0x04, 0x6D, 0x30, 0x6B, 0x02, 0x01, 0x01, 0x04, 0x20,
    ).map { value -> value.toByte() }.toByteArray()
    private val pkcs8PublicKeyPrefix = intArrayOf(0xA1, 0x44, 0x03, 0x42, 0x00).map { value -> value.toByte() }.toByteArray()

    internal fun pkcs8(scalar: ByteArray, point: ByteArray): ByteArray = pkcs8Prefix + scalar + pkcs8PublicKeyPrefix + point

    /** The 32 byte scalar inside PKCS#8 (or bare SEC1) ECDSA P-256 DER, or null. */
    internal fun scalarFromDer(der: ByteArray): ByteArray? {
        val outer = DerReader(der)
        val sequence = outer.readNested(0x30) ?: return null
        if (!outer.isAtEnd) return null
        val version = sequence.readContents(0x02)
        if (version == null || version.size != 1) return null
        // SEC1 ECPrivateKey: the scalar follows the version.
        if (version[0] == 1.toByte()) return privateKeyScalar(sequence)
        if (version[0] != 0.toByte()) return null
        // PKCS#8: AlgorithmIdentifier { id-ecPublicKey, prime256v1 }, then the ECPrivateKey in an OCTET STRING.
        val algorithm = sequence.readNested(0x30) ?: return null
        val keyType = algorithm.readContents(0x06) ?: return null
        val curve = algorithm.readContents(0x06) ?: return null
        if (!keyType.contentEquals(byteArrayOf(0x2A, 0x86.toByte(), 0x48, 0xCE.toByte(), 0x3D, 0x02, 0x01)) ||
            !curve.contentEquals(byteArrayOf(0x2A, 0x86.toByte(), 0x48, 0xCE.toByte(), 0x3D, 0x03, 0x01, 0x07))
        ) {
            return null
        }
        val wrapper = sequence.readNested(0x04) ?: return null
        val privateKey = wrapper.readNested(0x30) ?: return null
        val innerVersion = privateKey.readContents(0x02) ?: return null
        if (!innerVersion.contentEquals(byteArrayOf(1))) return null
        return privateKeyScalar(privateKey)
    }

    private fun privateKeyScalar(sequence: DerReader): ByteArray? {
        val scalar = sequence.readContents(0x04) ?: return null
        return if (scalar.size == 32) scalar else null
    }
}

/** Walks a DER byte string one element at a time. It reads only the definite lengths ASN.1 keys use, and gives up (null) on anything else. */
private class DerReader(private val bytes: ByteArray) {
    private var position = 0

    val isAtEnd: Boolean get() = position >= bytes.size

    /** Reads the next element if it has the tag; its contents are returned as the bytes. */
    fun readContents(tag: Int): ByteArray? {
        if (position + 2 > bytes.size || (bytes[position].toInt() and 0xFF) != tag) return null
        var length = bytes[position + 1].toInt() and 0xFF
        var contentStart = position + 2
        if (length and 0x80 != 0) {
            val lengthByteCount = length and 0x7F
            if (lengthByteCount < 1 || lengthByteCount > 2 || contentStart + lengthByteCount > bytes.size) return null
            length = 0
            for (index in 0 until lengthByteCount) length = (length shl 8) or (bytes[contentStart + index].toInt() and 0xFF)
            contentStart += lengthByteCount
        }
        if (contentStart + length > bytes.size) return null
        position = contentStart + length
        return bytes.copyOfRange(contentStart, contentStart + length)
    }

    /** Reads the next element if it has the tag, as a reader over its contents. */
    fun readNested(tag: Int): DerReader? = readContents(tag)?.let { contents -> DerReader(contents) }
}
