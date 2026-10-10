import { useMemo, useState } from 'react'

import { formatMoney } from './common'
import {
  CHART_BOTTOM,
  CHART_HEIGHT,
  CHART_TOP,
  ChartScale,
  ChartTooltip,
  axisLabelsFor,
  axisWidthFor,
  chartPointerHandlers,
  chartScale,
  compact,
  dayLabel,
  daysBetween,
  roundedTop,
  useChartHover,
  useWidth,
} from './seriesChart'
import { useTranslation } from '../i18n/i18n'

// The usage above its table: what the agents spent, drawn. By day it is a
// column a day across the range, the tokens stacked by what they were --
// fresh input, cached input, output -- so a day that cost more because
// nothing was cached looks different from one that simply did more; by a
// kind, a model, an agent or a mailbox it is a bar each, largest first. The
// same numbers as the table below, so the table stays the record and this
// is the shape of it.

export type UsageChartRow = {
  key: string
  cost: number
  currency: string
  totals: {
    promptTokens: number
    completionTokens: number
    cacheReadTokens: number
    cacheWriteTokens: number
    calls: number
  }
}

type Metric = 'tokens' | 'cost' | 'calls'

// The parts a column of tokens is stacked from, bottom first.
type Part = 'input' | 'cached' | 'output'
const PARTS: Part[] = ['input', 'cached', 'output']

const HEIGHT = CHART_HEIGHT
const TOP = CHART_TOP
const BOTTOM = CHART_BOTTOM
const RANKED = 8

function partsOf(row: UsageChartRow | undefined): Record<Part, number> {
  if (!row) return { input: 0, cached: 0, output: 0 }
  return {
    input: row.totals.promptTokens,
    cached: row.totals.cacheReadTokens + row.totals.cacheWriteTokens,
    output: row.totals.completionTokens,
  }
}

// dailyScale is the columns' scale: round steps from zero, and whole ones
// for calls and tokens, which come one at a time. Quarters of a top of 5 put
// gridlines at 1.25 and 3.75, and labels rounded to whole calls sat beside
// lines that were not at them.
function dailyScale(values: number[], metric: Metric): ChartScale {
  const scale = chartScale(values)
  const step = scale.grid.length > 1 ? scale.grid[1] - scale.grid[0] : 1
  if (metric === 'cost' || step >= 1) return scale
  const ceiling = Math.max(1, Math.ceil(Math.max(0, ...values)))
  return { floor: 0, ceiling, grid: Array.from({ length: ceiling + 1 }, (_, index) => index) }
}

function measure(row: UsageChartRow | undefined, metric: Metric): number {
  if (!row) return 0
  if (metric === 'cost') return row.cost
  if (metric === 'calls') return row.totals.calls
  const parts = partsOf(row)
  return parts.input + parts.cached + parts.output
}

