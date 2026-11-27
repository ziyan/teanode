package com.teanode.kit.goals

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

/** One condition of a goal that also takes a turn when mail arrives: a field of the message, how it is compared, and with what. */
@Serializable
data class GoalMailCondition(
    @SerialName("field") val mailField: String = "from",
    val header: String? = null,
    @SerialName("operator") val comparisonOperator: String = "contains",
    val value: String? = "",
) {
    /** Whether it asks nothing of a value ("any mail", "needs a reply", "a known sender"). */
    val isBare: Boolean get() = bareFields.contains(mailField)

    val variables: JsonElement
        get() {
            val fields = linkedMapOf<String, JsonElement>("field" to JsonPrimitive(mailField), "operator" to JsonPrimitive(comparisonOperator))
            header?.let { fields["header"] = JsonPrimitive(it) }
            value?.let { fields["value"] = JsonPrimitive(it) }
            return JsonObject(fields)
        }

    companion object {
        val bareFields = setOf("sender-known", "needs-reply", "any")
    }
}

/** What a goal that takes a turn when mail arrives is made of. */
object GoalMailTrigger {
    val fields = listOf("from", "to", "subject", "header", "any")

    /** Whether the conditions can be saved: there is one, and each that needs a value has it (and a header name for a header). */
    fun isComplete(conditions: List<GoalMailCondition>): Boolean {
        if (conditions.isEmpty()) return false
        return conditions.all { condition ->
            if (condition.isBare) return@all true
            if (condition.value.orEmpty().isBlank()) return@all false
            condition.mailField != "header" || !condition.header.isNullOrBlank()
        }
    }

    fun variables(conditions: List<GoalMailCondition>): JsonElement = JsonArray(conditions.map { condition -> condition.variables })
}

/** Days of the week as the goals number them: 1 is Monday and 7 is Sunday, where cron's 0 is Sunday too. */
object GoalWeekday {
    val allDayNumbers: List<Int> = (1..7).toList()

    /** The day numbers a cron day-of-week field stands for; null when it is not a list of days and ranges. */
    fun dayNumbers(cronField: String): List<Int>? {
        if (cronField == "*") return allDayNumbers
        val numbers = sortedSetOf<Int>()
        for (piece in cronField.split(',')) {
            val ends = piece.split('-')
            if (ends.size !in 1..2) return null
            val low = ends.first().toIntOrNull() ?: return null
            val high = ends.last().toIntOrNull() ?: return null
            if (low !in 0..7 || high !in 0..7 || low > high) return null
            for (cronDay in low..high) numbers.add(if (cronDay == 0) 7 else cronDay)
        }
        return numbers.toList()
    }

    /** A day's name as the locale writes it: "Friday". */
    fun name(dayNumber: Int, locale: java.util.Locale = java.util.Locale.getDefault()): String =
        java.time.DayOfWeek.of(dayNumber).getDisplayName(java.time.format.TextStyle.FULL, locale)

    /** Some days' short names in week order and in a list the locale's way: "Mon, Wed, and Fri", "Mon and Fri". */
    fun shortNamesText(dayNumbers: List<Int>, locale: java.util.Locale = java.util.Locale.getDefault()): String {
        val names = dayNumbers.distinct().sorted().map { day -> java.time.DayOfWeek.of(day).getDisplayName(java.time.format.TextStyle.SHORT, locale) }
        return when (locale.language) {
            "en" -> when (names.size) {
                0, 1 -> names.joinToString("")
                2 -> names.joinToString(" and ")
                else -> names.dropLast(1).joinToString(", ") + ", and " + names.last()
            }
            "ja", "zh" -> names.joinToString("\u3001")
            else -> names.joinToString(", ")
        }
    }

    /** A cron day-of-week field for these day numbers: `*` for all, else a list. */
    fun cronField(dayNumbers: List<Int>): String {
        val days = dayNumbers.distinct().sorted()
        return if (days == allDayNumbers) "*" else days.joinToString(",") { day -> if (day == 7) "0" else day.toString() }
    }
}

