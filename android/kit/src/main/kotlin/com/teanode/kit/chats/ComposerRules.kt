package com.teanode.kit.chats

import kotlin.math.abs
import kotlin.math.max
import kotlin.math.min

/** ComposerShape is the two shapes of the new composer: one line with its buttons beside the words, or a card. */
enum class ComposerShape { Pill, Card }

/** ComposerLayout holds the sizes of the composer and decides which shape it takes. All lengths are in dp. */
object ComposerLayout {
    const val CORNER_DP = 24f
    const val INSET_DP = 8f
    const val SIDE_MARGIN_DP = 12f
    const val BOTTOM_MARGIN_DP = 8f
    const val MAX_WIDTH_DP = 752f
    const val BUTTON_DP = 36f
    const val TARGET_DP = 48f

    /** A width that moves less than this is the same width; it would flip the shape back and forth. */
    const val MINIMUM_WIDTH_CHANGE_DP = 1f

    /**
     * The width the words have as a pill, before it has been measured: the screen's width less the margins, the inset
     * and the buttons beside the words. Without it a saved draft would open as a pill and jump to a card.
     */
    fun estimatedPillFieldWidthDp(screenWidthDp: Float, beside: Int = 3): Float {
        val boxWidth = min(screenWidthDp - 2 * SIDE_MARGIN_DP, MAX_WIDTH_DP)
        return max(0f, boxWidth - 2 * INSET_DP - beside * TARGET_DP)
    }

    /** The width to go by after [measuredWidthDp] was measured while the composer was a pill. */
    fun acceptedPillFieldWidthDp(previousWidthDp: Float?, measuredWidthDp: Float): Float {
        if (previousWidthDp == null) return measuredWidthDp
        return if (abs(measuredWidthDp - previousWidthDp) < MINIMUM_WIDTH_CHANGE_DP) previousWidthDp else measuredWidthDp
    }

    /** A card while the words would wrap at the pill's width or something is attached; otherwise a pill. */
    fun shapeFor(hasAttachments: Boolean, doesDraftWrapAsPill: Boolean): ComposerShape =
        if (hasAttachments || doesDraftWrapAsPill) ComposerShape.Card else ComposerShape.Pill
}

/**
 * EarlierPull follows a pull down past the top of a list. It arms at [ARM_DP] and disarms again below [DISARM_DP], so
 * a finger resting near the line does not flicker; only letting go while armed loads. A pull is followed only while a
 * finger is on the list: reaching the top by scrolling never loads anything.
 */
class EarlierPull {
    var pullDp: Float = 0f
        private set
    var isArmed: Boolean = false
        private set

    /** Adds [deltaDp] (positive pulls down) while [isFingerDown]; true when this movement is the one that armed it. */
    fun pulled(deltaDp: Float, isFingerDown: Boolean): Boolean {
        if (!isFingerDown) return false
        pullDp = max(0f, pullDp + deltaDp)
        if (!isArmed && pullDp >= ARM_DP) {
            isArmed = true
            return true
        }
        if (isArmed && pullDp < DISARM_DP) isArmed = false
        return false
    }

    /** The finger left the list: true when it let go while armed, and the pull starts again from nothing. */
    fun released(): Boolean {
        val shouldLoad = isArmed
        pullDp = 0f
        isArmed = false
        return shouldLoad
    }

    companion object {
        const val ARM_DP = 72f
        const val DISARM_DP = 56f

        /** The arrow's opacity: 0.6 at rest, 1 once the pull reaches the arming line. */
        fun arrowOpacity(pullDp: Float): Float = 0.6f + 0.4f * min(1f, max(0f, pullDp) / ARM_DP)
    }
}

/** What the row at the very top of a conversation says is above it. */
enum class EarlierRowState { PullHint, Armed, Loading, Failed, StartOfConversation }

object EarlierRow {
    /** How many messages one pull asks for. */
    const val PAGE_SIZE = 30

    fun stateOf(hasEarlier: Boolean, isLoading: Boolean, hasFailed: Boolean, isArmed: Boolean): EarlierRowState = when {
        isLoading -> EarlierRowState.Loading
        hasFailed -> EarlierRowState.Failed
        !hasEarlier -> EarlierRowState.StartOfConversation
        isArmed -> EarlierRowState.Armed
        else -> EarlierRowState.PullHint
    }
}

/** What a voice message recording may be, and what letting go does. */
object VoiceMessageRules {
    const val SHORTEST_SECONDS = 0.5
    const val LONGEST_SECONDS = 120.0
    const val COUNTDOWN_FROM_SECONDS = 110.0
    const val CANCEL_DRAG_DP = 80f

    enum class Outcome { Send, Cancel, TooShort }

    /** Sliding up this far arms the cancel. */
    fun isCancelArmed(dragUpDp: Float): Boolean = dragUpDp >= CANCEL_DRAG_DP

    fun isCountingDown(elapsedSeconds: Double): Boolean = elapsedSeconds >= COUNTDOWN_FROM_SECONDS

    fun hasReachedLongest(elapsedSeconds: Double): Boolean = elapsedSeconds >= LONGEST_SECONDS

    /** The seconds left to show in the countdown, never below zero. */
    fun secondsLeft(elapsedSeconds: Double): Double = max(0.0, LONGEST_SECONDS - elapsedSeconds)

    /** What happens when the finger lets go after [durationSeconds]. */
    fun outcome(durationSeconds: Double, isCancelArmed: Boolean): Outcome = when {
        isCancelArmed -> Outcome.Cancel
        durationSeconds < SHORTEST_SECONDS -> Outcome.TooShort
        else -> Outcome.Send
    }
}

/** When a conversation moves by itself to show what is new. */
object FollowRule {
    /** The list follows new messages only while it shows the end and no finger is on it. */
    fun shouldFollow(isShowingEnd: Boolean, isFingerDown: Boolean): Boolean = isShowingEnd && !isFingerDown

    /** The "New messages" button waits for what arrived while the list was not following, until the end is shown. */
    fun shouldOfferNewMessages(hasArrivedWhileAway: Boolean, isShowingEnd: Boolean): Boolean = hasArrivedWhileAway && !isShowingEnd
}
