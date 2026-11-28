package com.teanode.app.search

import android.content.Context
import android.content.res.Configuration
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowForward
import androidx.compose.material.icons.outlined.Bolt
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.History
import androidx.compose.material.icons.outlined.LocationOn
import androidx.compose.material.icons.outlined.PhotoLibrary
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.WarningAmber
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.AppPlace
import com.teanode.app.shell.Areas
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.shell.Routes
import com.teanode.app.shell.icon
import com.teanode.app.shell.titleResource
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.relativeTimeText
import com.teanode.app.ui.components.rememberServerPicture
import com.teanode.kit.AppSection
import com.teanode.kit.access.OperatorAccess
import com.teanode.kit.access.operatorAccess
import com.teanode.kit.search.PlaceName
import com.teanode.kit.search.PlaceSearch
import com.teanode.kit.search.SearchArea
import com.teanode.kit.search.SearchAreaResults
import com.teanode.kit.search.SearchEverythingResults
import com.teanode.kit.search.SearchOpening
import com.teanode.kit.search.SearchRecent
import com.teanode.kit.search.SearchRecents
import com.teanode.kit.search.SearchResult
import com.teanode.kit.search.SearchWords
import com.teanode.kit.search.searchEverything
import java.util.Locale
import kotlinx.coroutines.delay

/** The words of a string resource in English, which a place is also found by whatever language the app is shown in. */
private fun englishString(context: Context, resource: Int): String {
    val configuration = Configuration(context.resources.configuration).apply { setLocale(Locale.ENGLISH) }
    return context.createConfigurationContext(configuration).getString(resource)
}

private class OfferedPlace(val place: AppPlace, val title: String, val englishTitle: String, val parentTitles: List<String>, val parentEnglishTitles: List<String>) {
    val shownTitle: String get() = (parentTitles + title).joinToString(" › ")
    val placeName: PlaceName get() = PlaceName(place.id, listOf(title, englishTitle), parentTitles.indices.map { index -> listOf(parentTitles[index], parentEnglishTitles[index]) })
}

/** Every place of the app the search can jump to: the parts that are offered, then the places inside each, as the parts' areas list them. */
private fun offeredPlaces(context: Context, offeredSections: List<AppSection>, accountPlaces: List<AppPlace>): List<OfferedPlace> {
    fun named(place: AppPlace) = OfferedPlace(
        place, place.titleText ?: context.getString(place.titleResource), place.englishTitleText ?: englishString(context, place.titleResource),
        place.parentTitleResources.map { resource -> context.getString(resource) }, place.parentTitleResources.map { resource -> englishString(context, resource) },
    )
    val sectionPlaces = offeredSections.map { section ->
        named(AppPlace("section-${section.routeName}", section.titleResource, Routes.home(section), section.icon))
    }
    val insidePlaces = Areas.all.filter { area -> area.section == null || area.section in offeredSections }.flatMap { area -> area.places() }.map { place -> named(place) }
    return sectionPlaces + insidePlaces + accountPlaces.map { place -> named(place) }
}

@Composable
private fun areaTitle(area: SearchArea): String = stringResource(
    when (area) {
        SearchArea.Chats -> R.string.section_agent
        SearchArea.Mail -> R.string.section_mail
        SearchArea.Contacts -> R.string.section_contacts
        SearchArea.Photos -> R.string.section_photos
        SearchArea.Memory -> R.string.section_knowledge
        SearchArea.Documents -> R.string.search_area_documents
        SearchArea.Recordings -> R.string.section_recordings
        SearchArea.Places -> R.string.search_area_places
        SearchArea.Finance -> R.string.section_finance
    },
)

private fun areaIcon(area: SearchArea): ImageVector = when (area) {
    SearchArea.Chats -> AppSection.Agent.icon
    SearchArea.Mail -> AppSection.Mail.icon
    SearchArea.Contacts -> AppSection.Contacts.icon
    SearchArea.Photos -> AppSection.Photos.icon
    SearchArea.Memory -> AppSection.Knowledge.icon
    SearchArea.Documents -> Icons.Outlined.Description
    SearchArea.Recordings -> AppSection.Recordings.icon
    SearchArea.Places -> Icons.Outlined.LocationOn
    SearchArea.Finance -> AppSection.Finance.icon
}

