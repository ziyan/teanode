import { describe, expect, it } from 'vitest'

import { en } from '../../i18n/en'
import { Key, Values } from '../../i18n/i18n'
import type { FinanceStatementImport } from './financeApi'
import { statementImportSummary } from './statementImportSummary'

// The English catalogue with its placeholders filled in, as t() does.
const english = (key: Key, values?: Values) =>
  (en[key] as string).replace(/\{(\w+)\}/g, (whole, name: string) =>
    values && name in values ? String(values[name]) : whole,
  )

const imported = (overrides: Partial<FinanceStatementImport>): FinanceStatementImport => ({
  importedAt: '2026-02-01T10:00:00Z',
  statementImportOrigin: 'upload',
  statementFileNames: ['Invented Card Transactions.ofx'],
  addedTransactionCount: 0,
  updatedTransactionCount: 0,
  unchangedTransactionCount: 0,
  skippedTransactionCount: 0,
  transactionWithoutFitIdCount: 0,
  financeAccountIds: [],
  financeAccountNames: [],
  ...overrides,
})

describe('statementImportSummary', () => {
  it('names the account and the counts', () => {
    expect(
      statementImportSummary(
        english,
        imported({
          financeAccountIds: ['account-one'],
          financeAccountNames: ['Invented Card Issuer ··1a11'],
          addedTransactionCount: 12,
          updatedTransactionCount: 1,
          unchangedTransactionCount: 40,
        }),
      ),
    ).toBe('Imported into Invented Card Issuer ··1a11: 12 added, 1 updated, 40 already here.')
  })

  it('says why nothing was imported', () => {
    expect(statementImportSummary(english, imported({ importErrorMessage: 'export.csv could not be read' }))).toBe(
      'Nothing was imported: export.csv could not be read.',
    )
    expect(statementImportSummary(english, imported({}))).toBe('Nothing was imported: the file held no account.')
  })

  it('says what was left out beside what went in', () => {
    const said = statementImportSummary(
      english,
      imported({
        financeAccountNames: ['Invented Card Issuer ··1a11'],
        addedTransactionCount: 2,
        importErrorMessage: 'second.ofx could not be read',
      }),
    )
    expect(said).toContain('2 added')
    expect(said).toContain('Not imported: second.ofx could not be read.')
  })
})
