import { useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { graphql } from '../../api'
import { ErrorMessage } from '../../components/common'
import { ConfirmDialog } from '../../components/dialog'
import { CloseIcon } from '../../components/icons'
import { personLabel, personName } from '../../components/personLabel'
import { RelativeTime } from '../../components/relativeTime'
import { Select } from '../../components/select'
import { SettingsEmpty, SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import {
  ERASE_HEALTH_DATA,
  HEALTH_CATEGORIES,
  HealthSettings as HealthSettingsView,
  HealthShare,
  READ_HEALTH_SETTINGS,
  SET_HEALTH_SHARE,
  UPDATE_HEALTH_SETTINGS,
  categoryName,
} from './healthApi'

// The Settings tab of the Health page: whether the agent may read Health data,
// who it is shared with and which categories, and erasing it all.

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
  return (
    <>
      <AgentSwitch />
      <HealthSharing userId={userId} shares={shares} onChanged={onSharesChanged} />
      <EraseEverything onErased={onErased} />
    </>
  )
}

function AgentSwitch() {
  const { t, plural } = useTranslation()
  const toast = useToast()
  const settings = useQuery(
    async () => (await graphql<{ ReadHealthSettings: HealthSettingsView }>(READ_HEALTH_SETTINGS)).ReadHealthSettings,
    [],
    { refresh: false },
  )
  const [isBusy, setIsBusy] = useState(false)
  const current = settings.data

  async function change(canAgentReadHealth: boolean) {
    setIsBusy(true)
    try {
      await graphql(UPDATE_HEALTH_SETTINGS, { canAgentReadHealth })
      toast.done(canAgentReadHealth ? t('health.agentTurnedOn') : t('health.agentTurnedOff'))
      await settings.reload()
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
      description={
        current?.modelProviderName
          ? t('health.agentProvider', { provider: current.modelProviderName })
          : t('health.agentProviderUnknown')
      }
    >
      <ErrorMessage error={settings.error} />
      <label className="checkbox is-plain">
        <input
          type="checkbox"
          checked={current?.canAgentReadHealth ?? false}
          disabled={isBusy || !current}
          onChange={(event) => void change(event.target.checked)}
        />
        <span>{t('health.agentSwitch')}</span>
      </label>
      {current && current.sampleCount > 0 && (
        <p className="muted">
          {plural(
            current.sampleCount,
            { one: 'health.readingCountOne', other: 'health.readingCount' },
            { count: current.sampleCount },
          )}
          {current.lastReceivedAt && (
            <>
              {' · '}
              {t('health.lastReceived')} <RelativeTime value={current.lastReceivedAt} />
            </>
          )}
        </p>
      )}
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
  const [choice, setChoice] = useState('')
  // People and circles added here and not yet given a category: they have no
  // share on the server until one is ticked, and the row stays while the last
  // one is unticked, so it can be ticked again.
  const [added, setAdded] = useState<Grantee[]>([])
  const [isBusy, setIsBusy] = useState(false)

  const people = choices.data?.people ?? []
  const circles = choices.data?.circles ?? []
  const own = shares.filter((share) => share.ownerUserId === userId)
  const sharedGrantees = own.map((share): Grantee =>
    share.granteeCircleId
      ? { kind: 'circle', id: share.granteeCircleId }
      : { kind: 'user', id: share.granteeUserId ?? '' },
  )
  const rows = [
    ...sharedGrantees,
    ...added.filter((grantee) => !sharedGrantees.some((each) => granteeKey(each) === granteeKey(grantee))),
  ]
  const categoriesOf = (grantee: Grantee): string[] =>
    own.find((share) => (grantee.kind === 'circle' ? share.granteeCircleId : share.granteeUserId) === grantee.id)
      ?.healthCategories ?? []
  const nameOf = (grantee: Grantee): string => {
    if (grantee.kind === 'circle') {
      const circleName =
        own.find((share) => share.granteeCircleId === grantee.id)?.granteeCircleName ??
        circles.find((circle) => circle.id === grantee.id)?.name ??
        ''
      return t('health.shareEveryoneIn', { name: circleName })
    }
    const person = people.find((each) => each.userId === grantee.id)
    return person
      ? personName(person)
      : (own.find((share) => share.granteeUserId === grantee.id)?.granteeUsername ?? '')
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
    setAdded((previous) =>
      previous.some((each) => granteeKey(each) === granteeKey(grantee)) ? previous : [...previous, grantee],
    )
    const next = isChecked
      ? HEALTH_CATEGORIES.filter((each) => each === healthCategory || current.includes(each))
      : current.filter((each) => each !== healthCategory)
    void save(grantee, next)
  }

  function add() {
    const [kind, id] = choice.split(':')
    if (!id || (kind !== 'user' && kind !== 'circle')) return
    setAdded((previous) => [...previous, { kind, id }])
    setChoice('')
  }

  const availableChoices = [
    ...people
      .filter((person) => !rows.some((row) => granteeKey(row) === `user:${person.userId}`))
      .map((person) => ({
        value: `user:${person.userId}`,
        label: personLabel(person),
        group: t('health.sharePeople'),
      })),
    ...circles
      .filter((circle) => !rows.some((row) => granteeKey(row) === `circle:${circle.id}`))
      .map((circle) => ({
        value: `circle:${circle.id}`,
        label: circle.name,
        group: t('health.shareCircles'),
      })),
  ]

  return (
    <SettingsSection card title={t('health.shareTitle')} description={t('health.shareIntro')}>
      <ErrorMessage error={choices.error} />
      {rows.length === 0 && <SettingsEmpty>{t('health.shareNobody')}</SettingsEmpty>}
      {rows.map((grantee) => {
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
                    setAdded((previous) => previous.filter((each) => granteeKey(each) !== granteeKey(grantee)))
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
                    disabled={isBusy}
                    onChange={(event) => toggle(grantee, healthCategory, event.target.checked)}
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
                value={choice}
                label={t('health.shareWith')}
                placeholder={t('health.shareChoose')}
                options={availableChoices}
                onChange={setChoice}
              />
            </label>
            <div className="page-actions">
              <button type="submit" disabled={isBusy || choice === ''}>
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
  const { t } = useTranslation()
  const toast = useToast()
  const [isConfirming, setIsConfirming] = useState(false)
  const [isBusy, setIsBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)

  async function erase() {
    setIsBusy(true)
    setProblem(null)
    try {
      const answer = await graphql<{ EraseHealthData: { erasedSampleCount: number } }>(ERASE_HEALTH_DATA, {})
      const count = answer.EraseHealthData.erasedSampleCount
      toast.done(count === 1 ? t('health.erasedOne') : t('health.erased', { count }))
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
