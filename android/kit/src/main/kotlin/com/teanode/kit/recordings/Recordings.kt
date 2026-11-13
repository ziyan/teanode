package com.teanode.kit.recordings

import com.teanode.kit.Connection
import com.teanode.kit.HTTPRequest
import com.teanode.kit.TeaNodeError
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.photos.ServerTime
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

/** Recording is one recording on the server: its title, where it came from, and how its capture went. */
@Serializable
data class Recording(
    val id: String,
    val userId: String? = null,
    val createdAt: String,
    val updatedAt: String,
    val clientRecordingId: String,
    val recordingTitle: String,
    /** "capture" (recorded in the app) or "imported" (a file the person added). */
    val recordingOrigin: String,
    val recordedAt: String? = null,
    /** "user" when the person named it; otherwise the server named it from what was said. */
    val recordingTitleSource: String? = null,
    val recordingSummary: String? = null,
    val recordingCaptureState: String,
    val timelineDurationMS: Long = 0,
    val sourceGeneration: Long = 0,
    val captureGaps: List<RecordingGap> = emptyList(),
)

/** A stretch of a capture that is not on the recording: paused by the person, or interrupted by a call. */
@Serializable
data class RecordingGap(
    val gapId: String,
    var gapStartMS: Long,
    var gapEndMS: Long? = null,
    val recordingGapReason: String,
) {
    fun variables(): JsonElement = JsonObject(
        mapOf(
            "gapId" to JsonPrimitive(gapId), "gapStartMS" to JsonPrimitive(gapStartMS),
            "gapEndMS" to (gapEndMS?.let { end -> JsonPrimitive(end) } ?: JsonNull), "recordingGapReason" to JsonPrimitive(recordingGapReason),
        ),
    )
}

/** One audio file of a recording and how much of it the server holds. */
@Serializable
data class RecordingMedia(
    val id: String,
    val recordingId: String,
    val clientMediaId: String,
    val originalFilename: String,
    val originalContentType: String,
    val originalSizeBytes: Long,
    val originalSha256: String,
    val timelineStartMS: Long,
    val mediaDurationMS: Long,
    val partCount: Int,
    val recordingUploadState: String,
    val verifiedAt: String? = null,
) {
    /** Where in this file a passage that starts at this point of the recording begins. */
    fun playbackOffsetMS(passageStartMS: Long): Long = if (passageStartMS < timelineStartMS) 0 else passageStartMS - timelineStartMS

    /** Whether this is the original the phone asked the server to keep, verified and complete. */
    fun confirmsOriginal(expected: RecordingMedia): Boolean =
        id == expected.id && recordingId == expected.recordingId && clientMediaId == expected.clientMediaId &&
            originalSizeBytes == expected.originalSizeBytes && originalSha256 == expected.originalSha256 &&
            recordingUploadState == "complete" && ServerTime.instant(verifiedAt) != null
}

@Serializable
data class RecordingMediaPart(
    val recordingMediaId: String,
    val partIndex: Int,
    val partSizeBytes: Long,
    val partSha256: String,
    val storedAt: String,
) {
    fun confirmsPart(mediaId: String, partIndex: Int, partSizeBytes: Long, partSha256: String): Boolean =
        recordingMediaId == mediaId && this.partIndex == partIndex && this.partSizeBytes == partSizeBytes &&
            this.partSha256 == partSha256 && ServerTime.instant(storedAt) != null
}

/** A recording with its audio files, the parts of them the server holds, and its transcript runs. */
@Serializable
data class RecordingView(
    val recording: Recording,
    val mediaFiles: List<RecordingMedia> = emptyList(),
    val uploadParts: List<RecordingMediaPart> = emptyList(),
    val transcriptRuns: List<RecordingTranscriptRun>? = null,
)

/** What the server takes and whether it can transcribe. */
@Serializable
data class RecordingLimits(
    val maximumMediaBytes: Long,
    val maximumAccountBytes: Long,
    val uploadPartBytes: Long,
    val isTranscriptionAvailable: Boolean? = null,
    val transcriptionUnavailableMessage: String? = null,
    val transcriptionProviderModelName: String? = null,
)

@Serializable private class ListRecordingsAnswer(val ListRecordings: List<Recording>)

@Serializable private class ReadRecordingAnswer(val ReadRecording: RecordingView)

@Serializable private class ReadRecordingLimitsAnswer(val ReadRecordingLimits: RecordingLimits)

@Serializable private class CreateRecordingAnswer(val CreateRecording: Recording)

