import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { graphql } from '../api'
import { ErrorMessage, Loading } from '../components/common'
import { ConfirmDialog, FormDialog } from '../components/dialog'
import { SettingsSection, SettingsRow, SettingsEmpty, SecretDialog } from '../components/settingsList'
import { TrashIcon } from '../components/icons'
import { useQuery } from '../components/useQuery'
import { hasPermission, useSession } from '../session'
import { PasswordUsage, sortNewestFirst } from '../components/passwordUsage'
import { useTranslation } from '../i18n/i18n'
import { DeviceSetupDialog, DEVICE_SETUP_CAPABILITIES, type DeviceSetupCapabilities } from '../components/deviceSetup'

type Credential = {
  id: string
  credentialName: string
  hasCalendarAccess: boolean
  hasRemindersAccess: boolean
  hasContactsAccess: boolean
  hasPhotosAccess: boolean
  createdAt: string
  lastUsedAt?: string | null
}
const CREDENTIAL = `id credentialName hasCalendarAccess hasRemindersAccess hasContactsAccess hasPhotosAccess createdAt lastUsedAt`
const LIST = `{ ListSyncCredentials { ${CREDENTIAL} } }`
const CREATE = `mutation ($credentialName: String!, $hasCalendarAccess: Boolean!, $hasRemindersAccess: Boolean!, $hasContactsAccess: Boolean!, $hasPhotosAccess: Boolean!) {
 CreateSyncCredential(credentialName: $credentialName, hasCalendarAccess: $hasCalendarAccess, hasRemindersAccess: $hasRemindersAccess, hasContactsAccess: $hasContactsAccess, hasPhotosAccess: $hasPhotosAccess) { username password principalPath credential { ${CREDENTIAL} } }
}`

const REVOKE = `mutation ($credentialId: String!) { RevokeSyncCredential(credentialId: $credentialId) }`
export function SyncPage() {
  const session = useSession()
  return <AccountDeviceSetupPage key={session.userId ?? ''} />
}

