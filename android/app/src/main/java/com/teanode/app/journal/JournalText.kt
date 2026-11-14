package com.teanode.app.journal

import android.content.Context
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import com.teanode.app.R
import com.teanode.app.calendar.DateWords
import com.teanode.kit.calendar.ServerTime
import com.teanode.kit.journal.JournalDays
import com.teanode.kit.journal.JournalDaySummary
import com.teanode.kit.journal.JournalMoney
import com.teanode.kit.journal.JournalMoodLevel
import com.teanode.kit.journal.JournalPeriodKind
import com.teanode.kit.journal.JournalSearchHit
import com.teanode.kit.journal.JournalShoppingTrip
import com.teanode.kit.journal.JournalWeatherKind
import com.teanode.kit.journal.JournalWorkProject
import java.text.NumberFormat
import java.time.LocalDate
import java.time.ZoneId
import java.util.Currency
import java.util.Locale

/** JournalZoom is how much of the journal one screen covers, as the menu under the title offers it. */
enum class JournalZoom(val periodKind: JournalPeriodKind?) {
    Days(null), Weeks(JournalPeriodKind.Week), Months(JournalPeriodKind.Month), Years(JournalPeriodKind.Year);

    companion object {
        fun of(periodKind: JournalPeriodKind): JournalZoom = entries.first { zoom -> zoom.periodKind == periodKind }
    }
}

/** The color a mood is drawn in: cool for the low ones, warm green for the good ones. */
fun journalMoodColor(level: JournalMoodLevel): Color = when (level) {
    JournalMoodLevel.Awful -> Color(0xFF5856D6)
    JournalMoodLevel.Low -> Color(0xFF3B82F6)
    JournalMoodLevel.Okay -> Color(0xFF8E8E93)
    JournalMoodLevel.Good -> Color(0xFF14B8A6)
    JournalMoodLevel.Great -> Color(0xFF34C759)
}

/** The color of a mood number the server sent as one of a month's days: null for 0, a day with no mood. */
fun journalDayMoodColor(dayMood: Int): Color? = JournalMoodLevel.of(dayMood)?.let(::journalMoodColor)

/** The color of one stage of sleep. */
fun journalSleepStageColor(sleepStage: String): Color = when (sleepStage) {
    "deep" -> Color(0xFF5856D6)
    "core" -> Color(0xFF3B82F6)
    "rem" -> Color(0xFF22B8CF)
    "awake" -> Color(0xFFF08A24)
    else -> Color(0xFF8E8E93)
}

/**
 * JournalText is the words the journal's screens share. The days are the person's own calendar days, so a day is put
 * into words with the phone's calendar and language, never as the instant it begins (ios JournalText.swift).
 */
class JournalText(private val context: Context, private val locale: Locale = Locale.getDefault(), private val zone: ZoneId = ZoneId.systemDefault()) {
    private fun string(resource: Int, vararg arguments: Any): String = context.getString(resource, *arguments)

    // MARK: Mood

    fun moodName(level: JournalMoodLevel): String = string(
        when (level) {
            JournalMoodLevel.Awful -> R.string.journal_mood_awful
            JournalMoodLevel.Low -> R.string.journal_mood_low
            JournalMoodLevel.Okay -> R.string.journal_mood_okay
            JournalMoodLevel.Good -> R.string.journal_mood_good
            JournalMoodLevel.Great -> R.string.journal_mood_great
        },
    )

    // MARK: Weather

    fun weatherName(kind: JournalWeatherKind?): String = when (kind) {
        JournalWeatherKind.Clear -> string(R.string.journal_weather_clear)
        JournalWeatherKind.PartlyCloudy -> string(R.string.journal_weather_partly_cloudy)
        JournalWeatherKind.Cloudy -> string(R.string.journal_weather_cloudy)
        JournalWeatherKind.Fog -> string(R.string.journal_weather_fog)
        JournalWeatherKind.Drizzle -> string(R.string.journal_weather_drizzle)
        JournalWeatherKind.Rain -> string(R.string.journal_weather_rain)
        JournalWeatherKind.Snow -> string(R.string.journal_weather_snow)
        JournalWeatherKind.Showers -> string(R.string.journal_weather_showers)
        JournalWeatherKind.Thunderstorm -> string(R.string.journal_weather_thunderstorms)
        null -> ""
    }

    /** Whether the phone's region measures in miles and degrees Fahrenheit. */
    private val isUs: Boolean get() = locale.country in setOf("US", "LR", "MM")

    /** A temperature in the units of the phone's region, from the server's Celsius: "58°". */
    fun temperatureText(celsius: Double): String {
        val degrees = if (isUs) celsius * 9 / 5 + 32 else celsius
        return Math.round(degrees).toString() + "°"
    }

