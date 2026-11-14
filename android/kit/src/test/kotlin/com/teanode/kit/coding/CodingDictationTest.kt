package com.teanode.kit.coding

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class CodingDictationTest {
    @Test fun `the place is the one the server knows`() {
        assertEquals("coding", CodingDictation.PLACE)
    }

    @Test fun `the microphone is hidden until the server offers voice`() {
        assertFalse(CodingDictation.isMicrophoneShown(null, isDisabled = false))
        assertTrue(CodingDictation.isMicrophoneShown(24_000, isDisabled = false))
    }

    @Test fun `the microphone is hidden while the session asks`() {
        assertFalse(CodingDictation.isMicrophoneShown(24_000, isDisabled = true))
    }

    @Test fun `listening goes on while nothing changes`() {
        assertFalse(CodingDictation.shouldStop(isSending = false, isDisabled = false, isInFront = true))
    }

    @Test fun `sending stops listening`() {
        assertTrue(CodingDictation.shouldStop(isSending = true, isDisabled = false, isInFront = true))
    }

    @Test fun `a question arriving stops listening`() {
        assertTrue(CodingDictation.shouldStop(isSending = false, isDisabled = true, isInFront = true))
    }

    @Test fun `leaving the app stops listening`() {
        assertTrue(CodingDictation.shouldStop(isSending = false, isDisabled = false, isInFront = false))
    }
}
