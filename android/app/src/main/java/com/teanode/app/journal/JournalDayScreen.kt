package com.teanode.app.journal

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.automirrored.outlined.ArrowForward
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.EditNote
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.AcUnit
import androidx.compose.material.icons.outlined.Bolt
import androidx.compose.material.icons.outlined.Cloud
import androidx.compose.material.icons.outlined.PushPin
import androidx.compose.material.icons.outlined.Umbrella
import androidx.compose.material.icons.outlined.WbCloudy
import androidx.compose.material.icons.outlined.WbSunny
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import com.teanode.app.R
import com.teanode.app.location.LocationRoutes
import com.teanode.app.location.MapContent
import com.teanode.app.location.MapCoordinate
import com.teanode.app.location.MapFocus
import com.teanode.app.location.MapMarker
import com.teanode.app.location.TeaNodeMap
import com.teanode.app.goals.ChatHandoff
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.rememberServerPicture
import com.teanode.kit.AppSection
import com.teanode.kit.calendar.ServerTime
import com.teanode.kit.journal.JournalCodingSession
import com.teanode.kit.journal.JournalDay
import com.teanode.kit.journal.JournalDays
import com.teanode.kit.journal.JournalHealth
import com.teanode.kit.journal.JournalLink
import com.teanode.kit.journal.JournalMail
import com.teanode.kit.journal.JournalMoment
import com.teanode.kit.journal.JournalRecordOpening
import com.teanode.kit.journal.JournalRows
import com.teanode.kit.journal.JournalVisit
import kotlinx.coroutines.launch

private const val STRIP_PHOTO_COUNT = 24

/** The UI state of a day screen that the model does not keep: which folded parts are open, which long lists show all their rows, and the viewer. */
private class DayScreenState {
    var isShowingOriginal by mutableStateOf(false)
    var expandedParts by mutableStateOf(emptySet<String>())
    var fullLists by mutableStateOf(emptySet<String>())
    var openedPhotoId by mutableStateOf<String?>(null)

    fun toggleList(list: String) {
        fullLists = if (list in fullLists) fullLists - list else fullLists + list
    }

    fun togglePart(part: String) {
        expandedParts = if (part in expandedParts) expandedParts - part else expandedParts + part
    }
}

/**
 * JournalDayScreen is one day. The buttons in the bar go to the day before and the day after, and the menu opens the
 * week, month or year the day is in (ios JournalDayViews.swift).
 */
@Composable
fun JournalDayScreen(initialDay: String) {
    val host = rememberJournalModel()
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val session = LocalAppSession.current
    val context = LocalContext.current
    val text = rememberJournalText()
    val scope = rememberCoroutineScope()
    val model = remember(initialDay) { JournalDayModel(connection, initialDay, host, context) }
    val state = remember { DayScreenState() }
    LaunchedEffect(host) { host.ensureContext() }
    LaunchedEffect(model) { model.load() }
    // While the touched-up words or the story are being written, the day is read again every few seconds.
    LaunchedEffect(model.isPending) { model.readWhilePending() }
    val today = host.shownToday
    val isLatestDay = model.day >= today

    fun move(dayCount: Int) {
        val moved = JournalDays.adding(dayCount, model.day) ?: return
        scope.launch { model.show(moved); state.isShowingOriginal = false; state.fullLists = emptySet() }
    }

    TeaNodeScreen(
        title = text.dayTitle(model.day) + " " + text.daySubtitle(model.day, today),
        onBack = { navigator.back() },
        actions = {
            IconButton(onClick = { move(-1) }) { Icon(Icons.AutoMirrored.Outlined.ArrowBack, contentDescription = stringResource(R.string.journal_the_day_before)) }
            IconButton(onClick = { move(1) }, enabled = !isLatestDay) { Icon(Icons.AutoMirrored.Outlined.ArrowForward, contentDescription = stringResource(R.string.journal_the_day_after)) }
            ZoomMenu(JournalZoom.Days) { zoom -> navigator.back(); navigator.navigate(JournalRoutes.zoomed(zoom, model.day)) }
        },
    ) { contentPadding ->
        val loadFailure = model.loadFailure
        val journalDay = model.journalDay
        when {
            loadFailure != null -> ErrorState(session.describeLoadProblem(loadFailure), onRetry = { scope.launch { model.load() } }, modifier = Modifier.padding(contentPadding))
            journalDay == null -> LoadingState(Modifier.padding(contentPadding))
            else -> DayContent(host, model, journalDay, state, Modifier.padding(contentPadding))
        }
    }
    val openedPhoto = state.openedPhotoId
    if (openedPhoto != null) {
        JournalPhotoViewer(model.journalDay?.photos.orEmpty().take(STRIP_PHOTO_COUNT).map { photo -> photo.photoId }.ifEmpty { listOf(openedPhoto) }, openedPhoto, onClose = { state.openedPhotoId = null })
    }
}

