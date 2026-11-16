package com.teanode.app.photos

import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.selected
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.location.MapContent
import com.teanode.app.location.MapCoordinate
import com.teanode.app.location.MapFocus
import com.teanode.app.location.MapMarker
import com.teanode.app.location.TeaNodeMap
import com.teanode.app.ui.components.rememberServerPicture
import com.teanode.kit.photos.Photo

private val thumbnailSide = 44.dp
private val currentThumbnailSide = 56.dp

/**
 * The row of small squares under the picture (ios PhotoThumbnailStrip): the photos around the one on screen, which is a
 * little larger. A tap shows that photo, and the strip keeps the one on screen in view as the pager moves.
 */
@Composable
fun PhotoThumbnailStrip(photos: List<Photo>, currentIndex: Int, onChoose: (Int) -> Unit, modifier: Modifier = Modifier) {
    val state = rememberLazyListState()
    LaunchedEffect(currentIndex) {
        // The photo on screen goes in the middle: scrolling to an item puts it at the start, so back off by what fits before it.
        val visibleCount = state.layoutInfo.visibleItemsInfo.size
        state.animateScrollToItem((currentIndex - visibleCount / 2).coerceAtLeast(0))
    }
    val description = stringResource(R.string.photos_thumbnail_strip)
    LazyRow(
        state = state, modifier = modifier.fillMaxWidth().height(currentThumbnailSide + 16.dp).semantics { contentDescription = description },
        contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp), horizontalArrangement = Arrangement.spacedBy(2.dp), verticalAlignment = Alignment.CenterVertically,
    ) {
        itemsIndexed(photos, key = { _, photo -> photo.id }) { index, photo ->
            val isCurrent = index == currentIndex
            val side = if (isCurrent) currentThumbnailSide else thumbnailSide
            val picture = rememberServerPicture(photo.gridPreviewPath)
            val label = photoDescription(photo)
            Box(
                Modifier.size(side).clip(RoundedCornerShape(3.dp)).background(Color.White.copy(alpha = 0.12f))
                    .semantics { contentDescription = label; selected = isCurrent }
                    .clickable(role = Role.Button) { onChoose(index) },
            ) {
                if (picture != null) Image(picture, contentDescription = null, contentScale = ContentScale.Crop, modifier = Modifier.size(side))
            }
        }
    }
}

/**
 * A small map with one pin where a photo was taken (ios PhotoPositionMap). It does not take the scroll of the details
 * it is in; pressing it opens the position in the maps app.
 */
@Composable
fun PhotoPositionMap(latitudeDegrees: Double, longitudeDegrees: Double, title: String, modifier: Modifier = Modifier) {
    val context = LocalContext.current
    val coordinate = MapCoordinate(latitudeDegrees, longitudeDegrees)
    val description = stringResource(R.string.photos_position_map)
    val hint = stringResource(R.string.photos_opens_in_maps)
    Box(
        modifier.fillMaxWidth().height(180.dp).clip(RoundedCornerShape(12.dp)).border(1.dp, MaterialTheme.colorScheme.outlineVariant, RoundedCornerShape(12.dp))
            .semantics { contentDescription = description },
    ) {
        TeaNodeMap(
            content = MapContent(markers = listOf(MapMarker(coordinate, title))), focus = MapFocus(listOf(coordinate)),
            modifier = Modifier.fillMaxWidth().height(180.dp), isInteractive = false,
        )
        // The map underneath never takes touches, so a press anywhere opens the maps app.
        Box(
            Modifier.fillMaxWidth().height(180.dp).clickable(role = Role.Button, onClickLabel = hint) {
                val address = Uri.parse("geo:$latitudeDegrees,$longitudeDegrees?q=$latitudeDegrees,$longitudeDegrees(${Uri.encode(title)})")
                runCatching { context.startActivity(Intent(Intent.ACTION_VIEW, address).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) }
            },
        )
    }
}
