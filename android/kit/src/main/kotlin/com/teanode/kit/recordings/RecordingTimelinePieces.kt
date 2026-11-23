package com.teanode.kit.recordings

/**
 * A recording is one timeline made of several audio files (a pause or a call starts a new one). These find which file
 * holds a point of the timeline and where in it.
 */
object RecordingTimelinePieces {
    data class Location(val media: RecordingMedia, val offsetMS: Long, val timelineMS: Long)

    data class BoundedLocation(val start: Location, val endOffsetMS: Long) {
        val endTimelineMS: Long get() = start.media.timelineStartMS + endOffsetMS
    }

    /** The files that are on the server complete, in timeline order. */
    fun playable(mediaFiles: List<RecordingMedia>): List<RecordingMedia> =
        mediaFiles.filter { media -> media.recordingUploadState == "complete" }.sortedBy { media -> media.timelineStartMS }

    /** Where a file ends on the timeline: by its length, or where the next begins when the length is not known. */
    fun endMS(media: RecordingMedia, pieces: List<RecordingMedia>): Long {
        if (media.mediaDurationMS > 0) return media.timelineStartMS + media.mediaDurationMS
        val following = pieces.firstOrNull { piece -> piece.timelineStartMS > media.timelineStartMS }
        return following?.timelineStartMS ?: media.timelineStartMS
    }

    /** The file and offset for a point of the timeline. A point in a gap lands at the start of the next file. */
    fun locate(requestedMS: Long, mediaFiles: List<RecordingMedia>): Location? {
        val pieces = playable(mediaFiles)
        val first = pieces.firstOrNull() ?: return null
        val positionMS = maxOf(0L, requestedMS)
        if (positionMS < first.timelineStartMS) return Location(first, 0, first.timelineStartMS)
        for (index in pieces.indices.reversed()) {
            val piece = pieces[index]
            if (positionMS < piece.timelineStartMS) continue
            val pieceEndMS = endMS(piece, pieces)
            if (positionMS < pieceEndMS) return Location(piece, positionMS - piece.timelineStartMS, positionMS)
            if (index + 1 < pieces.size) {
                val next = pieces[index + 1]
                return Location(next, 0, next.timelineStartMS)
            }
            return Location(piece, pieceEndMS - piece.timelineStartMS, pieceEndMS)
        }
        return null
    }

    /** The file and offsets for a passage, cut at the end of its file. */
    fun locateRange(rangeStartMS: Long, rangeEndMS: Long, mediaFiles: List<RecordingMedia>): BoundedLocation? {
        if (rangeEndMS <= rangeStartMS) return null
        val start = locate(rangeStartMS, mediaFiles) ?: return null
        val pieceEndMS = endMS(start.media, playable(mediaFiles))
        val boundedEndMS = minOf(rangeEndMS, pieceEndMS)
        if (boundedEndMS <= start.timelineMS) return null
        return BoundedLocation(start, boundedEndMS - start.media.timelineStartMS)
    }

    /** The file that plays after this one, when there is one. */
    fun original(after: RecordingMedia, mediaFiles: List<RecordingMedia>): RecordingMedia? {
        val pieces = playable(mediaFiles)
        val index = pieces.indexOfFirst { piece -> piece.id == after.id }
        return if (index < 0 || index + 1 >= pieces.size) null else pieces[index + 1]
    }
}
