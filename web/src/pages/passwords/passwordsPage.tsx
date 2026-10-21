import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'

import { graphql } from '../../api'
import { ErrorMessage, Loading } from '../../components/common'
import { ConfirmDialog } from '../../components/dialog'
import { SettingsEmpty, SettingsSection } from '../../components/settingsList'
import { TabItem, Tabs } from '../../components/tabs'
import { useToast } from '../../components/toast'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { hasPermission, useSession } from '../../session'
import { PasswordsSetup, READ_PASSWORDS_SETUP } from './passwordsApi'
import { useFailureToast } from './passwordsErrors'
import { PasswordsItems } from './passwordsItems'
import { SafetyCode } from './passwordsParts'
import { KeyChange, passwordsSession, usePasswordsSession } from './passwordsSession'
import { PasswordsSettings } from './passwordsSettings'
import { PasswordsSetUp } from './passwordsSetup'
import { PasswordsUnlock } from './passwordsUnlock'

// Passwords: logins, passkeys, cards and notes that only the person's devices
// can read. Not set up, it explains itself and sets up; set up but locked, it
// asks for the passphrase; unlocked, it lists the items. What is open lives in
// memory (passwordsSession) and is gone on a reload or after the lock time.
//
// In the address: tab (Settings), item (the open item), edit, new (the kind of
// a new item), kind, deleted (Recently deleted) and page. The search text and
// the folder name are decrypted values, so they are kept out of the address
// (browser history and request logs) and live in the page's state.

const TABS: TabItem[] = [
  { id: 'items', label: 'passwords.tabItems' },
  { id: 'settings', label: 'passwords.tabSettings' },
]

// How often an open Passwords reads what other devices changed, and gives the
// people who set up Passwords since an item was shared the key to it.
const REFRESH_MILLISECONDS = 30_000

// canUseWebCrypto says whether this page can do the encryption: WebCrypto is
// only there on a secure origin (https, or localhost).
export function canUseWebCrypto(): boolean {
  return window.isSecureContext !== false && typeof crypto !== 'undefined' && crypto.subtle !== undefined
}

export function PasswordsPage() {
  const session = useSession()
  const { t } = useTranslation()
  if (!hasPermission(session.permissions, 'passwords:use')) {
    return (
      <SettingsSection card title={t('passwords.title')}>
        <SettingsEmpty>{t('passwords.off')}</SettingsEmpty>
      </SettingsSection>
    )
  }
  if (!canUseWebCrypto()) {
    return (
      <SettingsSection card title={t('passwords.title')}>
        <SettingsEmpty>{t('passwords.needsSecureContext')}</SettingsEmpty>
      </SettingsSection>
    )
  }
  return <PasswordsContent key={session.userId ?? ''} userId={session.userId ?? ''} />
}

