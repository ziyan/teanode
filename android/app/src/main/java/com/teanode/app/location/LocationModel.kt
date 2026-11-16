package com.teanode.app.location

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeError
import com.teanode.kit.location.CheckInTarget
import com.teanode.kit.location.FoundPlace
import com.teanode.kit.location.FoundPlacePage
import com.teanode.kit.location.LocationDays
import com.teanode.kit.location.LocationEvent
import com.teanode.kit.location.LocationPause
import com.teanode.kit.location.LocationPlace
import com.teanode.kit.location.LocationPoint
import com.teanode.kit.location.LocationSetup
import com.teanode.kit.location.LocationShare
import com.teanode.kit.location.LocationShareLevel
import com.teanode.kit.location.LocationSharingBanner
import com.teanode.kit.location.LocationSharingLimit
import com.teanode.kit.location.LocationSharingNotice
import com.teanode.kit.location.LocationVisitDay
import com.teanode.kit.location.LocationPauseChoice
import com.teanode.kit.location.PeopleCircle
import com.teanode.kit.location.PersonLocation
import com.teanode.kit.location.PlaceChange
import com.teanode.kit.location.PlaceLabel
import com.teanode.kit.location.PlaceVisit
import com.teanode.kit.location.PlaceVisitPage
import com.teanode.kit.location.ShareTarget
import com.teanode.kit.location.TravelMode
import com.teanode.kit.location.Trip
import com.teanode.kit.location.TripLeg
import com.teanode.kit.location.checkIn
import com.teanode.kit.location.circles
import com.teanode.kit.location.eraseLocationHistory
import com.teanode.kit.location.forgetPlace
import com.teanode.kit.location.forgetPlaceVisit
import com.teanode.kit.location.location
import com.teanode.kit.location.locationPauses
import com.teanode.kit.location.locationPlaces
import com.teanode.kit.location.locationPoints
import com.teanode.kit.location.locationShares
import com.teanode.kit.location.locationSharingLimit
import com.teanode.kit.location.locationVisits
import com.teanode.kit.location.muteLocationNotifications
import com.teanode.kit.location.nearbyPlaces
import com.teanode.kit.location.savePlace
import com.teanode.kit.location.searchAddresses
import com.teanode.kit.location.searchPlaces
import com.teanode.kit.location.setTripLegTravelMode
import com.teanode.kit.location.shareLocation
import com.teanode.kit.location.sharedLocations
import com.teanode.kit.location.trips
import com.teanode.kit.location.unmuteLocationNotifications
import com.teanode.kit.location.unshareLocation
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.async
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId

/** What a message in the snackbar says: a success, or a problem. */
sealed class LocationNotice {
    data class Done(val text: String) : LocationNotice()
    data class Problem(val text: String) : LocationNotice()
}

/**
 * LocationModel is the Location part: where the person is, the visits they have made (newest first, a page at a time),
 * the places those visits were at, the people sharing with them and their own shares. It mirrors the iPhone app's
 * `LocationModel`. Strings the person reads come from [words]; the screens show [notices] in the snackbar.
 */
