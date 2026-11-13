package com.teanode.kit.recordings

/** The prefix of the id of a passage that is a live utterance: words heard while the recording is still going. */
const val RECORDING_LIVE_ID_PREFIX = "live:"

/** A live utterance as a passage, so the conversation draws it among the transcribed ones. */
fun RecordingTranscriptSegment.Companion.live(utterance: RecordingLiveUtterance): RecordingTranscriptSegment = RecordingTranscriptSegment(
    id = RECORDING_LIVE_ID_PREFIX + utterance.utteranceId, localSpeakerId = "live", segmentStartMS = utterance.utteranceStartMS,
    segmentEndMS = utterance.utteranceEndMS, transcriptText = utterance.transcriptText,
)

val RecordingTranscriptSegment.isLiveUtterance: Boolean get() = recordingTranscriptRunId.isEmpty() && id.startsWith(RECORDING_LIVE_ID_PREFIX)

val RecordingTranscriptSegment.speakerLabelOfTemporarySpeaker: String? get() = temporarySpeakerLabel?.takeIf { label -> label.isNotEmpty() }

/** One speaker's run of passages, drawn as a bubble. */
data class RecordingConversationBubble(
    val id: String,
    val segments: List<RecordingTranscriptSegment>,
    val confirmedSpeakerName: String?,
    /** Which of the unconfirmed speakers this is, in the order they first speak: "Speaker A", "Speaker B". */
    val unconfirmedSpeakerOrdinal: Int,
    val isAccountOwner: Boolean,
    val startMS: Long,
    val temporarySpeakerLabel: String?,
    val isLive: Boolean,
) {
    val bubbleText: String get() = segments.joinToString(" ") { segment -> segment.displayedTranscriptText }
}

/** Groups the passages of a transcript into what each speaker said in a row, as a conversation. */
object RecordingConversation {
    fun bubbles(segments: List<RecordingTranscriptSegment>, liveUtterances: List<RecordingLiveUtterance> = emptyList()): List<RecordingConversationBubble> {
        val passages = (segments.filter(::hasWords) + liveUtterances.map { utterance -> RecordingTranscriptSegment.live(utterance) })
            .withIndex().sortedWith(compareBy({ entry -> entry.value.segmentStartMS }, { entry -> entry.index })).map { entry -> entry.value }
        val bubbles = mutableListOf<RecordingConversationBubble>()
        var currentKey: String? = null
        var currentSegments = mutableListOf<RecordingTranscriptSegment>()
        val ordinalByKey = mutableMapOf<String, Int>()
        fun flush() {
            val first = currentSegments.firstOrNull() ?: return
            val key = currentKey ?: return
            bubbles.add(
                RecordingConversationBubble(
                    id = first.id, segments = currentSegments, confirmedSpeakerName = first.confirmedSpeakerName, unconfirmedSpeakerOrdinal = ordinalByKey[key] ?: 0,
                    isAccountOwner = first.confirmedSpeakerName != null && first.isAccountOwner == true, startMS = first.segmentStartMS,
                    temporarySpeakerLabel = if (first.confirmedSpeakerName == null) first.speakerLabelOfTemporarySpeaker else null, isLive = first.isLiveUtterance,
                ),
            )
            currentSegments = mutableListOf()
        }
        for ((passageIndex, segment) in passages.withIndex()) {
            var key = speakerKey(segment)
            val previousKey = currentKey
            if (previousKey != null && key != previousKey && isBackchannel(segment)) {
                val following = passages.getOrNull(passageIndex + 1)
                val isMidSentence = !(currentSegments.lastOrNull()?.let { last -> endsSentence(last.displayedTranscriptText) } ?: true)
                // A short "mm-hm" from an unnamed voice stays inside the bubble it interrupts, as does a named one
                // that cuts into a sentence the same speaker goes on with.
                if (segment.confirmedSpeakerName == null || (isMidSentence && following?.let(::speakerKey) == previousKey)) key = previousKey
            }
            if (key.startsWith("local:") && key !in ordinalByKey) ordinalByKey[key] = ordinalByKey.size
            if (key != currentKey) flush()
            currentKey = key
            currentSegments.add(segment)
        }
        flush()
        return bubbles
    }

    const val BACKCHANNEL_WORDS_MAXIMUM = 3
    const val BACKCHANNEL_MILLISECONDS_MAXIMUM = 800L

    fun isBackchannel(segment: RecordingTranscriptSegment): Boolean {
        if (segment.isLiveUtterance) return false
        val wordCount = segment.displayedTranscriptText.split(Regex("\\s+")).count { word -> word.isNotEmpty() }
        return wordCount <= BACKCHANNEL_WORDS_MAXIMUM && segment.segmentEndMS - segment.segmentStartMS <= BACKCHANNEL_MILLISECONDS_MAXIMUM
    }

