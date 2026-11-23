package com.teanode.kit.passwords

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows

/**
 * These cover the access record sealed inside every item's fields: that it survives a decode, an edit and an encode, that
 * any other shape makes the item unreadable, how it is signed and verified, and the helpers the screens call.
 */
class PasswordsAccessTest {
    private val access = PasswordsItemAccess(
        isUsableByAgent = true,
        grantees = listOf(PasswordsGrantee.Person("user-ben"), PasswordsGrantee.Circle("circle-1")),
    )

    private fun jsonObject(fields: PasswordsItemFields): JsonObject = Json.parseToJsonElement(String(fields.jsonBytes(), Charsets.UTF_8)) as JsonObject

    private fun accessFromText(text: String): PasswordsItemAccess = PasswordsItemAccess.fromJson(Json.parseToJsonElement(text))

    private val allKinds = PasswordsItemKind.entries

    @Test fun `the record is written as the contract says`() {
        assertEquals("""{"grantees":[{"userId":"user-ben"},{"circleId":"circle-1"}],"isUsableByAgent":true}""", sortedJsonText(access.toJson()))
    }

    @Test fun `no record is left out of the JSON`() {
        for (fields in listOf(
            PasswordsItemFields.Login(PasswordsLoginFields()),
            PasswordsItemFields.Card(PasswordsCardFields()),
            PasswordsItemFields.Note(PasswordsNoteFields()),
        )) {
            assertNull(jsonObject(fields)["access"], fields.itemKind.rawValue)
        }
    }

    @Test fun `the record survives decode edit encode in every kind`() {
        val originals = listOf(
            PasswordsItemFields.Login(PasswordsLoginFields(name = "Example", access = access)),
            PasswordsItemFields.Card(PasswordsCardFields(name = "Example", access = access)),
            PasswordsItemFields.Note(PasswordsNoteFields(name = "Example", access = access)),
        )
        for (original in originals) {
            val decoded = PasswordsItemFields.fromJsonBytes(original.itemKind, original.jsonBytes())
            assertEquals(access, decoded.access)
            val edited = when (decoded) {
                is PasswordsItemFields.Login -> PasswordsItemFields.Login(decoded.fields.copy(notes = "edited"))
                is PasswordsItemFields.Card -> PasswordsItemFields.Card(decoded.fields.copy(notes = "edited"))
                is PasswordsItemFields.Note -> PasswordsItemFields.Note(decoded.fields.copy(text = "edited"))
            }
            val again = PasswordsItemFields.fromJsonBytes(original.itemKind, edited.jsonBytes())
            assertEquals(access, again.access, original.itemKind.rawValue)
        }
    }

    @Test fun `with access replaces only the record`() {
        val login = PasswordsItemFields.Login(PasswordsLoginFields(name = "Example", password = "secret"))
        assertNull(login.access)
        val shared = login.withAccess(access)
        assertEquals(access, shared.access)
        assertEquals(login, shared.withAccess(null))
        assertEquals(access, PasswordsItemFields.Note(PasswordsNoteFields(name = "n")).withAccess(access).access)
        assertEquals(access, PasswordsItemFields.Card(PasswordsCardFields(name = "c")).withAccess(access).access)
    }

    @Test fun `null or missing is no record`() {
        for (json in listOf("""{"name":"Example"}""", """{"name":"Example","access":null}""")) {
            val fields = PasswordsItemFields.fromJsonBytes(PasswordsItemKind.Note, json.toByteArray())
            assertNull(fields.access, json)
        }
    }

