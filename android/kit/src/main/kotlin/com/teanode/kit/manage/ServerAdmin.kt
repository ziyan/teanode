package com.teanode.kit.manage

import com.teanode.kit.Connection
import com.teanode.kit.photos.ServerTime
import com.teanode.kit.settings.variablesOf
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.doubleOrNull
import kotlinx.serialization.json.longOrNull

/** What the instance is: its version, since when it runs, and what waits for a restart. */
@Serializable
data class ServerStatus(
    val instance: String = "",
    val version: String = "",
    val commit: String = "",
    val startedAt: String = "",
    val uptimeSeconds: Long = 0,
    val pendingRestart: List<String>? = null,
    val supervision: String = "",
    @SerialName("restarting") val isRestarting: Boolean = false,
) {
    val shortCommit: String get() = commit.take(12)

    /** Whether nothing watches the server, so a restart would not bring it back. */
    val isUnsupervised: Boolean get() = supervision == "unknown"
}

/** What the release list says: the version running, a newer one, and whether this server may install it. */
@Serializable
data class ServerUpgrade(
    val current: String = "",
    val latest: String? = null,
    @SerialName("available") val isAvailable: Boolean = false,
    val notes: String? = null,
    val url: String? = null,
    val checkedAt: String? = null,
    val attemptedAt: String? = null,
    @SerialName("checking") val isChecking: Boolean? = null,
    @SerialName("error") val errorMessage: String? = null,
    val checkError: String? = null,
    @SerialName("applicable") val isApplicable: Boolean = false,
    @SerialName("reason") val refusalReason: String? = null,
    @SerialName("automatic") val isAutomatic: Boolean? = null,
    @SerialName("enabled") val isEnabled: Boolean? = null,
    @SerialName("window") val upgradeWindow: String? = null,
    @SerialName("upgrading") val isUpgrading: Boolean = false,
) {
    val canUpgrade: Boolean get() = isAvailable && isApplicable
}

/** One of the tabs of the server's settings page, with the sections of `GetSettings` it edits. */
enum class ServerSettingsTab(val serverName: String, val sections: List<String>) {
    Setup("setup", emptyList()),
    Identity("identity", listOf("identity")),
    Mail("mail", listOf("smtp")),
    Sending("sending", listOf("relay", "proxy")),
    Listeners("listeners", listOf("listen")),
    Certificates("certificates", listOf("certificates")),
    Storage("storage", listOf("storage", "s3", "geoip", "places", "route53")),
    Resolver("resolver", listOf("resolver")),
    Scanning("scanning", listOf("antivirus", "antispam")),
    Sessions("sessions", listOf("session", "passkey")),
    Sso("sso", listOf("sso")),
}

/**
 * One field of a section of the server's settings, as a form shows it. The type comes from what the server sent: a
 * switch, a number, a list or a text. A `has...` field says a sealed secret is kept and stands for the field of the
 * same name without `has` ("hasPassword" is "password"); its value is never read back, and a new one is sent only when
 * typed.
 */
sealed interface ServerSettingField {
    val name: String

    data class Switch(override val name: String, val isOn: Boolean) : ServerSettingField

    data class Number(override val name: String, val text: String, val isWhole: Boolean) : ServerSettingField

    data class Text(override val name: String, val text: String) : ServerSettingField

    data class TextList(override val name: String, val text: String) : ServerSettingField

    /** A sealed value: whether one is kept, never what it is. */
    data class Secret(override val name: String, val isKept: Boolean) : ServerSettingField

    /** Something the form does not edit (the sign-in providers), shown as it came. */
    data class Shown(override val name: String, val text: String) : ServerSettingField
}

object ServerSettingsForm {
    /** The fields of one section, in the order the server sent them. */
    fun fields(section: JsonObject): List<ServerSettingField> = section.entries.mapNotNull { (name, value) -> field(name, value) }

