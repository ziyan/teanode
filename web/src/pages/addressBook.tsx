import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'

import { graphql } from '../api'
import { Avatar } from '../components/avatar'
import { PageHeadingActions } from '../components/breadcrumb'
import { contactPhotoAddress, SET_CONTACT_ACCOUNT, useContactEditor } from '../components/contactEditor'
import { Column, DataTable } from '../components/dataTable'
import { ConfirmDialog } from '../components/dialog'
import { PencilIcon, TrashIcon, UserIcon, KeptPersonIcon } from '../components/icons'
import { Tooltip } from '../components/tooltip'
import { SenderLogo } from '../components/senderLogo'
import { useQuery } from '../components/useQuery'
import { useToast } from '../components/toast'
import { useTranslation } from '../i18n/i18n'
import { CIRCLE_PERSON_FIELDS, contactPersonPath, type CirclePerson } from '../lib/circlePeople'
import { personName } from '../components/personLabel'

// The address book: the people somebody chose to keep.
//
// It is the only list of people the server has. Nothing is learned from
// traffic any more: this is what the composer completes from, what the
// "sender is known" rule condition asks, and what the agent reads. A name in
// the list opens the person's card; the form that changes a contact is the
// one in components/contactEditor.tsx, opened by the pencil.

const BOOKS = `query { ListAddressBooks { id name description contacts } }`

const CONTACTS = `
  query ($addressBookId: String!, $query: String, $first: Int) {
    ListContacts(addressBookId: $addressBookId, query: $query, first: $first) {
      id uid name organization emails phones hasPhoto addresses { written }
      accountUserId account { ${CIRCLE_PERSON_FIELDS} }
    }
  }`

const DELETE = `mutation ($id: String!) { DeleteContact(id: $id) }`

// Contacts whose address is the address of a person in the circles, offered
// for the owner to confirm.
const SUGGESTIONS = `query {
  ListContactAccountSuggestions {
    addressBookId
    contact { id name emails }
    account { ${CIRCLE_PERSON_FIELDS} }
  }
}`

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
  // The person in the circles this contact is, when the owner said so.
  accountUserId?: string | null
  account?: CirclePerson | null
}

type Suggestion = {
  addressBookId: string
  contact: { id: string; name?: string; emails: string[] }
  account: CirclePerson
}

// "Not now" on a suggestion is the owner's choice about one contact, and it
// belongs to this browser: the server never hears of it.
const DISMISSED_SUGGESTIONS_KEY = 'teanode.addressBook.dismissedSuggestions'

function readDismissedSuggestions(): string[] {
  try {
    const parsed: unknown = JSON.parse(window.localStorage.getItem(DISMISSED_SUGGESTIONS_KEY) ?? '[]')
    return Array.isArray(parsed) ? parsed.filter((each): each is string => typeof each === 'string') : []
  } catch {
    return []
  }
}

function writeDismissedSuggestions(contactIds: string[]) {
  try {
    window.localStorage.setItem(DISMISSED_SUGGESTIONS_KEY, JSON.stringify(contactIds))
  } catch {
    // Dismissed until the page is closed.
  }
}

// Which contact is the person themselves. Kept on the account rather than
// on the agent, because it is true of them whether or not they have one:
// the composer and a CardDAV client can read it too. The agent's use of
// it is to know their own addresses, and so to tell their own mail and
// their own commits from anybody else's.
// Asked through the agent's own self page, which is where the card is
// read: one answer, and no second endpoint saying the same thing.
const ME = `query { AgentGraphPage(path: "self") { contact { id } } }`
const SET_ME = `mutation ($contactId: String) { SetMyContact(contactId: $contactId) }`

