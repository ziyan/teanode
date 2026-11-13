package com.teanode.kit.calendar

import java.time.DayOfWeek
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.temporal.WeekFields

// The month view's arithmetic: which days a month draws in which week, the week numbers, which events fall on each
// day, how many fit as chips, the color a calendar is painted, and the Chinese lunar date under a day.

/** CalendarRules is how a calendar counts: its time zone, the day a week starts on and what makes a year's first week. */
data class CalendarRules(
    val zone: ZoneId,
    val firstDayOfWeek: DayOfWeek = DayOfWeek.MONDAY,
    val minimumDaysInFirstWeek: Int = 4,
) {
    val weekFields: WeekFields get() = WeekFields.of(firstDayOfWeek, minimumDaysInFirstWeek)

    companion object {
        /** The rules the person's device has: its zone and its locale's first weekday. */
        fun current(): CalendarRules {
            val weekFields = WeekFields.of(java.util.Locale.getDefault())
            return CalendarRules(ZoneId.systemDefault(), weekFields.firstDayOfWeek, weekFields.minimalDaysInFirstWeek)
        }
    }
}

/** CalendarMonth is one month of one year. */
data class CalendarMonth(val year: Int, val month: Int) : Comparable<CalendarMonth> {
    /** Months counted from year zero, so neighbors differ by one. */
    val id: Int get() = year * 12 + (month - 1)

    fun adding(monthCount: Int): CalendarMonth = fromIndex(id + monthCount)

    /** The first day of the month. */
    val firstDay: CalendarDay get() = CalendarDay(year, month, 1)

    val dayCount: Int get() = LocalDate.of(year, month, 1).lengthOfMonth()

    /** The window the server is asked for: the first moment of the month to the first moment of the next. */
    fun window(zone: ZoneId): Pair<Instant, Instant> =
        firstDay.firstMoment(zone) to adding(1).firstDay.firstMoment(zone)

    override fun compareTo(other: CalendarMonth): Int = id.compareTo(other.id)

    companion object {
        /** A month by its index; month 13 of a year is January of the next. */
        fun fromIndex(monthIndex: Int): CalendarMonth {
            val year = Math.floorDiv(monthIndex, 12)
            return CalendarMonth(year, monthIndex - year * 12 + 1)
        }

        /** The month a moment falls in, in that zone. */
        fun containing(moment: Instant, zone: ZoneId): CalendarMonth {
            val date = moment.atZone(zone).toLocalDate()
            return CalendarMonth(date.year, date.monthValue)
        }

        /** Normalized, so month 13 is January of the next year. */
        fun normalized(year: Int, month: Int): CalendarMonth = fromIndex(year * 12 + (month - 1))
    }
}

/** CalendarDay is one day, without a time or a zone. */
data class CalendarDay(val year: Int, val month: Int, val day: Int) : Comparable<CalendarDay> {
    val calendarMonth: CalendarMonth get() = CalendarMonth(year, month)

    fun toLocalDate(): LocalDate = LocalDate.of(year, month, day)

    /** Local midnight at the start of the day. */
    fun firstMoment(zone: ZoneId): Instant = toLocalDate().atStartOfDay(zone).toInstant()

    fun plusDays(dayCount: Long): CalendarDay = of(toLocalDate().plusDays(dayCount))

    override fun compareTo(other: CalendarDay): Int =
        compareValuesBy(this, other, CalendarDay::year, CalendarDay::month, CalendarDay::day)

    companion object {
        fun of(date: LocalDate): CalendarDay = CalendarDay(date.year, date.monthValue, date.dayOfMonth)

        /** The day a moment falls on, in that zone. */
        fun containing(moment: Instant, zone: ZoneId): CalendarDay = of(moment.atZone(zone).toLocalDate())
    }
}

/**
 * MonthLayout is a month as rows of weeks: each row seven places from the calendar's first weekday, the places outside
 * the month left empty, and the week's number.
 */
