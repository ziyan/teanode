package com.teanode.kit.recordings

/** The GraphQL documents of Recordings. Copied from the iPhone app's Documents (ios/TeaNodeKit), text unchanged. */
object RecordingDocuments {
    val RECORDING_FIELDS = """
id userId createdAt updatedAt clientRecordingId recordingTitle recordingTitleSource recordingSummary recordingOrigin recordedAt
recordingCaptureState timelineDurationMS sourceGeneration captureGaps { gapId gapStartMS gapEndMS recordingGapReason }
""".trimEnd()

    val RECORDING_MEDIA_FIELDS = """
id recordingId clientMediaId originalFilename originalContentType originalSizeBytes originalSha256 timelineStartMS
mediaDurationMS partCount recordingUploadState verifiedAt
""".trimEnd()

    val LIST_RECORDINGS = """
query (${'$'}beforeRecordingId: String!, ${'$'}pageSize: Int!, ${'$'}searchText: String) { ListRecordings(beforeRecordingId: ${'$'}beforeRecordingId,
pageSize: ${'$'}pageSize, searchText: ${'$'}searchText) { ${RECORDING_FIELDS} } }
""".trimEnd()

    val RECORDING_TRANSCRIPT_RUN_FIELDS = """
id recordingId recordingMediaId versionNumber recordingProcessingState providerModelName processingErrorMessage createdAt
""".trimEnd()

    val READ_RECORDING = """
query (${'$'}recordingId: String!) { ReadRecording(recordingId: ${'$'}recordingId) { recording { ${RECORDING_FIELDS} } mediaFiles {
${RECORDING_MEDIA_FIELDS} } uploadParts { recordingMediaId partIndex partSizeBytes partSha256 storedAt } transcriptRuns {
${RECORDING_TRANSCRIPT_RUN_FIELDS} } } }
""".trimEnd()

    val RECORDING_LIMITS = """
query { ReadRecordingLimits { maximumMediaBytes maximumAccountBytes uploadPartBytes isTranscriptionAvailable transcriptionUnavailableMessage transcriptionProviderModelName } }
""".trimEnd()

    val CREATE_RECORDING = """
mutation (${'$'}clientRecordingId: String!, ${'$'}recordingTitle: String!, ${'$'}recordingOrigin: String!) { CreateRecording(clientRecordingId:
${'$'}clientRecordingId, recordingTitle: ${'$'}recordingTitle, recordingOrigin: ${'$'}recordingOrigin) { ${RECORDING_FIELDS} } }
""".trimEnd()

    val CREATE_RECORDING_MEDIA = """
mutation (${'$'}recordingId: String!, ${'$'}clientMediaId: String!, ${'$'}originalFilename: String!, ${'$'}originalContentType: String!, ${'$'}originalSizeBytes: Int64!, ${'$'}originalSha256: String!, ${'$'}timelineStartMS: Int64!, ${'$'}mediaDurationMS: Int64!) {
  CreateRecordingMedia(recordingId: ${'$'}recordingId, clientMediaId: ${'$'}clientMediaId, originalFilename: ${'$'}originalFilename, originalContentType: ${'$'}originalContentType,
    originalSizeBytes: ${'$'}originalSizeBytes, originalSha256: ${'$'}originalSha256, timelineStartMS: ${'$'}timelineStartMS, mediaDurationMS: ${'$'}mediaDurationMS) { ${RECORDING_MEDIA_FIELDS} }
}
""".trimEnd()

    val UPDATE_RECORDING_CAPTURE = """
mutation (${'$'}recordingId: String!, ${'$'}expectedGeneration: Int64!, ${'$'}recordingCaptureState: String!, ${'$'}timelineDurationMS: Int64!, ${'$'}captureGaps: [RecordingGapInput!]!) {
  UpdateRecordingCapture(recordingId: ${'$'}recordingId, expectedGeneration: ${'$'}expectedGeneration, recordingCaptureState: ${'$'}recordingCaptureState,
    timelineDurationMS: ${'$'}timelineDurationMS, captureGaps: ${'$'}captureGaps) { ${RECORDING_FIELDS} }
}
""".trimEnd()

    val UPDATE_RECORDING_METADATA = """
mutation (${'$'}recordingId: String!, ${'$'}expectedSourceGeneration: Int64!, ${'$'}recordingTitle: String, ${'$'}recordedAt: DateTime) {
UpdateRecordingMetadata(recordingId: ${'$'}recordingId, expectedSourceGeneration: ${'$'}expectedSourceGeneration, recordingTitle:
${'$'}recordingTitle, recordedAt: ${'$'}recordedAt) { ${RECORDING_FIELDS} } }
""".trimEnd()

