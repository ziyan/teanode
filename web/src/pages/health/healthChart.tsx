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
  durationText,
  numberText,
  numbersOf,
  parseDay,
  periodDayCount,
  quantityText,
  stageName,
  summaryValue,
  typeName,
} from './healthApi'

// One health type drawn: bars for what adds up (steps, energy, time), a line
// with a band from the lowest to the highest reading for what is read as a
// level (heart rate, weight), and the night's stages stacked for sleep. The
// range, the period and the type are the page's, in the address.

// What a query was asked, kept with its answer: the previous answer stays
// while the next is on its way, and the bars of another type or range must not
// stand under the new heading.
type Answered = { requestKey: string; summary: HealthSummary }

// The part of a period's amount that is one day's: a week of steps is drawn as
// the steps a day of it, as the Health app does, so a week and a day read on
// one scale.
const PER_DAY_KINDS = ['sum', 'duration']

export function HealthTypePanel({
  type,
  ownerUserId,
  range,
  period,
  today,
  onRange,
  onPeriod,
  onErased,
}: {
  type: HealthType
  // Whose data it is when it is somebody sharing it; null for the person's own.
  ownerUserId: string | null
  range: RangeID
  period: PeriodID
  today: string
  onRange: (range: RangeID) => void
  onPeriod: (period: PeriodID) => void
  onErased: () => void
}) {
  const { t, language } = useTranslation()
  const toast = useToast()
  const [isErasing, setIsErasing] = useState(false)
  const [isBusy, setIsBusy] = useState(false)
  const [erasingProblem, setErasingProblem] = useState<string | null>(null)

  const { fromDay, toDay } = chartDays(range, period, today, type.firstDay || null)
  const requestKey = JSON.stringify([ownerUserId, type.healthType, fromDay, toDay, period])
  const summary = useQuery(
    async (): Promise<Answered> => ({
      requestKey,
      summary: (
        await graphql<{ ReadHealthSummary: HealthSummary }>(READ_HEALTH_SUMMARY, {
          healthType: type.healthType,
          fromDay,
          toDay,
          period,
          ownerUserId,
        })
      ).ReadHealthSummary,
    }),
    [requestKey],
    { refresh: false },
  )
  const answered = summary.data && summary.data.requestKey === requestKey ? summary.data.summary : null

  const name = typeName(t, type.healthType, type.typeName)
  const summaryKind = type.summaryKind
  const unit = type.quantityUnit
  const periods = useMemo(() => answered?.periods ?? [], [answered])
  const numbers = numbersOf(periods, summaryKind, fromDay, toDay)

  const mediumDay = useMemo(() => new Intl.DateTimeFormat(language, { dateStyle: 'medium' }), [language])
  const monthFormat = useMemo(() => new Intl.DateTimeFormat(language, { year: 'numeric', month: 'long' }), [language])
  const shortMonth = useMemo(() => new Intl.DateTimeFormat(language, { month: 'short' }), [language])

  const chart = useMemo(() => {
    const byStart = new Map(periods.map((each) => [each.startDay.slice(0, 10), each]))
    // A day period is drawn over every day of the range, so a day nothing was
    // recorded on is a gap; weeks and months are the ones the server sent.
    const keys = period === 'day' ? daysBetween(fromDay, toDay) : [...byStart.keys()]
    const rows: (HealthPeriod | undefined)[] = keys.map((key) => byStart.get(key))
    const perDay = (row: HealthPeriod, value: number) =>
      period !== 'day' && PER_DAY_KINDS.includes(summaryKind) ? value / periodDayCount(row, fromDay, toDay) : value
    const valueOf = (row: HealthPeriod | undefined): number | null => {
      if (!row || row.sampleCount === 0) return null
      const value = summaryValue(row, summaryKind)
      return value === null ? null : perDay(row, value)
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
            return seconds === undefined ? null : perDay(row, seconds) / 3600
          }),
        })
      }
    } else if (summaryKind === 'average') {
      series.push({
        id: type.healthType,
        label: name,
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
        id: type.healthType,
        label: name,
        tone: 'output',
        shape: 'column',
        values: rows.map((row) => {
          const value = valueOf(row)
          return value === null ? null : summaryKind === 'duration' ? value / 3600 : value
        }),
      })
    }
    return { keys, rows, series, bands, isStaged }
  }, [periods, period, fromDay, toDay, summaryKind, type.healthType, name, t])

  const formatChartValue = (value: number): string =>
    summaryKind === 'duration'
      ? durationText(value * 3600, t)
      : summaryKind === 'count'
        ? numberText(value, language)
        : quantityText(value, unit, language)
  const axisFormat = (value: number, precision: number): string =>
    summaryKind === 'duration' ? t('health.hours', { hours: compact(value, precision) }) : compact(value, precision)

  const keyLabel = (key: string): string => (period === 'month' ? shortMonth.format(parseDay(key)) : dayLabel(key))
  const keyTitle = (key: string): string => {
    if (period === 'month') return monthFormat.format(parseDay(key))
    if (period === 'week') {
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
  const amount = (value: number | null) => amountText(value, summaryKind, unit, language, t)
  // The first figure is the headline; the rest stand under the chart.
  const figures: { label: Key; value: string }[] =
    summaryKind === 'average'
      ? [
          { label: 'health.average', value: amount(numbers.average) },
          { label: 'health.lowest', value: amount(numbers.lowest) },
          { label: 'health.highest', value: amount(numbers.highest) },
        ]
      : summaryKind === 'count'
        ? [{ label: 'health.total', value: amount(numbers.total) }]
        : [
            { label: 'health.averagePerDay', value: amount(numbers.averagePerDay) },
            { label: 'health.total', value: amount(numbers.total) },
          ]

  async function erase() {
    setIsBusy(true)
    setErasingProblem(null)
    try {
      const answer = await graphql<{ EraseHealthData: { erasedSampleCount: number } }>(ERASE_HEALTH_DATA, {
        healthType: type.healthType,
      })
      toast.done(
        answer.EraseHealthData.erasedSampleCount === 1
          ? t('health.erasedOne')
          : t('health.erased', { count: answer.EraseHealthData.erasedSampleCount }),
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
      title={name}
      action={
        ownerUserId === null ? (
          <MenuButton
            className="icon-action"
            label={t('health.moreMenu', { name })}
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
                {t('health.eraseType', { name })}
              </button>
            )}
          />
        ) : undefined
      }
    >
      <ErrorMessage error={summary.error} />
      {summary.loading && !answered ? <Loading /> : null}
      {answered && !hasData ? <SettingsEmpty>{t('health.noData')}</SettingsEmpty> : null}
      {answered && hasData ? (
        <SeriesChart
          label={t(periodWord[period], { name })}
          keys={chart.keys}
          keyLabel={keyLabel}
          keyTitle={keyTitle}
          series={chart.series}
          bands={chart.bands}
          isStacked={chart.isStaged}
          isFitted={summaryKind === 'average'}
          format={formatChartValue}
          axisFormat={axisFormat}
          headline={figures[0].value}
          caption={t(figures[0].label)}
          headAction={controls}
          tooltipNote={(index) => {
            const row = chart.rows[index]
            if (!row || row.sampleCount === 0) return null
            if (summaryKind === 'average' && row.minimumValue !== null && row.maximumValue !== null) {
              return {
                label: t('health.rangeTooltip'),
                text: t('health.periodRange', {
                  from: quantityText(row.minimumValue, unit, language),
                  until: quantityText(row.maximumValue, unit, language),
                }),
              }
            }
            if (chart.isStaged) {
              const total = chart.series.reduce((sum, one) => sum + (one.values[index] ?? 0), 0)
              return { label: t('health.total'), text: durationText(total * 3600, t) }
            }
            return null
          }}
        />
      ) : null}
      {answered && !hasData ? controls : null}
      {answered && hasData && figures.length > 1 ? (
        <dl className="health-numbers">
          {figures.slice(1).map((figure) => (
            <div key={figure.label}>
              <dt className="muted">{t(figure.label)}</dt>
              <dd>{figure.value}</dd>
            </div>
          ))}
        </dl>
      ) : null}

      {isErasing && (
        <ConfirmDialog
          title={t('health.eraseTypeTitle', { name })}
          body={t('health.eraseTypeBody', { name })}
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
