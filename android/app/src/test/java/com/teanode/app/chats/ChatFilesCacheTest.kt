package com.teanode.app.chats

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.io.File

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class ChatFilesCacheTest {
    private val context: Context = ApplicationProvider.getApplicationContext()

    @Test fun `old files and old voice messages are removed and new ones stay`() {
        val now = 1_000_000_000_000L
        val old = File(ChatFilesCache.directory(context), "old.pdf").also { file -> file.writeText("x"); file.setLastModified(now - ChatFilesCache.MOST_AGE_MS - 1) }
        val fresh = File(ChatFilesCache.directory(context), "fresh.pdf").also { file -> file.writeText("x"); file.setLastModified(now - 1000) }
        val voice = File(context.cacheDir, "Voice message 1.m4a").also { file -> file.writeText("x"); file.setLastModified(now - ChatFilesCache.MOST_AGE_MS - 1) }
        val other = File(context.cacheDir, "tiles.bin").also { file -> file.writeText("x"); file.setLastModified(now - ChatFilesCache.MOST_AGE_MS - 1) }
        ChatFilesCache.clean(context, now)
        assertFalse(old.exists())
        assertFalse(voice.exists())
        assertTrue(fresh.exists())
        assertTrue(other.exists())
    }

    @Test fun `a name cannot leave the directory`() {
        assertEquals("passwd", ChatFilesCache.safeName("../../etc/passwd"))
        assertEquals("file", ChatFilesCache.safeName(".."))
        assertEquals("file", ChatFilesCache.safeName(""))
        assertEquals("x.txt", ChatFilesCache.safeName("a\\b\\x.txt"))
        assertEquals("report.pdf", ChatFilesCache.safeName("report.pdf"))
    }
}
