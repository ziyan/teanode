package com.teanode.app.goals

import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.graphQLField
import kotlinx.serialization.json.JsonPrimitive
import java.time.Instant

private fun hoursFromNow(hours: Long): String = Instant.now().plusSeconds(hours * 3600).toString()

private fun goalFields(conversationId: String, title: String, description: String, state: String, status: String, nextHours: Long?): String =
    """"conversationId":"$conversationId","goalTitle":"$title","goalDescription":"$description","goalState":"$state","goalStatus":"$status",""" +
        """"goalSetAt":"${hoursFromNow(-72)}","goalNextAt":${nextHours?.let { hours -> "\"${hoursFromNow(hours)}\"" } ?: "null"},"lastAt":"${hoursFromNow(-3)}""""

private fun goalListEntry(conversationId: String, title: String, description: String, state: String, status: String, nextHours: Long?): String =
    "{" + goalFields(conversationId, title, description, state, status, nextHours) + "}"

private val goalEntries: Map<String, String> = mapOf(
    "goal-waiting" to goalListEntry(
        "goal-waiting", "Plan the garden for spring", "Draw up a planting plan for the back garden: what goes where, and what to order.",
        "waiting", "Waiting for you: which beds get sun in the afternoon?", 20,
    ),
    "goal-working" to goalListEntry(
        "goal-working", "Compare three standing desks", "Read the reviews and tell me which one to buy, with a price under 500.",
        "working", "Reading the third set of reviews", 5,
    ),
    "goal-done" to goalListEntry(
        "goal-done", "Book the dentist", "Find a time next month that does not clash with my calendar.",
        "met", "Booked for the 14th", null,
    ),
)

private fun detailOf(conversationId: String): String {
    val entry = goalEntries[conversationId] ?: goalEntries.getValue("goal-working")
    val withoutClose = entry.removeSuffix("}")
    return withoutClose +
        ""","goalOriginTitle":null,"activity":[
            {"id":"a1","createdAt":"${hoursFromNow(-30)}","goalActivityKind":"note","activityHeadline":"Read two sets of reviews","activityDetail":"The first desk has a wobble at full height; the second is quieter but costs more."},
            {"id":"a2","createdAt":"${hoursFromNow(-6)}","goalActivityKind":"note","activityHeadline":"Checked the prices","activityDetail":""}],
          "schedules":[
            {"id":"s1","name":"Morning check","cron":"0 8 * * 1-5","enabled":true,"nextRunAt":"${hoursFromNow(14)}","mailConditions":null},
            {"id":"s2","name":"When the shop writes","cron":"","enabled":true,"nextRunAt":null,"mailConditions":[{"field":"from","header":null,"operator":"contains","value":"shop@example.com"}]},
            {"id":"s3","name":"Weekly summary","cron":"0 18 * * 5","enabled":false,"nextRunAt":null,"mailConditions":null}]}"""
}

/**
 * What demo mode answers for Goals: a goal waiting for the person, one being worked on and one met, each with what it
 * did and its schedules.
 */
val goalsDemoAnswerers: List<DemoAnswerer> = listOf(
    graphQLField("ListAgentGoals", "[" + goalEntries.values.joinToString(",") + "]"),
    graphQLField("GetAgentGoal") { variables -> detailOf((variables["conversationId"] as? JsonPrimitive)?.content.orEmpty()) },
    graphQLField("StartAgentGoal", """{"conversationId":"goal-new"}"""),
    graphQLField("TellAgentGoal", """{"conversationId":"goal-working"}"""),
    graphQLField("SetAgentGoalState", """{"conversationId":"goal-working"}"""),
    graphQLField("MoveAgentScheduleToGoal", """{"id":"schedule-1"}"""),
    graphQLField("ListAgentSchedules") {
        """[
            {"id":"schedule-1","name":"Weekly review","cron":"0 17 * * 5","prompt":"","deliver":"drawer","enabled":true,"lastRunAt":null,"nextRunAt":"${hoursFromNow(40)}","conversationId":null,"mailConditions":null},
            {"id":"schedule-2","name":"Garden watering","cron":"0 7 * * *","prompt":"","deliver":"drawer","enabled":true,"lastRunAt":null,"nextRunAt":"${hoursFromNow(9)}","conversationId":"goal-working","mailConditions":null},
            {"id":"schedule-3","name":"Bills","cron":"0 9 1 * *","prompt":"","deliver":"mail","enabled":false,"lastRunAt":null,"nextRunAt":null,"conversationId":null,"mailConditions":null}]"""
    },
)

