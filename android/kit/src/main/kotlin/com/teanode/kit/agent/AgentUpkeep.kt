package com.teanode.kit.agent

import com.teanode.kit.Connection
import com.teanode.kit.photos.ServerTime
import com.teanode.kit.settings.variablesOf
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlin.math.max

/** The agent's own settings the upkeep pages read: its dreaming hours, alerts and the weekly look around. */
@Serializable
data class AgentUpkeepPreferences(
    val id: String,
    val enabled: Boolean = true,
    val operatorDisabledAt: String? = null,
    val dreamFrom: String? = null,
    val dreamUntil: String? = null,
    val dreamBootstrap: Boolean? = null,
    val isAlertsEnabled: Boolean = true,
    val alertQuietStart: String? = null,
    val alertQuietEnd: String? = null,
    val alertDailyMost: Int = 0,
    val lookAroundWeekday: String? = null,
    val lookAroundTime: String? = null,
) {
    val isRunning: Boolean get() = enabled && operatorDisabledAt.isNullOrEmpty()
    val shownDreamFrom: String get() = UpkeepClock.orDefault(dreamFrom, "01:00")
    val shownDreamUntil: String get() = UpkeepClock.orDefault(dreamUntil, "06:00")
    val shownAlertQuietStart: String get() = UpkeepClock.orDefault(alertQuietStart, "22:00")
    val shownAlertQuietEnd: String get() = UpkeepClock.orDefault(alertQuietEnd, "07:00")
    val shownAlertDailyMost: Int get() = if (alertDailyMost > 0) alertDailyMost else 5
    val shownLookAroundWeekday: String get() = lookAroundWeekday?.takeIf { day -> lookAroundWeekdays.contains(day) } ?: "thursday"
    val shownLookAroundTime: String get() = UpkeepClock.orDefault(lookAroundTime, "17:00")

    companion object {
        val lookAroundWeekdays = listOf("monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday")
    }
}

/** Times of day as the upkeep settings write them, "HH:MM". */
object UpkeepClock {
    fun hourAndMinute(text: String): Pair<Int, Int>? = AgentSettings.clockParts(text)

    fun text(hour: Int, minute: Int): String = AgentSettings.clockText(hour, minute)

    fun orDefault(text: String?, fallback: String): String = if (text != null && hourAndMinute(text) != null) text else fallback
}

/** What one night of dreaming did, and what it cost. */
@Serializable
data class AgentDream(
    val id: String,
    val jobId: String? = null,
    val startedAt: String,
    val finishedAt: String? = null,
    val digested: Int = 0, val filed: Int = 0, val merged: Int = 0, val rewritten: Int = 0, val moved: Int = 0, val dormant: Int = 0,
    val embedded: Int = 0, val backlog: Int = 0, val coarse: Boolean = false, val strengthened: Int = 0, val associated: Int = 0,
    val rehearsed: Int = 0, val gaps: Int = 0, val unknown: Int = 0, val revised: Int = 0, val overviewsWritten: Int = 0,
    val themesMade: Int = 0, val themesUpdated: Int = 0, val reflectionsWritten: Int = 0,
    val lastError: String? = null,
    val cost: Double = 0.0,
    val currency: String? = null,
) {
    enum class OutcomeKind { Read, Filed, Rewritten, Merged, Moved, Retired, Embedded, Strengthened, Associated, Rehearsed, Gaps, Unknown, Revised, OverviewsWritten, ThemesMade, ThemesUpdated, ReflectionsWritten }

    data class Outcome(val outcomeKind: OutcomeKind, val outcomeCount: Int)

    /** What it did, in the order the page lists it; what it did none of is left out, except the checks of what it knows. */
    val outcomes: List<Outcome>
        get() = buildList {
            fun add(kind: OutcomeKind, count: Int, isShownAtZero: Boolean = false) {
                if (count > 0 || isShownAtZero) add(Outcome(kind, count))
            }
            add(OutcomeKind.Read, digested)
            add(OutcomeKind.Filed, filed)
            add(OutcomeKind.Rewritten, rewritten)
            add(OutcomeKind.Merged, merged)
            add(OutcomeKind.Moved, moved)
            add(OutcomeKind.Retired, dormant)
            add(OutcomeKind.Embedded, embedded)
            add(OutcomeKind.Strengthened, strengthened)
            add(OutcomeKind.Associated, associated)
            if (rehearsed > 0) {
                add(OutcomeKind.Rehearsed, rehearsed)
                add(OutcomeKind.Gaps, gaps, isShownAtZero = true)
                add(OutcomeKind.Unknown, unknown, isShownAtZero = true)
            }
            add(OutcomeKind.Revised, revised)
            add(OutcomeKind.OverviewsWritten, overviewsWritten)
            add(OutcomeKind.ThemesMade, themesMade)
            add(OutcomeKind.ThemesUpdated, themesUpdated)
            add(OutcomeKind.ReflectionsWritten, reflectionsWritten)
        }

    /** How long it took in minutes; null while it runs. */
    val minutesTaken: Int?
        get() {
            val started = ServerTime.instant(startedAt) ?: return null
            val finished = ServerTime.instant(finishedAt) ?: return null
            return max(0, Math.round((finished.epochSecond - started.epochSecond) / 60.0).toInt())
        }
}

