package com.teanode.kit.journal

import java.math.BigDecimal
import java.time.DayOfWeek
import java.time.LocalDate
import java.time.format.DateTimeParseException

// The arithmetic on the journal's days, weeks, months and years. A journal day is a calendar day written YYYY-MM-DD,
// the person's own, so it is worked on as a date and never as an instant, which is on that day in every time zone. The
// screens put the day into words for the person's calendar and language.

/** JournalDays is the arithmetic on journal days. */
object JournalDays {
    private val dayPattern = Regex("^\\d{4}-\\d{2}-\\d{2}$")

    /** Whether the text is a day written as the server takes it. */
    fun isDayText(text: String): Boolean {
        if (!dayPattern.matches(text)) return false
        val month = text.substring(5, 7).toInt()
        val day = text.substring(8, 10).toInt()
        return month in 1..12 && day in 1..31
    }

    /** The day as a date; null for text that is not a day, or a day that does not exist (the 31st of June). */
    fun date(text: String): LocalDate? {
        if (!isDayText(text)) return null
        return try {
            LocalDate.parse(text)
        } catch (_: DateTimeParseException) {
            null
        }
    }

    /** The day a date is on, written YYYY-MM-DD. */
    fun text(date: LocalDate): String = "%04d-%02d-%02d".format(date.year, date.monthValue, date.dayOfMonth)

    /** The day a number of days after another (before, when negative). */
    fun adding(dayCount: Int, text: String): String? = date(text)?.let { start -> text(start.plusDays(dayCount.toLong())) }

    /** The first day of the week a day is in. weekStartsOn is "sunday" or "monday", as the server says; anything else counts as Monday. */
    fun weekStart(text: String, weekStartsOn: String): String? {
        val start = date(text) ?: return null
        val daysSinceStart = if (weekStartsOn == "sunday") start.dayOfWeek.value % 7 else start.dayOfWeek.value - DayOfWeek.MONDAY.value
        return adding(-daysSinceStart, text)
    }

    /** The first day of the month a day is in. */
    fun monthStart(text: String): String? = if (isDayText(text)) text.take(7) + "-01" else null

    /** The first day of the year a day is in. */
    fun yearStart(text: String): String? = if (isDayText(text)) text.take(4) + "-01-01" else null

    /** The first day of the period of a kind that contains a day. */
    fun periodStart(text: String, periodKind: JournalPeriodKind, weekStartsOn: String): String? = when (periodKind) {
        JournalPeriodKind.Week -> weekStart(text, weekStartsOn)
        JournalPeriodKind.Month -> monthStart(text)
        JournalPeriodKind.Year -> yearStart(text)
    }

    /** The first day of the period after (or before, with a negative count) the one that starts on a day. */
    fun movedPeriodStart(periodStart: String, periodKind: JournalPeriodKind, count: Int): String? {
        val start = date(periodStart) ?: return null
        val moved = when (periodKind) {
            JournalPeriodKind.Week -> start.plusDays(7L * count)
            JournalPeriodKind.Month -> start.plusMonths(count.toLong())
            JournalPeriodKind.Year -> start.plusYears(count.toLong())
        }
        return text(moved)
    }

    /** The days of a month in rows of seven, starting on the day the week starts on; a place before the month's first day or after its last is null. */
    fun monthRows(monthStart: String, weekStartsOn: String): List<List<String?>> {
        val first = monthStart(monthStart) ?: return emptyList()
        val firstDate = date(first) ?: return emptyList()
        val gridStart = weekStart(first, weekStartsOn) ?: return emptyList()
        val gridStartDate = date(gridStart) ?: return emptyList()
        val leadingCount = java.time.temporal.ChronoUnit.DAYS.between(gridStartDate, firstDate).toInt()
        val places = ArrayList<String?>()
        repeat(leadingCount) { places.add(null) }
        for (offset in 0 until firstDate.lengthOfMonth()) places.add(adding(offset, first))
        while (places.size % 7 != 0) places.add(null)
        return places.chunked(7)
    }

    /** The weekday names' order for a row of a month's grid: the first week's days, so the screen can name the columns. */
    fun weekDays(weekStart: String): List<String> = (0 until 7).mapNotNull { offset -> adding(offset, weekStart) }

    /** The timeline's days, newest first, grouped by the week each is in. Days of one week stay together however the pages were cut. */
    fun weekGroups(days: List<JournalDaySummary>, weekStartsOn: String): List<JournalWeekGroup> {
        val groups = ArrayList<JournalWeekGroup>()
        for (day in days) {
            val start = weekStart(day.day, weekStartsOn) ?: day.day
            val last = groups.lastOrNull()
            if (last != null && last.weekStart == start) {
                groups[groups.size - 1] = JournalWeekGroup(start, last.days + day)
            } else {
                groups.add(JournalWeekGroup(start, listOf(day)))
            }
        }
        return groups
    }
}

/** JournalWeekGroup is the days of the timeline that fall in one week. */
data class JournalWeekGroup(
    /** The first day of the week. */
    val weekStart: String,
    /** The week's days that have anything in them, newest first. */
    val days: List<JournalDaySummary>,
) {
    val id: String get() = weekStart
}

/** JournalEveningTime is the time of day the evening question is asked, which the server writes HH:MM in the person's zone. */
object JournalEveningTime {
    const val DEFAULT_TEXT = "21:00"

    data class Parts(val hour: Int, val minute: Int)

    /** The hour and minute a time is written as; null for text that is not a time of day. */
    fun parts(text: String): Parts? {
        val pieces = text.split(":")
        if (pieces.size != 2 || pieces[0].length > 2 || pieces[1].length != 2) return null
        val hour = pieces[0].toIntOrNull() ?: return null
        val minute = pieces[1].toIntOrNull() ?: return null
        if (hour !in 0..23 || minute !in 0..59) return null
        return Parts(hour, minute)
    }

    /** The time the server takes: two digits each. */
    fun text(hour: Int, minute: Int): String = "%02d:%02d".format(hour, minute)
}

/** JournalMoney turns the server's minor units (cents) into an amount. */
object JournalMoney {
    /** How many digits a currency has after its point. */
    fun fractionDigits(currency: String): Int {
        val code = currency.uppercase()
        if (code in setOf("JPY", "KRW", "VND", "CLP", "ISK", "UGX", "XAF", "XOF", "XPF", "PYG", "RWF")) return 0
        if (code in setOf("BHD", "KWD", "OMR", "JOD", "TND", "IQD", "LYD")) return 3
        return 2
    }

    /** The amount a number of minor units stands for: 5210 cents is 52.10. */
    fun amount(minorUnits: Long, currency: String): BigDecimal = BigDecimal.valueOf(minorUnits, fractionDigits(currency))
}

/** JournalPolling is how long a day waits for what the server writes in the background: the touched-up words and the narrative. */
object JournalPolling {
    /** How often the day is read again while something is being written. */
    const val INTERVAL_MS = 3_000L

    /** How long the screen keeps asking before it gives up. */
    const val LIMIT_MS = 120_000L

    /** How many reads that is. */
    val attemptLimit: Int get() = (LIMIT_MS / INTERVAL_MS).toInt()

    /** Whether to read again: something is being written, and the screen has not yet asked as often as it will. */
    fun shouldRead(isPending: Boolean, attemptCount: Int): Boolean = isPending && attemptCount < attemptLimit
}
