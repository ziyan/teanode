package com.teanode.app.calendar

import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.automirrored.outlined.ArrowForward
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Today
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.calendar.CalendarDay
import java.time.LocalDate

/** CalendarDayScreen is a day opened from the month: its hours, with today and the days either side along the top (ios CalendarView.swift, CalendarDayScreen). */
@Composable
fun CalendarDayScreen(model: CalendarModel, firstDay: LocalDate) {
    val navigator = LocalAppNavigator.current
    var day by remember { mutableStateOf(firstDay) }
    LaunchedEffect(day) { model.ensureMonth(CalendarDay.of(day).calendarMonth) }
    TeaNodeScreen(
        title = DateWords.weekdayMonthDay(day),
        onBack = { navigator.back() },
        actions = {
            IconButton(onClick = { day = LocalDate.now(model.rules.zone) }) { Icon(Icons.Outlined.Today, contentDescription = stringResource(R.string.calendar_today)) }
            IconButton(
                onClick = { navigator.navigate(CalendarRoutes.newEvent(CalendarModel.nextHour(day, java.time.ZonedDateTime.now(model.rules.zone)).toEpochMilli())) },
                enabled = model.calendar != null,
            ) { Icon(Icons.Outlined.Add, contentDescription = stringResource(R.string.calendar_new_event)) }
        },
    ) { contentPadding ->
        androidx.compose.foundation.layout.Column(Modifier.fillMaxSize().padding(contentPadding)) {
            Row(Modifier.fillMaxWidth().padding(horizontal = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                IconButton(onClick = { day = day.minusDays(1) }) { Icon(Icons.AutoMirrored.Outlined.ArrowBack, contentDescription = stringResource(R.string.calendar_previous_day)) }
                Text(
                    DateWords.monthYear(day), modifier = Modifier.weight(1f), style = MaterialTheme.typography.labelLarge,
                    textAlign = androidx.compose.ui.text.style.TextAlign.Center, color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                IconButton(onClick = { day = day.plusDays(1) }) { Icon(Icons.AutoMirrored.Outlined.ArrowForward, contentDescription = stringResource(R.string.calendar_next_day)) }
            }
            DayTimeline(
                model, day,
                onOpen = { event -> navigator.navigate(CalendarRoutes.event(event.calendarId, event.id)) },
                onCreate = { start -> navigator.navigate(CalendarRoutes.newEvent(start.toEpochMilli())) },
            )
        }
    }
}
