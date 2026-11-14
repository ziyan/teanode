package com.teanode.app.calendar

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.outlined.Person
import androidx.compose.material.icons.outlined.Repeat
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.kit.calendar.CalendarEvent
import com.teanode.kit.calendar.ServerTime
import kotlinx.coroutines.launch
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId

/** "All day", or the start and end times. */
@Composable
fun eventWhenText(event: CalendarEvent, zone: ZoneId = ZoneId.systemDefault()): String {
    if (event.allDay) return stringResource(R.string.calendar_all_day)
    val start = ServerTime.instant(event.startsAt) ?: return ""
    val end = ServerTime.instant(event.endsAt) ?: start
    return DateWords.shortTime(start, zone) + " - " + DateWords.shortTime(end, zone)
}

/** A change whose answer never came back: it is tried again or stopped before another is made, so nothing happens twice. */
@Composable
fun PendingChangeBox(model: CalendarModel, modifier: Modifier = Modifier) {
    val pending = model.pending ?: return
    val scope = rememberCoroutineScope()
    var isWorking by remember { mutableStateOf(false) }
    fun work(action: suspend () -> Unit) {
        isWorking = true
        scope.launch {
            action()
            isWorking = false
        }
    }
    Column(
        modifier.fillMaxWidth().background(MaterialTheme.colorScheme.surfaceVariant, RoundedCornerShape(12.dp)).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text(stringResource(R.string.calendar_pending_change, pending.summary), style = MaterialTheme.typography.bodyMedium)
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            androidx.compose.material3.Button(onClick = { work { model.retryPending() } }, enabled = !isWorking, modifier = Modifier.weight(1f)) {
                Text(stringResource(R.string.calendar_try_again))
            }
            androidx.compose.material3.OutlinedButton(onClick = { work { model.stopPending() } }, enabled = !isWorking, modifier = Modifier.weight(1f)) {
                Text(stringResource(R.string.calendar_stop))
            }
        }
    }
}

