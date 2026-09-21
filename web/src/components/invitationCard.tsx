import { useMemo, useState } from 'react'

import { graphql } from '../api'
import { useSession } from '../session'
import { InvitationAnswer, useInvitationAnswer } from '../hooks/useInvitationAnswer'
import { useQuery } from '../components/useQuery'
import { useToast } from '../components/toast'
import { useTranslation } from '../i18n/i18n'

// The card the reader draws above a message that carried an invitation.
//
// The server has already read the message, put the event in the person's
// calendar and worked out which of the answers is theirs, so this shows what
// it found and offers the three buttons. Pressing one marks their own copy
// and sends the answer to whoever asked.

const INVITATION = `
  query ($itemId: String!) {
    GetMailInvitation(itemId: $itemId) {
      id status method uid because summary location startsAt endsAt allDay cancelled
      organizer participation calendarId eventId
      attendees { address name participation role }
    }
  }`

type Attendee = { address: string; name?: string; participation?: string; role?: string }

type Invitation = {
  id: string
  status: string
  method: string
  uid?: string
  because?: string
  summary?: string
  location?: string
  startsAt?: string
  endsAt?: string
  allDay: boolean
  cancelled: boolean
  organizer?: string
  participation?: string
  calendarId?: string
  eventId?: string
  attendees?: Attendee[]
}

const ANSWERS = [
  { name: 'accept', value: 'ACCEPTED' },
  { name: 'tentative', value: 'TENTATIVE' },
  { name: 'decline', value: 'DECLINED' },
] as const

export function InvitationCard({ itemId }: { itemId: string }) {
  const session = useSession()
  return (
    <InvitationCardForAccount
      key={`${session.userId ?? ''}:${itemId}`}
      ownerId={session.userId ?? ''}
      itemId={itemId}
    />
  )
}

