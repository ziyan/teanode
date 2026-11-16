package com.teanode.kit.passwords

import javax.crypto.Mac
import javax.crypto.spec.SecretKeySpec

// One-time codes: RFC 6238 (time based) over RFC 4226, from the `otpauth://totp/...` link a website shows next to its QR
// code. The secret is base32 (any case, padding optional); `algorithm` is SHA1 (the default), SHA256 or SHA512; `digits`
// is 6 (the default) or 8; `period` is 30 seconds unless given.

/** Why an `otpauth://` link could not be used. */
enum class PasswordsOneTimeCodeFailure {
    /** Not an `otpauth://totp/` link (counter based `hotp` links are not supported). */
    NotTimeBasedLink,

    /** No `secret`, or one that is not base32. */
    InvalidSecret,

    /** An `algorithm` other than SHA1, SHA256 or SHA512. */
    UnsupportedAlgorithm,

    /** `digits` other than 6 or 8. */
    UnsupportedDigitCount,

    /** A `period` that is not a whole number of seconds above zero. */
    InvalidPeriod,
}

class PasswordsOneTimeCodeException(val failure: PasswordsOneTimeCodeFailure) : Exception(failure.name)

/** The hash a link names for its codes. */
enum class PasswordsOneTimeCodeAlgorithm(val rawValue: String, val macName: String) {
    Sha1("SHA1", "HmacSHA1"),
    Sha256("SHA256", "HmacSHA256"),
    Sha512("SHA512", "HmacSHA512"),
}

/** What an `otpauth://` link says. */
class PasswordsOneTimeCodeSettings(
    val secret: ByteArray,
    val algorithm: PasswordsOneTimeCodeAlgorithm = PasswordsOneTimeCodeAlgorithm.Sha1,
    val digitCount: Int = 6,
    val periodSeconds: Int = 30,
    /** The service named in the link, from `issuer` or the label's prefix; empty if neither. */
    val issuer: String = "",
    /** The account named in the label after the issuer; empty if none. */
    val accountName: String = "",
)

/** Reads `otpauth://` links and works out the codes. */
object PasswordsOneTimeCode {
    /** The longest period read: far longer than any site uses, and a number every language here holds exactly. */
    const val MAXIMUM_PERIOD_SECONDS = 2_147_483_647

    private fun isAsciiWhiteSpace(point: Int) = point == 0x20 || point == 0x09 || point == 0x0A || point == 0x0D || point == 0x0C || point == 0x0B

