package com.teanode.kit.recordings

import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeError
import com.teanode.kit.photos.ServerTime
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive

/** The knowledge source a recording is read into, as far as the Learning card shows it. */
@Serializable
data class RecordingLearningSource(
    val id: String,
    val name: String = "",
    val cron: String = "",
    val nextRunAt: String? = null,
    val lastRunAt: String? = null,
    val lastError: String = "",
)

/** Whether and how far the agent has learned from a recording's transcript. */
@Serializable
data class RecordingLearningView(
    val sourceGeneration: Long = 0,
    val hasAgent: Boolean = false,
    val knowledgeSource: RecordingLearningSource? = null,
    val pendingDocumentCount: Long = 0,
    val digestedDocumentCount: Long = 0,
    val lastPublishedSourceGeneration: Long = 0,
    val isCurationPending: Boolean = false,
    val hasStoppedReading: Boolean? = null,
)

/** What the Learning card says, one of its states. */
enum class RecordingLearningState { StoppedReading, NoAgent, Reading, WaitingForTranscript }

val RecordingLearningView.state: RecordingLearningState
    get() = when {
        hasStoppedReading == true -> RecordingLearningState.StoppedReading
        !hasAgent -> RecordingLearningState.NoAgent
        knowledgeSource != null -> RecordingLearningState.Reading
        else -> RecordingLearningState.WaitingForTranscript
    }

/** When the source next reads, or null when it reads only when asked. */
val RecordingLearningSource.nextRunEpochMS: Long? get() = ServerTime.instant(nextRunAt)?.toEpochMilli()

/** A page of the agent's memory linked to a record or to a passage of one. */
@Serializable
data class RecordingMemoryLink(
    val id: String,
    val nodePath: String = "",
    val nodeName: String = "",
    val nodeKind: String = "",
    val linkStatus: String = "",
) {
    /** A guess the agent has not yet confirmed. */
    val isProposed: Boolean get() = linkStatus == "proposed"
}

object RecordingMemory {
    /** Each page once, a statement kept over a guess. */
    fun pages(links: List<RecordingMemoryLink>): List<RecordingMemoryLink> {
        val kept = linkedMapOf<String, RecordingMemoryLink>()
        for (link in links) {
            val earlier = kept[link.nodePath]
            if (earlier == null || (earlier.isProposed && !link.isProposed)) kept[link.nodePath] = link
        }
        return kept.values.toList()
    }
}

@Serializable private class LearningAnswer(val ReadRecordingLearning: RecordingLearningView? = null, val ResumeRecordingLearning: RecordingLearningView? = null)

@Serializable private class MemoryLinksAnswer(val ReadRecordMemoryLinks: List<RecordingMemoryLink?>)

suspend fun Connection.recordingLearning(recordingId: String): RecordingLearningView =
    execute<LearningAnswer>(RecordingDocuments.READ_RECORDING_LEARNING, mapOf("recordingId" to JsonPrimitive(recordingId))).ReadRecordingLearning ?: throw TeaNodeError.UnreadableAnswer

suspend fun Connection.resumeRecordingLearning(recordingId: String, expectedSourceGeneration: Long): RecordingLearningView =
    execute<LearningAnswer>(
        RecordingDocuments.RESUME_RECORDING_LEARNING,
        mapOf("recordingId" to JsonPrimitive(recordingId), "expectedSourceGeneration" to JsonPrimitive(expectedSourceGeneration)),
    ).ResumeRecordingLearning ?: throw TeaNodeError.UnreadableAnswer

/** The pages of memory linked to a passage of a recording ("In memory"). */
suspend fun Connection.recordingPassageMemory(recordingId: String, passageStartMS: Long, passageEndMS: Long): List<RecordingMemoryLink> {
    val variables = mapOf<String, JsonElement>(
        "recordKind" to JsonPrimitive("recording_passage"), "recordId" to JsonPrimitive(recordingId),
        "passageStartMS" to JsonPrimitive(passageStartMS), "passageEndMS" to JsonPrimitive(passageEndMS),
    )
    return RecordingMemory.pages(execute<MemoryLinksAnswer>(RecordingDocuments.READ_RECORD_MEMORY_LINKS, variables).ReadRecordMemoryLinks.filterNotNull())
}

/** The attachment a recording is made from: what the chat says of it. */
data class RecordingAttachment(val id: String, val name: String, val contentType: String, val sizeBytes: Long)

/** The draft of importing an attachment: the client id is kept so a retry after a lost answer is the same import. */
data class RecordingAttachmentImportDraft(val accountKey: String, val agentAttachmentId: String, val clientRecordingId: String, val recordingTitle: String)

