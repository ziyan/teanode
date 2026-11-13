package com.teanode.app.recordings

import android.content.Context
import androidx.work.BackoffPolicy
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import com.teanode.app.TeaNodeApplication
import com.teanode.kit.Connection
import com.teanode.kit.TeaNodeError
import com.teanode.kit.recordings.RECORDING_PART_BYTES
import com.teanode.kit.recordings.RecordingLibrary
import com.teanode.kit.recordings.completeRecordingUpload
import com.teanode.kit.recordings.createRecording
import com.teanode.kit.recordings.createRecordingMedia
import com.teanode.kit.recordings.putRecordingPart
import com.teanode.kit.recordings.recording
import com.teanode.kit.recordings.recordingLimits
import com.teanode.kit.recordings.updateRecordingCapture
import com.teanode.kit.recordings.updateRecordingMetadata
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.RandomAccessFile
import java.time.Instant
import java.util.concurrent.TimeUnit

/** Starts the upload of what the phone holds of recordings; it carries on when the network comes back. */
object RecordingUploadScheduler {
    private const val NAME = "recording-upload"

    fun schedule(context: Context) {
        val request = OneTimeWorkRequestBuilder<RecordingUploadWorker>()
            .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
            .setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 30, TimeUnit.SECONDS).build()
        WorkManager.getInstance(context).enqueueUniqueWork(NAME, ExistingWorkPolicy.APPEND_OR_REPLACE, request)
    }
}

/**
 * Uploads every recording the phone holds for the signed-in account (ios/TeaNode/Sources/RecordingUploads.swift): makes the
 * recording on the server, publishes the capture's state and gaps, then for each original makes its manifest, sends its
 * parts of eight megabytes (each with its digest, skipping those the server already has) and completes it. When the
 * server holds every original, verified, the phone's copy goes and the recording is the server's.
 */
class RecordingUploadWorker(private val context: Context, parameters: WorkerParameters) : CoroutineWorker(context, parameters) {
    override suspend fun doWork(): Result = withContext(Dispatchers.IO) {
        val connection = (context.applicationContext as TeaNodeApplication).session.connection.value ?: return@withContext Result.success()
        val journal = RecordingJournal(context)
        var shouldRetry = false
        for (recording in journal.recordings(connection.accountKey)) {
            if (recording.isDeletedRemotely) continue
            try {
                advance(recording.id, connection, journal)
            } catch (failure: Throwable) {
                if (failure is kotlinx.coroutines.CancellationException) throw failure
                val isConnectivity = RecordingLibrary.isConnectivityFailure(failure)
                journal.update(recording.id) { current ->
                    current.copy(
                        uploadErrorMessage = failure.message ?: failure.toString(), isUploadWaitingForNetwork = isConnectivity,
                        isDeletedRemotely = failure == TeaNodeError.RecordingDeleted || current.isDeletedRemotely,
                    )
                }
                if (failure != TeaNodeError.RecordingDeleted && failure !is TeaNodeError.SignedOut) shouldRetry = true
            }
        }
        if (shouldRetry) Result.retry() else Result.success()
    }

