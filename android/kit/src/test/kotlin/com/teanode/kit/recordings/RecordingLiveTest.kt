package com.teanode.kit.recordings

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/** Ported from the iPhone app's RecordingCutPolicyTests and RecordingLiveConversationTests. */
class RecordingCutPolicyTest {
    /** Feeds the meter every 50 milliseconds from [fromMS] up to [untilMS] at one level. */
    private fun feed(policy: RecordingCutPolicy, fromMS: Long, untilMS: Long, decibels: Float) {
        var atMS = fromMS
        while (atMS <= untilMS) {
            policy.recordLevel(decibels, atMS)
            atMS += 50
        }
    }

    @Test
    fun nothingIsCutBeforeTwentySecondsWhateverIsHeard() {
        val policy = RecordingCutPolicy().apply { startPiece(0) }
        feed(policy, 0, 19_900, -50f)
        policy.recordUtteranceEnd(15_000)
        assertNull(policy.cutReason(19_900, true))
        assertNull(policy.cutReason(19_900, false))
    }

    @Test
    fun aConnectedStreamCutsAtTheFirstUtteranceEndAfterTwentySeconds() {
        val policy = RecordingCutPolicy().apply { startPiece(1_000) }
        // An end at 15 seconds into the piece is too early to count; the quiet level is ignored while connected.
        policy.recordUtteranceEnd(16_000)
        feed(policy, 1_000, 30_000, -60f)
        assertNull(policy.cutReason(25_000, true))
        policy.recordUtteranceEnd(27_300)
        assertEquals(RecordingCutReason.UtteranceEnd, policy.cutReason(27_350, true))
    }

    @Test
    fun anUtteranceEndOfThePreviousPieceDoesNotCutTheNextOne() {
        val policy = RecordingCutPolicy().apply { startPiece(0) }
        policy.recordUtteranceEnd(21_000)
        policy.startPiece(21_100)
        assertNull(policy.cutReason(41_200, true))
    }

    @Test
    fun withoutAStreamTheLevelHoldingNearItsLowestCuts() {
        val policy = RecordingCutPolicy().apply { startPiece(0) }
        // Loud speech, then 200 milliseconds of quiet: not yet a pause. Then 300: a pause.
        feed(policy, 0, 24_000, -20f)
        feed(policy, 24_050, 24_200, -58f)
        assertNull(policy.cutReason(24_200, false))
        feed(policy, 24_250, 24_300, -58f)
        assertEquals(RecordingCutReason.LevelPause, policy.cutReason(24_300, false))
    }

    @Test
    fun backgroundNoiseIsMeasuredAgainstItsOwnFloor() {
        val policy = RecordingCutPolicy().apply { startPiece(0) }
        feed(policy, 0, 22_000, -10f)
        feed(policy, 22_050, 22_400, -30f)
        assertEquals(RecordingCutReason.LevelPause, policy.cutReason(22_400, false))
    }

    @Test
    fun loudnessThatIsNeverNearTheLowestDoesNotCut() {
        val policy = RecordingCutPolicy().apply { startPiece(0) }
        feed(policy, 0, 22_000, -10f)
        feed(policy, 22_050, 22_100, -45f)
        feed(policy, 22_150, 24_000, -10f)
        assertNull(policy.cutReason(24_000, false))
    }

    @Test
    fun aMeterThatStoppedReportingDoesNotMakeAPause() {
        val policy = RecordingCutPolicy().apply { startPiece(0) }
        feed(policy, 0, 21_000, -40f)
        assertNull(policy.cutReason(22_000, false))
    }

    @Test
    fun everyPieceIsCutAtSixtySecondsWhateverIsHeard() {
        val policy = RecordingCutPolicy().apply { startPiece(5_000) }
        feed(policy, 5_000, 64_950, -10f)
        assertNull(policy.cutReason(64_950, true))
        assertEquals(RecordingCutReason.MaximumLength, policy.cutReason(65_000, true))
        assertEquals(RecordingCutReason.MaximumLength, policy.cutReason(65_000, false))
    }

