package com.teanode.kit.passwords

import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows

/**
 * The encryption format on its own: round trips, every way an open must fail, and values worked out by other
 * implementations (RFC 7914 and RFC 4648). PasswordsVectorsTest checks the bytes against Go.
 */
class PasswordsCryptoTest {
    private fun hex(text: String): ByteArray = text.chunked(2).map { pair -> pair.toInt(16).toByte() }.toByteArray()

    private val key = ByteArray(32) { index -> index.toByte() }
    private val nonce = ByteArray(12) { index -> (100 + index).toByte() }

    private fun failure(block: () -> Unit): PasswordsCryptoFailure = assertThrows<PasswordsCryptoException>(block).failure

    @Test fun `seal then open returns the plaintext`() {
        val sealed = PasswordsCrypto.seal("hello".toByteArray(), key, "account-key")
        assertEquals(1, sealed.version)
        assertEquals(12, sealed.nonce.size)
        assertEquals(5 + 16, sealed.ciphertext.size)
        assertArrayEquals("hello".toByteArray(), PasswordsCrypto.open(sealed, key, "account-key"))
    }

    @Test fun `seal with a given nonce is repeatable`() {
        val first = PasswordsCrypto.seal("hello".toByteArray(), key, "x", nonce)
        val second = PasswordsCrypto.seal("hello".toByteArray(), key, "x", nonce)
        assertEquals(first, second)
        assertArrayEquals(nonce, first.nonce)
    }

    @Test fun `two seals without a nonce use different nonces`() {
        val first = PasswordsCrypto.seal("hello".toByteArray(), key, "x")
        val second = PasswordsCrypto.seal("hello".toByteArray(), key, "x")
        assertTrue(!first.nonce.contentEquals(second.nonce))
    }

    @Test fun `open fails with the wrong key associated data or damage`() {
        val sealed = PasswordsCrypto.seal("hello".toByteArray(), key, "item-key:one")
        assertEquals(PasswordsCryptoFailure.OpenFailed, failure { PasswordsCrypto.open(sealed, ByteArray(32) { 9 }, "item-key:one") })
        assertEquals(PasswordsCryptoFailure.OpenFailed, failure { PasswordsCrypto.open(sealed, key, "item-key:two") })
        val damagedBytes = sealed.ciphertext.copyOf().also { bytes -> bytes[0] = (bytes[0].toInt() xor 1).toByte() }
        assertThrows<PasswordsCryptoException> { PasswordsCrypto.open(PasswordsSealedValue(nonce = sealed.nonce, ciphertext = damagedBytes), key, "item-key:one") }
        val tooShort = PasswordsSealedValue(nonce = sealed.nonce, ciphertext = ByteArray(5))
        assertEquals(PasswordsCryptoFailure.OpenFailed, failure { PasswordsCrypto.open(tooShort, key, "item-key:one") })
    }

    @Test fun `open refuses an unknown version`() {
        val sealed = PasswordsCrypto.seal("hello".toByteArray(), key, "x")
        val other = PasswordsSealedValue(version = 2, nonce = sealed.nonce, ciphertext = sealed.ciphertext)
        assertEquals(PasswordsCryptoFailure.UnsupportedVersion, failure { PasswordsCrypto.open(other, key, "x") })
    }

    @Test fun `seal refuses a key or nonce of the wrong length`() {
        assertEquals(PasswordsCryptoFailure.InvalidKeyLength, failure { PasswordsCrypto.seal(ByteArray(0), ByteArray(16), "x") })
        assertEquals(PasswordsCryptoFailure.InvalidNonceLength, failure { PasswordsCrypto.seal(ByteArray(0), key, "x", ByteArray(8)) })
    }

    @Test fun `an empty plaintext seals and opens`() {
        val sealed = PasswordsCrypto.seal(ByteArray(0), key, "x")
        assertEquals(16, sealed.ciphertext.size)
        assertEquals(0, PasswordsCrypto.open(sealed, key, "x").size)
    }

    @Test fun `sealed value json keeps the documented key order and round trips`() {
        val sealed = PasswordsSealedValue(nonce = byteArrayOf(1, 2, 3), ciphertext = byteArrayOf(4, 5, 6, 7))
        assertEquals("{\"version\":1,\"nonce\":\"AQID\",\"ciphertext\":\"BAUGBw==\"}", sealed.jsonText)
        assertEquals(sealed, PasswordsSealedValue.fromJsonText(sealed.jsonText))
    }

