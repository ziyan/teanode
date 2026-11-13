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
