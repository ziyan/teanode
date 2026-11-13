package com.teanode.kit.recordings

/** Milliseconds of recorded time and the pieces a capture is cut into. */
object RecordingClock {
    fun durationMS(seconds: Double): Long {
        val milliseconds = seconds * 1000
        return if (milliseconds.isFinite() && milliseconds >= 0 && milliseconds < Long.MAX_VALUE.toDouble()) milliseconds.toLong() else 0
    }
}

object RecordingPieceBoundary {
    fun lengthMS(writtenFrames: Long, sampleRate: Double): Long =
        if (writtenFrames <= 0 || sampleRate <= 0) 0 else Math.round(writtenFrames * 1000 / sampleRate)

    fun nextStartMS(previousStartMS: Long, writtenFrames: Long, sampleRate: Double): Long = previousStartMS + lengthMS(writtenFrames, sampleRate)

    fun gapMS(previousEndMS: Long, startMS: Long): Long = maxOf(0L, startMS - previousEndMS)
}

/** A change of the recorder that does not finish in thirty seconds is stuck, and the screen says so. */
object RecordingStuckChange {
    const val LIMIT_SECONDS = 30.0

    fun isStuck(startedUptimeSeconds: Double, nowUptimeSeconds: Double): Boolean = nowUptimeSeconds - startedUptimeSeconds > LIMIT_SECONDS
}

/**
 * What the recorder is asked to do while it is changing over from one piece to the next: a stop beats a pause beats a
 * rollover, and each kind is asked once however often it was asked for.
 */
class RecordingCaptureTransitions {
    sealed class Command {
        data object Stop : Command()

        data class Pause(val isInterruption: Boolean) : Command()

        data class Rollover(val recorderId: String, val wasSuccessful: Boolean) : Command()
    }

    private var hasPendingStop = false
    private var hasPendingPause = false
    private var hasPendingInterruption = false
    private var pendingRollover: Command? = null

    fun enqueue(command: Command) {
        when (command) {
            Command.Stop -> hasPendingStop = true
            is Command.Pause -> {
                hasPendingPause = true
                hasPendingInterruption = hasPendingInterruption || command.isInterruption
            }
            is Command.Rollover -> pendingRollover = command
        }
    }

    val shouldStartNextSegment: Boolean get() = !hasPendingStop && !hasPendingPause

    fun next(): Command? {
        if (hasPendingStop) {
            reset()
            return Command.Stop
        }
        if (hasPendingPause) {
            val isInterruption = hasPendingInterruption
            reset()
            return Command.Pause(isInterruption)
        }
        val command = pendingRollover
        pendingRollover = null
        return command
    }

    private fun reset() {
        hasPendingStop = false
        hasPendingPause = false
        hasPendingInterruption = false
        pendingRollover = null
    }
}

/** The accounts whose recordings wait to upload, each once, first come first served. */
class RecordingUploadScheduling {
    private val pendingAccountKeys = mutableListOf<String>()

    val isEmpty: Boolean get() = pendingAccountKeys.isEmpty()

    fun enqueue(accountKey: String) {
        if (accountKey !in pendingAccountKeys) pendingAccountKeys.add(accountKey)
    }

    fun next(): String? = if (pendingAccountKeys.isEmpty()) null else pendingAccountKeys.removeAt(0)

    fun remove(accountKey: String) {
        pendingAccountKeys.remove(accountKey)
    }
}

/** A link to a recording or to a passage of its transcript, as the dashboard and the agent write them. */
data class RecordingLink(val recordingId: String, val recordingTranscriptRunId: String?, val passageStartMS: Long?, val passageEndMS: Long?) {
    val id: String get() = listOf(recordingId, recordingTranscriptRunId.orEmpty(), passageStartMS?.toString().orEmpty(), passageEndMS?.toString().orEmpty()).joinToString("/")

    /** Whether a segment is inside the linked passage; every segment is when the link names no passage. */
    fun includes(segment: RecordingTranscriptSegment): Boolean {
        val start = passageStartMS ?: return true
        val end = passageEndMS ?: return true
        return segment.segmentEndMS > start && segment.segmentStartMS < end
    }

    companion object {
        /** Reads the path and query of a link to this server (/recordings/<id>?version=<run>&startMS=&endMS=), or null for anything else. */
        fun parse(pathAndQuery: String): RecordingLink? {
            if (pathAndQuery.length > 4096) return null
            val path = pathAndQuery.substringBefore('?')
            val segments = path.split('/')
            if (segments.size != 3 || segments[0].isNotEmpty() || segments[1] != "recordings" || !isRecordingIdentifier(segments[2])) return null
            val query = if (pathAndQuery.contains('?')) pathAndQuery.substringAfter('?').split('&').filter { pair -> pair.isNotEmpty() } else emptyList()
            val pairs = query.map { pair -> pair.substringBefore('=') to (if (pair.contains('=')) pair.substringAfter('=') else null) }
            val keys = listOf("version", "run", "startMS", "endMS")
            if (keys.any { key -> pairs.count { pair -> pair.first == key } > 1 }) return null
            if (pairs.any { pair -> pair.first in listOf("version", "run") && pair.second == null }) return null
            val version = pairs.firstOrNull { pair -> pair.first == "version" }?.second
            val legacyRun = pairs.firstOrNull { pair -> pair.first == "run" }?.second
            if (version != null && legacyRun != null && version != legacyRun) return null
            val runId = version ?: legacyRun
            if (runId != null && !isRecordingIdentifier(runId)) return null
            val start = pairs.firstOrNull { pair -> pair.first == "startMS" }
            val end = pairs.firstOrNull { pair -> pair.first == "endMS" }
            if (start == null && end == null) return RecordingLink(segments[2], runId, null, null)
            val startMS = start?.second?.toLongOrNull()
            val endMS = end?.second?.toLongOrNull()
            if (runId == null || startMS == null || endMS == null || startMS < 0 || endMS <= startMS) return null
            return RecordingLink(segments[2], runId, startMS, endMS)
        }
    }
}