function PasswordsContent({ userId }: { userId: string }) {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const reportFailure = useFailureToast()
  // keyChange is the person whose key is new to this device or is not the one
  // kept, being asked about; declinedKeys are the new keys already answered
  // no to, so the 30 second read does not ask again.
  const [keyChange, setKeyChange] = useState<KeyChange | null>(null)
  const [isTrusting, setIsTrusting] = useState(false)
  const declinedKeys = useRef(new Set<string>())
  const [parameters, setParameters] = useSearchParams()
  const snapshot = usePasswordsSession()
  const isUnlocked = snapshot.isUnlocked && snapshot.userId === userId

  function changeParameters(changes: Record<string, string | null>, options: { replace?: boolean } = {}) {
    setParameters(
      (previous) => {
        const written = new URLSearchParams(previous)
        for (const [parameterName, parameterValue] of Object.entries(changes)) {
          if (parameterValue === null) written.delete(parameterName)
          else written.set(parameterName, parameterValue)
        }
        return written
      },
      { replace: options.replace },
    )
  }

  // Passwords somebody else left open on this browser is closed.
  useEffect(() => {
    passwordsSession.lockIfOtherPerson(userId)
  }, [userId])

  const setup = useQuery(
    async () => (await graphql<{ ReadPasswordsSetup: PasswordsSetup }>(READ_PASSWORDS_SETUP)).ReadPasswordsSetup,
    [userId],
    { refresh: false },
  )
  useEffect(() => {
    if (setup.data) passwordsSession.setLockAfterMinutes(setup.data.lockAfterMinutes)
  }, [setup.data])

  // A question about a key does not outlive the unlocked session.
  useEffect(() => {
    if (!isUnlocked) setKeyChange(null)
  }, [isUnlocked])

  // Using the page puts the lock off.
  function touch() {
    passwordsSession.touch()
  }

  // While open: read what other devices changed, and complete the shares of
  // people who have set up Passwords since.
  useEffect(() => {
    if (!isUnlocked) return
    let isCurrent = true
    async function run(shouldRefresh: boolean) {
      try {
        if (shouldRefresh) await passwordsSession.refresh()
        const { sharedCount, changedKeys } = await passwordsSession.completeWaitingShares()
        if (isCurrent && sharedCount > 0) {
          toast.done(plural(sharedCount, { one: 'passwords.waitingSharedOne', other: 'passwords.waitingShared' }))
        }
        const asked = changedKeys.find((change) => !declinedKeys.current.has(change.newPublicKey))
        if (isCurrent && asked) setKeyChange((previous) => previous ?? asked)
      } catch {
        // Tried again at the next turn; a person is not told about a refresh
        // that did not go through.
      }
    }
    // Unlocking has just read the items.
    void run(false)
    const timer = window.setInterval(() => {
      if (document.visibilityState === 'visible') void run(true)
    }, REFRESH_MILLISECONDS)
    return () => {
      isCurrent = false
      window.clearInterval(timer)
    }
    // Once per unlock.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isUnlocked])

  async function trustKey(change: KeyChange) {
    setIsTrusting(true)
    try {
      const { sharedCount, changedKeys } = await passwordsSession.trustChangedKey(change)
      if (sharedCount > 0) {
        toast.done(plural(sharedCount, { one: 'passwords.waitingSharedOne', other: 'passwords.waitingShared' }))
      }
      const next = changedKeys.find((other) => !declinedKeys.current.has(other.newPublicKey))
      setKeyChange(next ?? null)
    } catch (failure) {
      reportFailure(failure, t('passwords.shareFailed'))
      setKeyChange(null)
    } finally {
      setIsTrusting(false)
    }
  }

  function declineKey(change: KeyChange) {
    declinedKeys.current.add(change.newPublicKey)
    setKeyChange(null)
  }

  const tab = parameters.get('tab') === 'settings' ? 'settings' : 'items'
  const data = setup.data

  return (
    <div className="passwords-page" onPointerDownCapture={touch} onKeyDownCapture={touch}>
      <ErrorMessage error={setup.error} />
      {setup.loading && !data && <Loading />}
      {setup.error !== null && data === null && !setup.loading && (
        <div className="page-actions">
          <button type="button" onClick={() => void setup.reload()}>
            {t('common.tryAgain')}
          </button>
        </div>
      )}

      {data && !data.isSetUp && <PasswordsSetUp userId={userId} onSetUp={() => void setup.reload()} />}

      {data && data.isSetUp && (
        <>
          <Tabs
            items={TABS}
            active={tab}
            onSelect={(id) => changeParameters({ tab: id === 'items' ? null : id, item: null, edit: null, new: null })}
          />
          {tab === 'settings' ? (
            <PasswordsSettings
              setup={data}
              onUnlock={() => changeParameters({ tab: null })}
              onSetupChanged={setup.reload}
              onErased={() => {
                changeParameters({ tab: null, item: null, edit: null, new: null })
                void setup.reload()
              }}
            />
          ) : isUnlocked ? (
            <PasswordsItems parameters={parameters} changeParameters={changeParameters} />
          ) : (
            <PasswordsUnlock userId={userId} />
          )}
        </>
      )}

      {keyChange && (
        <ConfirmDialog
          destructive={false}
          title={
            keyChange.oldPublicKey === null
              ? t('passwords.keyFirstTitle', { name: keyChange.recipientName || keyChange.recipientUsername })
              : t('passwords.keyChangedTitle', { name: keyChange.recipientName || keyChange.recipientUsername })
          }
          body={
            <>
              <p className="muted">
                {keyChange.oldPublicKey === null ? t('passwords.keyFirstBody') : t('passwords.keyChangedBody')}
              </p>
              {keyChange.oldPublicKey !== null && (
                <p>
                  <span className="muted">{t('passwords.keyChangedOld')} </span>
                  <SafetyCode publicKey={keyChange.oldPublicKey} />
                </p>
              )}
              <p>
                <span className="muted">
                  {keyChange.oldPublicKey === null ? t('passwords.safetyCode') : t('passwords.keyChangedNew')}{' '}
                </span>
                <SafetyCode publicKey={keyChange.newPublicKey} />
              </p>
            </>
          }
          confirmLabel={
            keyChange.oldPublicKey === null ? t('passwords.keyFirstConfirm') : t('passwords.keyChangedTrust')
          }
          busy={isTrusting}
          onClose={() => declineKey(keyChange)}
          onConfirm={() => void trustKey(keyChange)}
        />
      )}
    </div>
  )
}
