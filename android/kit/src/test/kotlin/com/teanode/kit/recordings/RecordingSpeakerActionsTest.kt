package com.teanode.kit.recordings

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Test
import java.util.Locale

class RecordingSpeakerActionsTest {
    private fun segment(id: String, label: String? = null, mergedLabel: String? = null) = RecordingTranscriptSegment(
        id = id, recordingTranscriptRunId = "run1", segmentStartMS = 0, segmentEndMS = 1000, transcriptText = "Hello", temporarySpeakerLabel = label, mergedTemporarySpeakerLabel = mergedLabel,
    )

    private fun bubble(vararg segments: RecordingTranscriptSegment) = RecordingConversation.bubbles(segments.toList()).first()

    @Test
    fun aSpeakerOfACaptureUnderWayIsNamedByItsLabel() {
        assertEquals("Speaker 2", RecordingTemporarySpeakers.labelToName(bubble(segment("a", label = "Speaker 2"))))
        assertNull(RecordingTemporarySpeakers.labelToName(bubble(segment("a"))))
        assertNull(RecordingTemporarySpeakers.labelToName(bubble(segment("a", label = ""))))
    }

    @Test
    fun aLabelFoundToBeAnotherVoiceCanBeSeparated() {
        assertEquals("Speaker 3", RecordingTemporarySpeakers.labelToSeparate(bubble(segment("a", label = "Speaker 2", mergedLabel = "Speaker 3"))))
        assertNull(RecordingTemporarySpeakers.labelToSeparate(bubble(segment("a", label = "Speaker 2", mergedLabel = ""))))
        assertNull(RecordingTemporarySpeakers.labelToSeparate(bubble(segment("a", label = "Speaker 2"))))
    }

    private fun namedSegment(id: String, correctionId: String?) = RecordingTranscriptSegment(
        id = id, recordingTranscriptRunId = "run1", segmentStartMS = 0, segmentEndMS = 1000, transcriptText = "Hello", appliedSpeakerCorrectionId = correctionId,
    )

    @Test
    fun aNameGivenToSeveralPassagesIsClearedOnce() {
        val segments = listOf(namedSegment("a", "c1"), namedSegment("b", "c1"), namedSegment("c", null), namedSegment("d", ""), namedSegment("e", "c2"))
        assertEquals(listOf("c1", "c2"), RecordingSpeakerNaming.correctionIds(segments))
        assertEquals(emptyList<String>(), RecordingSpeakerNaming.correctionIds(listOf(namedSegment("a", null))))
    }

    @Test
    fun aBubbleHasNamingOnlyWhenAPassageCarriesACorrection() {
        assertEquals(true, RecordingSpeakerNaming.hasNaming(bubble(namedSegment("a", "c1"))))
        assertEquals(false, RecordingSpeakerNaming.hasNaming(bubble(namedSegment("a", null), namedSegment("b", ""))))
    }

    private fun attempt(estimated: Double? = null, actual: Double? = null) = RecordingTranscriptionAttempt(
        id = "a1", providerModelName = "model-x", estimatedCostAmount = estimated, actualCostAmount = actual, currencyCode = "USD",
    )

    @Test
    fun theActualCostIsToldBeforeTheEstimateAndNothingWhenNeitherIsKnown() {
        assertEquals(RecordingAttemptCost.Actual("0.123", "USD"), RecordingAttemptCost.of(attempt(estimated = 0.5, actual = 0.1234), Locale.US))
        assertEquals(RecordingAttemptCost.Estimated("1.5", "USD"), RecordingAttemptCost.of(attempt(estimated = 1.5), Locale.US))
        assertEquals(RecordingAttemptCost.Unavailable, RecordingAttemptCost.of(attempt(), Locale.US))
    }
}
