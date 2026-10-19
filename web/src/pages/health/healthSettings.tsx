import { useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { graphql } from '../../api'
import { ErrorMessage } from '../../components/common'
import { ConfirmDialog } from '../../components/dialog'
import { CloseIcon } from '../../components/icons'
import { personLabel, personName } from '../../components/personLabel'
import { RelativeTime } from '../../components/relativeTime'
import { Select } from '../../components/select'
import { SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { hasPermission, useSession } from '../../session'
import {
  CHARACTERISTIC_TYPES,
  ERASE_HEALTH_DATA,
  HEALTH_CATEGORIES,
  HealthCharacteristic,
  HealthSettingsResult,
  HealthShare,
  READ_HEALTH_SETTINGS,
  SET_HEALTH_SHARE,
  UPDATE_HEALTH_SETTINGS,
  categoryName,
  characteristicText,
  typeName,
  wholeNumberText,
} from './healthApi'

// The Settings tab of the Health page: what is kept, whether the agent may
// read it, the facts about the person that Health holds, who it is shared
// with and which categories, and erasing it all. Everything here is the
// person's own.

type Person = { userId: string; username: string; name: string }
type CircleChoice = { id: string; name: string; members: { userId: string }[] }

const LIST_PEOPLE = `query { ListCirclePeople { userId username name } }`
const LIST_CIRCLES = `query { ListCircles { id name members { userId } } }`

export function HealthSettings({
  userId,
  shares,
  onSharesChanged,
  onErased,
}: {
  userId: string
  shares: HealthShare[]
  onSharesChanged: () => Promise<void>
  onErased: () => void
}) {
  const { t } = useTranslation()
  const settings = useQuery(
    async () => (await graphql<{ ReadHealthSettings: HealthSettingsResult }>(READ_HEALTH_SETTINGS)).ReadHealthSettings,
    [],
    { refresh: false },
  )
  return (
    <>
      <ErrorMessage error={settings.error} />
      {settings.error !== null && settings.data === null && !settings.loading && (
        <div className="page-actions">
          <button type="button" onClick={() => void settings.reload()}>
            {t('common.tryAgain')}
          </button>
        </div>
      )}
      <HealthStatus settings={settings.data} />
      <AgentSwitch settings={settings.data} onChanged={settings.reload} />
      <AboutYou characteristics={settings.data?.characteristics ?? []} />
      <HealthSharing userId={userId} shares={shares} onChanged={onSharesChanged} />
      <EraseEverything
        onErased={() => {
          // What is kept is read again, so the numbers above say it is gone.
          void settings.reload()
          onErased()
        }}
      />
    </>
  )
}

// HealthStatus is what the server keeps and when it last heard from the phone.
function HealthStatus({ settings }: { settings: HealthSettingsResult | null }) {
  const { t, plural, language } = useTranslation()
  if (!settings) return null
  return (
    <>
      <p className="muted health-status">
        {settings.sampleCount === 0
          ? t('health.readingCountNone')
          : plural(
              settings.sampleCount,
              { one: 'health.readingCountOne', other: 'health.readingCount' },
              { count: wholeNumberText(settings.sampleCount, language) },
            )}
        {settings.lastReceivedAt && (
          <>
            {' · '}
            {t('health.lastReceived')} <RelativeTime value={settings.lastReceivedAt} />
          </>
        )}
      </p>
      {settings.sampleCount === 0 && <p className="muted">{t('health.settingsNoData')}</p>}
    </>
  )
}

function AgentSwitch({
  settings,
  onChanged,
}: {
  settings: HealthSettingsResult | null
  onChanged: () => Promise<void>
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const [isBusy, setIsBusy] = useState(false)

  async function change(canAgentReadHealth: boolean) {
    // The box stays enabled while this runs, so keyboard focus is not lost
    // from it; a second change waits for the first.
    if (isBusy) return
    setIsBusy(true)
    try {
      await graphql(UPDATE_HEALTH_SETTINGS, { canAgentReadHealth })
      toast.done(canAgentReadHealth ? t('health.agentTurnedOn') : t('health.agentTurnedOff'))
      await onChanged()
    } catch (failure) {
      toast.failure(failure, t('health.settingsFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  return (
    <SettingsSection card title={t('health.settingsAgentTitle')} description={t('health.agentOwnOnly')}>
      <p className="muted">
        {settings?.modelProviderName
          ? t('health.agentProvider', { provider: settings.modelProviderName })
          : t('health.agentProviderUnknown')}
      </p>
      <label className="checkbox is-plain">
        <input
          type="checkbox"
          checked={settings?.canAgentReadHealth ?? false}
          disabled={!settings}
          aria-busy={isBusy}
          onChange={(event) => void change(event.target.checked)}
        />
        <span>{t('health.agentSwitch')}</span>
      </label>
    </SettingsSection>
  )
}

// AboutYou shows the facts Health holds about the person, read only: they are
// set in the Health app, and changed there. The words are the table at the top
// of internal/health/words.go.
function AboutYou({ characteristics }: { characteristics: HealthCharacteristic[] }) {
  const { t, language } = useTranslation()
  const dayFormat = useMemo(() => new Intl.DateTimeFormat(language, { dateStyle: 'long' }), [language])
  const shown = CHARACTERISTIC_TYPES.flatMap((healthType) => {
    const found = characteristics.find((characteristic) => characteristic.healthType === healthType)
    if (!found) return []
    return [
      {
        healthType,
        text: characteristicText(t, healthType, found.characteristicValue, new Date(), (day) => dayFormat.format(day)),
      },
    ]
  })
  if (shown.length === 0) return null
  return (
    <SettingsSection card title={t('health.aboutTitle')} description={t('health.aboutIntro')}>
      <dl className="health-numbers">
        {shown.map((fact) => (
          <div key={fact.healthType}>
            <dt className="muted">{typeName(t, fact.healthType, '')}</dt>
            <dd>{fact.text}</dd>
          </div>
        ))}
      </dl>
    </SettingsSection>
  )
}

// Who a share is with, as the choice in the list and the key of its row.
type Grantee = { kind: 'user' | 'circle'; id: string }
const granteeKey = (grantee: Grantee) => `${grantee.kind}:${grantee.id}`

function HealthSharing({
  userId,
  shares,
  onChanged,
}: {
  userId: string
  shares: HealthShare[]
  onChanged: () => Promise<void>
}) {
  const { t } = useTranslation()
  const toast = useToast()
  const navigate = useNavigate()
  const session = useSession()
  // Who can be shared with is found through circles, which a person without
  // circle:use cannot read: they get no list and no failure, and the shares
  // they already have are shown as they are.
  const canUseCircles = hasPermission(session.permissions, 'circle:use')
  const choices = useQuery(
    async () => {
      if (!canUseCircles) return { people: [] as Person[], circles: [] as CircleChoice[] }
      const [people, circles] = await Promise.all([
        graphql<{ ListCirclePeople: Person[] }>(LIST_PEOPLE),
        graphql<{ ListCircles: CircleChoice[] }>(LIST_CIRCLES),
      ])
      return { people: people.ListCirclePeople, circles: circles.ListCircles }
    },
    [canUseCircles],
    { refresh: false },
  )
  const [chosenGrantee, setChosenGrantee] = useState('')
  // People and circles added here and not yet given a category: they have no
  // share on the server until one is ticked, and the row stays while the last
  // one is unticked, so it can be ticked again.
  const [addedGrantees, setAddedGrantees] = useState<Grantee[]>([])
  // What was ticked and is not saved yet, by grantee: the boxes show it at
  // once, and go back to what the server has if the save fails. Changes are
  // saved one after the other, in the order they were made.
  const [pendingCategories, setPendingCategories] = useState<Record<string, string[]>>({})
  const [savingCount, setSavingCount] = useState(0)
  const saveQueue = useRef<Promise<void>>(Promise.resolve())
  const latestChangeNumbers = useRef<Record<string, number>>({})
  const isBusy = savingCount > 0

  const people = choices.data?.people ?? []
  const circles = choices.data?.circles ?? []
  const ownShares = shares.filter((share) => share.ownerUserId === userId)
  const sharedGrantees = ownShares.map((share): Grantee =>
    share.granteeCircleId
      ? { kind: 'circle', id: share.granteeCircleId }
      : { kind: 'user', id: share.granteeUserId ?? '' },
  )
  const granteeRows = [
    ...sharedGrantees,
    ...addedGrantees.filter((grantee) => !sharedGrantees.some((shared) => granteeKey(shared) === granteeKey(grantee))),
  ]
  const categoriesOf = (grantee: Grantee): string[] =>
    pendingCategories[granteeKey(grantee)] ??
    ownShares.find((share) => (grantee.kind === 'circle' ? share.granteeCircleId : share.granteeUserId) === grantee.id)
      ?.healthCategories ??
    []
  const nameOf = (grantee: Grantee): string => {
    if (grantee.kind === 'circle') {
      const circleName =
        ownShares.find((share) => share.granteeCircleId === grantee.id)?.granteeCircleName ??
        circles.find((circle) => circle.id === grantee.id)?.name ??
        ''
      return t('health.shareEveryoneIn', { name: circleName })
    }
    const person = people.find((candidate) => candidate.userId === grantee.id)
    return person
      ? personName(person)
      : (ownShares.find((share) => share.granteeUserId === grantee.id)?.granteeUsername ?? '')
  }

  async function save(grantee: Grantee, healthCategories: string[], changeNumber: number) {
    setSavingCount((previous) => previous + 1)
    try {
      await graphql(SET_HEALTH_SHARE, {
        granteeUserId: grantee.kind === 'user' ? grantee.id : null,
        granteeCircleId: grantee.kind === 'circle' ? grantee.id : null,
        healthCategories,
      })
      toast.done(
        healthCategories.length > 0
          ? t('health.shareSaved', { name: nameOf(grantee) })
          : t('health.shareStopped', { name: nameOf(grantee) }),
      )
      await onChanged()
    } catch (failure) {
      toast.failure(failure, t('health.shareFailed'))
    } finally {
      setSavingCount((previous) => previous - 1)
      // The last change made is the one the boxes were showing; once it is
      // saved (or has failed) they show what the server has.
      if (latestChangeNumbers.current[granteeKey(grantee)] === changeNumber) {
        setPendingCategories((previous) => {
          const remaining = { ...previous }
          delete remaining[granteeKey(grantee)]
          return remaining
        })
      }
    }
  }

  // change shows the new categories at once and saves them after the changes
  // made before it.
  function change(grantee: Grantee, healthCategories: string[]) {
    const key = granteeKey(grantee)
    const changeNumber = (latestChangeNumbers.current[key] ?? 0) + 1
    latestChangeNumbers.current[key] = changeNumber
    setPendingCategories((previous) => ({ ...previous, [key]: healthCategories }))
    saveQueue.current = saveQueue.current.then(() => save(grantee, healthCategories, changeNumber))
  }

  function toggle(grantee: Grantee, healthCategory: string, isChecked: boolean) {
    const current = categoriesOf(grantee)
    setAddedGrantees((previous) =>
      previous.some((added) => granteeKey(added) === granteeKey(grantee)) ? previous : [...previous, grantee],
    )
    change(
      grantee,
      isChecked
        ? HEALTH_CATEGORIES.filter((category) => category === healthCategory || current.includes(category))
        : current.filter((category) => category !== healthCategory),
    )
  }

  function add() {
    const [kind, id] = chosenGrantee.split(':')
    if (!id || (kind !== 'user' && kind !== 'circle')) return
    setAddedGrantees((previous) => [...previous, { kind, id }])
    setChosenGrantee('')
  }

  const availableChoices = [
    ...people
      .filter((person) => !granteeRows.some((row) => granteeKey(row) === `user:${person.userId}`))
      .map((person) => ({
        value: `user:${person.userId}`,
        label: personLabel(person),
        group: t('health.sharePeople'),
      })),
    ...circles
      .filter((circle) => !granteeRows.some((row) => granteeKey(row) === `circle:${circle.id}`))
      .map((circle) => ({
        value: `circle:${circle.id}`,
        label: circle.name,
        group: t('health.shareCircles'),
      })),
  ]

  return (
    <SettingsSection card title={t('health.shareTitle')} description={t('health.shareIntro')}>
      <ErrorMessage error={choices.error} />
      {granteeRows.length === 0 && <p className="muted">{t('health.shareNobody')}</p>}
      {granteeRows.map((grantee) => {
        const granteeName = nameOf(grantee)
        const categories = categoriesOf(grantee)
        return (
          <fieldset key={granteeKey(grantee)} className="health-share">
            <legend className="health-share-name">
              <strong>{granteeName}</strong>
            </legend>
            <div className="health-share-head">
              <span className="muted" id={`health-share-${granteeKey(grantee)}`}>
                {t('health.shareCategories', { name: granteeName })}
              </span>
              <Tooltip label={t('health.shareStop', { name: granteeName })}>
                <button
                  type="button"
                  className="icon-action danger"
                  aria-label={t('health.shareStop', { name: granteeName })}
                  onClick={() => {
                    setAddedGrantees((previous) =>
                      previous.filter((added) => granteeKey(added) !== granteeKey(grantee)),
                    )
                    if (categories.length > 0) change(grantee, [])
                  }}
                >
                  <CloseIcon size={16} />
                </button>
              </Tooltip>
            </div>
            <div
              className="health-share-categories"
              role="group"
              aria-labelledby={`health-share-${granteeKey(grantee)}`}
            >
              {HEALTH_CATEGORIES.map((healthCategory) => (
                <label key={healthCategory} className="checkbox is-plain">
                  <input
                    type="checkbox"
                    checked={categories.includes(healthCategory)}
                    onChange={(event) => toggle(grantee, healthCategory, event.target.checked)}
                  />
                  <span>{categoryName(t, healthCategory)}</span>
                </label>
              ))}
            </div>
          </fieldset>
        )
      })}

      {!canUseCircles ? (
        <p className="muted">{t('health.shareNeedsCircles')}</p>
      ) : choices.data && people.length === 0 && circles.length === 0 ? (
        <>
          <p className="muted">{t('health.shareNoPeople')}</p>
          <div className="page-actions">
            <button type="button" onClick={() => navigate('/circles')}>
              {t('circles.open')}
            </button>
          </div>
        </>
      ) : (
        availableChoices.length > 0 && (
          <form
            className="form-narrow"
            onSubmit={(event) => {
              event.preventDefault()
              add()
            }}
          >
            <label>
              <span>{t('health.shareWith')}</span>
              <Select
                block
                value={chosenGrantee}
                label={t('health.shareWith')}
                placeholder={t('health.shareChoose')}
                options={availableChoices}
                onChange={setChosenGrantee}
              />
            </label>
            <div className="page-actions">
              <button type="submit" disabled={isBusy || chosenGrantee === ''}>
                {t('health.shareAdd')}
              </button>
            </div>
          </form>
        )
      )}
    </SettingsSection>
  )
}

function EraseEverything({ onErased }: { onErased: () => void }) {
  const { t, plural, language } = useTranslation()
  const toast = useToast()
  const [isConfirming, setIsConfirming] = useState(false)
  const [isBusy, setIsBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

  async function erase() {
    setIsBusy(true)
    setProblem(null)
    try {
      const answer = await graphql<{ EraseHealthData: { erasedSampleCount: number } }>(ERASE_HEALTH_DATA, {})
      const erasedSampleCount = answer.EraseHealthData.erasedSampleCount
      toast.done(
        plural(
          erasedSampleCount,
          { one: 'health.erasedOne', other: 'health.erased' },
          {
            count: wholeNumberText(erasedSampleCount, language),
          },
        ),
      )
      setIsConfirming(false)
      onErased()
    } catch (failure) {
      setProblem(failure instanceof Error && failure.message ? failure.message : t('health.eraseFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  return (
    <SettingsSection card title={t('health.eraseTitle')}>
      <div className="page-actions">
        <button
          type="button"
          className="danger"
          onClick={() => {
            setProblem(null)
            setIsConfirming(true)
          }}
        >
          {t('health.eraseAll')}
        </button>
      </div>
      {isConfirming && (
        <ConfirmDialog
          title={t('health.eraseAllTitle')}
          body={t('health.eraseAllBody')}
          confirmLabel={t('health.eraseConfirm')}
          busy={isBusy}
          error={problem}
          onClose={() => setIsConfirming(false)}
          onConfirm={() => void erase()}
        />
      )}
    </SettingsSection>
  )
}
