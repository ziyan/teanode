package com.teanode.app.recordings

import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.graphQLField
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull

private fun JsonObject.text(name: String): String? = (this[name] as? JsonPrimitive)?.contentOrNull

/** One invented recording of the demo library. */
private class DemoRecording(
    val id: String,
    val title: String,
    val summary: String,
    val recordedAt: String,
    val durationMS: Long,
    val runState: String?,
    val isUploading: Boolean = false,
    val origin: String = "capture",
)

private val demoRecordings = listOf(
    DemoRecording("r1", "Weekly planning", "Agreed the launch moves a week.\nAlex takes the pricing page, Sam the announcement.", "2026-11-10T15:00:00Z", 1_412_000, "complete"),
    DemoRecording("r2", "", "", "2026-11-12T09:30:00Z", 312_000, "transcribing"),
    DemoRecording("r3", "Voice note for the garden", "", "2026-11-12T18:10:00Z", 95_000, null, isUploading = true),
    DemoRecording("r4", "Call with the supplier", "", "2026-11-08T11:00:00Z", 602_000, "failed"),
    DemoRecording("r5", "Interview.m4a", "A conversation about onboarding and what first-time users expect.", "2026-10-30T13:00:00Z", 2_710_000, "complete", origin = "imported"),
)

private fun recordingJson(recording: DemoRecording): String {
    val gaps = if (recording.id == "r1") """[{"gapId":"gap1","gapStartMS":600000,"gapEndMS":660000,"recordingGapReason":"paused"}]""" else "[]"
    return """{"id":"${recording.id}","userId":"user-robin","createdAt":"${recording.recordedAt}","updatedAt":"${recording.recordedAt}","clientRecordingId":"client-${recording.id}","recordingTitle":"${recording.title}",
        "recordingTitleSource":"${if (recording.title.isNotEmpty()) "user" else ""}","recordingSummary":"${recording.summary.replace("\n", "\\n")}","recordingOrigin":"${recording.origin}","recordedAt":"${recording.recordedAt}",
        "recordingCaptureState":"stopped","timelineDurationMS":${recording.durationMS},"sourceGeneration":4,"captureGaps":$gaps}""".replace("\n", " ")
}

private fun mediaJson(recording: DemoRecording): String {
    val complete = if (recording.isUploading) "uploading" else "complete"
    val first = """{"id":"m-${recording.id}-1","recordingId":"${recording.id}","clientMediaId":"cm1","originalFilename":"piece-1.m4a","originalContentType":"audio/mp4","originalSizeBytes":${if (recording.isUploading) 20000000 else 4200000},"originalSha256":"${"a".repeat(64)}","timelineStartMS":0,"mediaDurationMS":${if (recording.id == "r1") 600000 else recording.durationMS},"partCount":${if (recording.isUploading) 3 else 1},"recordingUploadState":"$complete","verifiedAt":${if (recording.isUploading) "null" else "\"2026-11-10T15:30:00Z\""}}"""
    val second = if (recording.id == "r1") ""","""+"""{"id":"m-r1-2","recordingId":"r1","clientMediaId":"cm2","originalFilename":"piece-2.m4a","originalContentType":"audio/mp4","originalSizeBytes":5100000,"originalSha256":"${"b".repeat(64)}","timelineStartMS":660000,"mediaDurationMS":752000,"partCount":1,"recordingUploadState":"complete","verifiedAt":"2026-11-10T15:31:00Z"}""" else ""
    return "[$first$second]"
}

private fun runsJson(recording: DemoRecording): String = recording.runState?.let { state ->
    """[{"id":"run-${recording.id}","recordingId":"${recording.id}","recordingMediaId":"m-${recording.id}-1","versionNumber":1,"recordingProcessingState":"$state","providerModelName":"example-transcribe","processingErrorMessage":"${if (state == "failed") "The transcription service did not answer." else ""}","createdAt":"${recording.recordedAt}"}]"""
} ?: "[]"

private fun segmentJson(id: String, start: Long, end: Long, speaker: String, text: String, name: String? = null, owner: Boolean = false) =
    """{"id":"$id","recordingTranscriptRunId":"run-r1","recordingTranscriptionAttemptId":"attempt-1","localSpeakerId":"$speaker","segmentStartMS":$start,"segmentEndMS":$end,"transcriptText":"$text",
        "effectiveSpeakerIdentityId":${if (name != null) "\"identity-$speaker\"" else "null"},"effectiveSpeakerName":${if (name != null) "\"$name\"" else "null"},"hasUserConfirmedSpeaker":${name != null},"isAccountOwner":$owner,"isFiller":false}""".replace("\n", " ")

private val demoSegments = listOf(
    segmentJson("s1", 4_000, 11_000, "A", "Thanks for joining. Let's start with the launch date, because the review turned up two blockers.", "Robin", owner = true),
    segmentJson("s2", 11_500, 19_000, "B", "Right. The pricing page is not ready, and the announcement still needs a second pass from legal."),
    segmentJson("s3", 19_000, 19_400, "A", "Mm-hm.", "Robin", owner = true),
    segmentJson("s4", 19_500, 27_000, "B", "So I would move the launch a week, and use the time to test the checkout with real customers."),
    segmentJson("s5", 28_000, 36_000, "C", "I can take the pricing page this week. Sam, can you own the announcement?", "Alex"),
    segmentJson("s6", 36_500, 43_000, "B", "Yes, I will send a draft on Thursday and we review it together on Friday."),
).joinToString(",", "[", "]")

