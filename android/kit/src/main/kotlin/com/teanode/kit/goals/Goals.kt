package com.teanode.kit.goals

import com.teanode.kit.Connection
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive

/** The goals' GraphQL: the same text as the command line's (internal/client/agent_goal.go) and the iPhone app's. */
object GoalDocuments {
    private const val GOAL_FIELDS =
        "conversationId goalTitle goalDescription goalState goalStatus goalTimes goalMailConditions { field header operator value } " +
            "goalWrittenBy isGoalNotifiedWhenPosted goalSetAt goalNextAt goalSurfacedAt lastAt"

    const val GOALS = """query (${'$'}goalStates: [String!]) { ListAgentGoals(goalStates: ${'$'}goalStates) { $GOAL_FIELDS } }"""
    const val GOAL = """query (${'$'}conversationId: String!) {
  GetAgentGoal(conversationId: ${'$'}conversationId) {
    $GOAL_FIELDS
    goalOriginTitle
    activity { id createdAt goalActivityKind activityHeadline activityDetail }
  }
}"""
    const val START_GOAL = """mutation (${'$'}goalTitle: String!, ${'$'}goalDescription: String!, ${'$'}originConversationId: String, ${'$'}goalTimes: [String!], ${'$'}goalMailConditions: [MailboxRuleConditionInput!], ${'$'}isGoalNotifiedWhenPosted: Boolean) {
  StartAgentGoal(goalTitle: ${'$'}goalTitle, goalDescription: ${'$'}goalDescription, originConversationId: ${'$'}originConversationId, goalTimes: ${'$'}goalTimes, goalMailConditions: ${'$'}goalMailConditions, isGoalNotifiedWhenPosted: ${'$'}isGoalNotifiedWhenPosted) { $GOAL_FIELDS }
}"""
    const val UPDATE_GOAL = """mutation (${'$'}conversationId: String!, ${'$'}goalTitle: String, ${'$'}goalDescription: String, ${'$'}goalTimes: [String!], ${'$'}goalMailConditions: [MailboxRuleConditionInput!], ${'$'}isGoalNotifiedWhenPosted: Boolean) {
  UpdateAgentGoal(conversationId: ${'$'}conversationId, goalTitle: ${'$'}goalTitle, goalDescription: ${'$'}goalDescription, goalTimes: ${'$'}goalTimes, goalMailConditions: ${'$'}goalMailConditions, isGoalNotifiedWhenPosted: ${'$'}isGoalNotifiedWhenPosted) { $GOAL_FIELDS }
}"""
    const val RUN_GOAL = """mutation (${'$'}conversationId: String!) { RunAgentGoal(conversationId: ${'$'}conversationId) { $GOAL_FIELDS } }"""
    const val TELL_GOAL = """mutation (${'$'}conversationId: String!, ${'$'}text: String!) {
  TellAgentGoal(conversationId: ${'$'}conversationId, text: ${'$'}text) { $GOAL_FIELDS }
}"""
    const val SET_GOAL_STATE = """mutation (${'$'}conversationId: String!, ${'$'}goalState: String!) {
  SetAgentGoalState(conversationId: ${'$'}conversationId, goalState: ${'$'}goalState) { $GOAL_FIELDS }
}"""
    const val SET_BRIEF = """mutation (${'$'}enabled: Boolean!, ${'$'}at: String, ${'$'}days: [Int!]) {
  SetAgentBrief(enabled: ${'$'}enabled, at: ${'$'}at, days: ${'$'}days) { $GOAL_FIELDS }
}"""
    const val RUN_BRIEF_NOW = """mutation { RunAgentBriefNow { $GOAL_FIELDS } }"""
}

/** AgentGoal is something the agent works toward on its own, in a conversation of its own, and when it takes its turns. */
@Serializable
data class AgentGoal(
    val conversationId: String,
    val goalTitle: String,
    val goalDescription: String,
    /** working, waiting, paused, met or dropped. */
    val goalState: String,
    val goalStatus: String,
    /** When its turns are, in the person's zone: a cron line or `@at yyyy-MM-dd HH:mm`. None means the agent keeps its own pace. */
    val goalTimes: List<String> = emptyList(),
    /** What a message must match for it to take a turn when mail arrives; none means no mail does. */
    val goalMailConditions: List<GoalMailCondition> = emptyList(),
    /** person, agent or built_in. */
    val goalWrittenBy: String = "person",
    val isGoalNotifiedWhenPosted: Boolean = false,
    val goalSetAt: String? = null,
    val goalNextAt: String? = null,
    val goalSurfacedAt: String? = null,
    val lastAt: String? = null,
    val goalOriginTitle: String? = null,
    val activity: List<Activity?>? = null,
) {
    @Serializable
    data class Activity(
        val id: String,
        val createdAt: String,
        val goalActivityKind: String,
        val activityHeadline: String,
        val activityDetail: String,
    )

    val id: String get() = conversationId

    val isOpen: Boolean get() = goalState == "working" || goalState == "waiting"

    val isPaused: Boolean get() = goalState == "paused"

    /** Whether it takes a turn when mail arrives. */
    val isTriggeredByMail: Boolean get() = goalMailConditions.isNotEmpty()

    /** Whether the agent keeps its own pace: no set times and no mail. */
    val isAtItsOwnPace: Boolean get() = goalTimes.isEmpty() && goalMailConditions.isEmpty()
}

