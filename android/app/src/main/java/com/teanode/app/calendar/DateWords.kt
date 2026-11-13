package com.teanode.app.calendar

import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import java.util.Locale

/**
 * DateWords writes dates and times as the phone's language writes them, in the way the iPhone app's `formatted` calls
 * do. The Calendar, Goals and Journal screens all use it, so one day reads the same wherever it is shown.
 */
object DateWords {
    private fun skeleton(pattern: String, locale: Locale): DateTimeFormatter =
        DateTimeFormatter.ofPattern(android.text.format.DateFormat.getBestDateTimePattern(locale, pattern), locale)

    /** "Oct 3, 2026, 2:30 PM". */
    fun abbreviatedDateTime(instant: Instant, zone: ZoneId = ZoneId.systemDefault(), locale: Locale = Locale.getDefault()): String =
        DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM, FormatStyle.SHORT).withLocale(locale).format(instant.atZone(zone))

    /** "Oct 3, 2026". */
    fun abbreviatedDate(date: LocalDate, locale: Locale = Locale.getDefault()): String =
        DateTimeFormatter.ofLocalizedDate(FormatStyle.MEDIUM).withLocale(locale).format(date)

    /** "2:30 PM". */
    fun shortTime(instant: Instant, zone: ZoneId = ZoneId.systemDefault(), locale: Locale = Locale.getDefault()): String =
        DateTimeFormatter.ofLocalizedTime(FormatStyle.SHORT).withLocale(locale).format(instant.atZone(zone))

    /** "Tuesday, Oct 20". */
    fun weekdayMonthDay(date: LocalDate, locale: Locale = Locale.getDefault()): String = skeleton("EEEEMMMd", locale).format(date)

    /** "Tuesday, October 20". */
    fun weekdayMonthWideDay(date: LocalDate, locale: Locale = Locale.getDefault()): String = skeleton("EEEEMMMMd", locale).format(date)

    /** "Tuesday, Oct 20, 2026". */
    fun weekdayMonthDayYear(date: LocalDate, locale: Locale = Locale.getDefault()): String = skeleton("EEEEMMMdy", locale).format(date)

    /** "Oct 20". */
    fun monthDay(date: LocalDate, locale: Locale = Locale.getDefault()): String = skeleton("MMMd", locale).format(date)

    /** "October". */
    fun monthWide(date: LocalDate, locale: Locale = Locale.getDefault()): String = skeleton("MMMM", locale).format(date)

    /** "October 2026". */
    fun monthYear(date: LocalDate, locale: Locale = Locale.getDefault()): String = skeleton("MMMMy", locale).format(date)

    /** "2026". */
    fun year(date: LocalDate, locale: Locale = Locale.getDefault()): String = skeleton("y", locale).format(date)

    /** A weekday's initial, "T". */
    fun weekdayNarrow(date: LocalDate, locale: Locale = Locale.getDefault()): String = skeleton("EEEEE", locale).format(date)

    /** "2 PM". */
    fun hour(hour: Int, locale: Locale = Locale.getDefault()): String =
        skeleton("j", locale).format(java.time.LocalTime.of(hour.coerceIn(0, 23), 0))
}