    /** "58° / 46°, rain". */
    fun weatherLine(highCelsius: Double, lowCelsius: Double, kind: JournalWeatherKind?): String {
        val range = temperatureText(highCelsius) + " / " + temperatureText(lowCelsius)
        val name = weatherName(kind)
        return if (name.isEmpty()) range else "$range, $name"
    }

    // MARK: Days

    private fun date(day: String): LocalDate? = JournalDays.date(day)

    /** "Thursday, October 22". */
    fun longDayText(day: String): String = date(day)?.let { found -> DateWords.weekdayMonthWideDay(found, locale) } ?: day

    /** "Thu". */
    fun weekdayText(day: String): String = date(day)?.let { found -> found.dayOfWeek.getDisplayName(java.time.format.TextStyle.SHORT, locale) }.orEmpty()

    /** "T", the first letter of a weekday, for the head of a column. */
    fun weekdayInitialText(day: String): String = date(day)?.let { found -> DateWords.weekdayNarrow(found, locale) }.orEmpty()

    /** "22". */
    fun dayNumberText(day: String): String = date(day)?.dayOfMonth?.toString().orEmpty()

    /** "October 22". */
    fun monthDayText(day: String): String = date(day)?.let { found -> DateWords.monthDay(found, locale) } ?: day

    /** "October 2026". */
    fun monthText(day: String): String = date(day)?.let { found -> DateWords.monthYear(found, locale) } ?: day

    /** "2026". */
    fun yearText(day: String): String = date(day)?.let { found -> DateWords.year(found, locale) } ?: day

    /** "March 1 to May 31". */
    fun dayRangeText(firstDay: String, lastDay: String): String {
        val first = date(firstDay)
        val last = date(lastDay)
        if (first == null || last == null || !first.isBefore(last)) return monthDayText(firstDay)
        return string(R.string.journal_time_range, monthDayText(firstDay), monthDayText(lastDay))
    }

    /** "Week of October 19". */
    fun weekTitle(weekStart: String): String = string(R.string.journal_week_of, monthDayText(weekStart))

    /** "Saturday", the title of a day's page, with the date under it. */
    fun dayTitle(day: String): String = date(day)?.dayOfWeek?.getDisplayName(java.time.format.TextStyle.FULL, locale) ?: day

    /** "October 24" under a day's title, with the year when it is not this year's. */
    fun daySubtitle(day: String, today: String): String {
        val found = date(day) ?: return ""
        if (day.take(4) == today.take(4)) return monthDayText(day)
        return DateWords.weekdayMonthDayYear(found, locale).substringAfter(", ", DateWords.abbreviatedDate(found, locale))
    }

    /** The title of a period page: "Week", "October 2026" or "2026". */
    fun periodTitle(periodKind: JournalPeriodKind, periodStart: String): String = when (periodKind) {
        JournalPeriodKind.Week -> string(R.string.journal_week)
        JournalPeriodKind.Month -> monthText(periodStart)
        JournalPeriodKind.Year -> yearText(periodStart)
    }

    /** "Oct 19 to 25" under a week's title; nothing under a month's or a year's, whose titles say it all. */
    fun periodSubtitle(periodKind: JournalPeriodKind, periodStart: String): String {
        if (periodKind != JournalPeriodKind.Week || date(periodStart) == null) return ""
        val end = JournalDays.adding(6, periodStart) ?: return ""
        return string(R.string.journal_time_range, monthDayText(periodStart), monthDayText(end))
    }

    // MARK: Shopping and search

    /** "Shopping at Corner Market", or "Shopping" for a trip with no store. */
    fun shoppingTitle(storeName: String): String {
        val trimmed = storeName.trim()
        return if (trimmed.isEmpty()) string(R.string.journal_shopping) else string(R.string.journal_shopping_at, trimmed)
    }

    /** "7:10 PM to 7:50 PM" and, under it, "for Groceries and Household". */
    fun shoppingDetail(trip: JournalShoppingTrip): String {
        val lines = ArrayList<String>()
        val times = timeRangeText(trip.startedAt, trip.finishedAt)
        if (times.isNotEmpty()) lines.add(times)
        if (trip.listNames.isNotEmpty()) lines.add(string(R.string.journal_for_lists, com.teanode.kit.calendar.ListWords.join(trip.listNames, locale)))
        return lines.joinToString("\n")
    }

    /** The title of a search hit: its day, or its week, month or year. */
    fun searchHitTitle(hit: JournalSearchHit): String = when (hit.kind) {
        null -> longDayText(hit.day)
        JournalPeriodKind.Week -> weekTitle(hit.day)
        JournalPeriodKind.Month -> monthText(hit.day)
        JournalPeriodKind.Year -> yearText(hit.day)
    }

    // MARK: Times and amounts

    /** "9:05 AM", from the server's time. */
    fun timeText(time: String?): String = ServerTime.instant(time)?.let { moment -> DateWords.shortTime(moment, zone, locale) }.orEmpty()

