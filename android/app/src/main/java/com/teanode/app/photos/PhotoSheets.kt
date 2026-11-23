package com.teanode.app.photos

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.LocationOn
import androidx.compose.material.icons.outlined.PhotoAlbum
import androidx.compose.material3.Button
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TimePicker
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.material3.rememberTimePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.photos.Photo
import com.teanode.kit.photos.PhotoAlbum
import com.teanode.kit.photos.PhotoDateChange
import com.teanode.kit.photos.PhotoFilter
import com.teanode.kit.photos.PhotoKindFilter
import com.teanode.kit.photos.PhotoPlace
import com.teanode.kit.photos.PhotoTimeline
import com.teanode.kit.photos.addPhotos
import com.teanode.kit.photos.changePhotoDate
import com.teanode.kit.photos.createPhotoAlbum
import com.teanode.kit.photos.photoAlbums
import com.teanode.kit.photos.removePhotoLocation
import com.teanode.kit.photos.searchPhotoPlaces
import com.teanode.kit.photos.setPhotoLocation
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import java.time.Instant
import java.time.LocalDate
import java.time.LocalTime
import java.time.ZoneId
import java.time.ZoneOffset

/** Narrows the library by kind and by the first and last day (counted on the phone's clock). */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PhotoFilterSheet(filter: PhotoFilter, onApply: (PhotoFilter) -> Unit, onDismiss: () -> Unit) {
    var draft by remember { mutableStateOf(filter) }
    var isFirstPicking by remember { mutableStateOf(false) }
    var isLastPicking by remember { mutableStateOf(false) }
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.padding(horizontal = 24.dp).padding(bottom = 32.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            Text(stringResource(R.string.photos_filter), style = MaterialTheme.typography.titleLarge)
            Text(stringResource(R.string.photos_filter_kind), style = MaterialTheme.typography.titleSmall)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                for (kind in PhotoKindFilter.entries) {
                    FilterChip(selected = draft.photoKindFilter == kind, onClick = { draft = draft.copy(photoKindFilter = kind) }, label = { Text(kindFilterName(kind)) })
                }
            }
            Text(stringResource(R.string.photos_filter_dates), style = MaterialTheme.typography.titleSmall)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedButton(onClick = { isFirstPicking = true }) { Text(draft.firstDay?.toString() ?: stringResource(R.string.photos_filter_first_day)) }
                OutlinedButton(onClick = { isLastPicking = true }) { Text(draft.lastDay?.toString() ?: stringResource(R.string.photos_filter_last_day)) }
                if (draft.firstDay != null || draft.lastDay != null) TextButton(onClick = { draft = draft.copy(firstDay = null, lastDay = null) }) { Text(stringResource(R.string.photos_clear_dates)) }
            }
            Text(stringResource(R.string.photos_filter_days_note), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            if (draft.isRangeBackwards) Text(stringResource(R.string.photos_filter_backwards), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(onClick = { onApply(draft) }, enabled = !draft.isRangeBackwards) { Text(stringResource(R.string.photos_apply)) }
                TextButton(onClick = { onApply(PhotoFilter()) }) { Text(stringResource(R.string.photos_clear_filters)) }
            }
        }
    }
    if (isFirstPicking) DayPickerDialog(draft.firstDay, onPicked = { day -> draft = draft.copy(firstDay = day); isFirstPicking = false }, onDismiss = { isFirstPicking = false })
    if (isLastPicking) DayPickerDialog(draft.lastDay, onPicked = { day -> draft = draft.copy(lastDay = day); isLastPicking = false }, onDismiss = { isLastPicking = false })
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun DayPickerDialog(day: LocalDate?, onPicked: (LocalDate) -> Unit, onDismiss: () -> Unit) {
    val state = rememberDatePickerState(initialSelectedDateMillis = day?.atStartOfDay(ZoneOffset.UTC)?.toInstant()?.toEpochMilli())
    DatePickerDialog(
        onDismissRequest = onDismiss,
        confirmButton = { TextButton(onClick = { state.selectedDateMillis?.let { millis -> onPicked(Instant.ofEpochMilli(millis).atZone(ZoneOffset.UTC).toLocalDate()) } }) { Text(stringResource(R.string.common_done)) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    ) { DatePicker(state) }
}

/** Changes when a photo was taken, in the clock of the place it was taken. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ChangeDateDialog(photo: Photo, onChanged: (Photo) -> Unit, onDismiss: () -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    val zone = PhotoDateChange.zone(photo)
    val moment = PhotoTimeline.moment(photo)?.instant ?: Instant.now()
    val local = moment.atZone(zone)
    val dateState = rememberDatePickerState(initialSelectedDateMillis = local.toLocalDate().atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli())
    val timeState = rememberTimePickerState(local.hour, local.minute, is24Hour = android.text.format.DateFormat.is24HourFormat(androidx.compose.ui.platform.LocalContext.current))
    var isPickingTime by remember { mutableStateOf(false) }
    if (!isPickingTime) {
        DatePickerDialog(
            onDismissRequest = onDismiss,
            confirmButton = { TextButton(onClick = { isPickingTime = true }) { Text(stringResource(R.string.photos_next)) } },
            dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
        ) { Column { Text(stringResource(R.string.photos_change_date_note), Modifier.padding(horizontal = 24.dp), style = MaterialTheme.typography.bodySmall); DatePicker(dateState) } }
    } else {
        androidx.compose.material3.AlertDialog(
            onDismissRequest = onDismiss,
            title = { Text(stringResource(R.string.photos_change_date)) },
            text = { TimePicker(timeState) },
            confirmButton = {
                TextButton(onClick = {
                    val day = Instant.ofEpochMilli(dateState.selectedDateMillis ?: return@TextButton).atZone(ZoneOffset.UTC).toLocalDate()
                    val chosen = day.atTime(LocalTime.of(timeState.hour, timeState.minute)).atZone(zone).toInstant()
                    scope.launch {
                        try {
                            onChanged(connection.changePhotoDate(photo.id, PhotoDateChange.takenAtText(chosen, photo)))
                            onDismiss()
                        } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
                    }
                }) { Text(stringResource(R.string.photos_save)) }
            },
            dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
        )
    }
}

/** Puts photos at a city, or takes their location away. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ChangeLocationSheet(photoIds: List<String>, hasLocation: Boolean, onDone: () -> Unit, onDismiss: () -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    var words by remember { mutableStateOf("") }
    var places by remember { mutableStateOf<List<PhotoPlace>?>(null) }
    var isRemoveConfirming by remember { mutableStateOf(false) }
    LaunchedEffect(words) {
        places = null
        if (words.isBlank()) return@LaunchedEffect
        delay(350)
        places = try { connection.searchPhotoPlaces(words.trim()) } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure); emptyList() }
    }
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.padding(horizontal = 24.dp).padding(bottom = 32.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(stringResource(R.string.photos_change_location), style = MaterialTheme.typography.titleLarge)
            OutlinedTextField(value = words, onValueChange = { words = it }, singleLine = true, placeholder = { Text(stringResource(R.string.photos_search_city)) }, modifier = Modifier.fillMaxWidth())
            val found = places
            when {
                words.isBlank() -> Text(stringResource(R.string.photos_search_city), color = MaterialTheme.colorScheme.onSurfaceVariant)
                found != null && found.isEmpty() -> Text(stringResource(R.string.photos_no_cities), color = MaterialTheme.colorScheme.onSurfaceVariant)
                found != null -> Column(Modifier.verticalScroll(rememberScrollState()).height(260.dp)) {
                    for (place in found) {
                        TeaNodeRow(
                            headline = place.placeName, leadingIcon = Icons.Outlined.LocationOn,
                            onClick = {
                                scope.launch {
                                    try { connection.setPhotoLocation(photoIds, place.latitude, place.longitude); onDone() } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
                                }
                            },
                        )
                    }
                }
            }
            if (hasLocation) TextButton(onClick = { isRemoveConfirming = true }) { Text(stringResource(R.string.photos_remove_location), color = MaterialTheme.colorScheme.error) }
        }
    }
    if (isRemoveConfirming) {
        com.teanode.app.ui.components.ConfirmationDialog(
            title = stringResource(R.string.photos_remove_location), confirmLabel = stringResource(R.string.photos_remove_location), isDestructive = true,
            text = stringResource(R.string.photos_remove_location_text),
            onConfirm = {
                isRemoveConfirming = false
                scope.launch {
                    try { connection.removePhotoLocation(photoIds); onDone() } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
                }
            },
            onDismiss = { isRemoveConfirming = false },
        )
    }
}

/** Choose an album to put photos in, or make a new one for them. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AddToAlbumSheet(photoIds: List<String>, onDone: () -> Unit, onDismiss: () -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    val context = androidx.compose.ui.platform.LocalContext.current
    val albums = rememberLoad { connection.photoAlbums() }
    var isCreating by remember { mutableStateOf(false) }
    fun add(album: PhotoAlbum) {
        scope.launch {
            try {
                connection.addPhotos(photoIds, album.id)
                snackbar.show(context.getString(R.string.photos_added_to_album, album.albumTitle))
                onDone()
            } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
        }
    }
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.padding(bottom = 32.dp)) {
            Text(stringResource(R.string.photos_add_to_album), style = MaterialTheme.typography.titleLarge, modifier = Modifier.padding(horizontal = 24.dp, vertical = 8.dp))
            TeaNodeRow(headline = stringResource(R.string.photos_new_album_ellipsis), leadingIcon = Icons.Outlined.Add, onClick = { isCreating = true })
            HorizontalDivider()
            LoadedContent(albums, modifier = Modifier.height(320.dp)) { list ->
                Column(Modifier.verticalScroll(rememberScrollState())) {
                    // A circle's albums take photos from any member; a shared one only when the person may contribute.
                    for (album in list.filter { candidate -> candidate.canCallerContribute }) {
                        TeaNodeRow(
                            headline = album.albumTitle, supporting = androidx.compose.ui.res.pluralStringResource(R.plurals.photos_item_count, album.photoCount, album.photoCount),
                            leadingIcon = Icons.Outlined.PhotoAlbum, onClick = { add(album) },
                        )
                    }
                }
            }
        }
    }
    if (isCreating) {
        NewAlbumDialog(
            onCreate = { title ->
                scope.launch {
                    try {
                        val created = connection.createPhotoAlbum(title, photoIds)
                        snackbar.show(context.getString(R.string.photos_added_to_album, created.albumTitle))
                        isCreating = false
                        onDone()
                    } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
                }
            },
            onDismiss = { isCreating = false },
        )
    }
}

/** Asks for the title of a new album. */
@Composable
fun NewAlbumDialog(onCreate: (String) -> Unit, onDismiss: () -> Unit, initialTitle: String = "") {
    var title by remember { mutableStateOf(initialTitle) }
    androidx.compose.material3.AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.photos_new_album)) },
        text = { OutlinedTextField(value = title, onValueChange = { title = it }, singleLine = true, placeholder = { Text(stringResource(R.string.photos_album_title)) }) },
        confirmButton = { TextButton(onClick = { onCreate(title) }, enabled = title.isNotBlank()) { Text(stringResource(R.string.photos_create_album)) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}
