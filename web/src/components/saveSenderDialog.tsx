import { useEffect, useState } from 'react'

import { graphql } from '../api'
import { FormDialog } from './dialog'
import { useToast } from './toast'
import { useTranslation } from '../i18n/i18n'

// Keeping the person who wrote to you.
//
// The address book is the server's only list of people, and nothing arrives
// in it by itself — so this is the way a sender becomes somebody you keep:
// from the message, in one press, with the form filled in from what the
// message said and shown before anything is written.
//
// Shown, because a message is a stranger's words. "Avalon Trending Posts"
// split into a first and a last name is a guess, and the guess is the
// reason the dialog exists: it is easier to correct two boxes than to find
// the contact afterwards and fix what was kept.

const BOOKS = `query { ListAddressBooks { id } }`

const FIND = `
  query ($addressBookId: String!, $query: String) {
    ListContacts(addressBookId: $addressBookId, query: $query, first: 20) { id name emails }
  }`

const GET = `query ($id: String!) { GetContact(id: $id) { id name organization emails note } }`

const SAVE = `
  mutation ($addressBookId: String!, $id: String, $name: String, $organization: String,
            $emails: [String!], $note: String) {
    SaveContact(addressBookId: $addressBookId, id: $id, name: $name, organization: $organization,
                emails: $emails, note: $note) { id name }
  }`

// splitName is a display name as a first and a last name.
//
// "Ziyan Zhou" is two names; "Zhou, Ziyan" is the same two the other way
// round, which is how a great many systems write them; and anything with
// more words puts the first word first and the rest after it, which is
// wrong for some names and right for most. All three are a guess the person
// is about to see and can correct.
export function splitName(display: string): { first: string; last: string } {
  const name = display.trim().replace(/\s+/g, ' ')
  if (!name) {
    return { first: '', last: '' }
  }
  const comma = name.indexOf(',')
  if (comma > 0) {
    return { first: name.slice(comma + 1).trim(), last: name.slice(0, comma).trim() }
  }
  const space = name.indexOf(' ')
  if (space < 0) {
    return { first: name, last: '' }
  }
  return { first: name.slice(0, space), last: name.slice(space + 1) }
}

export function SaveSenderDialog({
  address,
  displayName,
  onClose,
}: {
  address: string
  displayName?: string
  onClose: () => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const parted = splitName(displayName ?? '')
  const [first, setFirst] = useState(parted.first)
  const [last, setLast] = useState(parted.last)
  const [organization, setOrganization] = useState('')
  const [email, setEmail] = useState(address)
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)
  const [bookId, setBookId] = useState('')
  // The contact this address already belongs to, when it does. Saving then
  // changes that contact rather than making a second one with the same
  // address in it, which is how an address book becomes two of everybody.
  const [existing, setExisting] = useState<{ id: string; emails: string[] } | null>(null)

  useEffect(() => {
    let stopped = false
    void (async () => {
      try {
        const books = await graphql<{ ListAddressBooks: { id: string }[] }>(BOOKS)
        const book = books.ListAddressBooks[0]
        if (!book || stopped) {
          return
        }
        setBookId(book.id)
        const found = await graphql<{ ListContacts: { id: string; name?: string; emails?: string[] }[] }>(FIND, {
          addressBookId: book.id,
          query: address,
        })
        if (stopped) {
          return
        }
        const wanted = address.trim().toLowerCase()
        const already = found.ListContacts.find((contact) =>
          (contact.emails ?? []).some((each) => each.trim().toLowerCase() === wanted),
        )
        if (already) {
          // The whole contact, not the listing: every box has to show what
          // is kept before it is saved back. SaveContact reads an empty box
          // as "clear this", so a form that opened blank over somebody's
          // organization and note would delete them on the way past.
          const full = await graphql<{
            GetContact: { id: string; name?: string; organization?: string; emails?: string[]; note?: string }
          }>(GET, { id: already.id })
          if (stopped) {
            return
          }
          const kept = full.GetContact
          setExisting({ id: kept.id, emails: kept.emails ?? [] })
          // Their name as it is kept wins over the one this message put on
          // it: the kept one is what somebody chose.
          const parts = splitName(kept.name ?? '')
          if (parts.first || parts.last) {
            setFirst(parts.first)
            setLast(parts.last)
          }
          setOrganization(kept.organization ?? '')
          setNote(kept.note ?? '')
        }
      } catch (failure) {
        setProblem(failure instanceof Error ? failure.message : String(failure))
      }
    })()
    return () => {
      stopped = true
    }
  }, [address])

  const save = async () => {
    const name = [first.trim(), last.trim()].filter(Boolean).join(' ')
    if (!email.trim()) {
      setProblem(t('saveSender.needsAddress'))
      return
    }
    setBusy(true)
    try {
      // Adding an address rather than replacing the list: somebody with a
      // work address and a personal one keeps both, and this one joins them.
      // Emails is a whole list to SaveContact, so sending the one this
      // message came from would have been the only one they had left.
      const wanted = email.trim()
      const merged = [...(existing?.emails ?? [])]
      if (!merged.some((each) => each.trim().toLowerCase() === wanted.toLowerCase())) {
        merged.push(wanted)
      }
      await graphql(SAVE, {
        addressBookId: bookId,
        id: existing?.id ?? null,
        // A contact with no name is found by nobody, so the address stands
        // in for one — the same rule the agent's proposals follow.
        name: name || wanted,
        organization: organization.trim(),
        emails: merged,
        note: note.trim(),
      })
      toast.done(existing ? t('saveSender.updated') : t('saveSender.saved'))
      onClose()
    } catch (failure) {
      setProblem(failure instanceof Error ? failure.message : String(failure))
      toast.failure(failure, t('saveSender.failed'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <FormDialog
      title={existing ? t('saveSender.titleExisting') : t('saveSender.title')}
      submitLabel={existing ? t('common.save') : t('saveSender.keep')}
      busy={busy}
      error={problem}
      canSubmit={Boolean(bookId) && !busy}
      onSubmit={() => void save()}
      onClose={onClose}
    >
      {existing && <p className="muted">{t('saveSender.already')}</p>}
      <div className="row">
        <label>
          <span>{t('saveSender.firstName')}</span>
          <input value={first} disabled={busy} onChange={(event) => setFirst(event.target.value)} autoFocus />
        </label>
        <label>
          <span>{t('saveSender.lastName')}</span>
          <input value={last} disabled={busy} onChange={(event) => setLast(event.target.value)} />
        </label>
      </div>
      <label>
        <span>{t('saveSender.organization')}</span>
        <input value={organization} disabled={busy} onChange={(event) => setOrganization(event.target.value)} />
      </label>
      <label>
        <span>{t('saveSender.email')}</span>
        <input value={email} disabled={busy} onChange={(event) => setEmail(event.target.value)} />
      </label>
      <label>
        <span>{t('saveSender.note')}</span>
        <input value={note} disabled={busy} onChange={(event) => setNote(event.target.value)} />
      </label>
    </FormDialog>
  )
}
