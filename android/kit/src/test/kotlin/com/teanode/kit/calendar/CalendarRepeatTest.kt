package com.teanode.kit.calendar

import com.teanode.kit.calendar.CalendarRepeat.End
import com.teanode.kit.calendar.CalendarRepeat.Frequency
import com.teanode.kit.calendar.CalendarRepeat.Monthly
import com.teanode.kit.calendar.CalendarRepeat.Parts
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNotNull
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.time.ZoneId
import java.util.Locale

// The same table of rules is in the dashboard's calendarRepeat.test.ts; the two editors must read and write a rule alike.
class CalendarRepeatTest {
    private val english = Locale.US

    /** A Tuesday, in a zone that is eight hours behind UTC in winter. */
    private val timed = CalendarRepeat.Context(CalendarDay(2026, 10, 20), ZoneId.of("America/Los_Angeles"), isAllDay = false)
    private val allDay = CalendarRepeat.Context(CalendarDay(2026, 10, 20), ZoneId.of("UTC"), isAllDay = true)

    private fun parts(
        frequency: Frequency = Frequency.Weekly,
        intervalCount: Int = 1,
        weekdays: List<Int> = listOf(2),
        monthly: Monthly = Monthly.DayOfMonth(20),
        end: End = End.Never,
    ) = Parts(frequency, intervalCount, weekdays, monthly, end)

    private class Row(val name: String, val rule: String, val isAllDay: Boolean, val parts: Parts)

    private val rows = listOf(
        Row("every day", "FREQ=DAILY", false, parts(Frequency.Daily)),
        Row("every 3 days", "FREQ=DAILY;INTERVAL=3", false, parts(Frequency.Daily, intervalCount = 3)),
        Row("every week, on the start weekday", "FREQ=WEEKLY", false, parts()),
        Row("every weekday", "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR", false, parts(weekdays = listOf(1, 2, 3, 4, 5))),
        Row(
            "every 2 weeks on Monday, Wednesday and Friday until a date", "FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE,FR;UNTIL=20270101T075959Z", false,
            parts(intervalCount = 2, weekdays = listOf(1, 3, 5), end = End.OnDate(CalendarDay(2026, 12, 31))),
        ),
        Row("until a date in summer time", "FREQ=DAILY;UNTIL=20261101T065959Z", false, parts(Frequency.Daily, end = End.OnDate(CalendarDay(2026, 10, 31)))),
        Row("every week until a date, all day", "FREQ=WEEKLY;BYDAY=SA,SU;UNTIL=20261231", true, parts(weekdays = listOf(6, 7), end = End.OnDate(CalendarDay(2026, 12, 31)))),
        Row("every month, on the start day", "FREQ=MONTHLY", false, parts(Frequency.Monthly)),
        Row("every month on day 15", "FREQ=MONTHLY;BYMONTHDAY=15", false, parts(Frequency.Monthly, monthly = Monthly.DayOfMonth(15))),
        Row("every month on the second Tuesday", "FREQ=MONTHLY;BYDAY=2TU", false, parts(Frequency.Monthly, monthly = Monthly.OrdinalWeekday(2, 2))),
        Row(
            "every 3 months on the last Friday", "FREQ=MONTHLY;INTERVAL=3;BYDAY=-1FR", false,
            parts(Frequency.Monthly, intervalCount = 3, monthly = Monthly.OrdinalWeekday(-1, 5)),
        ),
        Row("every year", "FREQ=YEARLY", false, parts(Frequency.Yearly)),
        Row("ten times", "FREQ=DAILY;COUNT=10", false, parts(Frequency.Daily, end = End.AfterCount(10))),
    )

    private fun context(isAllDay: Boolean) = if (isAllDay) allDay else timed

    @Test
    fun everyRuleInTheTableReadsAndWrites() {
        for (row in rows) {
            assertEquals(row.parts, CalendarRepeat.parse(row.rule, context(row.isAllDay)), row.name)
            assertEquals(row.rule, CalendarRepeat.write(row.parts, context(row.isAllDay)), row.name)
        }
    }

    @Test
    fun lowerCaseAndAnIntervalOfOneAreReadAndWrittenPlainly() {
        val read = CalendarRepeat.parse("freq=weekly;interval=1;byday=mo,we", timed)
        assertEquals(parts(weekdays = listOf(1, 3)), read)
        assertEquals("FREQ=WEEKLY;BYDAY=MO,WE", CalendarRepeat.write(read!!, timed))
    }

