package com.teanode.app.location

import java.time.Instant
import com.teanode.kit.location.PlaceVisit
import com.teanode.kit.location.ServerTime
import com.teanode.app.photos.PhotoFilterRequest
import androidx.activity.ComponentActivity
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.BottomSheetScaffold
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SheetValue
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.material3.rememberBottomSheetScaffoldState
import androidx.compose.material3.rememberStandardBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewmodel.compose.viewModel
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.shell.Routes
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.AppSection
import kotlinx.coroutines.launch
import java.time.LocalDate

/** The routes Location can open, besides its first screen. */
object LocationRoutes {
    val home: String = Routes.home(AppSection.Location)
    val settings: String = Routes.child(AppSection.Location, "settings")
    val travel: String = Routes.child(AppSection.Location, "travel")
    val people: String = Routes.child(AppSection.Location, "people")
    const val USER_ARGUMENT = "userId"
    val personTravel: String = Routes.child(AppSection.Location, "travel/{$USER_ARGUMENT}")
    fun personTravel(userId: String): String = Routes.child(AppSection.Location, "travel/$userId")
    const val DAY_ARGUMENT = "day"
    const val PLACE_ARGUMENT = "placeId"
    val day: String = Routes.child(AppSection.Location, "day/{$DAY_ARGUMENT}")
    val place: String = Routes.child(AppSection.Location, "place/{$PLACE_ARGUMENT}")
    val homeAddress: String = Routes.child(AppSection.Location, "home-address")
    val placeAddress: String = Routes.child(AppSection.Location, "place-address/{$PLACE_ARGUMENT}")

    fun day(day: LocalDate): String = Routes.child(AppSection.Location, "day/$day")
    fun place(placeId: String): String = Routes.child(AppSection.Location, "place/$placeId")
    fun placeAddress(placeId: String): String = Routes.child(AppSection.Location, "place-address/$placeId")
}

/** What the drawer over the map shows, chosen at the top of the drawer. */
enum class LocationSegment(val titleResource: Int) {
    Visits(R.string.location_visits),
    Places(R.string.location_places),
    Sharing(R.string.location_sharing),
}

/** The model of the signed-in account, kept across the screens of the part and across rotation. */
@Composable
fun rememberLocationModel(): LocationModel {
    val connection = LocalConnection.current
    val session = LocalAppSession.current
    val context = LocalContext.current
    val recording = LocalLocationRecording.current
    val text = rememberLocationText()
    val words = remember(context, text, session) { LocationModelWords(context, text) { error -> session.describeLoadProblem(error) } }
    val owner = LocalContext.current as? ComponentActivity
    val factory = remember(connection, recording, words) { LocationModelFactory(connection, recording, words, isFeedEnabled = !session.isDemo) }
    return if (owner != null) {
        viewModel(viewModelStoreOwner = owner, key = "location-" + connection.accountKey, factory = factory)
    } else {
        viewModel(key = "location-" + connection.accountKey, factory = factory)
    }
}

/** Shows what the model wants said, in the snackbar. */
@Composable
fun LocationNoticeEffect(model: LocationModel) {
    val snackbar = LocalSnackbar.current
    val notice = model.notice
    LaunchedEffect(notice) {
        when (notice) {
            is LocationNotice.Done -> snackbar.show(notice.text)
            is LocationNotice.Problem -> snackbar.showProblem(notice.text)
            null -> Unit
        }
        if (notice != null) model.notice = null
    }
}

