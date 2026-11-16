package com.teanode.kit.calendar

import java.time.DayOfWeek
import java.time.LocalDate
import java.time.LocalDateTime
import java.time.ZoneId
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import java.time.format.TextStyle
import java.util.Locale

// How an event repeats, read from and written as an RFC 5545 recurrence rule (the RRULE text without its property name,
// such as "FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE,FR;UNTIL=20261231T235959Z").
//
// The server takes any rule as text and expands it, so the editor only has to build and read the parts a person can
// choose. The dashboard has the same logic in web/src/pages/calendarRepeat.ts, tested against the same table of rules;
// change them together.

/** CalendarRepeat turns a recurrence rule into the parts the event editor shows, and back. */
object CalendarRepeat {
    /** How often it comes round. */
    enum class Frequency {
        Daily, Weekly, Monthly, Yearly;

        internal val ruleName: String get() = name.uppercase()
    }

    /** The last day of a repeat that ends on a date, or how many times it happens, or neither. */
    sealed class End {
        data object Never : End()

        /** The last day it may happen on, in the event's own zone. */
        data class OnDate(val day: CalendarDay) : End()

        data class AfterCount(val occurrenceCount: Int) : End()
    }

    /** What a monthly repeat counts by. */
    sealed class Monthly {
        data class DayOfMonth(val dayOfMonth: Int) : Monthly()

        /** The second Tuesday: the ordinal is 1 to 5, or -1 for the last; the weekday is 1 (Monday) to 7 (Sunday). */
        data class OrdinalWeekday(val ordinal: Int, val weekday: Int) : Monthly()
    }

    /** Parts is what the editor shows of a rule. */
    data class Parts(
        val frequency: Frequency,
        /** Every this many days, weeks, months or years; 1 or more. */
        val intervalCount: Int = 1,
        /** Weekly only: the days it happens on, 1 (Monday) to 7 (Sunday), in order. */
        val weekdays: List<Int>,
        /** Monthly only. */
        val monthly: Monthly,
        val end: End = End.Never,
        /**
         * The rule has something the editor cannot show (BYSETPOS, BYMONTH, several days of a month, a time of day).
         * It is kept as it is unless the person changes what the editor does show.
         */
        val hasUnshownParts: Boolean = false,
    )

    /**
     * Context is what a rule is read against: the event's own first day, the zone its days are counted in, and
     * whether it is a whole-day event, which writes its end as a date and not as a moment.
     */
    data class Context(val startDay: CalendarDay, val zone: ZoneId, val isAllDay: Boolean)

    /** Preset is a repeat the Repeat list names; anything else is Custom. */
    enum class Preset(val rule: String?) {
        Never(""),
        Daily("FREQ=DAILY"),
        Weekdays("FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"),
        Weekly("FREQ=WEEKLY"),
        Monthly("FREQ=MONTHLY"),
        Yearly("FREQ=YEARLY"),

        /** Custom has no rule of its own. */
        Custom(null);

        companion object {
            /** The preset a rule is, whatever order another program wrote its parts in. */
            fun of(rule: String): Preset {
                val normalizedRule = normalized(rule)
                return entries.firstOrNull { preset -> preset.rule?.let(::normalized) == normalizedRule } ?: Custom
            }
        }
    }

    private val weekdayCodes = listOf("MO", "TU", "WE", "TH", "FR", "SA", "SU")

    /** The ordinals a month can name a weekday by: the first to the fifth, and the last, which is -1 in a rule. */
    val monthlyOrdinals: List<Int> = listOf(1, 2, 3, 4, 5, -1)

    private val knownKeys = setOf("FREQ", "INTERVAL", "BYDAY", "BYMONTHDAY", "COUNT", "UNTIL")

    /** The day of the week of a day, 1 for Monday to 7 for Sunday. */
    fun weekday(day: CalendarDay): Int = day.toLocalDate().dayOfWeek.value

    /**
     * The ordinal of a day's weekday within its month: the second Tuesday is 2. The fifth, which not every month has,
     * is the last, -1, since that is what somebody who started on one means.
     */
    fun ordinal(day: CalendarDay): Int {
        val ordinal = (day.day + 6) / 7
        return if (ordinal == 5) -1 else ordinal
    }

    /** The same date a year on, for a repeat that has just been told to end on a date. February 29 becomes March 1. */
    fun dayOneYearAfter(day: CalendarDay): CalendarDay {
        val later = LocalDate.of(day.year, day.month, day.day).plusYears(1)
        // Adding a year to February 29 lands on February 28; the person means the day after it.
        return if (day.month == 2 && day.day == 29 && later.dayOfMonth == 28) CalendarDay.of(later.plusDays(1)) else CalendarDay.of(later)
    }

