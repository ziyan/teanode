package com.teanode.app.location

import android.content.Context
import android.graphics.Bitmap
import android.graphics.Canvas as AndroidCanvas
import android.graphics.Paint
import android.graphics.drawable.BitmapDrawable
import android.view.MotionEvent
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalInspectionMode
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import com.teanode.app.session.LocalAppSession
import com.teanode.app.ui.components.AvatarView
import com.teanode.app.ui.theme.AvatarPalette
import com.teanode.kit.AvatarColor
import com.teanode.kit.Monogram
import org.osmdroid.config.Configuration
import org.osmdroid.events.MapEventsReceiver
import org.osmdroid.tileprovider.tilesource.TileSourceFactory
import org.osmdroid.util.BoundingBox
import org.osmdroid.util.GeoPoint
import org.osmdroid.views.MapView
import org.osmdroid.views.overlay.MapEventsOverlay
import org.osmdroid.views.overlay.Marker
import org.osmdroid.views.overlay.Polygon
import org.osmdroid.views.overlay.Polyline
import kotlin.math.abs
import kotlin.math.cos
import kotlin.math.max
import kotlin.math.min

/** A coordinate on the map. */
data class MapCoordinate(val latitudeDegrees: Double, val longitudeDegrees: Double)

/** A person's picture on the map: the person's own, or somebody sharing with them. */
data class MapPin(
    val id: String,
    val coordinate: MapCoordinate,
    val name: String,
    val avatarUrl: String?,
    val avatarColorIndex: Int?,
    val isChosen: Boolean,
)

/** A named spot on the map (a place, a start, a finish). */
data class MapMarker(val coordinate: MapCoordinate, val title: String)

/** A circle on the map, such as the radius of a place. */
data class MapCircle(val center: MapCoordinate, val radiusMeters: Double)

/** What the map draws. */
data class MapContent(
    val pins: List<MapPin> = emptyList(),
    val markers: List<MapMarker> = emptyList(),
    val path: List<MapCoordinate> = emptyList(),
    val circle: MapCircle? = null,
    /** A position the person picked on the map that is not saved yet. */
    val pickedCoordinate: MapCoordinate? = null,
)

/** Where the map looks: the box that holds these coordinates with room around them, never tighter than a few hundred meters. */
data class MapFocus(val coordinates: List<MapCoordinate>, val radiusMeters: Double? = null, val revision: Int = 0, val bottomRoomFraction: Double = 0.0) {
    fun bounds(): MapBounds? {
        if (coordinates.isEmpty()) return null
        var south = coordinates.first().latitudeDegrees
        var north = south
        var west = coordinates.first().longitudeDegrees
        var east = west
        for (coordinate in coordinates) {
            south = min(south, coordinate.latitudeDegrees)
            north = max(north, coordinate.latitudeDegrees)
            west = min(west, coordinate.longitudeDegrees)
            east = max(east, coordinate.longitudeDegrees)
        }
        val minimumLatitudeSpan = (radiusMeters?.let { meters -> meters * 3.2 / 111_320.0 }) ?: MINIMUM_SPAN_DEGREES
        val latitudeSpan = max((north - south) * 1.4, max(minimumLatitudeSpan, MINIMUM_SPAN_DEGREES))
        val centerLatitude = (south + north) / 2
        val longitudeScale = max(0.2, cos(Math.toRadians(centerLatitude)))
        val longitudeSpan = max((east - west) * 1.4, max(latitudeSpan / longitudeScale * 0.6, MINIMUM_SPAN_DEGREES))
        // Room under the coordinates for what covers the bottom of the map (the drawer), so they stay in the part left showing.
        val bottomRoom = latitudeSpan * bottomRoomFraction
        return MapBounds(centerLatitude - latitudeSpan / 2 - bottomRoom, centerLatitude + latitudeSpan / 2, (west + east) / 2 - longitudeSpan / 2, (west + east) / 2 + longitudeSpan / 2)
    }

