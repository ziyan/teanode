import { useCallback, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { graphql, openAgentConversation } from '../../api'
import { ErrorMessage } from '../../components/common'
import { Column, DataTable, Range } from '../../components/dataTable'
import { ZoomablePicture } from '../../components/lightbox'
import { usePageInAddress } from '../../components/pager'
import { RelativeTime } from '../../components/relativeTime'
import { SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { AGENT_ATTACHMENTS_PATH, FINANCE_IMPORTS, FinanceImport, FinanceImportPage } from './financeApi'

// ImportFile shows the file an import read: a picture as a small thumbnail
// that opens large on a tap, any other file as an Open file button, and for
// a mailed file an Open message button. A message moves between folders, so
// its folder is asked for when it is opened, as the receipts list does.
function ImportFile({ financeImport, name }: { financeImport: FinanceImport; name: string }) {
  const { t } = useTranslation()
  const toast = useToast()
  const navigate = useNavigate()
  const source = financeImport.agentAttachmentId
    ? `${AGENT_ATTACHMENTS_PATH}/${encodeURIComponent(financeImport.agentAttachmentId)}`
    : ''
  const isPicture = financeImport.attachmentContentType.startsWith('image/')
  const openMessage = async () => {
    try {
      const answer = await graphql<{ GetMailboxItem: { id: string; folderId: string } }>(
        'query ($itemId: String!) { GetMailboxItem(itemId: $itemId) { id folderId } }',
        { itemId: financeImport.mailboxItemId },
      )
      navigate(`/mailbox/${answer.GetMailboxItem.folderId}/${answer.GetMailboxItem.id}`)
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    }
  }
  if (!source && !financeImport.mailboxItemId) return null
  return (
    <div className="finance-import-file">
      {source && isPicture ? (
        <ZoomablePicture
          source={source}
          name={name}
          imageClassName="finance-import-thumbnail"
          openTitle={t('finance.importZoomPicture')}
        />
      ) : null}
      {source && !isPicture ? (
        <a
          className="button"
          href={source}
          target="_blank"
          rel="noopener noreferrer"
          aria-label={t('finance.importOpenFileOf', { name })}
        >
          {t('finance.importOpenFile')}
        </a>
      ) : null}
      {financeImport.mailboxItemId ? (
        <button
          type="button"
          className="button"
          aria-label={t('finance.importOpenMessageOf', { name })}
          onClick={() => void openMessage()}
        >
          {t('finance.importOpenMessage')}
        </button>
      ) : null}
    </div>
  )
}

// The Imports section: every file read into Finance in the background,
// newest first, as teanode finance imports lists them. A file is read as a
// tax document, a statement or a receipt, and arrives uploaded, picked out
// of the backed-up photos, or attached to mail. The server pages them: the
// page is in the address (?page=), as in the other lists. A row whose file
// has been read opens the run that read it, in the conversation drawer.
export function FinanceImportsSection() {
  const { t, plural, language } = useTranslation()
  const { pageIndex } = usePageInAddress()
  // The table says which page and how many rows to ask for once it is drawn.
  const [range, setRange] = useState<Range | null>(null)
  const onRange = useCallback((next: Range) => setRange(next), [])
  const offset = range === null ? null : pageIndex * range.limit
  const importsQuery = useQuery(
    async () => {
      if (!range || offset === null) return null
      const answer = await graphql<{ FinanceImports?: FinanceImportPage | null }>(FINANCE_IMPORTS, {
        limit: range.limit,
        offset,
      })
      return answer?.FinanceImports ?? { financeImports: [], hasMore: false, totalCount: 0 }
    },
    [offset, range?.limit],
    { refresh: false },
  )
  const shownPage = importsQuery.data ?? null
  const financeImports = useMemo(() => shownPage?.financeImports ?? [], [shownPage])
  const totalCount = shownPage?.totalCount ?? 0
  const fallbackName = useRef(t('finance.importNoFileName'))
  fallbackName.current = t('finance.importNoFileName')

  const importName = (financeImport: FinanceImport) => financeImport.fileName || fallbackName.current
  const countWords = (count: number) =>
    plural(
      count,
      { one: 'finance.importCountOne', other: 'finance.importCountOther' },
      { count: count.toLocaleString(language) },
    )

  const columns: Column<FinanceImport>[] = [
    {
      key: 'file',
      header: t('finance.importFile'),
      render: (financeImport) => (
        <>
          <div>{importName(financeImport)}</div>
          <div className="muted">
            {t(`finance.importKind.${financeImport.financeImportKind}`)}
            {' · '}
            {t(`finance.importSource.${financeImport.financeImportSource}`)}
          </div>
          <ImportFile financeImport={financeImport} name={importName(financeImport)} />
        </>
      ),
    },
    {
      key: 'status',
      header: t('finance.importStatus'),
      render: (financeImport) => (
        <>
          <div>{t(`finance.importState.${financeImport.agentJobStatus}`)}</div>
          <div className="muted">
            <RelativeTime value={financeImport.queuedAt} />
          </div>
        </>
      ),
    },
    {
      key: 'outcome',
      header: t('finance.importOutcome'),
      wrap: true,
      render: (financeImport) => (
        <>
          <div>{financeImport.outcomeText || t('finance.importNoOutcome')}</div>
          {financeImport.runConversationId ? (
            <button
              type="button"
              className="button"
              aria-label={t('finance.importOpenRunOf', { name: importName(financeImport) })}
              onClick={() => openAgentConversation(financeImport.runConversationId)}
            >
              {t('finance.importOpenRun')}
            </button>
          ) : null}
        </>
      ),
    },
  ]

  return (
    <SettingsSection card title={t('finance.importsTitle')} description={t('finance.importsHint')}>
      {shownPage ? <p className="muted finance-transaction-count">{countWords(totalCount)}</p> : null}
      <ErrorMessage error={importsQuery.error} />
      <div className="finance-imports-table">
        <DataTable
          columns={columns}
          rows={financeImports}
          rowKey={(financeImport) => financeImport.agentJobId}
          loading={!shownPage}
          remote={{ total: totalCount, onRange }}
          emptyMessage={t('finance.importsEmpty')}
          countLabel={countWords}
        />
      </div>
    </SettingsSection>
  )
}
