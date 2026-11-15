package com.teanode.kit.agent

import com.teanode.kit.Connection
import com.teanode.kit.settings.variablesOf
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import java.time.LocalDate

/** A bill, parcel, deadline or event the agent keeps track of from the person's mail. */
@Serializable
data class AgentFollowUp(
    val id: String,
    /** bill, parcel, deadline or event. */
    val followUpKind: String = "",
    val followUpTitle: String = "",
    val payee: String? = null,
    val carrier: String? = null,
    /** A decimal such as "84.20", in [currencyCode]. */
    val amount: String? = null,
    val currencyCode: String? = null,
    val trackingNumber: String? = null,
    /** A day written YYYY-MM-DD. */
    val dueOn: String? = null,
    val place: String? = null,
    /** The page a follow-up found on the web came from, null for what mail said. */
    val sourceUrl: String? = null,
    val followUpState: String = "",
) {
    val isOverdue: Boolean get() = followUpState == "overdue"
    val isStalled: Boolean get() = followUpState == "stalled"

    /** The page it was found on, when it is a web page: only http and https are linked, since the address came from a stranger's page. */
    val sourcePageUrl: String?
        get() {
            val address = sourceUrl ?: return null
            val lowered = address.lowercase()
            if (!lowered.startsWith("https://") && !lowered.startsWith("http://")) return null
            return address.takeIf { text -> sourcePageHost != null }
        }

    /** The page's host, for the link's label. */
    val sourcePageHost: String?
        get() = runCatching { java.net.URI(sourceUrl.orEmpty()).host }.getOrNull()?.takeIf { host -> host.isNotEmpty() }

    /** The day it is due, null when it has none or one that is not a day. */
    val dueDate: LocalDate? get() = dueOn?.let { text -> runCatching { LocalDate.parse(text) }.getOrNull() }

    companion object {
        /** The kinds a person can mute, all of them first, as the server's kind mutes name them. */
        val muteKinds = listOf("follow_up", "bill", "parcel", "deadline", "event", "nearby")

        /** The nearest day first, the ones with no day last, and the server's order within a day. */
        fun ordered(followUps: List<AgentFollowUp>): List<AgentFollowUp> =
            followUps.withIndex().sortedWith(compareBy({ entry -> entry.value.dueOn ?: "9999-12-31" }, { entry -> entry.index })).map { entry -> entry.value }
    }
}

/**
 * Something the person asks the agent to follow. The fields a kind does not use are kept while the kind is changed,
 * so a slip does not lose what was typed, and are left out of what is sent.
 */
data class NewAgentFollowUp(
    val followUpKind: String = "bill",
    val followUpTitle: String = "",
    /** A day written YYYY-MM-DD, empty for none. */
    val dueOn: String = "",
    val payee: String = "",
    val amount: String = "",
    val currencyCode: String = "",
    val carrier: String = "",
    val trackingNumber: String = "",
    val place: String = "",
) {
    /** The server's own rule: a title, a day unless it is a parcel, and an amount for a bill. */
    val canAdd: Boolean
        get() {
            if (followUpTitle.isBlank()) return false
            if (followUpKind != "parcel" && dueOn.isEmpty()) return false
            if (followUpKind == "bill" && amount.isBlank()) return false
            return true
        }

    /** What the mutation is given: only the fields the kind uses, and none that were left empty. */
    val variables: Map<String, JsonElement>
        get() {
            val given = mutableMapOf<String, JsonElement>()
            given.putAll(variablesOf("followUpKind" to followUpKind, "followUpTitle" to followUpTitle.trim()))
            val usedFields = mutableListOf("dueOn" to dueOn)
            when (followUpKind) {
                "bill" -> usedFields += listOf("payee" to payee, "amount" to amount, "currencyCode" to currencyCode)
                "parcel" -> usedFields += listOf("carrier" to carrier, "trackingNumber" to trackingNumber)
                "event" -> usedFields += listOf("place" to place)
            }
            usedFields.forEach { (name, text) -> if (text.trim().isNotEmpty()) given.putAll(variablesOf(name to text.trim())) }
            return given
        }

    companion object {
        /** The kinds a person can add, in the order they are offered. */
        val kinds = listOf("bill", "parcel", "deadline", "event")
    }
}

@Serializable private class ListAgentFollowUpsAnswer(val ListAgentFollowUps: List<AgentFollowUp?>)

/** What the agent is following for the person, the nearest day first: only those still being watched. */
suspend fun Connection.agentFollowUps(): List<AgentFollowUp> =
    AgentFollowUp.ordered(execute<ListAgentFollowUpsAnswer>(AgentUpkeepDocuments.AGENT_FOLLOW_UPS).ListAgentFollowUps.filterNotNull())

/** Stops following one: it is kept, and nothing more is said about it. */
suspend fun Connection.stopAgentFollowUp(followUpId: String) {
    execute<JsonObject>(AgentUpkeepDocuments.STOP_AGENT_FOLLOW_UP, variablesOf("followUpId" to followUpId))
}

/** Has the agent follow something the person names. */
suspend fun Connection.addAgentFollowUp(followUp: NewAgentFollowUp) {
    execute<JsonObject>(AgentUpkeepDocuments.ADD_AGENT_FOLLOW_UP, followUp.variables)
}

/** Stops the alerts about a kind of follow-up: one of [AgentFollowUp.muteKinds]. */
suspend fun Connection.muteAgentAlertKind(muteTarget: String) {
    execute<JsonObject>(AgentUpkeepDocuments.MUTE_AGENT_ALERT_KIND, variablesOf("muteTarget" to muteTarget))
}