@Serializable private class CreateRecordingMediaAnswer(val CreateRecordingMedia: RecordingMedia)

@Serializable private class UpdateRecordingCaptureAnswer(val UpdateRecordingCapture: Recording)

@Serializable private class UpdateRecordingMetadataAnswer(val UpdateRecordingMetadata: Recording)

@Serializable private class DeleteRecordingAnswer(val DeleteRecording: Boolean = true)

@Serializable private class RecordingRefusal(val error: String? = null)

/** What is checked of a part of an original: eight megabytes at most. */
const val RECORDING_PART_BYTES = 8 * 1024 * 1024

fun isRecordingDigest(digest: String): Boolean = digest.length == 64 && digest.all { character -> character in '0'..'9' || character in 'a'..'f' }

fun isRecordingIdentifier(identifier: String): Boolean =
    identifier.isNotEmpty() && identifier.all { character -> character in '0'..'9' || character in 'A'..'Z' || character in 'a'..'z' || character == '-' || character == '_' }

suspend fun Connection.recordings(beforeRecordingId: String = "", pageSize: Int = 50, searchText: String = ""): List<Recording> {
    val variables = mutableMapOf<String, JsonElement>("beforeRecordingId" to JsonPrimitive(beforeRecordingId), "pageSize" to JsonPrimitive(pageSize))
    if (searchText.isNotEmpty()) variables["searchText"] = JsonPrimitive(searchText)
    return execute<ListRecordingsAnswer>(RecordingDocuments.LIST_RECORDINGS, variables).ListRecordings
}

suspend fun Connection.recording(recordingId: String): RecordingView =
    execute<ReadRecordingAnswer>(RecordingDocuments.READ_RECORDING, mapOf("recordingId" to JsonPrimitive(recordingId))).ReadRecording

suspend fun Connection.recordingLimits(): RecordingLimits = execute<ReadRecordingLimitsAnswer>(RecordingDocuments.RECORDING_LIMITS).ReadRecordingLimits

/** Starts a recording on the server. The server must answer with the recording that was asked for, or it is not believed. */
suspend fun Connection.createRecording(clientRecordingId: String, recordingTitle: String, recordingOrigin: String): Recording {
    val recording = execute<CreateRecordingAnswer>(
        RecordingDocuments.CREATE_RECORDING,
        mapOf(
            "clientRecordingId" to JsonPrimitive(clientRecordingId), "recordingTitle" to JsonPrimitive(recordingTitle),
            "recordingOrigin" to JsonPrimitive(recordingOrigin),
        ),
    ).CreateRecording
    if (recording.clientRecordingId != clientRecordingId || recording.recordingOrigin != recordingOrigin || !isRecordingIdentifier(recording.id)) {
        throw TeaNodeError.UnreadableAnswer
    }
    return recording
}

suspend fun Connection.createRecordingMedia(
    recordingId: String,
    clientMediaId: String,
    originalFilename: String,
    originalContentType: String,
    originalSizeBytes: Long,
    originalSha256: String,
    timelineStartMS: Long,
    mediaDurationMS: Long,
): RecordingMedia {
    val media = execute<CreateRecordingMediaAnswer>(
        RecordingDocuments.CREATE_RECORDING_MEDIA,
        mapOf(
            "recordingId" to JsonPrimitive(recordingId), "clientMediaId" to JsonPrimitive(clientMediaId), "originalFilename" to JsonPrimitive(originalFilename),
            "originalContentType" to JsonPrimitive(originalContentType), "originalSizeBytes" to JsonPrimitive(originalSizeBytes),
            "originalSha256" to JsonPrimitive(originalSha256), "timelineStartMS" to JsonPrimitive(timelineStartMS),
            "mediaDurationMS" to JsonPrimitive(mediaDurationMS),
        ),
    ).CreateRecordingMedia
    val expectedPartCount = ((originalSizeBytes + RECORDING_PART_BYTES - 1) / RECORDING_PART_BYTES).toInt()
    if (originalSizeBytes <= 0 || originalSizeBytes > Long.MAX_VALUE - RECORDING_PART_BYTES || media.recordingId != recordingId ||
        media.clientMediaId != clientMediaId || !isRecordingIdentifier(media.id) || media.originalSizeBytes != originalSizeBytes ||
        media.originalSha256 != originalSha256 || media.originalFilename != originalFilename || media.originalContentType != originalContentType ||
        media.timelineStartMS != timelineStartMS || media.mediaDurationMS != mediaDurationMS || media.partCount != expectedPartCount
    ) {
        throw TeaNodeError.UnreadableAnswer
    }
    return media
}

