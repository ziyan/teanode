package com.teanode.app.photos

import com.teanode.kit.photos.PhotoFilter
import java.time.Instant

/**
 * PhotoFilterRequest is how another part of the app opens Photos at a filter, as the iPhone app's `openedPhotoFilter`
 * does: Location opens it at the time of a visit. The library takes the request once when it opens, and it is gone
 * after that, so coming back to Photos later does not show an old filter.
 */
object PhotoFilterRequest {
    private var pending: PhotoFilter? = null

    /** Asks for Photos to open at the photos taken from one moment to another. */
    fun open(takenAfter: Instant?, takenBefore: Instant?) {
        pending = PhotoFilter(takenAfter = takenAfter, takenBefore = takenBefore)
    }

    /** The filter asked for, once. */
    fun take(): PhotoFilter? = pending.also { pending = null }
}
