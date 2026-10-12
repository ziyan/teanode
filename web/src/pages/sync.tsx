import { useState } from 'react'
import { graphql } from '../api'
import { ErrorMessage, Loading } from '../components/common'
import { ConfirmDialog, FormDialog } from '../components/dialog'
import { SettingsSection, SettingsRow, SettingsEmpty, SecretDialog } from '../components/settingsList'
import { TrashIcon } from '../components/icons'
import { useQuery } from '../components/useQuery'
import { useSession } from '../session'
import { useTranslation } from '../i18n/i18n'

type Credential = {
  id: string
  credentialName: string
  hasCalendarAccess: boolean
  hasRemindersAccess: boolean
  hasContactsAccess: boolean
}
const CREDENTIAL = `id credentialName hasCalendarAccess hasRemindersAccess hasContactsAccess`
const LIST = `{ ListSyncCredentials { ${CREDENTIAL} } }`
const CREATE = `mutation ($credentialName: String!, $hasCalendarAccess: Boolean!, $hasRemindersAccess: Boolean!, $hasContactsAccess: Boolean!) {
 CreateSyncCredential(credentialName: $credentialName, hasCalendarAccess: $hasCalendarAccess, hasRemindersAccess: $hasRemindersAccess, hasContactsAccess: $hasContactsAccess) { username password principalPath credential { ${CREDENTIAL} } }
}`
const PROFILE = `mutation ($credentialName: String!, $hasCalendarAccess: Boolean!, $hasRemindersAccess: Boolean!, $hasContactsAccess: Boolean!) {
 CreateSyncSetupProfile(credentialName: $credentialName, hasCalendarAccess: $hasCalendarAccess, hasRemindersAccess: $hasRemindersAccess, hasContactsAccess: $hasContactsAccess) { downloadLink expiresAt credential { ${CREDENTIAL} } }
}`

