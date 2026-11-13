package com.teanode.kit.mail

import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId

/** ServerTime reads the times the server writes: RFC 3339, in UTC or with an offset. */
object ServerTime {
    /**
     * The moment an RFC 3339 time stands for; null for anything else. Go writes up to nine digits of fractions of a
     * second, which are dropped first: no screen shows less than a second.
     */
    fun instant(text: String?): Instant? {
        if (text.isNullOrEmpty()) return null
        val withoutFraction = Regex("""\.[0-9]+""").replaceFirst(text, "")
        return try {
            java.time.OffsetDateTime.parse(withoutFraction).toInstant()
        } catch (_: java.time.format.DateTimeParseException) {
            null
        }
    }

    /** A time as the server takes it: RFC 3339 in UTC. */
    fun text(moment: Instant): String = java.time.format.DateTimeFormatter.ISO_INSTANT.format(moment.truncatedTo(java.time.temporal.ChronoUnit.SECONDS))

    /** The start of a calendar day in the person's zone, as the server takes it. */
    fun startOfDayText(day: LocalDate, zone: ZoneId = ZoneId.systemDefault()): String = text(day.atStartOfDay(zone).toInstant())
}
