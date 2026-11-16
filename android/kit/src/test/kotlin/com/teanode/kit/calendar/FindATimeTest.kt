package com.teanode.kit.calendar

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test
import com.teanode.kit.TeaNodeJson
import java.time.Instant
import java.time.ZoneId
import java.time.ZoneOffset
import java.time.ZonedDateTime

class FindATimeTest {
    private val zone: ZoneId = ZoneOffset.UTC

    private fun moment(day: Int, hour: Int, minute: Int = 0): Instant = ZonedDateTime.of(2030, 1, day, hour, minute, 0, 0, zone).toInstant()

    private fun period(day: Int, startHour: Int, startMinute: Int = 0, endHour: Int, endMinute: Int = 0) =
        CalendarFreeBusy.Period(CalendarDates.text(moment(day, startHour, startMinute)), CalendarDates.text(moment(day, endHour, endMinute)))

    /** The command line's free-together test and the dashboard's expect the same stretches of the same busy times, counted in one zone. */
    @Test
    fun whatNoneOfThemIsBusyInIsFreeWithOverlapsMadeOne() {
        val free = FreeTime.freeByDay(
            listOf(period(2, 10, endHour = 11), period(2, 10, 30, 12), period(2, 15, endHour = 16)),
            moment(2, 0), moment(3, 0), 9 * 60, 17 * 60, zone,
        )
        assertEquals(
            listOf(listOf(9, 10), listOf(12, 15), listOf(16, 17)),
            free.map { stretch -> listOf(stretch.startsAt.atZone(zone).hour, stretch.endsAt.atZone(zone).hour) },
        )
    }

    @Test
    fun eachDayIsOfferedOnItsOwnAndABusyDayOffersNothing() {
        val both = FreeTime.freeByDay(emptyList(), moment(2, 14), moment(4, 0), 9 * 60, 17 * 60, zone)
        assertEquals(listOf(2, 3), both.map { stretch -> stretch.startsAt.atZone(zone).dayOfMonth })
        val taken = FreeTime.freeByDay(listOf(period(2, 8, endHour = 18)), moment(2, 0), moment(3, 0), 9 * 60, 17 * 60, zone)
        assertEquals(emptyList<FreeStretch>(), taken)
    }

    @Test
    fun aQuestionWhoseLastDayIsBeforeItsFirstOrWhoseDayEndsBeforeItBeginsHasAProblem() {
        var question = FreeTimeQuestion(moment(5, 0), moment(2, 0))
        assertEquals(FreeTimeQuestion.Problem.LastDayBeforeFirst, question.problem)
        question = FreeTimeQuestion(moment(2, 0), moment(5, 0))
        assertNull(question.problem)
        question = question.copy(latestMinutes = question.earliestMinutes)
        assertEquals(FreeTimeQuestion.Problem.DayEndsBeforeItBegins, question.problem)
    }

    private val ben = TeaNodeJson.decodeFromString(CalendarSharePerson.serializer(), """{"userId":"ben-id","username":"ben","name":"Ben Example"}""")

    @Test
    fun onlyThePeopleWhoSharedAreOfferedAndSomeoneLeftOutIsSaid() = runBlocking {
        val (connection, _) = fakeConnection { body ->
            if (body.contains("ListCirclePeople")) {
                """{"data":{"ListCirclePeople":[{"userId":"ben-id","username":"ben","name":"Ben Example"},{"userId":"cora-id","username":"cora","name":null}]}}"""
            } else {
                """{"data":{"ListCalendarFreeBusy":[{"userId":"ben-id","username":"ben","name":"Ben Example","isShared":true,"periods":[]},{"userId":"cora-id","username":"","name":"","isShared":false,"periods":[]}]}}"""
            }
        }
        val sharing = connection.peopleSharingTheirCalendar()
        assertEquals(listOf("ben-id"), sharing.people.map { person -> person.userId })
        assertTrue(sharing.isAnyoneLeftOut)
    }

    @Test
    fun thePersonAndThoseChosenAreAskedAboutAndTheirFreeTimeIsOffered() = runBlocking {
        val (connection, fake) = fakeConnection {
            """{"data":{"ListCalendarFreeBusy":[{"userId":"me-id","username":"me","name":"","isShared":true,"periods":[]},{"userId":"ben-id","username":"ben","name":"","isShared":true,"periods":[]}]}}"""
        }
        val question = FreeTimeQuestion(moment(2, 0), moment(2, 0))
        val answer = connection.freeTime("me-id", listOf(ben), question, zone)
        assertTrue(answer is FreeTimeAnswer.Free, answer.toString())
        assertEquals(1, (answer as FreeTimeAnswer.Free).stretches.size)
        val sent = fake.sent.last().body!!.toString(Charsets.UTF_8)
        assertTrue(sent.contains("me-id") && sent.contains("ben-id"), "the person is asked about too: $sent")
    }

    @Test
    fun someoneWhoStoppedSharingIsSaidAndNoTimeIsOffered() = runBlocking {
        val (connection, _) = fakeConnection {
            """{"data":{"ListCalendarFreeBusy":[{"userId":"me-id","username":"me","name":"","isShared":true,"periods":[]},{"userId":"ben-id","username":"","name":"","isShared":false,"periods":[]}]}}"""
        }
        val answer = connection.freeTime("me-id", listOf(ben), FreeTimeQuestion(moment(2, 0), moment(2, 0)), zone)
        assertEquals(FreeTimeAnswer.NotShared(listOf("ben")), answer)
    }

    @Test
    fun moreThanOneQuestionHoldsIsAskedInChunks() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"ListCalendarFreeBusy":[]}}""" }
        connection.calendarFreeBusy((1..25).map { number -> "user-$number" }, moment(2, 0), moment(3, 0))
        assertEquals(2, fake.sent.size)
        assertEquals(19, ((sentVariables(fake)["userIds"]) as JsonArray).size)
        assertEquals(JsonPrimitive("user-1"), (sentVariables(fake)["userIds"] as JsonArray)[0])
    }
}
