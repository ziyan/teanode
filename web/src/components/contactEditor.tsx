import { useCallback, useEffect, useState } from 'react'

import { graphql } from '../api'
import { Avatar } from './avatar'
import { ConfirmDialog, FormDialog } from './dialog'
import { SenderLogo } from './senderLogo'
import { useQuery } from './useQuery'
import { useToast } from './toast'
import { personLabel, personName } from './personLabel'
import { useTranslation } from '../i18n/i18n'
import { CIRCLE_PERSON_FIELDS, listCirclePeople, type CirclePerson } from '../lib/circlePeople'
import { InMemory } from '../memoryLinks'

// The form that makes or changes one contact, with the questions that hang off
// it: taking its picture off, and saying which person in the circles it is. The
// address book opens it from a row, and a person's card opens it from its Edit
// button, so both change a contact the same way.
//
// A contact is a vCard. The form here fills in the handful of boxes most
// contacts need, and the server applies them to the card it already holds, so
// that a photograph or a birthday put there by a phone survives somebody
// correcting a spelling in a browser.

const SAVE = `
  mutation ($addressBookId: String!, $id: String, $name: String, $organization: String,
            $emails: [String!], $phones: [String!], $note: String, $addresses: [AddressInput!]) {
    SaveContact(addressBookId: $addressBookId, id: $id, name: $name, organization: $organization,
                emails: $emails, phones: $phones, note: $note, addresses: $addresses) { id name }
  }`

const GET = `query ($id: String!) {
  GetContact(id: $id) {
    id name organization emails phones note hasPhoto
    addresses { street locality region postalCode country }
    accountUserId account { ${CIRCLE_PERSON_FIELDS} }
  }
}`

// The link from a contact to a person in the owner's circles. Only the owner
// sees it: nobody is told, and an empty account takes the link off.
export const SET_CONTACT_ACCOUNT = `
  mutation ($addressBookId: String!, $contactId: String!, $accountUserId: String) {
    SetContactAccount(addressBookId: $addressBookId, contactId: $contactId, accountUserId: $accountUserId) {
      id accountUserId account { ${CIRCLE_PERSON_FIELDS} }
    }
  }`

const REMOVE_PICTURE = `mutation ($id: String!) { RemoveContactPicture(id: $id) { id hasPhoto } }`

const FIRST_BOOK = `query { ListAddressBooks { id } }`

type Address = {
  label?: string
  street?: string
  locality?: string
  region?: string
  postalCode?: string
  country?: string
  written?: string
}

type FullContact = {
  id: string
  name?: string
  organization?: string
  emails: string[]
  phones: string[]
  note?: string
  hasPhoto?: boolean
  addresses?: Address[]
  account?: CirclePerson | null
}

// A form's worth of one contact. Addresses and numbers are edited as one box
// each, a line apiece, which is how somebody with three of them expects to
// type them and avoids a row of controls for adding and removing lines.
type Draft = {
  id: string
  name: string
  organization: string
  emails: string
  phones: string
  note: string
  // The first postal address, in the components a card keeps it in. Any
  // others the card carries are left alone.
  street: string
  locality: string
  region: string
  postalCode: string
  country: string
  addresses: Address[]
  // Whether the card carries a picture, which can be taken off here.
  hasPhoto: boolean
  // The person in the circles this contact is, if the owner linked one.
  account: CirclePerson | null
}

const empty: Draft = {
  id: '',
  name: '',
  organization: '',
  emails: '',
  phones: '',
  note: '',
  street: '',
  locality: '',
  region: '',
  postalCode: '',
  country: '',
  addresses: [],
  hasPhoto: false,
  account: null,
}

// Where a contact's picture is served from. It lives inside the card as
// base64 -- a card with a photograph on it is several hundred kilobytes -- so
// the listing says only whether there is one and the browser asks for it
// separately, where it is cached against the card's own version.
export function contactPhotoAddress(id: string): string {
  return `/api/v1/contacts/${encodeURIComponent(id)}/photo`
}

function lines(value: string): string[] {
  return value
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line.length > 0)
}

const NO_TAKEN_ACCOUNTS: ReadonlySet<string> = new Set()

