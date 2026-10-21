import { useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { graphql } from '../../api'
import { ErrorMessage } from '../../components/common'
import { FormDialog } from '../../components/dialog'
import { CloseIcon } from '../../components/icons'
import { personLabel, personName } from '../../components/personLabel'
import { Select } from '../../components/select'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { hasPermission, useSession } from '../../session'
import {
  LIST_CIRCLE_PEOPLE,
  LIST_CIRCLES,
  LIST_PASSWORD_RECIPIENTS,
  PasswordRecipient,
  PasswordShare,
} from './passwordsApi'
import { useFailureToast } from './passwordsErrors'
import { CompareCodesDialog } from './passwordsKeyNotices'
import { SafetyCode } from './passwordsParts'
import { PasswordItem, splitSharesByRecord } from './passwordsProtocol'
import { useRekeyFailedToast } from './passwordsRekey'
import { passwordsSession, ShareTarget } from './passwordsSession'

// Sharing one item with a circle or a person in one. The owner's device seals
// the item's key for each person who has set up Passwords, so the server never
// sees it. A person who has not set up Passwords is listed as waiting; the
// owner's device gives them their key the next time it is unlocked after they
// have. Saving seals for the key the server gives for each person at once and
// records it as the key known for them; nobody is asked to confirm it. The
// safety codes are shown, with a "Compare codes" action for the owner who
// wants to check one with the person, and "Codes compared" once it was.
// Taking somebody off gives the item a new key, which is done after the save.
//
// Who the item is shared with is what the owner's sealed access record says.
// The server's list of shares is shown only where it agrees with the record:
// a share the record does not have is named in a notice and is removed by
// saving, since only the owner's own device may grant access.

type Person = { userId: string; username: string; name: string }
type CircleChoice = { id: string; name: string; members: { userId: string }[] }

type Comparison = { userId: string; name: string; publicKey: string }

type GranteeRow = {
  kind: 'user' | 'circle'
  id: string
  name: string
  recipients: PasswordRecipient[]
}

// KEY_CHANGED_PHRASE is in the server's refusal of a key sealed for a public key
// the person no longer has.
const KEY_CHANGED_PHRASE = 'key changed since it was read'

const granteeKey = (grantee: { kind: string; id: string }) => `${grantee.kind}:${grantee.id}`

export function PasswordsShareDialog({ item, onClose }: { item: PasswordItem; onClose: () => void }) {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const reportFailure = useFailureToast()
  const sayRekeyFailed = useRekeyFailedToast()
  const navigate = useNavigate()
  const session = useSession()
  const canUseCircles = hasPermission(session.permissions, 'circle:use')
  const choices = useQuery(
    async () => {
      if (!canUseCircles) return { people: [] as Person[], circles: [] as CircleChoice[] }
      const [people, circles] = await Promise.all([
        graphql<{ ListCirclePeople: Person[] }>(LIST_CIRCLE_PEOPLE),
        graphql<{ ListCircles: CircleChoice[] }>(LIST_CIRCLES),
      ])
      return { people: people.ListCirclePeople, circles: circles.ListCircles }
    },
    [canUseCircles],
    { refresh: false },
  )

  // The shares the server lists that the sealed record also has are the rows;
  // the others are named in a notice and dropped when this is saved.
  const shareRow = (share: PasswordShare): GranteeRow => ({
    kind: share.granteeCircleId ? 'circle' : 'user',
    id: share.granteeCircleId ?? share.granteeUserId ?? '',
    name: share.granteeName,
    recipients: share.recipients,
  })
  const [rows, setRows] = useState<GranteeRow[]>(() => splitSharesByRecord(item).recorded.map(shareRow))
  const unrecordedNames = splitSharesByRecord(item).unrecorded.map((share) => share.granteeName)
  const [chosen, setChosen] = useState('')
  const [comparing, setComparing] = useState<Comparison | null>(null)
  const [isBusy, setIsBusy] = useState(false)

  const people = choices.data?.people ?? []
  const circles = choices.data?.circles ?? []
  const availableChoices = [
    ...people
      .filter((person) => !rows.some((row) => granteeKey(row) === `user:${person.userId}`))
      .map((person) => ({
        value: `user:${person.userId}`,
        label: personLabel(person),
        group: t('passwords.sharePeople'),
      })),
    ...circles
      .filter((circle) => !rows.some((row) => granteeKey(row) === `circle:${circle.id}`))
      .map((circle) => ({ value: `circle:${circle.id}`, label: circle.name, group: t('passwords.shareCircles') })),
  ]

  async function add() {
    const [kind, id] = chosen.split(':')
    if (!id || (kind !== 'user' && kind !== 'circle')) return
    const name =
      kind === 'circle'
        ? (circles.find((circle) => circle.id === id)?.name ?? '')
        : personLabel(people.find((person) => person.userId === id) ?? { username: '' })
    setIsBusy(true)
    try {
      const answer = await graphql<{ ListPasswordRecipients: PasswordRecipient[] | null }>(LIST_PASSWORD_RECIPIENTS, {
        granteeUserId: kind === 'user' ? id : null,
        granteeCircleId: kind === 'circle' ? id : null,
      })
      setRows((previous) => [...previous, { kind, id, name, recipients: answer.ListPasswordRecipients ?? [] }])
      setChosen('')
    } catch (failure) {
      reportFailure(failure, t('passwords.shareRecipientsFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  // saveRows seals the item's key for the people in these rows and saves who
  // the item is shared with, then says so and closes the dialog.
  async function saveRows(chosenRows: GranteeRow[]) {
    const targets: ShareTarget[] = chosenRows.map((row) => ({
      grantee: {
        granteeUserId: row.kind === 'user' ? row.id : null,
        granteeCircleId: row.kind === 'circle' ? row.id : null,
      },
      recipients: row.recipients,
    }))
    const recipients = chosenRows.flatMap((row) => row.recipients)
    const { hasRekeyFailed } = await passwordsSession.shareItem(item, targets)
    if (hasRekeyFailed) {
      // The removal went through; only the new key is missing. The server
      // keeps its mark on the item, so the next save or open tries again,
      // and the toast can too.
      sayRekeyFailed(item.id)
      onClose()
      return
    }
    const waitingCount = new Set(
      recipients.filter((recipient) => recipient.publicKey === null).map((recipient) => recipient.userId),
    ).size
    toast.done(
      waitingCount > 0
        ? plural(waitingCount, { one: 'passwords.shareSavedWaitingOne', other: 'passwords.shareSavedWaiting' })
        : t('passwords.shareSaved'),
    )
    onClose()
  }

  async function save() {
    setIsBusy(true)
    try {
      await saveRows(rows)
    } catch (failure) {
      // A person set Passwords up again while this was open, so the key read
      // is no longer theirs. Their keys are read again and the save is tried
      // once more with the new key, which is recorded as theirs; the notice
      // that their key changed stays on the page until it is dismissed.
      if (failure instanceof Error && failure.message.includes(KEY_CHANGED_PHRASE)) {
        const freshRows = await readRecipientsAgain()
        if (freshRows !== null) {
          try {
            await saveRows(freshRows)
          } catch (secondFailure) {
            reportFailure(secondFailure, t('passwords.shareFailed'))
          }
          return
        }
      }
      reportFailure(failure, t('passwords.shareFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  // readRecipientsAgain reads each row's people again and shows them, and
  // returns the rows read (null when that failed). The keys shown before are
  // kept as the known ones first, so the new ones count as a change.
  async function readRecipientsAgain(): Promise<GranteeRow[] | null> {
    try {
      const read = await Promise.all(
        rows.map(async (row) => {
          const answer = await graphql<{ ListPasswordRecipients: PasswordRecipient[] | null }>(
            LIST_PASSWORD_RECIPIENTS,
            {
              granteeUserId: row.kind === 'user' ? row.id : null,
              granteeCircleId: row.kind === 'circle' ? row.id : null,
            },
          )
          return { ...row, recipients: answer.ListPasswordRecipients ?? [] }
        }),
      )
      for (const row of rows) {
        for (const recipient of row.recipients) {
          if (recipient.publicKey !== null) {
            passwordsSession.recordKeyShown(recipient.userId, recipient.publicKey, personName(recipient))
          }
        }
      }
      setRows(read)
      return read
    } catch (failure) {
      reportFailure(failure, t('passwords.shareRecipientsFailed'))
      return null
    }
  }

  return (
    <FormDialog
      wide
      title={t('passwords.shareTitle', { name: item.fields.name })}
      submitLabel={t('passwords.shareConfirm')}
      busy={isBusy}
      canSubmit={
        rows.length > 0 ||
        item.shares.length > 0 ||
        (item.verifiedAccess?.grantees.length ?? 0) > 0 ||
        item.accessProblem !== null
      }
      onSubmit={() => void save()}
      onClose={onClose}
    >
      <p className="muted">{t('passwords.shareIntro')}</p>
      {rows.length === 0 && <p className="muted">{t('passwords.shareNobody')}</p>}
      {unrecordedNames.length > 0 && (
        <p className="muted">{t('passwords.shareNotFromYou', { names: unrecordedNames.join(', ') })}</p>
      )}
      {rows.map((row) => (
        <fieldset key={granteeKey(row)} className="passwords-share">
          <legend>
            <strong>{row.kind === 'circle' ? t('passwords.shareEveryoneIn', { name: row.name }) : row.name}</strong>
          </legend>
          <Tooltip label={t('passwords.shareStop', { name: row.name })}>
            <button
              type="button"
              className="icon-action danger passwords-share-remove"
              aria-label={t('passwords.shareStop', { name: row.name })}
              onClick={() => setRows((previous) => previous.filter((other) => granteeKey(other) !== granteeKey(row)))}
            >
              <CloseIcon size={16} />
            </button>
          </Tooltip>
          {row.kind === 'circle' && row.recipients.length === 0 && (
            <p className="muted">{t('passwords.shareNoRecipients')}</p>
          )}
          <ul className="passwords-recipients">
            {row.recipients.map((recipient) => {
              const earlierKey =
                recipient.publicKey !== null
                  ? passwordsSession.earlierKeyOf(recipient.userId, recipient.publicKey)
                  : null
              const isCompared =
                recipient.publicKey !== null && passwordsSession.isKeyCompared(recipient.userId, recipient.publicKey)
              return (
                <li key={recipient.userId}>
                  {/* A person is named once, as the title of their card; only a
                      circle needs a row to say which of its people a code is. */}
                  {row.kind === 'circle' && (
                    <span>{personLabel({ name: recipient.name, username: recipient.username })}</span>
                  )}
                  {recipient.publicKey === null ? (
                    <span className="muted">{t('passwords.recipientWaiting')}</span>
                  ) : (
                    <span>
                      <span className="muted">{t('passwords.safetyCode')} </span>
                      <SafetyCode publicKey={recipient.publicKey} />
                    </span>
                  )}
                  {earlierKey !== null && <p className="muted">{t('passwords.keyChangedWarning')}</p>}
                  {recipient.publicKey !== null &&
                    (isCompared ? (
                      <p className="muted">{t('passwords.codesCompared')}</p>
                    ) : (
                      <button
                        type="button"
                        onClick={() =>
                          setComparing({
                            userId: recipient.userId,
                            name: personName({ name: recipient.name, username: recipient.username }),
                            publicKey: recipient.publicKey as string,
                          })
                        }
                      >
                        {t('passwords.compareCodes')}
                      </button>
                    ))}
                </li>
              )
            })}
          </ul>
        </fieldset>
      ))}

      <ErrorMessage error={choices.error} />
      {!canUseCircles ? (
        <p className="muted">{t('passwords.shareNeedsCircles')}</p>
      ) : choices.data && people.length === 0 && circles.length === 0 ? (
        <>
          <p className="muted">{t('passwords.shareNoPeople')}</p>
          <div className="page-actions">
            <button type="button" onClick={() => navigate('/circles')}>
              {t('circles.open')}
            </button>
          </div>
        </>
      ) : (
        availableChoices.length > 0 && (
          <>
            <label>
              <span>{t('passwords.shareWith')}</span>
              <Select
                block
                value={chosen}
                label={t('passwords.shareWith')}
                placeholder={t('passwords.shareChoose')}
                options={availableChoices}
                onChange={setChosen}
              />
            </label>
            <div className="page-actions">
              <button type="button" disabled={isBusy || chosen === ''} onClick={() => void add()}>
                {t('passwords.shareAdd')}
              </button>
            </div>
          </>
        )
      )}
      {comparing && (
        <CompareCodesDialog
          otherUserId={comparing.userId}
          name={comparing.name}
          earlierPublicKey={passwordsSession.earlierKeyOf(comparing.userId, comparing.publicKey)}
          publicKey={comparing.publicKey}
          onClose={() => setComparing(null)}
        />
      )}
    </FormDialog>
  )
}
