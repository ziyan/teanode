package com.teanode.app.photos

import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.teanode.kit.Connection
import com.teanode.kit.photos.Photo
import com.teanode.kit.photos.PhotoListQuery
import com.teanode.kit.photos.PhotoWindow
import com.teanode.kit.photos.countPhotos
import com.teanode.kit.photos.photosAt
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch

/**
 * PhotoWindowController holds the part of a photo list near the screen (the iPhone app's PhotoWindow model): it asks
 * the server how many photos the list has, and reads a page by its number when a place of it comes near the screen.
 * Only one read of a page runs at a time, and pages far from the screen are let go. The first page shows loading, a
 * failure of it shows "try again", and a failure of a later page is told in a snackbar by the screen.
 */
@Stable
class PhotoWindowController(private val scope: CoroutineScope, private val connection: Connection, val query: PhotoListQuery) : PhotoListHandle {
    var window: PhotoWindow by mutableStateOf(PhotoWindow())
        private set
    override var isLoading: Boolean by mutableStateOf(false)
        private set
    override var isRefreshing: Boolean by mutableStateOf(false)
        private set
    override var error: Throwable? by mutableStateOf(null)
        private set
    override var moreError: Throwable? by mutableStateOf(null)
        private set
    override var isLoaded: Boolean by mutableStateOf(false)
        private set

    override val totalCount: Int get() = window.totalCount

    override val heldPhotos: List<Photo> get() = window.heldPhotos

    private var job: Job? = null
    private var generation = 0
    private var shownIndex = 0
    private val readingPageIndexes = mutableSetOf<Int>()

    override fun start() {
        if (!isLoaded && !isLoading) reload(isRefresh = false)
    }

    override fun retry() = reload(isRefresh = false)

    override fun refresh() = reload(isRefresh = isLoaded)

    private fun reload(isRefresh: Boolean) {
        job?.cancel()
        generation += 1
        val readGeneration = generation
        readingPageIndexes.clear()
        if (isRefresh) isRefreshing = true else isLoading = true
        error = null
        job = scope.launch {
            try {
                val totalCount = connection.countPhotos(query)
                val firstPage = if (totalCount > 0) connection.photosAt(query, 0, window.pageSize) else emptyList()
                // What is held stays on screen while a refresh reads it again; a first read starts from nothing.
                val base = if (isRefresh) window.withTotalCount(totalCount).markingAllStale() else PhotoWindow(totalCount)
                window = if (totalCount > 0) base.withPage(0, firstPage) else base
                isLoaded = true
                moreError = null
                readPagesNear(shownIndex)
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                if (isRefresh) moreError = failure else error = failure
            } finally {
                if (readGeneration == generation) {
                    isLoading = false
                    isRefreshing = false
                }
            }
        }
    }

    /** A place of the list is on screen: read the pages near it, and let go of the ones far from it. */
    fun showed(index: Int) {
        shownIndex = index
        if (isLoaded) readPagesNear(index)
    }

    private fun readPagesNear(index: Int) {
        window = window.releasing(window.pagesToRelease(index))
        for (pageIndex in window.missingPages(index)) {
            if (!readingPageIndexes.add(pageIndex)) continue
            val readGeneration = generation
            scope.launch {
                try {
                    val page = connection.photosAt(query, pageIndex * window.pageSize, window.pageSize)
                    if (readGeneration != generation) return@launch
                    window = window.withPage(pageIndex, page).let { read -> read.releasing(read.pagesToRelease(shownIndex)) }
                } catch (failure: CancellationException) {
                    throw failure
                } catch (failure: Throwable) {
                    if (readGeneration == generation) moreError = failure
                } finally {
                    if (readGeneration == generation) readingPageIndexes.remove(pageIndex)
                }
            }
        }
    }

    override fun clearMoreError() {
        moreError = null
    }

    /** A photo the person changed takes its place; a new date may move it, so the held pages are read again. */
    override fun replace(photo: Photo) {
        val held = window.heldPhotos.firstOrNull { candidate -> candidate.id == photo.id }
        window = window.replace(photo)
        if (held != null && (held.takenAt != photo.takenAt || held.takenAtOffsetMinutes != photo.takenAtOffsetMinutes)) {
            window = window.markingAllStale()
            readPagesNear(shownIndex)
        }
    }

    /** Photos taken out of the list: the count goes down at once and the held pages are read again. */
    override fun remove(photoIds: Collection<String>) {
        window = window.remove(photoIds)
        readPagesNear(shownIndex)
        scope.launch {
            try {
                val totalCount = connection.countPhotos(query)
                window = window.withTotalCount(totalCount)
            } catch (failure: CancellationException) {
                throw failure
            } catch (_: Throwable) {
                // The count was already lowered by what was removed; the next refresh sets it right.
            }
        }
    }
}