    @Test fun `any other shape makes the item unreadable`() {
        val wrong = listOf(
            "\"yes\"", "5", "true", "[]", "{}",
            """{"isUsableByAgent":true}""",
            """{"grantees":[]}""",
            """{"isUsableByAgent":"true","grantees":[]}""",
            """{"isUsableByAgent":true,"grantees":{}}""",
            """{"isUsableByAgent":true,"grantees":null}""",
            """{"isUsableByAgent":true,"grantees":["user-ben"]}""",
            """{"isUsableByAgent":true,"grantees":[null]}""",
            """{"isUsableByAgent":true,"grantees":[{}]}""",
            """{"isUsableByAgent":true,"grantees":[{"userId":5}]}""",
            """{"isUsableByAgent":true,"grantees":[{"userId":""}]}""",
            """{"isUsableByAgent":true,"grantees":[{"circleId":null}]}""",
            // A null beside the other id is still two keys, as in Go and the dashboard.
            """{"isUsableByAgent":true,"grantees":[{"userId":"user-ben","circleId":null}]}""",
            """{"isUsableByAgent":true,"grantees":[{"userId":null,"circleId":"circle-1"}]}""",
            """{"isUsableByAgent":true,"grantees":[{"userId":"user-ben","circleId":"circle-1"}]}""",
            """{"isUsableByAgent":true,"grantees":[{"groupId":"group-1"}]}""",
            """{"isUsableByAgent":true,"grantees":[{"userId":"user-ben","extra":true}]}""",
            """{"isUsableByAgent":true,"grantees":[],"extra":true}""",
            """{"isUsableByAgent":true,"grantees":[],"recordVersion":0}""",
            """{"isUsableByAgent":true,"grantees":[],"recordVersion":-1}""",
            """{"isUsableByAgent":true,"grantees":[],"recordVersion":1.5}""",
            """{"isUsableByAgent":true,"grantees":[],"recordVersion":"1"}""",
            """{"isUsableByAgent":true,"grantees":[],"recordVersion":null}""",
            """{"isUsableByAgent":true,"grantees":[],"recordVersion":9007199254740992}""",
            """{"isUsableByAgent":true,"grantees":[],"recordMac":null}""",
            """{"isUsableByAgent":true,"grantees":[],"recordMac":5}""",
            """{"isUsableByAgent":true,"grantees":[],"recordMac":""}""",
            // No padding, unused bits that are not zero, and 24 and 33 bytes.
            """{"isUsableByAgent":true,"grantees":[],"recordMac":"EBESExQVFhcYGRobHB0eHyAhIiMkJSYnKCkqKywtLi8"}""",
            """{"isUsableByAgent":true,"grantees":[],"recordMac":"EBESExQVFhcYGRobHB0eHyAhIiMkJSYnKCkqKywtLi9="}""",
            """{"isUsableByAgent":true,"grantees":[],"recordMac":"EBESExQVFhcYGRobHB0eHyAhIiMkJSYn"}""",
            """{"isUsableByAgent":true,"grantees":[],"recordMac":"EBESExQVFhcYGRobHB0eHyAhIiMkJSYnKCkqKywtLi8v"}""",
        )
        for (record in wrong) {
            val json = """{"name":"Example","access":$record}"""
            for (kind in allKinds) {
                assertThrows<Exception>("$kind: $record") { PasswordsItemFields.fromJsonBytes(kind, json.toByteArray()) }
            }
        }
    }

    @Test fun `a recipient is reached by a person or a circle grantee`() {
        assertTrue(PasswordsItemAccess.isReachedByAccess(access, "user-ben", emptyList()))
        assertTrue(PasswordsItemAccess.isReachedByAccess(access, "user-cy", listOf("circle-2", "circle-1")))
        assertFalse(PasswordsItemAccess.isReachedByAccess(access, "user-cy", listOf("circle-2")))
        assertFalse(PasswordsItemAccess.isReachedByAccess(access, "circle-1", emptyList()))
        assertFalse(PasswordsItemAccess.isReachedByAccess(access, "", listOf("circle-1")))
        assertFalse(PasswordsItemAccess.isReachedByAccess(null, "user-ben", listOf("circle-1")))
        assertFalse(PasswordsItemAccess.isReachedByAccess(PasswordsItemAccess(), "user-ben", listOf("circle-1")))
    }

