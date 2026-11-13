package com.teanode.app.photos

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Face
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.PersonAdd
import androidx.compose.material.icons.outlined.PersonOff
import androidx.compose.material3.Button
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.teanode.app.R
import com.teanode.app.session.LocalConnection
import com.teanode.app.ui.components.ConfirmationDialog
import com.teanode.app.ui.components.EmptyState
import com.teanode.app.ui.components.ErrorState
import com.teanode.app.ui.components.LoadedContent
import com.teanode.app.ui.components.LoadingState
import com.teanode.app.ui.components.LocalSnackbar
import com.teanode.app.ui.components.TeaNodeRow
import com.teanode.app.ui.components.TeaNodeScreen
import com.teanode.app.ui.components.rememberLoad
import com.teanode.kit.photos.NamedPhotoFaceGroup
import com.teanode.kit.photos.PhotoFaceGroup
import com.teanode.kit.photos.PhotoLibrary
import com.teanode.kit.photos.PhotoNamingContact
import com.teanode.kit.photos.PhotoNaming
import com.teanode.kit.photos.PhotoTimeline
import com.teanode.kit.photos.createNamedContact
import com.teanode.kit.photos.hidePhotoFaceGroup
import com.teanode.kit.photos.mergePhotoFaceGroups
import com.teanode.kit.photos.namePhotoFaceGroup
import com.teanode.kit.photos.photoFaceGroup
import com.teanode.kit.photos.photoFaceGroups
import com.teanode.kit.photos.photoNamingContacts
import com.teanode.kit.photos.setPhotoFaceRecognition
import com.teanode.kit.photos.unnamePhotoFaceGroup
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/** The People collection (ios/TeaNode/Sources/PhotoPeople.swift): the faces the server found, grouped, a circle for each person. */
@Composable
fun PeopleContent(isHiddenShown: Boolean, library: PhotoLibrary?, onOpenPerson: (PhotoFaceGroup) -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    var isEnabled by remember(library) { mutableStateOf(library?.isPhotoFaceRecognitionEnabled == true) }
    var isStopping by remember { mutableStateOf(false) }
    when {
        library != null && !library.isFaceRecognitionAvailable -> EmptyState(
            Icons.Outlined.PersonOff, stringResource(R.string.photos_faces_unavailable),
            description = library.faceRecognitionUnavailableMessage.ifEmpty { stringResource(R.string.photos_faces_unavailable_text) },
        )
        !isEnabled -> EmptyState(
            Icons.Outlined.Face, stringResource(R.string.photos_faces_off), description = stringResource(R.string.photos_faces_off_text),
            actionLabel = stringResource(R.string.photos_find_faces),
            onAction = { scope.launch { try { connection.setPhotoFaceRecognition(true); isEnabled = true } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) } } },
        )
        else -> {
            val groups = rememberLoad(isHiddenShown) { connection.photoFaceGroups(isHiddenIncluded = isHiddenShown) }
            LoadedContent(
                groups,
                isEmpty = { list -> list.isEmpty() },
                empty = { EmptyState(Icons.Outlined.Face, stringResource(R.string.photos_no_people), description = stringResource(R.string.photos_no_people_text)) },
            ) { list ->
                val visible = if (isHiddenShown) list else list.filter { group -> !group.isHidden }
                if (visible.isEmpty()) {
                    EmptyState(Icons.Outlined.Face, stringResource(R.string.photos_everyone_hidden), description = stringResource(R.string.photos_everyone_hidden_text))
                } else {
                    LazyVerticalGrid(GridCells.Adaptive(104.dp), modifier = Modifier.fillMaxSize(), contentPadding = androidx.compose.foundation.layout.PaddingValues(16.dp)) {
                        items(visible, key = { group -> group.id }) { group -> PersonCell(group) { onOpenPerson(group) } }
                        item(span = { GridItemSpan(maxLineSpan) }) {
                            TextButton(onClick = { isStopping = true }, modifier = Modifier.padding(top = 16.dp)) { Text(stringResource(R.string.photos_stop_faces), color = MaterialTheme.colorScheme.error) }
                        }
                    }
                }
            }
        }
    }
    if (isStopping) {
        ConfirmationDialog(
            title = stringResource(R.string.photos_stop_faces_title), confirmLabel = stringResource(R.string.photos_turn_off_and_delete), isDestructive = true,
            text = stringResource(R.string.photos_stop_faces_text),
            onConfirm = { isStopping = false; scope.launch { try { connection.setPhotoFaceRecognition(false); isEnabled = false } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) } } },
            onDismiss = { isStopping = false },
        )
    }
}

