import { useEffect, useState } from 'react'
import { FormDialog } from '../../components/dialog'
import { MoreIcon } from '../../components/icons'
import { MenuButton } from '../../components/menuButton'
import { useDebounced } from '../../components/preview'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { contactChoiceLabel, findContactChoices, personName, type PhotoFaceGroup } from '../../photos/people'

// The pieces the people pages and the viewer share: a person's round picture,
// the dialog that names a person from the address book, the dialog that says
// two people are one, and the menu of what can be done to a person.

// Where a person's page is. The page is the library's, filtered to the photos
// with a face in the group.
export function personAddress(photoFaceGroupId: string) {
  return `/photos/people/${encodeURIComponent(photoFaceGroupId)}`
}

// What a person is called when nobody has named them.
export function useDisplayedPersonName() {
  const { t } = useTranslation()
  return (group: Pick<PhotoFaceGroup, 'contactName'>) => personName(group) || t('photos.unnamedPerson')
}

// A face, round. The picture is cut from the photo by the server; until it
// arrives, or when the photo has no picture yet, the circle holds an initial.
export function FacePicture({
  facePictureUrl,
  name,
  size = 'medium',
}: {
  facePictureUrl: string
  name: string
  size?: 'small' | 'medium' | 'large'
}) {
  return (
    <span className={`face-picture is-${size}`} aria-hidden="true">
      {facePictureUrl ? (
        <img src={facePictureUrl} alt="" loading="lazy" draggable={false} />
      ) : (
        <span className="face-picture-initial">{(Array.from(name.trim())[0] ?? '?').toUpperCase()}</span>
      )}
    </span>
  )
}

// What the naming dialog hands back: a contact that is there already, or the
// name of one to make.
export type PersonNaming = { kind: 'contact'; contactId: string } | { kind: 'new'; contactName: string }

// The most characters a contact's name may have, as the dialog lets it be typed.
const MOST_CONTACT_NAME_CHARACTERS = 255