/** What a goal is started with or changed by: only what is not null is sent, and an empty list takes the times or conditions away. */
data class GoalChange(
    val goalTitle: String? = null,
    val goalDescription: String? = null,
    val goalTimes: List<String>? = null,
    val goalMailConditions: List<GoalMailCondition>? = null,
    val isGoalNotifiedWhenPosted: Boolean? = null,
) {
    fun variables(): MutableMap<String, JsonElement> {
        val variables = mutableMapOf<String, JsonElement>()
        goalTitle?.let { text -> variables["goalTitle"] = JsonPrimitive(text) }
        goalDescription?.let { text -> variables["goalDescription"] = JsonPrimitive(text) }
        goalTimes?.let { lines -> variables["goalTimes"] = JsonArray(lines.map { line -> JsonPrimitive(line) }) }
        goalMailConditions?.let { conditions -> variables["goalMailConditions"] = GoalMailTrigger.variables(conditions) }
        isGoalNotifiedWhenPosted?.let { isNotified -> variables["isGoalNotifiedWhenPosted"] = JsonPrimitive(isNotified) }
        return variables
    }
}

@Serializable
private class GoalsAnswer(val ListAgentGoals: List<AgentGoal?>)

@Serializable
private class GoalAnswer(val GetAgentGoal: AgentGoal? = null)

@Serializable
private class StartGoalAnswer(val StartAgentGoal: AgentGoal)

@Serializable
private class UpdateGoalAnswer(val UpdateAgentGoal: AgentGoal? = null)

@Serializable
private class RunGoalAnswer(val RunAgentGoal: AgentGoal? = null)

@Serializable
private class TellGoalAnswer(val TellAgentGoal: AgentGoal? = null)

@Serializable
private class SetGoalStateAnswer(val SetAgentGoalState: AgentGoal? = null)

@Serializable
private class SetBriefAnswer(val SetAgentBrief: AgentGoal? = null)

@Serializable
private class RunBriefAnswer(val RunAgentBriefNow: AgentGoal? = null)

suspend fun Connection.goals(): List<AgentGoal> = execute<GoalsAnswer>(GoalDocuments.GOALS).ListAgentGoals.filterNotNull()

suspend fun Connection.goal(conversationId: String): AgentGoal? =
    execute<GoalAnswer>(GoalDocuments.GOAL, mapOf("conversationId" to JsonPrimitive(conversationId))).GetAgentGoal

/** Starts a goal; returns its conversation. */
suspend fun Connection.startGoal(change: GoalChange): String {
    val variables = change.variables()
    if (!variables.containsKey("goalTitle")) variables["goalTitle"] = JsonPrimitive("")
    if (!variables.containsKey("goalDescription")) variables["goalDescription"] = JsonPrimitive("")
    return execute<StartGoalAnswer>(GoalDocuments.START_GOAL, variables).StartAgentGoal.conversationId
}

/** Changes a goal: its title, description, times, mail conditions and whether a post notifies. */
suspend fun Connection.updateGoal(conversationId: String, change: GoalChange): AgentGoal? {
    val variables = change.variables()
    variables["conversationId"] = JsonPrimitive(conversationId)
    return execute<UpdateGoalAnswer>(GoalDocuments.UPDATE_GOAL, variables).UpdateAgentGoal
}

/** Gives a working goal a turn now. */
suspend fun Connection.runGoal(conversationId: String) {
    execute<RunGoalAnswer>(GoalDocuments.RUN_GOAL, mapOf("conversationId" to JsonPrimitive(conversationId)))
}

suspend fun Connection.tellGoal(conversationId: String, text: String) {
    execute<TellGoalAnswer>(GoalDocuments.TELL_GOAL, mapOf("conversationId" to JsonPrimitive(conversationId), "text" to JsonPrimitive(text)))
}

/** Marks a goal met, dropped or paused, or opens it again as working. */
suspend fun Connection.setGoalState(conversationId: String, goalState: String) {
    execute<SetGoalStateAnswer>(
        GoalDocuments.SET_GOAL_STATE, mapOf("conversationId" to JsonPrimitive(conversationId), "goalState" to JsonPrimitive(goalState)),
    )
}

/** Turns the morning brief on or off, at a time on some days (1 is Monday, 7 is Sunday); no days keeps the goal's own. */
suspend fun Connection.setDailyBrief(isEnabled: Boolean, time: String, days: List<Int>): AgentGoal? {
    val variables = mutableMapOf<String, JsonElement>("enabled" to JsonPrimitive(isEnabled), "at" to JsonPrimitive(time))
    if (days.isNotEmpty()) variables["days"] = JsonArray(days.map { day -> JsonPrimitive(day) })
    return execute<SetBriefAnswer>(GoalDocuments.SET_BRIEF, variables).SetAgentBrief
}

/** Gives the brief a turn now. */
suspend fun Connection.runDailyBrief(): AgentGoal? = execute<RunBriefAnswer>(GoalDocuments.RUN_BRIEF_NOW).RunAgentBriefNow
