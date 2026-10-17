import { useState } from 'react'

import { graphql } from '../api'
import { Tag } from './common'
import { FormDialog } from './dialog'
import { CloseIcon } from './icons'
import { useToast } from './toast'
import { Tooltip } from './tooltip'
import { useQuery } from './useQuery'
import { useTranslation } from '../i18n/i18n'

// Sharing the person's own calendar: with one person in a circle they share, or
// with a whole circle, at a level. The levels, and why there are four, are in
// docs/decisions/20261016-a-calendar-is-shared-at-a-level.md; the server
// refuses anybody the person does not share a circle with, and answers as not
// found, so the choices here are only the people and circles it would accept.

export const SHARE_LEVELS = ['busy', 'titles', 'details', 'edit'] as const
export type GrantableLevel = (typeof SHARE_LEVELS)[number]

type Share = {
  id: string
  granteeUserId: string | null
  granteeUsername: string
  granteeName: string
  granteeCircleId: string | null
  granteeCircleName: string
  shareLevel: GrantableLevel
}

type Person = { userId: string; username: string; name: string; avatarUrl: string | null; avatarColorIndex: number }
type CircleChoice = { id: string; name: string }

const LIST_SHARES = `query ($calendarId: String!) {
  ListCalendarShares(calendarId: $calendarId) {
    id granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName shareLevel
  }
}`
const LIST_PEOPLE = `query { ListCirclePeople { userId username name avatarUrl avatarColorIndex } }`
const LIST_CIRCLES = `query { ListCircles { id name } }`
const SHARE = `mutation ($calendarId: String!, $granteeUserId: String, $granteeCircleId: String, $shareLevel: String!) {
  ShareCalendar(calendarId: $calendarId, granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, shareLevel: $shareLevel) { id }
}`
const UNSHARE = `mutation ($shareId: String!) { UnshareCalendar(shareId: $shareId) }`

function shareName(share: Share, everyoneIn: (name: string) => string): string {
  if (share.granteeCircleId) return everyoneIn(share.granteeCircleName)
  return share.granteeName || share.granteeUsername
}

// CalendarShareDialog lists who the calendar is shared with, at what level,
// with a way to stop, and a form to share it with somebody else.
export function CalendarShareDialog({
  calendar,
  onClose,
  initialLevel = 'titles',
}: {
  calendar: { id: string; name: string }
  onClose: () => void
  initialLevel?: GrantableLevel
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const shares = useQuery(
    () => graphql<{ ListCalendarShares: Share[] }>(LIST_SHARES, { calendarId: calendar.id }),
    [calendar.id],
    { refresh: false },
  )
  const choices = useQuery(
    async () => {
      const [people, circles] = await Promise.all([
        graphql<{ ListCirclePeople: Person[] }>(LIST_PEOPLE),
        graphql<{ ListCircles: CircleChoice[] }>(LIST_CIRCLES),
      ])
      return { people: people.ListCirclePeople, circles: circles.ListCircles }
    },
    [],
    { refresh: false },
  )
  const [choice, setChoice] = useState('')
  const [shareLevel, setShareLevel] = useState<GrantableLevel>(initialLevel)
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

  const current = shares.data?.ListCalendarShares ?? []
  const people = choices.data?.people ?? []
  const circles = choices.data?.circles ?? []
  const everyoneIn = (name: string) => t('calendar.shareEveryoneIn', { name })

  async function run(work: () => Promise<unknown>, done: string): Promise<boolean> {
    setBusy(true)
    setProblem(null)
    try {
      await work()
      toast.done(done)
      await shares.reload()
      return true
    } catch (failure) {
      setProblem(failure instanceof Error ? failure.message : String(failure))
      toast.failure(failure, t('calendar.failed'))
      return false
    } finally {
      setBusy(false)
    }
  }

  function add() {
    const [kind, id] = choice.split(':')
    if (!id) return
    const name =
      kind === 'circle'
        ? everyoneIn(circles.find((each) => each.id === id)?.name ?? '')
        : ((people.find((each) => each.userId === id)?.name || people.find((each) => each.userId === id)?.username) ??
          '')
    void run(
      () =>
        graphql(SHARE, {
          calendarId: calendar.id,
          granteeUserId: kind === 'user' ? id : null,
          granteeCircleId: kind === 'circle' ? id : null,
          shareLevel,
        }),
      t('calendar.shareSaid', { name }),
    ).then((isDone) => isDone && setChoice(''))
  }

  return (
    <FormDialog
      title={t('calendar.shareTitle')}
      submitLabel={t('calendar.shareAdd')}
      closeLabel={t('common.done')}
      busy={busy}
      error={problem}
      canSubmit={choice !== ''}
      onClose={onClose}
      onSubmit={add}
    >
      <p className="muted">{t('calendar.shareIntro')}</p>

      <div className="calendar-shares" aria-label={t('calendar.shareCurrent')}>
        {shares.data && current.length === 0 && <p className="muted">{t('calendar.shareNobody')}</p>}
        {current.map((share) => {
          const name = shareName(share, everyoneIn)
          return (
            <div key={share.id} className="calendar-share-row">
              <span className="calendar-share-name">{name}</span>
              <Tag value={t(`calendar.shareLevelName.${share.shareLevel}` as Parameters<typeof t>[0])} />
              <Tooltip label={t('calendar.shareStop', { name })}>
                <button
                  type="button"
                  className="icon-action danger"
                  aria-label={t('calendar.shareStop', { name })}
                  disabled={busy}
                  onClick={() =>
                    void run(() => graphql(UNSHARE, { shareId: share.id }), t('calendar.shareStopped', { name }))
                  }
                >
                  <CloseIcon size={16} />
                </button>
              </Tooltip>
            </div>
          )
        })}
      </div>

      {choices.data && people.length === 0 && circles.length === 0 ? (
        <p className="muted">{t('calendar.shareNoPeople')}</p>
      ) : (
        <>
          <label>
            {t('calendar.shareWith')}
            <select value={choice} onChange={(event) => setChoice(event.target.value)}>
              <option value="">{t('calendar.shareChoose')}</option>
              {people.length > 0 && (
                <optgroup label={t('calendar.sharePeople')}>
                  {people.map((person) => (
                    <option key={person.userId} value={`user:${person.userId}`}>
                      {person.name || person.username}
                    </option>
                  ))}
                </optgroup>
              )}
              {circles.length > 0 && (
                <optgroup label={t('calendar.shareCircles')}>
                  {circles.map((circle) => (
                    <option key={circle.id} value={`circle:${circle.id}`}>
                      {everyoneIn(circle.name)}
                    </option>
                  ))}
                </optgroup>
              )}
            </select>
          </label>
          <label>
            {t('calendar.shareLevelLabel')}
            <select value={shareLevel} onChange={(event) => setShareLevel(event.target.value as GrantableLevel)}>
              {SHARE_LEVELS.map((level) => (
                <option key={level} value={level}>
                  {t(`calendar.shareLevelName.${level}` as Parameters<typeof t>[0])}
                </option>
              ))}
            </select>
            <span className="muted">{t(`calendar.shareLevelHint.${shareLevel}` as Parameters<typeof t>[0])}</span>
          </label>
        </>
      )}
    </FormDialog>
  )
}
