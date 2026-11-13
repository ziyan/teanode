package com.teanode.kit.manage

import com.teanode.kit.Connection
import com.teanode.kit.agent.AgentBudget
import com.teanode.kit.settings.variablesOf
import kotlinx.serialization.Serializable

/** What an agent used over a time, in tokens and calls. */
@Serializable
data class AgentUsageTotals(
    val promptTokens: Long = 0, val completionTokens: Long = 0, val cacheReadTokens: Long = 0, val cacheWriteTokens: Long = 0, val audioInputTokens: Long = 0,
    val calls: Long = 0,
)

/** The mailbox an agent may reach, as the operator sees it. */
@Serializable
data class AgentAdminSource(val mailboxId: String = "", val name: String = "")

/** One person's agent as the operator sees it: its limits, what it used today, and whether it is switched off. */
@Serializable
data class AgentAdminSummary(
    val agentId: String,
    val userId: String = "",
    val username: String = "",
    val name: String = "",
    val enabled: Boolean = false,
    /** When an operator switched it off; while set, its person cannot turn it on. */
    val operatorDisabledAt: String? = null,
    /** The most tokens a day; zero is no limit. */
    val dailyTokens: Long = 0,
    /** The most money a day; zero is no limit. */
    val dailyCost: Double = 0.0,
    val dead: Int = 0,
    val queued: Int = 0,
    val lastRunAt: String? = null,
    val sources: List<AgentAdminSource> = emptyList(),
    val today: AgentBudget? = null,
    val totals: AgentUsageTotals? = null,
) {
    val isSwitchedOff: Boolean get() = !operatorDisabledAt.isNullOrEmpty()
    val shownName: String get() = name.ifEmpty { username }
}

/** The limits an operator types for one agent: digits for tokens, a decimal for money; empty is no limit. */
data class AgentLimitDraft(val dailyTokensText: String = "", val dailyCostText: String = "") {
    val dailyTokens: Long get() = dailyTokensText.trim().toLongOrNull()?.coerceAtLeast(0) ?: 0
    val dailyCost: Double get() = dailyCostText.trim().replace(',', '.').toDoubleOrNull()?.takeIf { cost -> cost.isFinite() && cost > 0 } ?: 0.0
    val isValid: Boolean get() = (dailyTokensText.isBlank() || dailyTokensText.trim().toLongOrNull() != null) && (dailyCostText.isBlank() || dailyCostText.trim().replace(',', '.').toDoubleOrNull() != null)

    companion object {
        fun of(summary: AgentAdminSummary): AgentLimitDraft = AgentLimitDraft(
            if (summary.dailyTokens > 0) summary.dailyTokens.toString() else "", if (summary.dailyCost > 0) summary.dailyCost.toString() else "",
        )
    }
}

private const val LIST_AGENTS = """query { ListAgents { agentId userId username name enabled operatorDisabledAt dailyTokens dailyCost dead queued lastRunAt
  sources { mailboxId name policy { granted } }
  today { used limit resetsAt cost costLimit currency }
  totals { promptTokens completionTokens cacheReadTokens cacheWriteTokens audioInputTokens calls } } }"""

@Serializable private class ListAgentsAnswer(val ListAgents: List<AgentAdminSummary>)

@Serializable private class SetAgentLimitAnswer(val SetAgentLimit: AgentAdminSummary)

@Serializable private class SetAgentDisabledAnswer(val SetAgentDisabled: AgentAdminSummary)

/** Everybody's agents with their limits and what they used today. Needs agent:audit. */
suspend fun Connection.managedAgents(): List<AgentAdminSummary> = execute<ListAgentsAnswer>(LIST_AGENTS).ListAgents

suspend fun Connection.setAgentLimit(agentId: String, draft: AgentLimitDraft): AgentAdminSummary =
    execute<SetAgentLimitAnswer>(ManageServerDocuments.MANAGE_SET_AGENT_LIMIT, variablesOf("agentId" to agentId, "dailyTokens" to draft.dailyTokens, "dailyCost" to draft.dailyCost)).SetAgentLimit

suspend fun Connection.setAgentDisabled(agentId: String, isSwitchedOff: Boolean): AgentAdminSummary =
    execute<SetAgentDisabledAnswer>(ManageServerDocuments.MANAGE_SET_AGENT_DISABLED, variablesOf("agentId" to agentId, "disabled" to isSwitchedOff)).SetAgentDisabled
