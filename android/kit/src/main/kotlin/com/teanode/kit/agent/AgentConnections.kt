package com.teanode.kit.agent

import com.teanode.kit.Connection
import com.teanode.kit.chats.AgentStatus
import com.teanode.kit.settings.variablesOf
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject

/** A value an installed skill asks this person for, and whether it is kept. */
@Serializable
data class AgentSkillSecret(val skill: String, val key: String, val description: String = "", val set: Boolean = false) {
    val secretID: String get() = "$skill.$key"
}

/** Which of the person's computers a skill or a connected server makes its requests through, or this server where it names none. */
@Serializable
data class AgentReach(val kind: String, val name: String, val computerName: String = "", val isOnComputer: Boolean = false) {
    val isSkill: Boolean get() = kind == "skill"

    /** One place a reach can go through. */
    data class Option(val computerName: String, val optionKind: OptionKind)

    enum class OptionKind { ThroughThisServer, NoComputerChosen, ThroughOnlyComputer, ThroughComputer, ThroughComputerNotAttached }

    /** Several computers are attached and none is chosen for a server that runs on a computer: its calls have nowhere to run. */
    fun needsChoice(attachedComputerCount: Int): Boolean = isOnComputer && computerName.isEmpty() && attachedComputerCount > 1

    /**
     * The places this reach can go through, the unset one first. A reach naming a computer that is not attached now is
     * kept, and shown as such, rather than quietly looking unset.
     */
    fun options(computers: List<AgentStatus.Computer>): List<Option> {
        val unsetKind = when {
            !isOnComputer -> OptionKind.ThroughThisServer
            computers.size > 1 -> OptionKind.NoComputerChosen
            else -> OptionKind.ThroughOnlyComputer
        }
        val options = mutableListOf(Option("", unsetKind))
        computers.forEach { computer -> options += Option(computer.name, OptionKind.ThroughComputer) }
        if (computerName.isNotEmpty() && computers.none { computer -> computer.name == computerName }) options += Option(computerName, OptionKind.ThroughComputerNotAttached)
        return options
    }
}

/** The person's own bot in a chat app: whether its token is kept, the chat it is linked to or the code that links one, and whether it runs. */
@Serializable
data class AgentChatChannel(
    val kind: String,
    val hasToken: Boolean = false,
    val botName: String? = null,
    val linked: Boolean = false,
    val linkedName: String? = null,
    val linkCode: String? = null,
    val enabled: Boolean = false,
    val running: Boolean = false,
    val lastError: String? = null,
) {
    enum class Condition { Off, NotAnswering, Running, Starting }

    val condition: Condition
        get() = when {
            !enabled -> Condition.Off
            !lastError.isNullOrEmpty() -> Condition.NotAnswering
            running -> Condition.Running
            else -> Condition.Starting
        }

    /** What the person sends the bot in the chat to link it. */
    val linkCommand: String? get() = linkCode?.takeIf { code -> code.isNotEmpty() && !linked }?.let { code -> "/link $code" }

    companion object {
        /** The chat apps a bot can be set up in, in the order offered. */
        val kinds = listOf("telegram", "discord")
    }
}

/** What state a connected server shows as in the list. */
enum class ServerCondition { Off, Shared, Connected, NotAnswering, AuthorizationUnderway, NotConnected }

val AgentConnectedServer.condition: ServerCondition
    get() = when {
        !enabled -> ServerCondition.Off
        !isPersonal -> ServerCondition.Shared
        status == "connected" -> ServerCondition.Connected
        status == "error" -> ServerCondition.NotAnswering
        status == "pending" -> ServerCondition.AuthorizationUnderway
        else -> ServerCondition.NotConnected
    }

/** Whether the person can connect this server with a credential of their own from the phone. */
val AgentConnectedServer.canConnectWithCredential: Boolean get() = isPersonal && !doesAuthorizeInBrowser && enabled && !isConnected

/** What the person can be told is held back, sent or refused of the replies the agent wrote for them. */
@Serializable
data class AgentReply(
    val id: String,
    val createdAt: String = "",
    val mailboxId: String? = null,
    val mailId: String? = null,
    val status: String = "",
    val reason: String? = null,
    val subject: String = "",
    val to: String? = null,
    val sendAfter: String? = null,
    val sentAt: String? = null,
) {
    val isHeld: Boolean get() = status == "held"
}

/** The newest replies the agent answered, held or declined, and how many there are. */
@Serializable
data class AgentReplyPage(val total: Int = 0, val replies: List<AgentReply> = emptyList())

@Serializable private class ConnectedAgentServer(val status: String = "", val lastError: String? = null)

@Serializable private class ConnectAgentServerAnswer(val ConnectAgentServer: ConnectedAgentServer)

@Serializable private class ListAgentSkillSecretsAnswer(val ListAgentSkillSecrets: List<AgentSkillSecret?>)

@Serializable private class ListAgentReachesAnswer(val ListAgentReaches: List<AgentReach?>, val ReadAgentComputers: AgentComputersAnswer? = null)

@Serializable private class AgentComputersAnswer(val computers: List<AgentStatus.Computer> = emptyList())

@Serializable private class ListAgentChannelsAnswer(val ListAgentChannels: List<AgentChatChannel?>)

@Serializable private class GetAgentBackgroundWorkAnswer(val GetAgentBackgroundWork: AgentBackgroundWork? = null)