    /**
     * Reads an `otpauth://totp/...` link. The reading is strict, and the same as the Go package and the dashboard's (the
     * cases in internal/passwords/testdata/onetimecode.json): only ASCII white space is trimmed from the ends; a control
     * character anywhere is refused; the host is exactly `totp` (no user name, no port); a malformed percent escape or a
     * semicolon in the query is refused; the secret is base32 letters with spaces and hyphens ignored and `=` only at the
     * end; the digit count and the period are plain ASCII digits, the period at most [MAXIMUM_PERIOD_SECONDS]. Written by
     * hand rather than with a URL class, whose answers differ from platform to platform.
     */
    fun parse(uri: String): PasswordsOneTimeCodeSettings {
        fun refuse(failure: PasswordsOneTimeCodeFailure): Nothing = throw PasswordsOneTimeCodeException(failure)
        var points = uri.codePoints().toArray().toList()
        while (points.isNotEmpty() && isAsciiWhiteSpace(points.first())) points = points.drop(1)
        while (points.isNotEmpty() && isAsciiWhiteSpace(points.last())) points = points.dropLast(1)
        if (points.any { point -> point < 0x20 || point == 0x7F }) refuse(PasswordsOneTimeCodeFailure.NotTimeBasedLink)
        var rest = String(points.toIntArray(), 0, points.size)
        // A fragment is not part of what the link says.
        rest.indexOf('#').takeIf { index -> index >= 0 }?.let { fragmentStart -> rest = rest.substring(0, fragmentStart) }

        val schemeEnd = rest.indexOf(':')
        if (schemeEnd < 0 || rest.substring(0, schemeEnd).lowercase() != "otpauth" || !rest.substring(schemeEnd + 1).startsWith("//")) {
            refuse(PasswordsOneTimeCodeFailure.NotTimeBasedLink)
        }
        rest = rest.substring(schemeEnd + 3)

        var queryText = ""
        rest.indexOf('?').takeIf { index -> index >= 0 }?.let { queryStart ->
            queryText = rest.substring(queryStart + 1)
            rest = rest.substring(0, queryStart)
        }
        var host = rest
        var pathText = ""
        rest.indexOf('/').takeIf { index -> index >= 0 }?.let { pathStart ->
            host = rest.substring(0, pathStart)
            pathText = rest.substring(pathStart)
        }
        if (host.lowercase() != "totp") refuse(PasswordsOneTimeCodeFailure.NotTimeBasedLink)

        val parameters = mutableMapOf<String, String>()
        if (queryText.contains(';')) refuse(PasswordsOneTimeCodeFailure.NotTimeBasedLink)
        for (pair in queryText.split('&').filter { piece -> piece.isNotEmpty() }) {
            val equals = pair.indexOf('=')
            val name = if (equals >= 0) pair.substring(0, equals) else pair
            val value = if (equals >= 0) pair.substring(equals + 1) else ""
            val decodedName = percentDecoded(name, isPlusSpace = true) ?: refuse(PasswordsOneTimeCodeFailure.NotTimeBasedLink)
            val decodedValue = percentDecoded(value, isPlusSpace = true) ?: refuse(PasswordsOneTimeCodeFailure.NotTimeBasedLink)
            // The first of a repeated name is the one read.
            if (!parameters.containsKey(decodedName)) parameters[decodedName] = decodedValue
        }

        val secretText = (parameters["secret"] ?: "").filter { character -> character != ' ' && character != '-' }
        val trimmedSecretText = secretText.trimEnd('=')
        val secret = if (trimmedSecretText.isEmpty()) null else lenientBase32(trimmedSecretText)
        if (secret == null || secret.isEmpty()) refuse(PasswordsOneTimeCodeFailure.InvalidSecret)

        var algorithm = PasswordsOneTimeCodeAlgorithm.Sha1
        parameters["algorithm"]?.takeIf { text -> text.isNotEmpty() }?.let { algorithmText ->
            algorithm = PasswordsOneTimeCodeAlgorithm.entries.firstOrNull { known -> known.rawValue == algorithmText.uppercase() }
                ?: refuse(PasswordsOneTimeCodeFailure.UnsupportedAlgorithm)
        }

        var digitCount = 6
        parameters["digits"]?.takeIf { text -> text.isNotEmpty() }?.let { digitText ->
            val parsed = asciiNumber(digitText)
            if (parsed != 6L && parsed != 8L) refuse(PasswordsOneTimeCodeFailure.UnsupportedDigitCount)
            digitCount = parsed!!.toInt()
        }

        var periodSeconds = 30
        parameters["period"]?.takeIf { text -> text.isNotEmpty() }?.let { periodText ->
            val parsed = asciiNumber(periodText)
            if (parsed == null || parsed < 1 || parsed > MAXIMUM_PERIOD_SECONDS) refuse(PasswordsOneTimeCodeFailure.InvalidPeriod)
            periodSeconds = parsed.toInt()
        }

        // The label is the path without its leading slash: "Issuer:account" or "account".
        var label = percentDecoded(pathText, isPlusSpace = false) ?: refuse(PasswordsOneTimeCodeFailure.NotTimeBasedLink)
        if (label.startsWith("/")) label = label.substring(1)
        var issuer = parameters["issuer"] ?: ""
        var accountName = label.trim()
        label.indexOf(':').takeIf { index -> index >= 0 }?.let { separator ->
            val labelIssuer = label.substring(0, separator).trim()
            accountName = label.substring(separator + 1).trim()
            if (issuer.isEmpty()) issuer = labelIssuer
        }
        return PasswordsOneTimeCodeSettings(secret, algorithm, digitCount, periodSeconds, issuer, accountName)
    }