class LocationModel(
    val connection: Connection,
    private val recording: LocationRecording,
    private val words: LocationModelWords,
    private val accountKey: String? = connection.accountKey,
    private val zone: ZoneId = ZoneId.systemDefault(),
    private val isFeedEnabled: Boolean = true,
) : ViewModel() {
    var location: PersonLocation? by mutableStateOf(null)
        private set
    var visits: List<PlaceVisit> by mutableStateOf(emptyList())
        private set
    var nextVisitOffset: Int? by mutableStateOf(null)
        private set

    /** The person's pauses over the stretch the visits cover, newest first, which the timeline shows among the visits. */
    var pauses: List<LocationPause> by mutableStateOf(emptyList())
        private set

    /** The person's trips over the stretch the visits cover, newest first, which the timeline shows between the visits. */
    var trips: List<Trip> by mutableStateOf(emptyList())
        private set
    var places: List<LocationPlace> by mutableStateOf(emptyList())
        private set
    var nextPlaceOffset: Int? by mutableStateOf(null)
        private set

    /** Whether the first read has finished, one way or the other. */
    var hasLoaded: Boolean by mutableStateOf(false)
        private set
    var isLoading: Boolean by mutableStateOf(false)
        private set
    var isLoadingMoreVisits: Boolean by mutableStateOf(false)
        private set
    var isLoadingMorePlaces: Boolean by mutableStateOf(false)
        private set

    /** Why the first read failed, when there is nothing to show instead. */
    var loadFailedText: String? by mutableStateOf(null)
        private set

    /** What to say in the snackbar next: set by the model, cleared by the screen once shown. */
    var notice: LocationNotice? by mutableStateOf(null)

    // What the lists are filtered by

    /** Only places with this label, when one is chosen. */
    var placeLabelFilter: PlaceLabel? by mutableStateOf(null)
        private set

    /** Words the places' names are searched for, as typed. */
    var placeSearchText: String by mutableStateOf("")
    private var readPlaceQuery = ""

    // People sharing, the person's own shares and the setup

    var people: List<PersonLocation> by mutableStateOf(emptyList())
        private set

    /** What limits the people the person sees, as the server last said; null until it has. */
    var sharingLimit: LocationSharingLimit? by mutableStateOf(null)
        private set
    var shares: List<LocationShare> by mutableStateOf(emptyList())
        private set
    var circles: List<PeopleCircle> by mutableStateOf(emptyList())
        private set
    var homePlaces: List<LocationPlace> by mutableStateOf(emptyList())
        private set

    /** Whether the shares and the home places were both read, which is what the setup is worked out from: a failed read must not end it. */
    var isSetupKnown: Boolean by mutableStateOf(false)
        private set
    var sharingProblemText: String? by mutableStateOf(null)
        private set
    var isSetupDismissed: Boolean by mutableStateOf(recording.isSetupDismissed(accountKey))
        private set

    // The person selected on the map

    /** Who the map is centered on: somebody sharing, or nobody for the person's own. */
    var selectedUserId: String? by mutableStateOf(null)
        private set
    var chosenPersonVisits: PersonVisitsState? by mutableStateOf(null)
        private set

    /** How many times the person asked to see their own position on the map; the map centers on it each time this changes. */
    var ownFocusCount: Int by mutableIntStateOf(0)
        private set

    fun focusOnOwn() {
        clearSelection()
        ownFocusCount += 1
    }

    val selectedPerson: PersonLocation? get() = people.firstOrNull { person -> person.userId == selectedUserId }

    /** How far setting Location up has got. */
    val setup: LocationSetup
        get() = LocationSetup.from(recording.permission.value == LocationPermissionState.Always, homePlaces, circles, shares)

    val isSetupShown: Boolean
        get() = hasLoaded && isSetupKnown && location != null && setup.isShown(isSetupDismissed, location?.latestPoint != null)

    val ownUserId: String? get() = location?.userId

    /** The notice at the top of the people, if one applies. */
    val sharingBanner: LocationSharingBanner? get() = LocationSharingNotice.banner(sharingLimit, people)

    /** The visits and pauses grouped by day, newest first. */
    val days: List<LocationVisitDay> get() = LocationDays.group(visits, pauses, trips, zone)

    val isFilteringPlaces: Boolean get() = placeLabelFilter != null || placeSearchText.trim().isNotEmpty()

    val isPlaceSearchPending: Boolean get() = placeSearchText.trim() != readPlaceQuery

    private fun done(text: String) {
        notice = LocationNotice.Done(text)
    }

    private fun problem(text: String) {
        notice = LocationNotice.Problem(text)
    }

    private fun refusalReason(error: Throwable): String =
        (error as? TeaNodeError.Refused)?.reason?.takeIf { reason -> reason.isNotBlank() } ?: words.tryAgain

    // Reading

    /** Reads where the person is, the first page of visits and of places. */
    suspend fun load() {
        if (isLoading) return
        isLoading = true
        try {
            val placeLabel = placeLabelFilter?.serverWord
            val placeQuery = placeSearchText.trim()
            val readLocation = viewModelScope.async { runCatching { connection.location() } }
            val readVisits = viewModelScope.async { runCatching { connection.locationVisits(limitCount = VISIT_PAGE_SIZE) } }
            val readPlaces = viewModelScope.async { runCatching { connection.locationPlaces(placeLabel = placeLabel, query = placeQuery, limitCount = PLACE_PAGE_SIZE) } }
            val readPeople = viewModelScope.async { runCatching { connection.sharedLocations() } }
            val readShares = viewModelScope.async { runCatching { connection.locationShares() } }
            val readCircles = viewModelScope.async { runCatching { connection.circles() } }
            val readHomes = viewModelScope.async { runCatching { connection.locationPlaces(placeLabel = PlaceLabel.Home.serverWord, limitCount = 1) } }
            val locationResult = readLocation.await()
            val visitsResult = readVisits.await()
            val placesResult = readPlaces.await()
            applyPeople(readPeople.await(), readShares.await(), readCircles.await(), readHomes.await())
            readSharingLimit()
            var failure: Throwable? = null
            locationResult.onSuccess { read ->
                location = read
                recording.learnPause(read.pause)
            }.onFailure { error -> failure = error }
            visitsResult.onSuccess { page ->
                visits = page.placeVisits
                nextVisitOffset = page.nextOffset
            }.onFailure { error -> failure = failure ?: error }
            placesResult.onSuccess { page ->
                places = page.places
                nextPlaceOffset = page.nextOffset
                readPlaceQuery = placeQuery
            }.onFailure { error -> failure = failure ?: error }
            val firstFailure = failure
            if (firstFailure != null) {
                // Said in the snackbar when there is something to show, and as the whole screen's problem when there is not.
                if (location == null && visits.isEmpty() && places.isEmpty()) loadFailedText = words.describeLoadProblem(firstFailure)
                else problem(words.describeLoadProblem(firstFailure))
            } else {
                loadFailedText = null
            }
            finishSetupIfDone()
            viewModelScope.launch {
                readPauses()
                readTrips()
            }
        } finally {
            isLoading = false
            hasLoaded = true
        }
    }

    private fun applyPeople(
        peopleResult: Result<List<PersonLocation>>, sharesResult: Result<List<LocationShare>>, circlesResult: Result<List<PeopleCircle>>,
        homesResult: Result<com.teanode.kit.location.PlacePage>,
    ) {
        peopleResult.onSuccess { read ->
            people = read
            // Somebody who stopped sharing is no longer selected.
            val selected = selectedUserId
            if (selected != null && read.none { person -> person.userId == selected }) clearSelection()
        }
        // A person with no circle permission has no circles; that is not a failure.
        circles = circlesResult.getOrDefault(emptyList())
        val readShares = sharesResult.getOrNull()
        val homes = homesResult.getOrNull()
        if (readShares != null && homes != null) {
            shares = readShares
            homePlaces = homes.places
            isSetupKnown = true
            sharingProblemText = null
        } else {
            sharingProblemText = words.describeLoadProblem(sharesResult.exceptionOrNull() ?: homesResult.exceptionOrNull()!!)
        }
    }

    /** Reads the trips from the oldest visit shown on. A trip begins at the departure from a visit, so none is older. */
    private suspend fun readTrips() {
        val oldest = visits.mapNotNull { visit -> visit.anchorInstant }.minOrNull()
        if (oldest == null) {
            trips = emptyList()
            return
        }
        val read = fetchTrips(connection, null, oldest, Instant.now().plusSeconds(3600)) ?: return
        trips = read
    }

    private suspend fun readTrips(from: Instant, until: Instant) {
        val read = fetchTrips(connection, null, from, until) ?: return
        val readIds = read.map { trip -> trip.id }.toSet()
        val merged = trips.filter { trip -> trip.id !in readIds } + read
        trips = merged.sortedByDescending { trip -> trip.startedInstant ?: Instant.MIN }
    }

    /** Corrects the way of travel of one of the person's own legs, or with null takes the correction back. True when the server took it. */
    suspend fun correct(leg: TripLeg, travelMode: TravelMode?): Boolean {
        try {
            connection.setTripLegTravelMode(leg.id, travelMode)
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            problem(refusalReason(error))
            return false
        }
        readTrips()
        return true
    }

    /** Reads the pauses over the stretch the visits shown cover: back to the oldest visit while there are more to read, else a year. */
    private suspend fun readPauses() {
        val oldest = visits.mapNotNull { visit -> visit.anchorInstant }.minOrNull()
        val from = if (nextVisitOffset != null && oldest != null) oldest else Instant.now().minusSeconds(PAUSE_LOOKBACK_SECONDS)
        pauses = runCatching { connection.locationPauses(null, from, Instant.now()) }.getOrNull() ?: return
    }

    /** The pauses of a stretch, for a screen that lists them among visits; none when they cannot be read. */
    suspend fun pauses(from: Instant, until: Instant): List<LocationPause> = runCatching { connection.locationPauses(null, from, until) }.getOrDefault(emptyList())

    /** The next page of visits, once the last one read comes into view. */
    suspend fun loadMoreVisits() {
        val offset = nextVisitOffset ?: return
        if (isLoadingMoreVisits) return
        isLoadingMoreVisits = true
        try {
            val page = connection.locationVisits(offset = offset, limitCount = VISIT_PAGE_SIZE)
            val known = visits.map { visit -> visit.id }.toSet()
            val previousOldest = visits.mapNotNull { visit -> visit.anchorInstant }.minOrNull()
            val added = page.placeVisits.filter { visit -> visit.id !in known }
            visits = visits + added
            nextVisitOffset = page.nextOffset
            readPauses()
            val addedOldest = added.mapNotNull { visit -> visit.anchorInstant }.minOrNull()
            if (previousOldest != null && addedOldest != null && addedOldest.isBefore(previousOldest)) readTrips(addedOldest, previousOldest)
            else if (previousOldest == null) readTrips()
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            problem(words.describeLoadProblem(error))
        } finally {
            isLoadingMoreVisits = false
        }
    }

    suspend fun loadMorePlaces() {
        val offset = nextPlaceOffset ?: return
        if (isLoadingMorePlaces) return
        isLoadingMorePlaces = true
        try {
            val page = connection.locationPlaces(placeLabel = placeLabelFilter?.serverWord, query = readPlaceQuery, offset = offset, limitCount = PLACE_PAGE_SIZE)
            val known = places.map { place -> place.id }.toSet()
            places = places + page.places.filter { place -> place.id !in known }
            nextPlaceOffset = page.nextOffset
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            problem(words.describeLoadProblem(error))
        } finally {
            isLoadingMorePlaces = false
        }
    }

    /** Shows only places with this label, or all of them. */
    suspend fun setPlaceLabelFilter(label: PlaceLabel?) {
        if (label == placeLabelFilter) return
        placeLabelFilter = label
        reloadPlaces()
    }

    /** Reads the first page of places again, for the label and the words typed. */
    suspend fun reloadPlaces() {
        val query = placeSearchText.trim()
        try {
            val page = connection.locationPlaces(placeLabel = placeLabelFilter?.serverWord, query = query, limitCount = PLACE_PAGE_SIZE)
            // The words changed while this was read.
            if (query != placeSearchText.trim()) return
            places = page.places
            nextPlaceOffset = page.nextOffset
            readPlaceQuery = query
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            problem(words.describeLoadProblem(error))
        }
    }

    /** The points of one day, for drawing the path. */
    suspend fun points(day: LocalDate): List<LocationPoint> {
        val window = LocationDays.window(day, zone)
        return connection.locationPoints(null, window.first, window.second)
    }

    /** The visits to one place, newest first, a page at a time. */
    suspend fun visits(atPlace: String, offset: Int = 0): PlaceVisitPage =
        connection.locationVisits(placeId = atPlace, offset = offset, limitCount = VISIT_PAGE_SIZE)

    // Changing

    /** Names, labels or resizes a place. The place is told to the lists that show it, and said plainly when the server refused. */
    suspend fun save(change: PlaceChange): LocationPlace? = try {
        val saved = connection.savePlace(change)
        replace(saved)
        recording.placesDidChange()
        saved
    } catch (error: Exception) {
        if (error is CancellationException) throw error
        problem(words.describeLoadProblem(error))
        null
    }

    /** Moves a place to an address the person chose, or makes the place there. The reason is returned, not shown, for the address screen to say itself. */
    suspend fun saveAddress(change: PlaceChange): Pair<LocationPlace?, String?> = try {
        val saved = connection.savePlace(change)
        replace(saved)
        recording.placesDidChange()
        saved to null
    } catch (error: Exception) {
        if (error is CancellationException) throw error
        null to refusalReason(error)
    }

    private fun replace(saved: LocationPlace) {
        places = places.map { place -> if (place.id == saved.id) saved else place }
        visits = visits.map { visit -> if (visit.place.id == saved.id) visit.copy(place = saved, photoCount = visit.photoCount, photos = visit.photos) else visit }
        val current = location
        val open = current?.openVisit
        if (current != null && open != null && open.place.id == saved.id) location = current.copy(openVisit = open.copy(place = saved))
    }

    /** Deletes a place and its visits. */
    suspend fun forget(place: LocationPlace): Boolean = try {
        connection.forgetPlace(place.id)
        places = places.filter { each -> each.id != place.id }
        visits = visits.filter { visit -> visit.place.id != place.id }
        if (location?.openVisit?.place?.id == place.id) load()
        recording.placesDidChange()
        done(words.forgot(words.shownName(place)))
        true
    } catch (error: Exception) {
        if (error is CancellationException) throw error
        problem(words.describeLoadProblem(error))
        false
    }

    /** Deletes one of the person's own visits, then reads the visits and trips again, since a trip that ended or began at the visit is rebuilt. */
    suspend fun forget(visit: PlaceVisit): Boolean {
        try {
            connection.forgetPlaceVisit(visit.id)
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            problem(refusalReason(error))
            return false
        }
        visits = visits.filter { each -> each.id != visit.id }
        refreshOwn()
        readTrips()
        done(words.forgotVisit)
        return true
    }

    /** The phone's position now, with how precise the phone says it is. */
    suspend fun currentReading(): LivePosition? {
        val live = recording.livePosition.value
        if (live != null && Instant.now().epochSecond - live.measuredAt.epochSecond < 60) return live
        val reading = recording.currentReading()
        if (reading != null) return reading
        problem(words.phoneCouldNotTell)
        return null
    }

    suspend fun nearby(latitudeDegrees: Double, longitudeDegrees: Double): List<FoundPlace> = connection.nearbyPlaces(latitudeDegrees, longitudeDegrees)

    /** Places matching words near a position, from OpenStreetMap, a page at a time. */
    suspend fun searchPlaces(wordsTyped: String, latitudeDegrees: Double, longitudeDegrees: Double, offset: Int = 0): FoundPlacePage =
        connection.searchPlaces(wordsTyped, latitudeDegrees, longitudeDegrees, offset = offset, limitCount = SEARCH_PAGE_SIZE)

    /** Addresses matching the words typed, anywhere, nearest first to the place when there is one, else to where the person last was. */
    suspend fun searchAddresses(wordsTyped: String, placeId: String? = null): List<FoundPlace> =
        connection.searchAddresses(wordsTyped, placeId = placeId, limitCount = ADDRESS_SEARCH_LIMIT)

    /** Records that the person is at a place now. Null when it worked, else the reason, for the sheet that asked to say. */
    suspend fun checkIn(target: CheckInTarget, placeName: String?): String? = try {
        connection.checkIn(target)
        done(if (placeName != null) words.checkedInAt(placeName) else words.checkedInHere)
        recording.placesDidChange()
        load()
        null
    } catch (error: Exception) {
        if (error is CancellationException) throw error
        refusalReason(error)
    }

    /** Erases the positions and visits of a range (with no range, all of them), and the places that leaves with no visit that the person did not name or label. */
    suspend fun eraseHistory(from: Instant? = null, until: Instant? = null, dayText: String? = null): Boolean {
        recording.holdReporting()
        try {
            connection.eraseLocationHistory(from, until)
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            recording.releaseReporting(from, until, false)
            problem(words.describeLoadProblem(error))
            return false
        }
        recording.releaseReporting(from, until, true)
        recording.placesDidChange()
        if (from == null && until == null) {
            location = null
            visits = emptyList()
            pauses = emptyList()
            trips = emptyList()
            places = emptyList()
            nextVisitOffset = null
            nextPlaceOffset = null
            done(words.positionsErased)
        } else if (dayText != null) {
            done(words.erasedDay(dayText))
        }
        load()
        return true
    }

    // Following people

    /** Keeps the screen current while it is visible: reads again whoever the server says moved, and everyone every thirty seconds in case a message was missed. */
    fun follow(): Job = viewModelScope.launch {
        launch { poll() }
        if (isFeedEnabled) launch { listen() }
    }

    private suspend fun poll() {
        while (true) {
            delay(POLL_INTERVAL_MS)
            refreshEveryone()
        }
    }

    private suspend fun listen() {
        var delayMS = 1000L
        while (true) {
            try {
                connection.subscribe(com.teanode.kit.location.LocationDocuments.LOCATION_EVENTS).collect { element ->
                    val event = runCatching { com.teanode.kit.TeaNodeJson.decodeFromJsonElement(LocationEvent.serializer(), (element as kotlinx.serialization.json.JsonObject)["LocationEvents"]!!) }.getOrNull()
                    if (event != null) refresh(event.userId)
                    delayMS = 1000L
                }
            } catch (error: CancellationException) {
                throw error
            } catch (_: Exception) {
                // The feed dropped; what was missed is read again below.
            }
            delay(delayMS)
            delayMS = minOf(delayMS * 2, 30_000L)
            refreshEveryone()
        }
    }

    /** Reads the person who moved again: the person's own, or somebody sharing with them. */
    suspend fun refresh(userId: String) {
        if (userId == ownUserId) {
            refreshOwn()
            return
        }
        try {
            val fresh = connection.location(userId)
            val index = people.indexOfFirst { person -> person.userId == userId }
            if (index < 0) {
                // Somebody not listed yet, such as a share just made.
                refreshPeople()
                return
            }
            people = people.toMutableList().also { list -> list[index] = fresh }
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            // They may have stopped sharing; the list says so.
            refreshPeople()
        }
    }

    /** The person's own position and newest visits, which a report from the phone changes. */
    private suspend fun refreshOwn() {
        val locationResult = runCatching { connection.location() }
        val visitsResult = runCatching { connection.locationVisits(limitCount = VISIT_PAGE_SIZE) }
        locationResult.onSuccess { read ->
            location = read
            recording.learnPause(read.pause)
        }
        visitsResult.onSuccess { page ->
            // The newest page in front of what was scrolled to, each visit once.
            val freshIds = page.placeVisits.map { visit -> visit.id }.toSet()
            if (visits.isEmpty()) nextVisitOffset = page.nextOffset
            visits = page.placeVisits + visits.filter { visit -> visit.id !in freshIds }
        }
        readPauses()
    }

    private suspend fun refreshPeople() {
        val read = runCatching { connection.sharedLocations() }.getOrNull() ?: return
        val chosenMeasuredAt = selectedPerson?.latestPoint?.measuredAt
        people = read
        val selected = selectedUserId
        if (selected != null && read.none { person -> person.userId == selected }) clearSelection()
        // The selected person moved: today's path on the map grows with them.
        val chosen = chosenPersonVisits
        val measuredAt = selectedPerson?.latestPoint?.measuredAt
        if (chosen != null && chosen.readDay != null && chosen.isShowingToday && measuredAt != null && measuredAt != chosenMeasuredAt) chosen.load()
    }

    private suspend fun refreshEveryone() {
        refreshOwn()
        refreshPeople()
        readSharingLimit()
    }

    /** Reads what limits the people the person sees. A failed read, or a server that cannot say, leaves the limit as it was. */
    private suspend fun readSharingLimit() {
        sharingLimit = runCatching { connection.locationSharingLimit() }.getOrNull() ?: return
    }

    /** The server took something that changes what the person sees: reads the people and the limit again. */
    suspend fun sharingLimitDidChange() {
        refreshPeople()
        readSharingLimit()
    }

    // The selected person

    /** Selects somebody sharing: the map centers on them. */
    fun select(userId: String) {
        if (userId == ownUserId) {
            clearSelection()
            return
        }
        selectedUserId = userId
        if (chosenPersonVisits?.userId != userId) chosenPersonVisits = PersonVisitsState(connection, userId, zone, isPathRead = true, tryAgain = words.tryAgain)
    }

    fun clearSelection() {
        selectedUserId = null
        chosenPersonVisits = null
    }

    // Sharing

    private suspend fun reloadShares() {
        try {
            shares = connection.locationShares()
            sharingProblemText = null
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            sharingProblemText = words.describeLoadProblem(error)
        }
    }

    /** Shares the person's location with a person or a circle, at a level. Null when it worked, else the reason, for the sheet that asked to say. */
    suspend fun share(target: ShareTarget, name: String, level: LocationShareLevel): String? = try {
        connection.shareLocation(target.granteeUserId, target.granteeCircleId, level)
        done(words.sharedWith(name))
        reloadShares()
        finishSetupIfDone()
        null
    } catch (error: Exception) {
        if (error is CancellationException) throw error
        refusalReason(error)
    }

    /** Changes how much of the person's location a share gives. */
    suspend fun change(share: LocationShare, level: LocationShareLevel) {
        try {
            connection.shareLocation(share.granteeUserId, share.granteeCircleId, level)
            done(words.changedWhatCanSee(words.shareName(share)))
            reloadShares()
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            problem(refusalReason(error))
        }
    }

    /** Takes a share back. */
    suspend fun stop(share: LocationShare) {
        try {
            connection.unshareLocation(share.id)
            done(words.stoppedSharingWith(words.shareName(share)))
            reloadShares()
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            problem(refusalReason(error))
        }
    }

    // Muting a person's notifications

    suspend fun muteNotifications(person: PersonLocation, choice: LocationPauseChoice) {
        try {
            val mute = connection.muteLocationNotifications(person.userId, choice.endInstant(Instant.now(), zone))
            done(mute?.let { words.mutedUntil(it) } ?: words.notificationsMuted)
            refreshPeople()
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            problem(refusalReason(error))
        }
    }

    suspend fun unmuteNotifications(person: PersonLocation) {
        try {
            connection.unmuteLocationNotifications(person.userId)
            done(words.notificationsUnmuted)
            refreshPeople()
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            problem(refusalReason(error))
        }
    }

    // Setting Location up

    /** The person's "Not now": the setup does not come back. */
    fun dismissSetup() {
        isSetupDismissed = true
        recording.dismissSetup(accountKey)
    }

    /** Ends the setup when every row that applies is done, saying so once. */
    fun finishSetupIfDone() {
        if (!isSetupKnown || isSetupDismissed || !setup.isFinished) return
        // Said when it was on the screen: nothing to say of one finished elsewhere.
        val wasOnScreen = hasLoaded && location != null && location?.latestPoint == null
        dismissSetup()
        if (wasOnScreen) done(words.locationIsSetUp)
    }

    /**
     * "This is home": labels the place the person is at now. A place of theirs that already holds the position is the one
     * labeled (a visit may have made it); otherwise `SavePlace` with the coordinates makes a new place there.
     */
    suspend fun markHome() {
        val position = currentReading() ?: return
        // The person's places are read here, up to 200 with no filter, not taken from the screen's list, which is a page narrowed by the label filter and the search words.
        val allPlaces = try {
            connection.locationPlaces(limitCount = 200).places
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            problem(words.describeLoadProblem(error))
            return
        }
        fun distanceTo(place: LocationPlace) = com.teanode.kit.location.LocationGeometry.distanceMeters(
            position.latitudeDegrees, position.longitudeDegrees, place.latitudeDegrees, place.longitudeDegrees,
        )
        val nearest = allPlaces.filter { place -> distanceTo(place) <= place.radiusMeters }.minByOrNull { place -> distanceTo(place) }
        val change = nearest?.let { place -> PlaceChange(placeId = place.id, placeLabel = PlaceLabel.Home.serverWord) }
            ?: PlaceChange(placeLabel = PlaceLabel.Home.serverWord, latitudeDegrees = position.latitudeDegrees, longitudeDegrees = position.longitudeDegrees)
        if (save(change) == null) return
        done(words.placeIsNowHome)
        load()
    }

    /** "Type my address": labels Home at the address the person chose. Null when it worked, else the reason, for the address screen. */
    suspend fun markHome(address: FoundPlace): String? {
        val result = saveAddress(
            PlaceChange(
                placeLabel = PlaceLabel.Home.serverWord, latitudeDegrees = address.latitudeDegrees, longitudeDegrees = address.longitudeDegrees,
                addressText = words.chosenAddressText(address),
            ),
        )
        if (result.first == null) return result.second
        done(words.placeIsNowHome)
        load()
        return null
    }

    /** Shares the person's location with one of their family circles from the setup, which is on the screen itself, so a refusal is said there. */
    suspend fun shareWithFamily(circle: PeopleCircle, level: LocationShareLevel) {
        val refusal = share(ShareTarget.Circle(circle.id), words.everyoneIn(circle.name), level)
        if (refusal != null) problem(refusal)
    }

    fun keptAddress(): String = words.keptAddress
    fun chosenAddress(found: FoundPlace): String = words.chosenAddressText(found)
    fun connectionProblem(error: Throwable): String = words.describeLoadProblem(error)
    fun moved(name: String): String = words.moved(name)
    fun unknownDistance(): String = words.unknownDistance
    fun phoneRough(roughness: String): String = words.phoneRough(roughness)
    fun mapWillNameAgain(): String = words.mapWillNameAgain

    /** Says something in the snackbar, such as a pause the server refused. */
    fun say(text: String, isProblem: Boolean) {
        if (isProblem) problem(text) else done(text)
    }

    companion object {
        const val VISIT_PAGE_SIZE = 50
        const val PLACE_PAGE_SIZE = 50
        const val SEARCH_PAGE_SIZE = 20

        /** How far back the pauses are read when every visit has been read. */
        const val PAUSE_LOOKBACK_SECONDS = 365L * 86400

        /** How many address matches one search lists. */
        const val ADDRESS_SEARCH_LIMIT = 8
        private const val POLL_INTERVAL_MS = 30_000L
    }
}

