import { useMemo, useState } from 'react'

import { graphql } from '../api'
import { ErrorMessage, Loading } from '../components/common'
import { Column, DataTable } from '../components/dataTable'
import { ConfirmDialog } from '../components/dialog'
import { PencilIcon, TrashIcon } from '../components/icons'
import { RelativeTime } from '../components/relativeTime'
import { useQuery } from '../components/useQuery'
import { useTranslation } from '../i18n/i18n'
import { useMailboxes } from '../mailboxes'

const CONTACTS = `
  query ($mailboxId: String!, $first: Int) {
    ListMailboxContacts(mailboxId: $mailboxId, first: $first) { address name lastSeenAt count }
  }`

const SAVE_CONTACT = `
  mutation ($mailboxId: String!, $address: String!, $name: String) {
    SaveMailboxContact(mailboxId: $mailboxId, address: $address, name: $name) { address }
  }`

const DELETE_CONTACT = `
  mutation ($mailboxId: String!, $address: String!) {
    DeleteMailboxContact(mailboxId: $mailboxId, address: $address)
  }`

type Contact = { address: string; name?: string; lastSeenAt: string; count: number }

// Everyone the mailbox has written to or heard from, and anyone added by
// hand: the list the compose page completes addresses from, and a rule can
// ask about. A page of its own, listed the way the other lists are.
export function MailboxContactsPage() {
  const { t, plural } = useTranslation()
  const mailboxes = useMailboxes()
  const view = mailboxes.current
  const mailboxId = view?.mailbox.id ?? ''
  const query = useQuery(
    () => (mailboxId ? graphql<{ ListMailboxContacts: Contact[] }>(CONTACTS, { mailboxId, first: 500 }) : Promise.resolve(null)),
    [mailboxId],
    { refresh: false },
  )
  const [address, setAddress] = useState('')
  const [name, setName] = useState('')
  const [editing, setEditing] = useState<Contact | null>(null)
  const [editName, setEditName] = useState('')
  const [deleting, setDeleting] = useState<Contact | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<unknown>(null)

  const run = async (action: () => Promise<unknown>) => {
    setBusy(true)
    try {
      await action()
      setError(null)
      await query.reload()
    } catch (failure) {
      setError(failure)
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
        render: (contact) =>
          editing?.address === contact.address ? (
            <form
              className="inline-form"
              onSubmit={(event) => {
                event.preventDefault()
                void run(async () => {
                  await graphql(SAVE_CONTACT, { mailboxId, address: contact.address, name: editName.trim() || null })
                  setEditing(null)
                })
              }}
            >
              <input value={editName} onChange={(event) => setEditName(event.target.value)} autoFocus placeholder={t('mailboxSettings.contactName')} />
              <button type="submit" className="primary" disabled={busy}>
                {t('common.save')}
              </button>
              <button type="button" onClick={() => setEditing(null)}>
                {t('common.cancel')}
              </button>
            </form>
          ) : (
            contact.name || <span className="muted">{t('mailboxSettings.contactUnnamed')}</span>
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
        render: (contact) =>
          editing?.address === contact.address ? null : (
            <div className="row-actions">
              <button
                type="button"
                className="icon-action"
                title={t('common.rename')}
                aria-label={`${contact.address}: ${t('common.rename')}`}
                disabled={busy}
                onClick={() => {
                  setEditing(contact)
                  setEditName(contact.name ?? '')
                }}
              >
                <PencilIcon size={16} />
              </button>
              <button
                type="button"
                className="icon-action danger"
                title={t('common.delete')}
                aria-label={`${contact.address}: ${t('common.delete')}`}
                disabled={busy}
                onClick={() => setDeleting(contact)}
              >
                <TrashIcon size={16} />
              </button>
            </div>
          ),
      },
    ],
    [t, editing, editName, busy, mailboxId],
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
      <form
        className="card form-narrow"
        onSubmit={(event) => {
          event.preventDefault()
          void run(async () => {
            await graphql(SAVE_CONTACT, { mailboxId, address: address.trim(), name: name.trim() || null })
            setAddress('')
            setName('')
          })
        }}
      >
        <h3>{t('mailboxSettings.newContact')}</h3>
        <label>
          {t('mailboxSettings.contactAddress')}
          <input type="email" value={address} onChange={(event) => setAddress(event.target.value)} required />
        </label>
        <label>
          {t('mailboxSettings.contactName')}
          <input value={name} onChange={(event) => setName(event.target.value)} />
        </label>
        {error ? <ErrorMessage error={error} /> : null}
        <div className="page-actions">
          <button className="primary" type="submit" disabled={busy || !address.trim()}>
            {t('common.create')}
          </button>
        </div>
      </form>

      {query.error ? <ErrorMessage error={query.error} /> : null}
      <DataTable
        columns={columns}
        rows={contacts}
        rowKey={(contact) => contact.address}
        loading={query.loading && !query.data}
        emptyMessage={t('mailboxSettings.noContacts')}
        countLabel={(count) => plural(count, { one: 'contacts.countOne', other: 'contacts.count' }, { count })}
      />

      {deleting && (
        <ConfirmDialog
          title={t('mailboxSettings.deleteContact')}
          body={t('mailboxSettings.deleteContactConfirm', { address: deleting.address })}
          confirmLabel={t('common.delete')}
          busy={busy}
          onConfirm={() =>
            run(async () => {
              await graphql(DELETE_CONTACT, { mailboxId, address: deleting.address })
              setDeleting(null)
            })
          }
          onClose={() => setDeleting(null)}
        />
      )}
    </>
  )
}
