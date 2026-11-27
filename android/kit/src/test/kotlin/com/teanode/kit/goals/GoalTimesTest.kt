package com.teanode.kit.goals

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class GoalTimesTest {
    @Test
    fun aCronDayFieldBecomesDayNumbersWithSundayLast() {
        assertEquals((1..7).toList(), GoalWeekday.dayNumbers("*"))
        assertEquals(listOf(1, 2, 3, 4, 5), GoalWeekday.dayNumbers("1-5"))
        assertEquals(listOf(1, 3, 7), GoalWeekday.dayNumbers("0,1,3"))
        assertEquals(listOf(6, 7), GoalWeekday.dayNumbers("6-7"))
        assertNull(GoalWeekday.dayNumbers("mon"))
        assertNull(GoalWeekday.dayNumbers("5-1"))
        assertNull(GoalWeekday.dayNumbers("9"))
    }

    @Test
    fun aWeeklyCronLineIsATimingAndAnythingElseIsNot() {
        val timing = GoalTiming.of("30 7 * * 1-5")!!
        assertEquals(7, timing.hour)
        assertEquals(30, timing.minute)
        assertEquals(GoalTiming.DayPattern.Weekdays, timing.dayPattern)
        assertNull(GoalTiming.of("*/5 * * * *"))
        assertNull(GoalTiming.of("0 8 1 * *"))
        assertNull(GoalTiming.of("0 24 * * *"))
        assertEquals(GoalTiming.DayPattern.EveryDay, GoalTiming.of("0 8 * * *")!!.dayPattern)
        assertEquals(GoalTiming.DayPattern.Weekends, GoalTiming.of("0 8 * * 0,6")!!.dayPattern)
        assertEquals(GoalTiming.DayPattern.OneDay(5), GoalTiming.of("0 17 * * 5")!!.dayPattern)
    }

    @Test
    fun aTimingWritesItsCronLineBack() {
        assertEquals("30 7 * * 1,2,3,4,5", GoalTiming(7, 30, (1..5).toList()).cron)
        assertEquals("0 9 * * *", GoalTiming(9, 0, (1..7).toList()).cron)
        assertEquals("0 9 * * 6,0", GoalTiming(9, 0, listOf(6, 7)).cron)
    }

    @Test
    fun aTimeLineIsAWeeklyTimeAMomentOrAnythingElseKeptAsWritten() {
        assertEquals(GoalTimeLine.Weekly(GoalTiming(7, 30, listOf(1, 2, 3, 4, 5))), GoalTimeLine.of("30 7 * * 1-5"))
        val moment = GoalTimeLine.of("@at 2030-12-11 09:05") as GoalTimeLine.Moment
        assertEquals(java.time.LocalDate.of(2030, 12, 11), moment.date)
        assertEquals(9, moment.hour)
        assertEquals(5, moment.minute)
        assertEquals("@at 2030-12-11 09:05", moment.line)
        assertEquals(GoalTimeLine.Other("*/15 * * * *"), GoalTimeLine.of("*/15 * * * *"))
        assertEquals(GoalTimeLine.Other("@at 2030-13-40 09:05"), GoalTimeLine.of("@at 2030-13-40 09:05"))
        assertEquals(GoalTimeLine.Other("@at 2030-12-11 25:00"), GoalTimeLine.of("@at 2030-12-11 25:00"))
    }

    @Test
    fun aMailTriggerNeedsEveryConditionFilledIn() {
        assertFalse(GoalMailTrigger.isComplete(emptyList()))
        assertTrue(GoalMailTrigger.isComplete(listOf(GoalMailCondition("any", value = null))))
        assertFalse(GoalMailTrigger.isComplete(listOf(GoalMailCondition("from", value = " "))))
        assertFalse(GoalMailTrigger.isComplete(listOf(GoalMailCondition("header", header = "", value = "x"))))
        assertTrue(GoalMailTrigger.isComplete(listOf(GoalMailCondition("header", header = "List-Id", value = "x"), GoalMailCondition("from", value = "a@example.com"))))
    }

    private fun spaced(text: String): String = text.replace('\u202F', ' ').replace('\u00A0', ' ')

    @Test
    fun aPlainCronLineIsDescribedAndAnyOtherIsLeftAlone() {
        assertEquals(GoalTiming.DayPattern.Weekdays, GoalTiming.of("30 7 * * 1-5")!!.dayPattern)
        assertEquals(7, GoalTiming.of("30 7 * * 1-5")!!.hour)
        assertEquals(30, GoalTiming.of("30 7 * * 1-5")!!.minute)
        assertEquals(GoalTiming.DayPattern.OneDay(7), GoalTiming.of("0 9 * * 0")!!.dayPattern)
        assertEquals(GoalTiming.DayPattern.EveryDay, GoalTiming.of("0 9 * * 1,2,3,4,5,6,0")!!.dayPattern)
        assertEquals(GoalTiming.DayPattern.Weekends, GoalTiming.of("0 10 * * 6,0")!!.dayPattern)
        assertEquals(GoalTiming.DayPattern.SomeDays(listOf(1, 3, 5)), GoalTiming.of("0 9 * * 1,3,5")!!.dayPattern)
        for (unplain in listOf("*/15 * * * *", "0 9 1 * *", "0 9 * 6 1", "0 9 * * 1-5/2", "@in 20m", "@at 2030-01-02 09:00", "60 9 * * *", "0 24 * * *", "0 +9 * * *", "0 9 * *")) {
            assertNull(GoalTiming.of(unplain), unplain)
        }
    }

    @Test
    fun aTimeIsWrittenTheLocalesWay() {
        assertEquals("7:30 AM", spaced(GoalTiming.of("30 7 * * 1-5")!!.timeText(java.util.Locale.US)))
        assertEquals("5:00 PM", spaced(GoalTiming.of("0 17 * * 5")!!.timeText(java.util.Locale.US)))
        assertEquals("17:00", GoalTiming.of("0 17 * * 5")!!.timeText(java.util.Locale.UK))
    }

    @Test
    fun daysAreNamedTheLocalesWay() {
        assertEquals("Friday", GoalWeekday.name(5, java.util.Locale.US))
        assertTrue(GoalWeekday.name(5, java.util.Locale.JAPAN) != "Friday")
        assertEquals("Mon, Wed, and Fri", GoalWeekday.shortNamesText(listOf(1, 3, 5), java.util.Locale.US))
        assertEquals("Mon and Fri", GoalWeekday.shortNamesText(listOf(5, 1), java.util.Locale.US))
        assertEquals("Sun", GoalWeekday.shortNamesText(listOf(7), java.util.Locale.US))
    }
}
