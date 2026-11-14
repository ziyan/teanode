package com.teanode.app.calendar

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Remove
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.calendar.CalendarDay
import com.teanode.kit.calendar.CalendarRepeat
import java.time.DayOfWeek
import java.time.format.TextStyle
import java.util.Locale

/**
 * CustomRepeatScreen is the screen behind Custom in an event's Repeat row: how often, the days, and when it stops, with
 * the sentence it makes under the first section. It writes the rule into the event as each part changes, so going back
 * is all it takes to keep it (ios CalendarRepeatViews.swift, CustomRepeatScreen).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun CustomRepeatScreen(rule: String, context: CalendarRepeat.Context, onRuleChange: (String) -> Unit, onBack: () -> Unit) {
    // The rule and its parts as the screen was opened on them, so that a rule this screen cannot show is kept exactly
    // until something it does show is changed.
    val originalRule = remember { rule }
    val readParts = remember { CalendarRepeat.parse(rule, context) }
    val initialParts = remember { readParts ?: CalendarRepeat.defaultParts(CalendarRepeat.Frequency.Weekly, context) }
    val isRuleReadable = remember { readParts != null || rule.isBlank() }
    var parts by remember { mutableStateOf(initialParts) }

    fun change(edited: CalendarRepeat.Parts) {
        parts = edited
        onRuleChange(CalendarRepeat.editedRule(originalRule, initialParts, edited, context))
    }

    TeaNodeScreen(title = stringResource(R.string.calendar_custom), onBack = onBack) { contentPadding ->
        Column(Modifier.fillMaxSize().padding(contentPadding).verticalScroll(rememberScrollState())) {
            // How often.
            FlowRow(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                for ((frequency, labelResource) in listOf(
                    CalendarRepeat.Frequency.Daily to R.string.calendar_every_day,
                    CalendarRepeat.Frequency.Weekly to R.string.calendar_every_week,
                    CalendarRepeat.Frequency.Monthly to R.string.calendar_every_month,
                    CalendarRepeat.Frequency.Yearly to R.string.calendar_every_year,
                )) {
                    FilterChip(selected = parts.frequency == frequency, onClick = { change(parts.copy(frequency = frequency)) }, label = { Text(stringResource(labelResource)) })
                }
            }
            Stepper(CalendarRepeatWords.shortText(parts), parts.intervalCount, 1..999) { count -> change(parts.copy(intervalCount = count)) }
            Column(Modifier.padding(horizontal = 16.dp, vertical = 4.dp)) {
                Text(CalendarRepeatWords.sentenceText(CalendarRepeat.sentence(parts)), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                // Whether to say that the rule was set elsewhere and is kept: it has a part this screen cannot show, or
                // it could not be read at all, and nothing has been changed yet.
                if ((initialParts.hasUnshownParts || !isRuleReadable) && parts == initialParts) {
                    Text(stringResource(R.string.calendar_set_in_another_program_kept), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(top = 6.dp))
                }
            }
            if (parts.frequency == CalendarRepeat.Frequency.Weekly) {
                TeaNodeSectionHeader(stringResource(R.string.calendar_repeat_on))
                WeekdayChecklist(parts.weekdays.toSet(), isEmptyAllowed = false) { days -> change(parts.copy(weekdays = days.sorted())) }
            }
            if (parts.frequency == CalendarRepeat.Frequency.Monthly) {
                TeaNodeSectionHeader(stringResource(R.string.calendar_repeat_on))
                MonthlySection(parts, context) { monthly -> change(parts.copy(monthly = monthly)) }
            }
            TeaNodeSectionHeader(stringResource(R.string.calendar_end_repeat))
            EndSection(parts, context) { end -> change(parts.copy(end = end)) }
        }
    }
}

@Composable
private fun Stepper(label: String, value: Int, range: IntRange, onChange: (Int) -> Unit) {
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(label, modifier = Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
        IconButton(onClick = { onChange((value - 1).coerceIn(range)) }, enabled = value > range.first) {
            Icon(Icons.Outlined.Remove, contentDescription = stringResource(R.string.calendar_fewer))
        }
        IconButton(onClick = { onChange((value + 1).coerceIn(range)) }, enabled = value < range.last) {
            Icon(Icons.Outlined.Add, contentDescription = stringResource(R.string.calendar_more))
        }
    }
}

/** The days of a week as chips, in the order the calendar starts the week. At least one stays chosen when [isEmptyAllowed] is false. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun WeekdayChecklist(dayNumbers: Set<Int>, isEmptyAllowed: Boolean, onChange: (Set<Int>) -> Unit) {
    val firstDay = remember { java.time.temporal.WeekFields.of(Locale.getDefault()).firstDayOfWeek }
    val locale = Locale.getDefault()
    FlowRow(Modifier.padding(horizontal = 16.dp, vertical = 4.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        for (offset in 0 until 7) {
            val dayNumber = (firstDay.value - 1 + offset) % 7 + 1
            val isOn = dayNumber in dayNumbers
            FilterChip(
                selected = isOn,
                onClick = {
                    val changed = if (isOn) dayNumbers - dayNumber else dayNumbers + dayNumber
                    if (changed.isNotEmpty() || isEmptyAllowed) onChange(changed)
                },
                label = { Text(DayOfWeek.of(dayNumber).getDisplayName(TextStyle.SHORT_STANDALONE, locale)) },
            )
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun MonthlySection(parts: CalendarRepeat.Parts, context: CalendarRepeat.Context, onChange: (CalendarRepeat.Monthly) -> Unit) {
    val monthly = parts.monthly
    val isOrdinal = monthly is CalendarRepeat.Monthly.OrdinalWeekday
    Row(Modifier.padding(horizontal = 16.dp, vertical = 4.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        FilterChip(selected = !isOrdinal, onClick = { onChange(CalendarRepeat.Monthly.DayOfMonth(context.startDay.day)) }, label = { Text(stringResource(R.string.calendar_day_of_the_month)) })
        FilterChip(
            selected = isOrdinal,
            onClick = { onChange(CalendarRepeat.Monthly.OrdinalWeekday(CalendarRepeat.ordinal(context.startDay), CalendarRepeat.weekday(context.startDay))) },
            label = { Text(stringResource(R.string.calendar_weekday)) },
        )
    }
    if (monthly is CalendarRepeat.Monthly.DayOfMonth) {
        ChoiceRow(stringResource(R.string.calendar_day), monthly.dayOfMonth.toString(), (1..31).map { day -> day.toString() }) { index -> onChange(CalendarRepeat.Monthly.DayOfMonth(index + 1)) }
    } else if (monthly is CalendarRepeat.Monthly.OrdinalWeekday) {
        val ordinals = CalendarRepeat.monthlyOrdinals
        ChoiceRow(stringResource(R.string.calendar_which), CalendarRepeatWords.ordinalName(monthly.ordinal), ordinals.map { value -> CalendarRepeatWords.ordinalName(value) }) { index ->
            onChange(monthly.copy(ordinal = ordinals[index]))
        }
        val firstDay = java.time.temporal.WeekFields.of(Locale.getDefault()).firstDayOfWeek
        val order = (0 until 7).map { offset -> (firstDay.value - 1 + offset) % 7 + 1 }
        ChoiceRow(
            stringResource(R.string.calendar_weekday), CalendarRepeat.weekdayName(monthly.weekday), order.map { number -> CalendarRepeat.weekdayName(number) },
        ) { index -> onChange(monthly.copy(weekday = order[index])) }
    }
}

/** A row with a label and the choice made, which opens a menu of the choices. */
@Composable
internal fun ChoiceRow(label: String, chosen: String, choices: List<String>, onChoose: (Int) -> Unit) {
    var isOpen by remember { mutableStateOf(false) }
    androidx.compose.foundation.layout.Box {
        Row(
            Modifier.fillMaxWidth().clickable { isOpen = true }.padding(horizontal = 16.dp, vertical = 14.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(label, style = MaterialTheme.typography.bodyLarge, modifier = Modifier.weight(1f))
            Text(chosen, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.primary)
        }
        androidx.compose.material3.DropdownMenu(expanded = isOpen, onDismissRequest = { isOpen = false }) {
            choices.forEachIndexed { index, choice ->
                androidx.compose.material3.DropdownMenuItem(text = { Text(choice) }, onClick = {
                    isOpen = false
                    onChoose(index)
                })
            }
        }
    }
}


private enum class EndKind { Never, OnDate, AfterCount }

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun EndSection(parts: CalendarRepeat.Parts, context: CalendarRepeat.Context, onChange: (CalendarRepeat.End) -> Unit) {
    val end = parts.end
    val kind = when (end) {
        CalendarRepeat.End.Never -> EndKind.Never
        is CalendarRepeat.End.OnDate -> EndKind.OnDate
        is CalendarRepeat.End.AfterCount -> EndKind.AfterCount
    }
    FlowRow(Modifier.padding(horizontal = 16.dp, vertical = 4.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        FilterChip(selected = kind == EndKind.Never, onClick = { onChange(CalendarRepeat.End.Never) }, label = { Text(stringResource(R.string.calendar_never)) })
        FilterChip(
            selected = kind == EndKind.OnDate, onClick = { onChange(CalendarRepeat.End.OnDate(CalendarRepeat.dayOneYearAfter(context.startDay))) },
            label = { Text(stringResource(R.string.calendar_on_date)) },
        )
        FilterChip(selected = kind == EndKind.AfterCount, onClick = { onChange(CalendarRepeat.End.AfterCount(10)) }, label = { Text(stringResource(R.string.calendar_after_a_number_of_times)) })
    }
    if (end is CalendarRepeat.End.OnDate) {
        DateField(
            stringResource(R.string.calendar_last_day), end.day.toLocalDate(),
            { date -> onChange(CalendarRepeat.End.OnDate(CalendarDay.of(date))) }, minimum = context.startDay.toLocalDate(),
        )
    }
    if (end is CalendarRepeat.End.AfterCount) {
        Stepper(stringResource(R.string.calendar_times, end.occurrenceCount), end.occurrenceCount, 1..999) { count -> onChange(CalendarRepeat.End.AfterCount(count)) }
    }
}

