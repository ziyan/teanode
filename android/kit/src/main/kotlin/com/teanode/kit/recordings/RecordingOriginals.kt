package com.teanode.kit.recordings

import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeError
import com.teanode.kit.FileDigest
import java.io.File
import java.security.MessageDigest

/** The original files of a recording, as the person shares them (ios RecordingPlaybackScreen's "Share full original"). */
object RecordingOriginals {
    fun sha256Text(bytes: ByteArray): String = MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { byte -> "%02x".format(byte) }

    /** Whether the bytes are the file the server recorded: the same length and the same digest, nothing else is handed on. */
    fun isVerified(bytes: ByteArray, media: RecordingMedia): Boolean = bytes.size.toLong() == media.originalSizeBytes && sha256Text(bytes) == media.originalSha256

    /** Whether the file is the one the server recorded, read in chunks: the same length and the same digest. */
    fun isVerified(file: File, media: RecordingMedia): Boolean = file.length() == media.originalSizeBytes && FileDigest.sha256Text(file) == media.originalSha256

    /** The originals that are whole on the server, in the order of the recording. */
    fun completeFiles(mediaFiles: List<RecordingMedia>): List<RecordingMedia> =
        mediaFiles.filter { media -> media.recordingUploadState == "complete" }.sortedBy { media -> media.timelineStartMS }

    /**
     * What an original is called: a recording made in pieces has one original per piece, all with the same file name, so
     * each is told by where it sits on the recording.
     */
    fun title(media: RecordingMedia, pieceCount: Int, range: (startText: String, endText: String) -> String): String {
        if (pieceCount <= 1) return media.originalFilename
        return range(clockText(media.timelineStartMS), clockText(media.timelineStartMS + media.mediaDurationMS))
    }

    private fun clockText(milliseconds: Long): String {
        val seconds = milliseconds / 1000
        return "%d:%02d:%02d".format(seconds / 3600, seconds % 3600 / 60, seconds % 60)
    }
}

/**
 * Reads one original from the server into [destination] as it arrives, and refuses it (deleting what was written) when it
 * is not the file the server says it holds. The file is never held whole in memory.
 */
suspend fun Connection.downloadRecordingOriginal(media: RecordingMedia, destination: File) {
    val response = download(recordingPlaybackRequest(media.id), destination)
    if (response.statusCode != 200 || !RecordingOriginals.isVerified(destination, media)) {
        destination.delete()
        throw TeaNodeError.UnreadableAnswer
    }
}
