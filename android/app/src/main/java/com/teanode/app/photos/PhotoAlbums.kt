package com.teanode.app.photos

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.PhotoAlbum
import androidx.compose.material.icons.outlined.PhotoLibrary
import androidx.compose.material3.Button
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ElevatedCard
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.shell.LocalAppNavigator
import com.teanode.app.shell.Routes
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.TeaNodeSectionHeader
import com.teanode.app.ui.components.rememberLoad
import com.teanode.app.ui.components.rememberServerPicture
import com.teanode.kit.AppSection
import com.teanode.kit.photos.Photo
import com.teanode.kit.photos.PhotoAlbum
import com.teanode.kit.photos.PhotoAlbumSections
import com.teanode.kit.photos.PhotoAlbumSuggestion
import com.teanode.kit.photos.PhotoTimeline
import com.teanode.kit.photos.createPhotoAlbum
import com.teanode.kit.photos.deletePhotoAlbum
import com.teanode.kit.photos.photoAlbumSuggestions
import com.teanode.kit.photos.photoAlbums
import com.teanode.kit.photos.photoDetail
import com.teanode.kit.photos.photoIds
import com.teanode.kit.photos.removePhotos
import com.teanode.kit.photos.renamePhotoAlbum
import com.teanode.kit.photos.savePhotoToLibrary
import com.teanode.kit.photos.trashPhotos
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch

/** What the Albums collection reads: the person's albums, and groups of photos the server suggests making albums of. */
private class AlbumsRead(val albums: List<PhotoAlbum>, val suggestions: List<PhotoAlbumSuggestion>)

/** The Albums collection (ios/TeaNode/Sources/PhotoAlbums.swift): suggestions, your albums, those shared with you, and your circles'. */
@Composable
fun AlbumsContent(onOpenAlbum: (PhotoAlbum) -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    val read = rememberLoad { AlbumsRead(connection.photoAlbums(), runCatching { connection.photoAlbumSuggestions() }.getOrDefault(emptyList())) }
    var isCreating by remember { mutableStateOf(false) }
    var dismissedSuggestionIds by remember { mutableStateOf(emptySet<String>()) }
    val context = androidx.compose.ui.platform.LocalContext.current
    Box(Modifier.fillMaxSize()) {
        com.teanode.app.ui.components.LoadedContent(
            read,
            isEmpty = { loaded -> loaded.albums.isEmpty() && loaded.suggestions.isEmpty() },
            empty = {
                EmptyState(
                    Icons.Outlined.PhotoAlbum, stringResource(R.string.photos_no_albums), description = stringResource(R.string.photos_no_albums_text),
                    actionLabel = stringResource(R.string.photos_new_album), onAction = { isCreating = true },
                )
            },
        ) { loaded ->
            val sections = PhotoAlbumSections.of(loaded.albums)
            val suggestions = loaded.suggestions.filter { suggestion -> suggestion.id !in dismissedSuggestionIds }
            LazyColumn(Modifier.fillMaxSize(), contentPadding = androidx.compose.foundation.layout.PaddingValues(bottom = 88.dp)) {
                if (suggestions.isNotEmpty()) {
                    item { TeaNodeSectionHeader(stringResource(R.string.photos_suggested)) }
                    items(suggestions.size) { index ->
                        val suggestion = suggestions[index]
                        SuggestionCard(
                            suggestion,
                            onMake = {
                                scope.launch {
                                    try {
                                        val ids = connection.photoIds(suggestion)
                                        connection.createPhotoAlbum(suggestion.albumTitle, ids)
                                        read.refresh()
                                        snackbar.show(context.getString(R.string.photos_album_made, suggestion.albumTitle))
                                    } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
                                }
                            },
                            onSkip = { dismissedSuggestionIds = dismissedSuggestionIds + suggestion.id },
                        )
                    }
                }
                if (sections.own.isNotEmpty()) {
                    item { TeaNodeSectionHeader(stringResource(R.string.photos_my_albums)) }
                    albumRows(sections.own, onOpenAlbum)
                }
                if (sections.sharedWithYou.isNotEmpty()) {
                    item { TeaNodeSectionHeader(stringResource(R.string.photos_shared_with_you)) }
                    albumRows(sections.sharedWithYou, onOpenAlbum)
                }
                for (circle in sections.circles) {
                    item { TeaNodeSectionHeader(circle.circleName) }
                    albumRows(circle.albums, onOpenAlbum)
                }
            }
        }
        FloatingActionButton(onClick = { isCreating = true }, modifier = Modifier.align(Alignment.BottomEnd).padding(16.dp)) {
            Icon(Icons.Outlined.Add, contentDescription = stringResource(R.string.photos_new_album))
        }
    }
    if (isCreating) {
        NewAlbumDialog(
            onCreate = { title ->
                scope.launch {
                    try { connection.createPhotoAlbum(title); isCreating = false; read.refresh() } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) }
                }
            },
            onDismiss = { isCreating = false },
        )
    }
}