    val DELETE_RECORDING = """
mutation (${'$'}recordingId: String!) { DeleteRecording(recordingId: ${'$'}recordingId) }
""".trimEnd()

    val RECORDING_SPEAKERS_FIELDS = """
sourceGeneration speakerIdentities { id confirmedName isAccountOwner contactAddressBookId contactId contactName } speakerCorrections { id recordingMediaId recordingTranscriptionAttemptId
speakerIdentityId clientCorrectionId localSpeakerId passageStartMS passageEndMS sourceGeneration isRemoved } voiceReferences {
id speakerIdentityId recordingMediaId recordingTranscriptRunId passageStartMS passageEndMS sourceGeneration isRevoked }
""".trimEnd()

    val READ_RECORDING_TRANSCRIPT = """
query (${'$'}recordingTranscriptRunId: String!, ${'$'}segmentOffset: Int!, ${'$'}pageSize: Int!) {
ReadRecordingTranscript(recordingTranscriptRunId: ${'$'}recordingTranscriptRunId, segmentOffset: ${'$'}segmentOffset, pageSize: ${'$'}pageSize)
{ transcriptRun { ${RECORDING_TRANSCRIPT_RUN_FIELDS} } segments { id recordingTranscriptRunId recordingTranscriptionAttemptId
localSpeakerId segmentStartMS segmentEndMS transcriptText effectiveTranscriptText appliedTextCorrectionId
hasUnalignedTextCorrection effectiveSpeakerIdentityId effectiveSpeakerName hasUserConfirmedSpeaker isRecognizedSpeaker isAccountOwner hasUnalignedSpeakerCorrection
appliedSpeakerCorrectionId } attempts { id recordingAttemptState providerModelName hasProviderUsage estimatedCostAmount
actualCostAmount currencyCode attemptErrorMessage } segmentCount hasMoreSegments } }
""".trimEnd()

    val REQUEST_RECORDING_TRANSCRIPTION = """
mutation (${'$'}recordingMediaId: String!, ${'$'}clientRunId: String!, ${'$'}isReprocessingRequested: Boolean!) {
RequestRecordingTranscription(recordingMediaId: ${'$'}recordingMediaId, clientRunId: ${'$'}clientRunId, isReprocessingRequested:
${'$'}isReprocessingRequested) { ${RECORDING_TRANSCRIPT_RUN_FIELDS} } }
""".trimEnd()

    val RETRY_RECORDING_TRANSCRIPTION = """
mutation (${'$'}recordingTranscriptRunId: String!) { RetryRecordingTranscription(recordingTranscriptRunId: ${'$'}recordingTranscriptRunId)
{ ${RECORDING_TRANSCRIPT_RUN_FIELDS} } }
""".trimEnd()

    val CANCEL_RECORDING_TRANSCRIPTION = """
mutation (${'$'}recordingTranscriptRunId: String!) { CancelRecordingTranscription(recordingTranscriptRunId:
${'$'}recordingTranscriptRunId) { ${RECORDING_TRANSCRIPT_RUN_FIELDS} } }
""".trimEnd()

    val SUGGEST_RECORDING_SPEAKER_NAMES = """
query (${'$'}searchText: String, ${'$'}pageSize: Int) { SuggestRecordingSpeakerNames(searchText: ${'$'}searchText, pageSize: ${'$'}pageSize) {
confirmedName speakerNameSource addressBookId contactId } }
""".trimEnd()

    val READ_RECORDING_SPEAKERS = """
query (${'$'}recordingId: String!) { ReadRecordingSpeakers(recordingId: ${'$'}recordingId) { ${RECORDING_SPEAKERS_FIELDS} } }
""".trimEnd()

    val CORRECT_RECORDING_SPEAKER = """
mutation (${'$'}recordingId: String!, ${'$'}expectedSourceGeneration: Int64!, ${'$'}clientCorrectionId: String!, ${'$'}confirmedName: String!,
${'$'}isAccountOwner: Boolean, ${'$'}contactAddressBookId: String, ${'$'}contactId: String, ${'$'}recordingTranscriptionAttemptId: String!, ${'$'}localSpeakerId: String!,
${'$'}passageStartMS: Int64!, ${'$'}passageEndMS: Int64!) {
CorrectRecordingSpeaker(recordingId: ${'$'}recordingId, expectedSourceGeneration: ${'$'}expectedSourceGeneration, clientCorrectionId:
${'$'}clientCorrectionId, confirmedName: ${'$'}confirmedName, isAccountOwner: ${'$'}isAccountOwner, contactAddressBookId: ${'$'}contactAddressBookId,
contactId: ${'$'}contactId, recordingTranscriptionAttemptId: ${'$'}recordingTranscriptionAttemptId,
localSpeakerId: ${'$'}localSpeakerId, passageStartMS: ${'$'}passageStartMS, passageEndMS: ${'$'}passageEndMS) { ${RECORDING_SPEAKERS_FIELDS} } }
""".trimEnd()