/** Whether a dream is queued or running now. */
@Serializable
data class AgentDreamState(val dreamJobStatus: String = "", val queuedAt: String? = null, val startedAt: String? = null) {
    val isQueued: Boolean get() = dreamJobStatus == "queued"
    val isRunning: Boolean get() = dreamJobStatus == "running"
    val isIdle: Boolean get() = dreamJobStatus.isEmpty()
}

/** How far the agent has read what it was given, and how long the rest takes. */
@Serializable
data class ReadingProgress(
    val waiting: Int = 0, val read: Int = 0, val perHour: Double = 0.0, val hoursLeft: Double = 0.0, val bootstrapping: Boolean = false,
    val spent: Double = 0.0, val costLeft: Double = 0.0, val currency: String? = null, val dreams: Int = 0,
)

/** A schedule: something the agent does at a time, or when mail matching conditions arrives. */
@Serializable
data class AgentSchedule(
    val id: String,
    val name: String,
    val cron: String,
    val prompt: String = "",
    val deliver: String = "drawer",
    val enabled: Boolean = true,
    val lastRunAt: String? = null,
    val nextRunAt: String? = null,
    val conversationId: String? = null,
    val mailConditions: List<JsonObject>? = null,
) {
    val isDeliveredByMail: Boolean get() = deliver == "mail"
    val isTriggeredByMail: Boolean get() = !mailConditions.isNullOrEmpty()
}

/** One mail an alert covers. */
@Serializable
data class AlertCovered(val mailId: String = "", val subject: String = "", val fromAddress: String = "")

/** A way to mute an alert: for one thing, or for a whole kind. */
@Serializable
data class AlertMuteChoice(val muteScope: String, val muteTarget: String = "")

/** A message the agent sent unasked about mail that arrived. */
@Serializable
data class AgentAlert(
    val id: String,
    val alertText: String = "",
    val sentAt: String = "",
    val subjectKey: String = "",
    val isUrgent: Boolean = false,
    val covered: List<AlertCovered> = emptyList(),
    val muteChoices: List<AlertMuteChoice> = emptyList(),
)

/** What the person asked the agent not to tell them about. */
@Serializable
data class AgentAlertMute(val id: String, val muteScope: String, val muteTarget: String = "", val createdAt: String = "")

/** A server the agent is connected to, whose tools it may use. */
@Serializable
data class AgentConnectedServer(
    val name: String,
    val transport: String = "",
    val auth: String = "",
    val oauthRedirect: String = "",
    val headless: Boolean = false,
    val enabled: Boolean = true,
    val status: String = "",
    val lastError: String? = null,
    val tools: Int = 0,
) {
    val isPersonal: Boolean get() = auth == "user" || auth == "oauth"
    val isConnected: Boolean get() = status == "connected"
    val doesAuthorizeInBrowser: Boolean get() = auth == "oauth"
}