    @Test fun `sealed value json refuses garbage`() {
        assertEquals(PasswordsCryptoFailure.InvalidSealedValue, failure { PasswordsSealedValue.fromJsonText("not json") })
        assertThrows<PasswordsCryptoException> { PasswordsSealedValue.fromJsonText("{\"version\":1,\"nonce\":\"%%%\",\"ciphertext\":\"AA==\"}") }
    }

    @Test fun `passphrase key matches the RFC 7914 vectors`() {
        assertArrayEquals(
            hex("55ac046e56e3089fec1691c22544b605f94185216dde0465e68b9d57c20dacbc"),
            PasswordsCrypto.derivePassphraseKey("passwd", "salt".toByteArray(), 1),
        )
        assertArrayEquals(
            hex("c5e478d59288c841aa530db6845c4c8d962893a001ce4e11a4963873aa98134a"),
            PasswordsCrypto.derivePassphraseKey("password", "salt".toByteArray(), 4096),
        )
    }

    @Test fun `passphrase key normalizes to NFC`() {
        val decomposed = "café"
        val composed = "café"
        assertTrue(!decomposed.toByteArray().contentEquals(composed.toByteArray()))
        val salt = ByteArray(16) { 7 }
        val count = PasswordsCrypto.MINIMUM_PASSPHRASE_ITERATION_COUNT
        assertArrayEquals(PasswordsCrypto.passphraseKey(decomposed, salt, count), PasswordsCrypto.passphraseKey(composed, salt, count))
    }

    @Test fun `passphrase key depends on salt and count`() {
        val count = PasswordsCrypto.MINIMUM_PASSPHRASE_ITERATION_COUNT
        val first = PasswordsCrypto.passphraseKey("a", ByteArray(16) { 1 }, count)
        val otherSalt = PasswordsCrypto.passphraseKey("a", ByteArray(16) { 2 }, count)
        val otherCount = PasswordsCrypto.passphraseKey("a", ByteArray(16) { 1 }, count + 1)
        assertEquals(32, first.size)
        assertTrue(!first.contentEquals(otherSalt))
        assertTrue(!first.contentEquals(otherCount))
    }

    @Test fun `passphrase key refuses a count outside the bounds`() {
        for (count in listOf(0, 1, 599_999, -5)) {
            assertEquals(PasswordsCryptoFailure.IterationCountTooLow, failure { PasswordsCrypto.passphraseKey("a", ByteArray(0), count) })
        }
        for (count in listOf(10_000_001, Int.MAX_VALUE)) {
            assertEquals(PasswordsCryptoFailure.IterationCountTooHigh, failure { PasswordsCrypto.passphraseKey("a", ByteArray(0), count) })
        }
        assertEquals(12, PasswordsCrypto.MINIMUM_PASSPHRASE_LENGTH)
    }

    @Test fun `wrap and proof keys differ and the proof hash is SHA-256`() {
        val wrap = PasswordsCrypto.wrapKey(key)
        val proof = PasswordsCrypto.proof(key)
        assertEquals(32, wrap.size)
        assertEquals(32, proof.size)
        assertTrue(!wrap.contentEquals(proof))
        assertTrue(!wrap.contentEquals(key))
        assertArrayEquals(wrap, PasswordsCrypto.wrapKey(key))
        assertArrayEquals(hex("66687aadf862bd776c8fc18b8e9f8e20089714856ee233b3902a591d0d5f2925"), PasswordsCrypto.proofHash(ByteArray(32)))
        assertThrows<PasswordsCryptoException> { PasswordsCrypto.wrapKey(ByteArray(5)) }
    }

    @Test fun `base32 matches RFC 4648`() {
        val vectors = listOf("" to "", "f" to "MY", "fo" to "MZXQ", "foo" to "MZXW6", "foob" to "MZXW6YQ", "fooba" to "MZXW6YTB", "foobar" to "MZXW6YTBOI")
        for ((text, expected) in vectors) {
            assertEquals(expected, PasswordsBase32.encode(text.toByteArray()))
            assertArrayEquals(text.toByteArray(), PasswordsBase32.decode(expected))
        }
        assertArrayEquals("foobar".toByteArray(), PasswordsBase32.decode("mzxw6ytboi======"))
        assertNull(PasswordsBase32.decode("MZXW1"))
    }

