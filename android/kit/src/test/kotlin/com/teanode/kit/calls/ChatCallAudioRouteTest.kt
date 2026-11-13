package com.teanode.kit.calls

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class ChatCallAudioRouteTest {
    @Test
    fun onlyHeadphonesAndBluetoothArePrivate() {
        assertTrue(ChatCallAudioRoute.Headphones.isPrivate)
        assertTrue(ChatCallAudioRoute.Bluetooth("Buds").isPrivate)
        assertFalse(ChatCallAudioRoute.Phone.isPrivate)
        assertFalse(ChatCallAudioRoute.Speaker.isPrivate)
        assertFalse(ChatCallAudioRoute.Other("Dock").isPrivate)
    }

    @Test
    fun theRoutesAreOfferedInOneOrder() {
        val shuffled = listOf(ChatCallAudioRoute.Other("Dock"), ChatCallAudioRoute.Bluetooth("Buds"), ChatCallAudioRoute.Speaker, ChatCallAudioRoute.Phone, ChatCallAudioRoute.Headphones)
        assertEquals(
            listOf(ChatCallAudioRoute.Phone, ChatCallAudioRoute.Speaker, ChatCallAudioRoute.Headphones, ChatCallAudioRoute.Bluetooth("Buds"), ChatCallAudioRoute.Other("Dock")),
            shuffled.sortedBy { route -> ChatCallAudioRoute.order(route) },
        )
    }

    @Test
    fun aPictureMovesAnEarpieceCallToTheSpeakerButNotAPrivateOne() {
        assertTrue(ChatCallAudioRoute.shouldMoveToSpeaker(ChatCallAudioRoute.Phone))
        assertFalse(ChatCallAudioRoute.shouldMoveToSpeaker(ChatCallAudioRoute.Speaker))
        assertFalse(ChatCallAudioRoute.shouldMoveToSpeaker(ChatCallAudioRoute.Bluetooth("Buds")))
    }
}
