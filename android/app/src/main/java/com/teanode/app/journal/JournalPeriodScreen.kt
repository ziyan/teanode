package com.teanode.app.journal

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.automirrored.outlined.ArrowForward
import androidx.compose.material.icons.outlined.EditNote
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.journal.JournalDays
import com.teanode.kit.journal.JournalDaySummary
import com.teanode.kit.journal.JournalFacts
import com.teanode.kit.journal.JournalPeriod
import com.teanode.kit.journal.JournalPeriodKind
import com.teanode.kit.journal.JournalRecordOpening
import kotlinx.coroutines.launch

/**
 * JournalPeriodScreen is a week, a month or a year of the journal: what TeaNode wrote about it, the person's looking
 * back, the days in it (a week as a list, a month as a grid colored by mood, a year as its months), and its totals
 * (ios JournalPeriodViews.swift).
 */
@Composable
fun JournalPeriodScreen(periodKind: JournalPeriodKind, anchorDay: String) {
    val host = rememberJournalModel()
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val session = LocalAppSession.current
    val context = LocalContext.current
    val text = rememberJournalText()
    val scope = rememberCoroutineScope()
    val model = remember(periodKind, anchorDay) { JournalPeriodModel(connection, periodKind, anchorDay, host, context) }
    var isShowingOriginal by remember { mutableStateOf(false) }
    LaunchedEffect(host) { host.ensureContext() }
    LaunchedEffect(model) { model.load() }
    LaunchedEffect(model.isPending) { model.readWhilePending() }
    val today = host.shownToday
    val weekStartsOn = host.weekStartsOn
    /** The first day of the period shown, once it is read. */
    val shownStart = model.period?.periodStart ?: JournalDays.periodStart(model.anchorDay, periodKind, weekStartsOn) ?: model.anchorDay

    fun move(count: Int) {
        val moved = JournalDays.movedPeriodStart(shownStart, periodKind, count) ?: return
        scope.launch { model.show(moved); isShowingOriginal = false }
    }

    TeaNodeScreen(
        title = text.periodTitle(periodKind, shownStart).let { title -> text.periodSubtitle(periodKind, shownStart).let { subtitle -> if (subtitle.isEmpty()) title else "$title $subtitle" } },
        onBack = { navigator.back() },
        actions = {
            IconButton(onClick = { move(-1) }) { Icon(Icons.AutoMirrored.Outlined.ArrowBack, contentDescription = stringResource(R.string.journal_before)) }
            IconButton(onClick = { move(1) }, enabled = !(model.period?.isCurrent ?: true)) { Icon(Icons.AutoMirrored.Outlined.ArrowForward, contentDescription = stringResource(R.string.journal_after)) }
            ZoomMenu(JournalZoom.of(periodKind)) { zoom ->
                // A day is the period's last day that has been, so a screen for today is not left for a day in the future.
                val zoomDay = model.period?.let { period -> if (period.isCurrent) today else period.periodStart } ?: model.anchorDay
                navigator.back()
                navigator.navigate(JournalRoutes.zoomed(zoom, zoomDay))
            }
        },
    ) { contentPadding ->
        val loadFailure = model.loadFailure
        val period = model.period
        when {
            loadFailure != null -> ErrorState(session.describeLoadProblem(loadFailure), onRetry = { scope.launch { model.load() } }, modifier = Modifier.padding(contentPadding))
            period == null -> LoadingState(Modifier.padding(contentPadding))
            else -> LazyColumn(Modifier.fillMaxSize().padding(contentPadding)) {
                // The story.
                item(key = "story-header") { Heading(stringResource(when (periodKind) { JournalPeriodKind.Week -> R.string.journal_the_week_as_teanode_saw_it; JournalPeriodKind.Month -> R.string.journal_the_month_as_teanode_saw_it; JournalPeriodKind.Year -> R.string.journal_the_year_as_teanode_saw_it })) }
                journalNarrativeRows(
                    period.narrative,
                    emptyTextResource = when (periodKind) { JournalPeriodKind.Week -> R.string.journal_week_no_story; JournalPeriodKind.Month -> R.string.journal_month_no_story; JournalPeriodKind.Year -> R.string.journal_year_no_story },
                    writeNowTitleResource = R.string.journal_write_now, isBusy = model.isBusy,
                    onOpen = { record -> openJournalRecord(navigator, JournalRecordOpening.of(record)) },
                    onCorrect = { navigator.navigate(JournalRoutes.correct(periodKind, model.anchorDay)) },
                    onWriteAgain = { scope.launch { model.writeAgain() } },
                    onRemoveStory = { scope.launch { model.removeNarrative() } },
                    onRemoveCorrection = { index -> scope.launch { model.removeCorrection(index) } },
                )
                if (period.narrative?.hasNarrative == true) item(key = "story-footer") { Footnote(stringOf(R.string.journal_reads_your_journal)) }
                // Looking back.
                val words = period.lookingBack
                item(key = "looking-back-header") { Heading(stringOf(R.string.journal_looking_back)) }
                journalWordsRows(words, isShowingOriginal) { isOriginal -> isShowingOriginal = isOriginal }
                item(key = "looking-back-write") {
                    JournalAction(stringOf(if (words?.hasOriginal == true) R.string.journal_edit_looking_back else R.string.journal_write_looking_back), Icons.Outlined.EditNote, onClick = { navigator.navigate(JournalRoutes.write(periodKind, model.anchorDay)) })
                }
                item(key = "looking-back-footer") { Footnote(stringOf(R.string.journal_your_own_words_about_stretch)) }
                // The days.
                when (periodKind) {
                    JournalPeriodKind.Week -> weekDays(period, text) { day -> navigator.navigate(JournalRoutes.day(day)) }
                    JournalPeriodKind.Month -> monthGrid(period, weekStartsOn, today, text) { day -> navigator.navigate(JournalRoutes.day(day)) }
                    JournalPeriodKind.Year -> yearMonths(period, text) { start -> navigator.navigate(JournalRoutes.period(JournalPeriodKind.Month, start)) }
                }
                // Mood and numbers.
                if (period.moodCounts.any { count -> count > 0 }) {
                    item(key = "mood-header") { Heading(stringOf(R.string.journal_how_the_days_were)) }
                    item(key = "mood-bar") { JournalMoodBar(period.moodCounts, Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) }
                }
                totalsSection(period.totals, text)
                // Chapters and what was chosen.
                if (period.chapters.isNotEmpty()) {
                    item(key = "chapters-header") { Heading(stringOf(R.string.journal_chapters)) }
                    items(period.chapters.size, key = { index -> "chapter-$index" }) { index ->
                        val chapter = period.chapters[index]
                        Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                            Text(chapter.title, style = MaterialTheme.typography.titleMedium)
                            Text(text.dayRangeText(chapter.fromDay, chapter.untilDay), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            if (chapter.detail.isNotEmpty()) Text(chapter.detail, style = MaterialTheme.typography.bodyLarge.copy(fontFamily = journalWritingFace))
                        }
                    }
                }
                val links = period.choices.filter { link -> !link.isHidden && link.record?.isUnavailable == false }
                if (links.isNotEmpty()) {
                    item(key = "moments-header") { Heading(stringOf(R.string.journal_moments)) }
                    items(links, key = { link -> "choice-${link.linkId}" }) { link ->
                        val record = link.record ?: return@items
                        JournalRecordRow(record.recordKind, record.title, detail = link.linkReason, opening = JournalRecordOpening.of(record), onOpen = { opening -> openJournalRecord(navigator, opening) })
                    }
                }
            }
        }
    }
}

