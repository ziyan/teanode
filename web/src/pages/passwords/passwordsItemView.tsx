import { useEffect, useMemo, useState } from 'react'

import { Tag } from '../../components/common'
import { ConfirmDialog } from '../../components/dialog'
import { AddPersonIcon, ArrowLeftIcon, PencilIcon, RestoreIcon, TrashIcon } from '../../components/icons'
import { RelativeTime } from '../../components/relativeTime'
import { SettingsEmpty, SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { Translation, useTranslation } from '../../i18n/i18n'
import { useFailureToast } from './passwordsErrors'
import { FinishSharingDialog } from './passwordsFinishSharing'
import { PasswordsFieldList } from './passwordsFieldList'
import { KIND_WORDS } from './passwordsList'
import { PasswordShare } from './passwordsApi'
import { isAgentAllowed, PasswordItem, splitSharesByRecord } from './passwordsProtocol'
import { useRekeyFailedToast } from './passwordsRekey'
import { passwordsSession, usePasswordsSession } from './passwordsSession'
import { PasswordsShareDialog } from './passwordsShare'
import { PasswordsVersionsDialog } from './passwordsVersions'

export { sitesToShow } from './passwordsFieldList'

// One item: its fields hidden until shown, a copy button on each, the
// one-time code running, the passkeys and the sites. The owner can change it,
// share it, delete it and let the agent use it; somebody it is shared with can
// only read and copy.

// whoItIsSharedWith says what each share of an item is: a person by name, with
// "waiting" when they have not set up Passwords yet, or a circle by name with
// how many people are in it and how many of them are waiting.
function whoItIsSharedWith(shares: PasswordShare[], { t, plural }: Pick<Translation, 't' | 'plural'>): string {
  const parts = shares.map((share) => {
    const name = share.granteeName
    const waitingCount = share.recipients.filter((recipient) => recipient.publicKey === null).length
    if (share.granteeCircleId === null) {
      return waitingCount > 0 ? t('passwords.sharedWithPersonWaiting', { name }) : name
    }
    const values = { name, waitingCount }
    return waitingCount > 0
      ? plural(
          share.recipients.length,
          { one: 'passwords.sharedWithCircleWaitingOne', other: 'passwords.sharedWithCircleWaiting' },
          values,
        )
      : plural(
          share.recipients.length,
          { one: 'passwords.sharedWithCircleOne', other: 'passwords.sharedWithCircle' },
          values,
        )
  })
  return t('passwords.sharedWith', { names: parts.join(' · ') })
}

export function PasswordsItemView({
  item,
  onBack,
  onEdit,
}: {
  item: PasswordItem
  onBack: () => void
  onEdit: () => void
}) {
  const translation = useTranslation()
  const { t } = translation
  const toast = useToast()
  const reportFailure = useFailureToast()
  const sayRekeyFailed = useRekeyFailedToast()
  const { itemIdsToFinish } = usePasswordsSession()
  const [isSharing, setIsSharing] = useState(false)
  const [isConfirmingDelete, setIsConfirmingDelete] = useState(false)
  const [isConfirmingFinish, setIsConfirmingFinish] = useState(false)
  const [isShowingVersions, setIsShowingVersions] = useState(false)
  const [isBusy, setIsBusy] = useState(false)
  const name = item.fields.name || t('passwords.unreadableItem')
  const isDeleted = item.deletedAt !== null
  const { recorded: recordedShares, unrecorded: unrecordedShares } = useMemo(() => splitSharesByRecord(item), [item])

  // An item the server marked as needing a new key (a removal could not give
  // it one) is given one when it is opened.
  useEffect(() => {
    if (!item.isOwnedByMe || !item.needsNewKey || item.accessProblem !== null) return
    void passwordsSession.rekeyIfNeeded(item.id).catch(() => sayRekeyFailed(item.id))
    // Once per item opened.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [item.id])

  async function remove() {
    setIsBusy(true)
    try {
      await passwordsSession.deleteItem(item.id)
      toast.done(t('passwords.itemDeleted', { name }))
      setIsConfirmingDelete(false)
      onBack()
    } catch (failure) {
      reportFailure(failure, t('passwords.deleteFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  async function restore() {
    setIsBusy(true)
    try {
      await passwordsSession.restoreItem(item.id)
      toast.done(t('passwords.itemRestored', { name }))
    } catch (failure) {
      reportFailure(failure, t('passwords.restoreFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  async function finishSharing() {
    if (isBusy) return
    setIsBusy(true)
    try {
      await passwordsSession.finishSharing(item.id)
      toast.done(t('passwords.finishSharingDone'))
    } catch (failure) {
      reportFailure(failure, t('passwords.finishSharingFailed'))
    } finally {
      setIsBusy(false)
      setIsConfirmingFinish(false)
    }
  }

  async function changeAgent(isUsableByAgent: boolean) {
    if (isBusy) return
    setIsBusy(true)
    try {
      await passwordsSession.setUsableByAgent(item, isUsableByAgent)
      toast.done(isUsableByAgent ? t('passwords.agentTurnedOn') : t('passwords.agentTurnedOff'))
    } catch (failure) {
      reportFailure(failure, t('passwords.agentFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  const actions = item.isOwnedByMe ? (
    <div className="row-actions head-icons">
      {isDeleted ? (
        <Tooltip label={t('passwords.restore')}>
          <button
            type="button"
            className="icon-action"
            aria-label={`${name}: ${t('passwords.restore')}`}
            disabled={isBusy}
            onClick={() => void restore()}
          >
            <RestoreIcon size={16} />
          </button>
        </Tooltip>
      ) : (
        <>
          {!item.isUnreadable && (
            <>
              <Tooltip label={t('common.edit')}>
                <button
                  type="button"
                  className="icon-action"
                  aria-label={`${name}: ${t('common.edit')}`}
                  onClick={onEdit}
                >
                  <PencilIcon size={16} />
                </button>
              </Tooltip>
              <Tooltip label={t('passwords.share')}>
                <button
                  type="button"
                  className="icon-action"
                  aria-label={`${name}: ${t('passwords.share')}`}
                  onClick={() => setIsSharing(true)}
                >
                  <AddPersonIcon size={16} />
                </button>
              </Tooltip>
            </>
          )}
          <Tooltip label={t('common.delete')}>
            <button
              type="button"
              className="icon-action danger"
              aria-label={`${name}: ${t('common.delete')}`}
              onClick={() => setIsConfirmingDelete(true)}
            >
              <TrashIcon size={16} />
            </button>
          </Tooltip>
        </>
      )}
    </div>
  ) : undefined

  return (
    <>
      <button type="button" className="button-quiet passwords-back" onClick={onBack}>
        <ArrowLeftIcon size={16} /> {t('passwords.back')}
      </button>

      <SettingsSection card title={name} action={actions}>
        <p className="passwords-tags">
          <Tag value={t(KIND_WORDS[item.itemKind])} />
          {item.isSharedWithMe && <Tag value={t('passwords.tagSharedBy', { name: item.ownerName })} />}
          {isAgentAllowed(item) && <Tag value={t('passwords.tagAgent')} tone="good" />}
          {isDeleted && <Tag value={t('passwords.tagDeleted')} tone="warn" />}
        </p>
        {item.isSharedWithMe && <p className="muted">{t('passwords.sharedReadOnly', { name: item.ownerName })}</p>}
        {item.isOwnedByMe && item.accessProblem !== null && !isDeleted && !item.isUnreadable && (
          <div className="passwords-key-changed" role="status">
            <h3>
              {t(
                item.accessProblem === 'replayed'
                  ? 'passwords.accessReplayedTitle'
                  : 'passwords.accessNotVerifiedTitle',
              )}
            </h3>
            <p className="muted">
              {t(
                item.accessProblem === 'replayed' ? 'passwords.accessReplayedBody' : 'passwords.accessNotVerifiedBody',
              )}
            </p>
            <div className="page-actions">
              <button type="button" onClick={() => setIsSharing(true)}>
                {t('passwords.accessSetAgain')}
              </button>
            </div>
          </div>
        )}
        {item.isOwnedByMe && !isDeleted && !item.isUnreadable && itemIdsToFinish.includes(item.id) && (
          <div className="passwords-key-changed" role="status">
            <p>{t('passwords.finishSharingBody')}</p>
            <div className="page-actions">
              <button type="button" disabled={isBusy} onClick={() => setIsConfirmingFinish(true)}>
                {t('passwords.finishSharing')}
              </button>
            </div>
          </div>
        )}
        {item.isOwnedByMe && item.accessProblem === null && unrecordedShares.length > 0 && (
          <p className="muted">
            {t('passwords.shareNotFromYou', { names: unrecordedShares.map((share) => share.granteeName).join(', ') })}
          </p>
        )}
        {item.isOwnedByMe && recordedShares.length > 0 && (
          <div className="passwords-shared-with">
            <p>{whoItIsSharedWith(recordedShares, translation)}</p>
            {!isDeleted && !item.isUnreadable && (
              <button type="button" onClick={() => setIsSharing(true)}>
                {t('passwords.share')}
              </button>
            )}
          </div>
        )}
        {item.isUnreadable ? (
          <SettingsEmpty>{t('passwords.unreadableBody')}</SettingsEmpty>
        ) : (
          <PasswordsFieldList view={item} />
        )}
      </SettingsSection>

      {item.isOwnedByMe && !isDeleted && !item.isUnreadable && item.itemKind !== 'card' && (
        <SettingsSection card title={t('passwords.agentTitle')}>
          <label className="checkbox is-plain">
            <input
              type="checkbox"
              checked={isAgentAllowed(item)}
              aria-busy={isBusy}
              onChange={(event) => void changeAgent(event.target.checked)}
            />
            <span>
              {t('passwords.agentSwitch')}
              <span className="muted passwords-switch-note">
                {t(item.itemKind === 'login' ? 'passwords.agentLoginBody' : 'passwords.agentNoteBody')}
              </span>
            </span>
          </label>
          {isAgentAllowed(item) && item.lastUsedByAgentAt && (
            <p className="muted">
              {t('passwords.agentLastUsed')} <RelativeTime value={item.lastUsedByAgentAt} />
            </p>
          )}
        </SettingsSection>
      )}

      {item.isOwnedByMe && !isDeleted && !item.isUnreadable && (
        <SettingsSection card title={t('passwords.versionsTitle')}>
          <p className="muted">{t('passwords.versionsBody')}</p>
          <div className="page-actions">
            <button type="button" onClick={() => setIsShowingVersions(true)}>
              {t('passwords.versionsShow')}
            </button>
          </div>
        </SettingsSection>
      )}

      {isShowingVersions && <PasswordsVersionsDialog item={item} onClose={() => setIsShowingVersions(false)} />}
      {isConfirmingFinish && (
        <FinishSharingDialog
          itemIds={[item.id]}
          busy={isBusy}
          onClose={() => setIsConfirmingFinish(false)}
          onConfirm={() => void finishSharing()}
        />
      )}
      {isSharing && <PasswordsShareDialog item={item} onClose={() => setIsSharing(false)} />}
      {isConfirmingDelete && (
        <ConfirmDialog
          title={t('passwords.deleteTitle', { name })}
          body={t('passwords.deleteBody')}
          confirmLabel={t('common.delete')}
          busy={isBusy}
          onClose={() => setIsConfirmingDelete(false)}
          onConfirm={() => void remove()}
        />
      )}
    </>
  )
}
