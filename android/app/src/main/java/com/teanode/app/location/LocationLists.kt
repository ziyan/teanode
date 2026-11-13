package com.teanode.app.location

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.background
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.*
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.AvatarView
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.TeaNodeDivider
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberServerPicture
import com.teanode.kit.location.LocationPlace
import com.teanode.kit.location.LocationPause
import com.teanode.kit.location.LocationPauseChoice
import com.teanode.kit.location.LocationShare
import com.teanode.kit.location.LocationShareLevel
import com.teanode.kit.location.LocationSharingBanner
import com.teanode.kit.location.LocationSharingNotice
import com.teanode.kit.location.LocationTimelineEntry
import com.teanode.kit.location.PersonLocation
import com.teanode.kit.location.PlaceLabel
import com.teanode.kit.location.PlaceVisit
import com.teanode.kit.location.TravelMode
import com.teanode.kit.location.Trip
import com.teanode.kit.location.TripLeg
import com.teanode.kit.location.VisitPhoto
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.time.LocalDate

/** The icon that stands for a place: its label's, else a map marker. */
fun placeIcon(place: LocationPlace?): ImageVector = placeLabelIcon(place?.label)

fun placeLabelIcon(label: PlaceLabel?): ImageVector = when (label) {
    PlaceLabel.Home -> Icons.Outlined.Home
    PlaceLabel.Work -> Icons.Outlined.Work
    PlaceLabel.School -> Icons.Outlined.School
    PlaceLabel.Family -> Icons.Outlined.FamilyRestroom
    PlaceLabel.Gym -> Icons.Outlined.FitnessCenter
    null -> Icons.Outlined.Place
}

fun travelModeIcon(travelMode: String): ImageVector = when (TravelMode.fromServerWord(travelMode)) {
    TravelMode.Walk -> Icons.AutoMirrored.Outlined.DirectionsWalk
    TravelMode.Run -> Icons.Outlined.DirectionsRun
    TravelMode.Bike -> Icons.Outlined.DirectionsBike
    TravelMode.Car -> Icons.Outlined.DirectionsCar
    TravelMode.Bus -> Icons.Outlined.DirectionsBus
    TravelMode.Train -> Icons.Outlined.Train
    TravelMode.Boat -> Icons.Outlined.DirectionsBoat
    TravelMode.Plane -> Icons.Outlined.Flight
    null -> Icons.Outlined.AltRoute
}

/** The footer of every list that shows names from OpenStreetMap. */
@Composable
fun OpenStreetMapCredit(modifier: Modifier = Modifier) {
    Text(
        stringResource(R.string.location_open_street_map_credit), modifier = modifier.fillMaxWidth().padding(16.dp), style = MaterialTheme.typography.bodySmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
    )
}

// Visits