    /** Text with its `%XX` escapes read (and `+` as a space in a query); null for an escape that is not two hex digits. */
    private fun percentDecoded(text: String, isPlusSpace: Boolean): String? {
        val bytes = java.io.ByteArrayOutputStream()
        val source = text.toByteArray(Charsets.UTF_8)
        var index = 0
        while (index < source.size) {
            val byte = source[index].toInt() and 0xFF
            if (byte == '%'.code) {
                if (index + 2 >= source.size) return null
                val high = hexValue(source[index + 1].toInt() and 0xFF) ?: return null
                val low = hexValue(source[index + 2].toInt() and 0xFF) ?: return null
                bytes.write(high * 16 + low)
                index += 3
            } else if (byte == '+'.code && isPlusSpace) {
                bytes.write(' '.code)
                index += 1
            } else {
                bytes.write(byte)
                index += 1
            }
        }
        return String(bytes.toByteArray(), Charsets.UTF_8)
    }

    private fun hexValue(byte: Int): Int? = when (byte) {
        in '0'.code..'9'.code -> byte - '0'.code
        in 'a'.code..'f'.code -> byte - 'a'.code + 10
        in 'A'.code..'F'.code -> byte - 'A'.code + 10
        else -> null
    }

    /** A whole number made only of ASCII digits (no sign, no space); null when it is anything else or too large. */
    private fun asciiNumber(text: String): Long? {
        if (text.isEmpty() || !text.all { character -> character in '0'..'9' }) return null
        return text.toLongOrNull()
    }

    /** Base32 letters (any case, 2 to 7) read to bytes; the bits left over after the last whole byte are dropped. Null for any other character. */
    private fun lenientBase32(text: String): ByteArray? {
        val bytes = java.io.ByteArrayOutputStream()
        var buffer = 0
        var bitCount = 0
        for (byte in text.toByteArray(Charsets.UTF_8)) {
            val code = byte.toInt() and 0xFF
            val value = when (code) {
                in 'A'.code..'Z'.code -> code - 'A'.code
                in 'a'.code..'z'.code -> code - 'a'.code
                in '2'.code..'7'.code -> code - '2'.code + 26
                else -> return null
            }
            buffer = (buffer shl 5) or value
            bitCount += 5
            if (bitCount >= 8) {
                bitCount -= 8
                bytes.write((buffer shr bitCount) and 0xFF)
                buffer = buffer and ((1 shl bitCount) - 1)
            }
        }
        return bytes.toByteArray()
    }

    /** The code at a time (Unix seconds), as digits with leading zeros kept. */
    fun code(settings: PasswordsOneTimeCodeSettings, unixSeconds: Long): String {
        val counter = maxOf(unixSeconds, 0) / settings.periodSeconds
        val message = ByteArray(8) { index -> ((counter shr (56 - index * 8)) and 0xFF).toByte() }
        val mac = Mac.getInstance(settings.algorithm.macName)
        mac.init(SecretKeySpec(settings.secret, settings.algorithm.macName))
        val digest = mac.doFinal(message)
        val offset = digest[digest.size - 1].toInt() and 0x0F
        val binary = ((digest[offset].toInt() and 0x7F) shl 24) or ((digest[offset + 1].toInt() and 0xFF) shl 16) or
            ((digest[offset + 2].toInt() and 0xFF) shl 8) or (digest[offset + 3].toInt() and 0xFF)
        var modulus = 1L
        repeat(settings.digitCount) { modulus *= 10 }
        return (binary.toLong() % modulus).toString().padStart(settings.digitCount, '0')
    }

    /** How many seconds the code at a time is still good for (1 to the period). */
    fun secondsLeft(settings: PasswordsOneTimeCodeSettings, unixSeconds: Long): Int =
        (settings.periodSeconds - maxOf(unixSeconds, 0) % settings.periodSeconds).toInt()
}
