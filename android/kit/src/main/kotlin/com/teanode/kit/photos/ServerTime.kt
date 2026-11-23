package com.teanode.kit.photos

import java.time.Instant
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.ZoneOffset
import java.time.ZonedDateTime
import java.time.format.DateTimeFormatter

/** ServerTime reads the RFC 3339 times the server writes. */
object ServerTime {
    private val fractionOfSecond = Regex("""\.\d+""")

    /**
     * The instant an RFC 3339 time stands for; null for anything else. Go writes up to nine digits of fractions of a
     * second, which are dropped first: no screen shows less than a second.
     */
    fun instant(text: String?): Instant? {
        if (text.isNullOrEmpty()) return null
        return try {
            OffsetDateTime.parse(fractionOfSecond.replaceFirst(text, ""), DateTimeFormatter.ISO_OFFSET_DATE_TIME).toInstant()
        } catch (_: Exception) {
            null
        }
    }

    /** An instant written the way the server reads it: UTC with a Z. */
    fun text(instant: Instant): String = DateTimeFormatter.ISO_INSTANT.format(instant.truncatedToSeconds())

    private fun Instant.truncatedToSeconds(): Instant = Instant.ofEpochSecond(epochSecond)

    fun zoneOfOffsetMinutes(offsetMinutes: Int): ZoneId = ZoneOffset.ofTotalSeconds(offsetMinutes * 60)

    fun zoned(instant: Instant, zone: ZoneId): ZonedDateTime = instant.atZone(zone)
}
