import { useState } from 'react'
import { Link } from 'react-router-dom'

import { graphql } from '../api'
import { Key, useTranslation } from '../i18n/i18n'
import { Tag } from './common'
import { CloseIcon } from './icons'
import { personLabel, personName } from './personLabel'
import { SettingsEmpty, SettingsRow, SettingsSection } from './settingsList'
import { useToast } from './toast'
import { Tooltip } from './tooltip'
import { useQuery } from './useQuery'

// Sharing where the person is: with one person in a circle they share, or with
// a whole circle, at "where I am now" or "where I am and where I have been".
// The same shape as sharing a calendar (calendarShare.tsx); the server refuses
// anybody the person does not share a circle with, so the choices are only the
// people and circles it would accept.

export const LOCATION_SHARE_LEVELS = ['current', 'history'] as const
export type LocationShareLevel = (typeof LOCATION_SHARE_LEVELS)[number]

type Share = {
  id: string
  granteeUserId: string | null
  granteeUsername: string
  granteeName: string
  granteeCircleId: string | null
  granteeCircleName: string
  shareLevel: LocationShareLevel
}

type Person = { userId: string; username: string; name: string }
type CircleChoice = { id: string; name: string; members: { userId: string }[] }

const LIST_SHARES = `query {
  ListLocationShares { id granteeUserId granteeUsername granteeName granteeCircleId granteeCircleName shareLevel }
}`
const LIST_PEOPLE = `query { ListCirclePeople { userId username name } }`
const LIST_CIRCLES = `query { ListCircles { id name members { userId } } }`
const SHARE = `mutation ($granteeUserId: String, $granteeCircleId: String, $shareLevel: String!) {
  ShareLocation(granteeUserId: $granteeUserId, granteeCircleId: $granteeCircleId, shareLevel: $shareLevel) { id }
}`
const UNSHARE = `mutation ($shareId: String!) { UnshareLocation(shareId: $shareId) }`

// LocationSharing is the Sharing tab of the Location page.
export function LocationSharing() {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const shares = useQuery(() => graphql<{ ListLocationShares: Share[] }>(LIST_SHARES), [], { refresh: false })
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
  const [shareLevel, setShareLevel] = useState<LocationShareLevel>('current')
  const [isBusy, setIsBusy] = useState(false)

  const current = shares.data?.ListLocationShares ?? []
  const people = choices.data?.people ?? []
  const circles = choices.data?.circles ?? []
  const everyoneIn = (name: string) => t('location.shareEveryoneIn', { name })
  const shareName = (share: Share) =>
    share.granteeCircleId ? everyoneIn(share.granteeCircleName) : share.granteeName || share.granteeUsername
  const circleOption = (circle: CircleChoice) =>
    t('location.shareCircleOption', {
      name: circle.name,
      people: plural(
        circle.members.length,
        { one: 'circles.memberCountOne', other: 'circles.memberCount' },
        { count: circle.members.length },
      ),
    })

  async function run(work: () => Promise<unknown>, done: string): Promise<boolean> {
    setIsBusy(true)
    try {
      await work()
      toast.done(done)
      await shares.reload()
      return true
    } catch (failure) {
      toast.failure(failure, t('location.shareFailed'))
      return false
    } finally {
      setIsBusy(false)
    }
  }

  function add() {
    const [kind, id] = choice.split(':')
    if (!id) return
    const person = people.find((each) => each.userId === id)
    const name =
      kind === 'circle'
        ? everyoneIn(circles.find((each) => each.id === id)?.name ?? '')
        : person
          ? personName(person)
          : ''
    void run(
      () =>
        graphql(SHARE, {
          granteeUserId: kind === 'user' ? id : null,
          granteeCircleId: kind === 'circle' ? id : null,
          shareLevel,
        }),
      t('location.shareSaved', { name }),
    ).then((isDone) => isDone && setChoice(''))
  }

  return (
    <SettingsSection card title={t('location.sharingTitle')} description={t('location.shareIntro')}>
      <h4>{t('location.shareCurrent')}</h4>
      {shares.data && current.length === 0 && <SettingsEmpty>{t('location.shareNobody')}</SettingsEmpty>}
      {current.map((share) => {
        const name = shareName(share)
        return (
          <SettingsRow
            key={share.id}
            title={name}
            badge={<Tag value={t(`location.shareLevelName.${share.shareLevel}` as Key)} />}
            actions={
              <Tooltip label={t('location.shareStop', { name })}>
                <button
                  type="button"
                  className="icon-action danger"
                  aria-label={t('location.shareStop', { name })}
                  disabled={isBusy}
                  onClick={() =>
                    void run(() => graphql(UNSHARE, { shareId: share.id }), t('location.shareStopped', { name }))
                  }
                >
                  <CloseIcon size={16} />
                </button>
              </Tooltip>
            }
          />
        )
      })}

      {choices.data && people.length === 0 && circles.length === 0 ? (
        <>
          <p className="muted">{t('location.shareNoPeople')}</p>
          <p>
            <Link to="/circles">{t('circles.open')}</Link>
          </p>
        </>
      ) : (
        <form
          className="form-narrow"
          onSubmit={(event) => {
            event.preventDefault()
            add()
          }}
        >
          <label>
            {t('location.shareWith')}
            <select value={choice} onChange={(event) => setChoice(event.target.value)}>
              <option value="">{t('location.shareChoose')}</option>
              {people.length > 0 && (
                <optgroup label={t('location.sharePeople')}>
                  {people.map((person) => (
                    <option key={person.userId} value={`user:${person.userId}`}>
                      {personLabel(person)}
                    </option>
                  ))}
                </optgroup>
              )}
              {circles.length > 0 && (
                <optgroup label={t('location.shareCircles')}>
                  {circles.map((circle) => (
                    <option key={circle.id} value={`circle:${circle.id}`}>
                      {circleOption(circle)}
                    </option>
                  ))}
                </optgroup>
              )}
            </select>
          </label>
          <label>
            {t('location.shareLevelLabel')}
            <select value={shareLevel} onChange={(event) => setShareLevel(event.target.value as LocationShareLevel)}>
              {LOCATION_SHARE_LEVELS.map((level) => (
                <option key={level} value={level}>
                  {t(`location.shareLevelName.${level}` as Key)}
                </option>
              ))}
            </select>
            <span className="muted">{t(`location.shareLevelHint.${shareLevel}` as Key)}</span>
          </label>
          <div className="page-actions">
            <button type="submit" className="primary" disabled={isBusy || choice === ''}>
              {t('location.shareAdd')}
            </button>
          </div>
        </form>
      )}
    </SettingsSection>
  )
}
