import { useEffect, useMemo, useRef } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

import { graphql } from '../../api'
import { ErrorMessage, Loading } from '../../components/common'
import { ArrowLeftIcon } from '../../components/icons'
import { Select } from '../../components/select'
import { SettingsEmpty, SettingsSection } from '../../components/settingsList'
import { Tabs } from '../../components/tabs'
import { useToast } from '../../components/toast'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import { HEALTH_TABS } from '../../pageTabs'
import { hasPermission, useSession } from '../../session'
import { HealthTypePanel } from './healthChart'
import {
  DEFAULT_RANGE,
  HealthShare,
  HealthType,
  LIST_HEALTH_SHARES,
  LIST_HEALTH_TYPES,
  PeriodID,
  RangeID,
  dayText,
  readPeriod,
  readRange,
} from './healthApi'
import { HealthSamples } from './healthSamples'
import { HealthSettings } from './healthSettings'
import { HealthTypeList, groupByCategory } from './healthTypes'

// The person's Apple Health data, copied to their server by the iPhone app:
// the kinds of data they have, grouped as the Health app groups them, a chart
// of the one chosen, its recent readings, and in Settings whether the agent
// may read it, who it is shared with, and erasing it. Somebody who shares
// Health with the person can be chosen at the top, and then only the kinds and
// charts they share are shown.
//
// In the address: tab (Settings), person, type, range, period, route (the
// workout whose route is open), and the page of readings.

type Sharer = { userId: string; name: string }

export function HealthPage() {
  const session = useSession()
  const { t } = useTranslation()
  if (!hasPermission(session.permissions, 'health:use')) {
    return (
      <SettingsSection card title={t('health.title')}>
        <SettingsEmpty>{t('health.off')}</SettingsEmpty>
      </SettingsSection>
    )
  }
  return <HealthContent key={session.userId ?? ''} userId={session.userId ?? ''} />
}

type AnsweredTypes = { requestKey: string; healthTypes: HealthType[] }

