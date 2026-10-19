import { useMemo, useState } from 'react'

import { graphql } from '../../api'
import { ErrorMessage, Loading } from '../../components/common'
import { ConfirmDialog } from '../../components/dialog'
import { MoreIcon } from '../../components/icons'
import { MenuButton } from '../../components/menuButton'
import { SettingsEmpty, SettingsSection } from '../../components/settingsList'
import { ChartBand, ChartSeries, SeriesChart, compact, dayLabel, daysBetween } from '../../components/seriesChart'
import { useToast } from '../../components/toast'
import { useQuery } from '../../components/useQuery'
import { Key, useTranslation } from '../../i18n/i18n'
import {
  ERASE_HEALTH_DATA,
  HealthPeriod,
  HealthSummary,
  HealthType,
  PERIODS,
  PeriodID,
  RANGES,
  RangeID,
  READ_HEALTH_SUMMARY,
  SLEEP_STAGES,
  amountText,
  categorySecondsOf,
  chartDays,
  displayUnit,
  durationText,
  numberText,
  numbersOf,
  parseDay,
  quantityText,
  shownValue,
  stageName,
  summarizedDayCount,
  summaryValue,
  typeName,
  wholeNumberText,
} from './healthApi'

// One health type drawn: bars for what adds up (steps, energy, time), a line
// with a band from the lowest to the highest reading for what is read as a
// level (heart rate, weight), and the night's stages stacked for sleep. The
// range, the period and the type are the page's, in the address.

// What a query was asked, kept with its answer. The previous answer stays on
// screen, dimmed, while the next is on its way, and it is drawn over the days
// and period it was asked for, not the ones the buttons say now.
type AnsweredSummary = {
  requestKey: string
  fromDay: string
  toDay: string
  period: PeriodID
  isCut: boolean
  mostPeriods: number
  summary: HealthSummary
}

// The part of a period's amount that is one day's: a week of steps is drawn as
// the steps a day of it, as the Health app does, so a week and a day read on
// one scale.
const PER_DAY_KINDS = ['sum', 'duration']

// The unit a chart of time is drawn in, in seconds: a night in hours, a
// workout in minutes.
const SLEEP_CHART_SECONDS = 3600
const WORKOUT_CHART_SECONDS = 60

// A distance chart is drawn in kilometers once its tallest column is this
// many meters, as the headline is.
const KILOMETER_AXIS_FROM_METERS = 1000

