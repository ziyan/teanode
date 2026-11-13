package com.teanode.kit.agent

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

/** One condition of a mail-triggered schedule: a field of the message, how it is compared, and with what. */
@Serializable
data class ScheduleMailCondition(
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

/** What a schedule that runs when mail arrives is made of. */
object ScheduleMailTrigger {
    val fields = listOf("from", "to", "subject", "header", "any")

    /** Whether the conditions can be saved: there is one, and each that needs a value has it (and a header name for a header). */
    fun isComplete(conditions: List<ScheduleMailCondition>): Boolean {
        if (conditions.isEmpty()) return false
        return conditions.all { condition ->
            if (condition.isBare) return@all true
            if (condition.value.orEmpty().isBlank()) return@all false
            condition.mailField != "header" || !condition.header.isNullOrBlank()
        }
    }

    fun variables(conditions: List<ScheduleMailCondition>): JsonElement = JsonArray(conditions.map { condition -> condition.variables })
}

/** The morning brief: a schedule of a fixed name, with its time and days read from its cron line. */
object DailyBrief {
    const val SCHEDULE_NAME = "Daily brief"
    private const val DEFAULT_CRON = "30 7 * * 1-5"

    fun schedule(schedules: List<AgentSchedule>): AgentSchedule? = schedules.firstOrNull { schedule -> schedule.name == SCHEDULE_NAME }

    private fun fields(cron: String?): List<String> = (cron ?: DEFAULT_CRON).split(' ').filter { field -> field.isNotEmpty() }

    /** The time of day a brief goes out, "HH:MM". */
    fun time(cron: String?): String {
        val fields = fields(cron)
        val hour = if (fields.size > 1) fields[1] else "7"
        val minute = if (fields.isNotEmpty()) fields[0] else "30"
        return hour.padStart(2, '0') + ":" + minute.padStart(2, '0')
    }

    /** The days a brief goes out on (1 is Monday); Monday to Friday when the cron line says none. */
    fun dayNumbers(cron: String?): List<Int> {
        val fields = fields(cron)
        if (fields.size != 5) return (1..5).toList()
        return ScheduleWeekday.dayNumbers(fields[4])?.takeIf { days -> days.isNotEmpty() } ?: (1..5).toList()
    }
}

/** Days of the week as the schedules number them: 1 is Monday and 7 is Sunday, where cron's 0 is Sunday too. */
object ScheduleWeekday {
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

    /** A cron day-of-week field for these day numbers: `*` for all, else a list. */
    fun cronField(dayNumbers: List<Int>): String {
        val days = dayNumbers.distinct().sorted()
        return if (days == allDayNumbers) "*" else days.joinToString(",") { day -> if (day == 7) "0" else day.toString() }
    }
}

/** A weekly time as a person sets it: an hour and minute, on some days. Cron lines that are anything else are not a timing. */
data class ScheduleTiming(val hour: Int, val minute: Int, val dayNumbers: List<Int>) {
    sealed interface DayPattern {
        data object EveryDay : DayPattern
        data object Weekdays : DayPattern
        data object Weekends : DayPattern
        data class OneDay(val dayNumber: Int) : DayPattern
        data class SomeDays(val dayNumbers: List<Int>) : DayPattern
    }

    val dayPattern: DayPattern get() = dayPattern(dayNumbers)

    /** The cron line for this timing. */
    val cron: String get() = "$minute $hour * * ${ScheduleWeekday.cronField(dayNumbers)}"

    companion object {
        fun of(cron: String): ScheduleTiming? {
            val fields = cron.split(' ').filter { field -> field.isNotEmpty() }
            if (fields.size != 5 || fields[2] != "*" || fields[3] != "*") return null
            if (!fields[0].all { character -> character in '0'..'9' } || !fields[1].all { character -> character in '0'..'9' }) return null
            val minute = fields[0].toIntOrNull() ?: return null
            val hour = fields[1].toIntOrNull() ?: return null
            if (minute !in 0..59 || hour !in 0..23) return null
            val days = ScheduleWeekday.dayNumbers(fields[4]) ?: return null
            return ScheduleTiming(hour, minute, days)
        }

        fun dayPattern(dayNumbers: List<Int>): DayPattern = when {
            dayNumbers == ScheduleWeekday.allDayNumbers -> DayPattern.EveryDay
            dayNumbers == (1..5).toList() -> DayPattern.Weekdays
            dayNumbers == listOf(6, 7) -> DayPattern.Weekends
            dayNumbers.size == 1 -> DayPattern.OneDay(dayNumbers[0])
            else -> DayPattern.SomeDays(dayNumbers)
        }
    }
}