    @Test fun `a recovery key is thirteen groups of four and reads back`() {
        val bytes = PasswordsCrypto.makeRecoveryKey()
        assertEquals(32, bytes.size)
        val text = PasswordsCrypto.formatRecoveryKey(bytes)
        val groups = text.split(" ")
        assertEquals(13, groups.size)
        assertTrue(groups.all { group -> group.length == 4 })
        assertArrayEquals(bytes, PasswordsCrypto.parseRecoveryKey(text))
        assertArrayEquals(bytes, PasswordsCrypto.parseRecoveryKey(text.lowercase().replace(" ", "-")))
        assertArrayEquals(bytes, PasswordsCrypto.parseRecoveryKey("  $text\n"))
    }

    @Test fun `a recovery key refuses the wrong length or letters`() {
        assertThrows<PasswordsCryptoException> { PasswordsCrypto.formatRecoveryKey(ByteArray(31)) }
        assertEquals(PasswordsCryptoFailure.InvalidRecoveryKey, failure { PasswordsCrypto.parseRecoveryKey("MZXW 6YTB") })
        val valid = PasswordsCrypto.formatRecoveryKey(ByteArray(32))
        assertThrows<PasswordsCryptoException> { PasswordsCrypto.parseRecoveryKey(valid.dropLast(1) + "1") }
    }

    @Test fun `a recovery key refuses padding non ASCII and non zero trailing bits`() {
        val valid = PasswordsCrypto.formatRecoveryKey(ByteArray(32))
        assertArrayEquals(ByteArray(32), PasswordsCrypto.parseRecoveryKey(valid))
        val letters = valid.replace(" ", "")
        assertThrows<PasswordsCryptoException> { PasswordsCrypto.parseRecoveryKey("$letters====") }
        assertThrows<PasswordsCryptoException> { PasswordsCrypto.parseRecoveryKey(letters.dropLast(1) + "B") }
        assertThrows<PasswordsCryptoException> { PasswordsCrypto.parseRecoveryKey("Ａ" + letters.drop(1)) }
        assertThrows<PasswordsCryptoException> { PasswordsCrypto.parseRecoveryKey("ß" + letters.drop(1)) }
        assertThrows<PasswordsCryptoException> { PasswordsCrypto.parseRecoveryKey(letters.take(4) + " " + letters.drop(4)) }
        assertThrows<PasswordsCryptoException> { PasswordsCrypto.parseRecoveryKey(letters.take(4) + "\u2013" + letters.drop(4)) }
    }

    @Test fun `a key pair has a 32 byte scalar and a 65 byte point`() {
        val keyPair = PasswordsCrypto.makeKeyPair()
        assertEquals(32, keyPair.privateKey.size)
        assertEquals(65, keyPair.publicKey.size)
        assertEquals(4, keyPair.publicKey[0].toInt())
        assertEquals(keyPair, PasswordsCrypto.keyPair(keyPair.privateKey))
        assertEquals(PasswordsCryptoFailure.InvalidKeyPair, failure { PasswordsCrypto.keyPair(ByteArray(32)) })
    }

    @Test fun `a safety code is eight groups of four digits from the key alone and stable`() {
        val keyPair = PasswordsCrypto.makeKeyPair()
        val code = PasswordsCrypto.safetyCode(keyPair.publicKey)
        assertEquals(code, PasswordsCrypto.safetyCode(keyPair.publicKey))
        val groups = code.split(" ")
        assertEquals(8, groups.size)
        assertTrue(groups.all { group -> group.length == 4 && group.all { character -> character.isDigit() } })
        assertNotEquals(code, PasswordsCrypto.safetyCode(PasswordsCrypto.makeKeyPair().publicKey))
        assertThrows<PasswordsCryptoException> { PasswordsCrypto.safetyCode(ByteArray(64)) }
    }

