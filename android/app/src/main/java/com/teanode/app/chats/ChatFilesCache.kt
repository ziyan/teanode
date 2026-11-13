package com.teanode.app.chats

import android.content.Context
import java.io.File

/** The files the app writes into its cache to hand to other apps or to take pictures into: they are not kept for long. */
object ChatFilesCache {
    /** How long a file stays: long enough for a share sheet or a camera to finish with it. */
    const val MOST_AGE_MS = 6L * 60 * 60 * 1000

    fun directory(context: Context): File = File(context.cacheDir, "chat-files").also { made -> made.mkdirs() }

    /** Removes the files older than [MOST_AGE_MS], and the voice message recordings left in the cache. */
    fun clean(context: Context, nowEpochMS: Long = System.currentTimeMillis()) {
        val doomed = File(context.cacheDir, "chat-files").listFiles().orEmpty().toList() +
            context.cacheDir.listFiles { file -> file.isFile && file.name.startsWith("Voice message") }.orEmpty().toList()
        for (file in doomed) if (nowEpochMS - file.lastModified() > MOST_AGE_MS) file.deleteRecursively()
    }

    /** A file name from another app or the server that cannot name a place outside the directory. */
    fun safeName(name: String): String {
        val leaf = name.substringAfterLast('/').substringAfterLast('\\').trim()
        return if (leaf.isEmpty() || leaf == "." || leaf == "..") "file" else leaf
    }
}
