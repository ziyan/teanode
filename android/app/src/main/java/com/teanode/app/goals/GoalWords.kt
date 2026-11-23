package com.teanode.app.goals

import android.content.res.Resources
import com.teanode.app.R
import com.teanode.kit.goals.AgentGoal
import com.teanode.kit.goals.GoalMailCondition
import com.teanode.kit.goals.GoalTimeLine
import com.teanode.kit.goals.GoalTiming
import com.teanode.kit.goals.GoalWeekday
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

/** How a goal says when it runs in a person's words: its times, its mail, and the next time. */
object GoalWords {
    /**
     * One of a goal's times: "Weekdays at 7:30 AM", "Fridays at 5:00 PM", "On Dec 11, 2026 at 9:00 AM"; a line of any other
     * kind is shown as it is written.
     */
    fun timeText(resources: Resources, line: String, locale: Locale = Locale.getDefault()): String =
        when (val timeLine = GoalTimeLine.of(line)) {
            is GoalTimeLine.Weekly -> weeklyText(resources, timeLine.timing, locale)
            is GoalTimeLine.Moment -> resources.getString(R.string.goals_times_moment, timeLine.dateText(locale), timeLine.timeText(locale))
            is GoalTimeLine.Other -> line
        }

    private fun weeklyText(resources: Resources, timing: GoalTiming, locale: Locale): String {
        val timeText = timing.timeText(locale)
        return when (val pattern = timing.dayPattern) {
            GoalTiming.DayPattern.EveryDay -> resources.getString(R.string.goals_times_every_day, timeText)
            GoalTiming.DayPattern.Weekdays -> resources.getString(R.string.goals_times_weekdays, timeText)
            GoalTiming.DayPattern.Weekends -> resources.getString(R.string.goals_times_weekends, timeText)
            is GoalTiming.DayPattern.OneDay -> resources.getString(R.string.goals_times_one_day, GoalWeekday.name(pattern.dayNumber, locale), timeText)
            is GoalTiming.DayPattern.SomeDays -> resources.getString(R.string.goals_times_some_days, GoalWeekday.shortNamesText(pattern.dayNumbers, locale), timeText)
        }
    }

    /** All of a goal's times in one phrase: "Mondays at 9:00 AM and on Dec 11, 2026 at 9:00 AM"; null for none. */
    fun timesText(resources: Resources, goalTimes: List<String>, locale: Locale = Locale.getDefault()): String? {
        if (goalTimes.isEmpty()) return null
        val phrases = goalTimes.mapIndexed { index, line ->
            val text = timeText(resources, line, locale)
            if (index > 0 && locale.language == "en") text.replaceFirstChar { letter -> letter.lowercase() } else text
        }
        if (phrases.size == 1) return phrases[0]
        val joined = phrases.dropLast(1).joinToString(resources.getString(R.string.goals_times_separator))
        return resources.getString(if (phrases.size == 2) R.string.goals_times_and else R.string.goals_times_and_more, joined, phrases.last())
    }

    /** The mail a goal takes a turn for: "When mail arrives: From contains news@school.example". */
    fun mailText(resources: Resources, conditions: List<GoalMailCondition>): String {
        val conditionTexts = conditions.filter { condition -> !condition.isBare }.map { condition -> conditionText(resources, condition) }
        if (conditionTexts.isEmpty()) return resources.getString(R.string.goals_mail_any)
        return resources.getString(R.string.goals_mail_when, conditionTexts.joinToString(", "))
    }

    private fun conditionText(resources: Resources, condition: GoalMailCondition): String {
        val fieldName = resources.getString(mailFieldNameResource(condition.mailField))
        val header = if (condition.mailField == "header") " " + condition.header.orEmpty() else ""
        val comparison = resources.getString(comparisonNameResource(condition.comparisonOperator))
        return "$fieldName$header $comparison ${condition.value.orEmpty()}".trim()
    }

    fun mailFieldNameResource(field: String): Int = when (field) {
        "from" -> R.string.goals_mail_field_from
        "to" -> R.string.goals_mail_field_to
        "subject" -> R.string.goals_mail_field_subject
        "header" -> R.string.goals_mail_field_header
        "score" -> R.string.goals_mail_field_score
        "sender-known" -> R.string.goals_mail_field_sender_known
        "category" -> R.string.goals_mail_field_category
        "priority" -> R.string.goals_mail_field_priority
        "needs-reply" -> R.string.goals_mail_field_needs_reply
        else -> R.string.goals_mail_field_any
    }

    fun comparisonNameResource(operator: String): Int = when (operator) {
        "equals" -> R.string.goals_mail_comparison_equals
        "matches" -> R.string.goals_mail_comparison_matches
        "above" -> R.string.goals_mail_comparison_above
        "below" -> R.string.goals_mail_comparison_below
        else -> R.string.goals_mail_comparison_contains
    }

    /** When a goal runs, a line for its times and a line for its mail; none for a goal at its own pace. */
    fun whenLines(resources: Resources, goal: AgentGoal, locale: Locale = Locale.getDefault()): List<String> =
        listOfNotNull(
            timesText(resources, goal.goalTimes, locale),
            if (goal.goalMailConditions.isEmpty()) null else mailText(resources, goal.goalMailConditions),
        )

    /** A goal's next time as a person reads it: "Tue 7:30 AM" within the week, else the day and time. */
    fun nextTimeText(next: Instant, now: Instant = Instant.now(), zone: ZoneId = ZoneId.systemDefault(), locale: Locale = Locale.getDefault()): String {
        val skeleton = if (next.isBefore(now.plusSeconds(6 * 24 * 3600L))) "EEEjmm" else "MMMdjmm"
        val pattern = android.text.format.DateFormat.getBestDateTimePattern(locale, skeleton)
        return DateTimeFormatter.ofPattern(pattern, locale).format(next.atZone(zone))
    }
}