/** A day's events under its date, for the week, five-day and agenda views. */
fun LazyListScope.daySection(model: CalendarModel, day: LocalDate, isEmptyShown: Boolean, onOpen: (CalendarEvent) -> Unit) {
    val events = model.events(day)
    if (events.isEmpty() && !isEmptyShown) return
    item(key = "day-header-$day") {
        val isToday = day == LocalDate.now(model.rules.zone)
        Text(
            DateWords.weekdayMonthDay(day),
            style = MaterialTheme.typography.titleSmall,
            color = if (isToday) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 16.dp, bottom = 8.dp),
        )
    }
    if (events.isEmpty()) {
        item(key = "day-empty-$day") {
            Text(
                stringResource(R.string.calendar_nothing_on_this_day),
                modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
    items(events.size, key = { index -> "event-$day-${events[index].occurrenceId}" }) { index ->
        val event = events[index]
        EventRow(model, event, Modifier.clickable { onOpen(event) })
    }
}

/** One event: its calendar's color, its time, its title and where it is. */
@Composable
fun EventRow(model: CalendarModel, event: CalendarEvent, modifier: Modifier = Modifier) {
    val title = model.shownTitle(event)
    // A busy block already carries the name in its title.
    val ownerName = if (event.isBusy) null else model.ownerName(event)
    val color = model.color(event)
    Row(modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp).height(IntrinsicSize.Min), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
        Box(Modifier.width(4.dp).fillMaxHeight().background(if (event.isCancelled) MaterialTheme.colorScheme.outlineVariant else color, RoundedCornerShape(2.dp)))
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                if (event.isMarkedPrivate) {
                    Icon(Icons.Filled.Lock, contentDescription = stringResource(R.string.calendar_private), modifier = Modifier.size(14.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                Text(title, style = MaterialTheme.typography.bodyLarge, textDecoration = if (event.isCancelled) TextDecoration.LineThrough else null)
            }
            if (ownerName != null) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                    Icon(Icons.Outlined.Person, contentDescription = null, modifier = Modifier.size(14.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                    Text(ownerName, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(eventWhenText(event, model.rules.zone), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                if (event.recurring) {
                    Icon(Icons.Outlined.Repeat, contentDescription = stringResource(R.string.calendar_repeats), modifier = Modifier.size(14.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
            event.location?.takeIf { place -> place.isNotEmpty() }?.let { place ->
                Text(place, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

private val hourHeight: Dp = 56.dp
private val hourLabelWidth: Dp = 52.dp

/** A timed event placed in the day's hours: its place from midnight, how long it lasts, and the column it shares the width in. */
internal data class PlacedEvent(val event: CalendarEvent, val top: Float, val height: Float, val column: Int = 0, val columns: Int = 1)

/** Lays timed events into columns, so ones that overlap sit side by side. */
internal fun placeEvents(events: List<CalendarEvent>, day: LocalDate, zone: ZoneId): List<PlacedEvent> {
    val start = day.atStartOfDay(zone).toInstant()
    val items = events.mapNotNull { event ->
        val from = ServerTime.instant(event.startsAt) ?: return@mapNotNull null
        val until = ServerTime.instant(event.endsAt) ?: from.plusSeconds(1800)
        val top = maxOf(0f, (from.epochSecond - start.epochSecond) / 3600f)
        val bottom = minOf(24f, maxOf(top + 0.25f, (until.epochSecond - start.epochSecond) / 3600f))
        PlacedEvent(event, top, bottom - top)
    }.toMutableList()
    var groupStart = 0
    var columnEnds = ArrayList<Float>()
    var groupEnd = -1f
    for (index in items.indices) {
        if (items[index].top >= groupEnd) {
            for (member in groupStart until index) items[member] = items[member].copy(columns = maxOf(1, columnEnds.size))
            groupStart = index
            columnEnds = ArrayList()
        }
        val free = columnEnds.indexOfFirst { end -> end <= items[index].top }
        if (free >= 0) {
            items[index] = items[index].copy(column = free)
            columnEnds[free] = items[index].top + items[index].height
        } else {
            items[index] = items[index].copy(column = columnEnds.size)
            columnEnds.add(items[index].top + items[index].height)
        }
        groupEnd = maxOf(groupEnd, items[index].top + items[index].height)
    }
    for (member in groupStart until items.size) items[member] = items[member].copy(columns = maxOf(1, columnEnds.size))
    return items
}

/** A day as hours, with all-day events above them and each event placed at its time; events at the same time share the width. */
@Composable
fun DayTimeline(model: CalendarModel, day: LocalDate, onOpen: (CalendarEvent) -> Unit, onCreate: (Instant) -> Unit) {
    val zone = model.rules.zone
    val events = model.events(day)
    val allDay = events.filter { event -> event.allDay }
    val timed = remember(events, day) { placeEvents(events.filter { event -> !event.allDay }, day, zone) }
    val density = LocalDensity.current
    val scale = density.fontScale
    val scaledHourHeight = hourHeight * scale
    val scrollState = rememberScrollState()
    val firstHour = timed.firstOrNull()?.top?.toInt() ?: maxOf(0, java.time.ZonedDateTime.now(zone).hour - 1)
    LaunchedEffect(day, timed.size) {
        scrollState.scrollTo(with(density) { (scaledHourHeight * minOf(firstHour, 20)).roundToPx() })
    }
    Column(Modifier.fillMaxSize()) {
        // All-day events and a pending change stay above the hours, as calendars keep them, rather than scrolling off with the morning.
        if (model.pending != null || allDay.isNotEmpty()) {
            Column(Modifier.padding(horizontal = 16.dp).padding(bottom = 6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                PendingChangeBox(model)
                for (event in allDay) {
                    EventRow(model, event, Modifier.background(MaterialTheme.colorScheme.surfaceVariant, RoundedCornerShape(8.dp)).clickable { onOpen(event) })
                }
            }
        }
        Box(Modifier.weight(1f).verticalScroll(scrollState).padding(horizontal = 16.dp)) {
            Box(
                Modifier.fillMaxWidth().height(scaledHourHeight * 24).pointerInput(day, scaledHourHeight) {
                    detectTapGestures { location ->
                        val halfHourHeight = (scaledHourHeight / 2).toPx()
                        val halfHours = ((location.y - 6.dp.toPx()) / halfHourHeight).toInt().coerceIn(0, 47)
                        onCreate(day.atStartOfDay(zone).toInstant().plusSeconds(halfHours * 1800L))
                    }
                },
            ) {
                Column {
                    for (hour in 0 until 24) {
                        Row(Modifier.height(scaledHourHeight), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                            Text(
                                DateWords.hour(hour),
                                style = MaterialTheme.typography.labelSmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                                modifier = Modifier.width(hourLabelWidth - 6.dp),
                                textAlign = androidx.compose.ui.text.style.TextAlign.End,
                            )
                            HorizontalDivider(Modifier.padding(top = 6.dp), color = MaterialTheme.colorScheme.outlineVariant)
                        }
                    }
                }
                if (day == LocalDate.now(zone)) {
                    val now = java.time.ZonedDateTime.now(zone)
                    val hours = now.hour + now.minute / 60f
                    Row(Modifier.padding(start = hourLabelWidth - 6.dp).offset(y = scaledHourHeight * hours + 2.dp), verticalAlignment = Alignment.CenterVertically) {
                        Box(Modifier.size(8.dp).background(Color(0xFFE5484D), androidx.compose.foundation.shape.CircleShape))
                        Box(Modifier.fillMaxWidth().height(1.5.dp).background(Color(0xFFE5484D)))
                    }
                }
                androidx.compose.foundation.layout.BoxWithConstraints(Modifier.fillMaxSize().padding(start = hourLabelWidth)) {
                    val width = maxWidth
                    for (item in timed) {
                        val color = model.color(item.event)
                        val columnWidth = width / item.columns
                        Column(
                            Modifier
                                .offset(x = columnWidth * item.column, y = scaledHourHeight * item.top + 6.dp)
                                .width(maxOf(0.dp, columnWidth - 2.dp))
                                .height(maxOf(22.dp, scaledHourHeight * item.height))
                                // The tint over the plain background, so the hour lines do not run through it.
                                .background(MaterialTheme.colorScheme.surface, RoundedCornerShape(6.dp))
                                .background(color.copy(alpha = 0.2f), RoundedCornerShape(6.dp))
                                .clickable { onOpen(item.event) }
                                .padding(start = 7.dp, top = 4.dp, end = 4.dp, bottom = 4.dp),
                        ) {
                            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(3.dp)) {
                                if (item.event.isMarkedPrivate) {
                                    Icon(Icons.Filled.Lock, contentDescription = stringResource(R.string.calendar_private), modifier = Modifier.size(10.dp))
                                }
                                Text(
                                    if (item.event.isBusy) model.shownTitle(item.event) else item.event.summary.orEmpty(),
                                    style = MaterialTheme.typography.labelLarge.copy(fontWeight = FontWeight.SemiBold),
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                            }
                            Text(eventWhenText(item.event, zone), style = MaterialTheme.typography.labelSmall, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        }
                        Box(
                            Modifier
                                .offset(x = columnWidth * item.column, y = scaledHourHeight * item.top + 6.dp)
                                .width(3.dp)
                                .height(maxOf(22.dp, scaledHourHeight * item.height))
                                .background(color, RoundedCornerShape(topStart = 6.dp, bottomStart = 6.dp)),
                        )
                    }
                }
            }
        }
    }
}

