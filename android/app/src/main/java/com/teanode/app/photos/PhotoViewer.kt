package com.teanode.app.photos

import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTransformGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.pager.HorizontalPager
import androidx.compose.foundation.pager.rememberPagerState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.ArrowBack
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material.icons.outlined.BrokenImage
import androidx.compose.material.icons.outlined.FavoriteBorder
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.rememberServerPicture
import com.teanode.kit.photos.Photo
import com.teanode.kit.photos.PhotoDescriptionPolling
import com.teanode.kit.photos.PhotoDetail
import com.teanode.kit.photos.PhotoLibrary
import com.teanode.kit.photos.PhotoOverview
import com.teanode.kit.photos.PhotoPositionPlace
import com.teanode.kit.photos.describePhoto
import com.teanode.kit.photos.deletePhotos
import com.teanode.kit.photos.photoDetail
import com.teanode.kit.photos.photoPlace
import com.teanode.kit.photos.restorePhotos
import com.teanode.kit.photos.removePhotos
import com.teanode.kit.photos.setPhotoAlbumCover
import com.teanode.kit.photos.retryPhotoProcessing
import com.teanode.kit.photos.savePhotoToLibrary
import com.teanode.kit.photos.setPhotoFavorite
import com.teanode.kit.photos.trashPhotos
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/**
 * The photo viewer (ios/TeaNode/Sources/PhotoViewer.swift): a photo at a time, swiped between, with its heart, its
 * info (date, camera, file, place, albums, what a model read in it, the people in it) and the actions that fit where
 * it was opened from. [album] is the album it was opened in, which adds "Use as album cover" and "Remove from album".
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun PhotoViewer(
    photos: List<Photo>,
    initialPhotoId: String,
    onClose: () -> Unit,
    library: PhotoLibrary?,
    onPhotoChanged: (Photo) -> Unit,
    onPhotosGone: (Collection<String>) -> Unit,
    album: com.teanode.kit.photos.PhotoAlbum? = null,
    onAlbumChanged: (com.teanode.kit.photos.PhotoAlbum) -> Unit = {},
    onShowNear: ((Photo) -> Unit)? = null,
    initialIsInfoOpen: Boolean = false,
) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val startIndex = photos.indexOfFirst { photo -> photo.id == initialPhotoId }.coerceAtLeast(0)
    val pagerState = rememberPagerState(initialPage = startIndex) { photos.size }
    val photo = photos.getOrNull(pagerState.currentPage) ?: return
    var isInfoOpen by remember { mutableStateOf(initialIsInfoOpen) }
    var isMenuOpen by remember { mutableStateOf(false) }
    var confirming by remember { mutableStateOf<ViewerConfirmation?>(null) }
    var isBarShown by remember { mutableStateOf(true) }
    var isDateChanging by remember { mutableStateOf(false) }
    var isLocationChanging by remember { mutableStateOf(false) }

    fun fail(failure: Throwable) {
        if (failure is CancellationException) throw failure
        if (failure is com.teanode.kit.photos.PhotoError) snackbar.showProblem(photoErrorWords(context, failure)) else snackbar.showError(failure)
    }

    fun act(message: Int?, work: suspend () -> Unit) {
        scope.launch { try { work(); message?.let { text -> snackbar.show(context.getString(text)) } } catch (failure: Throwable) { fail(failure) } }
    }

    Box(Modifier.fillMaxSize().background(Color.Black)) {
        HorizontalPager(pagerState, Modifier.fillMaxSize(), key = { index -> photos[index].id }) { page ->
            ViewerPage(photos[page], onToggleBar = { isBarShown = !isBarShown })
        }
        if (isBarShown) {
            Row(
                Modifier.fillMaxWidth().background(Brush.verticalGradient(listOf(Color.Black.copy(alpha = 0.6f), Color.Transparent))).padding(horizontal = 4.dp, vertical = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                IconButton(onClick = onClose) { Icon(Icons.AutoMirrored.Outlined.ArrowBack, contentDescription = stringResource(R.string.photos_close), tint = Color.White) }
                Column(Modifier.weight(1f)) {
                    Text(PhotoOverview.viewerDayText(photo) ?: stringResource(R.string.photos_unknown_date), color = Color.White, style = MaterialTheme.typography.titleMedium)
                    PhotoOverview.viewerTimeText(photo)?.let { time -> Text(time, color = Color.White.copy(alpha = 0.8f), style = MaterialTheme.typography.bodySmall) }
                }
                if (photo.isOwnedByCaller && !photo.isTrashed) {
                    IconButton(onClick = {
                        act(null) {
                            val changed = connection.setPhotoFavorite(photo.id, !photo.isFavorite)
                            onPhotoChanged(changed)
                        }
                    }) {
                        Icon(if (photo.isFavorite) Icons.Filled.Favorite else Icons.Outlined.FavoriteBorder, contentDescription = stringResource(if (photo.isFavorite) R.string.photos_remove_favorite else R.string.photos_add_favorite), tint = Color.White)
                    }
                }
                IconButton(onClick = { isInfoOpen = true }) { Icon(Icons.Outlined.Info, contentDescription = stringResource(R.string.photos_info), tint = Color.White) }
                Box {
                    IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.photos_more), tint = Color.White) }
                    DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                        ViewerMenu(
                            photo = photo, album = album,
                            onShare = {
                                isMenuOpen = false
                                act(null) { val file = connection.downloadPhotoFile(photo); sharePhoto(context, saveToGallery(context, file), file.contentType) }
                            },
                            onSaveToGallery = { role ->
                                isMenuOpen = false
                                act(R.string.photos_saved_to_gallery) { saveToGallery(context, connection.downloadPhotoFile(photo, role)) }
                            },
                            onSaveToLibrary = { isMenuOpen = false; act(R.string.photos_saved_to_library) { connection.savePhotoToLibrary(photo.id) } },
                            onRetryPreviews = { isMenuOpen = false; act(R.string.photos_previews_again) { connection.retryPhotoProcessing(listOf(photo.id)); onPhotoChanged(connection.photoDetail(photo.id).photo) } },
                            onUseAsCover = {
                                isMenuOpen = false
                                act(R.string.photos_cover_set) { album?.let { target -> onAlbumChanged(connection.setPhotoAlbumCover(target.id, photo.id)) } }
                            },
                            onRemoveFromAlbum = { isMenuOpen = false; confirming = ViewerConfirmation.RemoveFromAlbum },
                            onChangeDate = { isMenuOpen = false; isDateChanging = true },
                            onChangeLocation = { isMenuOpen = false; isLocationChanging = true },
                            onShowNear = onShowNear?.let { show -> { isMenuOpen = false; show(photo) } },
                            onTrash = { isMenuOpen = false; confirming = ViewerConfirmation.Trash },
                            onRestore = { isMenuOpen = false; act(null) { connection.restorePhotos(listOf(photo.id)); onPhotosGone(listOf(photo.id)) } },
                            onDelete = { isMenuOpen = false; confirming = ViewerConfirmation.Delete },
                        )
                    }
                }
            }
        }
    }
    if (isDateChanging) ChangeDateDialog(photo, onChanged = onPhotoChanged, onDismiss = { isDateChanging = false })
    if (isLocationChanging) {
        ChangeLocationSheet(listOf(photo.id), photo.latitude != null, onDone = { isLocationChanging = false; scope.launch { runCatching { connection.photoDetail(photo.id) }.getOrNull()?.let { fresh -> onPhotoChanged(fresh.photo) } } }, onDismiss = { isLocationChanging = false })
    }
    if (isInfoOpen) {
        ModalBottomSheet(onDismissRequest = { isInfoOpen = false }) {
            PhotoInfo(photo, onPhotoChanged = onPhotoChanged, albumIdOpenedFrom = album?.id)
        }
    }
    when (confirming) {
        ViewerConfirmation.Trash -> ConfirmationDialog(
            title = stringResource(R.string.photos_trash_one_title), confirmLabel = stringResource(R.string.photos_move_to_trash), isDestructive = true,
            text = stringResource(R.string.photos_trash_selected_text, library?.trashRetentionDays ?: 30),
            onConfirm = { confirming = null; act(null) { connection.trashPhotos(listOf(photo.id)); onPhotosGone(listOf(photo.id)) } }, onDismiss = { confirming = null },
        )
        ViewerConfirmation.Delete -> ConfirmationDialog(
            title = stringResource(R.string.photos_delete_one_title), confirmLabel = stringResource(R.string.photos_delete_forever), isDestructive = true,
            text = stringResource(R.string.photos_cannot_be_undone),
            onConfirm = { confirming = null; act(null) { connection.deletePhotos(listOf(photo.id)); onPhotosGone(listOf(photo.id)) } }, onDismiss = { confirming = null },
        )
        ViewerConfirmation.RemoveFromAlbum -> ConfirmationDialog(
            title = stringResource(R.string.photos_remove_from_album), confirmLabel = stringResource(R.string.photos_remove_from_album),
            text = stringResource(R.string.photos_remove_from_album_text),
            onConfirm = {
                confirming = null
                act(null) { album?.let { target -> connection.removePhotos(listOf(photo.id), target.id) }; onPhotosGone(listOf(photo.id)) }
            },
            onDismiss = { confirming = null },
        )
        null -> Unit
    }
}

private enum class ViewerConfirmation { Trash, Delete, RemoveFromAlbum }

@Composable
private fun ViewerMenu(
    photo: Photo,
    album: com.teanode.kit.photos.PhotoAlbum?,
    onShare: () -> Unit,
    onSaveToGallery: (String?) -> Unit,
    onSaveToLibrary: () -> Unit,
    onRetryPreviews: () -> Unit,
    onUseAsCover: () -> Unit,
    onRemoveFromAlbum: () -> Unit,
    onChangeDate: () -> Unit,
    onChangeLocation: () -> Unit,
    onShowNear: (() -> Unit)?,
    onTrash: () -> Unit,
    onRestore: () -> Unit,
    onDelete: () -> Unit,
) {
    DropdownMenuItem(text = { Text(stringResource(R.string.photos_share)) }, onClick = onShare)
    DropdownMenuItem(text = { Text(stringResource(R.string.photos_save_original)) }, onClick = { onSaveToGallery(null) })
    if (photo.hasEditedFile) DropdownMenuItem(text = { Text(stringResource(R.string.photos_save_edited)) }, onClick = { onSaveToGallery("edited") })
    if (photo.hasLiveVideoFile) DropdownMenuItem(text = { Text(stringResource(R.string.photos_save_live_video)) }, onClick = { onSaveToGallery("liveVideo") })
    if (!photo.isOwnedByCaller) DropdownMenuItem(text = { Text(stringResource(R.string.photos_save_to_library)) }, onClick = onSaveToLibrary)
    if (photo.isFailed) DropdownMenuItem(text = { Text(stringResource(R.string.photos_make_thumbnails_again)) }, onClick = onRetryPreviews)
    if (album != null) {
        if (album.canCallerManage) DropdownMenuItem(text = { Text(stringResource(R.string.photos_use_as_cover)) }, onClick = onUseAsCover)
        if (album.canCallerRemove(photo)) DropdownMenuItem(text = { Text(stringResource(R.string.photos_remove_from_album)) }, onClick = onRemoveFromAlbum)
    }
    if (photo.isOwnedByCaller && !photo.isTrashed) {
        DropdownMenuItem(text = { Text(stringResource(R.string.photos_change_date)) }, onClick = onChangeDate)
        DropdownMenuItem(text = { Text(stringResource(R.string.photos_change_location)) }, onClick = onChangeLocation)
    }
    if (onShowNear != null && photo.latitude != null) DropdownMenuItem(text = { Text(stringResource(R.string.photos_taken_near_here)) }, onClick = onShowNear)
    if (photo.isOwnedByCaller) {
        if (photo.isTrashed) {
            DropdownMenuItem(text = { Text(stringResource(R.string.photos_restore)) }, onClick = onRestore)
            DropdownMenuItem(text = { Text(stringResource(R.string.photos_delete_forever), color = MaterialTheme.colorScheme.error) }, onClick = onDelete)
        } else {
            DropdownMenuItem(text = { Text(stringResource(R.string.photos_move_to_trash), color = MaterialTheme.colorScheme.error) }, onClick = onTrash)
        }
    }
}

@Composable
private fun ViewerPage(photo: Photo, onToggleBar: () -> Unit) {
    val picture = rememberServerPicture(photo.viewerPreviewPath)
    var scale by remember { mutableFloatStateOf(1f) }
    var offsetX by remember { mutableFloatStateOf(0f) }
    var offsetY by remember { mutableFloatStateOf(0f) }
    var isPlaying by remember { mutableStateOf(false) }
    Box(
        Modifier.fillMaxSize().clickable(onClick = onToggleBar).pointerInput(Unit) {
            detectTransformGestures { _, pan, zoom, _ ->
                scale = (scale * zoom).coerceIn(1f, 5f)
                if (scale > 1f) { offsetX += pan.x; offsetY += pan.y } else { offsetX = 0f; offsetY = 0f }
            }
        },
        contentAlignment = Alignment.Center,
    ) {
        when {
            isPlaying && photo.videoPath != null -> VideoPlayback(photo, onEnded = { isPlaying = false })
            picture != null -> Image(
                picture, contentDescription = photoDescription(photo), contentScale = ContentScale.Fit,
                modifier = Modifier.fillMaxSize().graphicsLayer(scaleX = scale, scaleY = scale, translationX = offsetX, translationY = offsetY),
            )
            photo.isFailed -> Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Icon(Icons.Outlined.BrokenImage, contentDescription = null, tint = Color.White, modifier = Modifier.size(48.dp))
                Text(stringResource(R.string.photos_cannot_show), color = Color.White)
                if (photo.processingErrorMessage.isNotEmpty()) Text(photo.processingErrorMessage, color = Color.White.copy(alpha = 0.7f), style = MaterialTheme.typography.bodySmall)
            }
            else -> Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(12.dp)) {
                CircularProgressIndicator(color = Color.White)
                Text(stringResource(R.string.photos_getting_ready), color = Color.White)
            }
        }
        if (photo.isVideo && !isPlaying && photo.videoPath != null) {
            IconButton(onClick = { isPlaying = true }, modifier = Modifier.size(72.dp).background(Color.Black.copy(alpha = 0.5f), androidx.compose.foundation.shape.CircleShape)) {
                Icon(Icons.Filled.PlayArrow, contentDescription = stringResource(R.string.photos_play), tint = Color.White, modifier = Modifier.size(40.dp))
            }
        }
    }
}

/** What the info sheet shows: the file, the date, the place, the albums, what a model read, and the people. */
@Composable
internal fun PhotoInfo(photo: Photo, onPhotoChanged: (Photo) -> Unit, albumIdOpenedFrom: String?) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    var detail by remember(photo.id) { mutableStateOf<PhotoDetail?>(null) }
    var failure by remember(photo.id) { mutableStateOf<Throwable?>(null) }
    var place by remember(photo.id) { mutableStateOf<PhotoPositionPlace?>(null) }
    var description by remember(photo.id) { mutableStateOf(photo.description) }
    var waitedSeconds by remember(photo.id) { mutableStateOf(0) }
    LaunchedEffect(photo.id) {
        try {
            detail = connection.photoDetail(photo.id)
            description = detail!!.photoDescription
            if (photo.latitude != null) place = runCatching { connection.photoPlace(photo.id) }.getOrNull()
        } catch (cancelled: CancellationException) {
            throw cancelled
        } catch (error: Throwable) {
            failure = error
        }
    }
    // A description on its way is asked about every few seconds while the sheet stays open, slower after two minutes.
    LaunchedEffect(photo.id, description.isDescriptionRequested) {
        while (description.isDescriptionRequested && !description.isDescribed) {
            delay(PhotoDescriptionPolling.secondsBeforeAsking(waitedSeconds) * 1000L)
            waitedSeconds += PhotoDescriptionPolling.secondsBeforeAsking(waitedSeconds)
            runCatching { connection.photoDetail(photo.id) }.getOrNull()?.let { fresh -> detail = fresh; description = fresh.photoDescription }
        }
    }
    Column(Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).padding(horizontal = 24.dp).padding(bottom = 32.dp)) {
        Text(stringResource(R.string.photos_info), style = MaterialTheme.typography.titleLarge)
        Spacer(Modifier.height(12.dp))
        InfoLine(stringResource(R.string.photos_info_date), com.teanode.kit.photos.PhotoOverview.viewerDayText(photo)?.let { day -> day + (PhotoOverview.viewerTimeText(photo)?.let { time -> ", $time" } ?: "") })
        InfoLine(stringResource(R.string.photos_info_file_name), photo.filename)
        InfoLine(stringResource(R.string.photos_info_size), if (photo.widthPixels > 0) "${photo.widthPixels} x ${photo.heightPixels}" else null)
        InfoLine(stringResource(R.string.photos_info_camera), listOf(photo.cameraMake, photo.cameraModel).filter { part -> part.isNotEmpty() }.joinToString(" ").ifEmpty { null })
        InfoLine(stringResource(R.string.photos_info_place), place?.locationPlace?.placeName?.ifEmpty { null } ?: place?.addressText?.ifEmpty { null } ?: photo.placeName.ifEmpty { null })
        if (!photo.isOwnedByCaller) InfoLine(stringResource(R.string.photos_info_shared_by), photo.ownerName.ifEmpty { null })
        if (failure != null) Text(stringResource(R.string.photos_info_could_not_read), color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(vertical = 8.dp))
        detail?.albums?.takeIf { albums -> albums.isNotEmpty() }?.let { albums ->
            HorizontalDivider(Modifier.padding(vertical = 12.dp))
            Text(stringResource(R.string.photos_info_albums), style = MaterialTheme.typography.titleSmall)
            for (album in albums) Text(album.albumTitle, modifier = Modifier.padding(vertical = 4.dp))
        }
        val kindText = when (description.photoDocumentKind) {
            "receipt" -> stringResource(R.string.photos_read_as_receipt)
            "statement" -> stringResource(R.string.photos_read_as_statement)
            "taxDocument" -> stringResource(R.string.photos_read_as_tax_document)
            else -> null
        }
        if (description.isDescribed || description.isDescriptionRequested) HorizontalDivider(Modifier.padding(vertical = 12.dp))
        if (description.isDescribed) {
            Text(stringResource(R.string.photos_info_description), style = MaterialTheme.typography.titleSmall)
            Text(description.photoDescription, modifier = Modifier.padding(vertical = 4.dp))
            kindText?.let { text -> Text(text, color = MaterialTheme.colorScheme.primary, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.padding(vertical = 4.dp)) }
            if (description.descriptionVisibleText.isNotEmpty()) {
                Text(stringResource(R.string.photos_info_text_in_picture), style = MaterialTheme.typography.titleSmall, modifier = Modifier.padding(top = 8.dp))
                Text(description.descriptionVisibleText, modifier = Modifier.padding(vertical = 4.dp))
            }
            com.teanode.kit.photos.ServerTime.instant(description.describedAt)?.let { instant ->
                val day = java.time.format.DateTimeFormatter.ofLocalizedDate(java.time.format.FormatStyle.LONG).format(instant.atZone(java.time.ZoneId.systemDefault()))
                Text(stringResource(R.string.photos_described_on, day), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            OutlinedButton(onClick = {
                scope.launch {
                    try { description = connection.describePhoto(photo.id, isRedescribed = true) } catch (error: CancellationException) { throw error } catch (error: Throwable) { snackbar.showError(error) }
                }
            }, modifier = Modifier.padding(top = 8.dp)) { Text(stringResource(R.string.photos_describe_again)) }
        } else if (description.isDescriptionRequested) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                Text(stringResource(if (PhotoDescriptionPolling.isSlow(waitedSeconds)) R.string.photos_description_slow else R.string.photos_description_waiting))
            }
        }
        detail?.faces?.takeIf { faces -> faces.isNotEmpty() }?.let { faces ->
            HorizontalDivider(Modifier.padding(vertical = 12.dp))
            Text(stringResource(R.string.photos_section_people), style = MaterialTheme.typography.titleSmall)
            Text(stringResource(R.string.photos_faces_hint), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            for (face in faces) {
                Row(Modifier.padding(vertical = 6.dp), horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.CenterVertically) {
                    FaceThumbnail(face.facePictureUrl, 40.dp)
                    Text(face.contactName.ifEmpty { stringResource(R.string.photos_unnamed_person) })
                }
            }
        }
    }
}

@Composable
private fun InfoLine(label: String, text: String?) {
    if (text.isNullOrEmpty()) return
    Row(Modifier.fillMaxWidth().padding(vertical = 4.dp), horizontalArrangement = Arrangement.SpaceBetween) {
        Text(label, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Spacer(Modifier.size(16.dp))
        Text(text, modifier = Modifier.weight(1f), textAlign = androidx.compose.ui.text.style.TextAlign.End)
    }
}

@Composable
internal fun FaceThumbnail(path: String, size: androidx.compose.ui.unit.Dp) {
    val picture = rememberServerPicture(path.ifEmpty { null })
    Box(Modifier.size(size).background(MaterialTheme.colorScheme.surfaceVariant, androidx.compose.foundation.shape.CircleShape).androidxClip()) {
        if (picture != null) Image(picture, contentDescription = null, contentScale = ContentScale.Crop, modifier = Modifier.fillMaxSize())
    }
}

private fun Modifier.androidxClip(): Modifier = this.clip(androidx.compose.foundation.shape.CircleShape)
