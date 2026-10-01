import type { Key, Values } from '../../i18n/i18n'
import type { FinanceStatementImport } from './financeApi'

// statementImportSummary says what an import did, in a sentence: into
// which accounts, how many transactions were added, updated or already
// there, and what was not imported and why. The toast after an upload and
// the panel's last import both say it, so they say it the same way.
export function statementImportSummary(
  t: (key: Key, values?: Values) => string,
  result: FinanceStatementImport,
): string {
  if (result.financeAccountNames.length === 0) {
    return t('finance.statementNothingImported', {
      reason: result.importErrorMessage || t('finance.statementNoAccount'),
    })
  }
  let said = t('finance.statementImported', {
    accounts: result.financeAccountNames.join(', '),
    added: result.addedTransactionCount,
    updated: result.updatedTransactionCount,
    unchanged: result.unchangedTransactionCount,
  })
  if (result.importErrorMessage) {
    said += ' ' + t('finance.statementPartlyImported', { reason: result.importErrorMessage })
  }
  return said
}