    @Test fun `a typed safety code is read with or without spaces and other text is refused`() {
        val code = "0123 4567 8901 2345 6789 0123 4567 8901"
        assertEquals(code, PasswordsCrypto.normalizeSafetyCode(code))
        assertEquals(code, PasswordsCrypto.normalizeSafetyCode(code.replace(" ", "")))
        assertEquals(code, PasswordsCrypto.normalizeSafetyCode("  $code\n"))
        assertEquals(code, PasswordsCrypto.normalizeSafetyCode("0123 " + "4567 89012345 67890123 4567 8901"))
        for (text in listOf("", "0123 4567 8901 2345 6789", "$code 2", code.replace(" ", "-"), code.dropLast(1) + "x", "٣" + code.drop(1))) {
            assertNull(PasswordsCrypto.normalizeSafetyCode(text), text)
        }
    }

    @Test fun `sealed for a key pair opens only for that owner recipient and item`() {
        val owner = PasswordsCrypto.makeKeyPair()
        val recipient = PasswordsCrypto.makeKeyPair()
        val itemKey = PasswordsCrypto.makeKey()
        val sealed = PasswordsCrypto.sealForKeyPair(itemKey, owner, "user-owner", recipient.publicKey, "user-1", "item-1")
        assertEquals(65, sealed.ephemeralPublicKey.size)
        fun open(
            ownerPublicKey: ByteArray = owner.publicKey,
            ownerId: String = "user-owner",
            recipientId: String = "user-1",
            itemId: String = "item-1",
            recipientPrivateKey: ByteArray = recipient.privateKey,
        ): ByteArray = PasswordsCrypto.openForKeyPair(sealed, recipientPrivateKey, ownerPublicKey, ownerId, recipientId, itemId)
        assertArrayEquals(itemKey, open())
        assertThrows<PasswordsCryptoException> { open(ownerPublicKey = PasswordsCrypto.makeKeyPair().publicKey) }
        assertThrows<PasswordsCryptoException> { open(ownerPublicKey = ByteArray(65)) }
        assertThrows<PasswordsCryptoException> { open(ownerId = "user-other") }
        assertThrows<PasswordsCryptoException> { open(recipientId = "user-2") }
        assertThrows<PasswordsCryptoException> { open(itemId = "item-2") }
        assertThrows<PasswordsCryptoException> { open(recipientPrivateKey = PasswordsCrypto.makeKeyPair().privateKey) }
    }

    @Test fun `sealed for the agent uses the agent recipient id and the owners key`() {
        assertEquals("agent", PasswordsCrypto.AGENT_RECIPIENT_ID)
        val owner = PasswordsCrypto.makeKeyPair()
        val agent = PasswordsCrypto.makeKeyPair()
        val itemKey = PasswordsCrypto.makeKey()
        val sealed = PasswordsCrypto.sealForKeyPair(itemKey, owner, "user-owner", agent.publicKey, PasswordsCrypto.AGENT_RECIPIENT_ID, "item-1")
        assertArrayEquals(
            itemKey,
            PasswordsCrypto.openForKeyPair(sealed, agent.privateKey, owner.publicKey, "user-owner", PasswordsCrypto.AGENT_RECIPIENT_ID, "item-1"),
        )
    }

    @Test fun `sealed for a key pair with a given ephemeral key and nonce is repeatable`() {
        val owner = PasswordsCrypto.makeKeyPair()
        val recipient = PasswordsCrypto.makeKeyPair()
        val ephemeral = PasswordsCrypto.makeKeyPair()
        val first = PasswordsCrypto.sealForKeyPair(key, owner, "o", recipient.publicKey, "u", "i", ephemeral.privateKey, nonce)
        val second = PasswordsCrypto.sealForKeyPair(key, owner, "o", recipient.publicKey, "u", "i", ephemeral.privateKey, nonce)
        assertEquals(first, second)
        assertArrayEquals(ephemeral.publicKey, first.ephemeralPublicKey)
    }

    @Test fun `key pair sealed value json keeps the documented key order and round trips`() {
        val sealed = PasswordsKeyPairSealedValue(ephemeralPublicKey = byteArrayOf(9), nonce = byteArrayOf(1, 2, 3), ciphertext = byteArrayOf(4, 5, 6, 7))
        assertEquals("{\"version\":1,\"ephemeralPublicKey\":\"CQ==\",\"nonce\":\"AQID\",\"ciphertext\":\"BAUGBw==\"}", sealed.jsonText)
        assertEquals(sealed, PasswordsKeyPairSealedValue.fromJsonText(sealed.jsonText))
    }

