package com.teanode.kit.agent

import com.teanode.kit.Connection
import com.teanode.kit.photos.ServerTime
import kotlinx.serialization.Serializable
import java.time.Instant
import java.time.ZoneOffset

/** One price the check set beside the bill: what the settings say for a model and a kind of token, and what the bill implies, each per million tokens. */
@Serializable
data class AgentBilledPrice(
    val model: String,
    /** input, cachedInput, cacheWrite, output or audioInput. */
    val tokenKind: String,
    val configuredPerMillion: Double,
    val billedPerMillion: Double,
    val dayCount: Int,
    /** Whether the configured price comes from an entry whose model is a pattern covering other models too. The check reports such a price and never changes it. */
    val isPattern: Boolean = false,
) {
    val identity: String get() = "$model/$tokenKind"

    /** Whether the settings and the bill disagree by more than rounding. */
    val isChanged: Boolean get() = configuredPerMillion != billedPerMillion
}

/** One price a check set from the bill. */
@Serializable
data class AgentPriceChange(val model: String, val tokenKind: String, val beforePerMillion: Double, val afterPerMillion: Double) {
    val identity: String get() = "$model/$tokenKind"
}

/** The last check of one provider's prices against its bill. */
@Serializable
data class AgentBillingCheck(
    val providerName: String,
    /** Go writes its zero time for a provider never checked. */
    val checkedAt: String,
    /** Why the bill could not be read, such as a refused key; empty when it was. */
    val errorMessage: String = "",
    val compared: List<AgentBilledPrice> = emptyList(),
    val changed: List<AgentPriceChange> = emptyList(),
) {
    /** When it was checked; null for a provider that never was. */
    val checkedInstant: Instant?
        get() = ServerTime.instant(checkedAt)?.takeIf { instant -> instant.atZone(ZoneOffset.UTC).year > 1 }
}

@Serializable private class AgentBillingChecksAnswer(val AgentBillingChecks: List<AgentBillingCheck>)

@Serializable private class CheckAgentProviderPricesAnswer(val CheckAgentProviderPrices: List<AgentBillingCheck>)

/** What the last check of each provider that has a billing key compared and changed. A provider never checked is listed with no time. */
suspend fun Connection.agentBillingChecks(): List<AgentBillingCheck> = execute<AgentBillingChecksAnswer>(AgentBillingDocuments.AGENT_BILLING_CHECKS).AgentBillingChecks

/** Checks the prices of every provider that has a billing key against its bill now, changes the ones that differ, and answers with what was compared and changed. */
suspend fun Connection.checkAgentProviderPrices(): List<AgentBillingCheck> =
    execute<CheckAgentProviderPricesAnswer>(AgentBillingDocuments.CHECK_AGENT_PROVIDER_PRICES).CheckAgentProviderPrices
