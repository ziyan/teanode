import { useEffect, useState } from 'react'
import { graphql } from '../api'
import { formatClock } from './common'
import { FormDialog } from './dialog'
import { SecretDialog } from './settingsList'
import { useTranslation } from '../i18n/i18n'

export type DeviceSetupCapabilities = {
  canSetUpMail: boolean
  canSetUpCalendar: boolean
  canSetUpReminders: boolean
  canSetUpContacts: boolean
  mailboxes: { id: string; name: string; addresses: { address: string }[] }[]
}
export const DEVICE_SETUP_CAPABILITIES = `query {
 GetDeviceSetupCapabilities { canSetUpMail canSetUpCalendar canSetUpReminders canSetUpContacts mailboxes { id name addresses { address } } }
}`
const CREATE_DEVICE_SETUP_PROFILE = `mutation ($deviceName: String!, $mailboxId: String, $hasMailAccess: Boolean!, $hasCalendarAccess: Boolean!, $hasRemindersAccess: Boolean!, $hasContactsAccess: Boolean!) {
 CreateDeviceSetupProfile(deviceName: $deviceName, mailboxId: $mailboxId, hasMailAccess: $hasMailAccess, hasCalendarAccess: $hasCalendarAccess, hasRemindersAccess: $hasRemindersAccess, hasContactsAccess: $hasContactsAccess) {
 downloadLink qrCodeSvg expiresAt mailboxAppPassword { id name } syncCredential { id credentialName }
 }
}`
type CreatedDeviceSetupProfile = {
  downloadLink: string
  qrCodeSvg: string
  expiresAt: string
  mailboxAppPassword: { id: string; name: string } | null
  syncCredential: { id: string; credentialName: string } | null
}