    @Test
    fun anEmptyRuleAndTextThatIsNotARuleHaveNothingToRead() {
        for (rule in listOf("", "   ", "FREQ=HOURLY", "FREQ=DAILY;INTERVAL=0", "FREQ=DAILY;COUNT=x", "FREQ=WEEKLY;BYDAY=XX", "every other tuesday")) {
            assertNull(CalendarRepeat.parse(rule, timed), rule)
        }
    }

    @Test
    fun aRuleWithPartsItCannotShowIsKeptUntilChanged() {
        val rule = "FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=-1"
        val read = CalendarRepeat.parse(rule, timed)!!
        assertTrue(read.hasUnshownParts)
        assertEquals(Frequency.Monthly, read.frequency)
        // Left as it was shown, the rule is kept exactly.
        assertEquals(rule, CalendarRepeat.editedRule(rule, read, read, timed))
        // Changed, it becomes what the editor shows.
        val changed = read.copy(intervalCount = 2)
        assertEquals("FREQ=MONTHLY;INTERVAL=2", CalendarRepeat.editedRule(rule, read, changed, timed))
        // A rule the editor cannot read at all is kept the same way.
        val unreadable = CalendarRepeat.defaultParts(Frequency.Weekly, timed)
        assertEquals("FREQ=SECONDLY", CalendarRepeat.editedRule("FREQ=SECONDLY", unreadable, unreadable, timed))
    }

    @Test
    fun otherPartsTheEditorDoesNotShowAreMarked() {
        for (rule in listOf(
            "FREQ=YEARLY;BYMONTH=10", "FREQ=MONTHLY;BYMONTHDAY=1,15", "FREQ=MONTHLY;BYMONTHDAY=-1", "FREQ=WEEKLY;WKST=SU", "FREQ=DAILY;BYHOUR=9",
            "FREQ=WEEKLY;BYDAY=1MO", "FREQ=DAILY;COUNT=3;UNTIL=20261231",
        )) {
            assertEquals(true, CalendarRepeat.parse(rule, timed)?.hasUnshownParts, rule)
        }
    }

    @Test
    fun presetsAreNamedWhateverOrderAnotherProgramWroteThem() {
        assertEquals(CalendarRepeat.Preset.Never, CalendarRepeat.Preset.of(""))
        assertEquals(CalendarRepeat.Preset.Daily, CalendarRepeat.Preset.of("FREQ=DAILY"))
        assertEquals(CalendarRepeat.Preset.Weekly, CalendarRepeat.Preset.of("freq=weekly"))
        assertEquals(CalendarRepeat.Preset.Weekdays, CalendarRepeat.Preset.of("FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"))
        assertEquals(CalendarRepeat.Preset.Weekdays, CalendarRepeat.Preset.of("BYDAY=FR,MO,TU,WE,TH;FREQ=WEEKLY"))
        assertEquals(CalendarRepeat.Preset.Monthly, CalendarRepeat.Preset.of("FREQ=MONTHLY"))
        assertEquals(CalendarRepeat.Preset.Yearly, CalendarRepeat.Preset.of("FREQ=YEARLY"))
        assertEquals(CalendarRepeat.Preset.Custom, CalendarRepeat.Preset.of("FREQ=WEEKLY;INTERVAL=2"))
        assertEquals(CalendarRepeat.Preset.Custom, CalendarRepeat.Preset.of("FREQ=WEEKLY;BYDAY=MO,TU,WE,TH"))
        assertEquals("FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR", CalendarRepeat.Preset.Weekdays.rule)
        assertNull(CalendarRepeat.Preset.Custom.rule)
    }

    @Test
    fun theEndOfTheChosenDayIsInTheEventZone() {
        val end = End.OnDate(CalendarDay(2026, 12, 31))
        assertTrue(CalendarRepeat.write(parts(end = end), timed).endsWith("UNTIL=20270101T075959Z"))
        val summer = End.OnDate(CalendarDay(2026, 7, 1))
        assertTrue(CalendarRepeat.write(parts(end = summer), timed).endsWith("UNTIL=20260702T065959Z"))
        val tokyo = CalendarRepeat.Context(timed.startDay, ZoneId.of("Asia/Tokyo"), isAllDay = false)
        assertTrue(CalendarRepeat.write(parts(end = end), tokyo).endsWith("UNTIL=20261231T145959Z"))
    }

