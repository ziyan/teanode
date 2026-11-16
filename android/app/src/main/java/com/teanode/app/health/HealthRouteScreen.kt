package com.teanode.app.health

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.CircularProgressIndicator
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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.location.MapContent
import com.teanode.app.location.MapCoordinate
import com.teanode.app.location.MapFocus
import com.teanode.app.location.MapMarker
import com.teanode.app.location.TeaNodeMap
import com.teanode.app.session.LocalAppSession
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.kit.Connection
import com.teanode.kit.health.HealthRoutePoint
import com.teanode.kit.health.healthRoute
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

/** A workout's route: the path on the map with its start and finish, and the workout's date, time, distance and energy. */
@Composable
fun HealthRouteScreen(connection: Connection, request: HealthRouteRequest, onBack: () -> Unit) {
    val text = rememberHealthText()
    val session = LocalAppSession.current
    val scope = rememberCoroutineScope()
    var points by remember { mutableStateOf<List<HealthRoutePoint>>(emptyList()) }
    var isLoading by remember { mutableStateOf(true) }
    var loadFailed by remember { mutableStateOf<String?>(null) }
    suspend fun load() {
        isLoading = true
        try {
            points = connection.healthRoute(request.healthSampleId).points
            loadFailed = null
        } catch (error: Exception) {
            if (error is CancellationException) throw error
            loadFailed = session.describeLoadProblem(error)
        } finally {
            isLoading = false
        }
    }
    LaunchedEffect(request.healthSampleId) { load() }
    val coordinates = points.map { point -> MapCoordinate(point.latitudeDegrees, point.longitudeDegrees) }
    val start = stringResource(R.string.health_start)
    val finish = stringResource(R.string.health_finish)
    TeaNodeScreen(title = request.titleText, onBack = onBack) { padding ->
        Column(Modifier.padding(padding).fillMaxSize()) {
            TeaNodeMap(
                MapContent(path = coordinates, markers = listOfNotNull(coordinates.firstOrNull()?.let { first -> MapMarker(first, start) }, if (coordinates.size > 1) MapMarker(coordinates.last(), finish) else null)),
                MapFocus(coordinates.ifEmpty { listOf(MapCoordinate(0.0, 0.0)) }), Modifier.fillMaxWidth().height(320.dp), description = stringResource(R.string.health_map_of_the_route),
            )
            val failure = loadFailed
            if (failure != null) {
                ErrorState(failure, onRetry = { scope.launch { load() } })
            } else {
                Box(Modifier.fillMaxWidth()) {
                    Column(Modifier.fillMaxWidth().padding(vertical = 8.dp)) {
                        request.startedAt?.let { started -> DetailRow(stringResource(R.string.health_date), text.momentText(started)) }
                        DetailRow(stringResource(R.string.health_duration), text.durationText(request.durationSeconds))
                        request.distanceMeters?.let { meters -> DetailRow(stringResource(R.string.health_distance), text.quantityText(meters, "workout", "m")) }
                        request.energyKilocalories?.let { energy -> DetailRow(stringResource(R.string.health_energy), text.quantityText(energy, "workout", "kcal")) }
                        if (!isLoading && points.size < 2) Text(stringResource(R.string.health_no_route_recorded), Modifier.padding(16.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    if (isLoading) CircularProgressIndicator(Modifier.align(Alignment.Center))
                }
            }
        }
    }
}

@Composable
private fun DetailRow(label: String, value: String) {
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 10.dp)) {
        Text(label, Modifier.weight(1f), style = MaterialTheme.typography.bodyLarge)
        Text(value, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}