private fun androidx.compose.foundation.lazy.LazyListScope.albumRows(albums: List<PhotoAlbum>, onOpen: (PhotoAlbum) -> Unit) {
    items(albums.size) { index ->
        val album = albums[index]
        PhotoAlbumRow(album) { onOpen(album) }
    }
}

@Composable
fun PhotoAlbumRow(album: PhotoAlbum, onClick: () -> Unit) {
    val cover = rememberServerPicture(album.coverPreviewUrl.ifEmpty { null })
    Row(Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(16.dp)) {
        Box(Modifier.size(64.dp).clip(RoundedCornerShape(12.dp)).background(MaterialTheme.colorScheme.surfaceVariant), contentAlignment = Alignment.Center) {
            if (cover != null) Image(cover, contentDescription = null, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
            else Icon(Icons.Outlined.PhotoLibrary, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        Column(Modifier.weight(1f)) {
            Text(album.albumTitle, style = MaterialTheme.typography.bodyLarge, maxLines = 2)
            Text(
                listOfNotNull(pluralStringResource(R.plurals.photos_item_count, album.photoCount, album.photoCount), album.dateRangeText(), album.ownerDisplayName.takeIf { name -> !album.isOwnedByCaller && name.isNotEmpty() })
                    .joinToString(" · "),
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2,
            )
        }
    }
}

@Composable
private fun SuggestionCard(suggestion: PhotoAlbumSuggestion, onMake: () -> Unit, onSkip: () -> Unit) {
    ElevatedCard(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp)) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(suggestion.albumTitle, style = MaterialTheme.typography.titleMedium)
            Text(
                listOfNotNull(
                    suggestion.dateRangeText(), pluralStringResource(R.plurals.photos_item_count, suggestion.photoCount, suggestion.photoCount),
                    suggestion.placeNames.takeIf { places -> places.isNotEmpty() && !suggestion.isTitleMadeOfPlaces }?.joinToString(", "),
                ).joinToString(" · "),
                style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            LazyRow(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                items(suggestion.samplePhotoIds.take(PhotoAlbumSuggestion.SHOWN_SAMPLE_COUNT).size) { index ->
                    SamplePicture(suggestion.samplePhotoIds[index])
                }
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Button(onClick = onMake) { Text(stringResource(R.string.photos_create_album)) }
                OutlinedButton(onClick = onSkip) { Text(stringResource(R.string.photos_not_now)) }
            }
        }
    }
}

@Composable
private fun SamplePicture(photoId: String) {
    // A suggestion names its sample photos by id; each is read for the address of its small preview.
    val connection = LocalConnection.current
    val previewPath by produceState<String?>(null, photoId) { value = runCatching { connection.photoDetail(photoId).photo.gridPreviewPath }.getOrNull() }
    val picture = rememberServerPicture(previewPath)
    Box(Modifier.size(64.dp).clip(RoundedCornerShape(8.dp)).background(MaterialTheme.colorScheme.surfaceVariant)) {
        if (picture != null) Image(picture, contentDescription = null, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
    }
}

/**
 * One album's photos, opened from the Albums collection: choose photos to remove or trash, rename and delete the album
 * (its owner), share it (route `album/{id}/shares`), or save photos shared by somebody else into the person's library.
 */
@Composable
fun AlbumScreen(photoAlbumId: String, onBack: () -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val navigator = LocalAppNavigator.current
    val scope = rememberCoroutineScope()
    val context = androidx.compose.ui.platform.LocalContext.current
    val albumLoad = rememberLoad(photoAlbumId) { connection.photoAlbums().first { candidate -> candidate.id == photoAlbumId } }
    val list = rememberPhotoList(PhotoListSource.Album(photoAlbumId))
    var album by remember { mutableStateOf<PhotoAlbum?>(null) }
    LaunchedEffect(albumLoad.value) { albumLoad.value?.let { loaded -> album = loaded } }
    var isSelecting by remember { mutableStateOf(false) }
    var selectedIds by remember { mutableStateOf(emptySet<String>()) }
    var openedPhotoId by remember { mutableStateOf<String?>(null) }
    var isMenuOpen by remember { mutableStateOf(false) }
    var isRenaming by remember { mutableStateOf(false) }
    var isDeleting by remember { mutableStateOf(false) }
    var isRemoving by remember { mutableStateOf(false) }
    var isTrashing by remember { mutableStateOf(false) }
    val current = album
    val moreError = list.moreError
    LaunchedEffect(moreError) { if (moreError != null) { list.clearMoreError(); snackbar.showError(moreError) } }

    fun fail(failure: Throwable) { if (failure is CancellationException) throw failure else snackbar.showError(failure) }

    Box(Modifier.fillMaxSize()) {
    TeaNodeScreen(
        title = if (isSelecting) stringResource(R.string.photos_selected_count, selectedIds.size) else current?.albumTitle ?: "",
        onBack = { if (isSelecting) { isSelecting = false; selectedIds = emptySet() } else onBack() },
        actions = {
            if (current != null && !isSelecting) {
                Box {
                    IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.photos_more)) }
                    DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                        if (list.photos.isNotEmpty()) DropdownMenuItem(text = { Text(stringResource(R.string.photos_select)) }, onClick = { isMenuOpen = false; isSelecting = true })
                        if (current.isOwnedByCaller && current.circleId == null) DropdownMenuItem(text = { Text(stringResource(R.string.photos_rename_album)) }, onClick = { isMenuOpen = false; isRenaming = true })
                        if (current.canCallerShare) DropdownMenuItem(text = { Text(stringResource(R.string.photos_share)) }, onClick = { isMenuOpen = false; navigator.navigate(Routes.child(AppSection.Photos, "album/$photoAlbumId/shares")) })
                        if (current.canCallerManage) DropdownMenuItem(text = { Text(stringResource(R.string.photos_delete_album), color = MaterialTheme.colorScheme.error) }, onClick = { isMenuOpen = false; isDeleting = true })
                    }
                }
            }
        },
        bottomBar = {
            if (isSelecting && current != null) {
                androidx.compose.material3.BottomAppBar {
                    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceEvenly) {
                        if (current.canCallerManage || current.canCallerContribute) androidx.compose.material3.TextButton(onClick = { isRemoving = true }, enabled = selectedIds.isNotEmpty()) { Text(stringResource(R.string.photos_remove_from_album)) }
                        if (!current.isOwnedByCaller) androidx.compose.material3.TextButton(onClick = {
                            scope.launch {
                                try { selectedIds.forEach { photoId -> connection.savePhotoToLibrary(photoId) }; snackbar.show(context.getString(R.string.photos_saved_to_library)); isSelecting = false; selectedIds = emptySet() } catch (failure: Throwable) { fail(failure) }
                            }
                        }, enabled = selectedIds.isNotEmpty()) { Text(stringResource(R.string.photos_save_to_library)) }
                        else androidx.compose.material3.TextButton(onClick = { isTrashing = true }, enabled = selectedIds.isNotEmpty()) { Text(stringResource(R.string.photos_move_to_trash), color = MaterialTheme.colorScheme.error) }
                    }
                }
            }
        },
    ) { padding ->
        Box(Modifier.fillMaxSize().padding(padding)) {
            when {
                list.error != null && !list.isLoaded -> ErrorState(stringResource(R.string.photos_cannot_load), onRetry = { list.retry() })
                !list.isLoaded -> LoadingState()
                list.photos.isEmpty() -> EmptyState(Icons.Outlined.PhotoLibrary, stringResource(R.string.photos_empty_album), description = stringResource(if (current?.canCallerContribute == true) R.string.photos_empty_album_text_own else R.string.photos_empty_album_text_shared))
                else -> PhotoGrid(
                    PhotoGridContent.Days(PhotoTimeline.days(list.photos)), selectedIds, isSelecting,
                    onOpen = { photo -> openedPhotoId = photo.id },
                    onToggleSelected = { photo -> isSelecting = true; selectedIds = if (photo.id in selectedIds) selectedIds - photo.id else selectedIds + photo.id },
                    onShowed = { photo -> list.showed(photo.id) }, state = rememberLazyGridState(),
                )
            }
        }
    }
    val opened = list.photos.firstOrNull { photo -> photo.id == openedPhotoId }
            if (opened != null) {
                androidx.activity.compose.BackHandler { openedPhotoId = null }
                PhotoViewer(
                    photos = list.photos, initialPhotoId = opened.id, onClose = { openedPhotoId = null }, library = null,
                    onPhotoChanged = { photo -> list.replace(photo) }, onPhotosGone = { ids -> list.remove(ids); albumLoad.refresh() },
                    album = current, onAlbumChanged = { updated -> album = updated },
                )
            }
    }
    if (isRenaming && current != null) {
        NewAlbumDialog(
            initialTitle = current.albumTitle,
            onCreate = { title -> scope.launch { try { album = connection.renamePhotoAlbum(photoAlbumId, title); isRenaming = false } catch (failure: Throwable) { fail(failure) } } },
            onDismiss = { isRenaming = false },
        )
    }
    if (isDeleting && current != null) {
        ConfirmationDialog(
            title = stringResource(R.string.photos_delete_album_title), confirmLabel = stringResource(R.string.photos_delete_album), isDestructive = true,
            text = stringResource(if (current.isOwnedByCaller) R.string.photos_delete_album_text_own else R.string.photos_delete_album_text_shared),
            onConfirm = { isDeleting = false; scope.launch { try { connection.deletePhotoAlbum(photoAlbumId); onBack() } catch (failure: Throwable) { fail(failure) } } },
            onDismiss = { isDeleting = false },
        )
    }
    if (isRemoving) {
        ConfirmationDialog(
            title = stringResource(R.string.photos_remove_from_album), confirmLabel = stringResource(R.string.photos_remove_from_album),
            text = stringResource(R.string.photos_remove_from_album_text),
            onConfirm = {
                isRemoving = false
                val ids = selectedIds.toList()
                scope.launch { try { connection.removePhotos(ids, photoAlbumId); list.remove(ids); isSelecting = false; selectedIds = emptySet(); albumLoad.refresh() } catch (failure: Throwable) { fail(failure) } }
            },
            onDismiss = { isRemoving = false },
        )
    }
    if (isTrashing) {
        ConfirmationDialog(
            title = stringResource(R.string.photos_trash_selected_title), confirmLabel = stringResource(R.string.photos_move_to_trash), isDestructive = true,
            text = stringResource(R.string.photos_trash_selected_text, 30),
            onConfirm = {
                isTrashing = false
                val ids = selectedIds.toList()
                scope.launch { try { connection.trashPhotos(ids); list.remove(ids); isSelecting = false; selectedIds = emptySet() } catch (failure: Throwable) { fail(failure) } }
            },
            onDismiss = { isTrashing = false },
        )
    }
}
