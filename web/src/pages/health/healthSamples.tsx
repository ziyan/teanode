import { useCallback, useMemo, useState } from 'react'

import { graphql } from '../../api'
import { ErrorMessage, Loading } from '../../components/common'
import { Column, DataTable, Range } from '../../components/dataTable'
import { CloseIcon, LocationIcon } from '../../components/icons'
import { LocationMap } from '../../components/locationMap'
import { SettingsSection } from '../../components/settingsList'
import { Tooltip } from '../../components/tooltip'
import { useQuery } from '../../components/useQuery'
import { useTranslation } from '../../i18n/i18n'
import {
  HealthRoute,
  HealthSample,
  HealthSamplePage,
  HealthType,
  LIST_HEALTH_SAMPLES,
  READ_HEALTH_ROUTE,
  SLEEP_VALUE_NAMES,
  addDays,
  durationText,
  quantityText,
  stageName,
  typeName,
  workoutName,
} from './healthApi'

// The readings behind a type, newest first, a page at a time with the page in
// the address, and a workout's route on the same map the Location page draws
// a day's path on. Only the person's own data is listed: somebody sharing
// Health shares its charts, never its readings.

const NO_MAP_PEOPLE: never[] = []
const NO_MAP_PLACES: never[] = []

export function HealthSamples({
  type,
  routeId,
  onRoute,
}: {
  type: HealthType
  routeId: string | null
  onRoute: (healthSampleId: string | null) => void
}) {
  const { t, plural, language } = useTranslation()
  const [range, setRange] = useState<Range | null>(null)
  const onRange = useCallback((next: Range) => setRange(next), [])

  // From a day before the first reading to a day after today, so a reading
  // near midnight in any zone is inside.
  const from = type.firstDay ? `${addDays(type.firstDay.slice(0, 10), -1)}T00:00:00Z` : '2000-01-01T00:00:00Z'
  const until = `${addDays(new Date().toISOString().slice(0, 10), 1)}T00:00:00Z`
  const page = useQuery(
    async () =>
      range
        ? {
            requestKey: JSON.stringify([type.healthType, range.offset, range.limit]),
            answer: (
              await graphql<{ ListHealthSamples: HealthSamplePage }>(LIST_HEALTH_SAMPLES, {
                healthType: type.healthType,
                from,
                to: until,
                limitCount: range.limit,
                offset: range.offset,
              })
            ).ListHealthSamples,
          }
        : null,
    [type.healthType, from, until, range?.offset, range?.limit],
    { refresh: false },
  )
  const shown =
    page.data && range && page.data.requestKey === JSON.stringify([type.healthType, range.offset, range.limit])
      ? page.data.answer
      : null

  const momentFormat = useMemo(
    () => new Intl.DateTimeFormat(language, { dateStyle: 'medium', timeStyle: 'short' }),
    [language],
  )
  const name = typeName(t, type.healthType, type.typeName)

  const secondsOf = (sample: HealthSample) =>
    Math.max(0, (new Date(sample.endedAt).getTime() - new Date(sample.startedAt).getTime()) / 1000)
  const readingOf = (sample: HealthSample): string => {
    if (sample.quantityValue !== null) {
      return quantityText(sample.quantityValue, sample.quantityUnit || type.quantityUnit, language)
    }
    if (type.valueKind === 'workout') {
      return `${workoutName(t, sample.workoutActivityType)}, ${durationText(secondsOf(sample), t)}`
    }
    if (type.healthType === 'sleepAnalysis' && sample.categoryValue !== null) {
      const stage = SLEEP_VALUE_NAMES[sample.categoryValue]
      return stage
        ? `${stageName(t, stage)}, ${durationText(secondsOf(sample), t)}`
        : durationText(secondsOf(sample), t)
    }
    return durationText(secondsOf(sample), t)
  }

  const columns: Column<HealthSample>[] = [
    {
      key: 'startedAt',
      header: t('health.sampleTime'),
      value: (row) => row.startedAt,
      render: (row) => momentFormat.format(new Date(row.startedAt)),
    },
    {
      key: 'reading',
      header: t('health.sampleValue'),
      value: (row) => readingOf(row),
    },
    {
      key: 'sourceName',
      header: t('health.sampleSource'),
      optional: true,
      truncate: true,
      value: (row) => row.sourceName,
    },
    ...(type.valueKind === 'workout'
      ? [
          {
            key: 'route',
            header: t('health.sampleRoute'),
            width: '5rem',
            render: (row: HealthSample) =>
              row.hasRoute ? (
                <Tooltip label={t('health.showRoute')}>
                  <button
                    type="button"
                    className="icon-action"
                    aria-label={`${readingOf(row)}: ${t('health.showRoute')}`}
                    onClick={() => onRoute(row.healthSampleId)}
                  >
                    <LocationIcon size={16} />
                  </button>
                </Tooltip>
              ) : null,
          },
        ]
      : []),
  ]

  const routedSample = routeId ? (shown?.samples.find((each) => each.healthSampleId === routeId) ?? null) : null

  return (
    <>
      {routeId && (
        <WorkoutRoute
          healthSampleId={routeId}
          title={t('health.routeTitle', {
            name: routedSample ? workoutName(t, routedSample.workoutActivityType) : name,
          })}
          onClose={() => onRoute(null)}
        />
      )}
      <SettingsSection card title={t('health.samplesTitle')}>
        <ErrorMessage error={page.error} />
        <DataTable
          columns={columns}
          rows={shown?.samples ?? []}
          rowKey={(row) => row.healthSampleId}
          loading={!shown}
          remote={{ total: shown?.totalCount ?? 0, onRange }}
          emptyMessage={t('health.noSamples')}
          countLabel={(count) => plural(count, { one: 'health.sampleCountOne', other: 'health.sampleCount' })}
        />
      </SettingsSection>
    </>
  )
}

// WorkoutRoute is a workout's path on the map.
function WorkoutRoute({
  healthSampleId,
  title,
  onClose,
}: {
  healthSampleId: string
  title: string
  onClose: () => void
}) {
  const { t } = useTranslation()
  const route = useQuery(
    async () =>
      (await graphql<{ ReadHealthRoute: HealthRoute | null }>(READ_HEALTH_ROUTE, { healthSampleId })).ReadHealthRoute,
    [healthSampleId],
    { refresh: false },
  )
  const path = useMemo(
    () => (route.data?.routePoints ?? []).map((point) => ({ latitudeDegrees: point[1], longitudeDegrees: point[2] })),
    [route.data],
  )
  return (
    <SettingsSection
      card
      title={title}
      action={
        <Tooltip label={t('common.close')}>
          <button type="button" className="icon-action" aria-label={t('common.close')} onClick={onClose}>
            <CloseIcon size={16} />
          </button>
        </Tooltip>
      }
    >
      <ErrorMessage error={route.error} />
      {route.loading && !route.data ? <Loading /> : null}
      {route.data === null && !route.loading && !route.error ? (
        <p className="muted">{t('health.routeFailed')}</p>
      ) : null}
      {path.length > 0 ? (
        <div className="health-route-map">
          <LocationMap people={NO_MAP_PEOPLE} places={NO_MAP_PLACES} path={path} />
        </div>
      ) : null}
    </SettingsSection>
  )
}
