package com.teanode.app.photos

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.grid.rememberLazyGridState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.DeleteForever
import androidx.compose.material.icons.outlined.Favorite
import androidx.compose.material.icons.outlined.FilterList
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.PhotoAlbum
import androidx.compose.material.icons.outlined.PhotoLibrary
import androidx.compose.material.icons.outlined.Restore
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.CloudUpload
import androidx.compose.material.icons.outlined.ArrowDropDown
import androidx.compose.material.icons.outlined.People
import androidx.compose.material.icons.outlined.PlaylistAdd
import androidx.compose.material3.AssistChip
import androidx.compose.material3.BottomAppBar
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
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
import com.teanode.app.session.LocalAppSession
import com.teanode.kit.AppSection
import com.teanode.kit.photos.Photo
import com.teanode.kit.photos.PhotoCollection
import com.teanode.kit.photos.PhotoFilter
import com.teanode.kit.photos.PhotoOverview
import com.teanode.kit.photos.PhotoLibrary
import com.teanode.kit.photos.PhotoTimeline
import com.teanode.kit.photos.emptyPhotoTrash
import com.teanode.kit.photos.deletePhotos
import com.teanode.kit.photos.photoLibrary
import com.teanode.kit.photos.restorePhotos
import com.teanode.kit.photos.trashPhotos
import kotlinx.coroutines.launch

/** The parts of the person's library, switched from the menu beside the title. */
enum class PhotoSectionChoice(val collection: PhotoCollection?) {
    Library(PhotoCollection.Library),
    Favorites(PhotoCollection.Favorites),
    Albums(null),
    People(null),
    Trash(PhotoCollection.Trash),
}

@Composable
private fun PhotoSectionChoice.title(): String = stringResource(
    when (this) {
        PhotoSectionChoice.Library -> R.string.photos_section_library
        PhotoSectionChoice.Favorites -> R.string.photos_section_favorites
        PhotoSectionChoice.Albums -> R.string.photos_section_albums
        PhotoSectionChoice.People -> R.string.photos_section_people
        PhotoSectionChoice.Trash -> R.string.photos_section_trash
    },
)

/**
 * The first screen of Photos (ios/TeaNode/Sources/PhotosView.swift): the library in days, with its years and months
 * views, search, filters, choosing photos to act on, and the other collections (favorites, albums, people, trash).
 */
