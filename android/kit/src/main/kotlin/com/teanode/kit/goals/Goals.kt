package com.teanode.kit.goals

import com.teanode.kit.Connection
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonPrimitive

/** The goals' GraphQL: the same text as the iPhone app's (ios/TeaNodeKit Goals.swift). */
object GoalDocuments {
    private const val GOAL_FIELDS = "conversationId goalTitle goalDescription goalState goalStatus goalSetAt goalNextAt lastAt"

    const val GOALS = """query { ListAgentGoals { $GOAL_FIELDS } }"""
    const val GOAL = """query (${'$'}conversationId: String!) {
  GetAgentGoal(conversationId: ${'$'}conversationId) {
    $GOAL_FIELDS
    goalOriginTitle
    activity { id createdAt goalActivityKind activityHeadline activityDetail }
    schedules { id name cron enabled nextRunAt mailConditions { field header operator value } }
  }
}"""
    const val START_GOAL = """mutation (${'$'}goalTitle: String!, ${'$'}goalDescription: String!) {
  StartAgentGoal(goalTitle: ${'$'}goalTitle, goalDescription: ${'$'}goalDescription) { conversationId }
}"""
    const val TELL_GOAL = """mutation (${'$'}conversationId: String!, ${'$'}text: String!) {
  TellAgentGoal(conversationId: ${'$'}conversationId, text: ${'$'}text) { conversationId }
}"""
    const val MOVE_SCHEDULE_TO_GOAL = """mutation (${'$'}scheduleId: String!, ${'$'}conversationId: String!) {
  MoveAgentScheduleToGoal(scheduleId: ${'$'}scheduleId, conversationId: ${'$'}conversationId) { id }
}"""
    /** The agent's schedules, which a goal can take over (ios/TeaNodeKit AgentUpkeep.swift). */
    const val AGENT_SCHEDULES =
        """query { ListAgentSchedules { id name cron prompt deliver enabled lastRunAt nextRunAt conversationId mailConditions { field header operator value } } }"""
    const val SET_GOAL_STATE = """mutation (${'$'}conversationId: String!, ${'$'}goalState: String!) {
  SetAgentGoalState(conversationId: ${'$'}conversationId, goalState: ${'$'}goalState) { conversationId }
}"""
}

/** One test of a mail message that makes a schedule run when mail arrives. */
@Serializable
data class GoalMailCondition(
    val field: String,
    val header: String? = null,
    @SerialName("operator") val comparisonOperator: String = "contains",
    val value: String? = null,
)

/** AgentSchedule is one of the agent's schedules, as far as choosing one for a goal needs it; the Settings part holds the full form. */
@Serializable
data class AgentSchedule(
    val id: String,
    val name: String,
    val cron: String,
    val prompt: String = "",
    /** drawer, a turn in the conversation, or mail. */
    val deliver: String = "drawer",
    val enabled: Boolean = true,
    val lastRunAt: String? = null,
    val nextRunAt: String? = null,
    /** The conversation its answers go to: a goal's, for a schedule that runs a goal. */
    val conversationId: String? = null,
    val mailConditions: List<GoalMailCondition>? = null,
)

/** AgentGoal is something the agent works toward on its own, in a conversation of its own. */
@Serializable
data class AgentGoal(
    val conversationId: String,
    val goalTitle: String,
    val goalDescription: String,
    /** working, waiting, met or dropped. */
    val goalState: String,
    val goalStatus: String,
    val goalSetAt: String? = null,
    val goalNextAt: String? = null,
    val lastAt: String? = null,
    val goalOriginTitle: String? = null,
    val activity: List<Activity?>? = null,
    val schedules: List<Schedule?>? = null,
) {
    @Serializable
    data class Activity(
        val id: String,
        val createdAt: String,
        val goalActivityKind: String,
        val activityHeadline: String,
        val activityDetail: String,
    )

    @Serializable
    data class Schedule(
        val id: String,
        val name: String,
        val cron: String,
        val enabled: Boolean,
        val nextRunAt: String? = null,
        /** What a message must match for it to run, for a schedule that runs when mail arrives; its cron is then empty. */
        val mailConditions: List<GoalMailCondition>? = null,
    ) {
        /** Whether it runs when mail arrives, not at set times. */
        val isTriggeredByMail: Boolean get() = !mailConditions.isNullOrEmpty()
    }

    val id: String get() = conversationId

    val isOpen: Boolean get() = goalState == "working" || goalState == "waiting"
}

@Serializable
private class GoalsAnswer(val ListAgentGoals: List<AgentGoal?>)

@Serializable
private class GoalAnswer(val GetAgentGoal: AgentGoal? = null)

@Serializable
private class StartedGoal(val conversationId: String)

@Serializable
private class StartGoalAnswer(val StartAgentGoal: StartedGoal)

@Serializable
private class TellGoalAnswer(val TellAgentGoal: StartedGoal? = null)

@Serializable
private class SetGoalStateAnswer(val SetAgentGoalState: StartedGoal? = null)

@Serializable
private class SchedulesAnswer(val ListAgentSchedules: List<AgentSchedule?>)

@Serializable
private class MovedSchedule(val id: String)

@Serializable
private class MoveScheduleAnswer(val MoveAgentScheduleToGoal: MovedSchedule? = null)

suspend fun Connection.goals(): List<AgentGoal> = execute<GoalsAnswer>(GoalDocuments.GOALS).ListAgentGoals.filterNotNull()

suspend fun Connection.goal(conversationId: String): AgentGoal? =
    execute<GoalAnswer>(GoalDocuments.GOAL, mapOf("conversationId" to JsonPrimitive(conversationId))).GetAgentGoal

/** Starts a goal; returns its conversation. */
suspend fun Connection.startGoal(title: String, description: String): String =
    execute<StartGoalAnswer>(
        GoalDocuments.START_GOAL, mapOf("goalTitle" to JsonPrimitive(title), "goalDescription" to JsonPrimitive(description)),
    ).StartAgentGoal.conversationId

suspend fun Connection.tellGoal(conversationId: String, text: String) {
    execute<TellGoalAnswer>(GoalDocuments.TELL_GOAL, mapOf("conversationId" to JsonPrimitive(conversationId), "text" to JsonPrimitive(text)))
}

/** Marks a goal met or dropped, or opens it again as working. */
suspend fun Connection.setGoalState(conversationId: String, goalState: String) {
    execute<SetGoalStateAnswer>(
        GoalDocuments.SET_GOAL_STATE, mapOf("conversationId" to JsonPrimitive(conversationId), "goalState" to JsonPrimitive(goalState)),
    )
}

/**
 * Makes a schedule one of a goal's: its runs become the goal's turns, answered in the goal's conversation rather than
 * by mail, and the goal takes none of its own.
 */
suspend fun Connection.moveScheduleToGoal(scheduleId: String, conversationId: String) {
    execute<MoveScheduleAnswer>(
        GoalDocuments.MOVE_SCHEDULE_TO_GOAL, mapOf("scheduleId" to JsonPrimitive(scheduleId), "conversationId" to JsonPrimitive(conversationId)),
    )
}

suspend fun Connection.agentSchedules(): List<AgentSchedule> = execute<SchedulesAnswer>(GoalDocuments.AGENT_SCHEDULES).ListAgentSchedules.filterNotNull()