function InvitationCardForAccount({ ownerId, itemId }: { ownerId: string; itemId: string }) {
  const { t } = useTranslation()
  const toast = useToast()
  const submission = useInvitationAnswer(ownerId, itemId)
  const [isRefreshing, setIsRefreshing] = useState(false)

  const invitation = useQuery(
    () => graphql<{ GetMailInvitation: Invitation | null }>(INVITATION, { itemId }),
    [itemId],
    { refresh: false },
  )
  const found = invitation.data?.GetMailInvitation ?? null

  const when = useMemo(() => {
    if (!found?.startsAt) return ''
    const starts = new Date(found.startsAt)
    const ends = found.endsAt ? new Date(found.endsAt) : null
    if (found.allDay) {
      // Read from the parts the server wrote rather than from what they
      // become here: a whole-day event is its date everywhere, and turning
      // it into a local time names the day before west of Greenwich.
      return new Date(starts.getUTCFullYear(), starts.getUTCMonth(), starts.getUTCDate(), 12).toLocaleDateString(
        undefined,
        { weekday: 'long', day: 'numeric', month: 'long' },
      )
    }
    const day = starts.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' })
    const from = starts.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
    const until = ends ? ends.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' }) : ''
    return until ? `${day}, ${from} – ${until}` : `${day}, ${from}`
  }, [found?.startsAt, found?.endsAt, found?.allDay])

  const refresh = async () => {
    setIsRefreshing(true)
    try {
      await invitation.reload()
    } catch (failure) {
      toast.failure(failure, t('invitation.refreshFailed'))
    } finally {
      setIsRefreshing(false)
    }
  }

  const answer = async (value: InvitationAnswer) => {
    if (isRefreshing) return
    try {
      await submission.send(value)
      toast.done(
        t(
          `invitation.said.${value === 'ACCEPTED' ? 'accepted' : value === 'DECLINED' ? 'declined' : 'tentative'}` as Parameters<
            typeof t
          >[0],
        ),
      )
    } catch (failure) {
      toast.failure(failure, t('invitation.failed'))
      return
    }
    await refresh()
  }

  const stop = async () => {
    if (isRefreshing) return
    try {
      const isCompleted = await submission.cancel()
      toast.done(t(isCompleted ? 'invitation.recorded' : 'invitation.stopped'))
    } catch (failure) {
      toast.failure(failure, t('invitation.failed'))
      return
    }
    await refresh()
  }

  const recovery = submission.pending && (
    <div className="invitation-standing">
      <p className="muted">{t('invitation.pending')}</p>
      <div className="page-actions">
        <button
          type="button"
          disabled={submission.isWorking || isRefreshing}
          onClick={() => void answer(submission.pending!.answer)}
        >
          {t('invitation.retry')}
        </button>
        <button type="button" disabled={submission.isWorking || isRefreshing} onClick={() => void stop()}>
          {t('invitation.stop')}
        </button>
      </div>
      <p className="muted">{t('calendar.stopHint')}</p>
    </div>
  )
  if (!found || !found.uid) return recovery ? <div className="invitation-card">{recovery}</div> : null

  // A reply or a cancellation is a line rather than a card: there is nothing
  // to decide, only something to know.
  if (found.method !== 'REQUEST') {
    return (
      <div className="invitation-note">
        {recovery}
        <span className="invitation-mark">{t('invitation.title')}</span>
        <span>
          {found.method === 'CANCEL' ? t('invitation.wasCancelled') : t('invitation.wasAnswered')}
          {found.summary ? ` — ${found.summary}` : ''}
        </span>
      </div>
    )
  }

  return (
    <div className={`invitation-card${found.cancelled ? ' cancelled' : ''}`}>
      <div className="invitation-head">
        <span className="invitation-mark">{t('invitation.title')}</span>
        {found.cancelled && <span className="invitation-off">{t('invitation.cancelled')}</span>}
      </div>
      <h4 className="invitation-summary">{found.summary || t('invitation.untitled')}</h4>
      <dl className="invitation-detail">
        {when && (
          <>
            <dt>{t('invitation.when')}</dt>
            <dd>{when}</dd>
          </>
        )}
        {found.location && (
          <>
            <dt>{t('invitation.where')}</dt>
            <dd>{found.location}</dd>
          </>
        )}
        {found.organizer && (
          <>
            <dt>{t('invitation.from')}</dt>
            <dd className="mono">{found.organizer}</dd>
          </>
        )}
        {(found.attendees ?? []).length > 0 && (
          <>
            <dt>{t('invitation.who')}</dt>
            <dd>
              {/* With what each of them said. Somebody deciding whether to go
                  wants to know who else is going, and the answer is already
                  here -- listing the names alone threw it away. */}
              <ul className="invitation-who">
                {(found.attendees ?? []).map((attendee) => (
                  <li key={attendee.address}>
                    {attendee.name || attendee.address}
                    {attendee.participation && attendee.participation !== 'NEEDS-ACTION' && (
                      <span className="muted">
                        {' '}
                        {t(
                          attendee.participation === 'ACCEPTED'
                            ? 'invitation.saidYes'
                            : attendee.participation === 'DECLINED'
                              ? 'invitation.saidNo'
                              : 'invitation.saidMaybe',
                        )}
                      </span>
                    )}
                  </li>
                ))}
              </ul>
            </dd>
          </>
        )}
      </dl>
      {!found.cancelled && (
        <div className="invitation-answers">
          {ANSWERS.map((choice) => (
            <button
              key={choice.name}
              type="button"
              className={found.participation === choice.value ? 'chosen' : undefined}
              disabled={submission.isWorking || isRefreshing || submission.pending !== null}
              onClick={() => void answer(choice.value)}
            >
              {t(`invitation.${choice.name}` as Parameters<typeof t>[0])}
            </button>
          ))}
        </div>
      )}
      {recovery}
      {/* What the person has already said, so the card is not silent about a
          decision they have made. */}
      {found.participation && found.participation !== 'NEEDS-ACTION' && (
        <p className="muted invitation-standing">
          {t(
            found.participation === 'ACCEPTED'
              ? 'invitation.standingAccepted'
              : found.participation === 'DECLINED'
                ? 'invitation.standingDeclined'
                : 'invitation.standingTentative',
          )}
        </p>
      )}
    </div>
  )
}