    @Test fun `the builders keep the other half`() {
        assertEquals(PasswordsItemAccess(isUsableByAgent = true, grantees = emptyList()), PasswordsItemAccess.accessWithGrantees(access, emptyList()))
        assertEquals(
            PasswordsItemAccess(isUsableByAgent = false, grantees = access.grantees),
            PasswordsItemAccess.accessWithAgentUse(access, false),
        )
        assertEquals(
            PasswordsItemAccess(isUsableByAgent = false, grantees = listOf(PasswordsGrantee.Circle("circle-1"))),
            PasswordsItemAccess.accessWithGrantees(null, listOf(PasswordsGrantee.Circle("circle-1"))),
        )
        assertEquals(PasswordsItemAccess(isUsableByAgent = true, grantees = emptyList()), PasswordsItemAccess.accessWithAgentUse(null, true))
    }

    // The signature

    private val accountKey = ByteArray(32) { index -> (0xa0 + index).toByte() }
    private val otherAccountKey = ByteArray(32) { index -> (0xb0 + index).toByte() }
    private val validMac = "EBESExQVFhcYGRobHB0eHyAhIiMkJSYnKCkqKywtLi8="

    @Test fun `the version and signature are written and read back`() {
        val signed = PasswordsItemAccess(isUsableByAgent = false, grantees = listOf(PasswordsGrantee.Person("user-ben")), recordVersion = 3, recordMac = validMac)
        assertEquals(
            """{"grantees":[{"userId":"user-ben"}],"isUsableByAgent":false,"recordMac":"EBESExQVFhcYGRobHB0eHyAhIiMkJSYnKCkqKywtLi8=","recordVersion":3}""",
            sortedJsonText(signed.toJson()),
        )
        for (kind in allKinds) {
            val original = PasswordsItemFields.fromJsonBytes(kind, """{"name":"x"}""".toByteArray()).withAccess(signed)
            val decoded = PasswordsItemFields.fromJsonBytes(kind, original.jsonBytes())
            assertEquals(signed, decoded.access, kind.rawValue)
        }
        // Either one may be missing: the record is read, and verifyAccess then refuses it.
        val versionOnly = accessFromText("""{"isUsableByAgent":true,"grantees":[],"recordVersion":2}""")
        assertEquals(PasswordsItemAccess(isUsableByAgent = true, grantees = emptyList(), recordVersion = 2), versionOnly)
        val neither = accessFromText("""{"isUsableByAgent":true,"grantees":[]}""")
        assertNull(neither.recordVersion)
        assertNull(neither.recordMac)
    }

    @Test fun `the builders drop the signature`() {
        val signed = PasswordsItemAccess(isUsableByAgent = true, grantees = access.grantees, recordVersion = 4, recordMac = validMac)
        assertEquals(PasswordsItemAccess(isUsableByAgent = true), PasswordsItemAccess.accessWithGrantees(signed, emptyList()))
        assertEquals(
            PasswordsItemAccess(isUsableByAgent = false, grantees = access.grantees),
            PasswordsItemAccess.accessWithAgentUse(signed, false),
        )
    }

    @Test fun `the signed bytes are the documented ones`() {
        val record = PasswordsItemAccess(isUsableByAgent = true, grantees = listOf(PasswordsGrantee.Person("user-ben"), PasswordsGrantee.Circle("circle-1")), recordVersion = 12)
        val bytes = PasswordsCrypto.accessSignedBytes("item-1", PasswordsItemKind.Note, record)
        assertEquals("teanode passwords access record v1\u0000item-1\u0000note\u000012\u00001\u0000c:circle-1\u0000u:user-ben\u0000", String(bytes, Charsets.UTF_8))
        val off = PasswordsItemAccess(isUsableByAgent = false, grantees = emptyList(), recordVersion = 12)
        assertEquals(
            "teanode passwords access record v1\u0000item-1\u0000note\u000012\u00000\u0000",
            String(PasswordsCrypto.accessSignedBytes("item-1", PasswordsItemKind.Note, off), Charsets.UTF_8),
        )
        assertThrows<PasswordsCryptoException> { PasswordsCrypto.accessSignedBytes("item-1", PasswordsItemKind.Note, access) }
    }

