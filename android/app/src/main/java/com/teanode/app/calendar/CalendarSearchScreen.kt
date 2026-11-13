package com.teanode.app.calendar

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.EventBusy
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.calendar.CalendarDates
import com.teanode.kit.calendar.CalendarDay
import com.teanode.kit.calendar.CalendarEvent
import com.teanode.kit.calendar.calendarEvents
import com.teanode.kit.calendar.dayRange
import com.teanode.kit.calendar.shownOrder
import java.time.LocalDate

/**
 * CalendarSearchScreen finds events by their title or place, from a year back to a year ahead. The server has no search
 * of its own, so those two years are read once and searched on the phone; what is still to come is listed first (ios
 * CalendarView.swift, CalendarSearch).
 */
@Composable
fun CalendarSearchScreen(model: CalendarModel) {
    val navigator = LocalAppNavigator.current
    var searchText by remember { mutableStateOf("") }
    var events by remember { mutableStateOf<List<CalendarEvent>?>(null) }
    var problemText by remember { mutableStateOf<String?>(null) }
    val zone = model.rules.zone
    LaunchedEffect(Unit) {
        if (events != null) return@LaunchedEffect
        if (model.calendars.isEmpty()) model.load()
        val today = LocalDate.now(zone)
        if (model.calendar == null) {
            problemText = ""
            return@LaunchedEffect
        }
        try {
            val from = today.minusYears(1).atStartOfDay(zone).toInstant()
            val until = today.plusYears(1).atStartOfDay(zone).toInstant()
            // Every calendar shown, and none of the busy blocks: there is nothing in them to find.
            val found = ArrayList<CalendarEvent>()
            for (each in model.shownCalendars) found += model.connection.calendarEvents(each.id, from, until)
            events = found.filter { event -> !event.isBusy }
        } catch (failure: kotlinx.coroutines.CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            problemText = model.describeError(failure)
        }
    }
    val words = searchText.trim()
    val groups = remember(events, words) { foundGroups(events, words, zone) }
    TeaNodeScreen(title = stringResource(R.string.calendar_search), onBack = { navigator.back() }) { contentPadding ->
        androidx.compose.foundation.layout.Column(Modifier.fillMaxSize().padding(contentPadding)) {
            OutlinedTextField(
                value = searchText, onValueChange = { text -> searchText = text }, placeholder = { Text(stringResource(R.string.calendar_titles_and_places)) },
                leadingIcon = { androidx.compose.material3.Icon(Icons.Outlined.Search, contentDescription = null) },
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), singleLine = true,
            )
            val problem = problemText
            when {
                problem != null -> ErrorState(problem.ifEmpty { stringResource(R.string.calendar_not_read_yet) }, onRetry = { problemText = null; events = null })
                events == null -> LoadingState()
                words.isEmpty() -> EmptyState(Icons.Outlined.Search, stringResource(R.string.calendar_search_your_calendar), description = stringResource(R.string.calendar_search_hint))
                groups.isEmpty() -> EmptyState(Icons.Outlined.EventBusy, stringResource(R.string.calendar_no_results_for, words))
                else -> LazyColumn(Modifier.weight(1f)) {
                    for (group in groups) {
                        item(key = "day-${group.day}") {
                            Text(
                                DateWords.weekdayMonthDayYear(group.day.toLocalDate()), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                                modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 16.dp, bottom = 8.dp),
                            )
                        }
                        items(group.events.size, key = { index -> "event-${group.day}-${group.events[index].occurrenceId}" }) { index ->
                            val event = group.events[index]
                            EventRow(model, event, Modifier.clickable { navigator.navigate(CalendarRoutes.event(event.calendarId, event.id)) })
                        }
                    }
                }
            }
        }
    }
}

internal class SearchDayGroup(val day: CalendarDay, val events: List<CalendarEvent>)

/** The events that match, by the day they start: upcoming days first, soonest first, then past days, latest first. */
internal fun foundGroups(events: List<CalendarEvent>?, words: String, zone: java.time.ZoneId): List<SearchDayGroup> {
    if (events == null || words.isEmpty()) return emptyList()
    val matching = events.filter { event ->
        (event.summary.orEmpty()).contains(words, ignoreCase = true) || (event.location.orEmpty()).contains(words, ignoreCase = true)
    }
    val byDay = matching.groupBy { event -> CalendarDates.dayRange(event, zone)?.start ?: CalendarDay(1970, 1, 1) }
    val today = CalendarDay.of(LocalDate.now(zone))
    val upcoming = byDay.keys.filter { day -> day >= today }.sorted()
    val past = byDay.keys.filter { day -> day < today }.sortedDescending()
    return (upcoming + past).map { day -> SearchDayGroup(day, byDay.getValue(day).sortedWith(shownOrder)) }
}
