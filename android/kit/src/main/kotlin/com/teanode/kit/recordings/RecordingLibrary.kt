package com.teanode.kit.recordings

import com.teanode.kit.TeaNodeError
import com.teanode.kit.photos.ServerTime
import kotlinx.serialization.Serializable
import java.io.IOException
import java.net.ConnectException
import java.net.SocketTimeoutException
import java.net.UnknownHostException
import javax.net.ssl.SSLException

/**
 * LocalRecording is a recording as the phone holds it before the server has all of it: the capture, the files, and how
 * much of each went up. It is kept on the phone in the recording journal, so a recording survives a crash.
 */
@Serializable
data class LocalRecording(
    val id: String,
    val accountKey: String,
    val recordingUserId: String? = null,
    var clientRecordingId: String = id,
    var recordingId: String? = null,
    var recordingTitle: String,
    var recordingOrigin: String,
    val startedAtEpochMS: Long,
    var recordingCaptureState: String = if (recordingOrigin == "capture") "active" else "imported",
    var timelineDurationMS: Long = 0,
    var captureGaps: List<RecordingGap> = emptyList(),
    var localMedia: List<LocalRecordingMedia> = emptyList(),
    var uploadErrorMessage: String? = null,
    var isDeletedRemotely: Boolean = false,
    var hasPublishedCapture: Boolean? = null,
    var isUploadWaitingForNetwork: Boolean? = null,
) {
    /** Pauses the capture at this point of its timeline: by the person ("paused") or by a call ("interrupted"). */
    fun paused(timelineMS: Long, gapReason: String = "paused"): LocalRecording {
        if (recordingOrigin != "capture" || recordingCaptureState != "active" || timelineMS < timelineDurationMS || gapReason !in listOf("paused", "interrupted")) {
            throw TeaNodeError.Refused("This recording is not active.")
        }
        return copy(
            timelineDurationMS = timelineMS, recordingCaptureState = if (gapReason == "paused") "paused" else "interrupted",
            captureGaps = captureGaps + RecordingGap(gapId = newGapId(), gapStartMS = timelineMS, recordingGapReason = gapReason),
        )
    }

    /** Ends the open gap at this point and carries on. */
    fun resumed(timelineMS: Long): LocalRecording {
        val openGap = captureGaps.lastOrNull()
        if (recordingCaptureState !in listOf("paused", "interrupted") || timelineMS < timelineDurationMS || openGap == null || openGap.gapEndMS != null) {
            throw TeaNodeError.Refused("This recording cannot be resumed.")
        }
        return copy(
            timelineDurationMS = timelineMS, recordingCaptureState = "active",
            captureGaps = captureGaps.dropLast(1) + openGap.copy(gapEndMS = timelineMS),
        )
    }

    fun stopped(timelineMS: Long): LocalRecording {
        if (recordingOrigin != "capture" || recordingCaptureState == "stopped" || timelineMS < timelineDurationMS) {
            throw TeaNodeError.Refused("This recording cannot be stopped.")
        }
        val openGap = captureGaps.lastOrNull()
        val closedGaps = if (openGap != null && openGap.gapEndMS == null) captureGaps.dropLast(1) + openGap.copy(gapEndMS = timelineMS) else captureGaps
        return copy(timelineDurationMS = timelineMS, recordingCaptureState = "stopped", captureGaps = closedGaps)
    }

    val isCaptureUnderWay: Boolean get() = recordingOrigin == "capture" && recordingCaptureState in listOf("active", "paused", "interrupted")

    /** The gaps as the server is told them: an open gap ends where the timeline does. */
    val publishedCaptureGaps: List<RecordingGap>
        get() = captureGaps.map { gap -> if (gap.gapEndMS == null) gap.copy(gapEndMS = maxOf(timelineDurationMS, gap.gapStartMS)) else gap }

    fun belongsTo(accountKey: String, recordingUserId: String?): Boolean =
        !recordingUserId.isNullOrEmpty() && this.accountKey == accountKey && this.recordingUserId == recordingUserId

    private fun newGapId(): String = java.util.UUID.randomUUID().toString()
}

