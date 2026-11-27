package com.teanode.kit.manage

import com.teanode.kit.Connection
import com.teanode.kit.access.OperatorAccess
import com.teanode.kit.agent.AgentTool
import com.teanode.kit.settings.jsonOf
import com.teanode.kit.settings.variablesOf
import com.teanode.kit.photos.ServerTime
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import java.time.Instant

// The operator's agent pages: which tabs a person may open, the agent settings as the server stores them, and the drafts
// the forms edit. Ported from the iPhone app's ManageServer.swift.

/** One tab of the dashboard's agents page. */
enum class AgentAdminTab {
    General, Usage, Runs, Models, Tools, Skills, Sources;

    /** Whether it is one of the operator's agent settings, which need server:manage. */
    val isOperatorSettings: Boolean get() = this != Usage && this != Runs
}

/** The agents page's tabs this person may open, in the dashboard's order: the oversight lists need agent:observe or agent:act, the settings server:manage. */
val OperatorAccess.agentAdminTabs: List<AgentAdminTab>
    get() = AgentAdminTab.entries.filter { tab ->
        when {
            tab == AgentAdminTab.Runs -> canObserveAgents
            tab.isOperatorSettings -> canManageServer
            else -> true
        }
    }

/** How a list is typed into one field, as the dashboard's forms take it. */
object SettingsList {
    /** Commas or lines, as the agent's forms take a list. */
    fun lineItems(typed: String): List<String> = typed.split(',', '\n').map { item -> item.trim() }.filter { item -> item.isNotEmpty() }

    /** A list as one field shows it. */
    fun text(items: List<String>?): String = (items ?: emptyList()).joinToString(", ")

    /** What a number field holds, as the dashboard's Number(typed) || 0 reads it. */
    fun number(typed: String): Double = typed.trim().toDoubleOrNull()?.takeIf { number -> number.isFinite() } ?: 0.0

    /** A field's digits only, as the dashboard's numeric fields keep them. */
    fun digits(typed: String): String = typed.filter { character -> character.isDigit() }

    /** A price kept a number while it is typed: a comma for a decimal point, and one point at most. */
    fun price(typed: String): String {
        val kept = typed.replace(',', '.').filter { character -> character.isDigit() || character == '.' }
        val parts = kept.split('.')
        return if (parts.size > 1) parts.first() + "." + parts.drop(1).joinToString("") else kept
    }

    /** A stored number as a field shows it, empty for zero. */
    fun text(number: Double): String {
        if (number == 0.0) return ""
        return if (number == Math.rint(number) && Math.abs(number) < 1e15) number.toLong().toString() else number.toString()
    }

    /** A number as JSON: a whole one is written without a decimal point, as the server's integer fields need. */
    fun numberValue(number: Double): JsonElement = if (number == Math.rint(number) && Math.abs(number) < 1e15) JsonPrimitive(number.toLong()) else JsonPrimitive(number)

    fun listValue(items: List<String>): JsonElement = JsonArray(items.map { item -> JsonPrimitive(item) })
}

/** An object of JSON written as pairs; a list becomes an array and an element stays as it is. */
fun objectOf(vararg pairs: Pair<String, Any?>): JsonObject = JsonObject(pairs.associate { pair -> pair.first to jsonOf(pair.second) })

/** One allowance of a plan and how much of it is used. */
@Serializable
data class AgentPlanWindow(val usedPercent: Int = 0, val windowMinutes: Int = 0, val resetsAt: String? = null) {
    /** Whether its reset has passed since the reading, so the whole allowance is there again. */
    fun hasReset(now: Instant): Boolean = ServerTime.instant(resetsAt)?.let { reset -> !reset.isAfter(now) } ?: false

    /** What is used, as a percentage, nothing once it has reset. */
    fun shownUsedPercent(now: Instant): Int = if (hasReset(now)) 0 else usedPercent.coerceIn(0, 100)
}

@Serializable
data class AgentPlanUsage(val planName: String = "", val observedAt: String = "", val windows: List<AgentPlanWindow> = emptyList())

/** What a model costs per million tokens. An entry that has one has its own price, all zeros meaning free. */
@Serializable
data class AgentModelPricing(
    val input: Double = 0.0, val output: Double = 0.0, val cacheRead: Double = 0.0, val cacheWrite: Double = 0.0, val audioInput: Double = 0.0,
) {
    fun toJson(): JsonObject = objectOf(
        "input" to SettingsList.numberValue(input), "output" to SettingsList.numberValue(output),
        "cacheRead" to SettingsList.numberValue(cacheRead), "cacheWrite" to SettingsList.numberValue(cacheWrite), "audioInput" to SettingsList.numberValue(audioInput),
    )
}

/** One model a provider offers, with the settings of its own. [model] is the id the service knows it by, and what a role stores after provider and a colon. */
@Serializable
data class AgentModelEntry(
    val model: String = "",
    val name: String = "",
    /** The window the operator set, in tokens; zero takes the provider's own figure. */
    val contextWindow: Int = 0,
    /** The provider's own figure, read-only; zero when it reports none. */
    val reportedContextWindow: Int = 0,
    /** The model's own price, or null when it takes the provider's. */
    val pricing: AgentModelPricing? = null,
    val isEnabled: Boolean = true,
    /** The roles that point at it: default, quick, embedding, and so on. */
    val usedFor: List<String> = emptyList(),
) {
    /** What people read: the name, or the id when it has none. */
    val displayName: String get() = name.ifEmpty { model }

    /** As the server takes it: what is read-only (the reported window, the roles) is left out. */
    fun toJson(): JsonObject = objectOf(
        "model" to model, "name" to name, "contextWindow" to contextWindow, "pricing" to pricing?.toJson(), "isEnabled" to isEnabled,
    )
}

