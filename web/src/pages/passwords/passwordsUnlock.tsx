import { useState } from 'react'

import { SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { useTranslation } from '../../i18n/i18n'
import { parseRecoveryKey } from '../../passwords/crypto'
import { useFailureToast } from './passwordsErrors'
import { PasswordsSetup } from './passwordsApi'
import { passphraseSecret } from './passwordsProtocol'
import { passwordsSession } from './passwordsSession'

// The locked screen: the passphrase, or the recovery key, and Unlock. A wrong
// one is said in a toast, in the server's words, and the field keeps what was
// typed so a typo can be mended.

export function PasswordsUnlock({ userId, setup }: { userId: string; setup: PasswordsSetup }) {
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
        secret = await passphraseSecret(typedText, setup)
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
              autoComplete="off"
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
            className="button-quiet"
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
