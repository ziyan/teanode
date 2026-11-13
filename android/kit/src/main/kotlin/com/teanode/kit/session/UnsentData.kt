package com.teanode.kit.session

import com.teanode.kit.recordings.LocalRecording

/** What the phone holds for an account that the server does not have yet, and that signing out would delete. */
data class UnsentData(
    val unfinishedRecordingCount: Int = 0,
    val unsentLocationReportCount: Int = 0,
    val hasUnsentMail: Boolean = false,
) {
    val isEmpty: Boolean get() = unfinishedRecordingCount == 0 && unsentLocationReportCount == 0 && !hasUnsentMail

    companion object {
        /** A recording is finished once capture has ended and the server verifiably holds every original. */
        fun isUploadFinished(recording: LocalRecording): Boolean =
            recording.recordingCaptureState in listOf("stopped", "imported") && recording.localMedia.isNotEmpty() &&
                recording.localMedia.all { media -> media.isDurablyUploaded }

        fun unfinishedRecordingCount(recordings: List<LocalRecording>): Int = recordings.count { recording -> !isUploadFinished(recording) }
    }
}