/** A model entry as its screen holds it: the numbers as typed. The id is fixed once the entry exists. */
data class AgentModelEntryDraft(
    val model: String, val name: String = "", val contextWindowText: String = "", val inputText: String = "", val outputText: String = "", val cacheReadText: String = "",
    val cacheWriteText: String = "", val audioInputText: String = "", val isEnabled: Boolean = true,
) {
    private val contextWindow: Int get() = contextWindowText.toIntOrNull() ?: 0

    /** The server refuses a window under a thousand tokens, other than none at all. */
    val canSave: Boolean get() = model.trim().isNotEmpty() && (contextWindow == 0 || contextWindow >= AgentModelEntryDraft.MINIMUM_CONTEXT_WINDOW)

    /** Whether every price field is empty, which sends no price of its own so the model takes the provider's. */
    val isPricingEmpty: Boolean get() = listOf(inputText, outputText, cacheReadText, cacheWriteText, audioInputText).all { text -> text.isBlank() }

    val entry: AgentModelEntry
        get() = AgentModelEntry(
            model = model.trim(), name = name.trim(), contextWindow = contextWindow,
            pricing = if (isPricingEmpty) null else AgentModelPricing(
                SettingsList.number(inputText), SettingsList.number(outputText), SettingsList.number(cacheReadText), SettingsList.number(cacheWriteText),
                SettingsList.number(audioInputText),
            ),
            isEnabled = isEnabled,
        )

    companion object {
        const val MINIMUM_CONTEXT_WINDOW = 1000

        fun of(entry: AgentModelEntry): AgentModelEntryDraft {
            val pricing = entry.pricing
            // A price of zero is the model's own (free), so it is shown as zero rather than as empty, which would be sent as unset.
            val inputText = if (pricing == AgentModelPricing()) "0" else SettingsList.text(pricing?.input ?: 0.0)
            return AgentModelEntryDraft(
                model = entry.model, name = entry.name, contextWindowText = if (entry.contextWindow > 0) entry.contextWindow.toString() else "",
                inputText = inputText, outputText = SettingsList.text(pricing?.output ?: 0.0), cacheReadText = SettingsList.text(pricing?.cacheRead ?: 0.0),
                cacheWriteText = SettingsList.text(pricing?.cacheWrite ?: 0.0), audioInputText = SettingsList.text(pricing?.audioInput ?: 0.0), isEnabled = entry.isEnabled,
            )
        }
    }
}

/** A model service, as the operator's list shows it. Its key is never read back. */
@Serializable
data class AgentProviderSettings(
    val name: String = "",
    @SerialName("kind") val providerKind: String = "",
    val baseUrl: String = "",
    val hasApiKey: Boolean = false,
    /** Absent from a server older than the key. */
    val hasBillingApiKey: Boolean? = null,
    val hasRefreshToken: Boolean = false,
    val account: String = "",
    @SerialName("enabled") val isEnabled: Boolean = true,
    /** What the provider offers: the models it has entries for. */
    val models: List<AgentModelEntry> = emptyList(),
    /** What a model without a price of its own costs. */
    val pricingInput: Double = 0.0,
    val pricingOutput: Double = 0.0,
    val pricingCacheRead: Double = 0.0,
    val pricingCacheWrite: Double = 0.0,
    val pricingAudioInput: Double = 0.0,
    val planUsage: AgentPlanUsage? = null,
) {
    val isSignedInKind: Boolean get() = providerKind == SIGNED_IN_KIND

    /** Whether Test can ask it anything: it has a key or a sign-in, or it is of the openai kind, which a local server answers without either. */
    val canBeTested: Boolean get() = hasApiKey || hasRefreshToken || providerKind == "openai"

    /** As the API takes it back: the key blank, which keeps the stored one. */
    val storedParameters: JsonObject
        get() = objectOf(
            "name" to name, "kind" to providerKind, "baseUrl" to baseUrl, "apiKey" to "", "billingApiKey" to "", "enabled" to isEnabled,
            "pricingInput" to SettingsList.numberValue(pricingInput), "pricingOutput" to SettingsList.numberValue(pricingOutput),
            "pricingCacheRead" to SettingsList.numberValue(pricingCacheRead), "pricingCacheWrite" to SettingsList.numberValue(pricingCacheWrite),
            "pricingAudioInput" to SettingsList.numberValue(pricingAudioInput),
        )

    companion object {
        /** The kind that runs on a person's plan and is signed in to rather than given a key. */
        const val SIGNED_IN_KIND = "openai-codex"
    }
}