    private fun field(name: String, value: JsonElement): ServerSettingField? {
        if (name.length > 3 && name.startsWith("has") && name[3].isUpperCase() && (value as? JsonPrimitive)?.booleanOrNull != null) {
            return ServerSettingField.Secret(secretKey(name), (value as JsonPrimitive).booleanOrNull == true)
        }
        return when (value) {
            is JsonNull -> ServerSettingField.Text(name, "")
            is JsonArray -> if (value.all { item -> item is JsonPrimitive && item.isString }) ServerSettingField.TextList(name, value.joinToString(", ") { item -> (item as JsonPrimitive).content }) else ServerSettingField.Shown(name, value.toString())
            is JsonObject -> ServerSettingField.Shown(name, value.toString())
            is JsonPrimitive -> when {
                value.booleanOrNull != null && !value.isString -> ServerSettingField.Switch(name, value.booleanOrNull == true)
                !value.isString && value.longOrNull != null -> ServerSettingField.Number(name, value.content, isWhole = true)
                !value.isString && value.doubleOrNull != null -> ServerSettingField.Number(name, value.content, isWhole = false)
                else -> ServerSettingField.Text(name, value.content)
            }
        }
    }

    /** "hasSecretAccessKey" is the secret "secretAccessKey". */
    fun secretKey(hasName: String): String = hasName.removePrefix("has").replaceFirstChar { character -> character.lowercase() }

    /** Whether a typed number is one the field can hold. */
    fun isNumberValid(text: String, isWhole: Boolean): Boolean = text.isBlank() || (if (isWhole) text.trim().toLongOrNull() != null else text.trim().toDoubleOrNull() != null)

    /**
     * What to send for a section: only the fields the person changed, typed as the server sent them; a secret only when
     * one was typed, or the empty text to clear it.
     */
    fun changes(original: JsonObject, edited: Map<String, String>, switches: Map<String, Boolean>, secrets: Map<String, String>, clearedSecrets: Set<String>): JsonObject {
        val sent = linkedMapOf<String, JsonElement>()
        for (field in fields(original)) {
            when (field) {
                is ServerSettingField.Switch -> switches[field.name]?.takeIf { isOn -> isOn != field.isOn }?.let { isOn -> sent[field.name] = JsonPrimitive(isOn) }
                is ServerSettingField.Number -> edited[field.name]?.trim()?.takeIf { text -> text != field.text && text.isNotEmpty() }?.let { text -> sent[field.name] = if (field.isWhole) JsonPrimitive(text.toLong()) else JsonPrimitive(text.toDouble()) }
                is ServerSettingField.Text -> edited[field.name]?.takeIf { text -> text != field.text }?.let { text -> sent[field.name] = JsonPrimitive(text.trim()) }
                is ServerSettingField.TextList -> edited[field.name]?.let { text -> listOf(text) }?.firstOrNull()?.let { text ->
                    val items = text.split(',').map { item -> item.trim() }.filter { item -> item.isNotEmpty() }
                    if (items.joinToString(", ") != field.text) sent[field.name] = JsonArray(items.map { item -> JsonPrimitive(item) })
                }
                is ServerSettingField.Secret -> {
                    if (clearedSecrets.contains(field.name)) sent[field.name] = JsonPrimitive("") else secrets[field.name]?.takeIf { text -> text.isNotEmpty() }?.let { text -> sent[field.name] = JsonPrimitive(text) }
                }
                is ServerSettingField.Shown -> Unit
            }
        }
        return JsonObject(sent)
    }
}

/** What an agent used over a range, grouped by day, kind, model or agent. */
@Serializable
data class AgentUsageRow(
    @SerialName("key") val usageKey: String,
    val cost: Double = 0.0,
    val currency: String? = null,
    val billedAmount: Double? = null,
    val billedCurrencyCode: String? = null,
    val totals: AgentUsageTotals = AgentUsageTotals(),
)

/** A job the agent gave up on. */
@Serializable
data class AgentDeadJob(val id: String, val agentId: String = "", val mailboxId: String? = null, val kind: String = "", val subjectId: String? = null, val attempts: Int = 0, val error: String = "", val finishedAt: String? = null)