suspend fun Connection.updateRecordingCapture(
    recordingId: String,
    expectedGeneration: Long,
    recordingCaptureState: String,
    timelineDurationMS: Long,
    captureGaps: List<RecordingGap>,
): Recording = execute<UpdateRecordingCaptureAnswer>(
    RecordingDocuments.UPDATE_RECORDING_CAPTURE,
    mapOf(
        "recordingId" to JsonPrimitive(recordingId), "expectedGeneration" to JsonPrimitive(expectedGeneration),
        "recordingCaptureState" to JsonPrimitive(recordingCaptureState), "timelineDurationMS" to JsonPrimitive(timelineDurationMS),
        "captureGaps" to JsonArray(captureGaps.map { gap -> gap.variables() }),
    ),
).UpdateRecordingCapture

suspend fun Connection.updateRecordingMetadata(recordingId: String, expectedSourceGeneration: Long, recordingTitle: String?, recordedAt: String?): Recording =
    execute<UpdateRecordingMetadataAnswer>(
        RecordingDocuments.UPDATE_RECORDING_METADATA,
        mapOf(
            "recordingId" to JsonPrimitive(recordingId), "expectedSourceGeneration" to JsonPrimitive(expectedSourceGeneration),
            "recordingTitle" to (recordingTitle?.let { title -> JsonPrimitive(title) } ?: JsonNull),
            "recordedAt" to (recordedAt?.let { time -> JsonPrimitive(time) } ?: JsonNull),
        ),
    ).UpdateRecordingMetadata

suspend fun Connection.deleteRecording(recordingId: String) {
    execute<DeleteRecordingAnswer>(RecordingDocuments.DELETE_RECORDING, mapOf("recordingId" to JsonPrimitive(recordingId)))
}

private suspend inline fun <reified Receipt> Connection.recordingMediaRequest(
    mediaId: String,
    suffix: String,
    method: String,
    content: ByteArray,
    partSha256: String = "",
): Receipt {
    if (!isRecordingIdentifier(mediaId)) throw TeaNodeError.UnreadableAnswer
    val headers = mutableMapOf("Content-Type" to if (partSha256.isEmpty()) "application/json" else "application/octet-stream")
    if (partSha256.isNotEmpty()) headers["X-Recording-Part-SHA256"] = partSha256
    val response = send(HTTPRequest(account.server.endpoint("/api/v1/recordings/media/$mediaId$suffix").toString(), method, headers, content))
    if (response.statusCode == 410) throw TeaNodeError.RecordingDeleted
    if (response.body.size > 32 * 1024) throw TeaNodeError.UnreadableAnswer
    if (response.statusCode != 200) {
        val reason = runCatching { TeaNodeJson.decodeFromString(RecordingRefusal.serializer(), response.text).error }.getOrNull()
        throw TeaNodeError.Refused(reason ?: "The recording request was refused. Keep the original and retry.")
    }
    return try {
        TeaNodeJson.decodeFromString(response.text)
    } catch (_: Exception) {
        throw TeaNodeError.UnreadableAnswer
    }
}

/** Sends one part of an original and checks the server's receipt names that part. */
suspend fun Connection.putRecordingPart(mediaId: String, partIndex: Int, content: ByteArray, partSha256: String): RecordingMediaPart {
    if (partIndex < 0 || content.isEmpty() || content.size > RECORDING_PART_BYTES || !isRecordingDigest(partSha256)) throw TeaNodeError.UnreadableAnswer
    val receipt = recordingMediaRequest<RecordingMediaPart>(mediaId, "/parts/$partIndex", "PUT", content, partSha256)
    if (!receipt.confirmsPart(mediaId, partIndex, content.size.toLong(), partSha256)) throw TeaNodeError.UnreadableAnswer
    return receipt
}

suspend fun Connection.completeRecordingUpload(expected: RecordingMedia): RecordingMedia {
    val receipt = recordingMediaRequest<RecordingMedia>(expected.id, "/complete", "POST", "{}".toByteArray())
    if (!receipt.confirmsOriginal(expected)) throw TeaNodeError.UnreadableAnswer
    return receipt
}

/** The address and headers a player reads an original with. */
suspend fun Connection.recordingPlaybackRequest(mediaId: String): HTTPRequest {
    if (!isRecordingIdentifier(mediaId)) throw TeaNodeError.UnreadableAnswer
    return HTTPRequest(
        account.server.endpoint("/api/v1/recordings/media/$mediaId").toString(), "GET", mapOf("Authorization" to "Bearer ${accessToken()}"),
    )
}
