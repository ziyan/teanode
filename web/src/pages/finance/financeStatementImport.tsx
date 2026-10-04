import { useRef, useState } from 'react'

import { graphql } from '../../api'
import { CopyIconButton, ErrorMessage, Loading, formatTime } from '../../components/common'
import { ConfirmDialog } from '../../components/dialog'
import { RefreshIcon } from '../../components/icons'
import { SettingsRow, SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { uploadFiles } from '../../upload'
import {
  AGENT_ATTACHMENTS_PATH,
  FinanceStatementImport,
  IMPORT_STATEMENT,
  REGENERATE_STATEMENT_IMPORT_ADDRESS,
  STATEMENT_IMPORT,
  StatementImport,
} from './financeApi'
import { statementImportSummary } from './statementImportSummary'

// The statement import: for an account no provider reaches, such as a card
// whose wallet only exports OFX files. Its address is the person's own
// mailbox address with a token after a plus; mailing the exported file to
// it imports it. A file can be uploaded here too. The address is made the
// first time this panel is shown, and regenerating it ends the old one.
export function FinanceStatementImportSection({ onImported }: { onImported?: () => void }) {
  const { t } = useTranslation()
  const toast = useToast()
  const { data, error, loading, reload } = useQuery(
    () => graphql<{ StatementImport: StatementImport }>(STATEMENT_IMPORT),
    [],
    { refresh: false },
  )
  const [busy, setBusy] = useState(false)
  const [isRegenerating, setIsRegenerating] = useState(false)
  const picker = useRef<HTMLInputElement | null>(null)
  const statementImport = data?.StatementImport
  const last = statementImport?.lastStatementImport

  const upload = async (file: File) => {
    if (statementImport && file.size > statementImport.maximumStatementBytes) {
      toast.failure(new Error(t('finance.statementTooLarge', { name: file.name })), t('finance.failed'))
      return
    }
    setBusy(true)
    try {
      const uploaded = (await uploadFiles('POST', AGENT_ATTACHMENTS_PATH, [file], () => undefined).promise) as {
        attachments?: { id: string }[]
      }
      const attachmentId = uploaded?.attachments?.[0]?.id
      if (!attachmentId) throw new Error(t('finance.statementNotStored'))
      const answer = await graphql<{ ImportStatement: FinanceStatementImport }>(IMPORT_STATEMENT, {
        agentAttachmentId: attachmentId,
      })
      toast.done(statementImportSummary(t, answer.ImportStatement))
      await reload()
      onImported?.()
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    } finally {
      setBusy(false)
    }
  }

  const regenerate = async () => {
    setBusy(true)
    try {
      await graphql(REGENERATE_STATEMENT_IMPORT_ADDRESS)
      toast.done(t('finance.statementAddressRegenerated'))
      setIsRegenerating(false)
      await reload()
    } catch (caught) {
      toast.failure(caught, t('finance.failed'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <SettingsSection
        card
        title={t('finance.statementTitle')}
        description={t('finance.statementHint')}
        action={
          <button
            type="button"
            className="primary"
            disabled={busy || !statementImport}
            onClick={() => picker.current?.click()}
          >
            {t('finance.statementUpload')}
          </button>
        }
      >
        <input
          ref={picker}
          type="file"
          accept=".ofx,.qfx,.qbo,application/x-ofx,application/vnd.intu.qfx"
          hidden
          onChange={(event) => {
            const file = event.target.files?.[0]
            event.target.value = ''
            if (file) void upload(file)
          }}
        />
        <ErrorMessage error={error} />
        {loading && !data ? <Loading /> : null}
        {statementImport ? (
          <>
            <SettingsRow
              title={t('finance.statementAddress')}
              subtitle={
                statementImport.importAddress ? (
                  <code className="statement-address">{statementImport.importAddress}</code>
                ) : (
                  t('finance.statementNoAddress')
                )
              }
              actions={
                <div className="row-actions">
                  {statementImport.importAddress ? (
                    <CopyIconButton value={statementImport.importAddress} label={t('finance.statementCopyAddress')} />
                  ) : null}
                  <Tooltip label={t('finance.statementRegenerate')}>
                    <button
                      type="button"
                      className="icon-action"
                      disabled={busy}
                      aria-label={`${t('finance.statementAddress')}: ${t('finance.statementRegenerate')}`}
                      onClick={() => setIsRegenerating(true)}
                    >
                      <RefreshIcon size={16} />
                    </button>
                  </Tooltip>
                </div>
              }
            />
            {!statementImport.isEnabled ? <p className="muted">{t('finance.statementOff')}</p> : null}
            <p className="muted">{t('finance.statementHowTo')}</p>
            <p className="muted">{t('finance.statementScreenshots')}</p>
            <SettingsRow
              title={t('finance.statementLastImport')}
              subtitle={
                last ? (
                  <>
                    {formatTime(last.importedAt)}
                    {' · '}
                    {statementImportSummary(t, last)}
                  </>
                ) : (
                  t('finance.statementNeverImported')
                )
              }
            />
          </>
        ) : null}
      </SettingsSection>
      {isRegenerating ? (
        <ConfirmDialog
          title={t('finance.statementRegenerate')}
          body={<p className="muted">{t('finance.statementRegenerateBody')}</p>}
          confirmLabel={t('finance.statementRegenerateConfirm')}
          busy={busy}
          onClose={() => setIsRegenerating(false)}
          onConfirm={() => void regenerate()}
        />
      ) : null}
    </>
  )
}
