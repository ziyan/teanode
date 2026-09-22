import { useState } from 'react'

import { MailProposal, graphql } from '../api'
import { useSession } from '../session'
import { useCalendarMutation } from '../hooks/useCalendarMutation'
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

const SAVE_CONTACT = `
  mutation ($addressBookId: String!, $contactId: String, $name: String, $organization: String,
            $title: String, $emails: [String!], $phones: [String!], $note: String) {
    SaveContact(addressBookId: $addressBookId, id: $contactId, name: $name,
      organization: $organization, title: $title, emails: $emails, phones: $phones, note: $note) { id }
  }`

const CALENDARS = `query { ListCalendars { id name } }`
const BOOKS = `query { ListAddressBooks { id name } }`

const SET_STATUS = `
  mutation ($itemId: String!, $index: Int!, $status: String!) {
    SetMailProposalStatus(itemId: $itemId, index: $index, status: $status) { proposals { status } }
  }`

interface ProposalCardsProps {
  itemId: string
  proposals?: MailProposal[] | null
  onChanged: () => void
}
export function ProposalCards(props: ProposalCardsProps) {
  const session = useSession()
  return (
    <ProposalCardsForAccount
      key={`${session.userId ?? ''}:${props.itemId}`}
      ownerId={session.userId ?? ''}
      {...props}
    />
  )
}
function ProposalCardsForAccount({ ownerId, itemId, proposals, onChanged }: ProposalCardsProps & { ownerId: string }) {
  const submission = useCalendarMutation(ownerId)
  const offered = (proposals ?? [])
    .map((proposal, index) => ({ proposal, index }))
    .filter(({ proposal }) => !proposal.status)
  // A committed acceptance can disappear from the offers before its response
  // arrives. Keep its retained card available to resolve that response on reload.
  const retained = submission.pending?.variables
  if (
    retained?.proposalItemId === itemId &&
    typeof retained.proposalIndex === 'number' &&
    typeof retained.expectedProposal === 'string'
  ) {
    try {
      const original = JSON.parse(retained.expectedProposal) as MailProposal
      const existing = offered.find((entry) => entry.index === retained.proposalIndex)
      if (existing) existing.proposal = original
      else offered.push({ index: retained.proposalIndex, proposal: original })
    } catch {
      /* The shared calendar recovery panel still retains invalid input. */
    }
  }
  if (offered.length === 0) {
    return null
  }
  return (
    <>
      {offered.map(({ proposal, index }) => (
        <ProposalCard
          key={`${itemId}:${index}:${JSON.stringify(proposal)}`}
          submission={submission}
          itemId={itemId}
          index={index}
          proposal={proposal}
          onChanged={onChanged}
        />
      ))}
    </>
  )
}

