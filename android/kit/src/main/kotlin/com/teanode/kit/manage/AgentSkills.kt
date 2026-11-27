package com.teanode.kit.manage

import com.teanode.kit.Connection
import com.teanode.kit.chats.AgentUsage
import com.teanode.kit.settings.variablesOf
import java.time.Instant
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

// Skills, source types, every agent's runs and the model prices checked against a provider's bill: the rest of the
// operator's agent pages. Ported from the iPhone app's ManageServer.swift and AgentBilling.swift.

/** Something a registry publishes, a skill or a source type, and whether this server has it. */
@Serializable
data class RegistryOffer(
    val name: String,
    val description: String = "",
    val version: String = "",
    val tags: List<String> = emptyList(),
    /** The version installed here, when one is. */
    val installed: String? = null,
    @SerialName("newer") val isNewer: Boolean = false,
) {
    val isInstalled: Boolean get() = !installed.isNullOrEmpty()

    /** Installed, and as new as the registry has. */
    val isCurrent: Boolean get() = isInstalled && !isNewer

    /** Whether a filter's words find it, in its name, description or tags. */
    fun matches(filterText: String): Boolean {
        val words = filterText.trim().lowercase()
        if (words.isEmpty()) return true
        return (listOf(name, description) + tags).any { text -> text.lowercase().contains(words) }
    }

    companion object {
        /** Whether the registry has a newer version of something installed. */
        fun isBehind(installedCount: Int, offers: List<RegistryOffer>): Boolean = installedCount > 0 && offers.any { offer -> offer.isNewer }
    }
}

/** One tool a skill declares. */
@Serializable
data class SkillTool(
    val name: String = "", val description: String = "", @SerialName("kind") val toolKind: String = "", @SerialName("needsComputer") val doesNeedComputer: Boolean = false,
)

/** A skill the operator installed: what it brings, the keys it asks for, who fills them in, and whether it is offered. */
@Serializable
data class InstalledSkill(
    val name: String,
    val description: String = "",
    val version: String = "",
    val publisher: String = "",
    @SerialName("enabled") val isEnabled: Boolean = true,
    @SerialName("readable") val isReadable: Boolean = true,
    val problem: String? = null,
    /** Empty for as the skill says, operator, or person. */
    @SerialName("scope") val skillScope: String = "",
    /** The keys the operator fills in. */
    val secrets: List<String> = emptyList(),
    /** The keys each person fills in on their own agent page. */
    val personalSecrets: List<String> = emptyList(),
    val tools: List<SkillTool> = emptyList(),
) {
    /** Whether one of its tools runs commands on a person's computer. */
    val doesRunCommands: Boolean get() = tools.any { tool -> tool.doesNeedComputer }

    /** Whether it asks for any values, so who fills them in is a choice. */
    val hasSecrets: Boolean get() = secrets.isNotEmpty() || personalSecrets.isNotEmpty()

    /** How many of the operator's keys are filled in, from the set of "skill/key" names that are. */
    fun filledSecretCount(filledSecretNames: Set<String>): Int = secrets.count { key -> filledSecretNames.contains("$name/$key") }
}

/** The values typed for a skill's keys. Only what was typed is sent: a key left blank keeps its value. */
data class SkillSecretsDraft(val skillName: String, val valueByKey: Map<String, String> = emptyMap()) {
    /** The keys with something typed, in the order the skill asks for them. */
    fun typedKeys(keys: List<String>): List<String> = keys.filter { key -> !valueByKey[key].isNullOrEmpty() }

    fun agentParameters(keys: List<String>): JsonObject =
        objectOf("skillSecrets" to typedKeys(keys).map { key -> objectOf("skill" to skillName, "key" to key, "value" to valueByKey[key].orEmpty()) })
}

/** How a knowledge source is read, as the operator installed it: from the registry, or a local file of their own. */
@Serializable
data class InstalledSourceType(
    val name: String,
    val description: String = "",
    val version: String = "",
    val publisher: String = "",
    val isLocal: Boolean = false,
    /** files, journal, sent, web, or empty for a type that runs commands. */
    val reader: String = "",
    /** Where it runs: server, computer. */
    val runs: List<String> = emptyList(),
    val requires: List<String> = emptyList(),
    /** What it says about itself, in Markdown. */
    val guide: String = "",
    @SerialName("readable") val isReadable: Boolean = true,
    val problem: String? = null,
) {
    val hasGuide: Boolean get() = guide.trim().isNotEmpty()
}

