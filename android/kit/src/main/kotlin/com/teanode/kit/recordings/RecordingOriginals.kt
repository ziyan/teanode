package com.teanode.kit.recordings

import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeError
import java.security.MessageDigest

/** The original files of a recording, as the person shares them (ios RecordingPlaybackScreen's "Share full original"). */
object RecordingOriginals {
    fun sha256Text(bytes: ByteArray): String = MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { byte -> "%02x".format(byte) }

    /** Whether the bytes are the file the server recorded: the same length and the same digest, nothing else is handed on. */
    fun isVerified(bytes: ByteArray, media: RecordingMedia): Boolean = bytes.size.toLong() == media.originalSizeBytes && sha256Text(bytes) == media.originalSha256

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

/** Reads one original from the server, and refuses it when it is not the file the server says it holds. */
suspend fun Connection.downloadRecordingOriginal(media: RecordingMedia): ByteArray {
    val response = send(recordingPlaybackRequest(media.id))
    if (response.statusCode != 200 || !RecordingOriginals.isVerified(response.body, media)) throw TeaNodeError.UnreadableAnswer
    return response.body
}
