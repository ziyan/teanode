package com.teanode.kit

import java.io.ByteArrayOutputStream

/** QueryString reads and writes the `a=b&c=d` text of a query, a fragment or a form body. */
object QueryString {
    private const val UNRESERVED = "-._~"

    /** Percent-encodes everything but letters, digits and `-._~`. */
    fun encode(text: String): String {
        val encoded = StringBuilder()
        for (byte in text.toByteArray(Charsets.UTF_8)) {
            val code = byte.toInt() and 0xFF
            val character = code.toChar()
            if (code < 0x80 && (character.isLetterOrDigit() || UNRESERVED.indexOf(character) >= 0)) {
                encoded.append(character)
            } else {
                encoded.append('%').append("0123456789ABCDEF"[code shr 4]).append("0123456789ABCDEF"[code and 0xF])
            }
        }
        return encoded.toString()
    }

    fun decode(text: String): String {
        val bytes = ByteArrayOutputStream()
        var position = 0
        val source = text.toByteArray(Charsets.UTF_8)
        while (position < source.size) {
            val byte = source[position].toInt()
            if (byte == '%'.code && position + 2 < source.size) {
                val high = Character.digit(source[position + 1].toInt().toChar(), 16)
                val low = Character.digit(source[position + 2].toInt().toChar(), 16)
                if (high >= 0 && low >= 0) {
                    bytes.write(high * 16 + low)
                    position += 3
                    continue
                }
            }
            bytes.write(if (byte == '+'.code) ' '.code else byte)
            position += 1
        }
        return bytes.toString(Charsets.UTF_8.name())
    }

    /** The first value of each name in the text. */
    fun parse(text: String): Map<String, String> {
        val items = LinkedHashMap<String, String>()
        for (pair in text.split('&')) {
            if (pair.isEmpty()) continue
            val name = decode(pair.substringBefore('='))
            val value = if (pair.contains('=')) decode(pair.substringAfter('=')) else ""
            if (name !in items) items[name] = value
        }
        return items
    }

    /** The fields form-encoded, in a stable order. */
    fun form(fields: Map<String, String>): String =
        fields.toSortedMap().entries.joinToString("&") { (name, fieldValue) -> "${encode(name)}=${encode(fieldValue)}" }
}
