package com.teanode.app.goals

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.calendar.DateField
import com.teanode.app.calendar.TimeField
import com.teanode.app.settings.ChoiceRow
import com.teanode.app.settings.FooterText
import com.teanode.app.settings.FormField
import com.teanode.app.settings.SwitchRow
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.kit.goals.AgentGoal
import com.teanode.kit.goals.GoalChange
import com.teanode.kit.goals.GoalMailCondition
import com.teanode.kit.goals.GoalMailTrigger
import com.teanode.kit.goals.GoalTimeLine
import com.teanode.kit.goals.GoalTiming
import java.time.LocalDate
import java.time.LocalTime

/** Which of the three ways a goal can run is chosen. */
enum class GoalWhenMode { OwnPace, SetTimes, Mail }

/** One of a goal's times while it is edited: a weekly time, a single moment, or a line the editor cannot take apart. */
data class GoalTimeRow(
    val kind: Kind,
    val time: LocalTime = LocalTime.of(8, 0),
    val dayNumbers: List<Int> = (1..5).toList(),
    val date: LocalDate = LocalDate.now().plusDays(1),
    val rawLine: String = "",
) {
    enum class Kind { Weekly, Moment, Other }

    /** What goes to the server, or null while the row is not complete (a weekly time with no day, a line with nothing). */
    val line: String?
        get() = when (kind) {
            Kind.Weekly -> if (dayNumbers.isEmpty()) null else GoalTiming(time.hour, time.minute, dayNumbers).cron
            Kind.Moment -> GoalTimeLine.Moment(date, time.hour, time.minute).line
            Kind.Other -> rawLine.trim().ifEmpty { null }
        }

    companion object {
        fun of(line: String): GoalTimeRow = when (val timeLine = GoalTimeLine.of(line)) {
            is GoalTimeLine.Weekly -> GoalTimeRow(Kind.Weekly, LocalTime.of(timeLine.timing.hour, timeLine.timing.minute), timeLine.timing.dayNumbers)
            is GoalTimeLine.Moment -> GoalTimeRow(Kind.Moment, LocalTime.of(timeLine.hour, timeLine.minute), date = timeLine.date)
            is GoalTimeLine.Other -> GoalTimeRow(Kind.Other, rawLine = line)
        }
    }
}

/** What the Start form and the goal's When editor hold: how it runs, its times, its mail, and whether a post notifies. */
data class GoalWhenDraft(
    val mode: GoalWhenMode = GoalWhenMode.OwnPace,
    val rows: List<GoalTimeRow> = emptyList(),
    val conditions: List<GoalMailCondition> = emptyList(),
    val isNotified: Boolean = false,
) {
    /** Whether it can be saved: set times each complete, mail conditions each filled in. */
    val isComplete: Boolean
        get() = when (mode) {
            GoalWhenMode.OwnPace -> true
            GoalWhenMode.SetTimes -> rows.isNotEmpty() && rows.all { row -> row.line != null }
            GoalWhenMode.Mail -> GoalMailTrigger.isComplete(conditions)
        }

    val goalTimes: List<String> get() = if (mode == GoalWhenMode.SetTimes) rows.mapNotNull { row -> row.line } else emptyList()

    val goalMailConditions: List<GoalMailCondition> get() = if (mode == GoalWhenMode.OwnPace) emptyList() else conditions

    /** The times, the mail and the notice as the server takes them; the title and description are the caller's. */
    fun change(goalTitle: String? = null, goalDescription: String? = null): GoalChange = GoalChange(
        goalTitle = goalTitle, goalDescription = goalDescription, goalTimes = goalTimes, goalMailConditions = goalMailConditions,
        isGoalNotifiedWhenPosted = if (mode == GoalWhenMode.SetTimes) isNotified else false,
    )

    fun choosing(chosen: GoalWhenMode): GoalWhenDraft = when (chosen) {
        GoalWhenMode.OwnPace -> copy(mode = chosen, conditions = emptyList())
        GoalWhenMode.SetTimes -> copy(mode = chosen, rows = rows.ifEmpty { listOf(GoalTimeRow(GoalTimeRow.Kind.Weekly)) }, conditions = emptyList())
        GoalWhenMode.Mail -> copy(mode = chosen, conditions = conditions.ifEmpty { listOf(GoalMailCondition("any", value = null)) })
    }

    companion object {
        fun of(goal: AgentGoal?): GoalWhenDraft {
            if (goal == null) return GoalWhenDraft()
            val mode = when {
                goal.goalTimes.isNotEmpty() -> GoalWhenMode.SetTimes
                goal.goalMailConditions.isNotEmpty() -> GoalWhenMode.Mail
                else -> GoalWhenMode.OwnPace
            }
            return GoalWhenDraft(mode, goal.goalTimes.map { line -> GoalTimeRow.of(line) }, goal.goalMailConditions, goal.isGoalNotifiedWhenPosted)
        }
    }
}

