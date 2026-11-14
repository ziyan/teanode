package com.teanode.kit.calendar

import java.time.Instant
import java.time.OffsetDateTime

/** ServerTime reads the RFC 3339 times the server writes. */
object ServerTime {
    /**
     * The moment an RFC 3339 time stands for; null for anything else. Go writes up to nine digits of fractions of a
     * second, which are dropped first: no screen shows less than a second.
     */
    fun instant(text: String?): Instant? {
        if (text.isNullOrEmpty()) return null
        val withoutFraction = text.replace(Regex("\\.[0-9]*"), "")
        return try {
            OffsetDateTime.parse(withoutFraction).toInstant()
        } catch (_: Exception) {
            null
        }
    }

    /**
     * Whether the first RFC 3339 time is later than the second, down to the fractions of a second Go writes. A time
     * that is not one is never later, and anything is later than it.
     */
    fun isLater(text: String?, otherText: String?): Boolean {
        val moment = preciseMoment(text) ?: return false
        val otherMoment = preciseMoment(otherText) ?: return true
        val wholeSecondsOrder = moment.first.compareTo(otherMoment.first)
        if (wholeSecondsOrder != 0) return wholeSecondsOrder > 0
        return moment.second > otherMoment.second
    }

    private fun preciseMoment(text: String?): Pair<Instant, Double>? {
        val wholeSeconds = instant(text) ?: return null
        val digits = Regex("\\.([0-9]+)").find(text.orEmpty())?.groupValues?.get(1)
        val fraction = digits?.let { fractionDigits -> "0.$fractionDigits".toDoubleOrNull() } ?: 0.0
        return wholeSeconds to fraction
    }
}
