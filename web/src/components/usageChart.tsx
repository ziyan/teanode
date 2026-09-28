import { useLayoutEffect, useMemo, useRef, useState } from 'react'

import { formatMoney } from './common'
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

const HEIGHT = 220
const AXIS_WIDTH = 52
const TOP = 12
const BOTTOM = 24
const RANKED = 8

function partsOf(row: UsageChartRow | undefined): Record<Part, number> {
  if (!row) return { input: 0, cached: 0, output: 0 }
  return {
    input: row.totals.promptTokens,
    cached: row.totals.cacheReadTokens + row.totals.cacheWriteTokens,
    output: row.totals.completionTokens,
  }
}

function measure(row: UsageChartRow | undefined, metric: Metric): number {
  if (!row) return 0
  if (metric === 'cost') return row.cost
  if (metric === 'calls') return row.totals.calls
  const parts = partsOf(row)
  return parts.input + parts.cached + parts.output
}

// niceCeiling is the top of the scale: the largest value rounded up to 1, 2
// or 5 of its power of ten, so the gridlines fall on round numbers.
function niceCeiling(value: number): number {
  if (value <= 0) return 1
  const power = 10 ** Math.floor(Math.log10(value))
  for (const step of [1, 2, 5, 10]) {
    if (value <= step * power) return step * power
  }
  return 10 * power
}

// daysBetween is every day from the first to the last, as the keys the
// usage is grouped by, so a day nothing was spent on is a gap and not
// missing from the axis.
function daysBetween(first: string, last: string): string[] {
  const days: string[] = []
  const start = new Date(first + 'T00:00:00')
  const end = new Date(last + 'T00:00:00')
  if (Number.isNaN(start.getTime()) || Number.isNaN(end.getTime()) || end < start) return days
  for (let day = start; day <= end && days.length < 400; day = new Date(day.getTime() + 86_400_000)) {
    const month = String(day.getMonth() + 1).padStart(2, '0')
    const date = String(day.getDate()).padStart(2, '0')
    days.push(`${day.getFullYear()}-${month}-${date}`)
  }
  return days
}

function dayLabel(key: string): string {
  const day = new Date(key + 'T00:00:00')
  if (Number.isNaN(day.getTime())) return key
  return day.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

// useWidth is the width the chart has, followed as the page is resized. Read
// at once as well as observed: an observer reports only when the page is
// painted, and a page opened in a tab nobody is looking at would otherwise
// draw nothing until it was.
function useWidth(): [React.RefObject<HTMLDivElement | null>, number] {
  const element = useRef<HTMLDivElement | null>(null)
  const [width, setWidth] = useState(0)
  useLayoutEffect(() => {
    if (!element.current) return
    setWidth(Math.floor(element.current.getBoundingClientRect().width))
    const observer = new ResizeObserver((entries) => setWidth(Math.floor(entries[0].contentRect.width)))
    observer.observe(element.current)
    return () => observer.disconnect()
  }, [])
  return [element, width]
}

// compact is a count in a few characters, to the billions: the tokens of a
// month of reading run past a thousand million.
function compact(value: number): string {
  const size = Math.abs(value)
  if (size >= 1e9) return `${(value / 1e9).toFixed(size >= 1e10 ? 0 : 1)}B`
  if (size >= 1e6) return `${(value / 1e6).toFixed(size >= 1e7 ? 0 : 1)}M`
  if (size >= 1e3) return `${(value / 1e3).toFixed(size >= 1e4 ? 0 : 1)}k`
  return String(Math.round(value))
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
  const [hovered, setHovered] = useState<number | null>(null)
  const [holder, width] = useWidth()
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
  const ceiling = isDaily ? niceCeiling(Math.max(0, ...values)) : Math.max(1e-9, ...values)
  const sum = rows.reduce((total, row) => total + measure(row, metric), 0)
  const format = (value: number) => (metric === 'cost' ? formatMoney(value, currency) : compact(value))

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
              ceiling={ceiling}
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
  ceiling,
  width,
  format,
  hovered,
  onHover,
}: {
  items: { key: string; row?: UsageChartRow }[]
  metric: Metric
  ceiling: number
  width: number
  format: (value: number) => string
  hovered: number | null
  onHover: (index: number | null) => void
}) {
  const plotWidth = Math.max(40, width - AXIS_WIDTH)
  const plotHeight = HEIGHT - TOP - BOTTOM
  const slot = plotWidth / Math.max(1, items.length)
  const columnWidth = Math.max(2, Math.min(28, slot * 0.62))
  const scale = (value: number) => (value / ceiling) * plotHeight
  const labelEvery = Math.max(1, Math.ceil(items.length / Math.max(2, Math.floor(plotWidth / 64))))
  const grid = [0, 0.25, 0.5, 0.75, 1]

  // The drawing is scaled to the width it is shown at, so a pointer is
  // turned back into the drawing's own units first.
  const pointAt = (clientX: number, element: SVGSVGElement) => {
    const bounds = element.getBoundingClientRect()
    const x = ((clientX - bounds.left) * width) / Math.max(1, bounds.width)
    const index = Math.floor((x - AXIS_WIDTH) / slot)
    onHover(index >= 0 && index < items.length ? index : null)
  }

  return (
    <svg
      viewBox={`0 0 ${width} ${HEIGHT}`}
      role="img"
      aria-label={items.map((item) => `${item.key}: ${format(measure(item.row, metric))}`).join(', ')}
      onPointerMove={(event) => pointAt(event.clientX, event.currentTarget)}
      onPointerLeave={() => onHover(null)}
    >
      {grid.map((fraction) => {
        const y = TOP + plotHeight - fraction * plotHeight
        return (
          <g key={fraction}>
            <line className="usage-chart-grid" x1={AXIS_WIDTH} x2={width} y1={y} y2={y} />
            <text className="usage-chart-axis" x={AXIS_WIDTH - 8} y={y} textAnchor="end" dominantBaseline="middle">
              {format(ceiling * fraction)}
            </text>
          </g>
        )
      })}
      {items.map((item, index) => {
        const x = AXIS_WIDTH + index * slot + (slot - columnWidth) / 2
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
              const height = Math.max(1, scale(segment.value))
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

// roundedTop is a column with its top corners rounded and its foot square,
// so a stack reads as one column rather than a pile of pills.
function roundedTop(x: number, y: number, width: number, height: number, radius: number): string {
  return [
    `M${x},${y + height}`,
    `V${y + radius}`,
    `Q${x},${y} ${x + radius},${y}`,
    `H${x + width - radius}`,
    `Q${x + width},${y} ${x + width},${y + radius}`,
    `V${y + height}`,
    'Z',
  ].join(' ')
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
  currency,
  partLabel,
}: {
  item: { key: string; row?: UsageChartRow }
  index: number
  count: number
  width: number
  currency: string
  partLabel: Record<Part, string>
}) {
  const { t } = useTranslation()
  const slot = (width - AXIS_WIDTH) / Math.max(1, count)
  const center = AXIS_WIDTH + index * slot + slot / 2
  // Kept inside the chart: flipped to the left of the column past halfway.
  // In proportions, since the drawing is scaled to the width it is shown at.
  const isLeft = center > width / 2
  const parts = partsOf(item.row)
  return (
    <div
      className={`usage-chart-tooltip ${isLeft ? 'left' : 'right'}`}
      style={isLeft ? { right: `${((width - center) / width) * 100}%` } : { left: `${(center / width) * 100}%` }}
      role="status"
    >
      <div className="usage-chart-tooltip-day">{dayLabel(item.key)}</div>
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
    </div>
  )
}
