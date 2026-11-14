package com.teanode.app.contacts

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.location.LocationPersonVisitsList
import com.teanode.app.location.LocationRoutes
import com.teanode.app.location.MapContent
import com.teanode.app.location.MapCoordinate
import com.teanode.app.location.MapFocus
import com.teanode.app.location.MapMarker
import com.teanode.app.location.PersonVisitsState
import com.teanode.app.location.TeaNodeMap
import com.teanode.app.location.rememberLocationText
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.TeaNodeScreen
import java.time.ZoneId

/**
 * The page the card's Location section opens for somebody who shares where they have been: the day's path on a map and
 * their visits, pauses and trips for the day (the Location part's own list), with a way back and forth by the day, and
 * their places, to read and not to change.
 */
@Composable
fun PersonVisitsScreen(userId: String, onBack: () -> Unit, state: PersonVisitsState? = null) {
    val connection = LocalConnection.current
    val navigator = LocalAppNavigator.current
    val text = rememberLocationText()
    val tryAgain = LocalContext.current.getString(R.string.contacts_could_not_be_loaded)
    val shown = state ?: remember(connection, userId) { PersonVisitsState(connection, userId, ZoneId.systemDefault(), isPathRead = true, tryAgain = tryAgain) }
    LaunchedEffect(shown) { if (shown.readDay == null) shown.load() }
    TeaNodeScreen(title = stringResource(R.string.contacts_visits_and_places), onBack = onBack) { contentPadding ->
        Column(Modifier.padding(contentPadding).fillMaxSize()) {
            val coordinates = shown.path.map { point -> MapCoordinate(point.latitudeDegrees, point.longitudeDegrees) } +
                shown.visits.map { visit -> MapCoordinate(visit.latitudeDegrees, visit.longitudeDegrees) }
            val content = MapContent(
                markers = shown.visits.distinctBy { visit -> visit.place.id }.map { visit -> MapMarker(MapCoordinate(visit.latitudeDegrees, visit.longitudeDegrees), text.shownName(visit.place)) },
                path = shown.path.map { point -> MapCoordinate(point.latitudeDegrees, point.longitudeDegrees) },
            )
            TeaNodeMap(
                content = content, focus = MapFocus(coordinates.ifEmpty { listOf(MapCoordinate(0.0, 0.0)) }),
                modifier = Modifier.fillMaxWidth().height(220.dp), description = stringResource(R.string.contacts_map_of_their_day),
            )
            LocationPersonVisitsList(shown, onOpenPlace = { place -> navigator.navigate(LocationRoutes.place(place.id)) })
        }
    }
}