private fun LazyListScope.weekDays(period: JournalPeriod, text: JournalText, onOpenDay: (String) -> Unit) {
    item(key = "days-header") { Heading(stringOf(R.string.journal_the_days)) }
    items(period.days, key = { summary -> "week-day-${summary.day}" }) { summary -> JournalDayRow(summary, text, Modifier.clickable { onOpenDay(summary.day) }) }
}

// MARK: A month

private fun LazyListScope.monthGrid(period: JournalPeriod, weekStartsOn: String, today: String, text: JournalText, onOpenDay: (String) -> Unit) {
    item(key = "days-header") { Heading(stringOf(R.string.journal_the_days)) }
    item(key = "month-grid") {
        val rows = JournalDays.monthRows(period.periodStart, weekStartsOn)
        val summaries = period.days.associateBy { summary -> summary.day }
        val firstWeekStart = JournalDays.weekStart(period.periodStart, weekStartsOn) ?: period.periodStart
        Column(Modifier.padding(horizontal = 12.dp, vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                for (header in JournalDays.weekDays(firstWeekStart)) {
                    Text(text.weekdayInitialText(header), Modifier.weight(1f), style = MaterialTheme.typography.labelMedium, textAlign = TextAlign.Center, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
            for (row in rows) {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                    for (day in row) DayCell(day, day?.let { found -> summaries[found] }, today, text, Modifier.weight(1f), onOpenDay)
                }
            }
        }
    }
    item(key = "month-footer") { Footnote(stringOf(R.string.journal_each_day_shows_photo)) }
}

/**
 * A day of the month: its number on the first photo of the day, or on the color of its mood when it has no photo. A day
 * with both has its mood as the edge of the photo.
 */
@Composable
private fun DayCell(day: String?, summary: JournalDaySummary?, today: String, text: JournalText, modifier: Modifier, onOpenDay: (String) -> Unit) {
    if (day == null) {
        Box(modifier.height(40.dp))
        return
    }
    val moodTint = summary?.mood?.level?.let(::journalMoodColor)
    val photoId = summary?.shownPhotoId
    val isFuture = day > today
    val description = text.longDayText(day) + (summary?.mood?.level?.let { level -> ", " + text.moodName(level) } ?: "")
    Box(
        modifier.height(40.dp).clip(RoundedCornerShape(8.dp)).clickable(enabled = !isFuture) { onOpenDay(day) }.semantics { contentDescription = description },
        contentAlignment = Alignment.Center,
    ) {
        if (photoId != null) {
            JournalPhotoImage(photoId, Modifier.fillMaxSize().clip(RoundedCornerShape(8.dp)).then(if (moodTint != null) Modifier.border(3.dp, moodTint, RoundedCornerShape(8.dp)) else Modifier))
        } else if (moodTint != null) {
            Box(Modifier.fillMaxSize().padding(2.dp).background(moodTint.copy(alpha = 0.4f), CircleShape))
        }
        Text(
            text.dayNumberText(day), style = MaterialTheme.typography.bodyMedium, fontWeight = if (day == today) FontWeight.Bold else FontWeight.Normal,
            color = if (isFuture) MaterialTheme.colorScheme.outline else MaterialTheme.colorScheme.onSurface,
            modifier = Modifier.then(if (photoId != null) Modifier.background(MaterialTheme.colorScheme.surface.copy(alpha = 0.75f), RoundedCornerShape(50)).padding(horizontal = 6.dp) else Modifier),
        )
    }
}

// MARK: A year

private fun LazyListScope.yearMonths(period: JournalPeriod, text: JournalText, onOpenMonth: (String) -> Unit) {
    item(key = "months-header") { Heading(stringOf(R.string.journal_the_months)) }
    items(period.months, key = { month -> "month-${month.periodStart}" }) { month ->
        Column(Modifier.fillMaxWidth().clickable { onOpenMonth(month.periodStart) }.padding(horizontal = 16.dp, vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(text.monthText(month.periodStart), style = MaterialTheme.typography.titleMedium)
            if (month.headline.isNotEmpty()) Text(month.headline, style = MaterialTheme.typography.bodyLarge.copy(fontFamily = journalWritingFace), maxLines = 2)
            JournalMoodStrip(month.dayMoods)
        }
    }
}

// MARK: Numbers

private fun LazyListScope.totalsSection(totals: JournalFacts?, text: JournalText) {
    if (totals == null) return
    val rows = ArrayList<Pair<Int, String>>()
    if (totals.photoCount > 0) rows.add(R.string.journal_photos to text.countText(totals.photoCount))
    if (totals.placeCount > 0) rows.add(R.string.journal_places to text.countText(totals.placeCount))
    if (totals.walkedMeters > 0) rows.add(R.string.journal_walked_label to text.distanceText(totals.walkedMeters))
    if (totals.stepCount > 0) rows.add(R.string.journal_steps to text.countText(totals.stepCount))
    if (totals.sleepMinutes > 0) rows.add(R.string.journal_sleep to text.durationText(totals.sleepMinutes))
    if (totals.workoutMinutes > 0) rows.add(R.string.journal_workouts to text.durationText(totals.workoutMinutes))
    val workText = text.workText(totals.codingMinutes, totals.projectCount)
    if (workText.isNotEmpty()) rows.add(R.string.journal_work to workText)
    if (totals.meetingCount > 0) rows.add(R.string.journal_meetings to text.countText(totals.meetingCount))
    if (totals.mailReceivedCount > 0) rows.add(R.string.journal_mail_received to text.countText(totals.mailReceivedCount))
    if (totals.spentMinorUnits > 0) rows.add(R.string.journal_spent to text.moneyText(totals.spentMinorUnits, totals.spentCurrency))
    if (rows.isEmpty()) return
    item(key = "totals-header") { Heading(stringOf(R.string.journal_in_numbers)) }
    items(rows, key = { row -> "total-${row.first}" }) { (title, value) -> LabeledLine(stringOf(title), value) }
}

