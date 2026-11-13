package com.teanode.kit.agent

import com.teanode.kit.Connection
import com.teanode.kit.settings.trimmedText
import com.teanode.kit.settings.variablesOf
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

/** How much of today's budget is used: tokens and money, each with a limit where zero is no limit. */
@Serializable
data class AgentBudget(
    val used: Double = 0.0,
    val limit: Double = 0.0,
    val resetsAt: String? = null,
    val cost: Double = 0.0,
    val costLimit: Double = 0.0,
    val currency: String? = null,
) {
    /** How much of the day's budget is used, by whichever limit is closer to running out; null with no limit. */
    val fraction: Double?
        get() = listOfNotNull(if (limit > 0) used / limit else null, if (costLimit > 0) cost / costLimit else null).maxOrNull()
}

/** Voice is how the agent sounds when it writes for the person. */
@Serializable
data class AgentVoice(
    /** formal or casual; empty is neutral. */
    val tone: String? = null,
    /** short or long; empty is medium. */
    val length: String? = null,
    val greeting: String? = null,
    val signoff: String? = null,
)

/** One of the person's own categories, with the line that says what belongs in it. */
@Serializable
data class AgentCategory(val name: String = "", val description: String? = "")

/** The agent's own settings; there is none until the person turns it on. */
@Serializable
data class AgentRecord(
    val id: String,
    val name: String,
    val enabled: Boolean = false,
    val instructions: String? = null,
    /** The language it talks in, and the one it works in; empty follows the account. */
    val language: String? = null,
    val knowledgeLanguage: String? = null,
    /** The model for conversations; empty is the server's choice. */
    val askModel: String? = null,
    /** The voice answers are read aloud in on a call; empty is the server's. */
    val speechVoice: String? = null,
    /** When an operator switched it off; while set, the person cannot turn it on. */
    val operatorDisabledAt: String? = null,
    /** The families and tools that always ask first. */
    val confirm: List<String> = emptyList(),
    /** The kinds of action needing the person's word that the agent may take when they are not there. */
    val unattendedAllowedRisks: List<String>? = null,
    val isMemoryCheckEnabled: Boolean = false,
    val isIdeasEnabled: Boolean = false,
    val voice: AgentVoice? = null,
    val categories: List<AgentCategory> = emptyList(),
) {
    val isOperatorDisabled: Boolean get() = !operatorDisabledAt.isNullOrEmpty()
}

/** What the agent does in one mailbox: the sorting. */
@Serializable
data class MailboxTriage(
    val enabled: Boolean = false,
    /** none, recent or all; empty is recent. */
    val backfill: String? = null,
    /** direct or any; empty is direct. */
    val replyExpectation: String? = null,
)

/** The summarizing. */
@Serializable
data class MailboxSummaries(
    val enabled: Boolean = false,
    /** How long a conversation is before it gets a summary; zero is three. */
    val minimumMessages: Int? = null,
    /** brief or detailed; empty is brief. */
    val style: String? = null,
)

/** The person's working hours, as "HH:MM", on the days given (0 is Sunday). */
@Serializable
data class MailboxHours(val from: String, val until: String, val days: List<Int>) {
    companion object {
        /** What answering outside hours starts from: nine to half past five, Monday to Friday. */
        val workingHours = MailboxHours("09:00", "17:30", listOf(1, 2, 3, 4, 5))
    }
}

/** The answering on the person's behalf. */
@Serializable
data class MailboxAutoReply(
    val enabled: Boolean = false,
    val guidance: String? = null,
    /** known, everyone or list; empty is known. */
    val scope: String? = null,
    val allow: List<String>? = null,
    val never: List<String>? = null,
    /** Answer only in these categories; empty is any. */
    val categories: List<String>? = null,
    /** always, outsideHours or whenAway; empty is always. */
    val `when`: String? = null,
    val hours: MailboxHours? = null,
    /** How long a reply waits in Drafts; zero is ten. */
    val holdMinutes: Int? = null,
    /** The most replies in a day; zero is twenty. */
    val dailyLimit: Int? = null,
)

/** Whether what arrives may be told to the person unasked. */
@Serializable
data class MailboxAlerts(val enabled: Boolean)

