package com.teanode.app.settings

import android.content.res.Resources
import com.teanode.app.R
import com.teanode.kit.agent.ScheduleMailCondition
import com.teanode.kit.agent.ScheduleTiming
import com.teanode.kit.agent.ScheduleWeekday
import java.util.Locale

/** How the agent's upkeep pages say schedules and the memory check in a person's words. */
object UpkeepWords {
    /**
     * When a schedule comes round, "Weekdays at 7:30 AM", with the time and the days' names as the locale writes them;
     * null for a line of any other kind, which is shown as it is written.
     */
    fun scheduleTimingText(resources: Resources, cron: String, locale: Locale = Locale.getDefault()): String? {
        val timing = ScheduleTiming.of(cron) ?: return null
        val timeText = timing.timeText(locale)
        return when (val pattern = timing.dayPattern) {
            ScheduleTiming.DayPattern.EveryDay -> resources.getString(R.string.settings_schedules_every_day, timeText)
            ScheduleTiming.DayPattern.Weekdays -> resources.getString(R.string.settings_schedules_weekdays, timeText)
            ScheduleTiming.DayPattern.Weekends -> resources.getString(R.string.settings_schedules_weekends, timeText)
            is ScheduleTiming.DayPattern.OneDay -> resources.getString(R.string.settings_schedules_one_day, ScheduleWeekday.name(pattern.dayNumber, locale), timeText)
            is ScheduleTiming.DayPattern.SomeDays -> resources.getString(R.string.settings_schedules_some_days, ScheduleWeekday.shortNamesText(pattern.dayNumbers, locale), timeText)
        }
    }

    /** When a schedule that runs on mail comes round: "When mail arrives: From contains news@example.com". */
    fun scheduleMailText(resources: Resources, conditions: List<ScheduleMailCondition>): String {
        val conditionTexts = conditions.filter { condition -> !condition.isBare }.map { condition -> conditionText(resources, condition) }
        if (conditionTexts.isEmpty()) return resources.getString(R.string.settings_schedules_any_mail)
        return resources.getString(R.string.settings_schedules_mail_when, conditionTexts.joinToString(", "))
    }

    private fun conditionText(resources: Resources, condition: ScheduleMailCondition): String {
        val fieldName = resources.getString(mailFieldNameResource(condition.mailField))
        val header = if (condition.mailField == "header") " " + condition.header.orEmpty() else ""
        val comparison = resources.getString(comparisonNameResource(condition.comparisonOperator))
        return "$fieldName$header $comparison ${condition.value.orEmpty()}".trim()
    }

    fun mailFieldNameResource(field: String): Int = when (field) {
        "from" -> R.string.settings_schedules_field_from
        "to" -> R.string.settings_schedules_field_to
        "subject" -> R.string.settings_schedules_field_subject
        "header" -> R.string.settings_schedules_field_header
        else -> R.string.settings_schedules_field_any
    }

    fun comparisonNameResource(operator: String): Int = when (operator) {
        "equals" -> R.string.settings_schedules_is
        "matches" -> R.string.settings_schedules_matches
        else -> R.string.settings_schedules_contains
    }

    /** A question's state in the reader's words, or as the server names it when a newer server added one. */
    fun questionState(resources: Resources, questionState: String): String = when (questionState) {
        "asked" -> resources.getString(R.string.settings_memory_state_asked)
        "confirmed" -> resources.getString(R.string.settings_memory_state_confirmed)
        "corrected" -> resources.getString(R.string.settings_memory_state_corrected)
        "dropped" -> resources.getString(R.string.settings_memory_state_dropped)
        "unsure" -> resources.getString(R.string.settings_memory_state_unsure)
        else -> questionState
    }

    fun questionKind(resources: Resources, questionKind: String): String = when (questionKind) {
        "direct" -> resources.getString(R.string.settings_memory_kind_direct)
        "paraphrase" -> resources.getString(R.string.settings_memory_kind_paraphrase)
        "changed" -> resources.getString(R.string.settings_memory_kind_changed)
        "multihop" -> resources.getString(R.string.settings_memory_kind_multihop)
        "abstain" -> resources.getString(R.string.settings_memory_kind_abstain)
        else -> questionKind
    }

    fun verdict(resources: Resources, answerVerdict: String): String = when (answerVerdict) {
        "correct" -> resources.getString(R.string.settings_memory_verdict_correct)
        "partial" -> resources.getString(R.string.settings_memory_verdict_partial)
        "not_known" -> resources.getString(R.string.settings_memory_verdict_not_known)
        "missed" -> resources.getString(R.string.settings_memory_verdict_missed)
        "stale" -> resources.getString(R.string.settings_memory_verdict_stale)
        "invented" -> resources.getString(R.string.settings_memory_verdict_invented)
        "wrong" -> resources.getString(R.string.settings_memory_verdict_wrong)
        "ungraded" -> resources.getString(R.string.settings_memory_verdict_ungraded)
        else -> answerVerdict
    }
}