/** The menu that opens the same date as a day or as another kind of period. */
@Composable
fun ZoomMenu(current: JournalZoom, onZoom: (JournalZoom) -> Unit) {
    var isOpen by remember { mutableStateOf(false) }
    Box {
        IconButton(onClick = { isOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.journal_show)) }
        DropdownMenu(expanded = isOpen, onDismissRequest = { isOpen = false }) {
            for (zoom in JournalZoom.entries) {
                DropdownMenuItem(
                    text = { Text(zoomTitle(zoom), color = if (zoom == current) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface) },
                    onClick = {
                        isOpen = false
                        if (zoom != current) onZoom(zoom)
                    },
                )
            }
        }
    }
}

@Composable
fun zoomTitle(zoom: JournalZoom): String = stringResource(
    when (zoom) {
        JournalZoom.Days -> R.string.journal_days
        JournalZoom.Weeks -> R.string.journal_weeks
        JournalZoom.Months -> R.string.journal_months
        JournalZoom.Years -> R.string.journal_years
    },
)

@Composable
private fun DayContent(host: JournalModel, model: JournalDayModel, journalDay: JournalDay, state: DayScreenState, modifier: Modifier) {
    val navigator = LocalAppNavigator.current
    val scope = rememberCoroutineScope()
    val text = rememberJournalText()
    val chosenDocuments = journalDay.documents.filter { document ->
        val hiddenIds = journalDay.choices.filter { link -> link.isHidden && link.record?.recordKind == "agent_document" }.mapNotNull { link -> link.record?.recordId }.toSet()
        document.isChosen && document.documentId !in hiddenIds
    }

    /** Opens a record the day mentions. A photo of the day opens in the viewer over it, and the person stays on the day; anything else goes to the part of the app that shows it. */
    fun open(opening: JournalRecordOpening) {
        if (opening is JournalRecordOpening.Photo && journalDay.photos.take(STRIP_PHOTO_COUNT).any { photo -> photo.photoId == opening.id }) {
            state.openedPhotoId = opening.id
            return
        }
        openJournalRecord(navigator, opening)
    }

    LazyColumn(modifier.fillMaxSize()) {
        moodSection(journalDay, text, onChoose = { level -> scope.launch { model.setMood(level) } })
        wordsSection(journalDay, state, onWrite = { navigator.navigate(JournalRoutes.write(null, model.day)) })
        narrativeSection(journalDay, model, scope, onOpen = { record -> open(JournalRecordOpening.of(record)) },
            onCorrect = { navigator.navigate(JournalRoutes.correct(null, model.day)) })
        workSection(journalDay, chosenDocuments, state, text, model, scope)
        workChatSection(journalDay, chosenDocuments.map { document -> document.documentId }.toSet(), state)
        chosenSection(journalDay, state, model, scope, onOpen = ::open)
        mailSection(journalDay, state, model, scope, onOpen = ::open)
        if (journalDay.photos.isNotEmpty()) photosSection(journalDay, state, onOpenInPhotos = { navigator.openSection(AppSection.Photos) })
        if (journalDay.moments.isNotEmpty()) momentsSection(journalDay, state, text, model, scope, onOpen = ::open)
        if (journalDay.visits.isNotEmpty()) placesSection(journalDay, state, text, onOpenPlaces = { navigator.navigate(JournalDayPlaces.route(journalDay.day)) })
        talkedSection(journalDay, state, onOpenConversation = { id -> ChatHandoff.open(navigator, id) })
        if (journalDay.recordings.isNotEmpty()) recordingsSection(journalDay, state, text, onOpen = ::open)
        journalDay.health?.takeIf { health -> hasHealth(health) }?.let { health -> healthSection(health, text) }
        if (journalDay.transactions.isNotEmpty()) moneySection(journalDay, state, text, onOpenFinance = { navigator.openSection(AppSection.Finance) })
        if (journalDay.events.isNotEmpty() || journalDay.reminders.isNotEmpty()) calendarSection(journalDay, state, text, onOpen = ::open)
        if (journalDay.shoppingTrips.isNotEmpty()) shoppingSection(journalDay, state, text, onOpen = ::open)
        knowledgeSection(journalDay, state, onOpen = ::open)
    }
}