    /** The parts of a repeat that has just been chosen, such as the weekly one Custom opens on. */
    fun defaultParts(frequency: Frequency, context: Context): Parts =
        Parts(frequency, weekdays = listOf(weekday(context.startDay)), monthly = Monthly.DayOfMonth(context.startDay.day))

    private class DayEntry(val ordinalText: String, val weekday: Int)

    /**
     * The parts a rule says, to show in the editor. Null for an empty rule (it does not repeat) and for text that is
     * not a rule it can read, which the caller keeps as it is.
     */
    fun parse(rule: String, context: Context): Parts? {
        val text = rule.trim().uppercase()
        if (text.isEmpty()) return null
        val values = HashMap<String, String>()
        var hasUnshownParts = false
        for (entry in text.split(";").filter { piece -> piece.isNotEmpty() }) {
            val separator = entry.indexOf('=')
            if (separator <= 0) return null
            val key = entry.substring(0, separator).trim()
            if (values.containsKey(key)) return null
            values[key] = entry.substring(separator + 1).trim()
            if (key !in knownKeys) hasUnshownParts = true
        }
        val frequency = when (values["FREQ"]) {
            "DAILY" -> Frequency.Daily
            "WEEKLY" -> Frequency.Weekly
            "MONTHLY" -> Frequency.Monthly
            "YEARLY" -> Frequency.Yearly
            else -> return null
        }

        var parts = defaultParts(frequency, context)
        values["INTERVAL"]?.let { intervalText ->
            val intervalCount = wholeNumber(intervalText)
            if (intervalCount == null || intervalCount < 1) return null
            parts = parts.copy(intervalCount = intervalCount)
        }

        val days = ArrayList<DayEntry>()
        for (entry in (values["BYDAY"] ?: "").split(",").filter { piece -> piece.isNotEmpty() }) {
            days.add(dayEntry(entry) ?: return null)
        }
        val monthDayText = values["BYMONTHDAY"]

        when (frequency) {
            Frequency.Weekly -> {
                if (days.isNotEmpty()) {
                    if (days.any { day -> day.ordinalText.isNotEmpty() }) {
                        hasUnshownParts = true
                    } else {
                        parts = parts.copy(weekdays = days.map { day -> day.weekday }.toSet().sorted())
                    }
                }
                if (monthDayText != null) hasUnshownParts = true
            }
            Frequency.Monthly -> {
                if (days.isNotEmpty() && monthDayText != null) {
                    hasUnshownParts = true
                } else if (days.size == 1) {
                    val ordinal = days[0].ordinalText.toIntOrNull()
                    if (ordinal != null && ordinal in monthlyOrdinals) {
                        parts = parts.copy(monthly = Monthly.OrdinalWeekday(ordinal, days[0].weekday))
                    } else {
                        hasUnshownParts = true
                    }
                } else if (days.size > 1) {
                    hasUnshownParts = true
                } else if (monthDayText != null) {
                    val dayOfMonth = wholeNumber(monthDayText)
                    if (dayOfMonth != null && dayOfMonth in 1..31 && monthDayText.length <= 2) {
                        parts = parts.copy(monthly = Monthly.DayOfMonth(dayOfMonth))
                    } else {
                        hasUnshownParts = true
                    }
                }
            }
            // A daily or yearly rule narrowed to some days is not a repeat this editor can draw.
            Frequency.Daily, Frequency.Yearly -> if (days.isNotEmpty() || monthDayText != null) hasUnshownParts = true
        }

        val countText = values["COUNT"]
        val untilText = values["UNTIL"]
        if (countText != null) {
            val occurrenceCount = wholeNumber(countText)
            if (occurrenceCount == null || occurrenceCount < 1) return null
            parts = parts.copy(end = End.AfterCount(occurrenceCount))
            // A rule may have one or the other; with both, the count is shown and the rule is kept as it is.
            if (untilText != null) hasUnshownParts = true
        } else if (untilText != null) {
            parts = parts.copy(end = End.OnDate(endDayOfUntil(untilText, context) ?: return null))
        }

        return parts.copy(hasUnshownParts = hasUnshownParts)
    }

    private fun wholeNumber(text: String): Int? {
        if (text.isEmpty() || !text.all { character -> character in '0'..'9' }) return null
        return text.toIntOrNull()
    }

