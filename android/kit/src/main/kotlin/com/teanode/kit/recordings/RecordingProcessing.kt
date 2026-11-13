package com.teanode.kit.recordings

import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeError
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive

/** One try, by one model, to turn a recording's audio into text. */
@Serializable
data class RecordingTranscriptRun(
    val id: String,
    val recordingId: String,
    val recordingMediaId: String,
    val versionNumber: Int = 1,
    /** "queued", "preparing", "transcribing", "complete", "failed" or "cancelled". */
    val recordingProcessingState: String,
    val providerModelName: String = "",
    val processingErrorMessage: String = "",
    val createdAt: String = "",
) {
    val isRunning: Boolean get() = recordingProcessingState in RUNNING_STATES

    companion object {
        val RUNNING_STATES = setOf("queued", "preparing", "transcribing")
    }
}

/** One passage of a transcript, with who said it as the server worked it out and as the person corrected it. */
@Serializable
data class RecordingTranscriptSegment(
    val id: String,
    val recordingTranscriptRunId: String = "",
    val recordingTranscriptionAttemptId: String = "",
    val localSpeakerId: String = "",
    val segmentStartMS: Long,
    val segmentEndMS: Long,
    val transcriptText: String,
    val effectiveTranscriptText: String? = null,
    val appliedTextCorrectionId: String? = null,
    val hasUnalignedTextCorrection: Boolean? = null,
    val effectiveSpeakerIdentityId: String? = null,
    val effectiveSpeakerName: String? = null,
    val hasUserConfirmedSpeaker: Boolean? = null,
    val isRecognizedSpeaker: Boolean? = null,
    val isAccountOwner: Boolean? = null,
    val hasUnalignedSpeakerCorrection: Boolean? = null,
    val appliedSpeakerCorrectionId: String? = null,
    val temporarySpeakerLabel: String? = null,
    val mergedTemporarySpeakerLabel: String? = null,
    val isFiller: Boolean? = null,
) {
    companion object

    /** The speaker's name when the person confirmed it or the server recognized the voice; null for a guess. */
    val confirmedSpeakerName: String?
        get() {
            if (hasUserConfirmedSpeaker != true && isRecognizedSpeaker != true) return null
            if (effectiveSpeakerIdentityId.isNullOrEmpty() || effectiveSpeakerName.isNullOrEmpty()) return null
            return effectiveSpeakerName
        }

    /** The words as shown: the person's correction when there is one. */
    val displayedTranscriptText: String get() = effectiveTranscriptText ?: transcriptText
}

@Serializable
data class RecordingTranscriptionAttempt(
    val id: String,
    val recordingAttemptState: String = "",
    val providerModelName: String = "",
    val hasProviderUsage: Boolean = false,
    val estimatedCostAmount: Double? = null,
    val actualCostAmount: Double? = null,
    val currencyCode: String = "",
    val attemptErrorMessage: String = "",
)

/** A page of a transcript. */
@Serializable
data class RecordingTranscriptView(
    val transcriptRun: RecordingTranscriptRun,
    val segments: List<RecordingTranscriptSegment> = emptyList(),
    val attempts: List<RecordingTranscriptionAttempt> = emptyList(),
    val segmentCount: Int = 0,
    val hasMoreSegments: Boolean = false,
)

@Serializable
data class RecordingTemporarySpeaker(val speakerLabel: String, val speakerIdentityId: String? = null, val mergedIntoSpeakerLabel: String? = null)

/** Words said while the recording is still being made, before the server has transcribed it properly. */
@Serializable
data class RecordingLiveUtterance(val utteranceId: String, val utteranceStartMS: Long, val utteranceEndMS: Long, val transcriptText: String)

