package com.teanode.kit.journal

import com.teanode.kit.TeaNodeJson
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import java.math.BigDecimal

class JournalDaysTest {
    @Test
    fun aDayCountsOnAndBack() {
        val date = JournalDays.date("2026-10-22")!!
        assertEquals("2026-10-22", JournalDays.text(date))
        assertEquals("2026-10-23", JournalDays.adding(1, "2026-10-22"))
        assertEquals("2026-09-22", JournalDays.adding(-30, "2026-10-22"))
        // The night the clocks go back in a zone with summer time does not move a day: a day is a date, not an instant.
        assertEquals("2026-11-02", JournalDays.adding(1, "2026-11-01"))
        assertEquals("2026-03-08", JournalDays.adding(-1, "2026-03-09"))
    }

    @Test
    fun textThatIsNotADayIsNotOne() {
        assertNull(JournalDays.date("2026-06-31"))
        assertNull(JournalDays.date("yesterday"))
        assertNull(JournalDays.adding(1, "2026-13-01"))
    }

    @Test
    fun aWeekStartsWhereTheLocaleStartsIt() {
        // 2026-10-22 is a Thursday.
        assertEquals("2026-10-19", JournalDays.weekStart("2026-10-22", "monday"))
        assertEquals("2026-10-18", JournalDays.weekStart("2026-10-22", "sunday"))
        // The first day of a week is its own week's start.
        assertEquals("2026-10-19", JournalDays.weekStart("2026-10-19", "monday"))
        assertEquals("2026-10-18", JournalDays.weekStart("2026-10-18", "sunday"))
        // A Sunday ends a week that starts on Monday.
        assertEquals("2026-10-19", JournalDays.weekStart("2026-10-25", "monday"))
        // A week reaches back across a month and a year.
        assertEquals("2026-12-28", JournalDays.weekStart("2027-01-01", "monday"))
    }

    @Test
    fun monthsAndYearsStartOnTheirFirstDay() {
        assertEquals("2026-10-01", JournalDays.monthStart("2026-10-22"))
        assertEquals("2026-01-01", JournalDays.yearStart("2026-10-22"))
        assertNull(JournalDays.monthStart("October"))
        assertEquals("2026-10-01", JournalDays.periodStart("2026-10-22", JournalPeriodKind.Month, "monday"))
    }

    @Test
    fun aPeriodMovesByItsOwnLength() {
        assertEquals("2026-10-26", JournalDays.movedPeriodStart("2026-10-19", JournalPeriodKind.Week, 1))
        assertEquals("2026-10-12", JournalDays.movedPeriodStart("2026-10-19", JournalPeriodKind.Week, -1))
        assertEquals("2027-01-01", JournalDays.movedPeriodStart("2026-12-01", JournalPeriodKind.Month, 1))
        assertEquals("2026-02-01", JournalDays.movedPeriodStart("2026-03-01", JournalPeriodKind.Month, -1))
        assertEquals("2025-01-01", JournalDays.movedPeriodStart("2026-01-01", JournalPeriodKind.Year, -1))
    }

    @Test
    fun aMonthIsRowsOfSevenDaysStartingOnTheWeeksFirstDay() {
        // October 2026 starts on a Thursday and has 31 days.
        val mondayRows = JournalDays.monthRows("2026-10-01", "monday")
        assertEquals(5, mondayRows.size)
        assertTrue(mondayRows.all { row -> row.size == 7 })
        assertEquals(listOf("-", "-", "-", "2026-10-01", "2026-10-02", "2026-10-03", "2026-10-04"), mondayRows[0].map { day -> day ?: "-" })
        assertEquals(
            listOf("2026-10-26", "2026-10-27", "2026-10-28", "2026-10-29", "2026-10-30", "2026-10-31", "-"), mondayRows[4].map { day -> day ?: "-" },
        )
        val sundayRows = JournalDays.monthRows("2026-10-01", "sunday")
        assertEquals(listOf("-", "-", "-", "-", "2026-10-01", "2026-10-02", "2026-10-03"), sundayRows[0].map { day -> day ?: "-" })
        assertEquals(31, mondayRows.flatten().filterNotNull().size)
        assertEquals(emptyList<List<String?>>(), JournalDays.monthRows("nonsense", "monday"))
    }