@Serializable private class ListAgentRepliesAnswer(val ListAgentReplies: AgentReplyPage)

/** Connects to a server with the person's credential, and returns why it refused, if it did (empty when it gave no reason). */
suspend fun Connection.connectServer(server: String, credential: String): String? {
    val connected = execute<ConnectAgentServerAnswer>(AgentUpkeepDocuments.CONNECT_SERVER, variablesOf("server" to server, "credential" to credential)).ConnectAgentServer
    if (connected.status != "error") return null
    return connected.lastError.orEmpty()
}

suspend fun Connection.skillSecrets(): List<AgentSkillSecret> = execute<ListAgentSkillSecretsAnswer>(AgentUpkeepDocuments.SKILL_SECRETS).ListAgentSkillSecrets.filterNotNull()

suspend fun Connection.setSkillSecret(skill: String, key: String, value: String) {
    execute<JsonObject>(AgentUpkeepDocuments.SET_SKILL_SECRET, variablesOf("skill" to skill, "key" to key, "value" to value))
}

suspend fun Connection.clearSkillSecret(skill: String, key: String) {
    execute<JsonObject>(AgentUpkeepDocuments.CLEAR_SKILL_SECRET, variablesOf("skill" to skill, "key" to key))
}

/** The reach of each skill and server, and the computers attached. */
data class AgentReaches(val reaches: List<AgentReach>, val computers: List<AgentStatus.Computer>)

suspend fun Connection.agentReaches(): AgentReaches {
    val answer = execute<ListAgentReachesAnswer>(AgentUpkeepDocuments.AGENT_REACHES)
    return AgentReaches(answer.ListAgentReaches.filterNotNull(), answer.ReadAgentComputers?.computers.orEmpty())
}

/** Sends a skill's or server's requests through a computer, or through this server with an empty name. */
suspend fun Connection.setAgentReach(kind: String, name: String, computerName: String) {
    execute<JsonObject>(AgentUpkeepDocuments.SET_AGENT_REACH, variablesOf("kind" to kind, "name" to name, "computerName" to computerName))
}

suspend fun Connection.chatChannels(): List<AgentChatChannel> = execute<ListAgentChannelsAnswer>(AgentUpkeepDocuments.CHAT_CHANNELS).ListAgentChannels.filterNotNull()

/** Keeps a bot's token, turns a bot on or off, or both. */
suspend fun Connection.setChatChannel(kind: String, token: String? = null, isEnabled: Boolean? = null) {
    val variables = mutableMapOf<String, JsonElement>()
    variables.putAll(variablesOf("kind" to kind))
    if (!token.isNullOrEmpty()) variables.putAll(variablesOf("token" to token))
    isEnabled?.let { variables.putAll(variablesOf("enabled" to it)) }
    execute<JsonObject>(AgentUpkeepDocuments.SET_CHAT_CHANNEL, variables)
}

suspend fun Connection.unlinkChatChannel(kind: String) {
    execute<JsonObject>(AgentUpkeepDocuments.UNLINK_CHAT_CHANNEL, variablesOf("kind" to kind))
}

suspend fun Connection.removeChatChannel(kind: String) {
    execute<JsonObject>(AgentUpkeepDocuments.REMOVE_CHAT_CHANNEL, variablesOf("kind" to kind))
}

/** One piece of background work with what came of it. */
suspend fun Connection.backgroundWorkResult(workId: String): AgentBackgroundWork? =
    execute<GetAgentBackgroundWorkAnswer>(AgentUpkeepDocuments.BACKGROUND_WORK_RESULT, variablesOf("id" to workId)).GetAgentBackgroundWork

/** The newest replies the agent answered, held or declined. */
suspend fun Connection.agentReplies(first: Int = 20): AgentReplyPage = execute<ListAgentRepliesAnswer>(AgentUpkeepDocuments.AGENT_REPLIES, variablesOf("first" to first)).ListAgentReplies

/**
 * The kinds a run can be, for a filter over runs: a page holds one page of runs, so the kinds on it are not the kinds
 * there are.
 */
object AgentRunKinds {
    val jobKinds = listOf(
        "triage", "reply", "research", "summarize", "remember", "ingest", "recording", "dream", "schedule", "extract", "draft", "describe", "compact",
        "evaluate", "survey", "subagent", "mcp", "goal", "background", "speak_first", "alert", "categorize", "statement_import",
        "read_receipt", "describe_photo", "statement_photo", "tax_document", "tax_mail", "watch", "follow_up_sweep", "nearby", "journal",
        "match_shopping", "shopping_picture", "shopping_import",
    )
}

/** What the work in the background shows as, and how a run's page is merged with the next one. */
enum class WorkCondition { Queued, Running, Finished, Failed, Stopped }

val AgentBackgroundWork.condition: WorkCondition
    get() = when (workStatus) {
        "queued" -> WorkCondition.Queued
        "running" -> WorkCondition.Running
        "done" -> WorkCondition.Finished
        "failed" -> WorkCondition.Failed
        else -> WorkCondition.Stopped
    }

/** A page of runs followed by the next one, leaving out a run the first already has (the list may have moved between reads). */
fun mergeRunPages(loaded: List<AgentRunSummary>, nextPage: List<AgentRunSummary>): List<AgentRunSummary> {
    val knownIDs = loaded.map { run -> run.id }.toSet()
    return loaded + nextPage.filter { run -> run.id !in knownIDs }
}