    fun endsSentence(text: String): Boolean {
        val trimmed = text.trim().trim { character -> character in "\"'”’»」』)]）" }
        val last = trimmed.lastOrNull() ?: return true
        return last in ".?!。？！…‥"
    }

    fun speakerCount(segments: List<RecordingTranscriptSegment>): Int = segments.filter(::hasWords).map(::speakerKey).toSet().size

    fun hasWords(segment: RecordingTranscriptSegment): Boolean = segment.isFiller != true && !segment.displayedTranscriptText.all { character -> character.isWhitespace() }

    fun speakerLetter(ordinal: Int): String = if (ordinal in 0..25) ('A' + ordinal).toString() else (ordinal + 1).toString()

    private fun speakerKey(segment: RecordingTranscriptSegment): String {
        if (segment.isLiveUtterance) return "live:" + segment.id
        val label = segment.speakerLabelOfTemporarySpeaker
        if (segment.confirmedSpeakerName == null && label != null) return "temporary:$label"
        val identityId = segment.effectiveSpeakerIdentityId
        if (segment.confirmedSpeakerName != null && !identityId.isNullOrEmpty()) return "identity:$identityId"
        return "local:" + segment.recordingTranscriptionAttemptId + ":" + segment.localSpeakerId
    }
}

/** How loud the microphone is, on a scale from nothing to one. */
object RecordingInputLevel {
    const val FLOOR_DECIBELS = -50f

    fun normalized(decibels: Float): Double {
        if (!decibels.isFinite()) return 0.0
        val clamped = minOf(0f, maxOf(FLOOR_DECIBELS, decibels))
        return ((clamped - FLOOR_DECIBELS) / -FLOOR_DECIBELS).toDouble()
    }
}

/** The words heard while a recording is going, kept until the server's transcript covers them. */
object RecordingLiveWords {
    private const val COVERED_FRACTION = 0.6
    const val HEARD_UTTERANCES_KEPT = 100

    fun shown(
        serverUtterances: List<RecordingLiveUtterance>,
        heardUtterances: List<RecordingLiveUtterance>,
        partialText: String,
        partialStartMS: Long,
        segments: List<RecordingTranscriptSegment>,
        windowStartMS: Long = 0,
    ): List<RecordingLiveUtterance> {
        val utterancesById = mutableMapOf<String, RecordingLiveUtterance>()
        for (utterance in serverUtterances + heardUtterances) if (utterance.utteranceId !in utterancesById) utterancesById[utterance.utteranceId] = utterance
        val shown = utterancesById.values.filter { utterance ->
            utterance.utteranceEndMS >= windowStartMS && !isCovered(utterance, segments) && utterance.transcriptText.isNotBlank()
        }.sortedWith(compareBy({ utterance -> utterance.utteranceStartMS }, { utterance -> utterance.utteranceId })).toMutableList()
        val partial = partialText.trim()
        if (partial.isNotEmpty()) {
            val start = maxOf(partialStartMS, shown.lastOrNull()?.utteranceEndMS ?: 0)
            shown.add(RecordingLiveUtterance("partial", start, start, partial))
        }
        return shown
    }

    /** Whether the transcribed passages already cover most of an utterance, so it is no longer shown twice. */
    fun isCovered(utterance: RecordingLiveUtterance, segments: List<RecordingTranscriptSegment>): Boolean {
        val durationMS = utterance.utteranceEndMS - utterance.utteranceStartMS
        if (durationMS <= 0) return segments.any { segment -> segment.segmentStartMS <= utterance.utteranceStartMS && utterance.utteranceStartMS < segment.segmentEndMS }
        val coveredMS = segments.sumOf { segment -> maxOf(0L, minOf(segment.segmentEndMS, utterance.utteranceEndMS) - maxOf(segment.segmentStartMS, utterance.utteranceStartMS)) }
        return minOf(coveredMS, durationMS).toDouble() >= durationMS * COVERED_FRACTION
    }

    fun appending(utterance: RecordingLiveUtterance, heard: List<RecordingLiveUtterance>): List<RecordingLiveUtterance> =
        (heard.filter { kept -> kept.utteranceId != utterance.utteranceId } + utterance).takeLast(HEARD_UTTERANCES_KEPT)
}

/** The last bars of the level meter, newest on the right. */
class RecordingLevelHistory {
    private var levels = mutableListOf<Double>()

    fun append(decibels: Float) {
        levels.add(RecordingInputLevel.normalized(decibels))
        if (levels.size > BAR_COUNT) levels = levels.takeLast(BAR_COUNT).toMutableList()
    }

    val bars: List<Double> get() = List(BAR_COUNT - levels.size) { 0.0 } + levels

    companion object {
        const val BAR_COUNT = 48
    }
}

object RecordingLiveSession {
    const val RECYCLE_AFTER_SECONDS = 25.0 * 60

    fun shouldRecycle(sessionAgeSeconds: Double): Boolean = sessionAgeSeconds >= RECYCLE_AFTER_SECONDS
}