/** One audio file of a LocalRecording, with the manifest the server accepted for it and the parts it holds. */
@Serializable
data class LocalRecordingMedia(
    val id: String,
    var mediaManifest: RecordingMedia? = null,
    val originalFilename: String,
    val originalContentType: String,
    var timelineStartMS: Long,
    var mediaDurationMS: Long = 0,
    var originalSizeBytes: Long = 0,
    var originalSha256: String? = null,
    var acceptedParts: List<RecordingMediaPart> = emptyList(),
    var completionReceipt: RecordingMedia? = null,
) {
    /** Whether the server has the whole original, verified: only then may the phone let go of its copy. */
    val isDurablyUploaded: Boolean
        get() {
            val manifest = mediaManifest ?: return false
            val receipt = completionReceipt ?: return false
            return receipt.confirmsOriginal(manifest) && manifest.originalSizeBytes == originalSizeBytes && manifest.originalSha256 == originalSha256
        }
}

/** What a row of the recordings list says about the recording. */
sealed class RecordingRowStatus {
    data object Recording : RecordingRowStatus()

    data object Paused : RecordingRowStatus()

    data class Uploading(val percent: Int) : RecordingRowStatus()

    data object WaitingForNetwork : RecordingRowStatus()

    data object Transcribing : RecordingRowStatus()

    data object Ready : RecordingRowStatus()

    data object Failed : RecordingRowStatus()
}

/** What the server says about the upload and the transcript of a recording, which a recording the phone does not hold shows. */
data class RecordingServerProgress(val isUploadComplete: Boolean, val uploadedPercent: Int, val transcriptProcessingStates: List<String>) {
    val isSettled: Boolean
        get() = isUploadComplete && transcriptProcessingStates.isNotEmpty() && transcriptProcessingStates.none { state -> state in RecordingTranscriptRun.RUNNING_STATES }

    companion object {
        fun of(view: RecordingView): RecordingServerProgress {
            val totalPartCount = view.mediaFiles.sumOf { media -> media.partCount }
            val isComplete = view.mediaFiles.isNotEmpty() && view.mediaFiles.all { media -> media.recordingUploadState == "complete" }
            val percent = if (isComplete || totalPartCount == 0) (if (isComplete) 100 else 0) else minOf(99, view.uploadParts.size * 100 / totalPartCount)
            return RecordingServerProgress(isComplete, percent, view.transcriptRuns.orEmpty().map { run -> run.recordingProcessingState })
        }
    }
}

/** One row of the recordings list: a recording on the server, one only on the phone, or both. */
data class RecordingLibraryRow(
    val id: String,
    val recording: Recording?,
    val localRecording: LocalRecording?,
    val recordingTitle: String,
    val recordingSummaryFirstLine: String,
    val recordedAtEpochMS: Long,
    val timelineDurationMS: Long,
    val status: RecordingRowStatus,
) {
    /** Whether a capture is under way: the row opens the recorder, not a detail. */
    val isLiveSession: Boolean get() = localRecording?.recordingCaptureState in listOf("active", "paused", "interrupted")

    /** The phone's copy can be let go of once the server has the original. */
    val canRemoveFromPhone: Boolean get() = localRecording?.recordingCaptureState in listOf("stopped", "imported")

    val canRetryUpload: Boolean get() = localRecording?.let { local -> local.id in RecordingLibraryProgress.pendingLocalRecordingIds(listOf(local)) } ?: false

    val canPlayLocalOriginal: Boolean get() = localRecording?.let { local -> RecordingLibraryProgress.canPlayLocalOriginal(local) } ?: false

    val canDelete: Boolean get() = recording != null

    /** Whether the row is worth reading again soon: it is uploading, transcribing, or waiting to be named. */
    fun isStillChanging(nowEpochMS: Long): Boolean = when (status) {
        is RecordingRowStatus.Uploading, RecordingRowStatus.Transcribing -> true
        RecordingRowStatus.Ready -> {
            val serverRecording = recording
            if (serverRecording == null || serverRecording.recordingTitleSource == "user") {
                false
            } else {
                val isUnnamed = recordingTitle.isEmpty() && recordingSummaryFirstLine.isEmpty()
                val updatedAt = ServerTime.instant(serverRecording.updatedAt)?.toEpochMilli() ?: Long.MIN_VALUE / 2
                isUnnamed && kotlin.math.abs(nowEpochMS - updatedAt) < 600_000
            }
        }
        else -> false
    }
}