/** A piece of work the agent does in the background: a survey or a task it was given. */
@Serializable
data class AgentBackgroundWork(
    val id: String,
    val workKind: String = "survey",
    val workStatus: String,
    val title: String = "",
    val errorMessage: String = "",
    val runIds: List<String> = emptyList(),
    val createdAt: String = "",
    val finishedAt: String? = null,
    val resultText: String? = null,
) {
    val isStoppable: Boolean get() = workStatus == "queued" || workStatus == "running"
    val isOpenable: Boolean get() = workStatus == "done" || workStatus == "failed"
    val isSurvey: Boolean get() = workKind == "survey"
}

/** What a run used. */
@Serializable
data class AgentRunUsage(val promptTokens: Int = 0, val completionTokens: Int = 0, val cost: Double = 0.0, val cacheReadTokens: Int? = null)

/** One run of the agent, in a line. */
@Serializable
data class AgentRunSummary(val id: String, val title: String = "", val jobKind: String? = null, val lastAt: String = "", val usage: AgentRunUsage = AgentRunUsage()) {
    val tokenCount: Int get() = usage.promptTokens + (usage.cacheReadTokens ?: 0) + usage.completionTokens
}

/** A page of runs, and how many there are. */
@Serializable
data class AgentRunPage(val total: Int = 0, val runs: List<AgentRunSummary> = emptyList())

/** A fact the agent learned. */
@Serializable
data class LearnedFact(val id: String, val number: Int = 0, val kind: String = "", val text: String = "", val happenedAt: String? = null, val inferred: Boolean = false)

/** A fact the agent learned lately, and the page it was filed on. */
@Serializable
data class AgentLearnedFact(val fact: LearnedFact, val path: String = "", val name: String = "") {
    val id: String get() = fact.id
}

/** Something the person told the agent was wrong. */
@Serializable
data class AgentCorrection(val id: String, val createdAt: String = "", val kind: String = "", val said: String = "")

/** A question the agent is asked now and then to check what it remembers. */
@Serializable
data class MemoryCheckQuestion(
    val id: String,
    val createdAt: String = "",
    val modifiedAt: String = "",
    val questionKind: String = "direct",
    val questionText: String = "",
    val expectedAnswer: String = "",
    val outdatedAnswer: String? = null,
    val questionState: String = "confirmed",
    val isAnswerFiledAfter: Boolean = false,
    val conversationId: String? = null,
    val answeredAt: String? = null,
)

@Serializable private class ReadAgentUpkeepRead(val agent: AgentUpkeepPreferences? = null)

@Serializable private class ReadAgentUpkeepAnswer(val ReadAgent: ReadAgentUpkeepRead? = null)

@Serializable private class UpdateAgentUpkeepAnswer(val UpdateAgent: ReadAgentUpkeepRead? = null)

@Serializable private class ListAgentDreamsAnswer(val ListAgentDreams: List<AgentDream?>)

@Serializable private class AgentDreamStateAnswer(val AgentDreamState: AgentDreamState)

@Serializable private class AgentReadingProgressAnswer(val AgentReadingProgress: ReadingProgress)

@Serializable private class ListAgentSchedulesAnswer(val ListAgentSchedules: List<AgentSchedule?>)

@Serializable private class ListAgentAlertsAnswer(val ListAgentAlerts: List<AgentAlert?> = emptyList(), val ListAgentAlertMutes: List<AgentAlertMute?> = emptyList())

@Serializable private class ListAgentLearnedAnswer(val ListAgentLearned: List<AgentLearnedFact?>)

@Serializable private class ListAgentCorrectionsAnswer(val ListAgentCorrections: List<AgentCorrection?>)