    /** One BYDAY entry: an optional ordinal such as "2" or "-1", then a day. */
    private fun dayEntry(entry: String): DayEntry? {
        val code = entry.takeLast(2)
        val codeIndex = weekdayCodes.indexOf(code)
        if (codeIndex < 0) return null
        val ordinalText = entry.dropLast(2)
        if (ordinalText.isNotEmpty()) {
            val digits = if (ordinalText.startsWith("+") || ordinalText.startsWith("-")) ordinalText.drop(1) else ordinalText
            if (wholeNumber(digits) == null || digits.length > 2) return null
        }
        return DayEntry(if (ordinalText.startsWith("+")) ordinalText.drop(1) else ordinalText, codeIndex + 1)
    }

    /**
     * The rule for the parts: the plainest one that means them. A part that is what the event's own start already says
     * (a weekly repeat on the weekday it starts on, a monthly one on the day it starts on) is left out.
     */
    fun write(parts: Parts, context: Context): String {
        val pieces = arrayListOf("FREQ=${parts.frequency.ruleName}")
        if (parts.intervalCount > 1) pieces.add("INTERVAL=${parts.intervalCount}")
        when (parts.frequency) {
            Frequency.Weekly -> {
                val weekdays = parts.weekdays.toSet().sorted()
                val isStartWeekdayOnly = weekdays == listOf(weekday(context.startDay))
                if (weekdays.isNotEmpty() && !isStartWeekdayOnly) {
                    pieces.add("BYDAY=" + weekdays.joinToString(",") { weekday -> weekdayCodes[weekday - 1] })
                }
            }
            Frequency.Monthly -> when (val monthly = parts.monthly) {
                is Monthly.OrdinalWeekday -> pieces.add("BYDAY=${monthly.ordinal}${weekdayCodes[monthly.weekday - 1]}")
                is Monthly.DayOfMonth -> if (monthly.dayOfMonth != context.startDay.day) pieces.add("BYMONTHDAY=${monthly.dayOfMonth}")
            }
            Frequency.Daily, Frequency.Yearly -> Unit
        }
        when (val end = parts.end) {
            End.Never -> Unit
            is End.AfterCount -> pieces.add("COUNT=${end.occurrenceCount}")
            is End.OnDate -> pieces.add("UNTIL=${untilText(end.day, context)}")
        }
        return pieces.joinToString(";")
    }

    /**
     * The rule to keep after the editor was opened on `original`, showing `initial`, and left showing `edited`. Nothing
     * changed keeps the rule exactly as it was, so that opening the editor and pressing Done never loses what another
     * program set (a part the editor cannot show, or a rule it cannot read at all).
     */
    fun editedRule(original: String, initial: Parts, edited: Parts, context: Context): String {
        if (initial == edited) return original.trim()
        return write(edited.copy(hasUnshownParts = false), context)
    }

    /** The rule with its parts in a fixed order and its days sorted, so the same repeat in another order is the same repeat. */
    internal fun normalized(rule: String): String =
        rule.trim().uppercase().split(";").filter { piece -> piece.isNotEmpty() }.map { entry ->
            val trimmed = entry.trim()
            val separator = trimmed.indexOf('=')
            if (separator < 0) return@map trimmed
            val key = trimmed.substring(0, separator)
            val value = trimmed.substring(separator + 1)
            if (key != "BYDAY") return@map "$key=$value"
            val days = value.split(",").filter { piece -> piece.isNotEmpty() }.sortedBy { code -> weekdayCodes.indexOf(code) }
            "$key=${days.joinToString(",")}"
        }.sorted().joinToString(";")

    private fun zeroPadded(number: Int, width: Int): String = number.toString().padStart(width, '0')

    /**
     * The last day as RFC 5545 asks: a date for a whole-day event, and for a timed one the end of that day in the
     * event's zone, as a moment in UTC.
     */
    private fun untilText(endDay: CalendarDay, context: Context): String {
        val dateText = zeroPadded(endDay.year, 4) + zeroPadded(endDay.month, 2) + zeroPadded(endDay.day, 2)
        if (context.isAllDay) return dateText
        val moment = LocalDateTime.of(endDay.year, endDay.month, endDay.day, 23, 59, 59).atZone(context.zone).withZoneSameInstant(ZoneOffset.UTC)
        return zeroPadded(moment.year, 4) + zeroPadded(moment.monthValue, 2) + zeroPadded(moment.dayOfMonth, 2) + "T" +
            zeroPadded(moment.hour, 2) + zeroPadded(moment.minute, 2) + zeroPadded(moment.second, 2) + "Z"
    }

