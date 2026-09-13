import { useState } from 'react'

import { MailProposal, graphql } from '../api'
import { useToast } from './toast'
import { useTranslation } from '../i18n/i18n'

// What a message carries that belongs somewhere else, offered above it.
//
// The agent read "shall we say Thursday at four" and worked out which
// Thursday, or read a signature and found a number nobody had written down.
// Neither is in the calendar or the address book: this card is the offer, the
// fields are editable because the agent may have read it slightly wrong, and
// the line it came from is quoted underneath so the person can see what it
// thought it saw.
//
// Nothing here happens on its own. An appointment put in somebody's diary
// because a stranger's message mentioned a day is how a calendar stops being
// trusted.

const SAVE_EVENT = `
  mutation ($calendarId: String!, $summary: String, $location: String, $startsAt: String, $endsAt: String, $allDay: Boolean) {
    SaveCalendarEvent(calendarId: $calendarId, summary: $summary, location: $location,
      startsAt: $startsAt, endsAt: $endsAt, allDay: $allDay) { id }
  }`

const SAVE_CONTACT = `
  mutation ($addressBookId: String!, $contactId: String, $name: String, $organization: String,
            $title: String, $emails: [String!], $phones: [String!], $note: String) {
    SaveContact(addressBookId: $addressBookId, contactId: $contactId, name: $name,
      organization: $organization, title: $title, emails: $emails, phones: $phones, note: $note) { id }
  }`

const CALENDARS = `query { ListCalendars { id name } }`
const BOOKS = `query { ListAddressBooks { id name } }`

const SET_STATUS = `
  mutation ($itemId: String!, $index: Int!, $status: String!) {
    SetMailProposalStatus(itemId: $itemId, index: $index, status: $status) { proposals { status } }
  }`

export function ProposalCards({
  itemId,
  proposals,
  onChanged,
}: {
  itemId: string
  proposals?: MailProposal[] | null
  onChanged: () => void
}) {
  const offered = (proposals ?? [])
    .map((proposal, index) => ({ proposal, index }))
    .filter(({ proposal }) => !proposal.status)
  if (offered.length === 0) {
    return null
  }
  return (
    <>
      {offered.map(({ proposal, index }) => (
        <ProposalCard key={index} itemId={itemId} index={index} proposal={proposal} onChanged={onChanged} />
      ))}
    </>
  )
}

function ProposalCard({
  itemId,
  index,
  proposal,
  onChanged,
}: {
  itemId: string
  index: number
  proposal: MailProposal
  onChanged: () => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const [summary, setSummary] = useState(proposal.summary ?? proposal.name ?? '')
  const [starts, setStarts] = useState(proposal.starts ?? '')
  const [ends, setEnds] = useState(proposal.ends ?? '')
  const [location, setLocation] = useState(proposal.location ?? '')
  const [organization, setOrganization] = useState(proposal.organization ?? '')

  const event = proposal.kind === 'event'

  async function keep() {
    setBusy(true)
    try {
      if (event) {
        const calendars = await graphql<{ ListCalendars: { id: string; name: string }[] }>(CALENDARS)
        const calendar = calendars.ListCalendars[0]
        if (!calendar) {
          throw new Error(t('proposal.noCalendar'))
        }
        await graphql(SAVE_EVENT, {
          calendarId: calendar.id,
          summary,
          location,
          startsAt: starts,
          endsAt: ends || undefined,
          allDay: proposal.allDay ?? false,
        })
      } else {
        const books = await graphql<{ ListAddressBooks: { id: string; name: string }[] }>(BOOKS)
        const book = books.ListAddressBooks[0]
        if (!book) {
          throw new Error(t('proposal.noAddressBook'))
        }
        await graphql(SAVE_CONTACT, {
          addressBookId: book.id,
          contactId: proposal.contactId || undefined,
          name: summary,
          organization,
          title: proposal.title,
          emails: proposal.emails ?? [],
          phones: proposal.phones ?? [],
          note: proposal.note,
        })
      }
      await graphql(SET_STATUS, { itemId, index, status: 'accepted' })
      toast.done(event ? t('proposal.added') : t('proposal.saved'))
      onChanged()
    } catch (caught) {
      toast.failure(caught, event ? t('proposal.addFailed') : t('proposal.saveFailed'))
    } finally {
      setBusy(false)
    }
  }

  async function dismiss() {
    setBusy(true)
    try {
      await graphql(SET_STATUS, { itemId, index, status: 'dismissed' })
      toast.done(t('proposal.dismissed'))
      onChanged()
    } catch (caught) {
      toast.failure(caught, t('proposal.dismissFailed'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="invitation-card" aria-label={event ? t('proposal.event') : t('proposal.contact')}>
      <header>
        <h4>{event ? t('proposal.event') : t('proposal.contact')}</h4>
      </header>
      <div className="form-narrow">
        <label>
          <span>{event ? t('proposal.what') : t('proposal.who')}</span>
          <input value={summary} disabled={busy} onChange={(change) => setSummary(change.target.value)} />
        </label>
        {event ? (
          <>
            <div className="row">
              <label>
                <span>{t('proposal.starts')}</span>
                <input value={starts} disabled={busy} onChange={(change) => setStarts(change.target.value)} />
              </label>
              <label>
                <span>{t('proposal.ends')}</span>
                <input value={ends} disabled={busy} onChange={(change) => setEnds(change.target.value)} />
              </label>
            </div>
            <label>
              <span>{t('proposal.where')}</span>
              <input value={location} disabled={busy} onChange={(change) => setLocation(change.target.value)} />
            </label>
          </>
        ) : (
          <>
            <label>
              <span>{t('proposal.organization')}</span>
              <input value={organization} disabled={busy} onChange={(change) => setOrganization(change.target.value)} />
            </label>
            {(proposal.emails ?? []).length > 0 || (proposal.phones ?? []).length > 0 ? (
              <p className="muted">{[...(proposal.emails ?? []), ...(proposal.phones ?? [])].join(' · ')}</p>
            ) : null}
          </>
        )}
      </div>
      {proposal.because ? <blockquote className="muted">{proposal.because}</blockquote> : null}
      <div className="page-actions-end">
        <button type="button" disabled={busy} onClick={() => void dismiss()}>
          {t('proposal.dismiss')}
        </button>
        <button type="button" className="primary" disabled={busy} onClick={() => void keep()}>
          {event ? t('proposal.add') : t('proposal.save')}
        </button>
      </div>
    </section>
  )
}
