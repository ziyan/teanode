import { useMemo, useState } from 'react'
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
import { en } from '../../i18n/en'
import { Key, useTranslation } from '../../i18n/i18n'
import {
  ERASE_HEALTH_DATA,
  HEALTH_CATEGORIES,
  HealthCharacteristic,
  HealthSettingsResult,
  HealthShare,
  READ_HEALTH_SETTINGS,
  SET_HEALTH_SHARE,
  UPDATE_HEALTH_SETTINGS,
  categoryName,
  parseDay,
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
  const settings = useQuery(
    async () => (await graphql<{ ReadHealthSettings: HealthSettingsResult }>(READ_HEALTH_SETTINGS)).ReadHealthSettings,
    [],
    { refresh: false },
  )
  return (
    <>
      <ErrorMessage error={settings.error} />
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
    <SettingsSection
      card
      title={t('health.settingsAgentTitle')}
      description={`${t('health.agentOwnOnly')} ${
        settings?.modelProviderName
          ? t('health.agentProvider', { provider: settings.modelProviderName })
          : t('health.agentProviderUnknown')
      }`}
    >
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

// The facts about the person that the Health app holds and the iPhone copies,
// in the order they are listed. The others it sends (skin type, wheelchair
// use, how Activity is measured) are kept and not shown.
const SHOWN_CHARACTERISTICS = ['dateOfBirth', 'biologicalSex', 'bloodType'] as const

// bloodTypeText writes a blood type as it is on a card: "aPositive" is "A+".
function bloodTypeText(characteristicValue: string): string {
  const match = /^(a|b|ab|o)(Positive|Negative)$/.exec(characteristicValue)
  return match ? `${match[1].toUpperCase()}${match[2] === 'Positive' ? '+' : '-'}` : characteristicValue
}

// AboutYou shows the facts Health holds about the person, read only: they are
// set in the Health app, and changed there.
function AboutYou({ characteristics }: { characteristics: HealthCharacteristic[] }) {
  const { t, language } = useTranslation()
  const dayFormat = useMemo(() => new Intl.DateTimeFormat(language, { dateStyle: 'long' }), [language])
  const shown = SHOWN_CHARACTERISTICS.flatMap((healthType) => {
    const found = characteristics.find((each) => each.healthType === healthType)
    if (!found) return []
    const characteristicValue = found.characteristicValue
    const sexKey = `health.sex.${characteristicValue}`
    const text =
      healthType === 'dateOfBirth'
        ? dayFormat.format(parseDay(characteristicValue))
        : healthType === 'biologicalSex'
          ? sexKey in en
            ? t(sexKey as Key)
            : characteristicValue
          : bloodTypeText(characteristicValue)
    return [{ healthType, text }]
  })
  if (shown.length === 0) return null
  return (
    <SettingsSection card title={t('health.aboutTitle')} description={t('health.aboutIntro')}>
      <dl className="health-numbers">
        {shown.map((each) => (
          <div key={each.healthType}>
            <dt className="muted">{t(`health.characteristic.${each.healthType}` as Key)}</dt>
            <dd>{each.text}</dd>
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
  const [chosenGrantee, setChosenGrantee] = useState('')
  // People and circles added here and not yet given a category: they have no
  // share on the server until one is ticked, and the row stays while the last
  // one is unticked, so it can be ticked again.
  const [addedGrantees, setAddedGrantees] = useState<Grantee[]>([])
  const [isBusy, setIsBusy] = useState(false)

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
    ...addedGrantees.filter((grantee) => !sharedGrantees.some((each) => granteeKey(each) === granteeKey(grantee))),
  ]
  const categoriesOf = (grantee: Grantee): string[] =>
    ownShares.find((share) => (grantee.kind === 'circle' ? share.granteeCircleId : share.granteeUserId) === grantee.id)
      ?.healthCategories ?? []
  const nameOf = (grantee: Grantee): string => {
    if (grantee.kind === 'circle') {
      const circleName =
        ownShares.find((share) => share.granteeCircleId === grantee.id)?.granteeCircleName ??
        circles.find((circle) => circle.id === grantee.id)?.name ??
        ''
      return t('health.shareEveryoneIn', { name: circleName })
    }
    const person = people.find((each) => each.userId === grantee.id)
    return person
      ? personName(person)
      : (ownShares.find((share) => share.granteeUserId === grantee.id)?.granteeUsername ?? '')
  }

  async function save(grantee: Grantee, healthCategories: string[]) {
    setIsBusy(true)
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
      setIsBusy(false)
    }
  }

  function toggle(grantee: Grantee, healthCategory: string, isChecked: boolean) {
    const current = categoriesOf(grantee)
    setAddedGrantees((previous) =>
      previous.some((each) => granteeKey(each) === granteeKey(grantee)) ? previous : [...previous, grantee],
    )
    const next = isChecked
      ? HEALTH_CATEGORIES.filter((each) => each === healthCategory || current.includes(each))
      : current.filter((each) => each !== healthCategory)
    void save(grantee, next)
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
        const name = nameOf(grantee)
        const categories = categoriesOf(grantee)
        return (
          <fieldset key={granteeKey(grantee)} className="health-share">
            <legend className="health-share-name">
              <strong>{name}</strong>
            </legend>
            <div className="health-share-head">
              <span className="muted" id={`health-share-${granteeKey(grantee)}`}>
                {t('health.shareCategories', { name })}
              </span>
              <Tooltip label={t('health.shareStop', { name })}>
                <button
                  type="button"
                  className="icon-action danger"
                  aria-label={t('health.shareStop', { name })}
                  disabled={isBusy}
                  onClick={() => {
                    setAddedGrantees((previous) => previous.filter((each) => granteeKey(each) !== granteeKey(grantee)))
                    if (categories.length > 0) void save(grantee, [])
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
                    aria-disabled={isBusy}
                    onChange={(event) => {
                      // Not disabled while a save runs: a disabled box loses
                      // keyboard focus, and the next one is a Tab away.
                      if (!isBusy) toggle(grantee, healthCategory, event.target.checked)
                    }}
                  />
                  <span>{categoryName(t, healthCategory)}</span>
                </label>
              ))}
            </div>
          </fieldset>
        )
      })}

      {choices.data && people.length === 0 && circles.length === 0 ? (
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
