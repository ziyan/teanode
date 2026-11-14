package com.teanode.app.location

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.*
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Slider
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.kit.location.LocationDays
import com.teanode.kit.location.LocationPause
import com.teanode.kit.location.LocationPlace
import com.teanode.kit.location.LocationPoint
import com.teanode.kit.location.PlaceChange
import com.teanode.kit.location.PlaceLabel
import com.teanode.kit.location.PlaceVisit
import com.teanode.kit.location.TravelPeriod
import com.teanode.kit.location.TravelPeriodKind
import com.teanode.kit.location.TravelPeriods
import com.teanode.kit.location.Trip
import com.teanode.kit.location.travelTotals
import com.teanode.kit.location.locationVisits
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import java.time.LocalDate
import java.time.ZoneId

private fun List<LocationPoint>.coordinates() = map { point -> MapCoordinate(point.latitudeDegrees, point.longitudeDegrees) }

/** One day: the path walked on the map, the visits as markers, and the visits under it. */
@Composable
fun LocationDayScreen(model: LocationModel, day: LocalDate, onBack: () -> Unit, onOpenPlace: (LocationPlace) -> Unit) {
    val text = rememberLocationText()
    val session = LocalAppSession.current
    val scope = rememberCoroutineScope()
    val zone = remember { ZoneId.systemDefault() }
    var points by remember { mutableStateOf<List<LocationPoint>>(emptyList()) }
    var visits by remember { mutableStateOf<List<PlaceVisit>>(emptyList()) }
    var pauses by remember { mutableStateOf<List<LocationPause>>(emptyList()) }
    var trips by remember { mutableStateOf<List<Trip>>(emptyList()) }
    var isLoading by remember { mutableStateOf(true) }
    var loadFailed by remember { mutableStateOf<String?>(null) }
    var isConfirmingErase by remember { mutableStateOf(false) }
    var isErasing by remember { mutableStateOf(false) }
    var forgetting by remember { mutableStateOf<PlaceVisit?>(null) }
    val window = remember(day) { LocationDays.window(day, zone) }
    val path = remember(points, visits) { LocationDays.path(points, visits, window.first, window.second) }

    suspend fun loadTrips() {
        fetchTrips(model.connection, null, window.first, window.second)?.let { read -> trips = read }
    }
    suspend fun load() {
        isLoading = true
        try {
            val page = model.connection.locationVisits(from = window.first, until = window.second, limitCount = 200)
            val read = model.points(day)
            visits = page.placeVisits
            trips = fetchTrips(model.connection, null, window.first, window.second) ?: emptyList()
            pauses = model.pauses(window.first, window.second)
            points = read
            loadFailed = null
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            loadFailed = session.describeLoadProblem(error)
        } finally {
            isLoading = false
        }
    }
    LaunchedEffect(day) { load() }
    LocationNoticeEffect(model)

    TeaNodeScreen(title = text.dayTitle(day), onBack = onBack) { padding ->
        Column(Modifier.padding(padding).fillMaxSize()) {
            val content = MapContent(path = path.coordinates(), markers = visits.map { visit -> MapMarker(MapCoordinate(visit.latitudeDegrees, visit.longitudeDegrees), text.shownName(visit.place)) })
            val focus = MapFocus((points.coordinates() + visits.map { visit -> MapCoordinate(visit.latitudeDegrees, visit.longitudeDegrees) }).ifEmpty { listOf(MapCoordinate(0.0, 0.0)) })
            TeaNodeMap(content, focus, Modifier.fillMaxWidth().height(260.dp), description = stringResource(R.string.location_map_of_the_day))
            val failure = loadFailed
            if (failure != null) {
                ErrorState(failure, onRetry = { scope.launch { load() } })
            } else {
                LazyColumn(Modifier.weight(1f)) {
                    if (isLoading) item { Box(Modifier.fillMaxWidth().padding(24.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() } }
                    else if (visits.isEmpty() && pauses.isEmpty() && trips.isEmpty()) item { EmptyState(Icons.Outlined.AddLocationAlt, stringResource(R.string.location_no_visits_this_day), modifier = Modifier.height(200.dp)) }
                    items(LocationDays.entries(visits, pauses, trips), key = { entry -> entry.id }) { entry ->
                        LocationTimelineRow(
                            entry, onOpenPlace = onOpenPlace, correctLeg = { leg, mode -> scope.launch { if (model.correct(leg, mode)) loadTrips() } },
                            askToForgetVisit = { visit -> forgetting = visit },
                        )
                    }
                    item { OpenStreetMapCredit() }
                    item {
                        TextButton(onClick = { isConfirmingErase = true }, enabled = !isErasing, modifier = Modifier.padding(horizontal = 8.dp)) {
                            Icon(Icons.Outlined.Delete, contentDescription = null, tint = MaterialTheme.colorScheme.error)
                            Text(stringResource(R.string.location_erase_this_day), Modifier.padding(start = 8.dp), color = MaterialTheme.colorScheme.error)
                        }
                    }
                }
            }
        }
    }
    if (isConfirmingErase) {
        ConfirmationDialog(
            title = stringResource(R.string.location_erase_day_title, text.dayTitle(day)), text = stringResource(R.string.location_erase_day_explanation),
            confirmLabel = stringResource(R.string.location_erase_this_day), isDestructive = true, onDismiss = { isConfirmingErase = false },
            onConfirm = {
                isConfirmingErase = false
                scope.launch {
                    isErasing = true
                    if (model.eraseHistory(window.first, window.second, text.dayTitle(day))) load()
                    isErasing = false
                }
            },
        )
    }
    forgetting?.let { visit ->
        ForgetVisitDialog(onDismiss = { forgetting = null }, onForget = {
            forgetting = null
            scope.launch {
                if (model.forget(visit)) {
                    visits = visits.filter { each -> each.id != visit.id }
                    loadTrips()
                }
            }
        })
    }
}

/** One place: its name, label, address, position and radius, and the visits to it; and forgetting it. */
@Composable
fun LocationPlaceScreen(model: LocationModel, placeId: String, onBack: () -> Unit, onChangeAddress: (LocationPlace) -> Unit) {
    val place = model.places.firstOrNull { each -> each.id == placeId } ?: model.visits.firstOrNull { visit -> visit.place.id == placeId }?.place
    if (place == null) {
        TeaNodeScreen(title = stringResource(R.string.location_a_place), onBack = onBack) { padding ->
            if (model.hasLoaded) EmptyState(Icons.Outlined.AddLocationAlt, stringResource(R.string.location_place_not_here), Modifier.padding(padding)) else LoadingState(Modifier.padding(padding))
        }
        LaunchedEffect(Unit) { if (!model.hasLoaded) model.load() }
        return
    }
    LocationPlaceContent(model, place, onBack, onChangeAddress)
}

@Composable
private fun LocationPlaceContent(model: LocationModel, initial: LocationPlace, onBack: () -> Unit, onChangeAddress: (LocationPlace) -> Unit) {
    val text = rememberLocationText()
    val recording = LocalLocationRecording.current
    val scope = rememberCoroutineScope()
    var place by remember(initial.id) { mutableStateOf(initial) }
    var nameText by remember(initial.id) { mutableStateOf(if (initial.isNamedByPerson) initial.placeName else "") }
    var radius by remember(initial.id) { mutableFloatStateOf(initial.radiusMeters.toFloat()) }
    var visits by remember(initial.id) { mutableStateOf<List<PlaceVisit>>(emptyList()) }
    var nextOffset by remember(initial.id) { mutableStateOf<Int?>(null) }
    var isLoadingVisits by remember { mutableStateOf(false) }
    var isConfirmingForget by remember { mutableStateOf(false) }
    var forgetting by remember { mutableStateOf<PlaceVisit?>(null) }
    var tapped by remember(initial.id) { mutableStateOf<MapCoordinate?>(null) }
    var isMoving by remember { mutableStateOf(false) }
    var isLabelMenuOpen by remember { mutableStateOf(false) }
    val sameAsLast = place.isNamedByPerson && place.placeName == nameText.trim()

    suspend fun loadVisits() {
        if (isLoadingVisits || (visits.isNotEmpty() && nextOffset == null)) return
        isLoadingVisits = true
        try {
            val page = model.visits(place.id, nextOffset ?: 0)
            visits = visits + page.placeVisits
            nextOffset = page.nextOffset
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            model.say(model.connectionProblem(error), true)
        } finally {
            isLoadingVisits = false
        }
    }
    LaunchedEffect(initial.id) { loadVisits() }
    LocationNoticeEffect(model)

    suspend fun saveName() {
        val trimmed = nameText.trim()
        if (trimmed.isEmpty()) {
            nameText = if (place.isNamedByPerson) place.placeName else ""
            return
        }
        if (trimmed == place.placeName && place.isNamedByPerson) return
        model.save(PlaceChange(placeId = place.id, placeName = trimmed))?.let { saved -> place = saved; nameText = saved.placeName }
    }
    suspend fun move(coordinate: MapCoordinate?) {
        coordinate ?: return
        isMoving = true
        model.save(PlaceChange(placeId = place.id, latitudeDegrees = coordinate.latitudeDegrees, longitudeDegrees = coordinate.longitudeDegrees))?.let { saved ->
            place = saved
            tapped = null
            model.say(model.moved(text.shownName(saved)), false)
        }
        isMoving = false
    }
    suspend fun moveToPhone() {
        isMoving = true
        val reading = model.currentReading()
        if (reading != null) {
            // A negative accuracy means the phone could not say how rough the reading is; the live reading is already no older than 60 s.
            if (reading.accuracyMeters < 0 || reading.accuracyMeters > LocationText.ROUGH_ACCURACY_METERS) {
                val roughness = if (reading.accuracyMeters >= 0) text.distance(Math.round(reading.accuracyMeters).toDouble()) else model.unknownDistance()
                model.say(model.phoneRough(roughness), true)
            } else {
                move(MapCoordinate(reading.latitudeDegrees, reading.longitudeDegrees))
            }
        }
        isMoving = false
    }
    fun saveRadius() = scope.launch {
        if (radius.toDouble() == place.radiusMeters) return@launch
        val saved = model.save(PlaceChange(placeId = place.id, radiusMeters = radius.toDouble()))
        if (saved != null) place = saved else radius = place.radiusMeters.toFloat()
    }

    TeaNodeScreen(title = text.shownName(place), onBack = { scope.launch { saveName() }; onBack() }) { padding ->
        LazyColumn(Modifier.padding(padding).fillMaxSize()) {
            item {
                OutlinedTextField(
                    value = nameText, onValueChange = { typed -> nameText = typed }, singleLine = true, label = { Text(stringResource(R.string.location_name)) },
                    placeholder = { Text(stringResource(R.string.location_place_name)) },
                    supportingText = if (!place.isNamedByPerson && place.placeName.isNotEmpty()) ({ Text(place.placeName) }) else null,
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
                )
                if (nameText.trim().isNotEmpty() && !sameAsLast) {
                    TextButton(onClick = { scope.launch { saveName() } }, modifier = Modifier.padding(horizontal = 8.dp)) { Text(stringResource(R.string.location_save_name)) }
                }
            }
            item {
                Box {
                    Row(Modifier.fillMaxWidth().clickable { isLabelMenuOpen = true }.padding(horizontal = 16.dp, vertical = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                        Text(stringResource(R.string.location_label), Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
                        Icon(placeLabelIcon(place.label), contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
                        Text(place.label?.let { label -> text.labelTitle(label) } ?: stringResource(R.string.location_none), Modifier.padding(start = 8.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    DropdownMenu(expanded = isLabelMenuOpen, onDismissRequest = { isLabelMenuOpen = false }) {
                        DropdownMenuItem(text = { Text(stringResource(R.string.location_none)) }, onClick = { isLabelMenuOpen = false; scope.launch { model.save(PlaceChange(placeId = place.id, placeLabel = ""))?.let { saved -> place = saved } } })
                        PlaceLabel.entries.forEach { label ->
                            DropdownMenuItem(
                                text = { Text(text.labelTitle(label)) }, leadingIcon = { Icon(placeLabelIcon(label), contentDescription = null) },
                                onClick = { isLabelMenuOpen = false; scope.launch { model.save(PlaceChange(placeId = place.id, placeLabel = label.serverWord))?.let { saved -> place = saved } } },
                            )
                        }
                    }
                }
                HorizontalDivider()
            }
            item {
                Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp)) {
                    Text(stringResource(R.string.location_address), Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
                    Text(place.addressText.ifEmpty { stringResource(R.string.location_no_address) }, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                TextButton(onClick = { onChangeAddress(place) }, modifier = Modifier.padding(horizontal = 8.dp)) {
                    Icon(Icons.Outlined.Search, contentDescription = null)
                    Text(stringResource(R.string.location_change_address), Modifier.padding(start = 8.dp))
                }
                if (place.isAddressTyped) {
                    TextButton(
                        onClick = { scope.launch { model.save(PlaceChange(placeId = place.id, addressText = ""))?.let { saved -> place = saved; model.say(model.mapWillNameAgain(), false) } } },
                        modifier = Modifier.padding(horizontal = 8.dp),
                    ) {
                        Icon(Icons.Outlined.Map, contentDescription = null)
                        Text(stringResource(R.string.location_use_the_maps_address), Modifier.padding(start = 8.dp))
                    }
                }
            }
            item {
                val center = tapped ?: MapCoordinate(place.latitudeDegrees, place.longitudeDegrees)
                TeaNodeMap(
                    MapContent(markers = listOf(MapMarker(center, text.shownName(place))), circle = MapCircle(center, radius.toDouble()), pickedCoordinate = tapped),
                    MapFocus(listOf(center), radiusMeters = radius.toDouble()), Modifier.fillMaxWidth().height(200.dp),
                    description = stringResource(R.string.location_map_of_the_place), onMapTap = { coordinate -> tapped = coordinate },
                )
                if (tapped != null) {
                    TextButton(onClick = { scope.launch { move(tapped) } }, enabled = !isMoving, modifier = Modifier.padding(horizontal = 8.dp)) {
                        Icon(Icons.Outlined.AddLocationAlt, contentDescription = null); Text(stringResource(R.string.location_move_the_place_here), Modifier.padding(start = 8.dp))
                    }
                    TextButton(onClick = { tapped = null }, enabled = !isMoving, modifier = Modifier.padding(horizontal = 8.dp)) {
                        Icon(Icons.Outlined.Undo, contentDescription = null); Text(stringResource(R.string.location_keep_it_where_it_was), Modifier.padding(start = 8.dp))
                    }
                }
                TextButton(onClick = { scope.launch { moveToPhone() } }, enabled = !isMoving, modifier = Modifier.padding(horizontal = 8.dp)) {
                    Icon(Icons.Outlined.MyLocation, contentDescription = null); Text(stringResource(R.string.location_use_where_i_am_now), Modifier.padding(start = 8.dp))
                }
                Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp)) {
                    Text(stringResource(R.string.location_radius), Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
                    Text(text.radius(radius.toDouble()), color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                Slider(value = radius, onValueChange = { moved -> radius = Math.round(moved / 25f) * 25f }, valueRange = 25f..2000f, onValueChangeFinished = { saveRadius() }, modifier = Modifier.padding(horizontal = 16.dp))
                Text(stringResource(R.string.location_radius_footer), Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            item { TeaNodeSectionHeader(stringResource(R.string.location_visits)) }
            items(visits, key = { visit -> visit.id }) { visit ->
                Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
                    Text(visit.anchorInstant?.let { instant -> text.dayTitle(instant.atZone(ZoneId.systemDefault()).toLocalDate()) }.orEmpty(), style = MaterialTheme.typography.bodyLarge)
                    Text(text.summary(visit), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    // A visit in progress is ended by a check-in first.
                    if (visit.departedAt != null) TextButton(onClick = { forgetting = visit }) { Text(stringResource(R.string.location_forget), color = MaterialTheme.colorScheme.error) }
                }
                if (visit.id == visits.lastOrNull()?.id) LaunchedEffect(visit.id) { loadVisits() }
            }
            if (isLoadingVisits) item { Box(Modifier.fillMaxWidth().padding(16.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() } }
            item { OpenStreetMapCredit() }
            item {
                TextButton(onClick = { isConfirmingForget = true }, modifier = Modifier.padding(horizontal = 8.dp, vertical = 8.dp)) {
                    Icon(Icons.Outlined.Delete, contentDescription = null, tint = MaterialTheme.colorScheme.error)
                    Text(stringResource(R.string.location_forget_this_place), Modifier.padding(start = 8.dp), color = MaterialTheme.colorScheme.error)
                }
            }
        }
    }
    if (isConfirmingForget) {
        ConfirmationDialog(
            title = stringResource(R.string.location_forget_question, text.shownName(place)), text = stringResource(R.string.location_forget_place_explanation),
            confirmLabel = stringResource(R.string.location_forget_this_place), isDestructive = true, onDismiss = { isConfirmingForget = false },
            onConfirm = { isConfirmingForget = false; scope.launch { if (model.forget(place)) onBack() } },
        )
    }
    forgetting?.let { visit ->
        ForgetVisitDialog(onDismiss = { forgetting = null }, onForget = { forgetting = null; scope.launch { if (model.forget(visit)) visits = visits.filter { each -> each.id != visit.id } } })
    }
    recording.hashCode()
}

/** Travel: the distance by each way over days, weeks, months and years. */
@Composable
fun LocationTravelScreen(connection: com.teanode.kit.Connection, onBack: () -> Unit, userId: String? = null) {
    val text = rememberLocationText()
    val session = LocalAppSession.current
    var kind by remember { mutableStateOf(TravelPeriodKind.Week) }
    var pageBack by remember { mutableStateOf(0) }
    var periods by remember { mutableStateOf<List<TravelPeriod>>(emptyList()) }
    var loadedKind by remember { mutableStateOf(TravelPeriodKind.Week) }
    var isLoading by remember { mutableStateOf(true) }
    var loadFailed by remember { mutableStateOf<String?>(null) }
    val scope = rememberCoroutineScope()
    val shown = if (loadedKind == kind) periods else emptyList()
    suspend fun load() {
        val requestedKind = kind
        val requestedPage = pageBack
        val range = requestedKind.pageRange(requestedPage)
        isLoading = true
        try {
            val totals = connection.travelTotals(userId, requestedKind, range.first, range.second)
            // Another page was chosen while this was read: that load is the one that finishes loading.
            if (requestedKind != kind || requestedPage != pageBack) return
            periods = TravelPeriods.group(totals)
            loadedKind = requestedKind
            loadFailed = null
            isLoading = false
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            if (requestedKind != kind || requestedPage != pageBack) return
            loadFailed = session.describeLoadProblem(error)
            isLoading = false
        }
    }
    LaunchedEffect(kind, pageBack) { load() }
    TeaNodeScreen(title = stringResource(R.string.location_travel), onBack = onBack) { padding ->
        val failure = loadFailed
        if (failure != null) {
            ErrorState(failure, onRetry = { scope.launch { load() } }, modifier = Modifier.padding(padding))
        } else {
            LazyColumn(Modifier.padding(padding).fillMaxSize()) {
                item {
                    SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth().padding(16.dp)) {
                        TravelPeriodKind.entries.forEachIndexed { index, each ->
                            SegmentedButton(
                                selected = each == kind, onClick = { kind = each; periods = emptyList(); pageBack = 0 },
                                shape = SegmentedButtonDefaults.itemShape(index, TravelPeriodKind.entries.size),
                            ) { Text(text.title(each), maxLines = 1) }
                        }
                    }
                }
                item {
                    Row(Modifier.padding(horizontal = 8.dp)) {
                        if (pageBack < PAGE_BACK_MOST) TextButton(onClick = { pageBack += 1 }) { Icon(Icons.AutoMirrored.Outlined.KeyboardArrowLeft, contentDescription = null); Text(stringResource(R.string.location_earlier)) }
                        if (pageBack > 0) TextButton(onClick = { pageBack -= 1 }) { Text(stringResource(R.string.location_later)); Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = null) }
                    }
                }
                if (isLoading && shown.isEmpty()) item { Box(Modifier.fillMaxWidth().padding(32.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() } }
                else if (shown.isEmpty()) item { EmptyState(Icons.Outlined.AddLocationAlt, stringResource(R.string.location_no_travel_here), description = stringResource(R.string.location_trips_show_up_here), modifier = Modifier.height(280.dp)) }
                items(shown, key = { period -> period.periodStartDay }) { period -> LocationTravelPeriodRow(period, loadedKind) }
            }
        }
    }
}

private const val PAGE_BACK_MOST = 20

@Composable
fun LocationTravelPeriodRow(period: TravelPeriod, kind: TravelPeriodKind) {
    val text = rememberLocationText()
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Row(Modifier.fillMaxWidth()) {
            Text(text.title(period, kind), Modifier.weight(1f), style = MaterialTheme.typography.titleMedium)
            Text(text.distance(period.distanceMeters), style = MaterialTheme.typography.titleMedium)
        }
        period.totals.forEach { total ->
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Icon(travelModeIcon(total.travelMode), contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.width(28.dp))
                Text(text.travelModeTitle(total.travelMode), Modifier.weight(1f).padding(start = 12.dp), style = MaterialTheme.typography.bodyMedium)
                Text(text.distance(total.distanceMeters), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}