/** The search screen: a field, and under it the places of the app the typed words name, then what they find in every part, a few from each, grouped by part. */
@Composable
fun SearchSection() {
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    val navigator = LocalAppNavigator.current
    val context = LocalContext.current
    var typedText by remember { mutableStateOf("") }
    var found by remember { mutableStateOf<SearchEverythingResults?>(null) }
    var failure by remember { mutableStateOf<Throwable?>(null) }
    var retryCount by remember { mutableStateOf(0) }
    var access by remember { mutableStateOf<OperatorAccess?>(null) }
    val facts by session.facts.collectAsState()
    val offeredSections = remember(facts) { AppSection.entries.filter { section -> facts.isAvailable(section) } + AppSection.Settings }
    LaunchedEffect(connection) { access = runCatching { connection.operatorAccess() }.getOrNull() }
    val words = SearchWords.normalized(typedText)
    LaunchedEffect(words, retryCount) {
        if (words == null) {
            found = null
            failure = null
            return@LaunchedEffect
        }
        // A moment after the last key, as the other searches do.
        delay(300)
        try {
            found = connection.searchEverything(words)
            failure = null
        } catch (cancelled: kotlinx.coroutines.CancellationException) {
            throw cancelled
        } catch (error: Throwable) {
            found = null
            failure = error
        }
    }
    var accountPlaces by remember { mutableStateOf<List<AppPlace>>(emptyList()) }
    LaunchedEffect(connection, offeredSections) {
        accountPlaces = Areas.all.filter { area -> area.section == null || area.section in offeredSections }
            .flatMap { area -> runCatching { area.dynamicPlaces(context, connection) }.getOrDefault(emptyList()) }
    }
    val offered = remember(offeredSections, access, accountPlaces) { offeredPlaces(context, offeredSections, accountPlaces) }
    // What was opened from here lately, kept on the phone for each account; demo mode shows invented ones.
    val accountKey = session.accounts.collectAsState().value.active?.accountKey
    val preferences = remember { context.getSharedPreferences("search", Context.MODE_PRIVATE) }
    var recents by remember(accountKey) {
        mutableStateOf(
            when {
                session.isDemo -> searchDemoRecents
                accountKey == null -> emptyList()
                else -> SearchRecents.decode(preferences.getString(SearchRecents.preferenceKey(accountKey), null))
            },
        )
    }
    val keepRecent: (SearchRecent) -> Unit = { recent ->
        recents = SearchRecents.adding(recent, recents)
        if (!session.isDemo && accountKey != null) preferences.edit().putString(SearchRecents.preferenceKey(accountKey), SearchRecents.encode(recents)).apply()
    }
    val clearRecents: () -> Unit = {
        recents = emptyList()
        if (!session.isDemo && accountKey != null) preferences.edit().remove(SearchRecents.preferenceKey(accountKey)).apply()
    }
    val offeredById = remember(offered) { offered.associateBy { entry -> entry.place.id } }
    val shortcuts = remember(offeredById) { shortcutPlaceIds.mapNotNull { id -> offeredById[id] } }
    // A place the account can no longer open is left out.
    val recentEntries = remember(recents, offeredById) { recents.filter { recent -> recent.placeId == null || offeredById.containsKey(recent.placeId) } }
    val places = remember(offered, typedText) {
        val byId = offered.associateBy { entry -> entry.place.id }
        PlaceSearch.matchingPlaceIds(offered.map { entry -> entry.placeName }, typedText).mapNotNull { id -> byId[id] }
    }
    val isPhotosOffered = facts.isAvailable(AppSection.Photos)
    val openPlace: (OfferedPlace) -> Unit = { entry ->
        keepRecent(SearchRecent.place(entry.place.id))
        // The part opens first, so back from the page goes to the part.
        val section = Routes.sectionOf(entry.place.route)
        when {
            section == null -> navigator.navigate(entry.place.route)
            Routes.isHome(entry.place.route) -> navigator.openSection(section)
            else -> navigator.openInSection(section, entry.place.route)
        }
    }
    val open: (SearchDestination) -> Unit = { destination ->
        when (destination) {
            is SearchDestination.Item -> navigator.openInSection(destination.section, destination.route)
            is SearchDestination.PartWithWords -> {
                PartSearchRequest.ask(destination.search)
                navigator.openSection(destination.section)
            }
            SearchDestination.Nowhere -> Unit
        }
    }
    TeaNodeScreen(title = stringResource(R.string.common_search)) { contentPadding ->
        LazyColumn(Modifier.padding(contentPadding), contentPadding = PaddingValues(bottom = 24.dp)) {
            item {
                OutlinedTextField(
                    value = typedText, onValueChange = { typedText = it }, singleLine = true,
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
                    placeholder = { Text(stringResource(R.string.search_prompt)) },
                    leadingIcon = { Icon(Icons.Outlined.Search, null) },
                    trailingIcon = if (typedText.isNotEmpty()) ({ IconButton(onClick = { typedText = "" }) { Icon(Icons.Outlined.Close, stringResource(R.string.search_clear)) } }) else null,
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search), keyboardActions = KeyboardActions(onSearch = { retryCount += 1 }),
                )
            }
            if (words == null) {
                if (recentEntries.isEmpty() && shortcuts.isEmpty()) {
                    item { Box(Modifier.fillMaxWidth()) { EmptyState(Icons.Outlined.Search, stringResource(R.string.search_prompt), description = stringResource(R.string.search_prompt_detail)) } }
                    return@LazyColumn
                }
                if (recentEntries.isNotEmpty()) {
                    item {
                        SectionTitle(stringResource(R.string.search_recent), Icons.Outlined.History) {
                            TextButton(onClick = clearRecents) { Text(stringResource(R.string.search_clear)) }
                        }
                    }
                    items(recentEntries.size, key = { index -> recentEntries[index].identity }) { index ->
                        val recent = recentEntries[index]
                        val place = recent.placeId?.let { id -> offeredById[id] }
                        val result = recent.result
                        when {
                            place != null -> TeaNodeRow(headline = place.shownTitle, leadingIcon = place.place.icon, onClick = { openPlace(place) })
                            result != null -> {
                                val destination = SearchDestinations.of(result, recent.words)
                                TeaNodeRow(
                                    headline = if (result.isMain) stringResource(R.string.search_main_chat) else result.titleText.ifEmpty { result.detailText },
                                    supporting = result.detailText.takeIf { detail -> detail.isNotEmpty() && (result.isMain || result.titleText.isNotEmpty()) },
                                    leadingIcon = result.area?.let { area -> areaIcon(area) } ?: Icons.Outlined.Search,
                                    onClick = if (destination == SearchDestination.Nowhere) null else ({
                                        keepRecent(SearchRecent.found(result, recent.words))
                                        open(destination)
                                    }),
                                )
                            }
                        }
                    }
                }
                if (shortcuts.isNotEmpty()) {
                    item { SectionTitle(stringResource(R.string.search_shortcuts), Icons.Outlined.Bolt) }
                    items(shortcuts.size) { index ->
                        val entry = shortcuts[index]
                        TeaNodeRow(headline = entry.shownTitle, leadingIcon = entry.place.icon, onClick = { openPlace(entry) })
                    }
                }
                return@LazyColumn
            }
            if (places.isNotEmpty()) {
                item { SectionTitle(stringResource(R.string.search_go_to), Icons.AutoMirrored.Outlined.ArrowForward) }
                items(places.size) { index ->
                    val entry = places[index]
                    TeaNodeRow(headline = entry.shownTitle, leadingIcon = entry.place.icon, onClick = { openPlace(entry) })
                }
            }
            val answer = found
            val error = failure
            when {
                answer != null -> {
                    if (answer.isEmpty && answer.failedAreas.isEmpty() && places.isEmpty()) {
                        item {
                            EmptyState(
                                Icons.Outlined.Search, stringResource(R.string.search_nothing),
                                description = if (isPhotosOffered) stringResource(R.string.search_photos_explanation) else stringResource(R.string.search_nothing_detail, answer.words),
                                actionLabel = if (isPhotosOffered) stringResource(R.string.search_photos) else null, onAction = { navigator.openSection(AppSection.Photos) },
                            )
                        }
                    } else {
                        answer.areas.forEach { areaResults ->
                            areaResults.area?.let { area -> searchAreaSection(area, areaResults, words, open, onOpened = { result -> keepRecent(SearchRecent.found(result, words)) }) }
                        }
                        if (isPhotosOffered && !answer.areas.any { areaResults -> areaResults.area == SearchArea.Photos && areaResults.hasMore && areaResults.results.isNotEmpty() }) {
                            item {
                                TeaNodeRow(headline = stringResource(R.string.search_photos), leadingIcon = Icons.Outlined.PhotoLibrary, onClick = { navigator.openSection(AppSection.Photos) }, supporting = stringResource(R.string.search_photos_explanation))
                            }
                        }
                        if (answer.failedAreas.isNotEmpty()) {
                            item {
                                val names = answer.failedAreas.joinToString(", ") { name -> SearchArea.fromServerName(name)?.let { area -> areaTitleText(context, area) } ?: name }
                                TeaNodeRow(headline = stringResource(R.string.search_could_not, names), leadingIcon = Icons.Outlined.WarningAmber)
                            }
                        }
                    }
                }
                error != null && places.isEmpty() -> item { ErrorState(session.describeLoadProblem(error), onRetry = { retryCount += 1 }, modifier = Modifier.padding(16.dp)) }
                error != null -> item { TeaNodeRow(headline = session.describeLoadProblem(error), leadingIcon = Icons.Outlined.WarningAmber) }
                places.isEmpty() -> item { Box(Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) { LoadingState(Modifier.size(48.dp)) } }
            }
        }
    }
}

