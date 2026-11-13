package com.teanode.kit.calendar

import com.teanode.kit.TeaNodeJson
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.DayOfWeek
import java.time.ZoneId

class CalendarMonthTest {
    private fun rules(firstDayOfWeek: DayOfWeek, minimumDaysInFirstWeek: Int, zone: String = "UTC") =
        CalendarRules(ZoneId.of(zone), firstDayOfWeek, minimumDaysInFirstWeek)

    private fun event(id: String, startsAt: String, endsAt: String, allDay: Boolean = false, summary: String = "Event"): CalendarEvent =
        TeaNodeJson.decodeFromString(
            CalendarEvent.serializer(),
            """{"id":"$id","calendarId":"c","summary":"$summary","startsAt":"$startsAt","endsAt":"$endsAt","allDay":$allDay,"recurring":false}""",
        )

    @Test
    fun monthsCountOnAcrossTheYear() {
        val october = CalendarMonth(2026, 10)
        assertEquals(CalendarMonth(2027, 1), october.adding(3))
        assertEquals(CalendarMonth(2025, 12), october.adding(-10))
        assertEquals(1, october.adding(1).id - october.id)
        assertTrue(october < october.adding(1))
    }

    @Test
    fun aMonthStartingOnThursdayFromMonday() {
        // October 2026 starts on a Thursday and has 31 days.
        val layout = MonthLayout.of(CalendarMonth(2026, 10), rules(DayOfWeek.MONDAY, 4))
        assertEquals(3, layout.firstDayColumn)
        assertEquals(5, layout.weeks.size)
        assertEquals(0, layout.weeks[0].days.take(3).filterNotNull().size)
        assertEquals(CalendarDay(2026, 10, 1), layout.weeks[0].days[3])
        assertEquals(CalendarDay(2026, 10, 31), layout.weeks[4].days[5])
        assertNull(layout.weeks[4].days[6])
        assertTrue(layout.weeks.all { week -> week.days.size == 7 })
        // ISO 8601 weeks: October 1, 2026 is in week 40.
        assertEquals(listOf(40, 41, 42, 43, 44), layout.weeks.map { week -> week.weekNumber })
    }

    @Test
    fun aMonthFromSundayFillsItsRowsExactly() {
        val layout = MonthLayout.of(CalendarMonth(2026, 10), rules(DayOfWeek.SUNDAY, 1))
        assertEquals(4, layout.firstDayColumn)
        assertEquals(5, layout.weeks.size)
        assertEquals(CalendarDay(2026, 10, 31), layout.weeks[4].days[6])
    }

    @Test
    fun theWeekAcrossNewYearIsTheFirstOfTheNextYear() {
        // December 28, 2026 is a Monday; that week holds January 1, 2027, a Friday, so by ISO 8601 it is week 53 of 2026.
        val iso = rules(DayOfWeek.MONDAY, 4)
        val december = MonthLayout.of(CalendarMonth(2026, 12), iso)
        val january = MonthLayout.of(CalendarMonth(2027, 1), iso)
        assertEquals(january.weeks.first().weekNumber, december.weeks.last().weekNumber)
        assertEquals(53, january.weeks.first().weekNumber)
        assertEquals(1, january.weeks[1].weekNumber)
    }

    @Test
    fun februaryOfALeapYearHasItsTwentyNinthDay() {
        val layout = MonthLayout.of(CalendarMonth(2028, 2), rules(DayOfWeek.SUNDAY, 1))
        assertEquals(29, layout.weeks.flatMap { week -> week.days }.filterNotNull().size)
    }

    @Test
    fun eventsFallOnEveryDayTheyCover() {
        val zone = ZoneId.of("America/Los_Angeles")
        val events = listOf(
            // A trip of three days, written with its exclusive end.
            event("trip", "2026-10-18T00:00:00Z", "2026-10-21T00:00:00Z", allDay = true, summary = "Trip"),
            // In the evening in Los Angeles, though the next day in UTC.
            event("dinner", "2026-10-20T02:00:00Z", "2026-10-20T04:00:00Z", summary = "Dinner"),
            // Ends at midnight: nothing of it is on the next day.
            event("late", "2026-10-05T05:00:00Z", "2026-10-05T07:00:00Z", summary = "Late"),
            // Started in September, ends on October 2.
            event("across", "2026-09-29T00:00:00Z", "2026-10-03T00:00:00Z", allDay = true, summary = "Across"),
            // Another month altogether.
            event("november", "2026-11-03T17:00:00Z", "2026-11-03T18:00:00Z", summary = "Later"),
        )
        val byDay = CalendarDates.eventsByDay(events, CalendarMonth(2026, 10), zone)
        fun titles(day: Int): List<String> = (byDay[CalendarDay(2026, 10, day)] ?: emptyList()).map { event -> event.summary.orEmpty() }
        assertEquals(listOf("Trip"), titles(18))
        // The all-day event comes before the timed one on the same day.
        assertEquals(listOf("Trip", "Dinner"), titles(19))
        assertEquals(listOf("Trip"), titles(20))
        assertEquals(emptyList<String>(), titles(21))
        assertEquals(listOf("Late"), titles(4))
        assertEquals(emptyList<String>(), titles(5))
        assertEquals(listOf("Across"), titles(1))
        assertEquals(listOf("Across"), titles(2))
        assertEquals(emptyList<String>(), titles(3))
        assertTrue(byDay.keys.all { day -> day.month == 10 })
    }

