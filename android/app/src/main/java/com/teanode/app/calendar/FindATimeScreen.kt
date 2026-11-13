package com.teanode.app.calendar

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.calendar.FreeStretch
import com.teanode.kit.calendar.FreeTimeAnswer
import com.teanode.kit.calendar.FreeTimeQuestion
import com.teanode.kit.calendar.PeopleSharingTheirCalendar
import com.teanode.kit.calendar.freeTime
import com.teanode.kit.calendar.peopleSharingTheirCalendar
import kotlinx.coroutines.launch
import java.time.LocalDate
import java.time.LocalTime

/**
 * FindATimeScreen is when several people are all free: the people who shared their calendar with the person, a run of
 * days and the hours of the day to offer, and the stretches nobody has anything booked in. The dashboard's Find a time
 * dialog, the command line's `calendar free --with` and the agent's free_together make the same kind of sum from the
 * same busy times; the days and hours here are the phone's zone, which is what the calendar screen shows (ios
 * FindATimeViews.swift).
 */
@Composable
fun FindATimeScreen(model: CalendarModel, ownUserId: String) {
    val navigator = LocalAppNavigator.current
    val connection = model.connection
    val scope = rememberCoroutineScope()
    val zone = model.rules.zone
    val sharing = rememberLoad { connection.peopleSharingTheirCalendar(zone = zone) }
    val today = LocalDate.now(zone)
    var chosen by remember { mutableStateOf(emptySet<String>()) }
    var firstDay by remember { mutableStateOf(today) }
    var lastDay by remember { mutableStateOf(today.plusDays(6)) }
    var earliest by remember { mutableStateOf(LocalTime.of(9, 0)) }
    var latest by remember { mutableStateOf(LocalTime.of(17, 0)) }
    var stretches by remember { mutableStateOf<List<FreeStretch>?>(null) }
    var isWorking by remember { mutableStateOf(false) }
    val lastBeforeFirst = stringResource(R.string.calendar_last_day_before_first)
    val dayEndsBeforeBegins = stringResource(R.string.calendar_day_must_end_after_begin)
    val failedText = stringResource(R.string.calendar_did_not_work)

    fun find(known: PeopleSharingTheirCalendar) {
        val question = FreeTimeQuestion(
            firstDay.atStartOfDay(zone).toInstant(), lastDay.atStartOfDay(zone).toInstant(), earliest.hour * 60 + earliest.minute, latest.hour * 60 + latest.minute,
        )
        when (question.problem) {
            FreeTimeQuestion.Problem.LastDayBeforeFirst -> {
                model.snackbar?.showProblem(lastBeforeFirst)
                return
            }
            FreeTimeQuestion.Problem.DayEndsBeforeItBegins -> {
                model.snackbar?.showProblem(dayEndsBeforeBegins)
                return
            }
            null -> Unit
        }
        isWorking = true
        scope.launch {
            try {
                when (val answer = connection.freeTime(ownUserId, known.people.filter { person -> person.userId in chosen }, question, zone)) {
                    is FreeTimeAnswer.Free -> stretches = answer.stretches
                    is FreeTimeAnswer.NotShared -> {
                        stretches = null
                        model.snackbar?.showProblem(answer.usernames.joinToString(", ").let { names -> model.context.getString(R.string.calendar_stopped_sharing_with_you, names) })
                    }
                }
            } catch (failure: kotlinx.coroutines.CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                model.snackbar?.showProblem(model.describeError(failure).ifEmpty { failedText })
            }
            isWorking = false
        }
    }

    TeaNodeScreen(title = stringResource(R.string.calendar_find_a_time), onBack = { navigator.back() }) { contentPadding ->
        LoadedContent(sharing, modifier = Modifier.fillMaxSize().padding(contentPadding)) { known ->
            LazyColumn(Modifier.fillMaxSize()) {
                if (known.people.isEmpty()) {
                    item {
                        Text(stringResource(R.string.calendar_nobody_shared), modifier = Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                } else {
                    item { TeaNodeSectionHeader(stringResource(R.string.calendar_with)) }
                    items(known.people, key = { person -> person.userId }) { person ->
                        Row(Modifier.fillMaxWidth().clickable { chosen = if (person.userId in chosen) chosen - person.userId else chosen + person.userId }.padding(horizontal = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                            Checkbox(checked = person.userId in chosen, onCheckedChange = { isOn -> chosen = if (isOn) chosen + person.userId else chosen - person.userId })
                            Text(person.pickerLabel, style = MaterialTheme.typography.bodyLarge)
                        }
                    }
                    if (known.isAnyoneLeftOut) {
                        item { Text(stringResource(R.string.calendar_someone_left_out), modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                    }
                    // There is nothing to find before today.
                    item { DateField(stringResource(R.string.calendar_first_day), firstDay, { day -> firstDay = day; if (lastDay.isBefore(day)) lastDay = day }, minimum = today) }
                    item { DateField(stringResource(R.string.calendar_last_day), lastDay, { day -> lastDay = day }, minimum = firstDay) }
                    item { TimeField(stringResource(R.string.calendar_earliest_hour), earliest, { time -> earliest = time }) }
                    item { TimeField(stringResource(R.string.calendar_latest_hour), latest, { time -> latest = time }) }
                    item {
                        Button(enabled = !isWorking && chosen.isNotEmpty(), onClick = { find(known) }, modifier = Modifier.padding(16.dp)) {
                            Text(stringResource(R.string.calendar_find_times))
                        }
                    }
                    val found = stretches
                    if (found != null) {
                        item { TeaNodeSectionHeader(stringResource(R.string.calendar_when_everyone_free)) }
                        if (found.isEmpty()) {
                            item { Text(stringResource(R.string.calendar_no_time_free), modifier = Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
                        }
                        val days = freeDays(found)
                        items(days.size, key = { index -> "free-${days[index].day}" }) { index ->
                            val group = days[index]
                            Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) {
                                Text(DateWords.weekdayMonthWideDay(group.day.atZone(zone).toLocalDate()), style = MaterialTheme.typography.bodyLarge)
                                Text(
                                    group.stretches.joinToString(", ") { stretch -> DateWords.shortTime(stretch.startsAt, zone) + " - " + DateWords.shortTime(stretch.endsAt, zone) },
                                    style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                            }
                        }
                    }
                }
            }
        }
    }
}

/** One day's free stretches. */
internal class FreeDay(val day: java.time.Instant, val stretches: List<FreeStretch>)

/** The stretches gathered under their days, in order. */
internal fun freeDays(stretches: List<FreeStretch>): List<FreeDay> {
    val groups = ArrayList<FreeDay>()
    for (stretch in stretches) {
        val last = groups.lastOrNull()
        if (last != null && last.day == stretch.day) groups[groups.size - 1] = FreeDay(last.day, last.stretches + stretch) else groups.add(FreeDay(stretch.day, listOf(stretch)))
    }
    return groups
}