function AccountDeviceSetupPage() {
  const { t } = useTranslation()
  const session = useSession()
  const { data, error, loading, reload } = useQuery(() => graphql<{ ListSyncCredentials: Credential[] }>(LIST))
  const [credentialName, setCredentialName] = useState('')
  const [hasCalendarAccess, setHasCalendarAccess] = useState(false)
  const [hasRemindersAccess, setHasRemindersAccess] = useState(false)
  const [hasContactsAccess, setHasContactsAccess] = useState(false)
  const [hasPhotosAccess, setHasPhotosAccess] = useState(false)
  const [isBusy, setIsBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)
  const [isCreating, setIsCreating] = useState(false)
  const [revoking, setRevoking] = useState<Credential | null>(null)
  const [created, setCreated] = useState<{ username: string; password: string; principalPath: string } | null>(null)
  const [parameters, setParameters] = useSearchParams()
  const isSettingUp = parameters.get('setup') === '1'
  const capabilities = useQuery(
    () => graphql<{ GetDeviceSetupCapabilities: DeviceSetupCapabilities }>(DEVICE_SETUP_CAPABILITIES),
    [session.userId],
  )
  const availableServices = capabilities.data?.GetDeviceSetupCapabilities
  // Photos is not one of the services an Apple device is set up with, so the
  // capabilities do not list it; a device password may carry it when the
  // library is on and the person may keep one.
  const canUsePhotos = session.services?.isPhotosEnabled === true && hasPermission(session.permissions, 'photos:use')
  const canSetUp =
    !!availableServices &&
    (availableServices.canSetUpMail ||
      availableServices.canSetUpCalendar ||
      availableServices.canSetUpReminders ||
      availableServices.canSetUpContacts)
  async function create() {
    setIsBusy(true)
    setProblem(null)
    const variables = {
      credentialName,
      hasCalendarAccess: hasCalendarAccess && availableServices?.canSetUpCalendar === true,
      hasRemindersAccess: hasRemindersAccess && availableServices?.canSetUpReminders === true,
      hasContactsAccess: hasContactsAccess && availableServices?.canSetUpContacts === true,
      hasPhotosAccess: hasPhotosAccess && canUsePhotos,
    }
    try {
      const response = await graphql<{
        CreateSyncCredential: { username: string; password: string; principalPath: string }
      }>(CREATE, variables)
      setCreated(response.CreateSyncCredential)
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
  if (loading && !data) return <Loading />
  const canCreate =
    credentialName.trim() !== '' &&
    ((hasCalendarAccess && availableServices?.canSetUpCalendar === true) ||
      (hasRemindersAccess && availableServices?.canSetUpReminders === true) ||
      (hasContactsAccess && availableServices?.canSetUpContacts === true) ||
      (hasPhotosAccess && canUsePhotos))
  const hasCollections =
    availableServices?.canSetUpCalendar ||
    availableServices?.canSetUpReminders ||
    availableServices?.canSetUpContacts ||
    canUsePhotos
  return (
    <>
      <SettingsSection
        card
        title={t('deviceSetup.title')}
        description={t('deviceSetup.description')}
        action={
          <button
            type="button"
            className="primary"
            disabled={!canSetUp}
            onClick={() => {
              const nextParameters = new URLSearchParams(parameters)
              nextParameters.set('setup', '1')
              nextParameters.delete('mailboxId')
              setParameters(nextParameters)
            }}
          >
            {t(
              /iPhone|iPad|iPod/.test(navigator.userAgent)
                ? 'deviceSetup.setUpThisPhone'
                : 'mailboxSettings.setUpAppleDevice',
            )}
          </button>
        }
      >
        <ErrorMessage error={capabilities.error} />
        {capabilities.loading && !availableServices ? (
          <Loading />
        ) : !canSetUp ? (
          <SettingsEmpty>{t('deviceSetup.noServices')}</SettingsEmpty>
        ) : null}
      </SettingsSection>
      <SettingsSection
        card
        title={t('deviceSetup.manualSync')}
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
        {sortNewestFirst(data?.ListSyncCredentials ?? []).map((credential) => (
          <SettingsRow
            key={credential.id}
            title={credential.credentialName}
            subtitle={
              <>
                {[
                  credential.hasCalendarAccess && t('nav.calendar'),
                  credential.hasRemindersAccess && t('services.remindersLabel'),
                  credential.hasContactsAccess && t('nav.contacts'),
                  credential.hasPhotosAccess && t('nav.photos'),
                ]
                  .filter(Boolean)
                  .join(', ')}
                <br />
                <PasswordUsage password={credential} />
              </>
            }
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
          onSubmit={() => void create()}
          onClose={() => {
            if (!isBusy) setIsCreating(false)
          }}
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
          {availableServices?.canSetUpCalendar && (
            <label className="checkbox">
              <input
                type="checkbox"
                checked={hasCalendarAccess}
                onChange={(event) => setHasCalendarAccess(event.target.checked)}
              />
              {t('nav.calendar')}
            </label>
          )}
          {availableServices?.canSetUpReminders && (
            <label className="checkbox">
              <input
                type="checkbox"
                checked={hasRemindersAccess}
                onChange={(event) => setHasRemindersAccess(event.target.checked)}
              />
              {t('services.remindersLabel')}
            </label>
          )}
          {availableServices?.canSetUpContacts && (
            <label className="checkbox">
              <input
                type="checkbox"
                checked={hasContactsAccess}
                onChange={(event) => setHasContactsAccess(event.target.checked)}
              />
              {t('nav.contacts')}
            </label>
          )}
          {canUsePhotos && (
            <label className="checkbox">
              <input
                type="checkbox"
                checked={hasPhotosAccess}
                onChange={(event) => setHasPhotosAccess(event.target.checked)}
              />
              {t('nav.photos')}
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
      {isSettingUp && availableServices ? (
        <DeviceSetupDialog
          key={parameters.get('mailboxId') ?? 'account'}
          capabilities={availableServices}
          selectedMailboxId={parameters.get('mailboxId')}
          onCreated={reload}
          onClose={() => {
            const nextParameters = new URLSearchParams(parameters)
            nextParameters.delete('setup')
            nextParameters.delete('mailboxId')
            setParameters(nextParameters, { replace: true })
          }}
        />
      ) : null}
    </>
  )
}