/** What the agent does in one mailbox. */
@Serializable
data class MailboxPolicy(
    val granted: Boolean = false,
    val triage: MailboxTriage? = null,
    val summaries: MailboxSummaries? = null,
    val draftReplies: Boolean = false,
    val search: Boolean = false,
    val research: Boolean = false,
    val autoReply: MailboxAutoReply? = null,
    val alerts: MailboxAlerts? = null,
) {
    /** Alerts follow the sorting; a policy that never said is on. */
    val isAlertsEnabled: Boolean get() = alerts?.enabled ?: true

    /** The policy as the server takes it: granted, and the answering dropped when nothing in it is set, with working hours only when it answers outside them. */
    val cleaned: MailboxPolicy
        get() {
            val reply = autoReply ?: MailboxAutoReply()
            val keptReply = if (reply.enabled || !reply.guidance.isNullOrEmpty()) {
                reply.copy(hours = if (reply.`when` == "outsideHours") reply.hours ?: MailboxHours.workingHours else null)
            } else {
                null
            }
            return copy(granted = true, autoReply = keptReply)
        }

    val variables: JsonElement
        get() {
            val fields = mutableMapOf<String, JsonElement>(
                "granted" to JsonPrimitive(granted), "draftReplies" to JsonPrimitive(draftReplies), "search" to JsonPrimitive(search), "research" to JsonPrimitive(research),
            )
            triage?.let { found ->
                fields["triage"] = JsonObject(mapOf("enabled" to JsonPrimitive(found.enabled), "backfill" to JsonPrimitive(found.backfill ?: ""), "replyExpectation" to JsonPrimitive(found.replyExpectation ?: "")))
            }
            summaries?.let { found ->
                fields["summaries"] = JsonObject(mapOf("enabled" to JsonPrimitive(found.enabled), "minimumMessages" to JsonPrimitive(found.minimumMessages ?: 0), "style" to JsonPrimitive(found.style ?: "")))
            }
            autoReply?.let { reply ->
                fields["autoReply"] = JsonObject(
                    mapOf(
                        "enabled" to JsonPrimitive(reply.enabled), "guidance" to JsonPrimitive(reply.guidance ?: ""), "scope" to JsonPrimitive(reply.scope ?: ""),
                        "allow" to strings(reply.allow), "never" to strings(reply.never), "categories" to strings(reply.categories), "when" to JsonPrimitive(reply.`when` ?: ""),
                        "holdMinutes" to JsonPrimitive(reply.holdMinutes ?: 0), "dailyLimit" to JsonPrimitive(reply.dailyLimit ?: 0),
                        "hours" to (reply.hours?.let { hours ->
                            JsonObject(mapOf("from" to JsonPrimitive(hours.from), "until" to JsonPrimitive(hours.until), "days" to JsonArray(hours.days.map { day -> JsonPrimitive(day) })))
                        } ?: JsonNull),
                    ),
                )
            }
            alerts?.let { found -> fields["alerts"] = JsonObject(mapOf("enabled" to JsonPrimitive(found.enabled))) }
            return JsonObject(fields)
        }

    private fun strings(list: List<String>?): JsonElement = JsonArray((list ?: emptyList()).map { entry -> JsonPrimitive(entry) })

    companion object {
        /** The dashboard's policy for a mailbox never granted. */
        val defaultPolicy = MailboxPolicy(granted = false, draftReplies = true, search = true, research = false)
    }
}

/** One of the person's mailboxes, granted or not. */
@Serializable
data class MailboxSource(
    val mailboxId: String,
    val name: String,
    val addresses: List<String> = emptyList(),
    /** Null until the mailbox has been granted once. */
    val policy: MailboxPolicy? = null,
) {
    val isGranted: Boolean get() = policy?.granted == true

    /** The policy the forms start from: the stored one, or the dashboard's default. */
    val shownPolicy: MailboxPolicy get() = policy ?: MailboxPolicy.defaultPolicy
}

/** One of the person's calendars or address books: one switch, no policy. */
@Serializable
data class CollectionSource(
    val id: String,
    val name: String,
    /** calendar or addressBook. */
    val kind: String,
    val granted: Boolean = false,
    /** Events in the next month for a calendar, people for an address book. */
    val items: Int = 0,
) {
    val isCalendar: Boolean get() = kind == "calendar"
}

/** What the server offers, resolved against what is configured. */
@Serializable
data class AgentAllowed(
    val enabled: Boolean = false,
    val triage: Boolean = false,
    val summaries: Boolean = false,
    val draftReplies: Boolean = false,
    val search: Boolean = false,
    val research: Boolean = false,
    val autoReply: Boolean = false,
    val ask: Boolean = false,
    val schedules: Boolean = false,
    val browser: Boolean = false,
    val connectedServers: Boolean = false,
)