@Composable
fun PhotosSection(
    initialSection: PhotoSectionChoice = PhotoSectionChoice.Library,
    initialScale: PhotoScale = PhotoScale.All,
    initialOpenedPhotoId: String? = null,
    initialIsSelecting: Boolean = false,
    initialSelectedIds: Set<String> = emptySet(),
) {
    val connection = LocalConnection.current
    val snackbar = LocalSnackbar.current
    val navigator = LocalAppNavigator.current
    val scope = rememberCoroutineScope()
    val context = androidx.compose.ui.platform.LocalContext.current
    var section by rememberSaveable { mutableStateOf(initialSection) }
    var scale by rememberSaveable { mutableStateOf(initialScale) }
    var filter by remember { mutableStateOf(PhotoFilterRequest.take() ?: PhotoFilter()) }
    var isSearching by rememberSaveable { mutableStateOf(false) }
    var searchText by rememberSaveable { mutableStateOf("") }
    var isSelecting by rememberSaveable { mutableStateOf(initialIsSelecting) }
    var selectedIds by remember { mutableStateOf(initialSelectedIds) }
    var openedPhotoId by rememberSaveable { mutableStateOf(initialOpenedPhotoId) }
    var isSectionMenuOpen by remember { mutableStateOf(false) }
    var isMoreMenuOpen by remember { mutableStateOf(false) }
    var isFilterOpen by remember { mutableStateOf(false) }
    var isEmptyTrashConfirming by remember { mutableStateOf(false) }
    var isDeleteConfirming by remember { mutableStateOf(false) }
    var isTrashConfirming by remember { mutableStateOf(false) }
    var isAlbumChoosing by remember { mutableStateOf(false) }
    var isHiddenShown by remember { mutableStateOf(false) }
    val libraryLoad = com.teanode.app.ui.components.rememberLoad { runCatching { connection.photoLibrary() }.getOrNull() }
    val library = libraryLoad.value

    val searchWords = com.teanode.kit.photos.PhotoSearch.words(searchText).takeIf { section == PhotoSectionChoice.Library && isSearching }
    val source: PhotoListSource? = when {
        searchWords != null -> PhotoListSource.Search(searchWords)
        section.collection != null -> PhotoListSource.Collection(section.collection!!, if (section == PhotoSectionChoice.Library) filter else PhotoFilter())
        else -> null
    }
    val list = source?.let { current -> rememberPhotoList(current) }
    val moreError = list?.moreError
    LaunchedEffect(moreError) {
        if (moreError != null) {
            list.clearMoreError()
            snackbar.showError(moreError)
        }
    }
    var hasShownSection by remember { mutableStateOf(false) }
    LaunchedEffect(section) {
        // Switching collections ends choosing; the first draw of a screen opened already choosing keeps it.
        if (hasShownSection) { isSelecting = false; selectedIds = emptySet() }
        hasShownSection = true
    }
    BackHandler(enabled = isSelecting || openedPhotoId != null || isSearching) {
        when {
            openedPhotoId != null -> openedPhotoId = null
            isSelecting -> {
                isSelecting = false
                selectedIds = emptySet()
            }
            else -> {
                isSearching = false
                searchText = ""
            }
        }
    }

    fun toggle(photo: Photo) {
        isSelecting = true
        selectedIds = if (photo.id in selectedIds) selectedIds - photo.id else selectedIds + photo.id
    }

    fun act(isRestoring: Boolean = false, isDeleting: Boolean = false, successMessageId: Int) {
        val ids = selectedIds.toList()
        scope.launch {
            try {
                when {
                    isRestoring -> connection.restorePhotos(ids)
                    isDeleting -> connection.deletePhotos(ids)
                    else -> connection.trashPhotos(ids)
                }
                list?.remove(ids)
                isSelecting = false
                selectedIds = emptySet()
                libraryLoad.refresh()
                snackbar.show(context.resources.getQuantityString(successMessageId, ids.size, ids.size))
            } catch (failure: kotlinx.coroutines.CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                snackbar.showError(failure)
            }
        }
    }

    Box(Modifier.fillMaxSize()) {
    val title = if (isSelecting) stringResource(R.string.photos_selected_count, selectedIds.size) else section.title()
    TeaNodeScreen(
        title = title,
        onBack = if (isSelecting) ({ isSelecting = false; selectedIds = emptySet() }) else null,
        actions = {
            if (isSelecting) {
                TextButton(onClick = { selectedIds = list?.photos?.map { photo -> photo.id }?.toSet().orEmpty() }) { Text(stringResource(R.string.photos_select_all)) }
            } else {
                if (section == PhotoSectionChoice.Library) {
                    IconButton(onClick = { isSearching = !isSearching; if (!isSearching) searchText = "" }) {
                        Icon(Icons.Outlined.Search, contentDescription = stringResource(R.string.photos_search))
                    }
                    IconButton(onClick = { isFilterOpen = true }) {
                        Icon(Icons.Outlined.FilterList, contentDescription = stringResource(R.string.photos_filter), tint = if (filter.isActive) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                Box {
                    IconButton(onClick = { isSectionMenuOpen = true }) { Icon(Icons.Outlined.ArrowDropDown, contentDescription = stringResource(R.string.photos_collections)) }
                    DropdownMenu(expanded = isSectionMenuOpen, onDismissRequest = { isSectionMenuOpen = false }) {
                        for (choice in PhotoSectionChoice.entries) {
                            if (choice == PhotoSectionChoice.People && library != null && !library.isFaceRecognitionAvailable && !library.isPhotoFaceRecognitionEnabled) continue
                            DropdownMenuItem(
                                text = { Text(choice.title(), color = if (choice == section) MaterialTheme.colorScheme.primary else MaterialTheme.colorScheme.onSurface) },
                                leadingIcon = { Icon(choice.icon(), contentDescription = null) },
                                onClick = { isSectionMenuOpen = false; section = choice },
                            )
                        }
                    }
                }
                Box {
                    IconButton(onClick = { isMoreMenuOpen = true }) { Icon(Icons.Outlined.MoreVert, contentDescription = stringResource(R.string.photos_more)) }
                    DropdownMenu(expanded = isMoreMenuOpen, onDismissRequest = { isMoreMenuOpen = false }) {
                        if (section.collection != null && (list?.photos?.isNotEmpty() == true)) {
                            DropdownMenuItem(text = { Text(stringResource(R.string.photos_select)) }, onClick = { isMoreMenuOpen = false; isSelecting = true })
                        }
                        if (section == PhotoSectionChoice.Trash && (list?.photos?.isNotEmpty() == true)) {
                            DropdownMenuItem(text = { Text(stringResource(R.string.photos_empty_trash)) }, onClick = { isMoreMenuOpen = false; isEmptyTrashConfirming = true })
                        }
                        if (section == PhotoSectionChoice.People) {
                            DropdownMenuItem(
                                text = { Text(if (isHiddenShown) stringResource(R.string.photos_hide_hidden) else stringResource(R.string.photos_show_hidden)) },
                                onClick = { isMoreMenuOpen = false; isHiddenShown = !isHiddenShown },
                            )
                        }
                        DropdownMenuItem(
                            text = { Text(stringResource(R.string.photos_backup_menu)) },
                            leadingIcon = { Icon(Icons.Outlined.CloudUpload, contentDescription = null) },
                            onClick = { isMoreMenuOpen = false; navigator.navigate(Routes.child(AppSection.Photos, "backup")) },
                        )
                    }
                }
            }
        },
        bottomBar = {
            if (isSelecting) {
                SelectionBar(
                    section = section, count = selectedIds.size,
                    onTrash = { isTrashConfirming = true }, onRestore = { act(isRestoring = true, successMessageId = R.plurals.photos_restored) },
                    onDelete = { isDeleteConfirming = true }, onAddToAlbum = { isAlbumChoosing = true },
                )
            } else if (section == PhotoSectionChoice.Library && searchWords == null) {
                ScaleBar(scale) { chosen -> scale = chosen }
            }
        },
    ) { contentPadding ->
        Box(Modifier.fillMaxSize().padding(contentPadding)) {
            Column(Modifier.fillMaxSize()) {
                if (isSearching && section == PhotoSectionChoice.Library) {
                    OutlinedTextField(
                        value = searchText, onValueChange = { searchText = it }, singleLine = true,
                        placeholder = { Text(stringResource(R.string.photos_search_prompt)) },
                        modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
                    )
                }
                FilterChips(filter, section, searchWords) { filter = it }
                Box(Modifier.weight(1f)) {
                    when (section) {
                        PhotoSectionChoice.Albums -> AlbumsContent(onOpenAlbum = { album -> navigator.navigate(Routes.child(AppSection.Photos, "album/${album.id}")) })
                        PhotoSectionChoice.People -> PeopleContent(isHiddenShown, library, onOpenPerson = { group -> navigator.navigate(Routes.child(AppSection.Photos, "person/${group.id}")) })
                        else -> if (list != null) LibraryList(list, section, scale, library, searchWords, selectedIds, isSelecting, filter, { photo -> openedPhotoId = photo.id }, ::toggle) { scale = PhotoScale.All }
                    }
                }
            }
        }
    }
    val opened = list?.photos?.firstOrNull { photo -> photo.id == openedPhotoId }
            if (opened != null && list != null) {
                PhotoViewer(
                    photos = list.photos, initialPhotoId = opened.id, onClose = { openedPhotoId = null }, library = library,
                    onPhotoChanged = { photo -> list.replace(photo) },
                    onPhotosGone = { ids -> list.remove(ids); libraryLoad.refresh() },
                    onShowNear = { photo -> com.teanode.kit.photos.PhotoNearFilter.near(photo)?.let { near -> section = PhotoSectionChoice.Library; filter = PhotoFilter(photoNearFilter = near); openedPhotoId = null } },
                )
            }
    }
    if (isFilterOpen) PhotoFilterSheet(filter, onApply = { applied -> filter = applied; isFilterOpen = false }, onDismiss = { isFilterOpen = false })
    if (isTrashConfirming) {
        ConfirmationDialog(
            title = stringResource(R.string.photos_trash_selected_title), confirmLabel = stringResource(R.string.photos_move_to_trash), isDestructive = true,
            text = stringResource(R.string.photos_trash_selected_text, library?.trashRetentionDays ?: 30),
            onConfirm = { isTrashConfirming = false; act(successMessageId = R.plurals.photos_trashed) }, onDismiss = { isTrashConfirming = false },
        )
    }
    if (isDeleteConfirming) {
        ConfirmationDialog(
            title = stringResource(R.string.photos_delete_selected_title), confirmLabel = stringResource(R.string.photos_delete_forever), isDestructive = true,
            text = stringResource(R.string.photos_cannot_be_undone),
            onConfirm = { isDeleteConfirming = false; act(isDeleting = true, successMessageId = R.plurals.photos_deleted) }, onDismiss = { isDeleteConfirming = false },
        )
    }
    if (isEmptyTrashConfirming) {
        ConfirmationDialog(
            title = stringResource(R.string.photos_empty_trash_title), confirmLabel = stringResource(R.string.photos_empty_trash), isDestructive = true,
            text = stringResource(R.string.photos_cannot_be_undone),
            onConfirm = {
                isEmptyTrashConfirming = false
                scope.launch {
                    try {
                        connection.emptyPhotoTrash()
                        list?.retry()
                        libraryLoad.refresh()
                    } catch (failure: kotlinx.coroutines.CancellationException) {
                        throw failure
                    } catch (failure: Throwable) {
                        snackbar.showError(failure)
                    }
                }
            },
            onDismiss = { isEmptyTrashConfirming = false },
        )
    }
    if (isAlbumChoosing) {
        AddToAlbumSheet(
            photoIds = selectedIds.toList(), onDone = { isAlbumChoosing = false; isSelecting = false; selectedIds = emptySet() }, onDismiss = { isAlbumChoosing = false },
        )
    }
}

@Composable
internal fun rememberPhotoList(source: PhotoListSource): PhotoListController {
    val connection = LocalConnection.current
    val scope = rememberCoroutineScope()
    val controller = remember(source, connection) { PhotoListController(scope, connection, source) }
    LaunchedEffect(controller) { controller.start() }
    return controller
}

private fun PhotoSectionChoice.icon() = when (this) {
    PhotoSectionChoice.Library -> Icons.Outlined.PhotoLibrary
    PhotoSectionChoice.Favorites -> Icons.Outlined.Favorite
    PhotoSectionChoice.Albums -> Icons.Outlined.PhotoAlbum
    PhotoSectionChoice.People -> Icons.Outlined.People
    PhotoSectionChoice.Trash -> Icons.Outlined.Delete
}

@Composable
private fun ScaleBar(scale: PhotoScale, onChange: (PhotoScale) -> Unit) {
    Box(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), contentAlignment = Alignment.Center) {
        SingleChoiceSegmentedButtonRow {
            for ((index, choice) in PhotoScale.entries.withIndex()) {
                SegmentedButton(
                    selected = choice == scale, onClick = { onChange(choice) }, shape = SegmentedButtonDefaults.itemShape(index, PhotoScale.entries.size),
                ) {
                    Text(stringResource(when (choice) { PhotoScale.Years -> R.string.photos_scale_years; PhotoScale.Months -> R.string.photos_scale_months; PhotoScale.All -> R.string.photos_scale_all }))
                }
            }
        }
    }
}

@Composable
private fun SelectionBar(
    section: PhotoSectionChoice,
    count: Int,
    onTrash: () -> Unit,
    onRestore: () -> Unit,
    onDelete: () -> Unit,
    onAddToAlbum: () -> Unit,
) {
    BottomAppBar {
        Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceEvenly) {
            if (section == PhotoSectionChoice.Trash) {
                TextButton(onClick = onRestore, enabled = count > 0) {
                    Icon(Icons.Outlined.Restore, contentDescription = null); Text("  " + stringResource(R.string.photos_restore))
                }
                TextButton(onClick = onDelete, enabled = count > 0) {
                    Icon(Icons.Outlined.DeleteForever, contentDescription = null, tint = MaterialTheme.colorScheme.error)
                    Text("  " + stringResource(R.string.photos_delete_forever), color = MaterialTheme.colorScheme.error)
                }
            } else {
                TextButton(onClick = onAddToAlbum, enabled = count > 0) {
                    Icon(Icons.Outlined.PlaylistAdd, contentDescription = null); Text("  " + stringResource(R.string.photos_add_to_album))
                }
                TextButton(onClick = onTrash, enabled = count > 0) {
                    Icon(Icons.Outlined.Delete, contentDescription = null, tint = MaterialTheme.colorScheme.error)
                    Text("  " + stringResource(R.string.photos_move_to_trash), color = MaterialTheme.colorScheme.error)
                }
            }
        }
    }
}

@Composable
private fun FilterChips(filter: PhotoFilter, section: PhotoSectionChoice, searchWords: String?, onChange: (PhotoFilter) -> Unit) {
    if (section != PhotoSectionChoice.Library || searchWords != null || !filter.isActive) return
    androidx.compose.foundation.lazy.LazyRow(contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        item {
            AssistChip(
                onClick = { onChange(PhotoFilter()) }, label = { Text(stringResource(R.string.photos_clear_filters)) },
                leadingIcon = { Icon(Icons.Filled.Close, contentDescription = null) },
            )
        }
        filter.photoNearFilter?.let { near ->
            item {
                AssistChip(
                    onClick = { onChange(filter.copy(photoNearFilter = null)) },
                    label = { Text(near.placeName.ifEmpty { stringResource(R.string.photos_near_here) }) },
                    leadingIcon = { Icon(Icons.Filled.Close, contentDescription = null) },
                )
            }
        }
        if (filter.photoKindFilter != com.teanode.kit.photos.PhotoKindFilter.All) {
            item { AssistChip(onClick = { onChange(filter.copy(photoKindFilter = com.teanode.kit.photos.PhotoKindFilter.All)) }, label = { Text(kindFilterName(filter.photoKindFilter)) }, leadingIcon = { Icon(Icons.Filled.Close, contentDescription = null) }) }
        }
        if (filter.firstDay != null || filter.lastDay != null) {
            item { AssistChip(onClick = { onChange(filter.copy(firstDay = null, lastDay = null)) }, label = { Text(listOfNotNull(filter.firstDay, filter.lastDay).joinToString(" - ")) }, leadingIcon = { Icon(Icons.Filled.Close, contentDescription = null) }) }
        }
        if (!filter.accountUserId.isNullOrEmpty()) {
            item { AssistChip(onClick = { onChange(filter.copy(accountUserId = null, accountName = "")) }, label = { Text(filter.accountName) }, leadingIcon = { Icon(Icons.Filled.Close, contentDescription = null) }) }
        }
    }
}

@Composable
internal fun kindFilterName(kind: com.teanode.kit.photos.PhotoKindFilter): String = stringResource(
    when (kind) {
        com.teanode.kit.photos.PhotoKindFilter.All -> R.string.photos_kind_all
        com.teanode.kit.photos.PhotoKindFilter.Picture -> R.string.photos_kind_photo
        com.teanode.kit.photos.PhotoKindFilter.LivePhoto -> R.string.photos_kind_live_photo
        com.teanode.kit.photos.PhotoKindFilter.Video -> R.string.photos_kind_video
    },
)

@Composable
private fun LibraryList(
    list: PhotoListController,
    section: PhotoSectionChoice,
    scale: PhotoScale,
    library: PhotoLibrary?,
    searchWords: String?,
    selectedIds: Set<String>,
    isSelecting: Boolean,
    filter: PhotoFilter,
    onOpen: (Photo) -> Unit,
    onToggle: (Photo) -> Unit,
    onShowAll: () -> Unit,
) {
    val gridState = rememberLazyGridState()
    val photos = list.photos
    when {
        list.error != null && !list.isLoaded -> ErrorState(stringResource(R.string.photos_cannot_load), onRetry = { list.retry() })
        !list.isLoaded -> LoadingState()
        photos.isEmpty() -> PhotosEmpty(section, filter.isActive, searchWords != null, library)
        else -> androidx.compose.material3.pulltorefresh.PullToRefreshBox(isRefreshing = list.isRefreshing, onRefresh = { list.refresh() }, modifier = Modifier.fillMaxSize()) {
            val footer: @Composable () -> Unit = {
                if (list.isLoadingMore) Box(Modifier.fillMaxWidth().padding(16.dp), contentAlignment = Alignment.Center) { androidx.compose.material3.CircularProgressIndicator() }
                else if (list.moreError != null) TextButton(onClick = { list.loadMore() }) { Text(stringResource(R.string.photos_try_again)) }
            }
            when {
                section == PhotoSectionChoice.Library && searchWords == null && scale == PhotoScale.Years ->
                    PhotoOverviewGrid(PhotoOverview.years(photos), null, onOpenYear = { onShowAll() }, onOpenMonth = {})
                section == PhotoSectionChoice.Library && searchWords == null && scale == PhotoScale.Months ->
                    PhotoOverviewGrid(null, PhotoOverview.months(photos), onOpenYear = {}, onOpenMonth = { onShowAll() })
                section == PhotoSectionChoice.Trash -> PhotoGrid(
                    PhotoGridContent.TrashDays(PhotoTimeline.trashDays(photos, library?.trashRetentionDays ?: 30), library?.trashRetentionDays ?: 30),
                    selectedIds, isSelecting, onOpen, onToggle, { photo -> list.showed(photo.id) }, state = gridState, footer = footer,
                )
                else -> PhotoGrid(
                    PhotoGridContent.Days(if (searchWords != null) PhotoTimeline.rankedDay(photos) else PhotoTimeline.days(photos)),
                    selectedIds, isSelecting, onOpen, onToggle, { photo -> list.showed(photo.id) }, state = gridState, footer = footer,
                )
            }
        }
    }
}

@Composable
private fun PhotosEmpty(section: PhotoSectionChoice, isFiltered: Boolean, isSearch: Boolean, library: PhotoLibrary?) {
    val navigator = LocalAppNavigator.current
    when {
        isSearch -> EmptyState(Icons.Outlined.Search, stringResource(R.string.photos_empty_search))
        isFiltered -> EmptyState(Icons.Outlined.FilterList, stringResource(R.string.photos_empty_filtered))
        section == PhotoSectionChoice.Favorites -> EmptyState(Icons.Outlined.Favorite, stringResource(R.string.photos_empty_favorites), description = stringResource(R.string.photos_empty_favorites_text))
        section == PhotoSectionChoice.Trash -> EmptyState(
            Icons.Outlined.Delete, stringResource(R.string.photos_empty_trash_title),
            description = stringResource(R.string.photos_empty_trash_text, library?.trashRetentionDays ?: 30),
        )
        else -> EmptyState(
            Icons.Outlined.PhotoLibrary, stringResource(R.string.photos_empty_library), description = stringResource(R.string.photos_empty_library_text),
            actionLabel = stringResource(R.string.photos_backup_menu), onAction = { navigator.navigate(Routes.child(AppSection.Photos, "backup")) },
        )
    }
}
