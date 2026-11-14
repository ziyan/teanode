package com.teanode.kit.session

import com.teanode.kit.recordings.LocalRecording
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class UnsentDataTest {
    private fun recording(state: String) = LocalRecording("r-$state", "account", null, recordingTitle = "t", recordingOrigin = "capture", startedAtEpochMS = 0, recordingCaptureState = state)

    @Test fun `nothing waiting is empty`() {
        assertTrue(UnsentData().isEmpty)
    }

    @Test fun `any waiting kind makes it not empty`() {
        assertFalse(UnsentData(unfinishedRecordingCount = 1).isEmpty)
        assertFalse(UnsentData(unsentLocationReportCount = 3).isEmpty)
        assertFalse(UnsentData(hasUnsentMail = true).isEmpty)
    }

    @Test fun `a recording still capturing or with no verified upload is unfinished`() {
        assertEquals(2, UnsentData.unfinishedRecordingCount(listOf(recording("active"), recording("paused"))))
        // Stopped, but with no media recorded to the server yet.
        assertEquals(1, UnsentData.unfinishedRecordingCount(listOf(recording("stopped"))))
    }
}
