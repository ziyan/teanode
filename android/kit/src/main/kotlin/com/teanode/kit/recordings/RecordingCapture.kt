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

enum class RecordingCutReason { UtteranceEnd, LevelPause, MaximumLength }

/**
 * Where a capture is cut into pieces to upload and transcribe: never before twenty seconds, at the first sentence end the
 * live stream hears after that (or, without a stream, when the level falls to near its lowest for a quarter of a second),
 * and always at a minute.
 */
class RecordingCutPolicy {
    private class LevelReading(val atMS: Long, val decibels: Float)

    private var levelReadings = mutableListOf<LevelReading>()
    private var pieceStartMS: Long = 0
    private var utteranceEndHeardAtMS: Long? = null

    fun startPiece(atMS: Long) {
        pieceStartMS = atMS
        utteranceEndHeardAtMS = null
    }

    fun recordLevel(decibels: Float, atMS: Long) {
        levelReadings.add(LevelReading(atMS, if (decibels.isFinite()) decibels else -160f))
        val oldestMS = atMS - LEVEL_WINDOW_MS
        val firstKept = levelReadings.indexOfFirst { reading -> reading.atMS >= oldestMS }
        if (firstKept > 0) levelReadings = levelReadings.subList(firstKept, levelReadings.size).toMutableList()
    }

    fun recordUtteranceEnd(atMS: Long) {
        utteranceEndHeardAtMS = atMS
    }

    fun cutReason(atMS: Long, isStreamConnected: Boolean): RecordingCutReason? {
        val pieceMS = atMS - pieceStartMS
        if (pieceMS >= MAXIMUM_PIECE_MS) return RecordingCutReason.MaximumLength
        if (pieceMS < MINIMUM_PIECE_MS) return null
        if (isStreamConnected) {
            val heardAtMS = utteranceEndHeardAtMS
            return if (heardAtMS != null && heardAtMS - pieceStartMS >= MINIMUM_PIECE_MS) RecordingCutReason.UtteranceEnd else null
        }
        return if (isLevelPaused(atMS)) RecordingCutReason.LevelPause else null
    }

    private fun isLevelPaused(atMS: Long): Boolean {
        val windowReadings = levelReadings.filter { reading -> reading.atMS >= atMS - LEVEL_WINDOW_MS && reading.atMS <= atMS }
        val lowestDecibels = windowReadings.minOfOrNull { reading -> reading.decibels } ?: return false
        val holdStartMS = atMS - PAUSE_HOLD_MS
        val holdReadings = windowReadings.filter { reading -> reading.atMS >= holdStartMS }
        val firstHold = holdReadings.firstOrNull() ?: return false
        if (firstHold.atMS > holdStartMS + LEVEL_READING_TOLERANCE_MS || holdReadings.size < 2) return false
        if (atMS - holdReadings.last().atMS > LEVEL_READING_TOLERANCE_MS) return false
        return holdReadings.all { reading -> reading.decibels <= lowestDecibels + PAUSE_MARGIN_DECIBELS }
    }

    companion object {
        const val MINIMUM_PIECE_MS = 20_000L
        const val MAXIMUM_PIECE_MS = 60_000L
        const val LEVEL_WINDOW_MS = 10_000L
        const val PAUSE_HOLD_MS = 250L
        const val PAUSE_MARGIN_DECIBELS = 6f
        const val LEVEL_READING_TOLERANCE_MS = 100L
    }
}

/** How long the live stream waits to connect again after failing: a second, doubling, never over a minute. */
object RecordingLiveBackoff {
    const val LONGEST_DELAY_SECONDS = 60.0

    fun delaySeconds(failureCount: Int): Double = if (failureCount <= 0) 0.0 else minOf(LONGEST_DELAY_SECONDS, (1L shl minOf(failureCount - 1, 10)).toDouble())
}
