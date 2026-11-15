package com.teanode.kit.recordings

import com.teanode.kit.Account
import com.teanode.kit.Connection
import com.teanode.kit.FakeServer
import com.teanode.kit.ServerAddress
import com.teanode.kit.TeaNodeError
import kotlinx.coroutines.runBlocking
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class RecordingDraftsTest {
    private fun recordingAnswer(sourceGeneration: Long) =
        """{"data":{"ReadRecording":{"recording":{"id":"rec-1","createdAt":"2030-01-01T00:00:00Z","updatedAt":"2030-01-01T00:00:00Z","clientRecordingId":"c1","recordingTitle":"Standup","recordingOrigin":"capture","recordingCaptureState":"stopped","sourceGeneration":$sourceGeneration}}}}"""

    private fun correctionJson(clientCorrectionId: String, text: String, segmentId: String = "seg-1") =
        """{"id":"corr-1","recordingId":"rec-1","recordingMediaId":"m1","recordingTranscriptRunId":"run-1","recordingTranscriptSegmentId":"$segmentId","clientCorrectionId":"$clientCorrectionId","passageStartMS":0,"passageEndMS":1000,"originalTranscriptText":"original","correctedTranscriptText":"$text","sourceGeneration":4,"createdAt":"2030-01-01T00:00:00Z","isRemoved":false}"""

    private fun connectionWith(answer: (String) -> Pair<Int, String>): Pair<Connection, FakeServer> {
        val fake = FakeServer { request -> answer(request.body!!.toString(Charsets.UTF_8)) }
        val account = Account(ServerAddress.parse("assistant.example.com"), "test-client", "tnt_test", "", Long.MAX_VALUE / 4, "test-owner")
        return Connection(account, fake) to fake
    }

    private val accountKey = "account-key"

    @Test
    fun aDraftIsKeptPerAccountAndPassageAndFinishedOnlyByItself() {
        val drafts = RecordingTextCorrectionDrafts(InMemoryRecordingDraftStorage())
        val draft = RecordingTextCorrectionDraft.start("account-a", "rec-1", "seg-1", "Hello there", 4)
        drafts.save(draft)
        assertEquals(draft, drafts.read("account-a", "seg-1"))
        assertNull(drafts.read("account-b", "seg-1"))
        assertNull(drafts.read("account-a", "seg-2"))
        drafts.finish(draft.copy(clientCorrectionId = "someone-else"))
        assertEquals(draft, drafts.read("account-a", "seg-1"))
        drafts.finish(draft)
        assertNull(drafts.read("account-a", "seg-1"))
    }

    @Test
    fun theStorageKeyHidesNothingInTheNamesButKeepsThemApart() {
        assertNotEquals(RecordingTextCorrectionDrafts.storageKey("a.b", "c"), RecordingTextCorrectionDrafts.storageKey("a", "b.c"))
        assertNotEquals(RecordingTextCorrectionDrafts.storageKey("a", "c"), RecordingSpeakerDrafts.storageKey("a", "c"))
    }

    @Test
    fun savingAnEditSendsItAndForgetsTheDraft() = runBlocking {
        val drafts = RecordingTextCorrectionDrafts(InMemoryRecordingDraftStorage())
        val (connection, _) = connectionWith { body ->
            if (body.contains("CorrectRecordingTranscriptText")) {
                val clientCorrectionId = Regex("\"clientCorrectionId\":\"([^\"]+)\"").find(body)!!.groupValues[1]
                200 to """{"data":{"CorrectRecordingTranscriptText":${correctionJson(clientCorrectionId, "Hello there, Sam")}}}"""
            } else {
                200 to recordingAnswer(4)
            }
        }
        val draft = RecordingTextCorrectionDraft.start(connection.accountKey, "rec-1", "seg-1", "Hello there", 4)
        val outcome = connection.submitRecordingTextCorrection(drafts, draft, "Hello there, Sam")
        assertTrue(outcome is RecordingDraftOutcome.Saved)
        assertNull(drafts.read(connection.accountKey, "seg-1"))
    }

    @Test
    fun anAnswerThatIsNotTheSameCorrectionKeepsTheDraftLockedForRetry() = runBlocking {
        val drafts = RecordingTextCorrectionDrafts(InMemoryRecordingDraftStorage())
        val (connection, _) = connectionWith { body ->
            if (body.contains("CorrectRecordingTranscriptText")) 200 to """{"data":{"CorrectRecordingTranscriptText":${correctionJson("another-correction", "Hello there, Sam")}}}"""
            else 200 to recordingAnswer(4)
        }
        val draft = RecordingTextCorrectionDraft.start(connection.accountKey, "rec-1", "seg-1", "Hello there", 4)
        val outcome = connection.submitRecordingTextCorrection(drafts, draft, "Hello there, Sam")
        assertTrue(outcome is RecordingDraftOutcome.Unconfirmed)
        val kept = drafts.read(connection.accountKey, "seg-1")!!
        assertTrue(kept.hasSubmittedRequest)
        assertEquals("Hello there, Sam", kept.correctedTranscriptText)
        assertEquals(draft.clientCorrectionId, kept.clientCorrectionId)
    }

    @Test
    fun retryingSendsTheSameTextAndTheSameCorrectionEvenIfTheFieldWasChanged() = runBlocking {
        val drafts = RecordingTextCorrectionDrafts(InMemoryRecordingDraftStorage())
        val sentTexts = mutableListOf<String>()
        val (connection, _) = connectionWith { body ->
            if (body.contains("CorrectRecordingTranscriptText")) {
                sentTexts.add(Regex("\"correctedTranscriptText\":\"([^\"]+)\"").find(body)!!.groupValues[1])
                500 to "no answer"
            } else {
                200 to recordingAnswer(4)
            }
        }
        val draft = RecordingTextCorrectionDraft.start(connection.accountKey, "rec-1", "seg-1", "Hello there", 4)
        val first = connection.submitRecordingTextCorrection(drafts, draft, "First text")
        assertTrue(first is RecordingDraftOutcome.Unconfirmed)
        val kept = (first as RecordingDraftOutcome.Unconfirmed).draft
        connection.submitRecordingTextCorrection(drafts, kept, "A different text")
        assertEquals(listOf("First text", "First text"), sentTexts)
    }

    @Test
    fun aRefusalFreesTheTextAndReadsTheGenerationAgain() = runBlocking {
        val drafts = RecordingTextCorrectionDrafts(InMemoryRecordingDraftStorage())
        val (connection, _) = connectionWith { body ->
            if (body.contains("CorrectRecordingTranscriptText")) 200 to """{"errors":[{"message":"api: source generation changed"}],"data":null}"""
            else 200 to recordingAnswer(9)
        }
        val draft = RecordingTextCorrectionDraft.start(connection.accountKey, "rec-1", "seg-1", "Hello there", 4)
        val outcome = connection.submitRecordingTextCorrection(drafts, draft, "Hello there, Sam")
        assertTrue(outcome is RecordingDraftOutcome.Refused)
        val kept = drafts.read(connection.accountKey, "seg-1")!!
        assertFalse(kept.hasSubmittedRequest)
        assertEquals(9L, kept.expectedSourceGeneration)
    }

    @Test
    fun openingAPassageWhoseRequestWasSavedFindsNothingToRetry() = runBlocking {
        val drafts = RecordingTextCorrectionDrafts(InMemoryRecordingDraftStorage())
        val (connection, _) = connectionWith { body ->
            if (body.contains("ReadRecordingTranscriptTextCorrections")) 200 to """{"data":{"ReadRecordingTranscriptTextCorrections":[${correctionJson("kept-id", "Hello there, Sam")}]}}"""
            else 200 to recordingAnswer(5)
        }
        drafts.save(RecordingTextCorrectionDraft(connection.accountKey, "rec-1", "seg-1", "kept-id", "Hello there, Sam", 4, hasSubmittedRequest = true))
        val start = connection.startRecordingTextCorrection(drafts, "rec-1", "seg-1", "Hello there")
        assertEquals(RecordingTextCorrectionStart.AlreadySaved, start)
        assertNull(drafts.read(connection.accountKey, "seg-1"))
    }

    @Test
    fun openingAPassageWhoseRequestWasNotSavedOffersTheRetry() = runBlocking {
        val drafts = RecordingTextCorrectionDrafts(InMemoryRecordingDraftStorage())
        val (connection, _) = connectionWith { body ->
            if (body.contains("ReadRecordingTranscriptTextCorrections")) 200 to """{"data":{"ReadRecordingTranscriptTextCorrections":[]}}"""
            else 200 to recordingAnswer(5)
        }
        val sent = RecordingTextCorrectionDraft(connection.accountKey, "rec-1", "seg-1", "kept-id", "Hello there, Sam", 4, hasSubmittedRequest = true)
        drafts.save(sent)
        val start = connection.startRecordingTextCorrection(drafts, "rec-1", "seg-1", "Hello there")
        assertEquals(RecordingTextCorrectionStart.Editing(sent), start)
    }

    @Test
    fun openingAFreshPassageStartsADraftFromItsText() = runBlocking {
        val drafts = RecordingTextCorrectionDrafts(InMemoryRecordingDraftStorage())
        val (connection, _) = connectionWith { _ -> 200 to recordingAnswer(7) }
        val start = connection.startRecordingTextCorrection(drafts, "rec-1", "seg-1", "Hello there") as RecordingTextCorrectionStart.Editing
        assertEquals("Hello there", start.draft.correctedTranscriptText)
        assertEquals(7L, start.draft.expectedSourceGeneration)
        assertFalse(start.draft.hasSubmittedRequest)
        assertEquals(start.draft, drafts.read(connection.accountKey, "seg-1"))
    }

    private val segment = RecordingTranscriptSegment(
        id = "seg-9", recordingTranscriptionAttemptId = "attempt-1", localSpeakerId = "speaker-1", segmentStartMS = 1000, segmentEndMS = 5000, transcriptText = "Hi",
    )

    @Test
    fun aSpeakerNamingSentWithoutAnAnswerIsSettledFromWhatTheRecordingHolds() {
        val drafts = RecordingSpeakerDrafts(InMemoryRecordingDraftStorage())
        val sent = RecordingSpeakerDraft.start("account-a", "rec-1", segment, 4).copy(confirmedName = "Sam", hasSubmittedRequest = true)
        drafts.save(sent)
        // Not saved: rolled back, and it opens as a form again with the generation now current.
        val reopened = drafts.settled("account-a", "seg-9", RecordingSpeakersView(sourceGeneration = 6))!!
        assertFalse(reopened.hasSubmittedRequest)
        assertEquals(6L, reopened.expectedSourceGeneration)
        assertEquals("Sam", reopened.confirmedName)
        // Saved: done, nothing is kept.
        drafts.save(sent)
        val saved = RecordingSpeakersView(sourceGeneration = 6, speakerCorrections = listOf(RecordingSpeakerCorrection("x", clientCorrectionId = sent.clientCorrectionId)))
        assertNull(drafts.settled("account-a", "seg-9", saved))
        assertNull(drafts.read("account-a", "seg-9"))
        // Saved and then taken back: the request is not what is held, so it opens again.
        drafts.save(sent)
        val removed = RecordingSpeakersView(sourceGeneration = 8, speakerCorrections = listOf(RecordingSpeakerCorrection("x", clientCorrectionId = sent.clientCorrectionId, isRemoved = true)))
        assertFalse(drafts.settled("account-a", "seg-9", removed)!!.hasSubmittedRequest)
    }

    @Test
    fun aSpeakerNamingThatWasNotSentIsKeptAsItIs() {
        val drafts = RecordingSpeakerDrafts(InMemoryRecordingDraftStorage())
        val typed = RecordingSpeakerDraft.start("account-a", "rec-1", segment, 4).copy(confirmedName = "Sam")
        drafts.save(typed)
        assertEquals(typed, drafts.settled("account-a", "seg-9", RecordingSpeakersView(sourceGeneration = 9)))
        assertNull(drafts.settled("account-a", "seg-other", null))
    }

    @Test
    fun aSpeakerNamingNeverSentHasAnIDOfItsOwn() {
        val first = RecordingSpeakerDraft.start("account-a", "rec-1", segment, 4)
        val second = RecordingSpeakerDraft.start("account-a", "rec-1", segment, 4)
        assertNotEquals(first.clientCorrectionId, second.clientCorrectionId)
        assertEquals(segment.segmentStartMS, first.passageStartMS)
        assertEquals(segment.segmentEndMS, first.passageEndMS)
    }
}