/** Assign work to models, each written provider:model, and list the models a person may pick. */
@Serializable
data class AgentModelAssignments(
    val default: String = "", val fast: String = "", val quick: String = "", val embedding: String = "", val triage: String = "", val research: String = "",
    val summarize: String = "", val reply: String = "", val ask: String = "", val schedule: String = "", val compact: String = "", val scan: String = "",
    val synthesize: String = "", val decide: String = "", val categorize: String = "", val transcription: String? = null,
    val embeddingDimensions: Int = 0, val choices: List<String> = emptyList(),
) {
    /** One assignment by its field name; empty for a name it does not have. */
    fun assignment(fieldName: String): String = when (fieldName) {
        "default" -> default; "fast" -> fast; "quick" -> quick; "embedding" -> embedding; "triage" -> triage; "research" -> research
        "summarize" -> summarize; "reply" -> reply; "ask" -> ask; "schedule" -> schedule; "compact" -> compact; "scan" -> scan
        "synthesize" -> synthesize; "decide" -> decide; "categorize" -> categorize; "transcription" -> transcription.orEmpty()
        else -> ""
    }

    fun assigning(fieldName: String, modelName: String): AgentModelAssignments = when (fieldName) {
        "default" -> copy(default = modelName); "fast" -> copy(fast = modelName); "quick" -> copy(quick = modelName)
        "embedding" -> copy(embedding = modelName); "triage" -> copy(triage = modelName); "research" -> copy(research = modelName)
        "summarize" -> copy(summarize = modelName); "reply" -> copy(reply = modelName); "ask" -> copy(ask = modelName)
        "schedule" -> copy(schedule = modelName); "compact" -> copy(compact = modelName); "scan" -> copy(scan = modelName)
        "synthesize" -> copy(synthesize = modelName); "decide" -> copy(decide = modelName); "categorize" -> copy(categorize = modelName)
        "transcription" -> copy(transcription = modelName)
        else -> this
    }

    /** Every assignment written provider:model renamed from one provider to another, as a renamed provider takes its models with it. */
    fun renamingProvider(previousName: String, newName: String): AgentModelAssignments {
        fun rename(model: String): String = if (model.startsWith("$previousName:")) newName + model.substring(previousName.length) else model
        var renamed = this
        for (fieldName in ALL_FIELD_NAMES) renamed = renamed.assigning(fieldName, rename(renamed.assignment(fieldName)))
        return renamed.copy(choices = choices.map { model -> rename(model) })
    }

    val parameters: JsonObject
        get() = JsonObject(
            ALL_FIELD_NAMES.associateWith { fieldName -> JsonPrimitive(assignment(fieldName)) as JsonElement } +
                mapOf("embeddingDimensions" to SettingsList.numberValue(embeddingDimensions.toDouble()), "choices" to SettingsList.listValue(choices)),
        )

    /** The agent section that saves the assignments whole, as the models form does. */
    val agentParameters: JsonObject get() = objectOf("models" to parameters)

    companion object {
        /** The four set first. Quick is the model a typed turn starts on; empty starts it on the conversations model. */
        val BASE_FIELD_NAMES = listOf("default", "fast", "quick", "embedding")

        /** One per kind of work, for whoever spends differently on each. */
        val WORK_FIELD_NAMES = listOf("triage", "research", "summarize", "reply", "ask", "schedule", "compact", "scan", "synthesize", "transcription")

        /** Every assignment by field name, the deciding ones last. */
        val ALL_FIELD_NAMES = BASE_FIELD_NAMES + WORK_FIELD_NAMES + listOf("decide", "categorize")
    }
}

/** What people may turn on, by feature, as the features form holds it. A feature the server did not say is on. */
object AgentFeatureSwitches {
    fun isOn(feature: String, features: Map<String, Boolean>): Boolean = features[feature] != false

    fun agentParameters(features: Map<String, Boolean>): JsonObject = objectOf("features" to JsonObject(features.mapValues { entry -> JsonPrimitive(entry.value) }))
}

/** A model a role may name: the provider's name and the model's, which a role stores as [value], provider:model. */
data class AgentModelOption(val value: String, val providerName: String, val modelName: String) {
    /** As a picker reads it: Provider · Model. */
    val label: String get() = "$providerName · $modelName"
}

/** The models the providers offer, split the way the models form offers them: a deciding provider (kind typesafe) answers only the decide and categorize slots. */
data class AgentModelChoices(val forWork: List<AgentModelOption>, val forDeciding: List<AgentModelOption>) {
    companion object {
        /** What a role may name: every model that is on, of every provider that is on, in the providers' order. */
        fun offered(providers: List<AgentProviderSettings>): List<AgentModelOption> = providers.filter { provider -> provider.isEnabled }.flatMap { provider ->
            provider.models.filter { entry -> entry.isEnabled }.map { entry -> AgentModelOption("${provider.name}:${entry.model}", provider.name, entry.displayName) }
        }

        /** A stored provider:model as a picker names it: by the entry's name when the providers have it, else as stored. */
        fun optionFor(value: String, providers: List<AgentProviderSettings>): AgentModelOption {
            val providerName = value.substringBefore(':')
            val modelId = value.substringAfter(':', "")
            val entry = providers.firstOrNull { provider -> provider.name == providerName }?.models?.firstOrNull { candidate -> candidate.model == modelId }
            return AgentModelOption(value, providerName, entry?.displayName ?: modelId.ifEmpty { value })
        }

        /** The offered models narrowed to the ones a capability is known for (by provider:model); an entry is never added to the list. */
        fun offeredWithCapability(capableNames: Collection<String>, providers: List<AgentProviderSettings>): List<AgentModelOption> =
            offered(providers).filter { option -> capableNames.contains(option.value) }

        fun of(options: List<AgentModelOption>, providers: List<AgentProviderSettings>): AgentModelChoices {
            val deciderNames = providers.filter { provider -> provider.providerKind == "typesafe" }.map { provider -> provider.name }.toSet()
            fun isDecider(option: AgentModelOption): Boolean = deciderNames.contains(option.providerName)
            return AgentModelChoices(options.filter { option -> !isDecider(option) }, options.filter { option -> isDecider(option) })
        }

        /** What a picker lists: the known models, with the one chosen first when the list does not know it. */
        fun listed(known: List<AgentModelOption>, chosen: String, providers: List<AgentProviderSettings>): List<AgentModelOption> =
            if (chosen.isEmpty() || known.any { option -> option.value == chosen }) known else listOf(optionFor(chosen, providers)) + known

        /** The ids a provider's live list offers that are not yet entries: the list names models provider:model, and the provider is the part before the first colon. */
        fun idsToAdd(liveNames: List<String>, provider: AgentProviderSettings): List<String> {
            val taken = provider.models.map { entry -> entry.model }.toSet()
            return liveNames.filter { name -> name.startsWith("${provider.name}:") }.map { name -> name.substring(provider.name.length + 1) }
                .filter { modelId -> modelId.isNotEmpty() && !taken.contains(modelId) }.distinct()
        }
    }
}