val AgentUsageTotals.totalTokenCount: Long get() = promptTokens + completionTokens + cacheReadTokens + cacheWriteTokens + audioInputTokens

enum class AgentUsageGrouping(val serverName: String) { Day("day"), Kind("kind"), Model("model"), Agent("agent") }

/** Everybody's agents, their usage added up by what is asked, and the jobs given up on. */
data class AgentAdminOverview(val agents: List<AgentAdminSummary>, val usageRows: List<AgentUsageRow>, val deadJobs: List<AgentDeadJob>) {
    val isBilledShown: Boolean get() = usageRows.any { row -> row.billedAmount != null }

    fun username(agentId: String): String = agents.firstOrNull { agent -> agent.agentId == agentId }?.username ?: agentId
}

@Serializable private class ServerStatusAnswer(val GetServerStatus: ServerStatus)

@Serializable private class ServerUpgradeAnswer(val GetUpgrade: ServerUpgrade)

@Serializable private class AdminOverviewAnswer(val ListAgents: List<AgentAdminSummary?> = emptyList(), val AgentServerUsage: List<AgentUsageRow?> = emptyList(), val ListAgentDeadLetters: List<AgentDeadJob?> = emptyList())

suspend fun Connection.serverStatus(): ServerStatus = execute<ServerStatusAnswer>(ManageServerDocuments.MANAGE_SERVER_STATUS).GetServerStatus

/** Asks the instance to restart. It answers and then goes away, so a connection that closes mid-reply is no failure. */
suspend fun Connection.restartServer() {
    execute<JsonObject>(ManageServerDocuments.MANAGE_RESTART_SERVER)
}

suspend fun Connection.serverUpgrade(isChecking: Boolean = false): ServerUpgrade =
    execute<ServerUpgradeAnswer>(ManageServerDocuments.MANAGE_SERVER_UPGRADE, variablesOf("check" to isChecking)).GetUpgrade

/** Starts an upgrade to the version shown, so it installs what the operator agreed to rather than whatever is newest by then. */
suspend fun Connection.applyServerUpgrade(version: String?) {
    execute<JsonObject>(ManageServerDocuments.MANAGE_APPLY_UPGRADE, variablesOf("version" to version))
}

/** The server's settings, section by section, as `GetSettings` answers them. */
suspend fun Connection.serverSettings(): JsonObject = (execute<JsonObject>(ManageServerDocuments.MANAGE_SERVER_SETTINGS)["GetSettings"] as? JsonObject) ?: JsonObject(emptyMap())

/** Changes one section of the server's settings, and answers with everything now stored. */
suspend fun Connection.updateServerSettingsSection(section: String, changes: JsonObject): JsonObject =
    (execute<JsonObject>(ManageServerDocuments.MANAGE_UPDATE_SERVER_SETTINGS, mapOf(section to changes))["UpdateSettings"] as? JsonObject) ?: JsonObject(emptyMap())

/** Every agent, usage over a range added up by what is asked, and the jobs given up on. */
suspend fun Connection.agentAdminOverview(grouping: AgentUsageGrouping, sinceText: String, untilText: String): AgentAdminOverview {
    val answer = execute<AdminOverviewAnswer>(ManageServerDocuments.MANAGE_AGENT_ADMIN, variablesOf("by" to grouping.serverName, "since" to sinceText, "until" to untilText))
    return AgentAdminOverview(answer.ListAgents.filterNotNull(), answer.AgentServerUsage.filterNotNull(), answer.ListAgentDeadLetters.filterNotNull())
}

/** The bounds of "the last so many days" as the server takes them: UTC instants at day starts, the end being the start of tomorrow. */
fun usageBounds(days: Int, now: java.time.Instant = java.time.Instant.now(), zone: java.time.ZoneId = java.time.ZoneId.systemDefault()): Pair<String, String> {
    val today = now.atZone(zone).toLocalDate()
    val since = today.minusDays(days.toLong()).atStartOfDay(zone).toInstant()
    val until = today.plusDays(1).atStartOfDay(zone).toInstant()
    return ServerTime.text(since) to ServerTime.text(until)
}
