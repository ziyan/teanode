import { useMemo, useState } from 'react'

import { graphql } from '../api'
import { ErrorMessage, Loading } from '../components/common'
import { SenderLogo } from '../components/senderLogo'
import { useToast } from '../components/toast'
import { Tooltip } from '../components/tooltip'
import { Column, DataTable } from '../components/dataTable'
import { ConfirmDialog, FormDialog } from '../components/dialog'
import { PencilIcon, TrashIcon } from '../components/icons'
import { RelativeTime } from '../components/relativeTime'
import { useQuery } from '../components/useQuery'
import { useTranslation } from '../i18n/i18n'
import { useMailboxes } from '../mailboxes'

const CONTACTS = `
  query ($mailboxId: String!, $first: Int) {
    ListMailboxContacts(mailboxId: $mailboxId, first: $first) { address name lastSeenAt count logoDomain }
  }`

const SAVE_CONTACT = `
  mutation ($mailboxId: String!, $address: String!, $name: String) {
    SaveMailboxContact(mailboxId: $mailboxId, address: $address, name: $name) { address }
  }`

const DELETE_CONTACTS = `
  mutation ($mailboxId: String!, $addresses: [String!]!) {
    DeleteMailboxContacts(mailboxId: $mailboxId, addresses: $addresses)
  }`

const DELETE_CONTACT = `
  mutation ($mailboxId: String!, $address: String!) {
    DeleteMailboxContact(mailboxId: $mailboxId, address: $address)
  }`

type Contact = { address: string; name?: string; lastSeenAt: string; count: number; logoDomain?: string }

