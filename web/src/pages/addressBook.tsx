import { useMemo, useState } from 'react'

import { graphql } from '../api'
import { Column, DataTable } from '../components/dataTable'
import { ConfirmDialog, FormDialog } from '../components/dialog'
import { PencilIcon, TrashIcon } from '../components/icons'
import { Tooltip } from '../components/tooltip'
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
      id uid name organization emails phones
    }
  }`

const SAVE = `
  mutation ($addressBookId: String!, $id: String, $name: String, $organization: String,
            $emails: [String!], $phones: [String!], $note: String) {
    SaveContact(addressBookId: $addressBookId, id: $id, name: $name, organization: $organization,
                emails: $emails, phones: $phones, note: $note) { id name }
  }`

const GET = `query ($id: String!) { GetContact(id: $id) { id name organization emails phones note } }`

const DELETE = `mutation ($id: String!) { DeleteContact(id: $id) }`

type AddressBook = { id: string; name: string; description?: string; contacts: number }
type Contact = {
  id: string
  uid: string
  name?: string
  organization?: string
  emails: string[]
  phones: string[]
}

// A form's worth of one contact. Addresses and numbers are edited as one box
// each, a line apiece, which is how somebody with three of them expects to
// type them and avoids a row of controls for adding and removing lines.
type Draft = { id: string; name: string; organization: string; emails: string; phones: string; note: string }

const empty: Draft = { id: '', name: '', organization: '', emails: '', phones: '', note: '' }

function lines(value: string): string[] {
  return value
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line.length > 0)
}

export function AddressBookSection() {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const books = useQuery(() => graphql<{ ListAddressBooks: AddressBook[] }>(BOOKS), [], { refresh: false })
  const book = books.data?.ListAddressBooks?.[0] ?? null
  const bookId = book?.id ?? ''
  const contacts = useQuery(
    () =>
      bookId
        ? graphql<{ ListContacts: Contact[] }>(CONTACTS, { addressBookId: bookId, query: null, first: 500 })
        : Promise.resolve(null),
    [bookId],
    { refresh: false },
  )

  const [draft, setDraft] = useState<Draft | null>(null)
  const [deleting, setDeleting] = useState<Contact | null>(null)
  const [busy, setBusy] = useState(false)
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
  const edit = async (contact: Contact) => {
    setProblem(null)
    setDraft({
      id: contact.id,
      name: contact.name ?? '',
      organization: contact.organization ?? '',
      emails: (contact.emails ?? []).join('\n'),
      phones: (contact.phones ?? []).join('\n'),
      note: '',
    })
    try {
      const answer = await graphql<{ GetContact: Contact & { note?: string } }>(GET, { id: contact.id })
      const full = answer.GetContact
      setDraft((current) =>
        current && current.id === contact.id
          ? {
              ...current,
              name: full.name ?? '',
              organization: full.organization ?? '',
              emails: (full.emails ?? []).join('\n'),
              phones: (full.phones ?? []).join('\n'),
              note: full.note ?? '',
            }
          : current,
      )
    } catch (failure) {
      toast.failure(failure, t('addressBook.failed'))
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
        render: (contact) =>
          contact.name || <span className="muted">{t('mailboxSettings.contactUnnamed')}</span>,
      },
      {
        key: 'emails',
        header: t('contacts.address'),
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
        optional: true,
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
              <button type="button" disabled={busy} onClick={() => void edit(contact)}>
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
    [t, busy],
  )

  const rows = contacts.data?.ListContacts ?? []
  const editing = Boolean(draft?.id)

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