export function UsageChart({
  rows,
  by,
  since,
  until,
  label,
}: {
  rows: UsageChartRow[]
  by: string
  since: string
  until: string
  label: (key: string) => string
}) {
  const { t } = useTranslation()
  const [metric, setMetric] = useState<Metric>('tokens')
  const [holder, width] = useWidth()
  const [hovered, setHovered] = useChartHover(holder)
  const currency = rows.find((row) => row.currency)?.currency || 'USD'
  const isDaily = by === 'day'

  // The columns or bars: every day of the range, or the largest groups.
  const items = useMemo(() => {
    if (isDaily) {
      const byDay = new Map(rows.map((row) => [row.key, row]))
      const first = since || rows[0]?.key || ''
      const last = until || rows[rows.length - 1]?.key || ''
      return daysBetween(first, last).map((key) => ({ key, row: byDay.get(key) }))
    }
    return [...rows]
      .sort((left, right) => measure(right, metric) - measure(left, metric))
      .slice(0, RANKED)
      .map((row) => ({ key: row.key, row: row as UsageChartRow | undefined }))
  }, [rows, isDaily, since, until, metric])

  const values = items.map((item) => measure(item.row, metric))
  // Columns over time get a round scale with gridlines on it; bars ranked
  // against each other are scaled to the largest, which fills its track.
  const scale = dailyScale(values, metric)
  const ceiling = isDaily ? scale.ceiling : Math.max(1e-9, ...values)
  const sum = rows.reduce((total, row) => total + measure(row, metric), 0)
  const format = (value: number) => (metric === 'cost' ? formatMoney(value, currency) : compact(value))
  const axisLabels = axisLabelsFor(scale.grid, (value, precision) =>
    metric === 'cost' ? formatMoney(value, currency) : compact(value, precision),
  )
  const axisWidth = axisWidthFor(axisLabels)

  const metrics: { id: Metric; label: string }[] = [
    { id: 'tokens', label: t('usageChart.tokens') },
    { id: 'cost', label: t('usageChart.cost') },
    { id: 'calls', label: t('usageChart.calls') },
  ]
  const partLabel: Record<Part, string> = {
    input: t('usageChart.input'),
    cached: t('usageChart.cached'),
    output: t('usageChart.output'),
  }

  return (
    <div className="usage-chart">
      <div className="usage-chart-head">
        <div>
          <div className="usage-chart-total">{format(sum)}</div>
          <div className="muted usage-chart-caption">
            {isDaily
              ? t('usageChart.overRange')
              : t('usageChart.largest', { count: String(Math.min(RANKED, rows.length)) })}
          </div>
        </div>
        <div className="segmented" role="group" aria-label={t('usageChart.measure')}>
          {metrics.map((choice) => (
            <button
              key={choice.id}
              type="button"
              className={metric === choice.id ? 'active' : ''}
              aria-pressed={metric === choice.id}
              onClick={() => setMetric(choice.id)}
            >
              {choice.label}
            </button>
          ))}
        </div>
      </div>
      <div className="usage-chart-plot" ref={holder}>
        {width > 0 &&
          (isDaily ? (
            <DailyColumns
              items={items}
              metric={metric}
              scale={scale}
              axisLabels={axisLabels}
              axisWidth={axisWidth}
              width={width}
              format={format}
              hovered={hovered}
              onHover={setHovered}
            />
          ) : (
            <RankedBars items={items} metric={metric} ceiling={ceiling} format={format} label={label} />
          ))}
        {isDaily && hovered !== null && items[hovered] && width > 0 && (
          <DayTooltip
            item={items[hovered]}
            index={hovered}
            count={items.length}
            width={width}
            axisWidth={axisWidth}
            currency={currency}
            partLabel={partLabel}
          />
        )}
      </div>
      {metric === 'tokens' && (
        <div className="usage-chart-legend">
          {PARTS.map((part) => (
            <span key={part}>
              <i className={`usage-chart-swatch ${part}`} />
              {partLabel[part]}
            </span>
          ))}
        </div>
      )}
    </div>
  )
}

function DailyColumns({
  items,
  metric,
  scale: { ceiling, grid },
  axisLabels,
  axisWidth,
  width,
  format,
  hovered,
  onHover,
}: {
  items: { key: string; row?: UsageChartRow }[]
  metric: Metric
  scale: ChartScale
  axisLabels: string[]
  axisWidth: number
  width: number
  format: (value: number) => string
  hovered: number | null
  onHover: (index: number | null) => void
}) {
  const plotWidth = Math.max(40, width - axisWidth)
  const plotHeight = HEIGHT - TOP - BOTTOM
  const slot = plotWidth / Math.max(1, items.length)
  const columnWidth = Math.max(2, Math.min(28, slot * 0.62))
  const heightOf = (value: number) => (value / ceiling) * plotHeight
  const labelEvery = Math.max(1, Math.ceil(items.length / Math.max(2, Math.floor(plotWidth / 64))))

  // The drawing is scaled to the width it is shown at, so a pointer is
  // turned back into the drawing's own units first.
  const pointAt = (clientX: number, element: SVGSVGElement) => {
    const bounds = element.getBoundingClientRect()
    const x = ((clientX - bounds.left) * width) / Math.max(1, bounds.width)
    const index = Math.floor((x - axisWidth) / slot)
    onHover(index >= 0 && index < items.length ? index : null)
  }

  return (
    <svg
      viewBox={`0 0 ${width} ${HEIGHT}`}
      role="img"
      aria-label={items.map((item) => `${item.key}: ${format(measure(item.row, metric))}`).join(', ')}
      {...chartPointerHandlers(pointAt, onHover)}
    >
      {grid.map((value, index) => {
        const y = TOP + plotHeight - heightOf(value)
        return (
          <g key={value}>
            <line className="usage-chart-grid" x1={axisWidth} x2={width} y1={y} y2={y} />
            <text className="usage-chart-axis" x={axisWidth - 8} y={y} textAnchor="end" dominantBaseline="middle">
              {axisLabels[index]}
            </text>
          </g>
        )
      })}
      {items.map((item, index) => {
        const x = axisWidth + index * slot + (slot - columnWidth) / 2
        const isDimmed = hovered !== null && hovered !== index
        const segments: { part: Part | 'single'; value: number }[] =
          metric === 'tokens'
            ? PARTS.map((part) => ({ part, value: partsOf(item.row)[part] }))
            : [{ part: 'single', value: measure(item.row, metric) }]
        let base = TOP + plotHeight
        const drawn = segments.filter((segment) => segment.value > 0)
        return (
          <g key={item.key} className={isDimmed ? 'usage-chart-dim' : ''}>
            {drawn.map((segment, position) => {
              const height = Math.max(1, heightOf(segment.value))
              base -= height
              const isTop = position === drawn.length - 1
              const radius = Math.min(4, columnWidth / 2, height)
              return (
                <path
                  key={segment.part}
                  className={`usage-chart-column ${segment.part}`}
                  d={
                    isTop
                      ? roundedTop(x, base, columnWidth, height, radius)
                      : `M${x},${base}h${columnWidth}v${height}h${-columnWidth}z`
                  }
                />
              )
            })}
            {index % labelEvery === 0 && (
              <text
                className="usage-chart-axis"
                x={x + columnWidth / 2}
                y={HEIGHT - 6}
                textAnchor={index + labelEvery >= items.length ? 'end' : 'middle'}
              >
                {dayLabel(item.key)}
              </text>
            )}
          </g>
        )
      })}
    </svg>
  )
}

