package com.teanode.kit.passwords

import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.boolean
import kotlinx.serialization.json.int
import kotlinx.serialization.json.long
import org.junit.jupiter.api.Assertions.assertArrayEquals
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.assertThrows
import java.io.File

/** The codes against RFC 6238 appendix B and the links against the forms websites actually produce. */
class PasswordsOneTimeCodeTest {
    private val sha1Secret = "12345678901234567890"
    private val sha256Secret = "12345678901234567890123456789012"
    private val sha512Secret = "1234567890123456789012345678901234567890123456789012345678901234"

    private fun link(secret: String, algorithm: String): String =
        "otpauth://totp/Example:ada@example.com?secret=${PasswordsBase32.encode(secret.toByteArray())}&algorithm=$algorithm&digits=8&issuer=Example"

    private fun failure(link: String): PasswordsOneTimeCodeFailure? = try {
        PasswordsOneTimeCode.parse(link)
        null
    } catch (error: PasswordsOneTimeCodeException) {
        error.failure
    }

    @Test fun `RFC 6238 appendix B vectors`() {
        // seconds, SHA1, SHA256, SHA512
        val rows = listOf(
            listOf(59L, "94287082", "46119246", "90693936"),
            listOf(1_111_111_109L, "07081804", "68084774", "25091201"),
            listOf(1_111_111_111L, "14050471", "67062674", "99943326"),
            listOf(1_234_567_890L, "89005924", "91819424", "93441116"),
            listOf(2_000_000_000L, "69279037", "90698825", "38618901"),
            listOf(20_000_000_000L, "65353130", "77737706", "47863826"),
        )
        val sha1 = PasswordsOneTimeCode.parse(link(sha1Secret, "SHA1"))
        val sha256 = PasswordsOneTimeCode.parse(link(sha256Secret, "SHA256"))
        val sha512 = PasswordsOneTimeCode.parse(link(sha512Secret, "SHA512"))
        for (row in rows) {
            val seconds = row[0] as Long
            assertEquals(row[1], PasswordsOneTimeCode.code(sha1, seconds), "SHA1 at $seconds")
            assertEquals(row[2], PasswordsOneTimeCode.code(sha256, seconds), "SHA256 at $seconds")
            assertEquals(row[3], PasswordsOneTimeCode.code(sha512, seconds), "SHA512 at $seconds")
        }
    }

    @Test fun `defaults are SHA1 six digits thirty seconds`() {
        val settings = PasswordsOneTimeCode.parse("otpauth://totp/Example:ada@example.com?secret=${PasswordsBase32.encode(sha1Secret.toByteArray())}&issuer=Example")
        assertEquals(PasswordsOneTimeCodeAlgorithm.Sha1, settings.algorithm)
        assertEquals(6, settings.digitCount)
        assertEquals(30, settings.periodSeconds)
        assertEquals("Example", settings.issuer)
        assertEquals("ada@example.com", settings.accountName)
        assertEquals("287082", PasswordsOneTimeCode.code(settings, 59))
    }

    @Test fun `lower case unpadded and spaced secrets read the same`() {
        val upper = PasswordsOneTimeCode.parse("otpauth://totp/a?secret=JBSWY3DPEHPK3PXP")
        val lower = PasswordsOneTimeCode.parse("otpauth://totp/a?secret=jbswy3dpehpk3pxp")
        val padded = PasswordsOneTimeCode.parse("otpauth://totp/a?secret=JBSWY3DPEHPK3PXP======")
        val spaced = PasswordsOneTimeCode.parse("otpauth://totp/a?secret=jbsw%20y3dp%20ehpk%203pxp")
        assertArrayEquals(upper.secret, lower.secret)
        assertArrayEquals(upper.secret, padded.secret)
        assertArrayEquals(upper.secret, spaced.secret)
        assertArrayEquals(byteArrayOf(0x48, 0x65, 0x6C, 0x6C, 0x6F, 0x21, 0xDE.toByte(), 0xAD.toByte(), 0xBE.toByte(), 0xEF.toByte()), upper.secret)
    }