/**
 * One conversation of any person's agent, as the oversight lists show it: whose, what it was about, its kind (a run kind, or main or named
 * for a person's own conversations), when it last spoke, whether a turn is in flight now and what it used. Its id opens its transcript.
 */
@Serializable
data class ServerAgentRun(
    val id: String, val agentId: String = "", val title: String = "", val jobKind: String? = null, val lastAt: String = "", val usage: AgentUsage = AgentUsage(),
    val ownerUsername: String = "", val ownerName: String = "", val isRunning: Boolean = false,
) {
    /** Every token it used: read fresh, read from the cache, and written. */
    val tokenCount: Int get() = usage.promptTokens + (usage.cacheReadTokens ?: 0) + usage.completionTokens

    /** Whose it is, as a person reads it: their name, or their username when they have no name. */
    val ownerLabel: String get() = ownerName.ifEmpty { ownerUsername }
}

/** One page of every agent's runs, and how many match in all: `totalCount` where the server sends it, else the older `total`. */
@Serializable
data class ServerAgentRunPage(val total: Int = 0, val runs: List<ServerAgentRun> = emptyList(), val totalCount: Int? = null) {
    val matchCount: Int get() = totalCount ?: total
}

/** The kinds the oversight lists add to the run kinds: a person's main conversation, and their named ones. */
object OversightKinds {
    const val MAIN = "main"
    const val NAMED = "named"
}

/** Which part of the oversight page is shown: turns in flight now, the recent ones in a range, or the turns that failed. */
enum class OversightTab { Running, Recent, Failed }

/** How far back the Recent and Failed lists reach, by the conversation's last activity. */
enum class OversightRange(val dayCount: Long) {
    Day(1), Week(7), Month(30);

    /** The RFC 3339 start of the range, counted back from [now]. */
    fun since(now: Instant): String = now.minus(java.time.Duration.ofDays(dayCount)).toString()
}

/**
 * What every agent's runs are narrowed by, on the server: to one agent, one job given up on, a kind, words in the title, a time range on the
 * last activity (RFC 3339), and only the conversations with a turn in flight now.
 */
data class ServerAgentRunFilter(
    val agentId: String? = null, val jobId: String? = null, val jobKind: String? = null, val query: String = "",
    val since: String? = null, val until: String? = null, val isRunningOnly: Boolean = false,
) {
    fun variables(first: Int, offset: Int): Map<String, JsonElement> {
        val trimmedQuery = query.trim()
        return variablesOf(
            "first" to first, "offset" to offset, "agentId" to agentId, "jobId" to jobId, "kinds" to jobKind?.let { kind -> listOf(kind) }, "query" to trimmedQuery.ifEmpty { null },
            "since" to since, "until" to until, "isRunningOnly" to isRunningOnly.takeIf { isOnly -> isOnly },
        )
    }
}

/** One turn that ended in a failed note: whose conversation, which, when, and what the error said. */
@Serializable
data class AgentFailedTurn(
    val conversationId: String, val conversationKind: String = "", val conversationTitle: String? = null,
    val ownerUsername: String = "", val ownerName: String = "", val failedAt: String = "", val errorText: String = "", val agentId: String = "",
) {
    /** Whose it is, as a person reads it: their name, or their username when they have no name. */
    val ownerLabel: String get() = ownerName.ifEmpty { ownerUsername }
}

/** One page of failed turns, newest first, and how many there are in all. */
@Serializable
data class AgentFailedTurnPage(val failedTurns: List<AgentFailedTurn> = emptyList(), val totalCount: Int = 0)

/** One price the check set beside the bill, per million tokens. */
@Serializable
data class AgentBilledPrice(
    val model: String = "",
    /** input, cachedInput, cacheWrite, output or audioInput. */
    val tokenKind: String = "",
    val configuredPerMillion: Double = 0.0,
    val billedPerMillion: Double = 0.0,
    val dayCount: Int = 0,
    /** Whether the configured price comes from an entry whose model is a pattern; the check reports such a price and never changes it. */
    val isPattern: Boolean = false,
)

