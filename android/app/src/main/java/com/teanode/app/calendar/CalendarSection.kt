package com.teanode.app.calendar

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.automirrored.outlined.ArrowForward
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.CalendarMonth
import androidx.compose.material.icons.outlined.Checklist
import androidx.compose.material.icons.outlined.EventNote
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.Today
import androidx.compose.material.icons.outlined.ViewAgenda
import androidx.compose.material.icons.outlined.ViewDay
import androidx.compose.material.icons.outlined.ViewWeek
import androidx.compose.material.icons.outlined.WorkOutline
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.AppSection
import com.teanode.kit.calendar.CalendarMonth
import kotlinx.coroutines.launch
import java.time.LocalDate
import androidx.compose.ui.unit.dp

internal fun CalendarViewMode.icon(): ImageVector = when (this) {
    CalendarViewMode.Month -> Icons.Outlined.CalendarMonth
    CalendarViewMode.Week -> Icons.Outlined.ViewWeek
    CalendarViewMode.Workweek -> Icons.Outlined.WorkOutline
    CalendarViewMode.Day -> Icons.Outlined.ViewDay
    CalendarViewMode.Agenda -> Icons.Outlined.ViewAgenda
    CalendarViewMode.Reminders -> Icons.Outlined.Checklist
}

@Composable
internal fun CalendarViewMode.title(): String = stringResource(
    when (this) {
        CalendarViewMode.Month -> R.string.calendar_month
        CalendarViewMode.Week -> R.string.calendar_week
        CalendarViewMode.Workweek -> R.string.calendar_five_days
        CalendarViewMode.Day -> R.string.calendar_day
        CalendarViewMode.Agenda -> R.string.calendar_agenda
        CalendarViewMode.Reminders -> R.string.calendar_reminders
    },
)