/** The caps on what people and the whole server may spend, in tokens and in money, and nothing else: every other bound is fixed on the server. */
@Serializable
data class AgentLimits(
    val dailyTokensPerAgent: Double = 0.0, val dailyCostPerAgent: Double = 0.0, val monthlyCostPerServer: Double = 0.0, val monthlyTokensPerServer: Double = 0.0,
)

@Serializable
data class AgentWebSearch(@SerialName("kind") val searchKind: String = "", val hasApiKey: Boolean = false)

/** The tools that are off and the tools that ask first, by name, and every tool there is. */
@Serializable
data class AgentToolPolicy(val disabled: List<String> = emptyList(), val confirm: List<String> = emptyList(), val catalog: List<AgentTool> = emptyList())

/** Talking to the agent. */
@Serializable
data class AgentVoice(
    @SerialName("enabled") val isEnabled: Boolean = false, val provider: String = "", val transcriptionModel: String = "", val silenceMS: Int = 0,
    val speechModel: String = "", val speechVoice: String = "", val askModel: String = "",
)

/** What the server does while and after a recording is made. Both are on unless the operator turns them off. */
@Serializable
data class AgentRecordingOptions(val isLiveTranscriptionEnabled: Boolean = true, val isAccurateWordsEnabled: Boolean = true)

/** The headless browser beside the server. */
@Serializable
data class AgentBrowser(
    @SerialName("enabled") val isEnabled: Boolean = false, val cdpEndpoint: String = "", @SerialName("attachTabs") val canAttachTabs: Boolean = false,
    val allowPrivateAddresses: List<String> = emptyList(),
)

@Serializable
data class AgentPlaid(val environment: String = "", val clientId: String = "", val hasSecret: Boolean = false, val countryCodes: List<String> = emptyList(), val products: List<String> = emptyList())

/** Which providers people may link their institutions through. */
@Serializable
data class AgentFinance(val offeredProviders: List<String> = emptyList(), val plaid: AgentPlaid = AgentPlaid())

/** One connected server the operator declared. Its secrets are never read back. */
@Serializable
data class ConnectedServerSettings(
    val name: String = "", val transport: String = "", val effectiveTransport: String = "", val url: String = "", val command: String = "",
    val args: List<String> = emptyList(), val envNames: List<String> = emptyList(), val workingDir: String = "", val auth: String = "", val effectiveAuth: String = "",
    val hasAuthorization: Boolean = false, val oauthClientId: String = "", val hasOauthClientSecret: Boolean = false, val oauthScopes: List<String> = emptyList(),
    val oauthAuthorizationUrl: String = "", val oauthTokenUrl: String = "", val oauthRedirect: String = "", @SerialName("headless") val isHeadless: Boolean = false,
    val location: String = "", val readOnly: List<String> = emptyList(), val disabled: List<String> = emptyList(), val timeout: String = "",
    @SerialName("enabled") val isEnabled: Boolean = true,
) {
    /** As the API takes it back: secrets blank, which keeps them, and the environment as names with blank values, which keeps those too. */
    val storedParameters: JsonObject
        get() = objectOf(
            "name" to name, "transport" to transport, "url" to url, "command" to command, "args" to args, "env" to envNames.map { envName -> "$envName=" },
            "workingDir" to workingDir, "auth" to auth, "authorization" to "", "oauthClientId" to oauthClientId, "oauthClientSecret" to "", "oauthScopes" to oauthScopes,
            "oauthAuthorizationUrl" to oauthAuthorizationUrl, "oauthTokenUrl" to oauthTokenUrl, "oauthRedirect" to (if (oauthRedirect == "computer") "computer" else ""),
            "headless" to isHeadless, "location" to (if (location == "computer") "computer" else ""), "readOnly" to readOnly, "disabled" to disabled,
            "timeout" to timeout, "enabled" to isEnabled,
        )
}

/** The operator's agent settings. Every secret comes back as set or not. */
@Serializable
data class AgentServerSettings(
    @SerialName("enabled") val isEnabled: Boolean = false,
    val instructions: String = "",
    val allowPrivateAddresses: List<String> = emptyList(),
    val skipCertificateCheck: List<String> = emptyList(),
    val providers: List<AgentProviderSettings> = emptyList(),
    val models: AgentModelAssignments = AgentModelAssignments(),
    /** What people may turn on, by feature; one not said is on. */
    val features: Map<String, Boolean> = emptyMap(),
    val limits: AgentLimits = AgentLimits(),
    val currency: String = "",
    val search: AgentWebSearch = AgentWebSearch(),
    val tools: AgentToolPolicy = AgentToolPolicy(),
    val voice: AgentVoice = AgentVoice(),
    val recording: AgentRecordingOptions? = null,
    val browser: AgentBrowser = AgentBrowser(),
    val mcpServers: List<ConnectedServerSettings> = emptyList(),
    val finance: AgentFinance = AgentFinance(),
    val works: List<String>? = null,
    val families: List<String> = emptyList(),
    val kinds: List<String> = emptyList(),
) {
    companion object {
        /** The features in the order the dashboard lists them. */
        val FEATURE_NAMES = listOf(
            "triage", "summaries", "draftReplies", "search", "research", "autoReply", "ask", "schedules", "browser", "connectedServers", "computer", "chatApps",
            "skills", "subagents", "remember", "knowledge", "dreaming",
        )

        /** The finance providers and Plaid products, in the order the server lists them. */
        val FINANCE_PROVIDERS = listOf("plaid", "simplefin")
        val PLAID_PRODUCTS = listOf("transactions", "investments", "liabilities")

        /** The voices an answer can be read in; marin when none is chosen. */
        val SPEECH_VOICES = listOf("marin", "cedar", "alloy", "ash", "ballad", "coral", "echo", "fable", "nova", "onyx", "sage", "shimmer", "verse")
    }
}