/** One price a check set from the bill. */
@Serializable
data class AgentPriceChange(val model: String = "", val tokenKind: String = "", val beforePerMillion: Double = 0.0, val afterPerMillion: Double = 0.0)

/** The last check of one provider's prices against its bill. */
@Serializable
data class AgentBillingCheck(
    val providerName: String = "",
    /** Go writes its zero time for a provider never checked. */
    val checkedAt: String = "",
    /** Why the bill could not be read, such as a refused key; empty when it was. */
    val errorMessage: String = "",
    val compared: List<AgentBilledPrice> = emptyList(),
    val changed: List<AgentPriceChange> = emptyList(),
) {
    /** When it was checked; null for a provider that never was. */
    val checkedInstant: java.time.Instant?
        get() = com.teanode.kit.photos.ServerTime.instant(checkedAt)?.takeIf { instant -> instant.atZone(java.time.ZoneOffset.UTC).year > 1 }
}

object AgentBillingDocuments {
    const val FIELDS = """providerName checkedAt errorMessage
compared { model tokenKind configuredPerMillion billedPerMillion dayCount isPattern }
changed { model tokenKind beforePerMillion afterPerMillion }"""

    val AGENT_BILLING_CHECKS = """query { AgentBillingChecks { $FIELDS } }"""

    val CHECK_AGENT_PROVIDER_PRICES = """mutation { CheckAgentProviderPrices { $FIELDS } }"""
}

@Serializable private class AgentBillingChecksAnswer(val AgentBillingChecks: List<AgentBillingCheck> = emptyList())

@Serializable private class CheckPricesAnswer(val CheckAgentProviderPrices: List<AgentBillingCheck> = emptyList())

@Serializable private class RunsAnswer(val ListAllAgentRuns: ServerAgentRunPage)

@Serializable private class FailedTurnsAnswer(val ListFailedAgentTurns: AgentFailedTurnPage)

@Serializable private class InstalledSkillsAnswer(val ListAgentSkills: List<InstalledSkill?> = emptyList())

@Serializable private class SkillRegistryAnswer(val SearchAgentSkills: List<RegistryOffer?> = emptyList())

@Serializable private class FilledSecret(val skill: String = "", val key: String = "", val hasValue: Boolean = false)

@Serializable private class FilledSecretsAgent(val skillSecrets: List<FilledSecret?>? = null)

@Serializable private class FilledSecretsSettings(val agent: FilledSecretsAgent = FilledSecretsAgent())

@Serializable private class FilledSecretsAnswer(val GetSettings: FilledSecretsSettings = FilledSecretsSettings())

@Serializable private class InstalledSourceTypesAnswer(val ListAgentSourceTypes: List<InstalledSourceType?> = emptyList())

@Serializable private class SourceTypeRegistryAnswer(val SearchAgentSourceTypes: List<RegistryOffer?> = emptyList())

/** What the last check of each provider that has a billing key compared and changed. A provider never checked is listed with no time. */
suspend fun Connection.agentBillingChecks(): List<AgentBillingCheck> = execute<AgentBillingChecksAnswer>(AgentBillingDocuments.AGENT_BILLING_CHECKS).AgentBillingChecks

/** Checks the prices of every provider that has a billing key against its bill now, changes the ones that differ, and answers with what was compared and changed. One check covers them all. */
suspend fun Connection.checkAgentProviderPrices(): List<AgentBillingCheck> = execute<CheckPricesAnswer>(AgentBillingDocuments.CHECK_AGENT_PROVIDER_PRICES).CheckAgentProviderPrices

/** Queues a job given up on again. */
suspend fun Connection.retryAgentJob(jobId: String) {
    execute<JsonObject>(ManageServerDocuments.MANAGE_RETRY_AGENT_JOB, variablesOf("jobId" to jobId))
}

/** One page of every run any agent made, newest first, filtered on the server. */
suspend fun Connection.serverAgentRuns(filter: ServerAgentRunFilter, first: Int = 50, offset: Int = 0): ServerAgentRunPage =
    execute<RunsAnswer>(ManageServerDocuments.MANAGE_ALL_AGENT_RUNS, filter.variables(first, offset)).ListAllAgentRuns