export function HealthTypePanel({
  chosenType,
  ownerUserId,
  range,
  period,
  today,
  onRange,
  onPeriod,
  onErased,
}: {
  chosenType: HealthType
  // Whose data it is when it is somebody sharing it; null for the person's own.
  ownerUserId: string | null
  range: RangeID
  period: PeriodID
  today: string
  onRange: (range: RangeID) => void
  onPeriod: (period: PeriodID) => void
  onErased: () => void
}) {
  const { t, plural, language } = useTranslation()
  const toast = useToast()
  const [isErasing, setIsErasing] = useState(false)
  const [isBusy, setIsBusy] = useState(false)
  const [erasingProblem, setErasingProblem] = useState<string | null>(null)

  const requested = chartDays(range, period, today, chosenType.firstDay || null)
  const requestKey = JSON.stringify([ownerUserId, chosenType.healthType, requested.fromDay, requested.toDay, period])
  const summaryQuery = useQuery(
    async (): Promise<AnsweredSummary> => ({
      requestKey,
      fromDay: requested.fromDay,
      toDay: requested.toDay,
      period,
      isCut: requested.isCut,
      mostPeriods: requested.mostPeriods,
      summary: (
        await graphql<{ ReadHealthSummary: HealthSummary }>(READ_HEALTH_SUMMARY, {
          healthType: chosenType.healthType,
          fromDay: requested.fromDay,
          toDay: requested.toDay,
          period,
          ownerUserId,
        })
      ).ReadHealthSummary,
    }),
    [requestKey],
    { refresh: false },
  )
  // The panel is one type of one person's, so whatever it has answered before
  // is that type's. It stays under the buttons while the next answer is read.
  const answered = summaryQuery.data
  const isStale = answered !== null && answered.requestKey !== requestKey
  const isFailed = summaryQuery.error !== null && !summaryQuery.loading && (answered === null || isStale)
  const fromDay = answered?.fromDay ?? requested.fromDay
  const toDay = answered?.toDay ?? requested.toDay
  const drawnPeriod = answered?.period ?? period

  const typeLabel = typeName(t, chosenType.healthType, chosenType.typeName)
  const summaryKind = chosenType.summaryKind
  const unit = displayUnit(chosenType.healthType, chosenType.quantityUnit)
  const periods = useMemo(() => answered?.summary.periods ?? [], [answered])
  const numbers = numbersOf(periods, summaryKind, fromDay, toDay)
  const durationSeconds = chosenType.valueKind === 'workout' ? WORKOUT_CHART_SECONDS : SLEEP_CHART_SECONDS

  const mediumDay = useMemo(() => new Intl.DateTimeFormat(language, { dateStyle: 'medium' }), [language])
  const monthFormat = useMemo(() => new Intl.DateTimeFormat(language, { year: 'numeric', month: 'long' }), [language])
  const shortMonth = useMemo(() => new Intl.DateTimeFormat(language, { month: 'short' }), [language])

  const chart = useMemo(() => {
    const byStart = new Map(periods.map((each) => [each.startDay.slice(0, 10), each]))
    // A day period is drawn over every day of the range, so a day nothing was
    // recorded on is a gap; weeks and months are the ones the server sent.
    const keys = drawnPeriod === 'day' ? daysBetween(fromDay, toDay) : [...byStart.keys()]
    const rows: (HealthPeriod | undefined)[] = keys.map((key) => byStart.get(key))
    // A week's or month's amount is read per day, over the days of it that
    // have a day summary.
    const perDay = (row: HealthPeriod, periodAmount: number) =>
      drawnPeriod !== 'day' && PER_DAY_KINDS.includes(summaryKind)
        ? periodAmount / summarizedDayCount(row, fromDay, toDay)
        : periodAmount
    const valueOf = (row: HealthPeriod | undefined): number | null => {
      if (!row || row.sampleCount === 0) return null
      const periodAmount = summaryValue(row, summaryKind)
      return periodAmount === null ? null : perDay(row, periodAmount)
    }

    const stageIds = SLEEP_STAGES.filter((stage) =>
      periods.some((each) => (categorySecondsOf(each)[stage.id] ?? 0) > 0),
    )
    const isStaged = summaryKind === 'duration' && stageIds.length > 0
    const series: ChartSeries[] = []
    const bands: ChartBand[] = []
    if (isStaged) {
      for (const stage of stageIds) {
        series.push({
          id: stage.id,
          label: stageName(t, stage.id),
          tone: stage.tone,
          shape: 'column',
          values: rows.map((row) => {
            if (!row) return null
            const seconds = categorySecondsOf(row)[stage.id]
            return seconds === undefined ? null : perDay(row, seconds) / durationSeconds
          }),
        })
      }
    } else if (summaryKind === 'average') {
      series.push({
        id: chosenType.healthType,
        label: typeLabel,
        tone: 'output',
        shape: 'line',
        values: rows.map(valueOf),
      })
      bands.push({
        id: 'range',
        tone: 'band',
        lows: rows.map((row) => (row && row.sampleCount > 0 ? row.minimumValue : null)),
        highs: rows.map((row) => (row && row.sampleCount > 0 ? row.maximumValue : null)),
      })
    } else {
      series.push({
        id: chosenType.healthType,
        label: typeLabel,
        tone: 'output',
        shape: 'column',
        values: rows.map((row) => {
          const rowAmount = valueOf(row)
          return rowAmount === null ? null : summaryKind === 'duration' ? rowAmount / durationSeconds : rowAmount
        }),
      })
    }
    const peak = Math.max(0, ...series.flatMap((one) => one.values.filter((each): each is number => each !== null)))
    return { keys, rows, series, bands, isStaged, peak }
  }, [periods, drawnPeriod, fromDay, toDay, summaryKind, chosenType.healthType, typeLabel, durationSeconds, t])

  const formatChartValue = (chartValue: number): string =>
    summaryKind === 'duration'
      ? durationText(chartValue * durationSeconds, t)
      : summaryKind === 'count'
        ? numberText(chartValue, language)
        : quantityText(chartValue, unit, language, t)
  // The axis is written as the figures are: a percentage is the fraction
  // times a hundred, and meters are kilometers once the headline's are.
  const axisDivisor = unit === 'm' && chart.peak >= KILOMETER_AXIS_FROM_METERS ? KILOMETER_AXIS_FROM_METERS : 1
  const axisFormat = (axisValue: number, precision: number): string => {
    if (summaryKind === 'duration') {
      return durationSeconds === WORKOUT_CHART_SECONDS
        ? t('health.minutes', { minutes: compact(axisValue, precision) })
        : t('health.hours', { hours: compact(axisValue, precision) })
    }
    return compact(shownValue(axisValue, unit) / axisDivisor, precision)
  }

  const keyLabel = (key: string): string => (drawnPeriod === 'month' ? shortMonth.format(parseDay(key)) : dayLabel(key))
  const keyTitle = (key: string): string => {
    if (drawnPeriod === 'month') return monthFormat.format(parseDay(key))
    if (drawnPeriod === 'week') {
      const row = chart.rows[chart.keys.indexOf(key)]
      return row
        ? t('health.periodRange', {
            from: mediumDay.format(parseDay(row.startDay)),
            until: mediumDay.format(parseDay(row.endDay)),
          })
        : mediumDay.format(parseDay(key))
    }
    return mediumDay.format(parseDay(key))
  }

  const hasData = chart.rows.some((row) => row !== undefined && row.sampleCount > 0)
  const amount = (amountValue: number | null) => amountText(amountValue, summaryKind, unit, language, t)
  // The first figure is the headline; the rest stand under the chart.
  const figures: { label: Key; text: string }[] =
    summaryKind === 'average'
      ? [
          { label: 'health.average', text: amount(numbers.average) },
          { label: 'health.lowest', text: amount(numbers.lowest) },
          { label: 'health.highest', text: amount(numbers.highest) },
        ]
      : summaryKind === 'count'
        ? [{ label: 'health.total', text: amount(numbers.total) }]
        : [
            { label: 'health.averagePerDay', text: amount(numbers.averagePerDay) },
            { label: 'health.total', text: amount(numbers.total) },
          ]

  async function erase() {
    setIsBusy(true)
    setErasingProblem(null)
    try {
      const answer = await graphql<{ EraseHealthData: { erasedSampleCount: number } }>(ERASE_HEALTH_DATA, {
        healthType: chosenType.healthType,
      })
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
      setIsErasing(false)
      onErased()
    } catch (failure) {
      setErasingProblem(failure instanceof Error && failure.message ? failure.message : t('health.eraseFailed'))
    } finally {
      setIsBusy(false)
    }
  }

  const periodWord = { day: 'health.chartDay', week: 'health.chartWeek', month: 'health.chartMonth' } as const

  // The buttons are always there, whatever the chart under them is doing, so
  // a click does not remove the button it was made with and a failed read
  // can be answered with another choice.
  const controls = (
    <div className="health-controls">
      <div className="segmented" role="group" aria-label={t('health.range')}>
        {RANGES.map((each) => (
          <button
            key={each.id}
            type="button"
            className={range === each.id ? 'active' : ''}
            aria-pressed={range === each.id}
            onClick={() => onRange(each.id)}
          >
            {t(each.label)}
          </button>
        ))}
      </div>
      <div className="segmented" role="group" aria-label={t('health.period')}>
        {PERIODS.map((each) => (
          <button
            key={each.id}
            type="button"
            className={period === each.id ? 'active' : ''}
            aria-pressed={period === each.id}
            onClick={() => onPeriod(each.id)}
          >
            {t(each.label)}
          </button>
        ))}
      </div>
    </div>
  )

  return (
    <SettingsSection
      card
      title={typeLabel}
      action={
        ownerUserId === null ? (
          <MenuButton
            className="icon-action"
            label={t('health.moreMenu', { name: typeLabel })}
            icon={<MoreIcon size={16} />}
            render={(close) => (
              <button
                type="button"
                role="menuitem"
                onClick={() => {
                  close()
                  setErasingProblem(null)
                  setIsErasing(true)
                }}
              >
                {t('health.eraseType', { name: typeLabel })}
              </button>
            )}
          />
        ) : undefined
      }
    >
      <ErrorMessage error={summaryQuery.error} />
      {controls}
      {answered === null && !isFailed ? <Loading /> : null}
      {isFailed ? (
        <div className="health-failed">
          <SettingsEmpty>{t('health.summaryFailed')}</SettingsEmpty>
          <div className="page-actions">
            <button type="button" onClick={() => void summaryQuery.reload()}>
              {t('common.tryAgain')}
            </button>
          </div>
        </div>
      ) : null}
      {answered !== null && !isFailed && !hasData ? <SettingsEmpty>{t('health.noData')}</SettingsEmpty> : null}
      {answered !== null && !isFailed && hasData ? (
        <div className={isStale ? 'health-chart is-stale' : 'health-chart'} aria-busy={isStale}>
          <SeriesChart
            label={t(periodWord[drawnPeriod], { name: typeLabel })}
            keys={chart.keys}
            keyLabel={keyLabel}
            keyTitle={keyTitle}
            series={chart.series}
            bands={chart.bands}
            isStacked={chart.isStaged}
            isFitted={summaryKind === 'average'}
            format={formatChartValue}
            axisFormat={axisFormat}
            headline={figures[0].text}
            caption={t(figures[0].label)}
            tooltipNote={(index) => {
              const row = chart.rows[index]
              if (!row || row.sampleCount === 0) return null
              if (summaryKind === 'average' && row.minimumValue !== null && row.maximumValue !== null) {
                return {
                  label: t('health.rangeTooltip'),
                  text: t('health.valueRange', {
                    from: quantityText(row.minimumValue, unit, language, t),
                    until: quantityText(row.maximumValue, unit, language, t),
                  }),
                }
              }
              if (chart.isStaged) {
                // The night's total is the time asleep, as the headline is:
                // the time awake is a stage of its own, not part of it.
                const asleepChartValue = chart.series.reduce(
                  (sum, one) => sum + (one.id === 'awake' ? 0 : (one.values[index] ?? 0)),
                  0,
                )
                return { label: t('health.total'), text: durationText(asleepChartValue * durationSeconds, t) }
              }
              return null
            }}
          />
          {answered.isCut ? (
            <p className="muted">
              {t(drawnPeriod === 'week' ? 'health.rangeCutWeeks' : 'health.rangeCutDays', {
                count: answered.mostPeriods,
              })}
            </p>
          ) : null}
          {figures.length > 1 ? (
            <dl className="health-numbers">
              {figures.slice(1).map((figure) => (
                <div key={figure.label}>
                  <dt className="muted">{t(figure.label)}</dt>
                  <dd>{figure.text}</dd>
                </div>
              ))}
            </dl>
          ) : null}
        </div>
      ) : null}

      {isErasing && (
        <ConfirmDialog
          title={t('health.eraseTypeTitle', { name: typeLabel })}
          body={t('health.eraseTypeBody', { name: typeLabel })}
          confirmLabel={t('health.eraseConfirm')}
          busy={isBusy}
          error={erasingProblem}
          onClose={() => setIsErasing(false)}
          onConfirm={() => void erase()}
        />
      )}
    </SettingsSection>
  )
}
