package com.teanode.kit.photos

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

/** Ported from the iPhone app's PhotoBackupTimeLeftTests. */
class PhotoBackupTimeLeftTest {
    private val start = 1_000_000.0

    private fun recordSteadily(timeLeft: PhotoBackupTimeLeft, firstMinute: Int, minuteCount: Int, firstCount: Int, itemsPerMinute: Int, isUploading: Boolean = true) {
        for (minute in 0..minuteCount) {
            timeLeft.record(start + (firstMinute + minute) * 60.0, firstCount + minute * itemsPerMinute, isUploading)
        }
    }

    @Test
    fun thereIsNoEstimateBeforeTheSamplesSpanTwoMinutes() {
        val timeLeft = PhotoBackupTimeLeft()
        timeLeft.record(start, 0, true)
        timeLeft.record(start + 60, 100, true)
        assertNull(timeLeft.estimatedSecondsLeft(1_000))
        timeLeft.record(start + 119, 200, true)
        assertNull(timeLeft.estimatedSecondsLeft(1_000))
        timeLeft.record(start + 120, 200, true)
        assertNotNull(timeLeft.estimatedSecondsLeft(1_000))
    }

    @Test
    fun thereIsNoEstimateBeforeFiveItemsAreFinished() {
        val timeLeft = PhotoBackupTimeLeft()
        timeLeft.record(start, 10, true)
        timeLeft.record(start + 600, 14, true)
        assertNull(timeLeft.estimatedSecondsLeft(1_000))
        timeLeft.record(start + 660, 15, true)
        assertNotNull(timeLeft.estimatedSecondsLeft(1_000))
    }

    @Test
    fun aSteadyRateGivesTheItemsLeftOverTheRate() {
        val timeLeft = PhotoBackupTimeLeft()
        recordSteadily(timeLeft, 0, 10, 0, 10)
        assertEquals(3_600.0, timeLeft.estimatedSecondsLeft(600)!!.toDouble(), 1.0)
        assertEquals(600.0, timeLeft.estimatedSecondsLeft(100)!!.toDouble(), 1.0)
    }

    @Test
    fun aBurstAtTheStartIsForgottenOnceItLeavesTheWindow() {
        val timeLeft = PhotoBackupTimeLeft()
        timeLeft.record(start, 0, true)
        recordSteadily(timeLeft, 1, 60, 3_000, 10)
        assertEquals(3_600.0, timeLeft.estimatedSecondsLeft(600)!!.toDouble(), 60.0)
    }

    @Test
    fun theRateFollowsASlowdownButNotAtOnce() {
        val timeLeft = PhotoBackupTimeLeft()
        recordSteadily(timeLeft, 0, 30, 0, 20)
        val before = timeLeft.estimatedSecondsLeft(600)!!
        assertEquals(1_800.0, before.toDouble(), 1.0)
        recordSteadily(timeLeft, 31, 0, 605, 5)
        val soon = timeLeft.estimatedSecondsLeft(600)!!
        assertTrue(soon > before)
        assertTrue(soon < 3_600)
        recordSteadily(timeLeft, 32, 10, 610, 5)
        val later = timeLeft.estimatedSecondsLeft(600)!!
        assertTrue(later > soon)
        assertTrue(later < 7_200 + 600)
    }

    @Test
    fun aReadingWhileNothingUploadsForgetsTheRun() {
        val timeLeft = PhotoBackupTimeLeft()
        recordSteadily(timeLeft, 0, 10, 0, 10)
        assertNotNull(timeLeft.estimatedSecondsLeft(600))
        timeLeft.record(start + 700, 100, false)
        assertNull(timeLeft.estimatedSecondsLeft(600))
    }

    @Test
    fun theEstimateIsNeverUnderAMinute() {
        val timeLeft = PhotoBackupTimeLeft()
        recordSteadily(timeLeft, 0, 10, 0, 10)
        assertEquals(60, timeLeft.estimatedSecondsLeft(1))
    }

    @Test
    fun aTimeLeftIsSaidInRoundNumbers() {
        assertEquals(PhotoBackupSpokenDuration(1, PhotoBackupSpokenDurationUnit.Minutes), PhotoBackupSpokenDuration.ofSeconds(60))
        assertEquals(PhotoBackupSpokenDuration(7, PhotoBackupSpokenDurationUnit.Minutes), PhotoBackupSpokenDuration.ofSeconds(420))
        assertEquals(PhotoBackupSpokenDuration(25, PhotoBackupSpokenDurationUnit.Minutes), PhotoBackupSpokenDuration.ofSeconds(24 * 60))
        assertEquals(PhotoBackupSpokenDuration(3, PhotoBackupSpokenDurationUnit.Hours), PhotoBackupSpokenDuration.ofSeconds(3 * 3600))
        assertEquals(PhotoBackupSpokenDuration(3, PhotoBackupSpokenDurationUnit.Days), PhotoBackupSpokenDuration.ofSeconds(72 * 3600))
    }
}
