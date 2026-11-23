package com.teanode.app.photos

import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.teanode.kit.Connection
import com.teanode.kit.photos.Photo
import com.teanode.kit.photos.PhotoCollection
import com.teanode.kit.photos.PhotoPaging
import com.teanode.kit.photos.PhotoPlacement
import com.teanode.kit.photos.photos
import com.teanode.kit.photos.PhotoListQuery
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch

/**
 * Which photos a held list holds: the Trash, laid out in days with their time left, or the results of a search, which
 * are ranked and answered whole. Both are small and need every photo; the rest of the lists are windows.
 */
sealed class PhotoListSource {
    data object Trash : PhotoListSource()

    data class Search(val words: String) : PhotoListSource()

    /** A search lists by relevance and is read in one go; the Trash pages by the oldest photo held. */
    val isPaged: Boolean get() = this !is Search
}

/**
 * PhotoListController holds a whole list of photos, for the Trash and for a search: the first page shows loading, a
 * failure of it shows "try again", a failure of a later page leaves what was read and is told in a snackbar by the
 * screen, and photos a change moved are put back where the server would list them.
 */
@Stable
class PhotoListController(private val scope: CoroutineScope, private val connection: Connection, val source: PhotoListSource) : PhotoListHandle {
    var photos: List<Photo> by mutableStateOf(emptyList())
        private set
    override var isLoading: Boolean by mutableStateOf(false)
        private set
    var isLoadingMore: Boolean by mutableStateOf(false)
        private set
    override var isRefreshing: Boolean by mutableStateOf(false)
        private set
    override var error: Throwable? by mutableStateOf(null)
        private set
    override var moreError: Throwable? by mutableStateOf(null)
        private set
    override var isLoaded: Boolean by mutableStateOf(false)
        private set
    var hasMore: Boolean by mutableStateOf(false)
        private set

    private var pageCursorPhotoId: String? = null
    private var job: Job? = null

    override val totalCount: Int get() = photos.size

    override val heldPhotos: List<Photo> get() = photos

    override fun start() {
        if (!isLoaded && !isLoading) reload(isRefresh = false)
    }

    override fun retry() = reload(isRefresh = false)

    override fun refresh() = reload(isRefresh = isLoaded)

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

    override fun clearMoreError() {
        moreError = null
    }

    private suspend fun readPage(beforePhotoId: String?): List<Photo> = when (source) {
        is PhotoListSource.Trash -> connection.photos(PhotoListQuery(PhotoCollection.Trash), beforePhotoId)
        is PhotoListSource.Search -> connection.photos(source.words)
    }

    /** Puts a photo the person changed (a favorite, a date) where the server would now list it. */
    override fun replace(photo: Photo) {
        val placement = PhotoPlacement.place(photo, photos, pageCursorPhotoId, hasMore)
        photos = placement.photos
        pageCursorPhotoId = placement.pageCursorPhotoId
        if (placement.isBeyondHeldPhotos) loadMore()
    }

    /** Takes photos out of the list (trashed, restored, removed from the album) without asking the server again. */
    override fun remove(photoIds: Collection<String>) {
        val gone = photoIds.toSet()
        photos = photos.filter { photo -> photo.id !in gone }
    }
}

/** What a screen needs of a list of photos, whether the list is held whole or read a window at a time. */
interface PhotoListHandle {
    val isLoading: Boolean
    val isRefreshing: Boolean
    val isLoaded: Boolean
    val error: Throwable?

    /** A failure of a read after the first, told in a snackbar by the screen. */
    val moreError: Throwable?

    /** How many photos the list holds. */
    val totalCount: Int

    /** The photos held now, in the order of the list. */
    val heldPhotos: List<Photo>

    fun start()

    fun retry()

    fun refresh()

    fun clearMoreError()

    /** Puts a photo the person changed where the list holds it. */
    fun replace(photo: Photo)

    /** Takes photos out of the list (trashed, restored, removed from the album) without asking the server again. */
    fun remove(photoIds: Collection<String>)
}