    companion object {
        const val MINIMUM_SPAN_DEGREES = 0.006
    }
}

data class MapBounds(val south: Double, val north: Double, val west: Double, val east: Double)

/** The credit every map built on OpenStreetMap carries. */
const val OPEN_STREET_MAP_CREDIT = "© OpenStreetMap contributors"

/**
 * TeaNodeMap draws [content] on a map the person can pan and zoom. A real phone gets OpenStreetMap tiles (osmdroid, no
 * key); demo mode and the screenshot tests get a drawn stand-in that needs no network, since the demo's places are
 * invented ones in open water.
 */
@Composable
fun TeaNodeMap(
    content: MapContent,
    focus: MapFocus,
    modifier: Modifier = Modifier,
    description: String = "",
    onPinTap: (String) -> Unit = {},
    onMapTap: ((MapCoordinate) -> Unit)? = null,
    isInteractive: Boolean = true,
) {
    val isDemo = LocalInspectionMode.current || runCatching { LocalAppSession.current.isDemo }.getOrDefault(false)
    val mapModifier = modifier.semantics { if (description.isNotEmpty()) contentDescription = description }
    if (isDemo) {
        SchematicMap(content, focus, mapModifier, onPinTap, onMapTap)
    } else {
        OpenStreetMap(content, focus, mapModifier, onPinTap, onMapTap, isInteractive)
    }
}

@Composable
private fun OpenStreetMap(
    content: MapContent, focus: MapFocus, modifier: Modifier, onPinTap: (String) -> Unit, onMapTap: ((MapCoordinate) -> Unit)?, isInteractive: Boolean,
) {
    val context = LocalContext.current
    val density = LocalDensity.current
    val latestOnPinTap by rememberUpdatedState(onPinTap)
    val latestOnMapTap by rememberUpdatedState(onMapTap)
    val isDark = MaterialTheme.colorScheme.background.let { color -> (0.299f * color.red + 0.587f * color.green + 0.114f * color.blue) < 0.5f }
    val accent = MaterialTheme.colorScheme.primary
    val mapView = remember {
        Configuration.getInstance().apply {
            userAgentValue = context.packageName
            osmdroidBasePath = context.cacheDir
            osmdroidTileCache = context.cacheDir.resolve("osmdroid-tiles")
        }
        MapView(context).apply {
            setTileSource(TileSourceFactory.MAPNIK)
            setMultiTouchControls(isInteractive)
            zoomController.setVisibility(org.osmdroid.views.CustomZoomButtonsController.Visibility.NEVER)
            minZoomLevel = 3.0
            controller.setZoom(15.0)
        }
    }
    DisposableEffect(mapView) {
        mapView.onResume()
        onDispose { mapView.onPause(); mapView.onDetach() }
    }
    LaunchedEffect(focus) {
        val bounds = focus.bounds() ?: return@LaunchedEffect
        mapView.post { mapView.zoomToBoundingBox(BoundingBox(bounds.north, bounds.east, bounds.south, bounds.west), false, 24) }
    }
    AndroidView(
        modifier = modifier,
        factory = { mapView },
        update = { view ->
            view.overlays.clear()
            view.overlays.add(
                MapEventsOverlay(
                    object : MapEventsReceiver {
                        override fun singleTapConfirmedHelper(point: GeoPoint): Boolean {
                            val tap = latestOnMapTap ?: return false
                            tap(MapCoordinate(point.latitude, point.longitude))
                            return true
                        }

                        override fun longPressHelper(point: GeoPoint): Boolean = false
                    },
                ),
            )
            content.circle?.let { circle ->
                view.overlays.add(
                    Polygon().apply {
                        points = Polygon.pointsAsCircle(GeoPoint(circle.center.latitudeDegrees, circle.center.longitudeDegrees), circle.radiusMeters)
                        fillPaint.color = accent.copy(alpha = 0.2f).toArgb()
                        outlinePaint.color = accent.toArgb()
                        outlinePaint.strokeWidth = 2f * density.density
                    },
                )
            }
            if (content.path.size > 1) {
                view.overlays.add(
                    Polyline().apply {
                        setPoints(content.path.map { coordinate -> GeoPoint(coordinate.latitudeDegrees, coordinate.longitudeDegrees) })
                        outlinePaint.color = accent.toArgb()
                        outlinePaint.strokeWidth = 4f * density.density
                    },
                )
            }
            for (marker in content.markers) {
                view.overlays.add(
                    Marker(view).apply {
                        position = GeoPoint(marker.coordinate.latitudeDegrees, marker.coordinate.longitudeDegrees)
                        title = marker.title
                        setAnchor(Marker.ANCHOR_CENTER, Marker.ANCHOR_BOTTOM)
                    },
                )
            }
            content.pickedCoordinate?.let { picked ->
                view.overlays.add(Marker(view).apply { position = GeoPoint(picked.latitudeDegrees, picked.longitudeDegrees); setAnchor(Marker.ANCHOR_CENTER, Marker.ANCHOR_BOTTOM) })
            }
            for (pin in content.pins) {
                view.overlays.add(
                    Marker(view).apply {
                        position = GeoPoint(pin.coordinate.latitudeDegrees, pin.coordinate.longitudeDegrees)
                        title = pin.name
                        icon = BitmapDrawable(view.resources, monogramBitmap(context, pin, isDark, density.density, accent.toArgb()))
                        setAnchor(Marker.ANCHOR_CENTER, Marker.ANCHOR_CENTER)
                        setOnMarkerClickListener { _, _ -> latestOnPinTap(pin.id); true }
                    },
                )
            }
            view.invalidate()
        },
    )
}

