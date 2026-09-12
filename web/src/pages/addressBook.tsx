import { useEffect, useMemo, useState } from 'react'

import { graphql } from '../api'
import { Column, DataTable } from '../components/dataTable'
import { ConfirmDialog, FormDialog } from '../components/dialog'
import { PencilIcon, TrashIcon } from '../components/icons'
import { Tooltip } from '../components/tooltip'
import { SenderLogo } from '../components/senderLogo'
import { useQuery } from '../components/useQuery'
import { useToast } from '../components/toast'
import { useTranslation } from '../i18n/i18n'

// The address book: the people somebody chose to keep, as against the
// addresses the mailbox learned from traffic, which are listed below this on
// the same page and are a different thing.
//
// A contact is a vCard. The form here fills in the handful of boxes most
// contacts need, and the server applies them to the card it already holds, so
// that a photograph or a birthday put there by a phone survives somebody
// correcting a spelling in a browser.

const BOOKS = `query { ListAddressBooks { id name description contacts } }`

const CONTACTS = `
  query ($addressBookId: String!, $query: String, $first: Int) {
    ListContacts(addressBookId: $addressBookId, query: $query, first: $first) {
      id uid name organization emails phones hasPhoto addresses { written }
    }
  }`

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
  }
}`

const DELETE = `mutation ($id: String!) { DeleteContact(id: $id) }`

type AddressBook = { id: string; name: string; description?: string; contacts: number }
type Address = {
  label?: string
  street?: string
  locality?: string
  region?: string
  postalCode?: string
  country?: string
  written?: string
}

type Contact = {
  id: string
  uid: string
  name?: string
  organization?: string
  emails: string[]
  phones: string[]
  addresses?: Address[]
  hasPhoto?: boolean
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
}

const empty: Draft = {
  id: '', name: '', organization: '', emails: '', phones: '', note: '',
  street: '', locality: '', region: '', postalCode: '', country: '', addresses: [],
}

// Where a contact's picture is served from. It lives inside the card as
// base64 -- a card with a photograph on it is several hundred kilobytes -- so
// the listing says only whether there is one and the browser asks for it
// separately, where it is cached against the card's own version.
function photoAddress(id: string): string {
  return `/api/v1/contacts/${encodeURIComponent(id)}/photo`
}

function lines(value: string): string[] {
  return value
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line.length > 0)
}

// KeptAddresses is what the address book holds, for the learned list below to
// mark the addresses that are already contacts.
export interface KeptAddresses {
  has: (address: string) => boolean
  keep: (address: string, name?: string) => Promise<void>
  ready: boolean
}

export function AddressBookSection({ onReady }: { onReady?: (kept: KeptAddresses) => void } = {}) {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const books = useQuery(() => graphql<{ ListAddressBooks: AddressBook[] }>(BOOKS), [], { refresh: false })
  const book = books.data?.ListAddressBooks?.[0] ?? null
  const bookId = book?.id ?? ''
  const contacts = useQuery(
    () =>
      bookId
        ? graphql<{ ListContacts: Contact[] }>(CONTACTS, { addressBookId: bookId, query: null, first: null })
        : Promise.resolve(null),
    [bookId],
    { refresh: false },
  )

  const [draft, setDraft] = useState<Draft | null>(null)
  const [deleting, setDeleting] = useState<Contact | null>(null)
  const [busy, setBusy] = useState(false)
  // Which contact's form is being fetched, so its row says so rather than
  // appearing to do nothing.
  const [opening, setOpening] = useState('')
  const [problem, setProblem] = useState<string | null>(null)

  // Whether it worked, so a dialog closes only on success. What happened is
  // said in a toast either way; the dialog also shows the reason, because
  // that is where the person is looking when a submit is refused.
  const run = async (action: () => Promise<unknown>, done: string): Promise<boolean> => {
    setBusy(true)
    try {
      await action()
      setProblem(null)
      await Promise.all([contacts.reload(), books.reload()])
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
  const edit = async (contact: Contact) => {
    setProblem(null)
    setOpening(contact.id)
    try {
      const answer = await graphql<{ GetContact: Contact & { note?: string } }>(GET, { id: contact.id })
      const full = answer.GetContact
      const kept = full.addresses ?? []
      const first = kept[0] ?? {}
      setDraft({
        id: contact.id,
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
      })
    } catch (failure) {
      toast.failure(failure, t('addressBook.failed'))
    } finally {
      setOpening('')
    }
  }

  const columns = useMemo<Column<Contact>[]>(
    () => [
      {
        key: 'name',
        header: t('contacts.name'),
        filter: 'text',
        value: (contact) => contact.name ?? '',
        sort: (first, second) => (first.name ?? '').localeCompare(second.name ?? ''),
        render: (contact) => (
          <span className="sender-row">
            <SenderLogo
              name={contact.name || contact.emails?.[0] || ''}
              src={contact.hasPhoto ? photoAddress(contact.id) : undefined}
              size={24}
            />
            {contact.name || <span className="muted">{t('mailboxSettings.contactUnnamed')}</span>}
          </span>
        ),
      },
      {
        key: 'emails',
        header: t('addressBook.email'),
        filter: 'text',
        value: (contact) => (contact.emails ?? []).join(' '),
        render: (contact) => (
          <span className="mono">
            {(contact.emails ?? [])[0] ?? ''}
            {(contact.emails ?? []).length > 1 ? (
              <span className="muted"> {t('addressBook.more', { count: contact.emails.length - 1 })}</span>
            ) : null}
          </span>
        ),
      },
      {
        key: 'postal',
        header: t('addressBook.postal'),
        filter: 'text',
        optional: true,
        value: (contact) => (contact.addresses ?? []).map((address) => address.written ?? '').join(' '),
        render: (contact) => (
          <span className="muted">{(contact.addresses ?? [])[0]?.written ?? ''}</span>
        ),
      },
      {
        key: 'organization',
        header: t('addressBook.organization'),
        filter: 'text',
        optional: true,
        value: (contact) => contact.organization ?? '',
        render: (contact) => <span className="muted">{contact.organization ?? ''}</span>,
      },
      {
        key: 'phones',
        header: t('addressBook.phone'),
        width: '11rem',
        value: (contact) => (contact.phones ?? []).join(' '),
        render: (contact) => <span className="muted">{(contact.phones ?? [])[0] ?? ''}</span>,
      },
      {
        key: 'actions',
        header: '',
        width: '5rem',
        render: (contact) => (
          <div className="row-actions">
            <Tooltip label={t('common.edit')}>
              <button
                type="button"
                disabled={busy || opening === contact.id}
                onClick={() => void edit(contact)}
              >
                <PencilIcon size={16} />
              </button>
            </Tooltip>
            <Tooltip label={t('common.delete')}>
              <button type="button" disabled={busy} onClick={() => setDeleting(contact)}>
                <TrashIcon size={16} />
              </button>
            </Tooltip>
          </div>
        ),
      },
    ],
    [t, busy, opening],
  )

  const rows = contacts.data?.ListContacts ?? []
  const editing = Boolean(draft?.id)

  // The addresses this book holds, as one string.
  //
  // A string rather than the array, because the effect below depends on it
  // and `rows` is a fresh array on every render while the query is loading
  // -- `data?.ListContacts ?? []` builds a new empty one each time. Depending
  // on that identity ran the effect on every render, which handed the parent
  // a new object, which set state, which rendered again: a loop that spun
  // until the contacts arrived. A value that is equal when nothing has
  // changed is what a dependency list wants.
  const heldKey = useMemo(
    () =>
      rows
        .flatMap((contact) => (contact.emails ?? []).map((address) => address.trim().toLowerCase()))
        .sort()
        .join('\n'),
    [rows],
  )
  const ready = Boolean(bookId) && !contacts.loading

  // Handed to the learned list below, so it can say which addresses are
  // already kept and offer to keep the rest. Promoting one is an ordinary
  // save: the address book has one way in, not two.
  useEffect(() => {
    if (!onReady) return
    const held = new Set(heldKey ? heldKey.split('\n') : [])
    onReady({
      ready,
      has: (address) => held.has(address.trim().toLowerCase()),
      keep: async (address, name) => {
        await graphql(SAVE, {
          addressBookId: bookId,
          id: null,
          name: (name ?? '').trim() || address,
          organization: null,
          emails: [address],
          phones: [],
          note: null,
        })
        await Promise.all([contacts.reload(), books.reload()])
      },
    })
    // reload comes from useQuery and does not change; the rest are values.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [heldKey, bookId, ready, onReady])

  return (
    <>
      <h3>{t('addressBook.title')}</h3>
      <p className="muted">{t('addressBook.hint')}</p>
      <div className="page-actions">
        <button
          className="primary"
          type="button"
          disabled={!bookId}
          onClick={() => {
            setProblem(null)
            setDraft({ ...empty })
          }}
        >
          {t('addressBook.new')}
        </button>
      </div>

      <DataTable
        columns={columns}
        rows={rows}
        rowKey={(contact) => contact.id}
        loading={(contacts.loading && !contacts.data) || (books.loading && !books.data)}
        emptyMessage={t('addressBook.empty')}
        countLabel={(count) => plural(count, { one: 'contacts.countOne', other: 'contacts.count' }, { count })}
      />

      {draft && (
        <FormDialog
          title={editing ? t('addressBook.edit') : t('addressBook.new')}
          submitLabel={editing ? t('common.save') : t('common.create')}
          busy={busy}
          error={problem}
          canSubmit={draft.name.trim().length > 0 || lines(draft.emails).length > 0}
          onClose={() => setDraft(null)}
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
                ? t('addressBook.saidSaved', { name: draft.name.trim() || lines(draft.emails)[0] || '' })
                : t('addressBook.saidAdded', { name: draft.name.trim() || lines(draft.emails)[0] || '' }),
            ).then((done) => done && setDraft(null))
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
        </FormDialog>
      )}

      {deleting && (
        <ConfirmDialog
          title={t('addressBook.delete')}
          body={t('addressBook.deleteConfirm', {
            name: deleting.name || deleting.emails?.[0] || '',
          })}
          confirmLabel={t('common.delete')}
          busy={busy}
          error={problem}
          onConfirm={() =>
            void run(
              () => graphql(DELETE, { id: deleting.id }),
              t('addressBook.saidDeleted', { name: deleting.name || deleting.emails?.[0] || '' }),
            ).then((done) => done && setDeleting(null))
          }
          onClose={() => setDeleting(null)}
        />
      )}
    </>
  )
}
