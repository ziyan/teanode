package com.teanode.app.calendar

import android.icu.util.ChineseCalendar
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.outlined.CalendarToday
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.kit.calendar.CalendarDay
import com.teanode.kit.calendar.CalendarEvent
import com.teanode.kit.calendar.CalendarMonth
import com.teanode.kit.calendar.EventChipSplit
import com.teanode.kit.calendar.LunarDates
import com.teanode.kit.calendar.MonthLayout
import java.time.DayOfWeek
import java.time.LocalDate
import java.time.ZoneId
import java.util.Locale

/** How many lines a day's cell has for chips and the "+N": two chips and the count. */
private const val CELL_LINE_COUNT = 3

/** Seven days across a phone hold a number and a chip only up to a text size of 1.5; beyond it a cell shows how many events it has, and the grid stops growing. */
private const val COUNT_ONLY_FONT_SCALE = 1.5f

/** Lunar dates are shown under the days when the app speaks Chinese, as calendars printed in China show them. */
@Composable
internal fun isLunarShownForAppLanguage(): Boolean = LocalConfiguration.current.locales.get(0)?.language == "zh"

/** The lunar name of the day that starts at local midnight on [day], from the phone's Chinese calendar. */
internal fun lunarName(day: CalendarDay, zone: ZoneId): String {
    val moment = day.toLocalDate().atTime(12, 0).atZone(zone).toInstant().toEpochMilli()
    val chinese = ChineseCalendar(android.icu.util.TimeZone.getTimeZone(zone.id))
    chinese.setTimeInMillis(moment)
    return LunarDates.name(chinese.get(ChineseCalendar.MONTH) + 1, chinese.get(ChineseCalendar.DAY_OF_MONTH), chinese.get(ChineseCalendar.IS_LEAP_MONTH) == 1)
}

/** The months one after another down the screen, each read from the server as it comes into view; the weekdays stay at the top (ios CalendarView.swift, MonthScroll). */
@Composable
fun MonthScroll(model: CalendarModel, listState: LazyListState, onOpenDay: (CalendarDay) -> Unit) {
    val current = CalendarMonth(model.focus.year, model.focus.monthValue)
    val months = remember { (-MONTHS_BEFORE..MONTHS_AFTER).map { offset -> CalendarMonth.containing(java.time.Instant.now(), model.rules.zone).adding(offset) } }
    val isLunarShown = isLunarShownForAppLanguage()
    val density = LocalDensity.current
    val isCountOnly = density.fontScale >= COUNT_ONLY_FONT_SCALE
    LaunchedEffect(listState, months) {
        snapshotFlow { listState.layoutInfo.visibleItemsInfo.map { info -> info.index } }.collect { indexes ->
            val visible = indexes.mapNotNull { index -> months.getOrNull(index) }.toSet()
            model.visibleMonths = visible
            for (month in visible) model.ensureMonth(month)
        }
    }
    LaunchedEffect(current) { model.ensureMonth(current) }
    // Seven days across a phone hold a number and a chip only up to the first large text size; beyond it the numbers
    // would run into each other. The day a tap opens has every size.
    CompositionLocalProvider(LocalDensity provides Density(density.density, minOf(density.fontScale, COUNT_ONLY_FONT_SCALE))) {
        Column {
            PendingChangeBox(model, Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
            WeekdayRow(model.rules.firstDayOfWeek)
            LazyColumn(state = listState, modifier = Modifier.weight(1f)) {
                items(months, key = { month -> month.id }) { month ->
                    MonthSection(model, month, isLunarShown, isCountOnly, onOpenDay)
                }
                item { Spacer(Modifier.height(96.dp)) }
            }
        }
    }
}

/** How many months the month view has before this one, and after. */
const val MONTHS_BEFORE = 12
const val MONTHS_AFTER = 36

/** The index of the current month in the month view's list. */
const val CURRENT_MONTH_INDEX = MONTHS_BEFORE

/** The weekdays' initials over the columns, the weekend ones quieter, as the days under them are. */
@Composable
fun WeekdayRow(firstDayOfWeek: DayOfWeek) {
    val locale = Locale.getDefault()
    val monday = LocalDate.of(2026, 10, 19)
    Row(Modifier.fillMaxWidth().padding(horizontal = 4.dp, vertical = 6.dp).clearAndSetSemantics { }) {
        for (column in 0 until 7) {
            val day = monday.plusDays(Math.floorMod(firstDayOfWeek.value - 1 + column, 7).toLong())
            Text(
                DateWords.weekdayNarrow(day, locale),
                modifier = Modifier.weight(1f),
                style = MaterialTheme.typography.labelMedium,
                textAlign = androidx.compose.ui.text.style.TextAlign.Center,
                color = if (day.dayOfWeek.isWeekend()) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface,
            )
        }
    }
}

internal fun DayOfWeek.isWeekend(): Boolean = this == DayOfWeek.SATURDAY || this == DayOfWeek.SUNDAY

/** One month: its name in large type, then its weeks. */
@Composable
private fun MonthSection(model: CalendarModel, month: CalendarMonth, isLunarShown: Boolean, isCountOnly: Boolean, onOpenDay: (CalendarDay) -> Unit) {
    val rules = model.rules
    val layout = remember(month, rules) { MonthLayout.of(month, rules) }
    val first = month.firstDay.toLocalDate()
    Column(Modifier.fillMaxWidth()) {
        Text(
            DateWords.monthWide(first),
            style = MaterialTheme.typography.headlineSmall,
            modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 20.dp, bottom = 6.dp).semantics { contentDescription = DateWords.monthYear(first) },
        )
        val monthEvents = model.eventsByMonth[month].orEmpty()
        for (week in layout.weeks) {
            MonthWeekRow(model, monthEvents, week, isLunarShown, isCountOnly, onOpenDay)
        }
    }
}