/** Whether people may have an agent, the house instructions and the network exceptions, as the form holds them. */
data class AgentGeneralDraft(val isEnabled: Boolean, val instructions: String, val allowPrivateAddressesText: String, val skipCertificateCheckText: String) {
    val agentParameters: JsonObject
        get() = objectOf(
            "enabled" to isEnabled, "instructions" to instructions, "allowPrivateAddresses" to SettingsList.lineItems(allowPrivateAddressesText),
            "skipCertificateCheck" to SettingsList.lineItems(skipCertificateCheckText),
        )

    companion object {
        fun of(settings: AgentServerSettings) = AgentGeneralDraft(
            settings.isEnabled, settings.instructions, SettingsList.text(settings.allowPrivateAddresses), SettingsList.text(settings.skipCertificateCheck),
        )
    }
}

/** A provider as its editor holds it. The key is never read back: blank keeps the stored one. */
data class AgentProviderDraft(
    val name: String = "", val providerKind: String = "openai", val baseUrl: String = "", val apiKey: String = "", val hasApiKey: Boolean = false,
    val billingApiKey: String = "", val hasBillingApiKey: Boolean = false, val hasRefreshToken: Boolean = false, val isEnabled: Boolean = true,
    val pricingInputText: String = "", val pricingOutputText: String = "", val pricingCacheReadText: String = "", val pricingCacheWriteText: String = "",
    val pricingAudioInputText: String = "",
    /** The models the provider offers, as read. The settings save does not send them: the server keeps the entries, and they change only through their own calls. */
    val models: List<AgentModelEntry> = emptyList(),
) {
    val isSignedInKind: Boolean get() = providerKind == AgentProviderSettings.SIGNED_IN_KIND

    /** Whether it can be saved: it has a name, and a provider on a plan has been signed in to, since the sign-in is what saves one. */
    val canSave: Boolean get() = name.trim().isNotEmpty() && (!isSignedInKind || hasRefreshToken)

    val parameters: JsonObject
        get() = objectOf(
            "name" to name.trim(), "kind" to providerKind, "baseUrl" to baseUrl.trim(), "apiKey" to apiKey, "billingApiKey" to billingApiKey, "enabled" to isEnabled,
            "pricingInput" to SettingsList.numberValue(SettingsList.number(pricingInputText)), "pricingOutput" to SettingsList.numberValue(SettingsList.number(pricingOutputText)),
            "pricingCacheRead" to SettingsList.numberValue(SettingsList.number(pricingCacheReadText)),
            "pricingCacheWrite" to SettingsList.numberValue(SettingsList.number(pricingCacheWriteText)),
            "pricingAudioInput" to SettingsList.numberValue(SettingsList.number(pricingAudioInputText)),
        )

    /**
     * The agent section that saves the provider list with this draft added, or put in place of the provider at the index given. The list
     * is sent whole; a renamed provider takes its model assignments with it, or the server would refuse the settings for naming one that is gone.
     */
    fun agentParameters(replacingProviderAt: Int?, settings: AgentServerSettings): JsonObject {
        val providers = settings.providers.map { provider -> provider.storedParameters }.toMutableList()
        val section = mutableMapOf<String, JsonElement>()
        if (replacingProviderAt != null && settings.providers.indices.contains(replacingProviderAt)) {
            val previousName = settings.providers[replacingProviderAt].name
            providers[replacingProviderAt] = JsonObject(parameters + ("previousName" to JsonPrimitive(previousName)))
            val newName = name.trim()
            if (previousName != newName) section["models"] = settings.models.renamingProvider(previousName, newName).parameters
        } else {
            providers.add(parameters)
        }
        section["providers"] = JsonArray(providers)
        return JsonObject(section)
    }

    companion object {
        fun of(provider: AgentProviderSettings) = AgentProviderDraft(
            name = provider.name, providerKind = provider.providerKind, baseUrl = provider.baseUrl, hasApiKey = provider.hasApiKey,
            hasBillingApiKey = provider.hasBillingApiKey ?: false, hasRefreshToken = provider.hasRefreshToken, isEnabled = provider.isEnabled,
            pricingInputText = SettingsList.text(provider.pricingInput),
            pricingOutputText = SettingsList.text(provider.pricingOutput), pricingCacheReadText = SettingsList.text(provider.pricingCacheRead),
            pricingCacheWriteText = SettingsList.text(provider.pricingCacheWrite), pricingAudioInputText = SettingsList.text(provider.pricingAudioInput),
            models = provider.models,
        )

        /** The agent section that saves the provider list without the provider at the index given. */
        fun agentParameters(removingProviderAt: Int, settings: AgentServerSettings): JsonObject =
            objectOf("providers" to settings.providers.filterIndexed { index, _ -> index != removingProviderAt }.map { provider -> provider.storedParameters })
    }
}

/** The limits and the currency as their form holds them, each limit as typed. */
data class AgentLimitsDraft(val limitTexts: Map<String, String>, val currency: String) {
    val agentParameters: JsonObject
        get() {
            val limits = LIMIT_GROUPS.flatten().associateWith { limitName -> SettingsList.numberValue(SettingsList.number(limitTexts[limitName].orEmpty())) }
            return objectOf("limits" to JsonObject(limits), "currency" to currency)
        }