// NamePersonDialog names a person by choosing a contact from the address
// book, as the server asks, or by typing a name the address book does not
// have yet: that makes a contact with it first.
export function NamePersonDialog({
  currentName,
  isBusy,
  error,
  onSubmit,
  onClose,
}: {
  currentName: string
  isBusy?: boolean
  error?: string | null
  onSubmit: (naming: PersonNaming) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [typed, setTyped] = useState('')
  const [chosen, setChosen] = useState('')
  const searchText = useDebounced(typed.trim(), 250)
  const choices = useQuery(() => findContactChoices(searchText), [searchText], { refresh: false })
  const contacts = choices.data ?? []
  const typedName = typed.trim()
  // A name the address book has already is chosen from the list, not made again.
  const hasTypedNameContact = contacts.some(
    (contact) => contactChoiceLabel(contact).toLowerCase() === typedName.toLowerCase(),
  )
  const canMakeContact = typedName !== '' && !hasTypedNameContact
  // Only what is on the screen is chosen. A contact chosen before the search
  // changed, and no longer listed, is not submitted; neither is "new contact"
  // for a name that is now one in the list.
  const chosenContact = contacts.find((contact) => contact.id === chosen) ?? null
  const isNewChosen = chosen === NEW_CONTACT && canMakeContact
  const canSubmit = chosenContact !== null || isNewChosen
  useEffect(() => {
    if (chosen !== '' && chosenContact === null && !isNewChosen && !choices.loading) setChosen('')
  }, [chosen, chosenContact, isNewChosen, choices.loading])

  return (
    <FormDialog
      title={currentName ? t('photos.changeName') : t('photos.nameThisPerson')}
      submitLabel={t('common.save')}
      busy={isBusy}
      error={error ?? (choices.error ? messageOf(choices.error) : null)}
      canSubmit={canSubmit}
      onSubmit={() =>
        onSubmit(
          isNewChosen ? { kind: 'new', contactName: typedName } : { kind: 'contact', contactId: chosenContact!.id },
        )
      }
      onClose={onClose}
    >
      <p className="muted">{t('photos.nameHint')}</p>
      <label>
        <span>{t('photos.contactSearch')}</span>
        <input
          type="search"
          value={typed}
          maxLength={MOST_CONTACT_NAME_CHARACTERS}
          placeholder={t('photos.contactSearchPlaceholder')}
          autoComplete="off"
          onChange={(event) => setTyped(event.target.value)}
        />
      </label>
      <div className="contact-picker" role="radiogroup" aria-label={t('photos.contactSearch')}>
        {canMakeContact && (
          <label className="checkbox contact-picker-row">
            <input type="radio" name="named-contact" checked={isNewChosen} onChange={() => setChosen(NEW_CONTACT)} />
            <span>{t('photos.newContactNamed', { name: typedName })}</span>
          </label>
        )}
        {contacts.length === 0 && !choices.loading && !canMakeContact ? (
          <p className="muted">{t('photos.noContacts')}</p>
        ) : null}
        {contacts.map((contact) => (
          <label key={contact.id} className="checkbox contact-picker-row">
            <input
              type="radio"
              name="named-contact"
              checked={chosenContact?.id === contact.id}
              onChange={() => setChosen(contact.id)}
            />
            <span>
              {contactChoiceLabel(contact)}
              {contact.emails[0] && contactChoiceLabel(contact) !== contact.emails[0] ? (
                <span className="muted"> {contact.emails[0]}</span>
              ) : null}
            </span>
          </label>
        ))}
      </div>
    </FormDialog>
  )
}

function messageOf(caught: unknown): string {
  return caught instanceof Error ? caught.message : String(caught)
}

// Not an id the server could hand out.
const NEW_CONTACT = 'new contact'

// MergePersonDialog picks the person who is the same as this one. This
// person stays and takes the other's faces, and the other's name too when
// this one has none.
export function MergePersonDialog({
  person,
  others,
  isLoading,
  isBusy,
  error,
  onSubmit,
  onClose,
}: {
  person: PhotoFaceGroup
  others: PhotoFaceGroup[]
  // The others are still being listed.
  isLoading?: boolean
  isBusy?: boolean
  error?: string | null
  onSubmit: (other: PhotoFaceGroup) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const displayedName = useDisplayedPersonName()
  const [chosenId, setChosenId] = useState('')
  const chosen = others.find((other) => other.id === chosenId) ?? null
  return (
    <FormDialog
      title={t('photos.mergeTitle', { name: displayedName(person) })}
      submitLabel={t('photos.merge')}
      busy={isBusy}
      error={error}
      canSubmit={chosen !== null}
      onSubmit={() => chosen && onSubmit(chosen)}
      onClose={onClose}
    >
      <p className="muted">{t('photos.mergeHint')}</p>
      {others.length === 0 ? (
        <p className="muted">{isLoading ? t('common.loading') : t('photos.mergeNobody')}</p>
      ) : (
        <div className="photo-person-choices" role="radiogroup" aria-label={t('photos.mergeChoose')}>
          {others.map((other) => (
            <button
              key={other.id}
              type="button"
              role="radio"
              aria-checked={other.id === chosenId}
              className={other.id === chosenId ? 'photo-person-choice is-chosen' : 'photo-person-choice'}
              onClick={() => setChosenId(other.id)}
            >
              <FacePicture facePictureUrl={other.facePictureUrl} name={displayedName(other)} size="large" />
              <span className="photo-person-choice-name">{displayedName(other)}</span>
            </button>
          ))}
        </div>
      )}
      {chosen && (
        <p className="muted">
          {t('photos.mergeConfirm', { other: displayedName(chosen), name: displayedName(person) })}
        </p>
      )}
    </FormDialog>
  )
}

// PersonMenu is the button that lists what can be done to a person: name them
// or change the name, take the name away, put them together with another,
// hide them from People or show them again.
export function PersonMenu({
  person,
  onName,
  onRemoveName,
  onMerge,
  onToggleHidden,
}: {
  person: PhotoFaceGroup
  onName: () => void
  onRemoveName: () => void
  onMerge: () => void
  onToggleHidden: () => void
}) {
  const { t } = useTranslation()
  const isNamed = personName(person) !== ''
  return (
    <MenuButton
      label={t('photos.personMenu')}
      className="icon-button"
      icon={<MoreIcon size={18} />}
      render={(close) => {
        const choose = (run: () => void) => () => {
          close()
          run()
        }
        return (
          <>
            <button type="button" role="menuitem" onClick={choose(onName)}>
              {isNamed ? t('photos.changeName') : t('photos.nameThisPerson')}
            </button>
            {isNamed && (
              <button type="button" role="menuitem" onClick={choose(onRemoveName)}>
                {t('photos.removeName')}
              </button>
            )}
            <button type="button" role="menuitem" onClick={choose(onMerge)}>
              {t('photos.mergeWith')}
            </button>
            <button type="button" role="menuitem" onClick={choose(onToggleHidden)}>
              {person.isHidden ? t('photos.showPerson') : t('photos.hidePerson')}
            </button>
          </>
        )
      }}
    />
  )
}
