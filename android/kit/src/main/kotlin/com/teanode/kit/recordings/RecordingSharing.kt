package com.teanode.kit.recordings

import com.teanode.kit.Connection
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive

/** What a recording shared with somebody gives them: neither level gives the audio, a voice sample or the contact a speaker's name is tied to. */
enum class RecordingShareLevel(val serverName: String) {
    Summary("summary"),
    Transcript("transcript");

    val canReadTranscript: Boolean get() = this == Transcript

    companion object {
        val grantable: List<RecordingShareLevel> = listOf(Summary, Transcript)

        fun named(serverName: String): RecordingShareLevel? = entries.firstOrNull { level -> level.serverName == serverName }
    }
}

@Serializable
data class RecordingShare(
    val id: String,
    val recordingId: String = "",
    val granteeUserId: String? = null,
    val granteeUsername: String? = null,
    val granteeName: String? = null,
    val granteeCircleId: String? = null,
    val granteeCircleName: String? = null,
    val recordingShareLevel: String,
) {
    val level: RecordingShareLevel? get() = RecordingShareLevel.named(recordingShareLevel)
    val isWithCircle: Boolean get() = granteeCircleId != null
    val granteeDisplayName: String
        get() = if (isWithCircle) granteeCircleName.orEmpty() else granteeName?.takeIf { name -> name.isNotEmpty() } ?: granteeUsername.orEmpty()
}

@Serializable
data class SharedRecordingPassage(
    val segmentStartMS: Long,
    val segmentEndMS: Long,
    val speakerName: String = "",
    val transcriptText: String = "",
    val isFiller: Boolean = false,
)

/** What one speaker said in a row: the passages of a shared transcript with the same speaker joined. */
data class SharedRecordingTurn(val id: Int, val speakerName: String, val startMS: Long, val text: String) {
    companion object {
        fun turns(passages: List<SharedRecordingPassage>): List<SharedRecordingTurn> {
            val turns = mutableListOf<SharedRecordingTurn>()
            for (passage in passages) {
                if (passage.isFiller) continue
                val text = passage.transcriptText.trim()
                if (text.isEmpty()) continue
                val last = turns.lastOrNull()
                if (last != null && last.speakerName == passage.speakerName) {
                    turns[turns.size - 1] = last.copy(text = last.text + " " + text)
                } else {
                    turns.add(SharedRecordingTurn(turns.size, passage.speakerName, passage.segmentStartMS, text))
                }
            }
            return turns
        }
    }
}

@Serializable
data class SharedRecording(
    val recordingId: String,
    val ownerUserId: String = "",
    val ownerUsername: String = "",
    val ownerName: String = "",
    val recordingTitle: String = "",
    val createdAt: String = "",
    val recordedAt: String? = null,
    val timelineDurationMS: Long = 0,
    val recordingSummary: String = "",
    val recordingShareLevel: String = "summary",
    val speakerNames: List<String>? = null,
    val passages: List<SharedRecordingPassage>? = null,
    val passageCount: Int? = null,
    val hasMorePassages: Boolean? = null,
) {
    val id: String get() = recordingId
    val level: RecordingShareLevel? get() = RecordingShareLevel.named(recordingShareLevel)
    val ownerDisplayName: String get() = ownerName.ifEmpty { ownerUsername }
    val turns: List<SharedRecordingTurn> get() = SharedRecordingTurn.turns(passages.orEmpty())
}

@Serializable private class SharesAnswer(val ListRecordingShares: List<RecordingShare>)

@Serializable private class ShareAnswer(val ShareRecording: IdOnly)

@Serializable private class IdOnly(val id: String)

@Serializable private class UnshareAnswer(val UnshareRecording: Boolean? = null)

@Serializable private class SharedListAnswer(val ListSharedRecordings: List<SharedRecording>)

@Serializable private class SharedReadAnswer(val ReadSharedRecording: SharedRecording)

suspend fun Connection.recordingShares(recordingId: String): List<RecordingShare> =
    execute<SharesAnswer>(RecordingDocuments.RECORDING_SHARES, mapOf("recordingId" to JsonPrimitive(recordingId))).ListRecordingShares

suspend fun Connection.shareRecording(recordingId: String, granteeUserId: String?, granteeCircleId: String?, level: RecordingShareLevel) {
    execute<ShareAnswer>(
        RecordingDocuments.SHARE_RECORDING,
        mapOf<String, JsonElement>(
            "recordingId" to JsonPrimitive(recordingId), "granteeUserId" to (granteeUserId?.let { user -> JsonPrimitive(user) } ?: JsonNull),
            "granteeCircleId" to (granteeCircleId?.let { circle -> JsonPrimitive(circle) } ?: JsonNull), "recordingShareLevel" to JsonPrimitive(level.serverName),
        ),
    )
}

suspend fun Connection.unshareRecording(shareId: String) {
    execute<UnshareAnswer>(RecordingDocuments.UNSHARE_RECORDING, mapOf("recordingShareId" to JsonPrimitive(shareId)))
}

suspend fun Connection.sharedRecordings(beforeRecordingId: String? = null, pageSize: Int = 50): List<SharedRecording> {
    val variables = mutableMapOf<String, JsonElement>("pageSize" to JsonPrimitive(pageSize))
    if (beforeRecordingId != null) variables["beforeRecordingId"] = JsonPrimitive(beforeRecordingId)
    return execute<SharedListAnswer>(RecordingDocuments.SHARED_RECORDINGS, variables).ListSharedRecordings
}

suspend fun Connection.sharedRecording(recordingId: String, passageOffset: Int = 0, pageSize: Int = 50): SharedRecording =
    execute<SharedReadAnswer>(
        RecordingDocuments.SHARED_RECORDING,
        mapOf("recordingId" to JsonPrimitive(recordingId), "passageOffset" to JsonPrimitive(passageOffset), "pageSize" to JsonPrimitive(pageSize)),
    ).ReadSharedRecording