    @Test fun `period and seconds left`() {
        val settings = PasswordsOneTimeCode.parse("otpauth://totp/a?secret=JBSWY3DPEHPK3PXP&period=60")
        assertEquals(60, settings.periodSeconds)
        assertEquals(60, PasswordsOneTimeCode.secondsLeft(settings, 0))
        assertEquals(1, PasswordsOneTimeCode.secondsLeft(settings, 59))
        assertEquals(60, PasswordsOneTimeCode.secondsLeft(settings, 60))
        assertEquals(30, PasswordsOneTimeCode.secondsLeft(settings, 90))
        assertEquals(PasswordsOneTimeCode.code(settings, 61), PasswordsOneTimeCode.code(settings, 119))
        assertNotEquals(PasswordsOneTimeCode.code(settings, 59), PasswordsOneTimeCode.code(settings, 60))
    }

    @Test fun `codes keep leading zeros`() {
        val settings = PasswordsOneTimeCode.parse(link(sha1Secret, "SHA1"))
        assertEquals("07081804", PasswordsOneTimeCode.code(settings, 1_111_111_109))
    }

    @Test fun `a label without an issuer and an issuer only in the query`() {
        val plain = PasswordsOneTimeCode.parse("otpauth://totp/ada%40example.test?secret=JBSWY3DPEHPK3PXP&issuer=Example%20Inc")
        assertEquals("Example Inc", plain.issuer)
        assertEquals("ada@example.test", plain.accountName)
    }

    @Test fun `bad links are refused with a reason`() {
        assertEquals(PasswordsOneTimeCodeFailure.NotTimeBasedLink, failure("https://example.com"))
        assertEquals(PasswordsOneTimeCodeFailure.NotTimeBasedLink, failure("otpauth://hotp/a?secret=JBSWY3DPEHPK3PXP&counter=1"))
        assertEquals(PasswordsOneTimeCodeFailure.InvalidSecret, failure("otpauth://totp/a"))
        assertEquals(PasswordsOneTimeCodeFailure.InvalidSecret, failure("otpauth://totp/a?secret=!!!"))
        assertEquals(PasswordsOneTimeCodeFailure.UnsupportedAlgorithm, failure("otpauth://totp/a?secret=JBSWY3DPEHPK3PXP&algorithm=MD5"))
        assertEquals(PasswordsOneTimeCodeFailure.UnsupportedDigitCount, failure("otpauth://totp/a?secret=JBSWY3DPEHPK3PXP&digits=7"))
        assertEquals(PasswordsOneTimeCodeFailure.InvalidPeriod, failure("otpauth://totp/a?secret=JBSWY3DPEHPK3PXP&period=0"))
    }

    /** internal/passwords/testdata/onetimecode.json, which the Go package and the dashboard read too: all must read every address the same way. */
    @Test fun `the shared one time code cases`() {
        val file = File("../../internal/passwords/testdata/onetimecode.json")
        assertTrue(file.exists(), "onetimecode.json is not at ${file.absolutePath}")
        val shared = Json.parseToJsonElement(file.readText()) as JsonObject
        val unixSeconds = (shared["unixSeconds"] as JsonArray).map { element -> (element as JsonPrimitive).long }
        val cases = shared["cases"] as JsonArray
        assertFalse(cases.isEmpty())
        assertFalse(unixSeconds.isEmpty())
        for (element in cases) {
            val each = element as JsonObject
            val address = (each["address"] as JsonPrimitive).content
            val label = "${(each["name"] as JsonPrimitive).content}: $address"
            if (!(each["isValid"] as JsonPrimitive).boolean) {
                assertThrows<PasswordsOneTimeCodeException>(label) { PasswordsOneTimeCode.parse(address) }
                continue
            }
            val settings = PasswordsOneTimeCode.parse(address)
            assertEquals((each["secretHex"] as JsonPrimitive).content, settings.secret.joinToString("") { byte -> "%02x".format(byte) }, label)
            assertEquals((each["algorithm"] as JsonPrimitive).content, settings.algorithm.rawValue, label)
            assertEquals((each["digitCount"] as JsonPrimitive).int, settings.digitCount, label)
            assertEquals((each["periodSeconds"] as JsonPrimitive).int, settings.periodSeconds, label)
            assertEquals((each["issuer"] as JsonPrimitive).content, settings.issuer, label)
            assertEquals((each["accountName"] as JsonPrimitive).content, settings.accountName, label)
            val expected = (each["expectedCodes"] as JsonArray).map { code -> (code as JsonPrimitive).content }
            assertEquals(expected, unixSeconds.map { seconds -> PasswordsOneTimeCode.code(settings, seconds) }, label)
        }
    }
}
