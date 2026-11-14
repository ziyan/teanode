package com.teanode.kit.agent

import com.teanode.kit.Connection
import com.teanode.kit.finance.deleteFinanceSource
import com.teanode.kit.finance.switchFinanceSource
import com.teanode.kit.finance.syncFinanceSource
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.settings.variablesOf
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull

/** A source the agent reads: a folder, a mailbox, a repository, a feed; with what it found and how it last went. */
@Serializable
data class KnowledgeSourceRow(
    val id: String,
    val kind: String = "",
    val name: String,
    /** Where in the graph what it finds is filed. */
    val rootPath: String = "",
    val enabled: Boolean = true,
    val cron: String = "",
    val lastRunAt: String? = null,
    val lastError: String = "",
    val documentCount: Int = 0,
    val chunkCount: Int = 0,
    val refusedCount: Int = 0,
    /** Whether the last pass left work behind and wants another. */
    val more: Boolean = false,
    val specification: KnowledgeSourceSpecification = KnowledgeSourceSpecification(),
)

/** What a source reads and how: a type with its settings, or a path on a computer. */
@Serializable
data class KnowledgeSourceSpecification(
    val type: String = "",
    val settings: JsonElement? = null,
    val computer: String = "",
    val path: String = "",
    val format: String = "",
    val mailboxId: String = "",
    val readEveryCheckout: Boolean = false,
) {
    /** The stored settings as an object, whether the server sent them as one or as a JSON text. */
    val storedSettings: Map<String, JsonElement>
        get() = when (val stored = settings) {
            is JsonObject -> stored
            is JsonPrimitive -> if (stored.isString) runCatching { TeaNodeJson.parseToJsonElement(stored.content) as? JsonObject }.getOrNull() ?: emptyMap() else emptyMap()
            else -> emptyMap()
        }
}

/** How many files of a source the agent has not decided about, declined, or described. */
@Serializable
data class KnowledgeSourceAttachments(val sourceId: String, val undecided: Int = 0, val declined: Int = 0, val described: Int = 0)

/** The sources and what is waiting in each. */
data class KnowledgeSourceDetails(val sources: List<KnowledgeSourceRow>, val attachments: List<KnowledgeSourceAttachments>)

@Serializable private class KnowledgeSourceDetailsAnswer(val ListAgentKnowledgeSources: List<KnowledgeSourceRow> = emptyList(), val ListAgentSourceAttachments: List<KnowledgeSourceAttachments> = emptyList())

suspend fun Connection.knowledgeSourceDetails(): KnowledgeSourceDetails {
    val answer = execute<KnowledgeSourceDetailsAnswer>(AgentUpkeepDocuments.KNOWLEDGE_SOURCE_DETAILS)
    return KnowledgeSourceDetails(answer.ListAgentKnowledgeSources, answer.ListAgentSourceAttachments)
}

/** Reads a source again now. */
suspend fun Connection.syncKnowledgeSource(sourceId: String) = syncFinanceSource(sourceId)

/** Pauses reading a source, or reads it again; what it found is kept. */
suspend fun Connection.switchKnowledgeSource(sourceId: String, isEnabled: Boolean) = switchFinanceSource(sourceId, isEnabled)

/** Stops reading a source and forgets the documents it found. */
suspend fun Connection.deleteKnowledgeSource(sourceId: String) = deleteFinanceSource(sourceId)