/** A weekly time as a person sets it: an hour and minute, on some days. Cron lines that are anything else are not a timing. */
data class GoalTiming(val hour: Int, val minute: Int, val dayNumbers: List<Int>) {
    sealed interface DayPattern {
        data object EveryDay : DayPattern
        data object Weekdays : DayPattern
        data object Weekends : DayPattern
        data class OneDay(val dayNumber: Int) : DayPattern
        data class SomeDays(val dayNumbers: List<Int>) : DayPattern
    }

    val dayPattern: DayPattern get() = dayPattern(dayNumbers)

    /** The time of day as the locale writes it: "7:30 AM" or "07:30". */
    fun timeText(locale: java.util.Locale = java.util.Locale.getDefault()): String =
        java.time.format.DateTimeFormatter.ofLocalizedTime(java.time.format.FormatStyle.SHORT).withLocale(locale).format(java.time.LocalTime.of(hour, minute))

    /** The cron line for this timing. */
    val cron: String get() = "$minute $hour * * ${GoalWeekday.cronField(dayNumbers)}"

    companion object {
        fun of(cron: String): GoalTiming? {
            val fields = cron.split(' ').filter { field -> field.isNotEmpty() }
            if (fields.size != 5 || fields[2] != "*" || fields[3] != "*") return null
            if (!fields[0].all { character -> character in '0'..'9' } || !fields[1].all { character -> character in '0'..'9' }) return null
            val minute = fields[0].toIntOrNull() ?: return null
            val hour = fields[1].toIntOrNull() ?: return null
            if (minute !in 0..59 || hour !in 0..23) return null
            val days = GoalWeekday.dayNumbers(fields[4]) ?: return null
            return GoalTiming(hour, minute, days)
        }

        fun dayPattern(dayNumbers: List<Int>): DayPattern = when {
            dayNumbers == GoalWeekday.allDayNumbers -> DayPattern.EveryDay
            dayNumbers == (1..5).toList() -> DayPattern.Weekdays
            dayNumbers == listOf(6, 7) -> DayPattern.Weekends
            dayNumbers.size == 1 -> DayPattern.OneDay(dayNumbers[0])
            else -> DayPattern.SomeDays(dayNumbers)
        }
    }
}

/**
 * One of a goal's times as the editor holds it: a weekly time, a single moment, or a line it cannot take apart, which
 * is kept as it was written.
 */
sealed interface GoalTimeLine {
    /** What goes to the server: a cron line or `@at yyyy-MM-dd HH:mm`. */
    val line: String

    data class Weekly(val timing: GoalTiming) : GoalTimeLine {
        override val line: String get() = timing.cron
    }

    data class Moment(val date: java.time.LocalDate, val hour: Int, val minute: Int) : GoalTimeLine {
        override val line: String get() = "@at " + date + " " + "%02d:%02d".format(hour, minute)

        /** The day as the locale writes it: "Dec 11, 2026". */
        fun dateText(locale: java.util.Locale = java.util.Locale.getDefault()): String =
            java.time.format.DateTimeFormatter.ofLocalizedDate(java.time.format.FormatStyle.MEDIUM).withLocale(locale).format(date)

        /** The time of day as the locale writes it: "9:00 AM". */
        fun timeText(locale: java.util.Locale = java.util.Locale.getDefault()): String =
            java.time.format.DateTimeFormatter.ofLocalizedTime(java.time.format.FormatStyle.SHORT).withLocale(locale).format(java.time.LocalTime.of(hour, minute))
    }

    data class Other(override val line: String) : GoalTimeLine

    companion object {
        private val momentPattern = Regex("""^@at (\d{4}-\d{2}-\d{2}) (\d{2}):(\d{2})$""")

        fun of(line: String): GoalTimeLine {
            GoalTiming.of(line)?.let { timing -> return Weekly(timing) }
            val match = momentPattern.matchEntire(line.trim())
            if (match != null) {
                val date = runCatching { java.time.LocalDate.parse(match.groupValues[1]) }.getOrNull()
                val hour = match.groupValues[2].toInt()
                val minute = match.groupValues[3].toInt()
                if (date != null && hour in 0..23 && minute in 0..59) return Moment(date, hour, minute)
            }
            return Other(line)
        }

        /** The weekly time a new row starts as. */
        fun newWeekly(): GoalTimeLine = Weekly(GoalTiming(8, 0, (1..5).toList()))
    }
}