/** A whole recording's transcript across its files, with the speakers the server tells apart. */
@Serializable
data class RecordingTranscriptTimeline(
    val sourceGeneration: Long = 0,
    val segments: List<RecordingTranscriptSegment> = emptyList(),
    val transcriptRuns: List<RecordingTranscriptRun> = emptyList(),
    val segmentCount: Int = 0,
    val hasMoreSegments: Boolean = false,
    val temporarySpeakers: List<RecordingTemporarySpeaker>? = null,
    val liveUtterances: List<RecordingLiveUtterance>? = null,
)

/** Somebody the person named as a speaker, and the contact the name is tied to. */
@Serializable
data class RecordingSpeakerIdentity(
    val id: String,
    val confirmedName: String,
    val isAccountOwner: Boolean? = null,
    val contactAddressBookId: String? = null,
    val contactId: String? = null,
    val contactName: String? = null,
) {
    val isTiedToContact: Boolean get() = !contactId.isNullOrEmpty() && !contactAddressBookId.isNullOrEmpty()
}

@Serializable
data class RecordingSpeakerCorrection(
    val id: String,
    val recordingMediaId: String = "",
    val recordingTranscriptionAttemptId: String = "",
    val speakerIdentityId: String = "",
    val clientCorrectionId: String? = null,
    val localSpeakerId: String = "",
    val passageStartMS: Long = 0,
    val passageEndMS: Long = 0,
    val sourceGeneration: Long = 0,
    val isRemoved: Boolean = false,
)

/** A passage the server keeps to recognize a voice again. */
@Serializable
data class RecordingVoiceReference(
    val id: String,
    val speakerIdentityId: String = "",
    val recordingMediaId: String = "",
    val recordingTranscriptRunId: String = "",
    val passageStartMS: Long = 0,
    val passageEndMS: Long = 0,
    val sourceGeneration: Long = 0,
    val isRevoked: Boolean = false,
)

@Serializable
data class RecordingSpeakersView(
    val sourceGeneration: Long = 0,
    val speakerIdentities: List<RecordingSpeakerIdentity> = emptyList(),
    val speakerCorrections: List<RecordingSpeakerCorrection> = emptyList(),
    val voiceReferences: List<RecordingVoiceReference> = emptyList(),
)

/** A name to offer for a speaker, from earlier recordings or the person's contacts. */
@Serializable
data class RecordingSpeakerNameSuggestion(
    val confirmedName: String,
    val speakerNameSource: String = "",
    val addressBookId: String? = null,
    val contactId: String? = null,
)

@Serializable
data class RecordingTranscriptTextCorrection(
    val id: String,
    val recordingId: String = "",
    val recordingMediaId: String = "",
    val recordingTranscriptRunId: String = "",
    val recordingTranscriptSegmentId: String = "",
    val clientCorrectionId: String = "",
    val passageStartMS: Long = 0,
    val passageEndMS: Long = 0,
    val originalTranscriptText: String = "",
    val correctedTranscriptText: String = "",
    val sourceGeneration: Long = 0,
    val createdAt: String = "",
    val isRemoved: Boolean = false,
)

@Serializable private class ReadTranscriptAnswer(val ReadRecordingTranscript: RecordingTranscriptView)

@Serializable private class TimelineAnswer(val ReadRecordingTranscriptTimeline: RecordingTranscriptTimeline)

@Serializable private class RunAnswer(
    val RequestRecordingTranscription: RecordingTranscriptRun? = null,
    val RetryRecordingTranscription: RecordingTranscriptRun? = null,
    val CancelRecordingTranscription: RecordingTranscriptRun? = null,
)

@Serializable private class SuggestAnswer(val SuggestRecordingSpeakerNames: List<RecordingSpeakerNameSuggestion>)

@Serializable private class SpeakersAnswer(
    val ReadRecordingSpeakers: RecordingSpeakersView? = null,
    val CorrectRecordingSpeaker: RecordingSpeakersView? = null,
    val RenameRecordingSpeaker: RecordingSpeakersView? = null,
    val SetRecordingSpeakerContact: RecordingSpeakersView? = null,
    val RemoveRecordingSpeakerCorrection: RecordingSpeakersView? = null,
    val UndoRecordingSpeakerNaming: RecordingSpeakersView? = null,
    val DeleteRecordingVoiceReference: RecordingSpeakersView? = null,
)

