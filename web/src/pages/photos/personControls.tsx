import { useEffect, useState } from 'react'
import { FormDialog } from '../../components/dialog'
import { EyeIcon, EyeOffIcon, MergeIcon, PencilIcon, UnlinkIcon } from '../../components/icons'
import { useDebounced } from '../../components/preview'
import { useToast } from '../../components/toast'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import {
  NoAddressBookError,
  contactChoiceLabel,
  createContact,
  findContactChoices,
  listPhotoFaceGroups,
  namePhotoFaceGroup,
  personName,
  type PhotoFaceGroup,
} from '../../photos/people'
import { PhotoIconAction } from './toolbar'

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

// "You", in muted text, after the name of the person's own group, so that the
// one person in the list who is the reader is told apart from the others.
export function YouMark() {
  const { t } = useTranslation()
  return <span className="photo-you muted">{t('photos.you')}</span>
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
// have yet: that makes a contact with it first. A contact that already names
// another person is marked, since two people with one name are usually one
// person who should be merged.
export function NamePersonDialog({
  currentName,
  linkedContactIds = new Set<string>(),
  isBusy,
  error,
  onSubmit,
  onClose,
}: {
  currentName: string
  // The contacts that name some other person.
  linkedContactIds?: ReadonlySet<string>
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
  const contacts = choices.data?.contacts ?? []
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
          <label className={isNewChosen ? 'contact-picker-row is-chosen' : 'contact-picker-row'}>
            <input type="radio" name="named-contact" checked={isNewChosen} onChange={() => setChosen(NEW_CONTACT)} />
            <span className="contact-picker-text">
              <span className="contact-picker-name">{t('photos.newContactNamed', { name: typedName })}</span>
            </span>
          </label>
        )}
        {contacts.length === 0 && !choices.loading && !canMakeContact ? (
          <p className="muted contact-picker-empty">{t('photos.noContacts')}</p>
        ) : null}
        {contacts.map((contact) => {
          const label = contactChoiceLabel(contact)
          const secondary = [
            contact.emails[0] && label !== contact.emails[0] ? contact.emails[0] : '',
            linkedContactIds.has(contact.id) ? t('photos.contactIsLinked') : '',
          ].filter(Boolean)
          return (
            <label
              key={contact.id}
              className={chosenContact?.id === contact.id ? 'contact-picker-row is-chosen' : 'contact-picker-row'}
            >
              <input
                type="radio"
                name="named-contact"
                checked={chosenContact?.id === contact.id}
                onChange={() => setChosen(contact.id)}
              />
              <span className="contact-picker-text">
                <span className="contact-picker-name">{label}</span>
                {secondary.length > 0 && <span className="contact-picker-detail">{secondary.join(' · ')}</span>}
              </span>
            </label>
          )
        })}
      </div>
      {choices.data?.hasMore === true && <p className="muted field-hint">{t('photos.contactsMore')}</p>}
    </FormDialog>
  )
}

// NamePersonFlow is the naming dialog with what happens when it is answered:
// the contact is made when a new name was typed, the group is named, and a
// toast says so. The people page and a person's page both name people, and
// both are done when this calls back.
export function NamePersonFlow({
  group,
  onClose,
  onNamed,
}: {
  group: PhotoFaceGroup
  onClose: () => void
  onNamed: (named: PhotoFaceGroup) => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const displayedName = useDisplayedPersonName()
  const [isBusy, setIsBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const people = useQuery(() => listPhotoFaceGroups(true), [], { refresh: false })
  const linkedContactIds = new Set(
    (people.data ?? [])
      .filter((other) => other.id !== group.id && other.contactId !== '')
      .map((other) => other.contactId),
  )

  async function name(naming: PersonNaming) {
    setIsBusy(true)
    setError(null)
    try {
      const contactId = naming.kind === 'new' ? (await createContact(naming.contactName)).id : naming.contactId
      const named = await namePhotoFaceGroup(group.id, contactId)
      toast.done(t('photos.personNamed', { name: displayedName(named) }))
      onNamed(named)
      onClose()
    } catch (caught) {
      setError(
        caught instanceof NoAddressBookError
          ? t('photos.noAddressBook')
          : caught instanceof Error
            ? caught.message
            : String(caught),
      )
    } finally {
      setIsBusy(false)
    }
  }

  return (
    <NamePersonDialog
      currentName={group.contactName}
      linkedContactIds={linkedContactIds}
      isBusy={isBusy}
      error={error}
      onSubmit={(naming) => void name(naming)}
      onClose={onClose}
    />
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

// PersonActions are the icons of what can be done to a person, in the actions
// of their page: name them or change the name, take the name away, put them
// together with another, hide them from People or show them again. Each is its
// own icon with its name in the tooltip, as the actions of any one thing are.
export function PersonActions({
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
    <span className="photo-icon-actions">
      <PhotoIconAction
        label={isNamed ? t('photos.changeName') : t('photos.nameThisPerson')}
        icon={<PencilIcon size={16} />}
        onClick={onName}
      />
      {isNamed && (
        <PhotoIconAction label={t('photos.removeName')} icon={<UnlinkIcon size={16} />} onClick={onRemoveName} />
      )}
      <PhotoIconAction label={t('photos.mergeWith')} icon={<MergeIcon size={16} />} onClick={onMerge} />
      <PhotoIconAction
        label={person.isHidden ? t('photos.showPerson') : t('photos.hidePerson')}
        icon={person.isHidden ? <EyeIcon size={16} /> : <EyeOffIcon size={16} />}
        onClick={onToggleHidden}
      />
    </span>
  )
}
