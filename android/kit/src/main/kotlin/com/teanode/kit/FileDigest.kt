package com.teanode.kit

import java.io.File
import java.security.MessageDigest

/** Digests of files read in chunks, so that a file of any size is checked without holding it in memory. */
object FileDigest {
    private const val CHUNK_BYTES = 64 * 1024

    fun sha256Text(file: File): String {
        val digest = MessageDigest.getInstance("SHA-256")
        val chunk = ByteArray(CHUNK_BYTES)
        file.inputStream().use { input ->
            while (true) {
                val readCount = input.read(chunk)
                if (readCount < 0) break
                digest.update(chunk, 0, readCount)
            }
        }
        return digest.digest().joinToString("") { byte -> "%02x".format(byte) }
    }
}