    @Test
    fun daysKnowTheirWeekdayAndOrdinal() {
        assertEquals(2, CalendarRepeat.weekday(CalendarDay(2026, 10, 20)))
        assertEquals(7, CalendarRepeat.weekday(CalendarDay(2026, 10, 18)))
        assertEquals(1, CalendarRepeat.weekday(CalendarDay(2026, 10, 19)))
        assertEquals(3, CalendarRepeat.ordinal(CalendarDay(2026, 10, 20)))
        assertEquals(1, CalendarRepeat.ordinal(CalendarDay(2026, 10, 1)))
        // The fifth Saturday of the month is the last one.
        assertEquals(-1, CalendarRepeat.ordinal(CalendarDay(2026, 10, 31)))
        assertEquals(CalendarDay(2027, 10, 20), CalendarRepeat.dayOneYearAfter(CalendarDay(2026, 10, 20)))
        assertEquals(CalendarDay(2029, 3, 1), CalendarRepeat.dayOneYearAfter(CalendarDay(2028, 2, 29)))
    }

    private fun sentence(rule: String, context: CalendarRepeat.Context = timed): CalendarRepeat.Sentence {
        val read = CalendarRepeat.parse(rule, context)
        assertNotNull(read, rule)
        return CalendarRepeat.sentence(read!!, english)
    }

    @Test
    fun theSentenceUsesTheLocalesNamesListsAndDates() {
        assertEquals(CalendarRepeat.Sentence.Cadence.Daily(1), sentence("FREQ=DAILY").cadence)
        assertEquals(CalendarRepeat.Sentence.Cadence.Daily(3), sentence("FREQ=DAILY;INTERVAL=3").cadence)
        assertEquals(
            CalendarRepeat.Sentence.Cadence.Weekly(1, "Monday, Tuesday, Wednesday, Thursday, and Friday"),
            sentence("FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR").cadence,
        )
        val spaced = sentence("FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE,FR;UNTIL=20270101T075959Z")
        assertEquals(CalendarRepeat.Sentence.Cadence.Weekly(2, "Monday, Wednesday, and Friday"), spaced.cadence)
        assertEquals(CalendarRepeat.Sentence.Ending.OnDate("December 31, 2026"), spaced.ending)
        assertEquals(CalendarRepeat.Sentence.Cadence.MonthlyDay(1, 15), sentence("FREQ=MONTHLY;BYMONTHDAY=15").cadence)
        assertEquals(CalendarRepeat.Sentence.Cadence.MonthlyWeekday(1, 2, "Tuesday"), sentence("FREQ=MONTHLY;BYDAY=2TU").cadence)
        assertEquals(CalendarRepeat.Sentence.Cadence.MonthlyWeekday(3, -1, "Friday"), sentence("FREQ=MONTHLY;INTERVAL=3;BYDAY=-1FR").cadence)
        assertEquals(CalendarRepeat.Sentence.Cadence.Yearly(1), sentence("FREQ=YEARLY").cadence)
        assertEquals(CalendarRepeat.Sentence.Ending.AfterCount(10), sentence("FREQ=DAILY;COUNT=10").ending)
        assertEquals(CalendarRepeat.Sentence.Ending.OnDate("December 31, 2026"), sentence("FREQ=WEEKLY;UNTIL=20261231", allDay).ending)
        assertEquals(CalendarRepeat.Sentence.Ending.Never, sentence("FREQ=WEEKLY").ending)
    }

    @Test
    fun theSentenceNamesDaysInTheReadersLanguage() {
        val read = CalendarRepeat.parse("FREQ=WEEKLY;BYDAY=MO,WE", timed)!!
        val japanese = CalendarRepeat.sentence(read, Locale.JAPAN)
        val weekdayNamesText = (japanese.cadence as CalendarRepeat.Sentence.Cadence.Weekly).weekdayNamesText
        assertTrue(weekdayNamesText.contains("月曜日"))
        assertFalse(weekdayNamesText.contains("Monday"))
    }
}