    val NAME_RECORDING_TEMPORARY_SPEAKER = """
mutation (${'$'}recordingId: String!, ${'$'}speakerLabel: String!, ${'$'}confirmedName: String!, ${'$'}isAccountOwner: Boolean,
${'$'}contactAddressBookId: String, ${'$'}contactId: String) {
NameRecordingTemporarySpeaker(recordingId: ${'$'}recordingId, speakerLabel: ${'$'}speakerLabel, confirmedName: ${'$'}confirmedName, isAccountOwner:
${'$'}isAccountOwner, contactAddressBookId: ${'$'}contactAddressBookId, contactId: ${'$'}contactId) }
""".trimEnd()

    val SEPARATE_RECORDING_TEMPORARY_SPEAKER = """
mutation (${'$'}recordingId: String!, ${'$'}speakerLabel: String!) {
SeparateRecordingTemporarySpeaker(recordingId: ${'$'}recordingId, speakerLabel: ${'$'}speakerLabel) }
""".trimEnd()

    val RENAME_RECORDING_SPEAKER = """
mutation (${'$'}recordingId: String!, ${'$'}expectedSourceGeneration: Int64!, ${'$'}speakerIdentityId: String!, ${'$'}confirmedName: String!) {
RenameRecordingSpeaker(recordingId: ${'$'}recordingId, expectedSourceGeneration: ${'$'}expectedSourceGeneration, speakerIdentityId:
${'$'}speakerIdentityId, confirmedName: ${'$'}confirmedName) { ${RECORDING_SPEAKERS_FIELDS} } }
""".trimEnd()

    val SET_RECORDING_SPEAKER_CONTACT = """
mutation (${'$'}recordingId: String!, ${'$'}expectedSourceGeneration: Int64!, ${'$'}speakerIdentityId: String!, ${'$'}addressBookId: String!,
${'$'}contactId: String!) {
SetRecordingSpeakerContact(recordingId: ${'$'}recordingId, expectedSourceGeneration: ${'$'}expectedSourceGeneration, speakerIdentityId:
${'$'}speakerIdentityId, addressBookId: ${'$'}addressBookId, contactId: ${'$'}contactId) { ${RECORDING_SPEAKERS_FIELDS} } }
""".trimEnd()

    val REMOVE_RECORDING_SPEAKER_CORRECTION = """
mutation (${'$'}recordingId: String!, ${'$'}expectedSourceGeneration: Int64!, ${'$'}recordingSpeakerCorrectionId: String!) {
RemoveRecordingSpeakerCorrection(recordingId: ${'$'}recordingId, expectedSourceGeneration: ${'$'}expectedSourceGeneration,
recordingSpeakerCorrectionId: ${'$'}recordingSpeakerCorrectionId) { ${RECORDING_SPEAKERS_FIELDS} } }
""".trimEnd()

    val UNDO_RECORDING_SPEAKER_NAMING = """
mutation (${'$'}recordingId: String!, ${'$'}expectedSourceGeneration: Int64!, ${'$'}recordingSpeakerCorrectionId: String!) {
UndoRecordingSpeakerNaming(recordingId: ${'$'}recordingId, expectedSourceGeneration: ${'$'}expectedSourceGeneration,
recordingSpeakerCorrectionId: ${'$'}recordingSpeakerCorrectionId) { ${RECORDING_SPEAKERS_FIELDS} } }
""".trimEnd()

    val DELETE_RECORDING_VOICE_REFERENCE = """
mutation (${'$'}recordingId: String!, ${'$'}expectedSourceGeneration: Int64!, ${'$'}recordingVoiceReferenceId: String!) {
DeleteRecordingVoiceReference(recordingId: ${'$'}recordingId, expectedSourceGeneration: ${'$'}expectedSourceGeneration,
recordingVoiceReferenceId: ${'$'}recordingVoiceReferenceId) { ${RECORDING_SPEAKERS_FIELDS} } }
""".trimEnd()

    val RECORDING_SHARE_FIELDS = """
id recordingId granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName recordingShareLevel
""".trimEnd()

    val SHARED_RECORDING_LISTING_FIELDS = """
recordingId ownerUserId ownerUsername ownerName recordingTitle createdAt recordedAt timelineDurationMS recordingSummary
recordingShareLevel
""".trimEnd()

