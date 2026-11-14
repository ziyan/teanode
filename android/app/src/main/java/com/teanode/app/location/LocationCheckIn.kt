package com.teanode.app.location

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.kit.location.CheckInTarget
import com.teanode.kit.location.FoundPlace
import kotlinx.coroutines.launch

/** Records that the person is at a place now: the nearby places and a search by name, or just where the phone is. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun CheckInSheet(model: LocationModel, onDismiss: () -> Unit) {
    val text = rememberLocationText()
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    var nearby by remember { mutableStateOf<List<FoundPlace>?>(null) }
    var reading by remember { mutableStateOf<LivePosition?>(null) }
    var loadFailed by remember { mutableStateOf<String?>(null) }
    var searchText by remember { mutableStateOf("") }
    var searched by remember { mutableStateOf<List<FoundPlace>?>(null) }
    var searchedWords by remember { mutableStateOf("") }
    var searchNextOffset by remember { mutableStateOf<Int?>(null) }
    var isSearching by remember { mutableStateOf(false) }
    var isCheckingIn by remember { mutableStateOf(false) }
    val trimmed = searchText.trim()
    val listed = if (trimmed.isEmpty()) nearby else searched
    val session = LocalAppSession.current

    suspend fun load() {
        loadFailed = null
        val current = model.currentReading()
        if (current == null) {
            loadFailed = text.phoneCouldNotTellText()
            return
        }
        reading = current
        try {
            nearby = model.nearby(current.latitudeDegrees, current.longitudeDegrees)
        } catch (error: Exception) {
            if (error is kotlinx.coroutines.CancellationException) throw error
            loadFailed = session.describeLoadProblem(error)
        }
    }
    LaunchedEffect(Unit) { load() }

    suspend fun search(isNextPage: Boolean) {
        val position = reading ?: return
        if (trimmed.isEmpty()) return
        isSearching = true
        try {
            val page = model.searchPlaces(trimmed, position.latitudeDegrees, position.longitudeDegrees, if (isNextPage) searchNextOffset ?: 0 else 0)
            // Other words were typed while this was read.
            if (trimmed != searchText.trim()) return
            searched = (if (isNextPage) searched.orEmpty() else emptyList()) + page.foundPlaces
            searchedWords = trimmed
            searchNextOffset = page.nextOffset
        } catch (error: Exception) {
            if (error is kotlinx.coroutines.CancellationException) throw error
            snackbar.showProblem(session.describeLoadProblem(error))
        } finally {
            isSearching = false
        }
    }

    fun checkIn(target: CheckInTarget, placeName: String?) {
        isCheckingIn = true
        scope.launch {
            val refusal = model.checkIn(target, placeName)
            isCheckingIn = false
            if (refusal == null) onDismiss() else snackbar.showProblem(refusal)
        }
    }

    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.fillMaxWidth().fillMaxHeight(0.85f)) {
            Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(stringResource(R.string.location_check_in), style = MaterialTheme.typography.titleLarge, modifier = Modifier.weight(1f))
                TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) }
            }
            OutlinedTextField(
                value = searchText, onValueChange = { typed ->
                    searchText = typed
                    // Words changed: what was found for the old ones is not the answer.
                    if (typed.trim() != searchedWords) { searched = null; searchNextOffset = null }
                },
                singleLine = true, leadingIcon = { Icon(Icons.Outlined.Search, contentDescription = null) }, placeholder = { Text(stringResource(R.string.location_find_a_place)) },
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search), keyboardActions = KeyboardActions(onSearch = { scope.launch { search(false) } }),
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp),
            )
            val failure = loadFailed
            when {
                failure != null -> ErrorState(failure, onRetry = { scope.launch { load() } })
                trimmed.isNotEmpty() && isSearching && searched == null -> Box(Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
                trimmed.isNotEmpty() && searched == null -> Text(stringResource(R.string.location_press_search), Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
                trimmed.isEmpty() && nearby == null -> Box(Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() }
                else -> LazyColumn(Modifier.fillMaxWidth()) {
                    if (reading != null) {
                        item {
                            Row(
                                Modifier.fillMaxWidth().clickable(enabled = !isCheckingIn) {
                                    val position = reading ?: return@clickable
                                    checkIn(CheckInTarget.Position(position.latitudeDegrees, position.longitudeDegrees, position.accuracyMeters), null)
                                }.padding(horizontal = 16.dp, vertical = 12.dp),
                                verticalAlignment = Alignment.CenterVertically,
                            ) {
                                Icon(Icons.Outlined.MyLocation, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.width(28.dp))
                                Column(Modifier.padding(start = 12.dp)) {
                                    Text(stringResource(R.string.location_check_in_here), style = MaterialTheme.typography.bodyLarge)
                                    precisionText(text, reading)?.let { precision -> Text(precision, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                                }
                            }
                        }
                    }
                    val places = listed.orEmpty()
                    if (places.isEmpty() && trimmed.isEmpty() && nearby != null) {
                        item { Text(stringResource(R.string.location_nothing_nearby), Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
                    } else if (places.isEmpty() && trimmed.isNotEmpty() && searchedWords == trimmed) {
                        item { Text(stringResource(R.string.location_no_results), Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
                    }
                    items(places, key = { place -> place.id }) { place ->
                        Row(
                            Modifier.fillMaxWidth().clickable(enabled = !isCheckingIn) {
                                checkIn(place.placeId?.let { id -> CheckInTarget.OwnPlace(id) } ?: CheckInTarget.Found(place), text.shownName(place))
                            }.padding(horizontal = 16.dp, vertical = 10.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            place.label?.let { label -> Icon(placeLabelIcon(label), contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.width(28.dp)) }
                            Column(Modifier.padding(start = if (place.label != null) 12.dp else 0.dp)) {
                                Text(text.shownName(place), style = MaterialTheme.typography.bodyLarge)
                                text.secondaryName(place)?.let { secondary -> Text(secondary, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant) }
                                Text(details(text, place), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                            }
                        }
                    }
                    if (trimmed.isNotEmpty() && searchNextOffset != null) {
                        item { TextButton(onClick = { scope.launch { search(true) } }, enabled = !isSearching, modifier = Modifier.padding(horizontal = 8.dp)) { Text(stringResource(R.string.location_more_places)) } }
                    }
                    item { OpenStreetMapCredit() }
                }
            }
        }
    }
}

private fun details(text: LocationText, place: FoundPlace): String {
    val category = text.category(place.osmCategory)
    val distance = text.distance(place.distanceMeters)
    return if (category.isEmpty()) distance else text.categoryAndDistance(category, distance)
}

private fun precisionText(text: LocationText, reading: LivePosition?): String? {
    val accuracy = reading?.accuracyMeters ?: return null
    if (accuracy < 0) return null
    val within = text.within(text.distance(Math.round(accuracy).toDouble()))
    return if (accuracy > LocationText.ROUGH_ACCURACY_METERS) text.withinButRough(within) else within
}

/** The place's address, found by typing: the matches near the place or where the person last was, and a way to keep the words as typed. */
@Composable
fun LocationAddressScreen(
    model: LocationModel,
    onBack: () -> Unit,
    placeId: String? = null,
    keepAsTyped: (suspend (String) -> String?)? = null,
    choose: suspend (FoundPlace) -> String?,
) {
    val text = rememberLocationText()
    val snackbar = LocalSnackbar.current
    val session = LocalAppSession.current
    val scope = rememberCoroutineScope()
    var searchText by remember { mutableStateOf("") }
    var matches by remember { mutableStateOf<List<FoundPlace>?>(null) }
    var searchedWords by remember { mutableStateOf("") }
    var isSearching by remember { mutableStateOf(false) }
    var isChoosing by remember { mutableStateOf(false) }
    val trimmed = searchText.trim()
    val isAnswered = matches != null && searchedWords == trimmed && trimmed.isNotEmpty()

    suspend fun search() {
        if (trimmed.isEmpty()) return
        val words = trimmed
        isSearching = true
        try {
            val found = model.searchAddresses(words, placeId)
            // Other words were typed while this was read.
            if (words != searchText.trim()) return
            matches = found
            searchedWords = words
        } catch (error: Exception) {
            if (error is kotlinx.coroutines.CancellationException) throw error
            snackbar.showProblem(session.describeLoadProblem(error))
        } finally {
            isSearching = false
        }
    }
    suspend fun finish(work: suspend () -> String?) {
        isChoosing = true
        val refusal = work()
        isChoosing = false
        if (refusal == null) onBack() else snackbar.showProblem(refusal)
    }
    com.teanode.app.ui.components.TeaNodeScreen(title = stringResource(R.string.location_find_address), onBack = onBack) { padding ->
        LazyColumn(Modifier.padding(padding).fillMaxWidth()) {
            item {
                OutlinedTextField(
                    value = searchText, onValueChange = { typed ->
                        searchText = typed
                        if (typed.trim() != searchedWords) matches = null
                    },
                    singleLine = true, leadingIcon = { Icon(Icons.Outlined.Search, contentDescription = null) }, placeholder = { Text(stringResource(R.string.location_type_an_address)) },
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search), keyboardActions = KeyboardActions(onSearch = { scope.launch { search() } }),
                    modifier = Modifier.fillMaxWidth().padding(16.dp),
                )
            }
            if (isSearching && matches == null) {
                item { Box(Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() } }
            } else if (trimmed.isEmpty() || matches == null) {
                item { Text(stringResource(R.string.location_press_search_address), Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
            }
            if (isAnswered && matches?.isEmpty() == true) {
                item { Text(stringResource(R.string.location_nothing_matches, searchedWords), Modifier.padding(horizontal = 16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
                if (keepAsTyped == null) item { Text(stringResource(R.string.location_not_found_on_the_map), Modifier.padding(16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
            }
            items(matches.orEmpty(), key = { match -> match.id }) { match ->
                Column(Modifier.fillMaxWidth().clickable(enabled = !isChoosing) { scope.launch { finish { choose(match) } } }.padding(horizontal = 16.dp, vertical = 10.dp)) {
                    Text(text.chosenAddressText(match), style = MaterialTheme.typography.bodyLarge)
                    if (match.distanceMeters > 0) Text(text.distance(match.distanceMeters), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
            if (matches?.isNotEmpty() == true) item { OpenStreetMapCredit() }
            if (isAnswered && keepAsTyped != null) {
                item {
                    Column(Modifier.fillMaxWidth().clickable(enabled = !isChoosing) { scope.launch { finish { keepAsTyped(searchedWords) } } }.padding(16.dp)) {
                        Text(stringResource(R.string.location_keep_as_typed), color = MaterialTheme.colorScheme.primary, style = MaterialTheme.typography.bodyLarge)
                        Text(searchedWords, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        Text(stringResource(R.string.location_keep_as_typed_footer), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
        }
    }
}
