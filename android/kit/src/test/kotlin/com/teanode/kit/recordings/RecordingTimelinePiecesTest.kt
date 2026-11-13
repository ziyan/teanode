package com.teanode.kit.recordings

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Test

/** Ported from the iPhone app's RecordingTimelinePiecesTests. */
class RecordingTimelinePiecesTest {
    private fun media(id: String, startMS: Long, durationMS: Long, state: String = "complete") = RecordingMedia(
        id = id, recordingId = "recording-a", clientMediaId = "original-$id", originalFilename = "$id.m4a", originalContentType = "audio/mp4",
        originalSizeBytes = 1000, originalSha256 = "a".repeat(64), timelineStartMS = startMS, mediaDurationMS = durationMS, partCount = 1,
        recordingUploadState = state, verifiedAt = "2000-01-01T00:00:00Z",
    )

    /** Two originals with a ten second gap between them: 0 to 60 s, then 70 to 100 s. One more is still uploading. */
    private val pieces = listOf(media("second", 70000, 30000), media("first", 0, 60000), media("pending", 5, 5, "uploading"))

    @Test
    fun positionMapsToTheOriginalAndOffset() {
        assertEquals("first", RecordingTimelinePieces.locate(20000, pieces)?.media?.id)
        assertEquals(20000L, RecordingTimelinePieces.locate(20000, pieces)?.offsetMS)
        val second = RecordingTimelinePieces.locate(75000, pieces)
        assertEquals("second", second?.media?.id)
        assertEquals(5000L, second?.offsetMS)
        assertEquals(75000L, second?.timelineMS)
    }

    @Test
    fun positionInTheGapLandsAtTheStartOfTheNextOriginal() {
        for (gapMS in listOf(60000L, 65000L, 69999L)) {
            val location = RecordingTimelinePieces.locate(gapMS, pieces)
            assertEquals("second", location?.media?.id)
            assertEquals(0L, location?.offsetMS)
            assertEquals(70000L, location?.timelineMS)
        }
    }

    @Test
    fun positionsBeforeTheFirstAndAfterTheLastAreClamped() {
        assertEquals(0L, RecordingTimelinePieces.locate(-5, pieces)?.timelineMS)
        val end = RecordingTimelinePieces.locate(500000, pieces)
        assertEquals("second", end?.media?.id)
        assertEquals(30000L, end?.offsetMS)
        assertEquals(100000L, end?.timelineMS)
        assertNull(RecordingTimelinePieces.locate(0, emptyList()))
    }

    @Test
    fun anOriginalOfUnknownLengthEndsWhereTheNextStarts() {
        val unknown = listOf(media("first", 0, 0), media("second", 40000, 20000))
        assertEquals("first", RecordingTimelinePieces.locate(39000, unknown)?.media?.id)
        assertEquals("second", RecordingTimelinePieces.locate(41000, unknown)?.media?.id)
    }

    @Test
    fun theOriginalAfterIsTheNextOneOnTheTimeline() {
        val first = pieces.first { piece -> piece.id == "first" }
        val second = pieces.first { piece -> piece.id == "second" }
        assertEquals("second", RecordingTimelinePieces.original(first, pieces)?.id)
        assertNull(RecordingTimelinePieces.original(second, pieces))
    }

    @Test
    fun aRangeStopsAtItsEndInsideTheOriginal() {
        val range = RecordingTimelinePieces.locateRange(20000, 25000, pieces)!!
        assertEquals("first", range.start.media.id)
        assertEquals(20000L, range.start.offsetMS)
        assertEquals(25000L, range.endOffsetMS)
        assertEquals(25000L, range.endTimelineMS)
    }

    @Test
    fun aRangeRunningPastItsOriginalStopsAtTheEndOfThatOriginal() {
        val range = RecordingTimelinePieces.locateRange(50000, 90000, pieces)!!
        assertEquals("first", range.start.media.id)
        assertEquals(60000L, range.endOffsetMS)
        assertEquals(60000L, range.endTimelineMS)
    }

    @Test
    fun aRangeInTheSecondOriginalCountsFromThatOriginalsStart() {
        val range = RecordingTimelinePieces.locateRange(75000, 80000, pieces)!!
        assertEquals("second", range.start.media.id)
        assertEquals(5000L, range.start.offsetMS)
        assertEquals(10000L, range.endOffsetMS)
        assertEquals(80000L, range.endTimelineMS)
    }

    @Test
    fun aRangeThatEndsBeforeItStartsOrHasNothingToPlayIsRefused() {
        assertNull(RecordingTimelinePieces.locateRange(20000, 20000, pieces))
        assertNull(RecordingTimelinePieces.locateRange(20000, 10000, pieces))
        assertNull(RecordingTimelinePieces.locateRange(0, 1000, emptyList()))
    }
}
