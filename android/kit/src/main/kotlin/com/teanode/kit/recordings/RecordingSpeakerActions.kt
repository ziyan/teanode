package com.teanode.kit.recordings

import java.text.NumberFormat
import java.util.Locale

/** Which label of a speaker whose capture is still running (or was just made) a bubble's menu names or separates. */
object RecordingTemporarySpeakers {
    /**
     * The label a speaker of a capture under way is named by: every piece shows the name at once, so there is no passage to
     * choose. Null for a speaker of a finished transcript, who is named by passage.
     */
    fun labelToName(bubble: RecordingConversationBubble): String? = bubble.segments.firstOrNull()?.speakerLabelOfTemporarySpeaker

    /** The label that was found to be another speaker's voice, which "Separate from" gives back; null when none was. */
    fun labelToSeparate(bubble: RecordingConversationBubble): String? =
        bubble.segments.firstNotNullOfOrNull { segment -> segment.mergedTemporarySpeakerLabel?.takeIf { label -> label.isNotEmpty() } }
}

/** The interval a name is given to when it is not given to the whole speaker group (iOS saveSpeaker). */
object RecordingPassageInterval {
    /** The typed start and end in milliseconds, when both are whole numbers inside the passage and the start is before the end; null otherwise. */
    fun parse(startText: String, endText: String, passageStartMS: Long, passageEndMS: Long): LongRange? {
        val startMS = startText.trim().toLongOrNull() ?: return null
        val endMS = endText.trim().toLongOrNull() ?: return null
        if (startMS < passageStartMS || endMS > passageEndMS || startMS >= endMS) return null
        return startMS..endMS
    }
}

/** Taking a name back from a bubble (iOS RecordingConversationModel.clearName). */
object RecordingSpeakerNaming {
    /** Whether any passage of the bubble carries a name someone gave, so there is something to clear. */
    fun hasNaming(bubble: RecordingConversationBubble): Boolean = bubble.segments.any { segment -> !segment.appliedSpeakerCorrectionId.isNullOrEmpty() }

    /** Each naming applied to the bubble's passages once: a name given to several passages is one correction. */
    fun correctionIds(segments: List<RecordingTranscriptSegment>): List<String> =
        segments.mapNotNull { segment -> segment.appliedSpeakerCorrectionId?.takeIf { correctionId -> correctionId.isNotEmpty() } }.distinct()
}

/** What a transcription attempt cost, as the details say it. */
sealed class RecordingAttemptCost {
    /** Billing evidence gave the amount. */
    data class Actual(val amountText: String, val currencyCode: String) : RecordingAttemptCost()

    /** The configured pricing gave an estimate. */
    data class Estimated(val amountText: String, val currencyCode: String) : RecordingAttemptCost()

    data object Unavailable : RecordingAttemptCost()

    companion object {
        /** The actual cost when the server has billing evidence for it, else the estimate, else nothing. */
        fun of(attempt: RecordingTranscriptionAttempt, locale: Locale = Locale.getDefault()): RecordingAttemptCost {
            val format = NumberFormat.getNumberInstance(locale)
            attempt.actualCostAmount?.let { amount -> return Actual(format.format(amount), attempt.currencyCode) }
            attempt.estimatedCostAmount?.let { amount -> return Estimated(format.format(amount), attempt.currencyCode) }
            return Unavailable
        }
    }
}
