package com.teanode.kit.finance

import kotlinx.serialization.Serializable

/**
 * StatementImport is the person's statement import: the address to mail
 * OFX statements to (empty without a mailbox address), whether importing
 * is on, and what the last import did. The dashboard's Accounts tab shows
 * the same.
 */
@Serializable
data class StatementImport(
    val sourceId: String? = null,
    val importAddress: String? = null,
    val isEnabled: Boolean? = null,
    val lastStatementImport: LastImport? = null,
) {
    /** LastImport is what one import of statement files did. */
    @Serializable
    data class LastImport(
        val importedAt: String? = null,
        val statementImportOrigin: String? = null,
        val addedTransactionCount: Int? = null,
        val updatedTransactionCount: Int? = null,
        val unchangedTransactionCount: Int? = null,
        val financeAccountNames: List<String>? = null,
        val importErrorMessage: String? = null,
    )
}

/** ReadStatementQueued is the background job queued to read one account's transactions out of a picture or PDF. */
@Serializable
data class ReadStatementQueued(
    val agentAttachmentId: String? = null,
    val agentJobId: String? = null,
)

/**
 * FinanceImport is one file read into finance in the background. financeImportKind is "tax_document", "statement" or
 * "receipt"; financeImportSource is "upload", "photo" or "mail"; agentJobStatus is "queued", "running", "done", "dead"
 * or "cancelled". The dates are RFC 3339; outcomeText is empty while queued and runConversationId until a run began.
 */
@Serializable
data class FinanceImport(
    val agentJobId: String,
    val financeImportKind: String? = null,
    val financeImportSource: String? = null,
    val agentJobStatus: String? = null,
    val fileName: String? = null,
    val agentAttachmentId: String? = null,
    val attachmentContentType: String? = null,
    val mailboxItemId: String? = null,
    val queuedAt: String? = null,
    val finishedAt: String? = null,
    val outcomeText: String? = null,
    val runConversationId: String? = null,
)

@Serializable
data class FinanceImportPage(
    val financeImports: List<FinanceImport?>,
    val hasMore: Boolean? = null,
    val totalCount: Int? = null,
)