/** A week of a month under a hairline, its number small at the line's left end, and a cell for each of its days in this month. */
@Composable
private fun MonthWeekRow(
    model: CalendarModel,
    monthEvents: Map<CalendarDay, List<CalendarEvent>>,
    week: MonthLayout.Week,
    isLunarShown: Boolean,
    isCountOnly: Boolean,
    onOpenDay: (CalendarDay) -> Unit,
) {
    val today = CalendarDay.of(LocalDate.now(model.rules.zone))
    Column(Modifier.fillMaxWidth().padding(horizontal = 4.dp).padding(bottom = 4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.clearAndSetSemantics { }) {
            Text(week.weekNumber.toString(), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            HorizontalDivider(Modifier.padding(start = 4.dp), color = MaterialTheme.colorScheme.outlineVariant)
        }
        Row(Modifier.fillMaxWidth()) {
            for (column in 0 until 7) {
                val day = week.days[column]
                if (day != null) {
                    MonthDayCell(
                        model = model,
                        day = day,
                        events = monthEvents[day].orEmpty(),
                        isToday = day == today,
                        isWeekend = day.toLocalDate().dayOfWeek.isWeekend(),
                        lunarName = if (isLunarShown) lunarName(day, model.rules.zone) else null,
                        isCountOnly = isCountOnly,
                        modifier = Modifier.weight(1f),
                        onOpen = { onOpenDay(day) },
                    )
                } else {
                    Spacer(Modifier.weight(1f).height(1.dp))
                }
            }
        }
    }
}

/**
 * A day in the month: its number, today's in a red circle as Calendar apps draw it, the lunar date in Chinese, and its
 * first events as chips with "+N" for the rest, or at the largest text sizes only how many there are, since a chip is
 * cut to two letters there. TalkBack reads it as its date and how many events it has either way.
 */
@Composable
fun MonthDayCell(
    model: CalendarModel,
    day: CalendarDay,
    events: List<CalendarEvent>,
    isToday: Boolean,
    isWeekend: Boolean,
    lunarName: String?,
    isCountOnly: Boolean,
    modifier: Modifier = Modifier,
    onOpen: () -> Unit,
) {
    val split = EventChipSplit.of(events.size, CELL_LINE_COUNT)
    val dateText = DateWords.weekdayMonthDayYear(day.toLocalDate())
    val todayText = stringResource(R.string.calendar_today_date, dateText)
    val eventCountText = stringResource(R.string.calendar_event_count, events.size)
    val countColor = events.firstOrNull()?.let { first -> model.color(first) } ?: Color.Transparent
    Column(
        modifier
            .clickable(onClick = onOpen)
            .padding(horizontal = 1.dp)
            .clearAndSetSemantics { contentDescription = (if (isToday) todayText else dateText) + ", " + eventCountText },
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Box(
            Modifier.defaultMinSize(minWidth = 28.dp, minHeight = 28.dp).then(if (isToday) Modifier.background(Color(0xFFE5484D), CircleShape) else Modifier),
            contentAlignment = Alignment.Center,
        ) {
            Text(
                day.day.toString(),
                style = MaterialTheme.typography.labelLarge,
                maxLines = 1,
                softWrap = false,
                // White on the red circle in both appearances, as calendars have it.
                color = if (isToday) Color.White else if (isWeekend) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface,
            )
        }
        if (lunarName != null) {
            Text(lunarName, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
        }
        if (isCountOnly) {
            // A title would be cut to "Pa…", which says nothing; the day's own screen lists the events, so only the
            // count is kept, in the tint of the day's first event. An empty day keeps the line's height, so the weeks
            // stay even.
            Text(
                events.size.toString(),
                style = MaterialTheme.typography.labelSmall,
                modifier = Modifier.alpha(if (events.isEmpty()) 0f else 1f).background(countColor.copy(alpha = 0.22f), RoundedCornerShape(50)).padding(horizontal = 6.dp, vertical = 1.dp),
            )
        } else {
            Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                for (line in 0 until CELL_LINE_COUNT) {
                    if (line < split.chipCount) {
                        EventChip(events[line].let { event -> model.shownTitle(event) }, model.color(events[line]), events[line].isCancelled, events[line].isMarkedPrivate)
                    } else if (line == split.chipCount && split.moreCount > 0) {
                        Text(
                            "+${split.moreCount}",
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            maxLines = 1,
                            modifier = Modifier.fillMaxWidth().padding(horizontal = 3.dp, vertical = 1.dp),
                        )
                    } else {
                        // Keeps every row of the month the same height.
                        EventChip(" ", Color.Transparent, isCancelled = false, isPrivate = false, modifier = Modifier.alpha(0f))
                    }
                }
            }
        }
    }
}

/**
 * An event in a day of the month: a small rounded tag tinted with its calendar's color, a calendar glyph and the title
 * cut short. The words are the primary color, so they read on any calendar's tint in light and dark.
 */
@Composable
fun EventChip(title: String, color: Color, isCancelled: Boolean, isPrivate: Boolean, modifier: Modifier = Modifier) {
    Row(
        modifier.fillMaxWidth().background(color.copy(alpha = 0.22f), RoundedCornerShape(4.dp)).padding(horizontal = 3.dp, vertical = 1.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(Icons.Outlined.CalendarToday, contentDescription = null, tint = color, modifier = Modifier.size(10.dp))
        if (isPrivate) {
            Icon(Icons.Filled.Lock, contentDescription = stringResource(R.string.calendar_private), tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(start = 2.dp).size(9.dp))
        }
        Text(
            title,
            style = MaterialTheme.typography.labelSmall,
            maxLines = 1,
            overflow = TextOverflow.Clip,
            textDecoration = if (isCancelled) TextDecoration.LineThrough else null,
            color = if (isCancelled) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface,
            modifier = Modifier.padding(start = 2.dp),
        )
    }
}

/** The month view's scroll state, opened on the current month. */
@Composable
fun rememberMonthListState(): LazyListState = rememberLazyListState(initialFirstVisibleItemIndex = CURRENT_MONTH_INDEX)
