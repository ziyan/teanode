import { useState } from 'react'

import { SettingsSection } from '../../components/settingsList'
import { FormDialog } from '../../components/dialog'
import { useToast } from '../../components/toast'
import { useTranslation } from '../../i18n/i18n'
import {
  makeSetUp,
  MINIMUM_PASSPHRASE_LENGTH,
  PassphraseStrength,
  passphraseStrength,
  PendingSetUp,
} from './passwordsProtocol'
import { useFailureToast } from './passwordsErrors'
import { passwordsSession } from './passwordsSession'
import { RecoveryKeyPanel, NO_PASSWORD_MANAGER } from './passwordsParts'

// Setting up Passwords: what it is, the passphrase asked for twice, and the
// recovery key shown once. Nothing is sent to the server until the person
// says they saved the recovery key, so a setup abandoned half way leaves
// nothing behind.

const STRENGTH_WORDS = {
  empty: 'passwords.strengthEmpty',
  short: 'passwords.strengthShort',
  weak: 'passwords.strengthWeak',
  fair: 'passwords.strengthFair',
  strong: 'passwords.strengthStrong',
} as const satisfies Record<PassphraseStrength, string>

export function PasswordsSetUp({ userId, onSetUp }: { userId: string; onSetUp: () => void }) {
  const { t } = useTranslation()
  const toast = useToast()
  const reportFailure = useFailureToast()
  const [step, setStep] = useState<'intro' | 'passphrase' | 'recoveryKey'>('intro')
  const [passphrase, setPassphrase] = useState('')
  const [repeated, setRepeated] = useState('')
  const [isBusy, setIsBusy] = useState(false)
  const [pending, setPending] = useState<PendingSetUp | null>(null)
  const [isRecoveryKeySaved, setIsRecoveryKeySaved] = useState(false)

  const strength = passphraseStrength(passphrase)
  const isLongEnough = [...passphrase].length >= MINIMUM_PASSPHRASE_LENGTH
  const isMatching = passphrase === repeated

  function close() {
    setStep('intro')
    setPassphrase('')
    setRepeated('')
    setPending(null)
    setIsRecoveryKeySaved(false)
  }

  async function makeKeys() {
    if (!isLongEnough) {
      toast.failed(t('passwords.passphraseTooShort', { count: MINIMUM_PASSPHRASE_LENGTH }))
      return
    }
    if (!isMatching) {
      toast.failed(t('passwords.passphrasesDiffer'))
      return
    }
    setIsBusy(true)
    try {
      setPending(await makeSetUp(passphrase))
      setStep('recoveryKey')
    } catch (failure) {
      reportFailure(failure, t('passwords.setUpFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  async function finish() {
    if (!pending || !isRecoveryKeySaved) return
    setIsBusy(true)
    try {
      await passwordsSession.completeSetUp(userId, pending)
      toast.done(t('passwords.setUpDone'))
      close()
      onSetUp()
    } catch (failure) {
      reportFailure(failure, t('passwords.setUpFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  return (
    <>
      <SettingsSection card title={t('passwords.introTitle')} description={t('passwords.introBody')}>
        <div className="page-actions">
          <button type="button" className="primary" onClick={() => setStep('passphrase')}>
            {t('passwords.setUp')}
          </button>
        </div>
      </SettingsSection>

      {step === 'passphrase' && (
        <FormDialog
          title={t('passwords.passphraseTitle')}
          submitLabel={t('passwords.continue')}
          busy={isBusy}
          canSubmit={passphrase !== '' && repeated !== ''}
          onSubmit={() => void makeKeys()}
          onClose={close}
        >
          <p className="muted">{t('passwords.passphraseBody')}</p>
          <label>
            <span>{t('passwords.passphrase')}</span>
            <input
              type="password"
              name="passphrase"
              autoComplete="new-password"
              {...NO_PASSWORD_MANAGER}
              value={passphrase}
              onChange={(event) => setPassphrase(event.target.value)}
            />
          </label>
          <p className="muted passwords-strength" role="status">
            {t(STRENGTH_WORDS[strength])}
          </p>
          <label>
            <span>{t('passwords.passphraseRepeat')}</span>
            <input
              type="password"
              name="passphrase-repeat"
              autoComplete="new-password"
              {...NO_PASSWORD_MANAGER}
              value={repeated}
              onChange={(event) => setRepeated(event.target.value)}
            />
          </label>
          <p className="muted">{t('passwords.passphraseLoss')}</p>
        </FormDialog>
      )}

      {step === 'recoveryKey' && pending && (
        <FormDialog
          title={t('passwords.recoveryKeyTitle')}
          submitLabel={t('passwords.finishSetUp')}
          busy={isBusy}
          canSubmit={isRecoveryKeySaved}
          wide
          onSubmit={() => void finish()}
          onClose={close}
        >
          <p className="muted">{t('passwords.recoveryKeyBody')}</p>
          <RecoveryKeyPanel
            recoveryKeyText={pending.recoveryKeyText}
            isSaved={isRecoveryKeySaved}
            onSavedChange={setIsRecoveryKeySaved}
          />
          <p className="muted">{t('passwords.passphraseLoss')}</p>
        </FormDialog>
      )}
    </>
  )
}