/**
 * CalendarSection is the dashboard's Calendar page on a phone (ios CalendarView.swift, CalendarScreen). It opens on the
 * months scrolling one after another, and a tap on a day opens that day as hours. The menu switches to the agenda, a
 * day, a week or five days as a list of days, and the reminders.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun CalendarSection(initialMode: CalendarViewMode? = null) {
    val model = rememberCalendarModel()
    val navigator = LocalAppNavigator.current
    val session = LocalAppSession.current
    val scope = rememberCoroutineScope()
    val facts by session.facts.collectAsState()
    val isCalendarOn = facts.services?.isCalendarEnabled != false
    val isRemindersOn = facts.services?.isRemindersEnabled != false
    val monthState = rememberMonthListState()
    var isMenuOpen by remember { mutableStateOf(false) }
    val mode = model.viewMode
    LaunchedEffect(initialMode) { if (initialMode != null) model.showViewMode(initialMode) }
    LaunchedEffect(isCalendarOn, isRemindersOn) {
        if (!isCalendarOn) model.showViewMode(CalendarViewMode.Reminders)
        else if (!isRemindersOn && model.viewMode == CalendarViewMode.Reminders) model.showViewMode(CalendarViewMode.Month)
        model.load()
    }
    val titleText = calendarTitle(model, mode, monthState.firstVisibleItemIndex)
    TeaNodeScreen(
        title = titleText,
        actions = {
            if (mode != CalendarViewMode.Reminders) {
                IconButton(onClick = {
                    model.moveFocus(LocalDate.now(model.rules.zone))
                    if (mode == CalendarViewMode.Month) scope.launch { monthState.animateScrollToItem(CURRENT_MONTH_INDEX) }
                }) { Icon(Icons.Outlined.Today, contentDescription = stringResource(R.string.calendar_today)) }
                IconButton(onClick = { navigator.navigate(CalendarRoutes.SEARCH) }) {
                    Icon(Icons.Outlined.Search, contentDescription = stringResource(R.string.calendar_search_events))
                }
            }
            Box {
                IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.calendar_more_views)) }
                DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                    for (each in CalendarViewMode.entries) {
                        if (each == CalendarViewMode.Reminders && !isRemindersOn) continue
                        if (each != CalendarViewMode.Reminders && !isCalendarOn) continue
                        DropdownMenuItem(
                            text = { Text(each.title(), color = if (each == mode) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface) },
                            leadingIcon = { Icon(each.icon(), contentDescription = null, tint = if (each == mode) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant) },
                            onClick = {
                                isMenuOpen = false
                                model.showViewMode(each)
                            },
                        )
                    }
                    if (isCalendarOn) {
                        HorizontalDivider()
                        DropdownMenuItem(
                            text = { Text(stringResource(R.string.calendar_calendars)) },
                            leadingIcon = { Icon(Icons.Outlined.EventNote, contentDescription = null) },
                            onClick = {
                                isMenuOpen = false
                                navigator.navigate(CalendarRoutes.CALENDARS)
                            },
                        )
                    }
                }
            }
        },
        floatingActionButton = {
            FloatingActionButton(
                onClick = {
                    if (mode == CalendarViewMode.Reminders) navigator.navigate(CalendarRoutes.reminder(null))
                    else if (model.calendar != null) navigator.navigate(CalendarRoutes.newEvent(CalendarModel.nextHour(model.focus, java.time.ZonedDateTime.now(model.rules.zone)).toEpochMilli()))
                },
            ) {
                Icon(Icons.Outlined.Add, contentDescription = stringResource(if (mode == CalendarViewMode.Reminders) R.string.calendar_new_reminder else R.string.calendar_new_event))
            }
        },
    ) { contentPadding ->
        androidx.compose.foundation.layout.Column(Modifier.fillMaxSize().padding(contentPadding)) {
            if (mode != CalendarViewMode.Month && mode != CalendarViewMode.Reminders) StepBar(model)
            Box(Modifier.weight(1f)) {
                when (mode) {
                    CalendarViewMode.Month -> PullToRefreshBox(isRefreshing = model.isLoading && model.loadedMonths.isEmpty(), onRefresh = { scope.launch { model.reloadCalendars() } }) {
                        MonthScroll(model, monthState) { day -> navigator.navigate(CalendarRoutes.day(day.toLocalDate().toEpochDay())) }
                    }
                    CalendarViewMode.Week, CalendarViewMode.Workweek -> PullToRefreshBox(isRefreshing = false, onRefresh = { scope.launch { model.reloadCalendars() } }) {
                        LazyColumn(Modifier.fillMaxSize()) {
                            item { PendingChangeBox(model, Modifier.padding(16.dp)) }
                            for (day in model.days()) daySection(model, day, isEmptyShown = true) { event -> navigator.navigate(CalendarRoutes.event(event.calendarId, event.id)) }
                        }
                    }
                    CalendarViewMode.Day -> DayTimeline(model, model.focus, onOpen = { event -> navigator.navigate(CalendarRoutes.event(event.calendarId, event.id)) }, onCreate = { start -> navigator.navigate(CalendarRoutes.newEvent(start.toEpochMilli())) })
                    CalendarViewMode.Agenda -> {
                        val days = model.days().filter { day -> model.events(day).isNotEmpty() }
                        PullToRefreshBox(isRefreshing = false, onRefresh = { scope.launch { model.reloadCalendars() } }) {
                            if (days.isEmpty() && !model.isLoading) {
                                EmptyState(Icons.Outlined.CalendarMonth, stringResource(R.string.calendar_nothing_next_month))
                            } else {
                                LazyColumn(Modifier.fillMaxSize()) {
                                    item { PendingChangeBox(model, Modifier.padding(16.dp)) }
                                    for (day in days) daySection(model, day, isEmptyShown = false) { event -> navigator.navigate(CalendarRoutes.event(event.calendarId, event.id)) }
                                }
                            }
                        }
                    }
                    CalendarViewMode.Reminders -> RemindersList(model.reminders)
                }
            }
        }
    }
}

/** Back or forward a step at the top of the views that are not the month: a day, a week, or a month of agenda. */
@Composable
private fun StepBar(model: CalendarModel) {
    Row(Modifier.fillMaxWidth().padding(horizontal = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        IconButton(onClick = { model.step(-1) }) { Icon(Icons.AutoMirrored.Outlined.ArrowBack, contentDescription = stringResource(R.string.calendar_earlier)) }
        Text(
            DateWords.monthYear(model.focus), modifier = Modifier.weight(1f), style = MaterialTheme.typography.labelLarge,
            textAlign = androidx.compose.ui.text.style.TextAlign.Center, color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        IconButton(onClick = { model.step(1) }) { Icon(Icons.AutoMirrored.Outlined.ArrowForward, contentDescription = stringResource(R.string.calendar_later)) }
    }
}

/** The title of the screen: the year at the top of the month view, a range of days, one day, or the name of the view. */
@Composable
private fun calendarTitle(model: CalendarModel, mode: CalendarViewMode, topMonthIndex: Int): String = when (mode) {
    CalendarViewMode.Month -> {
        val current = CalendarMonth.containing(java.time.Instant.now(), model.rules.zone)
        DateWords.year(current.adding(topMonthIndex - CURRENT_MONTH_INDEX).firstDay.toLocalDate())
    }
    CalendarViewMode.Week, CalendarViewMode.Workweek -> {
        val days = model.days()
        DateWords.monthDay(days.first()) + " - " + DateWords.monthDay(days.last())
    }
    CalendarViewMode.Day -> DateWords.weekdayMonthDay(model.focus)
    CalendarViewMode.Agenda -> stringResource(R.string.calendar_agenda)
    CalendarViewMode.Reminders -> stringResource(R.string.calendar_reminders)
}