@Serializable private class TextCorrectionsAnswer(
    val ReadRecordingTranscriptTextCorrections: List<RecordingTranscriptTextCorrection>? = null,
    val CorrectRecordingTranscriptText: RecordingTranscriptTextCorrection? = null,
)

@Serializable private class AcknowledgedAnswer(
    val NameRecordingTemporarySpeaker: Boolean? = null,
    val SeparateRecordingTemporarySpeaker: Boolean? = null,
    val RemoveRecordingTranscriptTextCorrection: Boolean? = null,
)

private fun text(value: String): JsonElement = JsonPrimitive(value)

private fun nullableText(value: String?): JsonElement = if (value == null) JsonNull else JsonPrimitive(value)

suspend fun Connection.recordingTranscript(recordingTranscriptRunId: String, segmentOffset: Int = 0, pageSize: Int = 100): RecordingTranscriptView =
    execute<ReadTranscriptAnswer>(
        RecordingDocuments.READ_RECORDING_TRANSCRIPT,
        mapOf("recordingTranscriptRunId" to text(recordingTranscriptRunId), "segmentOffset" to JsonPrimitive(segmentOffset), "pageSize" to JsonPrimitive(pageSize)),
    ).ReadRecordingTranscript

suspend fun Connection.recordingTranscriptTimeline(
    recordingId: String,
    segmentOffset: Int = 0,
    pageSize: Int = 100,
    isUnnamedOnly: Boolean = false,
): RecordingTranscriptTimeline {
    val variables = mutableMapOf<String, JsonElement>("recordingId" to text(recordingId), "segmentOffset" to JsonPrimitive(segmentOffset), "pageSize" to JsonPrimitive(pageSize))
    if (isUnnamedOnly) variables["isUnnamedOnly"] = JsonPrimitive(true)
    return execute<TimelineAnswer>(RecordingDocuments.READ_RECORDING_TRANSCRIPT_TIMELINE, variables).ReadRecordingTranscriptTimeline
}

/** Asks for a transcript of an original; the client run id makes a repeated ask the same ask. */
suspend fun Connection.requestRecordingTranscription(recordingMediaId: String, clientRunId: String, isReprocessingRequested: Boolean): RecordingTranscriptRun =
    execute<RunAnswer>(
        RecordingDocuments.REQUEST_RECORDING_TRANSCRIPTION,
        mapOf("recordingMediaId" to text(recordingMediaId), "clientRunId" to text(clientRunId), "isReprocessingRequested" to JsonPrimitive(isReprocessingRequested)),
    ).RequestRecordingTranscription ?: throw TeaNodeError.UnreadableAnswer

suspend fun Connection.retryRecordingTranscription(recordingTranscriptRunId: String): RecordingTranscriptRun =
    execute<RunAnswer>(RecordingDocuments.RETRY_RECORDING_TRANSCRIPTION, mapOf("recordingTranscriptRunId" to text(recordingTranscriptRunId))).RetryRecordingTranscription
        ?: throw TeaNodeError.UnreadableAnswer

suspend fun Connection.cancelRecordingTranscription(recordingTranscriptRunId: String): RecordingTranscriptRun =
    execute<RunAnswer>(RecordingDocuments.CANCEL_RECORDING_TRANSCRIPTION, mapOf("recordingTranscriptRunId" to text(recordingTranscriptRunId))).CancelRecordingTranscription
        ?: throw TeaNodeError.UnreadableAnswer