/** A language the agent may talk or work in, named in itself. */
data class LanguageChoice(val code: String, val name: String)

/**
 * The person's agent as the dashboard's agent page reads it: the agent's own settings, every mailbox, calendar and
 * address book it may be given, what the server offers, and today's budget. What the server left out is read as
 * empty, so a server that answers with less is read as offering less.
 */
@Serializable
data class AgentSettings(
    val agent: AgentRecord? = null,
    val sources: List<MailboxSource> = emptyList(),
    val collections: List<CollectionSource> = emptyList(),
    val allowed: AgentAllowed = AgentAllowed(),
    val budget: AgentBudget? = null,
    /** The models the person may pick for conversations. */
    val choices: List<String> = emptyList(),
    /** The zone and language the agent resolves for the person, and the language its notes are written in. */
    val timezone: String = "",
    val language: String = "",
    val knowledgeLanguage: String = "",
    /** The fixed categories followed by the person's own. */
    val categories: List<String> = emptyList(),
) {
    /** The fixed categories, the first few, as the dashboard names them in its hint. */
    val fixedCategoryNames: List<String> get() = categories.take(NAMED_FIXED_CATEGORY_COUNT)

    /** One mailbox, by its id. */
    fun source(mailboxId: String): MailboxSource? = sources.firstOrNull { source -> source.mailboxId == mailboxId }

    companion object {
        /** The kinds of action needing the person's word, in the order the settings show them. */
        val unattendedRisks = listOf("outward", "money", "destructive", "granting", "listed")

        /** The languages offered by name; any other code may be typed. */
        val languages: List<LanguageChoice> = listOf(
            LanguageChoice("en", "English"), LanguageChoice("de", "Deutsch"), LanguageChoice("es", "Español"), LanguageChoice("fr", "Français"),
            LanguageChoice("it", "Italiano"), LanguageChoice("ja", "日本語"), LanguageChoice("ko", "한국어"), LanguageChoice("nl", "Nederlands"),
            LanguageChoice("pt", "Português"), LanguageChoice("ru", "Русский"), LanguageChoice("zh", "中文"),
        )

        /** How many of the fixed categories the dashboard names in its hint. */
        const val NAMED_FIXED_CATEGORY_COUNT = 9

        /** The longest name a category may have. */
        const val CATEGORY_NAME_LENGTH = 40

        /** The longest name the agent may have. */
        const val AGENT_NAME_LENGTH = 64

        /** A list typed with commas or one to a line, as the server takes it. */
        fun splitList(text: String): List<String> = text.split(',', '\n').map { entry -> entry.trim(' ', '\t') }.filter { entry -> entry.isNotEmpty() }

        /** A list as it is typed. */
        fun joinList(entries: List<String>?): String = (entries ?: emptyList()).joinToString(", ")

        /** A number as it is typed; anything else, and nothing, is zero, which the server reads as its default. */
        fun wholeNumber(text: String): Int = maxOf(0, text.trim().toIntOrNull() ?: 0)

        /** The hour and minute of an "HH:MM" time; null for anything else. */
        fun clockParts(text: String): Pair<Int, Int>? {
            val parts = text.split(':')
            if (parts.size != 2) return null
            val hour = parts[0].toIntOrNull() ?: return null
            val minute = parts[1].toIntOrNull() ?: return null
            return if (hour in 0..23 && minute in 0..59) hour to minute else null
        }

        /** An hour and minute as "HH:MM". */
        fun clockText(hour: Int, minute: Int): String = "${if (hour < 10) "0$hour" else "$hour"}:${if (minute < 10) "0$minute" else "$minute"}"
    }
}

