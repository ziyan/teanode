package com.teanode.app.goals

import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.graphQLField
import kotlinx.serialization.json.JsonPrimitive
import java.time.DayOfWeek
import java.time.Instant
import java.time.LocalDate
import java.time.ZonedDateTime

private fun hoursFromNow(hours: Long): String = Instant.now().plusSeconds(hours * 3600).toString()

/** The next time at [hour]:[minute] on one of [days], in this device's zone, as the server writes a time. */
private fun nextTime(hour: Int, minute: Int, days: Set<DayOfWeek>): String {
    var candidate = ZonedDateTime.now().withHour(hour).withMinute(minute).withSecond(0).withNano(0)
    while (!candidate.isAfter(ZonedDateTime.now()) || candidate.dayOfWeek !in days) {
        candidate = candidate.plusDays(1)
    }
    return candidate.toInstant().toString()
}

private val weekdays = setOf(DayOfWeek.MONDAY, DayOfWeek.TUESDAY, DayOfWeek.WEDNESDAY, DayOfWeek.THURSDAY, DayOfWeek.FRIDAY)

/** One goal as the server answers it; [times] are the lines of goalTimes and [mail] the JSON of its mail conditions. */
private fun goalEntry(
    conversationId: String, title: String, description: String, state: String, status: String, nextAt: String?,
    times: List<String> = emptyList(), mail: String = "[]", writtenBy: String = "person", isNotified: Boolean = false,
): String =
    """{"conversationId":"$conversationId","goalTitle":"$title","goalDescription":"$description","goalState":"$state","goalStatus":"$status",""" +
        """"goalTimes":[${times.joinToString(",") { line -> "\"$line\"" }}],"goalMailConditions":$mail,"goalWrittenBy":"$writtenBy","isGoalNotifiedWhenPosted":$isNotified,""" +
        """"goalSetAt":"${hoursFromNow(-72)}","goalNextAt":${nextAt?.let { at -> "\"$at\"" } ?: "null"},"goalSurfacedAt":null,"lastAt":"${hoursFromNow(-3)}"}"""

private val goalEntries: Map<String, String> = linkedMapOf(
    "goal-waiting" to goalEntry(
        "goal-waiting", "Plan the garden for spring", "Draw up a planting plan for the back garden: what goes where, and what to order.",
        "waiting", "Waiting for you: which beds get sun in the afternoon?", null,
    ),
    // On set times: every Monday and one moment.
    "goal-working" to goalEntry(
        "goal-working", "Compare three standing desks", "Read the reviews and tell me which one to buy, with a price under 500.",
        "working", "Reading the third set of reviews", nextTime(9, 0, setOf(DayOfWeek.MONDAY)),
        times = listOf("0 9 * * 1", "@at ${LocalDate.now().plusDays(20)} 09:00"), isNotified = true,
    ),
    // Waiting for mail: no times, a condition on the sender.
    "goal-mail" to goalEntry(
        "goal-mail", "Keep track of the school newsletter", "Note every date and form in the newsletter and add the dates to my calendar.",
        "working", "Nothing new since the last newsletter", null,
        mail = """[{"field":"from","header":null,"operator":"contains","value":"news@school.example"}]""",
    ),
    "goal-brief" to goalEntry(
        "goal-brief", "Daily brief", "What the day holds and what is waiting for you.",
        "working", "Sent this morning", nextTime(7, 30, weekdays), times = listOf("30 7 * * 1-5"), writtenBy = "built_in",
    ),
    "goal-paused" to goalEntry(
        "goal-paused", "Watch the price of the desk lamp", "Tell me when the lamp I liked drops below 40.",
        "paused", "Paused until you resume it", null, times = listOf("0 18 * * 5"),
    ),
    "goal-done" to goalEntry(
        "goal-done", "Book the dentist", "Find a time next month that does not clash with my calendar.",
        "met", "Booked for the 14th", null,
    ),
)

private fun detailOf(conversationId: String): String {
    val entry = goalEntries[conversationId] ?: goalEntries.getValue("goal-working")
    return entry.removeSuffix("}") +
        ""","goalOriginTitle":null,"activity":[
            {"id":"a1","createdAt":"${hoursFromNow(-30)}","goalActivityKind":"note","activityHeadline":"Read two sets of reviews","activityDetail":"The first desk has a wobble at full height; the second is quieter but costs more."},
            {"id":"a2","createdAt":"${hoursFromNow(-6)}","goalActivityKind":"note","activityHeadline":"Checked the prices","activityDetail":""}]}"""
}

/**
 * What demo mode answers for Goals: a goal waiting for the person, one on set times, one that runs when mail arrives,
 * the daily brief, a paused one and one met, each with what it did.
 */
val goalsDemoAnswerers: List<DemoAnswerer> = listOf(
    graphQLField("ListAgentGoals", "[" + goalEntries.values.joinToString(",") + "]"),
    graphQLField("GetAgentGoal") { variables -> detailOf((variables["conversationId"] as? JsonPrimitive)?.content.orEmpty()) },
    graphQLField("StartAgentGoal", goalEntries.getValue("goal-working")),
    graphQLField("UpdateAgentGoal", goalEntries.getValue("goal-working")),
    graphQLField("RunAgentGoal", goalEntries.getValue("goal-working")),
    graphQLField("TellAgentGoal", goalEntries.getValue("goal-working")),
    graphQLField("SetAgentGoalState", goalEntries.getValue("goal-working")),
)