@Serializable private class ListEvaluationQuestionsAnswer(val ListAgentEvaluationQuestions: List<MemoryCheckQuestion?>)

@Serializable private class ListAgentServersAnswer(val ListAgentServers: List<AgentConnectedServer?>)

@Serializable private class ListBackgroundWorkAnswer(val ListAgentBackgroundWork: List<AgentBackgroundWork?>)

@Serializable private class ListAgentRunsAnswer(val ListAgentRuns: AgentRunPage)

suspend fun Connection.agentUpkeep(): AgentUpkeepPreferences? = execute<ReadAgentUpkeepAnswer>(AgentUpkeepDocuments.AGENT_UPKEEP).ReadAgent?.agent

suspend fun Connection.updateAgentUpkeep(
    dreamFrom: String? = null, dreamUntil: String? = null, isAlertsEnabled: Boolean? = null, alertQuietStart: String? = null, alertQuietEnd: String? = null,
    alertDailyMost: Int? = null, lookAroundWeekday: String? = null, lookAroundTime: String? = null,
): AgentUpkeepPreferences? {
    val variables = mutableMapOf<String, JsonElement>()
    dreamFrom?.let { variables.putAll(variablesOf("dreamFrom" to it)) }
    dreamUntil?.let { variables.putAll(variablesOf("dreamUntil" to it)) }
    isAlertsEnabled?.let { variables.putAll(variablesOf("isAlertsEnabled" to it)) }
    alertQuietStart?.let { variables.putAll(variablesOf("alertQuietStart" to it)) }
    alertQuietEnd?.let { variables.putAll(variablesOf("alertQuietEnd" to it)) }
    alertDailyMost?.let { variables.putAll(variablesOf("alertDailyMost" to it)) }
    lookAroundWeekday?.let { variables.putAll(variablesOf("lookAroundWeekday" to it)) }
    lookAroundTime?.let { variables.putAll(variablesOf("lookAroundTime" to it)) }
    return execute<UpdateAgentUpkeepAnswer>(AgentUpkeepDocuments.UPDATE_AGENT_UPKEEP, variables).UpdateAgent?.agent
}

suspend fun Connection.agentDreams(first: Int = 60): List<AgentDream> =
    execute<ListAgentDreamsAnswer>(AgentUpkeepDocuments.AGENT_DREAMS, variablesOf("first" to first)).ListAgentDreams.filterNotNull()

suspend fun Connection.agentDreamState(): AgentDreamState = execute<AgentDreamStateAnswer>(AgentUpkeepDocuments.AGENT_DREAM_STATE).AgentDreamState

suspend fun Connection.readingProgress(): ReadingProgress = execute<AgentReadingProgressAnswer>(AgentUpkeepDocuments.READING_PROGRESS).AgentReadingProgress

suspend fun Connection.dreamNow() {
    execute<JsonObject>(AgentUpkeepDocuments.DREAM_NOW)
}

suspend fun Connection.setDreamBootstrap(isBootstrapping: Boolean) {
    execute<JsonObject>(AgentUpkeepDocuments.DREAM_BOOTSTRAP, variablesOf("on" to isBootstrapping))
}

suspend fun Connection.agentSchedules(): List<AgentSchedule> = execute<ListAgentSchedulesAnswer>(AgentUpkeepDocuments.AGENT_SCHEDULES).ListAgentSchedules.filterNotNull()

suspend fun Connection.saveAgentSchedule(scheduleId: String? = null, name: String? = null, cron: String? = null, prompt: String? = null, deliver: String? = null, isEnabled: Boolean? = null) {
    val variables = mutableMapOf<String, JsonElement>()
    scheduleId?.let { variables.putAll(variablesOf("scheduleId" to it)) }
    name?.let { variables.putAll(variablesOf("name" to it)) }
    cron?.let { variables.putAll(variablesOf("cron" to it)) }
    prompt?.let { variables.putAll(variablesOf("prompt" to it)) }
    deliver?.let { variables.putAll(variablesOf("deliver" to it)) }
    isEnabled?.let { variables.putAll(variablesOf("enabled" to it)) }
    execute<JsonObject>(AgentUpkeepDocuments.SAVE_AGENT_SCHEDULE, variables)
}