/** What one save changes about the agent; what is left null stays as it is. */
data class AgentChange(
    val enabled: Boolean? = null,
    val name: String? = null,
    val instructions: String? = null,
    val language: String? = null,
    val knowledgeLanguage: String? = null,
    val voice: AgentVoice? = null,
    val categories: List<AgentCategory>? = null,
    val confirm: List<String>? = null,
    val unattendedAllowedRisks: List<String>? = null,
    val askModel: String? = null,
    val speechVoice: String? = null,
    val isMemoryCheckEnabled: Boolean? = null,
    val isIdeasEnabled: Boolean? = null,
) {
    /** As the server takes it, as the dashboard sends it: the name and the languages trimmed, every part of the voice said, and categories without a name left out. */
    val variables: Map<String, JsonElement>
        get() {
            val variables = mutableMapOf<String, JsonElement>()
            enabled?.let { variables["enabled"] = JsonPrimitive(it) }
            name?.let { variables["name"] = JsonPrimitive(trimmedText(it)) }
            instructions?.let { variables["instructions"] = JsonPrimitive(it) }
            language?.let { variables["language"] = JsonPrimitive(trimmedText(it)) }
            knowledgeLanguage?.let { variables["knowledgeLanguage"] = JsonPrimitive(trimmedText(it)) }
            voice?.let { found ->
                variables["voice"] = JsonObject(
                    mapOf("tone" to JsonPrimitive(found.tone ?: ""), "length" to JsonPrimitive(found.length ?: ""), "greeting" to JsonPrimitive(found.greeting ?: ""), "signoff" to JsonPrimitive(found.signoff ?: "")),
                )
            }
            categories?.let { list ->
                variables["categories"] = JsonArray(
                    list.filter { category -> trimmedText(category.name).isNotEmpty() }.map { category ->
                        JsonObject(mapOf("name" to JsonPrimitive(category.name), "description" to JsonPrimitive(category.description ?: "")))
                    },
                )
            }
            confirm?.let { variables["confirm"] = JsonArray(it.map { entry -> JsonPrimitive(entry) }) }
            unattendedAllowedRisks?.let { variables["unattendedAllowedRisks"] = JsonArray(it.map { entry -> JsonPrimitive(entry) }) }
            askModel?.let { variables["askModel"] = JsonPrimitive(it) }
            speechVoice?.let { variables["speechVoice"] = JsonPrimitive(it) }
            isMemoryCheckEnabled?.let { variables["isMemoryCheckEnabled"] = JsonPrimitive(it) }
            isIdeasEnabled?.let { variables["isIdeasEnabled"] = JsonPrimitive(it) }
            return variables
        }
}

/** How the agent writes, as its form holds it. */
data class AgentVoiceDraft(val tone: String = "", val length: String = "", val greeting: String = "", val signoff: String = "") {
    val voice: AgentVoice get() = AgentVoice(tone, length, greeting, signoff)

    companion object {
        fun of(voice: AgentVoice?): AgentVoiceDraft = AgentVoiceDraft(voice?.tone ?: "", voice?.length ?: "", voice?.greeting ?: "", voice?.signoff ?: "")

        /** The tones, the first being neutral. */
        val tones = listOf("", "formal", "casual")

        /** The lengths, the first being medium. */
        val lengths = listOf("", "short", "long")
    }
}

/** One tool the agent may use, as the "ask me first" list shows it. */
@Serializable
data class AgentTool(
    val name: String,
    val family: String,
    val risk: String = "",
    val description: String = "",
    /** Whether it asks every time, whatever the list says. */
    val confirms: Boolean = false,
    val core: Boolean = false,
    /** The verbs a tool takes, for the tools that are one thing with several. */
    val actions: List<String>? = null,
) {
    companion object {
        /** The families, in the order their tools first come. */
        fun families(tools: List<AgentTool>): List<String> = tools.map { tool -> tool.family }.distinct()
    }
}

/** The "ask me first" list as its form holds it: the names of the families and tools that must ask before they run. A tool that is not named follows its family. */
data class AgentConfirmDraft(val askFirstNames: List<String>) {
    fun isAskingFirst(name: String): Boolean = askFirstNames.contains(name)

    fun settingAskingFirst(name: String, isAskingFirst: Boolean): AgentConfirmDraft =
        AgentConfirmDraft(if (isAskingFirst) (if (askFirstNames.contains(name)) askFirstNames else askFirstNames + name) else askFirstNames.filter { entry -> entry != name })

    /** Whether a tool asks first, by its own name or its family's. */
    fun doesToolAskFirst(tool: AgentTool): Boolean = isAskingFirst(tool.name) || isAskingFirst(tool.family)

    companion object {
        fun of(confirm: List<String>): AgentConfirmDraft = AgentConfirmDraft(confirm.distinct())
    }
}

/** Which kinds of action the agent may take when the person is not there, as its form holds it. */
data class AgentUnattendedDraft(val allowedRisks: Set<String>) {
    fun isAllowed(unattendedRisk: String): Boolean = allowedRisks.contains(unattendedRisk)

    fun setting(unattendedRisk: String, isAllowed: Boolean): AgentUnattendedDraft =
        AgentUnattendedDraft(if (isAllowed) allowedRisks + unattendedRisk else allowedRisks - unattendedRisk)