    private fun summary(day: String): JournalDaySummary = TeaNodeJson.decodeFromString(
        JournalDaySummary.serializer(),
        """{"day":"$day","isToday":false,"mood":null,"weather":null,"headline":"","wordsExcerpt":"","hasWords":false,"facts":null,"coverPhotoId":"","photoIds":null,"hasNarrative":false}""",
    )

    @Test
    fun theTimelineIsGroupedByWeekNewestFirst() {
        val days = listOf("2026-10-25", "2026-10-24", "2026-10-22", "2026-10-18", "2026-10-12").map(::summary)
        val groups = JournalDays.weekGroups(days, "monday")
        assertEquals(listOf("2026-10-19", "2026-10-12"), groups.map { group -> group.weekStart })
        assertEquals(listOf("2026-10-25", "2026-10-24", "2026-10-22"), groups[0].days.map { day -> day.day })
        assertEquals(listOf("2026-10-18", "2026-10-12"), groups[1].days.map { day -> day.day })
        // The same days with weeks that start on Sunday: the 25th starts a week of its own.
        val sundayGroups = JournalDays.weekGroups(days, "sunday")
        assertEquals(listOf("2026-10-25", "2026-10-18", "2026-10-11"), sundayGroups.map { group -> group.weekStart })
        assertEquals(listOf("2026-10-24", "2026-10-22", "2026-10-18"), sundayGroups[1].days.map { day -> day.day })
    }

    @Test
    fun moneyIsCountedInTheCurrencysOwnSmallestUnit() {
        assertEquals(0, BigDecimal("52.1").compareTo(JournalMoney.amount(5210, "USD")))
        assertEquals(0, BigDecimal("-7.8").compareTo(JournalMoney.amount(-780, "usd")))
        assertEquals(0, BigDecimal(1200).compareTo(JournalMoney.amount(1200, "JPY")))
        assertEquals(0, BigDecimal("1.5").compareTo(JournalMoney.amount(1500, "KWD")))
    }

    @Test
    fun theDayIsReadAgainOnlyWhileSomethingIsBeingWritten() {
        assertEquals(40, JournalPolling.attemptLimit)
        assertTrue(JournalPolling.shouldRead(isPending = true, attemptCount = 0))
        assertTrue(JournalPolling.shouldRead(isPending = true, attemptCount = 39))
        assertFalse(JournalPolling.shouldRead(isPending = true, attemptCount = 40))
        assertFalse(JournalPolling.shouldRead(isPending = false, attemptCount = 0))
    }

    @Test
    fun theEveningQuestionTimeIsHoursAndMinutes() {
        assertEquals(21, JournalEveningTime.parts("21:30")?.hour)
        assertEquals(30, JournalEveningTime.parts("21:30")?.minute)
        assertEquals(9, JournalEveningTime.parts("9:05")?.hour)
        assertNull(JournalEveningTime.parts("24:00"))
        assertNull(JournalEveningTime.parts("21:75"))
        assertNull(JournalEveningTime.parts("evening"))
        assertNull(JournalEveningTime.parts("21"))
        assertEquals("09:05", JournalEveningTime.text(9, 5))
        assertEquals(21, JournalEveningTime.parts(JournalEveningTime.DEFAULT_TEXT)?.hour)
    }

    @Test
    fun aMoodLevelIsOneToFive() {
        assertEquals(listOf(1, 2, 3, 4, 5), JournalMoodLevel.entries.map { level -> level.moodLevel })
        assertEquals(JournalMoodLevel.Good, JournalMoodLevel.of(4))
        assertNull(JournalMoodLevel.of(0))
        assertNull(JournalMoodLevel.of(6))
        assertNull(JournalMoodLevel.of(null))
    }
}