    val RECORDING_SHARES = """
query (${'$'}recordingId: String!) { ListRecordingShares(recordingId: ${'$'}recordingId) { ${RECORDING_SHARE_FIELDS} } }
""".trimEnd()

    val SHARE_RECORDING = """
mutation (${'$'}recordingId: String!, ${'$'}granteeUserId: String, ${'$'}granteeCircleId: String, ${'$'}recordingShareLevel: String!) {
  ShareRecording(recordingId: ${'$'}recordingId, granteeUserId: ${'$'}granteeUserId, granteeCircleId: ${'$'}granteeCircleId,
    recordingShareLevel: ${'$'}recordingShareLevel) { id }
}
""".trimEnd()

    val UNSHARE_RECORDING = """
mutation (${'$'}recordingShareId: String!) { UnshareRecording(recordingShareId: ${'$'}recordingShareId) }
""".trimEnd()

    val SHARED_RECORDINGS = """
query (${'$'}beforeRecordingId: String, ${'$'}pageSize: Int!) {
  ListSharedRecordings(beforeRecordingId: ${'$'}beforeRecordingId, pageSize: ${'$'}pageSize) { ${SHARED_RECORDING_LISTING_FIELDS} }
}
""".trimEnd()

    val SHARED_RECORDING = """
query (${'$'}recordingId: String!, ${'$'}passageOffset: Int!, ${'$'}pageSize: Int!) {
  ReadSharedRecording(recordingId: ${'$'}recordingId, passageOffset: ${'$'}passageOffset, pageSize: ${'$'}pageSize) {
    ${SHARED_RECORDING_LISTING_FIELDS} speakerNames passages { segmentStartMS segmentEndMS speakerName transcriptText isFiller }
    passageCount hasMorePassages
  }
}
""".trimEnd()

    val KNOWLEDGE_SOURCE_FIELDS = """
id kind name specification { type settings computer path format mailboxId readEveryCheckout }
rootPath enabled cron lastRunAt lastError documentCount chunkCount refusedCount more
checkoutsKeptToProfile filesKeptToProfile
""".trimEnd()

    val RESUME_RECORDING_LEARNING = """
mutation (${'$'}recordingId: String!, ${'$'}expectedSourceGeneration: Int64!) {
    ResumeRecordingLearning(recordingId: ${'$'}recordingId, expectedSourceGeneration: ${'$'}expectedSourceGeneration) {
        sourceGeneration hasAgent hasStoppedReading knowledgeSource { ${KNOWLEDGE_SOURCE_FIELDS} nextRunAt }
        pendingDocumentCount digestedDocumentCount lastPublishedSourceGeneration isCurationPending
    }
}
""".trimEnd()

    val READ_RECORDING_LEARNING = """
query (${'$'}recordingId: String!) { ReadRecordingLearning(recordingId: ${'$'}recordingId) { sourceGeneration hasAgent knowledgeSource {
${KNOWLEDGE_SOURCE_FIELDS} nextRunAt } hasStoppedReading pendingDocumentCount digestedDocumentCount
lastPublishedSourceGeneration isCurationPending } }
""".trimEnd()

    val IMPORT_RECORDING_ATTACHMENT = """
mutation (${'$'}agentAttachmentId: String!, ${'$'}clientRecordingId: String!, ${'$'}recordingTitle: String!, ${'$'}recordingMediaId: String) {
ImportRecordingAttachment(agentAttachmentId: ${'$'}agentAttachmentId, clientRecordingId: ${'$'}clientRecordingId, recordingTitle:
${'$'}recordingTitle, recordingMediaId: ${'$'}recordingMediaId) { recording { ${RECORDING_FIELDS} } mediaFiles { ${RECORDING_MEDIA_FIELDS} }
uploadParts { recordingMediaId partIndex partSizeBytes partSha256 storedAt } transcriptRuns { id recordingId recordingMediaId
versionNumber recordingProcessingState providerModelName processingErrorMessage createdAt } } }
""".trimEnd()

    val READ_RECORDING_REFERENCE_INVALIDATIONS = """
query (${'$'}recordingId: String!) { ReadRecordingReferenceInvalidations(recordingId: ${'$'}recordingId) { id recordingId
recordingTranscriptRunId recordingTranscriptionAttemptId referenceBlobId referenceRecordingId referenceTranscriptRunId
speakerIdentityId invalidatedConfirmedName referencePassageStartMS referencePassageEndMS referenceSourceGeneration
invalidationMessage createdAt isReconciled } }
""".trimEnd()

