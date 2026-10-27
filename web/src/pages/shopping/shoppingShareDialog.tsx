import { useState } from 'react'
import { Link } from 'react-router-dom'

import { ErrorMessage } from '../../components/common'
import { ConfirmDialog, FormDialog } from '../../components/dialog'
import { TrashIcon } from '../../components/icons'
import { personLabel, personName } from '../../components/personLabel'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { listCircleChoices, listCirclePeople } from '../../photos/sharing'
import {
  listShoppingListShares,
  setShoppingListReminder,
  ShoppingList,
  ShoppingListShare,
  shareShoppingList,
  unshareShoppingList,
} from './shoppingApi'

// Sharing a list with a person in a circle the owner shares, or with a whole
// circle. Everybody a list reaches does everything but share it further or
// delete it, so there is no level to choose. The server answers as not found
// for anybody the owner shares no circle with, so the choices are only the
// people and circles it would accept: the same two queries and the same
// picker as the calendar's.
//
// The dialog is also where the caller says whether the store reminder covers
// this list. That is the caller's own choice, so people the list is shared
// with get the dialog with only that.

export function ShoppingShareDialog({
  shoppingList,
  onChanged,
  onClose,
}: {
  shoppingList: ShoppingList
  // Called after a share or the reminder changed, so that the page reads again.
  onChanged: () => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const shares = useQuery(
    () => (shoppingList.isOwner ? listShoppingListShares(shoppingList.id) : Promise.resolve([])),
    [shoppingList.id, shoppingList.isOwner],
    { refresh: false },
  )
  const choices = useQuery(
    async () => {
      if (!shoppingList.isOwner) return { people: [], circles: [] }
      const [people, circles] = await Promise.all([listCirclePeople(), listCircleChoices()])
      return { people, circles }
    },
    [shoppingList.isOwner],
    { refresh: false },
  )
  const [choice, setChoice] = useState('')
  const [isReminded, setIsReminded] = useState(shoppingList.isReminded)
  const [isBusy, setIsBusy] = useState(false)
  // The share being ended, asked about first.
  const [stopping, setStopping] = useState<ShoppingListShare | null>(null)

  const current = shares.data ?? []
  const people = choices.data?.people ?? []
  const circles = choices.data?.circles ?? []
  const everyoneIn = (name: string) => t('shopping.shareEveryoneIn', { name })
  const nameOfShare = (share: ShoppingListShare) =>
    share.granteeCircleId ? everyoneIn(share.granteeCircleName) : share.granteeName || share.granteeUsername

  async function run(work: () => Promise<unknown>, done: string): Promise<boolean> {
    setIsBusy(true)
    try {
      await work()
      toast.done(done)
      await shares.reload(true)
      onChanged()
      return true
    } catch (caught) {
      toast.failure(caught, t('shopping.failed'))
      return false
    } finally {
      setIsBusy(false)
    }
  }

  function changeReminder(next: boolean) {
    setIsReminded(next)
    void run(
      () => setShoppingListReminder(shoppingList.id, next),
      next ? t('shopping.reminderOn') : t('shopping.reminderOff'),
    ).then((isDone) => {
      if (!isDone) setIsReminded(!next)
    })
  }

  function add() {
    const [kind, id] = choice.split(':')
    if (!id) return
    const person = people.find((each) => each.userId === id)
    const name =
      kind === 'circle'
        ? everyoneIn(circles.find((each) => each.id === id)?.name ?? '')
        : person
          ? personName(person)
          : ''
    void run(
      () => shareShoppingList(shoppingList.id, kind === 'circle' ? { granteeCircleId: id } : { granteeUserId: id }),
      t('shopping.shareSaid', { name }),
    ).then((isDone) => isDone && setChoice(''))
  }

  const reminderField = (
    <label className="checkbox is-plain">
      <input
        type="checkbox"
        checked={isReminded}
        disabled={isBusy}
        onChange={(event) => changeReminder(event.target.checked)}
      />
      <span>{t('shopping.remindMe')}</span>
    </label>
  )

  if (!shoppingList.isOwner) {
    return (
      <ConfirmDialog
        title={t('shopping.remindersTitle', { listName: shoppingList.listName })}
        body={
          <>
            <p className="muted">{t('shopping.remindersHint')}</p>
            {reminderField}
          </>
        }
        onClose={onClose}
      />
    )
  }

  if (stopping) {
    return (
      <ConfirmDialog
        title={t('sharing.stopTitle', { name: nameOfShare(stopping) })}
        body={t('sharing.stopBody', { name: nameOfShare(stopping), what: shoppingList.listName })}
        confirmLabel={t('sharing.stop')}
        busy={isBusy}
        onConfirm={() =>
          void run(() => unshareShoppingList(stopping.id), t('sharing.stopped', { name: nameOfShare(stopping) })).then(
            (isDone) => isDone && setStopping(null),
          )
        }
        onClose={() => setStopping(null)}
      />
    )
  }

  return (
    <FormDialog
      title={t('shopping.shareTitle', { listName: shoppingList.listName })}
      submitLabel={t('shopping.shareAdd')}
      closeLabel={t('common.done')}
      busy={isBusy}
      canSubmit={choice !== ''}
      onSubmit={add}
      onClose={onClose}
    >
      <ErrorMessage error={shares.error ?? choices.error} />
      <p className="muted">{t('shopping.shareIntro')}</p>

      <h4 className="shopping-share-heading">{t('shopping.shareCurrent')}</h4>
      <div className="shopping-shares" aria-label={t('shopping.shareCurrent')}>
        {shares.data && current.length === 0 ? <p className="muted">{t('shopping.shareNobody')}</p> : null}
        {current.map((share) => {
          const name = nameOfShare(share)
          return (
            <div key={share.id} className="shopping-share-row">
              <span className="shopping-share-name">{name}</span>
              <Tooltip label={t('sharing.stopFor', { name })}>
                <button
                  type="button"
                  className="icon-action danger"
                  aria-label={t('sharing.stopFor', { name })}
                  disabled={isBusy}
                  onClick={() => setStopping(share)}
                >
                  <TrashIcon size={16} />
                </button>
              </Tooltip>
            </div>
          )
        })}
      </div>

      {choices.data && people.length === 0 && circles.length === 0 ? (
        <>
          <p className="muted">{t('shopping.shareNoPeople')}</p>
          <p>
            <Link className="button" to="/circles" onClick={onClose}>
              {t('circles.open')}
            </Link>
          </p>
        </>
      ) : (
        <label>
          {t('shopping.shareWith')}
          <select value={choice} onChange={(event) => setChoice(event.target.value)}>
            <option value="">{t('shopping.shareChoose')}</option>
            {people.length > 0 && (
              <optgroup label={t('shopping.sharePeople')}>
                {people.map((person) => (
                  <option key={person.userId} value={`user:${person.userId}`}>
                    {personLabel(person)}
                  </option>
                ))}
              </optgroup>
            )}
            {circles.length > 0 && (
              <optgroup label={t('shopping.shareCircles')}>
                {circles.map((circle) => (
                  <option key={circle.id} value={`circle:${circle.id}`}>
                    {everyoneIn(circle.name)}
                  </option>
                ))}
              </optgroup>
            )}
          </select>
        </label>
      )}

      <h4 className="shopping-share-heading">{t('shopping.remindersHeading')}</h4>
      <p className="muted">{t('shopping.remindersHint')}</p>
      {reminderField}
    </FormDialog>
  )
}
