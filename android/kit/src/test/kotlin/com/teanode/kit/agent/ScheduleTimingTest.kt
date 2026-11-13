package com.teanode.kit.agent

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class ScheduleTimingTest {
    @Test
    fun aCronDayFieldBecomesDayNumbersWithSundayLast() {
        assertEquals((1..7).toList(), ScheduleWeekday.dayNumbers("*"))
        assertEquals(listOf(1, 2, 3, 4, 5), ScheduleWeekday.dayNumbers("1-5"))
        assertEquals(listOf(1, 3, 7), ScheduleWeekday.dayNumbers("0,1,3"))
        assertEquals(listOf(6, 7), ScheduleWeekday.dayNumbers("6-7"))
        assertNull(ScheduleWeekday.dayNumbers("mon"))
        assertNull(ScheduleWeekday.dayNumbers("5-1"))
        assertNull(ScheduleWeekday.dayNumbers("9"))
    }

    @Test
    fun aWeeklyCronLineIsATimingAndAnythingElseIsNot() {
        val timing = ScheduleTiming.of("30 7 * * 1-5")!!
        assertEquals(7, timing.hour)
        assertEquals(30, timing.minute)
        assertEquals(ScheduleTiming.DayPattern.Weekdays, timing.dayPattern)
        assertNull(ScheduleTiming.of("*/5 * * * *"))
        assertNull(ScheduleTiming.of("0 8 1 * *"))
        assertNull(ScheduleTiming.of("0 24 * * *"))
        assertEquals(ScheduleTiming.DayPattern.EveryDay, ScheduleTiming.of("0 8 * * *")!!.dayPattern)
        assertEquals(ScheduleTiming.DayPattern.Weekends, ScheduleTiming.of("0 8 * * 0,6")!!.dayPattern)
        assertEquals(ScheduleTiming.DayPattern.OneDay(5), ScheduleTiming.of("0 17 * * 5")!!.dayPattern)
    }

    @Test
    fun aTimingWritesItsCronLineBack() {
        assertEquals("30 7 * * 1,2,3,4,5", ScheduleTiming(7, 30, (1..5).toList()).cron)
        assertEquals("0 9 * * *", ScheduleTiming(9, 0, (1..7).toList()).cron)
        assertEquals("0 9 * * 6,0", ScheduleTiming(9, 0, listOf(6, 7)).cron)
    }

    @Test
    fun theDailyBriefReadsItsTimeAndDaysFromItsCronLine() {
        assertEquals("07:30", DailyBrief.time(null))
        assertEquals("08:05", DailyBrief.time("5 8 * * 1"))
        assertEquals(listOf(1, 2, 3, 4, 5), DailyBrief.dayNumbers(null))
        assertEquals(listOf(6, 7), DailyBrief.dayNumbers("0 9 * * 6,0"))
        assertEquals(listOf(1, 2, 3, 4, 5), DailyBrief.dayNumbers("garbage"))
        assertEquals("b", DailyBrief.schedule(listOf(AgentSchedule("a", "Other", ""), AgentSchedule("b", DailyBrief.SCHEDULE_NAME, "")))!!.id)
    }

    @Test
    fun aMailTriggerNeedsEveryConditionFilledIn() {
        assertFalse(ScheduleMailTrigger.isComplete(emptyList()))
        assertTrue(ScheduleMailTrigger.isComplete(listOf(ScheduleMailCondition("any", value = null))))
        assertFalse(ScheduleMailTrigger.isComplete(listOf(ScheduleMailCondition("from", value = " "))))
        assertFalse(ScheduleMailTrigger.isComplete(listOf(ScheduleMailCondition("header", header = "", value = "x"))))
        assertTrue(ScheduleMailTrigger.isComplete(listOf(ScheduleMailCondition("header", header = "List-Id", value = "x"), ScheduleMailCondition("from", value = "a@example.com"))))
    }
}
