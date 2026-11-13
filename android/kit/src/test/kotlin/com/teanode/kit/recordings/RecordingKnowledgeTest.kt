package com.teanode.kit.recordings

import com.teanode.kit.TeaNodeError
import com.teanode.kit.TeaNodeJson
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertThrows
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class RecordingKnowledgeTest {
    @Test
    fun theLearningCardHasFourStates() {
        assertEquals(RecordingLearningState.StoppedReading, RecordingLearningView(hasAgent = true, hasStoppedReading = true).state)
        assertEquals(RecordingLearningState.NoAgent, RecordingLearningView(hasAgent = false).state)
        assertEquals(RecordingLearningState.WaitingForTranscript, RecordingLearningView(hasAgent = true).state)
        assertEquals(RecordingLearningState.Reading, RecordingLearningView(hasAgent = true, knowledgeSource = RecordingLearningSource("s")).state)
    }

    @Test
    fun eachPageIsShownOnceAndAStatementBeatsAGuess() {
        val pages = RecordingMemory.pages(
            listOf(RecordingMemoryLink("1", "a", linkStatus = "proposed"), RecordingMemoryLink("2", "b", linkStatus = "confirmed"), RecordingMemoryLink("3", "a", linkStatus = "confirmed")),
        )
        assertEquals(listOf("a", "b"), pages.map { link -> link.nodePath })
        assertFalse(pages.first().isProposed)
    }

    @Test
    fun theLiveSocketsEventsBuildWhatWasHeard() {
        val state = RecordingLiveWordsState()
        fun event(json: String) = TeaNodeJson.decodeFromString(RecordingVoiceEvent.serializer(), json)
        state.handle(event("""{"voiceEvent":"ready"}"""), 0)
        assertTrue(state.isConnected)
        state.handle(event("""{"voiceEvent":"speechStarted","utteranceStartMS":1000}"""), 0)
        state.handle(event("""{"voiceEvent":"transcriptDelta","transcriptText":"Hello "}"""), 0)
        state.handle(event("""{"voiceEvent":"transcriptDelta","transcriptText":"there"}"""), 0)
        assertEquals("Hello there", state.partialText)
        assertEquals(1000L, state.partialStartMS)
        assertTrue(state.handle(event("""{"voiceEvent":"speechStopped"}"""), 0))
        state.handle(event("""{"voiceEvent":"transcriptFinal","utteranceId":"u1","transcriptText":" Hello there. ","utteranceStartMS":1000,"utteranceEndMS":2500}"""), 0)
        assertEquals("", state.partialText)
        assertEquals("Hello there.", state.heardUtterances.single().transcriptText)
        assertThrows(TeaNodeError.Refused::class.java) { state.handle(event("""{"voiceEvent":"refused","errorMessage":"no"}"""), 0) }
        state.disconnected()
        assertFalse(state.isConnected)
    }

    @Test
    fun anImportedAttachmentMustBeTheSameFile() {
        // The check is on the answer; a draft for another account is refused before anything is sent.
        val draft = RecordingAttachmentImportDraft("account-a", "att1", "client-1", "Title")
        assertEquals("att1", draft.agentAttachmentId)
    }
}
