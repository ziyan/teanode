package com.teanode.app.goals

import androidx.test.core.app.ApplicationProvider
import com.teanode.kit.goals.GoalMailCondition
import java.util.Locale
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class GoalWordsTest {
    private val resources = ApplicationProvider.getApplicationContext<android.content.Context>().resources

    private fun spaced(text: String): String = text.replace(' ', ' ').replace(' ', ' ')

    private fun times(vararg lines: String, locale: Locale = Locale.US): String? = GoalWords.timesText(resources, lines.toList(), locale)?.let { text -> spaced(text) }

    @Test
    fun aWeeklyTimeIsDescribedInAPersonsWords() {
        assertEquals("Weekdays at 7:30 AM", times("30 7 * * 1-5"))
        assertEquals("Every day at 9:00 AM", times("0 9 * * *"))
        assertEquals("Weekends at 10:00 AM", times("0 10 * * 6,0"))
        assertEquals("Fridays at 5:00 PM", times("0 17 * * 5"))
        assertEquals("Every Mon, Wed, and Fri at 9:00 AM", times("0 9 * * 1,3,5"))
        assertEquals("Weekdays at 17:00", times("0 17 * * 1-5", locale = Locale.UK))
    }

    @Test
    fun severalTimesAreJoinedAndAMomentSaysItsDay() {
        assertEquals("Mondays at 9:00 AM and on Dec 11, 2030 at 9:00 AM", times("0 9 * * 1", "@at 2030-12-11 09:00"))
        assertEquals("Mondays at 9:00 AM, fridays at 5:00 PM, and on Dec 11, 2030 at 9:00 AM", times("0 9 * * 1", "0 17 * * 5", "@at 2030-12-11 09:00"))
    }

    @Test
    fun noTimesIsNoWordsAndALineOfAnyOtherKindIsLeftAsWritten() {
        assertEquals(null, times())
        assertEquals("*/15 * * * *", times("*/15 * * * *"))
        assertEquals("0 9 1 * *", times("0 9 1 * *"))
    }

    @Test
    fun aGoalThatTakesTurnsOnMailNamesItsConditions() {
        assertEquals("When any mail arrives", GoalWords.mailText(resources, listOf(GoalMailCondition("any", value = null))))
        assertEquals(
            "When mail arrives: From contains news@school.example, Header List-Id is lists.example.com",
            GoalWords.mailText(resources, listOf(GoalMailCondition("from", value = "news@school.example"), GoalMailCondition("header", header = "List-Id", comparisonOperator = "equals", value = "lists.example.com"))),
        )
    }
}