    /** "9:05 AM to 5:20 PM", or just the first when there is no second. */
    fun timeRangeText(start: String?, end: String?): String {
        val startText = timeText(start)
        val endText = timeText(end)
        if (startText.isEmpty()) return endText
        if (endText.isEmpty()) return startText
        return string(R.string.journal_time_range, startText, endText)
    }

    /** "6 h 10 min". */
    fun durationText(minutes: Int): String {
        val hours = minutes / 60
        val rest = minutes % 60
        return when {
            hours > 0 && rest > 0 -> string(R.string.journal_duration_hours_minutes, hours, rest)
            hours > 0 -> string(R.string.journal_duration_hours, hours)
            else -> string(R.string.journal_duration_minutes, rest)
        }
    }

    fun durationText(milliseconds: Long): String = durationText((milliseconds / 60_000).toInt())

    /** "6 h 10 min, 3 projects": the time spent coding and how many projects it was spread over. Never the number of sessions. */
    fun workText(minutes: Int, projectCount: Int): String {
        val parts = ArrayList<String>()
        if (minutes > 0) parts.add(durationText(minutes))
        if (projectCount > 0) parts.add(context.resources.getQuantityString(R.plurals.journal_projects, projectCount, projectCount))
        return parts.joinToString(", ")
    }

    /** "2 h 10 min, 9:20 AM to 5:05 PM": how long a project took and when. */
    fun projectTimeText(project: JournalWorkProject): String {
        val parts = ArrayList<String>()
        if (project.activeMinutes > 0) parts.add(durationText(project.activeMinutes))
        val range = timeRangeText(project.firstAt, if (project.lastAt == project.firstAt) null else project.lastAt)
        if (range.isNotEmpty()) parts.add(range)
        return parts.joinToString(", ")
    }

    /** "$52.10", from minor units. */
    fun moneyText(minorUnits: Long, currency: String): String {
        val amount = JournalMoney.amount(minorUnits, currency)
        if (currency.isEmpty()) return NumberFormat.getNumberInstance(locale).format(amount)
        return try {
            val format = NumberFormat.getCurrencyInstance(locale)
            format.currency = Currency.getInstance(currency.uppercase())
            format.format(amount)
        } catch (_: IllegalArgumentException) {
            NumberFormat.getNumberInstance(locale).format(amount) + " " + currency
        }
    }

    /** "3.4 mi" or "5.4 km", in the units of the phone's region. */
    fun distanceText(meters: Double): String {
        val format = NumberFormat.getNumberInstance(locale).apply { maximumFractionDigits = 1; minimumFractionDigits = 0 }
        return if (isUs) {
            format.format(meters / 1609.344) + " mi"
        } else if (meters < 1000) {
            format.format(meters) + " m"
        } else {
            format.format(meters / 1000) + " km"
        }
    }

    fun countText(count: Int): String = NumberFormat.getIntegerInstance(locale).format(count)

    /** The short line a timeline row says under its headline: the high temperature, then the one number about the day most likely to mean something. */
    fun factsLine(summary: JournalDaySummary): String {
        val parts = ArrayList<String>()
        summary.weather?.let { weather -> parts.add(temperatureText(weather.highCelsius)) }
        summary.facts?.let { facts ->
            when {
                facts.walkedMeters >= 2000 -> parts.add(string(R.string.journal_walked, distanceText(facts.walkedMeters)))
                facts.photoCount > 0 -> parts.add(context.resources.getQuantityString(R.plurals.journal_photos_count, facts.photoCount, facts.photoCount))
                facts.spentMinorUnits > 0 -> parts.add(moneyText(facts.spentMinorUnits, facts.spentCurrency))
                facts.sleepMinutes > 0 -> parts.add(string(R.string.journal_sleep_duration, durationText(facts.sleepMinutes)))
                facts.stepCount > 0 -> parts.add(string(R.string.journal_steps_count, facts.stepCount))
            }
        }
        return parts.joinToString(", ")
    }

    /** "3 stretches of work". */
    fun stretchesOfWork(count: Int): String = context.resources.getQuantityString(R.plurals.journal_stretches_of_work, count, count)

    // MARK: Sleep

    fun sleepStageName(sleepStage: String): String = when (sleepStage) {
        "deep" -> string(R.string.journal_sleep_deep)
        "core" -> string(R.string.journal_sleep_core)
        "rem" -> string(R.string.journal_sleep_rem)
        "awake" -> string(R.string.journal_sleep_awake)
        "inBed" -> string(R.string.journal_sleep_in_bed)
        else -> sleepStage
    }

}

/** The journal's words for the phone's language, made once for a screen. */
@Composable
fun rememberJournalText(): JournalText {
    val context = LocalContext.current
    return remember(context) { JournalText(context) }
}

/** The symbol that stands for a kind of record. */
fun journalRecordIcon(recordKind: String): ImageVector = com.teanode.app.journal.JournalIcons.forRecordKind(recordKind)