object RecordingLibrary {
    /** The list: the server's recordings with the phone's copies joined by client id, then the ones only the phone holds. Newest first. */
    fun rows(recordings: List<Recording>, localRecordings: List<LocalRecording>, serverProgress: Map<String, RecordingServerProgress> = emptyMap()): List<RecordingLibraryRow> {
        val localByClientId = localRecordings.associateBy { local -> local.clientRecordingId }
        val rows = recordings.map { recording -> row(recording, localByClientId[recording.clientRecordingId], serverProgress[recording.id]) }.toMutableList()
        val serverClientIds = recordings.map { recording -> recording.clientRecordingId }.toSet()
        for (local in localRecordings) {
            if (local.clientRecordingId in serverClientIds || local.isDeletedRemotely) continue
            rows.add(row(null, local, null))
        }
        return rows.sortedWith(compareByDescending<RecordingLibraryRow> { row -> row.recordedAtEpochMS }.thenByDescending { row -> row.id })
    }

    /** The first line of a summary that has words in it. */
    fun firstLine(summary: String?): String = summary.orEmpty().lines().map { line -> line.trim() }.firstOrNull { line -> line.isNotEmpty() }.orEmpty()

    fun uploadedPercent(localRecording: LocalRecording): Int {
        val totalBytes = localRecording.localMedia.sumOf { media -> maxOf(0L, media.originalSizeBytes) }
        if (totalBytes <= 0) return 0
        val uploadedBytes = localRecording.localMedia.sumOf { media ->
            if (media.isDurablyUploaded) media.originalSizeBytes else media.acceptedParts.sumOf { part -> part.partSizeBytes }
        }
        return minOf(99L, maxOf(0L, uploadedBytes * 100 / totalBytes)).toInt()
    }

    fun status(recording: Recording?, localRecording: LocalRecording?, progress: RecordingServerProgress?): RecordingRowStatus {
        if (localRecording != null) {
            when (localRecording.recordingCaptureState) {
                "active" -> return RecordingRowStatus.Recording
                "paused", "interrupted" -> return RecordingRowStatus.Paused
            }
            if (localRecording.isDeletedRemotely) return RecordingRowStatus.Failed
            if (!isFullyUploaded(localRecording)) {
                if (localRecording.uploadErrorMessage != null) {
                    return if (localRecording.isUploadWaitingForNetwork == true) RecordingRowStatus.WaitingForNetwork else RecordingRowStatus.Failed
                }
                return RecordingRowStatus.Uploading(uploadedPercent(localRecording))
            }
        }
        if (recording == null) return RecordingRowStatus.Ready
        // A summary is written only from a finished transcript: whatever an earlier version did, the recording is ready.
        if (firstLine(recording.recordingSummary).isNotEmpty()) return RecordingRowStatus.Ready
        if (progress == null) return RecordingRowStatus.Transcribing
        if (!progress.isUploadComplete) return RecordingRowStatus.Uploading(progress.uploadedPercent)
        val states = progress.transcriptProcessingStates
        if (states.isEmpty() || states.any { state -> state in RecordingTranscriptRun.RUNNING_STATES }) return RecordingRowStatus.Transcribing
        return if ("complete" in states) RecordingRowStatus.Ready else RecordingRowStatus.Failed
    }