    companion object {
        /** The limits in the order the form groups them. */
        val LIMIT_GROUPS: List<List<String>> = listOf(
            listOf("dailyTokensPerAgent", "monthlyTokensPerServer"),
            listOf("dailyCostPerAgent", "monthlyCostPerServer"),
        )

        fun of(settings: AgentServerSettings): AgentLimitsDraft {
            val limits = settings.limits
            val numbers = mapOf(
                "dailyTokensPerAgent" to limits.dailyTokensPerAgent, "monthlyTokensPerServer" to limits.monthlyTokensPerServer,
                "dailyCostPerAgent" to limits.dailyCostPerAgent, "monthlyCostPerServer" to limits.monthlyCostPerServer,
            )
            // Zero is said as 0, as the dashboard's form shows a stored limit.
            val texts = numbers.mapValues { entry -> if (entry.value == 0.0) "0" else SettingsList.text(entry.value) }
            return AgentLimitsDraft(texts, settings.currency)
        }
    }
}

/** One word per family and per tool, allow, confirm or off. A tool's word follows its family's unless it says otherwise. */
data class AgentToolPolicyDraft(val names: List<String>, val wordByName: Map<String, String>) {
    fun word(name: String): String = wordByName[name] ?: "allow"

    fun setting(name: String, word: String): AgentToolPolicyDraft = copy(wordByName = wordByName + (name to word))

    val agentParameters: JsonObject
        get() = objectOf("tools" to objectOf("disabled" to names.filter { name -> word(name) == "off" }, "confirm" to names.filter { name -> word(name) == "confirm" }))

    companion object {
        val WORDS = listOf("allow", "confirm", "off")

        fun of(settings: AgentServerSettings): AgentToolPolicyDraft {
            val names = settings.families + settings.tools.catalog.map { tool -> tool.name }
            return AgentToolPolicyDraft(
                names,
                names.associateWith { name ->
                    when {
                        settings.tools.disabled.contains(name) -> "off"
                        settings.tools.confirm.contains(name) -> "confirm"
                        else -> "allow"
                    }
                },
            )
        }
    }
}

/** The web search service as its form holds it. */
data class AgentWebSearchDraft(val searchKind: String, val apiKey: String = "") {
    val agentParameters: JsonObject
        get() = objectOf("search" to JsonObject(mapOf("kind" to JsonPrimitive(searchKind)) + (if (apiKey.isNotEmpty()) mapOf("apiKey" to JsonPrimitive(apiKey)) else emptyMap())))
}

/** The headless browser as its form holds it. */
data class AgentBrowserDraft(
    val isEnabled: Boolean, val cdpEndpoint: String, val canAttachTabs: Boolean, val allowPrivateAddressesText: String,
) {
    val agentParameters: JsonObject
        get() = objectOf(
            "browser" to objectOf(
                "enabled" to isEnabled, "cdpEndpoint" to cdpEndpoint, "attachTabs" to canAttachTabs, "allowPrivateAddresses" to SettingsList.lineItems(allowPrivateAddressesText),
            ),
        )

    companion object {
        fun of(browser: AgentBrowser) = AgentBrowserDraft(
            browser.isEnabled, browser.cdpEndpoint, browser.canAttachTabs, SettingsList.text(browser.allowPrivateAddresses),
        )
    }
}

/** Talking to the agent, as the operator's form holds it. */
data class AgentServerVoiceDraft(
    val isEnabled: Boolean, /** A provider of kind openai by name, or empty for the first one. */ val provider: String, val transcriptionModel: String, val silenceMSText: String,
    val speechModel: String, /** Empty for marin. */ val speechVoice: String, val askModel: String,
) {
    val agentParameters: JsonObject
        get() = objectOf(
            "voice" to objectOf(
                "enabled" to isEnabled, "provider" to provider, "transcriptionModel" to transcriptionModel.trim(), "silenceMS" to SettingsList.numberValue(SettingsList.number(silenceMSText)),
                "speechModel" to speechModel.trim(), "speechVoice" to speechVoice, "askModel" to askModel.trim(),
            ),
        )

    companion object {
        fun of(voice: AgentVoice) = AgentServerVoiceDraft(
            voice.isEnabled, voice.provider, voice.transcriptionModel, if (voice.silenceMS > 0) voice.silenceMS.toString() else "", voice.speechModel, voice.speechVoice, voice.askModel,
        )
    }
}

/** What the server does with a recording, as the operator's form holds it. A server that does not say has both on. */
data class AgentServerRecordingDraft(val isLiveTranscriptionEnabled: Boolean, val isAccurateWordsEnabled: Boolean) {
    val agentParameters: JsonObject
        get() = objectOf("recording" to objectOf("isLiveTranscriptionEnabled" to isLiveTranscriptionEnabled, "isAccurateWordsEnabled" to isAccurateWordsEnabled))

    companion object {
        fun of(recording: AgentRecordingOptions?) = AgentServerRecordingDraft(recording?.isLiveTranscriptionEnabled ?: true, recording?.isAccurateWordsEnabled ?: true)
    }
}

/** The finance providers and the Plaid account as their form holds them. The Plaid secret is never read back. */
data class AgentFinanceDraft(
    val offeredProviders: List<String>, val environment: String, val clientId: String, val secret: String = "", val countryCodesText: String, val products: List<String>,
) {
    val isOfferingPlaid: Boolean get() = offeredProviders.contains("plaid")

    /** Whether a product is on. Transactions is what every finance source is read with, so it always is. */
    fun hasProduct(product: String): Boolean = product == "transactions" || products.contains(product)

    fun settingOffered(provider: String, isOffered: Boolean): AgentFinanceDraft = copy(offeredProviders = offeredProviders.filter { name -> name != provider } + (if (isOffered) listOf(provider) else emptyList()))

    fun settingProduct(product: String, isOn: Boolean): AgentFinanceDraft =
        copy(products = products.filter { name -> name != product && name != "transactions" } + "transactions" + (if (isOn) listOf(product) else emptyList()))

    val agentParameters: JsonObject
        get() {
            // In the order the server lists them, whatever order they were ticked in.
            fun ordered(chosen: List<String>, order: List<String>): List<String> = order.filter { name -> chosen.contains(name) }
            val plaid = mutableMapOf<String, JsonElement>(
                "environment" to JsonPrimitive(environment), "clientId" to JsonPrimitive(clientId),
                "countryCodes" to SettingsList.listValue(SettingsList.lineItems(countryCodesText).map { code -> code.uppercase() }),
                "products" to SettingsList.listValue(ordered(products, AgentServerSettings.PLAID_PRODUCTS)),
            )
            if (secret.isNotEmpty()) plaid["secret"] = JsonPrimitive(secret)
            return objectOf(
                "finance" to objectOf("offeredProviders" to ordered(offeredProviders, AgentServerSettings.FINANCE_PROVIDERS), "plaid" to JsonObject(plaid)),
            )
        }

    companion object {
        fun of(finance: AgentFinance) = AgentFinanceDraft(
            finance.offeredProviders, finance.plaid.environment, finance.plaid.clientId, "", SettingsList.text(finance.plaid.countryCodes), finance.plaid.products,
        )
    }
}