function HealthContent({ userId }: { userId: string }) {
  const { t } = useTranslation()
  const toast = useToast()
  const [parameters, setParameters] = useSearchParams()

  function changeParameters(changes: Record<string, string | null>, options: { replace?: boolean } = {}) {
    setParameters(
      (previous) => {
        const written = new URLSearchParams(previous)
        for (const [parameterName, parameterValue] of Object.entries(changes)) {
          if (parameterValue === null) written.delete(parameterName)
          else written.set(parameterName, parameterValue)
        }
        return written
      },
      { replace: options.replace },
    )
  }

  const shares = useQuery(
    async () => (await graphql<{ ListHealthShares: HealthShare[] | null }>(LIST_HEALTH_SHARES)).ListHealthShares ?? [],
    [userId],
    { refresh: false },
  )

  // The people who share Health with this person, once each, however many
  // circles or shares reach them.
  const sharers = useMemo(() => {
    const found = new Map<string, Sharer>()
    for (const share of shares.data ?? []) {
      if (share.ownerUserId !== userId && !found.has(share.ownerUserId)) {
        found.set(share.ownerUserId, { userId: share.ownerUserId, name: share.ownerName || share.ownerUsername })
      }
    }
    return [...found.values()].sort((first, second) => first.name.localeCompare(second.name))
  }, [shares.data, userId])

  const requestedPerson = parameters.get('person')
  const viewedPerson = sharers.find((sharer) => sharer.userId === requestedPerson) ?? null
  const ownerUserId = viewedPerson?.userId ?? null
  // A failed read of the shares does not leave the page waiting for them: the
  // link is taken out of the address as a stale one is.
  const isSharesFailed = shares.error !== null && shares.data === null
  const isPersonPending = requestedPerson !== null && shares.data === null && !isSharesFailed
  // A link to somebody who is not sharing (or stopped) is taken out of the
  // address once the shares are read, and the person is told.
  const isPersonGone = requestedPerson !== null && (shares.data !== null || isSharesFailed) && viewedPerson === null
  useEffect(() => {
    if (!isPersonGone) return
    toast.failed(t('health.personNotSharing'))
    changeParameters({ person: null, type: null, route: null, page: null }, { replace: true })
    // Once per link found stale.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isPersonGone, requestedPerson])

  const tab = ownerUserId === null && parameters.get('tab') === 'settings' ? 'settings' : 'data'
  const range: RangeID = readRange(parameters.get('range'))
  const period: PeriodID = readPeriod(parameters.get('period'), range)
  const today = dayText(new Date())

  const typesRequestKey = JSON.stringify([ownerUserId])
  const typesQuery = useQuery(
    async (): Promise<AnsweredTypes | null> =>
      isPersonPending
        ? null
        : {
            requestKey: typesRequestKey,
            healthTypes:
              (
                await graphql<{ ListHealthTypes: HealthType[] | null }>(LIST_HEALTH_TYPES, {
                  ownerUserId,
                })
              ).ListHealthTypes ?? [],
          },
    [typesRequestKey, isPersonPending, userId],
  )
  const typeList = useMemo(
    () => (typesQuery.data && typesQuery.data.requestKey === typesRequestKey ? typesQuery.data.healthTypes : null),
    [typesQuery.data, typesRequestKey],
  )

  const requestedType = parameters.get('type')
  const chosenFromAddress = typeList?.find((listedType) => listedType.healthType === requestedType) ?? null
  // With no type named, the first one in the list is shown (on a phone, the
  // list is shown instead until one is chosen).
  const chosenType = chosenFromAddress ?? (typeList ? (groupByCategory(typeList)[0]?.healthTypes[0] ?? null) : null)

  const sampleRouteId = ownerUserId === null ? parameters.get('route') : null

  // On a phone the list is hidden while a type is open; Back shows it again,
  // and focus goes to the row that was open instead of being lost.
  const typeToFocus = useRef<string | null>(null)
  useEffect(() => {
    if (chosenFromAddress || typeToFocus.current === null) return
    const row = [...document.querySelectorAll<HTMLElement>('[data-health-type]')].find(
      (candidate) => candidate.dataset.healthType === typeToFocus.current,
    )
    typeToFocus.current = null
    row?.focus()
  }, [chosenFromAddress])

  const backButton = useRef<HTMLButtonElement>(null)
  const shouldFocusBack = useRef(false)
  useEffect(() => {
    if (!chosenFromAddress || !shouldFocusBack.current) return
    shouldFocusBack.current = false
    // Back is drawn only where the list is hidden while a type is open.
    if (backButton.current && window.getComputedStyle(backButton.current).display !== 'none') {
      backButton.current.focus()
    }
  }, [chosenFromAddress])

  function choosePerson(chosenUserId: string) {
    changeParameters({
      person: chosenUserId || null,
      tab: null,
      type: null,
      route: null,
      page: null,
    })
  }

  function chooseType(healthType: string | null) {
    // On a phone the list the row sat in is hidden by now: focus goes to Back.
    shouldFocusBack.current = healthType !== null
    changeParameters({ type: healthType, route: null, page: null })
  }

  const personOptions = [
    { value: '', label: t('health.you') },
    ...sharers.map((sharer) => ({ value: sharer.userId, label: sharer.name })),
  ]

  const isEmpty = typeList !== null && typeList.length === 0

  return (
    <>
      <ErrorMessage error={shares.error} />
      {sharers.length > 0 && tab !== 'settings' && (
        <div className="health-toolbar">
          <label>
            <span>{t('health.person')}</span>
            <Select
              value={viewedPerson?.userId ?? ''}
              label={t('health.person')}
              options={personOptions}
              onChange={choosePerson}
            />
          </label>
        </div>
      )}

      {ownerUserId === null && (
        <Tabs
          items={HEALTH_TABS}
          active={tab}
          onSelect={(id) => changeParameters({ tab: id === 'data' ? null : id })}
        />
      )}

      {tab === 'settings' ? (
        <HealthSettings
          userId={userId}
          shares={shares.data ?? []}
          onSharesChanged={shares.reload}
          onErased={() => {
            changeParameters({ type: null, route: null, page: null })
            void typesQuery.reload()
          }}
        />
      ) : (
        <>
          <ErrorMessage error={typesQuery.error} />
          {(isPersonPending || (typesQuery.loading && !typeList)) && <Loading />}
          {typesQuery.error !== null && typeList === null && !typesQuery.loading && (
            <div className="page-actions">
              <button type="button" onClick={() => void typesQuery.reload()}>
                {t('common.tryAgain')}
              </button>
            </div>
          )}
          {isEmpty && viewedPerson && (
            <SettingsSection card title={t('health.title')}>
              <SettingsEmpty>{t('health.personEmpty', { name: viewedPerson.name })}</SettingsEmpty>
            </SettingsSection>
          )}
          {isEmpty && !viewedPerson && (
            <SettingsSection card title={t('health.emptyTitle')} description={t('health.emptyBody')}>
              <div className="page-actions">
                <Link className="button" to="/settings/devices">
                  {t('health.emptyAction')}
                </Link>
              </div>
            </SettingsSection>
          )}
          {typeList && typeList.length > 0 && (
            <div className={chosenFromAddress ? 'health-layout has-type' : 'health-layout'}>
              <div className="health-types-pane">
                <HealthTypeList
                  healthTypes={typeList}
                  chosenType={chosenType?.healthType ?? null}
                  onChoose={(healthType) => chooseType(healthType)}
                />
              </div>
              {chosenType && (
                <div className="health-detail">
                  <button
                    ref={backButton}
                    type="button"
                    className="button-quiet health-back"
                    onClick={() => {
                      typeToFocus.current = chosenType.healthType
                      chooseType(null)
                    }}
                  >
                    <ArrowLeftIcon size={16} /> {t('health.back')}
                  </button>
                  <HealthTypePanel
                    key={`${ownerUserId ?? ''}:${chosenType.healthType}`}
                    chosenType={chosenType}
                    ownerUserId={ownerUserId}
                    range={range}
                    period={period}
                    today={today}
                    onRange={(nextRange) => changeParameters({ range: nextRange === DEFAULT_RANGE ? null : nextRange })}
                    onPeriod={(nextPeriod) => changeParameters({ period: nextPeriod })}
                    onErased={() => {
                      chooseType(null)
                      void typesQuery.reload()
                    }}
                  />
                  {ownerUserId === null && (
                    <HealthSamples
                      key={chosenType.healthType}
                      chosenType={chosenType}
                      routeId={sampleRouteId}
                      onRoute={(healthSampleId) => changeParameters({ route: healthSampleId })}
                    />
                  )}
                </div>
              )}
            </div>
          )}
        </>
      )}
    </>
  )
}
