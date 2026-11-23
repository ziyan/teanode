package com.teanode.kit.recordings

import com.teanode.kit.Connection
import com.teanode.kit.HTTPRequest
import com.teanode.kit.TeaNodeError
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonPrimitive

/** A copy of an original the server made in a form every player reads (a WAV), when the original itself is not one. */
@Serializable
data class RecordingPlayback(
    val recordingMediaId: String,
    val playbackPath: String,
    val playbackContentType: String,
    val playbackSizeBytes: Long,
    val playbackSha256: String,
    val playbackDurationMS: Long,
    val timelineStartMS: Long,
) {
    /** Whether this is the playback of exactly this file: nothing else is played in its place. */
    fun confirmsMedia(media: RecordingMedia): Boolean =
        isRecordingIdentifier(media.id) && recordingMediaId == media.id && playbackPath == "/api/v1/recordings/media/${media.id}/playback" &&
            playbackContentType == "audio/wav" && playbackSizeBytes > 0 && isRecordingDigest(playbackSha256) && playbackDurationMS > 0 &&
            timelineStartMS == media.timelineStartMS
}

@Serializable private class ReadPlaybackAnswer(val ReadRecordingPlayback: RecordingPlayback? = null)

@Serializable private class PreparePlaybackAnswer(val PrepareRecordingPlayback: RecordingPlayback)

/** How long making a playback copy of a long recording may take. */
const val RECORDING_PREPARE_TIMEOUT_MS: Long = 11L * 60 * 1000

suspend fun Connection.recordingPlayback(media: RecordingMedia): RecordingPlayback? {
    if (!isRecordingIdentifier(media.id)) throw TeaNodeError.UnreadableAnswer
    val playback = execute<ReadPlaybackAnswer>(RecordingDocuments.READ_RECORDING_PLAYBACK, mapOf("recordingMediaId" to JsonPrimitive(media.id))).ReadRecordingPlayback
    if (playback != null && !playback.confirmsMedia(media)) throw TeaNodeError.UnreadableAnswer
    return playback
}

suspend fun Connection.prepareRecordingPlayback(media: RecordingMedia): RecordingPlayback {
    if (!isRecordingIdentifier(media.id)) throw TeaNodeError.UnreadableAnswer
    val playback = execute<PreparePlaybackAnswer>(
        RecordingDocuments.PREPARE_RECORDING_PLAYBACK, mapOf("recordingMediaId" to JsonPrimitive(media.id)), RECORDING_PREPARE_TIMEOUT_MS,
    ).PrepareRecordingPlayback
    if (!playback.confirmsMedia(media)) throw TeaNodeError.UnreadableAnswer
    return playback
}

suspend fun Connection.recordingCompatiblePlaybackRequest(playback: RecordingPlayback, media: RecordingMedia): HTTPRequest {
    if (!playback.confirmsMedia(media)) throw TeaNodeError.UnreadableAnswer
    return HTTPRequest(account.server.endpoint(playback.playbackPath).toString(), "GET", mapOf("Authorization" to "Bearer ${accessToken()}"))
}