    private suspend fun advance(recordingId: String, connection: Connection, journal: RecordingJournal) {
        var local = journal.read(recordingId) ?: return
        if (local.recordingId == null) {
            val remote = connection.createRecording(local.clientRecordingId, local.recordingTitle, local.recordingOrigin)
            local = journal.update(recordingId) { current -> current.copy(recordingId = remote.id) } ?: return
        }
        val remoteId = local.recordingId ?: throw TeaNodeError.UnreadableAnswer
        // The capture's state, duration and gaps go to the server as they are: while it runs, every pass reports how far it
        // has come; once it is stopped, the stop is published before any piece is uploaded.
        if (local.recordingOrigin == "capture" && local.hasPublishedCapture != true) {
            var remote = connection.recording(remoteId).recording
            if (remote.recordedAt == null) {
                remote = connection.updateRecordingMetadata(remoteId, remote.sourceGeneration, null, com.teanode.kit.photos.ServerTime.text(Instant.ofEpochMilli(local.startedAtEpochMS)))
            }
            val gaps = local.publishedCaptureGaps
            if (remote.recordingCaptureState != local.recordingCaptureState || remote.timelineDurationMS != local.timelineDurationMS || remote.captureGaps != gaps) {
                connection.updateRecordingCapture(remoteId, remote.sourceGeneration, local.recordingCaptureState, local.timelineDurationMS, gaps)
            }
            if (local.recordingCaptureState == "stopped") local = journal.update(recordingId) { current -> current.copy(hasPublishedCapture = true) } ?: return
        }
        val limits = connection.recordingLimits()
        if (limits.uploadPartBytes != RECORDING_PART_BYTES.toLong()) throw TeaNodeError.UnreadableAnswer
        for (mediaIndex in local.localMedia.indices) {
            var media = (journal.read(recordingId) ?: return).localMedia.getOrNull(mediaIndex) ?: continue
            val original = journal.originalFile(recordingId, media.id)
            // The piece still being recorded has no digest yet: it is still growing, and is not touched until it is closed.
            if ((journal.read(recordingId) ?: return).isCaptureUnderWay && media.originalSha256 == null) continue
            if (media.originalSha256 == null) {
                val size = original.length()
                val digest = RecordingJournal.sha256(original)
                journal.update(recordingId) { current -> current.copy(localMedia = current.localMedia.map { piece -> if (piece.id == media.id) piece.copy(originalSizeBytes = size, originalSha256 = digest) else piece }) }
                media = media.copy(originalSizeBytes = size, originalSha256 = digest)
            }
            if (media.isDurablyUploaded) { journal.releaseUploadedOriginal(recordingId, media.id); continue }
            val digest = media.originalSha256 ?: continue
            if (media.originalSizeBytes <= 0) continue
            if (media.mediaManifest == null) {
                if (RecordingJournal.sha256(original) != digest) throw TeaNodeError.Refused("The local original changed. Keep it and try importing again.")
                val manifest = connection.createRecordingMedia(
                    remoteId, media.id, media.originalFilename, media.originalContentType, media.originalSizeBytes, digest, media.timelineStartMS, media.mediaDurationMS,
                )
                journal.update(recordingId) { current -> current.copy(localMedia = current.localMedia.map { piece -> if (piece.id == media.id) piece.copy(mediaManifest = manifest) else piece }) }
                media = media.copy(mediaManifest = manifest)
            }
            val manifest = media.mediaManifest ?: continue
            val acceptedParts = connection.recording(remoteId).uploadParts.filter { part -> part.recordingMediaId == manifest.id }
            RandomAccessFile(original, "r").use { file ->
                for (partIndex in 0 until manifest.partCount) {
                    val offset = partIndex.toLong() * limits.uploadPartBytes
                    val size = minOf(limits.uploadPartBytes, manifest.originalSizeBytes - offset).toInt()
                    val part = ByteArray(size)
                    file.seek(offset)
                    file.readFully(part)
                    val partDigest = RecordingJournal.sha256(part)
                    if (acceptedParts.any { accepted -> accepted.confirmsPart(manifest.id, partIndex, size.toLong(), partDigest) }) continue
                    connection.putRecordingPart(manifest.id, partIndex, part, partDigest)
                    journal.update(recordingId) { current -> current.copy(uploadErrorMessage = null) }
                }
            }
            val receipt = connection.completeRecordingUpload(manifest)
            journal.update(recordingId) { current -> current.copy(localMedia = current.localMedia.map { piece -> if (piece.id == media.id) piece.copy(completionReceipt = receipt) else piece }) }
            journal.releaseUploadedOriginal(recordingId, media.id)
        }
        val current = journal.update(recordingId) { snapshot -> snapshot.copy(uploadErrorMessage = null, isUploadWaitingForNetwork = null) } ?: return
        // Once the server holds every original, verified, nothing is left that only the phone has: the recording is the
        // server's from here. A capture is not left before its stop has been published.
        if (current.recordingCaptureState in listOf("stopped", "imported") && current.localMedia.isNotEmpty() && current.localMedia.all { piece -> piece.isDurablyUploaded } &&
            (current.recordingOrigin != "capture" || current.hasPublishedCapture == true)
        ) {
            journal.remove(recordingId)
        }
    }
}