/**
 * GoalWhenEditor is the one editor of when a goal runs, used by the Start form and by a goal's When: a choice of at its
 * own pace, at set times or when mail arrives, then the times (weekly or one moment, several may be added and removed)
 * or the mail conditions, and with set times a switch to be notified when it posts.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun GoalWhenEditor(draft: GoalWhenDraft, onChange: (GoalWhenDraft) -> Unit) {
    Column(Modifier.fillMaxWidth()) {
        val modes = GoalWhenMode.entries
        SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
            modes.forEachIndexed { index, mode ->
                SegmentedButton(
                    modifier = Modifier.height(56.dp),
                    selected = draft.mode == mode, onClick = { onChange(draft.choosing(mode)) }, shape = SegmentedButtonDefaults.itemShape(index, modes.size),
                    icon = {},
                    label = { Text(stringResource(modeLabel(mode)), style = MaterialTheme.typography.labelMedium, maxLines = 2, overflow = TextOverflow.Ellipsis, textAlign = TextAlign.Center) },
                )
            }
        }
        when (draft.mode) {
            GoalWhenMode.OwnPace -> FooterText(stringResource(R.string.goals_when_own_pace_footer))
            GoalWhenMode.SetTimes -> SetTimesEditor(draft, onChange)
            GoalWhenMode.Mail -> MailEditor(draft, onChange)
        }
    }
}

private fun modeLabel(mode: GoalWhenMode): Int = when (mode) {
    GoalWhenMode.OwnPace -> R.string.goals_when_own_pace
    GoalWhenMode.SetTimes -> R.string.goals_when_set_times
    GoalWhenMode.Mail -> R.string.goals_when_mail
}

@Composable
private fun SetTimesEditor(draft: GoalWhenDraft, onChange: (GoalWhenDraft) -> Unit) {
    fun replaceRow(index: Int, row: GoalTimeRow) = onChange(draft.copy(rows = draft.rows.toMutableList().also { rows -> rows[index] = row }))
    draft.rows.forEachIndexed { index, row ->
        TeaNodeRow(
            headline = stringResource(
                when (row.kind) {
                    GoalTimeRow.Kind.Weekly -> R.string.goals_time_weekly
                    GoalTimeRow.Kind.Moment -> R.string.goals_time_moment
                    GoalTimeRow.Kind.Other -> R.string.goals_time_cron
                },
            ),
            trailing = {
                IconButton(onClick = { onChange(draft.copy(rows = draft.rows.toMutableList().also { rows -> rows.removeAt(index) })) }, enabled = draft.rows.size > 1) {
                    Icon(Icons.Outlined.Delete, stringResource(R.string.goals_time_remove))
                }
            },
        )
        when (row.kind) {
            GoalTimeRow.Kind.Weekly -> {
                TimeField(stringResource(R.string.goals_time_clock), row.time, { time -> replaceRow(index, row.copy(time = time)) })
                WeekdayChips(row.dayNumbers) { days -> replaceRow(index, row.copy(dayNumbers = days)) }
                if (row.dayNumbers.isEmpty()) FooterText(stringResource(R.string.settings_brief_choose_day))
            }
            GoalTimeRow.Kind.Moment -> {
                DateField(stringResource(R.string.goals_time_date), row.date, { date -> replaceRow(index, row.copy(date = date)) }, minimum = LocalDate.now())
                TimeField(stringResource(R.string.goals_time_clock), row.time, { time -> replaceRow(index, row.copy(time = time)) })
            }
            GoalTimeRow.Kind.Other -> FormField(row.rawLine, { text -> replaceRow(index, row.copy(rawLine = text)) }, stringResource(R.string.goals_time_cron), supporting = stringResource(R.string.goals_time_cron_hint))
        }
        HorizontalDivider(Modifier.padding(top = 8.dp))
    }
    TeaNodeRow(
        headline = stringResource(R.string.goals_time_add_weekly), leadingIcon = Icons.Outlined.Add,
        onClick = { onChange(draft.copy(rows = draft.rows + GoalTimeRow(GoalTimeRow.Kind.Weekly))) },
    )
    TeaNodeRow(
        headline = stringResource(R.string.goals_time_add_moment), leadingIcon = Icons.Outlined.Add,
        onClick = { onChange(draft.copy(rows = draft.rows + GoalTimeRow(GoalTimeRow.Kind.Moment))) },
    )
    SwitchRow(stringResource(R.string.goals_notify_when_posted), draft.isNotified, { isOn -> onChange(draft.copy(isNotified = isOn)) }, supporting = stringResource(R.string.goals_notify_footer), supportingMaxLines = 3)
}

@Composable
private fun MailEditor(draft: GoalWhenDraft, onChange: (GoalWhenDraft) -> Unit) {
    fun replaceCondition(index: Int, condition: GoalMailCondition) = onChange(draft.copy(conditions = draft.conditions.toMutableList().also { list -> list[index] = condition }))
    draft.conditions.forEachIndexed { index, condition ->
        ChoiceRow(
            stringResource(R.string.goals_mail_look_at), GoalMailTrigger.fields, condition.mailField, label = { field -> stringResource(GoalWords.mailFieldNameResource(field)) },
            onSelect = { field -> replaceCondition(index, condition.copy(mailField = field, value = if (field == "any") null else condition.value ?: "")) },
        )
        if (condition.mailField == "header") FormField(condition.header.orEmpty(), { typed -> replaceCondition(index, condition.copy(header = typed)) }, stringResource(R.string.goals_mail_header_name))
        if (!condition.isBare) {
            ChoiceRow(
                stringResource(R.string.goals_mail_compare), listOf("contains", "equals", "matches"), condition.comparisonOperator.ifEmpty { "contains" },
                label = { operator -> stringResource(GoalWords.comparisonNameResource(operator)) },
                onSelect = { operator -> replaceCondition(index, condition.copy(comparisonOperator = operator)) },
            )
            FormField(condition.value.orEmpty(), { typed -> replaceCondition(index, condition.copy(value = typed)) }, stringResource(R.string.goals_mail_value))
        }
        if (draft.conditions.size > 1) {
            TeaNodeRow(
                headline = stringResource(R.string.goals_mail_remove_condition), leadingIcon = Icons.Outlined.Delete,
                onClick = { onChange(draft.copy(conditions = draft.conditions.toMutableList().also { list -> list.removeAt(index) })) },
            )
        }
        HorizontalDivider(Modifier.padding(top = 8.dp))
    }
    TeaNodeRow(
        headline = stringResource(R.string.goals_mail_add_condition), leadingIcon = Icons.Outlined.Add,
        onClick = { onChange(draft.copy(conditions = draft.conditions + GoalMailCondition("from"))) },
    )
    FooterText(stringResource(R.string.goals_mail_footer))
}

/** The days of a weekly time as chips, Monday first. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun WeekdayChips(dayNumbers: List<Int>, onChange: (List<Int>) -> Unit) {
    FlowRow(Modifier.padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        (1..7).forEach { day ->
            FilterChip(
                selected = dayNumbers.contains(day),
                onClick = { onChange(if (dayNumbers.contains(day)) dayNumbers - day else (dayNumbers + day).sorted()) },
                label = { Text(java.time.DayOfWeek.of(day).getDisplayName(java.time.format.TextStyle.SHORT, java.util.Locale.getDefault())) },
            )
        }
    }
}