export function DeviceSetupDialog({
  capabilities,
  selectedMailboxId,
  onClose,
  onCreated,
}: {
  capabilities: DeviceSetupCapabilities
  selectedMailboxId?: string | null
  onClose: () => void
  onCreated: () => Promise<unknown>
}) {
  const { t } = useTranslation()
  const requestedMailbox = capabilities.mailboxes.find((mailbox) => mailbox.id === selectedMailboxId)
  const [deviceName, setDeviceName] = useState('')
  const [mailboxId, setMailboxId] = useState(requestedMailbox?.id ?? '')
  const [hasMailAccess, setHasMailAccess] = useState(capabilities.canSetUpMail && !!requestedMailbox)
  const [hasCalendarAccess, setHasCalendarAccess] = useState(capabilities.canSetUpCalendar)
  const [hasRemindersAccess, setHasRemindersAccess] = useState(capabilities.canSetUpReminders)
  const [hasContactsAccess, setHasContactsAccess] = useState(capabilities.canSetUpContacts)
  const [isCreating, setIsCreating] = useState(false)
  const [errorMessage, setErrorMessage] = useState<string | null>(null)
  const [createdProfile, setCreatedProfile] = useState<CreatedDeviceSetupProfile | null>(null)
  const [isProfileExpired, setIsProfileExpired] = useState(false)
  useEffect(() => {
    if (!createdProfile) return
    const profileValidityRemainingMS = Date.parse(createdProfile.expiresAt) - Date.now()
    const timer = window.setTimeout(() => setIsProfileExpired(true), Math.max(profileValidityRemainingMS, 0))
    return () => window.clearTimeout(timer)
  }, [createdProfile])
  const selection = {
    hasMailAccess: hasMailAccess && capabilities.canSetUpMail,
    hasCalendarAccess: hasCalendarAccess && capabilities.canSetUpCalendar,
    hasRemindersAccess: hasRemindersAccess && capabilities.canSetUpReminders,
    hasContactsAccess: hasContactsAccess && capabilities.canSetUpContacts,
  }
  const canCreate =
    deviceName.trim() !== '' &&
    Object.values(selection).some(Boolean) &&
    (!selection.hasMailAccess || capabilities.mailboxes.some((mailbox) => mailbox.id === mailboxId))
  const create = async () => {
    if (!canCreate || isCreating) return
    setIsCreating(true)
    setErrorMessage(null)
    try {
      const response = await graphql<{ CreateDeviceSetupProfile: CreatedDeviceSetupProfile }>(
        CREATE_DEVICE_SETUP_PROFILE,
        {
          deviceName: deviceName.trim(),
          mailboxId: selection.hasMailAccess ? mailboxId : null,
          ...selection,
        },
      )
      const createdDeviceProfile = response.CreateDeviceSetupProfile
      const profileExpiryMS = Date.parse(createdDeviceProfile.expiresAt)
      setIsProfileExpired(!Number.isFinite(profileExpiryMS) || profileExpiryMS <= Date.now())
      setCreatedProfile(createdDeviceProfile)
      await onCreated()
    } catch (caught) {
      setErrorMessage(caught instanceof Error ? caught.message : String(caught))
    } finally {
      setIsCreating(false)
    }
  }
  if (createdProfile)
    return (
      <SecretDialog
        title={t('deviceSetup.ready', { deviceName })}
        intro={t(isProfileExpired ? 'deviceSetup.profileExpired' : 'deviceSetup.profileOnce')}
        secret={createdProfile.downloadLink}
        onDone={onClose}
        extra={
          <>
            {!isProfileExpired ? (
              <img
                className="connect-qr"
                src={`data:image/svg+xml;utf8,${encodeURIComponent(createdProfile.qrCodeSvg)}`}
                alt={t('mailboxSettings.setupProfileQrAlt')}
              />
            ) : null}
            <p className="muted">
              {t('mailboxSettings.setupProfileExpires', { time: formatClock(createdProfile.expiresAt) })}
            </p>
            <div className="page-actions">
              {isProfileExpired ? (
                <button type="button" className="primary" disabled>
                  {t('deviceSetup.openProfile')}
                </button>
              ) : (
                <a
                  className="button primary"
                  href={createdProfile.downloadLink}
                  target="_blank"
                  rel="noopener noreferrer"
                  onClick={(event) => {
                    const profileExpiryMS = Date.parse(createdProfile.expiresAt)
                    if (!Number.isFinite(profileExpiryMS) || profileExpiryMS <= Date.now()) {
                      event.preventDefault()
                      setIsProfileExpired(true)
                    }
                  }}
                >
                  {t('deviceSetup.openProfile')}
                </a>
              )}
            </div>
            <p className="muted">{t('deviceSetup.revocationHint')}</p>
          </>
        }
      />
    )
  return (
    <FormDialog
      title={t('deviceSetup.chooseServices')}
      submitLabel={t('services.createProfile')}
      busy={isCreating}
      error={errorMessage}
      canSubmit={canCreate}
      onSubmit={() => void create()}
      onClose={() => {
        if (!isCreating) onClose()
      }}
    >
      <p className="muted">{t('deviceSetup.selectionHint')}</p>
      {!capabilities.canSetUpMail &&
      !capabilities.canSetUpCalendar &&
      !capabilities.canSetUpReminders &&
      !capabilities.canSetUpContacts ? (
        <p className="muted">{t('deviceSetup.noServices')}</p>
      ) : null}
      {selectedMailboxId && !requestedMailbox ? <p className="muted">{t('deviceSetup.mailboxUnavailable')}</p> : null}
      <label>
        <span>{t('services.deviceName')}</span>
        <input
          value={deviceName}
          maxLength={128}
          onChange={(event) => setDeviceName(event.target.value)}
          autoComplete="off"
          required
        />
      </label>
      {capabilities.canSetUpMail ? (
        <>
          <label className="checkbox">
            <input
              type="checkbox"
              checked={selection.hasMailAccess}
              onChange={(event) => setHasMailAccess(event.target.checked)}
            />
            {t('nav.mail')}
          </label>
          {selection.hasMailAccess ? (
            <label>
              <span>{t('deviceSetup.mailbox')}</span>
              <select value={mailboxId} onChange={(event) => setMailboxId(event.target.value)}>
                <option value="">{t('deviceSetup.chooseMailbox')}</option>
                {capabilities.mailboxes.map((mailbox) => (
                  <option key={mailbox.id} value={mailbox.id}>
                    {mailbox.name} ({mailbox.addresses.map((address) => address.address).join(', ')})
                  </option>
                ))}
              </select>
            </label>
          ) : null}
        </>
      ) : null}
      {capabilities.canSetUpCalendar ? (
        <label className="checkbox">
          <input
            type="checkbox"
            checked={selection.hasCalendarAccess}
            onChange={(event) => setHasCalendarAccess(event.target.checked)}
          />
          {t('nav.calendar')}
        </label>
      ) : null}
      {capabilities.canSetUpReminders ? (
        <label className="checkbox">
          <input
            type="checkbox"
            checked={selection.hasRemindersAccess}
            onChange={(event) => setHasRemindersAccess(event.target.checked)}
          />
          {t('services.remindersLabel')}
        </label>
      ) : null}
      {capabilities.canSetUpContacts ? (
        <label className="checkbox">
          <input
            type="checkbox"
            checked={selection.hasContactsAccess}
            onChange={(event) => setHasContactsAccess(event.target.checked)}
          />
          {t('nav.contacts')}
        </label>
      ) : null}
      <p className="muted">{t('deviceSetup.credentialsHint')}</p>
    </FormDialog>
  )
}
