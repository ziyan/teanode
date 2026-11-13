package com.teanode.app.journal

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.background
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CalendarMonth
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.EditNote
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.SearchOff
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SelectableDates
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.journal.JournalDays
import com.teanode.kit.journal.JournalDay
import com.teanode.kit.journal.JournalDaySummary
import com.teanode.kit.journal.JournalMoodLevel
import com.teanode.kit.journal.JournalSearchHit
import kotlinx.coroutines.launch
import java.time.ZoneOffset

/**
 * The Journal: the person's days, newest first, in weeks. A week has its headline as a row that opens the week; a day
 * opens the day. The menu opens the current week, month or year, the calendar button chooses a day, the pencil writes
 * about today, and the search field finds the days and periods that hold some words (ios JournalViews.swift).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun JournalSection() {
    val model = rememberJournalModel()
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val session = LocalAppSession.current
    val text = rememberJournalText()
    val scope = rememberCoroutineScope()
    val search = remember(model) { JournalSearchModel(connection, model) }
    var isChoosingDay by remember { mutableStateOf(false) }
    var isMenuOpen by remember { mutableStateOf(false) }
    LaunchedEffect(model) { model.load() }
    LaunchedEffect(search.query) { search.search() }
    val searchWords = search.searchWords

    TeaNodeScreen(
        title = stringResource(R.string.journal_title),
        actions = {
            IconButton(onClick = { isChoosingDay = true }) { Icon(Icons.Outlined.CalendarMonth, contentDescription = stringResource(R.string.journal_choose_a_day)) }
            IconButton(onClick = { navigator.navigate(JournalRoutes.write(null, model.shownToday)) }) { Icon(Icons.Outlined.EditNote, contentDescription = stringResource(R.string.journal_write_today)) }
            Box {
                IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.journal_show)) }
                DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                    for (zoom in JournalZoom.entries) {
                        DropdownMenuItem(
                            text = { Text(zoomTitle(zoom), color = if (zoom == JournalZoom.Days) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface) },
                            onClick = {
                                isMenuOpen = false
                                if (zoom != JournalZoom.Days) navigator.navigate(JournalRoutes.zoomed(zoom, model.shownToday))
                            },
                        )
                    }
                    DropdownMenuItem(text = { Text(stringResource(R.string.journal_settings)) }, onClick = { isMenuOpen = false; navigator.navigate(JournalRoutes.SETTINGS) })
                }
            }
        },
    ) { contentPadding ->
        Column(Modifier.fillMaxSize().padding(contentPadding)) {
            OutlinedTextField(
                value = search.query, onValueChange = { words -> search.query = words }, placeholder = { Text(stringResource(R.string.journal_search_prompt)) },
                leadingIcon = { Icon(Icons.Outlined.Search, contentDescription = null) },
                trailingIcon = { if (search.query.isNotEmpty()) IconButton(onClick = { search.query = "" }) { Icon(Icons.Outlined.Close, contentDescription = stringResource(R.string.journal_clear)) } },
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), singleLine = true,
            )
            val loadFailure = model.loadFailure
            when {
                searchWords != null -> JournalSearchResults(search, searchWords, text, onOpen = { hit -> navigator.navigate(destinationOf(hit)) })
                loadFailure != null -> ErrorState(session.describeLoadProblem(loadFailure), onRetry = { scope.launch { model.load() } })
                !model.hasLoaded -> LoadingState()
                else -> PullToRefreshBox(isRefreshing = false, onRefresh = { scope.launch { model.load(isRefreshing = true) } }, modifier = Modifier.weight(1f)) {
                    LazyColumn(Modifier.fillMaxSize()) {
                        item(key = "today") {
                            JournalTodayCard(model.shownToday, model.todayDay, text, onSetMood = { level -> scope.launch { model.setTodayMood(level) } }, onWrite = { navigator.navigate(JournalRoutes.write(null, model.shownToday)) })
                        }
                        for (group in model.weekGroups) {
                            item(key = "week-header-${group.weekStart}") { Heading(text.weekTitle(group.weekStart)) }
                            item(key = "week-${group.weekStart}") {
                                JournalWeekRow(model.weekHeadlines[group.weekStart].orEmpty(), Modifier.clickable { navigator.navigate(JournalRoutes.period(com.teanode.kit.journal.JournalPeriodKind.Week, group.weekStart)) })
                            }
                            items(group.days, key = { summary -> "day-${summary.day}" }) { summary ->
                                JournalDayRow(summary, text, Modifier.clickable { navigator.navigate(JournalRoutes.day(summary.day)) })
                                LaunchedEffect(summary.id, model.days.size) { model.loadMore(summary) }
                            }
                        }
                        if (model.days.isEmpty()) {
                            item(key = "empty") { Text(stringResource(R.string.journal_days_appear_here), modifier = Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
                        }
                    }
                }
            }
        }
    }
    if (isChoosingDay) {
        JournalDayPickerDialog(model.shownToday, model.firstDay, onDismiss = { isChoosingDay = false }) { chosenDay ->
            isChoosingDay = false
            navigator.navigate(JournalRoutes.day(chosenDay))
        }
    }
}

private fun destinationOf(hit: JournalSearchHit): String = hit.kind?.let { kind -> JournalRoutes.period(kind, hit.day) } ?: JournalRoutes.day(hit.day)

/** What the Journal shows while words are typed in its search field: the days and periods that hold them, newest first, a page at a time. */
@Composable
fun JournalSearchResults(model: JournalSearchModel, searchWords: String, text: JournalText, onOpen: (JournalSearchHit) -> Unit) {
    val session = LocalAppSession.current
    val scope = rememberCoroutineScope()
    val failure = model.failure
    when {
        failure != null && model.searchedQuery == searchWords -> ErrorState(session.describeLoadProblem(failure), onRetry = { scope.launch { model.search(isDebounced = false) } })
        model.hits.isEmpty() && model.searchedQuery != searchWords -> LoadingState()
        model.hits.isEmpty() -> EmptyState(Icons.Outlined.SearchOff, stringResource(R.string.journal_nothing_matches), description = stringResource(R.string.journal_nothing_has, searchWords))
        else -> LazyColumn(Modifier.fillMaxSize()) {
            item(key = "results-header") { Heading(stringResource(R.string.journal_results_for, model.searchedQuery)) }
            items(model.hits, key = { hit -> "hit-${hit.id}" }) { hit ->
                Column(Modifier.fillMaxWidth().clickable { onOpen(hit) }.padding(horizontal = 16.dp, vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                    Text(text.searchHitTitle(hit), style = MaterialTheme.typography.titleMedium)
                    if (hit.headline.isNotEmpty()) Text(hit.headline, style = MaterialTheme.typography.bodyLarge.copy(fontFamily = journalWritingFace), maxLines = 2, overflow = TextOverflow.Ellipsis)
                    if (hit.snippet.isNotEmpty()) Text(hit.snippet, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 3, overflow = TextOverflow.Ellipsis)
                }
                LaunchedEffect(hit.id, model.hits.size) { model.loadMore(hit) }
            }
            if (model.isLoadingMore) item(key = "loading-more") { Box(Modifier.fillMaxWidth().padding(16.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() } }
        }
    }
}

/**
 * Today at the top of the timeline: the weather and what the day has held so far, the five faces to say how it is
 * going, and the one button that writes about it.
 */
@Composable
fun JournalTodayCard(today: String, day: JournalDay?, text: JournalText, onSetMood: (JournalMoodLevel) -> Unit, onWrite: () -> Unit) {
    Column(Modifier.fillMaxWidth().padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(stringResource(R.string.journal_today), style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
            Text(text.longDayText(today), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        soFarText(day, text)?.let { line -> Text(line, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant) }
        JournalMoodPicker(day?.mood?.level, onSetMood)
        Button(onClick = onWrite, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.journal_write_about_today)) }
    }
}

/** "61° and clear. 2 meetings, 3,880 steps so far." */
@Composable
private fun soFarText(day: JournalDay?, text: JournalText): String? {
    val sentences = ArrayList<String>()
    day?.weather?.let { weather ->
        val name = text.weatherName(weather.kind)
        val temperature = text.temperatureText(weather.highCelsius)
        sentences.add(if (name.isEmpty()) temperature else stringResource(R.string.journal_temperature_and, temperature, name))
    }
    val counted = ArrayList<String>()
    day?.facts?.let { facts ->
        if (facts.meetingCount > 0) counted.add(pluralStringResource(R.plurals.journal_n_meetings, facts.meetingCount, facts.meetingCount))
        if (facts.stepCount > 0) counted.add(stringResource(R.string.journal_n_steps_so_far, text.countText(facts.stepCount)))
    }
    if (counted.isNotEmpty()) sentences.add(counted.joinToString(", ") + ".")
    return if (sentences.isEmpty()) null else sentences.joinToString(" ")
}

/** The row at the top of a week that opens it: the week's headline in the writing face, or the plain words when it has none yet. */
@Composable
fun JournalWeekRow(headline: String, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        if (headline.isEmpty()) {
            Text(stringResource(R.string.journal_open_the_week))
        } else {
            Text(headline, style = MaterialTheme.typography.bodyLarge.copy(fontFamily = journalWritingFace))
            Text(stringResource(R.string.journal_read_the_week), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.primary)
        }
    }
}

/** A day in the timeline: its date, what the day was in a line or two, its mood and numbers, and the photo that stands for it. */
@Composable
fun JournalDayRow(summary: JournalDaySummary, text: JournalText, modifier: Modifier = Modifier) {
    Row(modifier.fillMaxWidth().heightIn(min = 52.dp).padding(horizontal = 16.dp, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.width(44.dp), horizontalAlignment = Alignment.CenterHorizontally) {
            Text(text.weekdayText(summary.day).uppercase(), style = MaterialTheme.typography.labelSmall, fontWeight = FontWeight.SemiBold, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
            Text(text.dayNumberText(summary.day), style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
        }
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
            when {
                summary.headline.isNotEmpty() -> Text(summary.headline, style = MaterialTheme.typography.bodyLarge.copy(fontFamily = journalWritingFace), maxLines = 2, overflow = TextOverflow.Ellipsis)
                summary.wordsExcerpt.isNotEmpty() -> Text(summary.wordsExcerpt, style = MaterialTheme.typography.bodyLarge.copy(fontFamily = journalWritingFace), maxLines = 2, overflow = TextOverflow.Ellipsis)
                else -> Text(stringResource(R.string.journal_nothing_written_yet), color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                summary.mood?.level?.let { level -> Box(Modifier.size(9.dp).background(journalMoodColor(level), CircleShape)) }
                val parts = ArrayList<String>()
                summary.mood?.level?.let { level -> parts.add(text.moodName(level)) }
                text.factsLine(summary).takeIf { line -> line.isNotEmpty() }?.let { line -> parts.add(line) }
                Text(parts.joinToString(", "), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
        summary.shownPhotoId?.let { photoId -> JournalPhotoImage(photoId, Modifier.size(52.dp).clip(RoundedCornerShape(10.dp))) }
    }
}

/** Chooses a day to open, from the first day that has anything to today. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun JournalDayPickerDialog(today: String, firstDay: String, onDismiss: () -> Unit, onOpen: (String) -> Unit) {
    val latest = JournalDays.date(today) ?: java.time.LocalDate.now()
    val tenYearsBack = latest.minusYears(10)
    // The first day with anything in it, or ten years back; never later than the latest day.
    val earliest = minOf(JournalDays.date(firstDay) ?: tenYearsBack, latest)
    val latestMillis = latest.atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli()
    val earliestMillis = earliest.atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli()
    val state = rememberDatePickerState(
        initialSelectedDateMillis = latestMillis,
        selectableDates = object : SelectableDates {
            override fun isSelectableDate(utcTimeMillis: Long): Boolean = utcTimeMillis in earliestMillis..latestMillis
        },
    )
    DatePickerDialog(
        onDismissRequest = onDismiss,
        confirmButton = {
            TextButton(onClick = {
                state.selectedDateMillis?.let { millis -> onOpen(JournalDays.text(java.time.Instant.ofEpochMilli(millis).atZone(ZoneOffset.UTC).toLocalDate())) }
            }) { Text(stringResource(R.string.journal_open)) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.journal_cancel)) } },
    ) { DatePicker(state) }
}

