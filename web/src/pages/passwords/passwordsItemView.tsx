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
import { siteFromText } from './passwordsEditor'
import { KIND_WORDS, siteHost } from './passwordsList'
import { OneTimeCodeValue, PlainValue, SecretValue } from './passwordsParts'
import { PasswordShare } from './passwordsApi'
import { isAgentAllowed, PasswordItem, splitSharesByRecord } from './passwordsProtocol'
import { useRekeyFailedToast } from './passwordsRekey'
import { passwordsSession } from './passwordsSession'
import { PasswordsShareDialog } from './passwordsShare'

// One item: its fields hidden until shown, a copy button on each, the
// one-time code running, the passkeys and the sites. The owner can change it,
// share it, delete it and let the agent use it; somebody it is shared with can
// only read and copy.

// isWebAddress says whether a site may be a link: only http and https, so a
// saved "javascript:" address is shown as text and never run.
function isWebAddress(site: string): boolean {
  try {
    const protocol = new URL(site).protocol
    return protocol === 'https:' || protocol === 'http:'
  } catch {
    return false
  }
}

// The most sites one login lists. A login can carry thousands, and a page of
// them is no use to read; the rest are counted.
const MOST_SITES_SHOWN = 50

// sitesToShow are the sites as they are listed: a bare "example.com" becomes
// an https address, as the editor keeps it, and a site written twice is shown
// once, so each is a link and the list's keys are different. At most
// MOST_SITES_SHOWN are returned, with how many more there are.
export function sitesToShow(sites: string[]): { shown: string[]; hiddenCount: number } {
  const seen = new Set<string>()
  const shown: string[] = []
  for (const site of sites) {
    if (site.trim() === '') continue
    // Text that does not make a web address even with https in front stays as
    // it was saved.
    const normalized = siteFromText(site)
    const address = isWebAddress(normalized) ? normalized : site.trim()
    if (seen.has(address)) continue
    seen.add(address)
    if (shown.length < MOST_SITES_SHOWN) shown.push(address)
  }
  return { shown, hiddenCount: seen.size - shown.length }
}

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

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="passwords-field">
      <dt className="muted">{label}</dt>
      <dd>{children}</dd>
    </div>
  )
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
  const { t, language } = translation
  const toast = useToast()
  const reportFailure = useFailureToast()
  const sayRekeyFailed = useRekeyFailedToast()
  const [isSharing, setIsSharing] = useState(false)
  const [isConfirmingDelete, setIsConfirmingDelete] = useState(false)
  const [isBusy, setIsBusy] = useState(false)
  const name = item.fields.name || t('passwords.unreadableItem')
  const isDeleted = item.deletedAt !== null
  const { recorded: recordedShares, unrecorded: unrecordedShares } = useMemo(() => splitSharesByRecord(item), [item])
  const sites = useMemo(
    () => (item.itemKind === 'login' ? sitesToShow(item.fields.sites) : { shown: [], hiddenCount: 0 }),
    [item],
  )

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
          <dl className="passwords-fields">
            {item.itemKind === 'login' && (
              <>
                {item.fields.userName && (
                  <Row label={t('passwords.fieldUserName')}>
                    <PlainValue label={t('passwords.fieldUserName')} value={item.fields.userName} />
                  </Row>
                )}
                {item.fields.password && (
                  <Row label={t('passwords.fieldPassword')}>
                    <SecretValue label={t('passwords.fieldPassword')} value={item.fields.password} />
                  </Row>
                )}
                {item.fields.oneTimeCodeUri && (
                  <Row label={t('passwords.fieldOneTimeCode')}>
                    <OneTimeCodeValue uri={item.fields.oneTimeCodeUri} />
                  </Row>
                )}
                {sites.shown.length > 0 && (
                  <Row label={t('passwords.fieldSites')}>
                    <ul className="passwords-sites">
                      {sites.shown.map((site) => (
                        <li key={site}>
                          {isWebAddress(site) ? (
                            <a href={site} target="_blank" rel="noopener noreferrer">
                              {siteHost(site)}
                            </a>
                          ) : (
                            site
                          )}
                        </li>
                      ))}
                      {sites.hiddenCount > 0 && (
                        <li className="muted">{t('passwords.sitesMore', { count: sites.hiddenCount })}</li>
                      )}
                    </ul>
                  </Row>
                )}
                {item.fields.passkeys.length > 0 && (
                  <Row label={t('passwords.fieldPasskeys')}>
                    <ul className="passwords-passkeys">
                      {item.fields.passkeys.map((passkey) => (
                        <li key={passkey.credentialId}>
                          <strong>{passkey.relyingPartyId}</strong>
                          {passkey.userName && <span className="muted"> · {passkey.userName}</span>}
                          <span className="muted">
                            {' · '}
                            {t('passwords.passkeyCreated', {
                              date: new Date(passkey.createdAt).toLocaleDateString(language),
                            })}
                          </span>
                        </li>
                      ))}
                    </ul>
                  </Row>
                )}
              </>
            )}
            {item.itemKind === 'card' && (
              <>
                {item.fields.cardholderName && (
                  <Row label={t('passwords.fieldCardholderName')}>
                    <PlainValue label={t('passwords.fieldCardholderName')} value={item.fields.cardholderName} />
                  </Row>
                )}
                {item.fields.number && (
                  <Row label={t('passwords.fieldCardNumber')}>
                    <SecretValue label={t('passwords.fieldCardNumber')} value={item.fields.number} />
                  </Row>
                )}
                {(item.fields.expiryMonth || item.fields.expiryYear) && (
                  <Row label={t('passwords.fieldExpiry')}>
                    <PlainValue
                      label={t('passwords.fieldExpiry')}
                      value={[item.fields.expiryMonth, item.fields.expiryYear].filter(Boolean).join('/')}
                    />
                  </Row>
                )}
                {item.fields.securityCode && (
                  <Row label={t('passwords.fieldSecurityCode')}>
                    <SecretValue label={t('passwords.fieldSecurityCode')} value={item.fields.securityCode} />
                  </Row>
                )}
                {item.fields.brand && (
                  <Row label={t('passwords.fieldBrand')}>
                    <PlainValue label={t('passwords.fieldBrand')} value={item.fields.brand} />
                  </Row>
                )}
              </>
            )}
            {item.itemKind === 'note' && item.fields.text && (
              <Row label={t('passwords.fieldNoteText')}>
                <SecretValue label={t('passwords.fieldNoteText')} value={item.fields.text} isMultiline />
              </Row>
            )}
            {item.itemKind !== 'note' && item.fields.notes && (
              <Row label={t('passwords.fieldNotes')}>
                <SecretValue label={t('passwords.fieldNotes')} value={item.fields.notes} isMultiline />
              </Row>
            )}
            {item.fields.folder && <Row label={t('passwords.fieldFolder')}>{item.fields.folder}</Row>}
          </dl>
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