/** A connected server as its editor holds it. The secrets are never read back: blank keeps the stored ones, and the environment's stored values the same way, by name. */
data class ConnectedServerDraft(
    val name: String = "",
    /** http, stdio, or empty to infer it. */
    val transport: String = "",
    val url: String = "", val command: String = "", val argsText: String = "", val envText: String = "", val workingDir: String = "",
    /** none, static, user, oauth, or empty to infer it. */
    val auth: String = "",
    val authorization: String = "", val hasAuthorization: Boolean = false, val oauthClientId: String = "", val oauthClientSecret: String = "", val hasOauthClientSecret: Boolean = false,
    val oauthScopesText: String = "", val oauthAuthorizationUrl: String = "", val oauthTokenUrl: String = "",
    /** Empty for this server's address, computer for the attached computer. */
    val oauthRedirect: String = "",
    val isHeadless: Boolean = false,
    /** Empty for this server, computer for the person's attached computer. */
    val location: String = "",
    val readOnlyText: String = "", val disabledText: String = "", val timeout: String = "", val isEnabled: Boolean = true,
) {
    private val hasCommand: Boolean get() = command.trim().isNotEmpty()
    private val hasUrl: Boolean get() = url.trim().isNotEmpty()

    /** Whether it runs a command: said so, or inferred from a command and no address. */
    val isStdio: Boolean get() = transport == "stdio" || (transport.isEmpty() && hasCommand && !hasUrl)

    /** Whether where the command runs is asked. */
    val asksLocation: Boolean get() = transport == "stdio" || (transport.isEmpty() && hasCommand)
    val showsUrl: Boolean get() = transport != "stdio"
    val showsCommand: Boolean get() = transport != "http"
    val showsAuthorization: Boolean get() = auth == "static" || auth.isEmpty()
    val isOAuth: Boolean get() = auth == "oauth"

    /** A server on the person's computer is there only while they are, so a run with nobody present cannot use it. */
    val canBeHeadless: Boolean get() = location != "computer"
    val canSave: Boolean get() = name.trim().isNotEmpty() && (hasUrl || hasCommand)

    fun settingLocation(newLocation: String): ConnectedServerDraft = copy(location = newLocation, isHeadless = if (newLocation == "computer") false else isHeadless)

    val parameters: JsonObject
        get() = objectOf(
            "name" to name.trim(), "transport" to transport, "url" to url.trim(), "command" to command.trim(),
            "args" to argsText.split(Regex("\\s+")).filter { word -> word.isNotEmpty() },
            "env" to envText.split('\n').filter { line -> line.trim().isNotEmpty() }, "workingDir" to workingDir.trim(), "auth" to auth, "authorization" to authorization,
            "oauthClientId" to oauthClientId.trim(), "oauthClientSecret" to oauthClientSecret, "oauthScopes" to SettingsList.lineItems(oauthScopesText),
            "oauthAuthorizationUrl" to oauthAuthorizationUrl.trim(), "oauthTokenUrl" to oauthTokenUrl.trim(), "oauthRedirect" to oauthRedirect, "headless" to isHeadless,
            "location" to location, "readOnly" to SettingsList.lineItems(readOnlyText), "disabled" to SettingsList.lineItems(disabledText), "timeout" to timeout.trim(),
            "enabled" to isEnabled,
        )

    /** The agent section that saves the server list with this draft added, or put in place of the one at the index given, carrying its previous name so its stored secrets follow a rename. */
    fun agentParameters(replacingServerAt: Int?, settings: AgentServerSettings): JsonObject {
        val servers = settings.mcpServers.map { server -> server.storedParameters }.toMutableList()
        if (replacingServerAt != null && settings.mcpServers.indices.contains(replacingServerAt)) {
            servers[replacingServerAt] = JsonObject(parameters + ("previousName" to JsonPrimitive(settings.mcpServers[replacingServerAt].name)))
        } else {
            servers.add(parameters)
        }
        return objectOf("mcpServers" to JsonArray(servers))
    }

    companion object {
        fun of(server: ConnectedServerSettings) = ConnectedServerDraft(
            name = server.name, transport = server.transport, url = server.url, command = server.command, argsText = server.args.joinToString(" "),
            envText = server.envNames.joinToString("\n") { envName -> "$envName=" }, workingDir = server.workingDir, auth = server.auth, hasAuthorization = server.hasAuthorization,
            oauthClientId = server.oauthClientId, hasOauthClientSecret = server.hasOauthClientSecret, oauthScopesText = SettingsList.text(server.oauthScopes),
            oauthAuthorizationUrl = server.oauthAuthorizationUrl, oauthTokenUrl = server.oauthTokenUrl, oauthRedirect = if (server.oauthRedirect == "computer") "computer" else "",
            isHeadless = server.isHeadless, location = server.location, readOnlyText = SettingsList.text(server.readOnly), disabledText = SettingsList.text(server.disabled),
            timeout = server.timeout, isEnabled = server.isEnabled,
        )

        /** The agent section that saves the server list without the server at the index given. */
        fun agentParameters(removingServerAt: Int, settings: AgentServerSettings): JsonObject =
            objectOf("mcpServers" to settings.mcpServers.filterIndexed { index, _ -> index != removingServerAt }.map { server -> server.storedParameters })
    }
}

