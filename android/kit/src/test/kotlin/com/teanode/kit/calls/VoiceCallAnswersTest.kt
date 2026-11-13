package com.teanode.kit.calls

import com.teanode.kit.TeaNodeJson
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class VoiceCallAnswersTest {
    @Test
    fun aVoiceEventIsRead() {
        val event = TeaNodeJson.decodeFromString(VoiceCallEvent.serializer(), """{"voiceEvent":"transcriptFinal","utteranceId":"u1","transcriptText":"Hello"}""")
        assertEquals("Hello", event.transcriptText)
        assertNull(event.answerAudio)
    }

    @Test
    fun onlyAFewSentencesAreAskedForAhead() {
        val queue = VoiceAnswerQueue("call")
        val first = queue.speak(listOf("One.", "Two.", "Three.", "Four.", "Five."))
        assertEquals(listOf("One.", "Two.", "Three."), first.map { request -> request.text })
        assertEquals(listOf("call-1", "call-2", "call-3"), first.map { request -> request.segmentId })
        assertTrue(queue.hasWaitingSentences)
        // A sentence the server finished makes room for the next.
        assertEquals(listOf("Four."), queue.segmentDone("call-1").map { request -> request.text })
        assertEquals(listOf("Five."), queue.segmentDone("call-2").map { request -> request.text })
        assertFalse(queue.hasWaitingSentences)
    }

    @Test
    fun theAnswerEndsWhenTheLastSegmentHasPlayed() {
        val queue = VoiceAnswerQueue("call")
        queue.speak(listOf("One.", "Two."))
        assertTrue(queue.isSpeakingAnswer)
        queue.segmentDone("call-1")
        assertFalse(queue.played("call-1"), "the second sentence has not played")
        assertTrue(queue.isSpeakingAnswer)
        // A segment that is not done arriving has not played, whatever the speaker says.
        assertFalse(queue.played("call-2"))
        queue.segmentDone("call-2")
        assertTrue(queue.played("call-2"))
        assertFalse(queue.isSpeakingAnswer)
    }

    @Test
    fun talkingOverAnAnswerSaysHowMuchWasHeard() {
        val queue = VoiceAnswerQueue("call")
        queue.speak(listOf("It is sunny.", "Bring a hat.", "And a coat.", "Maybe gloves."))
        queue.segmentDone("call-1")
        queue.played("call-1")
        val interruption = queue.interruption()
        assertEquals("It is sunny.", interruption.heardText)
        assertEquals("Bring a hat. And a coat. Maybe gloves.", interruption.unheardText)
        // The server is told to stop the sentences not played.
        assertEquals(listOf("call-2", "call-3", "call-4"), queue.stop())
        assertFalse(queue.isBusy)
        assertFalse(queue.isKnown("call-2"))
    }

    @Test
    fun aQuestionIsSpokenWithItsChoices() {
        assertEquals("Which day? Friday, Saturday", VoiceCallSpeech.questionText("Which day?", "Friday\nSaturday"))
        assertEquals("Which day?", VoiceCallSpeech.questionText("Which day?", null))
        assertEquals("Friday", VoiceCallSpeech.questionText(null, "Friday"))
    }
}
