import { useState } from 'react'

import { graphql } from '../../api'
import { SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { useTranslation } from '../../i18n/i18n'
import { parseRecoveryKey } from '../../passwords/crypto'
import { useFailureToast } from './passwordsErrors'
import { PasswordsSetup, READ_PASSWORDS_SETUP } from './passwordsApi'
import { NO_PASSWORD_MANAGER } from './passwordsParts'
import { passphraseSecret } from './passwordsProtocol'
import { passwordsSession } from './passwordsSession'

// The locked screen: the passphrase, or the recovery key, and Unlock. A wrong
// one is said in a toast, in the server's words, and the field keeps what was
// typed so a typo can be mended.
//
// The salt and iteration count are read again before each attempt, not taken
// from when the page loaded: the passphrase may have been changed on another
// device since, and a right passphrase derived with the old salt would be
// refused by the server, and each refusal counts toward its limit on tries.

export function PasswordsUnlock({ userId }: { userId: string }) {
  const { t } = useTranslation()
  const toast = useToast()
  const reportFailure = useFailureToast()
  const [isRecoveryKey, setIsRecoveryKey] = useState(false)
  const [typedText, setTypedText] = useState('')
  const [isBusy, setIsBusy] = useState(false)

  async function unlock() {
    if (typedText.trim() === '' || isBusy) return
    setIsBusy(true)
    try {
      let secret: Uint8Array
      if (isRecoveryKey) {
        try {
          secret = parseRecoveryKey(typedText)
        } catch {
          toast.failed(t('passwords.recoveryKeyInvalid'))
          return
        }
      } else {
        const freshSetup = (await graphql<{ ReadPasswordsSetup: PasswordsSetup }>(READ_PASSWORDS_SETUP))
          .ReadPasswordsSetup
        secret = await passphraseSecret(typedText, freshSetup)
      }
      await passwordsSession.unlock(userId, { secret, isRecoveryKey })
      setTypedText('')
    } catch (failure) {
      reportFailure(failure, t('passwords.unlockFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  return (
    <SettingsSection
      card
      title={t('passwords.lockedTitle')}
      description={isRecoveryKey ? t('passwords.lockedRecoveryBody') : t('passwords.lockedBody')}
    >
      <form
        className="form-narrow"
        onSubmit={(event) => {
          event.preventDefault()
          void unlock()
        }}
      >
        {isRecoveryKey ? (
          <label>
            <span>{t('passwords.recoveryKey')}</span>
            <textarea
              rows={3}
              name="recovery-key"
              autoComplete="off"
              {...NO_PASSWORD_MANAGER}
              spellCheck={false}
              className="mono"
              value={typedText}
              onChange={(event) => setTypedText(event.target.value)}
            />
          </label>
        ) : (
          <label>
            <span>{t('passwords.passphrase')}</span>
            <input
              type="password"
              name="passphrase"
              autoComplete="current-password"
              {...NO_PASSWORD_MANAGER}
              value={typedText}
              onChange={(event) => setTypedText(event.target.value)}
            />
          </label>
        )}
        <div className="page-actions">
          <button type="submit" className="primary" disabled={isBusy || typedText.trim() === ''}>
            {t('passwords.unlock')}
          </button>
          <button
            type="button"
            onClick={() => {
              setIsRecoveryKey(!isRecoveryKey)
              setTypedText('')
            }}
          >
            {isRecoveryKey ? t('passwords.usePassphrase') : t('passwords.useRecoveryKey')}
          </button>
        </div>
      </form>
      <p className="muted">{t('passwords.lostBoth')}</p>
    </SettingsSection>
  )
}