@Serializable private class ImportAnswer(val ImportRecordingAttachment: RecordingView)

/** Makes a recording from an attachment the agent was given, and checks the server kept that very file. */
suspend fun Connection.importRecordingAttachment(attachment: RecordingAttachment, draft: RecordingAttachmentImportDraft): RecordingView {
    if (accountKey != draft.accountKey || attachment.id != draft.agentAttachmentId) throw TeaNodeError.SignedOut
    val view = execute<ImportAnswer>(
        RecordingDocuments.IMPORT_RECORDING_ATTACHMENT,
        mapOf(
            "agentAttachmentId" to JsonPrimitive(draft.agentAttachmentId), "clientRecordingId" to JsonPrimitive(draft.clientRecordingId),
            "recordingTitle" to JsonPrimitive(draft.recordingTitle), "recordingMediaId" to JsonNull,
        ),
    ).ImportRecordingAttachment
    val original = view.mediaFiles.firstOrNull { media -> media.clientMediaId == "attachment-" + attachment.id }
    if (view.recording.clientRecordingId != draft.clientRecordingId || original == null || original.recordingId != view.recording.id ||
        original.originalFilename != attachment.name || original.originalContentType != attachment.contentType || original.originalSizeBytes != attachment.sizeBytes ||
        original.recordingUploadState != "complete" || !isRecordingDigest(original.originalSha256) || ServerTime.instant(original.verifiedAt) == null
    ) {
        throw TeaNodeError.UnreadableAnswer
    }
    return view
}

/** One event of the live socket (`/api/v1/recordings/{id}/live`) of a recording being made. */
@Serializable
data class RecordingVoiceEvent(
    val voiceEvent: String,
    val utteranceId: String? = null,
    val transcriptText: String? = null,
    val errorMessage: String? = null,
    val utteranceStartMS: Long? = null,
    val utteranceEndMS: Long? = null,
)

/**
 * What the live stream has heard so far, from the events the socket sends: the words still being said, the finished
 * utterances, and whether a sentence just ended (which the cut policy uses).
 */
class RecordingLiveWordsState {
    var isConnected = false
        private set
    var heardUtterances: List<RecordingLiveUtterance> = emptyList()
        private set
    var partialText = ""
        private set
    var partialStartMS = 0L
        private set

    /** Takes one event; true when it says an utterance ended. A refusal or error is thrown, so the session ends. */
    fun handle(event: RecordingVoiceEvent, timelineMS: Long): Boolean {
        when (event.voiceEvent) {
            "ready" -> isConnected = true
            "speechStarted" -> { partialText = ""; partialStartMS = event.utteranceStartMS ?: timelineMS }
            "speechStopped" -> return true
            "transcriptDelta" -> partialText += event.transcriptText.orEmpty()
            "transcriptFinal" -> {
                partialText = ""
                val text = event.transcriptText.orEmpty().trim()
                val id = event.utteranceId
                val start = event.utteranceStartMS
                val end = event.utteranceEndMS
                if (id != null && text.isNotEmpty() && start != null && end != null) heardUtterances = RecordingLiveWords.appending(RecordingLiveUtterance(id, start, end, text), heardUtterances)
            }
            "transcriptFailed" -> partialText = ""
            "refused", "error" -> throw TeaNodeError.Refused(event.errorMessage.orEmpty())
        }
        return false
    }

    fun disconnected() {
        isConnected = false
        partialText = ""
    }
}

@Serializable private class PassageOffsetAnswer(val ReadRecordingTranscriptPassageOffset: Int)

/** Where a cited passage is among the segments of a transcript version, so the page that holds it can be read. */
suspend fun Connection.recordingTranscriptPassageOffset(recordingTranscriptRunId: String, passageStartMS: Long, passageEndMS: Long): Int {
    if (passageStartMS < 0 || passageEndMS <= passageStartMS) throw TeaNodeError.UnreadableAnswer
    val offset = execute<PassageOffsetAnswer>(
        RecordingDocuments.READ_RECORDING_TRANSCRIPT_PASSAGE_OFFSET,
        mapOf("recordingTranscriptRunId" to JsonPrimitive(recordingTranscriptRunId), "passageStartMS" to JsonPrimitive(passageStartMS), "passageEndMS" to JsonPrimitive(passageEndMS)),
    ).ReadRecordingTranscriptPassageOffset
    if (offset < 0) throw TeaNodeError.UnreadableAnswer
    return offset
}