/**
 * The first screen of Location: where the person is and where they have been. A map of the whole screen with their
 * position and the people sharing with them, and over it a drawer of their visits by day, their places and their sharing,
 * chosen at the top of the drawer.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun LocationSection() {
    val model = rememberLocationModel()
    val navigator = LocalAppNavigator.current
    LocationHome(
        model = model,
        onOpenDay = { day -> navigator.navigate(LocationRoutes.day(day)) },
        onOpenPlace = { place -> navigator.navigate(LocationRoutes.place(place.id)) },
        onOpenPeople = { navigator.navigate(LocationRoutes.people) },
        onOpenTravel = { navigator.navigate(LocationRoutes.travel) },
        onOpenPersonTravel = { userId -> navigator.navigate(LocationRoutes.personTravel(userId)) },
        onOpenSettings = { navigator.navigate(LocationRoutes.settings) },
        onOpenHomeAddress = { navigator.navigate(LocationRoutes.homeAddress) },
        onOpenPhotos = { visit ->
            // Photos opens at exactly this visit: from its arrival to when the photos counted end, or now for a visit still going on.
            PhotoFilterRequest.open(visit.arrivedInstant, visit.photosTakenBefore?.let { text -> ServerTime.instant(text) } ?: visit.departedInstant ?: Instant.now())
            navigator.navigate(Routes.home(AppSection.Photos))
        },
    )
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun LocationHome(
    model: LocationModel,
    onOpenDay: (LocalDate) -> Unit,
    onOpenPlace: (com.teanode.kit.location.LocationPlace) -> Unit,
    onOpenPeople: () -> Unit,
    onOpenTravel: () -> Unit,
    onOpenPersonTravel: (String) -> Unit = {},
    onOpenSettings: () -> Unit,
    onOpenHomeAddress: () -> Unit,
    onOpenPhotos: (PlaceVisit) -> Unit,
    initialSheetValue: SheetValue = SheetValue.PartiallyExpanded,
    initialSegment: LocationSegment = LocationSegment.Visits,
) {
    val recording = LocalLocationRecording.current
    var segment by rememberSaveable { mutableStateOf(initialSegment) }
    var isCheckingIn by remember { mutableStateOf(false) }
    val sheetState = rememberStandardBottomSheetState(initialValue = initialSheetValue, skipHiddenState = true)
    val scaffoldState = rememberBottomSheetScaffoldState(bottomSheetState = sheetState)
    val scope = rememberCoroutineScope()
    val activePause by recording.activePause.collectAsState()
    val limitRevision by recording.sharingLimitRevision.collectAsState()
    val permission by recording.permission.collectAsState()
    val text = rememberLocationText()

    LaunchedEffect(Unit) {
        model.load()
        // The first time, the setup asks in its own row instead.
        if (!model.isSetupShown && permission == LocationPermissionState.Undecided) recording.requestPermission()
    }
    LaunchedEffect(Unit) { model.follow().join() }
    LaunchedEffect(limitRevision) { if (limitRevision > 0) model.sharingLimitDidChange() }
    LaunchedEffect(permission) { model.finishSetupIfDone() }
    LocationNoticeEffect(model)

    val isShowingPeople = segment == LocationSegment.Visits
    TeaNodeScreen(
        title = stringResource(R.string.location_title),
        actions = {
            IconButton(onClick = { isCheckingIn = true }) {
                Icon(Icons.Outlined.PinDrop, contentDescription = stringResource(R.string.location_check_in))
            }
            IconButton(onClick = onOpenSettings) {
                Icon(Icons.Outlined.Settings, contentDescription = stringResource(R.string.location_settings_title))
            }
        },
    ) { contentPadding ->
        BottomSheetScaffold(
            scaffoldState = scaffoldState,
            modifier = Modifier.padding(contentPadding),
            sheetPeekHeight = 320.dp,
            sheetSwipeEnabled = true,
            sheetContent = {
                Column(Modifier.fillMaxWidth().fillMaxHeight(0.92f)) {
                    LocationDrawerHeader(
                        model = model, segment = segment, isShowingPeople = isShowingPeople, pauseText = activePause?.let { pause -> text.pausedUntil(pause) },
                        onSegment = { chosen ->
                            segment = chosen
                            if (chosen != LocationSegment.Visits) model.clearSelection()
                            if (model.isSetupShown) model.dismissSetup()
                        },
                        onMute = { person, choice -> scope.launch { model.muteNotifications(person, choice) } },
                        onUnmute = { person -> scope.launch { model.unmuteNotifications(person) } },
                    )
                    LocationDrawerContent(
                        model = model, segment = segment, isShowingPeople = isShowingPeople, onOpenDay = onOpenDay, onOpenPlace = onOpenPlace, onOpenPeople = onOpenPeople,
                        onOpenTravel = onOpenTravel, onOpenPersonTravel = onOpenPersonTravel, onOpenHomeAddress = onOpenHomeAddress, onOpenPhotos = onOpenPhotos, onShowSharing = { segment = LocationSegment.Sharing },
                        onRetry = { scope.launch { model.load() } },
                    )
                }
            },
        ) { _ ->
            Box(Modifier.fillMaxSize()) {
                LocationMapHeader(model = model, isShowingPeople = isShowingPeople, modifier = Modifier.fillMaxSize())
            }
        }
    }
    if (isCheckingIn) CheckInSheet(model = model, onDismiss = { isCheckingIn = false })
}

/** The top of the drawer: the lists to choose between, or, while a person sharing is chosen, who they are and the way back to everyone. */
@Composable
private fun LocationDrawerHeader(
    model: LocationModel,
    segment: LocationSegment,
    isShowingPeople: Boolean,
    pauseText: String?,
    onSegment: (LocationSegment) -> Unit,
    onMute: (com.teanode.kit.location.PersonLocation, com.teanode.kit.location.LocationPauseChoice) -> Unit,
    onUnmute: (com.teanode.kit.location.PersonLocation) -> Unit,
) {
    val person = model.selectedPerson
    if (isShowingPeople && person != null) {
        LocationChosenPersonHeader(person = person, close = { model.focusOnOwn() }, mute = { choice -> onMute(person, choice) }, unmute = { onUnmute(person) })
        return
    }
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp).padding(bottom = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        if (pauseText != null) {
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.Center, verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.Outlined.PauseCircle, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
                Text(pauseText, modifier = Modifier.padding(start = 6.dp), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
            LocationSegment.entries.forEachIndexed { index, choice ->
                SegmentedButton(
                    selected = choice == segment, onClick = { onSegment(choice) },
                    shape = SegmentedButtonDefaults.itemShape(index, LocationSegment.entries.size),
                ) { Text(stringResource(choice.titleResource), maxLines = 1) }
            }
        }
    }
}

