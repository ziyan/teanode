package com.teanode.kit.recordings

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class RecordingCaptureTest {
    @Test
    fun aStopBeatsAPauseAndEndsTheQueue() {
        val transitions = RecordingCaptureTransitions()
        transitions.enqueue(RecordingCaptureTransitions.Command.Pause(false))
        transitions.enqueue(RecordingCaptureTransitions.Command.Stop)
        assertFalse(transitions.shouldStartNextSegment)
        assertEquals(RecordingCaptureTransitions.Command.Stop, transitions.next())
        assertNull(transitions.next())
    }

    @Test
    fun anInterruptionIsRememberedAcrossPauses() {
        val transitions = RecordingCaptureTransitions()
        transitions.enqueue(RecordingCaptureTransitions.Command.Pause(true))
        transitions.enqueue(RecordingCaptureTransitions.Command.Pause(false))
        assertEquals(RecordingCaptureTransitions.Command.Pause(true), transitions.next())
    }

    @Test
    fun aRolloverWaitsForNothingElse() {
        val transitions = RecordingCaptureTransitions()
        transitions.enqueue(RecordingCaptureTransitions.Command.Rollover("a", true))
        assertTrue(transitions.shouldStartNextSegment)
        assertNotNull(transitions.next())
        assertNull(transitions.next())
    }

    @Test
    fun uploadsWaitInOrderOncePerAccount() {
        val scheduling = RecordingUploadScheduling()
        scheduling.enqueue("a")
        scheduling.enqueue("b")
        scheduling.enqueue("a")
        assertEquals("a", scheduling.next())
        scheduling.remove("b")
        assertTrue(scheduling.isEmpty)
        assertNull(scheduling.next())
    }

    @Test
    fun pieceBoundariesAreWorkedOutFromWrittenFrames() {
        assertEquals(1000L, RecordingPieceBoundary.lengthMS(48_000, 48_000.0))
        assertEquals(0L, RecordingPieceBoundary.lengthMS(0, 48_000.0))
        assertEquals(6000L, RecordingPieceBoundary.nextStartMS(5000, 48_000, 48_000.0))
        assertEquals(0L, RecordingPieceBoundary.gapMS(10, 5))
        assertEquals(4, RecordingClock.durationMS(0.004).toInt())
        assertEquals(0L, RecordingClock.durationMS(Double.NaN))
    }

    @Test
    fun aLinkNamesARecordingAndPerhapsAPassage() {
        val plain = RecordingLink.parse("/recordings/abc_1")!!
        assertEquals("abc_1", plain.recordingId)
        assertNull(plain.passageStartMS)
        val passage = RecordingLink.parse("/recordings/abc_1?version=run-2&startMS=1000&endMS=2000")!!
        assertEquals("run-2", passage.recordingTranscriptRunId)
        assertEquals(1000L, passage.passageStartMS)
        assertNull(RecordingLink.parse("/recordings/abc_1?startMS=1000&endMS=2000"))
        assertNull(RecordingLink.parse("/recordings/abc_1?version=r&startMS=2000&endMS=1000"))
        assertNull(RecordingLink.parse("/mail/abc"))
        assertNull(RecordingLink.parse("/recordings/a b"))
    }
}