export function AddressBookPage() {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const books = useQuery(() => graphql<{ ListAddressBooks: AddressBook[] }>(BOOKS), [], { refresh: false })
  const book = books.data?.ListAddressBooks?.[0] ?? null
  const bookId = book?.id ?? ''
  // The address can name a card ("?contact=") or words to look for
  // ("?search="), as the search across everything does: the words are asked of
  // the server, and a chip over the table says so and takes them off. A card
  // is opened as a person's card, which is where the memory, the search and
  // the photos send somebody who only knows the contact's id.
  const [parameters, setParameters] = useSearchParams()
  const navigate = useNavigate()
  const searchWords = (parameters.get('search') ?? '').trim()
  const contacts = useQuery(
    () =>
      bookId
        ? graphql<{ ListContacts: Contact[] }>(CONTACTS, {
            addressBookId: bookId,
            query: searchWords || null,
            first: null,
          })
        : Promise.resolve(null),
    [bookId, searchWords],
    { refresh: false },
  )
  const me = useQuery(() => graphql<{ AgentGraphPage: { contact: { id: string } | null } | null }>(ME), [], {
    refresh: false,
  })

  const myContactId = me.data?.AgentGraphPage?.contact?.id ?? ''

  // The contacts that look like people in the circles. Asked once; a failure
  // costs only the offer, so nothing is said about it.
  const suggestions = useQuery(() => graphql<{ ListContactAccountSuggestions: Suggestion[] | null }>(SUGGESTIONS), [], {
    refresh: false,
  })
  const [dismissedContactIds, setDismissedContactIds] = useState<string[]>(readDismissedSuggestions)

  const [deleting, setDeleting] = useState<Contact | null>(null)
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

  // The accounts that a loaded contact of this book already is, left out of
  // the editor's picker. The server is the guard: it refuses a second contact
  // for the same account in a book, and that refusal is shown as a toast.
  const rows = contacts.data?.ListContacts ?? []
  const takenAccountIds = new Set(
    rows.filter((contact) => contact.accountUserId).map((contact) => contact.accountUserId!),
  )
  const editor = useContactEditor({
    addressBookId: bookId,
    takenAccountIds,
    onChanged: () => Promise.all([contacts.reload(), books.reload(), suggestions.reload()]),
  })

  // A link from the agent's memory, the search or the photos names the card in
  // the address: the card's page opens in its place.
  const linkedContactId = parameters.get('contact') ?? ''
  useEffect(() => {
    if (!linkedContactId || !bookId) return
    void navigate(contactPersonPath(bookId, linkedContactId), { replace: true })
  }, [linkedContactId, bookId, navigate])

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

  // Naming the contact that is the person themselves. One at a time:
  // choosing a second clears the first, and choosing the one that is
  // already marked unmarks it.
  const markAsMe = useCallback(
    async (contact: Contact | null) => {
      setBusy(true)
      try {
        await graphql(SET_ME, { contactId: contact?.id ?? null })
        toast.done(t('agent.myContactSaved'))
        await me.reload()
      } catch (caught) {
        toast.failed(caught instanceof Error ? caught.message : String(caught))
      } finally {
        setBusy(false)
      }
    },
    [me, toast, t],
  )

  // The Address column is drawn only when the address book has an address at
  // all: most address books have none, and a wide empty column pushes the
  // organization and the phone off a laptop screen. It is read from the whole
  // list (never from what a filter leaves) and, once seen, stays, so the
  // columns do not move when a card is edited or the list reloads.
  const [hasAddress, setHasAddress] = useState(false)
  useEffect(() => {
    if (
      (contacts.data?.ListContacts ?? []).some((contact) =>
        (contact.addresses ?? []).some((address) => Boolean(address.written)),
      )
    )
      setHasAddress(true)
  }, [contacts.data])

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
              src={contact.hasPhoto ? contactPhotoAddress(contact.id) : undefined}
              size={24}
            />
            <span className="contact-name">
              <Link to={contactPersonPath(bookId, contact.id)}>
                {contact.name || <span className="muted">{t('mailboxSettings.contactUnnamed')}</span>}
              </Link>
              {/* The Email and Phone columns are dropped on a phone, so the
                  address and the number go under the name there. */}
              {(contact.emails ?? [])[0] || (contact.phones ?? [])[0] ? (
                <span className="contact-email-line muted">
                  {[(contact.emails ?? [])[0], (contact.phones ?? [])[0]].filter(Boolean).join(' · ')}
                </span>
              ) : null}
            </span>
          </span>
        ),
      },
      {
        key: 'emails',
        header: t('addressBook.email'),
        filter: 'text',
        optional: true,
        value: (contact) => (contact.emails ?? []).join(' '),
        render: (contact) => (
          <span>
            {(contact.emails ?? [])[0] ?? ''}
            {(contact.emails ?? []).length > 1 ? (
              <span className="muted"> {t('addressBook.more', { count: contact.emails.length - 1 })}</span>
            ) : null}
          </span>
        ),
      },
      ...(hasAddress
        ? [
            {
              key: 'postal',
              header: t('addressBook.postal'),
              filter: 'text' as const,
              optional: true,
              truncate: true,
              value: (contact: Contact) => (contact.addresses ?? []).map((address) => address.written ?? '').join(' '),
              render: (contact: Contact) => (
                <span className="contact-postal muted">{(contact.addresses ?? [])[0]?.written ?? ''}</span>
              ),
            },
          ]
        : []),
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
        width: '9rem',
        optional: true,
        value: (contact) => (contact.phones ?? []).join(' '),
        render: (contact) => <span className="muted">{(contact.phones ?? [])[0] ?? ''}</span>,
      },
      {
        key: 'actions',
        header: '',
        width: '9rem',
        render: (contact) => (
          <div className="row-actions">
            <Tooltip label={contact.id === myContactId ? t('agent.myContactClear') : t('agent.myContactSet')}>
              {/* The card that is the person shows a person with a tick,
                  in the accent: the same outline in both states left "not
                  me" and "that is you" telling apart by tooltip alone. */}
              <button
                type="button"
                className={contact.id === myContactId ? 'icon-action marked' : 'icon-action'}
                disabled={busy}
                aria-label={`${contact.name}: ${contact.id === myContactId ? t('agent.myContactClear') : t('agent.myContactSet')}`}
                onClick={() => void markAsMe(contact.id === myContactId ? null : contact)}
              >
                {contact.id === myContactId ? <KeptPersonIcon size={16} /> : <UserIcon size={16} />}
              </button>
            </Tooltip>
            <Tooltip label={t('common.edit')}>
              <button
                type="button"
                className="icon-action"
                aria-label={`${contact.name}: ${t('common.edit')}`}
                disabled={busy || editor.isBusy || editor.openingContactId === contact.id}
                onClick={() => void editor.open(contact.id)}
              >
                <PencilIcon size={16} />
              </button>
            </Tooltip>
            <Tooltip label={t('common.delete')}>
              <button
                type="button"
                className="icon-action danger"
                aria-label={`${contact.name}: ${t('common.delete')}`}
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
    [t, busy, editor, bookId, myContactId, markAsMe, hasAddress],
  )

  // A suggestion is answered one contact at a time. The editor makes the other
  // links, from a contact's own form.
  const linkSuggestion = async (suggestion: Suggestion, contactName: string) => {
    const isDone = await run(
      () =>
        graphql(SET_CONTACT_ACCOUNT, {
          addressBookId: suggestion.addressBookId,
          contactId: suggestion.contact.id,
          accountUserId: suggestion.account.userId,
        }),
      t('addressBook.accountLinked', { name: contactName, account: personName(suggestion.account) }),
    )
    if (isDone) void suggestions.reload()
  }

  const dismissSuggestion = (contactId: string) => {
    const next = [...dismissedContactIds, contactId]
    setDismissedContactIds(next)
    writeDismissedSuggestions(next)
  }
  // Suggestions come from every address book, each with the book its contact
  // is in. One the owner already answered "Not now" is not offered again.
  const offeredSuggestions = (suggestions.data?.ListContactAccountSuggestions ?? []).filter(
    (suggestion) => !dismissedContactIds.includes(suggestion.contact.id),
  )
  return (
    <>
      <PageHeadingActions>
        <button className="primary" type="button" disabled={!bookId} onClick={editor.openNew}>
          {t('addressBook.new')}
        </button>
      </PageHeadingActions>

      {offeredSuggestions.length > 0 && (
        <div className="card account-suggestions" role="region" aria-label={t('addressBook.suggestionsLabel')}>
          {offeredSuggestions.map((suggestion) => {
            const contactName = suggestion.contact.name || suggestion.contact.emails[0] || ''
            return (
              <div key={suggestion.contact.id} className="account-suggestion">
                <Avatar
                  name={personName(suggestion.account)}
                  avatarUrl={suggestion.account.avatarUrl}
                  colorIndex={suggestion.account.avatarColorIndex}
                  size={28}
                />
                <span className="account-suggestion-text">
                  {t('addressBook.suggestionText', { name: contactName, account: personName(suggestion.account) })}
                </span>
                <div className="account-suggestion-actions">
                  <button
                    type="button"
                    className="primary"
                    disabled={busy}
                    aria-label={t('addressBook.suggestionLinkFor', { name: contactName })}
                    onClick={() => void linkSuggestion(suggestion, contactName)}
                  >
                    {t('addressBook.suggestionLink')}
                  </button>
                  <button
                    type="button"
                    disabled={busy}
                    aria-label={t('addressBook.suggestionLaterFor', { name: contactName })}
                    onClick={() => dismissSuggestion(suggestion.contact.id)}
                  >
                    {t('addressBook.suggestionLater')}
                  </button>
                </div>
              </div>
            )
          })}
        </div>
      )}

      <DataTable
        columns={columns}
        rows={rows}
        rowKey={(contact) => contact.id}
        loading={(contacts.loading && !contacts.data) || (books.loading && !books.data)}
        emptyMessage={t('addressBook.empty')}
        pinned={
          searchWords
            ? [
                {
                  key: 'search',
                  label: t('search.matching', { words: searchWords }),
                  onClear: () =>
                    setParameters(
                      (previous) => {
                        const next = new URLSearchParams(previous)
                        next.delete('search')
                        return next
                      },
                      { replace: true },
                    ),
                },
              ]
            : undefined
        }
        countLabel={(count) => plural(count, { one: 'contacts.countOne', other: 'contacts.count' }, { count })}
      />

      {editor.dialogs}

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
