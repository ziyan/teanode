package com.teanode.kit.goals

import com.teanode.kit.TeaNodeJson
import com.teanode.kit.calendar.fakeConnection
import com.teanode.kit.calendar.sentVariables
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
import org.junit.jupiter.api.Assertions.assertNull
import org.junit.jupiter.api.Assertions.assertTrue
import org.junit.jupiter.api.Test

class GoalsTest {
    private fun goal(state: String): AgentGoal = TeaNodeJson.decodeFromString(
        AgentGoal.serializer(),
        """{"conversationId":"c","goalTitle":"T","goalDescription":"D","goalState":"$state","goalStatus":"","goalSetAt":null,"goalNextAt":null,"lastAt":null}""",
    )

    @Test
    fun aGoalIsOpenWhileWorkingOrWaiting() {
        assertTrue(goal("working").isOpen)
        assertTrue(goal("waiting").isOpen)
        assertFalse(goal("paused").isOpen)
        assertTrue(goal("paused").isPaused)
        assertFalse(goal("met").isOpen)
        assertFalse(goal("dropped").isOpen)
    }

    @Test
    fun aGoalCarriesItsTimesAndMailConditionsAndIsAtItsOwnPaceWithNeither() {
        val goal = TeaNodeJson.decodeFromString(
            AgentGoal.serializer(),
            """{"conversationId":"c","goalTitle":"T","goalDescription":"D","goalState":"working","goalStatus":"","goalTimes":["30 7 * * 1-5","@at 2030-12-11 09:00"],
                "goalMailConditions":[{"field":"from","header":null,"operator":"contains","value":"a@example.com"}],"goalWrittenBy":"agent","isGoalNotifiedWhenPosted":true,
                "goalSetAt":null,"goalNextAt":null,"goalSurfacedAt":null,"lastAt":null,
                "goalOriginTitle":null,"activity":[null,{"id":"a","createdAt":"2026-10-01T10:00:00Z","goalActivityKind":"note","activityHeadline":"H","activityDetail":""}]}""",
        )
        assertEquals(listOf("30 7 * * 1-5", "@at 2030-12-11 09:00"), goal.goalTimes)
        assertTrue(goal.isTriggeredByMail)
        assertFalse(goal.isAtItsOwnPace)
        assertEquals("contains", goal.goalMailConditions[0].comparisonOperator)
        assertTrue(goal.isGoalNotifiedWhenPosted)
        assertEquals(1, goal.activity.orEmpty().filterNotNull().size)
        assertTrue(goal("working").isAtItsOwnPace)
    }

    @Test
    fun theListLeavesOutTheNullsAGoSliceMayHold() = runBlocking {
        val (connection, _) = fakeConnection {
            """{"data":{"ListAgentGoals":[null,{"conversationId":"c","goalTitle":"T","goalDescription":"D","goalState":"working","goalStatus":"","goalTimes":[],"goalMailConditions":[],"goalSetAt":null,"goalNextAt":null,"lastAt":null}]}}"""
        }
        assertEquals(listOf("c"), connection.goals().map { goal -> goal.conversationId })
    }

    @Test
    fun startingAGoalSendsItsTitleDescriptionTimesAndMailConditionsAndAnswersItsConversation() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"StartAgentGoal":{"conversationId":"goal-1","goalTitle":"Title","goalDescription":"Details","goalState":"working","goalStatus":""}}}""" }
        val conversationId = connection.startGoal(
            GoalChange(
                goalTitle = "Title", goalDescription = "Details", goalTimes = listOf("30 7 * * 1-5"),
                goalMailConditions = listOf(GoalMailCondition("from", value = "news@school.example")), isGoalNotifiedWhenPosted = true,
            ),
        )
        assertEquals("goal-1", conversationId)
        assertEquals(JsonPrimitive("Title"), sentVariables(fake)["goalTitle"])
        assertEquals(JsonPrimitive("Details"), sentVariables(fake)["goalDescription"])
        assertEquals(JsonArray(listOf(JsonPrimitive("30 7 * * 1-5"))), sentVariables(fake)["goalTimes"])
        assertEquals(JsonPrimitive(true), sentVariables(fake)["isGoalNotifiedWhenPosted"])
        assertTrue(sentVariables(fake)["goalMailConditions"].toString().contains("news@school.example"))
    }

    @Test
    fun updatingAGoalSendsOnlyWhatChangesAndAnEmptyListTakesTimesAway() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"UpdateAgentGoal":{"conversationId":"c","goalTitle":"T","goalDescription":"D","goalState":"working","goalStatus":""}}}""" }
        connection.updateGoal("c", GoalChange(goalTimes = emptyList()))
        val variables = sentVariables(fake)
        assertEquals(JsonPrimitive("c"), variables["conversationId"])
        assertEquals(JsonArray(emptyList()), variables["goalTimes"])
        assertNull(variables["goalTitle"])
        assertNull(variables["goalMailConditions"])
    }

    @Test
    fun aGoalIsToldRunAndItsStateIsSet() = runBlocking {
        val (connection, fake) = fakeConnection {
            """{"data":{"TellAgentGoal":{"conversationId":"c","goalTitle":"T","goalDescription":"D","goalState":"working","goalStatus":""},
                "RunAgentGoal":{"conversationId":"c","goalTitle":"T","goalDescription":"D","goalState":"working","goalStatus":""},
                "SetAgentGoalState":{"conversationId":"c","goalTitle":"T","goalDescription":"D","goalState":"paused","goalStatus":""}}}"""
        }
        connection.tellGoal("c", "Keep going")
        connection.runGoal("c")
        connection.setGoalState("c", "paused")
        assertEquals(JsonPrimitive("Keep going"), sentVariables(fake, 0)["text"])
        assertEquals(JsonPrimitive("c"), sentVariables(fake, 1)["conversationId"])
        assertEquals(JsonPrimitive("paused"), sentVariables(fake, 2)["goalState"])
    }

    @Test
    fun theBriefIsSetAtATimeOnSomeDaysAndAnswersTheGoal() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"SetAgentBrief":{"conversationId":"b","goalTitle":"Daily brief","goalDescription":"","goalState":"working","goalStatus":"","goalTimes":["30 7 * * 1,2"],"goalWrittenBy":"built_in"}}}""" }
        val brief = connection.setDailyBrief(true, "07:30", listOf(1, 2))
        assertEquals("b", brief!!.conversationId)
        assertEquals(JsonPrimitive("07:30"), sentVariables(fake)["at"])
        assertEquals(JsonArray(listOf(JsonPrimitive(1), JsonPrimitive(2))), sentVariables(fake)["days"])
    }

    @Test
    fun theDetailAsksForActivityAndTheTimes() {
        assertTrue(GoalDocuments.GOAL.contains("activity { id createdAt goalActivityKind activityHeadline activityDetail }"))
        assertTrue(GoalDocuments.GOAL.contains("goalTimes goalMailConditions { field header operator value }"))
        assertFalse(GoalDocuments.GOAL.contains("schedules"))
    }
}
