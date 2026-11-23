package com.teanode.app.photos

import androidx.compose.runtime.Stable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import com.teanode.kit.Connection
import com.teanode.kit.photos.PhotoListQuery
import com.teanode.kit.photos.PhotoPeriod
import com.teanode.kit.photos.PhotoPeriods
import com.teanode.kit.photos.PhotoYearSpan
import com.teanode.kit.photos.countNewer
import com.teanode.kit.photos.overviewPeriod
import com.teanode.kit.photos.overviewYears
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Semaphore
import kotlinx.coroutines.sync.withPermit

/**
 * PhotoOverviewController reads the Years and Months views from the server a period at a time (the dashboard's
 * overview): first the span of years, then each year or month when its tile comes near the screen, at most
 * [MOST_READS_AT_ONCE] reads at once. A tile that leaves the screen before its read starts cancels it.
 */
@Stable
class PhotoOverviewController(private val scope: CoroutineScope, private val connection: Connection, val query: PhotoListQuery) {
    var span: PhotoYearSpan? by mutableStateOf(null)
        private set
    var isLoaded: Boolean by mutableStateOf(false)
        private set
    var error: Throwable? by mutableStateOf(null)
        private set

    /** A failure of one period's read, told in a snackbar by the screen. */
    var readError: Throwable? by mutableStateOf(null)
        private set

    /** The periods read so far by id; a null value is a period the list has no photo in. */
    val periods = mutableStateMapOf<String, PhotoPeriod?>()

    private val readLimit = Semaphore(MOST_READS_AT_ONCE)

    fun start() {
        if (isLoaded) return
        error = null
        scope.launch {
            try {
                span = connection.overviewYears(query)
                isLoaded = true
            } catch (failure: CancellationException) {
                throw failure
            } catch (failure: Throwable) {
                error = failure
            }
        }
    }

    fun clearReadError() {
        readError = null
    }

    /** Reads a year (monthIndex null) or a month when it is not read yet. */
    suspend fun read(year: Int, monthIndex: Int?) {
        val id = PhotoPeriods.id(year, monthIndex)
        if (periods.containsKey(id)) return
        try {
            readLimit.withPermit {
                if (!periods.containsKey(id)) periods[id] = connection.overviewPeriod(query, year, monthIndex)
            }
        } catch (failure: CancellationException) {
            throw failure
        } catch (failure: Throwable) {
            readError = failure
        }
    }

    /** The place in the list of a period's newest photo: how many photos are newer than the whole period. */
    suspend fun countNewer(period: PhotoPeriod): Int = connection.countNewer(query, period)

    companion object {
        const val MOST_READS_AT_ONCE = 6
    }
}
