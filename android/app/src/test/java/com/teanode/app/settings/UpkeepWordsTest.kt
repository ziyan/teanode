package com.teanode.app.settings

import androidx.test.core.app.ApplicationProvider
import com.teanode.kit.agent.ScheduleMailCondition
import java.util.Locale
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class UpkeepWordsTest {
    private val resources = ApplicationProvider.getApplicationContext<android.content.Context>().resources

    private fun spaced(text: String): String = text.replace(' ', ' ').replace(' ', ' ')

    private fun timing(cron: String, locale: Locale = Locale.US): String? = UpkeepWords.scheduleTimingText(resources, cron, locale)?.let { text -> spaced(text) }

    @Test
    fun aScheduleIsDescribedInAPersonsWords() {
        assertEquals("Weekdays at 7:30 AM", timing("30 7 * * 1-5"))
        assertEquals("Every day at 9:00 AM", timing("0 9 * * *"))
        assertEquals("Weekends at 10:00 AM", timing("0 10 * * 6,0"))
        assertEquals("Fridays at 5:00 PM", timing("0 17 * * 5"))
        assertEquals("Every Mon, Wed, and Fri at 9:00 AM", timing("0 9 * * 1,3,5"))
        assertEquals("Weekdays at 17:00", timing("0 17 * * 1-5", Locale.UK))
    }

    @Test
    fun aLineOfAnyOtherKindIsLeftAsWritten() {
        for (cron in listOf("*/15 * * * *", "0 9 1 * *", "@in 20m", "@at 2030-01-02 09:00", "")) assertNull(cron, timing(cron))
    }

    @Test
    fun aScheduleThatRunsOnMailNamesItsConditions() {
        assertEquals("When any mail arrives", UpkeepWords.scheduleMailText(resources, listOf(ScheduleMailCondition("any", value = null))))
        assertEquals(
            "When mail arrives: From contains news@example.com, A header List-Id is lists.example.com",
            UpkeepWords.scheduleMailText(resources, listOf(ScheduleMailCondition("from", value = "news@example.com"), ScheduleMailCondition("header", header = "List-Id", comparisonOperator = "equals", value = "lists.example.com"))),
        )
    }

    @Test
    fun theMemoryCheckUsesTheReadersWords() {
        assertEquals("Confirmed", UpkeepWords.questionState(resources, "confirmed"))
        assertEquals("Dropped", UpkeepWords.questionState(resources, "dropped"))
        assertEquals("Should not know", UpkeepWords.questionKind(resources, "abstain"))
        assertEquals("Out of date", UpkeepWords.verdict(resources, "stale"))
        assertEquals("a-new-state", UpkeepWords.questionState(resources, "a-new-state"))
    }
}