@Composable
fun LocationVisitsList(
    model: LocationModel,
    onOpenDay: (LocalDate) -> Unit,
    onOpenPlace: (LocationPlace) -> Unit,
    onOpenPeople: () -> Unit,
    onOpenTravel: () -> Unit,
    onShowSharing: () -> Unit,
    onOpenPhotos: (PlaceVisit) -> Unit,
) {
    val recording = LocalLocationRecording.current
    val isRecordingOn by recording.isRecordingOn.collectAsState()
    val text = rememberLocationText()
    val scope = rememberCoroutineScope()
    var forgetting by remember { mutableStateOf<PlaceVisit?>(null) }
    val days = model.days
    LazyColumn(Modifier.fillMaxWidth()) {
        item { LocationSharingBannerCard(model = model, onShowSharing = onShowSharing) }
        if (model.location != null || model.people.isNotEmpty()) item { LocationPeopleCard(model = model, onOpenPeople = onOpenPeople) }
        item {
            TeaNodeRow(
                headline = stringResource(R.string.location_travel), leadingIcon = Icons.AutoMirrored.Outlined.DirectionsWalk, onClick = onOpenTravel,
                trailing = { Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant) },
            )
        }
        if (days.isEmpty()) {
            item {
                EmptyState(
                    icon = Icons.Outlined.AddLocationAlt, title = stringResource(R.string.location_no_visits_yet),
                    description = stringResource(if (isRecordingOn) R.string.location_visit_shows_up_after_a_while else R.string.location_turn_on_record_where_i_go),
                    modifier = Modifier.height(260.dp),
                )
            }
        }
        days.forEach { day ->
            item(key = "day-" + day.id) {
                Row(
                    Modifier.fillMaxWidth().clickable { onOpenDay(day.day) }.padding(horizontal = 16.dp, vertical = 12.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text(text.dayTitle(day.day), style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
                    Icon(Icons.Outlined.Map, contentDescription = null, tint = MaterialTheme.colorScheme.primary)
                    Text(stringResource(R.string.location_map), modifier = Modifier.padding(start = 6.dp), color = MaterialTheme.colorScheme.primary, style = MaterialTheme.typography.labelLarge)
                }
            }
            items(day.entries, key = { entry -> entry.id }) { entry ->
                LocationTimelineRow(
                    entry = entry, onOpenPlace = onOpenPlace, onOpenPhotos = onOpenPhotos,
                    correctLeg = { leg, mode -> scope.launch { model.correct(leg, mode) } }, askToForgetVisit = { visit -> forgetting = visit },
                )
                if (entry is LocationTimelineEntry.Visit && entry.visit.id == model.visits.lastOrNull()?.id) {
                    LaunchedEffect(entry.visit.id) { model.loadMoreVisits() }
                }
            }
        }
        if (model.isLoadingMoreVisits) item { Box(Modifier.fillMaxWidth().padding(16.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() } }
        item { OpenStreetMapCredit() }
    }
    forgetting?.let { visit ->
        ForgetVisitDialog(onDismiss = { forgetting = null }, onForget = { forgetting = null; scope.launch { model.forget(visit) } })
    }
}

@Composable
fun ForgetVisitDialog(onDismiss: () -> Unit, onForget: () -> Unit) {
    ConfirmationDialog(
        title = stringResource(R.string.location_forget_this_visit), text = stringResource(R.string.location_forget_visit_explanation),
        confirmLabel = stringResource(R.string.location_forget), onConfirm = onForget, onDismiss = onDismiss, isDestructive = true,
    )
}

/** One row of a timeline: a visit (which opens its place), a pause, or the trip between two visits. */
@Composable
fun LocationTimelineRow(
    entry: LocationTimelineEntry,
    onOpenPlace: ((LocationPlace) -> Unit)? = null,
    onOpenPhotos: ((PlaceVisit) -> Unit)? = null,
    correctLeg: ((TripLeg, TravelMode?) -> Unit)? = null,
    askToForgetVisit: ((PlaceVisit) -> Unit)? = null,
) {
    when (entry) {
        is LocationTimelineEntry.Visit -> LocationVisitRow(entry.visit, onOpenPlace, onOpenPhotos, askToForgetVisit)
        is LocationTimelineEntry.Pause -> LocationPauseRow(entry.pause)
        is LocationTimelineEntry.TripEntry -> LocationTripRow(entry.trip, correctLeg)
    }
}

@Composable
fun LocationVisitRow(visit: PlaceVisit, onOpenPlace: ((LocationPlace) -> Unit)?, onOpenPhotos: ((PlaceVisit) -> Unit)?, askToForget: ((PlaceVisit) -> Unit)?) {
    val text = rememberLocationText()
    var isMenuOpen by remember { mutableStateOf(false) }
    Column(Modifier.fillMaxWidth().then(if (onOpenPlace != null) Modifier.clickable { onOpenPlace(visit.place) } else Modifier).padding(horizontal = 16.dp, vertical = 8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Icon(placeIcon(visit.place), contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.width(28.dp))
            Column(Modifier.weight(1f).padding(start = 12.dp)) {
                Text(text.shownName(visit.place), style = MaterialTheme.typography.bodyLarge)
                Text(text.summary(visit), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            // A visit in progress is ended by a check-in before it can be forgotten: the phone would report its end as a visit again.
            if (askToForget != null && visit.departedAt != null) {
                Box {
                    IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.location_more)) }
                    DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                        DropdownMenuItem(
                            text = { Text(stringResource(R.string.location_forget), color = MaterialTheme.colorScheme.error) },
                            leadingIcon = { Icon(Icons.Outlined.Delete, contentDescription = null, tint = MaterialTheme.colorScheme.error) },
                            onClick = { isMenuOpen = false; askToForget(visit) },
                        )
                    }
                }
            }
        }
        val photos = visit.photos
        if (!photos.isNullOrEmpty()) VisitPhotoStrip(visit, photos, onOpenPhotos)
    }
}

