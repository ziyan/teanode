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