suspend fun Connection.deleteAgentSchedule(scheduleId: String) {
    execute<JsonObject>(AgentUpkeepDocuments.DELETE_AGENT_SCHEDULE, variablesOf("scheduleId" to scheduleId))
}

suspend fun Connection.runAgentSchedule(scheduleId: String) {
    execute<JsonObject>(AgentUpkeepDocuments.RUN_AGENT_SCHEDULE, variablesOf("scheduleId" to scheduleId))
}

/** The alerts sent and the mutes the person set. */
data class AgentAlerts(val alerts: List<AgentAlert>, val mutes: List<AgentAlertMute>)

suspend fun Connection.agentAlerts(): AgentAlerts {
    val answer = execute<ListAgentAlertsAnswer>(AgentUpkeepDocuments.AGENT_ALERTS)
    return AgentAlerts(answer.ListAgentAlerts.filterNotNull(), answer.ListAgentAlertMutes.filterNotNull())
}

suspend fun Connection.muteAgentAlert(alertId: String, muteScope: String) {
    execute<JsonObject>(AgentUpkeepDocuments.MUTE_AGENT_ALERT, variablesOf("alertId" to alertId, "muteScope" to muteScope))
}

suspend fun Connection.unmuteAgentAlert(muteId: String) {
    execute<JsonObject>(AgentUpkeepDocuments.UNMUTE_AGENT_ALERT, variablesOf("muteId" to muteId))
}

suspend fun Connection.learnedFacts(days: Int = 2, first: Int = 40): List<AgentLearnedFact> =
    execute<ListAgentLearnedAnswer>(AgentUpkeepDocuments.LEARNED_FACTS, variablesOf("days" to days, "first" to first)).ListAgentLearned.filterNotNull()

suspend fun Connection.agentCorrections(): List<AgentCorrection> =
    execute<ListAgentCorrectionsAnswer>(AgentUpkeepDocuments.AGENT_CORRECTIONS).ListAgentCorrections.filterNotNull()

suspend fun Connection.memoryCheckQuestions(): List<MemoryCheckQuestion> =
    execute<ListEvaluationQuestionsAnswer>(AgentUpkeepDocuments.MEMORY_CHECK_QUESTIONS).ListAgentEvaluationQuestions.filterNotNull()

suspend fun Connection.startMemoryCheck() {
    execute<JsonObject>(AgentUpkeepDocuments.START_MEMORY_CHECK, variablesOf("speakFirstReason" to "memory_check"))
}

suspend fun Connection.gradeMemoryNow() {
    execute<JsonObject>(AgentUpkeepDocuments.GRADE_MEMORY_NOW)
}

suspend fun Connection.connectedServers(): List<AgentConnectedServer> =
    execute<ListAgentServersAnswer>(AgentUpkeepDocuments.CONNECTED_SERVERS).ListAgentServers.filterNotNull()

suspend fun Connection.disconnectServer(server: String) {
    execute<JsonObject>(AgentUpkeepDocuments.DISCONNECT_SERVER, variablesOf("server" to server))
}

suspend fun Connection.backgroundWork(): List<AgentBackgroundWork> =
    execute<ListBackgroundWorkAnswer>(AgentUpkeepDocuments.BACKGROUND_WORK).ListAgentBackgroundWork.filterNotNull()

suspend fun Connection.stopBackgroundWork(workId: String) {
    execute<JsonObject>(AgentUpkeepDocuments.STOP_BACKGROUND_WORK, variablesOf("id" to workId))
}

suspend fun Connection.agentRuns(first: Int = 50, offset: Int = 0): AgentRunPage =
    execute<ListAgentRunsAnswer>(AgentUpkeepDocuments.AGENT_RUNS, variablesOf("first" to first, "offset" to offset)).ListAgentRuns
