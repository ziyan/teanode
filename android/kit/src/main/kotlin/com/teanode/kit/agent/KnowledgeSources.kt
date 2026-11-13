package com.teanode.kit.agent

import com.teanode.kit.Connection
import com.teanode.kit.finance.deleteFinanceSource
import com.teanode.kit.finance.switchFinanceSource
import com.teanode.kit.finance.syncFinanceSource
import kotlinx.serialization.Serializable

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
)

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
