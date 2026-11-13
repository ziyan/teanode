package com.teanode.kit.location

import kotlinx.serialization.KSerializer
import kotlinx.serialization.descriptors.PrimitiveKind
import kotlinx.serialization.descriptors.PrimitiveSerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder
import java.time.Instant
import java.time.format.DateTimeFormatter
import java.time.temporal.ChronoUnit

/** ServerTime reads and writes the RFC 3339 times the server speaks, as the iPhone app's `ServerTime` and `CalendarDates.text` do. */
object ServerTime {
    /**
     * The instant an RFC 3339 time stands for; null for anything else. Go writes up to nine digits of fractions of a
     * second, which are dropped first: no screen shows less than a second.
     */
    fun instant(text: String?): Instant? {
        if (text.isNullOrEmpty()) return null
        val withoutFraction = text.replace(Regex("""\.\d+"""), "")
        return try {
            java.time.OffsetDateTime.parse(withoutFraction, DateTimeFormatter.ISO_OFFSET_DATE_TIME).toInstant()
        } catch (_: Exception) {
            null
        }
    }

    /** A time as the server takes it: RFC 3339 in UTC, to the second. */
    fun text(instant: Instant): String = DateTimeFormatter.ISO_INSTANT.format(instant.truncatedTo(ChronoUnit.SECONDS))

    /**
     * Whether the first time is later than the second, down to the fractions of a second Go writes. A time that is not
     * one is never later, and anything is later than it.
     */
    fun isLater(text: String?, thanText: String?): Boolean {
        val moment = preciseMoment(text) ?: return false
        val otherMoment = preciseMoment(thanText) ?: return true
        if (moment.first != otherMoment.first) return moment.first.isAfter(otherMoment.first)
        return moment.second > otherMoment.second
    }

    private fun preciseMoment(text: String?): Pair<Instant, Double>? {
        val wholeSeconds = instant(text) ?: return null
        val digits = Regex("""\.(\d+)""").find(text.orEmpty())?.groupValues?.get(1)
        val fraction = if (digits == null) 0.0 else "0.$digits".toDoubleOrNull() ?: 0.0
        return wholeSeconds to fraction
    }
}

/** An instant kept as milliseconds since 1970, which is how the app's own files hold a time. */
object InstantAsEpochMillisSerializer : KSerializer<Instant> {
    override val descriptor = PrimitiveSerialDescriptor("InstantAsEpochMillis", PrimitiveKind.LONG)
    override fun serialize(encoder: Encoder, value: Instant) = encoder.encodeLong(value.toEpochMilli())
    override fun deserialize(decoder: Decoder): Instant = Instant.ofEpochMilli(decoder.decodeLong())
}