    @Test fun `associated data is exactly what the plan says`() {
        assertEquals("account-key", PasswordsCrypto.ACCOUNT_KEY_ASSOCIATED_DATA)
        assertEquals("private-key", PasswordsCrypto.PRIVATE_KEY_ASSOCIATED_DATA)
        assertEquals("item-key:abc", PasswordsCrypto.itemKeyAssociatedData("abc"))
        assertEquals("item:abc:login", PasswordsCrypto.itemFieldsAssociatedData("abc", PasswordsItemKind.Login))
        assertEquals("item:abc:card", PasswordsCrypto.itemFieldsAssociatedData("abc", PasswordsItemKind.Card))
        assertEquals("item:abc:note", PasswordsCrypto.itemFieldsAssociatedData("abc", PasswordsItemKind.Note))
        assertEquals("share:abc:own:agent", PasswordsCrypto.shareAssociatedData("abc", "own", "agent"))
    }

    @Test fun `item fields round trip for every kind`() {
        val login = PasswordsItemFields.Login(
            PasswordsLoginFields(
                name = "Example", userName = "ada@example.com", password = "p4ss/w0rd", sites = listOf("https://example.com/sign-in"),
                oneTimeCodeUri = "otpauth://totp/Example:ada?secret=JBSWY3DPEHPK3PXP", notes = "line one\nline two",
                passkeys = listOf(PasswordsPasskeyFields(credentialId = "AAEC", relyingPartyId = "example.com", createdAt = "2026-10-20T10:00:00Z")),
                folder = "Work",
            ),
        )
        val card = PasswordsItemFields.Card(PasswordsCardFields(name = "Card", number = "4111111111111111", expiryMonth = "04", expiryYear = "2030"))
        val note = PasswordsItemFields.Note(PasswordsNoteFields(name = "Note", text = "Dinner at 7"))
        for (fields in listOf(login, card, note)) {
            val sealed = PasswordsCrypto.sealItemFields(fields, key, "item-1")
            assertEquals(fields, PasswordsCrypto.openItemFields(sealed, key, "item-1", fields.itemKind))
        }
    }

    @Test fun `item fields do not open as another kind or item`() {
        val sealed = PasswordsCrypto.sealItemFields(PasswordsItemFields.Note(PasswordsNoteFields(name = "N")), key, "item-1")
        assertThrows<PasswordsCryptoException> { PasswordsCrypto.openItemFields(sealed, key, "item-1", PasswordsItemKind.Login) }
        assertThrows<PasswordsCryptoException> { PasswordsCrypto.openItemFields(sealed, key, "item-2", PasswordsItemKind.Note) }
    }

    @Test fun `item fields use the names the plan fixes`() {
        val text = String(PasswordsItemFields.Login(PasswordsLoginFields(name = "A", sites = listOf("https://a.example/x"))).jsonBytes())
        assertEquals(
            "{\"folder\":\"\",\"name\":\"A\",\"notes\":\"\",\"oneTimeCodeUri\":\"\",\"passkeys\":[],\"password\":\"\"," +
                "\"sites\":[\"https://a.example/x\"],\"userName\":\"\"}",
            text,
        )
        val passkey = PasswordsPasskeyFields().toJson().keys
        assertEquals(setOf("credentialId", "relyingPartyId", "userHandle", "userName", "userDisplayName", "privateKey", "createdAt"), passkey)
        val card = PasswordsCardFields().toJson().keys
        assertEquals(setOf("name", "cardholderName", "number", "expiryMonth", "expiryYear", "securityCode", "brand", "notes", "folder"), card)
    }

    @Test fun `item fields fill a missing key with an empty value`() {
        val fields = PasswordsItemFields.fromJsonBytes(PasswordsItemKind.Login, "{\"name\":\"A\",\"password\":\"x\"}".toByteArray())
        assertEquals(PasswordsItemFields.Login(PasswordsLoginFields(name = "A", password = "x")), fields)
        assertEquals(PasswordsItemFields.Note(PasswordsNoteFields()), PasswordsItemFields.fromJsonBytes(PasswordsItemKind.Note, "{}".toByteArray()))
    }
}