suspend fun Connection.suggestRecordingSpeakerNames(searchText: String = "", pageSize: Int = 10): List<RecordingSpeakerNameSuggestion> =
    execute<SuggestAnswer>(
        RecordingDocuments.SUGGEST_RECORDING_SPEAKER_NAMES, mapOf("searchText" to text(searchText), "pageSize" to JsonPrimitive(pageSize)),
    ).SuggestRecordingSpeakerNames

suspend fun Connection.recordingSpeakers(recordingId: String): RecordingSpeakersView =
    execute<SpeakersAnswer>(RecordingDocuments.READ_RECORDING_SPEAKERS, mapOf("recordingId" to text(recordingId))).ReadRecordingSpeakers ?: throw TeaNodeError.UnreadableAnswer

/** Names the speaker of one passage, or of the whole voice in that recording. */
suspend fun Connection.correctRecordingSpeaker(
    recordingId: String,
    expectedSourceGeneration: Long,
    clientCorrectionId: String,
    confirmedName: String,
    isAccountOwner: Boolean?,
    contactAddressBookId: String?,
    contactId: String?,
    recordingTranscriptionAttemptId: String,
    localSpeakerId: String,
    passageStartMS: Long,
    passageEndMS: Long,
): RecordingSpeakersView = execute<SpeakersAnswer>(
    RecordingDocuments.CORRECT_RECORDING_SPEAKER,
    mapOf(
        "recordingId" to text(recordingId), "expectedSourceGeneration" to JsonPrimitive(expectedSourceGeneration),
        "clientCorrectionId" to text(clientCorrectionId), "confirmedName" to text(confirmedName),
        "isAccountOwner" to (isAccountOwner?.let { owner -> JsonPrimitive(owner) } ?: JsonNull),
        "contactAddressBookId" to nullableText(contactAddressBookId), "contactId" to nullableText(contactId),
        "recordingTranscriptionAttemptId" to text(recordingTranscriptionAttemptId), "localSpeakerId" to text(localSpeakerId),
        "passageStartMS" to JsonPrimitive(passageStartMS), "passageEndMS" to JsonPrimitive(passageEndMS),
    ),
).CorrectRecordingSpeaker ?: throw TeaNodeError.UnreadableAnswer

suspend fun Connection.nameRecordingTemporarySpeaker(
    recordingId: String,
    speakerLabel: String,
    confirmedName: String,
    isAccountOwner: Boolean?,
    contactAddressBookId: String?,
    contactId: String?,
) {
    execute<AcknowledgedAnswer>(
        RecordingDocuments.NAME_RECORDING_TEMPORARY_SPEAKER,
        mapOf(
            "recordingId" to text(recordingId), "speakerLabel" to text(speakerLabel), "confirmedName" to text(confirmedName),
            "isAccountOwner" to (isAccountOwner?.let { owner -> JsonPrimitive(owner) } ?: JsonNull),
            "contactAddressBookId" to nullableText(contactAddressBookId), "contactId" to nullableText(contactId),
        ),
    )
}

suspend fun Connection.separateRecordingTemporarySpeaker(recordingId: String, speakerLabel: String) {
    execute<AcknowledgedAnswer>(
        RecordingDocuments.SEPARATE_RECORDING_TEMPORARY_SPEAKER, mapOf("recordingId" to text(recordingId), "speakerLabel" to text(speakerLabel)),
    )
}

suspend fun Connection.renameRecordingSpeaker(recordingId: String, expectedSourceGeneration: Long, speakerIdentityId: String, confirmedName: String): RecordingSpeakersView =
    execute<SpeakersAnswer>(
        RecordingDocuments.RENAME_RECORDING_SPEAKER,
        mapOf(
            "recordingId" to text(recordingId), "expectedSourceGeneration" to JsonPrimitive(expectedSourceGeneration),
            "speakerIdentityId" to text(speakerIdentityId), "confirmedName" to text(confirmedName),
        ),
    ).RenameRecordingSpeaker ?: throw TeaNodeError.UnreadableAnswer

