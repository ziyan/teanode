import { useState } from 'react'

import { graphql } from '../../api'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { Select } from '../../components/select'
import { SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { Key, useTranslation } from '../../i18n/i18n'
import { parseRecoveryKey } from '../../passwords/crypto'
import { ImportCsvError, importedItemKey, ImportedItem, parseCsv, withoutDuplicates } from '../../passwords/importCsv'
import { useFailureToast } from './passwordsErrors'
import { ERASE_PASSWORDS, LOCK_AFTER_CHOICES, PasswordsSetup, UPDATE_PASSWORDS_SETTINGS } from './passwordsApi'
import { downloadText, NO_PASSWORD_MANAGER, RecoveryKeyPanel, SafetyCode } from './passwordsParts'
import { MINIMUM_PASSPHRASE_LENGTH, passphraseSecret, PasswordItem, SealedRecoveryKey } from './passwordsProtocol'
import { passwordsSession, usePasswordsSession } from './passwordsSession'

// The Settings tab: the lock time, the passphrase and recovery key, import,
// export, and erasing everything. What changes keys, or reads items, needs
// Passwords to be unlocked; the lock time and erasing do not, because erasing
// is the one way out when both the passphrase and the recovery key are lost.

const LOCK_AFTER_WORDS: Record<number, Key> = {
  1: 'passwords.lockAfter1',
  5: 'passwords.lockAfter5',
  15: 'passwords.lockAfter15',
  30: 'passwords.lockAfter30',
  60: 'passwords.lockAfter60',
  240: 'passwords.lockAfter240',
  1440: 'passwords.lockAfter1440',
  10080: 'passwords.lockAfter10080',
}

const FORMAT_NAMES = { chrome: 'Chrome', apple: 'Apple Passwords', bitwarden: 'Bitwarden' } as const

// exportText is the person's own items, readable, as JSON. It is not
// encrypted: the confirmation before it says so. The shape is the same as the
// command line's export: {version, exportedAt, items: [{id, itemKind,
// fields}]}. The access record (who may have the item, and its signature) is
// left out, as the command line leaves it out: it belongs to this account's
// devices and means nothing in another place.
function fieldsWithoutAccess(fields: PasswordItem['fields']): PasswordItem['fields'] {
  const exported = { ...fields }
  delete exported.access
  return exported
}

export function exportText(items: PasswordItem[], exportedAt: Date): string {
  return JSON.stringify(
    {
      version: 1,
      exportedAt: exportedAt.toISOString(),
      items: items
        .filter((item) => item.isOwnedByMe && item.deletedAt === null && !item.isUnreadable)
        .map((item) => ({ id: item.id, itemKind: item.itemKind, fields: fieldsWithoutAccess(item.fields) })),
    },
    null,
    2,
  )
}

export function PasswordsSettings({
  setup,
  onSetupChanged,
  onErased,
  onUnlock,
}: {
  setup: PasswordsSetup
  onSetupChanged: () => Promise<void>
  onErased: () => void
  // onUnlock goes to where Passwords is unlocked.
  onUnlock: () => void
}) {
  const { t } = useTranslation()
  const { isUnlocked } = usePasswordsSession()
  return (
    <>
      <LockTime setup={setup} onChanged={onSetupChanged} />
      {isUnlocked ? (
        <>
          <OwnSafetyCode />
          <Keys setup={setup} onChanged={onSetupChanged} />
          <ImportItems />
          <ExportItems setup={setup} />
        </>
      ) : (
        <SettingsSection card title={t('passwords.lockedTitle')} description={t('passwords.settingsLocked')}>
          <div className="page-actions">
            <button type="button" className="primary" onClick={onUnlock}>
              {t('passwords.unlock')}
            </button>
          </div>
        </SettingsSection>
      )}
      <EraseEverything onErased={onErased} />
    </>
  )
}

function LockTime({ setup, onChanged }: { setup: PasswordsSetup; onChanged: () => Promise<void> }) {
  const { t } = useTranslation()
  const toast = useToast()
  const reportFailure = useFailureToast()
  const [isBusy, setIsBusy] = useState(false)
  const choices: number[] = [...LOCK_AFTER_CHOICES]
  if (!choices.includes(setup.lockAfterMinutes)) choices.push(setup.lockAfterMinutes)

  async function change(lockAfterMinutes: number) {
    setIsBusy(true)
    try {
      await graphql(UPDATE_PASSWORDS_SETTINGS, { lockAfterMinutes })
      passwordsSession.setLockAfterMinutes(lockAfterMinutes)
      toast.done(t('passwords.lockAfterSaved'))
      await onChanged()
    } catch (failure) {
      reportFailure(failure, t('passwords.settingsFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  return (
    <SettingsSection card title={t('passwords.lockAfterTitle')} description={t('passwords.lockAfterBody')}>
      <label className="form-narrow">
        <span>{t('passwords.lockAfterLabel')}</span>
        <Select
          block
          disabled={isBusy}
          value={String(setup.lockAfterMinutes)}
          label={t('passwords.lockAfterLabel')}
          options={choices.map((minutes) => ({
            value: String(minutes),
            label: LOCK_AFTER_WORDS[minutes]
              ? t(LOCK_AFTER_WORDS[minutes])
              : t('passwords.lockAfterOther', { count: minutes }),
          }))}
          onChange={(value) => void change(Number(value))}
        />
      </label>
    </SettingsSection>
  )
}

// OwnSafetyCode is the person's own safety code, for somebody who wants to
// compare it. It is made from the key pair this device opened at unlock,
// never from what the server reports, so a server that swapped the key it
// hands out cannot make this agree with it.
function OwnSafetyCode() {
  const { t } = useTranslation()
  const { publicKey } = usePasswordsSession()
  return (
    <SettingsSection card title={t('passwords.ownSafetyCodeTitle')} description={t('passwords.ownSafetyCodeBody')}>
      <p>
        <SafetyCode publicKey={publicKey} />
      </p>
    </SettingsSection>
  )
}

function Keys({ setup, onChanged }: { setup: PasswordsSetup; onChanged: () => Promise<void> }) {
  const { t } = useTranslation()
  const [dialog, setDialog] = useState<'passphrase' | 'recoveryKey' | null>(null)
  return (
    <SettingsSection card title={t('passwords.keysTitle')} description={t('passwords.keysBody')}>
      <div className="page-actions">
        <button type="button" onClick={() => setDialog('passphrase')}>
          {t('passwords.changePassphrase')}
        </button>
        <button type="button" onClick={() => setDialog('recoveryKey')}>
          {t('passwords.replaceRecoveryKey')}
        </button>
      </div>
      {dialog === 'passphrase' && (
        <ChangePassphraseDialog setup={setup} onChanged={onChanged} onClose={() => setDialog(null)} />
      )}
      {dialog === 'recoveryKey' && <ReplaceRecoveryKeyDialog setup={setup} onClose={() => setDialog(null)} />}
    </SettingsSection>
  )
}

function ChangePassphraseDialog({
  setup,
  onChanged,
  onClose,
}: {
  setup: PasswordsSetup
  onChanged: () => Promise<void>
  onClose: () => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const reportFailure = useFailureToast()
  // isRecoveryKey proves it is the person with the recovery key instead of
  // the current passphrase, for one who has forgotten the passphrase.
  const [isRecoveryKey, setIsRecoveryKey] = useState(false)
  const [current, setCurrent] = useState('')
  const [replacement, setReplacement] = useState('')
  const [repeated, setRepeated] = useState('')
  const [isBusy, setIsBusy] = useState(false)

  async function change() {
    if ([...replacement].length < MINIMUM_PASSPHRASE_LENGTH) {
      toast.failed(t('passwords.passphraseTooShort', { count: MINIMUM_PASSPHRASE_LENGTH }))
      return
    }
    if (replacement !== repeated) {
      toast.failed(t('passwords.passphrasesDiffer'))
      return
    }
    setIsBusy(true)
    try {
      let oldSecret: Uint8Array
      if (isRecoveryKey) {
        try {
          oldSecret = parseRecoveryKey(current)
        } catch {
          toast.failed(t('passwords.recoveryKeyInvalid'))
          return
        }
      } else {
        oldSecret = await passphraseSecret(current, setup)
      }
      await passwordsSession.changePassphrase(oldSecret, replacement, isRecoveryKey)
      toast.done(t('passwords.passphraseChanged'))
      await onChanged()
      onClose()
    } catch (failure) {
      reportFailure(failure, t('passwords.settingsFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  return (
    <FormDialog
      title={t('passwords.changePassphrase')}
      submitLabel={t('passwords.changePassphraseConfirm')}
      busy={isBusy}
      canSubmit={current !== '' && replacement !== '' && repeated !== ''}
      otherAction={
        <button
          type="button"
          className="button-quiet"
          onClick={() => {
            setIsRecoveryKey(!isRecoveryKey)
            setCurrent('')
          }}
        >
          {isRecoveryKey ? t('passwords.usePassphrase') : t('passwords.useRecoveryKey')}
        </button>
      }
      onSubmit={() => void change()}
      onClose={onClose}
    >
      {isRecoveryKey ? (
        <label>
          <span>{t('passwords.recoveryKey')}</span>
          <textarea
            rows={3}
            autoComplete="off"
            {...NO_PASSWORD_MANAGER}
            spellCheck={false}
            className="mono"
            value={current}
            onChange={(event) => setCurrent(event.target.value)}
          />
        </label>
      ) : (
        <label>
          <span>{t('passwords.passphraseCurrent')}</span>
          <input
            type="password"
            autoComplete="current-password"
            {...NO_PASSWORD_MANAGER}
            value={current}
            onChange={(event) => setCurrent(event.target.value)}
          />
        </label>
      )}
      <label>
        <span>{t('passwords.passphraseNew')}</span>
        <input
          type="password"
          autoComplete="new-password"
          {...NO_PASSWORD_MANAGER}
          value={replacement}
          onChange={(event) => setReplacement(event.target.value)}
        />
      </label>
      <label>
        <span>{t('passwords.passphraseRepeat')}</span>
        <input
          type="password"
          autoComplete="new-password"
          {...NO_PASSWORD_MANAGER}
          value={repeated}
          onChange={(event) => setRepeated(event.target.value)}
        />
      </label>
    </FormDialog>
  )
}

function ReplaceRecoveryKeyDialog({ setup, onClose }: { setup: PasswordsSetup; onClose: () => void }) {
  const { t } = useTranslation()
  const toast = useToast()
  const reportFailure = useFailureToast()
  const [passphrase, setPassphrase] = useState('')
  const [isBusy, setIsBusy] = useState(false)
  const [made, setMade] = useState<{ secret: Uint8Array; sealed: SealedRecoveryKey } | null>(null)
  const [isSaved, setIsSaved] = useState(false)

  async function prove() {
    setIsBusy(true)
    try {
      const secret = await passphraseSecret(passphrase, setup)
      await passwordsSession.checkSecret({ secret, isRecoveryKey: false })
      setMade({ secret, sealed: await passwordsSession.makeRecoveryKey() })
    } catch (failure) {
      reportFailure(failure, t('passwords.settingsFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  async function replace() {
    if (!made || !isSaved) return
    setIsBusy(true)
    try {
      await passwordsSession.replaceRecoveryKey(made.secret, made.sealed)
      toast.done(t('passwords.recoveryKeyReplaced'))
      onClose()
    } catch (failure) {
      reportFailure(failure, t('passwords.settingsFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  if (made) {
    return (
      <FormDialog
        wide
        title={t('passwords.recoveryKeyTitle')}
        submitLabel={t('passwords.replaceRecoveryKeyConfirm')}
        busy={isBusy}
        canSubmit={isSaved}
        onSubmit={() => void replace()}
        onClose={onClose}
      >
        <p className="muted">{t('passwords.recoveryKeyReplaceBody')}</p>
        <RecoveryKeyPanel recoveryKeyText={made.sealed.recoveryKeyText} isSaved={isSaved} onSavedChange={setIsSaved} />
      </FormDialog>
    )
  }
  return (
    <FormDialog
      title={t('passwords.replaceRecoveryKey')}
      submitLabel={t('passwords.continue')}
      busy={isBusy}
      canSubmit={passphrase !== ''}
      onSubmit={() => void prove()}
      onClose={onClose}
    >
      <p className="muted">{t('passwords.replaceRecoveryKeyBody')}</p>
      <label>
        <span>{t('passwords.passphrase')}</span>
        <input
          type="password"
          autoComplete="current-password"
          {...NO_PASSWORD_MANAGER}
          value={passphrase}
          onChange={(event) => setPassphrase(event.target.value)}
        />
      </label>
    </FormDialog>
  )
}

type PendingImport = {
  formatName: string
  items: ImportedItem[]
  duplicateCount: number
  skippedCount: number
}

function ImportItems() {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const reportFailure = useFailureToast()
  const snapshot = usePasswordsSession()
  const [pending, setPending] = useState<PendingImport | null>(null)
  const [isBusy, setIsBusy] = useState(false)
  const [savedCount, setSavedCount] = useState(0)
  const [fileInputKey, setFileInputKey] = useState(0)

  async function choose(file: File | undefined) {
    if (!file) return
    try {
      const csvImport = parseCsv(await file.text())
      const existingKeys = new Set(
        snapshot.items
          .filter((item) => item.isOwnedByMe && item.deletedAt === null)
          .flatMap((item) =>
            item.itemKind === 'login'
              ? [importedItemKey('login', item.fields)]
              : item.itemKind === 'note'
                ? [importedItemKey('note', item.fields)]
                : [],
          ),
      )
      const { kept, duplicateCount } = withoutDuplicates(csvImport.items, existingKeys)
      if (kept.length === 0) {
        toast.failed(t('passwords.importNothingNew'))
        return
      }
      setPending({
        formatName: FORMAT_NAMES[csvImport.format],
        items: kept,
        duplicateCount,
        skippedCount: csvImport.skippedItemNames.length,
      })
    } catch (failure) {
      if (failure instanceof ImportCsvError) {
        toast.failed(t(failure.problem === 'empty' ? 'passwords.importEmpty' : 'passwords.importUnknown'))
      } else {
        reportFailure(failure, t('passwords.importFailed'))
      }
    } finally {
      // The same file can be chosen again.
      setFileInputKey((previous) => previous + 1)
    }
  }

  async function run() {
    if (!pending) return
    setIsBusy(true)
    setSavedCount(0)
    let importedCount = 0
    try {
      importedCount = await passwordsSession.importItems(
        pending.items.map((imported) =>
          imported.itemKind === 'login'
            ? { itemKind: 'login' as const, fields: imported.login }
            : { itemKind: 'note' as const, fields: imported.note },
        ),
        (count) => {
          importedCount = count
          setSavedCount(count)
        },
      )
      toast.done(plural(importedCount, { one: 'passwords.importDoneOne', other: 'passwords.importDone' }))
      setPending(null)
    } catch (failure) {
      reportFailure(
        failure,
        plural(importedCount, { one: 'passwords.importStoppedOne', other: 'passwords.importStopped' }),
      )
      setPending(null)
    } finally {
      setIsBusy(false)
    }
  }

  return (
    <SettingsSection card title={t('passwords.importTitle')} description={t('passwords.importBody')}>
      <label className="form-narrow">
        <span>{t('passwords.importFile')}</span>
        <input
          key={fileInputKey}
          type="file"
          accept=".csv,text/csv"
          onChange={(event) => void choose(event.target.files?.[0])}
        />
      </label>
      {pending && (
        <ConfirmDialog
          destructive={false}
          title={plural(
            pending.items.length,
            { one: 'passwords.importConfirmTitleOne', other: 'passwords.importConfirmTitle' },
            { format: pending.formatName },
          )}
          body={
            <>
              <p className="muted">{t('passwords.importConfirmBody')}</p>
              {isBusy && (
                <p className="muted" role="status">
                  {t('passwords.importProgress', { done: savedCount, total: pending.items.length })}
                </p>
              )}
              {pending.duplicateCount > 0 && (
                <p className="muted">
                  {plural(pending.duplicateCount, {
                    one: 'passwords.importDuplicatesOne',
                    other: 'passwords.importDuplicates',
                  })}
                </p>
              )}
              {pending.skippedCount > 0 && (
                <p className="muted">
                  {plural(pending.skippedCount, {
                    one: 'passwords.importSkippedOne',
                    other: 'passwords.importSkipped',
                  })}
                </p>
              )}
            </>
          }
          confirmLabel={t('passwords.importConfirm')}
          busy={isBusy}
          onClose={() => setPending(null)}
          onConfirm={() => void run()}
        />
      )}
    </SettingsSection>
  )
}

function ExportItems({ setup }: { setup: PasswordsSetup }) {
  const { t } = useTranslation()
  const reportFailure = useFailureToast()
  const snapshot = usePasswordsSession()
  const [isAsking, setIsAsking] = useState(false)
  const [passphrase, setPassphrase] = useState('')
  const [isBusy, setIsBusy] = useState(false)

  function close() {
    setIsAsking(false)
    setPassphrase('')
  }

  // The file holds every password readable, so the passphrase is asked for
  // again: an unlocked page left open is not enough to take it.
  async function exportItems() {
    setIsBusy(true)
    try {
      const secret = await passphraseSecret(passphrase, setup)
      await passwordsSession.checkSecret({ secret, isRecoveryKey: false })
      downloadText(
        'teanode-passwords-export.json',
        exportText(snapshot.items, new Date()),
        'application/json;charset=utf-8',
      )
      close()
    } catch (failure) {
      reportFailure(failure, t('passwords.exportFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  return (
    <SettingsSection card title={t('passwords.exportTitle')} description={t('passwords.exportBody')}>
      <div className="page-actions">
        <button type="button" onClick={() => setIsAsking(true)}>
          {t('passwords.exportAction')}
        </button>
      </div>
      {isAsking && (
        <FormDialog
          title={t('passwords.exportConfirmTitle')}
          submitLabel={t('passwords.exportConfirm')}
          busy={isBusy}
          canSubmit={passphrase !== ''}
          onSubmit={() => void exportItems()}
          onClose={close}
        >
          <p className="muted">{t('passwords.exportConfirmBody')}</p>
          <label>
            <span>{t('passwords.passphrase')}</span>
            <input
              type="password"
              autoComplete="current-password"
              {...NO_PASSWORD_MANAGER}
              value={passphrase}
              onChange={(event) => setPassphrase(event.target.value)}
            />
          </label>
        </FormDialog>
      )}
    </SettingsSection>
  )
}

function EraseEverything({ onErased }: { onErased: () => void }) {
  const { t } = useTranslation()
  const toast = useToast()
  const reportFailure = useFailureToast()
  const [isConfirming, setIsConfirming] = useState(false)
  const [isBusy, setIsBusy] = useState(false)

  async function erase() {
    setIsBusy(true)
    try {
      await graphql(ERASE_PASSWORDS)
      passwordsSession.lock()
      toast.done(t('passwords.erased'))
      setIsConfirming(false)
      onErased()
    } catch (failure) {
      reportFailure(failure, t('passwords.eraseFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  return (
    <SettingsSection card title={t('passwords.eraseTitle')} description={t('passwords.eraseBody')}>
      <div className="page-actions">
        <button type="button" className="danger" onClick={() => setIsConfirming(true)}>
          {t('passwords.eraseAction')}
        </button>
      </div>
      {isConfirming && (
        <ConfirmDialog
          title={t('passwords.eraseConfirmTitle')}
          body={t('passwords.eraseConfirmBody')}
          confirmLabel={t('passwords.eraseConfirm')}
          busy={isBusy}
          onClose={() => setIsConfirming(false)}
          onConfirm={() => void erase()}
        />
      )}
    </SettingsSection>
  )
}
