import { useEffect, useRef, useState } from 'react'

import { graphql } from '../../api'
import { ErrorMessage, Loading, Tag, formatTime } from '../../components/common'
import { FINANCE_LINK_PATH } from '../../components/dashboardPath'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { KeyIcon, RefreshIcon, ToggleOffIcon, ToggleOnIcon, TrashIcon } from '../../components/icons'
import { Select } from '../../components/select'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../../components/settingsList'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import {
  DELETE_SOURCE,
  FINANCE_PROVIDERS,
  FINANCE_SOURCES,
  FinanceProvider,
  FinanceSource,
  IMPORT_FINANCE_CREDENTIAL,
  LINK_SIMPLEFIN,
  ProviderKind,
  SWITCH_SOURCE,
  SYNC_SOURCE,
} from './financeApi'
import { Money, accountLabel, useAct, useFinanceWords } from './financeCommon'

// openFinanceLink opens Plaid's page in a window of its own, to link a new
// institution or, with a finance source, to sign in to it again. A window
// rather than this tab, so the Finance tab is still here when it closes.
function openFinanceLink(sourceId?: string): Window | null {
  const address = sourceId ? `${FINANCE_LINK_PATH}?source=${encodeURIComponent(sourceId)}` : FINANCE_LINK_PATH
  return window.open(address, 'teanode-finance-link', 'popup,width=520,height=760')
}