    val READ_RECORDING_TRANSCRIPT_TIMELINE = """
query (${'$'}recordingId: String!, ${'$'}segmentOffset: Int!, ${'$'}pageSize: Int!, ${'$'}isUnnamedOnly: Boolean) {
ReadRecordingTranscriptTimeline(recordingId: ${'$'}recordingId, segmentOffset: ${'$'}segmentOffset, pageSize: ${'$'}pageSize,
isUnnamedOnly: ${'$'}isUnnamedOnly) { sourceGeneration segments { id recordingTranscriptRunId
recordingTranscriptionAttemptId localSpeakerId segmentStartMS segmentEndMS transcriptText effectiveTranscriptText
appliedTextCorrectionId hasUnalignedTextCorrection effectiveSpeakerIdentityId effectiveSpeakerName hasUserConfirmedSpeaker isRecognizedSpeaker
isAccountOwner hasUnalignedSpeakerCorrection appliedSpeakerCorrectionId temporarySpeakerLabel mergedTemporarySpeakerLabel isFiller } transcriptRuns { id recordingId recordingMediaId versionNumber
recordingProcessingState providerModelName processingErrorMessage createdAt } temporarySpeakers { speakerLabel speakerIdentityId mergedIntoSpeakerLabel } segmentCount
hasMoreSegments liveUtterances { utteranceId utteranceStartMS utteranceEndMS transcriptText } } }
""".trimEnd()

    val READ_RECORDING_TRANSCRIPT_PASSAGE_OFFSET = """
query (${'$'}recordingTranscriptRunId: String!, ${'$'}passageStartMS: Int64!, ${'$'}passageEndMS: Int64!) {
    ReadRecordingTranscriptPassageOffset(recordingTranscriptRunId: ${'$'}recordingTranscriptRunId,
        passageStartMS: ${'$'}passageStartMS, passageEndMS: ${'$'}passageEndMS)
}
""".trimEnd()

    val RECORDING_PLAYBACK_FIELDS = """
recordingMediaId playbackPath playbackContentType playbackSizeBytes playbackSha256 playbackDurationMS timelineStartMS
""".trimEnd()

    val READ_RECORDING_PLAYBACK = """
query (${'$'}recordingMediaId: String!) {
  ReadRecordingPlayback(recordingMediaId: ${'$'}recordingMediaId) { ${RECORDING_PLAYBACK_FIELDS} }
}
""".trimEnd()

    val PREPARE_RECORDING_PLAYBACK = """
mutation (${'$'}recordingMediaId: String!) {
  PrepareRecordingPlayback(recordingMediaId: ${'$'}recordingMediaId) { ${RECORDING_PLAYBACK_FIELDS} }
}
""".trimEnd()

    val RECORDING_TEXT_CORRECTION_FIELDS = """
id recordingId recordingMediaId recordingTranscriptRunId recordingTranscriptSegmentId clientCorrectionId passageStartMS
passageEndMS originalTranscriptText correctedTranscriptText sourceGeneration createdAt isRemoved
""".trimEnd()

    val READ_RECORDING_TRANSCRIPT_TEXT_CORRECTIONS = """
query (${'$'}recordingId: String!) { ReadRecordingTranscriptTextCorrections(recordingId: ${'$'}recordingId) {
${RECORDING_TEXT_CORRECTION_FIELDS} } }
""".trimEnd()

    val CORRECT_RECORDING_TRANSCRIPT_TEXT = """
mutation (${'$'}recordingId: String!, ${'$'}recordingTranscriptSegmentId: String!, ${'$'}expectedSourceGeneration: Int64!, ${'$'}clientCorrectionId:
String!, ${'$'}correctedTranscriptText: String!) { CorrectRecordingTranscriptText(recordingId: ${'$'}recordingId,
recordingTranscriptSegmentId: ${'$'}recordingTranscriptSegmentId, expectedSourceGeneration: ${'$'}expectedSourceGeneration,
clientCorrectionId: ${'$'}clientCorrectionId, correctedTranscriptText: ${'$'}correctedTranscriptText) { ${RECORDING_TEXT_CORRECTION_FIELDS} }
}
""".trimEnd()

    val REMOVE_RECORDING_TRANSCRIPT_TEXT_CORRECTION = """
mutation (${'$'}recordingId: String!, ${'$'}recordingTranscriptTextCorrectionId: String!, ${'$'}expectedSourceGeneration: Int64!) {
RemoveRecordingTranscriptTextCorrection(recordingId: ${'$'}recordingId, recordingTranscriptTextCorrectionId:
${'$'}recordingTranscriptTextCorrectionId, expectedSourceGeneration: ${'$'}expectedSourceGeneration) }
""".trimEnd()

}