    @Test
    fun theLiveStreamBacksOffAndStaysAtAMinute() {
        assertEquals(listOf(0.0, 1.0, 2.0, 4.0, 8.0, 16.0, 32.0, 60.0, 60.0), (0..8).map { count -> RecordingLiveBackoff.delaySeconds(count) })
        assertEquals(60.0, RecordingLiveBackoff.delaySeconds(1_000))
    }
}

class RecordingLiveConversationTest {
    private fun utterance(id: String, start: Long, end: Long, text: String = "Words.") = RecordingLiveUtterance(id, start, end, text)

    private fun segment(
        id: String, start: Long, end: Long, speaker: String = "A", temporaryLabel: String? = null, confirmedName: String? = null,
        identityId: String? = null, isAccountOwner: Boolean = false, text: String = "Words.",
    ) = RecordingTranscriptSegment(
        id = id, recordingTranscriptRunId = "run-a", recordingTranscriptionAttemptId = "attempt-a", localSpeakerId = speaker, segmentStartMS = start,
        segmentEndMS = end, transcriptText = text, effectiveSpeakerIdentityId = identityId, effectiveSpeakerName = confirmedName,
        hasUserConfirmedSpeaker = confirmedName != null, isAccountOwner = isAccountOwner, temporarySpeakerLabel = temporaryLabel,
    )

    @Test
    fun eachLiveUtteranceIsABubbleWithoutASpeakerBetweenThePassagesAroundItInTime() {
        val bubbles = RecordingConversation.bubbles(
            listOf(segment("s1", 0, 10_000, text = "First."), segment("s2", 30_000, 40_000, text = "Last.")),
            listOf(utterance("u2", 20_000, 22_000, "Second."), utterance("u1", 12_000, 15_000, "Between.")),
        )
        assertEquals(listOf("First.", "Between.", "Second.", "Last."), bubbles.map { bubble -> bubble.bubbleText })
        assertEquals(listOf(false, true, true, false), bubbles.map { bubble -> bubble.isLive })
        assertNull(bubbles[1].confirmedSpeakerName)
        assertNull(bubbles[1].temporarySpeakerLabel)
        assertFalse(bubbles[1].isAccountOwner)
        assertEquals("live:u1", bubbles[1].id)
    }

    @Test
    fun liveUtterancesDoNotCountAsSpeakers() {
        assertEquals(1, RecordingConversation.speakerCount(listOf(segment("s1", 0, 1_000))))
    }

    @Test
    fun aTemporarySpeakerIsOneSpeakerAcrossLabelsAndIsNotGivenALetter() {
        val bubbles = RecordingConversation.bubbles(
            listOf(
                segment("s1", 0, 1_000, "A", temporaryLabel = "Speaker 1"), segment("s2", 1_000, 2_000, "Speaker 1", temporaryLabel = "Speaker 1"),
                segment("s3", 2_000, 3_000, "B"), segment("s4", 3_000, 4_000, "A", temporaryLabel = "Speaker 2"),
            ),
        )
        assertEquals(listOf("s1", "s3", "s4"), bubbles.map { bubble -> bubble.id })
        assertEquals(listOf("Speaker 1", null, "Speaker 2"), bubbles.map { bubble -> bubble.temporarySpeakerLabel })
        // The one speaker with a plain label is the first of those, whatever the temporary speakers before it.
        assertEquals(0, bubbles[1].unconfirmedSpeakerOrdinal)
    }

    @Test
    fun aNamedTemporarySpeakerShowsTheName() {
        val bubbles = RecordingConversation.bubbles(listOf(segment("s1", 0, 1_000, "Speaker 1", confirmedName = "Jun", identityId = "identity-a")))
        assertEquals("Jun", bubbles[0].confirmedSpeakerName)
        assertNull(bubbles[0].temporarySpeakerLabel)
    }

    @Test
    fun aShortInterjectionStaysInsideTheBubbleItInterrupts() {
        val bubbles = RecordingConversation.bubbles(
            listOf(
                segment("s1", 0, 4_000, "A", text = "So we went to the"), segment("s2", 4_000, 4_300, "B", text = "Mm-hm."),
                segment("s3", 4_300, 8_000, "A", text = "market and bought bread."),
            ),
        )
        assertEquals(1, bubbles.size)
        assertEquals("So we went to the Mm-hm. market and bought bread.", bubbles[0].bubbleText)
        assertEquals("A", RecordingConversation.speakerLetter(0))
        assertEquals("27", RecordingConversation.speakerLetter(26))
    }

