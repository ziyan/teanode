package com.teanode.app.location

import android.view.Gravity
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.view.doOnLayout
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import com.teanode.app.R
import com.teanode.app.session.LocalAppSession
import org.maplibre.android.camera.CameraUpdateFactory
import org.maplibre.android.geometry.LatLng
import org.maplibre.android.geometry.LatLngBounds
import org.maplibre.android.maps.MapLibreMap
import org.maplibre.android.maps.MapLibreMapOptions
import org.maplibre.android.maps.MapView
import org.maplibre.android.maps.Style
import org.maplibre.android.style.layers.FillLayer
import org.maplibre.android.style.layers.LineLayer
import org.maplibre.android.style.layers.Property
import org.maplibre.android.style.layers.PropertyFactory
import org.maplibre.android.style.sources.GeoJsonSource
import kotlin.math.cos
import kotlin.math.sin

/**
 * ServerMap is the map of a signed-in phone: MapLibre drawing the style the account's server serves at [MAP_STYLE_PATH],
 * with every file asked for through [ServerMapRequests]. The radius and the path are layers of the map; the dots of
 * places, their names and the pictures of people are drawn over it where the map's projection puts them.
 */
@Composable
internal fun ServerMap(
    content: MapContent, focus: MapFocus, modifier: Modifier, onPinTap: (String) -> Unit, onMapTap: ((MapCoordinate) -> Unit)?, isInteractive: Boolean,
) {
    val connection by LocalAppSession.current.connection.collectAsState()
    val activeConnection = connection
    if (activeConnection == null) {
        Box(modifier.background(MaterialTheme.colorScheme.surfaceContainerHigh))
        return
    }
    SideEffect { ServerMapRequests.connection = activeConnection }
    val styleUrl = activeConnection.account.server.endpoint(MAP_STYLE_PATH).toString()
    val context = LocalContext.current
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    val density = LocalDensity.current.density
    val accent = MaterialTheme.colorScheme.primary
    val latestOnMapTap by rememberUpdatedState(onMapTap)
    var map by remember(styleUrl) { mutableStateOf<MapLibreMap?>(null) }
    var style by remember(styleUrl) { mutableStateOf<Style?>(null) }
    var hasFailed by remember(styleUrl) { mutableStateOf(false) }
    // Read where the pictures and dots are drawn, so they are placed again whenever the map moves.
    var cameraRevision by remember { mutableIntStateOf(0) }
    var chooserGroup by remember { mutableStateOf<List<MapPin>?>(null) }
    val mapView = remember(styleUrl) {
        ServerMapRequests.install(context)
        // A texture rather than a surface, so the map moves and clips with the screens and drawers over it.
        MapView(context, MapLibreMapOptions.createFromAttributes(context).textureMode(true)).apply { onCreate(null) }
    }

    DisposableEffect(lifecycle, mapView) {
        val observer = LifecycleEventObserver { _, event ->
            when (event) {
                Lifecycle.Event.ON_START -> mapView.onStart()
                Lifecycle.Event.ON_RESUME -> mapView.onResume()
                Lifecycle.Event.ON_PAUSE -> mapView.onPause()
                Lifecycle.Event.ON_STOP -> mapView.onStop()
                else -> Unit
            }
        }
        lifecycle.addObserver(observer)
        onDispose {
            lifecycle.removeObserver(observer)
            if (lifecycle.currentState.isAtLeast(Lifecycle.State.RESUMED)) mapView.onPause()
            if (lifecycle.currentState.isAtLeast(Lifecycle.State.STARTED)) mapView.onStop()
            mapView.onDestroy()
        }
    }

    DisposableEffect(mapView) {
        mapView.addOnDidFailLoadingMapListener { hasFailed = true }
        mapView.getMapAsync { loaded ->
            loaded.uiSettings.apply {
                setLogoEnabled(false)
                setCompassEnabled(false)
                setAllGesturesEnabled(isInteractive)
                // The credit comes from the style's own sources, so it is right for whichever map host is set.
                setAttributionGravity(Gravity.BOTTOM or Gravity.END)
            }
            loaded.setMinZoomPreference(3.0)
            loaded.addOnCameraMoveListener { cameraRevision++ }
            loaded.addOnCameraIdleListener { cameraRevision++ }
            loaded.addOnMapClickListener { point ->
                val tap = latestOnMapTap ?: return@addOnMapClickListener false
                tap(MapCoordinate(point.latitude, point.longitude))
                true
            }
            loaded.setStyle(Style.Builder().fromUri(styleUrl)) { loadedStyle ->
                addOverlayLayers(loadedStyle, accent.toArgb())
                style = loadedStyle
                cameraRevision++
            }
            map = loaded
        }
        onDispose { }
    }

    LaunchedEffect(style, content.circle) {
        style?.getSourceAs<GeoJsonSource>(SOURCE_CIRCLE)?.setGeoJson(circleGeoJson(content.circle))
    }
    LaunchedEffect(style, content.path) {
        style?.getSourceAs<GeoJsonSource>(SOURCE_PATH)?.setGeoJson(pathGeoJson(content.path))
    }
    LaunchedEffect(map, focus) {
        val loaded = map ?: return@LaunchedEffect
        val bounds = focus.bounds() ?: return@LaunchedEffect
        mapView.doOnLayout {
            loaded.moveCamera(CameraUpdateFactory.newLatLngBounds(LatLngBounds.from(bounds.north, bounds.east, bounds.south, bounds.west), (24 * density).toInt()))
            cameraRevision++
        }
    }

    Box(modifier) {
        AndroidView(factory = { mapView }, modifier = Modifier.matchParentSize())
        val loaded = map
        if (loaded != null && style != null) {
            // Made again whenever the map moves, so that everything drawn over it is placed again.
            val project: (MapCoordinate) -> Offset = remember(loaded, cameraRevision) {
                { coordinate ->
                    val point = loaded.projection.toScreenLocation(LatLng(coordinate.latitudeDegrees, coordinate.longitudeDegrees))
                    Offset(point.x, point.y)
                }
            }
            val onAccent = MaterialTheme.colorScheme.onPrimary
            Canvas(Modifier.matchParentSize()) {
                for (marker in content.markers) {
                    val point = project(marker.coordinate)
                    drawCircle(Color.White, 9.dp.toPx(), point)
                    drawCircle(accent, 7.dp.toPx(), point)
                    drawCircle(onAccent, 2.5.dp.toPx(), point)
                }
                content.pickedCoordinate?.let { picked ->
                    val point = project(picked)
                    drawCircle(Color.White, 10.dp.toPx(), point)
                    drawCircle(Color(0xFFD14B3B), 8.dp.toPx(), point)
                }
            }
            MarkerTitles(content.markers, project)
            PinGroups(content.pins, project, onPinTap, onChooseGroup = { group -> chooserGroup = group })
        }
        if (hasFailed) {
            Text(
                stringResource(R.string.location_map_failed),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurface,
                modifier = Modifier
                    .align(Alignment.Center)
                    .background(MaterialTheme.colorScheme.surface, RoundedCornerShape(8.dp))
                    .padding(horizontal = 12.dp, vertical = 8.dp),
            )
        }
    }
    chooserGroup?.let { group -> PinGroupChooser(group, onChoose = { id -> chooserGroup = null; onPinTap(id) }, onDismiss = { chooserGroup = null }) }
}