/** A kind of source the server can read, with the settings it takes and the secrets it keeps sealed. */
@Serializable
data class KnowledgeSourceType(
    val name: String,
    val description: String = "",
    val isLocal: Boolean = false,
    val reader: String = "",
    val requires: List<String> = emptyList(),
    val readable: Boolean = true,
    val problem: String? = null,
    val settings: List<Setting> = emptyList(),
    val secrets: List<Secret> = emptyList(),
) {
    @Serializable
    data class Setting(
        val name: String,
        val description: String = "",
        val settingType: String = "string",
        val pattern: String = "",
        val itemPattern: String = "",
        @kotlinx.serialization.SerialName("default") val defaultValue: JsonElement? = null,
        val isRequired: Boolean = false,
        val minimum: Int? = null,
        val maximum: Int? = null,
    ) {
        val isBoolean: Boolean get() = settingType == "boolean"

        /** The words a pattern of the form `^(a|b|c)$` allows, when it is one. */
        val choices: List<String>? get() = choicesIn(pattern)

        /** What is wrong with the typed value, or null. */
        fun problem(draft: Draft): SettingProblem? {
            if (isBoolean) return null
            val text = (draft.texts[name] ?: "").trim()
            if (text.isEmpty()) return if (isRequired) SettingProblem.Missing else null
            if (settingType == "integer") {
                val number = text.toIntOrNull() ?: return SettingProblem.NotWhole
                if (minimum != null && number < minimum) return SettingProblem.TooSmall(minimum)
                if (maximum != null && number > maximum) return SettingProblem.TooLarge(maximum)
                return null
            }
            if (settingType == "array") return listOfText(text).firstOrNull { item -> !matches(itemPattern, item) }?.let { item -> SettingProblem.ItemRefused(item) }
            return if (matches(pattern, text)) null else SettingProblem.Refused
        }
    }

    @Serializable
    data class Secret(val key: String, val description: String = "", val isOptional: Boolean = false)

    sealed interface SettingProblem {
        data object Missing : SettingProblem
        data object NotWhole : SettingProblem
        data class TooSmall(val minimum: Int) : SettingProblem
        data class TooLarge(val maximum: Int) : SettingProblem
        data class ItemRefused(val item: String) : SettingProblem
        data object Refused : SettingProblem
    }

    /** What the form holds: each text setting as typed, each switch as set. */
    data class Draft(val texts: Map<String, String> = emptyMap(), val switches: Map<String, Boolean> = emptyMap())

    val isReadOnComputer: Boolean get() = reader == "files" || reader == "journal" || reader.isEmpty()

    /** A form's starting point: each setting as stored, or its default for a switch. */
    fun draft(stored: Map<String, JsonElement> = emptyMap()): Draft {
        val texts = mutableMapOf<String, String>()
        val switches = mutableMapOf<String, Boolean>()
        for (setting in settings) {
            val storedValue = stored[setting.name]
            if (setting.isBoolean) {
                switches[setting.name] = (storedValue as? JsonPrimitive)?.booleanOrNull ?: ((setting.defaultValue as? JsonPrimitive)?.booleanOrNull == true)
            } else {
                texts[setting.name] = text(storedValue)
            }
        }
        return Draft(texts, switches)
    }

    /** The first setting with something wrong with it, skipping some. */
    fun firstProblem(draft: Draft, skipping: Set<String> = emptySet()): Pair<Setting, SettingProblem>? {
        for (setting in settings) {
            if (skipping.contains(setting.name)) continue
            setting.problem(draft)?.let { problem -> return setting to problem }
        }
        return null
    }

    /** The settings as they are sent: switches as booleans, whole numbers as numbers, lists as arrays, the rest as text; what is empty is left out. */
    fun values(draft: Draft): Map<String, JsonElement> {
        val values = linkedMapOf<String, JsonElement>()
        for (setting in settings) {
            if (setting.isBoolean) {
                values[setting.name] = JsonPrimitive(draft.switches[setting.name] == true)
                continue
            }
            val typed = (draft.texts[setting.name] ?: "").trim()
            if (typed.isEmpty()) continue
            values[setting.name] = when (setting.settingType) {
                "integer" -> typed.toIntOrNull()?.let { number -> JsonPrimitive(number) } ?: JsonPrimitive(typed)
                "array" -> JsonArray(listOfText(typed).map { item -> JsonPrimitive(item) })
                else -> JsonPrimitive(typed)
            }
        }
        return values
    }

    companion object {
        private val builtInReaders = listOf("files", "journal", "sent", "web")

        /** The types a person can add, the built-in readers first. */
        fun offered(types: List<KnowledgeSourceType>): List<KnowledgeSourceType> {
            fun rank(type: KnowledgeSourceType): Int = builtInReaders.indexOf(type.reader).let { index -> if (index < 0) builtInReaders.size else index }
            return types.filter { type -> type.readable }.sortedWith(compareBy<KnowledgeSourceType> { type -> rank(type) }.thenBy(String.CASE_INSENSITIVE_ORDER) { type -> type.name })
        }

        fun text(value: JsonElement?): String = when (value) {
            null, is JsonNull -> ""
            is JsonPrimitive -> if (value.isString) value.content else if (value.booleanOrNull != null) value.content else value.content.removeSuffix(".0")
            is JsonArray -> value.joinToString(", ") { item -> text(item) }
            is JsonObject -> value.toString()
        }

        fun listOfText(typed: String): List<String> = typed.split(',').map { item -> item.trim() }.filter { item -> item.isNotEmpty() }

        fun matches(pattern: String, typed: String): Boolean {
            if (pattern.isEmpty()) return true
            val expression = runCatching { Regex(pattern) }.getOrNull() ?: return true
            return expression.containsMatchIn(typed)
        }

        private val acronyms = setOf("url", "id", "api", "cql", "mb")

        /** A setting's name as a label: `baseUrl` is "Base URL". */
        fun label(name: String): String {
            val words = mutableListOf<String>()
            var current = ""
            name.forEachIndexed { index, character ->
                val previous = name.getOrNull(index - 1)
                val next = name.getOrNull(index + 1)
                val startsWord = character.isUpperCase() && ((previous != null && (previous.isLowerCase() || previous.isDigit())) || (previous != null && previous.isUpperCase() && next != null && next.isLowerCase()))
                if (startsWord && current.isNotEmpty()) {
                    words.add(current)
                    current = ""
                }
                current += character
            }
            if (current.isNotEmpty()) words.add(current)
            val shown = words.mapIndexed { index, word ->
                when {
                    acronyms.contains(word.lowercase()) -> word.uppercase()
                    index > 0 && !(word.length >= 2 && word.all { character -> character.isUpperCase() }) -> word.lowercase()
                    else -> word
                }
            }
            return shown.joinToString(" ").replaceFirstChar { character -> character.uppercase() }
        }

        /** A description as a sentence: capital at the front, a stop at the end. */
        fun asSentence(description: String): String {
            val capitalized = description.trim().replaceFirstChar { character -> character.uppercase() }
            val last = capitalized.lastOrNull() ?: return capitalized
            return if (".!?)".contains(last)) capitalized else "$capitalized."
        }

        fun choicesIn(pattern: String): List<String>? {
            if (!pattern.startsWith("^(") || !pattern.endsWith(")$")) return null
            val words = pattern.drop(2).dropLast(2).split('|')
            if (words.size < 2 || !words.all { word -> word.isNotEmpty() && word.all { character -> character.isLetterOrDigit() && character.code < 128 || character == '_' || character == '-' } }) return null
            return words
        }
    }
}