// The finance sources: each institution linked, through which provider,
// the finance accounts it reports, when it last synced and what went
// wrong, with the controls every source has (sync, the switch, delete) and
// Sign in again when the institution asks for it.
export function FinanceSourcesSection() {
  const { t } = useTranslation()
  const words = useFinanceWords()
  const sources = useQuery(() => graphql<{ FinanceSources: FinanceSource[] }>(FINANCE_SOURCES), [], { refresh: true })
  const providers = useQuery(() => graphql<{ FinanceProviders: FinanceProvider[] }>(FINANCE_PROVIDERS), [], {
    refresh: false,
  })
  const { busy, act, run } = useAct(sources.reload)
  const [linking, setLinking] = useState(false)
  const [providerKind, setProviderKind] = useState<ProviderKind>('plaid')
  const [setupToken, setSetupToken] = useState('')
  // Bringing in a connection made elsewhere rather than linking a new one:
  // secondary, and closed until asked for, since most people link.
  const [isImporting, setIsImporting] = useState(false)
  const [credential, setCredential] = useState('')
  const [importInstitutionName, setImportInstitutionName] = useState('')
  const [deleting, setDeleting] = useState<FinanceSource | null>(null)

  // The window Plaid runs in, watched so the list is read again the moment
  // it closes: a new finance source appears without a reload.
  const linkWindow = useRef<Window | null>(null)
  const reload = sources.reload
  useEffect(() => {
    const timer = window.setInterval(() => {
      if (linkWindow.current && linkWindow.current.closed) {
        linkWindow.current = null
        void reload(true)
      }
    }, 1000)
    const onFocus = () => void reload(true)
    window.addEventListener('focus', onFocus)
    return () => {
      window.clearInterval(timer)
      window.removeEventListener('focus', onFocus)
    }
  }, [reload])

  const providerList = providers.data?.FinanceProviders ?? []
  const offered = providerList.map((provider) => provider.providerKind)
  // Plaid signs the person in in its own window; SimpleFIN takes a pasted
  // setup token. Which is which is the server's to say.
  const isBrowserRequired =
    providerList.find((provider) => provider.providerKind === providerKind)?.isBrowserRequired ?? false
  const nameOf = (source: FinanceSource) => source.institutionName || source.name || t('finance.unnamedInstitution')
  const list = sources.data?.FinanceSources ?? []

  const openLinking = () => {
    setProviderKind(
      (providerList.find((provider) => provider.isBrowserRequired) ?? providerList[0])?.providerKind ?? 'plaid',
    )
    setSetupToken('')
    setIsImporting(false)
    setCredential('')
    setImportInstitutionName('')
    setLinking(true)
  }

  const importCredential = () => {
    void act(
      () =>
        graphql(IMPORT_FINANCE_CREDENTIAL, {
          providerKind,
          credential: credential.trim(),
          institutionName: importInstitutionName.trim() || null,
        }),
      t('finance.imported'),
    ).then((isDone) => {
      if (isDone) {
        setCredential('')
        setLinking(false)
      }
    })
  }

  const linkThroughPlaid = (sourceId?: string) => {
    linkWindow.current = openFinanceLink(sourceId)
  }

  return (
    <>
      <SettingsSection
        card
        title={t('finance.sourcesTitle')}
        description={t('finance.sourcesHint')}
        action={
          offered.length > 0 ? (
            <button type="button" className="primary" onClick={openLinking}>
              {t('finance.link')}
            </button>
          ) : undefined
        }
      >
        <ErrorMessage error={sources.error} />
        {sources.loading && !sources.data ? <Loading /> : null}
        {sources.data && list.length === 0 ? (
          <SettingsEmpty>{offered.length > 0 ? t('finance.noSources') : t('finance.noProviders')}</SettingsEmpty>
        ) : null}
        {list.map((source) => (
          <SettingsRow
            key={source.id}
            title={nameOf(source)}
            badge={
              <>
                <Tag value={words.provider(source.providerKind)} />
                {source.isSignInRequired ? <Tag value={t('finance.signInRequired')} tone="warn" /> : null}
                {!source.isEnabled ? <Tag value={t('finance.sourceOff')} tone="warn" /> : null}
              </>
            }
            subtitle={
              <>
                {source.financeAccounts.length === 0
                  ? t('finance.noAccountsYet')
                  : source.financeAccounts.map((account, index) => (
                      <span key={account.id}>
                        {index > 0 ? ' · ' : ''}
                        {accountLabel(account)}{' '}
                        <Money amount={account.currentBalance} currency={account.currencyCode} />
                      </span>
                    ))}
                <br />
                {/* A new source syncs on its own within the minute; saying so
                    keeps anybody from reaching for Sync to start it. The
                    list reads itself again in the background, so this line
                    turns into the last sync's time when it is done. */}
                {source.lastRunAt
                  ? t('finance.lastSync', { time: formatTime(source.lastRunAt) })
                  : source.isEnabled
                    ? t('finance.firstSyncUnderWay')
                    : t('finance.neverSynced')}
                {source.lastError ? (
                  <>
                    <br />
                    <span className="muted">{source.lastError}</span>
                  </>
                ) : null}
              </>
            }
            actions={
              <div className="row-actions">
                {source.isSignInRequired && source.providerKind === 'plaid' ? (
                  <Tooltip label={t('finance.signInAgain')}>
                    <button
                      type="button"
                      className="icon-action"
                      aria-label={`${nameOf(source)}: ${t('finance.signInAgain')}`}
                      onClick={() => linkThroughPlaid(source.id)}
                    >
                      <KeyIcon size={16} />
                    </button>
                  </Tooltip>
                ) : null}
                <Tooltip label={t('finance.syncNow')}>
                  <button
                    type="button"
                    className="icon-action"
                    disabled={busy}
                    aria-label={`${nameOf(source)}: ${t('finance.syncNow')}`}
                    onClick={() => void run(SYNC_SOURCE, { sourceId: source.id }, t('finance.syncing'))}
                  >
                    <RefreshIcon size={16} />
                  </button>
                </Tooltip>
                <Tooltip label={source.isEnabled ? t('finance.turnOff') : t('finance.turnOn')}>
                  <button
                    type="button"
                    className="icon-action"
                    disabled={busy}
                    aria-pressed={source.isEnabled}
                    aria-label={`${nameOf(source)}: ${source.isEnabled ? t('finance.turnOff') : t('finance.turnOn')}`}
                    onClick={() =>
                      void run(
                        SWITCH_SOURCE,
                        { sourceId: source.id, enabled: !source.isEnabled },
                        source.isEnabled ? t('finance.turnedOff') : t('finance.turnedOn'),
                      )
                    }
                  >
                    {source.isEnabled ? <ToggleOnIcon size={16} /> : <ToggleOffIcon size={16} />}
                  </button>
                </Tooltip>
                <Tooltip label={t('finance.deleteSource')}>
                  <button
                    type="button"
                    className="icon-action danger"
                    aria-label={`${nameOf(source)}: ${t('finance.deleteSource')}`}
                    onClick={() => setDeleting(source)}
                  >
                    <TrashIcon size={16} />
                  </button>
                </Tooltip>
              </div>
            }
          />
        ))}
      </SettingsSection>
      {linking ? (
        <FormDialog
          title={t('finance.link')}
          submitLabel={
            isImporting ? t('finance.importSubmit') : isBrowserRequired ? t('finance.openPlaid') : t('finance.linkSubmit')
          }
          busy={busy}
          canSubmit={isImporting ? credential.trim() !== '' : isBrowserRequired || setupToken.trim() !== ''}
          onClose={() => setLinking(false)}
          onSubmit={() => {
            if (isImporting) {
              importCredential()
              return
            }
            if (isBrowserRequired) {
              linkThroughPlaid()
              setLinking(false)
              return
            }
            void act(
              () => graphql(LINK_SIMPLEFIN, { setupToken: setupToken.trim() }),
              t('finance.linkedSimpleFIN'),
            ).then((isDone) => {
              if (isDone) {
                setSetupToken('')
                setLinking(false)
              }
            })
          }}
        >
          {offered.length > 1 ? (
            <label>
              <span>{t('finance.provider')}</span>
              <Select
                block
                value={providerKind}
                label={t('finance.provider')}
                options={offered.map((kind) => ({ value: kind, label: words.provider(kind) }))}
                onChange={(value) => setProviderKind(value as ProviderKind)}
              />
            </label>
          ) : null}
          {isImporting ? (
            <>
              <label>
                <span>{t('finance.credential')}</span>
                <input
                  type="password"
                  autoComplete="off"
                  spellCheck={false}
                  value={credential}
                  onChange={(event) => setCredential(event.target.value)}
                />
              </label>
              <p className="muted field-hint">
                {providerKind === 'plaid' ? t('finance.credentialHintPlaid') : t('finance.credentialHintSimpleFIN')}
              </p>
              <label>
                <span>{t('finance.importInstitutionName')}</span>
                <input
                  value={importInstitutionName}
                  placeholder={t('finance.importInstitutionNamePlaceholder')}
                  onChange={(event) => setImportInstitutionName(event.target.value)}
                />
              </label>
            </>
          ) : isBrowserRequired ? (
            <p className="muted">{t('finance.plaidHint')}</p>
          ) : (
            <>
              <label>
                <span>{t('finance.setupToken')}</span>
                <input
                  type="password"
                  autoComplete="off"
                  spellCheck={false}
                  value={setupToken}
                  onChange={(event) => setSetupToken(event.target.value)}
                />
              </label>
              <p className="muted field-hint">{t('finance.setupTokenHint')}</p>
            </>
          )}
          <p>
            <button
              type="button"
              aria-expanded={isImporting}
              onClick={() => setIsImporting((previous) => !previous)}
            >
              {isImporting ? t('finance.linkNewInstead') : t('finance.bringExisting')}
            </button>
          </p>
        </FormDialog>
      ) : null}
      {deleting ? (
        <ConfirmDialog
          title={t('finance.deleteSource')}
          body={
            <>
              <p className="muted">{t('finance.deleteSourceBody', { name: nameOf(deleting) })}</p>
              <p className="muted">
                {deleting.providerKind === 'plaid' ? t('finance.deletePlaidLimit') : t('finance.deleteSimpleFINRevoke')}
              </p>
            </>
          }
          confirmLabel={t('finance.deleteSource')}
          busy={busy}
          onClose={() => setDeleting(null)}
          onConfirm={() => {
            void run(DELETE_SOURCE, { sourceId: deleting.id }, t('finance.sourceDeleted')).then(() => setDeleting(null))
          }}
        />
      ) : null}
    </>
  )
}