private fun areaTitleText(context: Context, area: SearchArea): String = context.getString(
    when (area) {
        SearchArea.Chats -> R.string.section_agent
        SearchArea.Mail -> R.string.section_mail
        SearchArea.Contacts -> R.string.section_contacts
        SearchArea.Photos -> R.string.section_photos
        SearchArea.Memory -> R.string.section_knowledge
        SearchArea.Documents -> R.string.search_area_documents
        SearchArea.Recordings -> R.string.section_recordings
        SearchArea.Places -> R.string.search_area_places
        SearchArea.Finance -> R.string.section_finance
    },
)

/** The pages the empty Search screen offers as shortcuts, by the ids its places are named by; those the account does not have are left out. */
private val shortcutPlaceIds = listOf(
    "section-calendar", "calendar-view-reminders", "section-contacts", "section-photos",
    "finance-Transactions", "finance-Imports", "section-passwords", "section-settings",
)

/** A heading over a group of rows, with an action at its end when it has one (the recents' Clear). */
@Composable
private fun SectionTitle(text: String, icon: ImageVector, action: (@Composable () -> Unit)? = null) {
    androidx.compose.foundation.layout.Row(
        Modifier.fillMaxWidth().padding(start = 16.dp, end = if (action == null) 16.dp else 4.dp, top = 16.dp, bottom = 4.dp),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        androidx.compose.foundation.layout.Row(Modifier.weight(1f).semantics { heading() }, verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Icon(icon, null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(18.dp))
            Text(text, style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary)
        }
        action?.invoke()
    }
}