// useContactEditor is the form and its questions, and how to open them: onto a
// new contact or onto one that is there. Render dialogs wherever the page
// draws its dialogs. onChanged is told after anything was saved, so the page
// can read again what it shows.
//
// The book the contact is in is the one given, else the person's first, which
// is the one the address book page lists.
export function useContactEditor({
  addressBookId,
  takenAccountIds = NO_TAKEN_ACCOUNTS,
  onChanged,
}: {
  addressBookId?: string
  // The accounts that another contact of the book already is, left out of the
  // picker. The server is the guard: it refuses a second contact for the same
  // account in a book, and that refusal is shown as a toast.
  takenAccountIds?: ReadonlySet<string>
  onChanged: () => Promise<unknown> | void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const firstBook = useQuery(
    () => (addressBookId ? Promise.resolve(null) : graphql<{ ListAddressBooks: { id: string }[] }>(FIRST_BOOK)),
    [addressBookId],
    { refresh: false },
  )
  const bookId = addressBookId || firstBook.data?.ListAddressBooks?.[0]?.id || ''

  const [draft, setDraft] = useState<Draft | null>(null)
  // Whether the picture of the contact being edited is being asked about.
  const [isRemovingPicture, setIsRemovingPicture] = useState(false)
  const [isChoosingAccount, setIsChoosingAccount] = useState(false)
  const [isBusy, setBusy] = useState(false)
  // Which contact's form is being fetched, so its row says so rather than
  // appearing to do nothing.
  const [openingContactId, setOpeningContactId] = useState('')
  const [problem, setProblem] = useState<string | null>(null)

  // Whether it worked, so a dialog closes only on success. What happened is
  // said in a toast either way; the dialog also shows the reason, because
  // that is where the person is looking when a submit is refused.
  const run = async (action: () => Promise<unknown>, done: string): Promise<boolean> => {
    setBusy(true)
    try {
      await action()
      setProblem(null)
      await onChanged()
      toast.done(done)
      return true
    } catch (failure) {
      setProblem(failure instanceof Error ? failure.message : String(failure))
      toast.failure(failure, t('addressBook.failed'))
      return false
    } finally {
      setBusy(false)
    }
  }

  // Editing reads the whole contact first, because the list does not carry
  // the note and there is no sense sending every card to draw a table.
  // Read the whole contact before showing the form, never alongside it.
  // The form sends every box, and an empty box means "clear this", so a
  // dialog opened before the note had arrived would delete the note of
  // anybody quick enough to save -- or of anybody at all, if the read
  // failed and left the box empty behind a toast nobody had to act on.
  const open = useCallback(
    async (contactId: string) => {
      setProblem(null)
      setOpeningContactId(contactId)
      try {
        const answer = await graphql<{ GetContact: FullContact }>(GET, { id: contactId })
        const full = answer.GetContact
        const kept = full.addresses ?? []
        const first = kept[0] ?? {}
        setDraft({
          id: contactId,
          name: full.name ?? '',
          organization: full.organization ?? '',
          emails: (full.emails ?? []).join('\n'),
          phones: (full.phones ?? []).join('\n'),
          note: full.note ?? '',
          street: first.street ?? '',
          locality: first.locality ?? '',
          region: first.region ?? '',
          postalCode: first.postalCode ?? '',
          country: first.country ?? '',
          addresses: kept,
          hasPhoto: full.hasPhoto ?? false,
          account: full.account ?? null,
        })
      } catch (failure) {
        toast.failure(failure, t('addressBook.failed'))
      } finally {
        setOpeningContactId('')
      }
    },
    [toast, t],
  )

  const openNew = () => {
    setProblem(null)
    setDraft({ ...empty })
  }

  const closeDraft = () => {
    setDraft(null)
    setIsRemovingPicture(false)
    setIsChoosingAccount(false)
  }

  // account is null to take the link off.
  const setAccount = async (contactId: string, contactName: string, account: CirclePerson | null) => {
    const isDone = await run(
      () => graphql(SET_CONTACT_ACCOUNT, { addressBookId: bookId, contactId, accountUserId: account?.userId ?? null }),
      account
        ? t('addressBook.accountLinked', { name: contactName, account: personName(account) })
        : t('addressBook.accountRemoved', { name: contactName }),
    )
    if (isDone) {
      setDraft((previous) => (previous && previous.id === contactId ? { ...previous, account } : previous))
    }
    return isDone
  }

  // A refusal is shown as a toast, so the dialog's inline problem is cleared
  // as the picker opens and closes: it is not shown twice, or left behind.
  const openAccountPicker = () => {
    setProblem(null)
    setIsChoosingAccount(true)
  }
  const closeAccountPicker = () => {
    setProblem(null)
    setIsChoosingAccount(false)
  }

  const editing = Boolean(draft?.id)
  const draftName = draft ? draft.name.trim() || lines(draft.emails)[0] || '' : ''

  const dialogs = (
    <>
      {draft && (
        <FormDialog
          title={editing ? t('addressBook.edit') : t('addressBook.new')}
          submitLabel={editing ? t('common.save') : t('common.create')}
          busy={isBusy || bookId === ''}
          error={problem}
          canSubmit={draft.name.trim().length > 0 || lines(draft.emails).length > 0}
          // Escape and the scrim belong to the question while it is asked.
          onClose={() => !isRemovingPicture && !isChoosingAccount && closeDraft()}
          onSubmit={() =>
            void run(
              () =>
                // Every box, including the empty ones. The server treats a
                // field left out as "leave it alone" and a field given
                // empty as "clear it", and this form shows all of them, so
                // a box somebody emptied means to clear the field.
                graphql(SAVE, {
                  addressBookId: bookId,
                  id: draft.id || null,
                  name: draft.name.trim(),
                  organization: draft.organization.trim(),
                  emails: lines(draft.emails),
                  phones: lines(draft.phones),
                  note: draft.note.trim(),
                  // The first address from the boxes, and any others the
                  // card already carried, in order: a card may hold a home
                  // and a work address, and this form shows one.
                  addresses: [
                    {
                      street: draft.street.trim(),
                      locality: draft.locality.trim(),
                      region: draft.region.trim(),
                      postalCode: draft.postalCode.trim(),
                      country: draft.country.trim(),
                    },
                    ...draft.addresses.slice(1).map((address) => ({
                      street: address.street ?? '',
                      locality: address.locality ?? '',
                      region: address.region ?? '',
                      postalCode: address.postalCode ?? '',
                      country: address.country ?? '',
                    })),
                  ],
                }),
              editing
                ? t('addressBook.saidSaved', { name: draftName })
                : t('addressBook.saidAdded', { name: draftName }),
            ).then((done) => done && closeDraft())
          }
        >
          <label>
            {t('contacts.name')}
            <input value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} />
          </label>
          <label>
            {t('addressBook.organization')}
            <input
              value={draft.organization}
              onChange={(event) => setDraft({ ...draft, organization: event.target.value })}
            />
          </label>
          <label>
            {t('addressBook.emails')}
            <textarea
              rows={2}
              value={draft.emails}
              onChange={(event) => setDraft({ ...draft, emails: event.target.value })}
            />
            <span className="muted">{t('addressBook.onePerLine')}</span>
          </label>
          <label>
            {t('addressBook.phones')}
            <textarea
              rows={2}
              value={draft.phones}
              onChange={(event) => setDraft({ ...draft, phones: event.target.value })}
            />
            <span className="muted">{t('addressBook.onePerLine')}</span>
          </label>
          <label>
            {t('addressBook.street')}
            <input value={draft.street} onChange={(event) => setDraft({ ...draft, street: event.target.value })} />
          </label>
          <div className="form-row">
            <label>
              {t('addressBook.locality')}
              <input
                value={draft.locality}
                onChange={(event) => setDraft({ ...draft, locality: event.target.value })}
              />
            </label>
            <label>
              {t('addressBook.region')}
              <input value={draft.region} onChange={(event) => setDraft({ ...draft, region: event.target.value })} />
            </label>
          </div>
          <div className="form-row">
            <label>
              {t('addressBook.postalCode')}
              <input
                value={draft.postalCode}
                onChange={(event) => setDraft({ ...draft, postalCode: event.target.value })}
              />
            </label>
            <label>
              {t('addressBook.country')}
              <input value={draft.country} onChange={(event) => setDraft({ ...draft, country: event.target.value })} />
            </label>
          </div>
          <label>
            {t('addressBook.note')}
            <textarea
              rows={2}
              value={draft.note}
              onChange={(event) => setDraft({ ...draft, note: event.target.value })}
            />
          </label>
          {editing && draft.hasPhoto ? (
            <div className="form-row">
              <SenderLogo
                name={draft.name || lines(draft.emails)[0] || ''}
                src={contactPhotoAddress(draft.id)}
                size={40}
              />
              <button
                type="button"
                className="danger"
                disabled={isBusy}
                onClick={() => {
                  setProblem(null)
                  setIsRemovingPicture(true)
                }}
              >
                {t('addressBook.removePicture')}
              </button>
            </div>
          ) : null}
          {editing && (
            <div className="account-link">
              {draft.account ? (
                <>
                  <Avatar
                    name={personName(draft.account)}
                    avatarUrl={draft.account.avatarUrl}
                    colorIndex={draft.account.avatarColorIndex}
                    size={28}
                  />
                  <span className="account-link-text">
                    {t('addressBook.accountIs', { name: personName(draft.account) })}
                  </span>
                  <div className="account-link-actions">
                    <button type="button" disabled={isBusy} onClick={openAccountPicker}>
                      {t('addressBook.accountChange')}
                    </button>
                    <button type="button" disabled={isBusy} onClick={() => void setAccount(draft.id, draftName, null)}>
                      {t('addressBook.accountRemove')}
                    </button>
                  </div>
                </>
              ) : (
                <button type="button" disabled={isBusy} onClick={openAccountPicker}>
                  {t('addressBook.accountLink')}
                </button>
              )}
            </div>
          )}
          {editing ? <InMemory recordKind="contact" recordId={draft.id} /> : null}
        </FormDialog>
      )}

      {draft && isChoosingAccount && (
        <AccountPickerDialog
          contactName={draftName}
          currentUserId={draft.account?.userId ?? ''}
          takenUserIds={takenAccountIds}
          busy={isBusy}
          error={problem}
          onChoose={(account) =>
            void setAccount(draft.id, draftName, account).then((isDone) => isDone && closeAccountPicker())
          }
          onClose={closeAccountPicker}
        />
      )}

      {draft && isRemovingPicture && (
        <ConfirmDialog
          title={t('addressBook.removePicture')}
          body={t('addressBook.removePictureConfirm', { name: draftName })}
          confirmLabel={t('addressBook.removePicture')}
          busy={isBusy}
          error={problem}
          onConfirm={() =>
            void run(
              () => graphql(REMOVE_PICTURE, { id: draft.id }),
              t('addressBook.pictureRemoved', { name: draftName }),
            ).then((done) => {
              if (done) {
                setIsRemovingPicture(false)
                setDraft((previous) => (previous ? { ...previous, hasPhoto: false } : previous))
              }
            })
          }
          onClose={() => setIsRemovingPicture(false)}
        />
      )}
    </>
  )

  return { open, openNew, isBusy, openingContactId, dialogs }
}

