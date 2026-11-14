package com.teanode.kit.recordings

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class RecordingOriginalsTest {
    private fun media(id: String, startMS: Long = 0, durationMS: Long = 60_000, state: String = "complete", bytes: ByteArray = "abc".toByteArray()) = RecordingMedia(
        id = id, recordingId = "r1", clientMediaId = "c-$id", originalFilename = "meeting.m4a", originalContentType = "audio/mp4",
        originalSizeBytes = bytes.size.toLong(), originalSha256 = RecordingOriginals.sha256Text(bytes), timelineStartMS = startMS,
        mediaDurationMS = durationMS, partCount = 1, recordingUploadState = state,
    )

    @Test
    fun theDigestIsTheOneEveryToolWrites() {
        assertEquals("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", RecordingOriginals.sha256Text("abc".toByteArray()))
    }

    @Test
    fun onlyTheFileTheServerRecordedIsVerified() {
        val file = media("m1")
        assertTrue(RecordingOriginals.isVerified("abc".toByteArray(), file))
        assertFalse(RecordingOriginals.isVerified("abd".toByteArray(), file))
        assertFalse(RecordingOriginals.isVerified("abcd".toByteArray(), file))
    }

    @Test
    fun onlyCompleteOriginalsAreOfferedInTheOrderOfTheRecording() {
        val listed = RecordingOriginals.completeFiles(listOf(media("late", startMS = 60_000), media("partial", state = "uploading"), media("early")))
        assertEquals(listOf("early", "late"), listed.map { file -> file.id })
    }

    @Test
    fun aRecordingInPiecesTellsItsOriginalsByWhereTheySit() {
        val first = media("m1")
        val second = media("m2", startMS = 3_661_000, durationMS = 120_000)
        assertEquals("meeting.m4a", RecordingOriginals.title(first, 1) { start, end -> "$start to $end" })
        assertEquals("0:00:00 to 0:01:00", RecordingOriginals.title(first, 2) { start, end -> "$start to $end" })
        assertEquals("1:01:01 to 1:03:01", RecordingOriginals.title(second, 2) { start, end -> "$start to $end" })
    }
}