    private fun isFullyUploaded(localRecording: LocalRecording): Boolean =
        localRecording.recordingCaptureState in listOf("stopped", "imported") && localRecording.localMedia.isNotEmpty() &&
            localRecording.localMedia.all { media -> media.isDurablyUploaded }

    private fun row(recording: Recording?, localRecording: LocalRecording?, progress: RecordingServerProgress?): RecordingLibraryRow {
        val serverTitle = recording?.recordingTitle.orEmpty()
        val recordedAtEpochMS = ServerTime.instant(recording?.recordedAt)?.toEpochMilli() ?: localRecording?.startedAtEpochMS
            ?: ServerTime.instant(recording?.createdAt)?.toEpochMilli() ?: 0L
        return RecordingLibraryRow(
            id = recording?.clientRecordingId ?: localRecording?.clientRecordingId.orEmpty(), recording = recording, localRecording = localRecording,
            recordingTitle = serverTitle.ifEmpty { localRecording?.recordingTitle.orEmpty() }, recordingSummaryFirstLine = firstLine(recording?.recordingSummary),
            recordedAtEpochMS = recordedAtEpochMS, timelineDurationMS = maxOf(localRecording?.timelineDurationMS ?: 0, recording?.timelineDurationMS ?: 0),
            status = status(recording, localRecording, progress),
        )
    }

    /** Whether a failure is the network being gone, which the upload waits out instead of failing. */
    fun isConnectivityFailure(error: Throwable): Boolean =
        error is UnknownHostException || error is ConnectException || error is SocketTimeoutException || error is SSLException ||
            (error is IOException && error !is java.io.FileNotFoundException)
}

object RecordingLibraryProgress {
    fun isCurrentOwner(
        accountKey: String,
        recordingUserId: String,
        currentAccountKey: String?,
        currentRecordingUserId: String?,
        isCancelled: Boolean = false,
    ): Boolean = !isCancelled && accountKey == currentAccountKey && recordingUserId == currentRecordingUserId

    fun canPlayLocalOriginal(recording: LocalRecording): Boolean =
        recording.recordingCaptureState in listOf("stopped", "imported") && recording.localMedia.any { media -> !media.isDurablyUploaded && media.originalSha256 != null }

    /** The local recordings whose originals are not all on the server yet. */
    fun pendingLocalRecordingIds(recordings: List<LocalRecording>): Set<String> = recordings.filter { recording ->
        recording.recordingCaptureState in listOf("stopped", "imported") && !recording.isDeletedRemotely && recording.localMedia.any { media -> !media.isDurablyUploaded }
    }.map { recording -> recording.id }.toSet()

    /** Puts a recording the phone just published into the loaded list at its place, when it is this account's and the list reaches that far. */
    fun mergingPublishedRecording(
        published: Recording,
        existing: List<Recording>,
        from: LocalRecording,
        accountKey: String,
        recordingUserId: String,
        currentAccountKey: String?,
        currentRecordingUserId: String?,
        hasMoreRecordings: Boolean = true,
        isCancelled: Boolean = false,
    ): List<Recording> {
        if (!isCurrentOwner(accountKey, recordingUserId, currentAccountKey, currentRecordingUserId, isCancelled) || !from.belongsTo(accountKey, recordingUserId) ||
            published.userId != recordingUserId || from.recordingId != published.id || published.clientRecordingId != from.clientRecordingId ||
            existing.any { recording -> recording.id == published.id }
        ) {
            return existing
        }
        val last = existing.lastOrNull()
        if (hasMoreRecordings && last != null && !(published.id > last.id)) return existing
        val insertionIndex = existing.indexOfFirst { recording -> published.id > recording.id }.let { index -> if (index < 0) existing.size else index }
        return existing.toMutableList().also { merged -> merged.add(insertionIndex, published) }
    }
}