// AccountPickerDialog chooses the person in the circles a contact is. People
// another contact of the book already is are left out, since a book keeps one
// contact for each person. The failures are toasts, from the page.
function AccountPickerDialog({
  contactName,
  currentUserId,
  takenUserIds,
  busy,
  error,
  onChoose,
  onClose,
}: {
  contactName: string
  currentUserId: string
  takenUserIds: ReadonlySet<string>
  busy: boolean
  error: string | null
  onChoose: (account: CirclePerson) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const people = useQuery(() => listCirclePeople(), [], { refresh: false })
  const [chosenUserId, setChosenUserId] = useState('')
  useEffect(() => {
    if (people.error) toast.failure(people.error, t('addressBook.failed'))
    // Once for each failure.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [people.error])
  const choices = (people.data ?? []).filter((person) => !takenUserIds.has(person.userId))
  const chosen = choices.find((person) => person.userId === chosenUserId) ?? null
  return (
    <FormDialog
      title={t('addressBook.accountPickerTitle', { name: contactName })}
      submitLabel={t('addressBook.suggestionLink')}
      busy={busy}
      error={error}
      canSubmit={chosen !== null && chosen.userId !== currentUserId}
      onSubmit={() => chosen && onChoose(chosen)}
      onClose={onClose}
    >
      <p className="muted">{t('addressBook.accountPickerHint')}</p>
      <div className="contact-picker" role="radiogroup" aria-label={t('addressBook.accountPickerLabel')}>
        {people.data && choices.length === 0 ? (
          <p className="muted contact-picker-empty">{t('addressBook.accountPickerEmpty')}</p>
        ) : null}
        {choices.map((person) => (
          <label
            key={person.userId}
            className={chosen?.userId === person.userId ? 'contact-picker-row is-chosen' : 'contact-picker-row'}
          >
            <input
              type="radio"
              name="circle-person"
              checked={chosen?.userId === person.userId}
              onChange={() => setChosenUserId(person.userId)}
            />
            <Avatar
              name={personName(person)}
              avatarUrl={person.avatarUrl}
              colorIndex={person.avatarColorIndex}
              size={28}
            />
            <span className="contact-picker-text">
              <span className="contact-picker-name">{personLabel(person)}</span>
            </span>
          </label>
        ))}
      </div>
    </FormDialog>
  )
}