@OptIn(ExperimentalLayoutApi::class)
private fun androidx.compose.foundation.lazy.LazyListScope.searchAreaSection(
    area: SearchArea, areaResults: SearchAreaResults, words: String, open: (SearchDestination) -> Unit, onOpened: (SearchResult) -> Unit,
) {
    item { SectionTitle(areaTitle(area), areaIcon(area)) }
    if (area == SearchArea.Photos) {
        item {
            FlowRow(Modifier.padding(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(4.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                areaResults.results.filter { result -> result.targetId.isNotEmpty() }.forEach { result ->
                    SearchPhotoThumbnail(result.targetId, onClick = {
                        onOpened(result)
                        open(SearchDestinations.of(result, words))
                    })
                }
            }
        }
    } else {
        items(areaResults.results.size) { index ->
            val result = areaResults.results[index]
            val title = if (result.isMain) stringResource(R.string.search_main_chat) else result.titleText.ifEmpty { result.detailText }
            val detail = if ((result.isMain || result.titleText.isNotEmpty()) && result.detailText.isNotEmpty()) result.detailText else null
            val destination = SearchDestinations.of(result, words)
            TeaNodeRow(
                headline = title, supporting = detail, overline = result.happenedInstant?.let { instant -> relativeTimeText(instant.toEpochMilli()) },
                onClick = if (destination == SearchDestination.Nowhere) null else ({
                    onOpened(result)
                    open(destination)
                }),
            )
        }
    }
    if (areaResults.hasMore && area.isShownInApp && sectionOfSearchArea(area) != null) {
        item { TeaNodeRow(headline = stringResource(R.string.search_show_all, areaTitle(area)), leadingIcon = Icons.AutoMirrored.Outlined.ArrowForward, onClick = { open(SearchDestinations.showAll(area, words)) }) }
    }
}

/** The picture of a photo found, a square that fills its place. A photo with no preview yet keeps the square, empty. */
@Composable
private fun SearchPhotoThumbnail(photoId: String, onClick: () -> Unit) {
    val picture = rememberServerPicture("/api/v1/photos/$photoId/previews/small")
    Box(Modifier.size(76.dp).clip(RoundedCornerShape(8.dp)).background(MaterialTheme.colorScheme.surfaceVariant).clickable(role = Role.Button, onClick = onClick)) {
        if (picture != null) Image(picture, contentDescription = stringResource(R.string.search_photo), contentScale = ContentScale.Crop, modifier = Modifier.size(76.dp))
    }
}
