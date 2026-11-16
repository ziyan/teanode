package com.teanode.kit.finance

/**
 * The GraphQL of this part, the same text as the iPhone app's (ios/TeaNodeKit). The server's tests check the iPhone
 * app's documents against its schema, so a document copied from there is checked too.
 */
object FinanceStatementDocuments {
    const val STATEMENT_IMPORT = $$"""query {
          StatementImport {
            sourceId importAddress isEnabled
            lastStatementImport { importedAt statementImportOrigin addedTransactionCount updatedTransactionCount unchangedTransactionCount financeAccountNames importErrorMessage }
          }
        }"""

    const val REGENERATE_STATEMENT_IMPORT_ADDRESS = $$"""mutation {
          RegenerateStatementImportAddress {
            sourceId importAddress isEnabled
            lastStatementImport { importedAt statementImportOrigin addedTransactionCount updatedTransactionCount unchangedTransactionCount financeAccountNames importErrorMessage }
          }
        }"""

    const val IMPORT_STATEMENT = $$"""mutation ($agentAttachmentId: String) {
          ImportStatement(agentAttachmentId: $agentAttachmentId) {
            importedAt statementImportOrigin addedTransactionCount updatedTransactionCount unchangedTransactionCount financeAccountNames importErrorMessage
          }
        }"""

}