data class MonthLayout(val month: CalendarMonth, val weeks: List<Week>, val firstDayColumn: Int) {
    data class Week(
        /** The week of the year, by the calendar's own rule for the first week. */
        val weekNumber: Int,
        /** Seven places; null where the day belongs to another month. */
        val days: List<CalendarDay?>,
    )

    companion object {
        fun of(month: CalendarMonth, rules: CalendarRules): MonthLayout {
            val first = month.firstDay.toLocalDate()
            val leading = Math.floorMod(first.dayOfWeek.value - rules.firstDayOfWeek.value, 7)
            val places = ArrayList<CalendarDay?>()
            repeat(leading) { places.add(null) }
            for (dayOfMonth in 1..month.dayCount) places.add(CalendarDay(month.year, month.month, dayOfMonth))
            repeat((7 - places.size % 7) % 7) { places.add(null) }
            val weeks = places.chunked(7).map { days ->
                // Every day of a row is in the same week, since rows start on the calendar's first weekday.
                val inMonth = days.filterNotNull().first()
                Week(inMonth.toLocalDate().get(rules.weekFields.weekOfWeekBasedYear()), days)
            }
            return MonthLayout(month, weeks, leading)
        }
    }
}

/**
 * The first and last day an event is on in this time zone. An all-day event covers its own days, read from UTC parts; a
 * timed one runs from its start's day to its end's, and an end at midnight belongs to the day before, as nothing of
 * the event happens on the next.
 */
fun CalendarDates.dayRange(event: CalendarEvent, zone: ZoneId): ClosedRange<CalendarDay>? {
    if (event.allDay) {
        val first = dayOfAllDay(event.startsAt) ?: return null
        val last = lastDay(event.startsAt, event.endsAt) ?: first
        return first..maxOf(first, last)
    }
    val start = ServerTime.instant(event.startsAt) ?: return null
    val first = CalendarDay.containing(start, zone)
    val end = ServerTime.instant(event.endsAt)
    if (end == null || !end.isAfter(start)) return first..first
    val last = CalendarDay.containing(end.minusSeconds(1), zone)
    return first..maxOf(first, last)
}

/**
 * The events on each day of a month, every day an event covers: all-day ones first, then by when they start. Days of
 * other months are left out, so a month read again replaces only its own days.
 */
fun CalendarDates.eventsByDay(events: List<CalendarEvent>, month: CalendarMonth, zone: ZoneId): Map<CalendarDay, List<CalendarEvent>> {
    val monthStart = CalendarDay(month.year, month.month, 1)
    val monthEnd = CalendarDay(month.year, month.month, month.dayCount)
    val found = HashMap<CalendarDay, MutableList<CalendarEvent>>()
    for (event in events) {
        val range = dayRange(event, zone) ?: continue
        if (range.endInclusive < monthStart || range.start > monthEnd) continue
        val firstDay = if (range.start < monthStart) 1 else range.start.day
        val lastDay = if (range.endInclusive > monthEnd) month.dayCount else range.endInclusive.day
        for (dayOfMonth in firstDay..lastDay) {
            found.getOrPut(CalendarDay(month.year, month.month, dayOfMonth)) { ArrayList() }.add(event)
        }
    }
    return found.mapValues { entry -> entry.value.sortedWith(shownOrder) }
}

/** The order a day's events are shown in: all-day first, then by start, then by title. */
val shownOrder: Comparator<CalendarEvent> = Comparator { left, right ->
    if (left.allDay != right.allDay) return@Comparator if (left.allDay) -1 else 1
    val leftStart = ServerTime.instant(left.startsAt) ?: Instant.MIN
    val rightStart = ServerTime.instant(right.startsAt) ?: Instant.MIN
    if (leftStart != rightStart) return@Comparator leftStart.compareTo(rightStart)
    left.summary.orEmpty().compareTo(right.summary.orEmpty())
}

