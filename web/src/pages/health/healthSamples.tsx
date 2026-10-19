import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { graphql } from '../../api'
import { ErrorMessage, Loading } from '../../components/common'
import { Column, DataTable, Range } from '../../components/dataTable'
import { CloseIcon, LocationIcon } from '../../components/icons'
import { LocationMap } from '../../components/locationMap'
import { SettingsSection } from '../../components/settingsList'
import { useToast } from '../../components/toast'
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
  wholeNumberText,
  workoutName,
} from './healthApi'

// The readings behind a type, newest first, a page at a time with the page in
// the address, and a workout's route on the same map the Location page draws
// a day's path on. Only the person's own data is listed: somebody sharing
// Health shares its charts, never its readings.

const NO_MAP_PEOPLE: never[] = []
const NO_MAP_PLACES: never[] = []

export function HealthSamples({
  chosenType,
  routeId,
  onRoute,
}: {
  chosenType: HealthType
  routeId: string | null
  onRoute: (healthSampleId: string | null) => void
}) {
  const { t, plural, language } = useTranslation()
  const [range, setRange] = useState<Range | null>(null)
  const onRange = useCallback((nextRange: Range) => setRange(nextRange), [])

  // From a day before the first reading to a day after today, so a reading
  // near midnight in any zone is inside.
  const fromMoment = chosenType.firstDay
    ? `${addDays(chosenType.firstDay.slice(0, 10), -1)}T00:00:00Z`
    : '2000-01-01T00:00:00Z'
  const untilMoment = `${addDays(new Date().toISOString().slice(0, 10), 1)}T00:00:00Z`
  const samplePage = useQuery(
    async () =>
      range
        ? {
            requestKey: JSON.stringify([chosenType.healthType, range.offset, range.limit]),
            answer: (
              await graphql<{ ListHealthSamples: HealthSamplePage }>(LIST_HEALTH_SAMPLES, {
                healthType: chosenType.healthType,
                from: fromMoment,
                to: untilMoment,
                limitCount: range.limit,
                offset: range.offset,
              })
            ).ListHealthSamples,
          }
        : null,
    [chosenType.healthType, fromMoment, untilMoment, range?.offset, range?.limit],
    { refresh: false },
  )
  const shown =
    samplePage.data &&
    range &&
    samplePage.data.requestKey === JSON.stringify([chosenType.healthType, range.offset, range.limit])
      ? samplePage.data.answer
      : null

  const momentFormat = useMemo(
    () => new Intl.DateTimeFormat(language, { dateStyle: 'medium', timeStyle: 'short' }),
    [language],
  )
  const typeLabel = typeName(t, chosenType.healthType, chosenType.typeName)

  const secondsOf = (sample: HealthSample) =>
    Math.max(0, (new Date(sample.endedAt).getTime() - new Date(sample.startedAt).getTime()) / 1000)
  const readingOf = (sample: HealthSample): string => {
    if (sample.quantityValue !== null) {
      return quantityText(
        sample.quantityValue,
        sample.quantityUnit || chosenType.quantityUnit,
        chosenType.healthType,
        language,
        t,
      )
    }
    if (chosenType.valueKind === 'workout') {
      return `${workoutName(t, sample.workoutActivityType)}, ${durationText(secondsOf(sample), t)}`
    }
    if (chosenType.healthType === 'sleepAnalysis' && sample.categoryValue !== null) {
      const stage = SLEEP_VALUE_NAMES[sample.categoryValue]
      return stage
        ? `${stageName(t, stage)}, ${durationText(secondsOf(sample), t)}`
        : durationText(secondsOf(sample), t)
    }
    // A reading of a kind whose value is a time (a stand hour, a recording)
    // that covers no time says nothing in seconds: show what kind it is.
    return secondsOf(sample) >= 1 ? durationText(secondsOf(sample), t) : typeLabel
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
    ...(chosenType.valueKind === 'workout'
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

  const routedSample = routeId
    ? (shown?.samples.find((listedSample) => listedSample.healthSampleId === routeId) ?? null)
    : null
  const dayFormat = useMemo(() => new Intl.DateTimeFormat(language, { month: 'short', day: 'numeric' }), [language])
  // The workout as the readings list it, with the day: "Running, Oct 17, 36 min".
  const routeLine = routedSample
    ? [
        workoutName(t, routedSample.workoutActivityType),
        dayFormat.format(new Date(routedSample.startedAt)),
        durationText(secondsOf(routedSample), t),
      ].join(', ')
    : t('health.routeTitleUnknown')

  return (
    <>
      {routeId && (
        <WorkoutRoute key={routeId} healthSampleId={routeId} line={routeLine} onClose={() => onRoute(null)} />
      )}
      <SettingsSection card title={t('health.samplesTitle')}>
        <ErrorMessage error={samplePage.error} />
        <DataTable
          columns={columns}
          rows={shown?.samples ?? []}
          rowKey={(row) => row.healthSampleId}
          loading={!shown}
          remote={{ total: shown?.totalCount ?? 0, onRange }}
          emptyMessage={t('health.noSamples')}
          countLabel={(sampleCount) =>
            plural(
              sampleCount,
              { one: 'health.sampleCountOne', other: 'health.sampleCount' },
              { count: wholeNumberText(sampleCount, language) },
            )
          }
        />
      </SettingsSection>
    </>
  )
}

// routeLengthMeters is how far a route goes, along its points. A point is
// [seconds, latitude, longitude, altitude].
function routeLengthMeters(points: { latitudeDegrees: number; longitudeDegrees: number }[]): number {
  const earthRadiusMeters = 6_371_000
  const radians = (degrees: number) => (degrees * Math.PI) / 180
  let meters = 0
  for (let index = 1; index < points.length; index++) {
    const from = points[index - 1]
    const to = points[index]
    const latitudeChange = radians(to.latitudeDegrees - from.latitudeDegrees)
    const longitudeChange = radians(to.longitudeDegrees - from.longitudeDegrees)
    const half =
      Math.sin(latitudeChange / 2) ** 2 +
      Math.cos(radians(from.latitudeDegrees)) *
        Math.cos(radians(to.latitudeDegrees)) *
        Math.sin(longitudeChange / 2) ** 2
    meters += 2 * earthRadiusMeters * Math.asin(Math.min(1, Math.sqrt(half)))
  }
  return meters
}

// WorkoutRoute is a workout's path on the map, titled with the workout's own
// line and, once the path is read, how far it went.
function WorkoutRoute({
  healthSampleId,
  line,
  onClose,
}: {
  healthSampleId: string
  line: string
  onClose: () => void
}) {
  const { t, language } = useTranslation()
  const toast = useToast()
  const closeButton = useRef<HTMLButtonElement>(null)
  // The card opens above the row whose pin was pressed, which on a phone is
  // out of sight: it is scrolled into view, and its close button is focused.
  useEffect(() => {
    closeButton.current?.closest('section')?.scrollIntoView?.({ block: 'nearest' })
    closeButton.current?.focus({ preventScroll: true })
  }, [])
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
  // A route that is not there is told as a failure is, and the card closes.
  const isMissing = route.data === null && !route.loading && !route.error
  useEffect(() => {
    if (!isMissing) return
    toast.failed(t('health.routeFailed'))
    onClose()
    // Once per route found missing.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isMissing])
  const lengthMeters = routeLengthMeters(path)
  const title = lengthMeters > 0 ? `${line}, ${quantityText(lengthMeters, 'm', '', language, t)}` : line
  return (
    <SettingsSection card>
      <div className="health-route-head">
        <h3>{title}</h3>
        <Tooltip label={t('common.close')}>
          <button
            ref={closeButton}
            type="button"
            className="icon-action"
            aria-label={t('common.close')}
            onClick={onClose}
          >
            <CloseIcon size={16} />
          </button>
        </Tooltip>
      </div>
      <ErrorMessage error={route.error} />
      {route.loading && !route.data ? <Loading /> : null}
      {path.length > 0 ? (
        <div className="health-route-map">
          <LocationMap people={NO_MAP_PEOPLE} places={NO_MAP_PLACES} path={path} />
        </div>
      ) : null}
    </SettingsSection>
  )
}