function ProposalCard({
  submission,
  itemId,
  index,
  proposal,
  onChanged,
}: {
  submission: ReturnType<typeof useCalendarMutation>
  itemId: string
  index: number
  proposal: MailProposal
  onChanged: () => void
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const isOwnPending =
    submission.pending?.variables.proposalItemId === itemId && submission.pending?.variables.proposalIndex === index
  const saved = isOwnPending ? submission.pending?.variables : null
  const [busy, setBusy] = useState(false)
  // An empty summary is "" rather than null, and `??` only falls through on
  // null -- so a contact whose name the run found was offered with an empty
  // name box, with the name it had found nowhere on the card.
  const [summary, setSummary] = useState(
    typeof saved?.summary === 'string' ? saved.summary : proposal.summary || proposal.name || '',
  )
  const [starts, setStarts] = useState(typeof saved?.startsAt === 'string' ? saved.startsAt : (proposal.starts ?? ''))
  const [ends, setEnds] = useState(typeof saved?.endsAt === 'string' ? saved.endsAt : (proposal.ends ?? ''))
  const [location, setLocation] = useState(
    typeof saved?.location === 'string' ? saved.location : (proposal.location ?? ''),
  )
  const [organization, setOrganization] = useState(proposal.organization ?? '')

  const event = proposal.kind === 'event'

  async function keep() {
    setBusy(true)
    try {
      if (event && isOwnPending) {
        await submission.execute()
      } else if (event) {
        const calendars = await graphql<{ ListCalendars: { id: string; name: string }[] }>(CALENDARS)
        const calendar = calendars.ListCalendars[0]
        if (!calendar) {
          throw new Error(t('proposal.noCalendar'))
        }
        await submission.execute({
          operation: 'save',
          summary,
          variables: {
            id: null,
            proposalItemId: itemId,
            proposalIndex: index,
            expectedProposal: JSON.stringify(proposal),
            calendarId: calendar.id,
            summary,
            location,
            startsAt: starts,
            endsAt: ends || undefined,
            allDay: proposal.allDay ?? false,
          },
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
      if (!event) await graphql(SET_STATUS, { itemId, index, status: 'accepted' })
      toast.done(event ? t('proposal.added') : t('proposal.saved'))
      onChanged()
    } catch (caught) {
      toast.failure(caught, event ? t('proposal.addFailed') : t('proposal.saveFailed'))
    } finally {
      setBusy(false)
    }
  }

  async function stop() {
    setBusy(true)
    try {
      const isCompleted = await submission.cancel()
      toast.done(t(isCompleted ? 'proposal.added' : 'calendar.stopped'))
      onChanged()
    } catch (failure) {
      toast.failure(failure, t('proposal.addFailed'))
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
          <input
            value={summary}
            disabled={busy || (event && !!submission.pending)}
            onChange={(change) => setSummary(change.target.value)}
          />
        </label>
        {event ? (
          <>
            <div className="row">
              <label>
                <span>{t('proposal.starts')}</span>
                <input
                  value={starts}
                  disabled={busy || (event && !!submission.pending)}
                  onChange={(change) => setStarts(change.target.value)}
                />
              </label>
              <label>
                <span>{t('proposal.ends')}</span>
                <input
                  value={ends}
                  disabled={busy || (event && !!submission.pending)}
                  onChange={(change) => setEnds(change.target.value)}
                />
              </label>
            </div>
            <label>
              <span>{t('proposal.where')}</span>
              <input
                value={location}
                disabled={busy || (event && !!submission.pending)}
                onChange={(change) => setLocation(change.target.value)}
              />
            </label>
          </>
        ) : (
          <>
            <label>
              <span>{t('proposal.organization')}</span>
              <input
                value={organization}
                disabled={busy || (event && !!submission.pending)}
                onChange={(change) => setOrganization(change.target.value)}
              />
            </label>
            {(proposal.emails ?? []).length > 0 || (proposal.phones ?? []).length > 0 ? (
              <p className="muted">{[...(proposal.emails ?? []), ...(proposal.phones ?? [])].join(' · ')}</p>
            ) : null}
          </>
        )}
      </div>
      {proposal.because ? <blockquote className="muted">{proposal.because}</blockquote> : null}
      {event && submission.pending && (
        <p className="muted">{t(isOwnPending ? 'calendar.pendingSave' : 'proposal.pendingElsewhere')}</p>
      )}
      <div className="page-actions page-actions-end">
        {event && isOwnPending && (
          <button type="button" disabled={busy || submission.isWorking} onClick={() => void stop()}>
            {t('calendar.stop')}
          </button>
        )}
        <button type="button" disabled={busy || (event && !!submission.pending)} onClick={() => void dismiss()}>
          {t('proposal.dismiss')}
        </button>
        <button
          type="button"
          className="primary"
          disabled={busy || submission.isWorking || (event && !!submission.pending && !isOwnPending)}
          onClick={() => void keep()}
        >
          {event ? t(isOwnPending ? 'calendar.retry' : 'proposal.add') : t('proposal.save')}
        </button>
      </div>
    </section>
  )
}
