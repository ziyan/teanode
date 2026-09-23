import { useState } from 'react'

import { graphql } from '../../api'
import { ErrorMessage, Loading, Tag, formatTime } from '../../components/common'
import { PencilIcon, UnlinkIcon } from '../../components/icons'
import { RelativeTime } from '../../components/relativeTime'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { SettingsEmpty, SettingsRow, SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { useTranslation } from '../../i18n/i18n'

const APPS = `
  query {
    ListApps {
      clientId name registeredName redirectHosts registered lastUsed lastUsedIp expires renewableUntil tokenCount
    }
  }`

const RENAME = `mutation ($clientId: String!, $name: String!) { RenameApp(clientId: $clientId, name: $name) }`

const DISCONNECT = `mutation ($clientId: String!) { DisconnectApp(clientId: $clientId) }`

type App = {
  clientId: string
  name: string
  registeredName: string
  redirectHosts: string[]
  registered?: string | null
  lastUsed?: string | null
  lastUsedIp?: string | null
  expires?: string | null
  renewableUntil?: string | null
  tokenCount: number
}

// AppsPage lists the apps that act as this person: programs they authorized,
// each holding a token of theirs that it renews by itself. The token changes
// at every renewal, so the app is what is named and disconnected here.
export function AppsPage() {
  const { t } = useTranslation()
  const toast = useToast()
  const { data, error, loading, reload } = useQuery(() => graphql<{ ListApps: App[] }>(APPS), [])

  const [renaming, setRenaming] = useState<App | null>(null)
  const [newName, setNewName] = useState('')
  const [disconnecting, setDisconnecting] = useState<App | null>(null)
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

  async function run(work: () => Promise<unknown>) {
    setBusy(true)
    setProblem(null)
    try {
      await work()
      await reload()
    } catch (caught) {
      setProblem(caught instanceof Error ? caught.message : t('domain.failed'))
      toast.failure(caught, t('domain.failed'))
    } finally {
      setBusy(false)
    }
  }

  const apps = data?.ListApps ?? []

  return (
    <>
      <SettingsSection description={t('apps.intro')}>
        {loading && !data && <Loading />}
        {error ? <ErrorMessage error={error} /> : null}
        {data && apps.length === 0 && <SettingsEmpty>{t('apps.empty')}</SettingsEmpty>}

        {apps.map((app) => (
          <SettingsRow
            key={app.clientId}
            title={app.name || app.registeredName || t('apps.unnamed')}
            badge={
              app.renewableUntil ? (
                <Tag value={t('apps.renewsUntil', { time: formatTime(app.renewableUntil) })} />
              ) : undefined
            }
            subtitle={
              <>
                {app.registeredName && app.registeredName !== app.name && (
                  <div>{t('apps.registeredAs', { name: app.registeredName })}</div>
                )}
                {app.redirectHosts.length > 0 && (
                  <div className="mono">{t('apps.returnsTo', { hosts: app.redirectHosts.join(', ') })}</div>
                )}
                <div>
                  {app.lastUsed ? (
                    <>
                      {t('tokens.lastUsedLabel')} <RelativeTime value={app.lastUsed} />
                      {app.lastUsedIp ? ` (${app.lastUsedIp})` : ''}
                    </>
                  ) : (
                    t('tokens.neverUsed')
                  )}
                  {app.tokenCount > 1 ? ` · ${t('apps.authorizedTimes', { count: app.tokenCount })}` : ''}
                </div>
              </>
            }
            actions={
              <>
                <Tooltip label={t('apps.rename')}>
                  <button
                    className="icon-action"
                    type="button"
                    aria-label={`${app.name}: ${t('apps.rename')}`}
                    onClick={() => {
                      setRenaming(app)
                      setNewName(app.name)
                    }}
                  >
                    <PencilIcon size={16} />
                  </button>
                </Tooltip>
                <Tooltip label={t('apps.disconnect')}>
                  <button
                    className="icon-action danger"
                    type="button"
                    aria-label={`${app.name}: ${t('apps.disconnect')}`}
                    onClick={() => setDisconnecting(app)}
                  >
                    <UnlinkIcon size={16} />
                  </button>
                </Tooltip>
              </>
            }
          />
        ))}
      </SettingsSection>

      {renaming && (
        <FormDialog
          title={t('apps.renameTitle')}
          submitLabel={t('common.save')}
          busy={busy}
          error={problem}
          canSubmit={newName.trim().length > 0}
          onClose={() => {
            setRenaming(null)
            setProblem(null)
          }}
          onSubmit={() =>
            void run(async () => {
              await graphql(RENAME, { clientId: renaming.clientId, name: newName.trim() })
              setRenaming(null)
              toast.done(t('apps.renamed'))
            })
          }
        >
          <label>
            <span>{t('apps.name')}</span>
            <input value={newName} onChange={(event) => setNewName(event.target.value)} />
          </label>
          <p className="muted">{t('apps.renameKeeps')}</p>
        </FormDialog>
      )}

      {disconnecting && (
        <ConfirmDialog
          title={t('apps.disconnectTitle')}
          body={t('apps.disconnectBody', { name: disconnecting.name })}
          confirmLabel={t('apps.disconnect')}
          busy={busy}
          onConfirm={() => {
            const clientId = disconnecting.clientId
            setDisconnecting(null)
            void run(() => graphql(DISCONNECT, { clientId }))
          }}
          onClose={() => setDisconnecting(null)}
        />
      )}
    </>
  )
}