    /** The day an UNTIL names, in the event's zone. */
    private fun endDayOfUntil(until: String, context: Context): CalendarDay? {
        if (until.length != 8 && until.length != 15 && until.length != 16) return null
        val year = wholeNumber(until.substring(0, 4)) ?: return null
        val month = wholeNumber(until.substring(4, 6)) ?: return null
        val day = wholeNumber(until.substring(6, 8)) ?: return null
        val dayOfUntil = CalendarDay(year, month, day)
        if (until.length == 8) return dayOfUntil
        if (until[8] != 'T') return null
        val hour = wholeNumber(until.substring(9, 11)) ?: return null
        val minute = wholeNumber(until.substring(11, 13)) ?: return null
        val second = wholeNumber(until.substring(13, 15)) ?: return null
        val isUniversal = until.length == 16
        if (isUniversal && until[15] != 'Z') return null
        // Without a Z the time is the event's own, and without a zone to move it to a whole-day event keeps the date as written.
        if (!isUniversal || context.isAllDay) return dayOfUntil
        val moment = try {
            LocalDateTime.of(year, month, day, hour, minute, second).atOffset(ZoneOffset.UTC).toInstant()
        } catch (_: Exception) {
            return null
        }
        return CalendarDay.containing(moment, context.zone)
    }

    /**
     * Sentence is what a repeat says in words, in the pieces a screen puts into its own localized sentences: the day
     * names and the date are the locale's already, and the screen only places them.
     */
    data class Sentence(val cadence: Cadence, val ending: Ending) {
        sealed class Cadence {
            data class Daily(val intervalCount: Int) : Cadence()
            data class Weekly(val intervalCount: Int, val weekdayNamesText: String) : Cadence()
            data class MonthlyDay(val intervalCount: Int, val dayOfMonth: Int) : Cadence()
            data class MonthlyWeekday(val intervalCount: Int, val ordinal: Int, val weekdayName: String) : Cadence()
            data class Yearly(val intervalCount: Int) : Cadence()
        }

        sealed class Ending {
            data object Never : Ending()
            data class OnDate(val dateText: String) : Ending()
            data class AfterCount(val occurrenceCount: Int) : Ending()
        }
    }

    /** The sentence for the parts, with day names, the list of them and the end date in the locale's words. */
    fun sentence(parts: Parts, locale: Locale = Locale.getDefault()): Sentence {
        val cadence = when (parts.frequency) {
            Frequency.Daily -> Sentence.Cadence.Daily(parts.intervalCount)
            Frequency.Yearly -> Sentence.Cadence.Yearly(parts.intervalCount)
            Frequency.Weekly -> {
                val names = parts.weekdays.toSet().sorted().map { weekday -> weekdayName(weekday, locale) }
                Sentence.Cadence.Weekly(parts.intervalCount, ListWords.join(names, locale))
            }
            Frequency.Monthly -> when (val monthly = parts.monthly) {
                is Monthly.DayOfMonth -> Sentence.Cadence.MonthlyDay(parts.intervalCount, monthly.dayOfMonth)
                is Monthly.OrdinalWeekday -> Sentence.Cadence.MonthlyWeekday(parts.intervalCount, monthly.ordinal, weekdayName(monthly.weekday, locale))
            }
        }
        val ending = when (val end = parts.end) {
            End.Never -> Sentence.Ending.Never
            is End.OnDate -> Sentence.Ending.OnDate(dateText(end.day, locale))
            is End.AfterCount -> Sentence.Ending.AfterCount(end.occurrenceCount)
        }
        return Sentence(cadence, ending)
    }

    /** The day's name in the locale, as it stands on its own: "Friday". Monday is 1. */
    fun weekdayName(weekday: Int, locale: Locale = Locale.getDefault()): String =
        DayOfWeek.of(weekday).getDisplayName(TextStyle.FULL_STANDALONE, locale)

    /** A day as the locale writes a long date: "December 31, 2026". */
    fun dateText(day: CalendarDay, locale: Locale = Locale.getDefault()): String =
        DateTimeFormatter.ofLocalizedDate(FormatStyle.LONG).withLocale(locale).format(day.toLocalDate())
}

/** ListWords joins names the way a language writes a list. */
object ListWords {
    /** "A, B, and C" in English; the languages the app is in without a conjunction write "A、B、C". */
    fun join(names: List<String>, locale: Locale): String = when (locale.language) {
        "zh", "ja" -> names.joinToString("、")
        "en" -> when (names.size) {
            0, 1, 2 -> names.joinToString(" and ")
            else -> names.dropLast(1).joinToString(", ") + ", and " + names.last()
        }
        else -> names.joinToString(", ")
    }
}