/** What the drawer holds under its header. */
@Composable
private fun LocationDrawerContent(
    model: LocationModel,
    segment: LocationSegment,
    isShowingPeople: Boolean,
    onOpenDay: (LocalDate) -> Unit,
    onOpenPlace: (com.teanode.kit.location.LocationPlace) -> Unit,
    onOpenPeople: () -> Unit,
    onOpenTravel: () -> Unit,
    onOpenPersonTravel: (String) -> Unit = {},
    onOpenHomeAddress: () -> Unit,
    onOpenPhotos: (PlaceVisit) -> Unit,
    onShowSharing: () -> Unit,
    onRetry: () -> Unit,
) {
    val chosen = model.selectedPerson
    val chosenVisits = model.chosenPersonVisits
    val failure = model.loadFailedText
    when {
        isShowingPeople && chosen != null && chosenVisits != null -> LocationChosenPersonVisits(state = chosenVisits, person = chosen, onOpenPlace = onOpenPlace, onOpenPersonTravel = onOpenPersonTravel)
        model.isSetupShown -> LocationSetupList(
            model = model, isShowingPeople = isShowingPeople, onOpenPeople = onOpenPeople, onShowSharing = onShowSharing, onOpenHomeAddress = onOpenHomeAddress,
        )
        failure != null -> ErrorState(failure, onRetry = onRetry)
        !model.hasLoaded -> LoadingState()
        else -> when (segment) {
            LocationSegment.Visits -> LocationVisitsList(
                model = model, onOpenDay = onOpenDay, onOpenPlace = onOpenPlace, onOpenPeople = onOpenPeople, onOpenTravel = onOpenTravel, onShowSharing = onShowSharing,
                onOpenPhotos = onOpenPhotos,
            )
            LocationSegment.Places -> LocationPlacesList(model = model, onOpenPlace = onOpenPlace)
            LocationSegment.Sharing -> LocationSharingList(model = model)
        }
    }
}
