package com.teanode.kit.goals

import com.teanode.kit.TeaNodeJson
import com.teanode.kit.calendar.fakeConnection
import com.teanode.kit.calendar.sentVariables
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonPrimitive
import org.junit.jupiter.api.Assertions.assertEquals
import org.junit.jupiter.api.Assertions.assertFalse
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
        assertFalse(goal("met").isOpen)
        assertFalse(goal("dropped").isOpen)
    }

    @Test
    fun aScheduleThatRunsOnMailHasItsConditionsAndNoClock() {
        val goal = TeaNodeJson.decodeFromString(
            AgentGoal.serializer(),
            """{"conversationId":"c","goalTitle":"T","goalDescription":"D","goalState":"working","goalStatus":"","goalSetAt":null,"goalNextAt":null,"lastAt":null,
                "goalOriginTitle":null,"activity":[null,{"id":"a","createdAt":"2026-10-01T10:00:00Z","goalActivityKind":"note","activityHeadline":"H","activityDetail":""}],
                "schedules":[{"id":"s1","name":"Mail","cron":"","enabled":true,"nextRunAt":null,"mailConditions":[{"field":"from","header":null,"operator":"contains","value":"a@example.com"}]},
                             {"id":"s2","name":"Clock","cron":"0 8 * * *","enabled":true,"nextRunAt":null,"mailConditions":null},null]}""",
        )
        assertEquals(listOf(true, false), goal.schedules.orEmpty().filterNotNull().map { schedule -> schedule.isTriggeredByMail })
        assertEquals("contains", goal.schedules!![0]!!.mailConditions!![0].comparisonOperator)
        assertEquals(1, goal.activity.orEmpty().filterNotNull().size)
    }

    @Test
    fun theListLeavesOutTheNullsAGoSliceMayHold() = runBlocking {
        val (connection, _) = fakeConnection {
            """{"data":{"ListAgentGoals":[null,{"conversationId":"c","goalTitle":"T","goalDescription":"D","goalState":"working","goalStatus":"","goalSetAt":null,"goalNextAt":null,"lastAt":null}]}}"""
        }
        assertEquals(listOf("c"), connection.goals().map { goal -> goal.conversationId })
    }

    @Test
    fun startingAGoalSendsItsTitleAndDescriptionAndAnswersItsConversation() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"StartAgentGoal":{"conversationId":"goal-1"}}}""" }
        assertEquals("goal-1", connection.startGoal("Title", "Details"))
        assertEquals(JsonPrimitive("Title"), sentVariables(fake)["goalTitle"])
        assertEquals(JsonPrimitive("Details"), sentVariables(fake)["goalDescription"])
    }

    @Test
    fun aGoalIsToldAndItsStateIsSet() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"TellAgentGoal":{"conversationId":"c"},"SetAgentGoalState":{"conversationId":"c"}}}""" }
        connection.tellGoal("c", "Keep going")
        connection.setGoalState("c", "met")
        assertEquals(JsonPrimitive("Keep going"), sentVariables(fake, 0)["text"])
        assertEquals(JsonPrimitive("met"), sentVariables(fake, 1)["goalState"])
    }

    @Test
    fun aSchedulePassesToAGoalByItsIds() = runBlocking {
        val (connection, fake) = fakeConnection { """{"data":{"MoveAgentScheduleToGoal":{"id":"s"}}}""" }
        connection.moveScheduleToGoal("s", "c")
        assertEquals(JsonPrimitive("s"), sentVariables(fake)["scheduleId"])
        assertEquals(JsonPrimitive("c"), sentVariables(fake)["conversationId"])
    }

    @Test
    fun theDetailAsksForActivityAndSchedules() {
        assertTrue(GoalDocuments.GOAL.contains("activity { id createdAt goalActivityKind activityHeadline activityDetail }"))
        assertTrue(GoalDocuments.GOAL.contains("mailConditions { field header operator value }"))
    }
}