/** A person's picture on the real map: their first letter on the color the server chose, ringed in the accent when chosen. */
private fun monogramBitmap(@Suppress("UNUSED_PARAMETER") context: Context, pin: MapPin, isDark: Boolean, scale: Float, accent: Int): Bitmap {
    val diameter = (36 * scale).toInt()
    val bitmap = Bitmap.createBitmap(diameter, diameter, Bitmap.Config.ARGB_8888)
    val canvas = AndroidCanvas(bitmap)
    val paint = Paint(Paint.ANTI_ALIAS_FLAG)
    paint.color = (if (pin.isChosen) accent else android.graphics.Color.WHITE)
    canvas.drawCircle(diameter / 2f, diameter / 2f, diameter / 2f, paint)
    paint.color = AvatarPalette.color(AvatarColor.index(pin.avatarColorIndex, pin.name), isDark).toArgb()
    canvas.drawCircle(diameter / 2f, diameter / 2f, diameter / 2f - 3 * scale, paint)
    paint.color = android.graphics.Color.WHITE
    paint.textSize = diameter * 0.46f
    paint.textAlign = Paint.Align.CENTER
    canvas.drawText(Monogram.letter(pin.name), diameter / 2f, diameter / 2f - (paint.descent() + paint.ascent()) / 2, paint)
    return bitmap
}