    @Test fun `the grantees are sorted by UTF-8 bytes not by character`() {
        // U+FF5E is three bytes starting 0xef and U+1F600 is four bytes starting 0xf0, so byte order puts the first one first.
        val record = PasswordsItemAccess(
            isUsableByAgent = false, grantees = listOf(PasswordsGrantee.Person("\uD83D\uDE00"), PasswordsGrantee.Person("\uFF5E")), recordVersion = 1,
        )
        val text = String(PasswordsCrypto.accessSignedBytes("item-1", PasswordsItemKind.Login, record), Charsets.UTF_8)
        assertTrue(text.endsWith("u:\uFF5E\u0000u:\uD83D\uDE00\u0000"), text)
    }

    @Test fun `signing counts one more than the highest version seen and verifies`() {
        val signed = PasswordsCrypto.signAccess(accountKey, "item-1", PasswordsItemKind.Login, access, 4)
        assertEquals(5L, signed.recordVersion)
        assertTrue(PasswordsCrypto.isAccessVerified(accountKey, "item-1", PasswordsItemKind.Login, signed))
        // A version or signature in the input is not used.
        val stale = signed.copy(recordVersion = 900, recordMac = validMac)
        assertEquals(signed, PasswordsCrypto.signAccess(accountKey, "item-1", PasswordsItemKind.Login, stale, 4))
        // The order of grantees does not matter.
        val reversed = signed.copy(grantees = signed.grantees.reversed())
        assertTrue(PasswordsCrypto.isAccessVerified(accountKey, "item-1", PasswordsItemKind.Login, reversed))
        // No record is signed as one that grants nobody and no agent use.
        val empty = PasswordsCrypto.signAccess(accountKey, "item-1", PasswordsItemKind.Login, null, 0)
        assertEquals(1L, empty.recordVersion)
        assertFalse(empty.isUsableByAgent)
        assertTrue(empty.grantees.isEmpty())
    }

    @Test fun `a record that changed in anything the signature covers is refused`() {
        val signed = PasswordsCrypto.signAccess(accountKey, "item-1", PasswordsItemKind.Login, access, 4)
        fun isVerified(
            record: PasswordsItemAccess?,
            key: ByteArray = accountKey,
            itemId: String = "item-1",
            itemKind: PasswordsItemKind = PasswordsItemKind.Login,
        ): Boolean = PasswordsCrypto.isAccessVerified(key, itemId, itemKind, record)
        assertTrue(isVerified(signed))
        assertFalse(isVerified(signed, key = otherAccountKey), "another account key")
        assertFalse(isVerified(signed, itemId = "item-2"), "another item")
        assertFalse(isVerified(signed, itemKind = PasswordsItemKind.Note), "another kind")
        assertFalse(isVerified(signed.copy(isUsableByAgent = false)), "agent use")
        assertFalse(isVerified(signed.copy(recordVersion = 6)), "version")
        assertFalse(isVerified(signed.copy(grantees = signed.grantees + PasswordsGrantee.Person("user-cy"))), "a grantee")
        assertFalse(
            isVerified(signed.copy(grantees = listOf(PasswordsGrantee.Circle("user-ben"), PasswordsGrantee.Circle("circle-1")))),
            "a user turned into a circle",
        )
        val flipped = unbase64(signed.recordMac!!)
        flipped[0] = (flipped[0].toInt() xor 1).toByte()
        assertFalse(isVerified(signed.copy(recordMac = base64(flipped))), "a flipped signature bit")
        assertFalse(isVerified(signed.copy(recordMac = "%%%")), "a signature that is not base64")
        assertFalse(isVerified(signed.copy(recordMac = null)), "no signature")
        assertFalse(isVerified(signed.copy(recordVersion = null)), "no version")
        assertFalse(isVerified(null), "no record")
    }

    @Test fun `a version written as a whole number is the same version`() {
        val accepted = listOf("1" to 1L, "1.0" to 1L, "1e0" to 1L, "1E0" to 1L, "7.000" to 7L, "0.5e1" to 5L, "9007199254740991" to PasswordsCrypto.MAXIMUM_RECORD_VERSION)
        for ((text, want) in accepted) {
            val record = accessFromText("""{"isUsableByAgent":true,"grantees":[],"recordVersion":$text}""")
            assertEquals(want, record.recordVersion, text)
        }
        for (text in listOf("0.0", "-1.0", "1.5", "\"1\"", "null", "9007199254740992", "9007199254740993", "1e400", "1e16")) {
            assertThrows<Exception>(text) { accessFromText("""{"isUsableByAgent":true,"grantees":[],"recordVersion":$text}""") }
        }
    }