    @Test
    fun aPassageCoveringMostOfAnUtteranceTakesItsPlace() {
        val covering = listOf(segment("s1", 0, 10_000))
        assertTrue(RecordingLiveWords.isCovered(utterance("a", 2_000, 6_000), covering))
        // Four tenths of it are covered: not enough, the words stay until the passage that has the rest arrives.
        assertFalse(RecordingLiveWords.isCovered(utterance("b", 8_000, 13_000), covering))
        assertTrue(RecordingLiveWords.isCovered(utterance("c", 7_000, 11_000), covering))
        assertFalse(RecordingLiveWords.isCovered(utterance("d", 12_000, 14_000), covering))
        assertTrue(RecordingLiveWords.isCovered(utterance("e", 5_000, 5_000), covering))
        assertFalse(RecordingLiveWords.isCovered(utterance("f", 15_000, 15_000), covering))
    }

    @Test
    fun shownWordsAreTheServersAndTheOnesJustHeardEachOnceMinusWhatThePassagesCover() {
        val shown = RecordingLiveWords.shown(
            serverUtterances = listOf(utterance("u1", 1_000, 2_000), utterance("u2", 50_000, 52_000, "Server.")),
            heardUtterances = listOf(utterance("u2", 50_000, 52_000, "Heard."), utterance("u3", 53_000, 55_000, "Newest."), utterance("u0", 0, 0, "  ")),
            partialText = "", partialStartMS = 0, segments = listOf(segment("s1", 0, 30_000)),
        )
        assertEquals(listOf("u2", "u3"), shown.map { utterance -> utterance.utteranceId })
        assertEquals("Server.", shown[0].transcriptText)
    }

    @Test
    fun wordsStillBeingSaidComeLastAndNothingOlderThanTheWindowIsShown() {
        val shown = RecordingLiveWords.shown(
            serverUtterances = listOf(utterance("old", 1_000, 2_000), utterance("u1", 70_000, 72_000)), heardUtterances = emptyList(),
            partialText = " and then ", partialStartMS = 71_000, segments = emptyList(), windowStartMS = 60_000,
        )
        assertEquals(listOf("u1", "partial"), shown.map { utterance -> utterance.utteranceId })
        assertEquals("and then", shown[1].transcriptText)
        assertEquals(72_000L, shown[1].utteranceStartMS)
    }

    @Test
    fun onlyTheLatestHeardUtterancesAreKept() {
        var heard = emptyList<RecordingLiveUtterance>()
        for (index in 0 until RecordingLiveWords.HEARD_UTTERANCES_KEPT + 20) {
            heard = RecordingLiveWords.appending(utterance("u$index", index.toLong(), index + 1L), heard)
        }
        assertEquals(RecordingLiveWords.HEARD_UTTERANCES_KEPT, heard.size)
        assertEquals("u20", heard.first().utteranceId)
        heard = RecordingLiveWords.appending(utterance("u50", 50, 51, "Again."), heard)
        assertEquals(1, heard.count { utterance -> utterance.utteranceId == "u50" })
        assertEquals("Again.", heard.last().transcriptText)
    }

    @Test
    fun theLevelMeterNormalizesAndKeepsTheLastBars() {
        assertEquals(0.0, RecordingInputLevel.normalized(-80f))
        assertEquals(1.0, RecordingInputLevel.normalized(5f))
        assertEquals(0.5, RecordingInputLevel.normalized(-25f), 0.0001)
        assertEquals(0.0, RecordingInputLevel.normalized(Float.NaN))
        val history = RecordingLevelHistory()
        repeat(60) { history.append(-25f) }
        assertEquals(RecordingLevelHistory.BAR_COUNT, history.bars.size)
        assertEquals(0.5, history.bars.last(), 0.0001)
        assertEquals(0.0, RecordingLevelHistory().bars.first())
        assertTrue(RecordingLiveSession.shouldRecycle(25.0 * 60))
        assertFalse(RecordingLiveSession.shouldRecycle(25.0 * 60 - 1))
    }
}
