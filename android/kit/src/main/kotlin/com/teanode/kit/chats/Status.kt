package com.teanode.kit.chats

import com.teanode.kit.Connection
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonPrimitive

/** AgentStatus is what the drawer's head shows: the browser tab and the computers attached to the agent, and how much of today's budget is used. */
data class AgentStatus(val tab: Tab?, val computers: List<Computer>, val budget: Budget?, val timezone: String?) {
    @Serializable
    data class Tab(val attached: Boolean = false, val title: String? = null, val url: String? = null)

    @Serializable
    data class Computer(val name: String, val system: String? = null, val description: String? = null)

    @Serializable
    data class Budget(
        /** Tokens used today, and the day's limit; 0 is no limit. */
        val used: Double = 0.0,
        val limit: Double = 0.0,
        val resetsAt: String? = null,
        /** Money spent today, and the day's limit in money; 0 is no limit. */
        val cost: Double = 0.0,
        val costLimit: Double = 0.0,
        val currency: String? = null,
    ) {
        /** How much of the day's budget is used, by whichever limit is closer to running out; null with no limit. */
        val fraction: Double?
            get() {
                val fractions = mutableListOf<Double>()
                if (limit > 0) fractions.add(used / limit)
                if (costLimit > 0) fractions.add(cost / costLimit)
                return fractions.maxOrNull()
            }
    }
}

/** AgentBackgroundCommand is a command the agent left running on a computer. */
@Serializable
data class AgentBackgroundCommand(
    val computer: String,
    val id: String,
    val command: String,
    val directory: String? = null,
    val conversationId: String? = null,
    val startedAt: String? = null,
    val endedAt: String? = null,
    val isRunning: Boolean = false,
    val exitCode: Int? = null,
    val stopReason: String? = null,
    val stdout: String? = null,
    val stderr: String? = null,
    val isStdoutTruncated: Boolean? = null,
    val isStderrTruncated: Boolean? = null,
    val stdoutByteCount: Int? = null,
    val stderrByteCount: Int? = null,
)

@Serializable
private class StatusAnswer(
    @SerialName("ReadAgentTab") val tab: AgentStatus.Tab? = null,
    @SerialName("ReadAgentComputers") val computers: Computers? = null,
    @SerialName("ReadAgent") val agent: Read? = null,
) {
    @Serializable
    class Computers(val computers: List<AgentStatus.Computer> = emptyList())

    @Serializable
    class Read(val budget: AgentStatus.Budget? = null, val timezone: String? = null)
}

@Serializable
private class BackgroundCommandsAnswer(@SerialName("ListAgentBackgroundCommands") val commands: List<AgentBackgroundCommand>)

@Serializable
private class BackgroundCommandAnswer(@SerialName("ReadAgentBackgroundCommand") val command: AgentBackgroundCommand)

@Serializable
private class StoppedCommandAnswer(@SerialName("StopAgentBackgroundCommand") val stopped: Stopped) {
    @Serializable
    class Stopped(val id: String)
}

/** What the drawer's head shows, read every few seconds while it is open. */
suspend fun Connection.status(): AgentStatus {
    val answer = execute(ChatDocuments.DEVICES, StatusAnswer.serializer())
    return AgentStatus(answer.tab, answer.computers?.computers ?: emptyList(), answer.agent?.budget, answer.agent?.timezone)
}

/** The commands left running, for one conversation or all. */
suspend fun Connection.backgroundCommands(conversationId: String?): List<AgentBackgroundCommand> {
    val variables = if (conversationId != null) mapOf("conversationId" to JsonPrimitive(conversationId)) else emptyMap()
    return execute(ChatDocuments.BACKGROUND_COMMANDS, BackgroundCommandsAnswer.serializer(), variables).commands
}

/** One command with what it printed. */
suspend fun Connection.backgroundCommand(computer: String, id: String): AgentBackgroundCommand =
    execute(ChatDocuments.BACKGROUND_COMMAND, BackgroundCommandAnswer.serializer(), mapOf("computer" to JsonPrimitive(computer), "id" to JsonPrimitive(id))).command

suspend fun Connection.stopBackgroundCommand(computer: String, id: String) {
    execute(ChatDocuments.STOP_BACKGROUND_COMMAND, StoppedCommandAnswer.serializer(), mapOf("computer" to JsonPrimitive(computer), "id" to JsonPrimitive(id)))
}
