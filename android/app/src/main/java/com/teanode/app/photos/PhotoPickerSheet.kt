package com.teanode.app.photos

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.PhotoLibrary
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.kit.photos.PhotoAlbum
import com.teanode.kit.photos.PhotoListQuery
import com.teanode.kit.photos.addPhotos
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

/**
 * Chooses photos of the person's own library to add to an album they contribute to (ios PhotoPickerSheet): the library's
 * grid, in selection. [onAdded] runs once the server has taken them, for the album under the sheet to read again.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PhotoPickerSheet(album: PhotoAlbum, onAdded: () -> Unit, onDismiss: () -> Unit) {
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        PhotoPickerContent(album, onAdded, onDismiss)
    }
}

/** What the picker shows, apart from the sheet it is in. */
@Composable
internal fun PhotoPickerContent(album: PhotoAlbum, onAdded: () -> Unit, onDismiss: () -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val list = rememberPhotoWindow(PhotoListQuery())
    var selectedIds by remember { mutableStateOf(emptySet<String>()) }
    var isAdding by remember { mutableStateOf(false) }
    val moreError = list.moreError
    androidx.compose.runtime.LaunchedEffect(moreError) { if (moreError != null) { list.clearMoreError(); snackbar.showError(moreError) } }

    fun add() {
        scope.launch {
            isAdding = true
            try {
                val addedCount = connection.addPhotos(selectedIds.toList(), album.id)
                snackbar.show(context.getString(if (addedCount == 0) R.string.photos_already_in_album else R.string.photos_added_to_album, album.albumTitle))
                onAdded()
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                snackbar.showError(failure)
            } finally {
                isAdding = false
            }
        }
    }

    Column(Modifier.fillMaxHeight(0.92f)) {
        Row(Modifier.fillMaxWidth().padding(horizontal = 8.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.SpaceBetween) {
            TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) }
            Text(
                if (selectedIds.isEmpty()) stringResource(R.string.photos_select_photos) else stringResource(R.string.photos_selected_count, selectedIds.size),
                style = MaterialTheme.typography.titleMedium,
            )
            if (isAdding) {
                Box(Modifier.padding(horizontal = 16.dp)) { CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp) }
            } else {
                TextButton(onClick = ::add, enabled = selectedIds.isNotEmpty()) { Text(stringResource(R.string.photos_add)) }
            }
        }
        Box(Modifier.weight(1f).fillMaxWidth()) {
            when {
                list.error != null && !list.isLoaded -> ErrorState(stringResource(R.string.photos_cannot_load), onRetry = { list.retry() })
                !list.isLoaded -> LoadingState()
                list.totalCount == 0 -> EmptyState(Icons.Outlined.PhotoLibrary, stringResource(R.string.photos_empty_library), description = stringResource(R.string.photos_picker_empty_text))
                else -> PhotoWindowGrid(
                    list, selectedIds, isSelecting = true,
                    onOpen = { _, photo -> selectedIds = if (photo.id in selectedIds) selectedIds - photo.id else selectedIds + photo.id },
                    onToggleSelected = { photo -> selectedIds = if (photo.id in selectedIds) selectedIds - photo.id else selectedIds + photo.id },
                    modifier = Modifier.fillMaxSize(), state = rememberLazyGridState(),
                )
            }
        }
    }
}
