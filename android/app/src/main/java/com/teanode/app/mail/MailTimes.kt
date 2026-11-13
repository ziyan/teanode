package com.teanode.app.mail

import com.teanode.kit.mail.ServerTime
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.ZonedDateTime
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import java.time.temporal.ChronoUnit
import java.time.temporal.WeekFields
import java.util.Locale

/** The stretch of time a row of a list falls in, as a mail app groups its rows. */
sealed class ListPeriod {
    data object Earlier : ListPeriod()
    data object Today : ListPeriod()
    data object Yesterday : ListPeriod()
    data object ThisWeek : ListPeriod()
    data object LastWeek : ListPeriod()
    data object ThisMonth : ListPeriod()
    data class Month(val year: Int, val month: Int, val isThisYear: Boolean) : ListPeriod()
}

/** One period of a list and the rows in it. */
data class ListPeriodRows<Row>(val period: ListPeriod, val rows: List<Row>)

/**
 * MailTimes says when a row happened, in the words a mail list uses: Today, Yesterday, This Week, Last Week, This
 * Month, then each month. A row says its time within the first two and its weekday or date beyond them.
 */
object MailTimes {
    /** The moment a server time stands for, or null for none. */
    fun moment(text: String?): Instant? = ServerTime.instant(text)

    private fun sameWeek(first: LocalDate, second: LocalDate, locale: Locale): Boolean {
        val fields = WeekFields.of(locale)
        return first.get(fields.weekBasedYear()) == second.get(fields.weekBasedYear()) &&
            first.get(fields.weekOfWeekBasedYear()) == second.get(fields.weekOfWeekBasedYear())
    }

    /** The period a moment falls in. */
    fun period(moment: Instant?, now: Instant = Instant.now(), zone: ZoneId = ZoneId.systemDefault(), locale: Locale = Locale.getDefault()): ListPeriod {
        if (moment == null) return ListPeriod.Earlier
        val day = moment.atZone(zone).toLocalDate()
        val today = now.atZone(zone).toLocalDate()
        if (day == today) return ListPeriod.Today
        if (day == today.minusDays(1)) return ListPeriod.Yesterday
        if (sameWeek(day, today, locale)) return ListPeriod.ThisWeek
        if (sameWeek(day, today.minusWeeks(1), locale)) return ListPeriod.LastWeek
        if (day.year == today.year && day.month == today.month) return ListPeriod.ThisMonth
        return ListPeriod.Month(day.year, day.monthValue, day.year == today.year)
    }

    /** The rows of a list already ordered newest first, grouped into periods in that order. */
    fun <Row> periods(
        rows: List<Row>,
        now: Instant = Instant.now(),
        zone: ZoneId = ZoneId.systemDefault(),
        locale: Locale = Locale.getDefault(),
        momentOf: (Row) -> Instant?,
    ): List<ListPeriodRows<Row>> {
        val grouped = mutableListOf<ListPeriodRows<Row>>()
        var currentPeriod: ListPeriod? = null
        var currentRows = mutableListOf<Row>()
        for (row in rows) {
            val period = period(momentOf(row), now, zone, locale)
            if (period != currentPeriod && currentPeriod != null && currentRows.isNotEmpty()) {
                grouped.add(ListPeriodRows(currentPeriod, currentRows))
                currentRows = mutableListOf()
            }
            currentPeriod = period
            currentRows.add(row)
        }
        if (currentPeriod != null && currentRows.isNotEmpty()) grouped.add(ListPeriodRows(currentPeriod, currentRows))
        return grouped
    }

    /** The name of a month, with its year when it is not this year's. */
    fun monthTitle(period: ListPeriod.Month, locale: Locale = Locale.getDefault()): String {
        val pattern = if (period.isThisYear) "LLLL" else "LLLL yyyy"
        return java.time.YearMonth.of(period.year, period.month).format(DateTimeFormatter.ofPattern(pattern, locale))
    }

    /** What a row shows on its right: the time today and yesterday, the weekday this week and last, else the short date. */
    fun label(moment: Instant?, now: Instant = Instant.now(), zone: ZoneId = ZoneId.systemDefault(), locale: Locale = Locale.getDefault()): String {
        if (moment == null) return ""
        val shown = moment.atZone(zone)
        return when (period(moment, now, zone, locale)) {
            ListPeriod.Today, ListPeriod.Yesterday -> time(shown, locale)
            ListPeriod.ThisWeek, ListPeriod.LastWeek -> shown.format(DateTimeFormatter.ofPattern("EEEE", locale))
            else -> shown.format(DateTimeFormatter.ofLocalizedDate(FormatStyle.SHORT).withLocale(locale))
        }
    }

    /** A time today, a weekday this week, else a date; "Yesterday" is said by the caller since it needs the app's words. */
    fun short(moment: Instant?, now: Instant = Instant.now(), zone: ZoneId = ZoneId.systemDefault(), locale: Locale = Locale.getDefault()): ShortTime {
        if (moment == null) return ShortTime.None
        val shown = moment.atZone(zone)
        val today = now.atZone(zone).toLocalDate()
        val day = shown.toLocalDate()
        if (day == today) return ShortTime.Text(time(shown, locale))
        if (day == today.minusDays(1)) return ShortTime.Yesterday
        if (ChronoUnit.DAYS.between(day, today) < 7) return ShortTime.Text(shown.format(DateTimeFormatter.ofPattern("EEEE", locale)))
        return ShortTime.Text(shown.format(DateTimeFormatter.ofLocalizedDate(FormatStyle.SHORT).withLocale(locale)))
    }

    /** A date and time, such as "Oct 10, 2026, 9:00 AM". */
    fun dateAndTime(moment: Instant, zone: ZoneId = ZoneId.systemDefault(), locale: Locale = Locale.getDefault()): String =
        moment.atZone(zone).format(DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM, FormatStyle.SHORT).withLocale(locale))

    /** A time of day, such as "9:00 AM". */
    fun timeOfDay(moment: Instant, zone: ZoneId = ZoneId.systemDefault(), locale: Locale = Locale.getDefault()): String = time(moment.atZone(zone), locale)

    /** A whole date, such as "Saturday, October 10, 2026". */
    fun wholeDate(moment: Instant, zone: ZoneId = ZoneId.systemDefault(), locale: Locale = Locale.getDefault()): String =
        moment.atZone(zone).format(DateTimeFormatter.ofLocalizedDate(FormatStyle.FULL).withLocale(locale))

    private fun time(shown: ZonedDateTime, locale: Locale): String = shown.format(DateTimeFormatter.ofLocalizedTime(FormatStyle.SHORT).withLocale(locale))
}

/** What [MailTimes.short] says, with Yesterday left to the caller's own words. */
sealed class ShortTime {
    data object None : ShortTime()
    data object Yesterday : ShortTime()
    data class Text(val text: String) : ShortTime()
}
