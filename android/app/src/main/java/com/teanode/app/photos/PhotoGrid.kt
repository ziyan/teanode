package com.teanode.app.photos

import androidx.compose.foundation.Image
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyGridState
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.outlined.BrokenImage
import androidx.compose.material.icons.outlined.Photo
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.ui.components.rememberServerPicture
import com.teanode.kit.photos.Photo
import com.teanode.kit.photos.PhotoDay
import com.teanode.kit.photos.PhotoMonth
import com.teanode.kit.photos.PhotoOverview
import com.teanode.kit.photos.PhotoTimeline
import com.teanode.kit.photos.PhotoTrashDay
import com.teanode.kit.photos.PhotoYear
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import java.util.Locale

/** The three scales the library is looked at in: a card a year, a card a month, or every day's photos. */
enum class PhotoScale { Years, Months, All }

/** What a grid is made of: photos in days, a day a header, or the Trash in days with their time left. */
sealed class PhotoGridContent {
    data class Days(val days: List<PhotoDay>) : PhotoGridContent()

    data class TrashDays(val days: List<PhotoTrashDay>, val retentionDays: Int) : PhotoGridContent()
}

/** One cell: the picture, with its video length, its heart, and its mark when it is chosen. */
@OptIn(ExperimentalFoundationApi::class)
@Composable
fun PhotoCell(
    photo: Photo,
    isSelecting: Boolean,
    isSelected: Boolean,
    onClick: () -> Unit,
    onLongClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val picture = rememberServerPicture(photo.gridPreviewPath)
    val description = photoDescription(photo)
    Box(
        modifier.aspectRatio(1f).background(MaterialTheme.colorScheme.surfaceVariant)
            .semantics { contentDescription = description }
            .combinedClickable(onClick = onClick, onLongClick = onLongClick),
        contentAlignment = Alignment.Center,
    ) {
        when {
            picture != null -> Image(picture, contentDescription = null, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
            photo.isFailed -> Icon(Icons.Outlined.BrokenImage, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
            photo.isWaiting || photo.gridPreviewPath == null -> CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
            else -> Icon(Icons.Outlined.Photo, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        if (photo.isVideo || photo.isLivePhoto) {
            Row(
                Modifier.align(Alignment.BottomStart).padding(4.dp).clip(RoundedCornerShape(50)).background(Color.Black.copy(alpha = 0.55f)).padding(horizontal = 6.dp, vertical = 2.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Icon(Icons.Filled.PlayArrow, contentDescription = null, tint = Color.White, modifier = Modifier.size(12.dp))
                if (photo.isVideo) Text(PhotoTimeline.durationText(photo.durationMS), color = Color.White, style = MaterialTheme.typography.labelSmall)
            }
        }
        if (photo.isFavorite) {
            Icon(Icons.Filled.Favorite, contentDescription = null, tint = Color.White, modifier = Modifier.align(Alignment.BottomEnd).padding(5.dp).size(14.dp))
        }
        if (isSelecting) {
            if (isSelected) Box(Modifier.fillMaxSize().background(MaterialTheme.colorScheme.primary.copy(alpha = 0.25f)))
            Box(
                Modifier.align(Alignment.TopEnd).padding(6.dp).size(22.dp).clip(CircleShape)
                    .background(if (isSelected) MaterialTheme.colorScheme.primary else Color.Black.copy(alpha = 0.25f))
                    .border(1.5.dp, Color.White, CircleShape),
                contentAlignment = Alignment.Center,
            ) {
                if (isSelected) Icon(Icons.Filled.Check, contentDescription = null, tint = MaterialTheme.colorScheme.onPrimary, modifier = Modifier.size(14.dp))
            }
        }
    }
}

@Composable
fun photoDescription(photo: Photo): String {
    val kind = when {
        photo.isVideo -> stringResource(R.string.photos_kind_video)
        photo.isLivePhoto -> stringResource(R.string.photos_kind_live_photo)
        else -> stringResource(R.string.photos_kind_photo)
    }
    val day = PhotoOverview.viewerDayText(photo)
    return listOfNotNull(kind, day, photo.placeName.takeIf { name -> name.isNotEmpty() }).joinToString(", ")
}

/** The day a run of photos belongs to, written the way the phone writes dates. */
@Composable
fun dayHeadingText(day: PhotoDay): String {
    val noon = day.noonUtc ?: return stringResource(R.string.photos_unknown_date)
    val date = noon.atZone(java.time.ZoneOffset.UTC).toLocalDate()
    return DateTimeFormatter.ofLocalizedDate(FormatStyle.FULL).withLocale(Locale.getDefault()).format(date)
}

/** The photos of a list in a grid of days (or the Trash in days with their time left), a page loaded as it is scrolled to. */
@Composable
fun PhotoGrid(
    content: PhotoGridContent,
    selectedIds: Set<String>,
    isSelecting: Boolean,
    onOpen: (Photo) -> Unit,
    onToggleSelected: (Photo) -> Unit,
    onShowed: (Photo) -> Unit,
    modifier: Modifier = Modifier,
    state: LazyGridState = rememberLazyGridState(),
    contentPadding: PaddingValues = PaddingValues(0.dp),
    footer: @Composable () -> Unit = {},
) {
    LazyVerticalGrid(GridCells.Adaptive(minSize = 96.dp), modifier = modifier.fillMaxSize(), state = state, contentPadding = contentPadding) {
        when (content) {
            is PhotoGridContent.Days -> for (day in content.days) {
                if (day.id != "ranked") item(key = "day-${day.id}", span = { GridItemSpan(maxLineSpan) }) { DayHeader(dayHeadingText(day)) }
                items(day.photos, key = { photo -> photo.id }) { photo -> Cell(photo, isSelecting, selectedIds, onOpen, onToggleSelected, onShowed) }
            }
            is PhotoGridContent.TrashDays -> for (day in content.days) {
                item(key = "trash-${day.id}", span = { GridItemSpan(maxLineSpan) }) { TrashDayHeader(day) }
                items(day.photos, key = { photo -> photo.id }) { photo -> Cell(photo, isSelecting, selectedIds, onOpen, onToggleSelected, onShowed) }
            }
        }
        item(key = "footer", span = { GridItemSpan(maxLineSpan) }) { footer() }
    }
}

@Composable
private fun Cell(
    photo: Photo,
    isSelecting: Boolean,
    selectedIds: Set<String>,
    onOpen: (Photo) -> Unit,
    onToggleSelected: (Photo) -> Unit,
    onShowed: (Photo) -> Unit,
) {
    LaunchedEffect(photo.id) { onShowed(photo) }
    PhotoCell(
        photo, isSelecting, photo.id in selectedIds,
        onClick = { if (isSelecting) onToggleSelected(photo) else onOpen(photo) },
        onLongClick = { onToggleSelected(photo) },
        modifier = Modifier.padding(1.dp),
    )
}

@Composable
private fun DayHeader(text: String) {
    Text(text, style = MaterialTheme.typography.titleSmall, modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp))
}

@Composable
private fun TrashDayHeader(day: PhotoTrashDay) {
    val trashedDay = day.trashedAt?.let { instant ->
        DateTimeFormatter.ofLocalizedDate(FormatStyle.LONG).withLocale(Locale.getDefault()).format(instant.atZone(java.time.ZoneId.systemDefault()).toLocalDate())
    }
    androidx.compose.foundation.layout.Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 12.dp)) {
        Text(
            if (trashedDay != null) stringResource(R.string.photos_trash_day_heading, trashedDay) else stringResource(R.string.photos_unknown_date),
            style = MaterialTheme.typography.titleSmall,
        )
        day.daysLeft?.let { daysLeft ->
            Text(
                androidx.compose.ui.res.pluralStringResource(R.plurals.photos_trash_days_left, daysLeft, daysLeft),
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

/** The Years or Months view: a card a year or month, with its cover, its name and how many photos it holds. */
@Composable
fun PhotoOverviewGrid(
    years: List<PhotoYear>?,
    months: List<PhotoMonth>?,
    onOpenYear: (PhotoYear) -> Unit,
    onOpenMonth: (PhotoMonth) -> Unit,
    modifier: Modifier = Modifier,
    contentPadding: PaddingValues = PaddingValues(8.dp),
) {
    LazyVerticalGrid(GridCells.Fixed(2), modifier = modifier.fillMaxSize(), contentPadding = contentPadding) {
        years?.let { list ->
            items(list, key = { year -> year.id }) { year ->
                OverviewCard(year.coverPhoto, year.titleText() ?: stringResource(R.string.photos_unknown_date), year.photos.size, Modifier.padding(4.dp)) { onOpenYear(year) }
            }
        }
        months?.let { list ->
            items(list, key = { month -> month.id }) { month ->
                OverviewCard(month.coverPhoto, month.titleText() ?: stringResource(R.string.photos_unknown_date), month.photos.size, Modifier.padding(4.dp)) { onOpenMonth(month) }
            }
        }
    }
}

@Composable
private fun OverviewCard(cover: Photo?, title: String, photoCount: Int, modifier: Modifier, onClick: () -> Unit) {
    val picture = rememberServerPicture(cover?.gridPreviewPath)
    Box(modifier.aspectRatio(1f).clip(RoundedCornerShape(16.dp)).background(MaterialTheme.colorScheme.surfaceVariant).combinedClickable(onClick = onClick)) {
        if (picture != null) Image(picture, contentDescription = null, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
        Box(Modifier.fillMaxSize().background(androidx.compose.ui.graphics.Brush.verticalGradient(listOf(Color.Transparent, Color.Black.copy(alpha = 0.6f)))))
        androidx.compose.foundation.layout.Column(Modifier.align(Alignment.BottomStart).padding(12.dp)) {
            Text(title, color = Color.White, style = MaterialTheme.typography.titleMedium, maxLines = 2)
            Text(
                androidx.compose.ui.res.pluralStringResource(R.plurals.photos_item_count, photoCount, photoCount), color = Color.White.copy(alpha = 0.85f),
                style = MaterialTheme.typography.bodySmall,
            )
        }
    }
}