function RankedBars({
  items,
  metric,
  ceiling,
  format,
  label,
}: {
  items: { key: string; row?: UsageChartRow }[]
  metric: Metric
  ceiling: number
  format: (value: number) => string
  label: (key: string) => string
}) {
  return (
    <div className="usage-chart-ranked">
      {items.map((item) => {
        const value = measure(item.row, metric)
        const parts = partsOf(item.row)
        const segments =
          metric === 'tokens'
            ? PARTS.map((part) => ({ part, value: parts[part] }))
            : [{ part: 'single' as const, value }]
        return (
          <div className="usage-chart-rank" key={item.key}>
            <span className="usage-chart-rank-name" title={label(item.key)}>
              {label(item.key)}
            </span>
            <span className="usage-chart-rank-track">
              {segments.map((segment) =>
                segment.value > 0 ? (
                  <span
                    key={segment.part}
                    className={`usage-chart-rank-bar ${segment.part}`}
                    style={{ width: `${(segment.value / ceiling) * 100}%` }}
                  />
                ) : null,
              )}
            </span>
            <span className="usage-chart-rank-value">{format(value)}</span>
          </div>
        )
      })}
    </div>
  )
}

function DayTooltip({
  item,
  index,
  count,
  width,
  axisWidth,
  currency,
  partLabel,
}: {
  item: { key: string; row?: UsageChartRow }
  index: number
  count: number
  width: number
  axisWidth: number
  currency: string
  partLabel: Record<Part, string>
}) {
  const { t } = useTranslation()
  const slot = (width - axisWidth) / Math.max(1, count)
  const center = axisWidth + index * slot + slot / 2
  const parts = partsOf(item.row)
  return (
    <ChartTooltip center={center} width={width}>
      <div className="usage-chart-tooltip-day">{dayLabel(item.key)}</div>
      {/* The column's whole height, which the parts under it add up to. */}
      <div className="usage-chart-tooltip-line">
        <span>{t('usageChart.tokens')}</span>
        <strong>{compact(parts.input + parts.cached + parts.output)}</strong>
      </div>
      {PARTS.map((part) => (
        <div key={part} className="usage-chart-tooltip-line">
          <i className={`usage-chart-swatch ${part}`} />
          <span>{partLabel[part]}</span>
          <strong>{compact(parts[part])}</strong>
        </div>
      ))}
      <div className="usage-chart-tooltip-line">
        <span>{t('usageChart.cost')}</span>
        <strong>{formatMoney(item.row?.cost ?? 0, currency)}</strong>
      </div>
      <div className="usage-chart-tooltip-line">
        <span>{t('usageChart.calls')}</span>
        <strong>{item.row?.totals.calls ?? 0}</strong>
      </div>
    </ChartTooltip>
  )
}
