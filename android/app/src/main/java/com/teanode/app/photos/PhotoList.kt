package com.teanode.app.photos

import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.teanode.kit.Connection
import com.teanode.kit.photos.Photo
import com.teanode.kit.photos.PhotoCollection
import com.teanode.kit.photos.PhotoFilter
import com.teanode.kit.photos.PhotoPaging
import com.teanode.kit.photos.PhotoPlacement
import com.teanode.kit.photos.photos
import com.teanode.kit.photos.photosInAlbum
import com.teanode.kit.photos.photosInFaceGroup
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch

/** Which photos a list holds: a part of the library, the results of a search, an album, or the photos of one person. */
sealed class PhotoListSource {
    data class Collection(val photoCollection: PhotoCollection, val filter: PhotoFilter = PhotoFilter()) : PhotoListSource()

    data class Search(val words: String) : PhotoListSource()

    data class Album(val photoAlbumId: String) : PhotoListSource()

    data class FaceGroup(val photoFaceGroupId: String) : PhotoListSource()

    /** A search lists by relevance and is read in one go; the others page by the oldest photo held. */
    val isPaged: Boolean get() = this !is Search
}

/**
 * PhotoListController reads a list of photos a page at a time, as the iPhone app's PhotosModel does: the first page
 * shows loading, a failure of it shows "try again", a failure of a later page leaves what was read and is told in a
 * snackbar by the screen, and photos a change moved are put back where the server would list them.
 */
@Stable
class PhotoListController(private val scope: CoroutineScope, private val connection: Connection, val source: PhotoListSource) {
    var photos: List<Photo> by mutableStateOf(emptyList())
        private set
    var isLoading: Boolean by mutableStateOf(false)
        private set
    var isLoadingMore: Boolean by mutableStateOf(false)
        private set
    var isRefreshing: Boolean by mutableStateOf(false)
        private set
    var error: Throwable? by mutableStateOf(null)
        private set
    var moreError: Throwable? by mutableStateOf(null)
        private set
    var isLoaded: Boolean by mutableStateOf(false)
        private set
    var hasMore: Boolean by mutableStateOf(false)
        private set

    private var pageCursorPhotoId: String? = null
    private var job: Job? = null

    fun start() {
        if (!isLoaded && !isLoading) reload(isRefresh = false)
    }

    fun retry() = reload(isRefresh = false)

    fun refresh() = reload(isRefresh = isLoaded)

    private fun reload(isRefresh: Boolean) {
        job?.cancel()
        if (isRefresh) isRefreshing = true else isLoading = true
        error = null
        job = scope.launch {
            try {
                val page = readPage(null)
                photos = page
                pageCursorPhotoId = page.lastOrNull()?.id
                hasMore = source.isPaged && PhotoPaging.hasMore(page.size)
                isLoaded = true
                moreError = null
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                if (isRefresh) moreError = failure else error = failure
            } finally {
                isLoading = false
                isRefreshing = false
            }
        }
    }

    /** Asks for the next page when the cell on screen is near the end of what is held. */
    fun showed(photoId: String) {
        if (hasMore && !isLoadingMore && moreError == null && PhotoPaging.shouldLoadMore(photoId, photos)) loadMore()
    }

    fun loadMore() {
        val cursor = pageCursorPhotoId ?: return
        if (isLoadingMore || !hasMore) return
        isLoadingMore = true
        moreError = null
        scope.launch {
            try {
                val page = readPage(cursor)
                photos = PhotoPaging.appending(page, photos)
                pageCursorPhotoId = page.lastOrNull()?.id ?: cursor
                hasMore = PhotoPaging.hasMore(page.size)
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                moreError = failure
            } finally {
                isLoadingMore = false
            }
        }
    }

    fun clearMoreError() {
        moreError = null
    }

    private suspend fun readPage(beforePhotoId: String?): List<Photo> = when (source) {
        is PhotoListSource.Collection -> connection.photos(source.photoCollection, source.filter, beforePhotoId)
        is PhotoListSource.Search -> connection.photos(source.words)
        is PhotoListSource.Album -> connection.photosInAlbum(source.photoAlbumId, beforePhotoId)
        is PhotoListSource.FaceGroup -> connection.photosInFaceGroup(source.photoFaceGroupId, beforePhotoId)
    }

    /** Puts a photo the person changed (a favorite, a date) where the server would now list it. */
    fun replace(photo: Photo) {
        val placement = PhotoPlacement.place(photo, photos, pageCursorPhotoId, hasMore)
        photos = placement.photos
        pageCursorPhotoId = placement.pageCursorPhotoId
        if (placement.isBeyondHeldPhotos) loadMore()
    }

    /** Takes photos out of the list (trashed, restored, removed from the album) without asking the server again. */
    fun remove(photoIds: Collection<String>) {
        val gone = photoIds.toSet()
        photos = photos.filter { photo -> photo.id !in gone }
    }
}
