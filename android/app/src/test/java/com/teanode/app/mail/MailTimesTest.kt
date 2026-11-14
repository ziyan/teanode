package com.teanode.app.mail

import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Test
import java.time.Instant
import java.time.ZoneOffset
import java.util.Locale

class MailTimesTest {
    private val now = Instant.parse("2030-03-13T15:00:00Z")
    private val zone = ZoneOffset.UTC
    private val locale = Locale.US

    private fun at(text: String) = Instant.parse(text)

    @Test
    fun aRowFallsInTheStretchOfTimeItHappenedIn() {
        fun period(text: String) = MailTimes.period(at(text), now, zone, locale)
        assertEquals(ListPeriod.Today, period("2030-03-13T01:00:00Z"))
        assertEquals(ListPeriod.Yesterday, period("2030-03-12T23:00:00Z"))
        // 2030-03-13 is a Wednesday; the week starts on Sunday in the United States.
        assertEquals(ListPeriod.ThisWeek, period("2030-03-10T08:00:00Z"))
        assertEquals(ListPeriod.LastWeek, period("2030-03-05T08:00:00Z"))
        assertEquals(ListPeriod.ThisMonth, period("2030-03-01T08:00:00Z"))
        assertEquals(ListPeriod.Month(2030, 2, true), period("2030-02-20T08:00:00Z"))
        assertEquals(ListPeriod.Month(2029, 12, false), period("2029-12-20T08:00:00Z"))
        assertEquals(ListPeriod.Earlier, MailTimes.period(null, now, zone, locale))
    }

    @Test
    fun rowsAreGroupedInTheOrderTheyCome() {
        val rows = listOf("2030-03-13T14:00:00Z", "2030-03-13T09:00:00Z", "2030-03-12T10:00:00Z", "2030-02-01T10:00:00Z", "2030-02-01T09:00:00Z")
        val grouped = MailTimes.periods(rows, now, zone, locale) { text -> at(text) }
        assertEquals(listOf(ListPeriod.Today, ListPeriod.Yesterday, ListPeriod.Month(2030, 2, true)), grouped.map { group -> group.period })
        assertEquals(listOf(2, 1, 2), grouped.map { group -> group.rows.size })
    }

    @Test
    fun aRowSaysItsTimeTodayAndYesterdayItsWeekdayThisWeekAndItsDateBeyond() {
        assertEquals("3:00 PM", MailTimes.label(at("2030-03-13T15:00:00Z"), now, zone, locale).replace(' ', ' '))
        assertEquals("Sunday", MailTimes.label(at("2030-03-10T08:00:00Z"), now, zone, locale))
        assertEquals("", MailTimes.label(null, now, zone, locale))
        assertEquals("2/20/30", MailTimes.label(at("2030-02-20T08:00:00Z"), now, zone, locale))
    }

    @Test
    fun theShortTimeOfAMessageSaysYesterdayInTheCallersWords() {
        assertEquals(ShortTime.Yesterday, MailTimes.short(at("2030-03-12T20:00:00Z"), now, zone, locale))
        assertEquals(ShortTime.Text("Friday"), MailTimes.short(at("2030-03-08T20:00:00Z"), now, zone, locale))
        assertEquals(ShortTime.None, MailTimes.short(null, now, zone, locale))
    }

    @Test
    fun aMonthIsNamedWithItsYearOnlyWhenItIsNotThisOne() {
        assertEquals("February", MailTimes.monthTitle(ListPeriod.Month(2030, 2, true), locale))
        assertEquals("December 2029", MailTimes.monthTitle(ListPeriod.Month(2029, 12, false), locale))
    }

    @Test
    fun aSizeIsWrittenAsAPersonReadsIt() {
        assertEquals("512 B", fileSize(512))
        assertEquals("48.2 KB", fileSize(48_213))
        assertEquals("1.5 MB", fileSize(1_500_000))
    }
}