    @Test fun `a control character in a grantee ID is refused when signing and verifying`() {
        // An id with a NUL and "u:" in it would sign to the same bytes as two grantees.
        val pair = PasswordsCrypto.signAccess(
            accountKey, "item-1", PasswordsItemKind.Note,
            PasswordsItemAccess(isUsableByAgent = false, grantees = listOf(PasswordsGrantee.Person("user-a"), PasswordsGrantee.Person("user-b"))), 0,
        )
        val identifiers = listOf("user-a\u0000u:user-b", "\u0000user-a", "user-a\n", "user\t-a", "user-a\u007f", "user-a\u0085")
        for (identifier in identifiers) {
            for (grantee in listOf(PasswordsGrantee.Person(identifier), PasswordsGrantee.Circle(identifier))) {
                val record = PasswordsItemAccess(isUsableByAgent = false, grantees = listOf(grantee))
                assertThrows<PasswordsCryptoException>(identifier) { PasswordsCrypto.signAccess(accountKey, "item-1", PasswordsItemKind.Note, record, 0) }
                val presented = record.copy(recordVersion = pair.recordVersion, recordMac = pair.recordMac)
                assertFalse(PasswordsCrypto.isAccessVerified(accountKey, "item-1", PasswordsItemKind.Note, presented), identifier)
            }
        }
        // A space, a non-breaking space, an accent and an emoji are not control characters.
        for (identifier in listOf("user a", "user\u00a0a", "user-\u00e9", "user-\uD83D\uDE00")) {
            val signed = PasswordsCrypto.signAccess(
                accountKey, "item-1", PasswordsItemKind.Note,
                PasswordsItemAccess(isUsableByAgent = false, grantees = listOf(PasswordsGrantee.Person(identifier))), 0,
            )
            assertTrue(PasswordsCrypto.isAccessVerified(accountKey, "item-1", PasswordsItemKind.Note, signed), identifier)
        }
    }

    @Test fun `signing refuses bad input`() {
        assertThrows<Exception> { PasswordsCrypto.signAccess(ByteArray(16), "item-1", PasswordsItemKind.Login, access, 0) }
        assertThrows<PasswordsCryptoException> { PasswordsCrypto.signAccess(accountKey, "item-1", PasswordsItemKind.Login, access, -1) }
        assertThrows<PasswordsCryptoException> {
            PasswordsCrypto.signAccess(accountKey, "item-1", PasswordsItemKind.Login, access, PasswordsCrypto.MAXIMUM_RECORD_VERSION)
        }
    }

    @Test fun `with signed access and verify access work on the fields`() {
        val fields = PasswordsItemFields.Note(PasswordsNoteFields(name = "Example", text = "hello"))
        val signedFields = PasswordsCrypto.withSignedAccess(accountKey, "item-1", PasswordsItemKind.Note, fields, access, 2)
        assertEquals("Example", signedFields.name)
        assertEquals(3L, signedFields.access?.recordVersion)
        assertEquals(signedFields.access, PasswordsCrypto.verifyAccess(accountKey, "item-1", PasswordsItemKind.Note, signedFields))
        assertNull(PasswordsCrypto.verifyAccess(accountKey, "item-2", PasswordsItemKind.Note, signedFields))
        assertNull(PasswordsCrypto.verifyAccess(otherAccountKey, "item-1", PasswordsItemKind.Note, signedFields))
        assertNull(PasswordsCrypto.verifyAccess(accountKey, "item-1", PasswordsItemKind.Note, fields))
        assertNull(PasswordsCrypto.verifyAccess(accountKey, "item-1", PasswordsItemKind.Note, fields.withAccess(access)))
    }
}