@Composable
private fun PersonCell(group: PhotoFaceGroup, onClick: () -> Unit) {
    Column(Modifier.clickable(onClick = onClick).padding(8.dp), horizontalAlignment = Alignment.CenterHorizontally) {
        FaceThumbnail(group.facePictureUrl, 88.dp)
        Text(
            PhotoNaming.shownName(group) ?: stringResource(R.string.photos_unnamed_person), style = MaterialTheme.typography.bodyMedium, maxLines = 1,
            textAlign = TextAlign.Center, modifier = Modifier.padding(top = 6.dp),
            color = if (group.isNamed) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(pluralStringResource(R.plurals.photos_item_count, group.photoCount, group.photoCount), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** One person: their photos, and naming (a contact from the address book), merging with another group, and hiding. */
@Composable
fun PersonScreen(photoFaceGroupId: String, onBack: () -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    val groupLoad = rememberLoad(photoFaceGroupId) { connection.photoFaceGroup(photoFaceGroupId) }
    val list = rememberPhotoList(PhotoListSource.FaceGroup(photoFaceGroupId))
    var group by remember { mutableStateOf<PhotoFaceGroup?>(null) }
    LaunchedEffect(groupLoad.value) { groupLoad.value?.let { loaded -> group = loaded } }
    var isMenuOpen by remember { mutableStateOf(false) }
    var isNaming by remember { mutableStateOf(false) }
    var isMerging by remember { mutableStateOf(false) }
    var openedPhotoId by remember { mutableStateOf<String?>(null) }
    val current = group

    fun fail(failure: Throwable) { if (failure is CancellationException) throw failure else snackbar.showError(failure) }

    Box(Modifier.fillMaxSize()) {
    TeaNodeScreen(
        title = current?.let { person -> PhotoNaming.shownName(person) } ?: stringResource(R.string.photos_unnamed_person), onBack = onBack,
        actions = {
            if (current != null) Box {
                IconButton(onClick = { isMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.photos_more)) }
                DropdownMenu(expanded = isMenuOpen, onDismissRequest = { isMenuOpen = false }) {
                    DropdownMenuItem(text = { Text(stringResource(R.string.photos_name_person)) }, onClick = { isMenuOpen = false; isNaming = true })
                    if (current.isNamed) DropdownMenuItem(text = { Text(stringResource(R.string.photos_remove_name)) }, onClick = { isMenuOpen = false; scope.launch { try { group = connection.unnamePhotoFaceGroup(current.id) } catch (failure: Throwable) { fail(failure) } } })
                    DropdownMenuItem(text = { Text(stringResource(R.string.photos_merge_with)) }, onClick = { isMenuOpen = false; isMerging = true })
                    DropdownMenuItem(
                        text = { Text(stringResource(if (current.isHidden) R.string.photos_show_in_people else R.string.photos_hide_from_people)) },
                        onClick = { isMenuOpen = false; scope.launch { try { group = connection.hidePhotoFaceGroup(current.id, !current.isHidden) } catch (failure: Throwable) { fail(failure) } } },
                    )
                }
            }
        },
    ) { padding ->
        Box(Modifier.fillMaxSize().padding(padding)) {
            when {
                list.error != null && !list.isLoaded -> ErrorState(stringResource(R.string.photos_cannot_load), onRetry = { list.retry() })
                !list.isLoaded -> LoadingState()
                list.photos.isEmpty() -> EmptyState(Icons.Outlined.Face, stringResource(R.string.photos_no_photos_of_person), description = stringResource(R.string.photos_no_photos_of_person_text))
                else -> Column {
                    if (current?.isInCircles == true) Text(
                        stringResource(R.string.photos_in_your_circles_as, current.account?.let { account -> account.name.ifEmpty { account.username } } ?: ""),
                        style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp),
                    )
                    PhotoGrid(
                        PhotoGridContent.Days(PhotoTimeline.days(list.photos)), emptySet(), false, onOpen = { photo -> openedPhotoId = photo.id },
                        onToggleSelected = {}, onShowed = { photo -> list.showed(photo.id) }, state = rememberLazyGridState(),
                    )
                }
            }
        }
    }
    val opened = list.photos.firstOrNull { photo -> photo.id == openedPhotoId }
            if (opened != null) {
                androidx.activity.compose.BackHandler { openedPhotoId = null }
                PhotoViewer(photos = list.photos, initialPhotoId = opened.id, onClose = { openedPhotoId = null }, library = null, onPhotoChanged = { photo -> list.replace(photo) }, onPhotosGone = { ids -> list.remove(ids) })
            }
    }
    if (isNaming && current != null) {
        NamePersonSheet(
            current, onNamed = { named -> group = named.photoFaceGroup; isNaming = false; if (named.isMerged) { groupLoad.refresh(); list.refresh() } },
            onDismiss = { isNaming = false },
        )
    }
    if (isMerging && current != null) {
        MergeSheet(current, onMerged = { merged -> group = merged; isMerging = false; list.refresh() }, onDismiss = { isMerging = false })
    }
}

/** Choose the contact who is in these photos, or make one; a contact that names another group merges the two. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun NamePersonSheet(group: PhotoFaceGroup, onNamed: (NamedPhotoFaceGroup) -> Unit, onDismiss: () -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    var typed by remember { mutableStateOf("") }
    var contacts by remember { mutableStateOf<List<PhotoNamingContact>?>(null) }
    LaunchedEffect(typed) {
        delay(if (contacts == null) 0 else 300)
        contacts = try { connection.photoNamingContacts(typed) } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure); emptyList() }
    }
    fun choose(contactId: String) {
        scope.launch { try { onNamed(connection.namePhotoFaceGroup(group.id, contactId)) } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) } }
    }
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.padding(bottom = 32.dp)) {
            Text(stringResource(R.string.photos_name_person), style = MaterialTheme.typography.titleLarge, modifier = Modifier.padding(horizontal = 24.dp))
            Text(stringResource(R.string.photos_name_person_text), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 24.dp, vertical = 4.dp))
            OutlinedTextField(value = typed, onValueChange = { typed = it }, singleLine = true, placeholder = { Text(stringResource(R.string.photos_search_contacts)) }, modifier = Modifier.fillMaxWidth().padding(horizontal = 24.dp))
            val found = contacts
            if (found != null && PhotoNaming.offersNewContact(typed, found.map { contact -> contact.shownName })) {
                TeaNodeRow(
                    headline = stringResource(R.string.photos_new_contact, PhotoNaming.trimmedName(typed)), leadingIcon = Icons.Outlined.PersonAdd,
                    onClick = {
                        scope.launch { try { choose(connection.createNamedContact(PhotoNaming.trimmedName(typed)).id) } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) } }
                    },
                )
            }
            if (found != null && found.isEmpty() && typed.isBlank()) Text(stringResource(R.string.photos_no_contacts), modifier = Modifier.padding(24.dp), color = MaterialTheme.colorScheme.onSurfaceVariant)
            Column(Modifier.verticalScroll(rememberScrollState()).heightIn(max = 280.dp)) {
                for (contact in found.orEmpty()) {
                    TeaNodeRow(headline = contact.shownName, supporting = if (contact.id == group.contactId) stringResource(R.string.photos_current_name) else null, onClick = { choose(contact.id) })
                }
            }
            Text(stringResource(R.string.photos_name_merges_hint), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 24.dp, vertical = 8.dp))
        }
    }
}

/** Choose the person who is the same as this one: their photos are shown together as one person. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun MergeSheet(group: PhotoFaceGroup, onMerged: (PhotoFaceGroup) -> Unit, onDismiss: () -> Unit) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val scope = rememberCoroutineScope()
    val others = rememberLoad { connection.photoFaceGroups(isHiddenIncluded = true).filter { other -> other.id != group.id } }
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.padding(bottom = 32.dp)) {
            Text(stringResource(R.string.photos_merge_with), style = MaterialTheme.typography.titleLarge, modifier = Modifier.padding(horizontal = 24.dp))
            Text(stringResource(R.string.photos_merge_text), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(horizontal = 24.dp, vertical = 4.dp))
            LoadedContent(others, modifier = Modifier.padding(top = 8.dp), isEmpty = { list -> list.isEmpty() }, empty = { Text(stringResource(R.string.photos_nobody_to_merge), modifier = Modifier.padding(24.dp)) }) { list ->
                Column {
                    for (other in list) {
                        TeaNodeRow(
                            headline = PhotoNaming.shownName(other) ?: stringResource(R.string.photos_unnamed_person),
                            supporting = pluralStringResource(R.plurals.photos_item_count, other.photoCount, other.photoCount),
                            leading = { FaceThumbnail(other.facePictureUrl, 40.dp) },
                            onClick = { scope.launch { try { onMerged(connection.mergePhotoFaceGroups(listOf(group.id, other.id))) } catch (failure: CancellationException) { throw failure } catch (failure: Throwable) { snackbar.showError(failure) } } },
                        )
                    }
                }
            }
        }
    }
}