/**
 * The trips that began in a range, a page at a time up to twenty pages; null when one page could not be read (a server that
 * cannot list trips, or a failed read), which leaves the trips as they were.
 */
suspend fun fetchTrips(connection: Connection, userId: String?, from: Instant, until: Instant): List<Trip>? {
    val pageSize = 100
    val pageCountMost = 20
    val trips = mutableListOf<Trip>()
    var offset = 0
    repeat(pageCountMost) {
        val page = runCatching { connection.trips(userId, from, until, offset, pageSize) }.getOrNull() ?: return null
        trips += page.trips
        offset = page.nextOffset ?: return trips
    }
    return trips
}

/** The factory the screens make the model with, one for each account. */
class LocationModelFactory(
    private val connection: Connection,
    private val recording: LocationRecording,
    private val words: LocationModelWords,
    private val isFeedEnabled: Boolean,
) : ViewModelProvider.Factory {
    @Suppress("UNCHECKED_CAST")
    override fun <Model : ViewModel> create(modelClass: Class<Model>): Model = LocationModel(connection, recording, words, isFeedEnabled = isFeedEnabled) as Model
}

/** PersonVisitsState is one day of somebody sharing with the person: their visits, pauses, trips and places, and on the map their path. */
class PersonVisitsState(
    val connection: Connection,
    val userId: String,
    private val zone: ZoneId,
    val isPathRead: Boolean = false,
    private val tryAgain: String,
) {
    var day: LocalDate by mutableStateOf(LocalDate.now(zone))
        private set
    var visits: List<PlaceVisit> by mutableStateOf(emptyList())
        private set
    var pauses: List<LocationPause> by mutableStateOf(emptyList())
        private set
    var trips: List<Trip> by mutableStateOf(emptyList())
        private set
    var places: List<LocationPlace> by mutableStateOf(emptyList())
        private set
    var points: List<LocationPoint> by mutableStateOf(emptyList())
        private set

    /** The day the visits shown were read for; null until the first read. */
    var readDay: LocalDate? by mutableStateOf(null)
        private set
    var isLoading: Boolean by mutableStateOf(false)
        private set
    var problemText: String? by mutableStateOf(null)
        private set

    /** The day's path as the map draws it: the positions joined through the places visited. */
    val path: List<LocationPoint>
        get() {
            val shown = readDay ?: return emptyList()
            val window = LocationDays.window(shown, zone)
            return LocationDays.path(points, visits, window.first, window.second)
        }

    val isShowingToday: Boolean get() = day == LocalDate.now(zone)

    val entries get() = LocationDays.entries(visits, pauses, trips)

    val canMoveLater: Boolean get() = day.isBefore(LocalDate.now(zone))

    suspend fun move(days: Long) {
        day = minOf(day.plusDays(days), LocalDate.now(zone))
        load()
    }

    suspend fun load() {
        val requestedDay = day
        val window = LocationDays.window(requestedDay, zone)
        isLoading = true
        try {
            val visitPage = connection.locationVisits(userId, window.first, window.second, limitCount = 200)
            val placePage = connection.locationPlaces(userId, limitCount = LocationModel.PLACE_PAGE_SIZE)
            // A server that cannot list pauses or trips has none to show.
            val pauseList = runCatching { connection.locationPauses(userId, window.first, window.second) }.getOrDefault(emptyList())
            val tripList = fetchTrips(connection, userId, window.first, window.second) ?: emptyList()
            val pointList = if (isPathRead) connection.locationPoints(userId, window.first, window.second) else emptyList()
            // Another day was chosen while this was read.
            if (day != requestedDay) return
            visits = visitPage.placeVisits
            pauses = pauseList
            trips = tripList
            places = placePage.places
            points = pointList
            readDay = requestedDay
            problemText = null
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            if (day == requestedDay) problemText = (error as? TeaNodeError.Refused)?.reason ?: tryAgain
        } finally {
            isLoading = false
        }
    }
}