/**
 * What demo mode answers for Recordings: five invented recordings (one with a transcript of three speakers and a pause,
 * one being transcribed, one still uploading, one that failed, one imported), shares and a recording shared by somebody else.
 */
val recordingsDemoAnswerers: List<DemoAnswerer> = listOf(
    graphQLField("ListRecordings") { variables ->
        val search = variables.text("searchText").orEmpty()
        demoRecordings.filter { recording -> search.isEmpty() || recording.title.contains(search, ignoreCase = true) || recording.summary.contains(search, ignoreCase = true) }.joinToString(",", "[", "]") { recording -> recordingJson(recording) }
    },
    graphQLField("ReadRecordingLimits", """{"maximumMediaBytes":2147483648,"maximumAccountBytes":53687091200,"uploadPartBytes":8388608,"isTranscriptionAvailable":true,"transcriptionUnavailableMessage":"","transcriptionProviderModelName":"example-transcribe"}"""),
    graphQLField("ReadRecordingTranscriptTimeline") { variables ->
        val isTranscribed = variables.text("recordingId") == "r1"
        """{"sourceGeneration":4,"segments":${if (isTranscribed) demoSegments else "[]"},"transcriptRuns":[],"segmentCount":${if (isTranscribed) 6 else 0},"hasMoreSegments":false,"temporarySpeakers":[],"liveUtterances":[]}"""
    },
    graphQLField("ReadRecordingSpeakers", """{"sourceGeneration":4,"speakerIdentities":[{"id":"identity-A","confirmedName":"Robin","isAccountOwner":true},{"id":"identity-C","confirmedName":"Alex"}],"speakerCorrections":[],"voiceReferences":[]}"""),
    graphQLField("SuggestRecordingSpeakerNames", """[{"confirmedName":"Sam Sample","speakerNameSource":"contact","addressBookId":"b1","contactId":"k2"},{"confirmedName":"Alex Example","speakerNameSource":"identity"}]"""),
    graphQLField("ReadRecording") { variables ->
        val recording = demoRecordings.firstOrNull { candidate -> candidate.id == variables.text("recordingId") } ?: demoRecordings.first()
        val parts = if (recording.isUploading) """[{"recordingMediaId":"m-r3-1","partIndex":0,"partSizeBytes":8388608,"partSha256":"${"c".repeat(64)}","storedAt":"2026-11-12T18:12:00Z"}]""" else "[]"
        """{"recording":${recordingJson(recording)},"mediaFiles":${mediaJson(recording)},"uploadParts":$parts,"transcriptRuns":${runsJson(recording)}}"""
    },
    graphQLField("ListRecordingShares", """[{"id":"rs1","recordingId":"r1","granteeUserId":"user-alex","granteeUsername":"alex","granteeName":"Alex Example","recordingShareLevel":"transcript"},{"id":"rs2","recordingId":"r1","granteeCircleId":"c1","granteeCircleName":"Family","recordingShareLevel":"summary"}]"""),
    graphQLField("ListSharedRecordings", """[{"recordingId":"sr1","ownerUserId":"user-alex","ownerUsername":"alex","ownerName":"Alex Example","recordingTitle":"Kickoff call","createdAt":"2026-11-05T10:00:00Z","recordedAt":"2026-11-05T10:00:00Z","timelineDurationMS":1800000,"recordingSummary":"The team agreed the scope and who owns what.","recordingShareLevel":"transcript"},
        {"recordingId":"sr2","ownerUserId":"user-sam","ownerUsername":"sam","ownerName":"Sam Sample","recordingTitle":"Budget review","createdAt":"2026-10-28T10:00:00Z","timelineDurationMS":900000,"recordingSummary":"","recordingShareLevel":"summary"}]""".replace("\n", " ")),
    graphQLField("ReadSharedRecording") { variables ->
        val isTranscript = variables.text("recordingId") == "sr1"
        """{"recordingId":"${variables.text("recordingId")}","ownerUserId":"user-alex","ownerUsername":"alex","ownerName":"Alex Example","recordingTitle":"${if (isTranscript) "Kickoff call" else "Budget review"}","createdAt":"2026-11-05T10:00:00Z","timelineDurationMS":1800000,
        "recordingSummary":"The team agreed the scope and who owns what.","recordingShareLevel":"${if (isTranscript) "transcript" else "summary"}","speakerNames":${if (isTranscript) "[\"Alex\",\"Sam\"]" else "null"},
        "passages":${if (isTranscript) """[{"segmentStartMS":0,"segmentEndMS":5000,"speakerName":"Alex","transcriptText":"Welcome, everyone.","isFiller":false},{"segmentStartMS":5000,"segmentEndMS":9000,"speakerName":"Alex","transcriptText":"Let's go through the scope first.","isFiller":false},{"segmentStartMS":9000,"segmentEndMS":14000,"speakerName":"Sam","transcriptText":"I have three items to add.","isFiller":false}]""" else "null"},"passageCount":3,"hasMorePassages":false}""".replace("\n", " ")
    },
)
