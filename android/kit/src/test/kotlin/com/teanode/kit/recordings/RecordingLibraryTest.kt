package com.teanode.kit.recordings

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class RecordingLibraryTest {
    private fun recording(id: String, clientId: String = "client-$id", summary: String? = null, recordedAt: String = "2026-01-02T03:04:05Z") = Recording(
        id = id, userId = "user-a", createdAt = recordedAt, updatedAt = recordedAt, clientRecordingId = clientId, recordingTitle = "Title $id",
        recordingOrigin = "capture", recordedAt = recordedAt, recordingSummary = summary, recordingCaptureState = "stopped",
    )

    private fun local(id: String, state: String = "stopped", media: List<LocalRecordingMedia> = emptyList()) = LocalRecording(
        id = id, accountKey = "account-a", recordingUserId = "user-a", clientRecordingId = "client-$id", recordingTitle = "Local $id",
        recordingOrigin = "capture", startedAtEpochMS = 1_700_000_000_000, recordingCaptureState = state, localMedia = media,
    )

    @Test
    fun firstLineSkipsBlankLines() {
        assertEquals("Hello", RecordingLibrary.firstLine("\n  \n Hello \nWorld"))
        assertEquals("", RecordingLibrary.firstLine(null))
    }

    @Test
    fun aRecordingWithASummaryIsReadyWithoutAskingTheServerForProgress() {
        assertEquals(RecordingRowStatus.Ready, RecordingLibrary.status(recording("a", summary = "A talk"), null, null))
    }

    @Test
    fun aRecordingWithoutASummaryIsTranscribingUntilTheServerSaysOtherwise() {
        assertEquals(RecordingRowStatus.Transcribing, RecordingLibrary.status(recording("a"), null, null))
        assertEquals(RecordingRowStatus.Transcribing, RecordingLibrary.status(recording("a"), null, RecordingServerProgress(true, 100, listOf("transcribing"))))
        assertEquals(RecordingRowStatus.Ready, RecordingLibrary.status(recording("a"), null, RecordingServerProgress(true, 100, listOf("failed", "complete"))))
        assertEquals(RecordingRowStatus.Failed, RecordingLibrary.status(recording("a"), null, RecordingServerProgress(true, 100, listOf("failed"))))
        assertEquals(RecordingRowStatus.Uploading(40), RecordingLibrary.status(recording("a"), null, RecordingServerProgress(false, 40, emptyList())))
    }

    @Test
    fun aCaptureUnderWayIsRecordingOrPaused() {
        assertEquals(RecordingRowStatus.Recording, RecordingLibrary.status(null, local("a", "active"), null))
        assertEquals(RecordingRowStatus.Paused, RecordingLibrary.status(null, local("a", "paused"), null))
        assertEquals(RecordingRowStatus.Paused, RecordingLibrary.status(null, local("a", "interrupted"), null))
    }

    @Test
    fun aRecordingStillUploadingFromThePhoneShowsHowMuchWentUp() {
        val pending = LocalRecordingMedia(id = "m", originalFilename = "a.m4a", originalContentType = "audio/mp4", timelineStartMS = 0, originalSizeBytes = 100)
        assertEquals(RecordingRowStatus.Uploading(0), RecordingLibrary.status(null, local("a", media = listOf(pending)), null))
        val withError = local("a", media = listOf(pending)).copy(uploadErrorMessage = "offline", isUploadWaitingForNetwork = true)
        assertEquals(RecordingRowStatus.WaitingForNetwork, RecordingLibrary.status(null, withError, null))
        assertEquals(RecordingRowStatus.Failed, RecordingLibrary.status(null, withError.copy(isUploadWaitingForNetwork = false), null))
    }

    @Test
    fun rowsJoinServerAndPhoneCopiesAndListNewestFirst() {
        val rows = RecordingLibrary.rows(
            recordings = listOf(recording("a", recordedAt = "2026-01-01T00:00:00Z"), recording("b", recordedAt = "2026-03-01T00:00:00Z")),
            localRecordings = listOf(local("a"), local("only-here"), local("gone").copy(isDeletedRemotely = true)),
        )
        assertEquals(listOf("client-b", "client-a", "client-only-here"), rows.map { row -> row.id })
        assertTrue(rows.first { row -> row.id == "client-a" }.localRecording != null)
        assertNull(rows.first { row -> row.id == "client-b" }.localRecording)
    }

    @Test
    fun pausingAndResumingAndStoppingKeepTheGapsHonest() {
        val capture = local("a", "active")
        val paused = capture.paused(5_000)
        assertEquals("paused", paused.recordingCaptureState)
        val resumed = paused.resumed(8_000)
        assertEquals("active", resumed.recordingCaptureState)
        assertEquals(8_000L, resumed.captureGaps.single().gapEndMS)
        val stopped = resumed.paused(10_000, "interrupted").stopped(12_000)
        assertEquals("stopped", stopped.recordingCaptureState)
        assertEquals(12_000L, stopped.captureGaps.last().gapEndMS)
        assertFalse(stopped.isCaptureUnderWay)
    }

    @Test
    fun aCaptureIsNotPausedBeforeItsOwnEnd() {
        val capture = local("a", "active").copy(timelineDurationMS = 9_000)
        assertTrue(runCatching { capture.paused(5_000) }.isFailure)
    }

    @Test
    fun aPublishedRecordingIsMergedIntoTheLoadedListInPlace() {
        val published = recording("m5")
        val existing = listOf(recording("m9"), recording("m1"))
        val source = local("x").copy(recordingId = "m5", clientRecordingId = published.clientRecordingId)
        val merged = RecordingLibraryProgress.mergingPublishedRecording(published, existing, source, "account-a", "user-a", "account-a", "user-a")
        assertEquals(listOf("m9", "m5", "m1"), merged.map { recording -> recording.id })
        assertEquals(existing, RecordingLibraryProgress.mergingPublishedRecording(published, existing, source, "account-a", "user-a", "account-b", "user-a"))
    }
}