suspend fun Connection.setRecordingSpeakerContact(
    recordingId: String,
    expectedSourceGeneration: Long,
    speakerIdentityId: String,
    addressBookId: String,
    contactId: String,
): RecordingSpeakersView = execute<SpeakersAnswer>(
    RecordingDocuments.SET_RECORDING_SPEAKER_CONTACT,
    mapOf(
        "recordingId" to text(recordingId), "expectedSourceGeneration" to JsonPrimitive(expectedSourceGeneration),
        "speakerIdentityId" to text(speakerIdentityId), "addressBookId" to text(addressBookId), "contactId" to text(contactId),
    ),
).SetRecordingSpeakerContact ?: throw TeaNodeError.UnreadableAnswer

suspend fun Connection.removeRecordingSpeakerCorrection(recordingId: String, expectedSourceGeneration: Long, recordingSpeakerCorrectionId: String): RecordingSpeakersView =
    execute<SpeakersAnswer>(
        RecordingDocuments.REMOVE_RECORDING_SPEAKER_CORRECTION,
        mapOf(
            "recordingId" to text(recordingId), "expectedSourceGeneration" to JsonPrimitive(expectedSourceGeneration),
            "recordingSpeakerCorrectionId" to text(recordingSpeakerCorrectionId),
        ),
    ).RemoveRecordingSpeakerCorrection ?: throw TeaNodeError.UnreadableAnswer

suspend fun Connection.deleteRecordingVoiceReference(recordingId: String, expectedSourceGeneration: Long, recordingVoiceReferenceId: String): RecordingSpeakersView =
    execute<SpeakersAnswer>(
        RecordingDocuments.DELETE_RECORDING_VOICE_REFERENCE,
        mapOf(
            "recordingId" to text(recordingId), "expectedSourceGeneration" to JsonPrimitive(expectedSourceGeneration),
            "recordingVoiceReferenceId" to text(recordingVoiceReferenceId),
        ),
    ).DeleteRecordingVoiceReference ?: throw TeaNodeError.UnreadableAnswer

suspend fun Connection.recordingTextCorrections(recordingId: String): List<RecordingTranscriptTextCorrection> =
    execute<TextCorrectionsAnswer>(
        RecordingDocuments.READ_RECORDING_TRANSCRIPT_TEXT_CORRECTIONS, mapOf("recordingId" to text(recordingId)),
    ).ReadRecordingTranscriptTextCorrections.orEmpty()

suspend fun Connection.correctRecordingTranscriptText(
    recordingId: String,
    recordingTranscriptSegmentId: String,
    expectedSourceGeneration: Long,
    clientCorrectionId: String,
    correctedTranscriptText: String,
): RecordingTranscriptTextCorrection = execute<TextCorrectionsAnswer>(
    RecordingDocuments.CORRECT_RECORDING_TRANSCRIPT_TEXT,
    mapOf(
        "recordingId" to text(recordingId), "recordingTranscriptSegmentId" to text(recordingTranscriptSegmentId),
        "expectedSourceGeneration" to JsonPrimitive(expectedSourceGeneration), "clientCorrectionId" to text(clientCorrectionId),
        "correctedTranscriptText" to text(correctedTranscriptText),
    ),
).CorrectRecordingTranscriptText ?: throw TeaNodeError.UnreadableAnswer

suspend fun Connection.removeRecordingTranscriptTextCorrection(recordingId: String, recordingTranscriptTextCorrectionId: String, expectedSourceGeneration: Long) {
    execute<AcknowledgedAnswer>(
        RecordingDocuments.REMOVE_RECORDING_TRANSCRIPT_TEXT_CORRECTION,
        mapOf(
            "recordingId" to text(recordingId), "recordingTranscriptTextCorrectionId" to text(recordingTranscriptTextCorrectionId),
            "expectedSourceGeneration" to JsonPrimitive(expectedSourceGeneration),
        ),
    )
}