// MARK: Folded parts and long lists

private fun <Element> shown(state: DayScreenState, rows: List<Element>, list: String, limit: Int = JournalRows.LIMIT): List<Element> =
    JournalRows.shown(rows, limit, list in state.fullLists)

private fun <Element> LazyListScope.showAllRow(state: DayScreenState, rows: List<Element>, list: String, limit: Int = JournalRows.LIMIT) {
    if (!JournalRows.hasMore(rows, limit)) return
    item(key = "show-all-$list") {
        TextButton(onClick = { state.toggleList(list) }, modifier = Modifier.padding(horizontal = 8.dp)) {
            Text(if (list in state.fullLists) stringResource(R.string.journal_show_fewer) else stringResource(R.string.journal_show_all_n, rows.size))
        }
    }
}

private fun LazyListScope.foldedPart(state: DayScreenState, part: String, label: @Composable () -> Unit, content: LazyListScope.() -> Unit) {
    item(key = "fold-$part") {
        val isOpen = part in state.expandedParts
        Row(Modifier.fillMaxWidth().clickable { state.togglePart(part) }.padding(horizontal = 16.dp, vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.weight(1f)) { label() }
            Icon(if (isOpen) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
    if (part in state.expandedParts) content()
}

// MARK: How the day was

private fun LazyListScope.moodSection(journalDay: JournalDay, text: JournalText, onChoose: (com.teanode.kit.journal.JournalMoodLevel) -> Unit) {
    item(key = "mood-header") { Heading(stringOf(R.string.journal_how_was_the_day)) }
    item(key = "mood-picker") { JournalMoodPicker(journalDay.mood?.level, onChoose, Modifier.padding(horizontal = 16.dp)) }
    journalDay.mood?.let { mood ->
        item(key = "mood-text") {
            Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                val level = mood.level
                Text(if (level == null) mood.moodWords else if (mood.moodWords.isEmpty()) text.moodName(level) else text.moodName(level) + ": " + mood.moodWords)
                if (mood.isFromHealth) Text(stringOf(R.string.journal_from_health), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
    journalDay.weather?.let { weather ->
        item(key = "weather") {
            val line = text.weatherLine(weather.highCelsius, weather.lowCelsius, weather.kind)
            Row(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
                Icon(weatherIcon(weather.kind), contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
                Text(if (weather.placeName.isEmpty()) line else line + ", " + weather.placeName)
            }
        }
    }
}

// MARK: The person's words

private fun LazyListScope.wordsSection(journalDay: JournalDay, state: DayScreenState, onWrite: () -> Unit) {
    val words = journalDay.words
    item(key = "words-header") { Heading(stringOf(R.string.journal_your_words)) }
    journalWordsRows(words, state.isShowingOriginal) { isOriginal -> state.isShowingOriginal = isOriginal }
    item(key = "words-write") {
        JournalAction(stringOf(if (words?.hasOriginal == true) R.string.journal_edit_your_words else R.string.journal_write_about_this_day), Icons.Outlined.EditNote, onClick = onWrite)
    }
}

// MARK: The story

private fun LazyListScope.narrativeSection(
    journalDay: JournalDay,
    model: JournalDayModel,
    scope: kotlinx.coroutines.CoroutineScope,
    onOpen: (com.teanode.kit.journal.JournalRecord) -> Unit,
    onCorrect: () -> Unit,
) {
    val narrative = journalDay.narrative
    item(key = "story-header") { Heading(stringOf(R.string.journal_the_day_as_teanode_saw_it)) }
    journalNarrativeRows(
        narrative,
        emptyTextResource = if (journalDay.isToday) R.string.journal_story_empty_today else R.string.journal_story_empty_day,
        writeNowTitleResource = if (journalDay.isToday) R.string.journal_write_today_so_far else R.string.journal_write_now,
        isBusy = model.isBusy, onOpen = onOpen, onCorrect = onCorrect,
        onWriteAgain = { scope.launch { model.writeAgain() } },
        onRemoveStory = { scope.launch { model.removeNarrative() } },
        onRemoveCorrection = { index -> scope.launch { model.removeCorrection(index) } },
    )
    if (narrative?.hasNarrative == true || narrative?.isBeyondDepth == true) {
        item(key = "story-footer") { Footnote(stringOf(R.string.journal_reads_your_journal)) }
    }
}

// MARK: Photos

private fun LazyListScope.photosSection(journalDay: JournalDay, state: DayScreenState, onOpenInPhotos: () -> Unit) {
    item(key = "photos-header") { Heading(stringResource(R.string.journal_photos_total, journalDay.photoTotal.toString())) }
    item(key = "photos-strip") {
        LazyRow(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            items(journalDay.photos.take(STRIP_PHOTO_COUNT), key = { photo -> photo.photoId }) { photo ->
                JournalPhotoImage(photo.photoId, Modifier.size(104.dp).clip10().clickable { state.openedPhotoId = photo.photoId }.semantics { contentDescription = "Photo" })
            }
        }
    }
    item(key = "photos-open") { JournalOpenRow(true, onOpenInPhotos) { Text(stringOf(R.string.journal_open_in_photos)) } }
}

private fun Modifier.clip10(): Modifier = this.then(Modifier.clip(RoundedCornerShape(10.dp)))

// MARK: Through the day

private fun LazyListScope.momentsSection(journalDay: JournalDay, state: DayScreenState, text: JournalText, model: JournalDayModel, scope: kotlinx.coroutines.CoroutineScope, onOpen: (JournalRecordOpening) -> Unit) {
    val placedReferences = journalDay.placedReferences
    item(key = "moments-header") { Heading(stringOf(R.string.journal_through_the_day)) }
    items(shown(state, journalDay.moments, "moments", JournalRows.MOMENT_LIMIT), key = { moment -> "moment-${moment.id}" }) { moment ->
        JournalMomentRow(moment, text, canPin = moment.canPin(placedReferences), onPin = { scope.launch { model.pin(moment) } }, onOpen = onOpen)
    }
    showAllRow(state, journalDay.moments, "moments", JournalRows.MOMENT_LIMIT)
}

/** One thing the day held, with the time it happened. A moment whose record is not on the day as a pin or a choice can be pinned to it from its menu. */
@Composable
fun JournalMomentRow(moment: JournalMoment, text: JournalText, canPin: Boolean, onPin: () -> Unit, onOpen: (JournalRecordOpening) -> Unit) {
    var isMenuOpen by remember { mutableStateOf(false) }
    val opening = JournalRecordOpening.of(moment)
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.weight(1f)) {
            JournalOpenRow(opening.isOpenable, { onOpen(opening) }) {
                Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    Text(text.timeText(moment.at), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.width(76.dp))
                    Icon(journalRecordIcon(moment.recordKind), contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.width(24.dp))
                    Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                        Text(moment.title)
                        if (moment.detail.isNotEmpty()) Text(moment.detail, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
        }
        if (canPin) {
            Box {
                IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringOf(R.string.journal_more_actions)) }
                DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                    DropdownMenuItem(text = { Text(stringOf(R.string.journal_pin_to_this_day)) }, leadingIcon = { Icon(Icons.Outlined.PushPin, contentDescription = null) }, onClick = { isMenuOpen = false; onPin() })
                }
            }
        }
    }
}

// MARK: Places

private fun LazyListScope.placesSection(journalDay: JournalDay, state: DayScreenState, text: JournalText, onOpenPlaces: () -> Unit) {
    item(key = "places-header") { Heading(stringOf(R.string.journal_places)) }
    val mapCoordinates = journalDay.visits.filter { visit -> !(visit.latitude == 0.0 && visit.longitude == 0.0) }.map { visit -> MapCoordinate(visit.latitude, visit.longitude) }
    if (mapCoordinates.isNotEmpty()) {
        item(key = "places-map") {
            TeaNodeMap(
                content = MapContent(markers = journalDay.visits.filter { visit -> !(visit.latitude == 0.0 && visit.longitude == 0.0) }.map { visit -> MapMarker(MapCoordinate(visit.latitude, visit.longitude), visit.placeName) }),
                focus = MapFocus(mapCoordinates), isInteractive = false,
                modifier = Modifier.fillMaxWidth().height(180.dp), description = stringResource(R.string.journal_places),
            )
        }
    }
    item(key = "places-summary") {
        val placeCount = journalDay.facts?.placeCount ?: journalDay.visits.size
        val places = stringResource(R.string.journal_places_count, placeCount.toString())
        val walkedMeters = journalDay.facts?.walkedMeters
        JournalOpenRow(true, onOpenPlaces) {
            Text(if (walkedMeters != null && walkedMeters >= 100) places + ", " + stringResource(R.string.journal_walked, text.distanceText(walkedMeters)) else places)
        }
    }
    items(shown(state, journalDay.visits, "visits"), key = { visit -> "visit-${visit.visitId}" }) { visit -> VisitRow(visit, text) }
    showAllRow(state, journalDay.visits, "visits")
}

@Composable
private fun VisitRow(visit: JournalVisit, text: JournalText) {
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(visit.placeName, modifier = Modifier.weight(1f))
        Text(text.timeRangeText(visit.arrivedAt, visit.departedAt), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

// MARK: What was chosen

/**
 * The choices and pins of kinds the sections below do not show already: a chosen mail is among the mail that mattered,
 * and a chosen document is among the work, each removable there. A pin or a choice whose record is gone is listed here,
 * so that it can be removed.
 */
private fun chosenLinks(journalDay: JournalDay): List<JournalLink> {
    val choices = journalDay.choices.filter { link ->
        val record = link.record
        if (link.isHidden || record == null) false else record.isUnavailable || (record.recordKind != "mail" && record.recordKind != "agent_document")
    }
    return choices + journalDay.pins.filter { link -> link.record != null }
}

/** The agent's choice of a record that a section below shows, which the person can remove there; null when the agent did not choose it. */
private fun choiceOf(journalDay: JournalDay, recordKind: String, recordId: String): JournalLink? =
    journalDay.choices.firstOrNull { link -> !link.isHidden && link.record?.recordKind == recordKind && link.record?.recordId == recordId }

/** A removable row: the three dots at its end offer Remove, which takes a pin off the day or hides a choice of the agent. */
@Composable
private fun RemoveMenu(link: JournalLink?, onRemove: (JournalLink) -> Unit) {
    if (link == null) return
    var isMenuOpen by remember { mutableStateOf(false) }
    Box {
        IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringOf(R.string.journal_more_actions)) }
        DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
            DropdownMenuItem(
                text = { Text(stringOf(R.string.journal_remove), color = MaterialTheme.colorScheme.error) }, leadingIcon = { Icon(Icons.Outlined.Delete, contentDescription = null) },
                onClick = { isMenuOpen = false; onRemove(link) },
            )
        }
    }
}

private fun LazyListScope.chosenSection(journalDay: JournalDay, state: DayScreenState, model: JournalDayModel, scope: kotlinx.coroutines.CoroutineScope, onOpen: (JournalRecordOpening) -> Unit) {
    val links = chosenLinks(journalDay)
    if (links.isEmpty()) return
    item(key = "chosen-header") { Heading(stringOf(R.string.journal_chosen_for_this_day)) }
    items(shown(state, links, "chosen"), key = { link -> "chosen-${link.linkId}" }) { link ->
        val record = link.record ?: return@items
        JournalRecordRow(
            recordKind = record.recordKind, title = if (record.isUnavailable) stringOf(R.string.journal_no_longer_available) else record.title, detail = link.linkReason,
            opening = if (record.isUnavailable) JournalRecordOpening.Nowhere else JournalRecordOpening.of(record), onOpen = onOpen,
            trailing = { RemoveMenu(link) { each -> scope.launch { model.remove(each) } } },
        )
    }
    showAllRow(state, links, "chosen")
}

// MARK: Work and mail

private fun LazyListScope.workSection(
    journalDay: JournalDay,
    chosen: List<com.teanode.kit.journal.JournalDocument>,
    state: DayScreenState,
    text: JournalText,
    model: JournalDayModel,
    scope: kotlinx.coroutines.CoroutineScope,
) {
    if (journalDay.workProjects.isEmpty() && journalDay.codingSessions.isEmpty() && chosen.isEmpty()) return
    item(key = "work-header") { Heading(stringOf(R.string.journal_what_you_worked_on)) }
    items(journalDay.workProjects, key = { project -> "project-${project.id}" }) { project -> JournalWorkProjectRow(project) }
    if (journalDay.codingSessions.isNotEmpty() || chosen.isNotEmpty()) {
        foldedPart(state, "work-details", { Text(stringOf(R.string.journal_details)) }) {
            items(shown(state, journalDay.codingSessions, "sessions"), key = { session -> "session-${session.documentId}" }) { session ->
                JournalRecordRow("agent_document", session.title, detail = codingDetail(session, text), onOpen = {})
            }
            showAllRow(state, journalDay.codingSessions, "sessions")
            items(shown(state, chosen, "documents"), key = { document -> "chosen-doc-${document.documentId}" }) { document ->
                val link = choiceOf(journalDay, "agent_document", document.documentId)
                JournalRecordRow(
                    "agent_document", document.title, detail = document.linkReason.ifEmpty { document.sourceName }, onOpen = {},
                    trailing = { RemoveMenu(link) { each -> scope.launch { model.remove(each) } } },
                )
            }
            showAllRow(state, chosen, "documents")
        }
    }
    val workText = text.workText(journalDay.facts?.codingMinutes ?: 0, journalDay.workProjects.size)
    if (workText.isNotEmpty()) item(key = "work-footer") { Footnote(workText) }
}

private fun codingDetail(session: JournalCodingSession, text: JournalText): String {
    val parts = arrayListOf(session.assistant)
    val started = ServerTime.instant(session.startedAt)
    val ended = ServerTime.instant(session.endedAt)
    if (started != null && ended != null) {
        val minutes = ((ended.epochSecond - started.epochSecond) / 60).toInt()
        if (minutes > 0) parts.add(text.durationText(minutes))
    }
    val stretchCount = session.stretchCount ?: 1
    if (stretchCount > 1) parts.add(text.stretchesOfWork(stretchCount))
    return parts.joinToString(", ")
}

/** The work chat and documents the agent left out as routine, counted, and folded away until asked for. */
private fun LazyListScope.workChatSection(journalDay: JournalDay, chosenIds: Set<String>, state: DayScreenState) {
    val others = journalDay.documents.filter { document -> document.documentId !in chosenIds }
    val leftOutCount = maxOf(journalDay.documentTotal - chosenIds.size, others.size)
    if (leftOutCount <= 0) return
    if (others.isEmpty()) {
        item(key = "work-chat-left-out") { Text(stringResource(R.string.journal_n_left_out_as_routine, leftOutCount.toString()), modifier = Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
    } else {
        foldedPart(state, "work-chat", { Row { Text(stringOf(R.string.journal_work_chat_and_documents), Modifier.weight(1f)); Text(leftOutCount.toString(), color = MaterialTheme.colorScheme.onSurfaceVariant) } }) {
            items(shown(state, others, "work-chat"), key = { document -> "other-doc-${document.documentId}" }) { document ->
                JournalRecordRow("agent_document", document.title, detail = document.sourceName, onOpen = {})
            }
            showAllRow(state, others, "work-chat")
        }
    }
}

private fun LazyListScope.mailSection(journalDay: JournalDay, state: DayScreenState, model: JournalDayModel, scope: kotlinx.coroutines.CoroutineScope, onOpen: (JournalRecordOpening) -> Unit) {
    val mattered = journalDay.mail.filter { message -> message.isChosen || message.isImportant }
    if (mattered.isEmpty()) return
    item(key = "mail-header") { Heading(stringOf(R.string.journal_mail_that_mattered)) }
    items(shown(state, mattered, "mail"), key = { message -> "mail-${message.itemId}" }) { message ->
        val link = if (message.isChosen) choiceOf(journalDay, "mail", message.itemId) else null
        JournalRecordRow("mail", message.subject, detail = mailDetail(message), opening = JournalRecordOpening.MailItem(message.itemId), onOpen = onOpen, trailing = { RemoveMenu(link) { each -> scope.launch { model.remove(each) } } })
    }
    showAllRow(state, mattered, "mail")
}

private fun mailDetail(message: JournalMail): String {
    var detail = if (message.linkReason.isEmpty()) message.fromName else message.fromName + ": " + message.linkReason
    val repeatCount = message.repeatCount ?: 1
    if (repeatCount > 1) detail += " ($repeatCount)"
    return detail
}

// MARK: Who and what the day talked with

private fun LazyListScope.talkedSection(journalDay: JournalDay, state: DayScreenState, onOpenConversation: (String) -> Unit) {
    if (journalDay.conversations.isEmpty() && journalDay.chats.isEmpty()) return
    item(key = "talked-header") { Heading(stringOf(R.string.journal_conversations)) }
    items(shown(state, journalDay.conversations, "conversations"), key = { conversation -> "conversation-${conversation.conversationId}" }) { conversation ->
        JournalOpenRow(true, { onOpenConversation(conversation.conversationId) }) {
            ConversationRow(if (conversation.isMainChat) stringOf(R.string.journal_main_chat) else conversation.title, conversation.messageCount)
        }
    }
    showAllRow(state, journalDay.conversations, "conversations")
    items(shown(state, journalDay.chats, "chats"), key = { chat -> "chat-${chat.chatId}" }) { chat ->
        JournalOpenRow(false, {}) { ConversationRow(chat.chatName, chat.messageCount) }
    }
    showAllRow(state, journalDay.chats, "chats")
}

@Composable
private fun ConversationRow(title: String, messageCount: Int) {
    Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        Icon(journalRecordIcon("chat_message"), contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.width(24.dp))
        Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(title)
            Text(pluralStringResource(R.plurals.journal_n_messages, messageCount, messageCount), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

private fun LazyListScope.recordingsSection(journalDay: JournalDay, state: DayScreenState, text: JournalText, onOpen: (JournalRecordOpening) -> Unit) {
    item(key = "recordings-header") { Heading(stringOf(R.string.journal_recordings)) }
    items(shown(state, journalDay.recordings, "recordings"), key = { recording -> "recording-${recording.recordingId}" }) { recording ->
        val duration = text.durationText(recording.durationMS)
        JournalRecordRow("recording", recording.title, detail = if (recording.summary.isEmpty()) duration else duration + ": " + recording.summary, opening = JournalRecordOpening.Path("/recordings/${recording.recordingId}"), onOpen = onOpen)
    }
    showAllRow(state, journalDay.recordings, "recordings")
}

// MARK: Health

private fun hasHealth(health: JournalHealth): Boolean = health.stepCount > 0 || health.sleepMinutes > 0 || health.workouts.isNotEmpty() || health.restingHeartRate > 0

private fun LazyListScope.healthSection(health: JournalHealth, text: JournalText) {
    item(key = "health-header") { Heading(stringOf(R.string.journal_health)) }
    if (health.stepCount > 0) item(key = "health-steps") { LabeledLine(stringOf(R.string.journal_steps), text.countText(health.stepCount)) }
    if (health.sleepMinutes > 0) {
        item(key = "health-sleep") {
            Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Row { Text(stringOf(R.string.journal_sleep), Modifier.weight(1f)); Text(text.durationText(health.sleepMinutes)) }
                if (health.sleepStages.isNotEmpty()) JournalSleepBar(health.sleepStages)
            }
        }
    }
    if (health.restingHeartRate > 0) item(key = "health-heart") { LabeledLine(stringOf(R.string.journal_resting_heart_rate), stringResource(R.string.journal_bpm, Math.round(health.restingHeartRate).toString())) }
    items(health.workouts, key = { workout -> "workout-${workout.sampleId}" }) { workout ->
        val minutes = text.durationText(workout.minutes)
        LabeledLine(workout.activityType, if (workout.meters > 0) minutes + ", " + text.distanceText(workout.meters) else minutes)
    }
}

@Composable
internal fun LabeledLine(label: String, value: String) {
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(label, Modifier.weight(1f))
        Text(value, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

// MARK: Money

private fun LazyListScope.moneySection(journalDay: JournalDay, state: DayScreenState, text: JournalText, onOpenFinance: () -> Unit) {
    item(key = "money-header") { Heading(stringOf(R.string.journal_money)) }
    items(shown(state, journalDay.transactions, "money"), key = { transaction -> "transaction-${transaction.transactionId}" }) { transaction ->
        val amount = text.moneyText(Math.abs(transaction.amountMinorUnits), transaction.currency)
        JournalOpenRow(true, onOpenFinance) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Text(transaction.description)
                    text.timeText(transaction.transactedAt).takeIf { time -> time.isNotEmpty() }?.let { time -> Text(time, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                }
                // Money spent is its amount, money received has a plus.
                Text(if (transaction.amountMinorUnits > 0) "+$amount" else amount, modifier = Modifier.padding(start = 8.dp))
            }
        }
    }
    showAllRow(state, journalDay.transactions, "money")
}

// MARK: Calendar

private fun LazyListScope.calendarSection(journalDay: JournalDay, state: DayScreenState, text: JournalText, onOpen: (JournalRecordOpening) -> Unit) {
    item(key = "calendar-header") { Heading(stringOf(R.string.journal_calendar)) }
    items(shown(state, journalDay.events, "events"), key = { event -> "event-${event.reference}" }) { event ->
        val whenText = if (event.isAllDay) stringOf(R.string.journal_all_day) else text.timeRangeText(event.startsAt, event.endsAt)
        JournalRecordRow("calendar_event", event.title, detail = if (event.location.isEmpty()) whenText else whenText + ", " + event.location, opening = JournalRecordOpening.Part("calendar"), onOpen = onOpen)
    }
    showAllRow(state, journalDay.events, "events")
    items(shown(state, journalDay.reminders, "reminders"), key = { reminder -> "reminder-${reminder.reminderId}" }) { reminder ->
        JournalRecordRow("reminder", reminder.title, detail = if (reminder.doneAt == null) "" else stringOf(R.string.journal_done), opening = JournalRecordOpening.Part("calendar"), onOpen = onOpen)
    }
    showAllRow(state, journalDay.reminders, "reminders")
}

// MARK: Shopping

private fun LazyListScope.shoppingSection(journalDay: JournalDay, state: DayScreenState, text: JournalText, onOpen: (JournalRecordOpening) -> Unit) {
    item(key = "shopping-header") { Heading(stringOf(R.string.journal_shopping)) }
    items(shown(state, journalDay.shoppingTrips, "shopping"), key = { trip -> "trip-${trip.tripId}" }) { trip ->
        JournalRecordRow("shopping_trip", text.shoppingTitle(trip.storeName), detail = text.shoppingDetail(trip), opening = trip.opening, onOpen = onOpen)
    }
    showAllRow(state, journalDay.shoppingTrips, "shopping")
}

// MARK: Knowledge

/** The pages of the agent's memory with a fact from this day: one line saying how many, and the pages behind it. */
private fun LazyListScope.knowledgeSection(journalDay: JournalDay, state: DayScreenState, onOpen: (JournalRecordOpening) -> Unit) {
    if (journalDay.memoryPages.isEmpty()) return
    item(key = "knowledge-header") { Heading(stringOf(R.string.journal_knowledge)) }
    foldedPart(state, "knowledge", { Text(stringResource(R.string.journal_teanode_wrote_memory_pages, journalDay.memoryPages.size.toString())) }) {
        items(shown(state, journalDay.memoryPages, "knowledge"), key = { page -> "memory-${page.nodeId}" }) { page ->
            JournalRecordRow("agent_document", page.title, detail = page.path, opening = JournalRecordOpening.Path("/knowledge/${page.path}"), onOpen = onOpen)
        }
        showAllRow(state, journalDay.memoryPages, "knowledge")
    }
}

// MARK: The photo viewer

/** JournalPhotoViewer opens a photo of the day over the day, with the day's other photos a swipe away. */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun JournalPhotoViewer(photoIds: List<String>, openedPhotoId: String, onClose: () -> Unit) {
    val pagerState = rememberPagerState(initialPage = photoIds.indexOf(openedPhotoId).coerceAtLeast(0)) { photoIds.size }
    Dialog(onDismissRequest = onClose, properties = DialogProperties(usePlatformDefaultWidth = false)) {
        Box(Modifier.fillMaxSize().background(Color.Black)) {
            HorizontalPager(pagerState, Modifier.fillMaxSize()) { page ->
                val picture = rememberServerPicture("/api/v1/photos/${photoIds[page]}/previews/large") ?: rememberServerPicture(com.teanode.kit.journal.JournalPhoto.smallPreviewPath(photoIds[page]))
                if (picture != null) {
                    androidx.compose.foundation.Image(picture, contentDescription = null, contentScale = ContentScale.Fit, modifier = Modifier.fillMaxSize())
                }
            }
            IconButton(onClick = onClose, modifier = Modifier.align(Alignment.TopEnd).padding(8.dp)) {
                Icon(Icons.Outlined.Close, contentDescription = stringOf(R.string.journal_close), tint = Color.White)
            }
        }
    }
}

@Composable
internal fun weatherIcon(kind: com.teanode.kit.journal.JournalWeatherKind?): androidx.compose.ui.graphics.vector.ImageVector = when (kind) {
    com.teanode.kit.journal.JournalWeatherKind.Clear -> Icons.Outlined.WbSunny
    com.teanode.kit.journal.JournalWeatherKind.PartlyCloudy -> Icons.Outlined.WbCloudy
    com.teanode.kit.journal.JournalWeatherKind.Cloudy -> Icons.Outlined.Cloud
    com.teanode.kit.journal.JournalWeatherKind.Fog -> Icons.Outlined.Cloud
    com.teanode.kit.journal.JournalWeatherKind.Drizzle, com.teanode.kit.journal.JournalWeatherKind.Rain, com.teanode.kit.journal.JournalWeatherKind.Showers -> Icons.Outlined.Umbrella
    com.teanode.kit.journal.JournalWeatherKind.Snow -> Icons.Outlined.AcUnit
    com.teanode.kit.journal.JournalWeatherKind.Thunderstorm -> Icons.Outlined.Bolt
    null -> Icons.Outlined.WbCloudy
}

/** Where the places of a journal day open: the day in Location, with its map and visits. */
internal object JournalDayPlaces {
    fun route(day: String): String = java.time.LocalDate.parse(day).let(LocationRoutes::day)
}
