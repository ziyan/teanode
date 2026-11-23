package com.teanode.app.location

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import com.teanode.app.R
import com.teanode.kit.location.LocationGeometry
import com.teanode.kit.location.LocationShareLevel
import com.teanode.kit.location.PersonLocation
import androidx.compose.runtime.collectAsState

/** Where to draw somebody: their latest position, else the place of the visit in progress. Nowhere while somebody else has paused: the person's own position is theirs to see. */
fun PersonLocation.mapCoordinate(): MapCoordinate? {
    if (level != LocationShareLevel.Owner && isPaused()) return null
    latestPoint?.let { point -> return MapCoordinate(point.latitudeDegrees, point.longitudeDegrees) }
    return openVisit?.let { visit -> MapCoordinate(visit.latitudeDegrees, visit.longitudeDegrees) }
}

/**
 * The map behind the drawer: the person's own position, the people sharing with them (pictures that would cover each
 * other at this zoom are listed by tapping, as Find My groups them), the places they named, and, while somebody sharing
 * their visits is chosen, the path of that person's day and the places they went.
 */
@Composable
fun LocationMapHeader(model: LocationModel, isShowingPeople: Boolean, modifier: Modifier = Modifier) {
    val recording = LocalLocationRecording.current
    val live by recording.livePosition.collectAsState()
    val text = rememberLocationText()
    val chosenDay = if (isShowingPeople && model.selectedPerson?.level == LocationShareLevel.History) model.chosenPersonVisits else null
    val ownCoordinate = model.location?.latestPoint?.let { point -> MapCoordinate(point.latitudeDegrees, point.longitudeDegrees) }
    val selected = model.selectedPerson
    val livePoint = live?.let { position -> MapCoordinate(position.latitudeDegrees, position.longitudeDegrees) }
    val shownPeople = if (isShowingPeople) model.people else emptyList()

    fun isUnderPicture(latitude: Double, longitude: Double, radius: Double): Boolean =
        (listOfNotNull(ownCoordinate) + shownPeople.mapNotNull { person -> person.mapCoordinate() }).any { coordinate ->
            LocationGeometry.distanceMeters(coordinate.latitudeDegrees, coordinate.longitudeDegrees, latitude, longitude) <= radius
        }

    val content = remember(model.places, model.people, model.location, model.selectedUserId, chosenDay?.readDay, chosenDay?.visits, chosenDay?.points, isShowingPeople) {
        val pinnedPlaces = if (chosenDay != null) emptyList() else model.places.filter { place -> place.isOfPersonsOwn && !isUnderPicture(place.latitudeDegrees, place.longitudeDegrees, place.radiusMeters) }
        val chosenDayPlaces = chosenDay?.visits.orEmpty().distinctBy { visit -> visit.place.id }
            .filter { visit -> !isUnderPicture(visit.latitudeDegrees, visit.longitudeDegrees, visit.place.radiusMeters) }
        val pins = buildList {
            val own = model.location
            if (own != null && ownCoordinate != null) add(MapPin("", ownCoordinate, own.displayName, own.avatarUrl, own.avatarColorIndex, isChosen = model.selectedUserId == null))
            shownPeople.forEach { person ->
                person.mapCoordinate()?.let { coordinate -> add(MapPin(person.userId, coordinate, person.displayName, person.avatarUrl, person.avatarColorIndex, isChosen = person.userId == model.selectedUserId)) }
            }
        }
        MapContent(
            pins = pins,
            markers = pinnedPlaces.take(40).map { place -> MapMarker(MapCoordinate(place.latitudeDegrees, place.longitudeDegrees), text.shownName(place)) } +
                chosenDayPlaces.map { visit -> MapMarker(MapCoordinate(visit.latitudeDegrees, visit.longitudeDegrees), text.shownName(visit.place)) },
            path = chosenDay?.path.orEmpty().map { point -> MapCoordinate(point.latitudeDegrees, point.longitudeDegrees) },
        )
    }
    val focus = remember(model.selectedUserId, model.ownFocusCount, ownCoordinate, selected?.latestPoint?.measuredAt, chosenDay?.readDay, livePoint == null) {
        val dayCoordinates = if (chosenDay?.readDay != null) {
            chosenDay.path.map { point -> MapCoordinate(point.latitudeDegrees, point.longitudeDegrees) } +
                chosenDay.visits.map { visit -> MapCoordinate(visit.latitudeDegrees, visit.longitudeDegrees) } +
                listOfNotNull(if (chosenDay.isShowingToday) selected?.mapCoordinate() else null)
        } else {
            emptyList()
        }
        val center = selected?.mapCoordinate() ?: livePoint ?: ownCoordinate
        val coordinates = if (dayCoordinates.isNotEmpty()) dayCoordinates else listOfNotNull(center)
        MapFocus(coordinates.ifEmpty { listOf(MapCoordinate(0.0, 0.0)) }, radiusMeters = if (dayCoordinates.isEmpty()) 1500.0 else null, revision = model.ownFocusCount, bottomRoomFraction = 0.45)
    }
    TeaNodeMap(
        content = content, focus = focus, modifier = modifier, description = stringResource(R.string.location_map_of_where_you_are),
        onPinTap = { id -> if (id.isEmpty()) model.focusOnOwn() else if (id != model.selectedUserId) model.select(id) },
    )
}