    /** As the server takes them, in the order the settings show them. */
    val orderedAllowedRisks: List<String> get() = AgentSettings.unattendedRisks.filter { risk -> allowedRisks.contains(risk) }

    companion object {
        fun of(agent: AgentRecord?): AgentUnattendedDraft = AgentUnattendedDraft((agent?.unattendedAllowedRisks ?: emptyList()).toSet())
    }
}

/** Whether the server speaks, and the voices the person may choose. */
@Serializable
data class VoiceAvailability(
    val isVoiceAvailable: Boolean = false,
    val sampleRate: Int? = null,
    /** Whether the server can prepare a recording sent as a voice message in a chat. */
    val isVoiceMessageAvailable: Boolean? = null,
    /** The voice answers are read in: the person's choice, or the server's. */
    val speechVoice: String? = null,
    /** The voices the person may choose. */
    val speechVoices: List<String>? = null,
    /** The server's voice, which an empty choice means. */
    val serverSpeechVoice: String? = null,
)

@Serializable private class ReadAgentAnswer(val ReadAgent: AgentSettings)

@Serializable private class UpdateAgentAnswer(val UpdateAgent: AgentSettings)

@Serializable private class GrantAgentMailboxAnswer(val GrantAgentMailbox: AgentSettings)

@Serializable private class RevokeAgentMailboxAnswer(val RevokeAgentMailbox: AgentSettings)

@Serializable private class GrantAgentSourceAnswer(val GrantAgentSource: AgentSettings)

@Serializable private class ListAgentToolsAnswer(val ListAgentTools: List<AgentTool>)

@Serializable private class ReadAgentVoiceAnswer(val ReadAgentVoice: VoiceAvailability)

/** The agent and everything its settings show. */
suspend fun Connection.agentSettings(): AgentSettings = execute<ReadAgentAnswer>(AgentSettingsDocuments.AGENT_SETTINGS).ReadAgent

/** Changes the agent, making it if there is none, and answers with the settings as they are now. */
suspend fun Connection.updateAgent(change: AgentChange): AgentSettings =
    execute<UpdateAgentAnswer>(AgentSettingsDocuments.UPDATE_AGENT_SETTINGS, change.variables).UpdateAgent

/** Deletes the agent and everything it learned; the mail stays. */
suspend fun Connection.forgetAgent(): AgentSettings =
    execute<UpdateAgentAnswer>(AgentSettingsDocuments.UPDATE_AGENT_SETTINGS, variablesOf("forget" to true)).UpdateAgent

/** Lets the agent reach a mailbox. With no policy the server sets its default the first time and keeps the stored one after that; with one, it replaces the stored one. */
suspend fun Connection.grantAgentMailbox(mailboxId: String, policy: MailboxPolicy? = null): AgentSettings {
    val variables = mutableMapOf<String, JsonElement>("mailboxId" to JsonPrimitive(mailboxId))
    policy?.let { found -> variables["policy"] = found.cleaned.variables }
    return execute<GrantAgentMailboxAnswer>(AgentSettingsDocuments.GRANT_AGENT_MAILBOX, variables).GrantAgentMailbox
}

/** Stops the agent reaching a mailbox: its queued work is cancelled, what it sorted stays. */
suspend fun Connection.revokeAgentMailbox(mailboxId: String): AgentSettings =
    execute<RevokeAgentMailboxAnswer>(AgentSettingsDocuments.REVOKE_AGENT_MAILBOX, variablesOf("mailboxId" to mailboxId)).RevokeAgentMailbox

/** Lets the agent read a calendar or an address book, or stops it. */
suspend fun Connection.grantAgentCollection(collection: CollectionSource, isGranted: Boolean): AgentSettings =
    execute<GrantAgentSourceAnswer>(AgentSettingsDocuments.GRANT_AGENT_COLLECTION, variablesOf("kind" to collection.kind, "id" to collection.id, "granted" to isGranted)).GrantAgentSource

/** Every tool the agent may use, for the "ask me first" list. */
suspend fun Connection.agentTools(): List<AgentTool> = execute<ListAgentToolsAnswer>(AgentSettingsDocuments.AGENT_TOOLS).ListAgentTools

/** Whether the server speaks, and the voices the person may choose. */
suspend fun Connection.speechVoices(): VoiceAvailability = execute<ReadAgentVoiceAnswer>(AgentSettingsDocuments.SPEECH_VOICES).ReadAgentVoice