private const val SOURCE_CIRCLE = "teanode-circle"
private const val SOURCE_PATH = "teanode-path"

/** The radius of a place and the path of a day, drawn above everything the style has. */
private fun addOverlayLayers(style: Style, accent: Int) {
    style.addSource(GeoJsonSource(SOURCE_CIRCLE, EMPTY_FEATURES))
    style.addSource(GeoJsonSource(SOURCE_PATH, EMPTY_FEATURES))
    style.addLayer(FillLayer("teanode-circle-fill", SOURCE_CIRCLE).withProperties(PropertyFactory.fillColor(accent), PropertyFactory.fillOpacity(0.2f)))
    style.addLayer(LineLayer("teanode-circle-line", SOURCE_CIRCLE).withProperties(PropertyFactory.lineColor(accent), PropertyFactory.lineWidth(2f)))
    style.addLayer(
        LineLayer("teanode-path-line", SOURCE_PATH).withProperties(
            PropertyFactory.lineColor(accent),
            PropertyFactory.lineWidth(4f),
            PropertyFactory.lineCap(Property.LINE_CAP_ROUND),
            PropertyFactory.lineJoin(Property.LINE_JOIN_ROUND),
        ),
    )
}

private const val EMPTY_FEATURES = """{"type":"FeatureCollection","features":[]}"""

/** How many corners the drawn circle has: enough that it reads as round at any zoom the map allows. */
private const val CIRCLE_CORNER_COUNT = 64

private const val METERS_PER_DEGREE_OF_LATITUDE = 111_320.0

private fun circleGeoJson(circle: MapCircle?): String {
    circle ?: return EMPTY_FEATURES
    val latitudeRadius = circle.radiusMeters / METERS_PER_DEGREE_OF_LATITUDE
    val longitudeRadius = latitudeRadius / maxOf(0.01, cos(Math.toRadians(circle.center.latitudeDegrees)))
    val ring = (0..CIRCLE_CORNER_COUNT).joinToString(",") { index ->
        val angle = 2 * Math.PI * index / CIRCLE_CORNER_COUNT
        "[${circle.center.longitudeDegrees + longitudeRadius * cos(angle)},${circle.center.latitudeDegrees + latitudeRadius * sin(angle)}]"
    }
    return """{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":{"type":"Polygon","coordinates":[[$ring]]}}]}"""
}

private fun pathGeoJson(path: List<MapCoordinate>): String {
    if (path.size < 2) return EMPTY_FEATURES
    val line = path.joinToString(",") { coordinate -> "[${coordinate.longitudeDegrees},${coordinate.latitudeDegrees}]" }
    return """{"type":"FeatureCollection","features":[{"type":"Feature","properties":{},"geometry":{"type":"LineString","coordinates":[$line]}}]}"""
}
