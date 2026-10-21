import { useEffect, useState } from 'react'

import { ConfirmDialog } from '../../components/dialog'
import { useToast } from '../../components/toast'
import { useTranslation } from '../../i18n/i18n'
import { PasswordItemVersion } from './passwordsApi'
import { useFailureToast } from './passwordsErrors'
import { PasswordsFieldList } from './passwordsFieldList'
import { PasswordItem } from './passwordsProtocol'
import { useRekeyFailedToast } from './passwordsRekey'
import { PasswordItemDraft, passwordsSession } from './passwordsSession'

// Earlier versions of an item the person owns: the list (when, and whether
// they or another device or program saved it), one version shown read-only
// with its fields hidden until shown, and "Restore this version" behind a
// confirmation. Restoring is the person's own action, so the item is given a
// new key right after (see restoreItemVersion).
type Step =
  | { name: 'list' }
  | { name: 'version'; version: PasswordItemVersion; view: PasswordItemDraft }
  | { name: 'confirm'; version: PasswordItemVersion; view: PasswordItemDraft }

export function PasswordsVersionsDialog({ item, onClose }: { item: PasswordItem; onClose: () => void }) {
  const { t, language } = useTranslation()
  const toast = useToast()
  const reportFailure = useFailureToast()
  const sayRekeyFailed = useRekeyFailedToast()
  const [versions, setVersions] = useState<PasswordItemVersion[] | null>(null)
  const [step, setStep] = useState<Step>({ name: 'list' })
  const [isBusy, setIsBusy] = useState(false)
  const itemId = item.id

  useEffect(() => {
    let isCurrent = true
    passwordsSession
      .listItemVersions(itemId)
      .then((listed) => {
        if (isCurrent) setVersions(listed)
      })
      .catch((failure) => {
        if (!isCurrent) return
        reportFailure(failure, t('passwords.versionsFailed'))
        onClose()
      })
    return () => {
      isCurrent = false
    }
    // Read once for the item opened.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [itemId])

  function dateOf(version: PasswordItemVersion): string {
    return new Date(version.savedAt).toLocaleString(language)
  }

  async function open(version: PasswordItemVersion) {
    if (isBusy) return
    setIsBusy(true)
    try {
      const view = await passwordsSession.openItemVersion(itemId, version)
      setStep({ name: 'version', version, view })
    } catch (failure) {
      reportFailure(failure, t('passwords.versionOpenFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  async function restore(version: PasswordItemVersion) {
    if (isBusy) return
    setIsBusy(true)
    try {
      const { hasRekeyFailed } = await passwordsSession.restoreItemVersion(itemId, version.versionId)
      toast.done(t('passwords.versionRestored'))
      if (hasRekeyFailed) sayRekeyFailed(itemId)
      onClose()
    } catch (failure) {
      reportFailure(failure, t('passwords.versionRestoreFailed'))
      setStep({ name: 'list' })
    } finally {
      setIsBusy(false)
    }
  }

  if (step.name === 'confirm') {
    const { version, view } = step
    return (
      <ConfirmDialog
        destructive={false}
        title={t('passwords.versionRestoreTitle', { date: dateOf(version) })}
        body={t('passwords.versionRestoreBody')}
        confirmLabel={t('passwords.versionRestoreConfirm')}
        busy={isBusy}
        onClose={() => setStep({ name: 'version', version, view })}
        onConfirm={() => void restore(version)}
      />
    )
  }

  if (step.name === 'version') {
    const { version, view } = step
    return (
      <ConfirmDialog
        destructive={false}
        wide
        title={t('passwords.versionTitle', { date: dateOf(version) })}
        body={
          <>
            <p className="muted">{t('passwords.versionReadOnly')}</p>
            <PasswordsFieldList view={view} />
          </>
        }
        confirmLabel={t('passwords.versionRestore')}
        onClose={() => setStep({ name: 'list' })}
        onConfirm={() => setStep({ name: 'confirm', version, view })}
      />
    )
  }

  return (
    <ConfirmDialog
      wide
      title={t('passwords.versionsTitle')}
      body={
        versions === null ? (
          <p className="muted">{t('common.loading')}</p>
        ) : versions.length === 0 ? (
          <p className="muted">{t('passwords.versionsEmpty')}</p>
        ) : (
          <dl className="passwords-fields">
            {versions.map((version) => (
              <div key={version.versionId} className="passwords-field">
                <dt className="muted">
                  {t(version.isSavedByPerson ? 'passwords.versionSavedByYou' : 'passwords.versionSavedByOther')}
                </dt>
                <dd>
                  {dateOf(version)}{' '}
                  <button
                    type="button"
                    aria-label={`${dateOf(version)}: ${t('passwords.versionOpen')}`}
                    disabled={isBusy}
                    onClick={() => void open(version)}
                  >
                    {t('passwords.versionOpen')}
                  </button>
                </dd>
              </div>
            ))}
          </dl>
        )
      }
      onClose={onClose}
    />
  )
}
