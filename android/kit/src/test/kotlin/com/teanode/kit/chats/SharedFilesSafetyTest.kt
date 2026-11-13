package com.teanode.kit.chats

import java.io.ByteArrayInputStream
import org.junit.jupiter.api.Test
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue

class SharedFilesSafetyTest {
    private val ownPackage = "com.example.app"

    @Test fun `a content address of another app is readable`() {
        assertTrue(SharedFiles.isReadableAddress("content", "com.other.provider", ownPackage))
        assertTrue(SharedFiles.isReadableAddress("content", "media", ownPackage))
    }

    @Test fun `a file address is refused`() {
        assertFalse(SharedFiles.isReadableAddress("file", "", ownPackage))
        assertFalse(SharedFiles.isReadableAddress("file", null, ownPackage))
    }

    @Test fun `the app's own providers are refused`() {
        assertFalse(SharedFiles.isReadableAddress("content", "com.example.app.chatfiles", ownPackage))
        assertFalse(SharedFiles.isReadableAddress("content", "COM.EXAMPLE.APP.chatfiles", ownPackage))
        assertFalse(SharedFiles.isReadableAddress("content", "com.example.app", ownPackage))
        assertFalse(SharedFiles.isReadableAddress("content", "someone@com.example.app.chatfiles", ownPackage))
    }

    @Test fun `other schemes and a missing scheme are refused`() {
        assertFalse(SharedFiles.isReadableAddress("android.resource", "com.other", ownPackage))
        assertFalse(SharedFiles.isReadableAddress(null, "com.other", ownPackage))
        assertFalse(SharedFiles.isReadableAddress("content", null, ownPackage))
    }

    @Test fun `a stream at the limit is read whole`() {
        assertEquals(10, SharedFiles.readCapped(ByteArrayInputStream(ByteArray(10)), 10).size)
    }

    @Test fun `a stream past the limit stops one byte over`() {
        val endless = object : java.io.InputStream() {
            var served = 0L
            override fun read(): Int { served += 1; return 7 }
            override fun read(buffer: ByteArray, offset: Int, length: Int): Int { served += length; return length }
        }
        val kept = SharedFiles.readCapped(endless, 1000)
        assertEquals(1001, kept.size)
        assertTrue(endless.served <= 1001 + 64 * 1024)
    }
}