    @Test
    fun chipsLeaveTheLastLineForWhatDoesNotFit() {
        assertEquals(EventChipSplit(0, 0), EventChipSplit.of(0, 3))
        assertEquals(EventChipSplit(3, 0), EventChipSplit.of(3, 3))
        assertEquals(EventChipSplit(2, 3), EventChipSplit.of(5, 3))
        // At the largest text sizes a cell has two lines: one chip and "+N".
        assertEquals(EventChipSplit(1, 3), EventChipSplit.of(4, 2))
        assertEquals(EventChipSplit(0, 2), EventChipSplit.of(2, 1))
    }

    @Test
    fun calendarColoursAreRead() {
        assertEquals(CalendarColorComponents.fromHexText("ff8000"), CalendarColorComponents.fromHexText("#FF8000"))
        val orange = CalendarColorComponents.fromHexText("#ff8000")
        assertEquals(1.0, orange?.red)
        assertEquals(128.0 / 255, orange?.green ?: 0.0, 0.0001)
        assertEquals(0.0, orange?.blue)
        assertEquals(CalendarColorComponents.fromHexText("#00ff00"), CalendarColorComponents.fromHexText("#0f0"))
        assertNull(CalendarColorComponents.fromHexText(null))
        assertNull(CalendarColorComponents.fromHexText(""))
        assertNull(CalendarColorComponents.fromHexText("blue"))
    }

    @Test
    fun aPaletteColorStaysWithItsCalendar() {
        val first = CalendarPalette.index("calendar-one", 8)
        assertEquals(first, CalendarPalette.index("calendar-one", 8))
        assertTrue(first in 0 until 8)
        val spread = (0 until 40).map { number -> CalendarPalette.index("calendar-$number", 8) }.toSet()
        assertTrue(spread.size > 4)
        assertEquals(0, CalendarPalette.index("any", 0))
        assertEquals(5, CalendarPalette.spreadIndex(5))
        assertEquals(0, CalendarPalette.spreadIndex(8))
        assertEquals(7, CalendarPalette.spreadIndex(-1))
    }

    @Test
    fun lunarNames() {
        assertEquals("正月", LunarDates.name(1, 1, false))
        assertEquals("闰六月", LunarDates.name(6, 1, true))
        assertEquals("十五", LunarDates.name(8, 15, false))
        assertEquals("二十", LunarDates.name(3, 20, false))
        assertEquals("廿一", LunarDates.name(3, 21, false))
        assertEquals("三十", LunarDates.name(3, 30, false))
        assertEquals("", LunarDates.name(3, 31, false))
    }

    @Test
    fun serverTimesReadWithFractionsAndOffsets() {
        assertNotNull(ServerTime.instant("2026-10-20T10:00:00.123456789Z"))
        assertEquals(ServerTime.instant("2026-10-20T10:00:00Z"), ServerTime.instant("2026-10-20T18:00:00+08:00"))
        assertNull(ServerTime.instant("tomorrow"))
        assertTrue(ServerTime.isLater("2026-10-20T10:00:00.500Z", "2026-10-20T10:00:00.250Z"))
        assertTrue(!ServerTime.isLater("2026-10-20T10:00:00Z", "2026-10-20T10:00:00Z"))
        assertTrue(ServerTime.isLater("2026-10-20T10:00:00Z", null))
        assertTrue(!ServerTime.isLater(null, "2026-10-20T10:00:00Z"))
    }

    @Test
    fun allDayEventsAreOnTheirOwnDayWhateverTheZone() {
        val all = event("a", "2026-10-18T00:00:00Z", "2026-10-19T00:00:00Z", allDay = true)
        assertEquals(CalendarDay(2026, 10, 18), CalendarDates.localDay(all, ZoneId.of("America/Los_Angeles")))
        assertEquals("2026-10-18T00:00:00Z", CalendarDates.allDayText(CalendarDay(2026, 10, 18)))
        assertEquals(CalendarDay(2026, 10, 19), CalendarDates.dayAfter(CalendarDay(2026, 10, 18)))
        assertEquals(CalendarDay(2026, 10, 18), CalendarDates.lastDay(all.startsAt, all.endsAt))
    }
}
