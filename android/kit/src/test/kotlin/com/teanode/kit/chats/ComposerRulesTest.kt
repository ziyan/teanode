package com.teanode.kit.chats

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class ComposerRulesTest {
    @Test fun `a short draft stays a pill and a wrapping one or an attachment makes a card`() {
        assertEquals(ComposerShape.Pill, ComposerLayout.shapeFor(hasAttachments = false, doesDraftWrapAsPill = false))
        assertEquals(ComposerShape.Card, ComposerLayout.shapeFor(hasAttachments = false, doesDraftWrapAsPill = true))
        assertEquals(ComposerShape.Card, ComposerLayout.shapeFor(hasAttachments = true, doesDraftWrapAsPill = false))
    }

    @Test fun `a change of the pill width under one dp is ignored`() {
        assertEquals(240f, ComposerLayout.acceptedPillFieldWidthDp(240f, 240.9f))
        assertEquals(240f, ComposerLayout.acceptedPillFieldWidthDp(240f, 239.2f))
        assertEquals(241f, ComposerLayout.acceptedPillFieldWidthDp(240f, 241f))
        assertEquals(300f, ComposerLayout.acceptedPillFieldWidthDp(null, 300f))
    }

    @Test fun `the width before the first measure is the screen less margins inset and three buttons`() {
        assertEquals(360f - 24f - 16f - 144f, ComposerLayout.estimatedPillFieldWidthDp(360f))
        assertEquals(752f - 16f - 144f, ComposerLayout.estimatedPillFieldWidthDp(1200f))
        assertEquals(0f, ComposerLayout.estimatedPillFieldWidthDp(100f))
    }

    @Test fun `the round buttons grow with the text up to 60 dp`() {
        assertEquals(36f, ComposerLayout.buttonDp(1f))
        assertEquals(36f, ComposerLayout.buttonDp(0.8f))
        assertEquals(54f, ComposerLayout.buttonDp(1.5f), 0.001f)
        assertEquals(60f, ComposerLayout.buttonDp(3f), 0.001f)
        assertEquals(48f, ComposerLayout.targetDp(1f))
        assertEquals(68f, ComposerLayout.targetDp(3f), 0.001f)
    }

    @Test fun `the pull arms at 72 and disarms under 56`() {
        val pull = EarlierPull()
        assertFalse(pull.pulled(71f, true))
        assertFalse(pull.isArmed)
        assertTrue(pull.pulled(1f, true))
        assertTrue(pull.isArmed)
        assertFalse(pull.pulled(1f, true), "the haptic is for the moment of arming only")
        pull.pulled(-10f, true)
        assertTrue(pull.isArmed, "62 is between the lines, still armed")
        pull.pulled(-10f, true)
        assertFalse(pull.isArmed, "52 is under 56")
    }

    @Test fun `only letting go while armed loads`() {
        val pull = EarlierPull()
        pull.pulled(80f, true)
        assertTrue(pull.released())
        assertEquals(0f, pull.pullDp)
        pull.pulled(40f, true)
        assertFalse(pull.released())
    }

    @Test fun `a pull is not followed without a finger on the list`() {
        val pull = EarlierPull()
        assertFalse(pull.pulled(200f, false))
        assertEquals(0f, pull.pullDp)
        assertFalse(pull.released())
    }

    @Test fun `the arrow fades in from 0_6 to 1`() {
        assertEquals(0.6f, EarlierPull.arrowOpacity(0f))
        assertEquals(0.8f, EarlierPull.arrowOpacity(36f), 0.001f)
        assertEquals(1f, EarlierPull.arrowOpacity(300f))
    }

    @Test fun `the top row says what is above`() {
        assertEquals(EarlierRowState.Loading, EarlierRow.stateOf(true, true, true, true))
        assertEquals(EarlierRowState.Failed, EarlierRow.stateOf(true, false, true, true))
        assertEquals(EarlierRowState.StartOfConversation, EarlierRow.stateOf(false, false, false, true))
        assertEquals(EarlierRowState.Armed, EarlierRow.stateOf(true, false, false, true))
        assertEquals(EarlierRowState.PullHint, EarlierRow.stateOf(true, false, false, false))
        assertEquals(30, EarlierRow.PAGE_SIZE)
    }

    @Test fun `a voice message is 0_5 to 120 seconds and sliding up 80 cancels`() {
        assertFalse(VoiceMessageRules.isCancelArmed(79.9f))
        assertTrue(VoiceMessageRules.isCancelArmed(80f))
        assertEquals(VoiceMessageRules.Outcome.TooShort, VoiceMessageRules.outcome(0.49, false))
        assertEquals(VoiceMessageRules.Outcome.Send, VoiceMessageRules.outcome(0.5, false))
        assertEquals(VoiceMessageRules.Outcome.Cancel, VoiceMessageRules.outcome(30.0, true))
        assertEquals(VoiceMessageRules.Outcome.Cancel, VoiceMessageRules.outcome(0.1, true))
    }

    @Test fun `the countdown starts at 110 seconds and the recording ends at 120`() {
        assertFalse(VoiceMessageRules.isCountingDown(109.9))
        assertTrue(VoiceMessageRules.isCountingDown(110.0))
        assertEquals(10.0, VoiceMessageRules.secondsLeft(110.0))
        assertEquals(0.0, VoiceMessageRules.secondsLeft(130.0))
        assertFalse(VoiceMessageRules.hasReachedLongest(119.9))
        assertTrue(VoiceMessageRules.hasReachedLongest(120.0))
    }

    @Test fun `the list follows only at the end with no finger down`() {
        assertTrue(FollowRule.shouldFollow(isShowingEnd = true, isFingerDown = false))
        assertFalse(FollowRule.shouldFollow(isShowingEnd = true, isFingerDown = true))
        assertFalse(FollowRule.shouldFollow(isShowingEnd = false, isFingerDown = false))
        assertTrue(FollowRule.shouldOfferNewMessages(hasArrivedWhileAway = true, isShowingEnd = false))
        assertFalse(FollowRule.shouldOfferNewMessages(hasArrivedWhileAway = true, isShowingEnd = true))
        assertFalse(FollowRule.shouldOfferNewMessages(hasArrivedWhileAway = false, isShowingEnd = false))
    }

    @Test fun `the end button is New messages only when lines arrived while away`() {
        assertEquals(FollowRule.EndButton.None, FollowRule.endButtonFor(hasArrivedWhileAway = true, isShowingEnd = true))
        assertEquals(FollowRule.EndButton.None, FollowRule.endButtonFor(hasArrivedWhileAway = false, isShowingEnd = true))
        assertEquals(FollowRule.EndButton.NewMessages, FollowRule.endButtonFor(hasArrivedWhileAway = true, isShowingEnd = false))
        assertEquals(FollowRule.EndButton.JumpToEnd, FollowRule.endButtonFor(hasArrivedWhileAway = false, isShowingEnd = false))
    }
}