/** The first few photos taken during a visit, and how many more there are. */
@Composable
fun VisitPhotoStrip(visit: PlaceVisit, photos: List<VisitPhoto>, onOpenPhotos: ((PlaceVisit) -> Unit)?) {
    val totalCount = maxOf(visit.photoCount ?: photos.size, photos.size)
    val remainingCount = totalCount - photos.size
    Row(
        Modifier.padding(start = 40.dp, top = 8.dp).then(if (onOpenPhotos != null) Modifier.clickable { onOpenPhotos(visit) } else Modifier),
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        photos.forEach { photo ->
            val picture = rememberServerPicture(photo.smallPreviewUrl.ifEmpty { null })
            Box(Modifier.size(44.dp).clip(RoundedCornerShape(3.dp)).background(MaterialTheme.colorScheme.surfaceContainerHighest)) {
                if (picture != null) androidx.compose.foundation.Image(picture, contentDescription = null, contentScale = androidx.compose.ui.layout.ContentScale.Crop, modifier = Modifier.size(44.dp))
            }
        }
        if (remainingCount > 0) {
            Box(Modifier.size(44.dp).clip(RoundedCornerShape(3.dp)).background(MaterialTheme.colorScheme.surfaceContainerHighest), contentAlignment = Alignment.Center) {
                Text("+$remainingCount", style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

@Composable
fun LocationPauseRow(pause: LocationPause) {
    val text = rememberLocationText()
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        Icon(Icons.Outlined.PauseCircle, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.width(28.dp))
        Column(Modifier.padding(start = 12.dp)) {
            Text(stringResource(R.string.location_location_paused), style = MaterialTheme.typography.bodyLarge)
            Text(text.times(pause), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
fun LocationTripRow(trip: Trip, correctLeg: ((TripLeg, TravelMode?) -> Unit)?) {
    // A trip with no leg has nothing to draw.
    if (trip.legs.isEmpty()) return
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        trip.legs.forEach { leg ->
            if (correctLeg != null) LocationTripLegMenu(leg, correctLeg) else LocationTripLegLine(leg, isMenu = false)
        }
    }
}

@Composable
fun LocationTripLegLine(leg: TripLeg, isMenu: Boolean) {
    val text = rememberLocationText()
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Icon(travelModeIcon(leg.travelMode), contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.width(28.dp))
        Column(Modifier.weight(1f).padding(start = 12.dp)) {
            Text(text.travelModeTitle(leg.travelMode), style = MaterialTheme.typography.bodyLarge)
            Text(text.summary(leg), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        if (isMenu) Icon(Icons.Outlined.SwapVert, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** A leg of the person's own trips; touching it offers the other ways of travel, and taking the correction back. */
@Composable
fun LocationTripLegMenu(leg: TripLeg, correctLeg: (TripLeg, TravelMode?) -> Unit) {
    val text = rememberLocationText()
    var isOpen by remember { mutableStateOf(false) }
    Box {
        Box(Modifier.clickable { isOpen = true }) { LocationTripLegLine(leg, isMenu = true) }
        DropdownMenu(expanded = isOpen, onDismissRequest = { isOpen = false }) {
            TravelMode.entries.forEach { mode ->
                DropdownMenuItem(
                    text = { Text(text.travelModeTitle(mode.serverWord), fontWeight = if (mode == leg.mode) FontWeight.SemiBold else FontWeight.Normal) },
                    leadingIcon = { Icon(travelModeIcon(mode.serverWord), contentDescription = null) },
                    onClick = { isOpen = false; correctLeg(leg, mode) },
                )
            }
            HorizontalDivider()
            DropdownMenuItem(text = { Text(stringResource(R.string.location_travel_as_detected)) }, enabled = leg.isTravelModeCorrected, onClick = { isOpen = false; correctLeg(leg, null) })
        }
    }
}

// The people sharing

/** The notice at the top of the people when the person sees less of others than they share: their phone's permission, the recording switch, a pause, or what they share back. */
@Composable
fun LocationSharingBannerCard(model: LocationModel, onShowSharing: () -> Unit) {
    val banner = model.sharingBanner ?: return
    val recording = LocalLocationRecording.current
    val scope = rememberCoroutineScope()
    val snackbarText = stringResource(R.string.location_recording_resumed)
    Card(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainerHigh)) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(bannerIcon(banner), contentDescription = null)
                Text(bannerTitle(banner), modifier = Modifier.padding(start = 12.dp), style = MaterialTheme.typography.titleMedium)
            }
            bannerLines(banner).forEach { line -> Text(line, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant) }
            TextButton(
                onClick = {
                    when (banner) {
                        LocationSharingBanner.Paused -> { recording.resumeRecording(); model.say(snackbarText, false) }
                        LocationSharingBanner.RecordingOff -> recording.setRecording(true)
                        LocationSharingBanner.PhoneLocationOff, LocationSharingBanner.PhoneWhileUsing -> recording.openSystemSettings()
                        is LocationSharingBanner.SharesLess -> onShowSharing()
                    }
                },
            ) {
                Icon(bannerActionIcon(banner), contentDescription = null)
                Text(bannerAction(banner), modifier = Modifier.padding(start = 8.dp))
            }
        }
    }
    scope.hashCode()
}

private fun bannerIcon(banner: LocationSharingBanner): ImageVector = when (banner) {
    LocationSharingBanner.Paused -> Icons.Outlined.PauseCircle
    LocationSharingBanner.RecordingOff, LocationSharingBanner.PhoneLocationOff -> Icons.Outlined.LocationOff
    LocationSharingBanner.PhoneWhileUsing -> Icons.Outlined.LocationOn
    is LocationSharingBanner.SharesLess -> Icons.Outlined.VisibilityOff
}

private fun bannerActionIcon(banner: LocationSharingBanner): ImageVector = when (banner) {
    LocationSharingBanner.Paused -> Icons.Outlined.PlayCircle
    LocationSharingBanner.RecordingOff -> Icons.Outlined.LocationOn
    LocationSharingBanner.PhoneLocationOff, LocationSharingBanner.PhoneWhileUsing -> Icons.Outlined.Settings
    is LocationSharingBanner.SharesLess -> Icons.Outlined.People
}

@Composable
private fun bannerTitle(banner: LocationSharingBanner): String = stringResource(
    when (banner) {
        LocationSharingBanner.Paused, LocationSharingBanner.RecordingOff, LocationSharingBanner.PhoneLocationOff -> R.string.location_cant_see_anyones_location
        LocationSharingBanner.PhoneWhileUsing -> R.string.location_cant_see_anyones_visits
        is LocationSharingBanner.SharesLess -> R.string.location_you_see_less_of_some
    },
)

@Composable
private fun bannerAction(banner: LocationSharingBanner): String = stringResource(
    when (banner) {
        LocationSharingBanner.Paused -> R.string.location_resume
        LocationSharingBanner.RecordingOff -> R.string.location_turn_on_recording
        LocationSharingBanner.PhoneLocationOff, LocationSharingBanner.PhoneWhileUsing -> R.string.location_open_settings
        is LocationSharingBanner.SharesLess -> R.string.location_sharing
    },
)

@Composable
private fun bannerLines(banner: LocationSharingBanner): List<String> = when (banner) {
    LocationSharingBanner.Paused -> listOf(stringResource(R.string.location_banner_paused))
    LocationSharingBanner.RecordingOff -> listOf(stringResource(R.string.location_banner_recording_off))
    LocationSharingBanner.PhoneLocationOff -> listOf(stringResource(R.string.location_banner_phone_location_off))
    LocationSharingBanner.PhoneWhileUsing -> listOf(stringResource(R.string.location_banner_while_using))
    is LocationSharingBanner.SharesLess -> {
        val lines = banner.people.take(LocationSharingNotice.MOST_NAMED_PEOPLE).map { person ->
            if (person.isSeeingNothing) stringResource(R.string.location_banner_share_nothing, person.displayName)
            else stringResource(R.string.location_banner_share_current_only, person.displayName)
        }
        val moreCount = banner.people.size - LocationSharingNotice.MOST_NAMED_PEOPLE
        if (moreCount > 0) lines + stringResource(R.string.location_and_n_more, moreCount) else lines
    }
}

/** One card of rows, as Find My lists people: the person's own row, then everyone sharing, or how many when there are many. */
@Composable
fun LocationPeopleCard(model: LocationModel, onOpenPeople: () -> Unit) {
    val text = rememberLocationText()
    Card(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceContainer)) {
        Column {
            LocationOwnRow(model, isHighlightedWhenChosen = true)
            if (model.people.size > MOST_ROWS) {
                HorizontalDivider()
                Row(Modifier.fillMaxWidth().clickable(onClick = onOpenPeople).padding(horizontal = 16.dp).heightIn(min = 60.dp), verticalAlignment = Alignment.CenterVertically) {
                    Icon(Icons.Outlined.People, contentDescription = null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.width(44.dp))
                    Text(pluralStringResource(R.plurals.location_people_count, model.people.size, model.people.size), modifier = Modifier.weight(1f).padding(start = 12.dp), style = MaterialTheme.typography.bodyLarge)
                    Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            } else {
                model.people.forEach { person ->
                    HorizontalDivider()
                    LocationPersonRow(person, model.location, person.userId == model.selectedUserId, isHighlightedWhenChosen = true) { model.select(person.userId) }
                }
            }
        }
    }
    text.hashCode()
}

private const val MOST_ROWS = 3

@Composable
fun LocationOwnRow(model: LocationModel, isHighlightedWhenChosen: Boolean = false, didChoose: () -> Unit = {}) {
    val own = model.location ?: return
    val text = rememberLocationText()
    LocationPeopleRowLabel(
        person = own, nameText = stringResource(R.string.location_you), whereText = text.whereText(own, isOwn = true), distanceText = null,
        isSelected = model.selectedUserId == null, isHighlightedWhenChosen = isHighlightedWhenChosen,
    ) { model.focusOnOwn(); didChoose() }
}

@Composable
fun LocationPersonRow(person: PersonLocation, own: PersonLocation?, isSelected: Boolean, isHighlightedWhenChosen: Boolean = false, onClick: () -> Unit) {
    val text = rememberLocationText()
    LocationPeopleRowLabel(
        person = person, nameText = person.displayName, whereText = text.whereText(person), distanceText = text.distanceText(own, person),
        isSelected = isSelected, isHighlightedWhenChosen = isHighlightedWhenChosen, onClick = onClick,
    )
}

@Composable
fun LocationPeopleRowLabel(
    person: PersonLocation, nameText: String, whereText: String, distanceText: String?, isSelected: Boolean, isHighlightedWhenChosen: Boolean, onClick: () -> Unit,
) {
    Row(
        Modifier.fillMaxWidth().heightIn(min = 60.dp)
            .background(if (isSelected && isHighlightedWhenChosen) MaterialTheme.colorScheme.surfaceContainerHighest else androidx.compose.ui.graphics.Color.Transparent)
            .clickable(onClick = onClick).padding(horizontal = 16.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        AvatarView(person.displayName, person.avatarUrl, person.avatarColorIndex, size = 44.dp)
        Column(Modifier.weight(1f).padding(start = 12.dp)) {
            Text(nameText, style = MaterialTheme.typography.bodyLarge.copy(fontWeight = FontWeight.SemiBold), maxLines = 1)
            Text(whereText, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
        }
        if (distanceText != null) Text(distanceText, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** The people screen: everyone sharing, to choose one to show on the map. */
@Composable
fun LocationPeopleScreen(model: LocationModel, onBack: () -> Unit) {
    com.teanode.app.ui.components.TeaNodeScreen(title = stringResource(R.string.location_people), onBack = onBack) { padding ->
        LazyColumn(Modifier.padding(padding)) {
            item { LocationOwnRow(model) { onBack() } }
            items(model.people, key = { person -> person.userId }) { person ->
                LocationPersonRow(person, model.location, person.userId == model.selectedUserId) { model.select(person.userId); onBack() }
            }
        }
    }
}

// Places

@Composable
fun LocationPlacesList(model: LocationModel, onOpenPlace: (LocationPlace) -> Unit) {
    val text = rememberLocationText()
    val scope = rememberCoroutineScope()
    var isFilterOpen by remember { mutableStateOf(false) }
    LaunchedEffect(model.placeSearchText) {
        if (!model.isPlaceSearchPending) return@LaunchedEffect
        // Read a moment after the last key, not at every key.
        delay(350)
        model.reloadPlaces()
    }
    LazyColumn(Modifier.fillMaxWidth()) {
        item {
            Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically) {
                OutlinedTextField(
                    value = model.placeSearchText, onValueChange = { typed -> model.placeSearchText = typed }, singleLine = true,
                    leadingIcon = { Icon(Icons.Outlined.Search, contentDescription = null) }, placeholder = { Text(stringResource(R.string.location_search_places)) },
                    modifier = Modifier.weight(1f),
                )
                Box {
                    TextButton(onClick = { isFilterOpen = true }) {
                        Text(model.placeLabelFilter?.let { label -> text.labelTitle(label) } ?: stringResource(R.string.location_all_labels), maxLines = 1)
                        Icon(Icons.Outlined.ExpandMore, contentDescription = null)
                    }
                    DropdownMenu(expanded = isFilterOpen, onDismissRequest = { isFilterOpen = false }) {
                        DropdownMenuItem(text = { Text(stringResource(R.string.location_all_labels)) }, onClick = { isFilterOpen = false; scope.launch { model.setPlaceLabelFilter(null) } })
                        PlaceLabel.entries.forEach { label ->
                            DropdownMenuItem(
                                text = { Text(text.labelTitle(label)) }, leadingIcon = { Icon(placeLabelIcon(label), contentDescription = null) },
                                onClick = { isFilterOpen = false; scope.launch { model.setPlaceLabelFilter(label) } },
                            )
                        }
                    }
                }
            }
        }
        if (model.places.isEmpty()) {
            item {
                if (model.isFilteringPlaces) {
                    EmptyState(Icons.Outlined.Search, stringResource(R.string.location_no_results), modifier = Modifier.height(240.dp))
                } else {
                    EmptyState(
                        Icons.Outlined.AddLocationAlt, stringResource(R.string.location_no_places_yet), description = stringResource(R.string.location_places_show_up_here),
                        modifier = Modifier.height(240.dp),
                    )
                }
            }
        }
        items(model.places, key = { place -> place.id }) { place ->
            LocationPlaceRow(place, onClick = { onOpenPlace(place) })
            if (place.id == model.places.lastOrNull()?.id) LaunchedEffect(place.id) { model.loadMorePlaces() }
        }
        if (model.isLoadingMorePlaces) item { Box(Modifier.fillMaxWidth().padding(16.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator() } }
        item { OpenStreetMapCredit() }
    }
}

@Composable
fun LocationPlaceRow(place: LocationPlace, onClick: (() -> Unit)? = null) {
    val text = rememberLocationText()
    Row(
        Modifier.fillMaxWidth().then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier).padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(placeIcon(place), contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.width(28.dp))
        Column(Modifier.weight(1f).padding(start = 12.dp)) {
            Text(text.shownName(place), style = MaterialTheme.typography.bodyLarge)
            text.secondaryName(place)?.let { secondary -> Text(secondary, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant) }
            Text(text.visits(place), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        if (onClick != null) Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

// Sharing

@Composable
fun LocationSharingList(model: LocationModel) {
    val text = rememberLocationText()
    val scope = rememberCoroutineScope()
    var isSharing by remember { mutableStateOf(false) }
    var stopping by remember { mutableStateOf<LocationShare?>(null) }
    LazyColumn(Modifier.fillMaxWidth()) {
        item { TeaNodeSectionHeader(stringResource(R.string.location_shared_with_header)) }
        if (model.shares.isEmpty()) {
            item { Text(stringResource(R.string.location_not_shared_yet), Modifier.padding(horizontal = 16.dp, vertical = 8.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) }
        }
        items(model.shares, key = { share -> share.id }) { share -> LocationShareRow(share, text, onChange = { level -> scope.launch { model.change(share, level) } }, onStop = { stopping = share }) }
        if (model.shares.isNotEmpty()) item { Text(stringResource(R.string.location_touch_a_row), Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
        item {
            TextButton(onClick = { isSharing = true }, modifier = Modifier.padding(horizontal = 8.dp)) {
                Icon(Icons.Outlined.PersonAdd, contentDescription = null)
                Text(stringResource(R.string.location_share), modifier = Modifier.padding(start = 8.dp))
            }
            Text(stringResource(R.string.location_share_footer), Modifier.padding(horizontal = 16.dp), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        model.sharingProblemText?.let { problem -> item { Text(problem, Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant) } }
    }
    stopping?.let { share ->
        ConfirmationDialog(
            title = stringResource(R.string.location_stop_sharing), text = stringResource(R.string.location_will_no_longer_see, text.shareName(share)),
            confirmLabel = stringResource(R.string.location_stop_sharing), onConfirm = { stopping = null; scope.launch { model.stop(share) } }, onDismiss = { stopping = null },
            isDestructive = true,
        )
    }
    if (isSharing) LocationShareSheet(model = model, onDismiss = { isSharing = false })
}

@Composable
private fun LocationShareRow(share: LocationShare, text: LocationText, onChange: (LocationShareLevel) -> Unit, onStop: () -> Unit) {
    var isOpen by remember { mutableStateOf(false) }
    Box {
        Row(Modifier.fillMaxWidth().clickable { isOpen = true }.padding(horizontal = 16.dp, vertical = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text(text.shareName(share), style = MaterialTheme.typography.bodyLarge)
                Text(share.level?.let { level -> text.levelTitle(level) } ?: share.shareLevel, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            Icon(Icons.Outlined.SwapVert, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        DropdownMenu(expanded = isOpen, onDismissRequest = { isOpen = false }) {
            LocationShareLevel.grantable.forEach { level ->
                DropdownMenuItem(
                    text = { Text(text.levelTitle(level), fontWeight = if (level == share.level) FontWeight.SemiBold else FontWeight.Normal) },
                    onClick = { isOpen = false; onChange(level) },
                )
            }
            HorizontalDivider()
            DropdownMenuItem(
                text = { Text(stringResource(R.string.location_stop_sharing), color = MaterialTheme.colorScheme.error) },
                leadingIcon = { Icon(Icons.Outlined.PersonRemove, contentDescription = null, tint = MaterialTheme.colorScheme.error) },
                onClick = { isOpen = false; onStop() },
            )
        }
    }
}
