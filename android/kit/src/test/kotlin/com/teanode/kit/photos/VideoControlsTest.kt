package com.teanode.kit.photos

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/** Ported from the iPhone app's VideoControlsTests. */
class VideoControlsTest {
    @Test
    fun onlyAPlayingVideoLeftAloneHidesItsControls() {
        assertTrue(VideoControls.shouldHide(isPlaying = true, hasEnded = false, isScrubbing = false, isScreenReaderRunning = false))
        assertFalse(VideoControls.shouldHide(isPlaying = false, hasEnded = false, isScrubbing = false, isScreenReaderRunning = false))
        assertFalse(VideoControls.shouldHide(isPlaying = false, hasEnded = true, isScrubbing = false, isScreenReaderRunning = false))
        assertFalse(VideoControls.shouldHide(isPlaying = true, hasEnded = true, isScrubbing = false, isScreenReaderRunning = false))
        assertFalse(VideoControls.shouldHide(isPlaying = true, hasEnded = false, isScrubbing = true, isScreenReaderRunning = false))
        assertFalse(VideoControls.shouldHide(isPlaying = true, hasEnded = false, isScrubbing = false, isScreenReaderRunning = true))
    }

    @Test
    fun theDelayBeforeHidingIsThreeSeconds() {
        assertEquals(3.0, VideoControls.HIDE_DELAY_SECONDS)
    }

    @Test
    fun playedFractionIsKeptBetweenZeroAndOne() {
        assertEquals(0.25, VideoControls.playedFraction(15.0, 60.0), 0.0001)
        assertEquals(1.0, VideoControls.playedFraction(70.0, 60.0))
        assertEquals(0.0, VideoControls.playedFraction(-2.0, 60.0))
    }

    @Test
    fun playedFractionOfAVideoWithNoKnownLengthIsZero() {
        assertEquals(0.0, VideoControls.playedFraction(5.0, 0.0))
        assertEquals(0.0, VideoControls.playedFraction(5.0, Double.NaN))
        assertEquals(0.0, VideoControls.playedFraction(Double.NaN, 60.0))
        assertEquals(0.0, VideoControls.playedFraction(5.0, Double.POSITIVE_INFINITY))
    }

    @Test
    fun aPointOnTheBarIsAMomentOfTheVideo() {
        assertEquals(40.0, VideoControls.seconds(0.5, 80.0), 0.0001)
        assertEquals(80.0, VideoControls.seconds(1.4, 80.0))
        assertEquals(0.0, VideoControls.seconds(-0.3, 80.0))
        assertEquals(0.0, VideoControls.seconds(0.5, 0.0))
        assertEquals(0.0, VideoControls.seconds(Double.NaN, 80.0))
    }

    @Test
    fun aVideoAtItsEndStartsAgainFromTheBeginning() {
        assertTrue(VideoControls.isAtEnd(60.0, 60.0))
        assertTrue(VideoControls.isAtEnd(59.98, 60.0))
        assertFalse(VideoControls.isAtEnd(59.0, 60.0))
        assertFalse(VideoControls.isAtEnd(0.0, 0.0))
    }

    @Test
    fun movingByAStepStaysInsideTheVideo() {
        assertEquals(15.0, VideoControls.seconds(10.0, 5.0, 60.0))
        assertEquals(0.0, VideoControls.seconds(3.0, -5.0, 60.0))
        assertEquals(60.0, VideoControls.seconds(58.0, 5.0, 60.0))
        assertEquals(0.0, VideoControls.seconds(10.0, 5.0, 0.0))
    }

    @Test
    fun timeTextCountsOnlyTheSecondsReached() {
        assertEquals("0:00", VideoControls.timeText(0.0))
        assertEquals("0:59", VideoControls.timeText(59.9))
        assertEquals("1:23", VideoControls.timeText(83.4))
        assertEquals("1:02:05", VideoControls.timeText(3725.0))
    }

    @Test
    fun timeTextOfATimeThatIsNotKnownIsZero() {
        assertEquals("0:00", VideoControls.timeText(Double.NaN))
        assertEquals("0:00", VideoControls.timeText(-4.0))
        assertEquals("0:00", VideoControls.timeText(Double.POSITIVE_INFINITY))
    }
}