/** EventChipSplit is how a day cell spends its lines: as many chips as fit, and when there are more events than lines, the last line says "+N". */
data class EventChipSplit(val chipCount: Int, val moreCount: Int) {
    companion object {
        /** `lineCount` is how many lines the cell has for chips and the "+N". */
        fun of(eventCount: Int, lineCount: Int): EventChipSplit {
            val lines = maxOf(1, lineCount)
            if (eventCount <= lines) return EventChipSplit(maxOf(0, eventCount), 0)
            val chips = lines - 1
            return EventChipSplit(chips, eventCount - chips)
        }
    }
}

/** CalendarColorComponents is a color as red, green and blue from 0 to 1. */
data class CalendarColorComponents(val red: Double, val green: Double, val blue: Double) {
    companion object {
        /** Reads "#rrggbb" or "#rgb", as the server keeps a calendar's colour; null for anything else. */
        fun fromHexText(hexText: String?): CalendarColorComponents? {
            var text = hexText?.trim().orEmpty()
            if (text.isEmpty()) return null
            if (text.startsWith("#")) text = text.drop(1)
            if (text.length == 3) text = text.map { digit -> "$digit$digit" }.joinToString("")
            if (text.length != 6 && text.length != 8) return null
            val packed = text.take(6).toLongOrNull(16) ?: return null
            return CalendarColorComponents(
                ((packed shr 16) and 0xFF) / 255.0,
                ((packed shr 8) and 0xFF) / 255.0,
                (packed and 0xFF) / 255.0,
            )
        }
    }
}

/** CalendarPalette picks a calendar's color from a small palette when the server keeps none: the same calendar always gets the same one. */
object CalendarPalette {
    /** How many colors the server spreads calendars over: the dashboard's `calendar-tone-0` to `calendar-tone-7`. */
    const val SPREAD_COLOR_COUNT = 8

    /** The place in the spread of colors for the index the server gave a calendar, kept inside the eight whatever it sent. */
    fun spreadIndex(serverIndex: Int): Int = Math.floorMod(serverIndex, SPREAD_COLOR_COUNT)

    /** A place in a palette of `paletteCount` colors, from the calendar's id, hashed by FNV-1a so it never changes. */
    fun index(calendarId: String, paletteCount: Int): Int {
        if (paletteCount <= 0) return 0
        var hash = 0xcbf29ce484222325uL.toLong()
        for (byte in calendarId.toByteArray(Charsets.UTF_8)) {
            hash = hash xor (byte.toLong() and 0xFF)
            hash *= 0x100000001b3L
        }
        return java.lang.Long.remainderUnsigned(hash, paletteCount.toLong()).toInt()
    }
}

/**
 * LunarDates writes a day's date in the Chinese lunar calendar, as China's calendars print it under the day: 初一 to
 * 三十, and the month's name on its first day. The conversion from a Gregorian day needs a Chinese calendar, which the
 * app has and this module does not, so the app finds the lunar month and day and this names them.
 */
object LunarDates {
    private val dayNames = listOf(
        "初一", "初二", "初三", "初四", "初五", "初六", "初七", "初八", "初九", "初十",
        "十一", "十二", "十三", "十四", "十五", "十六", "十七", "十八", "十九", "二十",
        "廿一", "廿二", "廿三", "廿四", "廿五", "廿六", "廿七", "廿八", "廿九", "三十",
    )
    private val monthNames = listOf("正月", "二月", "三月", "四月", "五月", "六月", "七月", "八月", "九月", "十月", "冬月", "腊月")

    /** The name shown for a lunar day: its month's name on the first day of a month, with 闰 before a leap month, and the day's name otherwise. */
    fun name(month: Int, day: Int, isLeapMonth: Boolean): String {
        if (day == 1 && month in 1..12) return (if (isLeapMonth) "闰" else "") + monthNames[month - 1]
        if (day !in 1..30) return ""
        return dayNames[day - 1]
    }
}