/** One page of the turns that ended in a failure, newest first, since a time (RFC 3339) and for one agent when given. */
suspend fun Connection.failedAgentTurns(since: String? = null, agentId: String? = null, first: Int = 50, offset: Int = 0): AgentFailedTurnPage =
    execute<FailedTurnsAnswer>(ManageServerDocuments.MANAGE_FAILED_AGENT_TURNS, variablesOf("first" to first, "offset" to offset, "since" to since, "agentId" to agentId)).ListFailedAgentTurns

/** The conversation that shows what a job given up on tried: its latest run, or, for a remember job that never ran, the conversation it was about. Null when there is neither. */
suspend fun Connection.deadJobConversationId(job: AgentDeadJob): String? {
    val page = serverAgentRuns(ServerAgentRunFilter(jobId = job.id), first = 1)
    page.runs.firstOrNull()?.let { run -> return run.id }
    if (job.kind == "remember" && !job.subjectId.isNullOrEmpty()) return job.subjectId
    return null
}

suspend fun Connection.installedSkills(): List<InstalledSkill> = execute<InstalledSkillsAnswer>(ManageServerDocuments.MANAGE_INSTALLED_SKILLS).ListAgentSkills.filterNotNull()

suspend fun Connection.skillRegistry(): List<RegistryOffer> = execute<SkillRegistryAnswer>(ManageServerDocuments.MANAGE_SKILL_REGISTRY).SearchAgentSkills.filterNotNull()

/** Which of the keys the installed skills ask the operator for are filled in, as "skill/key", without the values. */
suspend fun Connection.filledSkillSecrets(): Set<String> =
    (execute<FilledSecretsAnswer>(ManageServerDocuments.MANAGE_FILLED_SKILL_SECRETS).GetSettings.agent.skillSecrets ?: emptyList())
        .filterNotNull().filter { secret -> secret.hasValue }.map { secret -> "${secret.skill}/${secret.key}" }.toSet()

suspend fun Connection.installSkill(name: String) {
    execute<JsonObject>(ManageServerDocuments.MANAGE_INSTALL_SKILL, variablesOf("name" to name))
}

suspend fun Connection.removeSkill(name: String) {
    execute<JsonObject>(ManageServerDocuments.MANAGE_REMOVE_SKILL, variablesOf("name" to name))
}

/** Offers a skill's tools to everybody again, or stops offering them while keeping it installed. */
suspend fun Connection.setSkillEnabled(name: String, isEnabled: Boolean) {
    execute<JsonObject>(ManageServerDocuments.MANAGE_SET_SKILL_ENABLED, variablesOf("name" to name, "enabled" to isEnabled))
}

/** Who fills a skill's values in: empty as the skill says, operator, or person. */
suspend fun Connection.setSkillScope(name: String, skillScope: String) {
    execute<JsonObject>(ManageServerDocuments.MANAGE_SET_SKILL_SCOPE, variablesOf("name" to name, "scope" to skillScope))
}

/** Keeps the values typed for a skill's keys, one each for everybody. */
suspend fun Connection.saveSkillSecrets(draft: SkillSecretsDraft, keys: List<String>) {
    execute<JsonObject>(ManageServerDocuments.MANAGE_SAVE_SKILL_SECRETS, mapOf("agent" to draft.agentParameters(keys)))
}

suspend fun Connection.installedSourceTypes(): List<InstalledSourceType> =
    execute<InstalledSourceTypesAnswer>(ManageServerDocuments.MANAGE_INSTALLED_SOURCE_TYPES).ListAgentSourceTypes.filterNotNull()

suspend fun Connection.sourceTypeRegistry(): List<RegistryOffer> =
    execute<SourceTypeRegistryAnswer>(ManageServerDocuments.MANAGE_SOURCE_TYPE_REGISTRY).SearchAgentSourceTypes.filterNotNull()

suspend fun Connection.installSourceType(name: String) {
    execute<JsonObject>(ManageServerDocuments.MANAGE_INSTALL_SOURCE_TYPE, variablesOf("name" to name))
}

/** Removes a source type. The server refuses while a source is of it, and says which. */
suspend fun Connection.removeSourceType(name: String) {
    execute<JsonObject>(ManageServerDocuments.MANAGE_REMOVE_SOURCE_TYPE, variablesOf("name" to name))
}