/** The drawn stand-in: a plain ground with a grid, the path, the places, and the pictures of people, projected to fit the focus. */
@Composable
private fun SchematicMap(content: MapContent, focus: MapFocus, modifier: Modifier, onPinTap: (String) -> Unit, onMapTap: ((MapCoordinate) -> Unit)?) {
    val ground = MaterialTheme.colorScheme.surfaceContainerHigh
    val water = MaterialTheme.colorScheme.secondaryContainer
    val grid = MaterialTheme.colorScheme.outlineVariant
    val accent = MaterialTheme.colorScheme.primary
    val onAccent = MaterialTheme.colorScheme.onPrimary
    val bounds = focus.bounds() ?: MapBounds(-0.005, 0.005, -0.005, 0.005)
    BoxWithConstraints(modifier.background(ground)) {
        val widthPixels = with(LocalDensity.current) { maxWidth.toPx() }
        val heightPixels = with(LocalDensity.current) { maxHeight.toPx() }
        // One scale for both axes, so a circle stays round; the box is centered in the space.
        val longitudeScale = max(0.2, cos(Math.toRadians((bounds.south + bounds.north) / 2)))
        val spanX = (bounds.east - bounds.west) * longitudeScale
        val spanY = bounds.north - bounds.south
        val pixelsPerDegree = min(widthPixels / spanX, heightPixels / spanY).toFloat()
        val centerLatitude = (bounds.south + bounds.north) / 2
        val centerLongitude = (bounds.west + bounds.east) / 2
        fun project(coordinate: MapCoordinate): Offset = Offset(
            (widthPixels / 2 + (coordinate.longitudeDegrees - centerLongitude) * longitudeScale * pixelsPerDegree).toFloat(),
            (heightPixels / 2 - (coordinate.latitudeDegrees - centerLatitude) * pixelsPerDegree).toFloat(),
        )
        val tapModifier = if (onMapTap == null) Modifier else Modifier.pointerInput(bounds) {
            detectTapGestures { tap ->
                onMapTap(
                    MapCoordinate(
                        centerLatitude - (tap.y - heightPixels / 2) / pixelsPerDegree,
                        centerLongitude + (tap.x - widthPixels / 2) / (longitudeScale * pixelsPerDegree),
                    ),
                )
            }
        }
        Canvas(Modifier.matchParentSize().then(tapModifier)) {
            // A bend of water and a street grid, so the stand-in reads as a map.
            drawRect(water, topLeft = Offset(0f, size.height * 0.72f), size = Size(size.width, size.height * 0.28f))
            val step = 64.dp.toPx()
            var x = 0f
            while (x < size.width) {
                drawLine(grid, Offset(x, 0f), Offset(x, size.height), strokeWidth = 1.dp.toPx())
                x += step
            }
            var y = 0f
            while (y < size.height) {
                drawLine(grid, Offset(0f, y), Offset(size.width, y), strokeWidth = 1.dp.toPx())
                y += step
            }
            content.circle?.let { circle ->
                val center = project(circle.center)
                val radius = (circle.radiusMeters / 111_320.0 * pixelsPerDegree).toFloat()
                drawCircle(accent.copy(alpha = 0.2f), radius, center)
                drawCircle(accent, radius, center, style = Stroke(width = 2.dp.toPx()))
            }
            if (content.path.size > 1) {
                val path = Path()
                content.path.forEachIndexed { index, coordinate ->
                    val point = project(coordinate)
                    if (index == 0) path.moveTo(point.x, point.y) else path.lineTo(point.x, point.y)
                }
                drawPath(path, accent, style = Stroke(width = 4.dp.toPx(), cap = StrokeCap.Round))
            }
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
        for (marker in content.markers.take(40)) {
            val point = project(marker.coordinate)
            if (marker.title.isNotEmpty()) {
                Text(
                    marker.title,
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurface,
                    modifier = Modifier.offset { androidx.compose.ui.unit.IntOffset((point.x + 10.dp.toPx()).toInt(), (point.y - 8.dp.toPx()).toInt()) },
                )
            }
        }
        for (pin in content.pins) {
            val point = project(pin.coordinate)
            val ring = if (pin.isChosen) accent else Color.White
            Box(
                Modifier
                    .offset { androidx.compose.ui.unit.IntOffset((point.x - 20.dp.toPx()).toInt(), (point.y - 20.dp.toPx()).toInt()) }
                    .size(40.dp)
                    .clip(CircleShape)
                    .background(ring)
                    .clickable { onPinTap(pin.id) },
                contentAlignment = Alignment.Center,
            ) {
                AvatarView(pin.name, pin.avatarUrl, pin.avatarColorIndex, size = 34.dp)
            }
        }
        Text(
            OPEN_STREET_MAP_CREDIT,
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.align(Alignment.BottomEnd).padding(4.dp),
        )
    }
}