const REVOKE = `mutation ($credentialId: String!) { RevokeSyncCredential(credentialId: $credentialId) }`
export function SyncPage() {
  const { t } = useTranslation()
  const session = useSession()
  const { data, error, loading, reload } = useQuery(() => graphql<{ ListSyncCredentials: Credential[] }>(LIST))
  const [credentialName, setCredentialName] = useState('')
  const [hasCalendarAccess, setHasCalendarAccess] = useState(false)
  const [hasRemindersAccess, setHasRemindersAccess] = useState(false)
  const [hasContactsAccess, setHasContactsAccess] = useState(false)
  const [isBusy, setIsBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)
  const [isCreating, setIsCreating] = useState(false)
  const [revoking, setRevoking] = useState<Credential | null>(null)
  const [created, setCreated] = useState<{ username: string; password: string; principalPath: string } | null>(null)
  const [downloadLink, setDownloadLink] = useState<string | null>(null)
  async function create(isProfile: boolean) {
    setIsBusy(true)
    setProblem(null)
    const variables = {
      credentialName,
      hasCalendarAccess: hasCalendarAccess && session.services?.isCalendarEnabled !== false,
      hasRemindersAccess: hasRemindersAccess && session.services?.isRemindersEnabled !== false,
      hasContactsAccess: hasContactsAccess && session.services?.isContactsEnabled !== false,
    }
    try {
      if (isProfile) {
        const response = await graphql<{ CreateSyncSetupProfile: { downloadLink: string } }>(PROFILE, variables)
        setDownloadLink(response.CreateSyncSetupProfile.downloadLink)
      } else {
        const response = await graphql<{
          CreateSyncCredential: { username: string; password: string; principalPath: string }
        }>(CREATE, variables)
        setCreated(response.CreateSyncCredential)
      }
      setIsCreating(false)
      await reload()
    } catch (caught) {
      setProblem(String(caught))
    } finally {
      setIsBusy(false)
    }
  }
  async function revoke() {
    if (!revoking) return
    setIsBusy(true)
    setProblem(null)
    try {
      await graphql(REVOKE, { credentialId: revoking.id })
      setRevoking(null)
      await reload()
    } catch (caught) {
      setProblem(String(caught))
    } finally {
      setIsBusy(false)
    }
  }
  if (loading) return <Loading />
  const canCreate =
    credentialName.trim() !== '' &&
    ((hasCalendarAccess && session.services?.isCalendarEnabled !== false) ||
      (hasRemindersAccess && session.services?.isRemindersEnabled !== false) ||
      (hasContactsAccess && session.services?.isContactsEnabled !== false))
  const hasCollections =
    session.services?.isCalendarEnabled || session.services?.isRemindersEnabled || session.services?.isContactsEnabled
  return (
    <>
      <SettingsSection
        card
        title={t('services.sync')}
        description={t('services.syncDescription')}
        action={
          <button
            type="button"
            disabled={!hasCollections}
            onClick={() => {
              setProblem(null)
              setIsCreating(true)
            }}
          >
            {t('common.create')}
          </button>
        }
      >
        <ErrorMessage error={error} />
        {!data?.ListSyncCredentials.length && <SettingsEmpty>{t('services.noDevices')}</SettingsEmpty>}
        {data?.ListSyncCredentials.map((credential) => (
          <SettingsRow
            key={credential.id}
            title={credential.credentialName}
            subtitle={[
              credential.hasCalendarAccess && t('nav.calendar'),
              credential.hasRemindersAccess && t('services.remindersLabel'),
              credential.hasContactsAccess && t('nav.contacts'),
            ]
              .filter(Boolean)
              .join(', ')}
            actions={
              <button
                type="button"
                className="icon-action danger"
                title={t('services.revoke')}
                aria-label={credential.credentialName + ': ' + t('services.revoke')}
                onClick={() => {
                  setProblem(null)
                  setRevoking(credential)
                }}
              >
                <TrashIcon size={16} />
              </button>
            }
          />
        ))}
      </SettingsSection>
      {isCreating && (
        <FormDialog
          title={t('services.sync')}
          submitLabel={t('services.createCredential')}
          busy={isBusy}
          error={problem}
          canSubmit={canCreate}
          onSubmit={() => void create(false)}
          onClose={() => {
            if (!isBusy) setIsCreating(false)
          }}
          otherAction={
            <button
              type="button"
              disabled={!canCreate || isBusy || location.protocol !== 'https:'}
              onClick={() => void create(true)}
            >
              {t('services.createProfile')}
            </button>
          }
        >
          <label>
            {t('services.deviceName')}
            <input
              value={credentialName}
              maxLength={128}
              onChange={(event) => setCredentialName(event.target.value)}
              autoComplete="off"
            />
          </label>
          {session.services?.isCalendarEnabled && (
            <label className="checkbox">
              <input
                type="checkbox"
                checked={hasCalendarAccess}
                onChange={(event) => setHasCalendarAccess(event.target.checked)}
              />
              {t('nav.calendar')}
            </label>
          )}
          {session.services?.isRemindersEnabled && (
            <label className="checkbox">
              <input
                type="checkbox"
                checked={hasRemindersAccess}
                onChange={(event) => setHasRemindersAccess(event.target.checked)}
              />
              {t('services.remindersLabel')}
            </label>
          )}
          {session.services?.isContactsEnabled && (
            <label className="checkbox">
              <input
                type="checkbox"
                checked={hasContactsAccess}
                onChange={(event) => setHasContactsAccess(event.target.checked)}
              />
              {t('nav.contacts')}
            </label>
          )}
        </FormDialog>
      )}
      {created && (
        <SecretDialog
          title={t('services.sync')}
          intro={t('services.passwordOnce')}
          secret={created.password}
          extra={
            <>
              <p>{created.username}</p>
              <p>
                <code>{new URL(created.principalPath, location.origin).href}</code>
              </p>
            </>
          }
          onDone={() => setCreated(null)}
        />
      )}
      {downloadLink && (
        <ConfirmDialog
          title={t('services.createProfile')}
          body={<a href={downloadLink}>{t('services.download')}</a>}
          onClose={() => setDownloadLink(null)}
        />
      )}
      {revoking && (
        <ConfirmDialog
          title={t('services.revoke')}
          body={t('services.revokeDetail')}
          confirmLabel={t('services.revoke')}
          busy={isBusy}
          error={problem}
          onConfirm={() => void revoke()}
          onClose={() => {
            if (!isBusy) setRevoking(null)
          }}
        />
      )}
    </>
  )
}
