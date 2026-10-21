import { useState } from 'react'

import { ConfirmDialog } from '../../components/dialog'
import { useTranslation } from '../../i18n/i18n'
import { KeyChangeNotice } from './passwordsPins'
import { SafetyCode } from './passwordsParts'
import { passwordsSession, usePasswordsSession } from './passwordsSession'

// A person's key changed since this device first saw it. The notice stays
// until it is dismissed and gates nothing: their items still open with the new
// key and sharing with them still seals for it. "Compare codes" is the way to
// check with them, and is optional.
export function PasswordsKeyNotices() {
  const { t } = useTranslation()
  const { keyNotices } = usePasswordsSession()
  const [comparing, setComparing] = useState<KeyChangeNotice | null>(null)
  if (keyNotices.length === 0) return null
  return (
    <div className="passwords-key-notices">
      {keyNotices.map((notice) => (
        <div key={notice.otherUserId} className="passwords-key-changed" role="status">
          <p>{t('passwords.keyChangedNotice', { name: notice.name || t('passwords.somebody') })}</p>
          <div className="page-actions">
            <button type="button" onClick={() => setComparing(notice)}>
              {t('passwords.compareCodes')}
            </button>
            <button type="button" onClick={() => passwordsSession.dismissKeyNotice(notice.otherUserId)}>
              {t('passwords.keyNoticeDismiss')}
            </button>
          </div>
        </div>
      ))}
      {comparing && (
        <CompareCodesDialog
          otherUserId={comparing.otherUserId}
          name={comparing.name}
          earlierPublicKey={comparing.oldPublicKey}
          publicKey={comparing.newPublicKey}
          onClose={() => setComparing(null)}
        />
      )}
    </div>
  )
}

// CompareCodesDialog shows the safety codes two people read to each other:
// the key's code (and the one before it, when it changed) and the person's own.
// Saying the codes match marks the key as compared; closing it changes nothing.
export function CompareCodesDialog({
  otherUserId,
  name,
  earlierPublicKey,
  publicKey,
  onClose,
}: {
  otherUserId: string
  name: string
  earlierPublicKey: string | null
  publicKey: string
  onClose: () => void
}) {
  const { t } = useTranslation()
  const { publicKey: ownPublicKey } = usePasswordsSession()
  const shownName = name || t('passwords.somebody')
  return (
    <ConfirmDialog
      destructive={false}
      title={t('passwords.compareTitle', { name: shownName })}
      body={
        <>
          <p className="muted">{t('passwords.compareWords', { name: shownName })}</p>
          {earlierPublicKey !== null && (
            <p>
              <span className="muted">{t('passwords.keyChangedOld')} </span>
              <SafetyCode publicKey={earlierPublicKey} />
            </p>
          )}
          <p>
            <span className="muted">
              {earlierPublicKey === null ? t('passwords.theirSafetyCode') : t('passwords.keyChangedNew')}{' '}
            </span>
            <SafetyCode publicKey={publicKey} />
          </p>
          <p>
            <span className="muted">{t('passwords.ownSafetyCodeTitle')} </span>
            <SafetyCode publicKey={ownPublicKey} />
          </p>
        </>
      }
      confirmLabel={t('passwords.compareMatch')}
      onClose={onClose}
      onConfirm={() => {
        passwordsSession.markKeyCompared(otherUserId, publicKey, name)
        onClose()
      }}
    />
  )
}