/** Which of a source's sealed values are kept. The values themselves are never read back. */
@Serializable
data class KnowledgeSourceSecret(val key: String, val isSet: Boolean = false)

/** What a save of a source changes; what is left null stays. */
data class KnowledgeSourceChange(
    val sourceId: String? = null, val name: String? = null, val kind: String? = null, val computer: String? = null, val path: String? = null, val format: String? = null,
    val enabled: Boolean? = null, val mailboxId: String? = null, val rootPath: String? = null, val cron: String? = null, val readEveryCheckout: Boolean? = null,
    val type: String? = null, val settings: Map<String, JsonElement>? = null,
) {
    val variables: Map<String, JsonElement>
        get() = buildMap {
            sourceId?.let { put("sourceId", JsonPrimitive(it)) }
            name?.let { put("name", JsonPrimitive(it)) }
            kind?.let { put("kind", JsonPrimitive(it)) }
            computer?.let { put("computer", JsonPrimitive(it)) }
            path?.let { put("path", JsonPrimitive(it)) }
            format?.let { put("format", JsonPrimitive(it)) }
            mailboxId?.let { put("mailboxId", JsonPrimitive(it)) }
            rootPath?.let { put("rootPath", JsonPrimitive(it)) }
            cron?.let { put("cron", JsonPrimitive(it)) }
            type?.let { put("type", JsonPrimitive(it)) }
            enabled?.let { put("enabled", JsonPrimitive(it)) }
            readEveryCheckout?.let { put("readEveryCheckout", JsonPrimitive(it)) }
            settings?.let { put("settings", JsonObject(it)) }
        }
}

/** A computer attached to the agent. */
@Serializable
data class AttachedComputer(val name: String, val description: String? = null)

@Serializable private class ListSourceTypesAnswer(val ListAgentSourceTypes: List<KnowledgeSourceType> = emptyList())

@Serializable private class SavedKnowledgeSource(val id: String)

@Serializable private class SaveKnowledgeSourceAnswer(val SaveAgentKnowledgeSource: SavedKnowledgeSource)

@Serializable private class ListSourceSecretsAnswer(val ListAgentKnowledgeSourceSecrets: List<KnowledgeSourceSecret?> = emptyList())

@Serializable private class Computers(val computers: List<AttachedComputer> = emptyList())

@Serializable private class ReadComputersAnswer(val ReadAgentComputers: Computers? = null)

suspend fun Connection.knowledgeSourceTypes(): List<KnowledgeSourceType> = execute<ListSourceTypesAnswer>(AgentUpkeepDocuments.KNOWLEDGE_SOURCE_TYPES).ListAgentSourceTypes

/** Saves a source and answers its id. */
suspend fun Connection.saveKnowledgeSource(change: KnowledgeSourceChange): String =
    execute<SaveKnowledgeSourceAnswer>(AgentUpkeepDocuments.SAVE_KNOWLEDGE_SOURCE, change.variables).SaveAgentKnowledgeSource.id

suspend fun Connection.knowledgeSourceSecrets(sourceId: String): List<KnowledgeSourceSecret> =
    execute<ListSourceSecretsAnswer>(AgentUpkeepDocuments.KNOWLEDGE_SOURCE_SECRETS, variablesOf("sourceId" to sourceId)).ListAgentKnowledgeSourceSecrets.filterNotNull()

/** Seals a value on the server, for the computer that reads the source only. It is never shown again. */
suspend fun Connection.setKnowledgeSourceSecret(sourceId: String, key: String, value: String) {
    execute<JsonObject>(AgentUpkeepDocuments.SET_KNOWLEDGE_SOURCE_SECRET, variablesOf("sourceId" to sourceId, "key" to key, "value" to value))
}

suspend fun Connection.attachedComputers(): List<AttachedComputer> = execute<ReadComputersAnswer>(AgentUpkeepDocuments.ATTACHED_COMPUTERS).ReadAgentComputers?.computers ?: emptyList()
