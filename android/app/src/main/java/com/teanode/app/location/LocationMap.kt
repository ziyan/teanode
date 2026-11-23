package com.teanode.app.location

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
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalInspectionMode
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import com.teanode.app.session.LocalAppSession
import com.teanode.app.ui.components.AvatarView
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.res.stringResource
import com.teanode.app.R
import com.teanode.kit.location.LocationMapPinGrouping
import com.teanode.kit.location.LocationMapPinPoint
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
 * TeaNodeMap draws [content] on a map the person can pan and zoom. A real phone draws the map its own server serves
 * ([ServerMap]), so no map request goes anywhere else; demo mode and the screenshot tests get a drawn stand-in that
 * needs no network, since the demo's places are invented ones in open water.
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
        ServerMap(content, focus, mapModifier, onPinTap, onMapTap, isInteractive)
    }
}

/** The pin that is never a person's, so that nobody is chosen. */
internal const val NO_CHOSEN_PIN_ID = "\u0000none"

/** Lists who is in a group of pictures, to choose one, as the iPhone app's menu does. */
@Composable
internal fun PinGroupChooser(group: List<MapPin>, onChoose: (String) -> Unit, onDismiss: () -> Unit) {
    androidx.compose.material3.AlertDialog(
        onDismissRequest = onDismiss,
        confirmButton = { androidx.compose.material3.TextButton(onClick = onDismiss) { Text(stringResource(R.string.location_not_now)) } },
        title = { Text(stringResource(R.string.location_who_is_here)) },
        text = {
            Column {
                group.forEach { pin ->
                    androidx.compose.material3.TextButton(onClick = { onChoose(pin.id) }) { Text((if (pin.isChosen) "\u2713 " else "") + pin.name.ifEmpty { stringResource(R.string.location_you) }) }
                }
            }
        },
    )
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
    var chooserGroup by remember { mutableStateOf<List<MapPin>?>(null) }
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
        MarkerTitles(content.markers, ::project)
        PinGroups(content.pins, ::project, onPinTap, onChooseGroup = { group -> chooserGroup = group })
        chooserGroup?.let { group -> PinGroupChooser(group, onChoose = { id -> chooserGroup = null; onPinTap(id) }, onDismiss = { chooserGroup = null }) }
        Text(
            OPEN_STREET_MAP_CREDIT,
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.align(Alignment.BottomEnd).padding(4.dp),
        )
    }
}

/** The names of the places beside their dots, at most forty, placed by [project] in the map's pixels. */
@Composable
internal fun MarkerTitles(markers: List<MapMarker>, project: (MapCoordinate) -> Offset) {
    for (marker in markers.take(40)) {
        if (marker.title.isEmpty()) continue
        val point = project(marker.coordinate)
        Text(
            marker.title,
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurface,
            modifier = Modifier.offset { androidx.compose.ui.unit.IntOffset((point.x + 10.dp.toPx()).toInt(), (point.y - 8.dp.toPx()).toInt()) },
        )
    }
}

/**
 * The pictures of people, placed by [project] in the map's pixels. Pictures that would cover each other are drawn as one
 * group, the first in front and how many more beside them; tapping a group asks which person through [onChooseGroup].
 */
@Composable
internal fun PinGroups(pins: List<MapPin>, project: (MapCoordinate) -> Offset, onPinTap: (String) -> Unit, onChooseGroup: (List<MapPin>) -> Unit) {
    val accent = MaterialTheme.colorScheme.primary
    val density = LocalDensity.current.density
    val chosenId = pins.firstOrNull { pin -> pin.isChosen }?.id ?: NO_CHOSEN_PIN_ID
    val points = pins.map { pin -> project(pin.coordinate).let { point -> LocationMapPinPoint(pin.id, point.x / density.toDouble(), point.y / density.toDouble()) } }
    for (ids in LocationMapPinGrouping.groups(points, chosenId)) {
        val group = ids.mapNotNull { id -> pins.firstOrNull { pin -> pin.id == id } }
        val first = group.firstOrNull() ?: continue
        val point = project(first.coordinate)
        val shown = group.take(LocationMapPinGrouping.PICTURE_COUNT_MOST)
        val moreCount = group.size - shown.size
        Row(
            Modifier
                .offset { androidx.compose.ui.unit.IntOffset((point.x - 20.dp.toPx()).toInt(), (point.y - 20.dp.toPx()).toInt()) }
                .clickable(role = Role.Button) { if (group.size == 1) onPinTap(first.id) else onChooseGroup(group) },
            verticalAlignment = Alignment.CenterVertically,
        ) {
            // The first picture is in front: they are drawn last to first, each overlapping the one before.
            Box {
                for (index in shown.indices.reversed()) {
                    val pin = shown[index]
                    Box(
                        Modifier.offset(x = (18 * index).dp).size(40.dp).clip(CircleShape).background(if (pin.isChosen) accent else Color.White),
                        contentAlignment = Alignment.Center,
                    ) { AvatarView(pin.name, pin.avatarUrl, pin.avatarColorIndex, size = 34.dp) }
                }
                Spacer(Modifier.size(width = (40 + 18 * (shown.size - 1)).dp, height = 40.dp))
            }
            if (moreCount > 0) Text("+$moreCount", style = MaterialTheme.typography.labelMedium, modifier = Modifier.padding(start = 4.dp).background(MaterialTheme.colorScheme.surface, CircleShape).padding(horizontal = 8.dp, vertical = 4.dp))
        }
    }
}
