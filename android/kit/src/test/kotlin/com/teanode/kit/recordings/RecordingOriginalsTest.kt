package com.teanode.kit.recordings

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class RecordingOriginalsTest {
    private fun media(id: String, startMS: Long = 0, durationMS: Long = 60_000, state: String = "complete", bytes: ByteArray = "abc".toByteArray()) = RecordingMedia(
        id = id, recordingId = "r1", clientMediaId = "c-$id", originalFilename = "meeting.m4a", originalContentType = "audio/mp4",
        originalSizeBytes = bytes.size.toLong(), originalSha256 = RecordingOriginals.sha256Text(bytes), timelineStartMS = startMS,
        mediaDurationMS = durationMS, partCount = 1, recordingUploadState = state,
    )

    @Test
    fun theDigestIsTheOneEveryToolWrites() {
        assertEquals("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", RecordingOriginals.sha256Text("abc".toByteArray()))
    }

    @Test
    fun onlyTheFileTheServerRecordedIsVerified() {
        val file = media("m1")
        assertTrue(RecordingOriginals.isVerified("abc".toByteArray(), file))
        assertFalse(RecordingOriginals.isVerified("abd".toByteArray(), file))
        assertFalse(RecordingOriginals.isVerified("abcd".toByteArray(), file))
    }

    @Test
    fun onlyCompleteOriginalsAreOfferedInTheOrderOfTheRecording() {
        val listed = RecordingOriginals.completeFiles(listOf(media("late", startMS = 60_000), media("partial", state = "uploading"), media("early")))
        assertEquals(listOf("early", "late"), listed.map { file -> file.id })
    }

    @Test
    fun aRecordingInPiecesTellsItsOriginalsByWhereTheySit() {
        val first = media("m1")
        val second = media("m2", startMS = 3_661_000, durationMS = 120_000)
        assertEquals("meeting.m4a", RecordingOriginals.title(first, 1) { start, end -> "$start to $end" })
        assertEquals("0:00:00 to 0:01:00", RecordingOriginals.title(first, 2) { start, end -> "$start to $end" })
        assertEquals("1:01:01 to 1:03:01", RecordingOriginals.title(second, 2) { start, end -> "$start to $end" })
    }
}

/** A server that streams a large file in chunks and fails if anything asks for it whole. */
private class StreamingServer(private val totalBytes: Long, private val statusCode: Int = 200) : com.teanode.kit.HTTPTransport {
    override suspend fun send(request: com.teanode.kit.HTTPRequest): com.teanode.kit.HTTPResponse = throw AssertionError("the file must not be read whole")

    override suspend fun download(request: com.teanode.kit.HTTPRequest, destination: java.io.File): com.teanode.kit.HTTPResponse {
        destination.outputStream().use { output ->
            val chunk = ByteArray(1024 * 1024) { index -> (index % 251).toByte() }
            var written = 0L
            while (written < totalBytes) {
                val count = minOf(chunk.size.toLong(), totalBytes - written).toInt()
                output.write(chunk, 0, count)
                written += count
            }
        }
        return com.teanode.kit.HTTPResponse(statusCode)
    }
}

class RecordingOriginalDownloadTest {
    private val totalBytes = 192L * 1024 * 1024

    private fun connection(server: com.teanode.kit.HTTPTransport) = com.teanode.kit.Connection(
        com.teanode.kit.Account(com.teanode.kit.ServerAddress.parse("mail.example.com"), "a-client", "tnt", "", Long.MAX_VALUE / 4, "robin"), server,
    )

    private fun scratch(): java.io.File = java.io.File("build/tmp").also { folder -> folder.mkdirs() }.let { folder -> java.io.File.createTempFile("original-", ".bin", folder) }

    private fun expectedDigest(): String {
        val digest = java.security.MessageDigest.getInstance("SHA-256")
        val chunk = ByteArray(1024 * 1024) { index -> (index % 251).toByte() }
        var left = totalBytes
        while (left > 0) { val count = minOf(chunk.size.toLong(), left).toInt(); digest.update(chunk, 0, count); left -= count }
        return digest.digest().joinToString("") { byte -> "%02x".format(byte) }
    }

    private fun media(sizeBytes: Long, sha: String) = RecordingMedia(
        id = "m1", recordingId = "r1", clientMediaId = "c1", originalFilename = "long.m4a", originalContentType = "audio/mp4", originalSizeBytes = sizeBytes,
        originalSha256 = sha, timelineStartMS = 0, mediaDurationMS = 3_600_000, partCount = 1, recordingUploadState = "complete",
    )

    @Test
    fun aLargeOriginalIsStreamedToAFileAndVerifiedWithoutBeingReadWhole() {
        val file = scratch()
        try {
            kotlinx.coroutines.runBlocking { connection(StreamingServer(totalBytes)).downloadRecordingOriginal(media(totalBytes, expectedDigest()), file) }
            assertEquals(totalBytes, file.length())
        } finally { file.delete() }
    }

    @Test
    fun anOriginalThatIsNotTheRecordedFileIsRefusedAndDeleted() {
        val file = scratch()
        try {
            org.junit.jupiter.api.Assertions.assertThrows(com.teanode.kit.TeaNodeError::class.java) {
                kotlinx.coroutines.runBlocking { connection(StreamingServer(totalBytes)).downloadRecordingOriginal(media(totalBytes, "0".repeat(64)), file) }
            }
            assertFalse(file.exists())
        } finally { file.delete() }
    }

    @Test
    fun aRefusedDownloadIsNotShared() {
        val file = scratch()
        try {
            org.junit.jupiter.api.Assertions.assertThrows(com.teanode.kit.TeaNodeError::class.java) {
                kotlinx.coroutines.runBlocking { connection(StreamingServer(1024, statusCode = 404)).downloadRecordingOriginal(media(1024, "0".repeat(64)), file) }
            }
            assertFalse(file.exists())
        } finally { file.delete() }
    }
}