// Everyone the mailbox has written to or heard from, and anyone added by
// hand: the list the compose page completes addresses from, and a rule can
// ask about. A page of its own, listed the way the other lists are.
export function MailboxContactsPage() {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const mailboxes = useMailboxes()
  const view = mailboxes.current
  const mailboxId = view?.mailbox.id ?? ''
  const query = useQuery(
    () =>
      mailboxId
        ? graphql<{ ListMailboxContacts: Contact[] }>(CONTACTS, { mailboxId, first: 500 })
        : Promise.resolve(null),
    [mailboxId],
    { refresh: false },
  )
  // Adding and editing happen in a dialog over the list, the way the other
  // lists add their rows; deleting asks first.
  const [adding, setAdding] = useState(false)
  const [address, setAddress] = useState('')
  const [name, setName] = useState('')
  const [editing, setEditing] = useState<Contact | null>(null)
  const [editName, setEditName] = useState('')
  const [deleting, setDeleting] = useState<Contact | null>(null)
  // Rows chosen to be acted on together, held by address because that is what
  // names a contact.
  const [chosen, setChosen] = useState<Set<string>>(new Set())
  const [forgetting, setForgetting] = useState(false)
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

  // Whether it worked, so a dialog closes only on success.
  const run = async (action: () => Promise<unknown>): Promise<boolean> => {
    setBusy(true)
    try {
      await action()
      setProblem(null)
      await query.reload()
      return true
    } catch (failure) {
      setProblem(failure instanceof Error ? failure.message : String(failure))
      toast.failure(failure, t('domain.failed'))
      return false
    } finally {
      setBusy(false)
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
            <SenderLogo name={contact.name || contact.address} logoDomain={contact.logoDomain} size={24} />
            {contact.name || <span className="muted">{t('mailboxSettings.contactUnnamed')}</span>}
          </span>
        ),
      },
      {
        key: 'address',
        header: t('contacts.address'),
        filter: 'text',
        value: (contact) => contact.address,
        sort: (first, second) => first.address.localeCompare(second.address),
        render: (contact) => <span className="mono">{contact.address}</span>,
      },
      {
        key: 'count',
        header: t('contacts.messages'),
        width: '8rem',
        optional: true,
        value: (contact) => String(contact.count),
        sort: (first, second) => first.count - second.count,
        render: (contact) => <span className="muted">{contact.count}</span>,
      },
      {
        key: 'lastSeen',
        header: t('contacts.lastSeen'),
        width: '10rem',
        optional: true,
        value: (contact) => contact.lastSeenAt,
        sort: (first, second) => first.lastSeenAt.localeCompare(second.lastSeenAt),
        render: (contact) => (
          <span className="muted">
            <RelativeTime value={contact.lastSeenAt} />
          </span>
        ),
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
                className="icon-action"
                aria-label={`${contact.address}: ${t('common.edit')}`}
                disabled={busy}
                onClick={() => {
                  setEditing(contact)
                  setEditName(contact.name ?? '')
                  setProblem(null)
                }}
              >
                <PencilIcon size={16} />
              </button>
            </Tooltip>
            <Tooltip label={t('common.delete')}>
              <button
                type="button"
                className="icon-action danger"
                aria-label={`${contact.address}: ${t('common.delete')}`}
                disabled={busy}
                onClick={() => setDeleting(contact)}
              >
                <TrashIcon size={16} />
              </button>
            </Tooltip>
          </div>
        ),
      },
    ],
    [t, busy],
  )

  if (!mailboxes.loaded) {
    return <Loading />
  }
  if (!view) {
    return <p className="muted">{t('mailbox.none')}</p>
  }
  const contacts = query.data?.ListMailboxContacts ?? []

  return (
    <>
      <p className="muted">{t('mailboxSettings.contactsHint')}</p>
      <div className="page-actions">
        <button
          className="primary"
          type="button"
          onClick={() => {
            setAddress('')
            setName('')
            setProblem(null)
            setAdding(true)
          }}
        >
          {t('mailboxSettings.newContact')}
        </button>
      </div>

      {query.error ? <ErrorMessage error={query.error} /> : null}
      <DataTable
        columns={columns}
        selected={chosen}
        onSelect={setChosen}
        selectionActions={(addresses) => (
          // Shaped like the filter button it stands next to, rather than the
          // small bare icon a row's own actions use: this is a control in a
          // toolbar, and beside a bordered button with a word in it a 28px
          // icon reads as something half-drawn.
          <button type="button" className="danger" disabled={busy} onClick={() => setForgetting(true)}>
            <TrashIcon size={16} />
            {plural(
              addresses.length,
              { one: 'contacts.forgetChosenOne', other: 'contacts.forgetChosenOther' },
              { count: addresses.length },
            )}
          </button>
        )}
        rows={contacts}
        rowKey={(contact) => contact.address}
        loading={query.loading && !query.data}
        emptyMessage={t('mailboxSettings.noContacts')}
        countLabel={(count) => plural(count, { one: 'contacts.countOne', other: 'contacts.count' }, { count })}
      />

      {adding && (
        <FormDialog
          title={t('mailboxSettings.newContact')}
          submitLabel={t('common.create')}
          busy={busy}
          error={problem}
          canSubmit={address.trim().length > 0}
          onClose={() => setAdding(false)}
          onSubmit={() =>
            void run(async () => {
              await graphql(SAVE_CONTACT, { mailboxId, address: address.trim(), name: name.trim() || null })
            }).then((done) => done && setAdding(false))
          }
        >
          <label>
            {t('mailboxSettings.contactAddress')}
            <input type="email" value={address} onChange={(event) => setAddress(event.target.value)} required />
          </label>
          <label>
            {t('mailboxSettings.contactName')}
            <input value={name} onChange={(event) => setName(event.target.value)} />
          </label>
        </FormDialog>
      )}

      {editing && (
        <FormDialog
          title={t('contacts.edit')}
          submitLabel={t('common.save')}
          busy={busy}
          error={problem}
          onClose={() => setEditing(null)}
          onSubmit={() =>
            void run(async () => {
              await graphql(SAVE_CONTACT, { mailboxId, address: editing.address, name: editName.trim() || null })
            }).then((done) => done && setEditing(null))
          }
        >
          <label>
            {t('mailboxSettings.contactAddress')}
            <input type="email" value={editing.address} readOnly />
          </label>
          <label>
            {t('mailboxSettings.contactName')}
            <input value={editName} onChange={(event) => setEditName(event.target.value)} />
          </label>
        </FormDialog>
      )}

      {forgetting && (
        <ConfirmDialog
          title={plural(
            chosen.size,
            { one: 'contacts.forgetChosenTitleOne', other: 'contacts.forgetChosenTitleOther' },
            { count: chosen.size },
          )}
          body={t('contacts.forgetChosenBody', { count: chosen.size })}
          confirmLabel={t('common.delete')}
          busy={busy}
          error={problem}
          onConfirm={async () => {
            const ok = await run(() => graphql(DELETE_CONTACTS, { mailboxId, addresses: [...chosen] }))
            if (ok) {
              setChosen(new Set())
              setForgetting(false)
            }
          }}
          onClose={() => setForgetting(false)}
        />
      )}

      {deleting && (
        <ConfirmDialog
          title={t('mailboxSettings.deleteContact')}
          body={t('mailboxSettings.deleteContactConfirm', { address: deleting.address })}
          confirmLabel={t('common.delete')}
          busy={busy}
          onConfirm={() =>
            run(async () => {
              await graphql(DELETE_CONTACT, { mailboxId, address: deleting.address })
            }).then((done) => done && setDeleting(null))
          }
          onClose={() => setDeleting(null)}
        />
      )}
    </>
  )
}