/** A sign-in to a provider that runs on a person's plan: a one-time code to type on the service's own page, and, once typed, the account and plan it signed in as. */
@Serializable
data class ProviderSignIn(
    val signInId: String = "", val userCode: String = "", val verificationAddress: String = "", val isSignedIn: Boolean = false, val account: String = "", val plan: String = "",
)

@Serializable private class AgentSettingsInner(val agent: AgentServerSettings)

@Serializable private class AgentSettingsAnswer(val GetSettings: AgentSettingsInner)

@Serializable private class UpdateAgentSettingsAnswer(val UpdateSettings: AgentSettingsInner)

@Serializable private class AgentModelName(val name: String = "")

@Serializable private class ListAgentModelsAnswer(val ListAgentModels: List<AgentModelName?> = emptyList())

@Serializable private class TestAgentProviderAnswer(val TestAgentProvider: List<AgentModelName?> = emptyList())

@Serializable private class BeginProviderSignInAnswer(val BeginAgentProviderSignIn: ProviderSignIn)

@Serializable private class FinishProviderSignInAnswer(val FinishAgentProviderSignIn: ProviderSignIn)

/** The operator's agent settings. */
suspend fun Connection.agentServerSettings(): AgentServerSettings = execute<AgentSettingsAnswer>(ManageServerDocuments.MANAGE_AGENT_SETTINGS).GetSettings.agent

/** Changes part of the operator's agent settings, a section at a time, and answers with what is now stored. */
suspend fun Connection.updateAgentServerSettings(agentParameters: JsonObject): AgentServerSettings =
    execute<UpdateAgentSettingsAnswer>(ManageServerDocuments.MANAGE_UPDATE_AGENT_SETTINGS, mapOf("agent" to agentParameters)).UpdateSettings.agent

@Serializable private class SaveAgentModelEntryAnswer(val SaveAgentModelEntry: AgentModelEntry)

@Serializable private class DeleteAgentModelEntryAnswer(val DeleteAgentModelEntry: Boolean = true)

/** Adds a model to a provider, or changes the one with the same id. Settings read afterwards show it. */
suspend fun Connection.saveAgentModelEntry(providerName: String, entry: AgentModelEntry) {
    execute<SaveAgentModelEntryAnswer>(ManageServerDocuments.MANAGE_SAVE_AGENT_MODEL_ENTRY, variablesOf("providerName" to providerName, "entry" to entry.toJson()))
}

/** Takes a model off a provider. The server refuses while a role uses it, and says which. */
suspend fun Connection.deleteAgentModelEntry(providerName: String, model: String) {
    execute<DeleteAgentModelEntryAnswer>(ManageServerDocuments.MANAGE_DELETE_AGENT_MODEL_ENTRY, variablesOf("providerName" to providerName, "model" to model))
}

/** The models the enabled providers offer, written provider:model. A provider that cannot be reached contributes nothing. */
suspend fun Connection.agentModelNames(): List<String> = execute<ListAgentModelsAnswer>(ManageServerDocuments.MANAGE_AGENT_MODELS).ListAgentModels.mapNotNull { model -> model?.name }

/** Asks one provider what it offers, and says why when it cannot. */
suspend fun Connection.testAgentProvider(name: String): List<String> =
    execute<TestAgentProviderAnswer>(ManageServerDocuments.MANAGE_TEST_AGENT_PROVIDER, variablesOf("name" to name)).TestAgentProvider.mapNotNull { model -> model?.name }

/** Asks the service for a one-time code to sign a provider in to a person's plan. */
suspend fun Connection.beginProviderSignIn(provider: String): ProviderSignIn =
    execute<BeginProviderSignInAnswer>(ManageServerDocuments.MANAGE_BEGIN_PROVIDER_SIGN_IN, variablesOf("provider" to provider.trim())).BeginAgentProviderSignIn

/** Asks whether the code has been typed. The server waits up to half a minute before answering that it has not. */
suspend fun Connection.finishProviderSignIn(signInId: String): ProviderSignIn =
    execute<FinishProviderSignInAnswer>(ManageServerDocuments.MANAGE_FINISH_PROVIDER_SIGN_IN, variablesOf("signInId" to signInId)).FinishAgentProviderSignIn

/** What a model is known to do with a recording, from its provider's own evidence, never a guess from its name. */
@Serializable
data class RecordingModelCapability(
    val name: String,
    val canTranscribe: Boolean? = null,
    val hasSpeakerDiarization: Boolean? = null,
    val hasSegmentTimestamps: Boolean? = null,
    val hasSpeakerReferences: Boolean? = null,
    val maximumAudioBytes: Long? = null,
    val maximumSpeakerReferenceCount: Int? = null,
) {
    val canAttributeRecordingSpeakers: Boolean get() = canTranscribe == true && hasSpeakerDiarization == true && hasSegmentTimestamps == true
}

@Serializable private class RecordingCapabilitiesAnswer(val ListAgentModels: List<RecordingModelCapability?> = emptyList())

/** The models the enabled providers offer, with what each is known to do with a recording. */
suspend fun Connection.recordingModelCapabilities(): List<RecordingModelCapability> =
    execute<RecordingCapabilitiesAnswer>(ManageServerDocuments.MANAGE_AGENT_MODELS).ListAgentModels.filterNotNull()
